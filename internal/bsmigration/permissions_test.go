package bsmigration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func permissionFixture(t *testing.T) (*Snapshot, string) {
	s, dir := relationshipFixture(t)
	s.Users[0].Permission = 1
	for i, permission := range []int{2, 0, -1} {
		u := s.Users[0]
		u.ID = int64(8 + i)
		u.Email = []string{"super@example.invalid", "user@example.invalid", "banned@example.invalid"}[i]
		u.Permission = permission
		s.Users = append(s.Users, u)
	}
	return s, dir
}

func TestPermissionMigrationReport(t *testing.T) {
	s, dir := permissionFixture(t)
	r, err := Preflight(s, dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r)
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"policy": "source_superadmins_only", "super_administrators": float64(1), "administrators_requiring_review": float64(1), "ordinary_users": float64(1), "banned_users": float64(1)}
	if !reflect.DeepEqual(decoded["permissions"], want) {
		t.Fatalf("permission policy/counts: got %v, want %v", decoded["permissions"], want)
	}
	for _, user := range s.Users {
		if strings.Contains(string(raw), user.Email) || strings.Contains(string(raw), user.PasswordHash) {
			t.Fatal("permission report exposes account data")
		}
	}
}

func TestPermissionMigrationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("test DSN unset")
			}
			db := isolatedStore(t, dialect, dsn)
			s, dir := permissionFixture(t)
			if r, err := Import(context.Background(), db, s, dir, filepath.Join(t.TempDir(), "textures")); err != nil || !r.Imported {
				t.Fatal(r, err)
			}
			for _, u := range s.Users {
				var permission int
				var admin, banned bool
				if err := db.DB.QueryRow(db.Bind("SELECT permission,permission>=2,banned FROM users WHERE id=?"), u.ID).Scan(&permission, &admin, &banned); err != nil || permission != u.Permission || admin != (u.Permission == 2) || banned != (u.Permission == -1) {
					t.Fatal("source role mapping", u.ID, permission, admin, banned, err)
				}
			}
		})
	}
}
