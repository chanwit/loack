// Command loack-provider-sqs is a loack provider for the sqs controller,
// built as its OWN Go module. It links only the sqs controller's generated
// code plus the shared loack engine + protocol, and pins its own runtime
// (v0.59.1) independently of the core and every other provider.
//
// The core dispatches sqs resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	sqsapis "github.com/aws-controllers-k8s/sqs-controller/apis/v1alpha1"
	sqsresource "github.com/aws-controllers-k8s/sqs-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/sqs-controller/pkg/resource/queue"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(sqsresource.GetManagerFactories)
	provisioner.RegisterScheme(sqsapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-sqs:", err)
		os.Exit(1)
	}
}
