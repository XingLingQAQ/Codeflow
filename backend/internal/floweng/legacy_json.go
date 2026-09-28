package floweng

// JSON codec for Flow documents (T3.01.a, §28 "不能丢未识别但仍需兼容的字段").
//
// A stored Flow document may have been written by a build older or newer than
// this one. Older documents simply lack members this build knows; newer ones may
// carry members it does not. encoding/json would silently drop every member it
// does not recognize, and it matches member names case-insensitively, so an
// unrecognized member such as "Status" is worse than lost: it is folded into the
// known `status` field and then re-encoded under that name with a value the
// stored document never held there. Neither is acceptable for a document this
// package will copy into the runtime database (T3.01.b), so Flow and its parts
// decode and encode through the rules below.
//
// Decoding one JSON object:
//
//   - The members are read one by one, each value kept as raw bytes. A member
//     whose key is byte-for-byte one of the type's JSON keys is a known field;
//     every other member — a case variant, a key from a newer build, a
//     misspelling — is an extra.
//   - The known members are reassembled into a JSON object and handed to
//     encoding/json as the plain alias type (flowJSON and its siblings: same
//     fields, no methods), so every normal rule still applies. Nested objects
//     are decoded by their own rules, so their members are classified against
//     their own known keys; a known member that does not fit its field is an
//     error exactly as before. A member the document does not mention leaves its
//     field at the zero value even when the destination was used before, because
//     every load resets its target first.
//   - Extras are kept verbatim (key and value bytes) in an unexported map and
//     carried, never interpreted: this build does not know what they mean.
//   - A key that appears twice inside one object is an error. The object holds
//     two values for one member and there is no defensible choice between them
//     (the same "do not guess" rule loadStoredEventIDs follows for a document it
//     cannot read). A body that is not exactly one JSON object, or that has
//     content after it, is an error too.
//
// Encoding one JSON object:
//
//   - The known fields are written exactly as encoding/json writes them today:
//     same keys, same order, same omitempty behavior, nested objects through
//     their own rules.
//   - The extras are appended after them in key order with their raw bytes
//     written through. A number such as 12345678901234567890.5 keeps every
//     digit: no float64 round trip, no reformatting.
//
// A document without extras therefore encodes byte for byte the way it does
// today, and decodes to the value the standard decoder would produce for
// canonical keys. TestLegacyFlowJSONMatchesStandardMarshal and
// TestLegacyFlowJSONMatchesStandardUnmarshal pin those two properties against
// shadow types that have the same fields and no methods.
//
// What "verbatim" can and cannot mean for extras, measured rather than assumed
// (TestLegacyFlowJSONPreservesUnknownValuesByteForByte): decoding keeps a value
// exactly as the document spelled it — json.RawMessage does not go through
// float64, so a number of any length survives digit for digit — but encoding/json
// runs the bytes a MarshalJSON returns through a compactor and its HTML escaper
// before they reach the caller. A value that was already compact and free of
// '<', '>' and '&' — a number, a list, a plain object — therefore comes back out
// byte for byte, while one that carried insignificant whitespace or one of
// those three characters comes back with them normalised. Everything that
// carries meaning survives: the key, the value, the nesting, and the spelling of
// every number (the precision that matters for identifiers and hashes). Nothing
// is dropped and nothing is reinterpreted; the normalisation is the outer
// encoder's, and it is the same one it applies to every other string in every
// document it writes.
//
// One asymmetry is deliberate. Flow, Stage, Artifact, FlowEvent and LoopEdge
// carry JSON methods; Gate does not. A struct that embeds Gate inherits a
// promoted method, and encoding/json then treats the *outer* object as the gate
// — api/handlers builds its flattened gate rows exactly that way (gateRow embeds
// floweng.Gate), so a Gate method would silently turn that response into a bare
// gate. Gate therefore has no method, and the stage codec handles the "gates"
// member itself, element by element, through loadGateDocument and
// encodeGateDocument. Everything that needs gate extras (a stored document, and
// the copy T3.01.b makes of it) goes through that path;
// TestLegacyGateHasNoJSONMethods pins the constraint.
//
// Two consequences are intended. Handlers serialize floweng.Flow directly, so an
// API response can now carry the fields the stored document had (kind, revision,
// binding_id, ... plus whatever extras the document preserved); consumers that
// ignore unknown keys are unaffected. And an absent value stays absent: extras
// are only ever carried, never invented.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
	"unicode/utf8"
)

// The known JSON key set of each type. Membership is exact, so no case variant
// is ever a known key. The alias types below declare the same keys as struct
// tags and TestLegacyFlowJSONAliasesMirrorFields pins the two representations
// together, so a field cannot become known to one half of the codec only.
func flowKnownMembers() map[string]bool {
	return memberSet(
		"id", "project_id", "session_id", "template_id", "status", "kind", "revision",
		"binding_id", "template_revision", "parent_project_flow_id",
		"stages", "loops", "artifacts", "events", "created_at", "updated_at",
	)
}

func stageKnownMembers() map[string]bool {
	return memberSet(
		"id", "type", "name", "canvas", "agent_id", "status", "optional",
		"snapshot_id", "gates", "order",
	)
}

func gateKnownMembers() map[string]bool {
	return memberSet("id", "phase", "kind", "on_fail", "config", "passed")
}

func artifactKnownMembers() map[string]bool {
	return memberSet(
		"id", "stage_id", "type", "version", "status", "created_by", "content_ref", "created_at",
	)
}

func flowEventKnownMembers() map[string]bool {
	return memberSet("id", "type", "stage_id", "message", "timestamp")
}

func loopEdgeKnownMembers() map[string]bool {
	return memberSet("from", "to")
}

func memberSet(keys ...string) map[string]bool {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	return set
}

// Type names used in error messages.
const (
	flowTypeName     = "flow"
	stageTypeName    = "stage"
	gateTypeName     = "gate"
	artifactTypeName = "artifact"
	eventTypeName    = "flow event"
	loopTypeName     = "loop edge"
)

// parseObjectMembers reads one JSON object into its members, each value as raw
// bytes. It refuses a body that is not exactly one JSON object, an object that
// names the same key twice, and content after the object.
//
// Values are taken with Decode into json.RawMessage rather than assembled from
// Tokens: that is the documented way to keep a value verbatim (a number never
// passes through float64), and it consumes exactly one value, so what follows
// the object is still decided by the Token call at the end.
func parseObjectMembers(body []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(body))
	tok, err := d.Token()
	if err != nil {
		return nil, fmt.Errorf("read JSON object: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("not a JSON object: %s", jsonPreview(body))
	}
	members := map[string]json.RawMessage{}
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("read JSON object: %w", err)
		}
		if delim, ok := tok.(json.Delim); ok {
			if delim != '}' {
				return nil, fmt.Errorf("not a JSON object: %s", jsonPreview(body))
			}
			break
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("not a JSON object: %s", jsonPreview(body))
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return nil, fmt.Errorf("read JSON value of %q: %w", key, err)
		}
		if _, dup := members[key]; dup {
			return nil, fmt.Errorf("duplicate JSON key %q", key)
		}
		members[key] = raw
	}
	// A second value after the object means the body is not one object. The
	// known trap: Decoder.More() answers false before a stray '}', so the check
	// has to consume tokens until io.EOF.
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing content after the JSON object: %v", err)
	}
	return members, nil
}

// splitDocumentMembers divides the members of one decoded object into the type's
// known fields and its extras. Both result maps are nil when empty, so a
// document without extras decodes to the natural zero value.
func splitDocumentMembers(members map[string]json.RawMessage, knownKeys map[string]bool) (known, extras map[string]json.RawMessage) {
	for k, v := range members {
		if knownKeys[k] {
			if known == nil {
				known = make(map[string]json.RawMessage, len(members))
			}
			known[k] = v
			continue
		}
		if extras == nil {
			extras = make(map[string]json.RawMessage)
		}
		extras[k] = v
	}
	return known, extras
}

// assembleKnownObject renders the known members as one JSON object for the
// standard decoder. Member order does not matter here (decoding is
// order-insensitive) and the values are raw, so nested objects still reach
// their own rules untouched.
func assembleKnownObject(known map[string]json.RawMessage) ([]byte, error) {
	out := make([]byte, 0, 2+len(known)*16)
	out = append(out, '{')
	for i, k := range sortedRawKeys(known) {
		if i > 0 {
			out = append(out, ',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("encode JSON key %q: %w", k, err)
		}
		out = append(out, key...)
		out = append(out, ':')
		out = append(out, known[k]...)
	}
	out = append(out, '}')
	return out, nil
}

// appendExtras writes the extras into an already encoded JSON object, in key
// order, immediately before the closing brace. Values are written through.
func appendExtras(obj []byte, extras map[string]json.RawMessage) ([]byte, error) {
	if len(extras) == 0 {
		return obj, nil
	}
	trimmed := bytes.TrimRight(obj, " \t\r\n")
	if len(trimmed) < 2 || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("cannot append extras to %s", jsonPreview(trimmed))
	}
	out := make([]byte, 0, len(trimmed)+len(extras)*16)
	out = append(out, trimmed[:len(trimmed)-1]...)
	if out[len(out)-1] != '{' {
		out = append(out, ',')
	}
	for i, k := range sortedRawKeys(extras) {
		if i > 0 {
			out = append(out, ',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("encode JSON key %q: %w", k, err)
		}
		out = append(out, key...)
		out = append(out, ':')
		value := extras[k]
		if len(value) == 0 {
			return nil, fmt.Errorf("extra %q has no value", k)
		}
		out = append(out, value...)
	}
	out = append(out, '}')
	return out, nil
}

func sortedRawKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// jsonPreview returns a short, rune-safe prefix of body for error messages.
func jsonPreview(body []byte) string {
	const max = 64
	if len(body) <= max {
		return string(body)
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return string(body[:cut]) + "..."
}

// decodeRawArray turns the raw value of an array member into one raw value per
// element. A null or absent member is an empty slice (the value a nil slice
// encodes to), so a caller can tell "no member" from "empty array" the way the
// standard decoder would.
func decodeRawArray(raw json.RawMessage, typeName string) ([]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode %s array: %w", typeName, err)
	}
	return items, nil
}

// loadDocumentObject decodes one JSON object of the given type into out (a
// pointer to the no-method alias type) and returns the extras it carried. The
// caller has already reset *out, so a member the document does not mention
// leaves its field at the zero value even when the destination was used before.
func loadDocumentObject(body []byte, out any, knownKeys map[string]bool, typeName string) (map[string]json.RawMessage, error) {
	members, err := parseObjectMembers(body)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", typeName, err)
	}
	known, extras := splitDocumentMembers(members, knownKeys)
	view, err := assembleKnownObject(known)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", typeName, err)
	}
	if err := json.Unmarshal(view, out); err != nil {
		return nil, fmt.Errorf("decode %s: %w", typeName, err)
	}
	return extras, nil
}

// normalizeFlowDocument applies the document invariant a Flow carries wherever
// it is written: a Flow with no kind is the project's flow of record (the state
// of every document written before T3.01, and of the engine's own fixtures), and
// a kind this build does not know is refused rather than guessed at.
//
// Both stores call it before writing, so a document that arrives without a kind
// is stored as kind=project and reads back as kind=project. Without it the
// flows.kind column would say project — that is its default — while the payload
// said nothing, and the two halves of the same row would disagree.
func normalizeFlowDocument(f *Flow) error {
	if f.Kind == "" {
		f.Kind = FlowKindProject
	}
	if !f.Kind.valid() {
		return fmt.Errorf("flow %s: unknown kind %q", f.ID, f.Kind)
	}
	return nil
}

// normalizeFlowFields applies the reading rules of a stored document: the
// document invariant above plus the revision default, because a document
// written before T3.01 has no revision and means revision 1.
func normalizeFlowFields(f *Flow) error {
	if err := normalizeFlowDocument(f); err != nil {
		return err
	}
	if f.Revision == 0 {
		f.Revision = 1
	}
	if f.Revision < 0 {
		return fmt.Errorf("flow %s: revision %d is not a revision", f.ID, f.Revision)
	}
	return nil
}

// The no-method aliases. Each repeats the fields of its type, including the
// unexported extras map, for two reasons: a conversion between the two types is
// only legal while the underlying types stay identical (so the extras map must
// be repeated too), and encoding/json must see a type without the JSON methods,
// or the methods would call themselves.
//
// stageJSON is the one alias that does not mirror its type literally: its Gates
// member holds the already-encoded gates, because Gate has no methods (see the
// file comment) and both directions therefore run through this codec
// explicitly. Field order and tags are still the stage's own, which is what the
// byte-identity test checks.
type flowJSON struct {
	ID                  string      `json:"id"`
	ProjectID           string      `json:"project_id"`
	SessionID           string      `json:"session_id,omitempty"`
	TemplateID          TemplateID  `json:"template_id"`
	Status              FlowStatus  `json:"status"`
	Kind                FlowKind    `json:"kind"`
	Revision            int64       `json:"revision"`
	BindingID           string      `json:"binding_id,omitempty"`
	TemplateRevision    int64       `json:"template_revision,omitempty"`
	ParentProjectFlowID string      `json:"parent_project_flow_id,omitempty"`
	Stages              []Stage     `json:"stages"`
	Loops               []LoopEdge  `json:"loops"`
	Artifacts           []Artifact  `json:"artifacts"`
	Events              []FlowEvent `json:"events"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
	extras              map[string]json.RawMessage
}

type stageJSON struct {
	ID         string            `json:"id"`
	Type       StageType         `json:"type"`
	Name       string            `json:"name"`
	Canvas     string            `json:"canvas"`
	AgentID    string            `json:"agent_id,omitempty"`
	Status     StageStatus       `json:"status"`
	Optional   bool              `json:"optional"`
	SnapshotID string            `json:"snapshot_id,omitempty"`
	Gates      []json.RawMessage `json:"gates,omitempty"`
	Order      int               `json:"order"`
	extras     map[string]json.RawMessage
}

type gateJSON struct {
	ID     string            `json:"id"`
	Phase  GatePhase         `json:"phase"`
	Kind   GateKind          `json:"kind"`
	OnFail GateOnFail        `json:"on_fail,omitempty"`
	Config map[string]string `json:"config,omitempty"`
	Passed bool              `json:"passed"`
	extras map[string]json.RawMessage
}

type artifactJSON struct {
	ID         string         `json:"id"`
	StageID    string         `json:"stage_id"`
	Type       string         `json:"type"`
	Version    int            `json:"version"`
	Status     ArtifactStatus `json:"status"`
	CreatedBy  string         `json:"created_by,omitempty"`
	ContentRef string         `json:"content_ref,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	extras     map[string]json.RawMessage
}

type flowEventJSON struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	StageID   string    `json:"stage_id,omitempty"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	extras    map[string]json.RawMessage
}

type loopEdgeJSON struct {
	From   StageType `json:"from"`
	To     StageType `json:"to"`
	extras map[string]json.RawMessage
}

// MarshalJSON encodes the flow with the document rules: known fields first,
// then the preserved extras.
func (f Flow) MarshalJSON() ([]byte, error) {
	return encodeFlowDocument(&f)
}

// UnmarshalJSON decodes the flow with the document rules.
func (f *Flow) UnmarshalJSON(body []byte) error {
	return loadFlowDocument(body, f)
}

// MarshalJSON encodes the stage with the document rules.
func (s Stage) MarshalJSON() ([]byte, error) {
	return encodeStageDocument(s)
}

// UnmarshalJSON decodes the stage with the document rules.
func (s *Stage) UnmarshalJSON(body []byte) error {
	return loadStageDocument(body, s)
}

// MarshalJSON encodes the artifact with the document rules.
func (a Artifact) MarshalJSON() ([]byte, error) {
	return encodeArtifactDocument(a)
}

// UnmarshalJSON decodes the artifact with the document rules.
func (a *Artifact) UnmarshalJSON(body []byte) error {
	return loadArtifactDocument(body, a)
}

// MarshalJSON encodes the flow event with the document rules.
func (e FlowEvent) MarshalJSON() ([]byte, error) {
	return encodeFlowEventDocument(e)
}

// UnmarshalJSON decodes the flow event with the document rules.
func (e *FlowEvent) UnmarshalJSON(body []byte) error {
	return loadFlowEventDocument(body, e)
}

// MarshalJSON encodes the loop edge with the document rules.
func (l LoopEdge) MarshalJSON() ([]byte, error) {
	return encodeLoopEdgeDocument(l)
}

// UnmarshalJSON decodes the loop edge with the document rules.
func (l *LoopEdge) UnmarshalJSON(body []byte) error {
	return loadLoopEdgeDocument(body, l)
}

// DecodeFlowDocument decodes a stored Flow document; it is the named entry
// point for the rules above and behaves exactly like json.Unmarshal into a
// *Flow, because Flow decodes with those rules wherever it is decoded.
func DecodeFlowDocument(body []byte, f *Flow) error {
	if f == nil {
		return fmt.Errorf("decode %s: destination is nil", flowTypeName)
	}
	return json.Unmarshal(body, f)
}

// EncodeFlowDocument encodes a Flow document; it is the named entry point for
// the rules above and behaves exactly like json.Marshal of a Flow value,
// including the normalisation of preserved values that the outer encoder
// performs (see the file comment).
func EncodeFlowDocument(f *Flow) ([]byte, error) {
	return json.Marshal(f)
}

// CloneFlowDocument returns a deep copy of f: the document slices, the gate
// config maps and every preserved extra are copied, so a caller can change one
// document without changing the other. It is the copy the stores hand out, and
// the extras are included on purpose — T3.01.b copies a document into the
// runtime database and must not lose the keys it does not understand.
func CloneFlowDocument(f *Flow) *Flow {
	return cloneFlow(f)
}
