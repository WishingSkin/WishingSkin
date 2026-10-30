// Package storage defines the object storage boundary used by texture files.
package storage

import (
	"context"
	"io"
)

type BlobStore interface {
	Put(context.Context, string, string, io.Reader) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}
