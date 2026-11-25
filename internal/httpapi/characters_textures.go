package httpapi

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/WishingSkin/WishingSkin/internal/textureimport"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxTextureBytes = textureimport.MaxBytes
	maxUploadBody   = maxTextureBytes + (64 << 10)
	maxDescription  = 200
)

var (
	characterNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)
	recordIDPattern      = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

type textureRecord struct {
	Category     string   `json:"category"`
	Tags         []string `json:"tags"`
	ReviewState  string   `json:"reviewState"`
	ReviewReason string   `json:"reviewReason"`
	ID           string   `json:"id"`
	FileHash     string   `json:"fileHash"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Kind         string   `json:"kind"`
	Model        string   `json:"model"`
	Public       bool     `json:"public"`
	Owned        bool     `json:"owned"`
	CreatedAt    int64    `json:"createdAt"`
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
	limit, err := lockedEffectiveCharacterLimit(ctx, tx, a.s.Bind, u.ID)
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
	uuid := offlinePlayerUUID(name)
	if err := a.chargePoints(ctx, tx, u.ID, "character", uuid, body.ExpectedCost); err != nil {
		pointError(w, err)
		return
	}
	if _, err := tx.ExecContext(ctx, a.s.Bind("INSERT INTO characters(id,user_id,uuid,name,name_key,model) VALUES(?,?,?,?,?,'default')"), id, u.ID, uuid, name, strings.ToLower(name)); err != nil {
		if uniqueViolation(err) {
			failure(w, 409, "这个角色名或 UUID 已被使用")
		} else {
			failure(w, 503, "暂时无法创建角色")
		}
		return
	}
	if err := tx.Commit(); err != nil {
		failure(w, 503, "暂时无法创建角色")
		return
	}
	a.publishPluginEvent(r.Context(), "character.create", u.ID, uuid)
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
		RecordID string  `json:"recordId"`
		Model    *string `json:"model"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.RecordID != "" && !recordIDPattern.MatchString(body.RecordID) {
		failure(w, 400, "材质记录无效")
		return
	}
	if body.Model != nil && (kind != "skin" || body.RecordID == "" || (*body.Model != "default" && *body.Model != "slim")) {
		failure(w, 400, "请选择标准或纤细手臂模型")
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
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT r.file_hash,r.model,r.kind,r.owner_id,r.is_public FROM texture_records r JOIN textures f ON f.hash=r.file_hash WHERE r.record_id=? AND r.deleted=? AND f.deleted=? AND "+approvedRecordSQL+" FOR UPDATE"), body.RecordID, false, false).Scan(&fileHash, &model, &recordKind, &owner, &public)
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
	if body.Model != nil {
		model = *body.Model
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
		where += " AND (POSITION(? IN r.name)>0 OR POSITION(? IN r.description)>0)"
		args = append(args, query, query)
	}
	page, size, paginated, ok := texturePagination(w, r)
	if !ok {
		return
	}
	var total int64
	tables := " FROM texture_records r JOIN textures f ON f.hash=r.file_hash LEFT JOIN texture_metadata m ON m.record_id=r.record_id"
	if paginated {
		if err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*)"+tables+where), args...).Scan(&total); err != nil {
			failure(w, 503, "暂时无法读取材质")
			return
		}
	}
	limit := ""
	if paginated {
		limit = " LIMIT ? OFFSET ?"
		args = append(args, size, (page-1)*size)
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT r.record_id,r.file_hash,r.name,r.description,r.kind,r.model,r.is_public,r.owner_id,COALESCE(m.category,''),COALESCE(m.tags,'[]'),COALESCE(m.review_state,'approved'),COALESCE(m.review_reason,''),r.created_at FROM texture_records r JOIN textures f ON f.hash=r.file_hash LEFT JOIN texture_metadata m ON m.record_id=r.record_id"+where+" ORDER BY r.name,r.record_id"+limit), args...)
	if err != nil {
		failure(w, 503, "暂时无法读取材质")
		return
	}
	defer rows.Close()
	items := []textureRecord{}
	for rows.Next() {
		var item textureRecord
		var owner sql.NullInt64
		var tags string
		if err := rows.Scan(&item.ID, &item.FileHash, &item.Name, &item.Description, &item.Kind, &item.Model, &item.Public, &owner, &item.Category, &tags, &item.ReviewState, &item.ReviewReason, &item.CreatedAt); err != nil {
			failure(w, 503, "暂时无法读取材质")
			return
		}
		if err := json.Unmarshal([]byte(tags), &item.Tags); err != nil {
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
	if !paginated {
		total = int64(len(items))
	}
	respond(w, 200, map[string]any{"textures": items, "total": total, "page": page, "pageSize": size})
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
	category := strings.TrimSpace(r.FormValue("category"))
	tags, err := normalizeTextureTags(strings.Split(r.FormValue("tags"), ","))
	if err != nil {
		failure(w, 400, "最多 10 个标签，每个 24 个字，仅使用文字、数字、空格、下划线或短横线")
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
	data, config, err := textureimport.DecodePNG(file)
	decodeErr := err
	if err != nil && !errors.Is(err, textureimport.ErrDimensions) {
		failure(w, 400, err.Error())
		return
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	provider, err := a.objectStore(r.Context())
	if err != nil {
		failure(w, 503, "无法保存材质文件")
		return
	}
	conn, releaseStorage, err := a.s.LeaseStorageConn(r.Context())
	if err != nil {
		failure(w, 503, "存储正在维护，请稍后重试")
		return
	}
	defer releaseStorage()
	recordID, err := randomUUID()
	if err != nil {
		failure(w, 500, "无法创建材质记录")
		return
	}
	tx, err := conn.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法保存材质记录")
		return
	}
	defer tx.Rollback()
	var mode string
	if err := tx.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode); err != nil {
		failure(w, 503, "暂时无法读取内容设置")
		return
	}
	policy, err := readContentPolicy(r.Context(), tx)
	if err != nil {
		failure(w, 503, "暂时无法读取内容设置")
		return
	}
	_, err = textureimport.ValidatePNGForPolicy(kind, "", config.Width, config.Height, len(data), policy)
	if err == nil {
		err = decodeErr
	}
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	if !policy.ValidCategory(category) {
		failure(w, 400, "请选择有效分类")
		return
	}
	reviewState := "approved"
	if policy.ReviewUploads {
		reviewState = "pending"
	}
	if err := a.lockWebAccount(r.Context(), tx, u.ID, token); err != nil {
		sessionError(w, err)
		return
	}
	var deleted bool
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT deleted FROM textures WHERE hash=? FOR UPDATE"), hash).Scan(&deleted)
	existing := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		failure(w, 503, "暂时无法读取材质文件记录")
		return
	}
	if deleted {
		failure(w, 409, "这份材质文件已被停用")
		return
	}
	size := int64(len(data))
	if !a.checkTextureCapacity(w, r.Context(), tx, u.ID, size, existing) {
		return
	}
	if err := a.chargePoints(r.Context(), tx, u.ID, "upload", recordID, expectedCost); err != nil {
		pointError(w, err)
		return
	}
	if !existing {
		if _, err := tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES(?,?,?,?,?,?)"), hash, name, kind, model, false, false); err != nil {
			failure(w, 503, "暂时无法保存材质文件记录")
			return
		}
	}
	if err := a.recordTextureIntegrity(r.Context(), tx, hash, size); err != nil {
		failure(w, 503, "暂时无法保存材质文件记录")
		return
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO texture_records(record_id,file_hash,owner_id,name,description,kind,model,is_public,deleted,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)"), recordID, hash, u.ID, name, description, kind, model, isPublic, false, now); err != nil {
		failure(w, 503, "暂时无法保存材质记录")
		return
	}
	if err := a.putTextureMetadata(r.Context(), tx, recordID, category, tags, reviewState, ""); err != nil {
		failure(w, 503, "暂时无法保存材质信息")
		return
	}
	created, err := a.saveTextureFile(r.Context(), provider, hash, data)
	if err != nil {
		if created && !existing {
			_ = os.Remove(filepath.Join(a.cfg.TextureDir, hash))
		}
		failure(w, 503, "无法保存材质文件")
		return
	}
	if err := tx.Commit(); err != nil {
		failure(w, 503, "暂时无法保存材质记录")
		return
	}
	releaseStorage()
	a.publishPluginEvent(r.Context(), "texture.upload", u.ID, recordID)
	respond(w, 201, textureRecord{ID: recordID, FileHash: hash, Name: name, Description: description, Kind: kind, Model: model, Public: isPublic, Owned: true, Category: category, Tags: tags, ReviewState: reviewState, CreatedAt: now})
}

func (a *Server) updateAdminSettings(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		CharacterLimit   *int64 `json:"characterLimit"`
		YggNonEmailLogin *bool  `json:"yggNonEmailLogin"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.CharacterLimit == nil && body.YggNonEmailLogin == nil {
		failure(w, 400, "请选择要保存的站点设置")
		return
	}
	if body.CharacterLimit != nil && *body.CharacterLimit < 1 {
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
	if body.CharacterLimit != nil {
		var previous string
		err := tx.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='character_limit'").Scan(&previous)
		if err != nil {
			failure(w, 503, "暂时无法读取站点设置")
			return
		}
		nextValue := strconv.FormatInt(*body.CharacterLimit, 10)
		a.auditChange(r.Context(), "character_limit", previous, nextValue)
		if _, err = tx.ExecContext(r.Context(), a.s.Bind(query), "character_limit", nextValue); err != nil {
			failure(w, 503, "暂时无法保存站点设置")
			return
		}
		if err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.character_limit", "site", "character_limit"); err != nil {
			failure(w, 503, "暂时无法保存站点设置")
			return
		}
	}
	if body.YggNonEmailLogin != nil {
		previousNonEmail, err := yggNonEmailLogin(r.Context(), tx)
		if err != nil {
			failure(w, 503, "暂时无法读取站点设置")
			return
		}
		if err = a.putSetting(r.Context(), tx, "ygg_non_email_login", *body.YggNonEmailLogin); err != nil {
			failure(w, 503, "暂时无法保存站点设置")
			return
		}
		if previousNonEmail != *body.YggNonEmailLogin {
			if err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.yggdrasil", "site", "yggdrasil"); err != nil {
				failure(w, 503, "暂时无法保存站点设置")
				return
			}
		}
	}
	currentLimit, err := characterLimit(r.Context(), tx, a.s.Bind)
	if err == nil {
		var currentNonEmail bool
		currentNonEmail, err = yggNonEmailLogin(r.Context(), tx)
		if err == nil {
			if err = tx.Commit(); err == nil {
				respond(w, 200, map[string]any{"characterLimit": currentLimit, "yggNonEmailLogin": currentNonEmail})
				return
			}
		}
	}
	failure(w, 503, "暂时无法保存站点设置")
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

func effectiveCharacterLimit(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, bind func(string) string, userID int64) (int64, error) {
	var override sql.NullInt64
	err := q.QueryRowContext(ctx, bind("SELECT character_limit FROM users WHERE id=?"), userID).Scan(&override)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if override.Valid && override.Int64 >= 1 {
		return override.Int64, nil
	}
	return characterLimit(ctx, q, bind)
}

func lockedEffectiveCharacterLimit(ctx context.Context, tx *sql.Tx, bind func(string) string, userID int64) (int64, error) {
	var override sql.NullInt64
	err := tx.QueryRowContext(ctx, bind("SELECT character_limit FROM users WHERE id=?"), userID).Scan(&override)
	if err != nil {
		return 0, err
	}
	if override.Valid && override.Int64 >= 1 {
		return override.Int64, nil
	}
	return lockedCharacterLimit(ctx, tx, bind)
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

// offlinePlayerUUID is Minecraft's UUID v3 compatibility algorithm used by
// Blessing Skin and the vanilla offline player profile convention.
func offlinePlayerUUID(name string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	sum[6] = sum[6]&0x0f | 0x30
	sum[8] = sum[8]&0x3f | 0x80
	return hex.EncodeToString(sum[:])
}

func uniqueViolation(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
