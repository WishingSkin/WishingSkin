package plugins

import (
	"context"
	"fmt"
	"sync"
)

type EventHandler func(context.Context, any) error

type EventBus struct {
	mu       sync.RWMutex
	handlers map[string][]EventHandler
}

func NewEventBus() *EventBus { return &EventBus{handlers: map[string][]EventHandler{}} }

func (b *EventBus) Subscribe(event string, handler EventHandler) error {
	if event == "" || handler == nil {
		return fmt.Errorf("event and handler are required")
	}
	b.mu.Lock()
	b.handlers[event] = append(b.handlers[event], handler)
	b.mu.Unlock()
	return nil
}

func (b *EventBus) Publish(ctx context.Context, event string, payload any) []error {
	b.mu.RLock()
	handlers := append([]EventHandler(nil), b.handlers[event]...)
	b.mu.RUnlock()
	errs := make([]error, 0)
	for _, handler := range handlers {
		func() {
			defer func() {
				if value := recover(); value != nil {
					errs = append(errs, fmt.Errorf("plugin event panic: %v", value))
				}
			}()
			if err := handler(ctx, payload); err != nil {
				errs = append(errs, err)
			}
		}()
	}
	return errs
}
