package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	blob "wishingskin/pkg/storage"
)

func (a *Server) remoteOrphanPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	_, revision, err := a.objectStorageSettings(ctx)
	if err != nil {
		failure(w, 503, "暂时无法读取对象存储配置")
		return
	}
	provider, err := a.objectStore(ctx)
	inventory, ok := provider.(blob.InventoryStore)
	if err != nil || !ok {
		failure(w, 409, "当前对象存储不支持文件列举")
		return
	}
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 2048 {
		failure(w, 400, "分页位置无效")
		return
	}
	page, err := inventory.List(ctx, cursor, 1000)
	if err != nil {
		failure(w, 503, "暂时无法列举远端存储")
		return
	}
	files := []orphanFile{}
	candidates := []blob.ObjectInfo{}
	args := []any{}
	for _, object := range page.Objects {
		if validHash.MatchString(object.Key) && !object.ModifiedAt.IsZero() && !object.ModifiedAt.After(time.Now().Add(-time.Hour)) {
			candidates = append(candidates, object)
			args = append(args, object.Key)
		}
	}
	referenced := map[string]bool{}
	if len(args) > 0 {
		rows, e := a.s.DB.QueryContext(ctx, a.s.Bind("SELECT hash FROM textures WHERE hash IN ("+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+")"), args...)
		if e != nil {
			failure(w, 503, "暂时无法检查材质引用")
			return
		}
		for rows.Next() {
			var hash string
			if e = rows.Scan(&hash); e != nil {
				break
			}
			referenced[hash] = true
		}
		rowErr := rows.Err()
		rows.Close()
		if e != nil || rowErr != nil {
			failure(w, 503, "暂时无法检查材质引用")
			return
		}
	}
	for _, object := range candidates {
		if !referenced[object.Key] {
			files = append(files, orphanFile{object.Key, object.Bytes})
		}
	}
	_, current, err := a.objectStorageSettings(ctx)
	if err != nil || current != revision {
		failure(w, 409, "存储配置已更新，请重新预览")
		return
	}
	respond(w, 200, map[string]any{"files": files, "next": page.Next, "scanned": len(page.Objects), "limit": 1000, "revision": tokenHash(revision)})
}

// Caller holds the storage lease. Every item is checked again after preview.
func (a *Server) collectRemoteOrphans(ctx context.Context, job *operationStatus, hashes []string, expected string) error {
	_, revision, err := a.objectStorageSettings(ctx)
	if err != nil || expected == "" || tokenHash(revision) != expected {
		return errors.New("storage configuration changed")
	}
	provider, err := a.objectStore(ctx)
	inventory, ok := provider.(blob.InventoryStore)
	if err != nil || !ok {
		return errors.New("remote inventory unavailable")
	}
	job.Total = len(hashes)
	seen := map[string]bool{}
	for _, hash := range hashes {
		_, current, e := a.objectStorageSettings(ctx)
		enabled, ee := a.storedPluginEnabled(ctx, "wishingskin.object-storage")
		if e != nil || ee != nil || !enabled || current != revision {
			return errors.New("storage configuration changed")
		}
		if !validHash.MatchString(hash) || seen[hash] {
			job.Failed++
			continue
		}
		seen[hash] = true
		var count int
		if err = a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM textures WHERE hash=?"), hash).Scan(&count); err != nil {
			return err
		}
		object, e := inventory.Stat(ctx, hash)
		if count != 0 || e != nil || object.ModifiedAt.IsZero() || object.ModifiedAt.After(time.Now().Add(-time.Hour)) {
			job.Failed++
			continue
		}
		if err = inventory.Delete(ctx, hash); err != nil {
			job.Failed++
		} else {
			job.Completed++
			job.Bytes += object.Bytes
		}
	}
	return nil
}
