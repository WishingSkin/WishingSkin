package plugins

import (
	"fmt"
	"sort"
	"sync"
)

type State struct {
	Manifest Manifest `json:"manifest"`
	Enabled  bool     `json:"enabled"`
	Error    string   `json:"error,omitempty"`
}

type Registry struct {
	mu     sync.RWMutex
	states map[string]State
}

func NewRegistry(manifests ...Manifest) (*Registry, error) {
	r := &Registry{states: map[string]State{}}
	for _, manifest := range manifests {
		if err := r.Register(manifest); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Registry) Register(manifest Manifest) error {
	for _, p := range []*[]string{&manifest.Dependencies, &manifest.Routes, &manifest.Capabilities, &manifest.Events, &manifest.Permissions} {
		if *p == nil {
			*p = []string{}
		}
	}
	if manifest.Pages == nil {
		manifest.Pages = []PageContribution{}
	}
	if manifest.Tasks == nil {
		manifest.Tasks = []TaskDeclaration{}
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.states[manifest.ID]; exists {
		return fmt.Errorf("duplicate plugin id %q", manifest.ID)
	}
	state := State{Manifest: manifest, Enabled: true}
	for _, dependency := range manifest.Dependencies {
		if _, exists := r.states[dependency]; !exists {
			state.Enabled = false
			state.Error = "missing dependency: " + dependency
			break
		}
	}
	r.states[manifest.ID] = state
	return nil
}

func (r *Registry) SetEnabled(id string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.states[id]
	if !ok {
		return fmt.Errorf("unknown plugin %q", id)
	}
	if state.Error != "" && enabled {
		return fmt.Errorf("plugin %q is unavailable: %s", id, state.Error)
	}
	state.Enabled = enabled
	r.states[id] = state
	return nil
}

func (r *Registry) State(id string) (State, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state, ok := r.states[id]
	return state, ok
}

func (r *Registry) States() []State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]State, 0, len(r.states))
	for _, state := range r.states {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}
