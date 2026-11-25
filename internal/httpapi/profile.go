package httpapi

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type profile struct {
	Nickname      string `json:"nickname"`
	AvatarMode    string `json:"avatarMode"`
	CharacterUUID string `json:"characterUUID"`
	AvatarImage   string `json:"avatarImage"`
}

func (a *Server) accountProfile(w http.ResponseWriter, r *http.Request) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	value := profile{Nickname: u.Nickname, AvatarMode: "default"}
	err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT avatar_mode,character_uuid,avatar_image FROM user_profiles WHERE user_id=?"), u.ID).Scan(&value.AvatarMode, &value.CharacterUUID, &value.AvatarImage)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		failure(w, 503, "暂时无法读取个人资料")
		return
	}
	respond(w, 200, value)
}

func (a *Server) updateAccountProfile(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	var body profile
	if !decodeLimit(w, r, &body, 710000) {
		return
	}
	body.Nickname = strings.TrimSpace(body.Nickname)
	if !utf8.ValidString(body.Nickname) || utf8.RuneCountInString(body.Nickname) < 1 || utf8.RuneCountInString(body.Nickname) > 64 || strings.IndexFunc(body.Nickname, unicode.IsControl) >= 0 {
		failure(w, 400, "昵称须为 1–64 个字符且不能包含控制字符")
		return
	}
	if body.AvatarMode != "default" && body.AvatarMode != "upload" && body.AvatarMode != "character" {
		failure(w, 400, "头像模式无效")
		return
	}
	if decision := a.runTextHook(r.Context(), "profile.nickname", u.ID, map[string]string{"nickname": body.Nickname}); decision.Decision == "reject" {
		failure(w, 400, decision.Reason)
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		failure(w, 503, "暂时无法保存个人资料")
		return
	}
	defer tx.Rollback()
	// Lock the owner first, matching character deletion and other account writes.
	err = a.lockWebAccount(r.Context(), tx, u.ID, token)
	if err != nil {
		if errors.Is(err, errCredentials) {
			sessionError(w, err)
			return
		}
		failure(w, 503, "暂时无法保存个人资料")
		return
	}
	if body.AvatarMode == "character" {
		var id int64
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT id FROM characters WHERE user_id=? AND uuid=? FOR UPDATE"), u.ID, body.CharacterUUID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			failure(w, 400, "请选择属于自己的角色")
			return
		}
		if err != nil {
			failure(w, 503, "暂时无法保存个人资料")
			return
		}
		body.AvatarImage = ""
	} else {
		body.CharacterUUID = ""
	}
	if body.AvatarMode == "upload" {
		if body.AvatarImage == "" {
			_ = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT avatar_image FROM user_profiles WHERE user_id=?"), u.ID).Scan(&body.AvatarImage)
		}
		body.AvatarImage, err = normalizedImage(body.AvatarImage)
		if err != nil {
			failure(w, 400, "请选择不超过 512 KiB、2048×2048 的 PNG 或 JPEG 图片")
			return
		}
	} else {
		body.AvatarImage = ""
	}
	_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE users SET nickname=? WHERE id=?"), body.Nickname, u.ID)
	query := "INSERT INTO user_profiles(user_id,avatar_mode,character_uuid,avatar_image) VALUES(?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET avatar_mode=EXCLUDED.avatar_mode,character_uuid=EXCLUDED.character_uuid,avatar_image=EXCLUDED.avatar_image"
	if a.s.Dialect == "mysql" {
		query = "INSERT INTO user_profiles(user_id,avatar_mode,character_uuid,avatar_image) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE avatar_mode=VALUES(avatar_mode),character_uuid=VALUES(character_uuid),avatar_image=VALUES(avatar_image)"
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind(query), u.ID, body.AvatarMode, body.CharacterUUID, body.AvatarImage)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存个人资料")
		return
	}
	respond(w, 200, body)
}

func (a *Server) avatar(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimSuffix(r.PathValue("id"), ".png"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	var nickname, mode, character, data string
	err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT u.nickname,COALESCE(p.avatar_mode,'default'),COALESCE(p.character_uuid,''),COALESCE(p.avatar_image,'') FROM users u LEFT JOIN user_profiles p ON p.user_id=u.id WHERE u.id=? AND u.banned=FALSE"), id).Scan(&nickname, &mode, &character, &data)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		failure(w, 503, "头像暂不可用")
		return
	}
	var avatar image.Image
	if mode == "upload" {
		_, raw, _ := strings.Cut(data, ",")
		blob, e := base64.StdEncoding.DecodeString(raw)
		if e == nil {
			avatar, _, _ = image.Decode(bytes.NewReader(blob))
		}
	}
	if mode == "character" {
		var hash string
		err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT c.skin_hash FROM characters c JOIN textures f ON f.hash=c.skin_hash WHERE c.user_id=? AND c.uuid=? AND "+downloadableTextureSQL), id, character).Scan(&hash)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			failure(w, 503, "头像暂不可用")
			return
		}
		if err == nil && validHash.MatchString(hash) {
			blob, e := a.textureFileBytes(r.Context(), hash)
			if e == nil && len(blob) <= 8<<20 {
				skin, _, e := image.Decode(bytes.NewReader(blob))
				if e == nil && skin.Bounds().Dx() >= 64 {
					avatar = copyFace(skin)
				}
			}
		}
	}
	if avatar == nil {
		img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				c := color.NRGBA{R: 68, G: 105, B: 175, A: 255}
				if x > 1 && x < 6 && y > 1 && y < 6 {
					c = color.NRGBA{R: 180, G: 205, B: 245, A: 255}
				}
				img.Set(x, y, c)
			}
		}
		avatar = img
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_ = png.Encode(w, avatar)
}
