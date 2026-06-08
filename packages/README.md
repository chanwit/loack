# packages — installer packages provisioned by loack

[ConfigHub installer](https://github.com/confighub/installer) packages whose
resources are **ACK custom resources**. The installer renders each to plain KRM
YAML; **loack** provisions that YAML to AWS directly — no Kubernetes cluster and
no ACK controllers running. Naming follows `loack-<svc>-<resource>`.

| Package | OCI artifact | Resources | loack provisions |
|---------|--------------|-----------|------------------|
| [loack-s3-bucket](loack-s3-bucket) | `ghcr.io/chanwit/loack-s3-bucket` | an S3 bucket (+ encryption/versioning components) | ✓ |
| [loack-ec2-network](loack-ec2-network) | `ghcr.io/chanwit/loack-ec2-network` | VPC, subnets, gateways, route tables | ✓ |
| [loack-iam-eks-roles](loack-iam-eks-roles) | `ghcr.io/chanwit/loack-iam-eks-roles` | EKS cluster + node IAM roles | ✓ |
| [loack-eks-cluster](loack-eks-cluster) | `ghcr.io/chanwit/loack-eks-cluster` | EKS Cluster + Nodegroup | ✓ |

Each package is published as an OCI artifact by the `publish-packages` workflow
(on a push to `main` that touches `packages/`), so the installer can pull any of
them directly: `installer setup --pull oci://ghcr.io/chanwit/<pkg>:<version>`.

## EKS foundation

`loack-ec2-network`, `loack-iam-eks-roles`, and `loack-eks-cluster` together
stand up the AWS foundation for a from-scratch EKS cluster — a dedicated VPC,
public/private subnets across AZs, the cluster and node IAM roles, and the EKS
control plane + managed node group. They cross-reference by `metadata.name`, so
render them into **subdirectories of one workspace** and loack provisions the
whole tree in dependency order (resolving IDs from state) — no copy, no `-f`:

```sh
WS=~/eks
installer setup --pull oci://ghcr.io/chanwit/loack-ec2-network:0.1.0   --work-dir "$WS/network" --non-interactive --namespace eks
installer setup --pull oci://ghcr.io/chanwit/loack-iam-eks-roles:0.1.0 --work-dir "$WS/roles"   --non-interactive --namespace eks
installer setup --pull oci://ghcr.io/chanwit/loack-eks-cluster:0.1.0   --work-dir "$WS/eks"     --non-interactive --namespace eks

loack -C "$WS" init --region us-east-1     # workspace root (state in .loack/)
loack -C "$WS" apply                       # recurses network/ roles/ eks/ -> one config
```

The EKS control plane (~10–15 min) and node group (~5–8 min) are long-running,
but `apply` **waits for each resource to become ready by default**, so the single
command above blocks until the cluster and node group are `ACTIVE` — no re-runs.
Expect the whole foundation to take ~20–25 min. See
[loack-eks-cluster](loack-eks-cluster) for the dependency order.
