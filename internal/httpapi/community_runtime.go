package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

type registeredTask struct {
	task     sdk.Task
	interval time.Duration
}
type communityRuntime struct {
	mu      sync.Mutex
	pages   map[string]sdk.Page
	tasks   map[string]registeredTask
	running map[string]context.CancelFunc
	ctx     context.Context
	closed  bool
	jobs    sync.WaitGroup
}
type pluginTaskStatus struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	Message    string `json:"message,omitempty"`
}

var errPluginTaskBusy = errors.New("plugin task is running")
var errPluginUnavailable = errors.New("plugin unavailable")

func runtimeKey(id, contribution string) string { return id + "/" + contribution }
func taskSettingKey(id, task string) string {
	hash := sha256.Sum256([]byte(runtimeKey(id, task)))
	return "plugin_task_" + hex.EncodeToString(hash[:24])
}
func (h *communityHost) Page(page sdk.Page) error {
	declared := false
	for _, p := range h.manifest.Pages {
		if p.ID == page.ID {
			declared = true
		}
	}
	key := runtimeKey(h.manifest.ID, page.ID)
	if !declared || page.Handler == nil {
		return fmt.Errorf("plugin page not declared")
	}
	h.a.communityRuntime.mu.Lock()
	defer h.a.communityRuntime.mu.Unlock()
	if _, exists := h.a.communityRuntime.pages[key]; exists {
		return fmt.Errorf("duplicate plugin page")
	}
	h.a.communityRuntime.pages[key] = page
	return nil
}
func (h *communityHost) Task(task sdk.Task) error {
	var declaration *sdk.TaskDeclaration
	for _, d := range h.manifest.Tasks {
		if d.ID == task.ID {
			v := d
			declaration = &v
		}
	}
	if declaration == nil || task.Handler == nil {
		return fmt.Errorf("plugin task not declared")
	}
	if task.Timeout == 0 {
		task.Timeout = time.Minute
	}
	if task.Timeout < time.Second || task.Timeout > 15*time.Minute {
		return fmt.Errorf("invalid plugin task timeout")
	}
	interval, err := sdk.TaskInterval(declaration.Schedule)
	if err != nil {
		return err
	}
	key := runtimeKey(h.manifest.ID, task.ID)
	h.a.communityRuntime.mu.Lock()
	defer h.a.communityRuntime.mu.Unlock()
	if _, exists := h.a.communityRuntime.tasks[key]; exists {
		return fmt.Errorf("duplicate plugin task")
	}
	h.a.communityRuntime.tasks[key] = registeredTask{task: task, interval: interval}
	return nil
}
func (a *Server) availableCommunity(ctx context.Context, id string) bool {
	state, ok := a.plugins.State(id)
	if !ok || pluginOrigin(id) != "community" || state.Error != "" {
		return false
	}
	on, err := a.communityEnabled(ctx, id)
	return err == nil && on
}
func validPageDocument(doc sdk.PageDocument) bool {
	validText := func(s string, max int) bool { return utf8.ValidString(s) && len(s) <= max }
	if !validText(doc.Title, 256) || doc.Title == "" || !validText(doc.Summary, 4096) || len(doc.Sections) > 32 {
		return false
	}
	for _, section := range doc.Sections {
		if !validText(section.Title, 256) || !validText(section.Body, 8192) || len(section.Items) > 64 {
			return false
		}
		for _, item := range section.Items {
			if !validText(item.Label, 256) || !validText(item.Value, 4096) {
				return false
			}
		}
	}
	encoded, err := json.Marshal(doc)
	return err == nil && len(encoded) <= 65536
}
func (a *Server) adminPluginPage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRead(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	a.communityRuntime.mu.Lock()
	page, ok := a.communityRuntime.pages[runtimeKey(id, r.PathValue("page"))]
	a.communityRuntime.mu.Unlock()
	if !ok || !a.availableCommunity(r.Context(), id) {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	doc, err := page.Handler(ctx, sdk.Principal{ID: u.ID, Admin: true})
	if err != nil || ctx.Err() != nil || !validPageDocument(doc) {
		failure(w, 503, "暂时无法读取插件页面")
		return
	}
	if !a.availableCommunity(r.Context(), id) {
		http.NotFound(w, r)
		return
	}
	respond(w, 200, doc)
}
func (a *Server) readPluginTask(ctx context.Context, reader settingReader, id, task string) (pluginTaskStatus, error) {
	result := pluginTaskStatus{ID: task, State: "idle"}
	var raw string
	err := reader.QueryRowContext(ctx, a.s.Bind("SELECT value FROM settings WHERE setting_key=?"), taskSettingKey(id, task)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &result)
	}
	result.ID = task
	return result, err
}
func (a *Server) adminPluginRuntime(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	id := r.PathValue("id")
	if !a.availableCommunity(r.Context(), id) {
		http.NotFound(w, r)
		return
	}
	state, _ := a.plugins.State(id)
	a.communityRuntime.mu.Lock()
	defer a.communityRuntime.mu.Unlock()
	pages := []sdk.PageContribution{}
	tasks := []map[string]any{}
	for _, p := range state.Manifest.Pages {
		if _, ok := a.communityRuntime.pages[runtimeKey(id, p.ID)]; ok {
			pages = append(pages, p)
		}
	}
	for _, d := range state.Manifest.Tasks {
		if task, ok := a.communityRuntime.tasks[runtimeKey(id, d.ID)]; ok {
			status, err := a.readPluginTask(r.Context(), a.s.DB, id, d.ID)
			if err != nil {
				failure(w, 503, "暂时无法读取插件任务")
				return
			}
			tasks = append(tasks, map[string]any{"id": d.ID, "schedule": d.Schedule, "timeoutSeconds": int(task.task.Timeout.Seconds()), "status": status})
		}
	}
	respond(w, 200, map[string]any{"pages": pages, "tasks": tasks})
}
func (a *Server) adminStartPluginTask(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	id, task := r.PathValue("id"), r.PathValue("task")
	a.communityRuntime.mu.Lock()
	_, exists := a.communityRuntime.tasks[runtimeKey(id, task)]
	a.communityRuntime.mu.Unlock()
	if !exists || !a.availableCommunity(r.Context(), id) {
		http.NotFound(w, r)
		return
	}
	status, err := a.launchCommunityTask(r.Context(), id, task, actor.ID, false, time.Now())
	if errors.Is(err, errPluginTaskBusy) {
		failure(w, 409, "插件任务正在运行")
		return
	}
	if errors.Is(err, errPluginUnavailable) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		failure(w, 503, "暂时无法启动插件任务")
		return
	}
	respond(w, 202, status)
}
func (a *Server) launchCommunityTask(ctx context.Context, id, taskID string, actor int64, scheduled bool, now time.Time) (pluginTaskStatus, error) {
	runtime := &a.communityRuntime
	key := runtimeKey(id, taskID)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	task, ok := runtime.tasks[key]
	if !ok || runtime.closed || runtime.ctx.Err() != nil {
		return pluginTaskStatus{}, errPluginUnavailable
	}
	if _, ok := runtime.running[key]; ok {
		return pluginTaskStatus{}, errPluginTaskBusy
	}
	if len(runtime.running) >= 2 {
		return pluginTaskStatus{}, errPluginTaskBusy
	}
	release, err := a.s.LeasePluginTask(ctx, id, taskID)
	if err != nil {
		return pluginTaskStatus{}, errPluginTaskBusy
	}
	launched := false
	defer func() {
		if !launched {
			release()
		}
	}()
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return pluginTaskStatus{}, err
	}
	defer tx.Rollback()
	var mode string
	if err = tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode); err != nil {
		return pluginTaskStatus{}, err
	}
	if !a.availableCommunity(ctx, id) {
		return pluginTaskStatus{}, errPluginUnavailable
	}
	if actor != 0 {
		var permission int
		var banned bool
		err = tx.QueryRowContext(ctx, a.s.Bind("SELECT permission,banned FROM users WHERE id=? FOR UPDATE"), actor).Scan(&permission, &banned)
		if err != nil || permission < 2 || banned {
			return pluginTaskStatus{}, errPluginUnavailable
		}
	}
	previous, err := a.readPluginTask(ctx, tx, id, taskID)
	if err != nil {
		return pluginTaskStatus{}, err
	}
	if scheduled && (task.interval == 0 || previous.StartedAt != 0 && now.Unix()-previous.StartedAt < int64(task.interval.Seconds())) {
		return previous, nil
	}
	status := pluginTaskStatus{ID: taskID, State: "running", StartedAt: now.Unix()}
	err = a.putSetting(ctx, tx, taskSettingKey(id, taskID), status)
	if err == nil && actor != 0 {
		err = a.s.RecordAdminEvent(ctx, tx, actor, "plugin.task.run", "site", key)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return status, err
	}
	workerCtx, cancel := context.WithTimeout(runtime.ctx, task.task.Timeout)
	runtime.running[key] = cancel
	runtime.jobs.Add(1)
	launched = true
	go func(status pluginTaskStatus) {
		defer runtime.jobs.Done()
		defer release()
		defer cancel()
		err := executePluginTask(workerCtx, task.task.Handler)
		status.State = "succeeded"
		if err != nil || workerCtx.Err() != nil {
			status.State = "failed"
			status.Message = "插件任务未完成，请检查插件配置后重试。"
		}
		status.FinishedAt = time.Now().Unix()
		saveCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = a.persistPluginTask(saveCtx, id, taskID, status)
		runtime.mu.Lock()
		delete(runtime.running, key)
		runtime.mu.Unlock()
	}(status)
	return status, nil
}
func executePluginTask(ctx context.Context, handler func(context.Context) error) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("plugin task panic")
		}
	}()
	return handler(ctx)
}
func (a *Server) persistPluginTask(ctx context.Context, id, task string, status pluginTaskStatus) error {
	tx, err := a.s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = a.putSetting(ctx, tx, taskSettingKey(id, task), status); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *Server) cancelCommunityTasks(id string) {
	runtime := &a.communityRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	for key, cancel := range runtime.running {
		owner := strings.SplitN(key, "/", 2)[0]
		state, _ := a.plugins.State(owner)
		cancelTask := owner == id
		for _, dep := range state.Manifest.Dependencies {
			cancelTask = cancelTask || dep == id
		}
		if cancelTask {
			cancel()
		}
	}
}
func (a *Server) stopCommunityTasks() {
	runtime := &a.communityRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.closed = true
	for _, cancel := range runtime.running {
		cancel()
	}
}
func (a *Server) runCommunityTasks(ctx context.Context, now time.Time) {
	for _, state := range a.plugins.States() {
		if pluginOrigin(state.Manifest.ID) != "community" {
			continue
		}
		id := state.Manifest.ID
		for _, d := range state.Manifest.Tasks {
			a.communityRuntime.mu.Lock()
			task, ok := a.communityRuntime.tasks[runtimeKey(id, d.ID)]
			a.communityRuntime.mu.Unlock()
			if !ok {
				continue
			}
			if !a.availableCommunity(ctx, id) {
				a.cancelCommunityTasks(id)
				continue
			}
			status, err := a.readPluginTask(ctx, a.s.DB, id, d.ID)
			if err != nil {
				continue
			}
			if status.State == "running" {
				a.recoverCommunityTask(ctx, id, d.ID)
				continue
			}
			if task.interval > 0 && (status.StartedAt == 0 || now.Unix()-status.StartedAt >= int64(task.interval.Seconds())) {
				_, _ = a.launchCommunityTask(ctx, id, d.ID, 0, true, now)
			}
		}
	}
}
func (a *Server) recoverCommunityTask(ctx context.Context, id, task string) {
	runtime := &a.communityRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if _, ok := runtime.running[runtimeKey(id, task)]; ok {
		return
	}
	release, err := a.s.LeasePluginTask(ctx, id, task)
	if err != nil {
		return
	}
	defer release()
	status, err := a.readPluginTask(ctx, a.s.DB, id, task)
	if err != nil || status.State != "running" {
		return
	}
	status.State = "failed"
	status.FinishedAt = time.Now().Unix()
	status.Message = "任务因服务中断而停止，请重新执行。"
	_ = a.persistPluginTask(ctx, id, task, status)
}
