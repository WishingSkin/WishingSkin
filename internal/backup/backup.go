package backup

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	localstorage "github.com/WishingSkin/WishingSkin/internal/storage"
	"github.com/WishingSkin/WishingSkin/internal/store"
)

const formatVersion = 1
const maxRowBytes = 64 << 20
const maxManifestBytes = 32 << 20

var hashName = regexp.MustCompile(`^[a-fA-F0-9]{32,128}$`)
var sha256Text = regexp.MustCompile(`^[a-f0-9]{64}$`)

type TableInfo struct {
	Name   string `json:"name"`
	Rows   int64  `json:"rows"`
	SHA256 string `json:"sha256"`
}

type FileInfo struct {
	Hash             string `json:"hash"`
	Bytes            int64  `json:"bytes"`
	SHA256           string `json:"sha256"`
	SourceUnverified bool   `json:"sourceUnverified,omitempty"`
	contentMatch     bool
}

type Manifest struct {
	FormatVersion   int            `json:"formatVersion"`
	SchemaVersion   int            `json:"schemaVersion"`
	CreatedAt       string         `json:"createdAt"`
	Tables          []TableInfo    `json:"tables"`
	Files           []FileInfo     `json:"files"`
	UnverifiedFiles int            `json:"unverifiedFiles,omitempty"`
	PluginBundles   []PluginBundle `json:"pluginBundles,omitempty"`
	PluginTables    []PluginTable  `json:"pluginTables,omitempty"`
	PluginDialect   string         `json:"pluginDialect,omitempty"`
}

// Report contains counts only; table rows, configuration and keys never enter logs.
type Report struct {
	SchemaVersion      int              `json:"schemaVersion"`
	Rows               map[string]int64 `json:"rows"`
	Files              int              `json:"files"`
	UnverifiedFiles    int              `json:"unverifiedFiles"`
	CredentialsRevoked bool             `json:"credentialsRevoked"`
}

// Export takes a database snapshot and excludes upload/cleanup file writes.
// Other CLI writers (imports/restores) must remain stopped.
// A missing manifest identifies an unfinished backup; existing paths are never reused.
func Export(ctx context.Context, db *store.Store, textureDir, destination string, pluginDirs ...string) (Report, error) {
	release, err := db.LeaseStorage(ctx)
	if err != nil {
		return Report{}, err
	}
	defer release()
	return ExportWithStorageLease(ctx, db, textureDir, destination, pluginDirs...)
}

// ExportWithStorageLease requires the caller to hold db.LeaseStorage.
func ExportWithStorageLease(ctx context.Context, db *store.Store, textureDir, destination string, pluginDirs ...string) (Report, error) {
	var report Report
	if err := independent(textureDir, destination); err != nil {
		return report, err
	}
	path := pluginPath(textureDir, pluginDirs)
	if _, err := os.Stat(path); err == nil {
		if err = independent(path, destination); err != nil {
			return report, err
		}
	} else if !os.IsNotExist(err) {
		return report, err
	}
	tx, err := db.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	if err := checkSchema(ctx, tx); err != nil {
		return report, err
	}
	var pending int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM migration_state WHERE status='pending'").Scan(&pending); err != nil {
		return report, err
	}
	if pending != 0 {
		return report, fmt.Errorf("finish the pending BS migration before backup")
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return report, fmt.Errorf("backup requires a new directory: %w", err)
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return report, err
	}
	defer root.Close()
	if err := root.Mkdir("textures", 0700); err != nil {
		return report, err
	}
	source, err := os.OpenRoot(textureDir)
	if err != nil {
		return report, err
	}
	defer source.Close()
	m := Manifest{FormatVersion: formatVersion, SchemaVersion: store.CurrentSchemaVersion, CreatedAt: time.Now().UTC().Format(time.RFC3339), Files: []FileInfo{}}
	for _, t := range tables {
		info, err := exportTable(ctx, tx, root, t)
		if err != nil {
			return report, err
		}
		m.Tables = append(m.Tables, info)
	}
	integrity, legacyCustom, err := textureSourceIntegrity(ctx, root, m)
	if err != nil {
		return report, err
	}
	// The exported inventory and source digests share the database snapshot.
	seenHashes := map[string]bool{}
	textureIndex := slices.IndexFunc(tables, func(spec table) bool { return spec.name == "textures" })
	if err := walkTable(ctx, root, tables[textureIndex], m.Tables[textureIndex], func(row []*string) error {
		hash := *row[0]
		if !hashName.MatchString(hash) || seenHashes[strings.ToLower(hash)] {
			return fmt.Errorf("invalid stored texture hash")
		}
		seenHashes[strings.ToLower(hash)] = true
		info, err := copyTexture(ctx, source, hash, root, "textures/"+hash, nil)
		if err != nil {
			return err
		}
		info.SourceUnverified, err = verifyTextureSource(info, integrity[hash], legacyCustom)
		if err != nil {
			return err
		}
		if info.SourceUnverified {
			m.UnverifiedFiles++
		}
		m.Files = append(m.Files, info)
		return nil
	}); err != nil {
		return report, err
	}
	if err := exportPlugins(ctx, db, tx, root, pluginPath(textureDir, pluginDirs), &m); err != nil {
		return report, err
	}
	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("finish consistent backup read: %w", err)
	}
	// Publish the completion manifest only after all synced data is available.
	if err := writeJSON(root, "manifest.json", m); err != nil {
		return report, err
	}
	return makeReport(m, false), nil
}

func exportTable(ctx context.Context, tx *sql.Tx, root *os.Root, t table) (TableInfo, error) {
	info := TableInfo{Name: t.name}
	rows, err := tx.QueryContext(ctx, "SELECT "+t.columns+" FROM "+t.name+" ORDER BY "+t.order)
	if err != nil {
		return info, fmt.Errorf("read backup table %s", t.name)
	}
	defer rows.Close()
	f, err := root.OpenFile(t.name+".jsonl", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return info, err
	}
	defer f.Close()
	h := sha256.New()
	encoder := json.NewEncoder(io.MultiWriter(f, h))
	for rows.Next() {
		cells := make([]sql.NullString, len(t.types))
		args := make([]any, len(cells))
		for i := range cells {
			args[i] = &cells[i]
		}
		if err := rows.Scan(args...); err != nil {
			return info, fmt.Errorf("scan backup table %s", t.name)
		}
		row := make([]*string, len(cells))
		for i, cell := range cells {
			if cell.Valid {
				value := cell.String
				if t.types[i] == 'b' {
					switch value {
					case "1", "true":
						value = "true"
					case "0", "false":
						value = "false"
					default:
						return info, fmt.Errorf("invalid boolean in %s", t.name)
					}
				}
				row[i] = &value
			}
		}
		if _, err := rowArgs(t, row); err != nil {
			return info, err
		}
		data, err := json.Marshal(row)
		if err != nil || len(data)+1 >= maxRowBytes {
			return info, fmt.Errorf("backup row exceeds supported size in %s", t.name)
		}
		if err := encoder.Encode(row); err != nil {
			return info, err
		}
		info.Rows++
	}
	if err := rows.Err(); err != nil {
		return info, fmt.Errorf("read backup table %s", t.name)
	}
	info.SHA256 = hex.EncodeToString(h.Sum(nil))
	if err := f.Sync(); err != nil {
		return info, err
	}
	return info, f.Close()
}

func walkTable(ctx context.Context, root *os.Root, t table, info TableInfo, visit func([]*string) error) error {
	f, err := openRegular(root, t.name+".jsonl")
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(f, h))
	scanner.Buffer(make([]byte, 64<<10), maxRowBytes)
	var count int64
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row []*string
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return fmt.Errorf("invalid row in %s", t.name)
		}
		if _, err := rowArgs(t, row); err != nil {
			return err
		}
		if visit != nil {
			if err := visit(row); err != nil {
				return err
			}
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read table %s: %w", t.name, err)
	}
	if count != info.Rows || hex.EncodeToString(h.Sum(nil)) != info.SHA256 {
		return fmt.Errorf("backup table %s count or checksum mismatch", t.name)
	}
	return nil
}

func rowArgs(t table, row []*string) ([]any, error) {
	if len(row) != len(t.types) {
		return nil, fmt.Errorf("invalid column count in %s", t.name)
	}
	args := make([]any, len(row))
	for i, cell := range row {
		kind := t.types[i]
		if cell == nil {
			if kind != 'I' && kind != 'S' {
				return nil, fmt.Errorf("unexpected NULL in %s", t.name)
			}
			continue
		}
		var err error
		switch kind {
		case 'i', 'I':
			args[i], err = strconv.ParseInt(*cell, 10, 64)
		case 'b':
			if *cell != "true" && *cell != "false" {
				return nil, fmt.Errorf("invalid boolean in %s", t.name)
			}
			args[i] = *cell == "true"
		default:
			args[i] = *cell
		}
		if err != nil {
			return nil, fmt.Errorf("invalid integer in %s", t.name)
		}
	}
	return args, nil
}

func checkSchema(ctx context.Context, tx *sql.Tx) error {
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("initialize the target schema with migrate first")
	}
	if version != store.CurrentSchemaVersion {
		return fmt.Errorf("backup requires schema %d; found %d", store.CurrentSchemaVersion, version)
	}
	return nil
}

func openRegular(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("backup file %s is missing or not a regular file", name)
	}
	return root.Open(name)
}

func writeJSON(root *os.Root, name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil || len(data)+1 > maxManifestBytes {
		return fmt.Errorf("backup metadata exceeds supported size")
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

func independent(source, target string) error {
	s, err := filepath.EvalSymlinks(source)
	if err != nil {
		return fmt.Errorf("source directory is unavailable")
	}
	t, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(t))
	if err != nil {
		return fmt.Errorf("target parent must exist")
	}
	t = filepath.Join(parent, filepath.Base(t))
	s, err = filepath.Abs(s)
	if err != nil {
		return err
	}
	s = strings.ToLower(filepath.Clean(s))
	t = strings.ToLower(filepath.Clean(t))
	sep := string(filepath.Separator)
	if s == t || strings.HasPrefix(s, t+sep) || strings.HasPrefix(t, s+sep) {
		return fmt.Errorf("source and target directories must be independent")
	}
	return nil
}

func copyTexture(ctx context.Context, source *os.Root, sourceName string, target *os.Root, targetName string, expected *FileInfo) (FileInfo, error) {
	info := FileInfo{Hash: filepath.Base(sourceName)}
	f, err := openRegular(source, sourceName)
	if err != nil {
		return info, err
	}
	defer f.Close()
	var out *os.File
	w := io.Writer(io.Discard)
	if target != nil {
		out, err = target.OpenFile(targetName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return info, err
		}
		defer out.Close()
		w = out
	}
	h := sha256.New()
	multi := io.MultiWriter(w, h)
	legacyHash := localstorage.ContentHasher(info.Hash)
	if legacyHash != nil && len(info.Hash) != 64 {
		multi = io.MultiWriter(w, h, legacyHash)
	}
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return info, err
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			if _, err := multi.Write(buffer[:n]); err != nil {
				return info, err
			}
			info.Bytes += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return info, readErr
		}
	}
	info.SHA256 = hex.EncodeToString(h.Sum(nil))
	if len(info.Hash) == 64 {
		info.contentMatch = strings.EqualFold(info.Hash, info.SHA256)
	} else if legacyHash != nil {
		info.contentMatch = strings.EqualFold(info.Hash, hex.EncodeToString(legacyHash.Sum(nil)))
	}
	if expected != nil && (info.Bytes != expected.Bytes || info.SHA256 != expected.SHA256) {
		return info, fmt.Errorf("texture %s checksum mismatch", info.Hash)
	}
	if out != nil {
		if err := out.Sync(); err != nil {
			return info, err
		}
		return info, out.Close()
	}
	return info, nil
}

func makeReport(m Manifest, revoked bool) Report {
	r := Report{SchemaVersion: m.SchemaVersion, Rows: map[string]int64{}, Files: len(m.Files), UnverifiedFiles: m.UnverifiedFiles, CredentialsRevoked: revoked}
	for _, t := range m.Tables {
		r.Rows[t.Name] = t.Rows
	}
	return r
}
