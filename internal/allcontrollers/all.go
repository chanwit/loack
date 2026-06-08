// Package allcontrollers registers every wired ACK controller with the
// provisioner. A binary that wants all controllers blank-imports this package;
// a per-service provider registers only its own controller in its main instead.
//
// This is the single place that links the controllers' generated code (and the
// only reason a binary importing it is large). Adding a controller: add its
// blank import + Register/RegisterScheme line here (and a controllers.lock pin).
package allcontrollers

import (
	"loack/provisioner"

	cwlapis "github.com/aws-controllers-k8s/cloudwatchlogs-controller/apis/v1alpha1"
	cwlresource "github.com/aws-controllers-k8s/cloudwatchlogs-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/cloudwatchlogs-controller/pkg/resource/log_group"
	ddbapis "github.com/aws-controllers-k8s/dynamodb-controller/apis/v1alpha1"
	ddbresource "github.com/aws-controllers-k8s/dynamodb-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/dynamodb-controller/pkg/resource/backup"
	_ "github.com/aws-controllers-k8s/dynamodb-controller/pkg/resource/global_table"
	_ "github.com/aws-controllers-k8s/dynamodb-controller/pkg/resource/table"
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
	ecrapis "github.com/aws-controllers-k8s/ecr-controller/apis/v1alpha1"
	ecrresource "github.com/aws-controllers-k8s/ecr-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/ecr-controller/pkg/resource/repository"
	eksapis "github.com/aws-controllers-k8s/eks-controller/apis/v1alpha1"
	eksresource "github.com/aws-controllers-k8s/eks-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/eks-controller/pkg/resource/cluster"
	_ "github.com/aws-controllers-k8s/eks-controller/pkg/resource/nodegroup"
	iamapis "github.com/aws-controllers-k8s/iam-controller/apis/v1alpha1"
	iamresource "github.com/aws-controllers-k8s/iam-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/iam-controller/pkg/resource/group"
	_ "github.com/aws-controllers-k8s/iam-controller/pkg/resource/policy"
	_ "github.com/aws-controllers-k8s/iam-controller/pkg/resource/role"
	_ "github.com/aws-controllers-k8s/iam-controller/pkg/resource/user"
	kmsapis "github.com/aws-controllers-k8s/kms-controller/apis/v1alpha1"
	kmsresource "github.com/aws-controllers-k8s/kms-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/kms-controller/pkg/resource/key"
	s3apis "github.com/aws-controllers-k8s/s3-controller/apis/v1alpha1"
	s3resource "github.com/aws-controllers-k8s/s3-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/s3-controller/pkg/resource/bucket"
	smapis "github.com/aws-controllers-k8s/secretsmanager-controller/apis/v1alpha1"
	smresource "github.com/aws-controllers-k8s/secretsmanager-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/secretsmanager-controller/pkg/resource/secret"
	snsapis "github.com/aws-controllers-k8s/sns-controller/apis/v1alpha1"
	snsresource "github.com/aws-controllers-k8s/sns-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/sns-controller/pkg/resource/topic"
	sqsapis "github.com/aws-controllers-k8s/sqs-controller/apis/v1alpha1"
	sqsresource "github.com/aws-controllers-k8s/sqs-controller/pkg/resource"
	_ "github.com/aws-controllers-k8s/sqs-controller/pkg/resource/queue"
)

func init() {
	provisioner.Register(s3resource.GetManagerFactories)
	provisioner.Register(ddbresource.GetManagerFactories)
	provisioner.Register(cwlresource.GetManagerFactories)
	provisioner.Register(sqsresource.GetManagerFactories)
	provisioner.Register(snsresource.GetManagerFactories)
	provisioner.Register(ecrresource.GetManagerFactories)
	provisioner.Register(eksresource.GetManagerFactories)
	provisioner.Register(ec2resource.GetManagerFactories)
	provisioner.Register(iamresource.GetManagerFactories)
	provisioner.Register(kmsresource.GetManagerFactories)
	provisioner.Register(smresource.GetManagerFactories)

	provisioner.RegisterScheme(s3apis.AddToScheme)
	provisioner.RegisterScheme(cwlapis.AddToScheme)
	provisioner.RegisterScheme(ddbapis.AddToScheme)
	provisioner.RegisterScheme(sqsapis.AddToScheme)
	provisioner.RegisterScheme(snsapis.AddToScheme)
	provisioner.RegisterScheme(ecrapis.AddToScheme)
	provisioner.RegisterScheme(iamapis.AddToScheme)
	provisioner.RegisterScheme(kmsapis.AddToScheme)
	provisioner.RegisterScheme(smapis.AddToScheme)
	provisioner.RegisterScheme(eksapis.AddToScheme)
	provisioner.RegisterScheme(ec2apis.AddToScheme)
}
