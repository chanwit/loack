# loack-iam-eks-roles — EKS cluster & node IAM roles

The two IAM roles a from-scratch EKS cluster needs, as a
[ConfigHub installer](https://github.com/confighub/installer) package of ACK
`iam` `Role` resources. The installer renders it to plain KRM; **loack**
provisions it to AWS directly.

## Resources (2)

| Role (`metadata.name`) | AWS name (default) | Trust | Attached managed policies |
|------------------------|--------------------|-------|---------------------------|
| `s0-cluster-role` | `eks-cluster-cluster-role` | `eks.amazonaws.com` | `AmazonEKSClusterPolicy` |
| `s0-node-role` | `eks-cluster-node-role` | `ec2.amazonaws.com` | `AmazonEKSWorkerNodePolicy`, `AmazonEKS_CNI_Policy`, `AmazonEC2ContainerRegistryReadOnly` |

`spec.policies` attaches the AWS-managed policies by ARN. The `metadata.name`s
are fixed so [`loack-eks-cluster`](../loack-eks-cluster) can reference them
(`roleRef` → `s0-cluster-role`, `nodeRoleRef` → `s0-node-role`). Both roles are
provisioned by loack today (the `iam` provider).

## Inputs

| input | default | sets |
|-------|---------|------|
| `cluster_name` | `eks-cluster` | prefixes the AWS role names (`<name>-cluster-role`, `<name>-node-role`) |

## Use

```sh
mkdir -p /tmp/roles && cd /tmp/roles
installer setup --pull /path/to/loack/packages/loack-iam-eks-roles \
  --non-interactive --namespace stage0 --input cluster_name=eks-cluster

cd out/manifests
loack init --region us-east-1
loack apply
```

Apply after [`loack-ec2-network`](../loack-ec2-network) and before
[`loack-eks-cluster`](../loack-eks-cluster).

> A full EKS bootstrap also needs IRSA/operator roles (an OIDC provider +
> per-ACK-service roles) when an ACK service is used. Those are conditional and
> depend on the live cluster's OIDC issuer, so they are not part of this static
> package.
