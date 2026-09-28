package run

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

// This file is the construction half of the Run command service (T1.04.a). It
// defines the dependency set the service is built from and proves — once, at
// construction — that every dependency is present.
//
// The rule this file exists to make true: a missing dependency must never be
// discovered by a nil-pointer panic in the middle of a command, after a
// transaction has been opened and a key claimed. §28 T1.04.a asks for
// "constructor validation, no nil dependency may succeed silently", so
// NewService checks every field and refuses to return a service at all when one
// is missing.
//
// What this step deliberately does not do: it implements no command method and
// leaves no "not implemented" stub behind. §29.1 item 6 forbids returning a
// fake success, and a stub that returns an error is not a command either; the
// methods arrive in T1.04.b together with their tests.

// ErrMissingDependency is returned, joined into the construction error, when
// NewService is called with a missing dependency. Callers should not match it
// by string: use errors.Is to detect the class, or errors.As with
// *MissingDependencyError to learn which fields were missing.
var ErrMissingDependency = errors.New("run: missing service dependency")

// ErrCommandKeyReused is returned by CommandLedger.Execute when a key is
// presented again with a different request hash. It is declared here because the
// ledger contract is owned by this package; the implementation returns this
// value and the transport layer maps it to its own conflict response.
var ErrCommandKeyReused = errors.New("run: idempotency key reused with a different request")

// MissingDependencyError names one ServiceDeps field that NewService refused.
// It is a value, not just a string, so that a caller can list every missing
// dependency programmatically instead of parsing an error message.
type MissingDependencyError struct {
	// Field is the ServiceDeps field name, e.g. "Store" or "Clock".
	Field string
	// Reason is "nil" for an unset interface or an unset function, and "typed
	// nil" for an interface that holds a nil pointer, map, slice or channel: the
	// field is not usable, but a naive check against the interface would miss
	// it. The two function dependencies have only the "nil" case — a func value
	// has no second way of being unusable — so Clock and NewID never report
	// "typed nil".
	Reason string
}

// Error implements error.
func (e *MissingDependencyError) Error() string {
	return fmt.Sprintf("%s: %s is %s", ErrMissingDependency, e.Field, e.Reason)
}

// Unwrap reports ErrMissingDependency, so errors.Is works on a construction
// error even when only one dependency is missing.
func (e *MissingDependencyError) Unwrap() error { return ErrMissingDependency }

// ServiceDeps is the complete dependency set of the Run command service. Every
// field is required: there are no implicit defaults and no optional
// dependencies, because a default would hide the same wiring mistake that the
// nil check catches. NewService refuses to build a service until all of them
// are present.
//
// The field order is the order NewService reports missing fields in, and the
// order the construction tests iterate: a field added anywhere but the end
// still has to be covered by them.
type ServiceDeps struct {
	// Store opens the transaction one command is atomic in.
	Store RunStore
	// Commands is the idempotency ledger of write commands (§27.3).
	Commands CommandLedger
	// Projects reads the project and its primary binding.
	Projects ProjectResolver
	// Flows reads the legacy flow and stage a task belongs to.
	Flows FlowResolver
	// Agents reads the agent revision a run pins.
	Agents AgentResolver
	// Backends reports what the selected execution backend supports.
	Backends BackendCatalog
	// Baselines captures the workspace baseline a run freezes.
	Baselines BaselineCapturer
	// Policy authorises the command before anything is written.
	Policy PolicyGate
	// Clock returns the instant a command happened at. It is injected rather
	// than called through time.Now so tests can pin the timestamps.
	Clock func() time.Time
	// NewID generates an id for a new runtime resource; kind names the resource
	// family ("run", "snapshot", ...) so an implementation can prefix it.
	NewID func(kind string) string
}

// Service is the Run command service. It holds the dependencies NewService
// verified; nothing else in this package constructs one.
//
// The command methods are T1.04.b. They are absent here on purpose: §29.1 item 6
// forbids a stub that pretends to succeed, and a half-written command returning
// "not implemented" would be a second place for the command contract to drift
// from handlers.RunCommand.
type Service struct {
	store     RunStore
	commands  CommandLedger
	projects  ProjectResolver
	flows     FlowResolver
	agents    AgentResolver
	backends  BackendCatalog
	baselines BaselineCapturer
	policy    PolicyGate
	clock     func() time.Time
	newID     func(kind string) string
}

// NewService builds a Service from deps.
//
// Every field of deps is checked, in declaration order. A field is missing when
// it is nil, or when it is an interface holding a typed nil — a (*runstore.Store)(nil)
// inside a RunStore interface is not nil, so the naive check would let it
// through and the first command using it would panic or, worse, silently do
// nothing. Both cases produce one *MissingDependencyError with Reason "nil" or
// "typed nil" respectively.
//
// If anything is missing the function returns a nil *Service and an error that
// joins every missing field — all of them, not just the first, so one call tells
// the caller everything it has to wire. The joined error satisfies
// errors.Is(err, ErrMissingDependency), and errors.As finds the first
// *MissingDependencyError.
func NewService(deps ServiceDeps) (*Service, error) {
	var missing []error
	for _, f := range serviceDepFields(deps) {
		if f.isNil() {
			missing = append(missing, &MissingDependencyError{Field: f.name, Reason: f.reason()})
		}
	}
	if len(missing) > 0 {
		return nil, errors.Join(missing...)
	}
	return &Service{
		store:     deps.Store,
		commands:  deps.Commands,
		projects:  deps.Projects,
		flows:     deps.Flows,
		agents:    deps.Agents,
		backends:  deps.Backends,
		baselines: deps.Baselines,
		policy:    deps.Policy,
		clock:     deps.Clock,
		newID:     deps.NewID,
	}, nil
}

// depField is one ServiceDeps field as a value the nil check can look at without
// knowing its concrete type. The check is written as a static list rather than a
// reflect walk of ServiceDeps so that it names every field explicitly: a field
// renamed here fails to compile, while a reflect walk would keep compiling while
// checking the wrong thing. serviceDepFields is the single place that list is
// spelled out, and service_test.go holds it to ServiceDeps by reflection.
type depField struct {
	// name is the ServiceDeps field name, used verbatim in
	// MissingDependencyError.Field. It is not the name of the interface type:
	// the field is what a caller must assign to.
	name string
	// value is the field as an interface. It is nil for an unset interface; for
	// a set interface it is the interface value itself, so an interface holding a
	// nil pointer keeps its dynamic type and is recognised below.
	value any
}

// isNil reports whether the dependency is unusable.
func (f depField) isNil() bool {
	_, missing := dependencyMissingReason(f.value)
	return missing
}

// reason names why the dependency is unusable, in the vocabulary of
// MissingDependencyError.Reason. It is only called once isNil returned true.
func (f depField) reason() string {
	reason, _ := dependencyMissingReason(f.value)
	return reason
}

// dependencyMissingReason decides, in one place, whether one dependency value is
// usable and what to call its absence. Splitting this between isNil and reason
// would be the easy way to report a nil function as "typed nil", or a typed nil
// store as present.
func dependencyMissingReason(value any) (string, bool) {
	if value == nil {
		return "nil", true
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Func {
		// A function dependency has exactly one way of being absent: a nil
		// func. It reaches this table as a non-nil interface purely because it
		// crossed a conversion to any, which is an artefact of how the fields
		// are collected and not a second kind of absence, so it is reported as
		// "nil" — unlike a nil pointer, which really is a value of the right
		// type that no caller supplied.
		if rv.IsNil() {
			return "nil", true
		}
		return "", false
	}
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan:
		if rv.IsNil() {
			return "typed nil", true
		}
	}
	return "", false
}

// serviceDepFields returns every ServiceDeps field as a depField, in
// declaration order.
func serviceDepFields(deps ServiceDeps) []depField {
	return []depField{
		{name: "Store", value: deps.Store},
		{name: "Commands", value: deps.Commands},
		{name: "Projects", value: deps.Projects},
		{name: "Flows", value: deps.Flows},
		{name: "Agents", value: deps.Agents},
		{name: "Backends", value: deps.Backends},
		{name: "Baselines", value: deps.Baselines},
		{name: "Policy", value: deps.Policy},
		{name: "Clock", value: deps.Clock},
		{name: "NewID", value: deps.NewID},
	}
}
