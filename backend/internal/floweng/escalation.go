package floweng

import "context"

// GateEscalationHandler is an optional callback invoked after a gate rejection
// with on_fail=escalate_to_debate is persisted. The call is synchronous from
// DecideGate's caller perspective, but it runs after the engine lock is released
// so implementations may safely re-enter the engine. The flow, stage, and gate
// arguments are independent clones; mutating them has no effect on engine state
// or on the other callback arguments. The handler's return value is not checked.
type GateEscalationHandler interface {
	OnGateEscalation(ctx context.Context, flow *Flow, stage *Stage, gate *Gate, reason string)
}

// GateEscalationFunc adapts a plain function to the GateEscalationHandler
// interface (cf. http.HandlerFunc).
type GateEscalationFunc func(ctx context.Context, flow *Flow, stage *Stage, gate *Gate, reason string)

// OnGateEscalation implements GateEscalationHandler.
func (f GateEscalationFunc) OnGateEscalation(ctx context.Context, flow *Flow, stage *Stage, gate *Gate, reason string) {
	f(ctx, flow, stage, gate, reason)
}
