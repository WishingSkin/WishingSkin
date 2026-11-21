package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

type runtimeFixture struct{ calls *atomic.Int32 }

func (runtimeFixture) Manifest() sdk.Manifest {
	return sdk.Manifest{ID: "community.runtime", Version: "1", SettingsNamespace: "community.runtime", Pages: []sdk.PageContribution{{ID: "overview", Area: "admin.plugins", Label: "Overview"}, {ID: "invalid", Area: "admin.plugins", Label: "Invalid"}}, Tasks: []sdk.TaskDeclaration{{ID: "scheduled", Schedule: "@every 1m"}, {ID: "blocking"}, {ID: "failure"}, {ID: "panic"}, {ID: "timeout"}}}
}
func (p runtimeFixture) Register(h sdk.Host) error {
	if err := h.Page(sdk.Page{ID: "overview", Handler: func(_ context.Context, u sdk.Principal) (sdk.PageDocument, error) {
		return sdk.PageDocument{Title: "Runtime", Summary: "<script>unsafe()</script>", Sections: []sdk.PageSection{{Items: []sdk.PageItem{{Label: "Actor", Value: fmt.Sprint(u.ID)}}}}}, nil
	}}); err != nil {
		return err
	}
	if err := h.Page(sdk.Page{ID: "invalid", Handler: func(context.Context, sdk.Principal) (sdk.PageDocument, error) {
		return sdk.PageDocument{Title: strings.Repeat("x", 257)}, nil
	}}); err != nil {
		return err
	}
	for _, id := range []string{"scheduled", "blocking", "failure", "panic", "timeout"} {
		task := sdk.Task{ID: id, Timeout: time.Second}
		if id == "blocking" {
			task.Timeout = 15 * time.Second
		}
		task.Handler = func(ctx context.Context) error {
			p.calls.Add(1)
			switch id {
			case "blocking", "timeout":
				<-ctx.Done()
				return ctx.Err()
			case "failure":
				return errors.New("synthetic-private-credential")
			case "panic":
				panic("synthetic-private-credential")
			default:
				return nil
			}
		}
		if err := h.Task(task); err != nil {
			return err
		}
	}
	return nil
}
func awaitPluginTask(t *testing.T, a *Server, id, state string) pluginTaskStatus {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		status, err := a.readPluginTask(context.Background(), a.s.DB, "community.runtime", id)
		if err != nil {
			t.Fatal(err)
		}
		a.communityRuntime.mu.Lock()
		_, running := a.communityRuntime.running[runtimeKey("community.runtime", id)]
		a.communityRuntime.mu.Unlock()
		if status.State == state && !running {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not become %s", id, state)
	return pluginTaskStatus{}
}
func TestCommunityRuntimeBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			calls := &atomic.Int32{}
			cfg := f.app.cfg
			cfg.CommunityPlugins = []sdk.Plugin{runtimeFixture{calls: calls}}
			app, err := New(f.db, cfg)
			if err != nil {
				t.Fatal(err)
			}
			f.app = app
			t.Cleanup(func() { app.stopCommunityTasks(); app.WaitBackground() })
			base := "/api/admin/plugin-runtime/community.runtime"
			webStatus(t, f.call(3, "GET", base, nil), 404)
			webStatus(t, f.call(3, "POST", base+"/tasks/scheduled", nil), 404)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/community.runtime", map[string]bool{"enabled": true}), 200)
			webStatus(t, f.call(0, "GET", base, nil), 401)
			webStatus(t, f.call(1, "GET", base, nil), 403)
			webStatus(t, f.call(1, "GET", base+"/pages/overview", nil), 403)
			webStatus(t, f.call(1, "POST", base+"/tasks/scheduled", nil), 403)
			w := f.call(3, "GET", base, nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), "scheduled") {
				t.Fatal("registered task missing")
			}
			w = f.call(3, "GET", base+"/pages/overview", nil)
			webStatus(t, w, 200)
			var doc sdk.PageDocument
			if json.Unmarshal(w.Body.Bytes(), &doc) != nil || doc.Sections[0].Items[0].Value != "3" || strings.Contains(w.Body.String(), "<script>") {
				t.Fatal("page response invalid")
			}
			webStatus(t, f.call(3, "GET", base+"/pages/invalid", nil), 503)
			r := httptest.NewRequest("POST", "http://example.test"+base+"/tasks/scheduled", nil)
			r.Header.Set("Origin", "http://example.test")
			r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 3)})
			w = httptest.NewRecorder()
			app.ServeHTTP(w, r)
			webStatus(t, w, 403)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "POST", base+"/tasks/scheduled", nil), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			if calls.Load() != 0 {
				t.Fatal("failed audit executed task")
			}
			webStatus(t, f.call(3, "POST", base+"/tasks/scheduled", nil), 202)
			first := awaitPluginTask(t, app, "scheduled", "succeeded")
			app.runCommunityTasks(context.Background(), time.Unix(first.StartedAt+30, 0))
			if calls.Load() != 1 {
				t.Fatal("schedule ignored interval")
			}
			app.runCommunityTasks(context.Background(), time.Unix(first.StartedAt+61, 0))
			awaitPluginTask(t, app, "scheduled", "succeeded")
			if calls.Load() != 2 {
				t.Fatal("due schedule did not execute")
			}
			for _, id := range []string{"failure", "panic", "timeout"} {
				webStatus(t, f.call(3, "POST", base+"/tasks/"+id, nil), 202)
				status := awaitPluginTask(t, app, id, "failed")
				if strings.Contains(status.Message, "credential") {
					t.Fatal("task error leaked")
				}
			}
			webStatus(t, f.call(3, "POST", base+"/tasks/blocking", nil), 202)
			webStatus(t, f.call(3, "POST", base+"/tasks/blocking", nil), 409)
			// A second server shares the same database lease, but not the Go mutex.
			other, err := New(f.db, cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { other.stopCommunityTasks(); other.WaitBackground() })
			// Hold an independent lease to test exclusion without relying on timer timing.
			release, err := f.db.LeasePluginTask(context.Background(), "community.runtime", "failure")
			if err != nil {
				t.Fatal(err)
			}
			before := calls.Load()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_, err = other.launchCommunityTask(ctx, "community.runtime", "failure", 3, false, time.Now())
			cancel()
			release()
			if !errors.Is(err, errPluginTaskBusy) || calls.Load() != before {
				t.Fatal("cross-process exclusion failed", err)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/community.runtime", map[string]bool{"enabled": false}), 200)
			awaitPluginTask(t, app, "blocking", "failed")
			webStatus(t, f.call(3, "GET", base+"/pages/overview", nil), 404)
			before = calls.Load()
			app.runCommunityTasks(context.Background(), time.Now().Add(time.Hour))
			if calls.Load() != before {
				t.Fatal("disabled task ran")
			}
			// Restart recovery may mark stale work only after obtaining its lease.
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/community.runtime", map[string]bool{"enabled": true}), 200)
			stale := pluginTaskStatus{ID: "failure", State: "running", StartedAt: time.Now().Add(-time.Hour).Unix()}
			if err = app.persistPluginTask(context.Background(), "community.runtime", "failure", stale); err != nil {
				t.Fatal(err)
			}
			other.recoverCommunityTask(context.Background(), "community.runtime", "failure")
			awaitPluginTask(t, other, "failure", "failed")
			app.stopCommunityTasks()
			webStatus(t, f.call(3, "POST", base+"/tasks/failure", nil), 404)
		})
	}
}

func TestCommunityRuntimeRejectsUndeclaredHandlers(t *testing.T) {
	a := &Server{communityRuntime: communityRuntime{pages: map[string]sdk.Page{}, tasks: map[string]registeredTask{}}}
	h := communityHost{a: a, manifest: runtimeFixture{}.Manifest()}
	if h.Page(sdk.Page{ID: "unknown", Handler: func(context.Context, sdk.Principal) (sdk.PageDocument, error) { return sdk.PageDocument{}, nil }}) == nil {
		t.Fatal("undeclared page accepted")
	}
	if h.Task(sdk.Task{ID: "unknown", Handler: func(context.Context) error { return nil }}) == nil {
		t.Fatal("undeclared task accepted")
	}
	if h.Task(sdk.Task{ID: "scheduled", Timeout: time.Hour, Handler: func(context.Context) error { return nil }}) == nil {
		t.Fatal("unbounded task timeout accepted")
	}
	task := sdk.Task{ID: "scheduled", Handler: func(context.Context) error { return nil }}
	if err := h.Task(task); err != nil {
		t.Fatal(err)
	}
	if h.Task(task) == nil {
		t.Fatal("duplicate task accepted")
	}
}
