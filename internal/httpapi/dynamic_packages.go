package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"wishingskin/internal/runtimeplugin"
)

func (a *Server) initDynamicPlugins() error {
	root := a.cfg.PluginDir
	if root == "" {
		root = filepath.Join(a.cfg.TextureDir, ".plugins")
	}
	m, err := runtimeplugin.Open(root)
	if err != nil {
		return err
	}
	a.dynamic = m
	for _, item := range m.Items() {
		if _, exists := a.plugins.State(item.Package.Manifest.ID); exists {
			return errors.New("duplicate runtime plugin")
		}
		if err = a.plugins.Replace(item.Package.Manifest); err != nil {
			return err
		}
	}
	for _, item := range m.Items() {
		a.registerDynamicContributions(item)
	}
	a.mux.HandleFunc("/api/plugins/{id}/{path...}", a.dynamicRoute)
	a.mux.HandleFunc("GET /api/admin/plugin-packages", a.dynamicPackages)
	a.mux.HandleFunc("POST /api/admin/plugin-packages/inspect", a.inspectDynamicPackage)
	a.mux.HandleFunc("POST /api/admin/plugin-packages/install", a.installDynamicPackage)
	a.mux.HandleFunc("POST /api/admin/plugin-packages/{id}/reload", a.reloadDynamicPackage)
	a.mux.HandleFunc("DELETE /api/admin/plugin-packages/{id}", a.removeDynamicPackage)
	a.mux.HandleFunc("GET /api/admin/plugin-packages/{id}/frontend", a.dynamicFrontend)
	a.registerPluginMarket()
	a.registerWebhooks()
	return nil
}
func (a *Server) dynamicPackages(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	respond(w, 200, map[string]any{"packages": a.dynamic.Items()})
}
func (a *Server) packageBytes(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, runtimeplugin.MaxPackage)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		failure(w, 400, "插件包过大或无法读取")
		return nil, false
	}
	return data, true
}
func (a *Server) inspectDynamicPackage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRequest(w, r); !ok {
		return
	}
	data, ok := a.packageBytes(w, r)
	if !ok {
		return
	}
	pkg, err := runtimeplugin.Inspect(data)
	if err != nil {
		failure(w, 400, "插件包格式无效或不支持当前平台")
		return
	}
	respond(w, 200, map[string]any{"package": pkg, "digest": runtimeplugin.Digest(data)})
}
func (a *Server) installDynamicPackage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	data, ok := a.packageBytes(w, r)
	if !ok {
		return
	}
	var grants []string
	if json.Unmarshal([]byte(r.Header.Get("X-Plugin-Permissions")), &grants) != nil || r.Header.Get("X-Plugin-Digest") != runtimeplugin.Digest(data) {
		failure(w, 400, "请先检查插件包并确认权限")
		return
	}
	if err := a.activatePackage(r.Context(), data, grants); err != nil {
		failure(w, 409, "插件安装失败，请检查依赖、迁移和运行协议")
		return
	}
	pkg, _ := runtimeplugin.Inspect(data)
	_ = a.recordDynamicEvent(r.Context(), u.ID, "plugin.install", pkg.Manifest.ID)
	respond(w, 200, map[string]bool{"installed": true})
}
func (a *Server) activatePackage(ctx context.Context, data []byte, grants []string) error {
	a.dynamicMu.Lock()
	defer a.dynamicMu.Unlock()
	pkg, err := runtimeplugin.Inspect(data)
	if err != nil {
		return err
	}
	id := pkg.Manifest.ID
	lock := a.dynamicLock(id)
	lock.Lock()
	defer lock.Unlock()
	if _, exists := a.plugins.State(id); exists {
		if _, dynamic := a.dynamic.Get(id); !dynamic {
			return errors.New("source plugin id collision")
		}
	}
	graph := map[string][]string{}
	for _, state := range a.plugins.States() {
		graph[state.Manifest.ID] = state.Manifest.Dependencies
	}
	graph[id] = pkg.Manifest.Dependencies
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(node string) bool {
		if visiting[node] {
			return false
		}
		if done[node] {
			return true
		}
		visiting[node] = true
		for _, dep := range graph[node] {
			if !visit(dep) {
				return false
			}
		}
		visiting[node] = false
		done[node] = true
		return true
	}
	if !visit(id) {
		return errors.New("cyclic dependency")
	}
	for _, dep := range pkg.Manifest.Dependencies {
		if dep == id {
			return errors.New("cyclic dependency")
		}
		if on, err := a.communityEnabled(ctx, dep); err != nil || !on {
			return errors.New("missing dependency")
		}
	}
	item, err := a.dynamic.Stage(data, grants)
	if err != nil {
		return err
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = a.dynamic.Probe(probe, item)
	cancel()
	if err != nil {
		return err
	}
	enabled, _ := a.storedPluginEnabled(ctx, id)
	tx, err := a.s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	err = a.putPluginEnabled(ctx, tx, id, pkg.Manifest.Version, false)
	if err == nil {
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	if err != nil {
		return err
	}
	a.cancelCommunityTasks(id)
	_ = a.dynamic.Reload(id)
	if err = a.applyPluginMigrations(ctx, item); err != nil {
		return err
	}
	if err = a.dynamic.Activate(item); err != nil {
		return err
	}
	a.unregisterDynamicContributions(id)
	if err = a.plugins.Replace(pkg.Manifest); err != nil {
		return err
	}
	a.registerDynamicContributions(item)
	tx, err = a.s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = a.putPluginEnabled(ctx, tx, id, pkg.Manifest.Version, enabled); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *Server) applyPluginMigrations(ctx context.Context, item runtimeplugin.Installed) error {
	id := item.Package.Manifest.ID
	release, err := a.s.LeasePluginTask(ctx, id, "database-migrations")
	if err != nil {
		return err
	}
	defer release()
	h := sha256.Sum256([]byte(id))
	prefix := "plugin_" + hex.EncodeToString(h[:8]) + "_"
	rows, err := a.s.DB.QueryContext(ctx, a.s.Bind("SELECT version,step,checksum FROM plugin_migrations WHERE plugin_id=?"), id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var version, step int
		var checksum string
		if err = rows.Scan(&version, &step, &checksum); err != nil {
			break
		}
		if version < 1 || version > len(item.Package.Migrations) {
			err = errors.New("migration downgrade is not supported")
			break
		}
		migration := item.Package.Migrations[version-1]
		queries := migration.Postgres
		if a.s.Dialect == "mysql" {
			queries = migration.MySQL
		}
		if step < 0 || step >= len(queries) {
			err = errors.New("applied migration removed")
			break
		}
		hash := sha256.Sum256([]byte(queries[step]))
		if checksum != hex.EncodeToString(hash[:]) {
			err = errors.New("applied migration changed")
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, migration := range item.Package.Migrations {
		steps := migration.Postgres
		if a.s.Dialect == "mysql" {
			steps = migration.MySQL
		}
		for index, query := range steps {
			hash := sha256.Sum256([]byte(query))
			checksum := hex.EncodeToString(hash[:])
			var old string
			err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT checksum FROM plugin_migrations WHERE plugin_id=? AND version=? AND step=?"), id, migration.Version, index).Scan(&old)
			if err == nil {
				if old != checksum {
					return errors.New("changed applied migration")
				}
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if !strings.Contains(query, "{{prefix}}") {
				return errors.New("migration must name plugin tables with prefix")
			}
			if _, err = a.s.DB.ExecContext(ctx, strings.ReplaceAll(query, "{{prefix}}", prefix)); err != nil {
				return err
			}
			if _, err = a.s.DB.ExecContext(ctx, a.s.Bind("INSERT INTO plugin_migrations(plugin_id,version,step,checksum,applied_at) VALUES(?,?,?,?,?)"), id, migration.Version, index, checksum, time.Now().Unix()); err != nil {
				return err
			}
		}
	}
	return nil
}
func (a *Server) reloadDynamicPackage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	a.dynamicMu.Lock()
	defer a.dynamicMu.Unlock()
	id := r.PathValue("id")
	lock := a.dynamicLock(id)
	lock.Lock()
	defer lock.Unlock()
	a.cancelCommunityTasks(id)
	if err := a.dynamic.Reload(id); err != nil {
		http.NotFound(w, r)
		return
	}
	_ = a.recordDynamicEvent(r.Context(), u.ID, "plugin.reload", id)
	respond(w, 200, map[string]bool{"reloaded": true})
}
func (a *Server) removeDynamicPackage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	a.dynamicMu.Lock()
	defer a.dynamicMu.Unlock()
	id := r.PathValue("id")
	lock := a.dynamicLock(id)
	lock.Lock()
	defer lock.Unlock()
	for _, s := range a.plugins.States() {
		if slices.Contains(s.Manifest.Dependencies, id) {
			failure(w, 409, "请先移除依赖此插件的插件")
			return
		}
	}
	a.cancelCommunityTasks(id)
	if err := a.dynamic.Uninstall(id); err != nil {
		http.NotFound(w, r)
		return
	}
	a.unregisterDynamicContributions(id)
	a.plugins.Remove(id)
	_, _ = a.s.DB.ExecContext(r.Context(), a.s.Bind("UPDATE plugins SET enabled=? WHERE plugin_id=?"), false, id)
	_ = a.recordDynamicEvent(r.Context(), u.ID, "plugin.uninstall", id)
	respond(w, 200, map[string]bool{"removed": true})
}

func (a *Server) recordDynamicEvent(ctx context.Context, actor int64, action, id string) error {
	tx, err := a.s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = a.s.RecordAdminEvent(ctx, tx, actor, action, "plugin", id); err != nil {
		return err
	}
	return tx.Commit()
}
