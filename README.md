# loack

A tiny, **controller-less provisioner** for [ACK](https://github.com/aws-controllers-k8s)
resources. It's like `terraform apply`, except the desired state is an ACK
Kubernetes custom resource (KRM) instead of HCL, and the "provider" is the ACK
controller's own *generated* `sdk.go` code.

`loack` works like Terraform: the YAML manifests in the working directory are
the desired configuration. `loack init` once, then `plan` / `apply` / `destroy`
reconcile AWS to match — driving each generated ACK **resource manager**
directly against the AWS API, with no Kubernetes cluster, no controller-runtime
manager loop, and no operator pod. State, backups, and config live in `.loack/`.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/chanwit/loack/main/install.sh | sh
```

Installs the latest `loack` core to `~/bin` (verified against the release
`SHA256SUMS`). Override with env vars:

```sh
# a different directory
curl -fsSL https://raw.githubusercontent.com/chanwit/loack/main/install.sh \
  | LOACK_INSTALL_DIR=/usr/local/bin sh

# pin a version, or install the all-in-one (every controller in one binary)
curl -fsSL https://raw.githubusercontent.com/chanwit/loack/main/install.sh \
  | LOACK_VERSION=v0.1.1 LOACK_VARIANT=loack-aio sh
```

The core pulls per-service providers from the release on first use; the
all-in-one (`loack-aio`) bundles them all. To build from source instead, see
*Build* below.

## How it works

Each ACK controller is a separate Go module under `./<service>-controller`.
Its generated code already contains everything needed to talk to AWS:

- `pkg/resource/<res>/manager.go` — `ReadOne` / `Create` / `Update` / `Delete`
- `pkg/resource/<res>/sdk.go`     — the actual AWS SDK calls
- `pkg/resource/<res>/descriptor.go` — KRM ⇆ `AWSResource` conversion
- an `init()` that **self-registers** a manager factory in the service registry

`loack` reuses all of it through the runtime's *exported interfaces* only — it
never touches unexported symbols:

1. Blank-import the resource package → its `init()` registers the factory.
2. Match the manifest's `apiVersion`/`kind` against each factory's GVK.
3. `descriptor.EmptyRuntimeObject()` + unmarshal YAML → desired `AWSResource`.
4. `factory.ManagerFor(...)` → a live resource manager bound to your AWS creds.
5. `apply` runs a bounded reconcile loop — `ReadOne`, then `Create` (if absent)
   or `Update` (while the `descriptor.Delta` is non-empty), honoring ACK requeue
   signals — until the resource converges.

The controller's `Reconciler` dependency is stubbed with a no-op
(`offlineReconciler`); it's only needed for Secret/reference resolution, which a
one-off provision of a simple resource doesn't exercise.

### Convergence (loack's "late-initialization")

A controller normally writes server-defaulted fields back to the CR between
reconciles so the next `Delta` is clean. Without a cluster, `loack` does the
equivalent in memory: before each diff it builds an **effective desired** state
where your manifest's fields win and every field you left unset is filled from
the observed resource. That stops `apply` from fighting AWS over defaults you
never declared, so it converges in one shot and the persisted state stays
stable across runs. In short: *loack only manages the fields you declare.*

## State

`loack` keeps a Terraform-tfstate-style file at `.loack/state.json` recording
every resource it has provisioned: the effective applied object, its ARN /
account / region, and when it was last applied. `init` creates `.loack/`; writes
are atomic (temp-file + rename), keep a `.backup` of the prior state, and bump a
`serial`. The desired config is the directory of manifests, so:

- `apply` creates/updates resources in the config and **destroys resources in
  state that are no longer in the config**.
- `destroy` tears down everything in state (rehydrated from the stored objects).
- `refresh` updates state to match live AWS.
- `state list | show <addr> | rm <addr> | mv <a> <b>` inspect and edit state.

## Build

```sh
make vendor     # clone the pinned ACK controllers (controllers.lock) into ./
make build      # -> bin/loack   (the core; links no controllers)
make providers  # -> bin/loack-provider-<svc> for every providers/<svc>/
make check      # vendor-verify + vet + test  (make help for all targets)
```

`bin/loack` is the **core**: a small binary that links no controllers and
dispatches each resource to a provider — found locally or downloaded (see
*Architecture* and *Releases*). `make providers` builds the per-service providers
next to it; `make provider-s3` builds just one.

For a single self-contained binary with every controller compiled in (no
providers, no downloads), build the all-in-one instead:

```sh
make aio            # -> bin/loack-aio   (the engine + every wired controller)
```

`controllers.lock` pins each controller clone to an exact commit, all sharing
`runtime v0.59.1`; `make vendor` clones them and verifies it (`make vendor-verify`
checks an existing checkout and also reports each provider module's runtime). The
clones are referenced via `replace` directives and are **not** part of this repo.
See the architecture below and [PORT_GUIDELINE.md](PORT_GUIDELINE.md) to add one.

## Architecture: core and providers

loack follows Terraform's core/provider split. The **engine** — routing, the
reconcile loop, convergence, references, state — is schema-agnostic and lives in
`provisioner/`; the ACK-free **protocol** that wraps it (JSON over stdin/stdout)
lives in `provider/`. When the core launches a provider it performs a plugin
**handshake** (a magic-cookie guard so a provider run directly prints guidance
instead of hanging, plus protocol-version negotiation so a mismatched
core/provider fails fast) — ported from HashiCorp go-plugin onto stdio (see
`provider/handshake.go` and THIRD_PARTY_NOTICES.md). Controllers are linked
*behind* that protocol, not into the core, so loack ships in two shapes:

- **Core** (`make build` → `bin/loack`, built `-tags split`): a small binary that
  links **no** controllers and **no** ACK runtime (~13M). It dispatches each
  resource, by API group, to a separate **provider binary** over the protocol. By
  default it **auto-discovers** providers — the group `<svc>.services.k8s.aws`
  maps to the binary `loack-provider-<svc>`, found next to `loack` (or in
  `.loack/providers/`, `$LOACK_PROVIDERS_DIR`, or `$PATH`), and **downloaded from
  the release** if it's missing (see *Releases*):

  ```sh
  make build providers
  bin/loack apply             # finds bin/loack-provider-<svc> automatically;
                              # Bucket -> s3 provider, Role -> iam provider
  ```

  Set `LOACK_PROVIDERS` (a PATH-list of binaries) to pin an explicit set instead
  of discovering.
- **All-in-one** (`make aio` → `bin/loack-aio`): the engine runs in-process with
  every controller linked (via `internal/allcontrollers`). One self-contained
  binary, no providers or downloads; simplest to run offline.

Each provider under `providers/<svc>/` is **its own Go module** linking one
controller, so it pins its **own** ACK runtime independently of the core and of
every other provider. The wired set all pins `runtime v0.59.1`, but any provider
*can* pin a different version (a `replace ... runtime => ... vX` in its `go.mod`)
with no effect on the others — they run in separate processes. The one bound: the
shared engine sets a runtime *floor* (it calls APIs added in v0.59).

## Use

```sh
export AWS_REGION=us-east-1          # or pass --region to init
# credentials resolved the same way the AWS CLI does (env, profile, role, ...)

mkdir myinfra && cd myinfra
cp .../examples/*.yaml .            # the *.yaml files here ARE the config
loack init                         # create .loack/, verify creds, record defaults
loack plan                         # show what apply would change
loack apply                        # plan + confirm, then reconcile
loack apply --auto-approve         # skip the confirmation prompt
# edit/remove a manifest, then:
loack apply                        # creates/updates/destroys to match the dir
loack refresh                      # pull live state into state
loack destroy                      # plan + confirm, then tear everything down
```

To delete a resource, remove its manifest from the directory and `apply` —
the plan shows it as `- will be destroyed`.

### Saved plans

Like Terraform, a plan can be written to a file and applied verbatim later:

```sh
loack plan --out             # snapshot the planned changes to plan.loack
loack apply plan.loack       # executes exactly that plan, no re-prompt
```

The saved plan stores the change set plus the region/account resolved at plan
time. Field paths in plan output use KRM names (`spec.requestPayment.payer`),
not Go struct names. The plan is an *approximation* — it shows the field-level
delta apply will attempt, not a guaranteed outcome (it does not predict
server-computed values or in-place-vs-replace).

Persistent flags: `--region`, `--account` (skips the STS lookup),
`--partition` (default `aws`), `--state` (default `.loack/state.json`).

## Display

Display is modeled on the Terraform CLI — a plan, then streaming per-resource
progress and a change summary (colored when stdout is a terminal; honors
`NO_COLOR`):

```text
$ loack apply --auto-approve
loack will perform the following actions:

  + Queue.demo-queue will be created
      + spec:
      +   queueName: demo-queue
  - Topic.old-topic will be destroyed      # removed from the directory

Plan: 1 to add, 0 to change, 1 to destroy.

Queue.demo-queue: Creating...
Queue.demo-queue: Creation complete after 2s [id=arn:aws:sqs:...]
Topic.old-topic: Destroying... [id=arn:aws:sns:...]
Topic.old-topic: Destruction complete after 0s

Apply complete! Resources: 1 added, 0 changed, 1 destroyed.
```

## Multi-document manifests & secrets

Each `*.yaml` file in the directory may hold several resources separated by
`---`; all files together form the configuration. `apply` orders them so
referenced resources come before their referrers (and destroys in reverse).

A native Kubernetes `Secret` (`apiVersion: v1`) is treated as a local secret
store: applying one records it in loack state, and ACK `SecretKeyReference`
fields are resolved from those stored Secrets — no cluster required. Values are
shown redacted and never written into saved plans. This lets one file carry a
Secret plus the resource that consumes it:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-app-creds
  namespace: default
stringData:
  password: s3cr3t
---
apiVersion: secretsmanager.services.k8s.aws/v1alpha1
kind: Secret
metadata:
  name: app-secret
spec:
  name: app-secret
  secretString:
    namespace: default
    name: my-app-creds
    key: password
```

```text
$ loack apply --auto-approve
Secret.my-app-creds: Storing in loack state...
Secret.my-app-creds: Stored in state (1 key(s))
Secret.app-secret: Creating...
Secret.app-secret: Creation complete after 1s [id=arn:aws:secretsmanager:...]

Apply complete! Resources: 2 added, 0 changed, 0 destroyed.
```

`apply`/`destroy`/`refresh` operate on the object recorded in state for existing
resources (the manifest spec merged onto the stored status), so resources whose
AWS lookup is identifier-based — most of them — are found correctly.

### Cross-resource references

ACK resources can point at each other with `...Ref` fields (e.g. an eks
`Cluster`'s `roleRef` → an iam `Role`, or a secretsmanager `Secret`'s
`kmsKeyRef` → a kms `Key`). loack resolves these from **state**: it runs the
controller's own generated `ResolveReferences` against a state-backed reader, so
a referenced resource that was applied earlier (and recorded) has its identifier
(ARN / ID) substituted into the dependent resource before it is created.

Put the referenced resource earlier in the same multi-document file (or apply it
first) so it is in state when the dependent one resolves:

```yaml
apiVersion: kms.services.k8s.aws/v1alpha1
kind: Key
metadata:
  name: app-key
spec:
  description: app key
---
apiVersion: secretsmanager.services.k8s.aws/v1alpha1
kind: Secret
metadata:
  name: app-secret
spec:
  name: app-secret
  kmsKeyRef:
    from:
      name: app-key            # resolved to the Key's ID from state
  secretString:
    namespace: default
    name: my-app-creds
    key: password
```

You can always sidestep a reference by supplying the literal instead (`kmsKeyID`,
`roleARN`, `subnetIDs`, ...). Note: a single `plan` of a file where one resource
references another not-yet-applied resource will report the reference missing —
the dependency only exists in state after apply.

## Namespaces, identity, and CARM targeting

ACK custom resources are **namespace-scoped**. loack honors that:

- **Identity** — state is keyed by `<group/version>/<kind>/<namespace>/<name>`,
  so the same `metadata.name` in two namespaces is two distinct resources.
- **References** — `...Ref` fields resolve **within a namespace** (the
  referrer's, or an explicit `namespace:` on the ref).
- **Account/region targeting (CARM)** — a `v1/Namespace` is not provisioned, but
  its annotations route the resources in that namespace to a specific AWS
  account/region, just like ACK's Cross-Account Resource Management:

  ```yaml
  apiVersion: v1
  kind: Namespace
  metadata:
    name: west
    annotations:
      services.k8s.aws/default-region: us-west-2        # region for this namespace
      services.k8s.aws/owner-account-id: "111122223333" # account for this namespace
  ```

  Every resource with `metadata.namespace: west` is then created in
  `us-west-2` / account `111122223333`. Region needs no extra setup. Targeting a
  **different account** requires a role to assume (CARM): map it in
  `.loack/config.json`, and loack assumes it via STS before provisioning —

  ```json
  { "roleAccountMap": { "111122223333": "arn:aws:iam::111122223333:role/loack" } }
  ```

  Resources in unannotated namespaces use the workspace default
  (`loack init --region …` / `--account`). State records each resource's resolved
  region/account, so `destroy`/`refresh` target it correctly later.

## Layout

A thin Cobra command layer (one file per verb, like the Flux CLI) over the
engine, with Terraform-style rendering isolated in `output.go`. The engine and
protocol sit in their own top-level packages so a provider module can import them:

```
cmd/loack/            # Cobra commands + Terraform-style output (the CLI / core)
  main.go             #   root command, persistent flags, state helpers
  config.go           #   .loack/config.json + effective AWS targeting
  manifest.go         #   gather *.yaml in the dir, dependency-order them
  plan_core.go        #   compute/render/execute the create/update/destroy plan
  output.go           #   progress observer, color, summaries, confirm prompt
  providers*.go       #   dispatcher: route each GVK to a provider (build-tagged)
  init.go plan.go apply.go destroy.go refresh.go state.go secret.go
provider/             # the ACK-free provider protocol (JSON over stdio) + Remote
provisioner/          # the engine: routing, reconcile loop, convergence, refs,
                      #   scheme, and the in-process Local provider
internal/state/       # the on-disk state file (.loack/state.json + .backup)
internal/allcontrollers/  # blank-imports + registers every controller (all-in-one)
cmd/loack-provider/   # all-in-one provider binary (serves the protocol)
providers/<svc>/      # one standalone Go module per controller — own go.mod,
                      #   own runtime; built to bin/loack-provider-<svc>
```

Only `internal/allcontrollers`, `cmd/loack-provider`, and the `providers/<svc>/`
modules link controller code; the core talks to providers through `provider`. The
core `bin/loack` (`-tags split`) links none of it; `bin/loack-aio` is the one
that compiles every controller in.

## Supported resources

| Controller | Kind                          | Status        |
|------------|-------------------------------|---------------|
| s3         | Bucket                        | working       |
| cloudwatchlogs | LogGroup                  | working       |
| dynamodb   | Table, Backup, GlobalTable    | working       |
| sqs        | Queue                         | working       |
| sns        | Topic                         | working       |
| ecr        | Repository                    | working       |
| iam        | Role, Policy, User, Group     | working       |
| kms        | Key                           | working       |
| secretsmanager | Secret                    | working (SecretKeyReference + kmsKeyRef) |
| eks        | Cluster, Nodegroup            | wired (roleRef/subnetRefs/clusterRef resolve; create is long-running) |
| ec2        | VPC, Subnet, SecurityGroup, InternetGateway, RouteTable, NATGateway, ElasticIPAddress, NetworkACL, VPCEndpoint, DHCPOptions, EgressOnlyInternetGateway, TransitGateway, VPCPeeringConnection | working (network stack; `...Ref` fields resolve from state) |

All 11 controllers above are wired with the recipe below. In the all-in-one
(`loack-aio`) they share one binary and resolve a single `runtime v0.59.1` (Go
MVS reconciles the rest); as providers each is a standalone module pinning its
own runtime. Each addition is a `replace` line, one blank import per resource, and
one `Register`/`RegisterScheme` call — no per-resource code.

## Adding another controller / resource

Wiring is mechanical — [PORT_GUIDELINE.md](PORT_GUIDELINE.md) has the full recipe.
In short:

1. Add a `replace` for the controller clone in `go.mod`, and pin it in
   `controllers.lock`.
2. In `internal/allcontrollers/all.go`, blank-import each resource package and
   call `provisioner.Register(<svc>resource.GetManagerFactories)` (plus
   `provisioner.RegisterScheme(<svc>apis.AddToScheme)` if it's a reference
   target).
3. `go mod tidy && make build`.

Routing, the apply/get/delete flow, convergence, references, and state are all
generic — no per-resource code. To ship it as a standalone provider too, copy an
existing `providers/<svc>/` module (its own `go.mod` replacing the controller
clones in its closure) and `make provider-<svc>`.

> Secret values (`SecretKeyReference`) and cross-resource references (`...Ref`)
> are both resolved from loack state — apply a Kubernetes `Secret` / the
> referenced resource first. Resources that genuinely need a live Kubernetes API
> for something else may still hit the offlineReconciler's limits.

## Releases

`git push` a `v*` tag and the `release` workflow builds the core and every
provider module for linux/darwin × amd64/arm64 and publishes them as release
assets, named for the download half to resolve by convention:

```
loack_<version>_<os>_<arch>                 # the core
loack-aio_<version>_<os>_<arch>             # all-in-one (every controller)
loack-provider-<svc>_<version>_<os>_<arch>  # one per providers/<svc>/
SHA256SUMS                                  # verify what you fetched
```

### Downloading providers

The core (`loack`) **fetches a missing provider automatically**: on a discovery
miss it downloads `loack-provider-<svc>_<version>_<os>_<arch>` from the release,
verifies its sha256 against `SHA256SUMS`, and caches it under `.loack/providers/`
(subsequent runs reuse the cache — no re-download). So on a released core,
`loack apply` just works with no providers pre-installed.

- **Version**: the core's own release version by default (core and providers ship
  from one tag); override with `LOACK_PROVIDER_VERSION`. A dev/dirty build does
  **not** auto-download — it falls back to the local "build it with `make
  provider-<svc>`" guidance.
- **Source**: `chanwit/loack` releases by default; override with
  `LOACK_PROVIDER_REPO`.
- **Local first**: an installed binary (next to the core, in `.loack/providers/`,
  `$LOACK_PROVIDERS_DIR`, or `$PATH`) is always preferred over downloading.

## License

[Apache-2.0](LICENSE). One file, `provider/handshake.go`, is adapted from
HashiCorp go-plugin and remains under MPL-2.0 — see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
