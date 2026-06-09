// Command loack-provider-ec2 is a loack provider for the ec2 controller,
// built as its OWN Go module. It links only the ec2 controller's generated
// code plus the shared loack engine + protocol, and pins its own runtime
// (v0.59.1) independently of the core and every other provider.
//
// The core dispatches ec2 resources here over the provider protocol.
package main

import (
	"fmt"
	"os"

	ec2apis "github.com/aws-controllers-k8s/ec2-controller/apis/v1alpha1"
	ec2resource "github.com/aws-controllers-k8s/ec2-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/dhcp_options"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/egress_only_internet_gateway"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/elastic_ip_address"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/internet_gateway"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/nat_gateway"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/network_acl"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/route_table"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/security_group"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/subnet"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/transit_gateway"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/vpc"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/vpc_endpoint"
	_ "github.com/aws-controllers-k8s/ec2-controller/pkg/resource/vpc_peering_connection"
	iamapis "github.com/aws-controllers-k8s/iam-controller/apis/v1alpha1"

	"loack/provider"
	"loack/provisioner"
)

func main() {
	provisioner.Register(ec2resource.GetManagerFactories)
	provisioner.RegisterScheme(ec2apis.AddToScheme)
	// Some ec2 resources reference an iam Role (e.g. a VPC flow log's
	// deliverLogsPermissionArnRef); resolving it needs the iam apis in loack's
	// ref scheme (see the split-provider note in eks).
	provisioner.RegisterScheme(iamapis.AddToScheme)

	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider-ec2:", err)
		os.Exit(1)
	}
}
