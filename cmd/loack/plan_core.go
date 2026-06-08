package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"sigs.k8s.io/yaml"

	"loack/provider"
	"loack/internal/state"
)

// actionKind is what a planned change will do to a resource.
type actionKind string

const (
	aCreate       actionKind = "create"
	aUpdate       actionKind = "update"
	aDestroy      actionKind = "destroy"
	aSecretCreate actionKind = "secret-create"
	aSecretUpdate actionKind = "secret-update"
	aSecretDelete actionKind = "secret-delete"
)

// change is a single planned action. It carries enough to both render the plan
// and execute it (or be saved to a plan file and executed later).
type change struct {
	Address     string          `json:"address"`
	Action      actionKind      `json:"action"`
	APIVersion  string          `json:"apiVersion"`
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Namespace   string          `json:"namespace,omitempty"`
	Doc         []byte          `json:"doc,omitempty"`         // create/update/secret
	StateObject json.RawMessage `json:"stateObject,omitempty"` // destroy
	AppliedAt   time.Time       `json:"appliedAt,omitempty"`   // destroy ordering

	// Resolved AWS targeting for this resource (per its namespace's CARM, or
	// from state for destroys). Persisted in saved plans so apply targets the
	// same account/region.
	Region  string `json:"region,omitempty"`
	Account string `json:"account,omitempty"`
	RoleARN string `json:"roleARN,omitempty"`

	// display-only, not persisted
	desiredYAML  []byte                `json:"-"`
	fieldChanges []provider.FieldChange `json:"-"`
	secretKeys   []string              `json:"-"`
}

func (c change) display() string {
	if c.isSecret() {
		return secretDisplay(c.Name)
	}
	return c.Kind + "." + c.Name
}

func (c change) isSecret() bool {
	return c.Action == aSecretCreate || c.Action == aSecretUpdate || c.Action == aSecretDelete
}

func (c change) isDestroy() bool { return c.Action == aDestroy || c.Action == aSecretDelete }

// computePlan diffs the working directory (the configuration) against state.
// Resources in the config are created/updated; resources in state but absent
// from the config are destroyed. With destroyAll, the config is ignored and
// everything in state is planned for destruction.
func computePlan(ctx context.Context, destroyAll bool) ([]change, error) {
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return nil, err
	}
	disp, err := providers()
	if err != nil {
		return nil, err
	}
	baseOpts, err := effectiveOptions()
	if err != nil {
		return nil, err
	}

	var changes []change
	desired := map[string]bool{}

	if !destroyAll {
		docs, carm, err := gatherDocs()
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			desired[d.address()] = true
			c, err := planDoc(ctx, d, st, baseOpts, carm, disp)
			if err != nil {
				return nil, err
			}
			if c != nil {
				changes = append(changes, *c)
			}
		}
	}

	for _, r := range st.List() {
		if desired[r.Address] {
			continue
		}
		if isK8sSecret(r.APIVersion, r.Kind) {
			changes = append(changes, change{
				Address: r.Address, Action: aSecretDelete,
				APIVersion: r.APIVersion, Kind: r.Kind, Name: r.Name, Namespace: r.Namespace,
				AppliedAt: r.AppliedAt,
			})
			continue
		}
		tgt, terr := targetOptions(baseOpts, r.Account, r.Region)
		if terr != nil {
			return nil, terr
		}
		changes = append(changes, change{
			Address: r.Address, Action: aDestroy,
			APIVersion: r.APIVersion, Kind: r.Kind, Name: r.Name, Namespace: r.Namespace,
			StateObject: r.Object, AppliedAt: r.AppliedAt,
			Region: tgt.Region, Account: tgt.Account, RoleARN: tgt.RoleARN,
		})
	}
	return changes, nil
}

// planDoc plans a single config document. Returns nil for a no-op.
func planDoc(ctx context.Context, d manifestDoc, st *state.File, baseOpts provider.Options, carm map[string]nsTarget, disp *dispatcher) (*change, error) {
	ch := change{
		Address: d.address(), APIVersion: d.apiVersion, Kind: d.kind,
		Name: d.name, Namespace: d.namespace, Doc: d.raw,
	}

	if d.isSecret {
		_, _, values, obj, err := normalizeSecret(d.raw)
		if err != nil {
			return nil, err
		}
		ch.secretKeys = sortedKeys(values)
		existing, ok := st.Get(d.address())
		if !ok {
			ch.Action = aSecretCreate
			return &ch, nil
		}
		if bytes.Equal(canonicalJSON(existing.Object), canonicalJSON(obj)) {
			return nil, nil
		}
		ch.Action = aSecretUpdate
		return &ch, nil
	}

	// Fail early if no provider handles this group (launch-free), so a missing
	// provider surfaces at plan time rather than mid-apply.
	if err := disp.preflight(d.apiVersion); err != nil {
		return nil, err
	}

	// Resolve this resource's AWS targeting from its namespace's CARM (if any).
	t := carm[namespaceOrDefault(d.namespace)]
	tgt, err := targetOptions(baseOpts, t.account, t.region)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ch.display(), err)
	}
	ch.Region, ch.Account, ch.RoleARN = tgt.Region, tgt.Account, tgt.RoleARN

	// A resource not yet in state is a create -- no AWS read needed, and no
	// reference resolution (its refs may target resources created in this plan).
	if _, inState := st.Get(d.address()); !inState {
		ch.Action = aCreate
		ch.desiredYAML = stripStatus(d.raw)
		return &ch, nil
	}

	// In state: refresh and diff. Merge the manifest spec onto the recorded
	// status so the read carries the server-assigned identifiers.
	existing, _ := st.Get(d.address())
	merged, err := mergedDesired(d.raw, existing.Object)
	if err != nil {
		return nil, err
	}
	prov, err := disp.For(d.apiVersion)
	if err != nil {
		return nil, err
	}
	refs, err := stateRefs()
	if err != nil {
		return nil, err
	}
	resp, err := prov.Call(ctx, provider.Request{
		Op: provider.OpPlan, Object: merged, Options: tgt, Refs: refs,
	}, nil)
	if err != nil {
		return nil, err
	}
	switch resp.Action {
	case provider.ActNoop:
		return nil, nil
	case provider.ActCreate: // in state but gone from AWS -> recreate
		ch.Action = aCreate
		ch.desiredYAML = stripStatus(d.raw)
		return &ch, nil
	default:
		ch.Action = aUpdate
		ch.fieldChanges = resp.Changes
		return &ch, nil
	}
}

// namespaceOrDefault normalizes an empty namespace to "default".
func namespaceOrDefault(ns string) string {
	if ns == "" {
		return "default"
	}
	return ns
}

// optionsForChange builds the AWS Options for executing a change: the resource's
// resolved targeting (region/account/role) plus the state-backed secret store.
func optionsForChange(c change) (provider.Options, error) {
	secrets, err := secretStoreFromState()
	if err != nil {
		return provider.Options{}, err
	}
	part := rootArgs.partition
	if part == "" {
		part = "aws"
	}
	return provider.Options{
		Region:    c.Region,
		Account:   c.Account,
		RoleARN:   c.RoleARN,
		Partition: part,
		Secrets:   secrets,
	}, nil
}

func planCounts(changes []change) (add, chg, del int) {
	for _, c := range changes {
		switch c.Action {
		case aCreate, aSecretCreate:
			add++
		case aUpdate, aSecretUpdate:
			chg++
		case aDestroy, aSecretDelete:
			del++
		}
	}
	return add, chg, del
}

// executePlan carries out the change set: mutations in dependency order, then
// destroys in reverse-application order. Returns the realized counts.
func executePlan(ctx context.Context, changes []change) (add, chg, del int, err error) {
	var muts, dels []change
	for _, c := range changes {
		if c.isDestroy() {
			dels = append(dels, c)
		} else {
			muts = append(muts, c)
		}
	}
	sort.SliceStable(dels, func(i, j int) bool { return dels[i].AppliedAt.After(dels[j].AppliedAt) })

	obs := newObserver()
	for _, c := range muts {
		a, ch, e := executeMutation(ctx, c, obs)
		if e != nil {
			return add, chg, del, e
		}
		add += a
		chg += ch
	}
	for _, c := range dels {
		d, e := executeDestroy(ctx, c, obs)
		if e != nil {
			return add, chg, del, e
		}
		del += d
	}
	return add, chg, del, nil
}

func executeMutation(ctx context.Context, c change, obs *observer) (add, chg int, err error) {
	if c.isSecret() {
		return applyLocalSecretDoc(c.Doc)
	}

	// For an existing resource, merge the manifest spec onto the recorded status
	// so the apply's reads carry the server-assigned identifiers.
	doc := c.Doc
	if st, lerr := state.Load(rootArgs.state); lerr == nil {
		if existing, ok := st.Get(c.Address); ok && len(existing.Object) > 0 {
			if m, merr := mergedDesired(c.Doc, existing.Object); merr == nil {
				doc = m
			}
		}
	}
	objJSON, err := yaml.YAMLToJSON(doc)
	if err != nil {
		return 0, 0, err
	}
	prov, err := providerFor(c.APIVersion)
	if err != nil {
		return 0, 0, err
	}
	opts, err := optionsForChange(c)
	if err != nil {
		return 0, 0, err
	}
	refs, err := stateRefs()
	if err != nil {
		return 0, 0, err
	}
	resp, err := prov.Call(ctx, provider.Request{
		Op: provider.OpApply, Object: objJSON, Options: opts, Refs: refs,
	}, obs.hook())
	if err != nil {
		return 0, 0, err
	}
	if serr := recordResource(c.APIVersion, c.Kind, c.Namespace, c.Name, resp.Region, resp.Account, resp.ARN, resp.Object); serr != nil {
		errorf("could not write state: %v", serr)
	}
	if resp.NotConverged {
		outf("%s %s is provisioned but not yet fully converged; re-run apply.",
			yellow("Warning:"), c.display())
	}
	switch resp.Action {
	case provider.ActCreated:
		add = 1
	case provider.ActUpdated:
		chg = 1
	}
	return add, chg, nil
}

func executeDestroy(ctx context.Context, c change, obs *observer) (int, error) {
	if c.Action == aSecretDelete {
		st, err := state.Load(rootArgs.state)
		if err != nil {
			return 0, err
		}
		st.Remove(c.Address)
		if err := st.Save(); err != nil {
			return 0, err
		}
		outf("%s: Removed from state", c.display())
		return 1, nil
	}

	prov, err := providerFor(c.APIVersion)
	if err != nil {
		return 0, err
	}
	opts, err := optionsForChange(c)
	if err != nil {
		return 0, err
	}
	resp, err := prov.Call(ctx, provider.Request{
		Op: provider.OpDelete, Object: c.StateObject, Options: opts,
	}, obs.hook())
	if err != nil {
		return 0, err
	}
	if serr := forgetResource(c.APIVersion, c.Kind, c.Namespace, c.Name); serr != nil {
		errorf("could not update state: %v", serr)
	}
	if resp.Action == provider.ActDeleted {
		return 1, nil
	}
	return 0, nil
}

// providerFor looks up the provider for an apiVersion via the dispatcher.
func providerFor(apiVersion string) (provider.Provider, error) {
	disp, err := providers()
	if err != nil {
		return nil, err
	}
	return disp.For(apiVersion)
}

// --- helpers ---

// mergedDesired produces a CR whose spec/metadata come from the manifest but
// whose status (the server-assigned identifiers) comes from the recorded state
// object. If there is no state object, the manifest (as JSON) is returned.
func mergedDesired(manifestRaw []byte, stateObj []byte) ([]byte, error) {
	mj, err := yaml.YAMLToJSON(manifestRaw)
	if err != nil {
		return nil, err
	}
	if len(stateObj) == 0 {
		return mj, nil
	}
	var mm, sm map[string]any
	if err := json.Unmarshal(mj, &mm); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(stateObj, &sm); err != nil {
		return mj, nil
	}
	out := map[string]any{}
	for k, v := range sm {
		out[k] = v
	}
	out["apiVersion"] = mm["apiVersion"]
	out["kind"] = mm["kind"]
	out["metadata"] = mm["metadata"]
	out["spec"] = mm["spec"]
	return json.Marshal(out)
}

func stripStatus(data []byte) []byte {
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return data
	}
	delete(m, "status")
	if out, err := yaml.Marshal(m); err == nil {
		return out
	}
	return data
}

// canonicalJSON re-encodes JSON with sorted keys for stable comparison.
func canonicalJSON(raw []byte) []byte {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}
