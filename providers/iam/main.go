// Command loack-provider-iam is a loack provider for the iam controller,
// built as its OWN Go module. It links only the iam controller's generated
// code plus the shared loack engine + protocol, and pins its own runtime
// (v0.59.1) independently of the core and every other provider.
//
// The core dispatches iam resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	iamapis "github.com/aws-controllers-k8s/iam-controller/apis/v1alpha1"
	iamresource "github.com/aws-controllers-k8s/iam-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/iam-controller/pkg/resource/group"
	_ "github.com/aws-controllers-k8s/iam-controller/pkg/resource/policy"
	_ "github.com/aws-controllers-k8s/iam-controller/pkg/resource/role"
	_ "github.com/aws-controllers-k8s/iam-controller/pkg/resource/user"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(iamresource.GetManagerFactories)
	provisioner.RegisterScheme(iamapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-iam:", err)
		os.Exit(1)
	}
}
