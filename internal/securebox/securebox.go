// Package securebox provides the small AES-GCM envelope used for recoverable
// site secrets. Callers must provide the site's 32-byte settings key.
package securebox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

var ErrInvalidKey = errors.New("settings key must be 32 bytes")

func Seal(key []byte, value, purpose string) (string, error) {
	if len(key) != 32 {
		return "", ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), []byte(purpose))
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func Open(key []byte, sealed, purpose string) (string, error) {
	if len(key) != 32 {
		return "", ErrInvalidKey
	}
	blob, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(blob) < gcm.NonceSize() {
		return "", errors.New("invalid ciphertext")
	}
	plain, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], []byte(purpose))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
