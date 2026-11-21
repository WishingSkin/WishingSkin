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

var errPluginUpgradePending = errors.New("plugin upgrade pending")

const pluginPackageTimeout = 2 * time.Minute

type dynamicCheckpoint struct {
	Digest          string `json:"digest"`
	Pending         bool   `json:"pending,omitempty"`
	PreviousEnabled bool   `json:"previousEnabled,omitempty"`
}

func dynamicCheckpointKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "plugin_runtime_" + hex.EncodeToString(sum[:16])
}

func (a *Server) dynamicCheckpoint(ctx context.Context, reader settingReader, id string) (dynamicCheckpoint, error) {
	var state dynamicCheckpoint
	_, err := a.readSettingFrom(ctx, reader, dynamicCheckpointKey(id), &state)
	return state, err
}

func (a *Server) saveDynamicCheckpoint(ctx context.Context, pkg runtimeplugin.Package, enabled bool, state dynamicCheckpoint) error {
	tx, err := a.s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = a.putPluginEnabled(ctx, tx, pkg.Manifest.ID, pkg.Manifest.Version, enabled); err != nil {
		return err
	}
	if err = a.putSetting(ctx, tx, dynamicCheckpointKey(pkg.Manifest.ID), state); err != nil {
		return err
	}
	return tx.Commit()
}

func pluginActivationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errPluginUpgradePending) {
		pluginRecoveryResponse(w, "插件安装失败，请检查依赖、迁移和运行协议")
		return
	}
	failure(w, 409, "插件安装失败，请检查依赖、迁移和运行协议")
}

func pluginRecoveryResponse(w http.ResponseWriter, message string) {
	respond(w, 409, map[string]any{"error": message, "recoveryRequired": true, "retrySamePackage": true})
}

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
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(pluginPackageTimeout)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline.Add(30 * time.Second))
	r.Body = http.MaxBytesReader(w, r.Body, runtimeplugin.MaxPackage)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		failure(w, 400, "插件包过大或无法读取")
		return nil, false
	}
	return data, true
}
func (a *Server) acquirePluginUploadSlot(w http.ResponseWriter) (func(), bool) {
	select {
	case a.uploads <- struct{}{}:
		return func() { <-a.uploads }, true
	default:
		failure(w, 429, "资源处理繁忙，请稍后重试")
		return nil, false
	}
}
func (a *Server) inspectDynamicPackage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRequest(w, r); !ok {
		return
	}
	release, ok := a.acquirePluginUploadSlot(w)
	if !ok {
		return
	}
	defer release()
	data, ok := a.packageBytes(w, r)
	if !ok {
		return
	}
	pkg, err := runtimeplugin.Inspect(data)
	if err != nil || pkg.ValidateRoutePaths() != nil {
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
	release, ok := a.acquirePluginUploadSlot(w)
	if !ok {
		return
	}
	defer release()
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
		pluginActivationError(w, err)
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
	if err = pkg.ValidateRoutePaths(); err != nil {
		return err
	}
	id := pkg.Manifest.ID
	lock := a.dynamicLock(id)
	lock.Lock()
	defer lock.Unlock()
	release, err := a.s.LeasePluginTask(ctx, id, "database-migrations")
	if err != nil {
		return err
	}
	defer release()
	state, err := a.dynamicCheckpoint(ctx, a.s.DB, id)
	if err != nil {
		return err
	}
	if state.Pending && state.Digest != runtimeplugin.Digest(data) {
		return errPluginUpgradePending
	}
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
	steps, err := a.planPluginMigrations(ctx, pkg)
	if err != nil {
		return err
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
	if !state.Pending {
		enabled, err := a.storedPluginEnabled(ctx, id)
		if err != nil {
			return err
		}
		state = dynamicCheckpoint{Digest: item.Digest, Pending: true, PreviousEnabled: enabled}
		if err = a.saveDynamicCheckpoint(ctx, pkg, false, state); err != nil {
			return err
		}
	}
	a.cancelCommunityTasks(id)
	_ = a.dynamic.Reload(id)
	if err = a.applyPluginMigrations(ctx, id, steps); err != nil {
		return errors.Join(errPluginUpgradePending, err)
	}
	if err = a.dynamic.Activate(item); err != nil {
		return errors.Join(errPluginUpgradePending, err)
	}
	a.unregisterDynamicContributions(id)
	if err = a.plugins.Replace(pkg.Manifest); err != nil {
		return errors.Join(errPluginUpgradePending, err)
	}
	a.registerDynamicContributions(item)
	if err = a.saveDynamicCheckpoint(ctx, pkg, state.PreviousEnabled, dynamicCheckpoint{Digest: item.Digest}); err != nil {
		return errors.Join(errPluginUpgradePending, err)
	}
	return nil
}

type pluginMigrationStep struct {
	version, index  int
	query, checksum string
}

func (a *Server) planPluginMigrations(ctx context.Context, pkg runtimeplugin.Package) ([]pluginMigrationStep, error) {
	rows, err := a.s.DB.QueryContext(ctx, a.s.Bind("SELECT version,step,checksum FROM plugin_migrations WHERE plugin_id=?"), pkg.Manifest.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := map[[2]int]string{}
	for rows.Next() {
		var version, step int
		var checksum string
		if err = rows.Scan(&version, &step, &checksum); err != nil {
			return nil, err
		}
		applied[[2]int{version, step}] = checksum
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(pkg.Manifest.ID))
	prefix := "plugin_" + hex.EncodeToString(hash[:8]) + "_"
	var plan []pluginMigrationStep
	for _, migration := range pkg.Migrations {
		steps := migration.Postgres
		if a.s.Dialect == "mysql" {
			steps = migration.MySQL
		}
		for index, query := range steps {
			hash := sha256.Sum256([]byte(query))
			checksum := hex.EncodeToString(hash[:])
			key := [2]int{migration.Version, index}
			if old, exists := applied[key]; exists {
				if old != checksum {
					return nil, errors.New("applied migration changed")
				}
				delete(applied, key)
				continue
			}
			if !strings.Contains(query, "{{prefix}}") {
				return nil, errors.New("migration must name plugin tables with prefix")
			}
			plan = append(plan, pluginMigrationStep{version: migration.Version, index: index, query: strings.ReplaceAll(query, "{{prefix}}", prefix), checksum: checksum})
		}
	}
	if len(applied) != 0 {
		return nil, errors.New("applied migration removed")
	}
	return plan, nil
}

func (a *Server) applyPluginMigrations(ctx context.Context, id string, steps []pluginMigrationStep) error {
	if len(steps) == 0 {
		return nil
	}
	var executor interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	} = a.s.DB
	if a.s.Dialect == "postgres" {
		tx, err := a.s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		executor = tx
	}
	insert := a.s.Bind("INSERT INTO plugin_migrations(plugin_id,version,step,checksum,applied_at) VALUES(?,?,?,?,?)")
	for _, step := range steps {
		if _, err := executor.ExecContext(ctx, step.query); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, insert, id, step.version, step.index, step.checksum, time.Now().Unix()); err != nil {
			return err
		}
	}
	if tx, ok := executor.(*sql.Tx); ok {
		return tx.Commit()
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
	item, exists := a.dynamic.Get(id)
	if !exists {
		http.NotFound(w, r)
		return
	}
	release, err := a.s.LeasePluginTask(r.Context(), id, "database-migrations")
	if err != nil {
		failure(w, 503, "暂时无法卸载插件")
		return
	}
	defer release()
	state, err := a.dynamicCheckpoint(r.Context(), a.s.DB, id)
	if err != nil {
		failure(w, 503, "暂时无法读取插件状态")
		return
	}
	if state.Pending {
		pluginRecoveryResponse(w, "操作失败")
		return
	}
	if state.Digest != "" && state.Digest != item.Digest {
		respond(w, 409, map[string]any{"error": "操作失败", "staleRuntime": true})
		return
	}
	for _, s := range a.plugins.States() {
		if slices.Contains(s.Manifest.Dependencies, id) {
			failure(w, 409, "请先移除依赖此插件的插件")
			return
		}
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	if err = a.putPluginEnabled(r.Context(), tx, id, item.Package.Manifest.Version, false); err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM plugin_event_deliveries WHERE plugin_id=? AND state='pending'"), id)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE webhook_subscriptions SET enabled=FALSE WHERE plugin_id=?"), id)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM webhook_deliveries WHERE state<>'delivered' AND subscription_id IN (SELECT id FROM webhook_subscriptions WHERE plugin_id=?)"), id)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "plugin.uninstall", "plugin", id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "操作失败")
		return
	}
	a.cancelCommunityTasks(id)
	if err := a.dynamic.Uninstall(id); err != nil {
		failure(w, 503, "操作失败")
		return
	}
	a.unregisterDynamicContributions(id)
	a.plugins.Remove(id)
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
