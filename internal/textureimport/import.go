package textureimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/WishingSkin/WishingSkin/internal/storage"
	"github.com/WishingSkin/WishingSkin/internal/store"
	blob "github.com/WishingSkin/WishingSkin/pkg/storage"
)

type Options struct {
	Dir, Owner, Kind, Model, Category, TextureDir         string
	Recursive, Public, DryRun, SkipReview, SkipDuplicates bool
	Tags                                                  []string
	SettingsKey                                           []byte
	AccountLimitBytes, SiteLimitBytes                     int64
}

type Report struct {
	DryRun      bool   `json:"dryRun"`
	OwnerID     int64  `json:"ownerId"`
	Directory   string `json:"directory"`
	TotalFiles  int    `json:"totalFiles"`
	Imported    int    `json:"imported"`
	Skipped     int    `json:"skipped"`
	Failed      int    `json:"failed"`
	ReusedFiles int    `json:"reusedFiles"`
	ReviewState string `json:"reviewState"`
	Items       []Item `json:"items"`
}

type Item struct {
	File       string `json:"file"`
	Status     string `json:"status"`
	RecordID   string `json:"recordId"`
	FileHash   string `json:"fileHash"`
	Kind       string `json:"kind"`
	Model      string `json:"model"`
	ReusedFile bool   `json:"reusedFile"`
	Bytes      int64  `json:"bytes"`
	Error      string `json:"error"`
}

func Run(ctx context.Context, db *store.Store, options Options) (Report, error) {
	report := Report{DryRun: options.DryRun, Directory: options.Dir, Items: []Item{}}
	options.Owner = strings.TrimSpace(options.Owner)
	options.Category = strings.TrimSpace(options.Category)
	if options.Dir == "" || options.TextureDir == "" || options.Owner == "" {
		return report, errors.New("source directory, texture directory and owner are required")
	}
	if options.Kind == "" {
		options.Kind = "auto"
	}
	if options.Model == "" {
		options.Model = "default"
	}
	if options.Kind != "auto" && options.Kind != "skin" && options.Kind != "cape" || options.Model != "default" && options.Model != "slim" {
		return report, errors.New("invalid texture kind or model")
	}
	tags, err := NormalizeTags(options.Tags)
	if err != nil {
		return report, err
	}
	options.Tags = tags
	source, err := os.OpenRoot(options.Dir)
	if err != nil {
		return report, err
	}
	defer source.Close()
	var files []string
	err = fs.WalkDir(source.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != "." && !options.Recursive {
			return fs.SkipDir
		}
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(path), ".png") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	report.TotalFiles = len(files)
	query := "SELECT id FROM users WHERE email_key=?"
	var owner any = strings.ToLower(options.Owner)
	if id, err := strconv.ParseInt(options.Owner, 10, 64); err == nil {
		query, owner = "SELECT id FROM users WHERE id=?", id
	}
	if err := db.DB.QueryRowContext(ctx, db.Bind(query), owner).Scan(&report.OwnerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return report, errors.New("texture owner not found")
		}
		return report, err
	}
	state := importState{db: db, options: options, ownerID: report.OwnerID, virtualFiles: map[string]bool{}, virtualRecords: map[string]bool{}}
	var dryTx *sql.Tx
	if options.DryRun {
		dryTx, err = db.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
		if err != nil {
			return report, err
		}
		defer dryTx.Rollback()
	}
	var reader RowReader = db.DB
	if dryTx != nil {
		reader = dryTx
	}
	policy, review, err := state.review(ctx, reader)
	if err != nil {
		return report, err
	}
	report.ReviewState = review
	if dryTx != nil {
		state.accountLimit, state.siteLimit, err = EffectiveLimits(ctx, dryTx, options.AccountLimitBytes, options.SiteLimitBytes)
		if err == nil && state.accountLimit > 0 {
			state.accountUsed, err = Usage(ctx, dryTx, db, options.TextureDir, AccountUsageSQL, "t.file_hash", report.OwnerID)
		}
		if err == nil && state.siteLimit > 0 {
			state.siteUsed, err = Usage(ctx, dryTx, db, options.TextureDir, SiteUsageSQL, "t.hash")
		}
		if err != nil {
			return report, err
		}
	}
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		item := Item{File: path, Kind: options.Kind, Model: options.Model}
		err := state.process(ctx, source, dryTx, path, &item, policy, &report.ReviewState)
		switch {
		case err != nil:
			item.Status, item.Error, item.RecordID = "failed", err.Error(), ""
			report.Failed++
		case item.Status == "skipped":
			report.Skipped++
		default:
			item.Status = "imported"
			report.Imported++
			if item.ReusedFile {
				report.ReusedFiles++
			}
		}
		report.Items = append(report.Items, item)
	}
	if report.Failed > 0 {
		return report, fmt.Errorf("%d texture files failed validation or import", report.Failed)
	}
	return report, nil
}

type importState struct {
	db                                             *store.Store
	options                                        Options
	ownerID                                        int64
	accountLimit, siteLimit, accountUsed, siteUsed int64
	virtualFiles, virtualRecords                   map[string]bool
	provider                                       blob.BlobStore
	providerConfig                                 string
}

func (state *importState) review(ctx context.Context, reader RowReader) (ContentPolicy, string, error) {
	policy, err := ReadContentPolicy(ctx, reader)
	if err != nil {
		return policy, "", errors.New("cannot read content policy")
	}
	if !policy.ValidCategory(state.options.Category) {
		return policy, "", errors.New("unknown texture category")
	}
	if policy.ReviewUploads && !state.options.SkipReview {
		return policy, "pending", nil
	}
	return policy, "approved", nil
}

func (state *importState) process(ctx context.Context, source *os.Root, dryTx *sql.Tx, path string, item *Item, policy ContentPolicy, reviewState *string) error {
	name := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if name == "" || utf8.RuneCountInString(name) > 255 || !utf8.ValidString(name) {
		return errors.New("invalid texture name")
	}
	file, err := source.Open(path)
	if err != nil {
		return errors.New("cannot open source PNG")
	}
	data, config, err := DecodePNG(file)
	file.Close()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	item.FileHash, item.Bytes = hex.EncodeToString(sum[:]), int64(len(data))
	checkPolicy := func(policy ContentPolicy) error {
		kind, err := ValidatePNGForPolicy(state.options.Kind, name+filepath.Ext(path), config.Width, config.Height, len(data), policy)
		item.Kind = kind
		if kind == "cape" {
			item.Model = "default"
		}
		return err
	}
	if dryTx != nil {
		if err := checkPolicy(policy); err != nil {
			return err
		}
		return state.importFile(ctx, dryTx, name, item, nil, *reviewState)
	}
	conn, release, err := state.db.LeaseStorageConn(ctx)
	if err != nil {
		return errors.New("cannot acquire storage lease")
	}
	defer release()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return errors.New("cannot start texture transaction")
	}
	defer tx.Rollback()
	var mode string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode); err != nil {
		return errors.New("cannot lock content settings")
	}
	var ownerID int64
	if err := tx.QueryRowContext(ctx, state.db.Bind("SELECT id FROM users WHERE id=? FOR UPDATE"), state.ownerID).Scan(&ownerID); err != nil {
		return errors.New("texture owner no longer available")
	}
	policy, *reviewState, err = state.review(ctx, tx)
	if err != nil {
		return err
	}
	if err := checkPolicy(policy); err != nil {
		return err
	}
	state.accountLimit, state.siteLimit, err = EffectiveLimits(ctx, tx, state.options.AccountLimitBytes, state.options.SiteLimitBytes)
	if err != nil {
		return errors.New("cannot read storage quotas")
	}
	return state.importFile(ctx, tx, name, item, data, *reviewState)
}

func (state *importState) importFile(ctx context.Context, tx *sql.Tx, name string, item *Item, data []byte, review string) error {
	bind := state.db.Bind
	dry := state.options.DryRun
	hash := item.FileHash
	query := "SELECT deleted FROM textures WHERE hash=?"
	if !dry {
		query += " FOR UPDATE"
	}
	var deleted bool
	err := tx.QueryRowContext(ctx, bind(query), hash).Scan(&deleted)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("cannot read texture file record")
	}
	if deleted {
		return errors.New("texture file is disabled")
	}
	item.ReusedFile = err == nil || state.virtualFiles[hash]
	recordKey := hash + ":" + item.Kind
	if state.options.SkipDuplicates {
		if state.virtualRecords[recordKey] {
			item.Status = "skipped"
			return nil
		}
		err := tx.QueryRowContext(ctx, bind("SELECT record_id FROM texture_records WHERE owner_id=? AND file_hash=? AND kind=? AND deleted=FALSE ORDER BY record_id LIMIT 1"), state.ownerID, hash, item.Kind).Scan(&item.RecordID)
		if err == nil {
			item.Status = "skipped"
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return errors.New("cannot check duplicate texture")
		}
	}
	if err := state.checkCapacity(ctx, tx, item); err != nil {
		return err
	}
	if dry {
		state.accountUsed += item.Bytes
		if !item.ReusedFile {
			state.siteUsed += item.Bytes
		}
		state.virtualFiles[hash], state.virtualRecords[recordKey] = true, true
		return nil
	}
	provider, err := state.objectStore(ctx, tx)
	if err != nil {
		return err
	}
	id, err := randomID()
	if err != nil {
		return errors.New("cannot generate texture record ID")
	}
	item.RecordID = id
	if !item.ReusedFile {
		if _, err := tx.ExecContext(ctx, bind("INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES(?,?,?,?,FALSE,FALSE)"), hash, name, item.Kind, item.Model); err != nil {
			return errors.New("cannot insert texture file record")
		}
	}
	if err := RecordIntegrity(ctx, tx, state.db, hash, item.Bytes); err != nil {
		return errors.New("cannot record texture file integrity")
	}
	if _, err := tx.ExecContext(ctx, bind("INSERT INTO texture_records(record_id,file_hash,owner_id,name,description,kind,model,is_public,deleted,created_at) VALUES(?,?,?,?,'',?,?,?,FALSE,?)"), id, hash, state.ownerID, name, item.Kind, item.Model, state.options.Public, time.Now().Unix()); err != nil {
		return errors.New("cannot insert texture record")
	}
	tags, _ := json.Marshal(state.options.Tags)
	if _, err := tx.ExecContext(ctx, bind("INSERT INTO texture_metadata(record_id,category,tags,review_state,review_reason) VALUES(?,?,?,?,'')"), id, state.options.Category, string(tags), review); err != nil {
		return errors.New("cannot insert texture metadata")
	}
	created, err := SaveFile(ctx, provider, state.options.TextureDir, hash, data)
	if err != nil {
		if created && !item.ReusedFile {
			_ = os.Remove(filepath.Join(state.options.TextureDir, hash))
		}
		return errors.New("cannot save texture file")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("cannot commit texture record")
	}
	return nil
}

func (state *importState) checkCapacity(ctx context.Context, tx *sql.Tx, item *Item) error {
	account, site := state.accountUsed, state.siteUsed
	var err error
	if !state.options.DryRun {
		if state.accountLimit > 0 {
			account, err = Usage(ctx, tx, state.db, state.options.TextureDir, AccountUsageSQL, "t.file_hash", state.ownerID)
		}
		if err == nil && state.siteLimit > 0 && !item.ReusedFile {
			site, err = Usage(ctx, tx, state.db, state.options.TextureDir, SiteUsageSQL, "t.hash")
		}
		if err != nil {
			return errors.New("cannot check texture storage usage")
		}
	}
	if state.accountLimit > 0 && item.Bytes > state.accountLimit-account {
		return errors.New("account texture quota exceeded")
	}
	if state.siteLimit > 0 && !item.ReusedFile && item.Bytes > state.siteLimit-site {
		return errors.New("site texture quota exceeded")
	}
	return nil
}

func (state *importState) objectStore(ctx context.Context, tx *sql.Tx) (blob.BlobStore, error) {
	var enabled bool
	err := tx.QueryRowContext(ctx, "SELECT enabled FROM plugins WHERE plugin_id='wishingskin.object-storage'").Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !enabled {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("cannot read object storage state")
	}
	options, sealed, err := ReadObjectStorage(ctx, tx, state.options.SettingsKey)
	if err != nil || sealed == "" {
		return nil, errors.New("cannot read object storage configuration")
	}
	if state.provider == nil || state.providerConfig != sealed {
		provider, err := storage.NewS3(options)
		if err != nil {
			return nil, errors.New("invalid object storage configuration")
		}
		state.provider, state.providerConfig = provider, sealed
	}
	return state.provider, nil
}
