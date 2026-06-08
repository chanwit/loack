package provisioner

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
)

// schemeAdders are the AddToScheme funcs of every controller's apis a binary has
// registered. The scheme lets the state-backed reader map a referenced object
// (e.g. *iam.Role) to its GroupVersionKind. A binary registers the apis of the
// types it owns or references via RegisterScheme (see internal/allcontrollers).
var schemeAdders []func(*runtime.Scheme) error

// RegisterScheme adds an apis package's AddToScheme so referenced types of that
// group can be resolved.
func RegisterScheme(add func(*runtime.Scheme) error) {
	schemeAdders = append(schemeAdders, add)
}

var (
	builtScheme *runtime.Scheme
	schemeOnce  sync.Once
)

// refScheme returns the lazily-built scheme of all registered apis.
func refScheme() *runtime.Scheme {
	schemeOnce.Do(func() {
		s := runtime.NewScheme()
		for _, add := range schemeAdders {
			_ = add(s)
		}
		builtScheme = s
	})
	return builtScheme
}
