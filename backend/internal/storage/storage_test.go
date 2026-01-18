package storage

import (
	"sync"
	"testing"
	"time"
)

func TestSessionStorage_CreateGetSession(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, err := storage.CreateSession(CreateSessionInput{
		Title: "Test Session",
		Model: "claude-3",
		Config: map[string]interface{}{
			"temperature": 0.7,
		},
	})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	if session.ID == "" {
		t.Error("Expected session ID")
	}
	if session.Title != "Test Session" {
		t.Errorf("Expected title 'Test Session', got '%s'", session.Title)
	}
	if session.Model != "claude-3" {
		t.Errorf("Expected model 'claude-3', got '%s'", session.Model)
	}

	// Get session
	retrieved, err := storage.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession error: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected to retrieve session")
	}
	if retrieved.ID != session.ID {
		t.Errorf("Expected ID '%s', got '%s'", session.ID, retrieved.ID)
	}
}

func TestSessionStorage_DefaultTitle(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, err := storage.CreateSession(CreateSessionInput{})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	if session.Title != "New Session" {
		t.Errorf("Expected default title 'New Session', got '%s'", session.Title)
	}
}

func TestSessionStorage_GetAllSessions(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	// Create multiple sessions
	storage.CreateSession(CreateSessionInput{Title: "Session 1"})
	time.Sleep(10 * time.Millisecond)
	storage.CreateSession(CreateSessionInput{Title: "Session 2"})
	time.Sleep(10 * time.Millisecond)
	storage.CreateSession(CreateSessionInput{Title: "Session 3"})

	sessions, err := storage.GetAllSessions(nil)
	if err != nil {
		t.Fatalf("GetAllSessions error: %v", err)
	}

	if len(sessions) != 3 {
		t.Errorf("Expected 3 sessions, got %d", len(sessions))
	}

	// Default order is DESC by updated_at
	if sessions[0].Title != "Session 3" {
		t.Errorf("Expected 'Session 3' first, got '%s'", sessions[0].Title)
	}
}

func TestSessionStorage_UpdateSession(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Original"})
	time.Sleep(5 * time.Millisecond)

	updated, err := storage.UpdateSession(session.ID, &Session{Title: "Updated"})
	if err != nil {
		t.Fatalf("UpdateSession error: %v", err)
	}

	if updated.Title != "Updated" {
		t.Errorf("Expected title 'Updated', got '%s'", updated.Title)
	}
	if updated.UpdatedAt <= session.UpdatedAt {
		t.Error("Expected updated_at to be updated")
	}
}

func TestSessionStorage_DeleteSession(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "To Delete"})

	deleted, err := storage.DeleteSession(session.ID)
	if err != nil {
		t.Fatalf("DeleteSession error: %v", err)
	}
	if !deleted {
		t.Error("Expected delete to return true")
	}

	retrieved, _ := storage.GetSession(session.ID)
	if retrieved != nil {
		t.Error("Expected session to be deleted")
	}
}

func TestSessionStorage_CreateGetMessage(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})

	msg, err := storage.CreateMessage(CreateMessageInput{
		SessionID:  session.ID,
		Role:       RoleUser,
		Content:    "Hello, world!",
		TokenCount: 5,
	})
	if err != nil {
		t.Fatalf("CreateMessage error: %v", err)
	}

	if msg.ID == "" {
		t.Error("Expected message ID")
	}
	if msg.Role != RoleUser {
		t.Errorf("Expected role 'user', got '%s'", msg.Role)
	}
	if msg.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", msg.Content)
	}

	retrieved, err := storage.GetMessage(msg.ID)
	if err != nil {
		t.Fatalf("GetMessage error: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected to retrieve message")
	}
}

func TestSessionStorage_GetSessionMessages(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})

	storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleUser, Content: "Message 1"})
	time.Sleep(5 * time.Millisecond)
	storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleAssistant, Content: "Message 2"})
	time.Sleep(5 * time.Millisecond)
	storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleUser, Content: "Message 3"})

	messages, err := storage.GetSessionMessages(session.ID, nil)
	if err != nil {
		t.Fatalf("GetSessionMessages error: %v", err)
	}

	if len(messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(messages))
	}

	// Default order is ASC by timestamp
	if messages[0].Content != "Message 1" {
		t.Errorf("Expected 'Message 1' first, got '%s'", messages[0].Content)
	}
}

func TestSessionStorage_DeleteMessage(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})
	msg, _ := storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleUser, Content: "To Delete"})

	deleted, err := storage.DeleteMessage(msg.ID)
	if err != nil {
		t.Fatalf("DeleteMessage error: %v", err)
	}
	if !deleted {
		t.Error("Expected delete to return true")
	}

	retrieved, _ := storage.GetMessage(msg.ID)
	if retrieved != nil {
		t.Error("Expected message to be deleted")
	}
}

func TestSessionStorage_DeleteSessionMessages(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})
	storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleUser, Content: "Msg 1"})
	storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleUser, Content: "Msg 2"})
	storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleUser, Content: "Msg 3"})

	count, err := storage.DeleteSessionMessages(session.ID)
	if err != nil {
		t.Fatalf("DeleteSessionMessages error: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected 3 deleted, got %d", count)
	}

	messages, _ := storage.GetSessionMessages(session.ID, nil)
	if len(messages) != 0 {
		t.Error("Expected all messages to be deleted")
	}
}

func TestSessionStorage_CreateGetCheckpoint(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})

	cp, err := storage.CreateCheckpoint(CreateCheckpointInput{
		SessionID:       session.ID,
		GitHash:         "abc123",
		DialogStateHash: "hash123",
		Description:     "Test checkpoint",
	})
	if err != nil {
		t.Fatalf("CreateCheckpoint error: %v", err)
	}

	if cp.ID == "" {
		t.Error("Expected checkpoint ID")
	}
	if cp.GitHash != "abc123" {
		t.Errorf("Expected git hash 'abc123', got '%s'", cp.GitHash)
	}

	retrieved, err := storage.GetCheckpoint(cp.ID)
	if err != nil {
		t.Fatalf("GetCheckpoint error: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected to retrieve checkpoint")
	}
}

func TestSessionStorage_GetSessionCheckpoints(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})

	storage.CreateCheckpoint(CreateCheckpointInput{SessionID: session.ID, DialogStateHash: "hash1"})
	time.Sleep(5 * time.Millisecond)
	storage.CreateCheckpoint(CreateCheckpointInput{SessionID: session.ID, DialogStateHash: "hash2"})

	checkpoints, err := storage.GetSessionCheckpoints(session.ID, nil)
	if err != nil {
		t.Fatalf("GetSessionCheckpoints error: %v", err)
	}

	if len(checkpoints) != 2 {
		t.Errorf("Expected 2 checkpoints, got %d", len(checkpoints))
	}

	// Default order is DESC by created_at
	if checkpoints[0].DialogStateHash != "hash2" {
		t.Errorf("Expected 'hash2' first, got '%s'", checkpoints[0].DialogStateHash)
	}
}

func TestSessionStorage_DeleteCheckpoint(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})
	cp, _ := storage.CreateCheckpoint(CreateCheckpointInput{SessionID: session.ID, DialogStateHash: "hash"})

	deleted, err := storage.DeleteCheckpoint(cp.ID)
	if err != nil {
		t.Fatalf("DeleteCheckpoint error: %v", err)
	}
	if !deleted {
		t.Error("Expected delete to return true")
	}

	retrieved, _ := storage.GetCheckpoint(cp.ID)
	if retrieved != nil {
		t.Error("Expected checkpoint to be deleted")
	}
}

func TestSessionStorage_GetSessionWithMessages(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})
	storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleUser, Content: "Hello"})
	storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleAssistant, Content: "Hi there"})

	result, err := storage.GetSessionWithMessages(session.ID)
	if err != nil {
		t.Fatalf("GetSessionWithMessages error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result")
	}
	if result.Title != "Test" {
		t.Errorf("Expected title 'Test', got '%s'", result.Title)
	}
	if len(result.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result.Messages))
	}
}

func TestSessionStorage_CascadeDelete(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})
	msg, _ := storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleUser, Content: "Hello"})
	cp, _ := storage.CreateCheckpoint(CreateCheckpointInput{SessionID: session.ID, DialogStateHash: "hash"})

	// Delete session should cascade delete messages and checkpoints
	storage.DeleteSession(session.ID)

	retrievedMsg, _ := storage.GetMessage(msg.ID)
	if retrievedMsg != nil {
		t.Error("Expected message to be cascade deleted")
	}

	retrievedCp, _ := storage.GetCheckpoint(cp.ID)
	if retrievedCp != nil {
		t.Error("Expected checkpoint to be cascade deleted")
	}
}

func TestSessionStorage_QueryOptions(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	for i := 0; i < 10; i++ {
		storage.CreateSession(CreateSessionInput{Title: "Session"})
		time.Sleep(5 * time.Millisecond)
	}

	// Test limit
	sessions, _ := storage.GetAllSessions(&QueryOptions{Limit: 5})
	if len(sessions) != 5 {
		t.Errorf("Expected 5 sessions with limit, got %d", len(sessions))
	}

	// Test offset
	sessions, _ = storage.GetAllSessions(&QueryOptions{Limit: 5, Offset: 5})
	if len(sessions) != 5 {
		t.Errorf("Expected 5 sessions with offset, got %d", len(sessions))
	}

	// Test order
	sessions, _ = storage.GetAllSessions(&QueryOptions{Order: "ASC"})
	if sessions[0].CreatedAt > sessions[len(sessions)-1].CreatedAt {
		t.Error("Expected ASC order")
	}
}

func TestSessionStorage_ParentMessage(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})
	parent, _ := storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleUser, Content: "Parent"})
	child, _ := storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleAssistant, Content: "Child", ParentID: parent.ID})

	retrieved, _ := storage.GetMessage(child.ID)
	if retrieved.ParentID != parent.ID {
		t.Errorf("Expected parent ID '%s', got '%s'", parent.ID, retrieved.ParentID)
	}
}

func TestSessionStorage_NonExistent(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.GetSession("nonexistent")
	if session != nil {
		t.Error("Expected nil for nonexistent session")
	}

	msg, _ := storage.GetMessage("nonexistent")
	if msg != nil {
		t.Error("Expected nil for nonexistent message")
	}

	cp, _ := storage.GetCheckpoint("nonexistent")
	if cp != nil {
		t.Error("Expected nil for nonexistent checkpoint")
	}

	updated, _ := storage.UpdateSession("nonexistent", &Session{Title: "New"})
	if updated != nil {
		t.Error("Expected nil for update nonexistent session")
	}
}

func TestSessionStorage_Concurrent(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	// Create sessions sequentially first
	var sessionIDs []string
	for i := 0; i < 5; i++ {
		session, err := storage.CreateSession(CreateSessionInput{Title: "Concurrent"})
		if err != nil {
			t.Fatalf("CreateSession error: %v", err)
		}
		sessionIDs = append(sessionIDs, session.ID)
	}

	// Test concurrent reads
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = storage.GetAllSessions(nil)
			if n < len(sessionIDs) {
				_, _ = storage.GetSessionWithMessages(sessionIDs[n%len(sessionIDs)])
			}
		}(i)
	}
	wg.Wait()

	sessions, err := storage.GetAllSessions(nil)
	if err != nil {
		t.Fatalf("GetAllSessions error: %v", err)
	}
	if len(sessions) != 5 {
		t.Errorf("Expected 5 sessions, got %d", len(sessions))
	}
}

func TestSessionStorage_MessageUpdatesSession(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})
	originalUpdatedAt := session.UpdatedAt

	time.Sleep(10 * time.Millisecond)
	storage.CreateMessage(CreateMessageInput{SessionID: session.ID, Role: RoleUser, Content: "Hello"})

	updated, _ := storage.GetSession(session.ID)
	if updated.UpdatedAt <= originalUpdatedAt {
		t.Error("Expected session updated_at to be updated after creating message")
	}
}

func TestSessionStorage_RoleValidation(t *testing.T) {
	storage, err := NewSessionStorage("")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	session, _ := storage.CreateSession(CreateSessionInput{Title: "Test"})

	// Test all valid roles
	roles := []Role{RoleUser, RoleAssistant, RoleSystem}
	for _, role := range roles {
		msg, err := storage.CreateMessage(CreateMessageInput{
			SessionID: session.ID,
			Role:      role,
			Content:   "Test",
		})
		if err != nil {
			t.Errorf("Failed to create message with role %s: %v", role, err)
		}
		if msg.Role != role {
			t.Errorf("Expected role %s, got %s", role, msg.Role)
		}
	}
}
