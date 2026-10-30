package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
	"wishingskin/internal/securebox"
	localstorage "wishingskin/internal/storage"
	blob "wishingskin/pkg/storage"
)

func (a *Server) objectStorageSettings(ctx context.Context) (localstorage.S3Options, string, error) {
	var v localstorage.S3Options
	var sealed string
	_, err := a.readSetting(ctx, "object_storage_config", &sealed)
	if err != nil || sealed == "" {
		return v, sealed, err
	}
	plain, err := securebox.Open(a.cfg.SettingsKey, sealed, "wishingskin:object-storage:v1")
	if err == nil {
		err = json.Unmarshal([]byte(plain), &v)
	}
	return v, sealed, err
}
func (a *Server) adminObjectStorage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	v, _, err := a.objectStorageSettings(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取对象存储配置")
		return
	}
	configured := v.AccessKey != "" && v.SecretKey != ""
	v.AccessKey = ""
	v.SecretKey = ""
	respond(w, 200, map[string]any{"config": v, "credentialsConfigured": configured})
}
func (a *Server) updateObjectStorage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body localstorage.S3Options
	if !decode(w, r, &body) {
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	previous, _, err := a.objectStorageSettings(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取对象存储配置")
		return
	}
	if body.AccessKey == "" {
		body.AccessKey = previous.AccessKey
	}
	if body.SecretKey == "" {
		body.SecretKey = previous.SecretKey
	}
	if body.Region == "" {
		body.Region = "us-east-1"
	}
	if localstorage.ValidateS3(body) != nil {
		failure(w, 400, "请填写有效的 S3 地址、存储桶和访问凭据")
		return
	}
	data, err := json.Marshal(body)
	var sealed string
	if err == nil {
		sealed, err = securebox.Seal(a.cfg.SettingsKey, string(data), "wishingskin:object-storage:v1")
	}
	if err == nil {
		err = a.putSetting(r.Context(), tx, "object_storage_config", sealed)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "plugin.configure", "site", "wishingskin.object-storage")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存对象存储配置")
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}
func (a *Server) objectStore(ctx context.Context) (blob.BlobStore, error) {
	enabled, err := a.storedPluginEnabled(ctx, "wishingskin.object-storage")
	if err != nil || !enabled {
		return nil, err
	}
	options, sealed, err := a.objectStorageSettings(ctx)
	if err != nil {
		return nil, err
	}
	if sealed == "" {
		return nil, errors.New("object storage not configured")
	}
	a.storageMu.Lock()
	defer a.storageMu.Unlock()
	if a.storageConfigHash != tokenHash(sealed) {
		a.remoteStorage, err = localstorage.NewS3(options)
		if err != nil {
			return nil, err
		}
		a.storageConfigHash = tokenHash(sealed)
	}
	return a.remoteStorage, nil
}
func (a *Server) saveTextureFile(ctx context.Context, hash string, data []byte) error {
	if err := saveTexture(a.cfg.TextureDir, hash, data); err != nil {
		return err
	}
	provider, err := a.objectStore(ctx)
	if err != nil {
		return err
	}
	if provider == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return provider.Put(ctx, hash, "image/png", bytes.NewReader(data))
}
func (a *Server) textureFileBytes(ctx context.Context, hash string) ([]byte, error) {
	f, err := a.openTextureFile(ctx, hash)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxTextureBytes+1))
	if err == nil && len(data) > maxTextureBytes {
		err = errors.New("texture too large")
	}
	return data, err
}

type memoryTexture struct{ *bytes.Reader }

func (*memoryTexture) Close() error { return nil }

func (a *Server) openTextureFile(ctx context.Context, hash string) (io.ReadSeekCloser, error) {
	provider, err := a.objectStore(ctx)
	if err == nil && provider != nil {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		f, e := provider.Open(ctx, hash)
		if e == nil {
			data, e := io.ReadAll(io.LimitReader(f, maxTextureBytes+1))
			f.Close()
			if e == nil && len(data) <= maxTextureBytes && localstorage.MatchesContentHash(data, hash) {
				return &memoryTexture{bytes.NewReader(data)}, nil
			}
		}
	}
	local := &localstorage.Local{Root: a.cfg.TextureDir}
	return local.Open(ctx, hash)
}
