package plugins

import (
	"context"
	"fmt"
	"sync"
)

type TaskHandler func(context.Context) error

type TaskRegistry struct {
	mu    sync.RWMutex
	tasks map[string]TaskHandler
}

func NewTaskRegistry() *TaskRegistry { return &TaskRegistry{tasks: map[string]TaskHandler{}} }

func (r *TaskRegistry) Register(id string, handler TaskHandler) error {
	if id == "" || handler == nil {
		return fmt.Errorf("task id and handler are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tasks[id]; exists {
		return fmt.Errorf("duplicate task id %q", id)
	}
	r.tasks[id] = handler
	return nil
}

func (r *TaskRegistry) Run(ctx context.Context, id string) error {
	r.mu.RLock()
	handler, ok := r.tasks[id]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown task %q", id)
	}
	return handler(ctx)
}
