package run

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This file tests the construction contract of T1.04.a: the dependency set is
// complete or there is no service. Three properties are load-bearing and are
// each pinned by a test that does not depend on the others:
//
//   - Every ServiceDeps field is checked. The test walks ServiceDeps by
//     reflection and zeroes one field at a time, so a field added later is
//     covered without editing the table — and fails until NewService checks it.
//   - A typed nil is refused too. An interface holding (*fakeStore)(nil) is not
//     nil, so this is the case a naive check silently accepts.
//   - The package stays standard-library-only. The import guard parses the
//     non-test sources and fails on any import whose first path segment is a
//     dotted domain, which is what importing runstore or handlers would look
//     like.

// The fakes below are the smallest types that satisfy each dependency. They
// return zero values: no command logic exists yet (T1.04.b), so the construction
// tests only need values that are not nil.
type (
	fakeStore     struct{}
	fakeTx        struct{}
	fakeLedger    struct{}
	fakeProjects  struct{}
	fakeFlows     struct{}
	fakeAgents    struct{}
	fakeBackends  struct{}
	fakeBaselines struct{}
	fakePolicy    struct{}
)

// Compile-time proof that the fakes satisfy the interfaces the tests hand to
// NewService. A signature change in service_deps.go breaks this file instead of
// silently testing nothing.
var (
	_ RunStore         = (*fakeStore)(nil)
	_ RunTx            = (*fakeTx)(nil)
	_ CommandLedger    = (*fakeLedger)(nil)
	_ ProjectResolver  = (*fakeProjects)(nil)
	_ FlowResolver     = (*fakeFlows)(nil)
	_ AgentResolver    = (*fakeAgents)(nil)
	_ BackendCatalog   = (*fakeBackends)(nil)
	_ BaselineCapturer = (*fakeBaselines)(nil)
	_ PolicyGate       = (*fakePolicy)(nil)
	_ AnswerEncoder    = fakeEncode
	_ error            = (*MissingDependencyError)(nil)
)

func (*fakeStore) WithinTx(ctx context.Context, fn func(ctx context.Context, tx RunTx) error) error {
	return fn(ctx, &fakeTx{})
}

func (*fakeTx) GetTask(context.Context, string) (Task, error)       { return Task{}, nil }
func (*fakeTx) GetRun(context.Context, string) (Run, error)         { return Run{}, nil }
func (*fakeTx) GetAttempt(context.Context, string) (Attempt, error) { return Attempt{}, nil }

func (*fakeTx) ListRunsByTask(context.Context, string) ([]Run, error) { return nil, nil }

func (*fakeTx) ListRunsByProject(context.Context, string, int) ([]Run, error) { return nil, nil }

func (*fakeTx) InsertInputSnapshot(context.Context, string, string, json.RawMessage, time.Time) (string, error) {
	return "", nil
}

func (*fakeTx) InsertRun(context.Context, *Run) error { return nil }

func (*fakeTx) TransitionRun(context.Context, RunTransitionInput) (RunTransitionResult, error) {
	return RunTransitionResult{}, nil
}

func (*fakeTx) RetryRun(context.Context, RetryRunInput) (RetryRunResult, error) {
	return RetryRunResult{}, nil
}

func (*fakeLedger) Execute(context.Context, CommandKey, string, func(context.Context, RunTx) (CommandAnswer, error)) (CommandAnswer, CommandDisposition, error) {
	return CommandAnswer{}, CommandExecuted, nil
}

func (*fakeProjects) ResolveProject(context.Context, string) (ProjectSnapshot, error) {
	return ProjectSnapshot{}, nil
}

func (*fakeProjects) PrimaryBinding(context.Context, string) (BindingSnapshot, error) {
	return BindingSnapshot{}, nil
}

func (*fakeFlows) ResolveTaskParent(context.Context, string, Task) (FlowParent, error) {
	return FlowParent{}, nil
}

func (*fakeAgents) ResolveAgentRevision(context.Context, string, string) (AgentRevisionSnapshot, error) {
	return AgentRevisionSnapshot{}, nil
}

func (*fakeBackends) BackendCapabilities(context.Context, string) (BackendCapabilitySnapshot, error) {
	return BackendCapabilitySnapshot{}, nil
}

func (*fakeBaselines) CaptureBaseline(context.Context, BindingSnapshot) (BaselineSnapshot, error) {
	return BaselineSnapshot{}, nil
}

func (*fakePolicy) AuthorizeRunCommand(context.Context, RunPolicyRequest) (PolicyVerdict, error) {
	return PolicyVerdict{Allowed: true}, nil
}

func fakeEncode(CommandResponse) (CommandAnswer, error) { return CommandAnswer{}, nil }

// usableDeps returns a complete, usable dependency set. Every construction test
// starts from it and breaks exactly one thing, so a failure names the field
// under test rather than a setup mistake.
func usableDeps() ServiceDeps {
	return ServiceDeps{
		Store:     &fakeStore{},
		Commands:  &fakeLedger{},
		Projects:  &fakeProjects{},
		Flows:     &fakeFlows{},
		Agents:    &fakeAgents{},
		Backends:  &fakeBackends{},
		Baselines: &fakeBaselines{},
		Policy:    &fakePolicy{},
		Clock:     func() time.Time { return time.UnixMilli(0).UTC() },
		NewID:     func(kind string) string { return kind + "_test" },
	}
}

// missingFields returns the *MissingDependencyError values inside err, in the
// order NewService joined them.
func missingFields(t *testing.T, err error) []*MissingDependencyError {
	t.Helper()
	if err == nil {
		t.Fatal("NewService returned no error, want the missing dependencies")
	}
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatalf("error %v does not join the missing dependencies", err)
	}
	out := make([]*MissingDependencyError, 0, len(joined.Unwrap()))
	for _, e := range joined.Unwrap() {
		var mde *MissingDependencyError
		if !errors.As(e, &mde) {
			t.Fatalf("joined error %v is not a *MissingDependencyError", e)
		}
		out = append(out, mde)
	}
	return out
}

// TestServiceDepsFieldOrder pins serviceDepFields to ServiceDeps by reflection:
// the two must name the same fields in the same order, or the nil check is
// testing a set the production code does not use.
func TestServiceDepsFieldOrder(t *testing.T) {
	deps := usableDeps()
	want := make([]string, 0, reflect.TypeOf(deps).NumField())
	for i := range reflect.TypeOf(deps).NumField() {
		want = append(want, reflect.TypeOf(deps).Field(i).Name)
	}

	got := make([]string, 0, len(want))
	for _, f := range serviceDepFields(deps) {
		got = append(got, f.name)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("serviceDepFields names %v, ServiceDeps declares %v", got, want)
	}
}

// TestNewServiceAcceptsCompleteDeps is the positive case: a fully wired service
// is built and holds every dependency.
func TestNewServiceAcceptsCompleteDeps(t *testing.T) {
	deps := usableDeps()

	svc, err := NewService(deps)
	if err != nil {
		t.Fatalf("NewService(complete deps) = %v, want nil error", err)
	}
	if svc == nil {
		t.Fatal("NewService(complete deps) returned a nil *Service")
	}

	if got := reflect.ValueOf(svc).Elem(); got.NumField() != len(serviceDepFields(deps)) {
		t.Fatalf("Service holds %d dependencies, ServiceDeps declares %d", got.NumField(), len(serviceDepFields(deps)))
	}
	if svc.clock == nil || svc.newID == nil {
		t.Fatal("the service did not keep the injected Clock/NewID")
	}
	if now := svc.clock(); !now.Equal(time.UnixMilli(0).UTC()) {
		t.Fatalf("the service clock is not the injected one: got %v", now)
	}
	if id := svc.newID("run"); id != "run_test" {
		t.Fatalf("the service NewID is not the injected one: got %q", id)
	}
}

// TestNewServiceRejectsEachNilField zeroes one ServiceDeps field at a time,
// by reflection, and requires NewService to name that field. A field added to
// ServiceDeps without a check fails here.
func TestNewServiceRejectsEachNilField(t *testing.T) {
	deps := usableDeps()
	typ := reflect.TypeOf(deps)

	for i := range typ.NumField() {
		name := typ.Field(i).Name
		t.Run(name, func(t *testing.T) {
			broken := usableDeps()
			v := reflect.ValueOf(&broken).Elem().Field(i)
			v.Set(reflect.Zero(v.Type()))

			svc, err := NewService(broken)
			if svc != nil {
				t.Fatalf("NewService with a nil %s returned a non-nil *Service, want nil", name)
			}
			got := missingFields(t, err)
			if len(got) != 1 {
				t.Fatalf("missing %s: got %d missing dependencies %v, want exactly 1", name, len(got), got)
			}
			if got[0].Field != name || got[0].Reason != "nil" {
				t.Fatalf("missing %s: got %+v, want Field=%q Reason=%q", name, got[0], name, "nil")
			}
			if !errors.Is(err, ErrMissingDependency) {
				t.Fatalf("error %v does not match ErrMissingDependency", err)
			}
			var mde *MissingDependencyError
			if !errors.As(err, &mde) {
				t.Fatalf("error %v does not yield a *MissingDependencyError", err)
			}
		})
	}
}

// typedNilByField maps each interface field of ServiceDeps to a typed nil of a
// type that implements it. It is checked by reflection below, so a field renamed
// or added fails the test instead of being skipped.
var typedNilByField = map[string]any{
	"Store":     (*fakeStore)(nil),
	"Commands":  (*fakeLedger)(nil),
	"Projects":  (*fakeProjects)(nil),
	"Flows":     (*fakeFlows)(nil),
	"Agents":    (*fakeAgents)(nil),
	"Backends":  (*fakeBackends)(nil),
	"Baselines": (*fakeBaselines)(nil),
	"Policy":    (*fakePolicy)(nil),
}

// TestTypedNilTableCoversInterfaceFields keeps the typed-nil table and
// ServiceDeps in step: every interface field needs an entry, and the entry must
// actually implement the field's interface. The two function fields (Clock,
// NewID) are the only non-interface fields and are asserted to be exactly that.
func TestTypedNilTableCoversInterfaceFields(t *testing.T) {
	typ := reflect.TypeOf(usableDeps())
	for i := range typ.NumField() {
		f := typ.Field(i)
		switch f.Type.Kind() {
		case reflect.Interface:
			val, ok := typedNilByField[f.Name]
			if !ok {
				t.Fatalf("ServiceDeps.%s is an interface with no typed-nil test entry", f.Name)
			}
			if val == nil {
				t.Fatalf("typedNilByField[%q] is an untyped nil: it would test the plain nil case", f.Name)
			}
			if !reflect.TypeOf(val).Implements(f.Type) {
				t.Fatalf("typedNilByField[%q] is %T, which does not implement %s", f.Name, val, f.Type)
			}
		case reflect.Func:
			if f.Name != "Clock" && f.Name != "NewID" {
				t.Fatalf("ServiceDeps.%s is a function dependency this test does not cover", f.Name)
			}
			if _, ok := typedNilByField[f.Name]; ok {
				t.Fatalf("typedNilByField[%q] exists for a function field", f.Name)
			}
		default:
			t.Fatalf("ServiceDeps.%s has unexpected kind %s", f.Name, f.Type.Kind())
		}
	}
	if len(typedNilByField) != 8 {
		t.Fatalf("typedNilByField has %d entries, want 8", len(typedNilByField))
	}
}

// TestNewServiceRejectsEachTypedNilField is the case a naive check misses: an
// interface that is filled with a typed nil pointer. NewService must refuse it
// and say "typed nil", because the field is not usable even though a nil check
// passes.
func TestNewServiceRejectsEachTypedNilField(t *testing.T) {
	typ := reflect.TypeOf(usableDeps())
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		val, ok := typedNilByField[name]
		if !ok {
			continue // a function field: no typed nil exists, see the table test
		}
		t.Run(name, func(t *testing.T) {
			broken := usableDeps()
			v := reflect.ValueOf(&broken).Elem().Field(i)
			v.Set(reflect.ValueOf(val))

			svc, err := NewService(broken)
			if svc != nil {
				t.Fatalf("NewService with a typed nil %s returned a non-nil *Service, want nil", name)
			}
			got := missingFields(t, err)
			if len(got) != 1 {
				t.Fatalf("typed nil %s: got %d missing dependencies %v, want exactly 1", name, len(got), got)
			}
			if got[0].Field != name || got[0].Reason != "typed nil" {
				t.Fatalf("typed nil %s: got %+v, want Field=%q Reason=%q", name, got[0], name, "typed nil")
			}
		})
	}
}

// TestNewServiceReportsEveryMissingFieldInOrder requires the refusal to be
// complete and deterministic: all missing fields, in ServiceDeps order, joined
// into one error.
func TestNewServiceReportsEveryMissingFieldInOrder(t *testing.T) {
	deps := usableDeps()
	deps.Store = nil
	deps.Flows = nil
	deps.Policy = nil

	svc, err := NewService(deps)
	if svc != nil {
		t.Fatal("NewService with three missing dependencies returned a non-nil *Service, want nil")
	}

	got := missingFields(t, err)
	want := []string{"Store", "Flows", "Policy"}
	if len(got) != len(want) {
		t.Fatalf("got %d missing dependencies %v, want %v", len(got), got, want)
	}
	for i, name := range want {
		if got[i].Field != name {
			t.Fatalf("missing dependency %d is %q, want %q (order must follow ServiceDeps)", i, got[i].Field, name)
		}
		if got[i].Reason != "nil" {
			t.Fatalf("missing dependency %d has Reason %q, want %q", i, got[i].Reason, "nil")
		}
	}

	if !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("error %v does not match ErrMissingDependency", err)
	}
	var mde *MissingDependencyError
	if !errors.As(err, &mde) {
		t.Fatalf("error %v does not yield a *MissingDependencyError", err)
	}
	if mde.Field != "Store" {
		t.Fatalf("errors.As yielded %q, want the first missing field %q", mde.Field, "Store")
	}
	for _, name := range want {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error text %q does not name %q", err.Error(), name)
		}
	}
}

// TestMissingDependencyErrorUnwrap keeps Unwrap and Error honest: the sentinel
// must be reachable, and the message must name the field and the reason.
func TestMissingDependencyErrorUnwrap(t *testing.T) {
	err := &MissingDependencyError{Field: "Policy", Reason: "typed nil"}

	if !errors.Is(err, ErrMissingDependency) {
		t.Fatal("MissingDependencyError does not unwrap to ErrMissingDependency")
	}
	if errors.Is(err, ErrCommandKeyReused) {
		t.Fatal("MissingDependencyError matches ErrCommandKeyReused")
	}
	for _, want := range []string{"Policy", "typed nil", ErrMissingDependency.Error()} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error text %q does not contain %q", err.Error(), want)
		}
	}
}

// TestRunPackageImportsOnlyStandardLibrary enforces the dependency direction the
// package comment freezes: run imports only the standard library, so runstore,
// handlers and every other business package stay out. A dotted first path
// segment is exactly what such an import looks like, which is why the check is
// on the segment rather than on a blocklist of package names.
func TestRunPackageImportsOnlyStandardLibrary(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	files := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files++
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquote import %s: %v", name, imp.Path.Value, err)
			}
			segment, _, _ := strings.Cut(path, "/")
			if strings.Contains(segment, ".") {
				t.Errorf("%s imports %q: the first path segment contains a dot, so run no longer imports only the standard library", name, path)
			}
		}
	}
	if files == 0 {
		t.Fatal("no non-test .go files found: the import guard checked nothing")
	}
}
