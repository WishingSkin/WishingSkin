package plugins

import (
	"context"
	"errors"
	"testing"
)

func TestRegistryRejectsDuplicateAndKeepsMissingDependencyDisabled(t *testing.T) {
	r, err := NewRegistry(Manifest{ID: "base", Version: "1.0.0", SettingsNamespace: "base"}, Manifest{ID: "child", Version: "1.0.0", SettingsNamespace: "child", Dependencies: []string{"missing"}})
	if err != nil {
		t.Fatal(err)
	}
	state, ok := r.State("child")
	if !ok || state.Enabled || state.Error == "" {
		t.Fatalf("missing dependency state: %+v", state)
	}
	if err := r.Register(Manifest{ID: "base", Version: "1.0.0", SettingsNamespace: "base"}); err == nil {
		t.Fatal("duplicate plugin accepted")
	}
}

func TestEventFailureDoesNotStopOtherHandlers(t *testing.T) {
	bus := NewEventBus()
	called := false
	_ = bus.Subscribe("character.create", func(context.Context, any) error { return errors.New("broken") })
	_ = bus.Subscribe("character.create", func(context.Context, any) error { called = true; return nil })
	if errs := bus.Publish(context.Background(), "character.create", nil); len(errs) != 1 || !called {
		t.Fatalf("event isolation failed: errs=%v called=%v", errs, called)
	}
}

func TestTaskRegistryRejectsDuplicate(t *testing.T) {
	tasks := NewTaskRegistry()
	if err := tasks.Register("cleanup", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Register("cleanup", func(context.Context) error { return nil }); err == nil {
		t.Fatal("duplicate task accepted")
	}
}
