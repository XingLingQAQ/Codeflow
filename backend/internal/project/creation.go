package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/storage"
)

// ProjectCreation is returned only after both the project and its initial Flow
// are durable. Embedding Project preserves the existing project response shape.
type ProjectCreation struct {
	*Project
	Flow    *floweng.Flow    `json:"flow"`
	Session *storage.Session `json:"session"`
}

// SessionProvisioner is the small cross-store contract required by project
// creation. The default implementation keeps a durable session whenever the
// application wires storage, while tests can inject a deterministic fake.
type SessionProvisioner interface {
	CreateDefaultSession(ctx context.Context, projectID, title string) (*storage.Session, error)
	GetSession(ctx context.Context, sessionID string) (*storage.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
}

type CreateOperationRecord struct {
	IdempotencyKey string
	ProjectID      string
	FlowID         string
	SessionID      string
	State          string
	Title          string
	WorkspaceRoot  string
	// NormalizedRequestHash binds the payload first accepted under this key;
	// see normalizedProjectCreateRequestHash for the canonicalization rules.
	// Records written before the hash column existed read back as "": that
	// empty value is the legacy marker meaning "payload hash unknown", never
	// "payload matches an empty hash". It is deliberately not backfilled: the
	// journal persists only Title and WorkspaceRoot of the original intent,
	// so the original hash cannot be recomputed faithfully, and fabricating a
	// partial hash would make the same-key payload check reject legitimate
	// retries. Legacy intent must never be rewritten from a new payload.
	NormalizedRequestHash string
}

// createRequestHashPreimagePrefix domain-separates and versions the canonical
// hash encoding. Changing the encoding requires bumping this prefix, which
// changes every derived hash; stored hashes are comparable only within one
// version.
const createRequestHashPreimagePrefix = "project-create-request/v1\n"

// canonicalProjectCreatePayload is the hashed view of a create request. Only
// client-supplied payload fields are included; ID, DefaultFlowID and
// DefaultSessionID are server-assigned and excluded.
type canonicalProjectCreatePayload struct {
	Title         string                 `json:"title"`
	Description   string                 `json:"description"`
	Status        string                 `json:"status"`
	Tags          []string               `json:"tags"`
	GitBranch     string                 `json:"git_branch"`
	WorkspaceRoot string                 `json:"workspace_root"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// normalizedProjectCreateRequestHash returns the SHA-256 hex of the canonical
// encoding of a project create payload. Normalization rules:
//   - the request is first passed through normalizeProjectCreateRequest, so
//     leading/trailing whitespace on title, description, git branch and
//     workspace root is insignificant, an empty status equals the default
//     "planning", and blank or duplicate tags are insignificant;
//   - tag order is insignificant (the tag set is sorted before encoding),
//     while the tag set itself is significant;
//   - metadata key order is insignificant (JSON encoding sorts map keys) and
//     absent metadata equals empty metadata (omitempty);
//   - strings are never case-folded: a case change in any value is a
//     semantic difference and produces a different hash;
//   - any semantic difference in name, description, status, tag set, branch,
//     workspace root or metadata value produces a different hash.
//
// The function is pure and deterministic. The caller canonicalizes the
// workspace root through the owning service first so equivalent path
// spellings hash equal. It errors only when metadata cannot be JSON-encoded.
func normalizedProjectCreateRequestHash(req *ProjectCreateRequest) (string, error) {
	normalized, err := normalizeProjectCreateRequest(req)
	if err != nil {
		return "", err
	}
	tags := cloneProjectStrings(normalized.Tags)
	sort.Strings(tags)
	payload := canonicalProjectCreatePayload{
		Title:         normalized.Title,
		Description:   normalized.Description,
		Status:        string(normalized.Status),
		Tags:          tags,
		GitBranch:     normalized.GitBranch,
		WorkspaceRoot: normalized.WorkspaceRoot,
		Metadata:      normalized.Metadata,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode canonical project create payload: %w", err)
	}
	sum := sha256.Sum256(append([]byte(createRequestHashPreimagePrefix), data...))
	return hex.EncodeToString(sum[:]), nil
}

const (
	createOperationPending        = "pending"
	createOperationSessionCreated = "session_created"
	createOperationFlowCreated    = "flow_created"
	createOperationCompleted      = "completed"
	// createOperationRolledBack is the reserved terminal state for an operation
	// whose provisioned resources were compensated and abandoned. No current
	// code path writes it: this saga is forward-only (a failure leaves the
	// operation resumable rather than rolled back), and the constant is kept
	// so a future rollback writer has one stable, documented value. The
	// recovery whitelist below deliberately excludes it — a rolled_back row
	// must never be resurrected as "incomplete".
	createOperationRolledBack = "rolled_back"
)

// isResumableCreateOperationState reports whether a journaled create operation
// state may be picked up by startup recovery or listed as incomplete. This is
// an explicit whitelist of the three in-flight states, not a blacklist of
// terminal ones: completed is finished, rolled_back is compensated, and any
// unknown state must not be resurrected either.
func isResumableCreateOperationState(state string) bool {
	switch state {
	case createOperationPending, createOperationSessionCreated, createOperationFlowCreated:
		return true
	}
	return false
}

type CreateOperationStore interface {
	GetCreateOperation(ctx context.Context, key string) (*CreateOperationRecord, error)
	ListIncompleteCreateOperations(ctx context.Context) ([]*CreateOperationRecord, error)
	SaveCreateOperation(ctx context.Context, key string, record *CreateOperationRecord) error
}

type storageSessionProvisioner struct{ store storage.ISessionStorage }

func (p storageSessionProvisioner) CreateDefaultSession(ctx context.Context, projectID, title string) (*storage.Session, error) {
	_ = ctx
	sessionID := "project-" + projectID
	if existing, err := p.store.GetSession(sessionID); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}
	return p.store.CreateSession(storage.CreateSessionInput{ID: sessionID, Title: title, Config: map[string]interface{}{"project_id": projectID, "default": true}})
}
func (p storageSessionProvisioner) GetSession(ctx context.Context, sessionID string) (*storage.Session, error) {
	_ = ctx
	return p.store.GetSession(sessionID)
}
func (p storageSessionProvisioner) DeleteSession(ctx context.Context, sessionID string) error {
	_ = ctx
	_, err := p.store.DeleteSession(sessionID)
	return err
}

var (
	provisionerMu             sync.RWMutex
	defaultSessionProvisioner SessionProvisioner
	idempotencyMu             sync.Mutex
	idempotentCreations       = map[string]ProjectCreation{}
)

// SetSessionProvisioner wires the application's session store.
func SetSessionProvisioner(p SessionProvisioner) {
	provisionerMu.Lock()
	defaultSessionProvisioner = p
	provisionerMu.Unlock()
}
func getSessionProvisioner() SessionProvisioner {
	provisionerMu.RLock()
	defer provisionerMu.RUnlock()
	return defaultSessionProvisioner
}

// SetSessionStorage is a convenience for application bootstrap.
func SetSessionStorage(store storage.ISessionStorage) {
	if store == nil {
		SetSessionProvisioner(nil)
		return
	}
	SetSessionProvisioner(storageSessionProvisioner{store: store})
}

// ProjectCreationError describes which part of the compensated create failed.
type ProjectCreationError struct {
	Phase      string
	Cause      error
	CleanupErr error
}

func (e *ProjectCreationError) Error() string {
	if e.CleanupErr != nil {
		return fmt.Sprintf("%s failed: %v; cleanup failed: %v", e.Phase, e.Cause, e.CleanupErr)
	}
	return fmt.Sprintf("%s failed: %v", e.Phase, e.Cause)
}

func (e *ProjectCreationError) Unwrap() error { return e.Cause }

// ErrProjectCreateConflict marks idempotency-key replays whose payload differs
// from the payload first accepted under the key.
var ErrProjectCreateConflict = errors.New("project create idempotency conflict")

// ProjectCreateConflictError is the deterministic rejection of an idempotent
// create replayed with a different payload under a key that already accepted
// one. It carries the operation identity (key and reserved project id) and
// both normalized request hashes. DifferingFields names the journaled intent
// fields (title, workspace_root) known to differ; the journal deliberately
// does not persist full payloads, so an empty list means the difference is in
// non-journaled fields (description, status, tags, git branch or metadata).
// API handlers map it to HTTP 409 (wired in T0.11.c).
type ProjectCreateConflictError struct {
	IdempotencyKey  string
	ProjectID       string
	AcceptedHash    string
	RejectedHash    string
	DifferingFields []string
}

func (e *ProjectCreateConflictError) Error() string {
	fields := "outside the journaled intent"
	if len(e.DifferingFields) > 0 {
		fields = "in " + strings.Join(e.DifferingFields, ",")
	}
	return fmt.Sprintf("idempotency key %q already accepted a different create payload for project %s (differing %s)", e.IdempotencyKey, e.ProjectID, fields)
}

func (e *ProjectCreateConflictError) Unwrap() error { return ErrProjectCreateConflict }

// Create operation reconcile projection states.
const (
	CreateOperationProjectionReady    = "ready"
	CreateOperationProjectionDegraded = "degraded"
	CreateOperationProjectionFailed   = "failed"
)

// Create operation steps, in reconcile check order.
const (
	CreateOperationStepProject = "project"
	CreateOperationStepFlow    = "flow"
	CreateOperationStepSession = "session"
	CreateOperationStepBinding = "binding"
)

// CreateOperationProjection is the read-only reconcile view of a create
// operation: which journaled resources still exist and whether the saga can
// still complete under its key. It is a derived computation over the journal
// record plus live existence checks — the journal schema is frozen by the
// normalized_request_hash migration, so the projection deliberately persists
// no state of its own and can never drift from the record it describes.
//
//	ready:    every recorded resource exists and the persisted project's
//	          default bindings reference the operation's flow and session.
//	degraded: one or more steps are missing but the continuation can
//	          re-provision them; MissingSteps lists the absent steps and
//	          Reasons explains each absence.
//	failed:   the record contradicts reality (foreign flow, project bound to
//	          different resources, or a missing flow the persisted project is
//	          still bound to — create recovery never rebinds a persisted
//	          project), so no retry under this key can complete the saga.
type CreateOperationProjection struct {
	State        string   `json:"state"`
	MissingSteps []string `json:"missing_steps,omitempty"`
	Reasons      []string `json:"reasons,omitempty"`
}

// ProjectCreateReconcileError rejects a replay whose operation reconcile
// projection is failed: the journaled resources contradict reality, so the
// saga cannot complete under this key. The projection explains the failure.
type ProjectCreateReconcileError struct {
	IdempotencyKey string
	ProjectID      string
	Projection     *CreateOperationProjection
}

func (e *ProjectCreateReconcileError) Error() string {
	return fmt.Sprintf("project create operation %q (project %s) is unrecoverable: %s", e.IdempotencyKey, e.ProjectID, strings.Join(e.Projection.Reasons, "; "))
}

// ReconcileCreateOperation checks every resource ID journaled by the
// operation against live existence, step by step in project/flow/session/
// binding order. It never creates or deletes resources — provisioning belongs
// to the create continuation — so the projection is a pure function of the
// record and the stores.
func ReconcileCreateOperation(ctx context.Context, projects IProjectService, flows floweng.Engine, record *CreateOperationRecord) (*CreateOperationProjection, error) {
	if record == nil {
		return nil, fmt.Errorf("create operation record is required")
	}
	var missing, reasons, failed []string

	projectID := strings.TrimSpace(record.ProjectID)
	if projectID == "" {
		failed = append(failed, "operation record has no project id")
	}

	// Step project: the persisted project row.
	var project *Project
	if projectID != "" {
		p, err := projects.GetProject(ctx, projectID)
		if err != nil {
			return nil, err
		}
		project = p
		if project == nil {
			missing = append(missing, CreateOperationStepProject)
			reasons = append(reasons, fmt.Sprintf("project %s is not persisted", projectID))
		}
	}

	// Step flow: the recorded default Flow, or the adoptable new-project Flow
	// the continuation would find by listing (a journal save can have failed
	// after the Flow was created). Listing is project-scoped, so a resolved
	// Flow always belongs to the operation's project.
	var flow *floweng.Flow
	if projectID != "" {
		f, err := findCreateOperationFlow(ctx, flows, record)
		if err != nil {
			return nil, err
		}
		flow = f
	}
	flowForeign := false
	if flow == nil {
		if record.FlowID != "" {
			// Distinguish "gone" from "tracked under another project"; the
			// latter contradicts the journal and fails the operation.
			if foreign, err := flows.Get(ctx, record.FlowID); err == nil && foreign != nil {
				flowForeign = true
				failed = append(failed, fmt.Sprintf("flow %s recorded by the operation is tracked under project %s, not %s", foreign.ID, foreign.ProjectID, projectID))
			} else {
				missing = append(missing, CreateOperationStepFlow)
				reasons = append(reasons, fmt.Sprintf("flow %s recorded by the operation no longer exists", record.FlowID))
			}
		} else {
			missing = append(missing, CreateOperationStepFlow)
			reasons = append(reasons, "operation recorded no flow")
		}
	}

	// Step session: durable only when a provisioner is wired. Without one the
	// session is process-local and deterministically re-derived by the
	// continuation, so there is no stored resource to verify.
	var session *storage.Session
	provisioner := getSessionProvisioner()
	if provisioner != nil {
		if record.SessionID != "" {
			s, err := provisioner.GetSession(ctx, record.SessionID)
			if err != nil {
				return nil, err
			}
			session = s
		}
		if session == nil {
			missing = append(missing, CreateOperationStepSession)
			if record.SessionID != "" {
				reasons = append(reasons, fmt.Sprintf("session %s recorded by the operation no longer exists", record.SessionID))
			} else {
				reasons = append(reasons, "operation recorded no session")
			}
		}
	}

	// Step binding: a persisted project must reference the operation's flow
	// and session. Create recovery never rebinds a persisted project, so a
	// contradiction — or a binding that points at a flow that is gone — makes
	// the operation unrecoverable under its key.
	if project != nil {
		wantFlow := record.FlowID
		if flow != nil {
			wantFlow = flow.ID
		}
		wantSession := record.SessionID
		if session != nil {
			wantSession = session.ID
		} else if provisioner == nil && wantSession == "" {
			// Mirror the continuation's deterministic ephemeral session id.
			wantSession = "project-" + projectID
		}
		switch {
		case project.DefaultFlowID != wantFlow || project.DefaultSessionID != wantSession:
			failed = append(failed, fmt.Sprintf("project %s is bound to flow %q/session %q but the operation recorded flow %q/session %q", project.ID, project.DefaultFlowID, project.DefaultSessionID, wantFlow, wantSession))
		case flow == nil && !flowForeign:
			failed = append(failed, fmt.Sprintf("project %s is bound to flow %s which no longer exists; create recovery does not rebind persisted projects", project.ID, wantFlow))
		}
	}

	projection := &CreateOperationProjection{State: CreateOperationProjectionReady}
	switch {
	case len(failed) > 0:
		projection.State = CreateOperationProjectionFailed
		projection.Reasons = failed
	case len(missing) > 0:
		projection.State = CreateOperationProjectionDegraded
		projection.MissingSteps = missing
		projection.Reasons = reasons
	}
	return projection, nil
}

// GetCreateOperationProjection loads the journaled operation for key and
// returns it with its reconcile projection. It is the read path for status
// reporting (the API wiring is T0.11.c). A nil record with a nil error means
// no operation exists under the key.
func GetCreateOperationProjection(ctx context.Context, projects IProjectService, flows floweng.Engine, key string) (*CreateOperationRecord, *CreateOperationProjection, error) {
	journal, ok := projects.(CreateOperationStore)
	if !ok {
		return nil, nil, fmt.Errorf("project service does not persist create operations")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil, nil
	}
	record, err := journal.GetCreateOperation(ctx, key)
	if err != nil || record == nil {
		return nil, nil, err
	}
	projection, err := ReconcileCreateOperation(ctx, projects, flows, record)
	if err != nil {
		return nil, nil, err
	}
	return record, projection, nil
}

// checkCreateOperationReplay decides whether a request may reuse the
// operation journaled under its idempotency key:
//
//   - record hash == request hash: same payload, reuse the operation.
//   - record hash == "": legacy record; the payload hash is unknowable (the
//     journal persists only Title and WorkspaceRoot of the original intent),
//     so the key alone authorizes reuse and the new payload is validated but
//     never written back. Accepted per the T0.11.a legacy convention.
//   - otherwise: same key with a different payload — reject with a typed
//     *ProjectCreateConflictError (HTTP 409 mapping is T0.11.c).
func checkCreateOperationReplay(key string, record *CreateOperationRecord, normalized *ProjectCreateRequest, requestHash string) error {
	if record.NormalizedRequestHash == "" || record.NormalizedRequestHash == requestHash {
		return nil
	}
	fields := []string{}
	if record.Title != normalized.Title {
		fields = append(fields, "title")
	}
	if record.WorkspaceRoot != normalized.WorkspaceRoot {
		fields = append(fields, "workspace_root")
	}
	return &ProjectCreateConflictError{
		IdempotencyKey:  key,
		ProjectID:       record.ProjectID,
		AcceptedHash:    record.NormalizedRequestHash,
		RejectedHash:    requestHash,
		DifferingFields: fields,
	}
}

// failUnrecoverableCreateOperation is the replay pre-flight: it reconciles
// the journaled record against live resources and returns a typed
// *ProjectCreateReconcileError when the projection is failed. Degraded
// operations proceed — the continuation re-provisions the missing steps.
func failUnrecoverableCreateOperation(ctx context.Context, projects IProjectService, flows floweng.Engine, key string, record *CreateOperationRecord) error {
	projection, err := ReconcileCreateOperation(ctx, projects, flows, record)
	if err != nil {
		return err
	}
	if projection.State == CreateOperationProjectionFailed {
		return &ProjectCreateReconcileError{IdempotencyKey: key, ProjectID: record.ProjectID, Projection: projection}
	}
	return nil
}

// CreateProjectWithDefaultFlow provisions the runnable new-project Flow before
// exposing the project. If project persistence fails, the already-created Flow
// is deleted using a detached, bounded cleanup context.
func CreateProjectWithDefaultFlow(
	ctx context.Context,
	projects IProjectService,
	flows floweng.Engine,
	req *ProjectCreateRequest,
) (*ProjectCreation, error) {
	return createProjectWithDefaultFlow(ctx, projects, flows, req, nil)
}

type createProgressFunc func(*CreateOperationRecord) error

func createProjectWithDefaultFlow(
	ctx context.Context,
	projects IProjectService,
	flows floweng.Engine,
	req *ProjectCreateRequest,
	onProgress createProgressFunc,
) (*ProjectCreation, error) {
	if projects == nil {
		return nil, &ProjectCreationError{Phase: "project service validation", Cause: fmt.Errorf("project service is required")}
	}
	if flows == nil {
		return nil, &ProjectCreationError{Phase: "flow service validation", Cause: fmt.Errorf("flow engine is required")}
	}

	normalized, err := normalizeProjectCreateRequest(req)
	if err != nil {
		return nil, err
	}
	if err := canonicalizeCreateWorkspaceRoot(projects, normalized); err != nil {
		return nil, err
	}
	if normalized.ID == "" {
		normalized.ID = uuid.New().String()
	}
	var session *storage.Session
	if p := getSessionProvisioner(); p != nil {
		session, err = p.CreateDefaultSession(ctx, normalized.ID, normalized.Title+" session")
		if err != nil {
			return nil, &ProjectCreationError{Phase: "default session creation", Cause: err}
		}
	} else {
		now := time.Now().UnixMilli()
		session = &storage.Session{ID: uuid.NewString(), Title: normalized.Title + " session", CreatedAt: now, UpdatedAt: now}
	}
	normalized.DefaultSessionID = session.ID
	if onProgress != nil {
		if err := onProgress(&CreateOperationRecord{ProjectID: normalized.ID, SessionID: session.ID, State: createOperationSessionCreated}); err != nil {
			cleanupErr := cleanupCreatedSession(ctx, session.ID)
			return nil, &ProjectCreationError{Phase: "session journal update", Cause: err, CleanupErr: cleanupErr}
		}
	}
	if normalized.WorkspaceRoot != "" {
		// CreateProject applies the service's allowed-root policy and canonicalizes
		// the value before persistence.
	}

	flow, err := flows.Create(ctx, &floweng.CreateFlowRequest{
		ProjectID:  normalized.ID,
		TemplateID: floweng.TemplateNewProject,
		SessionID:  session.ID,
	})
	if err != nil {
		var cleanupErr error
		if p := getSessionProvisioner(); p != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			cleanupErr = p.DeleteSession(cleanupCtx, session.ID)
			cancel()
		}
		return nil, &ProjectCreationError{Phase: "default flow creation", Cause: err, CleanupErr: cleanupErr}
	}
	normalized.DefaultFlowID = flow.ID
	if onProgress != nil {
		if err := onProgress(&CreateOperationRecord{ProjectID: normalized.ID, FlowID: flow.ID, SessionID: session.ID, State: createOperationFlowCreated}); err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			cleanupErr := flows.Delete(cleanupCtx, flow.ID)
			if err := cleanupCreatedSession(cleanupCtx, session.ID); cleanupErr == nil {
				cleanupErr = err
			}
			return nil, &ProjectCreationError{Phase: "flow journal update", Cause: err, CleanupErr: cleanupErr}
		}
	}

	created, err := projects.CreateProject(ctx, normalized)
	if err == nil {
		return &ProjectCreation{Project: created, Flow: flow, Session: session}, nil
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	cleanupErr := flows.Delete(cleanupCtx, flow.ID)
	if p := getSessionProvisioner(); p != nil {
		if err := p.DeleteSession(cleanupCtx, session.ID); cleanupErr == nil {
			cleanupErr = err
		}
	}
	return nil, &ProjectCreationError{
		Phase:      "project persistence",
		Cause:      err,
		CleanupErr: cleanupErr,
	}
}

func cleanupCreatedSession(ctx context.Context, sessionID string) error {
	if p := getSessionProvisioner(); p != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return p.DeleteSession(cleanupCtx, sessionID)
	}
	return nil
}

// CreateProjectWithDefaultFlowIdempotent makes retries return the original
// durable result. SQLite services additionally persist the operation journal;
// the in-process map covers memory-backed services and concurrent retries.
//
// Replay decision for journaled services (the journal binds the first payload
// accepted under the key, first-write-wins):
//
//	key unused                   -> start a new operation.
//	same key, same payload hash  -> reuse the operation: a completed operation
//	                                returns the original resources, an
//	                                interrupted one continues from its missing
//	                                steps (per-step existence checks prevent
//	                                duplicate resources).
//	same key, legacy record      -> payload hash unknowable: reuse by key.
//	same key, different hash     -> *ProjectCreateConflictError (409 in T0.11.c).
func CreateProjectWithDefaultFlowIdempotent(ctx context.Context, projects IProjectService, flows floweng.Engine, req *ProjectCreateRequest, key string) (*ProjectCreation, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return CreateProjectWithDefaultFlow(ctx, projects, flows, req)
	}
	journal, durable := projects.(CreateOperationStore)
	if !durable {
		// Memory-backed services keep no payload hash, so only the concurrent
		// retry map deduplicates them; conflict enforcement requires the
		// durable journal.
		idempotencyMu.Lock()
		defer idempotencyMu.Unlock()
		cacheKey := fmt.Sprintf("%p:%s", projects, key)
		if prior, ok := idempotentCreations[cacheKey]; ok {
			return cloneProjectCreation(&prior), nil
		}
		created, err := CreateProjectWithDefaultFlow(ctx, projects, flows, req)
		if err != nil {
			return nil, err
		}
		idempotentCreations[cacheKey] = *cloneProjectCreation(created)
		return created, nil
	}
	return createProjectWithDefaultFlowReplayable(ctx, projects, flows, journal, req, key)
}

func createProjectWithDefaultFlowReplayable(ctx context.Context, projects IProjectService, flows floweng.Engine, journal CreateOperationStore, req *ProjectCreateRequest, key string) (*ProjectCreation, error) {
	normalized, err := normalizeProjectCreateRequest(req)
	if err != nil {
		return nil, err
	}
	if err := canonicalizeCreateWorkspaceRoot(projects, normalized); err != nil {
		return nil, err
	}
	// The hash is computed after normalization and root canonicalization so
	// equivalent payloads hash equal. A metadata value that cannot be JSON
	// encoded fails here; such requests already fail later at persistence.
	requestHash, err := normalizedProjectCreateRequestHash(normalized)
	if err != nil {
		return nil, err
	}

	idempotencyMu.Lock()
	defer idempotencyMu.Unlock()
	// The success cache is keyed by payload hash as well, so a same-key
	// replay with a different payload always reaches the durable conflict
	// check instead of being answered from the cache.
	cacheKey := fmt.Sprintf("%p:%s:%s", projects, key, requestHash)
	if prior, ok := idempotentCreations[cacheKey]; ok {
		return cloneProjectCreation(&prior), nil
	}

	record, err := journal.GetCreateOperation(ctx, key)
	if err != nil {
		return nil, err
	}
	if record == nil {
		record = &CreateOperationRecord{IdempotencyKey: key, ProjectID: uuid.NewString(), State: createOperationPending, Title: normalized.Title, WorkspaceRoot: normalized.WorkspaceRoot, NormalizedRequestHash: requestHash}
		if err := journal.SaveCreateOperation(ctx, key, record); err != nil {
			return nil, err
		}
	} else {
		if err := checkCreateOperationReplay(key, record, normalized, requestHash); err != nil {
			return nil, err
		}
		// Pre-existing records reconcile before any provisioning; a failed
		// projection is terminal for the key. Fresh records skip the
		// reconcile: every step is missing by construction.
		if err := failUnrecoverableCreateOperation(ctx, projects, flows, key, record); err != nil {
			return nil, err
		}
	}
	return continueCreateOperationLocked(ctx, projects, flows, journal, key, record, normalized, cacheKey)
}

// continueCreateOperationLocked drives the saga forward from the journaled
// record, re-provisioning only the steps whose resources are missing. The
// caller holds idempotencyMu and has already admitted the replay (new record,
// matching hash, or legacy reuse) and reconciled pre-existing records.
func continueCreateOperationLocked(ctx context.Context, projects IProjectService, flows floweng.Engine, journal CreateOperationStore, key string, record *CreateOperationRecord, normalized *ProjectCreateRequest, cacheKey string) (*ProjectCreation, error) {
	if record.Title != "" {
		normalized.Title = record.Title
	}
	normalized.WorkspaceRoot = record.WorkspaceRoot
	normalized.ID = record.ProjectID

	project, err := projects.GetProject(ctx, record.ProjectID)
	if err != nil {
		return nil, err
	}
	var session *storage.Session
	if p := getSessionProvisioner(); p != nil {
		if record.SessionID != "" {
			session, err = p.GetSession(ctx, record.SessionID)
			if err != nil {
				return nil, err
			}
		}
		if session == nil {
			session, err = p.CreateDefaultSession(ctx, record.ProjectID, normalized.Title+" session")
			if err != nil {
				return nil, &ProjectCreationError{Phase: "default session creation", Cause: err}
			}
		}
	} else {
		sessionID := record.SessionID
		if sessionID == "" {
			sessionID = "project-" + record.ProjectID
		}
		now := time.Now().UnixMilli()
		session = &storage.Session{ID: sessionID, Title: normalized.Title + " session", CreatedAt: now, UpdatedAt: now}
	}
	record.SessionID = session.ID
	record.State = createOperationSessionCreated
	if err := journal.SaveCreateOperation(ctx, key, record); err != nil {
		return nil, err
	}

	flow, err := findCreateOperationFlow(ctx, flows, record)
	if err != nil {
		return nil, err
	}
	if flow == nil {
		flow, err = flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: record.ProjectID, TemplateID: floweng.TemplateNewProject, SessionID: session.ID})
		if err != nil {
			return nil, &ProjectCreationError{Phase: "default flow creation", Cause: err}
		}
	}
	record.FlowID = flow.ID
	record.State = createOperationFlowCreated
	if err := journal.SaveCreateOperation(ctx, key, record); err != nil {
		return nil, err
	}

	if project == nil {
		normalized.DefaultSessionID = session.ID
		normalized.DefaultFlowID = flow.ID
		project, err = projects.CreateProject(ctx, normalized)
		if err != nil {
			return nil, &ProjectCreationError{Phase: "project persistence", Cause: err}
		}
	}
	if project.DefaultSessionID != session.ID || project.DefaultFlowID != flow.ID {
		return nil, &ProjectCreationError{Phase: "create operation recovery", Cause: fmt.Errorf("project %s has inconsistent default bindings", project.ID)}
	}
	record.State = createOperationCompleted
	if err := journal.SaveCreateOperation(ctx, key, record); err != nil {
		return nil, err
	}
	created := &ProjectCreation{Project: project, Flow: flow, Session: session}
	idempotentCreations[cacheKey] = *cloneProjectCreation(created)
	return created, nil
}

// findCreateOperationFlow resolves the operation's default Flow by listing
// the project's Flows: an exact ID match wins, otherwise the adoptable
// new-project Flow for the recorded session is reused (a journal save can
// have failed after the Flow was created). Listing doubles as the existence
// check: a recorded Flow that no longer exists resolves to nil so the caller
// can classify or re-create it, instead of failing on the store-level
// "flow not found" error that both Flow stores return from Get.
func findCreateOperationFlow(ctx context.Context, flows floweng.Engine, record *CreateOperationRecord) (*floweng.Flow, error) {
	items, err := flows.List(ctx, record.ProjectID)
	if err != nil {
		return nil, err
	}
	var fallback *floweng.Flow
	for _, flow := range items {
		if flow == nil {
			continue
		}
		if record.FlowID != "" && flow.ID == record.FlowID {
			return flow, nil
		}
		if fallback == nil && flow.TemplateID == floweng.TemplateNewProject && flow.SessionID == record.SessionID {
			fallback = flow
		}
	}
	return fallback, nil
}

// CreateOperationRecoveryDiagnostic describes one journaled create operation
// that startup recovery could not complete. Recovery is per-record: an
// unrecoverable or invalid operation is reported here and skipped, never
// aborts the run, so a single bad journal row cannot block server startup
// (§26.17 P2). The operation keeps its last durable journal state; a later
// replay under the same key re-attempts resumable (degraded) operations,
// while failed ones keep rejecting with *ProjectCreateReconcileError until
// the row is repaired.
type CreateOperationRecoveryDiagnostic struct {
	IdempotencyKey string `json:"idempotency_key"`
	ProjectID      string `json:"project_id"`
	State          string `json:"state"`
	Error          string `json:"error"`
}

var (
	recoveryDiagnosticsMu sync.RWMutex
	recoveryDiagnostics   []CreateOperationRecoveryDiagnostic
)

// CreateOperationRecoveryDiagnostics returns a copy of the diagnostics
// recorded by the most recent RecoverIncompleteProjectCreations run, in
// journal order. It is the queryable channel for startup recovery health: an
// empty slice means every incomplete operation was completed (or none
// existed). It reports nothing about runs from before the process started.
func CreateOperationRecoveryDiagnostics() []CreateOperationRecoveryDiagnostic {
	recoveryDiagnosticsMu.RLock()
	defer recoveryDiagnosticsMu.RUnlock()
	return append([]CreateOperationRecoveryDiagnostic(nil), recoveryDiagnostics...)
}

func recordCreateOperationRecoveryDiagnostics(diagnostics []CreateOperationRecoveryDiagnostic) {
	recoveryDiagnosticsMu.Lock()
	defer recoveryDiagnosticsMu.Unlock()
	recoveryDiagnostics = append([]CreateOperationRecoveryDiagnostic(nil), diagnostics...)
}

// RecoverIncompleteProjectCreations completes every durable create saga before
// the server starts accepting requests. Recovery is forward-only: already
// durable Sessions and Flows are reused and the missing Project binding is
// committed last. Recovery re-uses the journaled intent; there is no client
// payload to conflict with, so the replay payload check does not apply.
//
// Recovery is per-record: each operation that cannot be resumed (reconcile
// projection failed, invalid row, store error) is aggregated into the returned
// diagnostics — also queryable via CreateOperationRecoveryDiagnostics — and
// startup continues with the next record. Only a journal that cannot even
// list its rows produces a non-nil error, which aborts startup: without the
// record list there is nothing to recover or report.
func RecoverIncompleteProjectCreations(ctx context.Context, projects IProjectService, flows floweng.Engine) ([]CreateOperationRecoveryDiagnostic, error) {
	journal, ok := projects.(CreateOperationStore)
	if !ok {
		return nil, nil
	}
	records, err := journal.ListIncompleteCreateOperations(ctx)
	if err != nil {
		return nil, err
	}
	var diagnostics []CreateOperationRecoveryDiagnostic
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.IdempotencyKey) == "" {
			diagnostics = append(diagnostics, CreateOperationRecoveryDiagnostic{Error: "invalid incomplete project operation"})
			continue
		}
		if err := resumeCreateOperation(ctx, projects, flows, journal, record); err != nil {
			diagnostics = append(diagnostics, CreateOperationRecoveryDiagnostic{
				IdempotencyKey: record.IdempotencyKey,
				ProjectID:      record.ProjectID,
				State:          record.State,
				Error:          err.Error(),
			})
		}
	}
	recordCreateOperationRecoveryDiagnostics(diagnostics)
	return diagnostics, nil
}

// resumeCreateOperation continues one journaled operation from its missing
// steps under the same serialization and reconcile pre-flight as client
// replays. Completed operations cache under the record's hash segment, which
// stays empty for legacy records.
func resumeCreateOperation(ctx context.Context, projects IProjectService, flows floweng.Engine, journal CreateOperationStore, record *CreateOperationRecord) error {
	normalized, err := normalizeProjectCreateRequest(&ProjectCreateRequest{
		Title:         record.Title,
		WorkspaceRoot: record.WorkspaceRoot,
	})
	if err != nil {
		return err
	}
	if err := canonicalizeCreateWorkspaceRoot(projects, normalized); err != nil {
		return err
	}
	idempotencyMu.Lock()
	defer idempotencyMu.Unlock()
	if err := failUnrecoverableCreateOperation(ctx, projects, flows, record.IdempotencyKey, record); err != nil {
		return err
	}
	cacheKey := fmt.Sprintf("%p:%s:%s", projects, record.IdempotencyKey, record.NormalizedRequestHash)
	_, err = continueCreateOperationLocked(ctx, projects, flows, journal, record.IdempotencyKey, record, normalized, cacheKey)
	return err
}

func cloneProjectCreation(in *ProjectCreation) *ProjectCreation {
	if in == nil {
		return nil
	}
	out := &ProjectCreation{Project: cloneProject(in.Project), Flow: in.Flow, Session: in.Session}
	return out
}

type projectWorkspaceRootCanonicalizer interface {
	CanonicalizeWorkspaceRoot(root string) (string, error)
}

func canonicalizeCreateWorkspaceRoot(projects IProjectService, req *ProjectCreateRequest) error {
	if req == nil || strings.TrimSpace(req.WorkspaceRoot) == "" {
		return nil
	}
	canonicalizer, ok := projects.(projectWorkspaceRootCanonicalizer)
	if !ok {
		return fmt.Errorf("%w: project service does not expose a workspace root policy", ErrInvalidProjectCreate)
	}
	canonical, err := canonicalizer.CanonicalizeWorkspaceRoot(req.WorkspaceRoot)
	if err != nil {
		return fmt.Errorf("%w: workspace root: %v", ErrInvalidProjectCreate, err)
	}
	req.WorkspaceRoot = canonical
	return nil
}
