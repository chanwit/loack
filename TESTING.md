# loack — manual test checklist

End-to-end checks for the directory workflow (`init` → `plan` → `apply` →
`destroy`). Most steps hit **real AWS** and create/destroy real resources — run
them in a throwaway account/region, in a scratch directory.

## Setup

- [ ] `go build -o loack-bin ./cmd/loack` succeeds
- [ ] `go vet ./cmd/... ./internal/...` clean
- [ ] `go test ./internal/...` passes
- [ ] Put `loack-bin` on PATH (or use an absolute path), then:

  ```sh
  export AWS_PROFILE=<profile-with-creds>
  mkdir /tmp/loack-test && cd /tmp/loack-test
  ```

## init

- [ ] `loack plan` before init → "not initialized; run 'loack init' first"
- [ ] `loack init --region us-east-1` → prints account, region, state path,
      resource/controller counts; creates `.loack/{config.json,state.json}`
- [ ] re-running `loack init` is safe (idempotent)

## Create (plan → apply)

Drop two cheap, free, immediately-deletable resources in the dir:

```sh
cat > queue.yaml <<'EOF'
apiVersion: sqs.services.k8s.aws/v1alpha1
kind: Queue
metadata: {name: loack-test-queue}
spec: {queueName: loack-test-queue}
EOF
cat > topic.yaml <<'EOF'
apiVersion: sns.services.k8s.aws/v1alpha1
kind: Topic
metadata: {name: loack-test-topic}
spec: {name: loack-test-topic}
EOF
```

- [ ] `loack plan` → `+ Queue.… will be created`, `+ Topic.…`, `Plan: 2 to add`
- [ ] `loack apply` → shows plan, prompts `Enter a value:`; typing anything but
      `yes` → "Apply cancelled.", nothing created
- [ ] `loack apply` then `yes` (or `loack apply --auto-approve`)
      → Creating / Creation complete; `Apply complete! Resources: 2 added`
- [ ] `loack state list` shows both with ARNs
- [ ] `.loack/state.json` + `.loack/state.json.backup` exist

## Idempotence + no-op

- [ ] `loack plan` again → `No changes. Your infrastructure matches…`
- [ ] `loack apply --auto-approve` → `Resources: 0 added, 0 changed, 0 destroyed`

## Destroy-on-removal (the key directory behavior)

- [ ] `rm topic.yaml`
- [ ] `loack plan` → Queue is a no-op (not listed), `- Topic.… will be destroyed`,
      `Plan: 0 to add, 0 to change, 1 to destroy`
- [ ] `loack apply --auto-approve` → Topic destroyed, Queue untouched
- [ ] `aws sns list-topics` no longer shows the topic; `loack state list` has only the Queue

## refresh

- [ ] `loack refresh` → `Refreshing state...` per resource, `Refresh complete. N updated, 0 removed`
- [ ] Delete the queue out-of-band (`aws sqs delete-queue …`), then `loack refresh`
      → reports `1 removed from state`

## Saved plan

- [ ] (recreate queue.yaml) `loack plan --out` → "Saved the plan to: plan.loack"
- [ ] `loack apply plan.loack` → executes with no prompt
- [ ] `loack plan --out plan.loack` with no changes → "No changes, so no plan was saved."

## state subcommands

- [ ] `loack state show <address>` → prints the recorded object as YAML
- [ ] `loack state mv <a> <b>` → re-keys; `state list` shows the new address
- [ ] `loack state rm <address>` → "removed … (AWS resource left untouched)";
      resource still exists in AWS (forget, not destroy)

## destroy

- [ ] `loack destroy` → plan of all state + prompt; `no` cancels
- [ ] `loack destroy --auto-approve` → destroys everything; `Destroy complete!`
- [ ] `loack state list` → "No resources tracked"

## References & secrets (one combined file)

```yaml
# net.yaml
apiVersion: ec2.services.k8s.aws/v1alpha1
kind: VPC
metadata: {name: t-vpc}
spec: {cidrBlocks: [10.50.0.0/16]}
---
apiVersion: ec2.services.k8s.aws/v1alpha1
kind: Subnet
metadata: {name: t-subnet}
spec: {cidrBlock: 10.50.1.0/24, vpcRef: {from: {name: t-vpc}}}
```

- [ ] `loack apply --auto-approve` → VPC created first, Subnet's `vpcRef` resolved
      from state; subnet's VpcId matches the created VPC
- [ ] `loack destroy --auto-approve` → Subnet destroyed before VPC (reverse order)
- [ ] A native `v1/Secret` in the dir is stored in state (`Secret.<name>: Stored`)
      and resolves an ACK `SecretKeyReference`; values shown as "(sensitive value hidden)"

## Known limitations to confirm (not bugs)

- [ ] `plan` of a brand-new config where A references not-yet-created B shows A as
      a plain create (refs resolve at apply time, not plan time)
- [ ] Long-running creates (eks Cluster) report "provisioned but not yet fully
      converged"; the resource is still created
- [ ] `spec.acl` on an S3 Bucket renders oddly — S3 ACL is a special field

## Cleanup

- [ ] `loack destroy --auto-approve` leaves state empty
- [ ] `rm -rf /tmp/loack-test`
- [ ] secretsmanager/kms leftovers: `--force-delete-without-recovery` (KMS keys
      can't be purged faster than 7 days)
