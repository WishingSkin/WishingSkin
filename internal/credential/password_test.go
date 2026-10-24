package credential

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestLegacyAndNewPasswords(t *testing.T) {
	b, err := bcrypt.GenerateFromPassword([]byte("legacy-password"), 4)
	if err != nil {
		t.Fatal(err)
	}
	modern, err := Hash("long-password-可迁移")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ method, hash, password string }{
		{"BCRYPT", string(b), "legacy-password"},
		{"PHP_PASSWORD_HASH", string(b), "legacy-password"},
		{"ARGON2ID", modern, "long-password-可迁移"},
		{"MD5", "5f4dcc3b5aa765d61d8327deb882cf99", "password"},
		{"SHA256", "5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8", "password"},
	} {
		t.Run(tt.method, func(t *testing.T) {
			ok, err := Verify(tt.method, tt.hash, "", tt.password)
			if err != nil || !ok {
				t.Fatalf("correct password rejected: %v", err)
			}
			ok, err = Verify(tt.method, tt.hash, "", "wrong")
			if err != nil || ok {
				t.Fatalf("wrong password accepted or error: %v", err)
			}
		})
	}
}

func TestRejectSaltedDigestsAndCorruptBcrypt(t *testing.T) {
	for _, tt := range []struct{ method, hash, salt string }{
		{"MD5", "5f4dcc3b5aa765d61d8327deb882cf99", "site-salt"},
		{"SHA256", strings.Repeat("0", 64), "site-salt"},
		{"SHA512", strings.Repeat("0", 128), "site-salt"},
		{"BCRYPT", "$2a$04$" + strings.Repeat("!", 53), ""},
		{"PHP_PASSWORD_HASH", "$2a$04$" + strings.Repeat("!", 53), ""},
		{"BCRYPT", "$2z$04$" + strings.Repeat(".", 53), ""},
		{"BCRYPT", "$2a$04$" + strings.Repeat(".", 21) + "/" + strings.Repeat(".", 31), ""},
		{"BCRYPT", "$2a$04$" + strings.Repeat(".", 52) + "/", ""},
	} {
		if err := Validate(tt.method, tt.hash, tt.salt); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("accepted invalid %s credential", tt.method)
		}
		if _, err := Verify(tt.method, tt.hash, tt.salt, "password"); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("verification accepted invalid %s credential", tt.method)
		}
	}
}

func TestRejectUnboundedOrUnsupportedHash(t *testing.T) {
	for _, hash := range []string{
		"$argon2id$v=19$m=4294967295,t=2,p=1$c2FsdHNhbHQ$MTIzNDU2Nzg5MDEyMzQ1Ng",
		"$argon2id$v=19$m=32768,t=0,p=1$c2FsdHNhbHQ$MTIzNDU2Nzg5MDEyMzQ1Ng",
		"$argon2id$v=19$m=32768,t=2,p=0$c2FsdHNhbHQ$MTIzNDU2Nzg5MDEyMzQ1Ng",
	} {
		if _, err := Verify("ARGON2ID", hash, "", "password"); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("expected bounds rejection: %v", err)
		}
	}
	if _, err := Verify("PLUGIN_CUSTOM", "value", "", "password"); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}
