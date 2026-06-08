//go:build split

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"loack/internal/state"
)

// defaultProviderRepo is the GitHub repo whose releases hold the published
// provider binaries (loack-provider-<svc>_<version>_<os>_<arch>) and SHA256SUMS.
const defaultProviderRepo = "chanwit/loack"

// errDownloadDisabled means no repo/version is configured to download from, so
// the caller should fall back to the local "not found, build it" guidance.
var errDownloadDisabled = errors.New("provider download not configured")

// resolveProviderBinary returns a usable provider binary for svc: a locally
// installed one if present, otherwise one downloaded from the release and
// verified against SHA256SUMS, cached under .loack/providers/.
func resolveProviderBinary(svc string) (string, error) {
	if p, err := locateProviderBinary(svc); err == nil {
		return p, nil
	} else {
		dl, derr := downloadProvider(svc)
		if derr == nil {
			return dl, nil
		}
		if errors.Is(derr, errDownloadDisabled) {
			return "", err // surface the local "build it with make" guidance
		}
		return "", fmt.Errorf("%v; download also failed: %w", err, derr)
	}
}

// providerRepo is the GitHub repo to download published providers from.
func providerRepo() string { return envOr("LOACK_PROVIDER_REPO", defaultProviderRepo) }

// providerVersion resolves which release to download from. An explicit
// LOACK_PROVIDER_VERSION always wins; otherwise the core's own stamped version
// is used, but ONLY when it is a clean release tag (vMAJOR.MINOR.PATCH) -- a
// dev/dirty build returns "" so download is disabled and the caller falls back
// to the local "build it" guidance instead of 404-ing on a non-existent tag.
func providerVersion() string {
	if v := os.Getenv("LOACK_PROVIDER_VERSION"); v != "" {
		return v
	}
	if releaseVersionRe.MatchString(version) {
		return version
	}
	return ""
}

var releaseVersionRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// downloadProvider fetches loack-provider-<svc> for this OS/arch from the
// configured repo's release at the configured version, verifies its sha256
// against the release SHA256SUMS, and caches it under .loack/providers/.
func downloadProvider(svc string) (string, error) {
	repo, ver := providerRepo(), providerVersion()
	if repo == "" || ver == "" {
		return "", errDownloadDisabled
	}

	asset := fmt.Sprintf("loack-provider-%s_%s_%s_%s", svc, ver, runtime.GOOS, runtime.GOARCH)
	cacheDir := filepath.Join(state.WorkDir, "providers")
	dst := filepath.Join(cacheDir, asset)
	if isExecutableFile(dst) {
		return dst, nil // already downloaded
	}

	base := fmt.Sprintf("https://github.com/%s/releases/download/%s", repo, ver)

	sums, err := httpGetString(base + "/SHA256SUMS")
	if err != nil {
		return "", fmt.Errorf("fetching checksums for %s %s: %w", repo, ver, err)
	}
	want := shaFromSums(sums, asset)
	if want == "" {
		return "", fmt.Errorf("no checksum for %s in %s release %s (is %s/%s published there?)",
			asset, repo, ver, runtime.GOOS, runtime.GOARCH)
	}

	fmt.Fprintf(os.Stderr, "loack: downloading %s from %s %s...\n", asset, repo, ver)
	data, err := httpGetBytes(base + "/" + asset)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", asset, err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return "", fmt.Errorf("checksum mismatch for %s: got %s, want %s", asset, got, want)
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// shaFromSums returns the hex sha256 for name from a SHA256SUMS body
// ("<sha>  <name>" per line), or "".
func shaFromSums(sums, name string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == name {
			return f[0]
		}
	}
	return ""
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

func httpGetBytes(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func httpGetString(url string) (string, error) {
	b, err := httpGetBytes(url)
	return string(b), err
}
