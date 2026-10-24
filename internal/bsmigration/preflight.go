package bsmigration

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"image/png"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"wishingskin/internal/credential"
)

var hashPattern = regexp.MustCompile(`^[0-9a-fA-F]{32,128}$`)
var invitationHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var sourceUUIDPattern = regexp.MustCompile(`(?i)^(?:[0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)

func normalizedUUID(v string) string { return strings.ToLower(strings.ReplaceAll(v, "-", "")) }
func emailKey(v string) string       { return strings.ToLower(strings.TrimSpace(v)) }
func fingerprint(s *Snapshot) string {
	b, _ := json.Marshal(s)
	v := sha256.Sum256(b)
	return hex.EncodeToString(v[:])
}

// Preflight reads files and source metadata without writing either source or target.
func Preflight(s *Snapshot, sourceDir string) (Report, error) {
	r := Report{Status: "partial", SessionReset: true, Unsupported: []Omitted{
		{Scope: "phase", Count: -1, Reason: "Accounts, characters, textures and signing key are imported; v2 also preserves uploader IDs, descriptions, wardrobe, explicit email verification, plain-text branding and read-only report history when present. OAuth, other site rules and extra plugin data are not migrated. Collection counts come from wardrobe relationships, not cached likes."},
		{Scope: "sessions", Count: -1, Reason: "Web sessions and game tokens are deliberately reset; all users must sign in again."},
	}}
	r.Points = PointsReport{Policy: "preserve_balance_disabled_manual_configuration", Source: "unavailable_in_snapshot"}
	r.Permissions.Policy = "source_superadmins_only"
	r.Branding = "unavailable_in_snapshot"
	block := func(code, scope, message string) { r.Blockers = append(r.Blockers, Issue{code, scope, message}) }
	if s == nil {
		block("snapshot", "source", "snapshot is nil")
		r.Status = "blocked"
		return r, fmt.Errorf("preflight blocked")
	}
	r.Fingerprint = fingerprint(s)
	checkLegacyReports(s, &r, block)
	r.Users = len(s.Users)
	r.Characters = len(s.Characters)
	r.Textures = len(s.Textures)
	r.Collections = len(s.Wardrobe)
	checkInvitations(s, &r, block)
	r.Unsupported = append(r.Unsupported, s.Omitted...)
	if len(s.Schema) == 0 {
		r.Unsupported = append(r.Unsupported, Omitted{Scope: "source_schema_inventory", Count: -1, Reason: "Snapshot has no source schema inventory; extra tables and columns have not been checked."})
	}
	r.Unsupported = append(r.Unsupported, Omitted{Scope: "source_runtime", Count: -1, Reason: "Installed PHP plugins, password hooks and runtime behavior were not inspected; source versions and configuration are operator-confirmed declarations."})
	if (s.FormatVersion != 1 && s.FormatVersion != FormatVersion) || s.Source.CoreVersion != CoreVersion || s.Source.CoreCommit != CoreCommit || s.Source.PluginVersion != PluginVersion || s.Source.PluginCommit != PluginCommit {
		block("version", "source", "source does not match the frozen BS/plugin contract")
	}
	if s.FormatVersion == 1 && len(s.Wardrobe) != 0 {
		block("version", "wardrobe", "wardrobe requires snapshot format v2")
	}
	if len(s.ScoreRules) != 0 {
		if s.FormatVersion == 1 {
			block("version", "score_rules", "score rules inventory requires snapshot format v2")
		}
		valid := len(s.ScoreRules) == len(scoreDefaults)
		for key, value := range s.ScoreRules {
			valid = valid && validScoreRule(key, value)
		}
		if !valid {
			block("score_rules", "score_rules", "score rule inventory is incomplete or invalid")
		} else {
			r.Points.Source = "frozen_core_defaults_with_database_overrides"
			r.Points.OriginalRules = s.ScoreRules
		}
	}
	if len(s.Branding) != 0 {
		if s.FormatVersion == 1 {
			block("version", "branding", "site branding requires snapshot format v2")
		}
		if !validBranding(s.Branding) {
			block("branding", "branding", "source branding is incomplete or exceeds plain-text limits; truncation is forbidden")
		} else {
			r.Branding = "preserved_as_plain_text"
		}
	}
	if strings.TrimSpace(s.Source.Instance) == "" {
		block("instance", "source", "source instance identifier is required")
	}
	if s.Source.UUIDAlgorithm != "v3" && s.Source.UUIDAlgorithm != "v4" {
		block("uuid_algorithm", "source", "unsupported UUID algorithm")
	}
	if !credential.Supported(s.Source.PasswordMethod) {
		block("password_method", "users", "unsupported password method")
	}
	if s.Source.HashMode != "sha256" && s.Source.HashMode != "preserve-custom" {
		block("hash_mode", "textures", "hash mode must be sha256 or preserve-custom")
	}
	if s.Source.HashMode == "preserve-custom" {
		if strings.TrimSpace(s.Source.HashExplanation) == "" {
			block("hash_mode", "textures", "custom source hash mode requires its filter/provenance explanation")
		}
		r.Unsupported = append(r.Unsupported, Omitted{Scope: "texture_hash_filter", Count: int64(len(s.Textures)), Reason: "Custom hash identifiers are preserved; only copy integrity is verified, not the source filter algorithm."})
	}
	if err := validatePrivateKey(s.SignaturePrivateKey); err != nil {
		block("signature_key", "signature", err.Error())
	} else {
		r.Signature = true
	}
	users := map[int64]bool{}
	emails := map[string]bool{}
	for _, u := range s.Users {
		if u.Verified == nil {
			r.Verification.Unknown++
		} else if *u.Verified {
			r.Verification.Verified++
		} else {
			r.Verification.Unverified++
		}
		switch u.Permission {
		case -1:
			r.Permissions.BannedUsers++
		case 0:
			r.Permissions.OrdinaryUsers++
		case 1:
			r.Permissions.AdministratorsRequiringReview++
		case 2:
			r.Permissions.SuperAdministrators++
		}
		scope := fmt.Sprintf("user:%d", u.ID)
		if s.FormatVersion == 1 && u.Verified != nil {
			block("version", scope, "explicit email verification requires snapshot format v2")
		}
		if u.ID <= 0 || users[u.ID] {
			block("duplicate_id", scope, "invalid or duplicate user ID")
		}
		users[u.ID] = true
		key := emailKey(u.Email)
		addr, err := mail.ParseAddress(key)
		if err != nil || addr.Address != key || len(key) > 100 {
			block("email", scope, "invalid email address")
		}
		if emails[key] {
			block("email_conflict", scope, "email normalization conflicts with another user")
		}
		emails[key] = true
		if u.PasswordHash == "" {
			block("password_hash", scope, "password hash is missing")
		}
		if credential.Supported(s.Source.PasswordMethod) {
			if err := credential.Validate(s.Source.PasswordMethod, u.PasswordHash, s.Source.PasswordSalt); err != nil {
				block("password_format", scope, "unsupported or invalid source password encoding/parameters")
			}
		}
		if u.Permission < -1 || u.Permission > 2 {
			block("permission", scope, "unsupported permission value")
		}
	}
	textures := map[int64]Texture{}
	if s.FormatVersion == 1 && len(s.Textures) > 0 {
		r.Unsupported = append(r.Unsupported, Omitted{Scope: "texture_ownership", Count: int64(len(s.Textures)), Reason: "This snapshot does not contain texture uploader IDs or descriptions; records retain unknown owners and empty descriptions. Character bindings do not establish ownership."})
	}
	hashNames := map[string]string{}
	for _, t := range s.Textures {
		scope := fmt.Sprintf("texture:%d", t.ID)
		if s.FormatVersion == 1 && (t.Uploader != nil || t.Description != "") {
			block("version", scope, "uploader and description require snapshot format v2")
		}
		if s.FormatVersion == 2 && t.Uploader == nil {
			block("texture_owner", scope, "v2 requires the actual source uploader ID; zero denotes no uploader")
		}
		if t.Uploader != nil && (*t.Uploader < 0 || (*t.Uploader > 0 && !users[*t.Uploader])) {
			block("user_reference", scope, "texture uploader does not exist")
		}
		if t.Uploader != nil && *t.Uploader == 0 {
			r.Unsupported = append(r.Unsupported, Omitted{Scope: scope + ":uploader", Count: 1, Reason: "Source uploader ID is zero; owner remains unknown."})
		}
		if !utf8.ValidString(t.Description) || utf8.RuneCountInString(t.Description) > 200 {
			block("description", scope, "description exceeds 200 Unicode code points or is invalid UTF-8; truncation is forbidden")
		}
		if _, ok := textures[t.ID]; t.ID <= 0 || ok {
			block("duplicate_id", scope, "invalid or duplicate texture ID")
		}
		textures[t.ID] = t
		if !hashPattern.MatchString(t.Hash) {
			block("hash_path", scope, "hash must be 32-128 hexadecimal filename characters")
			continue
		}
		key := strings.ToLower(t.Hash)
		previous, seenFile := hashNames[key]
		if seenFile && previous != t.Hash {
			block("hash_conflict", scope, "texture filenames differ only by case")
		}
		hashNames[key] = t.Hash
		if t.Type != "steve" && t.Type != "alex" && t.Type != "cape" {
			block("texture_type", scope, "unsupported texture type")
		}
		b, err := readTexture(sourceDir, t.Hash)
		if err != nil {
			block("texture_file", scope, err.Error())
			continue
		}
		if s.Source.HashMode == "sha256" {
			h := sha256.Sum256(b)
			if hex.EncodeToString(h[:]) != t.Hash {
				block("texture_hash", scope, "file SHA-256 differs from source hash")
			}
		}
		config, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil || !validDimensions(t.Type, config.Width, config.Height) {
			block("texture_png", scope, "invalid PNG or unsupported Minecraft texture dimensions")
			continue
		}
		if _, err := png.Decode(bytes.NewReader(b)); err != nil {
			block("texture_png", scope, "PNG could not be decoded")
			continue
		}
		if !seenFile {
			r.Files++
		}
	}
	collections := map[[2]int64]bool{}
	for _, c := range s.Wardrobe {
		scope := fmt.Sprintf("wardrobe:%d:%d", c.UserID, c.TextureID)
		if !users[c.UserID] {
			block("user_reference", scope, "collection user does not exist")
		}
		if _, ok := textures[c.TextureID]; !ok {
			block("texture_reference", scope, "collection texture does not exist")
		}
		key := [2]int64{c.UserID, c.TextureID}
		if collections[key] {
			block("collection_conflict", scope, "duplicate collection; automatic merging is forbidden")
		}
		collections[key] = true
		if c.Name != nil && (!utf8.ValidString(*c.Name) || utf8.RuneCountInString(*c.Name) > 255) {
			block("collection_name", scope, "collection name exceeds 255 Unicode code points or is invalid UTF-8")
		}
	}
	ids := map[int64]bool{}
	names := map[string]bool{}
	uuids := map[string]bool{}
	for _, c := range s.Characters {
		scope := fmt.Sprintf("character:%d", c.ID)
		if c.ID <= 0 || ids[c.ID] {
			block("duplicate_id", scope, "invalid or duplicate character ID")
		}
		ids[c.ID] = true
		if !users[c.UserID] {
			block("user_reference", scope, "character owner does not exist")
		}
		if !namePattern.MatchString(c.Name) {
			block("name", scope, "character name must match [A-Za-z0-9_]{1,16}")
		}
		key := strings.ToLower(c.Name)
		if names[key] {
			block("name_conflict", scope, "character name normalization conflicts")
		}
		names[key] = true
		uuid := normalizedUUID(c.UUID)
		if !sourceUUIDPattern.MatchString(c.UUID) || !uuidPattern.MatchString(uuid) || uuid == strings.Repeat("0", 32) {
			block("uuid", scope, "missing or invalid source UUID; generation is forbidden")
		}
		if uuids[uuid] {
			block("uuid_conflict", scope, "duplicate source UUID")
		}
		uuids[uuid] = true
		for _, ref := range []struct {
			id   int64
			kind string
		}{{c.SkinID, "skin"}, {c.CapeID, "cape"}} {
			if ref.id == 0 {
				continue
			}
			t, ok := textures[ref.id]
			if !ok || ((ref.kind == "cape") != (t.Type == "cape")) {
				block("texture_reference", scope, "missing or wrong-kind "+ref.kind+" texture reference")
			}
		}
	}
	if len(r.Blockers) > 0 {
		r.Status = "blocked"
		return r, fmt.Errorf("preflight blocked by %d issue(s)", len(r.Blockers))
	}
	return r, nil
}

func checkInvitations(s *Snapshot, r *Report, block func(string, string, string)) {
	if len(s.InvitationCodes) == 0 {
		return
	}
	r.Invitations = InvitationReport{Source: "included", Total: len(s.InvitationCodes)}
	ids := map[int64]bool{}
	hashes := map[string]bool{}
	for _, item := range s.InvitationCodes {
		scope := fmt.Sprintf("invitation:%d", item.ID)
		if item.ID <= 0 || ids[item.ID] {
			block("duplicate_id", scope, "invalid or duplicate source invitation ID")
		}
		ids[item.ID] = true
		if item.Code == "" || len(item.Code) > 255 || !utf8.ValidString(item.Code) {
			block("invitation_code", scope, "source invitation code is empty, too long or invalid UTF-8")
		}
		hash := sha256.Sum256([]byte(item.Code))
		expected := hex.EncodeToString(hash[:])
		if !invitationHashPattern.MatchString(item.CodeHash) || item.CodeHash != expected {
			block("invitation_hash", scope, "source invitation SHA-256 does not match its value")
		}
		if hashes[expected] {
			block("invitation_conflict", scope, "duplicate invitation value")
		}
		hashes[expected] = true
		if item.UsedBy < 0 {
			block("invitation_status", scope, "source invitation user ID cannot be negative")
		}
		used := item.UsedBy != 0 || item.UsedAt != nil
		if used {
			r.Invitations.Used++
		} else {
			r.Invitations.Unused++
		}
		if item.UsedBy == 0 && item.UsedAt != nil || item.UsedBy != 0 && item.UsedAt == nil {
			block("invitation_status", scope, "source invitation used_by and used_at disagree")
		}
		if _, err := parseSourceTime(item.GeneratedAt, false); err != nil {
			block("invitation_time", scope, "source invitation generated_at is invalid")
		}
		if item.UsedAt != nil {
			if _, err := parseSourceTime(*item.UsedAt, true); err != nil {
				block("invitation_time", scope, "source invitation used_at is invalid")
			}
		}
	}
}

func parseSourceTime(value string, nullable bool) (int64, error) {
	if nullable && value == "" {
		return 0, nil
	}
	if value == "0000-00-00 00:00:00" {
		return 0, fmt.Errorf("zero timestamp")
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	if err != nil {
		parsed, err = time.ParseInLocation("2006-01-02 15:04:05.999999", value, time.UTC)
	}
	if err != nil {
		return 0, err
	}
	return parsed.Unix(), nil
}

func validatePrivateKey(value string) error {
	b, rest := pem.Decode([]byte(value))
	if b == nil || len(bytes.TrimSpace(rest)) != 0 {
		return fmt.Errorf("missing or invalid PEM signing private key")
	}
	var key *rsa.PrivateKey
	if b.Type == "RSA PRIVATE KEY" {
		key, _ = x509.ParsePKCS1PrivateKey(b.Bytes)
	} else if b.Type == "PRIVATE KEY" {
		v, err := x509.ParsePKCS8PrivateKey(b.Bytes)
		if err == nil {
			key, _ = v.(*rsa.PrivateKey)
		}
	}
	if key == nil || key.N.BitLen() < 4096 {
		return fmt.Errorf("signing key must be an unencrypted RSA private key of at least 4096 bits")
	}
	if err := key.Validate(); err != nil {
		return fmt.Errorf("invalid RSA signing key")
	}
	return nil
}

func validDimensions(kind string, w, h int) bool {
	if kind == "cape" {
		return w >= 64 && w <= 1024 && w%64 == 0 && h*2 == w
	}
	return w >= 64 && w <= 1024 && w%64 == 0 && (h == w || h*2 == w)
}

func readTexture(dir, hash string) ([]byte, error) {
	if !hashPattern.MatchString(hash) {
		return nil, fmt.Errorf("unsafe texture filename")
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("source texture directory is unavailable")
	}
	path := filepath.Join(root, hash)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("source texture file is missing")
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil, fmt.Errorf("source texture must be a regular file up to 8 MiB")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, fmt.Errorf("source texture links are not supported")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("source texture file could not be read")
	}
	return b, nil
}
