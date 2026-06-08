package main

import (
	"context"
	"fmt"
	"strings"

	"loack/provider"
)

// dispatcher routes a resource (by API group) to the provider that handles it.
// A provider may be in-process (Local) or a separate binary (Remote); the core
// only talks to the Provider interface, so it links no controllers itself when
// only remote providers are configured.
type dispatcher struct {
	providers []provider.Provider
	byGroup   map[string]provider.Provider
	kinds     int

	// discover, when set, locates and launches a provider for an API group that
	// isn't registered yet (lazy auto-discovery; see the split build). nil means
	// the provider set is fixed (all-in-one, or an explicit LOACK_PROVIDERS list).
	discover func(group string) (provider.Provider, error)
	// locate, when set, checks a provider is available for a group WITHOUT
	// launching it -- used by plan to fail early on a missing provider.
	locate func(group string) error
	// installed names the provider binaries found on disk, and installedDirs the
	// directories they were found in (status output only; launched on demand).
	installed     []string
	installedDirs []string
}

var theDispatcher *dispatcher

// providers returns the process-wide dispatcher, building it on first use.
func providers() (*dispatcher, error) {
	if theDispatcher != nil {
		return theDispatcher, nil
	}
	d, err := buildDispatcher()
	if err != nil {
		return nil, err
	}
	theDispatcher = d
	return d, nil
}

func (d *dispatcher) register(p provider.Provider) error {
	resp, err := p.Call(context.Background(), provider.Request{Op: provider.OpCapabilities}, nil)
	if err != nil {
		return fmt.Errorf("provider capabilities: %w", err)
	}
	for _, g := range resp.GVKs {
		d.byGroup[g.Group] = p
	}
	d.providers = append(d.providers, p)
	d.kinds += len(resp.GVKs)
	return nil
}

// For returns the provider that handles the given apiVersion's group, launching
// it on demand if auto-discovery is enabled and it isn't registered yet.
func (d *dispatcher) For(apiVersion string) (provider.Provider, error) {
	group := groupOf(apiVersion)
	if p, ok := d.byGroup[group]; ok {
		return p, nil
	}
	if d.discover != nil {
		p, err := d.discover(group)
		if err != nil {
			return nil, err
		}
		if err := d.register(p); err != nil {
			return nil, err
		}
		if p, ok := d.byGroup[group]; ok {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no provider for %s", apiVersion)
}

// groupOf returns the API group of an apiVersion ("s3.services.k8s.aws/v1alpha1"
// -> "s3.services.k8s.aws").
func groupOf(apiVersion string) string {
	if i := strings.Index(apiVersion, "/"); i >= 0 {
		return apiVersion[:i]
	}
	return apiVersion
}

// preflight checks a provider is available for apiVersion's group without
// launching it, so plan fails early on a missing provider instead of at apply.
// An already-registered group is fine; in discovery mode it checks the binary is
// installed; otherwise the group must already be registered.
func (d *dispatcher) preflight(apiVersion string) error {
	group := groupOf(apiVersion)
	if _, ok := d.byGroup[group]; ok {
		return nil
	}
	if d.locate != nil {
		return d.locate(group)
	}
	return fmt.Errorf("no provider for %s", apiVersion)
}

// summaryLine describes the configured providers for `init` output.
func (d *dispatcher) summaryLine() string {
	if d.discover != nil {
		if len(d.installed) == 0 {
			return "none installed; the needed ones are downloaded from the release on demand"
		}
		return fmt.Sprintf("%d installed in %s; others downloaded on demand by API group",
			len(d.installed), strings.Join(d.installedDirs, ", "))
	}
	return fmt.Sprintf("%d kinds across %d API groups wired", d.kinds, len(d.byGroup))
}

func (d *dispatcher) controllers() int   { return len(d.byGroup) }
func (d *dispatcher) resourceKinds() int { return d.kinds }

func (d *dispatcher) close() {
	for _, p := range d.providers {
		_ = p.Close()
	}
}
