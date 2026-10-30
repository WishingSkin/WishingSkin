package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"strconv"
)

func randomUserIDs(ctx context.Context, reader settingReader) (bool, error) {
	var raw string
	err := reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='random_user_ids'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return raw == "true", err
}

// The caller holds registration_mode FOR UPDATE until the user insert commits.
// The independent counter deliberately ignores the maximum legacy/random ID.
func (a *Server) allocateUserID(ctx context.Context, tx *sql.Tx) (int64, error) {
	random, err := randomUserIDs(ctx, tx)
	if err != nil {
		return 0, err
	}
	if random {
		for range 32 {
			n, err := rand.Int(rand.Reader, big.NewInt(9_000_000_000))
			if err != nil {
				return 0, err
			}
			id := n.Int64() + 1_000_000_000
			var occupied int
			if err = tx.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM users WHERE id=?"), id).Scan(&occupied); err != nil {
				return 0, err
			}
			if occupied == 0 {
				return id, nil
			}
		}
		return 0, errors.New("cannot allocate random user id")
	}
	next := int64(1)
	var raw string
	err = tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='next_user_id'").Scan(&raw)
	if err == nil {
		next, err = strconv.ParseInt(raw, 10, 64)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if next < 1 {
		return 0, errors.New("invalid user id counter")
	}
	for {
		rows, err := tx.QueryContext(ctx, a.s.Bind("SELECT id FROM users WHERE id>=? ORDER BY id LIMIT 1000"), next)
		if err != nil {
			return 0, err
		}
		count := 0
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return 0, err
			}
			if id != next {
				break
			}
			next++
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		if count < 1000 {
			break
		}
	}
	if next >= 1<<53 {
		return 0, errors.New("user id counter exhausted")
	}
	if err := a.putSetting(ctx, tx, "next_user_id", next+1); err != nil {
		return 0, err
	}
	return next, nil
}
