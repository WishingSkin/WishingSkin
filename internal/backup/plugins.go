package backup

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"wishingskin/internal/runtimeplugin"
	"wishingskin/internal/store"
)

type PluginBundle struct {
	ID     string   `json:"id"`
	Digest string   `json:"digest"`
	Grants []string `json:"grants"`
	Active bool     `json:"active"`
}
type PluginTable struct {
	TableInfo
	Columns    []string `json:"columns"`
	Binary     []bool   `json:"binary"`
	References []string `json:"references,omitempty"`
}

var sqlIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

func pluginPrefix(id string) string {
	h := sha256.Sum256([]byte(id))
	return "plugin_" + hex.EncodeToString(h[:8]) + "_"
}
func quoteColumn(db *store.Store, name string) string {
	if db.Dialect == "mysql" {
		return "`" + name + "`"
	}
	return `"` + name + `"`
}
func pluginPath(texture string, configured []string) string {
	if len(configured) > 0 && configured[0] != "" {
		return configured[0]
	}
	return filepath.Join(texture, ".plugins")
}

// Applied migration checksums select the schema actually present in the backup.
// A retained upgrade archive may include steps that have never run.
type pluginStep struct {
	Version, Step int
	Checksum      string
}

func schemaQueries(pkg runtimeplugin.Package, steps []pluginStep, dialect string) ([]string, error) {
	queries := []string{}
	for _, step := range steps {
		if step.Version < 1 || step.Version > len(pkg.Migrations) {
			return nil, fmt.Errorf("plugin migration package missing")
		}
		migration := pkg.Migrations[step.Version-1]
		statements := migration.Postgres
		if dialect == "mysql" {
			statements = migration.MySQL
		}
		if step.Step < 0 || step.Step >= len(statements) {
			return nil, fmt.Errorf("plugin migration step missing")
		}
		query := statements[step.Step]
		h := sha256.Sum256([]byte(query))
		if hex.EncodeToString(h[:]) != step.Checksum || !strings.Contains(query, "{{prefix}}") {
			return nil, fmt.Errorf("plugin migration checksum mismatch")
		}
		queries = append(queries, query)
	}
	return queries, nil
}
func backupPluginSteps(ctx context.Context, root *os.Root, m Manifest) (map[string][]pluginStep, error) {
	result := map[string][]pluginStep{}
	for i, table := range tables {
		if table.name != "plugin_migrations" {
			continue
		}
		err := walkTable(ctx, root, table, m.Tables[i], func(row []*string) error {
			version, e := strconv.Atoi(*row[1])
			if e != nil {
				return e
			}
			step, e := strconv.Atoi(*row[2])
			if e != nil {
				return e
			}
			result[*row[0]] = append(result[*row[0]], pluginStep{version, step, *row[3]})
			return nil
		})
		return result, err
	}
	return result, nil
}

func exportPlugins(ctx context.Context, db *store.Store, tx *sql.Tx, root *os.Root, path string, m *Manifest) error {
	ledger, err := backupPluginSteps(ctx, root, *m)
	if err != nil {
		return err
	}
	// Always inspect namespaces, including when packages have been removed on disk.
	query := "SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_type='BASE TABLE' ORDER BY table_name"
	if db.Dialect == "mysql" {
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema=DATABASE() AND table_type='BASE TABLE' ORDER BY table_name"
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	allNames := []string{}
	namespace := regexp.MustCompile(`^plugin_[a-f0-9]{16}_`)
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			break
		}
		if namespace.MatchString(name) {
			allNames = append(allNames, name)
		}
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if rowErr != nil {
		return rowErr
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if len(allNames) > 0 || len(ledger) > 0 {
			return fmt.Errorf("plugin packages missing for stored data")
		}
		return nil
	} else if err != nil {
		return err
	}
	manager, err := runtimeplugin.Open(path)
	if err != nil {
		return err
	}
	defer manager.Close()
	selected := map[string]runtimeplugin.Installed{}
	active := map[string]bool{}
	for _, item := range manager.Items() {
		selected[item.Package.Manifest.ID] = item
		active[item.Package.Manifest.ID] = true
	}
	// Uninstalled packages retain data. Include their latest retained schema package.
	archives, err := filepath.Glob(filepath.Join(path, "*", "*.zip"))
	if err != nil {
		return err
	}
	for _, file := range archives {
		data, e := os.ReadFile(file)
		if e != nil {
			return e
		}
		pkg, e := runtimeplugin.Inspect(data)
		if e != nil {
			return e
		}
		id := pkg.Manifest.ID
		if _, e = schemaQueries(pkg, ledger[id], db.Dialect); e != nil {
			continue
		}
		if !active[id] {
			old, exists := selected[id]
			if !exists || len(old.Package.Migrations) > len(pkg.Migrations) || len(old.Package.Migrations) == len(pkg.Migrations) && runtimeplugin.Digest(data) < old.Digest {
				selected[id] = runtimeplugin.Installed{Package: pkg, Digest: runtimeplugin.Digest(data), Grants: pkg.Manifest.Permissions}
			}
		}
	}
	for id := range ledger {
		if _, exists := selected[id]; !exists {
			return fmt.Errorf("plugin schema package missing")
		}
	}
	if len(selected) == 0 && len(allNames) == 0 {
		return nil
	}
	if err = root.Mkdir("plugins", 0700); err != nil {
		return err
	}
	ids := []string{}
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		item := selected[id]
		if _, e := schemaQueries(item.Package, ledger[id], db.Dialect); e != nil {
			return e
		}
		data, e := os.ReadFile(manager.Root(item) + ".zip")
		if e != nil || runtimeplugin.Digest(data) != item.Digest {
			return fmt.Errorf("plugin archive unavailable")
		}
		f, e := root.OpenFile("plugins/"+item.Digest+".zip", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(data)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		m.PluginBundles = append(m.PluginBundles, PluginBundle{ID: id, Digest: item.Digest, Grants: item.Grants, Active: active[id]})
	}
	names := []string{}
	for _, name := range allNames {
		matched := false
		for _, id := range ids {
			if strings.HasPrefix(name, pluginPrefix(id)) {
				names = append(names, name)
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("plugin table has no retained package: %s", name)
		}
	}
	for _, name := range names {
		if !sqlIdentifier.MatchString(name) {
			return fmt.Errorf("unsupported plugin table name")
		}
		info, e := exportPluginTable(ctx, db, tx, root, name)
		if e != nil {
			return e
		}
		m.PluginTables = append(m.PluginTables, info)
	}
	for i := range m.PluginTables {
		query := "SELECT DISTINCT parent.relname FROM pg_constraint c JOIN pg_class child ON child.oid=c.conrelid JOIN pg_namespace n ON n.oid=child.relnamespace JOIN pg_class parent ON parent.oid=c.confrelid WHERE c.contype='f' AND n.nspname=current_schema() AND child.relname=?"
		if db.Dialect == "mysql" {
			query = "SELECT DISTINCT referenced_table_name FROM information_schema.key_column_usage WHERE table_schema=DATABASE() AND table_name=? AND referenced_table_name IS NOT NULL"
		}
		refs, e := tx.QueryContext(ctx, db.Bind(query), m.PluginTables[i].Name)
		if e != nil {
			return e
		}
		for refs.Next() {
			var parent string
			if e = refs.Scan(&parent); e != nil {
				break
			}
			m.PluginTables[i].References = append(m.PluginTables[i].References, parent)
		}
		rowErr := refs.Err()
		refs.Close()
		if e != nil {
			return e
		}
		if rowErr != nil {
			return rowErr
		}
	}
	ordered := []PluginTable{}
	pending := append([]PluginTable{}, m.PluginTables...)
	for len(pending) > 0 {
		progress := false
		for i := 0; i < len(pending); {
			ready := true
			for _, parent := range pending[i].References {
				for _, other := range pending {
					if other.Name == parent && parent != pending[i].Name {
						ready = false
					}
				}
			}
			if ready {
				ordered = append(ordered, pending[i])
				pending = append(pending[:i], pending[i+1:]...)
				progress = true
			} else {
				i++
			}
		}
		if !progress {
			return fmt.Errorf("cyclic plugin table references require a plugin-specific backup")
		}
	}
	m.PluginTables = ordered
	if len(m.PluginTables) > 0 || len(ledger) > 0 {
		m.PluginDialect = db.Dialect
	}
	return nil
}
func exportPluginTable(ctx context.Context, db *store.Store, tx *sql.Tx, root *os.Root, name string) (PluginTable, error) {
	info := PluginTable{TableInfo: TableInfo{Name: name}}
	query := "SELECT column_name,data_type,is_generated FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=? ORDER BY ordinal_position"
	if db.Dialect == "mysql" {
		query = "SELECT column_name,data_type,IF(generation_expression='', 'NEVER', 'ALWAYS') FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? ORDER BY ordinal_position"
	}
	rows, err := tx.QueryContext(ctx, db.Bind(query), name)
	if err != nil {
		return info, err
	}
	for rows.Next() {
		var column, kind, generated string
		if err = rows.Scan(&column, &kind, &generated); err != nil {
			break
		}
		if !sqlIdentifier.MatchString(column) || generated != "NEVER" {
			err = fmt.Errorf("unsupported plugin column")
			break
		}
		info.Columns = append(info.Columns, column)
		info.Binary = append(info.Binary, strings.Contains(kind, "blob") || strings.Contains(kind, "binary") || kind == "bytea")
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return info, err
	}
	if rowErr != nil {
		return info, rowErr
	}
	columns := []string{}
	for _, c := range info.Columns {
		columns = append(columns, quoteColumn(db, c))
	}
	rows, err = tx.QueryContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM "+quoteColumn(db, name))
	if err != nil {
		return info, err
	}
	defer rows.Close()
	f, err := root.OpenFile("plugins/"+name+".jsonl", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return info, err
	}
	defer f.Close()
	h := sha256.New()
	encoder := json.NewEncoder(io.MultiWriter(f, h))
	for rows.Next() {
		raw := make([]sql.RawBytes, len(columns))
		args := make([]any, len(raw))
		for i := range raw {
			args[i] = &raw[i]
		}
		if err = rows.Scan(args...); err != nil {
			return info, err
		}
		record := make([]*string, len(raw))
		for i, v := range raw {
			if v == nil {
				continue
			}
			s := string(v)
			if info.Binary[i] {
				s = base64.StdEncoding.EncodeToString(v)
			} else if !utf8.Valid(v) {
				return info, fmt.Errorf("unsupported non-UTF8 plugin value")
			}
			record[i] = &s
		}
		if row, e := json.Marshal(record); e != nil || len(row)+1 >= maxRowBytes {
			return info, fmt.Errorf("plugin row exceeds supported size")
		}
		if err = encoder.Encode(record); err != nil {
			return info, err
		}
		info.Rows++
	}
	if err = rows.Err(); err != nil {
		return info, err
	}
	info.SHA256 = hex.EncodeToString(h.Sum(nil))
	if err = f.Sync(); err != nil {
		return info, err
	}
	return info, f.Close()
}
func pluginRows(ctx context.Context, root *os.Root, info PluginTable, visit func([]any) error) error {
	f, err := openRegular(root, "plugins/"+info.Name+".jsonl")
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(f, h))
	scanner.Buffer(make([]byte, 64<<10), maxRowBytes)
	var count int64
	for scanner.Scan() {
		if err = ctx.Err(); err != nil {
			return err
		}
		var row []*string
		if json.Unmarshal(scanner.Bytes(), &row) != nil || len(row) != len(info.Columns) {
			return fmt.Errorf("invalid plugin row")
		}
		args := make([]any, len(row))
		for i, v := range row {
			if v == nil {
				continue
			}
			if info.Binary[i] {
				data, e := base64.StdEncoding.DecodeString(*v)
				if e != nil {
					return e
				}
				args[i] = data
			} else {
				args[i] = *v
			}
		}
		if visit != nil {
			if err = visit(args); err != nil {
				return err
			}
		}
		count++
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	if count != info.Rows || hex.EncodeToString(h.Sum(nil)) != info.SHA256 {
		return fmt.Errorf("plugin table checksum mismatch")
	}
	return nil
}
func verifyPlugins(ctx context.Context, root *os.Root, m Manifest) error {
	ledger, err := backupPluginSteps(ctx, root, m)
	if err != nil {
		return err
	}
	if len(m.PluginTables) > 0 && m.PluginDialect != "mysql" && m.PluginDialect != "postgres" {
		return fmt.Errorf("invalid plugin database engine")
	}
	seen := map[string]bool{}
	prefixes := []string{}
	for _, bundle := range m.PluginBundles {
		if !sha256Text.MatchString(bundle.Digest) || seen[bundle.ID] {
			return fmt.Errorf("invalid plugin bundle")
		}
		seen[bundle.ID] = true
		data, err := readBundle(root, bundle)
		if err != nil {
			return err
		}
		pkg, err := runtimeplugin.Inspect(data)
		if err != nil || pkg.Manifest.ID != bundle.ID {
			return fmt.Errorf("invalid plugin archive")
		}
		if len(bundle.Grants) != len(pkg.Manifest.Permissions) {
			return fmt.Errorf("invalid plugin grants")
		}
		for _, grant := range pkg.Manifest.Permissions {
			if !slices.Contains(bundle.Grants, grant) {
				return fmt.Errorf("missing plugin grant")
			}
		}
		if _, err = schemaQueries(pkg, ledger[bundle.ID], m.PluginDialect); err != nil {
			return err
		}
		prefixes = append(prefixes, pluginPrefix(bundle.ID))
	}
	for id := range ledger {
		if !seen[id] {
			return fmt.Errorf("missing plugin schema package")
		}
	}
	seen = map[string]bool{}
	for _, table := range m.PluginTables {
		allowed := false
		for _, prefix := range prefixes {
			allowed = allowed || strings.HasPrefix(table.Name, prefix)
		}
		if !allowed || !sqlIdentifier.MatchString(table.Name) || seen[table.Name] || len(table.Columns) == 0 || len(table.Columns) != len(table.Binary) || table.Rows < 0 || !sha256Text.MatchString(table.SHA256) {
			return fmt.Errorf("invalid plugin table inventory")
		}
		seen[table.Name] = true
		columns := map[string]bool{}
		for _, c := range table.Columns {
			if !sqlIdentifier.MatchString(c) || columns[c] {
				return fmt.Errorf("invalid plugin column")
			}
			columns[c] = true
		}
		if err := pluginRows(ctx, root, table, nil); err != nil {
			return err
		}
	}
	return nil
}
func readBundle(root *os.Root, bundle PluginBundle) ([]byte, error) {
	f, err := openRegular(root, "plugins/"+bundle.Digest+".zip")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, runtimeplugin.MaxPackage+1))
	if err != nil || runtimeplugin.Digest(data) != bundle.Digest {
		return nil, fmt.Errorf("plugin checksum mismatch")
	}
	return data, nil
}
func preparePluginRestore(ctx context.Context, db *store.Store, root *os.Root, m Manifest, textureDir string, configured []string) error {
	if len(m.PluginBundles) == 0 {
		return nil
	}
	if m.PluginDialect != "" && m.PluginDialect != db.Dialect {
		return fmt.Errorf("plugin SQL tables require the original database engine")
	}
	path := pluginPath(textureDir, configured)
	if entries, err := os.ReadDir(path); err == nil && len(entries) > 0 {
		return fmt.Errorf("restore requires an empty plugin directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	manager, err := runtimeplugin.Open(path)
	if err != nil {
		return err
	}
	defer manager.Close()
	ledger, err := backupPluginSteps(ctx, root, m)
	if err != nil {
		return err
	}
	for _, bundle := range m.PluginBundles {
		data, e := readBundle(root, bundle)
		if e != nil {
			return e
		}
		item, e := manager.Stage(data, bundle.Grants)
		if e != nil {
			return e
		}
		steps, e := schemaQueries(item.Package, ledger[bundle.ID], db.Dialect)
		if e != nil {
			return e
		}
		for _, query := range steps {
			if _, e = db.DB.ExecContext(ctx, strings.ReplaceAll(query, "{{prefix}}", pluginPrefix(bundle.ID))); e != nil {
				return fmt.Errorf("plugin schema restore failed")
			}
		}
		if bundle.Active {
			if e = manager.Activate(item); e != nil {
				return e
			}
		}
	}
	return nil
}
func restorePluginTables(ctx context.Context, db *store.Store, tx *sql.Tx, root *os.Root, m Manifest) error {
	// Plugins own their schema and must make migration seed rows idempotent.
	for i := len(m.PluginTables) - 1; i >= 0; i-- {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteColumn(db, m.PluginTables[i].Name)); err != nil {
			return fmt.Errorf("plugin table initialization failed")
		}
	}
	for _, info := range m.PluginTables {
		columns := []string{}
		for _, c := range info.Columns {
			columns = append(columns, quoteColumn(db, c))
		}
		override := ""
		if db.Dialect == "postgres" {
			override = " OVERRIDING SYSTEM VALUE"
		}
		query := "INSERT INTO " + quoteColumn(db, info.Name) + "(" + strings.Join(columns, ",") + ")" + override + " VALUES(" + strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",") + ")"
		if err := pluginRows(ctx, root, info, func(args []any) error {
			_, e := tx.ExecContext(ctx, db.Bind(query), args...)
			if e != nil {
				return fmt.Errorf("plugin table restore failed")
			}
			return nil
		}); err != nil {
			return err
		}
		if db.Dialect == "postgres" {
			for _, column := range info.Columns {
				var sequence sql.NullString
				if err := tx.QueryRowContext(ctx, db.Bind("SELECT pg_get_serial_sequence(?,?)"), info.Name, column).Scan(&sequence); err != nil {
					return err
				}
				if sequence.Valid {
					query := "SELECT setval(?::regclass,COALESCE(MAX(" + quoteColumn(db, column) + "),1),COUNT(*)>0) FROM " + quoteColumn(db, info.Name)
					if _, err := tx.ExecContext(ctx, db.Bind(query), sequence.String); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
