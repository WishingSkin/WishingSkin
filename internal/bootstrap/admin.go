// Package bootstrap creates the first local administrator without changing public registration.
package bootstrap

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/WishingSkin/WishingSkin/internal/credential"
	"github.com/WishingSkin/WishingSkin/internal/store"
)

var ErrNotEmpty = errors.New("站点已配置或已有账号/迁移记录，不能初始化首个管理员；不会提升或覆盖原账号")

func CreateAdmin(ctx context.Context, s *store.Store, email, nickname, password string) (int64, error) {
	email, nickname = strings.TrimSpace(email), strings.TrimSpace(nickname)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 || strings.ContainsAny(email, "\r\n") || !utf8.ValidString(nickname) || utf8.RuneCountInString(nickname) > 255 {
		return 0, errors.New("邮箱或昵称格式无效")
	}
	if !credential.ValidNewPassword(password) {
		return 0, errors.New("新密码须为 8–128 个字符，且同时包含英文字母、数字和符号")
	}
	hash, err := credential.Hash(password)
	if err != nil {
		return 0, errors.New("暂时无法处理密码")
	}
	var bytes [8]byte
	if _, err = rand.Read(bytes[:]); err != nil {
		return 0, err
	}
	id := int64(binary.BigEndian.Uint64(bytes[:])%((1<<53)-1)) + 1
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// The same row serializes registration and BS import claims with initialization.
	var mode string
	if err = tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode); err != nil {
		return 0, err
	}
	if mode != "closed" && mode != "open" {
		return 0, ErrNotEmpty
	}
	for _, table := range []string{"users", "migration_state", "admin_events"} {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			return 0, err
		}
		if count != 0 {
			return 0, ErrNotEmpty
		}
	}
	_, err = tx.ExecContext(ctx, s.Bind("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt,permission) VALUES(?,?,?,?,?,'ARGON2ID','',2)"), id, email, strings.ToLower(email), nickname, hash)
	if err == nil {
		_, err = tx.ExecContext(ctx, s.Bind("INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(?,FALSE,FALSE)"), id)
	}
	if err == nil {
		err = s.RecordAdminEvent(ctx, tx, 0, "administrator.initialize", "user", strconv.FormatInt(id, 10))
	}
	if err == nil {
		err = tx.Commit()
	}
	return id, err
}
