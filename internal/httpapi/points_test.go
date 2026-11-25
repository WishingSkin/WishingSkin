package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPointCodeLifecycleBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			const codesPath = "/api/admin/points/codes"
			const redeemPath = "/api/points/redeem"
			body := map[string]any{"count": 1, "points": 13, "note": " =Event reward "}
			webStatus(t, f.call(1, "GET", codesPath, nil), 403)
			webStatus(t, f.call(3, "POST", codesPath, body), 409)
			configurePoints(t, f, true, 0, 0, 0)
			webStatus(t, f.call(3, "POST", codesPath, map[string]any{"count": 1, "points": 1, "maxUses": 0}), 400)
			webStatus(t, f.call(3, "POST", codesPath, map[string]any{"count": 1, "points": 1, "note": "bad\n"}), 400)
			issued := []string{}
			create := func() string {
				t.Helper()
				w := f.call(3, "POST", codesPath, body)
				webStatus(t, w, 201)
				var reply struct{ Codes []string }
				if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || len(reply.Codes) != 1 || len(strings.Split(reply.Codes[0], "-")) != 5 {
					t.Fatal("invalid batch response", err)
				}
				issued = append(issued, reply.Codes[0])
				return reply.Codes[0]
			}
			redeem := func(user int, code string) int {
				return f.call(user, "POST", redeemPath, map[string]string{"code": code}).Code
			}
			race := func(code string, users ...int) map[int]int {
				var wg sync.WaitGroup
				statuses := make(chan int, len(users))
				for _, user := range users {
					wg.Go(func() { statuses <- redeem(user, code) })
				}
				wg.Wait()
				close(statuses)
				result := map[int]int{}
				for status := range statuses {
					result[status]++
				}
				return result
			}
			single := create()
			if got := race(single, 1, 2); got[200] != 1 || got[409] != 1 {
				t.Fatal("single-use race", got)
			}
			body["maxUses"] = 3
			multi := create()
			if got := race(strings.ToLower(strings.ReplaceAll(multi, "-", " \t")), 1, 1, 2); got[200] != 2 || got[409] != 1 {
				t.Fatal("multi-use race", got)
			}
			w := f.call(1, "POST", redeemPath, map[string]string{"code": multi})
			webStatus(t, w, 409)
			if !strings.Contains(w.Body.String(), "你已经兑换过此积分码") {
				t.Fatal("unexpected duplicate error", w.Body)
			}
			if total := pointsBalance(t, f, 1) + pointsBalance(t, f, 2); total != 39 {
				t.Fatalf("unexpected combined balance: %d", total)
			}
			w = f.call(1, "GET", "/api/points", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"note":"=Event reward"`) || strings.Contains(w.Body.String(), multi) {
				t.Fatal("incorrect ledger projection", w.Body)
			}
			var notifications int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM user_notifications WHERE kind='points.redeem' AND link='/user/account/points'").Scan(&notifications); err != nil || notifications != 3 {
				t.Fatal("incorrect redemption notifications", notifications, err)
			}
			w = f.call(3, "GET", codesPath+"?limit=1", nil)
			webStatus(t, w, 200)
			var listing struct {
				Codes []struct{ ID, Code, Status string }
				Total int
			}
			if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil || listing.Total != 2 || len(listing.Codes) != 1 {
				t.Fatal("incorrect code listing", err)
			}
			w = f.call(3, "GET", codesPath+"/export?format=csv", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), multi) || !strings.Contains(w.Body.String(), "'=Event reward") || strings.Contains(w.Body.String(), single) {
				t.Fatal("incorrect available CSV export")
			}
			multiHash := tokenHash(strings.ReplaceAll(multi, "-", ""))
			webStatus(t, f.call(3, "POST", codesPath+"/"+multiHash+"/disable", nil), 204)
			w = f.call(3, "POST", redeemPath, map[string]string{"code": multi})
			webStatus(t, w, 409)
			if !strings.Contains(w.Body.String(), "该积分码已停用") {
				t.Fatal("incorrect disabled error", w.Body)
			}
			now := time.Now()
			f.app.pointsNow = func() time.Time { return now }
			body["hours"] = 1
			expiring := create()
			f.app.pointsNow = func() time.Time { return now.Add(time.Hour) }
			w = f.call(3, "POST", redeemPath, map[string]string{"code": expiring})
			webStatus(t, w, 409)
			if !strings.Contains(w.Body.String(), "该积分码已过期") {
				t.Fatal("incorrect expired error", w.Body)
			}
			w = f.call(3, "GET", codesPath+"/export?format=txt&status=all", nil)
			webStatus(t, w, 200)
			for _, code := range issued {
				if !strings.Contains(w.Body.String(), code) {
					t.Fatal("full export omitted a code")
				}
			}
			for range 8 {
				if status := redeem(3, strings.Repeat("2", 20)); status != 404 {
					t.Fatal("valid-shaped missing code status", status)
				}
			}
			if status := redeem(3, strings.Repeat("2", 20)); status != 429 {
				t.Fatal("rate limit status", status)
			}
			w = f.call(3, "GET", "/api/admin/events?pageSize=60", nil)
			webStatus(t, w, 200)
			for _, action := range []string{"points.code.create", "points.code.disable", "points.code.export"} {
				if !strings.Contains(w.Body.String(), action) {
					t.Fatal("missing audit action", action)
				}
			}
			for _, code := range issued {
				if strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), strings.ReplaceAll(code, "-", "")) {
					t.Fatal("audit record exposed a point code")
				}
			}
		})
	}
}

func TestPointsConcurrentChargeAndRevocationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			configurePoints(t, f, true, 0, 5, 0)
			f.exec("UPDATE users SET score=5 WHERE id=1")
			first, second := testPNG(t, 64, 64), testPNG(t, 64, 32)
			var wg sync.WaitGroup
			codes := make(chan int, 2)
			for _, data := range [][]byte{first, second} {
				wg.Add(1)
				go func(data []byte) { defer wg.Done(); codes <- f.upload(1, data, map[string]string{}).Code }(data)
			}
			wg.Wait()
			close(codes)
			got := map[int]int{}
			for code := range codes {
				got[code]++
			}
			if got[201] != 1 || got[402] != 1 || pointsBalance(t, f, 1) != 0 {
				t.Fatal("concurrent spend", got)
			}
			for _, table := range []string{"point_entries", "texture_records"} {
				var count int
				if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 1 {
					t.Fatal("spend atomicity", table, count, err)
				}
			}
			f.exec("DELETE FROM web_sessions WHERE user_id=1")
			tx, err := f.db.DB.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := f.app.lockWebAccount(context.Background(), tx, 1, strings.Repeat("0", 63)+"1"); !errors.Is(err, errCredentials) {
				t.Fatal("revoked session admitted", err)
			}
		})
	}
}

func configurePoints(t *testing.T, f *managementApp, enabled bool, reward, upload, character int64) {
	t.Helper()
	webStatus(t, f.call(3, "PATCH", "/api/admin/points", map[string]any{"enabled": enabled, "dailyReward": reward, "uploadCost": upload, "characterCost": character}), 204)
}

func pointsBalance(t *testing.T, f *managementApp, user int) int64 {
	t.Helper()
	var balance int64
	if err := f.db.DB.QueryRow(f.db.Bind("SELECT score FROM users WHERE id=?"), user).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	return balance
}

func TestPointsDisabledAndSettingsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			webStatus(t, f.call(0, "GET", "/api/points", nil), 401)
			webStatus(t, f.call(1, "PATCH", "/api/admin/points", map[string]bool{"enabled": true}), 403)
			webStatus(t, f.call(3, "PATCH", "/api/admin/points", map[string]bool{"enabled": true}), 400)
			webStatus(t, f.call(1, "POST", "/api/points/checkin", nil), 409)
			configurePoints(t, f, false, 50, 20, 30)
			webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "FreeCharacter"}))
			uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{}))
			if pointsBalance(t, f, 1) != 0 {
				t.Fatal("disabled charge")
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM point_entries").Scan(&count); err != nil || count != 0 {
				t.Fatal("disabled ledger", count, err)
			}
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/points", map[string]any{"enabled": true, "dailyReward": 5, "uploadCost": 2, "characterCost": 3}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			w := f.call(1, "GET", "/api/points", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"enabled":false`) {
				t.Fatal("settings rollback", w.Body)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/points", map[string]any{"enabled": true, "dailyReward": -1, "uploadCost": 0, "characterCost": 0}), 400)
		})
	}
}

func TestPointsCheckinConcurrentAndBeijingBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			configurePoints(t, f, true, 10, 0, 0)
			f.app.pointsNow = func() time.Time { return time.Date(2026, 10, 23, 15, 59, 59, 0, time.UTC) }
			var wg sync.WaitGroup
			codes := make(chan int, 8)
			for range 8 {
				wg.Add(1)
				go func() { defer wg.Done(); codes <- f.call(1, "POST", "/api/points/checkin", nil).Code }()
			}
			wg.Wait()
			close(codes)
			got := map[int]int{}
			for code := range codes {
				got[code]++
			}
			if got[204] != 1 || got[409] != 7 || pointsBalance(t, f, 1) != 10 {
				t.Fatal("checkin race", got)
			}
			f.app.pointsNow = func() time.Time { return time.Date(2026, 10, 23, 16, 0, 0, 0, time.UTC) }
			webStatus(t, f.call(1, "POST", "/api/points/checkin", nil), 204)
			if pointsBalance(t, f, 1) != 20 {
				t.Fatal("Beijing midnight")
			}
			w := f.call(1, "GET", "/api/points", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"day":"2026-10-24"`) || !strings.Contains(w.Body.String(), `"checkedIn":true`) || !strings.Contains(w.Body.String(), `"balance":"20"`) {
				t.Fatal("point projection", w.Body)
			}
			w = f.call(2, "GET", "/api/points", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"total":0`) {
				t.Fatal("ledger leak", w.Body)
			}
			f.exec("UPDATE users SET score=? WHERE id=2", int64(math.MaxInt64))
			webStatus(t, f.call(2, "POST", "/api/points/checkin", nil), 409)
			if pointsBalance(t, f, 2) != math.MaxInt64 {
				t.Fatal("overflow")
			}
			f.exec("ALTER TABLE point_entries RENAME COLUMN kind TO unavailable_kind")
			webStatus(t, f.call(2, "POST", "/api/points/checkin", nil), 503)
			f.exec("ALTER TABLE point_entries RENAME COLUMN unavailable_kind TO kind")
			if pointsBalance(t, f, 2) != math.MaxInt64 {
				t.Fatal("ledger failure changed balance")
			}
			f.exec("UPDATE users SET score=0 WHERE id=2")
			f.exec("ALTER TABLE point_entries RENAME COLUMN balance_after TO unavailable_balance_after")
			webStatus(t, f.call(2, "POST", "/api/points/checkin", nil), 503)
			f.exec("ALTER TABLE point_entries RENAME COLUMN unavailable_balance_after TO balance_after")
			if pointsBalance(t, f, 2) != 0 {
				t.Fatal("failed reward ledger changed balance")
			}
		})
	}
}

func TestPointsChargeAndFailureAtomicBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			configurePoints(t, f, true, 20, 5, 7)
			webStatus(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "NeedPoints"}), 402)
			webStatus(t, f.upload(1, testPNG(t, 64, 64), map[string]string{}), 402)
			webStatus(t, f.call(1, "POST", "/api/points/checkin", nil), 204)
			webStatus(t, f.call(1, "POST", "/api/characters", map[string]any{"name": "StaleFee", "expectedCost": 0}), 409)
			webStatus(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"expectedCost": "0"}), 409)
			if pointsBalance(t, f, 1) != 20 {
				t.Fatal("stale displayed price charged")
			}
			c := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "PaidCharacter"}))
			if pointsBalance(t, f, 1) != 13 {
				t.Fatal("character charge")
			}
			webStatus(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "AboveLimit"}), 409)
			if pointsBalance(t, f, 1) != 13 {
				t.Fatal("failed limit charge")
			}
			uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{}))
			if pointsBalance(t, f, 1) != 8 {
				t.Fatal("upload charge")
			}
			// Fail after charge but before creating the actual upload record.
			f.exec("ALTER TABLE texture_records RENAME COLUMN description TO unavailable_description")
			webStatus(t, f.upload(1, testPNG(t, 64, 32), map[string]string{}), 503)
			f.exec("ALTER TABLE texture_records RENAME COLUMN unavailable_description TO description")
			if pointsBalance(t, f, 1) != 8 {
				t.Fatal("failed record write charged")
			}
			// Ledger failure rolls back the operation and balance as well.
			f.exec("ALTER TABLE point_entries RENAME COLUMN balance_after TO unavailable_balance_after")
			webStatus(t, f.upload(1, testPNG(t, 64, 32), map[string]string{}), 503)
			f.exec("ALTER TABLE point_entries RENAME COLUMN unavailable_balance_after TO balance_after")
			if pointsBalance(t, f, 1) != 8 {
				t.Fatal("failed ledger charged")
			}
			var view struct {
				Entries []struct{ Kind, Delta, BalanceAfter string }
				Total   int
			}
			w := f.call(1, "GET", "/api/points", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Total != 3 {
				t.Fatal("ledger", view, err)
			}
			configurePoints(t, f, false, 20, 5, 7)
			webStatus(t, f.call(1, "PATCH", "/api/characters/"+strconv.FormatInt(c.ID, 10), map[string]string{"name": "RenameIsFree"}), 200)
			uploadedRecord(t, f.upload(1, testPNG(t, 64, 32), map[string]string{}))
			if pointsBalance(t, f, 1) != 8 {
				t.Fatal("disabled fee")
			}
		})
	}
}
