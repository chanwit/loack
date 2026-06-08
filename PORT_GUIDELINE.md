# Porting an ACK controller into loack

This guide gives the exact steps to make an ACK controller's resources usable by
loack (`plan` / `apply` / `get` / `delete` / `destroy`). No per-resource code is
required — wiring is a `go.mod` `replace`, one blank import per resource, and one
registry entry per controller. Reference targets additionally need a scheme
entry.

Everything below uses `<svc>` for the AWS service alias (e.g. `s3`, `ec2`,
`secretsmanager`) and `<resource>` for a resource package directory (e.g.
`bucket`, `vpc`, `secret`).

---

## 0. Prerequisites

The controller repo must be cloned next to the others, at
`./<svc>-controller` (sibling of `internal/`, `cmd/`). They were bulk-cloned
from `github.com/aws-controllers-k8s/<svc>-controller`.

```sh
ls ./<svc>-controller/go.mod        # must exist
```

Check the controller pins the same `runtime` as the rest (low version skew):

```sh
grep 'aws-controllers-k8s/runtime ' <svc>-controller/go.mod
# want: github.com/aws-controllers-k8s/runtime v0.59.1  (matches the others)
```

If it pins a very different `runtime`/`aws-sdk-go-v2`/`k8s.io/*`, expect Go MVS
to reconcile to the highest version; usually fine, occasionally a compile break
(see Troubleshooting).

---

## 1. Discover the resource

```sh
# resource package names (these are the blank-import paths)
ls <svc>-controller/pkg/resource/ | grep -v '\.go$'

# API group + version
grep -E 'Group:|Version:' <svc>-controller/apis/v1alpha1/groupversion_info.go

# required spec fields (you'll need these in the manifest)
awk '/\+kubebuilder:validation:Required/{getline; print}' \
  <svc>-controller/apis/v1alpha1/<resource>.go

# reference (...Ref) fields — do any spec fields point at other resources?
grep -nE 'Ref \*ackv1alpha1|Refs \[\]\*ackv1alpha1' \
  <svc>-controller/apis/v1alpha1/<resource>.go

# does it read Secret values? (SecretKeyReference)
grep -n 'SecretKeyReference' <svc>-controller/apis/v1alpha1/<resource>.go
```

`apiVersion` for manifests is `<group>/<version>`, e.g. `s3.services.k8s.aws/v1alpha1`.

---

## 2. Add the `replace` to `go.mod`

Point the module at the local clone. Add a `require` (placeholder version is
fine; `replace` overrides it) and a `replace`:

```go
// in require ( ... )
github.com/aws-controllers-k8s/<svc>-controller v0.0.0-00010101000000-000000000000

// after the other replaces
replace github.com/aws-controllers-k8s/<svc>-controller => ./<svc>-controller
```

`go mod tidy` may rewrite the `require` line to a real tag — that's fine, the
`replace` still wins and uses your local clone.

**Pin it in `controllers.lock`.** Add a line `<svc>-controller <commit-sha>` so
`make vendor` can reproduce the checkout. Critically, the commit you pick MUST
use the same `RUNTIME` as the rest of the file — `make vendor` verifies this and
fails otherwise, which is exactly the guard that stops the combined dependency
graph from drifting apart. To bump after cloning a newer commit, run
`make vendor-relock` (it refuses to relock a set that no longer shares one
runtime).

---

## 3. Register the resource — `internal/allcontrollers/all.go`

Controllers register themselves with the provisioner; `internal/allcontrollers`
is the all-in-one set linked by `loack-aio` and the all-in-one provider. Add
your controller there (a per-service provider main registers only its own).

**3a. Blank-import each resource you want, plus the controller's registry and
apis:**

```go
import (
    ...
    <svc>apis "github.com/aws-controllers-k8s/<svc>-controller/apis/v1alpha1"
    <svc>resource "github.com/aws-controllers-k8s/<svc>-controller/pkg/resource"
    _ "github.com/aws-controllers-k8s/<svc>-controller/pkg/resource/<resource>"
    // one blank import per resource of this controller you want
)
```

**3b. Register it in `init()`** (once per controller):

```go
provisioner.Register(<svc>resource.GetManagerFactories)
provisioner.RegisterScheme(<svc>apis.AddToScheme)
```

That's the whole mechanical wiring. `plan` / `apply` / `destroy` / `refresh`,
state, drift, references, and convergence are all generic over the GVK + ACK
interfaces. The engine (`provisioner/`) imports no controllers, so a binary links
only what it registers — see `providers/<svc>/` for a standalone single-service
provider module.

---

## 4. Register the API types in the scheme — same `RegisterScheme` call

Do this if the resource is a **reference target** (something else points at it
with a `...Ref`) **or references another type**. The state-backed reader uses a
scheme to map a referenced Go object to its GVK, built from every `AddToScheme`
passed to `provisioner.RegisterScheme`. You already did this in step 3b:

```go
provisioner.RegisterScheme(<svc>apis.AddToScheme)
```

so there is nothing extra to edit (the old hand-maintained `scheme.go` list is
gone — registration is injected). When unsure whether a type is referenced, add
the call anyway — it's cheap.

Note: a resource can reference types owned by **other** controllers (e.g. eks
`Cluster.roleRef` → iam `Role`, `subnetRefs` → ec2 `Subnet`). Make sure every
referenced controller's apis is registered (its own `RegisterScheme` call), even
if you didn't wire that controller's resource managers. Importing only
`apis/v1alpha1` is lightweight — it does **not** pull the resource managers (a
provider that references other services links only their `apis` packages, not
their controllers; verify with `go list -deps`).

---

## 5. Build

```sh
go mod tidy
make aio            # bin/loack-aio (all controllers in-process) + bin/loack-provider
make vet            # go vet ./... + the core
```

A clean build means routing works. If `go mod tidy` downloads a new
`aws-sdk-go-v2/service/<svc>` that's expected.

---

## 5b. (Optional) Ship it as a standalone provider module

The steps above wire the controller into `loack-aio` and the all-in-one
provider. To also expose it as its **own** provider binary — its own Go module,
pinning its own runtime, dispatched to (and downloaded) by the core — add
`providers/<svc>/`. Copy an existing one (e.g. `providers/iam/`) and adapt:

`providers/<svc>/go.mod` — a module that replaces the loack engine and **every**
controller clone in this provider's closure (the primary one plus any referenced
controllers' apis, e.g. eks → ec2/iam/kms) to the vendored siblings, so the build
is reproducible and resolves `runtime v0.59.1`:

```go
module loack-provider-<svc>
go 1.25.5
require (
    github.com/aws-controllers-k8s/<svc>-controller v0.0.0
    loack v0.0.0
)
replace loack => ../..
replace github.com/aws-controllers-k8s/<svc>-controller => ../../<svc>-controller
// + one replace per referenced controller in the closure -> ../../<ctrl>
```

`providers/<svc>/main.go` — register only this controller, then serve:

```go
provisioner.Register(<svc>resource.GetManagerFactories)
provisioner.RegisterScheme(<svc>apis.AddToScheme)
provider.Serve(provisioner.NewLocal())
```

Then:

```sh
cd providers/<svc> && go mod tidy   # resolves the module's own graph
make provider-<svc>                 # -> bin/loack-provider-<svc>
make vendor-verify                  # lists the new module + its runtime
```

`make vendor-verify` confirms every controller the module replaces is pinned in
`controllers.lock` and reports the module's effective runtime.

---

## 6. Write a manifest and verify (read-only first)

```yaml
apiVersion: <group>/v1alpha1
kind: <Kind>
metadata:
  name: my-thing
spec:
  # the required fields from step 1
```

Put it (and only it) in a fresh working directory:

```sh
mkdir port-test && cd port-test && cp .../my-thing.yaml .
AWS_PROFILE=<profile> loack init --region <region>
loack plan      # read-only: confirms routing + a real AWS read. No mutation.
```

Expect `+ <Kind>.my-thing will be created` / `Plan: 1 to add`. A credentials
error past the "Refreshing state..." line still proves routing.

Then a real round-trip in a throwaway account:

```sh
loack apply --auto-approve   # create
loack plan                   # should report no changes
rm my-thing.yaml && loack apply --auto-approve   # removed from config -> destroyed
```

---

## References and secrets

These work out of the box once wired — no extra code — but the referenced data
must be in loack state first.

**`...Ref` fields** (cross-resource references): loack runs the controller's own
generated `ResolveReferences` against a state-backed reader, substituting the
referenced resource's ARN/ID. So **apply the referenced resource first**, or put
it earlier in the same multi-document file:

```yaml
apiVersion: ec2.services.k8s.aws/v1alpha1
kind: VPC
metadata:
  name: my-vpc
spec:
  cidrBlocks:
    - 10.0.0.0/16
---
apiVersion: ec2.services.k8s.aws/v1alpha1
kind: Subnet
metadata:
  name: my-subnet
spec:
  cidrBlock: 10.0.1.0/24
  vpcRef:
    from:
      name: my-vpc            # resolved from state to the VPC's ID
```

You can always sidestep a reference with the literal field (`vpcID`, `roleARN`,
`kmsKeyID`, `subnetIDs`, ...). The referenced type must be registered in the
scheme (step 4).

**`SecretKeyReference` fields** (secret values): apply a native Kubernetes
`Secret` (`apiVersion: v1`); it is stored in loack state and the reference
resolves from it:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: creds
  namespace: default
stringData:
  password: s3cr3t
---
apiVersion: rds.services.k8s.aws/v1alpha1
kind: DBInstance
spec:
  masterUserPassword:
    namespace: default
    name: creds
    key: password
```

---

## What to expect per resource type

| Trait | Behavior in loack | Action needed |
|-------|-------------------|---------------|
| Self-contained (no refs/secrets) | Just works | none |
| Found by name (e.g. S3 Bucket) | Works | none |
| Found by ARN/ID (most resources) | Works — engine threads the created resource's identifiers into later reads | none |
| Has `...Ref` fields | Resolved from state | apply referenced resource first; register its apis in scheme |
| Has `SecretKeyReference` | Resolved from a state-stored k8s Secret | apply the `v1/Secret` first |
| Long-running create (e.g. EKS Cluster, ~10+ min) | Create succeeds; apply reports "provisioned but not yet fully converged" after ~50s | re-run `apply`/`get` later |
| Needs a live Kubernetes API for something other than refs/secrets | May hit the offlineReconciler's limits | not currently supported |

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `no registered resource manager for <group>/<version>/<Kind>` | resource not blank-imported, or registry entry missing | step 3 |
| Compile error after adding controller | `runtime`/sdk version skew via MVS | align the clone to a compatible tag, or pin in `go.mod`; worst case, one-binary-per-controller |
| `apply` says created but resource missing / "already exists" on 2nd try | resource found by ARN, not name; identifiers weren't threaded | already handled by the engine — ensure you're on current `provisioner/provisioner.go` |
| `delete` reports fewer destroyed than expected; resource leaks | delete read the bare manifest (no identifiers) | already handled — delete/get use `targetForManifest` (state object). Ensure resource is in state |
| Apply loops then "not yet fully converged" on a secret/ref resource | ACK wraps an unresolved Secret/ref error as a requeue | provide the secret/ref (apply the `v1/Secret` or referenced resource first); the engine surfaces the underlying cause after 2 stuck create-requeues |
| `unknown type for reference "x"` during apply | referenced type not in the scheme | step 4: add the referenced controller's `apis.AddToScheme` |
| Reference resolves to "not synced" / "missing ARN" | referenced resource not in state, or stored without an ARN | apply the referenced resource first (loack injects the synced condition automatically) |

---

## Worked example: add `sqs` `Queue`

```sh
ls sqs-controller/pkg/resource/        # -> queue
grep Group: sqs-controller/apis/v1alpha1/groupversion_info.go   # sqs.services.k8s.aws
```

`go.mod`:
```go
github.com/aws-controllers-k8s/sqs-controller v0.0.0-00010101000000-000000000000
replace github.com/aws-controllers-k8s/sqs-controller => ./sqs-controller
```

`internal/allcontrollers/all.go` (Queue is also a potential reference target, so
register its apis too):
```go
sqsresource "github.com/aws-controllers-k8s/sqs-controller/pkg/resource"
_           "github.com/aws-controllers-k8s/sqs-controller/pkg/resource/queue"
sqsapis     "github.com/aws-controllers-k8s/sqs-controller/apis/v1alpha1"
// ... in init():
provisioner.Register(sqsresource.GetManagerFactories)
provisioner.RegisterScheme(sqsapis.AddToScheme)
```

```sh
go mod tidy && make aio        # links sqs in-process for a quick test
mkdir demo && cd demo
cat > q.yaml <<'EOF'
apiVersion: sqs.services.k8s.aws/v1alpha1
kind: Queue
metadata:
  name: demo-queue
spec:
  queueName: demo-queue
EOF
AWS_PROFILE=<p> ../bin/loack-aio init --region us-east-1
AWS_PROFILE=<p> ../bin/loack-aio apply --auto-approve
```

---

## Testing hygiene

- Use a throwaway account/region; loack creates real resources.
- `secretsmanager` Secrets and `kms` Keys have **deletion recovery windows** —
  repeat tests collide on names. Use unique names, or
  `aws secretsmanager delete-secret --force-delete-without-recovery`. KMS keys
  cannot be purged faster than 7 days.
- Tear down with `loack destroy --auto-approve` (everything in state), or remove
  a manifest and `loack apply`.
