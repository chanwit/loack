package provisioner

import (
	"context"
	"encoding/json"
	"fmt"

	"loack/provider"
)

// localProvider is an in-process provider.Provider backed by the controllers
// linked into this binary. It lives in the engine package (not the protocol
// package) so that a binary importing only loack/provider links no controllers.
type localProvider struct{}

// NewLocal returns an in-process provider over whatever controllers are
// registered (see Register) in this binary.
func NewLocal() provider.Provider { return &localProvider{} }

func (*localProvider) Close() error { return nil }

func (l *localProvider) Call(ctx context.Context, req provider.Request, hook provider.Hook) (provider.Response, error) {
	switch req.Op {
	case provider.OpCapabilities:
		return l.capabilities()
	case provider.OpPlan:
		return l.plan(ctx, req)
	case provider.OpApply:
		return l.apply(ctx, req, hook)
	case provider.OpDelete:
		return l.delete(ctx, req, hook)
	case provider.OpRead:
		return l.read(ctx, req)
	default:
		return provider.Response{}, fmt.Errorf("unknown op %q", req.Op)
	}
}

func (l *localProvider) capabilities() (provider.Response, error) {
	var gvks []provider.GVK
	for _, g := range RegisteredGVKs() {
		gvks = append(gvks, provider.GVK{Group: g.Group, Version: g.Version, Kind: g.Kind})
	}
	return provider.Response{GVKs: gvks}, nil
}

func (l *localProvider) plan(ctx context.Context, req provider.Request) (provider.Response, error) {
	target, rm, err := l.bind(ctx, req)
	if err != nil {
		return provider.Response{}, err
	}
	p, err := target.Plan(ctx, rm)
	if err != nil {
		return provider.Response{}, err
	}
	resp := provider.Response{}
	switch p.Kind {
	case PlanNoop:
		resp.Action = provider.ActNoop
	case PlanCreate:
		resp.Action = provider.ActCreate
	default:
		resp.Action = provider.ActUpdate
		for _, c := range p.Changes {
			resp.Changes = append(resp.Changes, provider.FieldChange{Path: c.Path, Old: c.Old, New: c.New})
		}
	}
	return resp, nil
}

func (l *localProvider) apply(ctx context.Context, req provider.Request, hook provider.Hook) (provider.Response, error) {
	target, rm, err := l.bind(ctx, req)
	if err != nil {
		return provider.Response{}, err
	}
	resolved := optsFrom(req.Options)
	result, aerr := target.Apply(ctx, rm, engineHook(hook))
	if aerr != nil && !ErrStillReconciling(aerr) {
		return provider.Response{}, aerr
	}
	resp := resultResponse(result.Resource, resolved)
	resp.NotConverged = ErrStillReconciling(aerr)
	switch result.Action {
	case ActionCreated:
		resp.Action = provider.ActCreated
	case ActionUpdated:
		resp.Action = provider.ActUpdated
	default:
		resp.Action = provider.ActUnchanged
	}
	return resp, nil
}

func (l *localProvider) delete(ctx context.Context, req provider.Request, hook provider.Hook) (provider.Response, error) {
	target, rm, err := l.bind(ctx, req)
	if err != nil {
		return provider.Response{}, err
	}
	result, derr := target.Delete(ctx, rm, engineHook(hook))
	if derr != nil && !ErrStillReconciling(derr) {
		return provider.Response{}, derr
	}
	resp := provider.Response{NotConverged: ErrStillReconciling(derr)}
	if result.Action == ActionDeleted {
		resp.Action = provider.ActDeleted
	} else {
		resp.Action = provider.ActAbsent
	}
	return resp, nil
}

func (l *localProvider) read(ctx context.Context, req provider.Request) (provider.Response, error) {
	target, rm, err := l.bind(ctx, req)
	if err != nil {
		return provider.Response{}, err
	}
	result, gerr := target.Get(ctx, rm)
	if gerr != nil {
		return provider.Response{}, gerr
	}
	if result.Action == ActionAbsent {
		return provider.Response{Action: provider.ActAbsent}, nil
	}
	resp := resultResponse(result.Resource, optsFrom(req.Options))
	resp.Action = provider.ActObserved
	return resp, nil
}

// bind loads the target, builds its manager from the request options, and
// resolves references from the supplied snapshot.
func (l *localProvider) bind(ctx context.Context, req provider.Request) (*Target, AWSResourceManager, error) {
	target, err := LoadBytes(req.Object)
	if err != nil {
		return nil, nil, err
	}
	rm, _, err := target.Manager(ctx, optsFrom(req.Options))
	if err != nil {
		return nil, nil, err
	}
	if err := target.ResolveReferences(ctx, rm, refLookup(req.Refs)); err != nil {
		return nil, nil, err
	}
	return target, rm, nil
}

func optsFrom(o provider.Options) Options {
	return Options{
		Region:    o.Region,
		AccountID: o.Account,
		RoleARN:   o.RoleARN,
		Partition: o.Partition,
		Secrets:   SecretStore(o.Secrets),
	}
}

func refLookup(refs map[string]json.RawMessage) RefLookup {
	return func(apiVersion, kind, namespace, name string) ([]byte, bool) {
		obj, ok := refs[provider.Address(apiVersion, kind, namespace, name)]
		if !ok || len(obj) == 0 {
			return nil, false
		}
		return obj, true
	}
}

func resultResponse(r AWSResource, resolved Options) provider.Response {
	resp := provider.Response{}
	if obj, err := ObjectJSON(r); err == nil {
		resp.Object = obj
	}
	arn, account, region := Metadata(r)
	if account == "" {
		account = resolved.AccountID
	}
	if region == "" {
		region = resolved.Region
	}
	resp.ARN, resp.Account, resp.Region = arn, account, region
	return resp
}

// engineHook adapts a protocol Hook to the engine's hook.
func engineHook(h provider.Hook) Hook {
	if h == nil {
		return nil
	}
	return func(e Event) {
		h(provider.Event{Kind: provider.EventKind(e.Kind), Address: e.Address, ID: e.ID})
	}
}
