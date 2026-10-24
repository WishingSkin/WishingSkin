package httpapi

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdminCharacterDeletionAtomicBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			c := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "AdminDelete"}))
			other := webCharacter(t, f.call(2, "POST", "/api/characters", map[string]string{"name": "RetainOther"}))
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{}))
			webStatus(t, f.call(2, "PUT", "/api/wardrobe/"+record.ID, nil), 404)
			webStatus(t, f.call(1, "PUT", "/api/wardrobe/"+record.ID, nil), 204)
			for _, uuid := range []string{c.UUID, other.UUID} {
				f.exec("INSERT INTO game_tokens(token_hash,client_token,user_id,profile_uuid,expires_at) VALUES(?, 'delete-client',?,?,?)", tokenHash(uuid), map[bool]int{true: 1, false: 2}[uuid == c.UUID], uuid, time.Now().Add(time.Hour).Unix())
				f.exec("INSERT INTO ygg_join_sessions(token_hash,server_id,profile_uuid,client_ip,expires_at) VALUES(?,'delete-server',?,'127.0.0.1',?)", tokenHash(uuid), uuid, time.Now().Add(time.Minute).Unix())
			}
			path := fmt.Sprintf("/api/admin/characters/%d", c.ID)
			body := map[string]string{"confirmUUID": c.UUID}
			webStatus(t, f.call(0, "DELETE", path, body), 401)
			webStatus(t, f.call(1, "DELETE", path, body), 403)
			webStatus(t, f.call(3, "DELETE", path, map[string]string{"confirmUUID": strings.Repeat("0", 32)}), 409)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "DELETE", path, body), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			var count int
			for _, table := range []string{"characters", "game_tokens", "ygg_join_sessions"} {
				if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 2 {
					t.Fatal("delete rollback", table, count, err)
				}
			}
			var wg sync.WaitGroup
			codes := make(chan int, 2)
			for range 2 {
				wg.Add(1)
				go func() { defer wg.Done(); codes <- f.call(3, "DELETE", path, body).Code }()
			}
			wg.Wait()
			close(codes)
			got := map[int]int{}
			for code := range codes {
				got[code]++
			}
			if got[204] != 1 || got[404] != 1 {
				t.Fatal("delete race", got)
			}
			for _, table := range []string{"characters", "game_tokens", "ygg_join_sessions", "texture_records", "wardrobe"} {
				if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 1 {
					t.Fatal("unrelated resources changed", table, count, err)
				}
			}
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 200)
			webStatus(t, f.call(0, "GET", "/textures/"+record.FileHash, nil), 200)
			webStatus(t, f.call(0, "POST", "/api/yggdrasil/authserver/validate", map[string]string{"accessToken": c.UUID}), 403)
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_events WHERE action='character.delete'").Scan(&count); err != nil || count != 1 {
				t.Fatal("delete audit", count, err)
			}
			var target string
			if err := f.db.DB.QueryRow("SELECT target_id FROM admin_events WHERE action='character.delete'").Scan(&target); err != nil || !strings.Contains(target, c.UUID) {
				t.Fatal("deleted identity missing", target, err)
			}
		})
	}
}
