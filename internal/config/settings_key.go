package config

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// SettingsKey is kept outside database backups. Never replace a key on read errors.
func SettingsKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 32 {
			return nil, errors.New("invalid settings key file")
		}
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("cannot read settings key file")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return SettingsKey(path)
	}
	if err != nil {
		return nil, err
	}
	_, writeErr := file.Write(key)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return nil, io.ErrShortWrite
	}
	return key, nil
}
