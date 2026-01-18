package storage

// Role 消息角色
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Session 会话
type Session struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	Model     string `json:"model,omitempty"`
	Config    string `json:"config,omitempty"`
}

// SessionMessage 会话消息
type SessionMessage struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id"`
	Role       Role   `json:"role"`
	Content    string `json:"content"`
	Timestamp  int64  `json:"timestamp"`
	Model      string `json:"model,omitempty"`
	TokenCount int    `json:"token_count,omitempty"`
	ParentID   string `json:"parent_id,omitempty"`
}

// Checkpoint 检查点
type Checkpoint struct {
	ID              string `json:"id"`
	SessionID       string `json:"session_id"`
	GitHash         string `json:"git_hash,omitempty"`
	DialogStateHash string `json:"dialog_state_hash"`
	VectorStateHash string `json:"vector_state_hash,omitempty"`
	CreatedAt       int64  `json:"created_at"`
	Description     string `json:"description,omitempty"`
}

// SessionWithMessages 带消息的会话
type SessionWithMessages struct {
	Session
	Messages []SessionMessage `json:"messages"`
}

// CreateSessionInput 创建会话输入
type CreateSessionInput struct {
	Title  string                 `json:"title,omitempty"`
	Model  string                 `json:"model,omitempty"`
	Config map[string]interface{} `json:"config,omitempty"`
}

// CreateMessageInput 创建消息输入
type CreateMessageInput struct {
	SessionID  string `json:"session_id"`
	Role       Role   `json:"role"`
	Content    string `json:"content"`
	Model      string `json:"model,omitempty"`
	TokenCount int    `json:"token_count,omitempty"`
	ParentID   string `json:"parent_id,omitempty"`
}

// CreateCheckpointInput 创建检查点输入
type CreateCheckpointInput struct {
	SessionID       string `json:"session_id"`
	GitHash         string `json:"git_hash,omitempty"`
	DialogStateHash string `json:"dialog_state_hash"`
	VectorStateHash string `json:"vector_state_hash,omitempty"`
	Description     string `json:"description,omitempty"`
}

// QueryOptions 查询选项
type QueryOptions struct {
	Limit   int    `json:"limit,omitempty"`
	Offset  int    `json:"offset,omitempty"`
	OrderBy string `json:"order_by,omitempty"`
	Order   string `json:"order,omitempty"` // ASC or DESC
}

// ISessionStorage 会话存储接口
type ISessionStorage interface {
	// Session CRUD
	CreateSession(input CreateSessionInput) (*Session, error)
	GetSession(id string) (*Session, error)
	GetAllSessions(options *QueryOptions) ([]Session, error)
	UpdateSession(id string, updates *Session) (*Session, error)
	DeleteSession(id string) (bool, error)

	// Message CRUD
	CreateMessage(input CreateMessageInput) (*SessionMessage, error)
	GetMessage(id string) (*SessionMessage, error)
	GetSessionMessages(sessionID string, options *QueryOptions) ([]SessionMessage, error)
	DeleteMessage(id string) (bool, error)
	DeleteSessionMessages(sessionID string) (int, error)

	// Checkpoint CRUD
	CreateCheckpoint(input CreateCheckpointInput) (*Checkpoint, error)
	GetCheckpoint(id string) (*Checkpoint, error)
	GetSessionCheckpoints(sessionID string, options *QueryOptions) ([]Checkpoint, error)
	DeleteCheckpoint(id string) (bool, error)

	// Utility
	GetSessionWithMessages(id string) (*SessionWithMessages, error)
	Close() error
}

// SchemaVersion 当前 Schema 版本
const SchemaVersion = 1
