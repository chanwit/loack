package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"sigs.k8s.io/yaml"

	"loack/internal/state"
)

// loack treats a native Kubernetes Secret (apiVersion: v1, kind: Secret) as a
// local secret store: applying one records it in loack state, and ACK
// SecretKeyReferences are resolved from those stored Secrets. The value never
// leaves the state file -- there is no cluster and nothing is sent to AWS.

func isK8sSecret(apiVersion, kind string) bool {
	return apiVersion == "v1" && kind == "Secret"
}

// isIgnoredKind reports whether a manifest is a Kubernetes resource that loack
// recognizes but does not provision: currently only Namespace, which is pure
// cluster-scoping with no AWS counterpart and which installer/kustomize tooling
// commonly co-renders. Unknown kinds that are NOT ignored still error, so a
// stray Deployment/ConfigMap is never silently dropped.
func isIgnoredKind(apiVersion, kind string) bool {
	return apiVersion == "v1" && kind == "Namespace"
}

type k8sSecret struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   metadata          `json:"metadata"`
	Type       string            `json:"type,omitempty"`
	Data       map[string][]byte `json:"data,omitempty"`       // base64 in JSON, decoded into bytes
	StringData map[string]string `json:"stringData,omitempty"` // plaintext
}

type metadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// parseK8sSecret extracts namespace, name, and flattened key/value pairs from a
// stored Secret object (JSON). stringData takes precedence over data.
func parseK8sSecret(obj json.RawMessage) (namespace, name string, values map[string]string, err error) {
	var s k8sSecret
	if err := json.Unmarshal(obj, &s); err != nil {
		return "", "", nil, err
	}
	values = map[string]string{}
	for k, v := range s.Data {
		values[k] = string(v)
	}
	for k, v := range s.StringData {
		values[k] = v
	}
	return s.Metadata.Namespace, s.Metadata.Name, values, nil
}

func secretAddress(namespace, name string) string {
	if namespace == "" {
		namespace = "default"
	}
	return "v1/Secret/" + namespace + "/" + name
}

func secretDisplay(name string) string { return "Secret." + name }

// applyLocalSecretDoc records a Kubernetes Secret in loack state, returning how
// many entries it added/changed. It prints progress but not the final summary.
func applyLocalSecretDoc(data []byte) (added, changed int, err error) {
	ns, name, values, obj, err := normalizeSecret(data)
	if err != nil {
		return 0, 0, err
	}
	if name == "" {
		return 0, 0, fmt.Errorf("secret manifest is missing metadata.name")
	}

	st, err := state.Load(rootArgs.state)
	if err != nil {
		return 0, 0, err
	}
	addr := secretAddress(ns, name)
	_, existed := st.Get(addr)

	outf("%s: Storing in loack state...", secretDisplay(name))
	st.Put(state.Resource{
		Address:    addr,
		APIVersion: "v1",
		Kind:       "Secret",
		Name:       name,
		Namespace:  ns,
		AppliedAt:  time.Now().UTC(),
		Object:     obj,
	})
	if err := st.Save(); err != nil {
		return 0, 0, err
	}
	outf("%s: Stored in state (%d key(s))", secretDisplay(name), len(values))

	if existed {
		return 0, 1, nil
	}
	return 1, 0, nil
}

// normalizeSecret parses a Secret manifest into its namespace, name, flattened
// values, and a canonical JSON object suitable for storage.
func normalizeSecret(data []byte) (namespace, name string, values map[string]string, obj json.RawMessage, err error) {
	jsonBytes, err := yaml.YAMLToJSON(data)
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("parsing secret: %w", err)
	}
	ns, nm, vals, err := parseK8sSecret(jsonBytes)
	if err != nil {
		return "", "", nil, nil, err
	}
	return ns, nm, vals, jsonBytes, nil
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
