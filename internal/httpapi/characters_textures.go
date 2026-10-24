package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxTextureBytes = 8 << 20
	maxUploadBody   = maxTextureBytes + (64 << 10)
	maxDescription  = 200
)

var (
	characterNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)
	recordIDPattern      = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

type textureRecord struct {
	ID          string `json:"id"`
	FileHash    string `json:"fileHash"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Model       string `json:"model"`
	Public      bool   `json:"public"`
	Owned       bool   `json:"owned"`
}

func (a *Server) createCharacter(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	var body struct {
		Name         string `json:"name"`
		ExpectedCost *int64 `json:"expectedCost"`
	}
	if !decode(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if !characterNamePattern.MatchString(name) {
		failure(w, 400, "角色名须为 3–16 位英文字母、数字或下划线")
		return
	}
	ctx := r.Context()
	tx, err := a.s.DB.BeginTx(ctx, nil)
	if err != nil {
		failure(w, 503, "暂时无法创建角色")
		return
	}
	defer tx.Rollback()
	if err := a.lockWebAccount(ctx, tx, u.ID, token); err != nil {
		sessionError(w, err)
		return
	}
	limit, err := lockedCharacterLimit(ctx, tx, a.s.Bind)
	if err != nil {
		failure(w, 503, "暂时无法读取角色数量限制")
		return
	}
	var count int64
	if err := tx.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM characters WHERE user_id=?"), u.ID).Scan(&count); err != nil {
		failure(w, 503, "暂时无法创建角色")
		return
	}
	if count >= limit {
		failure(w, 409, fmt.Sprintf("当前角色上限为 %d 个", limit))
		return
	}
	id, err := randomID()
	if err != nil {
		failure(w, 500, "无法创建角色")
		return
	}
	uuid, err := randomUUID()
	if err != nil {
		failure(w, 500, "无法创建角色")
		return
	}
	if err := a.chargePoints(ctx, tx, u.ID, "character", uuid, body.ExpectedCost); err != nil {
		pointError(w, err)
		return
	}
	if _, err := tx.ExecContext(ctx, a.s.Bind("INSERT INTO characters(id,user_id,uuid,name,name_key,model) VALUES(?,?,?,?,?,'default')"), id, u.ID, uuid, name, strings.ToLower(name)); err != nil {
		if uniqueViolation(err) {
			failure(w, 409, "这个角色名已被使用")
		} else {
			failure(w, 503, "暂时无法创建角色")
		}
		return
	}
	if err := tx.Commit(); err != nil {
		failure(w, 503, "暂时无法创建角色")
		return
	}
	respond(w, 201, Character{ID: id, UUID: uuid, Name: name, Model: "default"})
}

func (a *Server) updateCharacter(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if !characterNamePattern.MatchString(name) {
		failure(w, 400, "角色名须为 3–16 位英文字母、数字或下划线")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		failure(w, 503, "暂时无法修改角色名")
		return
	}
	defer tx.Rollback()
	if !a.lockCharacter(w, r, tx, id, u.ID) {
		return
	}
	_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE characters SET name=?,name_key=? WHERE id=? AND user_id=?"), name, strings.ToLower(name), id, u.ID)
	if err != nil {
		if uniqueViolation(err) {
			failure(w, 409, "这个角色名已被使用")
		} else {
			failure(w, 503, "暂时无法修改角色名")
		}
		return
	}
	if err := tx.Commit(); err != nil {
		failure(w, 503, "暂时无法修改角色名")
		return
	}
	respond(w, 200, map[string]string{"name": name})
}

func (a *Server) deleteCharacter(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	result, err := a.s.DB.ExecContext(r.Context(), a.s.Bind("DELETE FROM characters WHERE id=? AND user_id=?"), id, u.ID)
	if err != nil {
		failure(w, 503, "暂时无法删除角色")
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		failure(w, 503, "暂时无法删除角色")
		return
	}
	if changed == 0 {
		failure(w, 404, "角色不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *Server) bindTexture(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	if kind != "skin" && kind != "cape" {
		failure(w, 404, "材质类型不存在")
		return
	}
	var body struct {
		RecordID string `json:"recordId"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.RecordID != "" && !recordIDPattern.MatchString(body.RecordID) {
		failure(w, 400, "材质记录无效")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		failure(w, 503, "暂时无法应用材质")
		return
	}
	defer tx.Rollback()
	if !a.lockCharacter(w, r, tx, id, u.ID) {
		return
	}
	fileHash, model := "", "default"
	if body.RecordID != "" {
		var recordKind string
		var owner sql.NullInt64
		var public bool
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT r.file_hash,r.model,r.kind,r.owner_id,r.is_public FROM texture_records r JOIN textures f ON f.hash=r.file_hash WHERE r.record_id=? AND r.deleted=? AND f.deleted=? FOR UPDATE"), body.RecordID, false, false).Scan(&fileHash, &model, &recordKind, &owner, &public)
		if errors.Is(err, sql.ErrNoRows) {
			failure(w, 404, "找不到可应用的材质")
			return
		}
		if err != nil {
			failure(w, 503, "暂时无法读取材质")
			return
		}
		if recordKind != kind {
			failure(w, 404, "找不到可应用的材质")
			return
		}
		if !public && (!owner.Valid || owner.Int64 != u.ID) {
			var grants int
			err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM wardrobe WHERE user_id=? AND record_id=?"), u.ID, body.RecordID).Scan(&grants)
			if err == nil && grants == 0 && !owner.Valid {
				err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM characters WHERE user_id=? AND (skin_hash=? OR cape_hash=?)"), u.ID, fileHash, fileHash).Scan(&grants)
			}
			if err != nil {
				failure(w, 503, "暂时无法读取材质权限")
				return
			}
			if grants == 0 {
				failure(w, 404, "找不到可应用的材质")
				return
			}
		}
	}
	query := "UPDATE characters SET skin_hash=?,model=? WHERE id=? AND user_id=?"
	args := []any{fileHash, model, id, u.ID}
	if kind == "cape" {
		query = "UPDATE characters SET cape_hash=? WHERE id=? AND user_id=?"
		args = []any{fileHash, id, u.ID}
	}
	if _, err := tx.ExecContext(r.Context(), a.s.Bind(query), args...); err != nil {
		failure(w, 503, "暂时无法应用材质")
		return
	}
	if err := tx.Commit(); err != nil {
		failure(w, 503, "暂时无法应用材质")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *Server) listTextures(w http.ResponseWriter, r *http.Request) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	where := " WHERE r.deleted=? AND f.deleted=? AND (r.owner_id=? OR (r.owner_id IS NULL AND r.file_hash IN (SELECT skin_hash FROM characters WHERE user_id=? UNION SELECT cape_hash FROM characters WHERE user_id=?)))"
	args := []any{false, false, u.ID, u.ID, u.ID}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		if kind != "skin" && kind != "cape" {
			failure(w, 400, "材质类型无效")
			return
		}
		where += " AND r.kind=?"
		args = append(args, kind)
	}
	if query := strings.TrimSpace(r.URL.Query().Get("q")); query != "" {
		if utf8.RuneCountInString(query) > 255 {
			failure(w, 400, "搜索内容不能超过 255 个字符")
			return
		}
		where += " AND (POSITION(? IN r.name)>0 OR POSITION(? IN r.description)>0 OR POSITION(? IN r.file_hash)>0)"
		args = append(args, query, query, strings.ToLower(query))
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT r.record_id,r.file_hash,r.name,r.description,r.kind,r.model,r.is_public,r.owner_id FROM texture_records r JOIN textures f ON f.hash=r.file_hash"+where+" ORDER BY r.name,r.record_id"), args...)
	if err != nil {
		failure(w, 503, "暂时无法读取材质")
		return
	}
	defer rows.Close()
	items := []textureRecord{}
	for rows.Next() {
		var item textureRecord
		var owner sql.NullInt64
		if err := rows.Scan(&item.ID, &item.FileHash, &item.Name, &item.Description, &item.Kind, &item.Model, &item.Public, &owner); err != nil {
			failure(w, 503, "暂时无法读取材质")
			return
		}
		item.Owned = owner.Valid && owner.Int64 == u.ID
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		failure(w, 503, "暂时无法读取材质")
		return
	}
	respond(w, 200, map[string]any{"textures": items})
}

func (a *Server) uploadTexture(w http.ResponseWriter, r *http.Request) {
	if !a.uploadIPLimit.allow(r) {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "上传请求过于频繁，请一分钟后重试")
		return
	}
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.uploadAccountLimit.allowKey(strconv.FormatInt(u.ID, 10)) {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "上传请求过于频繁，请一分钟后重试")
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	select {
	case a.uploads <- struct{}{}:
		defer func() { <-a.uploads }()
	default:
		failure(w, 429, "正在处理其他上传，请稍后重试")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBody)
	// The entire bounded body fits in memory; no system temporary files are created.
	if err := r.ParseMultipartForm(maxUploadBody); err != nil {
		failure(w, 400, "上传请求无效或超过 8 MiB")
		return
	}
	kind := r.FormValue("kind")
	var expectedCost *int64
	if raw := r.FormValue("expectedCost"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 || parsed > 1_000_000_000 {
			failure(w, 400, "费用确认值无效")
			return
		}
		expectedCost = &parsed
	}
	if kind != "skin" && kind != "cape" {
		failure(w, 400, "请选择皮肤或披风")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || utf8.RuneCountInString(name) > 255 {
		failure(w, 400, "材质名称不能为空且不能超过 255 个字符")
		return
	}
	description := strings.TrimSpace(r.FormValue("description"))
	if utf8.RuneCountInString(description) > maxDescription {
		failure(w, 400, "材质描述不能超过 200 个字符")
		return
	}
	model := r.FormValue("model")
	if kind == "cape" {
		model = "default"
	} else if model != "default" && model != "slim" {
		failure(w, 400, "请选择标准或纤细手臂模型")
		return
	}
	isPublic := false
	if value := r.FormValue("isPublic"); value != "" {
		if value != "true" && value != "false" {
			failure(w, 400, "公开选项无效")
			return
		}
		isPublic = value == "true"
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		failure(w, 400, "请选择 PNG 文件")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxTextureBytes+1))
	if err != nil || len(data) > maxTextureBytes {
		failure(w, 400, "PNG 文件不能超过 8 MiB")
		return
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || !validTextureDimensions(kind, config.Width, config.Height) {
		failure(w, 400, "PNG 尺寸不符合 Minecraft 皮肤或披风比例，最长边为 1024 像素")
		return
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		failure(w, 400, "PNG 图像数据无效")
		return
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if err := saveTexture(a.cfg.TextureDir, hash, data); err != nil {
		failure(w, 503, "无法保存材质文件")
		return
	}
	recordID, err := randomUUID()
	if err != nil {
		failure(w, 500, "无法创建材质记录")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		failure(w, 503, "暂时无法保存材质记录")
		return
	}
	defer tx.Rollback()
	baseInsert := "INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES(?,?,?,?,?,?) ON CONFLICT(hash) DO NOTHING"
	if err := a.lockWebAccount(r.Context(), tx, u.ID, token); err != nil {
		sessionError(w, err)
		return
	}
	if err := a.chargePoints(r.Context(), tx, u.ID, "upload", recordID, expectedCost); err != nil {
		pointError(w, err)
		return
	}
	if a.s.Dialect == "mysql" {
		baseInsert = "INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE hash=VALUES(hash)"
	}
	if _, err := tx.ExecContext(r.Context(), a.s.Bind(baseInsert), hash, name, kind, model, false, false); err != nil {
		failure(w, 503, "暂时无法保存材质文件记录")
		return
	}
	var deleted bool
	if err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT deleted FROM textures WHERE hash=? FOR UPDATE"), hash).Scan(&deleted); err != nil {
		failure(w, 503, "暂时无法读取材质文件记录")
		return
	}
	if deleted {
		failure(w, 409, "这份材质文件已被停用")
		return
	}
	if _, err := tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO texture_records(record_id,file_hash,owner_id,name,description,kind,model,is_public,deleted) VALUES(?,?,?,?,?,?,?,?,?)"), recordID, hash, u.ID, name, description, kind, model, isPublic, false); err != nil {
		failure(w, 503, "暂时无法保存材质记录")
		return
	}
	if err := tx.Commit(); err != nil {
		failure(w, 503, "暂时无法保存材质记录")
		return
	}
	respond(w, 201, textureRecord{ID: recordID, FileHash: hash, Name: name, Description: description, Kind: kind, Model: model, Public: isPublic, Owned: true})
}

func (a *Server) updateAdminSettings(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		CharacterLimit int64 `json:"characterLimit"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.CharacterLimit < 1 {
		failure(w, 400, "角色数量上限至少为 1")
		return
	}
	query := "INSERT INTO settings(setting_key,value) VALUES(?,?) ON CONFLICT(setting_key) DO UPDATE SET value=EXCLUDED.value"
	if a.s.Dialect == "mysql" {
		query = "INSERT INTO settings(setting_key,value) VALUES(?,?) ON DUPLICATE KEY UPDATE value=VALUES(value)"
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	_, err := tx.ExecContext(r.Context(), a.s.Bind(query), "character_limit", strconv.FormatInt(body.CharacterLimit, 10))
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.character_limit", "site", "character_limit")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存站点设置")
		return
	}
	respond(w, 200, map[string]int64{"characterLimit": body.CharacterLimit})
}

func (a *Server) validWriteRequest(w http.ResponseWriter, r *http.Request, token string) bool {
	if !a.sameOrigin(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf(token))) != 1 {
		failure(w, 403, "请求校验失败")
		return false
	}
	return true
}

func characterLimit(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, bind func(string) string) (int64, error) {
	var raw string
	err := q.QueryRowContext(ctx, bind("SELECT value FROM settings WHERE setting_key=?"), "character_limit").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	limit, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || limit < 1 {
		return 0, errors.New("invalid character limit setting")
	}
	return limit, nil
}

func lockedCharacterLimit(ctx context.Context, tx *sql.Tx, bind func(string) string) (int64, error) {
	var raw string
	err := tx.QueryRowContext(ctx, bind("SELECT value FROM settings WHERE setting_key=? FOR UPDATE"), "character_limit").Scan(&raw)
	if err != nil {
		return 0, err
	}
	limit, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || limit < 1 {
		return 0, errors.New("invalid character limit setting")
	}
	return limit, nil
}

func lockUser(ctx context.Context, tx *sql.Tx, query string, id int64) error {
	var locked int64
	return tx.QueryRowContext(ctx, query, id).Scan(&locked)
}

func (a *Server) lockCharacter(w http.ResponseWriter, r *http.Request, tx *sql.Tx, id, userID int64) bool {
	var locked int64
	err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT id FROM characters WHERE id=? AND user_id=? FOR UPDATE"), id, userID).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "角色不存在")
		return false
	}
	if err != nil {
		failure(w, 503, "暂时无法读取角色")
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		failure(w, 400, "角色编号无效")
		return 0, false
	}
	return id, true
}

func randomID() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	// JSON consumers represent numbers as IEEE 754 doubles.
	id := int64(binary.BigEndian.Uint64(b[:]) & ((1 << 53) - 1))
	if id == 0 {
		return randomID()
	}
	return id, nil
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:]), nil
}

func uniqueViolation(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func validTextureDimensions(kind string, width, height int) bool {
	if width < 64 || width > 1024 || width%64 != 0 {
		return false
	}
	if kind == "cape" {
		return height*2 == width
	}
	return height == width || height*2 == width
}

func saveTexture(directory, hash string, data []byte) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	id, err := randomUUID()
	if err != nil {
		return err
	}
	stage := ".upload-" + id
	f, err := root.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(stage)
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	// Publish only a complete file, without replacing an existing hash path.
	if err = root.Link(stage, hash); err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	existing, err := root.Open(hash)
	if err != nil {
		return err
	}
	defer existing.Close()
	previous, err := io.ReadAll(io.LimitReader(existing, maxTextureBytes+1))
	if err != nil || len(previous) > maxTextureBytes || !bytes.Equal(previous, data) {
		return errors.New("existing texture file does not match its content hash")
	}
	return nil
}
