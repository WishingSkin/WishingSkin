// Package storage defines the object storage boundary used by texture files.
package storage

import (
	"context"
	"io"
	"time"
)

type BlobStore interface {
	Put(context.Context, string, string, io.Reader) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

// InventoryStore adds paginated maintenance without changing basic providers.
type ObjectInfo struct {
	Key        string    `json:"key"`
	Bytes      int64     `json:"bytes"`
	ModifiedAt time.Time `json:"modifiedAt"`
}
type ObjectPage struct {
	Objects []ObjectInfo `json:"objects"`
	Next    string       `json:"next"`
}
type InventoryStore interface {
	BlobStore
	List(context.Context, string, int) (ObjectPage, error)
	Stat(context.Context, string) (ObjectInfo, error)
}
