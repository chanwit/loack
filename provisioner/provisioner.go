// Package provisioner drives ACK controllers' generated resource managers to
// reconcile a single resource against the AWS API, without running the
// controller / controller-runtime manager loop.
package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	smithy "github.com/aws/smithy-go"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"github.com/go-logr/logr"
	"sigs.k8s.io/yaml"

	ackv1alpha1 "github.com/aws-controllers-k8s/runtime/apis/core/v1alpha1"
	ackcompare "github.com/aws-controllers-k8s/runtime/pkg/compare"
	ackcfg "github.com/aws-controllers-k8s/runtime/pkg/config"
	ackerr "github.com/aws-controllers-k8s/runtime/pkg/errors"
	ackmetrics "github.com/aws-controllers-k8s/runtime/pkg/metrics"
	ackrequeue "github.com/aws-controllers-k8s/runtime/pkg/requeue"
	acktypes "github.com/aws-controllers-k8s/runtime/pkg/types"
)

// registries holds the resource-manager-factory registries of every controller
// linked into this binary. A binary registers its controllers via Register (see
// internal/allcontrollers for the all-in-one set, or a per-service provider's
// main for a subset). The provisioner itself imports no controllers, so a
// binary links only what it registers.
var registries []func() []acktypes.AWSResourceManagerFactory

// Register adds a controller's manager-factory registry getter.
func Register(getFactories func() []acktypes.AWSResourceManagerFactory) {
	registries = append(registries, getFactories)
}

// These aliases re-export ACK runtime types so callers need not import the
// runtime module directly.
type (
	AWSResourceManager = acktypes.AWSResourceManager
	AWSResource        = acktypes.AWSResource
)

// Options holds the AWS targeting parameters shared by every action.
type Options struct {
	Region    string
	AccountID string
	Partition string
	// RoleARN, when set, is assumed (via STS) to obtain credentials for the
	// target account before provisioning -- loack's analogue of ACK's
	// cross-account resource management (CARM). Empty means use ambient creds.
	RoleARN string
	// Secrets resolves SecretKeyReferences without a Kubernetes cluster.
	Secrets SecretStore
}

// Target is a manifest that has been loaded and bound to the registered ACK
// resource-manager factory able to handle it.
type Target struct {
	APIVersion string
	Kind       string
	Name       string
	Namespace  string

	desired    acktypes.AWSResource
	descriptor acktypes.AWSResourceDescriptor
	factory    acktypes.AWSResourceManagerFactory
}

// Address returns the resource's display address, "<Kind>.<Name>", used in
// Terraform-style progress output.
func (t *Target) Address() string { return t.Kind + "." + t.Name }

// EventKind enumerates the lifecycle phases an action streams as it runs.
type EventKind int

const (
	EventRefreshing EventKind = iota
	EventCreating
	EventCreated
	EventModifying
	EventModified
	EventDestroying
	EventDestroyed
)

// Event is a progress signal emitted during Apply/Delete so the CLI can render
// Terraform-style streaming output ("Creating...", "Creation complete...").
type Event struct {
	Kind    EventKind
	Address string
	ID      string // ARN, when known
}

// Hook receives progress Events. A nil Hook is ignored.
type Hook func(Event)

func emit(h Hook, kind EventKind, address, id string) {
	if h != nil {
		h(Event{Kind: kind, Address: address, ID: id})
	}
}

// Action describes what a reconcile did to the live AWS resource.
type Action string

const (
	ActionCreated   Action = "created"
	ActionUpdated   Action = "updated"
	ActionUnchanged Action = "unchanged"
	ActionObserved  Action = "observed"
	ActionDeleted   Action = "deleted"
	ActionAbsent    Action = "absent"
)

// Result is the outcome of an action. Resource is the latest known state and
// may be nil (e.g. after a delete, or when the resource was absent).
type Result struct {
	Action   Action
	Resource acktypes.AWSResource
}

// Load reads a manifest from path and binds it to the resource manager factory
// registered for its apiVersion/kind.
func Load(path string) (*Target, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	return LoadBytes(data)
}

// LoadBytes is like Load but takes the manifest (YAML or JSON) directly. It is
// used both for on-disk manifests and for rehydrating a resource from the
// stored object in the state file.
func LoadBytes(data []byte) (*Target, error) {
	var head struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := yaml.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("parsing manifest header: %w", err)
	}
	if head.APIVersion == "" || head.Kind == "" {
		return nil, errors.New("manifest is missing apiVersion or kind")
	}

	factory, descriptor, err := lookup(head.APIVersion, head.Kind)
	if err != nil {
		return nil, err
	}

	obj := descriptor.EmptyRuntimeObject()
	if err := yaml.Unmarshal(data, obj); err != nil {
		return nil, fmt.Errorf("unmarshaling %s: %w", head.Kind, err)
	}
	desired := descriptor.ResourceFromRuntimeObject(obj)

	name, namespace := "", ""
	if mo := desired.MetaObject(); mo != nil {
		name = mo.GetName()
		namespace = mo.GetNamespace()
	}

	return &Target{
		APIVersion: head.APIVersion,
		Kind:       head.Kind,
		Name:       name,
		Namespace:  namespace,
		desired:    desired,
		descriptor: descriptor,
		factory:    factory,
	}, nil
}

// Manager resolves AWS credentials/region/account and returns a live resource
// manager bound to them, along with the fully-resolved Options.
func (t *Target) Manager(ctx context.Context, opts Options) (acktypes.AWSResourceManager, Options, error) {
	if opts.Partition == "" {
		opts.Partition = "aws"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(opts.Region))
	if err != nil {
		return nil, opts, fmt.Errorf("loading AWS config: %w", err)
	}
	if opts.Region == "" {
		opts.Region = awsCfg.Region
	}
	if opts.Region == "" {
		return nil, opts, errors.New("no AWS region set (use --region or $AWS_REGION)")
	}

	// CARM: assume the target account's role to obtain its credentials. The
	// ACK manager factory expects clientcfg to already carry the right creds; it
	// only uses roleARN to key its manager cache.
	if opts.RoleARN != "" {
		stsClient := sts.NewFromConfig(awsCfg)
		provider := stscreds.NewAssumeRoleProvider(stsClient, opts.RoleARN)
		awsCfg.Credentials = aws.NewCredentialsCache(provider)
	}

	if opts.AccountID == "" {
		out, err := sts.NewFromConfig(awsCfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return nil, opts, fmt.Errorf("looking up account ID via STS: %w", err)
		}
		opts.AccountID = *out.Account
	}

	rm, err := t.factory.ManagerFor(
		ackcfg.Config{Region: opts.Region, AccountID: opts.AccountID, Partition: opts.Partition},
		awsCfg, logr.Discard(), ackmetrics.NewMetrics(t.Kind),
		offlineReconciler{secrets: opts.Secrets},
		ackv1alpha1.AWSAccountID(opts.AccountID),
		ackv1alpha1.AWSRegion(opts.Region),
		ackv1alpha1.AWSResourceName(opts.RoleARN),
	)
	if err != nil {
		return nil, opts, fmt.Errorf("building resource manager: %w", err)
	}
	return rm, opts, nil
}

// reconcileTimeout bounds Apply's wait-by-default loop: loack keeps reconciling
// and polling a resource that is still transitioning (e.g. an EKS cluster in
// CREATING) until it converges or this elapses.
const reconcileTimeout = 30 * time.Minute

// pollInterval is how often Apply re-reads a resource whose spec matches but
// whose lifecycle status is still transitioning.
const pollInterval = 10 * time.Second

// pendingStates are lifecycle status/state values (lowercased) that mean a
// resource is still transitioning toward ready. Terminal values (active,
// available, failed, ...) and resources with no such field are treated as done,
// so loack only waits for genuinely async resources and never hangs on a simple
// one like an S3 bucket (which has no lifecycle status field).
var pendingStates = map[string]bool{
	"creating": true, "pending": true, "provisioning": true, "activating": true,
	"updating": true, "modifying": true, "configuring": true, "starting": true,
	"upgrading": true, "restoring": true, "rebooting": true, "resetting": true,
	"snapshotting": true, "backing-up": true, "create_in_progress": true,
	"update_in_progress": true, "in_progress": true,
}

// pendingStatus reports whether r exposes a lifecycle status/state field still
// in a transitioning value (e.g. EKS Cluster.Status.Status == "CREATING", NAT
// gateway Status.State == "pending"). Resources without such a field return
// false. Used by Apply to wait for async creates to reach a terminal state.
func pendingStatus(r acktypes.AWSResource) bool {
	v := reflect.ValueOf(r.RuntimeObject())
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	st := v.FieldByName("Status")
	for st.IsValid() && st.Kind() == reflect.Ptr {
		if st.IsNil() {
			return false
		}
		st = st.Elem()
	}
	if !st.IsValid() || st.Kind() != reflect.Struct {
		return false
	}
	for _, name := range []string{"Status", "State"} {
		f := st.FieldByName(name)
		for f.IsValid() && f.Kind() == reflect.Ptr {
			if f.IsNil() {
				f = reflect.Value{}
				break
			}
			f = f.Elem()
		}
		if f.IsValid() && f.Kind() == reflect.String && pendingStates[strings.ToLower(f.String())] {
			return true
		}
	}
	return false
}

// maxRequeueBackoff caps how long we honor an ACK requeue delay, keeping a
// one-off provision responsive.
const maxRequeueBackoff = 5 * time.Second

// maxCreateRequeues bounds how many times a create may requeue while the
// resource is still not found before we treat it as a failed create and
// surface the underlying cause (e.g. an unresolved Secret reference).
const maxCreateRequeues = 2

// Apply reconciles the desired state into AWS, looping until it converges.
//
// ACK's generated code uses requeue signals (e.g. "bucket created, requeue for
// updates") to drive the controller's eventual-consistency loop across multiple
// reconcile passes. A one-off provisioner has to run that loop itself: create or
// update, and while the resource manager keeps asking to be requeued -- or the
// observed state still differs from desired -- read and reconcile again.
func (t *Target) Apply(ctx context.Context, rm acktypes.AWSResourceManager, hook Hook) (*Result, error) {
	addr := t.Address()
	action := ActionUnchanged // the headline action, set on first create/update
	var latest acktypes.AWSResource
	creating, modifying := false, false
	createRequeues := 0

	// current is the resource handed to ReadOne. It starts as the desired CR but
	// is replaced by the created/updated resource as soon as one exists, so that
	// subsequent reads carry the server-assigned identifiers (ARN, etc.). Some
	// resources' sdkFind looks up by ARN, not by a spec field, and would
	// otherwise report NotFound after a successful create and double-create.
	current := t.desired

	emit(hook, EventRefreshing, addr, "")

	// Wait by default: keep reconciling, and poll a resource whose spec already
	// matches but whose lifecycle status is still transitioning, until it
	// converges or the timeout elapses -- so one apply carries a long async
	// create like an EKS cluster all the way to ACTIVE.
	deadline := time.Now().Add(reconcileTimeout)
	for time.Now().Before(deadline) {
		observed, err := rm.ReadOne(ctx, current)
		switch {
		case errors.Is(err, ackerr.NotFound):
			if !creating {
				emit(hook, EventCreating, addr, "")
				creating = true
			}
			created, cerr := rm.Create(ctx, t.desired)
			if created != nil {
				latest = created
				current = created
			}
			if action == ActionUnchanged {
				action = ActionCreated
			}
			if wait, ok := requeueAfter(cerr); ok {
				// A genuine "created, requeue to sync" makes the next ReadOne
				// find the resource. If we're still here on NotFound after a
				// create requeued, the create never took effect (e.g. an
				// unresolved Secret wrapped as a requeue) -- surface the cause.
				createRequeues++
				if createRequeues >= maxCreateRequeues {
					if cause := requeueCause(cerr); cause != nil {
						return nil, cause
					}
				}
				if serr := sleep(ctx, wait); serr != nil {
					return nil, serr
				}
				continue
			}
			if cerr != nil {
				return nil, cerr
			}
			continue // re-read to sync any post-create fields
		case err != nil:
			if wait, ok := requeueAfter(err); ok {
				if serr := sleep(ctx, wait); serr != nil {
					return nil, serr
				}
				continue
			}
			return nil, err
		}

		latest = observed
		current = observed
		eff, eerr := t.effectiveDesired(observed)
		if eerr != nil {
			return nil, fmt.Errorf("computing effective desired state: %w", eerr)
		}
		delta := t.descriptor.Delta(eff, observed)
		if len(delta.Differences) == 0 {
			// Spec matches. If the resource is still transitioning (e.g. EKS
			// CREATING), wait and re-read; otherwise it has converged.
			if pendingStatus(observed) {
				if serr := sleep(ctx, pollInterval); serr != nil {
					return nil, serr
				}
				continue
			}
			id, _, _ := Metadata(latest)
			switch action {
			case ActionCreated:
				emit(hook, EventCreated, addr, id)
			case ActionUpdated:
				emit(hook, EventModified, addr, id)
			}
			return &Result{Action: action, Resource: latest}, nil
		}

		if action == ActionUnchanged && !modifying {
			id, _, _ := Metadata(observed)
			emit(hook, EventModifying, addr, id)
			modifying = true
		}
		updated, uerr := rm.Update(ctx, eff, observed, delta)
		if updated != nil {
			latest = updated
			current = updated
		}
		if action == ActionUnchanged {
			action = ActionUpdated
		}
		if wait, ok := requeueAfter(uerr); ok {
			if serr := sleep(ctx, wait); serr != nil {
				return nil, serr
			}
			continue
		}
		if uerr != nil {
			return nil, uerr
		}
	}

	// Ran out of attempts while still reconciling: the create/update calls
	// themselves succeeded, so report the latest state rather than fail.
	return &Result{Action: action, Resource: latest}, errStillReconciling
}

// errStillReconciling is returned when Apply exhausts its attempts but the
// underlying AWS mutations succeeded; callers may treat it as a soft warning.
var errStillReconciling = errors.New("resource provisioned but not yet fully converged")

// ErrStillReconciling reports whether err is the soft not-yet-converged signal.
func ErrStillReconciling(err error) bool { return errors.Is(err, errStillReconciling) }

// isNotFound reports whether err means the AWS resource does not exist. ACK's
// generated sdkFind maps most "missing" responses to ackerr.NotFound, but some
// services instead surface the raw AWS API error -- e.g. ec2 DescribeSubnets on
// a deleted id returns InvalidSubnetID.NotFound, EIP DescribeAddresses returns
// InvalidAllocationID.NotFound. During teardown those mean "already gone", so we
// also treat any smithy API error whose code denotes absence as NotFound.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ackerr.NotFound) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		// Covers Invalid<Thing>ID.NotFound, ResourceNotFoundException,
		// NoSuchEntity, NoSuchBucket, ...NotFoundException, etc.
		if strings.Contains(code, "NotFound") || strings.Contains(code, "NoSuch") {
			return true
		}
	}
	return false
}

// requeueAfter reports whether err is an ACK requeue signal and, if so, how long
// to wait before reconciling again (clamped to maxRequeueBackoff).
func requeueAfter(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	var after *ackrequeue.RequeueNeededAfter
	if errors.As(err, &after) {
		d := after.Duration()
		if d > maxRequeueBackoff {
			d = maxRequeueBackoff
		}
		return d, true
	}
	var now *ackrequeue.RequeueNeeded
	if errors.As(err, &now) {
		return 0, true
	}
	return 0, false
}

// requeueCause unwraps the underlying error a requeue signal carries, if any.
// ACK wraps real failures (like an unresolved Secret) as requeues; this digs
// the real error back out so loack can report it instead of looping.
func requeueCause(err error) error {
	var after *ackrequeue.RequeueNeededAfter
	if errors.As(err, &after) {
		if u := after.Unwrap(); u != nil {
			return u
		}
	}
	var now *ackrequeue.RequeueNeeded
	if errors.As(err, &now) {
		if u := now.Unwrap(); u != nil {
			return u
		}
	}
	return nil
}

// sleep waits for d, returning early if the context is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Get returns the currently-observed AWS state of the resource.
func (t *Target) Get(ctx context.Context, rm acktypes.AWSResourceManager) (*Result, error) {
	latest, err := rm.ReadOne(ctx, t.desired)
	if errors.Is(err, ackerr.NotFound) {
		return &Result{Action: ActionAbsent}, nil
	}
	if err != nil {
		return nil, err
	}
	return &Result{Action: ActionObserved, Resource: latest}, nil
}

// Delete destroys the resource if it exists.
func (t *Target) Delete(ctx context.Context, rm acktypes.AWSResourceManager, hook Hook) (*Result, error) {
	addr := t.Address()
	latest, err := rm.ReadOne(ctx, t.desired)
	if isNotFound(err) {
		return &Result{Action: ActionAbsent}, nil
	}
	if err != nil {
		return nil, err
	}
	id, _, _ := Metadata(latest)
	emit(hook, EventDestroying, addr, id)
	// Issue the delete, then wait until the resource is actually gone before
	// reporting it deleted. Some resources delete asynchronously (an EKS cluster,
	// a NAT gateway), and a dependent -- e.g. the subnet that holds a NAT
	// gateway's network interface -- cannot be deleted until this one is fully
	// gone. Bounded by reconcileTimeout so a stuck delete can't hang forever.
	deadline := time.Now().Add(reconcileTimeout)
	issued, gone := false, false
	for time.Now().Before(deadline) && !gone {
		if !issued {
			_, derr := rm.Delete(ctx, latest)
			switch {
			case isNotFound(derr):
				gone = true
				continue
			case derr == nil:
				issued = true
			default:
				if _, ok := requeueAfter(derr); !ok {
					return nil, derr
				}
				issued = true
			}
		}
		if _, rerr := rm.ReadOne(ctx, latest); isNotFound(rerr) {
			gone = true
			continue
		} else if rerr != nil {
			if _, ok := requeueAfter(rerr); !ok {
				return nil, rerr
			}
		}
		if serr := sleep(ctx, pollInterval); serr != nil {
			return nil, serr
		}
	}
	if !gone {
		return &Result{Action: ActionDeleted}, errStillReconciling
	}
	emit(hook, EventDestroyed, addr, id)
	return &Result{Action: ActionDeleted}, nil
}

// Marshal renders an AWSResource (the latest known CR, spec+status) as YAML.
func Marshal(r acktypes.AWSResource) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	return yaml.Marshal(r.RuntimeObject())
}

// ResourceFromJSON materializes an AWSResource of this target's type from a
// stored CR (e.g. the object recorded in the state file). It is used to compare
// previously-recorded state against the live resource for drift detection.
func (t *Target) ResourceFromJSON(data []byte) (AWSResource, error) {
	obj := t.descriptor.EmptyRuntimeObject()
	if err := yaml.Unmarshal(data, obj); err != nil {
		return nil, err
	}
	return t.descriptor.ResourceFromRuntimeObject(obj), nil
}

// Difference is a single field-level discrepancy between two resources.
type Difference struct {
	Path     string // dotted field path, e.g. "Spec.RequestPayment.Payer"
	Recorded string // value in the first (recorded/state) resource
	Live     string // value in the second (live/observed) resource
}

// Diff compares a recorded resource against the live one and returns the
// field-level differences (drift). An empty slice means they agree.
func (t *Target) Diff(recorded, live AWSResource) []Difference {
	delta := t.descriptor.Delta(recorded, live)
	out := make([]Difference, 0, len(delta.Differences))
	for _, d := range delta.Differences {
		out = append(out, Difference{
			Path:     t.jsonPath(d.Path),
			Recorded: valueString(d.A),
			Live:     valueString(d.B),
		})
	}
	return out
}

// pathParts extracts the segments of an ackcompare.Path. The parts slice is
// unexported, but MarshalJSON exposes it.
func pathParts(p ackcompare.Path) []string {
	b, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	var wrap struct{ Parts []string }
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil
	}
	return wrap.Parts
}

// jsonPath renders a delta Path using the resource's KRM/JSON field names
// (e.g. "spec.requestPayment.payer") rather than Go struct field names
// (e.g. "Spec.RequestPayment.Payer").
func (t *Target) jsonPath(p ackcompare.Path) string {
	return jsonPathOf(t.desired.RuntimeObject(), p)
}

// jsonPathOf resolves each segment of p to its json tag by walking obj's Go type
// with reflection, falling back to a lowercased segment when a tag can't be
// resolved. obj is any generated resource object (its methods are never called,
// only its type is reflected), which keeps it unit-testable without a controller.
func jsonPathOf(obj any, p ackcompare.Path) string {
	parts := pathParts(p)
	cur := reflect.TypeOf(obj)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name, next := jsonFieldName(cur, part)
		out = append(out, name)
		cur = next
	}
	return strings.Join(out, ".")
}

// jsonFieldName returns the json tag name for the struct field named fieldName
// on type t (after dereferencing pointers/slices), plus that field's type for
// continuing the walk. Falls back to lowercasing when unresolvable.
func jsonFieldName(t reflect.Type, fieldName string) (string, reflect.Type) {
	for t != nil && (t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return lowerFirst(fieldName), nil
	}
	f, ok := t.FieldByName(fieldName)
	if !ok {
		return lowerFirst(fieldName), nil
	}
	name := strings.Split(f.Tag.Get("json"), ",")[0]
	if name == "" || name == "-" {
		name = lowerFirst(fieldName)
	}
	return name, f.Type
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// valueString renders a diff value compactly, dereferencing pointers.
func valueString(v any) string {
	if v == nil {
		return "<nil>"
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return "<nil>"
		}
		v = rv.Elem().Interface()
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

// PlanKind is the action a plan would take on a resource.
type PlanKind string

const (
	PlanCreate PlanKind = "create"
	PlanUpdate PlanKind = "update"
	PlanNoop   PlanKind = "noop"
)

// Change is a single planned field mutation (old -> new).
type Change struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// Plan is the read-only result of comparing desired config against live state.
type Plan struct {
	Address string   `json:"address"`
	Kind    PlanKind `json:"kind"`
	Changes []Change `json:"changes,omitempty"`
}

// Desired returns the desired resource as loaded from the manifest. Used to
// render the fields of a to-be-created resource in a plan.
func (t *Target) Desired() AWSResource { return t.desired }

// Plan computes what Apply would do, without mutating anything. It refreshes the
// live state and diffs it against the effective desired state.
func (t *Target) Plan(ctx context.Context, rm acktypes.AWSResourceManager) (*Plan, error) {
	live, err := rm.ReadOne(ctx, t.desired)
	if errors.Is(err, ackerr.NotFound) {
		return &Plan{Address: t.Address(), Kind: PlanCreate}, nil
	}
	if err != nil {
		return nil, err
	}

	eff, err := t.effectiveDesired(live)
	if err != nil {
		return nil, fmt.Errorf("computing effective desired state: %w", err)
	}
	// Delta(live, eff): A is the old (live) value, B is the new (desired) value.
	delta := t.descriptor.Delta(live, eff)
	if len(delta.Differences) == 0 {
		return &Plan{Address: t.Address(), Kind: PlanNoop}, nil
	}
	changes := make([]Change, 0, len(delta.Differences))
	for _, d := range delta.Differences {
		changes = append(changes, Change{
			Path: t.jsonPath(d.Path),
			Old:  valueString(d.A),
			New:  valueString(d.B),
		})
	}
	return &Plan{Address: t.Address(), Kind: PlanUpdate, Changes: changes}, nil
}

// RegisteredControllers returns how many controllers are wired.
func RegisteredControllers() int { return len(registries) }

// RegisteredKinds returns the total number of registered resource kinds.
func RegisteredKinds() int {
	n := 0
	for _, get := range registries {
		n += len(get())
	}
	return n
}

// RegisteredGVKs returns the GroupVersionKind of every registered resource.
func RegisteredGVKs() []schema.GroupVersionKind {
	var out []schema.GroupVersionKind
	for _, get := range registries {
		for _, f := range get() {
			out = append(out, f.ResourceDescriptor().GroupVersionKind())
		}
	}
	return out
}

// Metadata returns the AWS identifiers recorded on a resource: ARN, account,
// and region. Any may be empty if not yet populated by the service.
func Metadata(r AWSResource) (arn, account, region string) {
	if r == nil {
		return "", "", ""
	}
	ids := r.Identifiers()
	if ids == nil {
		return "", "", ""
	}
	if v := ids.ARN(); v != nil {
		arn = string(*v)
	}
	if v := ids.OwnerAccountID(); v != nil {
		account = string(*v)
	}
	if v := ids.Region(); v != nil {
		region = string(*v)
	}
	return arn, account, region
}

// ObjectJSON renders the resource's CR (spec + status) as JSON for storage.
func ObjectJSON(r AWSResource) (json.RawMessage, error) {
	if r == nil {
		return nil, nil
	}
	return json.Marshal(r.RuntimeObject())
}

// effectiveDesired computes the desired state that loack will actually try to
// enforce: the user's manifest fields take precedence, and every field the user
// left unset is filled from the observed resource. This is loack's stand-in for
// the controller's late-initialization step -- it stops the reconcile loop from
// fighting AWS over server-defaulted fields the user never declared, so apply
// converges and the persisted state stays stable across runs.
func (t *Target) effectiveDesired(latest AWSResource) (AWSResource, error) {
	desiredJSON, err := json.Marshal(t.desired.RuntimeObject())
	if err != nil {
		return nil, err
	}
	latestJSON, err := json.Marshal(latest.RuntimeObject())
	if err != nil {
		return nil, err
	}

	var dm, lm map[string]any
	if err := json.Unmarshal(desiredJSON, &dm); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(latestJSON, &lm); err != nil {
		return nil, err
	}

	dSpec, _ := dm["spec"].(map[string]any)
	lSpec, _ := lm["spec"].(map[string]any)

	effective := map[string]any{
		"apiVersion": dm["apiVersion"],
		"kind":       dm["kind"],
		"metadata":   dm["metadata"],
		"spec":       deepMerge(lSpec, dSpec), // user (dSpec) overrides observed (lSpec)
		// Carry the observed status so a follow-up Update has the server-assigned
		// identifiers (e.g. a subnet's Status.SubnetID, needed by the second-pass
		// ModifySubnetAttribute for mapPublicIPOnLaunch). Without it those calls
		// fail with "missing required field".
		"status": lm["status"],
	}
	effJSON, err := json.Marshal(effective)
	if err != nil {
		return nil, err
	}

	obj := t.descriptor.EmptyRuntimeObject()
	if err := json.Unmarshal(effJSON, obj); err != nil {
		return nil, err
	}
	return t.descriptor.ResourceFromRuntimeObject(obj), nil
}

// deepMerge returns base with over's keys layered on top, recursing into nested
// objects. Values present in over always win.
func deepMerge(base, over map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if bv, ok := out[k]; ok {
			if bm, ok1 := bv.(map[string]any); ok1 {
				if om, ok2 := v.(map[string]any); ok2 {
					out[k] = deepMerge(bm, om)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// lookup finds the registered factory + descriptor whose GVK matches the
// manifest's apiVersion/kind.
func lookup(apiVersion, kind string) (acktypes.AWSResourceManagerFactory, acktypes.AWSResourceDescriptor, error) {
	for _, getFactories := range registries {
		for _, f := range getFactories() {
			d := f.ResourceDescriptor()
			gvk := d.GroupVersionKind()
			if gvk.Kind == kind && apiVersion == gvk.GroupVersion().String() {
				return f, d, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("no registered resource manager for %s/%s", apiVersion, kind)
}
