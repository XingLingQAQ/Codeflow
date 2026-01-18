// Package agent - Agent service for managing AI agents and conversations
package agent

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AgentStatus 智能体状态
type AgentStatus string

const (
	AgentStatusIdle    AgentStatus = "idle"
	AgentStatusRunning AgentStatus = "running"
	AgentStatusPaused  AgentStatus = "paused"
	AgentStatusStopped AgentStatus = "stopped"
	AgentStatusError   AgentStatus = "error"
)

// AgentRole 智能体角色
type AgentRole string

const (
	RoleMain      AgentRole = "main"
	RoleCoder     AgentRole = "coder"
	RoleSubExpert AgentRole = "sub_expert"
	RoleReviewer  AgentRole = "reviewer"
	RolePlanner   AgentRole = "planner"
)

// Agent 智能体
type Agent struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Role        AgentRole   `json:"role"`
	Status      AgentStatus `json:"status"`
	Model       string      `json:"model"`
	SessionID   string      `json:"session_id,omitempty"`
	StartedAt   int64       `json:"started_at"`
	LastActiveAt int64      `json:"last_active_at"`
	TokensUsed  int         `json:"tokens_used"`
	TaskCount   int         `json:"task_count"`
	ErrorCount  int         `json:"error_count"`
}

// AgentLog 智能体日志
type AgentLog struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id"`
	Level     string `json:"level"` // debug, info, warn, error
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// CallTrace 调用追踪
type CallTrace struct {
	ID        string                 `json:"id"`
	ParentID  string                 `json:"parent_id,omitempty"`
	AgentID   string                 `json:"agent_id"`
	AgentRole AgentRole              `json:"agent_role"`
	ToolName  string                 `json:"tool_name"`
	Input     map[string]interface{} `json:"input,omitempty"`
	Output    string                 `json:"output,omitempty"`
	Status    string                 `json:"status"` // pending, running, completed, failed
	StartTime int64                  `json:"start_time"`
	EndTime   int64                  `json:"end_time,omitempty"`
	Duration  int64                  `json:"duration_ms,omitempty"`
	Children  []*CallTrace           `json:"children,omitempty"`
}

// Conversation 对话
type Conversation struct {
	ID          string       `json:"id"`
	SessionID   string       `json:"session_id"`
	Status      string       `json:"status"` // active, paused, stopped, completed
	StartedAt   int64        `json:"started_at"`
	UpdatedAt   int64        `json:"updated_at"`
	AgentIDs    []string     `json:"agent_ids"`
	TraceRoot   *CallTrace   `json:"trace_root,omitempty"`
	MessageCount int         `json:"message_count"`
}

// AgentListResponse 智能体列表响应
type AgentListResponse struct {
	Agents []Agent `json:"agents"`
	Total  int     `json:"total"`
}

// AgentLogsResponse 智能体日志响应
type AgentLogsResponse struct {
	Logs    []AgentLog `json:"logs"`
	Total   int        `json:"total"`
	HasMore bool       `json:"has_more"`
}

// ConversationTraceResponse 对话追踪响应
type ConversationTraceResponse struct {
	SessionID string       `json:"session_id"`
	Trace     *CallTrace   `json:"trace"`
	Agents    []Agent      `json:"agents"`
	Duration  int64        `json:"duration_ms"`
}

// IAgentService 智能体服务接口
type IAgentService interface {
	ListAgents(ctx context.Context) (*AgentListResponse, error)
	GetAgent(ctx context.Context, id string) (*Agent, error)
	GetAgentLogs(ctx context.Context, agentID string, limit int) (*AgentLogsResponse, error)
	GetConversationTrace(ctx context.Context, sessionID string) (*ConversationTraceResponse, error)
	StopConversation(ctx context.Context, sessionID string) error
	RetryConversation(ctx context.Context, sessionID string) error

	// 内部方法
	RegisterAgent(agent *Agent)
	UpdateAgentStatus(agentID string, status AgentStatus)
	AddLog(agentID string, level string, message string, metadata map[string]interface{})
	StartTrace(sessionID string, agentID string, toolName string, input map[string]interface{}) string
	EndTrace(traceID string, output string, status string)
}

// InMemoryAgentService 内存实现的智能体服务
type InMemoryAgentService struct {
	mu            sync.RWMutex
	agents        map[string]*Agent
	logs          map[string][]*AgentLog // agentID -> logs
	conversations map[string]*Conversation
	traces        map[string]*CallTrace
}

// NewInMemoryAgentService 创建内存智能体服务
func NewInMemoryAgentService() *InMemoryAgentService {
	return &InMemoryAgentService{
		agents:        make(map[string]*Agent),
		logs:          make(map[string][]*AgentLog),
		conversations: make(map[string]*Conversation),
		traces:        make(map[string]*CallTrace),
	}
}

// ListAgents 列出所有智能体
func (s *InMemoryAgentService) ListAgents(ctx context.Context) (*AgentListResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agents := make([]Agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, *a)
	}

	sort.Slice(agents, func(i, j int) bool {
		return agents[i].LastActiveAt > agents[j].LastActiveAt
	})

	return &AgentListResponse{
		Agents: agents,
		Total:  len(agents),
	}, nil
}

// GetAgent 获取单个智能体
func (s *InMemoryAgentService) GetAgent(ctx context.Context, id string) (*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agent, ok := s.agents[id]
	if !ok {
		return nil, nil
	}
	return agent, nil
}

// GetAgentLogs 获取智能体日志
func (s *InMemoryAgentService) GetAgentLogs(ctx context.Context, agentID string, limit int) (*AgentLogsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	agentLogs, ok := s.logs[agentID]
	if !ok {
		return &AgentLogsResponse{
			Logs:    []AgentLog{},
			Total:   0,
			HasMore: false,
		}, nil
	}

	// 按时间倒序
	sorted := make([]*AgentLog, len(agentLogs))
	copy(sorted, agentLogs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp > sorted[j].Timestamp
	})

	total := len(sorted)
	if limit > total {
		limit = total
	}

	logs := make([]AgentLog, limit)
	for i := 0; i < limit; i++ {
		logs[i] = *sorted[i]
	}

	return &AgentLogsResponse{
		Logs:    logs,
		Total:   total,
		HasMore: total > limit,
	}, nil
}

// GetConversationTrace 获取对话追踪
func (s *InMemoryAgentService) GetConversationTrace(ctx context.Context, sessionID string) (*ConversationTraceResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conv, ok := s.conversations[sessionID]
	if !ok {
		return nil, nil
	}

	// 收集相关智能体
	agents := make([]Agent, 0)
	for _, agentID := range conv.AgentIDs {
		if agent, ok := s.agents[agentID]; ok {
			agents = append(agents, *agent)
		}
	}

	var duration int64
	if conv.TraceRoot != nil && conv.TraceRoot.EndTime > 0 {
		duration = conv.TraceRoot.EndTime - conv.TraceRoot.StartTime
	}

	return &ConversationTraceResponse{
		SessionID: sessionID,
		Trace:     conv.TraceRoot,
		Agents:    agents,
		Duration:  duration,
	}, nil
}

// StopConversation 停止对话
func (s *InMemoryAgentService) StopConversation(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conv, ok := s.conversations[sessionID]
	if !ok {
		return nil
	}

	conv.Status = "stopped"
	conv.UpdatedAt = time.Now().Unix()

	// 停止相关智能体
	for _, agentID := range conv.AgentIDs {
		if agent, ok := s.agents[agentID]; ok {
			agent.Status = AgentStatusStopped
		}
	}

	return nil
}

// RetryConversation 重试对话
func (s *InMemoryAgentService) RetryConversation(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conv, ok := s.conversations[sessionID]
	if !ok {
		return nil
	}

	conv.Status = "active"
	conv.UpdatedAt = time.Now().Unix()

	// 重新激活相关智能体
	for _, agentID := range conv.AgentIDs {
		if agent, ok := s.agents[agentID]; ok {
			agent.Status = AgentStatusRunning
			agent.LastActiveAt = time.Now().Unix()
		}
	}

	return nil
}

// RegisterAgent 注册智能体
func (s *InMemoryAgentService) RegisterAgent(agent *Agent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if agent.ID == "" {
		agent.ID = uuid.New().String()
	}
	now := time.Now().Unix()
	agent.StartedAt = now
	agent.LastActiveAt = now

	s.agents[agent.ID] = agent
	s.logs[agent.ID] = make([]*AgentLog, 0)

	// 如果有sessionID，关联到对话
	if agent.SessionID != "" {
		if conv, ok := s.conversations[agent.SessionID]; ok {
			conv.AgentIDs = append(conv.AgentIDs, agent.ID)
		} else {
			s.conversations[agent.SessionID] = &Conversation{
				ID:        uuid.New().String(),
				SessionID: agent.SessionID,
				Status:    "active",
				StartedAt: now,
				UpdatedAt: now,
				AgentIDs:  []string{agent.ID},
			}
		}
	}
}

// UpdateAgentStatus 更新智能体状态
func (s *InMemoryAgentService) UpdateAgentStatus(agentID string, status AgentStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if agent, ok := s.agents[agentID]; ok {
		agent.Status = status
		agent.LastActiveAt = time.Now().Unix()
	}
}

// AddLog 添加日志
func (s *InMemoryAgentService) AddLog(agentID string, level string, message string, metadata map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log := &AgentLog{
		ID:        uuid.New().String(),
		AgentID:   agentID,
		Level:     level,
		Message:   message,
		Timestamp: time.Now().Unix(),
		Metadata:  metadata,
	}

	if _, ok := s.logs[agentID]; !ok {
		s.logs[agentID] = make([]*AgentLog, 0)
	}
	s.logs[agentID] = append(s.logs[agentID], log)

	// 限制日志数量
	if len(s.logs[agentID]) > 1000 {
		s.logs[agentID] = s.logs[agentID][len(s.logs[agentID])-1000:]
	}
}

// StartTrace 开始追踪
func (s *InMemoryAgentService) StartTrace(sessionID string, agentID string, toolName string, input map[string]interface{}) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	trace := &CallTrace{
		ID:        uuid.New().String(),
		AgentID:   agentID,
		ToolName:  toolName,
		Input:     input,
		Status:    "running",
		StartTime: time.Now().UnixMilli(),
		Children:  make([]*CallTrace, 0),
	}

	// 获取智能体角色
	if agent, ok := s.agents[agentID]; ok {
		trace.AgentRole = agent.Role
	}

	s.traces[trace.ID] = trace

	// 关联到对话
	if conv, ok := s.conversations[sessionID]; ok {
		if conv.TraceRoot == nil {
			conv.TraceRoot = trace
		} else {
			// 添加为子追踪
			conv.TraceRoot.Children = append(conv.TraceRoot.Children, trace)
			trace.ParentID = conv.TraceRoot.ID
		}
	}

	return trace.ID
}

// EndTrace 结束追踪
func (s *InMemoryAgentService) EndTrace(traceID string, output string, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if trace, ok := s.traces[traceID]; ok {
		trace.Output = output
		trace.Status = status
		trace.EndTime = time.Now().UnixMilli()
		trace.Duration = trace.EndTime - trace.StartTime
	}
}

// 全局服务实例
var defaultAgentService IAgentService

// GetAgentService 获取智能体服务实例
func GetAgentService() IAgentService {
	if defaultAgentService == nil {
		defaultAgentService = NewInMemoryAgentService()
	}
	return defaultAgentService
}

// SetAgentService 设置智能体服务实例
func SetAgentService(svc IAgentService) {
	defaultAgentService = svc
}
