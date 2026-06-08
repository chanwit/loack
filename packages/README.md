# packages — installer packages provisioned by loack

[ConfigHub installer](https://github.com/confighub/installer) packages whose
resources are **ACK custom resources**. The installer renders each to plain KRM
YAML; **loack** provisions that YAML to AWS directly — no Kubernetes cluster and
no ACK controllers running. Naming follows `loack-<svc>-<resource>`.

| Package | Resources | loack provisions |
|---------|-----------|------------------|
| [loack-s3-bucket](loack-s3-bucket) | an S3 bucket (+ encryption/versioning components) | ✓ |
| [loack-ec2-network](loack-ec2-network) | VPC, subnets, gateways, route tables | ✓ |
| [loack-iam-eks-roles](loack-iam-eks-roles) | EKS cluster + node IAM roles | ✓ |
| [loack-eks-cluster](loack-eks-cluster) | EKS Cluster + Nodegroup | ✓ |

## EKS foundation

`loack-ec2-network`, `loack-iam-eks-roles`, and `loack-eks-cluster` together
stand up the AWS foundation for a from-scratch EKS cluster — a dedicated VPC,
public/private subnets across AZs, the cluster and node IAM roles, and the EKS
control plane + managed node group — as declarative config-as-data. They
cross-reference by `metadata.name`, so render them into one workspace and apply
in order — loack resolves the IDs from state:

```
loack-ec2-network  →  loack-iam-eks-roles  →  loack-eks-cluster
```
