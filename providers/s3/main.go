// Command loack-provider-s3 is a loack provider for the s3 controller, built as
// its OWN Go module. It links one controller's generated code plus the shared
// loack engine and protocol (via a replace to the loack module). Because it is a
// separate module, it pins its own runtime / aws-sdk versions independently of
// the core and of every other provider -- which is what dissolves loack's
// single-runtime constraint.
//
// The core dispatches s3 resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	s3apis "github.com/aws-controllers-k8s/s3-controller/apis/v1alpha1"
	s3resource "github.com/aws-controllers-k8s/s3-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/s3-controller/pkg/resource/bucket"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(s3resource.GetManagerFactories)
	provisioner.RegisterScheme(s3apis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-s3:", err)
		os.Exit(1)
	}
}
