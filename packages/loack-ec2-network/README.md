# loack-ec2-network — VPC network stack for EKS

The VPC networking layer for a from-scratch EKS cluster, as a
[ConfigHub installer](https://github.com/confighub/installer) package of ACK
`ec2` custom resources. The installer renders it to plain KRM; **loack**
provisions it to AWS directly — no cluster, no ACK controllers running.

## Resources (10)

| Kind | Name | Notes |
|------|------|-------|
| VPC | `s0-vpc` | `10.0.0.0/16`, DNS support + hostnames on |
| Subnet ×2 (public) | `s0-public-1/2` | `10.0.0.0/20`, `10.0.16.0/20`; `kubernetes.io/role/elb`; map-public-IP |
| Subnet ×2 (private) | `s0-private-1/2` | `10.0.128.0/20`, `10.0.144.0/20`; `kubernetes.io/role/internal-elb` |
| InternetGateway | `s0-igw` | attached to the VPC |
| ElasticIPAddress | `s0-nat-eip` | for the NAT gateway |
| NATGateway | `s0-nat` | in `s0-public-1` |
| RouteTable (public) | `s0-public-rt` | default route → IGW |
| RouteTable (private) | `s0-private-rt` | default route → NAT |

Everything is wired by `...Ref` (by `metadata.name`), so the whole stack applies
in one shot and loack resolves IDs from state. All ten kinds are provisioned by
loack today (the `ec2` provider).

## Inputs

| input | default | sets |
|-------|---------|------|
| `vpc_cidr` | `10.0.0.0/16` | the VPC CIDR |
| `az_1` | `us-east-1a` | AZ of the `-1` subnets |
| `az_2` | `us-east-1b` | AZ of the `-2` subnets |

## Use

```sh
mkdir -p /tmp/net && cd /tmp/net
installer setup --pull /path/to/loack/packages/loack-ec2-network \
  --non-interactive --namespace stage0 \
  --input vpc_cidr=10.0.0.0/16 --input az_1=us-east-1a --input az_2=us-east-1b

cd out/manifests
loack init --region us-east-1
loack apply
```

This is the first of the EKS-foundation packages; apply it before
[`loack-iam-eks-roles`](../loack-iam-eks-roles) and
[`loack-eks-cluster`](../loack-eks-cluster), whose EKS cluster references these
subnets by name.

> Subnet ↔ route-table **associations** are not modeled by the ACK `ec2`
> `RouteTable` CR; associate them out of band if your workloads need it.
