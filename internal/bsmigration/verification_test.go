package bsmigration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func verificationFixture(t *testing.T) (*Snapshot, string) {
	t.Helper()
	s, dir := relationshipFixture(t)
	for _, id := range []int64{8, 9} {
		u := s.Users[0]
		u.ID = id
		u.Email = map[int64]string{8: "unverified@example.invalid", 9: "unknown@example.invalid"}[id]
		s.Users = append(s.Users, u)
	}
	raw, _ := json.Marshal(s)
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	users := document["users"].([]any)
	users[0].(map[string]any)["verified"] = true
	users[1].(map[string]any)["verified"] = false
	raw, _ = json.Marshal(document)
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal("explicit source verification state rejected", err)
	}
	return s, dir
}

func TestVerificationSnapshotAndReport(t *testing.T) {
	s, dir := verificationFixture(t)
	r, err := Preflight(s, dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r)
	var report struct {
		Verification struct {
			Verified, Unverified, Unknown int
		}
	}
	if err := json.Unmarshal(raw, &report); err != nil || report.Verification.Verified != 1 || report.Verification.Unverified != 1 || report.Verification.Unknown != 1 {
		t.Fatal("source verification counts", report, err)
	}
	s.FormatVersion = 1
	s.Wardrobe = nil
	s.Textures[0].Uploader = nil
	s.Textures[0].Description = ""
	legacy, err := Preflight(s, dir)
	if err == nil {
		t.Fatal("v1 cannot claim explicit email verification")
	}
	var verificationBlock bool
	for _, issue := range legacy.Blockers {
		verificationBlock = verificationBlock || issue.Code == "version" && strings.Contains(issue.Message, "email verification")
	}
	if !verificationBlock {
		t.Fatal("v1 verification blocker missing", legacy.Blockers)
	}
}

func TestVerificationImportBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("test DSN unset")
			}
			db := isolatedStore(t, dialect, dsn)
			s, dir := verificationFixture(t)
			target := filepath.Join(t.TempDir(), "textures")
			undo := installFailure(t, db)
			if r, err := Import(context.Background(), db, s, dir, target); err == nil || r.Imported {
				t.Fatal("expected injected failure", err)
			}
			var count int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM account_states").Scan(&count); err != nil || count != 0 {
				t.Fatal("email state did not roll back", count, err)
			}
			undo()
			if r, err := Import(context.Background(), db, s, dir, target); err != nil || !r.Imported {
				t.Fatal(r, err)
			}
			for _, id := range []int64{7, 8, 9} {
				var verified, required bool
				if err := db.DB.QueryRow(db.Bind("SELECT COALESCE(s.email_verified,FALSE),COALESCE(s.verification_required,FALSE) FROM users u LEFT JOIN account_states s ON s.user_id=u.id WHERE u.id=?"), id).Scan(&verified, &required); err != nil || verified != (id == 7) || required {
					t.Fatal("migrated email state/gate", id, verified, required, err)
				}
			}
			if r, err := Import(context.Background(), db, s, dir, target); err != nil || !r.AlreadyImported {
				t.Fatal("same-task recovery", r, err)
			}
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM account_states").Scan(&count); err != nil || count != 1 {
				t.Fatal("email states duplicated or invented", count, err)
			}
		})
	}
}
