// Command loack-provider-cloudwatchlogs is a loack provider for the cloudwatchlogs controller,
// built as its OWN Go module. It links only the cloudwatchlogs controller's generated
// code plus the shared loack engine + protocol, and pins its own runtime
// (v0.59.1) independently of the core and every other provider.
//
// The core dispatches cloudwatchlogs resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	cwlapis "github.com/aws-controllers-k8s/cloudwatchlogs-controller/apis/v1alpha1"
	cwlresource "github.com/aws-controllers-k8s/cloudwatchlogs-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/cloudwatchlogs-controller/pkg/resource/log_group"
	kmsapis "github.com/aws-controllers-k8s/kms-controller/apis/v1alpha1"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(cwlresource.GetManagerFactories)
	provisioner.RegisterScheme(cwlapis.AddToScheme)
	// A LogGroup's kmsKeyRef -> a kms Key; resolving it needs the kms apis in
	// loack's ref scheme (see the split-provider note in eks).
	provisioner.RegisterScheme(kmsapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-cloudwatchlogs:", err)
		os.Exit(1)
	}
}
