package bsmigration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScoreInventoryPolicy(t *testing.T) {
	s, dir := relationshipFixture(t)
	s.ScoreRules = map[string]string{}
	for key, value := range scoreDefaults {
		s.ScoreRules[key] = value
	}
	s.ScoreRules["sign_score"] = "3,30"
	r, err := Preflight(s, dir)
	if err != nil || r.Points.OriginalRules["sign_score"] != "3,30" || r.Points.Policy != "preserve_balance_disabled_manual_configuration" {
		t.Fatal("missing score report", err)
	}
	s.ScoreRules["smtp_password"] = "never-report-secret"
	r, err = Preflight(s, dir)
	encoded, _ := json.Marshal(r)
	if err == nil || strings.Contains(string(encoded), "never-report-secret") || strings.Contains(string(encoded), "smtp_password") {
		t.Fatal("unknown rule leaked or accepted")
	}
	delete(s.ScoreRules, "smtp_password")
	s.ScoreRules["sign_score"] = "never-report-secret"
	r, err = Preflight(s, dir)
	encoded, _ = json.Marshal(r)
	if err == nil || strings.Contains(string(encoded), "never-report-secret") {
		t.Fatal("invalid rule leaked or accepted")
	}
	delete(s.ScoreRules, "sign_score")
	if _, err := Preflight(s, dir); err == nil {
		t.Fatal("partial inventory accepted")
	}
	s.ScoreRules = nil
	r, err = Preflight(s, dir)
	if err != nil || r.Points.Source != "unavailable_in_snapshot" || len(r.Points.OriginalRules) != 0 {
		t.Fatal("older snapshot invented original rules", err)
	}
}
