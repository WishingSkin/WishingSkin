package bsmigration

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/WishingSkin/WishingSkin/internal/credential"
)

var fixtureKeyOnce sync.Once
var fixtureKey string
var fixtureKeyErr error

func fixture(t *testing.T) (*Snapshot, string) {
	t.Helper()
	s, err := Load(filepath.Join("..", "..", "testdata", "bs", "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixtureKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 4096)
		fixtureKeyErr = err
		if err == nil {
			fixtureKey = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
		}
	})
	if fixtureKeyErr != nil {
		t.Fatal(fixtureKeyErr)
	}
	s.SignaturePrivateKey = fixtureKey
	dir := t.TempDir()
	for _, texture := range s.Textures {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "bs", texture.Hash))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, texture.Hash), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return s, dir
}

func TestLegacyHighResolutionCapes(t *testing.T) {
	for _, size := range []image.Point{{1280, 640}, {4096, 2048}, {4160, 2080}} {
		s, dir := fixture(t)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, image.NewNRGBA(image.Rectangle{Max: size})); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(encoded.Bytes())
		s.Textures[0].Hash = hex.EncodeToString(hash[:])
		s.Textures[0].Type = "cape"
		s.Characters[0].SkinID, s.Characters[0].CapeID = 0, s.Textures[0].ID
		if err := os.WriteFile(filepath.Join(dir, s.Textures[0].Hash), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		r, err := Preflight(s, dir)
		if size.X <= 4096 && (err != nil || r.Files != 1) {
			t.Fatalf("legacy cape %v was rejected", size)
		}
		if size.X > 4096 && (err == nil || r.Status != "blocked") {
			t.Fatal("oversized cape accepted")
		}
	}
}

func TestArchivedSourceContract(t *testing.T) {
	s, dir := fixture(t)
	s.Source.CoreCommit, s.Source.PluginCommit = "", ""
	s.Source.CoreTreeSHA256, s.Source.PluginTreeSHA256 = ArchivedCoreTree, ArchivedPluginTree
	s.Source.PluginVersion = ArchivedPluginVersion
	if report, err := Preflight(s, dir); err != nil || report.Status != "partial" {
		t.Fatal("audited archived source rejected")
	}
	for _, mutate := range []func(*Source){
		func(source *Source) { source.PluginTreeSHA256 = strings.Repeat("0", 64) },
		func(source *Source) { source.CoreTreeSHA256 = "" },
		func(source *Source) { source.CoreCommit = CoreCommit },
		func(source *Source) { source.PluginVersion = "5.2.2" },
	} {
		copy := *s
		mutate(&copy.Source)
		if report, err := Preflight(&copy, dir); err == nil || report.Status != "blocked" {
			t.Fatal("unverified source variant accepted")
		}
	}
}

func TestFixturePreservesActualIdentityAndLegacyPassword(t *testing.T) {
	s, dir := fixture(t)
	r, err := Preflight(s, dir)
	if err != nil {
		t.Fatalf("%v: %+v", err, r.Blockers)
	}
	if r.Status != "partial" || len(r.Unsupported) == 0 || !r.Signature || !r.SessionReset || r.Files != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
	u := s.Users[0]
	if ok, err := credential.Verify(s.Source.PasswordMethod, u.PasswordHash, s.Source.PasswordSalt, "password"); err != nil || !ok {
		t.Fatalf("fixture password not verified: %v", err)
	}
	if ok, err := credential.Verify(s.Source.PasswordMethod, u.PasswordHash, s.Source.PasswordSalt, "incorrect"); err != nil || ok {
		t.Fatalf("wrong password accepted: %v", err)
	}
	h := md5.Sum([]byte("OfflinePlayer:" + s.Characters[0].Name))
	h[6] = h[6]&0x0f | 0x30
	h[8] = h[8]&0x3f | 0x80
	if s.Characters[0].UUID == hex.EncodeToString(h[:]) {
		t.Fatal("fixture UUID must differ from name-generated UUID")
	}
}

func TestPreflightBlocksInvalidSource(t *testing.T) {
	tests := []struct {
		name, code string
		change     func(*Snapshot, string)
	}{
		{"duplicate UUID", "uuid_conflict", func(s *Snapshot, _ string) {
			c := s.Characters[0]
			c.ID++
			c.Name = "SecondPlayer"
			s.Characters = append(s.Characters, c)
		}},
		{"normalized email", "email_conflict", func(s *Snapshot, _ string) {
			u := s.Users[0]
			u.ID++
			u.Email = " fixture@example.INVALID "
			s.Users = append(s.Users, u)
		}},
		{"missing file", "texture_file", func(s *Snapshot, d string) { os.Remove(filepath.Join(d, s.Textures[0].Hash)) }},
		{"unknown password", "password_method", func(s *Snapshot, _ string) { s.Source.PasswordMethod = "custom-plugin" }},
		{"invalid password encoding", "password_format", func(s *Snapshot, _ string) { s.Users[0].PasswordHash = "not-a-bcrypt-hash" }},
		{"unverified digest salt", "password_format", func(s *Snapshot, _ string) {
			s.Source.PasswordMethod = "MD5"
			s.Source.PasswordSalt = "unknown-salt-formula"
			s.Users[0].PasswordHash = strings.Repeat("a", 32)
		}},
		{"hash path", "hash_path", func(s *Snapshot, _ string) { s.Textures[0].Hash = "../outside" }},
		{"hash filename case conflict", "hash_conflict", func(s *Snapshot, _ string) {
			s.Source.HashMode = "preserve-custom"
			s.Source.HashExplanation = "fixture filter"
			copy := s.Textures[0]
			copy.ID++
			copy.Hash = strings.ToUpper(copy.Hash)
			s.Textures = append(s.Textures, copy)
		}},
		{"wrong owner", "user_reference", func(s *Snapshot, _ string) { s.Characters[0].UserID = 999 }},
		{"wrong texture", "texture_reference", func(s *Snapshot, _ string) { s.Characters[0].SkinID = 999 }},
		{"wrong texture kind", "texture_reference", func(s *Snapshot, _ string) { s.Characters[0].CapeID = s.Textures[0].ID }},
		{"missing UUID", "uuid", func(s *Snapshot, _ string) { s.Characters[0].UUID = "" }},
		{"invalid PNG", "texture_png", func(s *Snapshot, d string) {
			s.Source.HashMode = "preserve-custom"
			s.Source.HashExplanation = "test filter"
			os.WriteFile(filepath.Join(d, s.Textures[0].Hash), []byte("not png"), 0600)
		}},
		{"changed hash", "texture_hash", func(s *Snapshot, d string) {
			b, _ := os.ReadFile(filepath.Join(d, s.Textures[0].Hash))
			s.Textures[0].Hash = strings.Repeat("a", 64)
			os.WriteFile(filepath.Join(d, s.Textures[0].Hash), b, 0600)
		}},
		{"unknown hash mode", "hash_mode", func(s *Snapshot, _ string) { s.Source.HashMode = "plugin" }},
		{"custom hash provenance", "hash_mode", func(s *Snapshot, _ string) { s.Source.HashMode = "preserve-custom" }},
		{"missing key", "signature_key", func(s *Snapshot, _ string) { s.SignaturePrivateKey = "" }},
		{"different core", "version", func(s *Snapshot, _ string) { s.Source.CoreVersion = "6.0.1" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, d := fixture(t)
			tt.change(s, d)
			r, err := Preflight(s, d)
			if err == nil || r.Status != "blocked" {
				t.Fatalf("must block: %+v", r)
			}
			for _, issue := range r.Blockers {
				if issue.Code == tt.code {
					return
				}
			}
			t.Fatalf("missing %s: %+v", tt.code, r.Blockers)
		})
	}
}

func TestLoadRejectsUnknownAndTrailingData(t *testing.T) {
	for _, value := range []string{`{"format_version":1,"unknown_plugin_data":[]}`, `{} {}`} {
		path := filepath.Join(t.TempDir(), "snapshot.json")
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
}

func TestCopyRecoveryAndTargetProtection(t *testing.T) {
	s, source := fixture(t)
	fp := fingerprint(s)
	target := filepath.Join(t.TempDir(), "target")
	j, err := prepareDirectory(s, source, target, fp)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyFiles(context.Background(), s, source, target, j); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDirectory(s, source, target, fp); err != nil {
		t.Fatal(err)
	}
	if err := copyFiles(context.Background(), s, source, target, j); err != nil {
		t.Fatal("same-task recovery:", err)
	}
	if err := os.WriteFile(filepath.Join(target, s.Textures[0].Hash), []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyFiles(context.Background(), s, source, target, j); err == nil {
		t.Fatal("existing corrupted file must not be overwritten")
	}
	b, _ := os.ReadFile(filepath.Join(target, s.Textures[0].Hash))
	if string(b) != "different" {
		t.Fatal("existing file was modified")
	}
	if _, err := prepareDirectory(s, source, target, strings.Repeat("0", 64)); err == nil {
		t.Fatal("different task accepted")
	}
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "unrelated"), []byte("preserve"), 0600)
	if _, err := prepareDirectory(s, source, other, fp); err == nil {
		t.Fatal("nonempty directory accepted")
	}
	if err := independentDirectory(source, source); err == nil {
		t.Fatal("same source/target accepted")
	}
	if err := independentDirectory(source, filepath.Join(source, "child")); err == nil {
		t.Fatal("nested target accepted")
	}
}

func TestMigrationTargetAcceptsOnlySettingsKeyBeforeJournal(t *testing.T) {
	s, source := fixture(t)
	fp := fingerprint(s)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("k", 32)
	keyPath := filepath.Join(target, ".settings.key")
	if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDirectory(s, source, target, fp); err != nil {
		t.Fatalf("valid initial settings key rejected: %v", err)
	}
	if _, err := prepareDirectory(s, source, target, fp); err != nil {
		t.Fatalf("settings key blocked journal recovery: %v", err)
	}
	if got, err := os.ReadFile(keyPath); err != nil || string(got) != key {
		t.Fatalf("settings key changed: %v", err)
	}

	invalid := filepath.Join(t.TempDir(), "invalid")
	if err := os.Mkdir(invalid, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalid, ".settings.key"), []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDirectory(s, source, invalid, fp); err == nil {
		t.Fatal("invalid initial settings key accepted")
	}
}

func TestCustomHashPreservationAndInterruptedCopy(t *testing.T) {
	s, source := fixture(t)
	b, err := os.ReadFile(filepath.Join(source, s.Textures[0].Hash))
	if err != nil {
		t.Fatal(err)
	}
	s.Source.HashMode = "preserve-custom"
	s.Source.HashExplanation = "Synthetic test-only source filter using a fixed hexadecimal identifier"
	s.Textures[0].Hash = strings.Repeat("c7", 16)
	if err := os.WriteFile(filepath.Join(source, s.Textures[0].Hash), b, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Preflight(s, source)
	if err != nil {
		t.Fatalf("custom source rejected: %v %+v", err, r.Blockers)
	}
	found := false
	for _, o := range r.Unsupported {
		if o.Scope == "texture_hash_filter" {
			found = true
		}
	}
	if !found {
		t.Fatal("custom filter verification gap missing")
	}
	target := filepath.Join(t.TempDir(), "target")
	j, err := prepareDirectory(s, source, target, fingerprint(s))
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(target, ".copy-"+s.Textures[0].Hash)
	if err := os.WriteFile(stage, []byte("interrupted partial copy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyFiles(context.Background(), s, source, target, j); err != nil {
		t.Fatal(err)
	}
	if err := verifyCopies(target, j); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("own temporary copy was not cleaned")
	}
	got, err := os.ReadFile(filepath.Join(target, s.Textures[0].Hash))
	if err != nil || string(got) != string(b) {
		t.Fatal("custom identifier or bytes changed", err)
	}
}

func TestReportsDoNotContainSigningKeyOrSalt(t *testing.T) {
	s, dir := fixture(t)
	s.Source.PasswordSalt = "secret-test-salt"
	r, err := Preflight(s, dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), s.SignaturePrivateKey) || strings.Contains(string(b), s.Source.PasswordSalt) || strings.Contains(string(b), s.Users[0].PasswordHash) {
		t.Fatal("report leaked credentials")
	}
}
