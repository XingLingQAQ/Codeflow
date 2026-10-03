package floweng

// Immutable template revisions (T3.01.a group 3, §28).
//
// A template — built-in like new_project, or a custom one saved through
// SaveTemplate — used to be a single current definition: saving over a custom
// template replaced its payload and nothing recorded what it looked like before.
// A Flow, meanwhile, is an instance of a template, and "which template did this
// flow come from, exactly?" has to survive edits and deletions, because T3.01.b
// copies Flows into the runtime database and has to check the copy against the
// template it was built from.
//
// So a template definition is now frozen into a revision: an append-only row
// holding the canonical JSON of the definition and its content hash. The table
// refuses UPDATE and DELETE from SQLite itself (triggers), so immutability is a
// property of the database, not of this package's discipline. Saving a custom
// template whose content did not change appends nothing (saving twice is
// idempotent); changing one field appends the next revision; deleting the
// template leaves every revision in place, because flows still point at them.
//
// Built-in templates have revisions too: opening a database ensures each
// built-in template's current code definition has a row. A definition that has
// not changed writes nothing on the next open, and one that has changed appends
// the next revision, so a flow created under an older build can still name the
// exact template it was built from.
//
// The canonical encoding is a private JSON document — fixed field order, sorted
// map keys, arrays in order — and the content hash is "sha256:" + hex of it.
// Two encodings of the same definition are byte-for-byte identical, so the hash
// is comparable across processes and machines.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TemplateRevisionSource says where a revision came from: the code (builtin) or
// a saved custom definition (custom). It is the value of the source column.
type TemplateRevisionSource string

const (
	TemplateRevisionSourceBuiltin TemplateRevisionSource = "builtin"
	TemplateRevisionSourceCustom  TemplateRevisionSource = "custom"
)

// TemplateRevision is one immutable revision of a template definition.
type TemplateRevision struct {
	TemplateID  TemplateID             `json:"template_id"`
	Revision    int64                  `json:"revision"`
	ContentHash string                 `json:"content_hash"`
	Payload     string                 `json:"payload"`
	Source      TemplateRevisionSource `json:"source"`
	CreatedAt   time.Time              `json:"created_at"`
}

// ErrTemplateRevisionImmutable is the sentinel every attempt to change or delete
// a template revision is mapped to. The database refuses such a statement with a
// stable trigger message, and the stores translate that message into this error
// so a caller can classify it with errors.Is without matching on SQL text.
var ErrTemplateRevisionImmutable = errors.New("template revision is immutable")

// templateRevisionImmutableMessage is the message every refusal trigger raises.
// It is stable on purpose: the SQL layer uses it to recognise its own refusal,
// and a test pins it.
const templateRevisionImmutableMessage = "template revisions are immutable (T3.01.a)"

// The trigger names, exported to the tests through the same constants the
// schema uses so the two cannot drift apart.
const (
	templateRevisionNoUpdateTrigger = "trg_flow_template_revisions_no_update"
	templateRevisionNoDeleteTrigger = "trg_flow_template_revisions_no_delete"
)

// canonicalTemplateContent encodes a template definition as the canonical JSON
// document a revision's payload and content hash are computed from.
//
// The document has a fixed shape, written below, and every map it contains
// (gate configs) is emitted with sorted keys; array order is the definition's
// own order, because the stage sequence and the loop edges are part of what a
// template is. Nothing about the encoding depends on Go's map iteration order,
// so two runs — or two machines — encode the same definition byte for byte the
// same way, which is what makes the hash meaningful.
func canonicalTemplateContent(def templateDef) ([]byte, error) {
	doc := canonicalTemplate{
		Name:        def.Name,
		Description: def.Description,
		Stages:      make([]canonicalStage, 0, len(def.Stages)),
		Loops:       make([]canonicalLoop, 0, len(def.Loops)),
	}
	for _, st := range def.Stages {
		cs := canonicalStage{
			Type:     st.Type,
			Name:     st.Name,
			Canvas:   st.Canvas,
			AgentID:  st.AgentID,
			Optional: st.Optional,
			Gates:    make([]canonicalGate, 0, len(st.Gates)),
		}
		for _, g := range st.Gates {
			cs.Gates = append(cs.Gates, canonicalGate{
				Phase:  g.Phase,
				Kind:   g.Kind,
				OnFail: g.OnFail,
				Config: sortedStringMap(g.Config),
			})
		}
		doc.Stages = append(doc.Stages, cs)
	}
	for _, l := range def.Loops {
		doc.Loops = append(doc.Loops, canonicalLoop{From: l.From, To: l.To})
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode template %s: %w", def.ID, err)
	}
	return body, nil
}

// canonicalTemplate is the canonical encoding of a template definition. The
// field order below is the document's field order, and json.Marshal follows it.
type canonicalTemplate struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Stages      []canonicalStage `json:"stages"`
	Loops       []canonicalLoop  `json:"loops"`
}

type canonicalStage struct {
	Type     StageType       `json:"type"`
	Name     string          `json:"name"`
	Canvas   string          `json:"canvas"`
	AgentID  string          `json:"agent_id,omitempty"`
	Optional bool            `json:"optional"`
	Gates    []canonicalGate `json:"gates"`
}

type canonicalGate struct {
	Phase  GatePhase  `json:"phase"`
	Kind   GateKind   `json:"kind"`
	OnFail GateOnFail `json:"on_fail,omitempty"`
	// Config is a map element in a struct field: encoding/json sorts map keys,
	// so the keys are already in a fixed order. There is no omitempty here
	// because an empty config is part of a gate's identity.
	Config map[string]string `json:"config"`
}

type canonicalLoop struct {
	From StageType `json:"from"`
	To   StageType `json:"to"`
}

// sortedStringMap returns m as a non-nil map (nil becomes empty) so the encoded
// document has the same shape whether a gate carried a config or not.
func sortedStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// templateContentHash returns the canonical hash of an encoded definition:
// "sha256:" + the lowercase hex digest.
func templateContentHash(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// canonicalTemplateHash encodes and hashes a definition in one step.
func canonicalTemplateHash(def templateDef) (string, []byte, error) {
	body, err := canonicalTemplateContent(def)
	if err != nil {
		return "", nil, err
	}
	return templateContentHash(body), body, nil
}

// customTemplateDef loads a stored custom template back into the internal
// templateDef shape so it is hashed and frozen through the same canonical
// encoding a built-in goes through.
func customTemplateDef(def CustomTemplate) templateDef {
	return def.toTemplateDef()
}

// TemplateRevisionStore is the revision half of a durable flow store. It is
// implemented by *SQLiteFlowStore; memoryStore implements the same rules in
// process (see store.go), and an engine whose store implements neither reports
// revision 0, which is "unknown".
type TemplateRevisionStore interface {
	// EnsureTemplateRevision appends the definition as a revision unless the
	// latest revision already holds exactly this content, and returns the
	// revision number the content now has.
	EnsureTemplateRevision(def templateDef, source TemplateRevisionSource) (int64, error)
	// LatestTemplateRevision returns the current revision number of a template,
	// or 0 when the store has none.
	LatestTemplateRevision(id TemplateID) (int64, error)
	// GetTemplateRevision returns one revision, or nil when it does not exist.
	GetTemplateRevision(id TemplateID, revision int64) (*TemplateRevision, error)
}

// ensureTemplateRevisionInTx is the append rule of a template revision, inside
// the caller's transaction:
//
//   - The latest revision of the template is the row with the greatest
//     revision number.
//   - If its content hash equals the hash of def, nothing is appended and the
//     existing number is returned (saving the same template twice is
//     idempotent, and reopening a database whose built-in definition has not
//     changed writes nothing).
//   - Otherwise the next number — latest + 1, starting at 1 — is inserted with
//     the canonical payload and hash.
//
// It is a function on *sql.Tx, not a method, so PutTemplate (its own
// transaction) and the built-in seeding/backfill at open time (one transaction
// for all templates) share one implementation.
func ensureTemplateRevisionInTx(tx *sql.Tx, def templateDef, source TemplateRevisionSource) (int64, error) {
	hash, payload, err := canonicalTemplateHash(def)
	if err != nil {
		return 0, err
	}
	revision, _, err := ensureRevisionTx(tx, def.ID, hash, payload, source)
	return revision, err
}

// ensureRevisionTx is the append rule once a definition has been encoded: it
// reads the latest revision of id and appends the next one unless the latest
// already carries exactly this hash. It is shared by the canonical path above
// and by built-in seeding, whose hash may come from the test seam.
func ensureRevisionTx(tx *sql.Tx, id TemplateID, hash string, payload []byte, source TemplateRevisionSource) (int64, bool, error) {
	var (
		latest   int64
		latestOK bool
		lastHash string
	)
	err := tx.QueryRow(
		`SELECT revision, content_hash FROM flow_template_revisions
		  WHERE template_id = ? ORDER BY revision DESC LIMIT 1`, string(id),
	).Scan(&latest, &lastHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, false, fmt.Errorf("read latest template revision %s: %w", id, err)
	}
	latestOK = err == nil
	if latestOK && lastHash == hash {
		return latest, false, nil
	}
	// The content may have been frozen by an older revision: the ruling's
	// UNIQUE (template_id, content_hash) makes a second row for the same content
	// impossible, and a template edited back to an earlier state has exactly
	// that shape. The definition is already frozen and immutable, so the honest
	// answer is the revision that holds it — appending would be refused by the
	// constraint, and refusing the save would make a reverted template
	// unsavable forever.
	if existing, found, err := revisionByHashTx(tx, id, hash); err != nil {
		return 0, false, err
	} else if found {
		return existing, false, nil
	}
	next := int64(1)
	if latestOK {
		next = latest + 1
	}
	if _, err := tx.Exec(`
INSERT INTO flow_template_revisions (template_id, revision, content_hash, payload, source, created_at)
VALUES (?, ?, ?, ?, ?, ?)
`, string(id), next, hash, string(payload), string(source), time.Now().UTC().UnixMilli()); err != nil {
		return 0, false, classifyTemplateRevisionWrite(err)
	}
	return next, true, nil
}

// revisionByHashTx returns the revision that already holds this content, if
// any. There is at most one: the table's UNIQUE (template_id, content_hash)
// says so.
func revisionByHashTx(tx *sql.Tx, id TemplateID, hash string) (int64, bool, error) {
	var revision int64
	err := tx.QueryRow(
		`SELECT revision FROM flow_template_revisions WHERE template_id = ? AND content_hash = ?`,
		string(id), hash,
	).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read template revision %s by hash: %w", id, err)
	}
	return revision, true, nil
}

// classifyTemplateRevisionWrite maps a refused write to a template revision onto
// ErrTemplateRevisionImmutable. The refusal the table raises itself is the
// trigger message; a UNIQUE failure on (template_id, content_hash) means the
// same content is already frozen, which is the same class of "this is a fact,
// not a draft". Anything else is returned as it was.
func classifyTemplateRevisionWrite(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, templateRevisionImmutableMessage) || strings.Contains(msg, "UNIQUE constraint failed") {
		return fmt.Errorf("%w: %v", ErrTemplateRevisionImmutable, err)
	}
	return err
}

// latestTemplateRevision reads the current revision number of a template, or 0.
func latestTemplateRevision(q rowQueryer, id TemplateID) (int64, error) {
	var revision int64
	err := q.QueryRow(
		`SELECT revision FROM flow_template_revisions WHERE template_id = ? ORDER BY revision DESC LIMIT 1`,
		string(id),
	).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read latest template revision %s: %w", id, err)
	}
	return revision, nil
}

// EnsureTemplateRevision appends the current definition of def as a revision
// (builtin or custom) unless the latest revision already holds exactly that
// content, and returns the revision number the content now carries. It is
// idempotent: calling it with an unchanged definition writes nothing.
func (s *SQLiteFlowStore) EnsureTemplateRevision(def templateDef, source TemplateRevisionSource) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("ensure template revision %s: begin: %w", def.ID, err)
	}
	defer tx.Rollback()
	revision, err := ensureTemplateRevisionInTx(tx, def, source)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("ensure template revision %s: commit: %w", def.ID, err)
	}
	return revision, nil
}

// LatestTemplateRevision returns the current revision number of a template, or
// 0 when the store holds none (unknown).
func (s *SQLiteFlowStore) LatestTemplateRevision(id TemplateID) (int64, error) {
	return latestTemplateRevision(s.db, id)
}

// GetTemplateRevision returns one template revision, or nil when the template
// has no such revision.
func (s *SQLiteFlowStore) GetTemplateRevision(id TemplateID, revision int64) (*TemplateRevision, error) {
	if revision < 1 {
		return nil, nil
	}
	var (
		rec       TemplateRevision
		template  string
		source    string
		createdAt int64
	)
	err := s.db.QueryRow(`
SELECT template_id, revision, content_hash, payload, source, created_at
  FROM flow_template_revisions WHERE template_id = ? AND revision = ?`,
		string(id), revision,
	).Scan(&template, &rec.Revision, &rec.ContentHash, &rec.Payload, &source, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get template revision %s/%d: %w", id, revision, err)
	}
	rec.TemplateID = TemplateID(template)
	rec.Source = TemplateRevisionSource(source)
	rec.CreatedAt = time.UnixMilli(createdAt).UTC()
	return &rec, nil
}

// primedBuiltinHashHook is the seam the built-in priming uses to hash a
// built-in definition. It exists so a test can simulate "the next build changed
// a built-in template" without touching the package-level map: the test
// replaces this function, opens the database, and the seeding sees a different
// hash. Production installs nothing.
var primedBuiltinHashHook func(templateDef) (string, []byte, error)

// builtinHash returns the canonical hash of a built-in definition, through the
// test seam when one is installed.
func builtinHash(def templateDef) (string, []byte, error) {
	if primedBuiltinHashHook != nil {
		return primedBuiltinHashHook(def)
	}
	return canonicalTemplateHash(def)
}

// seedBuiltinRevisions ensures every built-in template has a revision for its
// current code definition, in one transaction. It writes only what is missing:
// a definition that is already frozen at the latest revision appends nothing,
// so opening a database repeatedly is silent. A definition the code changed
// appends the next revision, which is what lets a Flow created by an older
// build keep naming the template it was built from. It returns the built-in
// ids whose revision number moved, in template-id order, so a caller can log
// the seeding.
//
// It runs inside initSchema, before the legacy flows are backfilled with the
// revisions this produced (see backfillTemplateRevisions), so the backfill can
// rely on every built-in template having one.
func (s *SQLiteFlowStore) seedBuiltinRevisions() ([]TemplateID, error) {
	ids := make([]TemplateID, 0, len(builtinTemplates))
	for id := range builtinTemplates {
		ids = append(ids, id)
	}
	sortTemplateIDs(ids)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("seed builtin template revisions: begin: %w", err)
	}
	defer tx.Rollback()

	moved := make([]TemplateID, 0, len(ids))
	for _, id := range ids {
		def := builtinTemplates[id]
		hash, payload, err := builtinHash(def)
		if err != nil {
			return nil, err
		}
		_, appended, err := ensureRevisionTx(tx, id, hash, payload, TemplateRevisionSourceBuiltin)
		if err != nil {
			return nil, err
		}
		if appended {
			moved = append(moved, id)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("seed builtin template revisions: commit: %w", err)
	}
	return moved, nil
}

// sortTemplateIDs sorts ids ascending. It is a tiny helper so the built-in
// seeding order (and therefore the bytes it writes, in row order) is
// deterministic without pulling in a sort call in two places.
func sortTemplateIDs(ids []TemplateID) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

// --- legacy flow backfill (T3.01.a group 3) --------------------------------

// templateRevisionBackfillMigration is the receipt name of the one-time
// template_revision backfill below.
const templateRevisionBackfillMigration = "t301a_template_revision_backfill"

// templateRevisionBackfillNote is the honest caveat the receipt carries: the
// revision written into a legacy flow is the one the template had at migration
// time, which may be newer than the definition the flow was actually created
// from. Nothing pretends the original definition is known.
const templateRevisionBackfillNote = "template_revision is the template revision current at migration time; its content may be newer than this flow's creation. A flow whose template no longer exists keeps 0 (unknown)."

// TemplateRevisionBackfilledFlow is one legacy flow the backfill filled in.
type TemplateRevisionBackfilledFlow struct {
	FlowID     string `json:"flow_id"`
	TemplateID string `json:"template_id"`
	Revision   int64  `json:"revision"`
}

// TemplateRevisionUnresolvedFlow is one legacy flow the backfill could not fill
// in: its template has no revision (it was deleted, or it was never saved as a
// custom template), so its template_revision stays 0.
type TemplateRevisionUnresolvedFlow struct {
	FlowID     string `json:"flow_id"`
	TemplateID string `json:"template_id"`
	Reason     string `json:"reason"`
}

// TemplateRevisionBackfillReceipt is the receipt of the template_revision
// backfill, recorded in flow_store_migrations once.
type TemplateRevisionBackfillReceipt struct {
	Note       string                           `json:"note"`
	Backfilled []TemplateRevisionBackfilledFlow `json:"backfilled"`
	Unresolved []TemplateRevisionUnresolvedFlow `json:"unresolved"`
}

// backfillTemplateRevisions fills template_revision on the stored flows that
// predate T3.01.a group 3, once, at open time.
//
// It runs after the built-in revisions were confirmed and the stored custom
// templates were frozen as revision 1, so "the template's migration-time
// revision" is well defined for every template the database still knows about:
// built-ins always have one, and a custom template that still has its
// flow_templates row got revision 1 from its current payload. A flow whose
// template has no revision at all — the template was deleted — keeps 0, which
// means unknown, and is listed in the receipt instead of guessed at.
//
// The rewrite goes through writeFlowDocument, the one writer of a stored Flow:
// the document gains its template_revision member, the mirror column is filled
// from it and the revision moves by one. The flow's UpdatedAt is deliberately
// not touched: this is a system migration, not user activity, and updated_at
// orders the lists the migration of group 2 (which runs just before this) uses
// to choose each project's flow of record. The write appends no event and
// queues no outbox row, because nothing happened to the flow — it always was
// this template's instance.
//
// A database nothing needs to change is left with no receipt, exactly like the
// group-2 migration: it stays eligible if a later open finds rows that need the
// backfill (rows another build wrote straight into the old database).
func (s *SQLiteFlowStore) backfillTemplateRevisions() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("migrate %s: begin: %w", templateRevisionBackfillMigration, err)
	}
	defer tx.Rollback()

	applied, err := readAppliedMigration(tx, templateRevisionBackfillMigration)
	if err != nil {
		return err
	}
	if applied != nil {
		return nil
	}
	receipt, err := backfillTemplateRevisionsTx(tx)
	if err != nil {
		return err
	}
	if len(receipt.Backfilled) == 0 && len(receipt.Unresolved) == 0 {
		return nil
	}
	body, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("migrate %s: encode receipt: %w", templateRevisionBackfillMigration, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO flow_store_migrations (name, applied_at, receipt) VALUES (?, ?, ?)`,
		templateRevisionBackfillMigration, time.Now().UTC().UnixMilli(), string(body),
	); err != nil {
		return fmt.Errorf("migrate %s: record receipt: %w", templateRevisionBackfillMigration, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate %s: commit: %w", templateRevisionBackfillMigration, err)
	}
	return nil
}

// backfillTemplateRevisionsTx is the work of the backfill, inside the caller's
// transaction, and returns the receipt it would record. It is separate from the
// transaction handling so a test can run it against a database of its own.
//
// The scan is ordered by flow id so the receipt's lists are reproducible. Only
// rows whose template_revision column is NULL or 0 are considered; a payload
// that already carries a revision is left exactly as it is (the column is its
// mirror, and a disagreement is somebody else's bug, not a reason to rewrite
// the document).
func backfillTemplateRevisionsTx(tx *sql.Tx) (TemplateRevisionBackfillReceipt, error) {
	receipt := TemplateRevisionBackfillReceipt{Note: templateRevisionBackfillNote}

	rows, err := tx.Query(`
SELECT id, template_id FROM flows
 WHERE template_revision IS NULL OR template_revision = 0
 ORDER BY id`)
	if err != nil {
		return receipt, fmt.Errorf("migrate %s: read flows: %w", templateRevisionBackfillMigration, err)
	}
	type pendingRow struct{ id, templateID string }
	pending := []pendingRow{}
	for rows.Next() {
		var id, templateID string
		if err := rows.Scan(&id, &templateID); err != nil {
			rows.Close()
			return receipt, fmt.Errorf("migrate %s: scan flow: %w", templateRevisionBackfillMigration, err)
		}
		pending = append(pending, pendingRow{id: id, templateID: templateID})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return receipt, fmt.Errorf("migrate %s: read flows: %w", templateRevisionBackfillMigration, err)
	}
	rows.Close()

	for _, row := range pending {
		templateID := TemplateID(row.templateID)
		latest, err := latestTemplateRevision(tx, templateID)
		if err != nil {
			return receipt, err
		}
		if latest == 0 {
			receipt.Unresolved = append(receipt.Unresolved, TemplateRevisionUnresolvedFlow{
				FlowID:     row.id,
				TemplateID: row.templateID,
				Reason:     "no revision exists for this template (it was deleted, or it was never saved)",
			})
			continue
		}
		flow, err := loadStoredFlow(tx, row.id)
		if err != nil {
			return receipt, err
		}
		if flow.TemplateRevision > 0 {
			// The document already names a revision; the empty column is a
			// disagreement this migration does not read, let alone rewrite.
			continue
		}
		flow.TemplateRevision = latest
		// flow.UpdatedAt is intentionally left as stored, and the outbox is
		// skipped: this is a system migration, not user activity, and nothing
		// happened to the flow — see the method doc.
		if err := writeFlowDocumentWith(tx, flow, flowWriteOptions{skipOutbox: true}); err != nil {
			return receipt, err
		}
		receipt.Backfilled = append(receipt.Backfilled, TemplateRevisionBackfilledFlow{
			FlowID:     row.id,
			TemplateID: row.templateID,
			Revision:   latest,
		})
	}
	return receipt, nil
}

// FindAppliedTemplateRevisionBackfill returns the receipt of the legacy-flow
// template_revision backfill, or nil when it has not run (a fresh database, or
// one where every flow already carries a revision).
func (s *SQLiteFlowStore) FindAppliedTemplateRevisionBackfill() (*FlowStoreMigration, error) {
	return s.FindAppliedStoreMigration(templateRevisionBackfillMigration)
}
