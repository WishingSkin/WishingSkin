package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wishingskin/internal/backup"
	localstorage "wishingskin/internal/storage"
)

type backupSchedule struct {
	Enabled       bool   `json:"enabled"`
	Directory     string `json:"directory"`
	IntervalHours int    `json:"intervalHours"`
}
type operationStatus struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	StartedAt   int64  `json:"startedAt"`
	FinishedAt  int64  `json:"finishedAt"`
	Total       int    `json:"total"`
	Completed   int    `json:"completed"`
	Failed      int    `json:"failed"`
	Bytes       int64  `json:"bytes"`
	Destination string `json:"destination,omitempty"`
	Message     string `json:"message,omitempty"`
}
type orphanFile struct {
	Hash  string `json:"hash"`
	Bytes int64  `json:"bytes"`
}
type operationsRuntime struct {
	mu      sync.Mutex
	ctx     context.Context
	running bool
	jobs    sync.WaitGroup
}

func (a *Server) registerOperations() {
	a.ops.ctx = context.Background()
	a.mux.HandleFunc("GET /api/admin/operations", a.adminOperations)
	a.mux.HandleFunc("PATCH /api/admin/operations", a.updateOperations)
	a.mux.HandleFunc("GET /api/admin/storage/orphans", a.orphanPreview)
	a.mux.HandleFunc("POST /api/admin/operations/{kind}", a.startOperation)
}
func (a *Server) adminOperations(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	schedule := backupSchedule{IntervalHours: 24}
	cdn := cdnSettings{MaxAge: 60}
	job := operationStatus{}
	if _, err := a.readSetting(r.Context(), "backup_schedule", &schedule); err != nil {
		failure(w, 503, "暂时无法读取备份设置")
		return
	}
	if _, err := a.readSetting(r.Context(), "cdn_config", &cdn); err != nil {
		failure(w, 503, "暂时无法读取 CDN 设置")
		return
	}
	if _, err := a.readSetting(r.Context(), "storage_operation", &job); err != nil {
		failure(w, 503, "暂时无法读取任务状态")
		return
	}
	respond(w, 200, map[string]any{"backup": schedule, "cdn": cdn, "job": job})
}
func independentBackupDirectory(textures, directory string) bool {
	if !filepath.IsAbs(directory) {
		return false
	}
	source, err := filepath.EvalSymlinks(textures)
	if errors.Is(err, os.ErrNotExist) {
		source, err = filepath.Abs(textures)
	}
	if err != nil {
		return false
	}
	target, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return false
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return false
	}
	source, _ = filepath.Abs(source)
	target, _ = filepath.Abs(target)
	source = strings.ToLower(filepath.Clean(source))
	target = strings.ToLower(filepath.Clean(target))
	sep := string(filepath.Separator)
	return source != target && !strings.HasPrefix(source, target+sep) && !strings.HasPrefix(target, source+sep)
}
func (a *Server) updateOperations(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		Backup backupSchedule `json:"backup"`
		CDN    cdnSettings    `json:"cdn"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Backup.Directory = strings.TrimSpace(body.Backup.Directory)
	body.CDN.BaseURL = strings.TrimRight(strings.TrimSpace(body.CDN.BaseURL), "/")
	if body.Backup.IntervalHours < 1 || body.Backup.IntervalHours > 168 || body.Backup.Directory != "" && !independentBackupDirectory(a.cfg.TextureDir, body.Backup.Directory) || body.Backup.Enabled && body.Backup.Directory == "" {
		failure(w, 400, "备份目录须为独立且已存在的绝对路径，间隔为 1–168 小时")
		return
	}
	if body.CDN.MaxAge < 1 || body.CDN.MaxAge > 60 || body.CDN.Enabled && (!validOIDCIssuer(body.CDN.BaseURL) || len(a.cfg.SettingsKey) != 32) {
		failure(w, 400, "CDN 地址无效，缓存时间须为 1–60 秒")
		return
	}
	if body.CDN.Enabled {
		u, _ := url.Parse(body.CDN.BaseURL)
		origin, _ := url.Parse(a.cfg.PublicURL)
		if strings.EqualFold(u.Host, origin.Host) {
			failure(w, 400, "CDN 域名不能使用本站域名")
			return
		}
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	err := a.putSetting(r.Context(), tx, "backup_schedule", body.Backup)
	if err == nil {
		err = a.putSetting(r.Context(), tx, "cdn_config", body.CDN)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.operations", "site", "operations")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存运维设置")
		return
	}
	w.WriteHeader(204)
}
func (a *Server) orphanFiles(ctx context.Context) ([]orphanFile, error) {
	root, err := os.OpenRoot(a.cfg.TextureDir)
	if errors.Is(err, os.ErrNotExist) {
		return []orphanFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	items := []orphanFile{}
	for {
		entries, e := dir.ReadDir(256)
		for _, entry := range entries {
			if !validHash.MatchString(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			info, e := entry.Info()
			if e != nil || !info.Mode().IsRegular() || info.ModTime().After(time.Now().Add(-time.Hour)) {
				continue
			}
			var count int
			if err = a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM textures WHERE hash=?"), entry.Name()).Scan(&count); err != nil {
				return nil, err
			}
			if count == 0 {
				items = append(items, orphanFile{entry.Name(), info.Size()})
				if len(items) >= 1000 {
					return items, nil
				}
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return items, nil
}
func (a *Server) orphanPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	release, err := a.s.LeaseStorage(r.Context())
	if err != nil {
		failure(w, 409, "存储正在使用，请稍后重试")
		return
	}
	defer release()
	items, err := a.orphanFiles(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法扫描存储")
		return
	}
	var bytes int64
	for _, item := range items {
		bytes += item.Bytes
	}
	respond(w, 200, map[string]any{"files": items, "bytes": bytes, "limit": 1000})
}
func (a *Server) persistOperation(ctx context.Context, job operationStatus) error {
	tx, err := a.s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = a.putSetting(ctx, tx, "storage_operation", job)
	if err == nil && job.Kind == "backup" && job.State == "running" {
		err = a.putSetting(ctx, tx, "backup_last_attempt", job.StartedAt)
	}
	if err == nil {
		err = tx.Commit()
	}
	return err
}
func (a *Server) startOperation(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	var body struct {
		Hashes       []string `json:"hashes"`
		DeleteRemote bool     `json:"deleteRemote"`
	}
	if !decodeLimit(w, r, &body, 160<<10) {
		return
	}
	if kind != "sync" && kind != "gc" && kind != "backup" {
		failure(w, 404, "任务不存在")
		return
	}
	if kind == "gc" && (len(body.Hashes) == 0 || len(body.Hashes) > 1000) {
		failure(w, 400, "请先预览并选择最多 1000 个孤立文件")
		return
	}
	for _, hash := range body.Hashes {
		if !validHash.MatchString(hash) {
			failure(w, 400, "文件标识无效")
			return
		}
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	err := a.s.RecordAdminEvent(r.Context(), tx, u.ID, "storage."+kind, "site", "operations")
	if err == nil {
		err = tx.Commit()
	}
	tx.Rollback()
	if err != nil {
		failure(w, 503, "暂时无法记录任务")
		return
	}
	a.ops.mu.Lock()
	ctx := a.ops.ctx
	a.ops.mu.Unlock()
	job, err := a.launchOperation(ctx, kind, body.Hashes, body.DeleteRemote, false)
	if err != nil {
		failure(w, 409, "任务未开始，请检查存储启用状态、备份目录或正在执行的任务")
		return
	}
	respond(w, 202, job)
}
func (a *Server) launchOperation(ctx context.Context, kind string, hashes []string, deleteRemote, scheduled bool) (operationStatus, error) {
	a.ops.mu.Lock()
	if a.ops.running {
		a.ops.mu.Unlock()
		return operationStatus{}, errors.New("operation running")
	}
	a.ops.running = true
	a.ops.mu.Unlock()
	reset := func() { a.ops.mu.Lock(); a.ops.running = false; a.ops.mu.Unlock() }
	if err := ctx.Err(); err != nil {
		reset()
		return operationStatus{}, err
	}
	release, err := a.s.LeaseStorage(ctx)
	if err != nil {
		reset()
		return operationStatus{}, err
	}
	if kind == "sync" {
		provider, e := a.objectStore(ctx)
		if e != nil || provider == nil {
			release()
			reset()
			return operationStatus{}, errors.New("storage disabled")
		}
	}
	var schedule backupSchedule
	if kind == "backup" {
		_, err = a.readSetting(ctx, "backup_schedule", &schedule)
		if err != nil || !independentBackupDirectory(a.cfg.TextureDir, schedule.Directory) {
			release()
			reset()
			return operationStatus{}, errors.New("backup directory invalid")
		}
	}
	if scheduled {
		var last int64
		_, err = a.readSetting(ctx, "backup_last_attempt", &last)
		if err != nil || !schedule.Enabled || time.Now().Unix()-last < int64(schedule.IntervalHours)*3600 {
			release()
			reset()
			return operationStatus{}, errors.New("backup not due")
		}
	}
	id, err := randomUUID()
	job := operationStatus{ID: id, Kind: kind, State: "running", StartedAt: time.Now().Unix()}
	if err == nil {
		err = a.persistOperation(ctx, job)
	}
	if err != nil {
		release()
		reset()
		return job, err
	}
	a.ops.jobs.Go(func() {
		defer release()
		defer reset()
		jobCtx, cancel := context.WithTimeout(ctx, 6*time.Hour)
		defer cancel()
		var err error
		switch kind {
		case "sync":
			err = a.syncStoredTextures(jobCtx, &job)
		case "gc":
			err = a.collectOrphans(jobCtx, &job, hashes, deleteRemote)
		case "backup":
			job.Destination = filepath.Join(schedule.Directory, "wishingskin-backup-"+time.Now().UTC().Format("20060102T150405Z")+"-"+id[:8])
			var report backup.Report
			report, err = backup.ExportWithStorageLease(jobCtx, a.s, a.cfg.TextureDir, job.Destination)
			job.Total = report.Files
			job.Completed = report.Files
		}
		job.State = "completed"
		if job.Failed > 0 {
			job.State = "completed_with_errors"
		}
		if err != nil {
			job.State = "failed"
			job.Message = "任务未完成。请检查存储、目录权限和连接配置后重试。"
		}
		job.FinishedAt = time.Now().Unix()
		saveCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = a.persistOperation(saveCtx, job)
	})
	return job, nil
}
func (a *Server) syncStoredTextures(ctx context.Context, job *operationStatus) error {
	provider, err := a.objectStore(ctx)
	if err != nil || provider == nil {
		return errors.New("storage disabled")
	}
	_, sealed, err := a.objectStorageSettings(ctx)
	if err != nil {
		return err
	}
	if err = a.s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM textures").Scan(&job.Total); err != nil {
		return err
	}
	last := ""
	for {
		rows, err := a.s.DB.QueryContext(ctx, a.s.Bind("SELECT hash FROM textures WHERE hash>? ORDER BY hash LIMIT 100"), last)
		if err != nil {
			return err
		}
		hashes := []string{}
		for rows.Next() {
			var hash string
			if err = rows.Scan(&hash); err != nil {
				break
			}
			hashes = append(hashes, hash)
		}
		scanErr := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if scanErr != nil {
			return scanErr
		}
		if len(hashes) == 0 {
			break
		}
		_, current, e := a.objectStorageSettings(ctx)
		enabled, enableErr := a.storedPluginEnabled(ctx, "wishingskin.object-storage")
		if e != nil || enableErr != nil || !enabled || current != sealed {
			return errors.New("storage configuration changed")
		}
		for _, hash := range hashes {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			last = hash
			local := &localstorage.Local{Root: a.cfg.TextureDir}
			f, e := local.Open(ctx, hash)
			var data []byte
			if e == nil {
				data, e = io.ReadAll(io.LimitReader(f, maxTextureBytes+1))
				f.Close()
			}
			if e != nil || len(data) > maxTextureBytes || !localstorage.MatchesContentHash(data, hash) {
				job.Failed++
				continue
			}
			fileCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			remote, e := provider.Open(fileCtx, hash)
			matched := false
			if e == nil {
				remoteData, readErr := io.ReadAll(io.LimitReader(remote, maxTextureBytes+1))
				remote.Close()
				matched = readErr == nil && bytes.Equal(data, remoteData)
			}
			if !matched {
				e = provider.Put(fileCtx, hash, "image/png", bytes.NewReader(data))
				if e == nil {
					remote, e = provider.Open(fileCtx, hash)
					if e == nil {
						check, readErr := io.ReadAll(io.LimitReader(remote, maxTextureBytes+1))
						remote.Close()
						if readErr != nil || !bytes.Equal(data, check) {
							e = errors.New("remote integrity mismatch")
						}
					}
				}
			}
			cancel()
			if matched || e == nil {
				job.Completed++
				job.Bytes += int64(len(data))
			} else {
				job.Failed++
			}
		}
		if err = a.persistOperation(ctx, *job); err != nil {
			return err
		}
	}
	return nil
}
func (a *Server) collectOrphans(ctx context.Context, job *operationStatus, hashes []string, deleteRemote bool) error {
	job.Total = len(hashes)
	root, err := os.OpenRoot(a.cfg.TextureDir)
	if err != nil {
		return err
	}
	defer root.Close()
	provider, err := a.objectStore(ctx)
	if err != nil && deleteRemote {
		return err
	}
	seen := map[string]bool{}
	for _, hash := range hashes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if seen[hash] {
			job.Failed++
			continue
		}
		seen[hash] = true
		var count int
		if err = a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM textures WHERE hash=?"), hash).Scan(&count); err != nil {
			return err
		}
		info, err := root.Lstat(hash)
		if count > 0 || err != nil || !info.Mode().IsRegular() || info.ModTime().After(time.Now().Add(-time.Hour)) {
			job.Failed++
			continue
		}
		if deleteRemote && provider != nil {
			fileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err = provider.Delete(fileCtx, hash)
			cancel()
			if err != nil {
				job.Failed++
				continue
			}
		}
		if err = root.Remove(hash); err != nil {
			job.Failed++
		} else {
			job.Completed++
			job.Bytes += info.Size()
		}
	}
	return nil
}

// RunBackground starts the backup scheduler and registered community tasks.
func (a *Server) RunBackground(ctx context.Context) {
	a.communityRuntime.mu.Lock()
	a.communityRuntime.ctx = ctx
	a.communityRuntime.mu.Unlock()
	defer a.stopCommunityTasks()
	a.ops.mu.Lock()
	a.ops.ctx = ctx
	a.ops.mu.Unlock()
	a.recoverInterruptedOperation(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.recoverInterruptedOperation(ctx)
			a.runScheduledBackup(ctx, now)
			a.runCommunityTasks(ctx, now)
		}
	}
}

// Only the holder of the shared storage lease may mark a persisted job stale.
// A worker in another process keeps the lease until its final status is saved.
func (a *Server) recoverInterruptedOperation(ctx context.Context) {
	a.ops.mu.Lock()
	running := a.ops.running
	a.ops.mu.Unlock()
	if running {
		return
	}
	var job operationStatus
	if _, err := a.readSetting(ctx, "storage_operation", &job); err != nil || job.State != "running" {
		return
	}
	release, err := a.s.LeaseStorage(ctx)
	if err != nil {
		return
	}
	defer release()
	if _, err = a.readSetting(ctx, "storage_operation", &job); err != nil || job.State != "running" {
		return
	}
	job.State = "failed"
	job.FinishedAt = time.Now().Unix()
	job.Message = "任务因服务中断而停止，请重新执行。"
	_ = a.persistOperation(ctx, job)
}
func (a *Server) runScheduledBackup(ctx context.Context, now time.Time) {
	var schedule backupSchedule
	_, err := a.readSetting(ctx, "backup_schedule", &schedule)
	if err != nil || !schedule.Enabled {
		return
	}
	var last int64
	_, err = a.readSetting(ctx, "backup_last_attempt", &last)
	if err != nil || now.Unix()-last < int64(schedule.IntervalHours)*3600 {
		return
	}
	a.ops.mu.Lock()
	running := a.ops.running
	a.ops.mu.Unlock()
	if running {
		return
	}
	// launchOperation holds the cross-process lease; a second process rechecks
	// the timestamp while holding the same lease before starting another backup.
	_, err = a.launchOperation(ctx, "backup", nil, false, true)
	if err != nil {
		return
	}
}

func (a *Server) WaitBackground() { a.ops.jobs.Wait(); a.communityRuntime.jobs.Wait() }
