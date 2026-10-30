package storage

import (
	"context"
	"io"
	"os"
)

type Local struct{ Root string }

func (l *Local) Open(_ context.Context, key string) (io.ReadSeekCloser, error) {
	root, err := os.OpenRoot(l.Root)
	if err != nil {
		return nil, err
	}
	f, err := root.Open(key)
	if err != nil {
		root.Close()
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		root.Close()
		return nil, os.ErrNotExist
	}
	return &rootFile{File: f, root: root}, nil
}

type rootFile struct {
	*os.File
	root *os.Root
}

func (f *rootFile) Close() error {
	err := f.File.Close()
	rootErr := f.root.Close()
	if err != nil {
		return err
	}
	return rootErr
}
