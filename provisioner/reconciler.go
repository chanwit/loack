package provisioner

import (
	"context"
	"errors"
	"fmt"

	ctrlreconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"

	ackv1alpha1 "github.com/aws-controllers-k8s/runtime/apis/core/v1alpha1"
)

// SecretStore resolves ACK SecretKeyReferences to literal values without a
// Kubernetes cluster. Values are keyed by "<namespace>/<name>/<key>" and, as a
// namespace-agnostic fallback, by "<name>/<key>".
type SecretStore map[string]string

// Get returns the value for a (namespace, name, key) reference, trying the
// namespaced key first then the namespace-agnostic one.
func (s SecretStore) Get(namespace, name, key string) (string, bool) {
	if s == nil {
		return "", false
	}
	if v, ok := s[namespace+"/"+name+"/"+key]; ok {
		return v, true
	}
	v, ok := s[name+"/"+key]
	return v, ok
}

// offlineReconciler is a clusterless stand-in for the ACK Reconciler. It serves
// Secret values from a loack-provided SecretStore instead of reading Kubernetes
// Secrets. Writing back to Secrets is unsupported.
type offlineReconciler struct {
	secrets SecretStore
}

func (offlineReconciler) Reconcile(context.Context, ctrlreconcile.Request) (ctrlreconcile.Result, error) {
	return ctrlreconcile.Result{}, nil
}

func (r offlineReconciler) SecretValueFromReference(_ context.Context, ref *ackv1alpha1.SecretKeyReference) (string, error) {
	if ref == nil {
		return "", nil
	}
	if v, ok := r.secrets.Get(ref.Namespace, ref.Name, ref.Key); ok {
		return v, nil
	}
	return "", fmt.Errorf(
		"secret %s/%s key %q not found in loack state; apply a Kubernetes Secret with that name/key first",
		ref.Namespace, ref.Name, ref.Key,
	)
}

func (offlineReconciler) WriteToSecret(context.Context, string, string, string, string) error {
	return errors.New("loack: writing to Secrets is not supported in offline mode")
}
