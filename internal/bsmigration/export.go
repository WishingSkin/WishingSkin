package bsmigration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type ExportOptions struct {
	// Source is the operator-confirmed version/configuration of the frozen source.
	Source      Source
	TablePrefix string
}

var prefixPattern = regexp.MustCompile(`^[A-Za-z0-9_]*$`)

// ExportMySQL uses only SELECTs in a read-only repeatable-read transaction.
// It never executes SQL dump content, calls the PHP application, or allocates UUIDs.
func ExportMySQL(ctx context.Context, db *sql.DB, opts ExportOptions) (*Snapshot, error) {
	if !prefixPattern.MatchString(opts.TablePrefix) {
		return nil, fmt.Errorf("invalid source table prefix")
	}
	if opts.Source.Instance == "" || !supportedSource(opts.Source) {
		return nil, fmt.Errorf("source instance and confirmed frozen core/plugin versions are required")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin read-only source snapshot: %w", err)
	}
	defer tx.Rollback()
	s := &Snapshot{FormatVersion: FormatVersion, Source: opts.Source, Schema: map[string][]string{}}
	q := func(table string) string { return "`" + opts.TablePrefix + table + "`" }
	rows, err := tx.QueryContext(ctx, "SELECT TABLE_NAME,COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() ORDER BY TABLE_NAME,ORDINAL_POSITION")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			rows.Close()
			return nil, err
		}
		if !strings.HasPrefix(table, opts.TablePrefix) {
			continue
		}
		table = strings.TrimPrefix(table, opts.TablePrefix)
		if !prefixPattern.MatchString(table) {
			rows.Close()
			return nil, fmt.Errorf("unsupported source table identifier")
		}
		s.Schema[table] = append(s.Schema[table], column)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, required := range []string{"users", "players", "textures", "uuid", "options", "user_closet"} {
		if _, ok := s.Schema[required]; !ok {
			return nil, fmt.Errorf("required source table %s is missing", required)
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT uid,email,nickname,password,score,permission,verified FROM "+q("users")+" ORDER BY uid")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u User
		var verified int
		if err := rows.Scan(&u.ID, &u.Email, &u.Nickname, &u.PasswordHash, &u.Score, &u.Permission, &verified); err != nil {
			rows.Close()
			return nil, err
		}
		if verified != 0 && verified != 1 {
			rows.Close()
			return nil, fmt.Errorf("invalid source email verification for user %d", u.ID)
		}
		state := verified == 1
		u.Verified = &state
		s.Users = append(s.Users, u)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// LEFT JOIN deliberately retains duplicate/missing UUID mappings for preflight.
	rows, err = tx.QueryContext(ctx, "SELECT p.pid,p.uid,p.name,COALESCE(u.uuid,''),p.tid_skin,p.tid_cape FROM "+q("players")+" p LEFT JOIN "+q("uuid")+" u ON BINARY u.name=BINARY p.name ORDER BY p.pid,u.id")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Character
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.UUID, &c.SkinID, &c.CapeID); err != nil {
			rows.Close()
			return nil, err
		}
		s.Characters = append(s.Characters, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, "SELECT tid,name,type,hash,public,uploader FROM "+q("textures")+" ORDER BY tid")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t Texture
		var uploader int64
		if err := rows.Scan(&t.ID, &t.Name, &t.Type, &t.Hash, &t.Public, &uploader); err != nil {
			rows.Close()
			return nil, err
		}
		t.Uploader = &uploader
		s.Textures = append(s.Textures, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if _, present := s.Schema["textures_description"]; present {
		rows, err = tx.QueryContext(ctx, "SELECT tid,description FROM "+q("textures_description")+" ORDER BY tid")
		if err != nil {
			return nil, err
		}
		byID := map[int64]int{}
		for i, texture := range s.Textures {
			byID[texture.ID] = i
		}
		seen := map[int64]bool{}
		for rows.Next() {
			var id int64
			var description string
			if err := rows.Scan(&id, &description); err != nil {
				rows.Close()
				return nil, err
			}
			i, ok := byID[id]
			if !ok || seen[id] {
				rows.Close()
				return nil, fmt.Errorf("orphan or duplicate texture description for source texture %d", id)
			}
			seen[id] = true
			s.Textures[i].Description = description
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	rows, err = tx.QueryContext(ctx, "SELECT user_uid,texture_tid,item_name FROM "+q("user_closet")+" ORDER BY user_uid,texture_tid,item_name")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Collection
		var name sql.NullString
		if err := rows.Scan(&c.UserID, &c.TextureID, &name); err != nil {
			rows.Close()
			return nil, err
		}
		if name.Valid {
			c.Name = &name.String
		}
		s.Wardrobe = append(s.Wardrobe, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, "SELECT option_name,option_value FROM "+q("options")+" WHERE option_name IN ('ygg_private_key','ygg_uuid_algorithm')")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			rows.Close()
			return nil, err
		}
		if seen[key] {
			rows.Close()
			return nil, fmt.Errorf("duplicate source signing/UUID option")
		}
		seen[key] = true
		if key == "ygg_private_key" {
			s.SignaturePrivateKey = value
		} else {
			s.Source.UUIDAlgorithm = value
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if !seen["ygg_private_key"] || !seen["ygg_uuid_algorithm"] {
		return nil, fmt.Errorf("source signing key or explicit UUID algorithm is missing")
	}
	s.ScoreRules, err = readScoreRules(ctx, tx, q("options"))
	if err != nil {
		return nil, err
	}
	s.Branding, err = readBranding(ctx, tx, q("options"))
	if err != nil {
		return nil, err
	}
	if _, present := s.Schema["reports"]; present {
		s.LegacyReports, err = readLegacyReports(ctx, tx, q("reports"))
		if err != nil {
			return nil, err
		}
	} else {
		s.Omitted = append(s.Omitted, Omitted{Scope: "legacy_reports", Count: -1, Reason: "Source reports table is missing; history was not checked."})
	}
	if _, present := s.Schema["invitation_codes"]; present {
		columns := map[string]bool{}
		for _, column := range s.Schema["invitation_codes"] {
			columns[column] = true
		}
		for _, column := range []string{"id", "code", "generated_at", "used_by", "used_at"} {
			if !columns[column] {
				return nil, fmt.Errorf("required source invitation_codes column %s is missing", column)
			}
		}
		rows, err = tx.QueryContext(ctx, "SELECT id,CAST(code AS CHAR),CAST(generated_at AS CHAR),used_by,CAST(used_at AS CHAR) FROM "+q("invitation_codes")+" ORDER BY id")
		if err != nil {
			return nil, fmt.Errorf("read source invitation codes: %w", err)
		}
		for rows.Next() {
			var item InvitationCode
			var usedAt sql.NullString
			if err := rows.Scan(&item.ID, &item.Code, &item.GeneratedAt, &item.UsedBy, &usedAt); err != nil {
				rows.Close()
				return nil, fmt.Errorf("read source invitation code row: %w", err)
			}
			if usedAt.Valid {
				item.UsedAt = &usedAt.String
			}
			hash := sha256.Sum256([]byte(item.Code))
			item.CodeHash = hex.EncodeToString(hash[:])
			s.InvitationCodes = append(s.InvitationCodes, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read source invitation codes: %w", err)
		}
		rows.Close()
	}
	selected := map[string]string{
		"users":                "uid,email,nickname,password,score,permission,verified",
		"players":              "pid,uid,name,tid_skin,tid_cape",
		"textures":             "tid,name,type,hash,public,uploader",
		"user_closet":          "user_uid,texture_tid,item_name",
		"textures_description": "tid,description",
		"uuid":                 "id,name,uuid",
		"options":              "option_name,option_value",
		"reports":              "id,tid,uploader,reporter,reason,status,report_at",
		"invitation_codes":     "id,code,generated_at,used_by,used_at",
	}
	tables := make([]string, 0, len(s.Schema))
	for name := range s.Schema {
		tables = append(tables, name)
	}
	sort.Strings(tables)
	for _, table := range tables {
		if _, ok := selected[table]; !ok {
			var count int64
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+q(table)).Scan(&count); err != nil {
				return nil, err
			}
			s.Omitted = append(s.Omitted, Omitted{Scope: "table:" + table, Count: count, Reason: "Outside the supported import scope; contents were not exported."})
		} else {
			for _, column := range s.Schema[table] {
				if !strings.Contains(","+selected[table]+",", ","+column+",") {
					s.Omitted = append(s.Omitted, Omitted{Scope: "column:" + table + "." + column, Count: -1, Reason: "Column is inventoried but not imported."})
				}
			}
		}
	}
	var extraOptions int64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+q("options")+" WHERE option_name NOT IN ('ygg_private_key','ygg_uuid_algorithm','site_name','site_description','announcement')").Scan(&extraOptions); err != nil {
		return nil, err
	}
	s.Omitted = append(s.Omitted, Omitted{Scope: "site_options", Count: extraOptions, Reason: "Branding is preserved as plain text. Registration, mail, URLs and other options are not applied; score rules are inventoried for manual configuration."})
	var orphanUUIDs int64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+q("uuid")+" u LEFT JOIN "+q("players")+" p ON BINARY p.name=BINARY u.name WHERE p.pid IS NULL").Scan(&orphanUUIDs); err != nil {
		return nil, err
	}
	if orphanUUIDs > 0 {
		s.Omitted = append(s.Omitted, Omitted{Scope: "orphan_uuid_mappings", Count: orphanUUIDs, Reason: "UUID mapping has no corresponding current character; not imported."})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s, nil
}
