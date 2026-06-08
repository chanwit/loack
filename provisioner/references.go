package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ackv1alpha1 "github.com/aws-controllers-k8s/runtime/apis/core/v1alpha1"
	acktypes "github.com/aws-controllers-k8s/runtime/pkg/types"
)

// RefLookup returns the stored object JSON for a referenced resource, addressed
// by apiVersion, kind, namespace, and name. ACK resources are namespace-scoped,
// so a reference resolves within a namespace. ok is false when not tracked.
type RefLookup func(apiVersion, kind, namespace, name string) ([]byte, bool)

// ResolveReferences resolves any "...Ref" fields on the desired resource (e.g.
// an eks Cluster's roleRef -> an iam Role) by reading the referenced resources
// from loack state via the supplied lookup. It mutates the desired resource in
// place, populating the corresponding literal fields (roleARN, subnetIDs, ...).
// With no references set this is a no-op.
func (t *Target) ResolveReferences(ctx context.Context, rm acktypes.AWSResourceManager, lookup RefLookup) error {
	reader := stateReader{lookup: lookup}
	if _, _, err := rm.ResolveReferences(ctx, reader, t.desired); err != nil {
		return fmt.Errorf("resolving references: %w", err)
	}
	return nil
}

// stateReader is a controller-runtime client.Reader backed by loack state. It
// serves the referenced CRs that ACK's generated ResolveReferences asks for.
type stateReader struct {
	lookup RefLookup
}

func (r stateReader) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	gvks, _, err := refScheme().ObjectKinds(obj)
	if err != nil || len(gvks) == 0 {
		return fmt.Errorf("loack: unknown type for reference %q: %w", key.Name, err)
	}
	gvk := gvks[0]
	gr := schema.GroupResource{Group: gvk.Group, Resource: gvk.Kind}

	if r.lookup == nil {
		return apierrors.NewNotFound(gr, key.Name)
	}
	// key.Namespace is the referrer's namespace (or an explicit one on the ref).
	raw, ok := r.lookup(gvk.GroupVersion().String(), gvk.Kind, key.Namespace, key.Name)
	if !ok {
		return apierrors.NewNotFound(gr, key.Name)
	}
	synced, err := markSynced(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(synced, obj)
}

func (stateReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("loack: List is not supported by the state-backed reader")
}

// markSynced injects an ACK.ResourceSynced=True condition into a stored object
// so that ACK's reference checks accept it. In loack's model, a resource present
// in state has been applied successfully, i.e. it is synced; its ARN is already
// recorded in status.ackResourceMetadata.
func markSynced(raw []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	status, _ := m["status"].(map[string]any)
	if status == nil {
		status = map[string]any{}
		m["status"] = status
	}
	status["conditions"] = []any{
		map[string]any{
			"type":   string(ackv1alpha1.ConditionTypeResourceSynced),
			"status": "True",
		},
	}
	return json.Marshal(m)
}
