// Package plugin contains the public contracts for source-built and executable plugins.
package plugin

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

type PageContribution struct {
	ID    string `json:"id"`
	Area  string `json:"area"`
	Label string `json:"label"`
}

type TaskDeclaration struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
}

type Manifest struct {
	Name              string             `json:"name,omitempty"`
	Description       string             `json:"description,omitempty"`
	ID                string             `json:"id"`
	Version           string             `json:"version"`
	Dependencies      []string           `json:"dependencies"`
	SettingsNamespace string             `json:"settingsNamespace"`
	MigrationVersion  int                `json:"migrationVersion"`
	Routes            []string           `json:"routes"`
	Capabilities      []string           `json:"capabilities"`
	Events            []string           `json:"events"`
	Pages             []PageContribution `json:"pages"`
	Tasks             []TaskDeclaration  `json:"tasks"`
	Permissions       []string           `json:"permissions"`
	Settings          []SettingField     `json:"settings,omitempty"`
}

func (m Manifest) Validate() error {
	if !idPattern.MatchString(m.ID) || len(m.ID) > 64 {
		return fmt.Errorf("invalid plugin id %q", m.ID)
	}
	if m.Version == "" || m.SettingsNamespace == "" {
		return fmt.Errorf("plugin %q requires version and settings namespace", m.ID)
	}
	if !idPattern.MatchString(m.SettingsNamespace) {
		return fmt.Errorf("invalid settings namespace %q", m.SettingsNamespace)
	}
	if len(m.Settings) > 32 {
		return fmt.Errorf("too many plugin settings")
	}
	seen := map[string]bool{}
	for _, f := range m.Settings {
		if !idPattern.MatchString(f.Key) || seen[f.Key] || f.Label == "" || (f.Type != "text" && f.Type != "secret" && f.Type != "boolean" && f.Type != "number") {
			return fmt.Errorf("invalid plugin setting")
		}
		seen[f.Key] = true
	}
	if len(m.Pages) > 16 || len(m.Tasks) > 16 {
		return fmt.Errorf("too many plugin contributions")
	}
	seen = map[string]bool{}
	for _, p := range m.Pages {
		if !idPattern.MatchString(p.ID) || len(p.ID) > 64 || p.Area != "admin.plugins" || p.Label == "" || len(p.Label) > 256 || seen[p.ID] {
			return fmt.Errorf("invalid plugin page")
		}
		seen[p.ID] = true
	}
	seen = map[string]bool{}
	for _, task := range m.Tasks {
		if !idPattern.MatchString(task.ID) || len(task.ID) > 64 || seen[task.ID] {
			return fmt.Errorf("invalid plugin task")
		}
		if _, err := TaskInterval(task.Schedule); err != nil {
			return err
		}
		seen[task.ID] = true
	}
	return nil
}

// TaskInterval accepts an empty manual schedule or a fixed interval.
func TaskInterval(schedule string) (time.Duration, error) {
	if schedule == "" {
		return 0, nil
	}
	if len(schedule) < 8 || schedule[:7] != "@every " {
		return 0, fmt.Errorf("invalid plugin schedule")
	}
	d, err := time.ParseDuration(schedule[7:])
	if err != nil || d < time.Minute || d > 168*time.Hour {
		return 0, fmt.Errorf("plugin interval must be 1m–168h")
	}
	return d, nil
}

// Host is the intentionally narrow surface exposed to a community plugin.
// Authentication, CSRF, permissions and SQL transactions remain owned by the
// application and are never handed to plugin code.
type Host interface {
	Handle(Route) error
	Settings(context.Context, any) error
	Subscribe(string, func(context.Context, Event) error) error
	Page(Page) error
	Task(Task) error
}

// PageDocument is rendered as text by the host. It contains no HTML or scripts.
type PageDocument struct {
	Title    string        `json:"title"`
	Summary  string        `json:"summary"`
	Sections []PageSection `json:"sections"`
}
type PageSection struct {
	Title string     `json:"title"`
	Body  string     `json:"body"`
	Items []PageItem `json:"items"`
}
type PageItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
type Page struct {
	ID      string
	Handler func(context.Context, Principal) (PageDocument, error)
}

// Task handlers must honor context cancellation. The host never kills goroutines.
type Task struct {
	ID      string
	Timeout time.Duration
	Handler func(context.Context) error
}

type Plugin interface {
	Manifest() Manifest
	Register(Host) error
}

type SettingField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
}
type Principal struct {
	ID    int64
	Admin bool
}
type Route struct {
	Method  string
	Path    string
	Access  string
	Handler func(http.ResponseWriter, *http.Request, Principal)
}
type Event struct {
	Name       string
	UserID     int64
	ResourceID string
}
