// Package state implements loack's on-disk state file, a small analogue of
// Terraform's tfstate. It records every resource loack has provisioned -- the
// effective applied object (spec + status), its AWS identifiers, and when it
// was last touched -- so that re-applies are idempotent, drift can be detected,
// and resources can be destroyed without re-reading their manifests.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// WorkDir is loack's per-directory working directory (analogue of .terraform).
const WorkDir = ".loack"

// DefaultPath is the state file used when none is given on the command line.
var DefaultPath = filepath.Join(WorkDir, "state.json")

// Version is the schema version of the state file.
const Version = 1

// Resource is one tracked resource instance.
type Resource struct {
	Address    string          `json:"address"`
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Namespace  string          `json:"namespace,omitempty"`
	Region     string          `json:"region,omitempty"`
	Account    string          `json:"account,omitempty"`
	ARN        string          `json:"arn,omitempty"`
	AppliedAt  time.Time       `json:"appliedAt"`
	Object     json.RawMessage `json:"object,omitempty"`
}

// File is the root document persisted to disk.
type File struct {
	Version   int                 `json:"version"`
	Serial    int                 `json:"serial"`
	UpdatedAt time.Time           `json:"updatedAt"`
	Resources map[string]Resource `json:"resources"`

	path string
}

// Address builds the stable key for a resource:
// "<group/version>/<kind>/<namespace>/<name>". ACK resources are
// namespace-scoped, so the namespace is part of a resource's identity; an empty
// namespace is normalized to "default" to match Kubernetes semantics.
func Address(apiVersion, kind, namespace, name string) string {
	if namespace == "" {
		namespace = "default"
	}
	return fmt.Sprintf("%s/%s/%s/%s", apiVersion, kind, namespace, name)
}

// Load reads the state file at path. A missing file yields an empty,
// ready-to-use state bound to that path (it is created on first Save).
func Load(path string) (*File, error) {
	f := &File{Version: Version, Resources: map[string]Resource{}, path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return nil, fmt.Errorf("reading state %s: %w", path, err)
	}
	if err := json.Unmarshal(data, f); err != nil {
		return nil, fmt.Errorf("parsing state %s: %w", path, err)
	}
	if f.Resources == nil {
		f.Resources = map[string]Resource{}
	}
	f.path = path
	return f, nil
}

// Get returns the tracked resource at address, if present.
func (f *File) Get(address string) (Resource, bool) {
	r, ok := f.Resources[address]
	return r, ok
}

// Put inserts or replaces a tracked resource, keying it by its Address.
func (f *File) Put(r Resource) {
	if r.Address == "" {
		r.Address = Address(r.APIVersion, r.Kind, r.Namespace, r.Name)
	}
	f.Resources[r.Address] = r
}

// Remove drops a tracked resource. It is a no-op if the address is unknown.
func (f *File) Remove(address string) {
	delete(f.Resources, address)
}

// List returns the tracked resources sorted by address for stable output.
func (f *File) List() []Resource {
	out := make([]Resource, 0, len(f.Resources))
	for _, r := range f.Resources {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

// Save writes the state back to disk atomically (write-temp-then-rename),
// bumping the serial and keeping a ".backup" copy of the previous contents.
func (f *File) Save() error {
	f.Version = Version
	f.Serial++
	f.UpdatedAt = time.Now().UTC()

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding state: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(f.path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating state dir: %w", err)
		}
	}

	// Back up the prior state before overwriting it.
	if prior, rerr := os.ReadFile(f.path); rerr == nil {
		_ = os.WriteFile(f.path+".backup", prior, 0o644)
	}

	tmp, err := os.CreateTemp(dir, ".loack-state-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp state: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp state: %w", err)
	}
	if err := os.Rename(tmpName, f.path); err != nil {
		return fmt.Errorf("replacing state %s: %w", f.path, err)
	}
	return nil
}
