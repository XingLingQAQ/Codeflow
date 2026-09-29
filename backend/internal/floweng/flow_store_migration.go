package floweng

// One-time data migrations of the Flow store (T3.01.a group 2).
//
// A migration here is a change to stored rows that the schema cannot express:
// adding a column has a default that backfills every existing row, while
// "this project must hold exactly one active project flow" has no default that
// can repair a database that already holds several. The rule has to be enforced
// going forward — that is idx_flows_one_active_project — and the rows that
// violate it have to be brought into line once, before the index is created.
//
// Two properties are load-bearing:
//
//   - Each migration runs at most once per database, and its effects are
//     durable together with the record that says it ran. flow_store_migrations
//     holds one row per migration, written in the same transaction that made
//     the changes; a second open reads the row and returns without touching
//     anything. A crash between the two is impossible: either both committed or
//     neither did, and the migration is idempotent by construction because its
//     first act in the transaction is to re-check the receipt inside it.
//
//   - The migration and the ongoing writes share one writer. A suspended flow is
//     written by writeFlowDocument — the same function Put uses, in the
//     migration's transaction — so the document, its mirror columns, its
//     revision and its outbox row are produced by exactly one implementation.
//     There is no second opinion about what a stored Flow looks like.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// activeProjectFlowCandidate is one stored active project flow as the migration
// scan sees it: the columns the choice between several of them is made on.
type activeProjectFlowCandidate struct {
	id        string
	updatedAt int64
	createdAt int64
}

// oneActiveProjectFlowIndexName is the index that enforces the one-active-
// project-flow rule, and the name of the migration that makes a database
// satisfy it. Both spellings matter: the migration is recorded under this name,
// and a driver error that names the constraint rather than its columns is
// recognised by it.
const oneActiveProjectFlowIndexName = "idx_flows_one_active_project"

// singleActiveProjectFlowMigration is the receipt name of the migration below.
const singleActiveProjectFlowMigration = "t301a_single_active_project_flow"

// suspendedProjectFlowReason is the reason recorded in the flow.suspended event
// and in the migration receipt of every flow the migration parks.
const suspendedProjectFlowReason = "one active project flow per project (T3.01.a)"

// FlowStoreMigration is the receipt of one applied store migration: what it was
// called, when it ran, and the JSON report of what it changed.
type FlowStoreMigration struct {
	Name      string
	AppliedAt time.Time
	Receipt   string
}

// SuspendedProjectFlow is one flow the migration parked: its id and the id of
// the flow that took the project's active slot.
type SuspendedProjectFlow struct {
	FlowID        string `json:"flow_id"`
	PrimaryFlowID string `json:"primary_flow_id"`
}

// SingleActiveProjectFlowReceipt is the receipt payload of
// t301a_single_active_project_flow. PrimaryFlowID is the migration's choice of
// each project's flow of record; the suspended flows hang off it, in the order
// they were chosen (oldest updated_at first among equals).
type SingleActiveProjectFlowReceipt struct {
	Reason string `json:"reason"`
	// Projects lists, in project-id order, every project whose database held
	// more than one active project flow.
	Projects []SingleActiveProjectFlowProject `json:"projects"`
}

// SingleActiveProjectFlowProject is one project's part of the receipt.
type SingleActiveProjectFlowProject struct {
	ProjectID     string                 `json:"project_id"`
	PrimaryFlowID string                 `json:"primary_flow_id"`
	Suspended     []SuspendedProjectFlow `json:"suspended"`
}

// SuspendedCount returns how many flows the receipt says were suspended.
func (r SingleActiveProjectFlowReceipt) SuspendedCount() int {
	n := 0
	for _, p := range r.Projects {
		n += len(p.Suspended)
	}
	return n
}

// readAppliedMigration returns the receipt of a migration, or nil when the
// database does not record it. It is also the read-only accessor a caller uses
// to report which migrations a database has: FindAppliedStoreMigration is the
// exported form and the reason this function takes a queryer rather than a
// store.
func readAppliedMigration(q rowQueryer, name string) (*FlowStoreMigration, error) {
	var (
		appliedAt int64
		receipt   string
	)
	err := q.QueryRow(`SELECT applied_at, receipt FROM flow_store_migrations WHERE name = ?`, name).
		Scan(&appliedAt, &receipt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read flow store migration %s: %w", name, err)
	}
	return &FlowStoreMigration{
		Name:      name,
		AppliedAt: time.UnixMilli(appliedAt).UTC(),
		Receipt:   receipt,
	}, nil
}

// rowQueryer is the one-method slice of *sql.DB and *sql.Tx that the receipt
// read needs, so the same code serves "outside a transaction" and "inside the
// migration's transaction".
type rowQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

// FindAppliedStoreMigration returns the receipt a database holds for a named
// store migration, or nil when that migration has not run there. It is the
// read-only half of the migration record: startup logging and diagnostics use
// it, and nothing in this package writes through it.
func (s *SQLiteFlowStore) FindAppliedStoreMigration(name string) (*FlowStoreMigration, error) {
	return readAppliedMigration(s.db, name)
}

// FindAppliedSingleActiveProjectFlowMigration returns the receipt of the
// one-active-project-flow migration, or nil when it has not run (a database this
// build has never opened, or one that held no project with several active flows
// and was therefore left alone — see the migration: it records nothing when it
// changes nothing).
func (s *SQLiteFlowStore) FindAppliedSingleActiveProjectFlowMigration() (*FlowStoreMigration, error) {
	return s.FindAppliedStoreMigration(singleActiveProjectFlowMigration)
}

// migrateSingleActiveProjectFlow brings a database written before T3.01.a group
// 2 into line with the one-active-project-flow rule, once.
//
// What it does, in one transaction:
//
//  1. Reads the migration receipt inside the transaction. A database that has
//     one returns immediately, which is what makes a second, third or
//     interrupted open harmless.
//  2. Reads every stored flow document (the payload is the document of record)
//     and groups them by project.
//  3. For each project that holds more than one active project flow, chooses the
//     flow of record: the greatest updated_at, then the greatest created_at,
//     then the smallest id. That is the first active flow the project page
//     shows today — it lists a project's flows by updated_at DESC — so the
//     choice is the one already in front of the user, and it is a total order,
//     so it is testable rather than arbitrary.
//  4. Parks the rest as FlowStatusSuspended. Their stages, artifacts and events
//     are untouched; only the status, the revision (one write more) and one
//     appended flow.suspended event differ. The event carries the reason and the
//     id of the flow of record.
//  5. Writes each parked flow with writeFlowDocument — the same function Put
//     uses, inside this transaction — so the document, its columns, its
//     revision and its new flow_event_outbox row land together, and the
//     projection of that event is the projector's ordinary job afterwards.
//  6. Records the receipt in flow_store_migrations in the same transaction.
//
// It writes nothing to a project that holds at most one active project flow, so
// an ordinary database gains no migration row, no event and no revision. It
// creates no index: initSchema creates idx_flows_one_active_project after this
// returns, and the order is the point — the index can only be created on a
// database that satisfies it.
func (s *SQLiteFlowStore) migrateSingleActiveProjectFlow() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("migrate %s: begin: %w", singleActiveProjectFlowMigration, err)
	}
	defer tx.Rollback()

	applied, err := readAppliedMigration(tx, singleActiveProjectFlowMigration)
	if err != nil {
		return err
	}
	if applied != nil {
		return nil
	}

	receipt, err := suspendExtraActiveProjectFlows(tx)
	if err != nil {
		return err
	}
	if len(receipt.Projects) == 0 {
		// Nothing to change, so nothing to record: a database with no
		// violation is left with no trace of a migration that did nothing, and
		// it stays eligible to run the migration if a later open finds rows
		// that need it (rows written by another build straight into the old
		// database). This is the only path where the receipt is not written,
		// and it is safe for exactly that reason.
		return nil
	}

	body, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("migrate %s: encode receipt: %w", singleActiveProjectFlowMigration, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO flow_store_migrations (name, applied_at, receipt) VALUES (?, ?, ?)`,
		singleActiveProjectFlowMigration, time.Now().UTC().UnixMilli(), string(body),
	); err != nil {
		return fmt.Errorf("migrate %s: record receipt: %w", singleActiveProjectFlowMigration, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate %s: commit: %w", singleActiveProjectFlowMigration, err)
	}
	return nil
}

// suspendExtraActiveProjectFlows is the work of the migration, inside the
// caller's transaction, and returns the receipt it would record. It is separate
// from the transaction handling so a test can run it against a database of its
// own.
func suspendExtraActiveProjectFlows(tx *sql.Tx) (SingleActiveProjectFlowReceipt, error) {
	receipt := SingleActiveProjectFlowReceipt{Reason: suspendedProjectFlowReason}

	byProject := map[string][]activeProjectFlowCandidate{}
	projectIDs := []string{}

	// The scan reads the document and the columns the unicity rule needs in one
	// pass. Rows are grouped in Go rather than by a window function because the
	// document is what must be rewritten and the ids are what the receipt names;
	// a project holds few flows.
	rows, err := tx.Query(`SELECT id, project_id, updated_at, created_at, kind, status FROM flows`)
	if err != nil {
		return receipt, fmt.Errorf("migrate %s: read flows: %w", singleActiveProjectFlowMigration, err)
	}
	for rows.Next() {
		var (
			id, projectID string
			updatedAt     int64
			createdAt     int64
			kind, status  string
		)
		if err := rows.Scan(&id, &projectID, &updatedAt, &createdAt, &kind, &status); err != nil {
			rows.Close()
			return receipt, fmt.Errorf("migrate %s: scan flow: %w", singleActiveProjectFlowMigration, err)
		}
		if kind != string(FlowKindProject) || status != string(FlowStatusActive) {
			continue
		}
		if _, seen := byProject[projectID]; !seen {
			projectIDs = append(projectIDs, projectID)
		}
		byProject[projectID] = append(byProject[projectID], activeProjectFlowCandidate{id: id, updatedAt: updatedAt, createdAt: createdAt})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return receipt, fmt.Errorf("migrate %s: read flows: %w", singleActiveProjectFlowMigration, err)
	}
	rows.Close()

	sort.Strings(projectIDs)
	for _, projectID := range projectIDs {
		group := byProject[projectID]
		if len(group) < 2 {
			continue
		}
		// primaryIsBetter orders two candidates: the flow of record is the one
		// with the greatest updated_at, then the greatest created_at, then the
		// smallest id. Descending on time and ascending on id is a total order,
		// so the choice never depends on map or query order.
		primary := group[0]
		for _, c := range group[1:] {
			if primaryIsBetter(c, primary) {
				primary = c
			}
		}
		// Suspend the rest oldest-first: the flows the project page listed
		// furthest down are the ones that lost their slot least recently, and
		// the order makes the receipt's suspended list reproducible.
		rest := make([]activeProjectFlowCandidate, 0, len(group)-1)
		for _, c := range group {
			if c.id != primary.id {
				rest = append(rest, c)
			}
		}
		sort.Slice(rest, func(i, j int) bool {
			if rest[i].updatedAt != rest[j].updatedAt {
				return rest[i].updatedAt < rest[j].updatedAt
			}
			if rest[i].createdAt != rest[j].createdAt {
				return rest[i].createdAt < rest[j].createdAt
			}
			return rest[i].id < rest[j].id
		})

		entry := SingleActiveProjectFlowProject{ProjectID: projectID, PrimaryFlowID: primary.id}
		for _, c := range rest {
			if err := suspendProjectFlow(tx, c.id, primary.id); err != nil {
				return receipt, err
			}
			entry.Suspended = append(entry.Suspended, SuspendedProjectFlow{FlowID: c.id, PrimaryFlowID: primary.id})
		}
		receipt.Projects = append(receipt.Projects, entry)
	}
	return receipt, nil
}

// primaryIsBetter reports whether candidate a should take the project's active
// slot over candidate b: greatest updated_at, then greatest created_at, then
// smallest id.
func primaryIsBetter(a, b activeProjectFlowCandidate) bool {
	if a.updatedAt != b.updatedAt {
		return a.updatedAt > b.updatedAt
	}
	if a.createdAt != b.createdAt {
		return a.createdAt > b.createdAt
	}
	return a.id < b.id
}

// suspendProjectFlow parks one flow: it loads its document, sets the status to
// suspended, appends the flow.suspended event that says why and who took the
// slot, and writes the result through writeFlowDocument — the same path Put
// uses — so the document, its columns, its revision and its outbox row are all
// produced by that one function. The caller holds the transaction; when it
// commits, everything this wrote is durable together.
func suspendProjectFlow(tx *sql.Tx, flowID, primaryFlowID string) error {
	flow, err := loadStoredFlow(tx, flowID)
	if err != nil {
		return err
	}
	if flow.Status != FlowStatusActive {
		// Another writer moved it between the scan and here (impossible in this
		// transaction, which holds the write lock, but a caller of this helper
		// must not silently overwrite a status it did not expect).
		return fmt.Errorf("migrate %s: flow %s is %s, not active", singleActiveProjectFlowMigration, flowID, flow.Status)
	}
	now := time.Now().UTC()
	flow.Status = FlowStatusSuspended
	flow.UpdatedAt = now
	flow.Events = append(flow.Events, FlowEvent{
		ID:        uuid.NewString(),
		Type:      "flow.suspended",
		Message:   fmt.Sprintf("suspended: %s; project flow of record is %s", suspendedProjectFlowReason, primaryFlowID),
		Timestamp: now,
	})
	return writeFlowDocument(tx, flow)
}

// loadStoredFlow reads one stored Flow document inside a transaction. The
// payload is the document of record; the columns are its mirror and Put keeps
// the two in step, so a mismatch is reported instead of read (the same check Get
// and List make).
func loadStoredFlow(tx *sql.Tx, id string) (*Flow, error) {
	var (
		payload string
		mirror  flowMirror
	)
	err := tx.QueryRow(`SELECT id, payload, kind, revision FROM flows WHERE id = ?`, id).
		Scan(&mirror.rowID, &payload, &mirror.kind, &mirror.revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("flow not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("migrate %s: read flow %s: %w", singleActiveProjectFlowMigration, id, err)
	}
	var flow Flow
	if err := json.Unmarshal([]byte(payload), &flow); err != nil {
		return nil, fmt.Errorf("migrate %s: decode flow %s: %w", singleActiveProjectFlowMigration, id, err)
	}
	if err := mirror.check(&flow); err != nil {
		return nil, fmt.Errorf("migrate %s: %w", singleActiveProjectFlowMigration, err)
	}
	return &flow, nil
}
