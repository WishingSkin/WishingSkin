package credential

import (
	"crypto/md5" // Legacy BS verification only; never used for new passwords.
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

var ErrUnsupported = errors.New("unsupported password format")

func Supported(method string) bool {
	switch strings.ToUpper(method) {
	case "ARGON2ID", "BCRYPT", "PHP_PASSWORD_HASH", "ARGON2I", "MD5", "SHA256", "SHA512":
		return true
	}
	return false
}

// Hash is deliberately bounded: two concurrent hashes use about 64 MiB total.
func Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 2, 32*1024, 1, 32)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=19$m=32768,t=2,p=1$%s$%s", b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

func Verify(method, hash, salt, password string) (bool, error) {
	method = strings.ToUpper(method)
	if err := Validate(method, hash, salt); err != nil {
		return false, ErrUnsupported
	}
	switch method {
	case "BCRYPT":
		return verifyBcrypt(hash, password)
	case "ARGON2I", "ARGON2ID", "PHP_PASSWORD_HASH":
		if strings.HasPrefix(hash, "$2") && method == "PHP_PASSWORD_HASH" {
			return verifyBcrypt(hash, password)
		}
		return verifyArgon(hash, password)
	case "MD5":
		sum := md5.Sum([]byte(password))
		return equalHex(hash, sum[:]), nil
	case "SHA256":
		sum := sha256.Sum256([]byte(password))
		return equalHex(hash, sum[:]), nil
	case "SHA512":
		sum := sha512.Sum512([]byte(password))
		return equalHex(hash, sum[:]), nil
	}
	return false, ErrUnsupported
}

func equalHex(hash string, expected []byte) bool {
	b, err := hex.DecodeString(hash)
	return err == nil && subtle.ConstantTimeCompare(b, expected) == 1
}

func verifyBcrypt(hash, password string) (bool, error) {
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil || cost > 14 {
		return false, ErrUnsupported
	}
	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	return err == nil, err
}

func verifyArgon(hash, password string) (bool, error) {
	p, salt, want, m, t, threads, err := parseArgon(hash)
	if err != nil {
		return false, err
	}
	var got []byte
	if p == "argon2id" {
		got = argon2.IDKey([]byte(password), salt, t, m, threads, uint32(len(want)))
	} else {
		got = argon2.Key([]byte(password), salt, t, m, threads, uint32(len(want)))
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// Validate checks source encoding and work factors without performing a hash.
func Validate(method, hash, salt string) error {
	method = strings.ToUpper(method)
	if !Supported(method) {
		return ErrUnsupported
	}
	if method == "BCRYPT" || (method == "PHP_PASSWORD_HASH" && strings.HasPrefix(hash, "$2")) {
		cost, err := bcrypt.Cost([]byte(hash))
		if err != nil || cost > 14 || len(hash) != 60 || hash[6] != '$' ||
			(hash[:4] != "$2a$" && hash[:4] != "$2b$" && hash[:4] != "$2y$") {
			return ErrUnsupported
		}
		encoding := base64.NewEncoding("./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789").WithPadding(base64.NoPadding).Strict()
		if _, err := encoding.DecodeString(hash[7:29]); err != nil {
			return ErrUnsupported
		}
		if _, err := encoding.DecodeString(hash[29:]); err != nil {
			return ErrUnsupported
		}
		return nil
	}
	if method == "ARGON2I" || method == "ARGON2ID" || method == "PHP_PASSWORD_HASH" {
		kind, _, _, _, _, _, err := parseArgon(hash)
		if err != nil {
			return err
		}
		if method != "PHP_PASSWORD_HASH" && strings.ToLower(method) != kind {
			return ErrUnsupported
		}
		return nil
	}
	// Digest methods here are unsalted. Other source formulas require a separate,
	// verified implementation rather than silently ignoring the site's salt.
	if salt != "" {
		return ErrUnsupported
	}
	sizes := map[string]int{"MD5": 16, "SHA256": 32, "SHA512": 64}
	b, err := hex.DecodeString(hash)
	if err != nil || len(b) != sizes[method] {
		return ErrUnsupported
	}
	return nil
}

func parseArgon(hash string) (kind string, salt, want []byte, m, t uint32, threads uint8, err error) {
	invalid := func() (string, []byte, []byte, uint32, uint32, uint8, error) {
		return "", nil, nil, 0, 0, 0, ErrUnsupported
	}
	p := strings.Split(hash, "$")
	if len(p) != 6 || (p[1] != "argon2id" && p[1] != "argon2i") || p[2] != "v=19" {
		return invalid()
	}
	fields := strings.Split(p[3], ",")
	if len(fields) != 3 {
		return invalid()
	}
	values := make([]uint64, 3)
	for i, prefix := range []string{"m=", "t=", "p="} {
		if !strings.HasPrefix(fields[i], prefix) {
			return invalid()
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(fields[i], prefix), 10, 32)
		if err != nil {
			return invalid()
		}
		values[i] = v
	}
	m, t = uint32(values[0]), uint32(values[1])
	if values[2] < 1 || values[2] > 4 || m < 8*uint32(values[2]) || m > 64*1024 || t < 1 || t > 4 {
		return invalid()
	}
	threads = uint8(values[2])
	salt, err = base64.RawStdEncoding.DecodeString(p[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return invalid()
	}
	want, err = base64.RawStdEncoding.DecodeString(p[5])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return invalid()
	}
	return p[1], salt, want, m, t, threads, nil
}
