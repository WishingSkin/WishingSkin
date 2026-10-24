// Package bsmigration imports a deliberately limited, offline Blessing Skin snapshot.
package bsmigration

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const (
	FormatVersion = 2
	CoreVersion   = "6.0.2"
	CoreCommit    = "b737a65f5f5fce1f897f8603db74fd970437b351"
	PluginVersion = "5.2.1"
	PluginCommit  = "de016f4cbb43df7bd6d46193d5625693f8c79b3f"
)

type Source struct {
	Instance        string `json:"instance"`
	CoreVersion     string `json:"core_version"`
	CoreCommit      string `json:"core_commit"`
	PluginVersion   string `json:"plugin_version"`
	PluginCommit    string `json:"plugin_commit"`
	PasswordMethod  string `json:"password_method"`
	PasswordSalt    string `json:"password_salt"`
	HashMode        string `json:"hash_mode"` // sha256, or preserve-custom with a documented filter
	HashExplanation string `json:"hash_explanation,omitempty"`
	UUIDAlgorithm   string `json:"uuid_algorithm"`
}

type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Nickname     string `json:"nickname"`
	PasswordHash string `json:"password_hash"`
	Score        int64  `json:"score"`
	Permission   int    `json:"permission"`
	Verified     *bool  `json:"verified,omitempty"`
}

type Character struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	UUID   string `json:"uuid"`
	SkinID int64  `json:"skin_id"`
	CapeID int64  `json:"cape_id"`
}

type Texture struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // BS steve, alex, cape
	Hash        string `json:"hash"`
	Public      bool   `json:"public"`
	Uploader    *int64 `json:"uploader,omitempty"`
	Description string `json:"description,omitempty"`
}

type Collection struct {
	UserID    int64   `json:"user_id"`
	TextureID int64   `json:"texture_id"`
	Name      *string `json:"name"`
}

// Source references are historical values, not foreign keys to current users or records.
type LegacyReport struct {
	ID       int64   `json:"id"`
	Texture  int64   `json:"tid"`
	Uploader int64   `json:"uploader"`
	Reporter int64   `json:"reporter"`
	Reason   string  `json:"reason"`
	Status   int     `json:"status"`
	ReportAt *string `json:"report_at"`
}

// A missing wrapper means unavailable; an explicit empty list means checked and empty.
type LegacyReports struct {
	Items []LegacyReport `json:"items"`
}

// Omitted records scope/counts without exporting unrelated personal data.
type Omitted struct {
	Scope  string `json:"scope"`
	Count  int64  `json:"count"`
	Reason string `json:"reason"`
}

type Snapshot struct {
	FormatVersion       int                 `json:"format_version"`
	Source              Source              `json:"source"`
	Users               []User              `json:"users"`
	Characters          []Character         `json:"characters"`
	Textures            []Texture           `json:"textures"`
	SignaturePrivateKey string              `json:"signature_private_key"`
	Omitted             []Omitted           `json:"omitted"`
	Schema              map[string][]string `json:"schema,omitempty"`
	Wardrobe            []Collection        `json:"wardrobe,omitempty"`
	ScoreRules          map[string]string   `json:"score_rules,omitempty"`
	Branding            map[string]string   `json:"branding,omitempty"`
	LegacyReports       *LegacyReports      `json:"legacy_reports,omitempty"`
}

type Issue struct {
	Code    string `json:"code"`
	Scope   string `json:"scope"`
	Message string `json:"message"`
}

type Report struct {
	Status          string              `json:"status"` // blocked, partial; never complete in this phase
	Fingerprint     string              `json:"source_fingerprint"`
	Users           int                 `json:"users"`
	Characters      int                 `json:"characters"`
	Textures        int                 `json:"textures"`
	Files           int                 `json:"files"`
	Collections     int                 `json:"collections,omitempty"`
	Signature       bool                `json:"signature"`
	SessionReset    bool                `json:"session_reset"`
	Imported        bool                `json:"imported"`
	AlreadyImported bool                `json:"already_imported"`
	Blockers        []Issue             `json:"blockers"`
	Unsupported     []Omitted           `json:"unsupported"`
	Points          PointsReport        `json:"points"`
	Permissions     PermissionsReport   `json:"permissions"`
	Verification    VerificationReport  `json:"verification"`
	Branding        string              `json:"branding"`
	LegacyReports   LegacyReportsReport `json:"legacy_reports"`
}

type LegacyReportsReport struct {
	Policy   string `json:"policy"`
	Source   string `json:"source"`
	Total    int    `json:"total"`
	Pending  int    `json:"pending"`
	Resolved int    `json:"resolved"`
	Rejected int    `json:"rejected"`
}

type VerificationReport struct {
	Verified   int `json:"verified"`
	Unverified int `json:"unverified"`
	Unknown    int `json:"unknown"`
}

type PermissionsReport struct {
	Policy                        string `json:"policy"`
	SuperAdministrators           int    `json:"super_administrators"`
	AdministratorsRequiringReview int    `json:"administrators_requiring_review"`
	OrdinaryUsers                 int    `json:"ordinary_users"`
	BannedUsers                   int    `json:"banned_users"`
}

type PointsReport struct {
	Policy        string            `json:"policy"`
	Source        string            `json:"source"`
	OriginalRules map[string]string `json:"original_rules,omitempty"`
}

func Load(path string) (*Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 64<<20 {
		return nil, fmt.Errorf("snapshot exceeds 64 MiB limit")
	}
	d := json.NewDecoder(io.LimitReader(f, 64<<20))
	d.DisallowUnknownFields()
	var s Snapshot
	if err := d.Decode(&s); err != nil {
		return nil, fmt.Errorf("snapshot JSON: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("snapshot contains trailing data")
	}
	return &s, nil
}
