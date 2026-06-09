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

	ec2apis "github.com/aws-controllers-k8s/ec2-controller/apis/v1alpha1"
	eksapis "github.com/aws-controllers-k8s/eks-controller/apis/v1alpha1"
	eksresource "github.com/aws-controllers-k8s/eks-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/eks-controller/pkg/resource/cluster"
	_ "github.com/aws-controllers-k8s/eks-controller/pkg/resource/nodegroup"
	iamapis "github.com/aws-controllers-k8s/iam-controller/apis/v1alpha1"
	kmsapis "github.com/aws-controllers-k8s/kms-controller/apis/v1alpha1"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(eksresource.GetManagerFactories)
	// The eks Cluster/Nodegroup carry cross-service references — subnetRefs to
	// ec2 Subnets, roleRef/nodeRoleRef to iam Roles, and the cluster's
	// encryptionConfig keyRef to a kms Key. Resolving them needs those foreign
	// types in loack's ref scheme, not just the eks apis. (In the all-in-one
	// binary every controller's apis are registered, which is why this only
	// surfaces in the split provider.)
	provisioner.RegisterScheme(eksapis.AddToScheme)
	provisioner.RegisterScheme(ec2apis.AddToScheme)
	provisioner.RegisterScheme(iamapis.AddToScheme)
	provisioner.RegisterScheme(kmsapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-eks:", err)
		os.Exit(1)
	}
}
