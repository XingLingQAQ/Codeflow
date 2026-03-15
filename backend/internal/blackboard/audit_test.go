package blackboard

import (
	"context"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/stretchr/testify/assert"
)

func newBlackboardAuditContext() context.Context {
	return audit.ContextWithTrace(context.Background(), &audit.AuditTrace{
		RequestID: "req-approval-001",
		SessionID: "session-approval-001",
		TaskID:    "task-approval-001",
		AgentID:   "agent-approval-001",
		Method:    "BLACKBOARD",
		Path:      "votes",
	})
}

func lastApprovalAuditEntry(t *testing.T, storage *audit.MemoryStorage) *audit.AuditLogEntry {
	t.Helper()

	entry, err := storage.GetLastEntry(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, audit.EventApproval, entry.EventType)
	return entry
}

func approvalAuditEntryByAction(t *testing.T, storage *audit.MemoryStorage, action string) *audit.AuditLogEntry {
	t.Helper()

	entries, err := storage.Query(context.Background(), &audit.AuditQuery{
		Limit:      10,
		EventTypes: []audit.AuditEventType{audit.EventApproval},
	})
	assert.NoError(t, err)

	for i := range entries {
		if entries[i].Action == action {
			return &entries[i]
		}
	}

	t.Fatalf("approval audit entry not found for action %s", action)
	return nil
}

func assertApprovalTrace(t *testing.T, entry *audit.AuditLogEntry) {
	t.Helper()
	if assert.NotNil(t, entry.Trace) {
		assert.Equal(t, "req-approval-001", entry.Trace.RequestID)
		assert.Equal(t, "session-approval-001", entry.Trace.SessionID)
		assert.Equal(t, "task-approval-001", entry.Trace.TaskID)
		assert.Equal(t, "agent-approval-001", entry.Trace.AgentID)
		assert.Equal(t, "BLACKBOARD", entry.Trace.Method)
		assert.Equal(t, "votes", entry.Trace.Path)
	}
}

func TestInMemoryBlackboard_CreateVote_AuditCreatedAndApproved(t *testing.T) {
	storage := audit.NewMemoryStorage()
	audit.SetAuditService(audit.NewAuditService(storage))
	t.Cleanup(func() {
		audit.SetAuditService(nil)
	})

	bb := NewInMemoryBlackboard()
	defer close(bb.stopChan)

	ctx := newBlackboardAuditContext()
	entry, err := bb.CreateEntry(ctx, &EntryCreateRequest{
		Type:    EntryTypeProposal,
		Title:   "Proposal",
		Content: "Ship CFO-070",
		Author:  "alice",
	})
	assert.NoError(t, err)

	vote, err := bb.CreateVote(ctx, &VoteCreateRequest{
		EntryID:     entry.ID,
		Title:       "Approve CFO-070",
		Description: "Need sign-off",
		Initiator:   "alice",
		Threshold:   0.5,
		TimeoutSec:  60,
		Metadata: map[string]interface{}{
			"source": "unit-test",
		},
	})
	assert.NoError(t, err)

	createdEntry := approvalAuditEntryByAction(t, storage, "vote_created")
	assert.Equal(t, audit.EventApproval, createdEntry.EventType)
	assert.Equal(t, audit.OutcomeSuccess, createdEntry.Outcome)
	assert.Equal(t, "vote_created", createdEntry.Action)
	assert.Equal(t, "vote", createdEntry.Resource.Type)
	assert.Equal(t, vote.ID, createdEntry.Resource.ID)
	assert.Equal(t, "alice", createdEntry.Actor.ID)
	assert.Equal(t, "session-approval-001", createdEntry.Actor.SessionID)
	assert.Equal(t, entry.ID, createdEntry.Details["entry_id"])
	assert.Equal(t, vote.Threshold, createdEntry.Details["threshold"])
	assert.Equal(t, 60, createdEntry.Details["timeout_sec"])
	assertApprovalTrace(t, createdEntry)

	response, err := bb.CastVote(ctx, vote.ID, &VoteCastRequest{
		AgentID: "reviewer-1",
		Approve: true,
	})
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, VoteStatusApproved, response.Vote.Status)

	castEntry := approvalAuditEntryByAction(t, storage, "vote_cast")
	assert.Equal(t, audit.OutcomeSuccess, castEntry.Outcome)
	assert.Equal(t, vote.ID, castEntry.Resource.ID)
	assert.Equal(t, "reviewer-1", castEntry.Details["agent_id"])
	assert.Equal(t, true, castEntry.Details["approved"])
	assert.Equal(t, string(VoteStatusPending), castEntry.Details["status"])
	assert.Equal(t, 1, castEntry.Details["approve_count"])
	assert.Equal(t, 0, castEntry.Details["reject_count"])
	assert.Equal(t, 1, castEntry.Details["total_votes"])
	assertApprovalTrace(t, castEntry)

	approvedEntry := lastApprovalAuditEntry(t, storage)
	assert.Equal(t, audit.OutcomeSuccess, approvedEntry.Outcome)
	assert.Equal(t, "vote_approved", approvedEntry.Action)
	assert.Equal(t, vote.ID, approvedEntry.Resource.ID)
	assert.Equal(t, string(VoteStatusApproved), approvedEntry.Details["status"])
	assert.Equal(t, 1, approvedEntry.Details["approve_count"])
	assert.Equal(t, 0, approvedEntry.Details["reject_count"])
	assert.Equal(t, 1, approvedEntry.Details["total_votes"])
	assert.Equal(t, 1.0, approvedEntry.Details["approve_rate"])
	assertApprovalTrace(t, approvedEntry)
	assert.NotEqual(t, vote.ID, createdEntry.ID)
}

func TestInMemoryBlackboard_CheckExpiredVotes_AuditTimeout(t *testing.T) {
	storage := audit.NewMemoryStorage()
	audit.SetAuditService(audit.NewAuditService(storage))
	t.Cleanup(func() {
		audit.SetAuditService(nil)
	})

	bb := NewInMemoryBlackboard()
	defer close(bb.stopChan)

	ctx := newBlackboardAuditContext()
	entry, err := bb.CreateEntry(ctx, &EntryCreateRequest{
		Type:    EntryTypeProposal,
		Title:   "Timeout proposal",
		Content: "Wait for quorum",
		Author:  "alice",
	})
	assert.NoError(t, err)

	vote, err := bb.CreateVote(ctx, &VoteCreateRequest{
		EntryID:    entry.ID,
		Title:      "Timeout vote",
		Initiator:  "alice",
		Threshold:  0.75,
		TimeoutSec: 1,
	})
	assert.NoError(t, err)

	bb.mu.Lock()
	vote.ExpiresAt = time.Now().Unix() - 1
	bb.mu.Unlock()

	bb.CheckExpiredVotes()

	timeoutEntry := lastApprovalAuditEntry(t, storage)
	assert.Equal(t, audit.OutcomeFailure, timeoutEntry.Outcome)
	assert.Equal(t, audit.SeverityWarning, timeoutEntry.Severity)
	assert.Equal(t, "vote_timeout", timeoutEntry.Action)
	assert.Equal(t, vote.ID, timeoutEntry.Resource.ID)
	assert.Equal(t, string(VoteStatusTimeout), timeoutEntry.Details["status"])
	assert.Equal(t, 0, timeoutEntry.Details["approve_count"])
	assert.Equal(t, 0, timeoutEntry.Details["reject_count"])
	assert.Equal(t, 0, timeoutEntry.Details["total_votes"])
	assert.Nil(t, timeoutEntry.Trace)
	assert.NotEqual(t, vote.ID, timeoutEntry.ID)
}
