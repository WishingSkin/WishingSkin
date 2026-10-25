package bsmigration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wishingskin/internal/securebox"
)

func invitationFixtureItem(code string, id, usedBy int64, usedAt *string) InvitationCode {
	hash := sha256.Sum256([]byte(code))
	return InvitationCode{ID: id, Code: code, CodeHash: hex.EncodeToString(hash[:]), GeneratedAt: "2025-01-02 03:04:05", UsedBy: usedBy, UsedAt: usedAt}
}

func TestInvitationImportRecoveryBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("isolated database DSN not set")
			}
			snapshot, source := fixture(t)
			usedAt := "2025-01-02 03:05:06"
			for i := 1; i <= 210; i++ {
				item := invitationFixtureItem(fmt.Sprintf("synthetic-invitation-%03d", i), int64(i), 0, nil)
				if i > 101 {
					item.UsedBy = 73
					item.UsedAt = &usedAt
				}
				snapshot.InvitationCodes = append(snapshot.InvitationCodes, item)
			}
			db := isolatedStore(t, dialect, dsn)
			target := filepath.Join(t.TempDir(), "target")
			ctx := context.Background()
			key := bytes.Repeat([]byte{17}, 32)
			if _, err := Import(ctx, db, snapshot, source, target); err == nil {
				t.Fatal("invitation import accepted missing encryption key")
			}
			if _, err := db.DB.Exec("ALTER TABLE invitation_codes RENAME COLUMN encrypted_code TO unavailable_encrypted_code"); err != nil {
				t.Fatal(err)
			}
			if _, err := ImportWithSettingsKey(ctx, db, snapshot, source, target, key); err == nil {
				t.Fatal("injected invitation insert failure ignored")
			}
			var count int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil || count != 0 {
				t.Fatal("failed import left business data", err)
			}
			if _, err := db.DB.Exec("ALTER TABLE invitation_codes RENAME COLUMN unavailable_encrypted_code TO encrypted_code"); err != nil {
				t.Fatal(err)
			}
			report, err := ImportWithSettingsKey(ctx, db, snapshot, source, target, key)
			if err != nil || !report.Imported || report.Invitations.Total != 210 || report.Invitations.Unused != 101 || report.Invitations.Used != 109 {
				t.Fatal("invitation recovery counts differ", err)
			}
			for _, test := range []struct {
				where string
				count int
			}{{"1=1", 210}, {"remaining_uses=1 AND expires_at=0", 101}, {"remaining_uses=0 AND used_at IS NOT NULL AND source_user_id=73", 109}} {
				if err := db.DB.QueryRow("SELECT COUNT(*) FROM invitation_codes WHERE " + test.where).Scan(&count); err != nil || count != test.count {
					t.Fatal("migrated invitation state mismatch", err)
				}
			}
			for _, i := range []int{0, 100, 209} {
				item := snapshot.InvitationCodes[i]
				var sealed string
				if err := db.DB.QueryRow(db.Bind("SELECT encrypted_code FROM invitation_codes WHERE source_id=? AND code_hash=?"), item.ID, item.CodeHash).Scan(&sealed); err != nil {
					t.Fatal(err)
				}
				plain, err := securebox.Open(key, sealed, "wishingskin:invitation_code:v1")
				if err != nil || plain != item.Code || strings.Contains(sealed, item.Code) {
					t.Fatal("invitation encryption mismatch", err)
				}
			}
			again, err := ImportWithSettingsKey(ctx, db, snapshot, source, target, key)
			if err != nil || !again.AlreadyImported {
				t.Fatal("same-task invitation import not idempotent", err)
			}
			var persistedReport string
			if err := db.DB.QueryRow("SELECT report FROM migration_state WHERE id=1").Scan(&persistedReport); err != nil {
				t.Fatal(err)
			}
			reportJSON, _ := json.Marshal(report)
			if strings.Contains(string(reportJSON)+persistedReport, "synthetic-invitation-") {
				t.Fatal("invitation plaintext leaked to report")
			}
		})
	}
}

func TestInvitationPreflightPreservesSingleUseStatus(t *testing.T) {
	s, dir := fixture(t)
	usedAt := "2025-01-02 03:05:06"
	s.InvitationCodes = []InvitationCode{
		invitationFixtureItem("unused-code", 1, 0, nil),
		invitationFixtureItem("used-code", 2, 73, &usedAt),
	}
	r, err := Preflight(s, dir)
	if err != nil || r.Invitations.Total != 2 || r.Invitations.Unused != 1 || r.Invitations.Used != 1 {
		t.Fatalf("invitation report: %+v, %v", r.Invitations, err)
	}
}

func TestInvitationPreflightBlocksHashAndStatusMismatch(t *testing.T) {
	s, dir := fixture(t)
	usedAt := "2025-01-02 03:05:06"
	item := invitationFixtureItem("mismatch", 1, 0, &usedAt)
	item.CodeHash = strings.Repeat("a", 64)
	s.InvitationCodes = []InvitationCode{item}
	r, err := Preflight(s, dir)
	if err == nil {
		t.Fatal("expected invitation blockers")
	}
	var hash, status bool
	for _, blocker := range r.Blockers {
		hash = hash || blocker.Code == "invitation_hash"
		status = status || blocker.Code == "invitation_status"
	}
	if !hash || !status {
		t.Fatalf("missing invitation blockers: %+v", r.Blockers)
	}
}
