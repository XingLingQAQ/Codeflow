package floweng

import "context"

// GateEscalationHandler is an optional callback invoked after a gate rejection
// with on_fail=escalate_to_debate is persisted. Implementations must be fast or
// dispatch async themselves — the call is synchronous under the engine lock and
// the handler's return value is not checked (fire-and-forget). The flow, stage,
// and gate arguments are clones; mutating them has no effect on engine state.
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
