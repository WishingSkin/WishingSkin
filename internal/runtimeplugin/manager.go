package runtimeplugin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"sync"
)

type Options struct {
	OfficialPublicKeys []ed25519.PublicKey
	HostAPIURL         string
	HostTokenFunc      func(pluginID string) string
}

type Installed struct {
	Package        Package  `json:"package"`
	Digest         string   `json:"digest"`
	Grants         []string `json:"grants"`
	SignedOfficial bool     `json:"signedOfficial,omitempty"`
}
type Manager struct {
	mu            sync.RWMutex
	root          string
	items         map[string]Installed
	processes     map[string]*process
	closed        bool
	officialKeys  []ed25519.PublicKey
	hostAPIURL    string
	hostTokenFunc func(string) string
}

func Open(root string) (*Manager, error) {
	return OpenWithOptions(root, Options{OfficialPublicKeys: BuiltInOfficialPublicKeys()})
}

func OpenWithOptions(root string, opts Options) (*Manager, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	m := &Manager{
		root:          root,
		items:         map[string]Installed{},
		processes:     map[string]*process{},
		officialKeys:  slices.Clone(opts.OfficialPublicKeys),
		hostAPIURL:    opts.HostAPIURL,
		hostTokenFunc: opts.HostTokenFunc,
	}
	data, err := os.ReadFile(filepath.Join(root, "installed.json"))
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &m.items); err != nil {
		return nil, err
	}
	for id, item := range m.items {
		if _, err := hex.DecodeString(item.Digest); err != nil {
			return nil, errors.New("invalid digest")
		}
		archive, err := os.ReadFile(m.Root(item) + ".zip")
		if err != nil || Digest(archive) != item.Digest {
			return nil, errors.New("installed package checksum mismatch")
		}
		pkg, signed, err := InspectWithKeys(archive, m.officialKeys)
		if err != nil || !reflect.DeepEqual(pkg, item.Package) {
			return nil, errors.New("installed package manifest mismatch")
		}
		if item.Package.Manifest.ID != id || item.Package.validateStructure() != nil || len(item.Digest) != 64 {
			return nil, errors.New("invalid installed plugin index")
		}
		item.SignedOfficial = signed
		m.items[id] = item
		if err := verifyExtractedWithKeys(m.Root(item), item.Package, m.officialKeys); err != nil {
			return nil, err
		}
		for _, grant := range item.Package.Manifest.Permissions {
			if !slices.Contains(item.Grants, grant) {
				return nil, errors.New("missing installed permission")
			}
		}
	}
	return m, nil
}

func (m *Manager) SetHostAPI(hostAPIURL string, tokenFunc func(string) string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hostAPIURL = hostAPIURL
	m.hostTokenFunc = tokenFunc
}

func (m *Manager) Root(item Installed) string {
	return filepath.Join(m.root, item.Package.Manifest.ID, item.Digest)
}
func (m *Manager) Items() []Installed {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Installed{}
	for _, item := range m.items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package.Manifest.ID < out[j].Package.Manifest.ID })
	return out
}
func (m *Manager) Get(id string) (Installed, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.items[id]
	return item, ok
}
func (m *Manager) save() error {
	data, err := json.Marshal(m.items)
	if err != nil {
		return err
	}
	name := filepath.Join(m.root, "installed.json.new")
	if err = os.WriteFile(name, data, 0600); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(m.root, "installed.json"))
}

// Stage validates and unpacks an archive before activation. Previous versions remain available on disk.
func (m *Manager) Stage(data []byte, grants []string) (Installed, error) {
	pkg, signed, err := InspectWithKeys(data, m.officialKeys)
	if err != nil {
		return Installed{}, err
	}
	for _, p := range pkg.Manifest.Permissions {
		if !slices.Contains(grants, p) {
			return Installed{}, errors.New("permissions must be approved")
		}
	}
	if len(grants) != len(pkg.Manifest.Permissions) {
		return Installed{}, errors.New("unexpected permissions")
	}
	item := Installed{Package: pkg, Digest: Digest(data), Grants: slices.Clone(grants), SignedOfficial: signed}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Installed{}, errors.New("runtime closed")
	}
	root := m.Root(item)
	if archive, e := os.ReadFile(root + ".zip"); e == nil && Digest(archive) == item.Digest {
		return item, verifyExtractedWithKeys(root, pkg, m.officialKeys)
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return Installed{}, err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return Installed{}, err
	}
	defer dir.Close()
	z, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			err = dir.MkdirAll(f.Name, 0700)
		} else {
			if parent := filepath.ToSlash(filepath.Dir(f.Name)); parent != "." {
				err = dir.MkdirAll(parent, 0700)
			}
			if err == nil {
				var dst *os.File
				dst, err = dir.OpenFile(f.Name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
				if err == nil {
					var src io.ReadCloser
					src, err = f.Open()
					if err == nil {
						_, err = io.Copy(dst, io.LimitReader(src, MaxPackage+1))
						src.Close()
					}
					dst.Close()
				}
			}
		}
		if err != nil {
			return Installed{}, err
		}
	}
	if err = os.WriteFile(root+".zip", data, 0600); err != nil {
		return Installed{}, err
	}
	return item, nil
}
func (m *Manager) Probe(ctx context.Context, item Installed) error {
	p := &process{}
	defer p.close()
	m.mu.RLock()
	officialKeys := m.officialKeys
	hostAPIURL := m.hostAPIURL
	hostToken := ""
	if m.hostTokenFunc != nil {
		hostToken = m.hostTokenFunc(item.Package.Manifest.ID)
	}
	m.mu.RUnlock()
	data, err := p.call(ctx, m.Root(item), item.Package, map[string]string{"kind": "health"}, officialKeys, hostAPIURL, hostToken)
	if err != nil {
		return err
	}
	var health struct {
		Protocol int `json:"protocol"`
	}
	if json.Unmarshal(data, &health) != nil || health.Protocol != item.Package.Protocol {
		return errors.New("unsupported runtime protocol")
	}
	return nil
}
func (m *Manager) Activate(item Installed) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("runtime closed")
	}
	id := item.Package.Manifest.ID
	previous, exists := m.items[id]
	m.items[id] = item
	if err := m.save(); err != nil {
		if exists {
			m.items[id] = previous
		} else {
			delete(m.items, id)
		}
		return err
	}
	if process := m.processes[id]; process != nil {
		process.close()
		delete(m.processes, id)
	}
	return nil
}
func (m *Manager) Install(data []byte, grants []string) (Installed, error) {
	item, err := m.Stage(data, grants)
	if err == nil {
		err = m.Activate(item)
	}
	return item, err
}
func (m *Manager) Reload(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[id]; !ok {
		return errors.New("unknown plugin")
	}
	if p := m.processes[id]; p != nil {
		p.close()
		delete(m.processes, id)
	}
	return nil
}
func (m *Manager) Uninstall(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return errors.New("unknown plugin")
	}
	delete(m.items, id)
	if err := m.save(); err != nil {
		m.items[id] = item
		return err
	}
	if p := m.processes[id]; p != nil {
		p.close()
		delete(m.processes, id)
	}
	return nil
}
func (m *Manager) Call(ctx context.Context, id string, request any) (json.RawMessage, error) {
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok || m.closed {
		m.mu.Unlock()
		return nil, errors.New("plugin unavailable")
	}
	p := m.processes[id]
	if p == nil {
		p = &process{}
		m.processes[id] = p
	}
	root := m.Root(item)
	officialKeys := m.officialKeys
	hostAPIURL := m.hostAPIURL
	hostToken := ""
	if m.hostTokenFunc != nil {
		hostToken = m.hostTokenFunc(id)
	}
	m.mu.Unlock()
	return p.call(ctx, root, item.Package, request, officialKeys, hostAPIURL, hostToken)
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for _, p := range m.processes {
		p.close()
	}
	m.processes = map[string]*process{}
}

// verifyExtracted checks executable and frontend bytes before launching a process.
func verifyExtracted(root string, pkg Package) error {
	return verifyExtractedWithKeys(root, pkg, BuiltInOfficialPublicKeys())
}

func verifyExtractedWithKeys(root string, pkg Package, officialKeys []ed25519.PublicKey) error {
	data, err := os.ReadFile(root + ".zip")
	if err != nil {
		return err
	}
	if Digest(data) != filepath.Base(root) {
		return errors.New("package checksum mismatch")
	}
	expected, _, err := InspectWithKeys(data, officialKeys)
	if err != nil || !reflect.DeepEqual(expected, pkg) {
		return errors.New("package manifest mismatch")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		info, err := dir.Lstat(entry.Name)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("invalid extracted file")
		}
		actual, err := dir.ReadFile(entry.Name)
		if err != nil {
			return err
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		original, err := io.ReadAll(io.LimitReader(reader, MaxPackage+1))
		reader.Close()
		if err != nil || !bytes.Equal(actual, original) {
			return errors.New("extracted file checksum mismatch")
		}
	}
	return nil
}
