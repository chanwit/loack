// Package provider defines the boundary between loack's core (directory model,
// state, plan orchestration) and the per-resource AWS reconcile engine. A
// Provider reconciles ACK resources for one or more API groups; it may run
// in-process (Local) or as a separate binary spoken to over a pipe (Remote).
//
// The protocol types here are deliberately ACK-free and JSON-serializable so the
// core can talk to a provider across a process boundary without linking any
// controller, AWS SDK, or Kubernetes code.
package provider

import (
	"context"
	"encoding/json"
)

// GVK identifies a resource kind.
type GVK struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// APIVersion returns "group/version" (or just version for core types).
func (g GVK) APIVersion() string {
	if g.Group == "" {
		return g.Version
	}
	return g.Group + "/" + g.Version
}

// Options is the AWS targeting for one reconcile, including the resolved secret
// store (name/key -> value) so the provider needs no cluster.
type Options struct {
	Region    string            `json:"region,omitempty"`
	Account   string            `json:"account,omitempty"`
	RoleARN   string            `json:"roleARN,omitempty"`
	Partition string            `json:"partition,omitempty"`
	Secrets   map[string]string `json:"secrets,omitempty"`
}

// FieldChange is one field-level difference in a plan.
type FieldChange struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// Op identifiers.
const (
	OpCapabilities = "capabilities"
	OpPlan         = "plan"
	OpApply        = "apply"
	OpDelete       = "delete"
	OpRead         = "read"
)

// Action values in responses.
const (
	ActCreate    = "create"
	ActUpdate    = "update"
	ActNoop      = "noop"
	ActCreated   = "created"
	ActUpdated   = "updated"
	ActUnchanged = "unchanged"
	ActDeleted   = "deleted"
	ActAbsent    = "absent"
	ActObserved  = "observed"
)

// Request is one operation on one resource.
type Request struct {
	Op      string                     `json:"op"`
	Object  json.RawMessage            `json:"object,omitempty"`
	Options Options                    `json:"options,omitempty"`
	// Refs is a snapshot of recorded objects (keyed by Address) used to resolve
	// "...Ref" fields. The core supplies it so the provider needs no callback.
	Refs map[string]json.RawMessage `json:"refs,omitempty"`
}

// Response is the result of an operation.
type Response struct {
	Action       string          `json:"action,omitempty"`
	Object       json.RawMessage `json:"object,omitempty"`
	Changes      []FieldChange   `json:"changes,omitempty"`
	ARN          string          `json:"arn,omitempty"`
	Account      string          `json:"account,omitempty"`
	Region       string          `json:"region,omitempty"`
	NotConverged bool            `json:"notConverged,omitempty"`
	GVKs         []GVK           `json:"gvks,omitempty"`
	Err          string          `json:"error,omitempty"`
}

// EventKind mirrors the engine's progress phases.
type EventKind int

const (
	EventRefreshing EventKind = iota
	EventCreating
	EventCreated
	EventModifying
	EventModified
	EventDestroying
	EventDestroyed
)

// Event is a progress signal streamed during apply/delete.
type Event struct {
	Kind    EventKind `json:"kind"`
	Address string    `json:"address"`
	ID      string    `json:"id,omitempty"`
}

// Hook receives progress events. nil is ignored.
type Hook func(Event)

// Provider reconciles resources. Call performs one operation; hook (may be nil)
// receives progress for apply/delete.
type Provider interface {
	Call(ctx context.Context, req Request, hook Hook) (Response, error)
	Close() error
}

// Address is the reference/state key: "<apiVersion>/<kind>/<namespace>/<name>",
// with an empty namespace normalized to "default".
func Address(apiVersion, kind, namespace, name string) string {
	if namespace == "" {
		namespace = "default"
	}
	return apiVersion + "/" + kind + "/" + namespace + "/" + name
}
