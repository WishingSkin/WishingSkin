package bsmigration

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func invitationFixtureItem(code string, id, usedBy int64, usedAt *string) InvitationCode {
	hash := sha256.Sum256([]byte(code))
	return InvitationCode{ID: id, Code: code, CodeHash: hex.EncodeToString(hash[:]), GeneratedAt: "2025-01-02 03:04:05", UsedBy: usedBy, UsedAt: usedAt}
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
