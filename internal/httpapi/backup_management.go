package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"wishingskin/internal/backup"
)

var managedBackupName = regexp.MustCompile(`^wishingskin-backup-[0-9]{8}T[0-9]{6}Z-[a-f0-9]{8}$`)

type backupEntry struct {
	ID              string `json:"id"`
	CreatedAt       string `json:"createdAt"`
	Schema          int    `json:"schema"`
	Files           int    `json:"files"`
	UnverifiedFiles int    `json:"unverifiedFiles"`
	State           string `json:"state"`
}

func (a *Server) registerBackupManagement() {
	a.mux.HandleFunc("GET /api/admin/backups", a.listBackups)
	a.mux.HandleFunc("POST /api/admin/backups/{id}/verify", a.verifyBackup)
	a.mux.HandleFunc("POST /api/admin/backups/{id}/restore-plan", a.backupRestorePlan)
}
func (a *Server) backupDirectory(ctx context.Context) (string, error) {
	var schedule backupSchedule
	_, err := a.readSetting(ctx, "backup_schedule", &schedule)
	if err != nil || !independentBackupDirectory(a.cfg.TextureDir, schedule.Directory) {
		return "", errors.New("backup directory unavailable")
	}
	return schedule.Directory, nil
}
func readBackupEntries(directory string) ([]backupEntry, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	items := []backupEntry{}
	for _, entry := range entries {
		if !managedBackupName.MatchString(entry.Name()) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		item := backupEntry{ID: entry.Name(), State: "incomplete"}
		info, e := root.Lstat(entry.Name() + "/manifest.json")
		if e == nil && info.Mode().IsRegular() && info.Size() <= 32<<20 {
			f, e := root.Open(entry.Name() + "/manifest.json")
			if e == nil {
				var m backup.Manifest
				e = json.NewDecoder(io.LimitReader(f, 32<<20)).Decode(&m)
				f.Close()
				if e == nil {
					item.CreatedAt = m.CreatedAt
					item.Schema = m.SchemaVersion
					item.Files = len(m.Files)
					item.UnverifiedFiles = m.UnverifiedFiles
					item.State = "unchecked"
				}
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}
func (a *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	directory, err := a.backupDirectory(r.Context())
	if err != nil {
		respond(w, 200, map[string]any{"backups": []backupEntry{}})
		return
	}
	items, err := readBackupEntries(directory)
	if err != nil {
		failure(w, 503, "暂时无法读取备份列表")
		return
	}
	total := len(items)
	if len(items) > 500 {
		items = items[:500]
	}
	respond(w, 200, map[string]any{"backups": items, "total": total})
}
func (a *Server) selectedBackup(ctx context.Context, id string) (string, error) {
	if !managedBackupName.MatchString(id) {
		return "", errors.New("invalid backup id")
	}
	directory, err := a.backupDirectory(ctx)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Lstat(id)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("invalid backup directory")
	}
	return filepath.Join(directory, id), nil
}
func (a *Server) verifyBackup(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRequest(w, r); !ok {
		return
	}
	path, err := a.selectedBackup(r.Context(), r.PathValue("id"))
	if err != nil {
		failure(w, 404, "备份不存在")
		return
	}
	m, err := backup.Verify(r.Context(), path)
	if err != nil {
		failure(w, 409, "备份校验未通过，请检查完整性和程序版本")
		return
	}
	respond(w, 200, map[string]any{"valid": true, "schema": m.SchemaVersion, "files": len(m.Files), "unverifiedFiles": m.UnverifiedFiles, "plugins": len(m.PluginBundles), "createdAt": m.CreatedAt})
}
func (a *Server) backupRestorePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRequest(w, r); !ok {
		return
	}
	path, err := a.selectedBackup(r.Context(), r.PathValue("id"))
	var manifest backup.Manifest
	if err == nil {
		manifest, err = backup.Verify(r.Context(), path)
	}
	if err != nil {
		failure(w, 409, "请先选择完整且版本匹配的备份")
		return
	}
	respond(w, 200, map[string]any{"directory": path, "arguments": []string{"restore", "--offline", "--directory", path}, "requiresOffline": true, "unverifiedFiles": manifest.UnverifiedFiles})
}
func (a *Server) exportBackup(ctx context.Context, destination string) (backup.Report, error) {
	a.dynamicMu.Lock()
	defer a.dynamicMu.Unlock()
	return backup.ExportWithStorageLease(ctx, a.s, a.cfg.TextureDir, destination, a.cfg.PluginDir)
}

// Caller holds the storage lease; only validated application-generated siblings
// with verified source files are eligible. Verify the new backup before
// removing anything and keep it even if timestamps tie at second precision.
func (a *Server) pruneBackups(ctx context.Context, schedule backupSchedule, newest string) error {
	if schedule.Keep <= 0 {
		return nil
	}
	manifest, err := backup.Verify(ctx, newest)
	if err != nil {
		return err
	}
	if manifest.UnverifiedFiles > 0 {
		return nil
	}
	items, err := readBackupEntries(schedule.Directory)
	if err != nil {
		return err
	}
	valid := 1
	root, err := os.OpenRoot(schedule.Directory)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, item := range items {
		if err = ctx.Err(); err != nil {
			return err
		}
		if item.ID == filepath.Base(newest) || item.State != "unchecked" || item.UnverifiedFiles > 0 {
			continue
		}
		path := filepath.Join(schedule.Directory, item.ID)
		manifest, err := backup.Verify(ctx, path)
		if err != nil || manifest.UnverifiedFiles > 0 {
			continue
		}
		valid++
		if valid <= schedule.Keep {
			continue
		}
		info, e := root.Lstat(item.ID)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup directory changed")
		}
		if err = root.RemoveAll(item.ID); err != nil {
			return err
		}
	}
	return nil
}
