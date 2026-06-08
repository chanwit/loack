package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"loack/internal/state"
)

// manifestDoc is one parsed resource document from the working directory.
type manifestDoc struct {
	raw        []byte
	apiVersion string
	kind       string
	name       string
	namespace  string
	isSecret   bool
	refs       []string // metadata.names this doc references via "from: {name: ...}"
	file       string
	order      int // position across all files, for stable tie-breaking
}

func (d manifestDoc) address() string {
	if d.isSecret {
		return secretAddress(d.namespace, d.name)
	}
	return state.Address(d.apiVersion, d.kind, d.namespace, d.name)
}

// nsTarget is the AWS account/region a Kubernetes Namespace binds its resources
// to, via ACK's CARM annotations (services.k8s.aws/owner-account-id and
// /default-region). Either field may be empty.
type nsTarget struct {
	account string
	region  string
}

func (d manifestDoc) display() string {
	if d.isSecret {
		return secretDisplay(d.name)
	}
	return d.kind + "." + d.name
}

// gatherDocs reads every *.yaml / *.yml file in the working directory (the
// configuration), splits them into documents, and returns them ordered so that
// referenced resources come before the resources that reference them. It also
// returns the CARM targeting declared by any Kubernetes Namespaces (keyed by
// namespace name), used to route resources to per-namespace accounts/regions.
// manifestFiles returns every manifest under the workspace root (the current
// directory, after any -C). It recurses the tree like Terraform treats its root
// module, but because KRM is flat data with global references, the whole tree is
// one configuration -- no module wiring needed. So you render each installer
// package into its own subdirectory and loack picks them all up; no copy step.
//
// Discovery rule, per directory:
//   - a directory holding an "out/manifests/" subdir is an installer render:
//     read only out/manifests/*.yaml and skip the rest of that subtree (its
//     un-rendered package/ source and out/spec/ staging);
//   - otherwise read the directory's own *.yaml/*.yml and keep descending.
// Hidden directories (".loack/", etc.) are always skipped.
func manifestFiles() ([]string, error) {
	yamlIn := func(dir string, files *[]string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if ext := filepath.Ext(e.Name()); ext == ".yaml" || ext == ".yml" {
				*files = append(*files, filepath.Join(dir, e.Name()))
			}
		}
		return nil
	}
	hasRendered := func(dir string) bool {
		fi, err := os.Stat(filepath.Join(dir, "out", "manifests"))
		return err == nil && fi.IsDir()
	}

	var files []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil // files are collected per-directory below
		}
		if path != "." && strings.HasPrefix(filepath.Base(path), ".") {
			return fs.SkipDir // .loack and other hidden dirs
		}
		if hasRendered(path) {
			if e := yamlIn(filepath.Join(path, "out", "manifests"), &files); e != nil {
				return e
			}
			return fs.SkipDir // don't descend into package/ source or out/spec
		}
		return yamlIn(path, &files)
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func gatherDocs() ([]manifestDoc, map[string]nsTarget, error) {
	files, err := manifestFiles()
	if err != nil {
		return nil, nil, err
	}

	var docs []manifestDoc
	carm := map[string]nsTarget{}
	ignored := 0
	order := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, nil, err
		}
		for _, raw := range splitDocuments(data) {
			d, perr := parseDoc(raw)
			if perr != nil {
				return nil, nil, fmt.Errorf("%s: %w", f, perr)
			}
			if isIgnoredKind(d.apiVersion, d.kind) {
				// A Namespace isn't provisioned, but its CARM annotations select
				// the account/region for resources in that namespace.
				if t, ok := namespaceTarget(raw); ok {
					carm[d.name] = t
				}
				ignored++
				continue
			}
			d.file = f
			d.order = order
			order++
			docs = append(docs, d)
		}
	}
	if ignored > 0 {
		outf("Note: ignoring %d Kubernetes Namespace resource(s) (used only for account/region targeting).", ignored)
	}
	return orderDocs(docs), carm, nil
}

// namespaceTarget extracts the CARM account/region annotations from a Namespace
// manifest. ok is false when neither annotation is present.
func namespaceTarget(raw []byte) (nsTarget, bool) {
	var ns struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if err := yaml.Unmarshal(raw, &ns); err != nil {
		return nsTarget{}, false
	}
	a := ns.Metadata.Annotations
	t := nsTarget{
		account: a["services.k8s.aws/owner-account-id"],
		region:  a["services.k8s.aws/default-region"],
	}
	return t, t.account != "" || t.region != ""
}

func parseDoc(raw []byte) (manifestDoc, error) {
	var head struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}
	if err := yaml.Unmarshal(raw, &head); err != nil {
		return manifestDoc{}, err
	}
	if head.APIVersion == "" || head.Kind == "" {
		return manifestDoc{}, fmt.Errorf("document missing apiVersion or kind")
	}
	return manifestDoc{
		raw:        raw,
		apiVersion: head.APIVersion,
		kind:       head.Kind,
		name:       head.Metadata.Name,
		namespace:  head.Metadata.Namespace,
		isSecret:   isK8sSecret(head.APIVersion, head.Kind),
		refs:       scanRefs(raw),
	}, nil
}

// scanRefs finds the metadata.names a document references via the ACK
// "from: {name: X}" reference wrapper, used to derive apply ordering.
func scanRefs(raw []byte) []string {
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if from, ok := t["from"].(map[string]any); ok {
				if name, ok := from["name"].(string); ok && name != "" {
					seen[name] = true
				}
			}
			for _, vv := range t {
				walk(vv)
			}
		case []any:
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	walk(m)
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	return out
}

// orderDocs returns docs with referenced resources before their referrers
// (topological by "from.name" edges). Kubernetes Secrets are placed first so
// SecretKeyReferences resolve. Ties keep original file/document order.
func orderDocs(docs []manifestDoc) []manifestDoc {
	byName := map[string]manifestDoc{}
	for _, d := range docs {
		byName[d.name] = d
	}

	visited := map[int]bool{}
	var out []manifestDoc
	var visit func(d manifestDoc, stack map[int]bool)
	visit = func(d manifestDoc, stack map[int]bool) {
		if visited[d.order] || stack[d.order] {
			return
		}
		stack[d.order] = true
		// resolve dependencies first
		deps := append([]string(nil), d.refs...)
		sort.Strings(deps)
		for _, depName := range deps {
			if dep, ok := byName[depName]; ok {
				visit(dep, stack)
			}
		}
		delete(stack, d.order)
		visited[d.order] = true
		out = append(out, d)
	}

	// Secrets first (stable), then everything else in topological order.
	ordered := append([]manifestDoc(nil), docs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].isSecret != ordered[j].isSecret {
			return ordered[i].isSecret
		}
		return ordered[i].order < ordered[j].order
	})
	for _, d := range ordered {
		visit(d, map[int]bool{})
	}
	return out
}

// splitDocuments splits multi-document YAML on lines that are exactly "---".
func splitDocuments(data []byte) [][]byte {
	var docs [][]byte
	var cur []string
	flush := func() {
		text := strings.Join(cur, "\n")
		if nonEmptyYAML(text) {
			docs = append(docs, []byte(text))
		}
		cur = nil
	}
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.TrimRight(ln, " \t\r") == "---" {
			flush()
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	return docs
}

func nonEmptyYAML(s string) bool {
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		return true
	}
	return false
}

// docKind peeks at a single document's apiVersion and kind.
func docKind(doc []byte) (apiVersion, kind string, err error) {
	var head struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := yaml.Unmarshal(doc, &head); err != nil {
		return "", "", fmt.Errorf("parsing manifest document: %w", err)
	}
	return head.APIVersion, head.Kind, nil
}
