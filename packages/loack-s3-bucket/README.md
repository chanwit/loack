# loack-s3-bucket — an installer package provisioned by loack

A minimal [ConfigHub installer](https://github.com/confighub/installer) package
whose resources are **ACK custom resources** instead of Kubernetes workloads.
The installer renders it to plain KRM YAML; **loack** then provisions that YAML
directly to AWS — no Kubernetes cluster and no ACK controllers running.

It demonstrates the `installer → loack → AWS` pipeline: config-as-data packaged
and distributed (optionally as a signed OCI artifact) by the installer, then
applied to AWS by loack.

## Layout

```
loack-s3-bucket/
├── installer.yaml              # Package: base, components, inputs, transformers
├── bases/default/
│   ├── kustomization.yaml
│   └── bucket.yaml             # an ACK s3 Bucket CR (spec.name set from input)
└── components/
    ├── encryption/             # optional: default SSE-S3 encryption
    └── versioning/             # optional: object versioning
```

- **input** `bucket_name` (required) → written into `spec.name` via a `yq-i`
  transformer at render time.
- **components** `encryption` / `versioning` → kustomize patches added when
  selected (`--select encryption`).

Region and account are **not** in the package — they are loack targeting
concerns supplied to `loack init`, not fields on the CR.

## Use it

Requires the `installer` binary (build from
[confighub/installer](https://github.com/confighub/installer)) and `loack`.

```sh
# 1. Render the package to plain KRM YAML (no OCI/ConfigHub server needed).
mkdir -p /tmp/bucket && cd /tmp/bucket
installer setup \
  --pull /path/to/loack/packages/loack-s3-bucket \
  --non-interactive \
  --select encryption --select versioning \
  --input bucket_name=my-unique-bucket-name

# 2. Provision the rendered ACK CR to AWS with loack.
cd out/manifests
loack init --region us-east-1
loack plan
loack apply
```

> `installer setup` (non-interactive) requires `--namespace` and emits a
> `v1/Namespace` alongside the Bucket. loack recognizes Namespace as a
> Kubernetes-only resource with no AWS counterpart and ignores it (with a note),
> so the rendered directory works as-is — no stripping required.

To distribute it instead of using a local path, push it as an OCI artifact and
pull by reference. The `publish-packages` GitHub workflow does this for every
package automatically — on a push to `main` that touches `packages/` (or via
manual dispatch) it publishes `ghcr.io/chanwit/<package>:<version>` (and
`:latest`), where `<version>` is `installerMetadata.version` from
`installer.yaml`. To do it by hand:

```sh
installer push packages/loack-s3-bucket oci://ghcr.io/chanwit/loack-s3-bucket:0.1.0
installer sign  oci://ghcr.io/chanwit/loack-s3-bucket:0.1.0   # optional, cosign
# consumer:
installer setup --pull oci://ghcr.io/chanwit/loack-s3-bucket:0.1.0 --input bucket_name=... ...
```

## Extending

- Add an input (e.g. `environment`) and a transformer that writes it into a tag:
  `yq-i` with `.spec.tagging.tagSet += [{"key":"Environment","value":"{{ .Inputs.environment }}"}]`.
- Add components for `publicAccessBlock`, `logging`, `lifecycle`, etc. — every
  field the ACK `Bucket` spec supports is fair game, since it's all just data.
