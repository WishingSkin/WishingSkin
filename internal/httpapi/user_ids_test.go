package httpapi

import (
	"context"
	"database/sql"
	"sync"
	"testing"
)

func TestUserIDModesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			f.exec("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt) VALUES(900000000000001,'old@example.test','old@example.test','old','','ARGON2ID','')")
			allocate := func() (int64, error) {
				tx, err := f.db.DB.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
				if err != nil {
					return 0, err
				}
				defer tx.Rollback()
				var mode string
				if err = tx.QueryRow("SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode); err != nil {
					return 0, err
				}
				id, err := f.app.allocateUserID(context.Background(), tx)
				if err != nil {
					return 0, err
				}
				return id, tx.Commit()
			}
			first, err := allocate()
			if err != nil || first != 4 {
				t.Fatalf("first small id=%d err=%v", first, err)
			}
			var wg sync.WaitGroup
			var mu sync.Mutex
			seen := map[int64]bool{}
			for range 8 {
				wg.Go(func() {
					id, err := allocate()
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						t.Error(err)
					}
					if seen[id] {
						t.Error("duplicate sequence")
					}
					seen[id] = true
				})
			}
			wg.Wait()
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "closed", "randomUserIds": true}), 204)
			for range 8 {
				id, err := allocate()
				if err != nil || id < 1_000_000_000 || id > 9_999_999_999 {
					t.Fatalf("invalid random id: %d %v", id, err)
				}
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "closed", "randomUserIds": false}), 204)
			id, err := allocate()
			if err != nil || id != 13 {
				t.Fatalf("sequence lost: %d %v", id, err)
			}
		})
	}
}
