// Command loack-provider-kms is a loack provider for the kms controller,
// built as its OWN Go module. It links only the kms controller's generated
// code plus the shared loack engine + protocol, and pins its own runtime
// (v0.59.1) independently of the core and every other provider.
//
// The core dispatches kms resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	kmsapis "github.com/aws-controllers-k8s/kms-controller/apis/v1alpha1"
	kmsresource "github.com/aws-controllers-k8s/kms-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/kms-controller/pkg/resource/key"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(kmsresource.GetManagerFactories)
	provisioner.RegisterScheme(kmsapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-kms:", err)
		os.Exit(1)
	}
}
