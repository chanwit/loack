// Command loack-provider-ecr is a loack provider for the ecr controller,
// built as its OWN Go module. It links only the ecr controller's generated
// code plus the shared loack engine + protocol, and pins its own runtime
// (v0.59.1) independently of the core and every other provider.
//
// The core dispatches ecr resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	ecrapis "github.com/aws-controllers-k8s/ecr-controller/apis/v1alpha1"
	ecrresource "github.com/aws-controllers-k8s/ecr-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/ecr-controller/pkg/resource/repository"
	iamapis "github.com/aws-controllers-k8s/iam-controller/apis/v1alpha1"
	smapis "github.com/aws-controllers-k8s/secretsmanager-controller/apis/v1alpha1"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(ecrresource.GetManagerFactories)
	provisioner.RegisterScheme(ecrapis.AddToScheme)
	// ecr resources reference iam Roles and secretsmanager Secrets (pull-through
	// cache / replication credentials); resolving them needs those apis in
	// loack's ref scheme (see the split-provider note in eks).
	provisioner.RegisterScheme(iamapis.AddToScheme)
	provisioner.RegisterScheme(smapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-ecr:", err)
		os.Exit(1)
	}
}
