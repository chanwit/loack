// Command loack-provider-dynamodb is a loack provider for the dynamodb controller,
// built as its OWN Go module. It links only the dynamodb controller's generated
// code plus the shared loack engine + protocol, and pins its own runtime
// (v0.59.1) independently of the core and every other provider.
//
// The core dispatches dynamodb resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	ddbapis "github.com/aws-controllers-k8s/dynamodb-controller/apis/v1alpha1"
	ddbresource "github.com/aws-controllers-k8s/dynamodb-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/dynamodb-controller/pkg/resource/backup"
	_ "github.com/aws-controllers-k8s/dynamodb-controller/pkg/resource/global_table"
	_ "github.com/aws-controllers-k8s/dynamodb-controller/pkg/resource/table"
	kmsapis "github.com/aws-controllers-k8s/kms-controller/apis/v1alpha1"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(ddbresource.GetManagerFactories)
	provisioner.RegisterScheme(ddbapis.AddToScheme)
	// A Table's SSE config carries a kmsKeyRef -> a kms Key; resolving it needs
	// the kms apis in loack's ref scheme (see the split-provider note in eks).
	provisioner.RegisterScheme(kmsapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-dynamodb:", err)
		os.Exit(1)
	}
}
