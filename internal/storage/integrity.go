package storage

import (
	"crypto/md5"  // Legacy texture filenames, never passwords.
	"crypto/sha1" // Legacy texture filenames, never signatures.
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"strings"
)

// ContentHasher selects the digest used by recognized texture filenames.
func ContentHasher(expected string) hash.Hash {
	switch len(expected) {
	case 32:
		return md5.New()
	case 40:
		return sha1.New()
	case 64:
		return sha256.New()
	case 128:
		return sha512.New()
	default:
		return nil
	}
}

func MatchesContentHash(data []byte, expected string) bool {
	h := ContentHasher(expected)
	if h == nil {
		return false
	}
	h.Write(data)
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), expected)
}
