package storage

import (
	"crypto/md5"  // Legacy texture filenames, never passwords.
	"crypto/sha1" // Legacy texture filenames, never signatures.
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"strings"
)

func MatchesContentHash(data []byte, expected string) bool {
	var digest []byte
	switch len(expected) {
	case 32:
		v := md5.Sum(data)
		digest = v[:]
	case 40:
		v := sha1.Sum(data)
		digest = v[:]
	case 64:
		v := sha256.Sum256(data)
		digest = v[:]
	case 128:
		v := sha512.Sum512(data)
		digest = v[:]
	default:
		return false
	}
	return hex.EncodeToString(digest) == strings.ToLower(expected)
}
