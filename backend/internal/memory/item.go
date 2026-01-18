// Package memory - Memory item types for STM/LTM management
package memory

import (
	"math"
	"time"
)

// MemoryType 记忆类型
type MemoryType string

const (
	MemoryTypeSTM MemoryType = "stm" // Short-term memory
	MemoryTypeLTM MemoryType = "ltm" // Long-term memory
)

// MemoryStatus 记忆状态
type MemoryStatus string

const (
	MemoryStatusActive         MemoryStatus = "active"
	MemoryStatusArchived       MemoryStatus = "archived"
	MemoryStatusPendingArchive MemoryStatus = "pending_archive"
	MemoryStatusPendingDelete  MemoryStatus = "pending_delete"
)

// MemoryItem 记忆项
type MemoryItem struct {
	ID           string       `json:"id"`
	Content      string       `json:"content"`
	Type         MemoryType   `json:"type"`
	Status       MemoryStatus `json:"status"`
	SessionID    string       `json:"session_id"`
	MessageIndex int          `json:"message_index"`
	Timestamp    int64        `json:"timestamp"`
	Heat         float64      `json:"heat"`     // 0-1, recency/frequency score
	Surprise     float64      `json:"surprise"` // 0-1, novelty score
	Tags         []string     `json:"tags,omitempty"`
	Source       SourceType   `json:"source"`
	IsPermanent  bool         `json:"is_permanent"`
	ArchivedAt   *int64       `json:"archived_at,omitempty"`
}

// MemoryItemCreateRequest 创建记忆请求
type MemoryItemCreateRequest struct {
	Content     string     `json:"content" binding:"required"`
	Type        MemoryType `json:"type"`
	SessionID   string     `json:"session_id"`
	Source      SourceType `json:"source"`
	Tags        []string   `json:"tags,omitempty"`
	IsPermanent bool       `json:"is_permanent"`
}

// MemoryItemUpdateRequest 更新记忆请求
type MemoryItemUpdateRequest struct {
	Tags        *[]string `json:"tags,omitempty"`
	IsPermanent *bool     `json:"is_permanent,omitempty"`
}

// MemoryListOptions 列表查询选项
type MemoryListOptions struct {
	Type      MemoryType `form:"type"`
	SessionID string     `form:"session_id"`
	Status    string     `form:"status"`
	SortBy    string     `form:"sort_by"` // timestamp, heat, surprise
	SortOrder string     `form:"order"`   // asc, desc
	Limit     int        `form:"limit"`
	Offset    int        `form:"offset"`
}

// MemoryListResponse 列表响应
type MemoryListResponse struct {
	Items      []MemoryItem `json:"items"`
	Total      int          `json:"total"`
	HasMore    bool         `json:"has_more"`
	NextOffset int          `json:"next_offset,omitempty"`
}

// CalculateHeat 计算热度分数 (基于时间衰减和访问频率)
// 使用ACT-R认知架构的BLA (Base-Level Activation) 公式简化版
func CalculateHeat(timestamp int64, accessCount int, decayRate float64) float64 {
	if decayRate <= 0 {
		decayRate = 0.5 // 默认衰减率
	}

	now := time.Now().Unix()
	ageSeconds := float64(now - timestamp)
	if ageSeconds < 0 {
		ageSeconds = 0
	}

	// 时间衰减: heat = (1 + accessCount) * e^(-decay * age_in_hours)
	ageHours := ageSeconds / 3600.0
	heat := float64(1+accessCount) * math.Exp(-decayRate*ageHours)

	// 归一化到 0-1
	if heat > 1 {
		heat = 1
	}
	if heat < 0 {
		heat = 0
	}

	return heat
}

// CalculateSurprise 计算惊喜度分数 (基于与现有记忆的相似度)
// 惊喜度 = 1 - 最大相似度 (越不相似越惊喜)
func CalculateSurprise(maxSimilarity float64) float64 {
	surprise := 1.0 - maxSimilarity
	if surprise < 0 {
		surprise = 0
	}
	if surprise > 1 {
		surprise = 1
	}
	return surprise
}

// GetSurpriseColor 根据惊喜度返回颜色
func GetSurpriseColor(surprise float64) string {
	if surprise >= 0.8 {
		return "#F44336" // Red - very surprising
	} else if surprise >= 0.6 {
		return "#FF9800" // Orange - moderately surprising
	} else if surprise >= 0.4 {
		return "#FFEB3B" // Yellow - somewhat surprising
	} else if surprise >= 0.2 {
		return "#8BC34A" // Light green - slightly surprising
	}
	return "#4CAF50" // Green - not surprising
}
