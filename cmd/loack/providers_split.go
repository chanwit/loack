//go:build split

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"loack/internal/state"
	"loack/provider"
)

// buildDispatcher (split build) wires out-of-process providers. This build links
// no controllers, no AWS SDK service clients, and no Kubernetes API machinery --
// the core is small; each provider binary carries its own (and may be its own
// module with its own runtime version).
//
// Two modes:
//   - explicit:  $LOACK_PROVIDERS is a PATH-list of provider binaries to launch.
//   - discovery (default): providers are located by API group and launched on
//     demand. A manifest's group "<svc>.services.k8s.aws" maps to the binary
//     "loack-provider-<svc>", searched for in the provider dirs and $PATH.
func buildDispatcher() (*dispatcher, error) {
	d := &dispatcher{byGroup: map[string]provider.Provider{}}

	if list := strings.TrimSpace(os.Getenv("LOACK_PROVIDERS")); list != "" {
		for _, bin := range strings.Split(list, string(os.PathListSeparator)) {
			if bin = strings.TrimSpace(bin); bin == "" {
				continue
			}
			p, err := provider.NewRemote(bin)
			if err != nil {
				return nil, err
			}
			if err := d.register(p); err != nil {
				return nil, err
			}
		}
		return d, nil
	}

	d.discover = discoverProvider
	d.locate = locateProvider
	d.installed, d.installedDirs = installedProviders()
	return d, nil
}

// locateProvider checks, without launching, that a provider binary exists for
// the API group (used by plan's pre-flight).
func locateProvider(group string) error {
	svc, ok := svcForGroup(group)
	if !ok {
		return fmt.Errorf("cannot derive a provider for API group %q", group)
	}
	_, err := resolveProviderBinary(svc)
	return err
}

// providerSearchDirs lists the directories searched for provider binaries, in
// priority order: $LOACK_PROVIDERS_DIR entries, the directory of the running
// executable (so bin/loack finds bin/loack-provider-*), then
// .loack/providers in the workspace.
func providerSearchDirs() []string {
	var dirs []string
	if v := os.Getenv("LOACK_PROVIDERS_DIR"); v != "" {
		dirs = append(dirs, filepath.SplitList(v)...)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	dirs = append(dirs, filepath.Join(state.WorkDir, "providers"))
	return dirs
}

// svcForGroup maps an ACK API group to its service alias:
// "s3.services.k8s.aws" -> "s3". Returns false for non-ACK groups.
func svcForGroup(group string) (string, bool) {
	const suffix = ".services.k8s.aws"
	if !strings.HasSuffix(group, suffix) {
		return "", false
	}
	return strings.TrimSuffix(group, suffix), true
}

// discoverProvider locates and launches the provider for an API group.
func discoverProvider(group string) (provider.Provider, error) {
	svc, ok := svcForGroup(group)
	if !ok {
		return nil, fmt.Errorf("cannot derive a provider for API group %q", group)
	}
	bin, err := resolveProviderBinary(svc)
	if err != nil {
		return nil, err
	}
	return provider.NewRemote(bin)
}

// locateProviderBinary finds the loack-provider-<svc> executable in the search
// dirs, then on $PATH.
func locateProviderBinary(svc string) (string, error) {
	name := "loack-provider-" + svc
	for _, dir := range providerSearchDirs() {
		if p := filepath.Join(dir, name); isExecutableFile(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("provider %q not found (searched %s and $PATH); build it with 'make provider-%s'",
		name, strings.Join(providerSearchDirs(), ", "), svc)
}

// installedProviders returns the svc names of provider binaries found in the
// search dirs, and the directories they were found in. It does not launch them;
// it's for status output.
func installedProviders() (svcs []string, dirs []string) {
	seenSvc := map[string]bool{}
	seenDir := map[string]bool{}
	for _, dir := range providerSearchDirs() {
		matches, _ := filepath.Glob(filepath.Join(dir, "loack-provider-*"))
		for _, m := range matches {
			if !isExecutableFile(m) {
				continue
			}
			svc := strings.TrimPrefix(filepath.Base(m), "loack-provider-")
			if svc == "" || seenSvc[svc] {
				continue
			}
			seenSvc[svc] = true
			svcs = append(svcs, svc)
			if !seenDir[dir] {
				seenDir[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	sort.Strings(svcs)
	return svcs, dirs
}

func isExecutableFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}
