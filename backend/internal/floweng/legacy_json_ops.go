package floweng

// Per-type decoding and encoding. The rules themselves are in legacy_json.go;
// this file only spells them once per document node. Gate has no JSON methods
// (an embedded Gate would otherwise take over its outer object's encoding), so
// stages call the gate functions directly — see loadStageDocument.

import (
	"encoding/json"
	"fmt"
)

// loadFlowDocument decodes one Flow object. The extras it kept stay on the
// value, so the document can be written back out with everything it carried.
func loadFlowDocument(body []byte, f *Flow) error {
	*f = Flow{}
	extras, err := loadDocumentObject(body, (*flowJSON)(f), flowKnownMembers(), flowTypeName)
	if err != nil {
		return err
	}
	f.extras = extras
	return normalizeFlowFields(f)
}

// encodeFlowDocument encodes one Flow object: known fields, then extras.
func encodeFlowDocument(f *Flow) ([]byte, error) {
	if f == nil {
		return []byte("null"), nil
	}
	base, err := json.Marshal(flowJSON(*f))
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", flowTypeName, err)
	}
	return appendExtras(base, f.extras)
}

// loadStageDocument decodes one Stage object and then its gates, one raw value
// at a time, so a gate carrying members this build does not know keeps them.
//
// The known stage members go through the alias and are then copied field by
// field, rather than converted: stageJSON's Gates member holds the raw gates so
// that both directions can reach the gate codec, which makes the two struct
// types not identical (the field types differ) and a conversion illegal. The
// copy is the price of leaving Gate method-free, and
// TestLegacyFlowJSONAliasesMirrorFields fails if a stage field is ever added to
// one side only.
func loadStageDocument(body []byte, s *Stage) error {
	*s = Stage{}
	var view stageJSON
	extras, err := loadDocumentObject(body, &view, stageKnownMembers(), stageTypeName)
	if err != nil {
		return err
	}
	gates := make([]Gate, 0, len(view.Gates))
	for i, raw := range view.Gates {
		var gate Gate
		if err := loadGateDocument(raw, &gate); err != nil {
			return fmt.Errorf("decode %s: gate %d: %w", stageTypeName, i, err)
		}
		gates = append(gates, gate)
	}
	s.ID = view.ID
	s.Type = view.Type
	s.Name = view.Name
	s.Canvas = view.Canvas
	s.AgentID = view.AgentID
	s.Status = view.Status
	s.Optional = view.Optional
	s.SnapshotID = view.SnapshotID
	s.Order = view.Order
	s.Gates = gates
	s.extras = extras
	return nil
}

func encodeStageDocument(s Stage) ([]byte, error) {
	view := stageJSON{
		ID:         s.ID,
		Type:       s.Type,
		Name:       s.Name,
		Canvas:     s.Canvas,
		AgentID:    s.AgentID,
		Status:     s.Status,
		Optional:   s.Optional,
		SnapshotID: s.SnapshotID,
		Order:      s.Order,
		extras:     s.extras,
	}
	if len(s.Gates) > 0 {
		gates := make([]json.RawMessage, len(s.Gates))
		for i := range s.Gates {
			raw, err := encodeGateDocument(s.Gates[i])
			if err != nil {
				return nil, fmt.Errorf("encode %s: gate %d: %w", stageTypeName, i, err)
			}
			gates[i] = raw
		}
		view.Gates = gates
	}
	base, err := json.Marshal(view)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", stageTypeName, err)
	}
	return appendExtras(base, s.extras)
}

// loadGateDocument and encodeGateDocument are the gate half of the codec. They
// are called by the stage functions and by nothing else outside this package's
// tests: Gate deliberately has no JSON methods.
func loadGateDocument(body []byte, g *Gate) error {
	*g = Gate{}
	extras, err := loadDocumentObject(body, (*gateJSON)(g), gateKnownMembers(), gateTypeName)
	if err != nil {
		return err
	}
	g.extras = extras
	return nil
}

func encodeGateDocument(g Gate) ([]byte, error) {
	base, err := json.Marshal(gateJSON(g))
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", gateTypeName, err)
	}
	return appendExtras(base, g.extras)
}

func loadArtifactDocument(body []byte, a *Artifact) error {
	*a = Artifact{}
	extras, err := loadDocumentObject(body, (*artifactJSON)(a), artifactKnownMembers(), artifactTypeName)
	if err != nil {
		return err
	}
	a.extras = extras
	return nil
}

func encodeArtifactDocument(a Artifact) ([]byte, error) {
	base, err := json.Marshal(artifactJSON(a))
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", artifactTypeName, err)
	}
	return appendExtras(base, a.extras)
}

func loadFlowEventDocument(body []byte, e *FlowEvent) error {
	*e = FlowEvent{}
	extras, err := loadDocumentObject(body, (*flowEventJSON)(e), flowEventKnownMembers(), eventTypeName)
	if err != nil {
		return err
	}
	e.extras = extras
	return nil
}

func encodeFlowEventDocument(e FlowEvent) ([]byte, error) {
	base, err := json.Marshal(flowEventJSON(e))
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", eventTypeName, err)
	}
	return appendExtras(base, e.extras)
}

func loadLoopEdgeDocument(body []byte, l *LoopEdge) error {
	*l = LoopEdge{}
	extras, err := loadDocumentObject(body, (*loopEdgeJSON)(l), loopEdgeKnownMembers(), loopTypeName)
	if err != nil {
		return err
	}
	l.extras = extras
	return nil
}

func encodeLoopEdgeDocument(l LoopEdge) ([]byte, error) {
	base, err := json.Marshal(loopEdgeJSON(l))
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", loopTypeName, err)
	}
	return appendExtras(base, l.extras)
}
