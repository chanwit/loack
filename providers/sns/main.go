// Command loack-provider-sns is a loack provider for the sns controller,
// built as its OWN Go module. It links only the sns controller's generated
// code plus the shared loack engine + protocol, and pins its own runtime
// (v0.59.1) independently of the core and every other provider.
//
// The core dispatches sns resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	iamapis "github.com/aws-controllers-k8s/iam-controller/apis/v1alpha1"
	kmsapis "github.com/aws-controllers-k8s/kms-controller/apis/v1alpha1"
	snsapis "github.com/aws-controllers-k8s/sns-controller/apis/v1alpha1"
	snsresource "github.com/aws-controllers-k8s/sns-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/sns-controller/pkg/resource/topic"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(snsresource.GetManagerFactories)
	provisioner.RegisterScheme(snsapis.AddToScheme)
	// A Topic references iam Roles (delivery feedback) and a kms Key
	// (kmsMasterKeyRef); resolving them needs those apis in loack's ref scheme
	// (see the split-provider note in eks).
	provisioner.RegisterScheme(iamapis.AddToScheme)
	provisioner.RegisterScheme(kmsapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-sns:", err)
		os.Exit(1)
	}
}
