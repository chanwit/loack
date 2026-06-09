// Command loack-provider-secretsmanager is a loack provider for the secretsmanager controller,
// built as its OWN Go module. It links only the secretsmanager controller's generated
// code plus the shared loack engine + protocol, and pins its own runtime
// (v0.59.1) independently of the core and every other provider.
//
// The core dispatches secretsmanager resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	kmsapis "github.com/aws-controllers-k8s/kms-controller/apis/v1alpha1"
	smapis "github.com/aws-controllers-k8s/secretsmanager-controller/apis/v1alpha1"
	smresource "github.com/aws-controllers-k8s/secretsmanager-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/secretsmanager-controller/pkg/resource/secret"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(smresource.GetManagerFactories)
	provisioner.RegisterScheme(smapis.AddToScheme)
	// A Secret's kmsKeyRef -> a kms Key; resolving it needs the kms apis in
	// loack's ref scheme (see the split-provider note in eks).
	provisioner.RegisterScheme(kmsapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-secretsmanager:", err)
		os.Exit(1)
	}
}
