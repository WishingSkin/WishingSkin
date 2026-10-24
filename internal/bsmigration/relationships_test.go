package bsmigration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func relationshipFixture(t *testing.T) (*Snapshot, string) {
	s, source := fixture(t)
	s.FormatVersion = FormatVersion
	s.Textures[0].Uploader = &s.Users[0].ID
	s.Textures[0].Description = strings.Repeat("😀", 200)
	alias := "Private collection <b>plain</b>"
	s.Wardrobe = []Collection{{UserID: 7, TextureID: 11, Name: &alias}}
	return s, source
}

func TestLegacySnapshotFingerprintUnchanged(t *testing.T) {
	s, err := Load(filepath.Join("..", "..", "testdata", "bs", "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Frozen canonical v1 fixture hash, before v2 fields were introduced.
	if got := fingerprint(s); got != "b189ef68c51bf2b12afa704bec484d9d7c5106921e2b54bc0d55b7a7736282b2" {
		t.Fatal("legacy task fingerprint changed", got)
	}
}

func TestRelationshipPreflightRejectsDataLoss(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*Snapshot)
	}{
		{"missing uploader", func(s *Snapshot) { s.Textures[0].Uploader = nil }},
		{"unknown uploader", func(s *Snapshot) { id := int64(999); s.Textures[0].Uploader = &id }},
		{"negative uploader", func(s *Snapshot) { id := int64(-1); s.Textures[0].Uploader = &id }},
		{"long description", func(s *Snapshot) { s.Textures[0].Description += "😀" }},
		{"missing user", func(s *Snapshot) { s.Wardrobe[0].UserID = 999 }},
		{"missing texture", func(s *Snapshot) { s.Wardrobe[0].TextureID = 999 }},
		{"duplicate collection", func(s *Snapshot) { s.Wardrobe = append(s.Wardrobe, s.Wardrobe[0]) }},
		{"long alias", func(s *Snapshot) { name := strings.Repeat("😀", 256); s.Wardrobe[0].Name = &name }},
		{"v1 cannot claim relationships", func(s *Snapshot) { s.FormatVersion = 1 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, source := relationshipFixture(t)
			c.change(s)
			if r, err := Preflight(s, source); err == nil || r.Status != "blocked" || len(r.Blockers) == 0 {
				t.Fatal("invalid relationships accepted", r, err)
			}
		})
	}
}

func TestRelationshipImportRecoveryBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("test DSN unset")
			}
			db := isolatedStore(t, dialect, dsn)
			s, source := relationshipFixture(t)
			other := s.Users[0]
			other.ID, other.Email = 8, "other@example.invalid"
			s.Users = append(s.Users, other)
			shared := s.Textures[0]
			shared.ID, shared.Name, shared.Type = 12, "Separate owner", "steve"
			shared.Uploader = &other.ID
			s.Textures = append(s.Textures, shared)
			s.Wardrobe = append(s.Wardrobe, Collection{UserID: 8, TextureID: 11}, Collection{UserID: 7, TextureID: 12})
			unknown := shared
			zero := int64(0)
			unknown.ID, unknown.Uploader = 13, &zero
			s.Textures = append(s.Textures, unknown)
			var capePNG bytes.Buffer
			if err := png.Encode(&capePNG, image.NewNRGBA(image.Rect(0, 0, 64, 32))); err != nil {
				t.Fatal(err)
			}
			cape := s.Textures[0]
			capeSum := sha256.Sum256(capePNG.Bytes())
			cape.ID, cape.Name, cape.Type, cape.Hash = 14, "Migrated cape", "cape", hex.EncodeToString(capeSum[:])
			if err := os.WriteFile(filepath.Join(source, cape.Hash), capePNG.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			s.Textures = append(s.Textures, cape)
			s.Characters[0].CapeID = cape.ID
			s.Wardrobe = append(s.Wardrobe, Collection{UserID: 7, TextureID: cape.ID})
			target := filepath.Join(t.TempDir(), "textures")
			undo := installFailure(t, db)
			if r, err := Import(context.Background(), db, s, source, target); err == nil || r.Imported {
				t.Fatal("injected character failure ignored")
			}
			for _, table := range []string{"users", "texture_records", "wardrobe", "characters"} {
				var count int
				if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatal("relationship rollback", table, count, err)
				}
			}
			undo()
			r, err := Import(context.Background(), db, s, source, target)
			if err != nil || !r.Imported || r.Files != 2 || r.Textures != 4 || r.Collections != 4 {
				t.Fatal("recovery", r, err)
			}
			for _, c := range []struct {
				query string
				want  int
			}{
				{"SELECT COUNT(*) FROM textures", 2},
				{"SELECT COUNT(*) FROM texture_records WHERE owner_id=7", 2},
				{"SELECT COUNT(*) FROM texture_records WHERE owner_id=8", 1},
				{"SELECT COUNT(*) FROM texture_records WHERE owner_id IS NULL", 1},
				{"SELECT COUNT(*) FROM wardrobe WHERE collected_at=0", 4},
				{"SELECT COUNT(*) FROM account_states", 0},
				{"SELECT COUNT(*) FROM web_sessions", 0},
				{"SELECT COUNT(*) FROM game_tokens", 0},
			} {
				var count int
				if err := db.DB.QueryRow(c.query).Scan(&count); err != nil || count != c.want {
					t.Fatal("v2 mismatch", c.query, count, err)
				}
			}
			var name, description string
			var skinHash, capeHash, model, uuid string
			if err := db.DB.QueryRow("SELECT skin_hash,cape_hash,model,uuid FROM characters WHERE id=19").Scan(&skinHash, &capeHash, &model, &uuid); err != nil || skinHash != s.Textures[0].Hash || capeHash != cape.Hash || model != "slim" || uuid != s.Characters[0].UUID {
				t.Fatal("combined skin/cape identity changed on recovery", err)
			}
			if err := db.DB.QueryRow("SELECT w.display_name,r.description FROM wardrobe w JOIN texture_records r ON w.record_id=r.record_id WHERE w.user_id=8").Scan(&name, &description); err != nil || name != s.Textures[0].Name || description != s.Textures[0].Description {
				t.Fatal("null alias or description lost", err)
			}
			if r, err := Import(context.Background(), db, s, source, target); err != nil || !r.AlreadyImported {
				t.Fatal("idempotent relationships", r, err)
			}
			raw, _ := json.Marshal(r)
			if strings.Contains(string(raw), s.Textures[0].Description) || strings.Contains(string(raw), *s.Wardrobe[0].Name) || strings.Contains(string(raw), s.SignaturePrivateKey) {
				t.Fatal("report copied private contents")
			}
		})
	}
}
