# loack-eks-cluster — EKS control plane + node group

An EKS control plane and managed node group, as a
[ConfigHub installer](https://github.com/confighub/installer) package of ACK
`eks` resources. The installer renders it to plain KRM; **loack** provisions it.

## Resources (2)

| Kind | Name | Wired by reference to |
|------|------|------------------------|
| Cluster | `s0-eks` | `roleRef` → `s0-cluster-role`; `subnetRefs` → the 4 subnets |
| Nodegroup | `s0-eks-ng` | `clusterRef` → `s0-eks`; `nodeRoleRef` → `s0-node-role`; `subnetRefs` → the 2 private subnets |

The cluster exposes both public and private API endpoints. The node group runs in
the private subnets (`m5.large` ×3 by default, min 1).

## Inputs

| input | default | sets |
|-------|---------|------|
| `cluster_name` | `eks-cluster` | the cluster's AWS name (`spec.name`) + node-group prefix |
| `instance_type` | `m5.large` | node group instance type |
| `node_count` | `3` | desired (and max) node count |

Both kinds are provisioned by loack today (the `eks` provider registers
`Cluster` and `Nodegroup`).

## Use

```sh
mkdir -p /tmp/eks && cd /tmp/eks
installer setup --pull /path/to/loack/packages/loack-eks-cluster \
  --non-interactive --namespace stage0 \
  --input cluster_name=eks-cluster --input instance_type=m5.large --input node_count=3

cd out/manifests
loack init --region us-east-1
loack apply
```

Apply **last** in the foundation sequence — its references resolve from the state
written by [`loack-ec2-network`](../loack-ec2-network) and
[`loack-iam-eks-roles`](../loack-iam-eks-roles), so render all three into the
same workspace (or apply them in order):

```
loack-ec2-network  →  loack-iam-eks-roles  →  loack-eks-cluster
```
