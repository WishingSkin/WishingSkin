package credential

import (
	"strings"
	"testing"
)

func TestNewPasswordPolicy(t *testing.T) {
	for _, test := range []struct {
		password string
		valid    bool
	}{
		{"abc123!", false}, {"abc123!x", true}, {"abcdefgh", false}, {"abcdef12", false}, {"123456!@", false}, {"abc123  ", false},
		{"abcdef1😀", true}, {"密码密码123!", false}, {"abc123! " + strings.Repeat("界", 120), true}, {"abc123! " + strings.Repeat("界", 121), false},
	} {
		if ValidNewPassword(test.password) != test.valid {
			t.Errorf("policy mismatch for %d characters", len([]rune(test.password)))
		}
	}
}
