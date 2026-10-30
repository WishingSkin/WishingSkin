package storage

import (
	"bytes"
	"context"
	"errors"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type S3Options struct {
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	Region    string `json:"region"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}
type S3 struct {
	client         *minio.Client
	bucket, prefix string
}

var bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
var prefixPattern = regexp.MustCompile(`^[a-zA-Z0-9/_-]*$`)

func ValidateS3(v S3Options) error {
	u, err := url.Parse(v.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || (u.Scheme != "https" && u.Scheme != "http") || !bucketPattern.MatchString(v.Bucket) || len(v.Region) > 64 || len(v.Prefix) > 128 || !prefixPattern.MatchString(v.Prefix) || v.AccessKey == "" || v.SecretKey == "" || len(v.AccessKey) > 256 || len(v.SecretKey) > 2048 {
		return errors.New("invalid S3 configuration")
	}
	return nil
}
func NewS3(v S3Options) (*S3, error) {
	if err := ValidateS3(v); err != nil {
		return nil, err
	}
	u, _ := url.Parse(v.Endpoint)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 10 * time.Second
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(v.AccessKey, v.SecretKey, ""), Secure: u.Scheme == "https", Region: v.Region, BucketLookup: minio.BucketLookupPath, Transport: transport, MaxRetries: 2})
	if err != nil {
		return nil, err
	}
	prefix := strings.Trim(v.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return &S3{client: client, bucket: v.Bucket, prefix: prefix}, nil
}
func (s *S3) Put(ctx context.Context, key, contentType string, r io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(r, 8<<20+1))
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return errors.New("object exceeds 8 MiB")
	}
	_, err = s.client.PutObject(ctx, s.bucket, s.prefix+key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType, DisableMultipart: true})
	return err
}
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(ctx, s.bucket, s.prefix+key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	info, err := object.Stat()
	if err != nil {
		object.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	if info.Size > 8<<20 {
		object.Close()
		return nil, errors.New("object exceeds 8 MiB")
	}
	return object, nil
}
func (s *S3) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, s.prefix+key, minio.RemoveObjectOptions{})
}
