// Command loack-provider-eks is a loack provider for the eks controller,
// built as its OWN Go module. It links only the eks controller's generated
// code plus the shared loack engine + protocol, and pins its own runtime
// (v0.59.1) independently of the core and every other provider.
//
// The core dispatches eks resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	eksapis "github.com/aws-controllers-k8s/eks-controller/apis/v1alpha1"
	eksresource "github.com/aws-controllers-k8s/eks-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/eks-controller/pkg/resource/cluster"
	_ "github.com/aws-controllers-k8s/eks-controller/pkg/resource/nodegroup"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(eksresource.GetManagerFactories)
	provisioner.RegisterScheme(eksapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-eks:", err)
		os.Exit(1)
	}
}
