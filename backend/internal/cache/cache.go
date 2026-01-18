package cache

import (
	"sync"
	"time"
)

// CacheEntry 缓存条目
type CacheEntry struct {
	Key            string      `json:"key"`
	Value          interface{} `json:"value"`
	PrefixHash     string      `json:"prefix_hash"`
	TokenCount     int         `json:"token_count"`
	CreatedAt      int64       `json:"created_at"`
	LastAccessedAt int64       `json:"last_accessed_at"`
	AccessCount    int         `json:"access_count"`
	TTL            int64       `json:"ttl"`
}

// EvictionPolicy 淘汰策略
type EvictionPolicy string

const (
	PolicyLRU EvictionPolicy = "lru"
	PolicyLFU EvictionPolicy = "lfu"
	PolicyTTL EvictionPolicy = "ttl"
)

// PrefixCacheConfig 前缀缓存配置
type PrefixCacheConfig struct {
	MaxEntries           int            `json:"max_entries"`
	MaxTokens            int            `json:"max_tokens"`
	DefaultTTL           int64          `json:"default_ttl"`
	MinPrefixLength      int            `json:"min_prefix_length"`
	EvictionPolicy       EvictionPolicy `json:"eviction_policy"`
	EnableCompression    bool           `json:"enable_compression"`
	CompressionThreshold int            `json:"compression_threshold"`
}

// CacheStats 缓存统计
type CacheStats struct {
	Hits          int     `json:"hits"`
	Misses        int     `json:"misses"`
	HitRate       float64 `json:"hit_rate"`
	TotalEntries  int     `json:"total_entries"`
	TotalTokens   int     `json:"total_tokens"`
	Evictions     int     `json:"evictions"`
	AvgAccessTime float64 `json:"avg_access_time"`
}

// PrefixMatchResult 前缀匹配结果
type PrefixMatchResult struct {
	Found               bool        `json:"found"`
	Entry               *CacheEntry `json:"entry,omitempty"`
	MatchedPrefixLength int         `json:"matched_prefix_length"`
	MatchedTokens       int         `json:"matched_tokens"`
	RemainingTokens     int         `json:"remaining_tokens"`
}

// BudgetCategory 预算分类
type BudgetCategory string

const (
	CategorySystem   BudgetCategory = "system"
	CategoryHistory  BudgetCategory = "history"
	CategoryContext  BudgetCategory = "context"
	CategoryUser     BudgetCategory = "user"
	CategoryReserved BudgetCategory = "reserved"
)

// BudgetAllocation 预算分配
type BudgetAllocation struct {
	Category     BudgetCategory `json:"category"`
	Tokens       int            `json:"tokens"`
	Priority     int            `json:"priority"`
	Compressible bool           `json:"compressible"`
}

// ContextBudget 上下文预算
type ContextBudget struct {
	TotalTokens     int                 `json:"total_tokens"`
	UsedTokens      int                 `json:"used_tokens"`
	RemainingTokens int                 `json:"remaining_tokens"`
	Allocations     []*BudgetAllocation `json:"allocations"`
}

// BLAConfig BLA配置（Base Level Activation）
type BLAConfig struct {
	DecayRate       float64 `json:"decay_rate"`
	BaseActivation  float64 `json:"base_activation"`
	RecencyWeight   float64 `json:"recency_weight"`
	FrequencyWeight float64 `json:"frequency_weight"`
	MinActivation   float64 `json:"min_activation"`
}

// ObservationMaskConfig 观察屏蔽配置
type ObservationMaskConfig struct {
	Enabled           bool     `json:"enabled"`
	MaskPatterns      []string `json:"mask_patterns"`
	PreserveStructure bool     `json:"preserve_structure"`
	ReplacementToken  string   `json:"replacement_token"`
}

// IPrefixCache 前缀缓存接口
type IPrefixCache interface {
	Get(prefix string) PrefixMatchResult
	Set(prefix string, value interface{}, tokenCount int, ttl int64)
	Has(prefix string) bool
	Delete(prefix string) bool
	Clear()
	GetStats() CacheStats
	Prune() int
	Configure(config PrefixCacheConfig)
}

// IContextBudgetManager 上下文预算管理器接口
type IContextBudgetManager interface {
	Allocate(category BudgetCategory, tokens int) bool
	Release(category BudgetCategory, tokens int)
	GetBudget() ContextBudget
	CanAllocate(tokens int) bool
	Compress(targetTokens int) int
	Reset()
}

// DefaultPrefixCacheConfig 默认前缀缓存配置
var DefaultPrefixCacheConfig = PrefixCacheConfig{
	MaxEntries:           1000,
	MaxTokens:            500000,
	DefaultTTL:           3600000, // 1 hour in ms
	MinPrefixLength:      100,
	EvictionPolicy:       PolicyLRU,
	EnableCompression:    true,
	CompressionThreshold: 10000,
}

// DefaultContextBudget 默认上下文预算
var DefaultContextBudget = ContextBudget{
	TotalTokens:     128000,
	UsedTokens:      0,
	RemainingTokens: 128000,
	Allocations: []*BudgetAllocation{
		{Category: CategorySystem, Tokens: 2000, Priority: 1, Compressible: false},
		{Category: CategoryHistory, Tokens: 50000, Priority: 3, Compressible: true},
		{Category: CategoryContext, Tokens: 60000, Priority: 2, Compressible: true},
		{Category: CategoryUser, Tokens: 10000, Priority: 1, Compressible: false},
		{Category: CategoryReserved, Tokens: 6000, Priority: 0, Compressible: false},
	},
}

// DefaultBLAConfig 默认BLA配置
var DefaultBLAConfig = BLAConfig{
	DecayRate:       0.5,
	BaseActivation:  0.0,
	RecencyWeight:   0.7,
	FrequencyWeight: 0.3,
	MinActivation:   0.01,
}

// PrefixCache 前缀缓存实现
type PrefixCache struct {
	config      PrefixCacheConfig
	cache       map[string]*CacheEntry
	prefixIndex map[string]map[string]bool
	stats       CacheStats
	accessTimes []float64
	mu          sync.RWMutex
}

// NewPrefixCache 创建前缀缓存
func NewPrefixCache(config *PrefixCacheConfig) *PrefixCache {
	cfg := DefaultPrefixCacheConfig
	if config != nil {
		cfg = *config
	}
	return &PrefixCache{
		config:      cfg,
		cache:       make(map[string]*CacheEntry),
		prefixIndex: make(map[string]map[string]bool),
		stats: CacheStats{
			Hits:          0,
			Misses:        0,
			HitRate:       0,
			TotalEntries:  0,
			TotalTokens:   0,
			Evictions:     0,
			AvgAccessTime: 0,
		},
		accessTimes: make([]float64, 0),
	}
}

// Configure 配置缓存
func (c *PrefixCache) Configure(config PrefixCacheConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = config
}

// Get 获取缓存
func (c *PrefixCache) Get(prefix string) PrefixMatchResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	startTime := time.Now()
	prefixHash := c.hashPrefix(prefix)

	// 精确匹配
	if entry, ok := c.cache[prefixHash]; ok && !c.isExpired(entry) {
		c.recordHit(entry, startTime)
		return PrefixMatchResult{
			Found:               true,
			Entry:               entry,
			MatchedPrefixLength: len(prefix),
			MatchedTokens:       entry.TokenCount,
			RemainingTokens:     0,
		}
	}

	// 前缀匹配
	if result := c.findLongestPrefixMatch(prefix); result != nil && result.Entry != nil {
		c.recordHit(result.Entry, startTime)
		return *result
	}

	// 未命中
	c.recordMiss(startTime)
	return PrefixMatchResult{
		Found:               false,
		MatchedPrefixLength: 0,
		MatchedTokens:       0,
		RemainingTokens:     c.estimateTokens(prefix),
	}
}

// Set 设置缓存
func (c *PrefixCache) Set(prefix string, value interface{}, tokenCount int, ttl int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(prefix) < c.config.MinPrefixLength {
		return
	}

	c.ensureCapacity(tokenCount)

	prefixHash := c.hashPrefix(prefix)
	now := time.Now().UnixMilli()

	if ttl <= 0 {
		ttl = c.config.DefaultTTL
	}

	entry := &CacheEntry{
		Key:            prefixHash,
		Value:          value,
		PrefixHash:     prefixHash,
		TokenCount:     tokenCount,
		CreatedAt:      now,
		LastAccessedAt: now,
		AccessCount:    1,
		TTL:            ttl,
	}

	c.cache[prefixHash] = entry
	c.indexPrefix(prefix, prefixHash)
	c.updateStats()
}

// Has 检查是否存在
func (c *PrefixCache) Has(prefix string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	prefixHash := c.hashPrefix(prefix)
	entry, ok := c.cache[prefixHash]
	return ok && !c.isExpired(entry)
}

// Delete 删除缓存
func (c *PrefixCache) Delete(prefix string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	prefixHash := c.hashPrefix(prefix)
	if _, ok := c.cache[prefixHash]; ok {
		delete(c.cache, prefixHash)
		c.removeFromIndex(prefix, prefixHash)
		c.updateStats()
		return true
	}
	return false
}

// Clear 清空缓存
func (c *PrefixCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*CacheEntry)
	c.prefixIndex = make(map[string]map[string]bool)
	c.stats = CacheStats{}
	c.accessTimes = make([]float64, 0)
}

// GetStats 获取统计
func (c *PrefixCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// Prune 清理过期条目
func (c *PrefixCache) Prune() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	pruned := 0
	for key, entry := range c.cache {
		if c.isExpired(entry) {
			delete(c.cache, key)
			pruned++
		}
	}

	if pruned > 0 {
		c.rebuildIndex()
		c.updateStats()
	}

	return pruned
}

// ==================== 私有方法 ====================

func (c *PrefixCache) hashPrefix(prefix string) string {
	hash := 0
	for i := 0; i < len(prefix); i++ {
		char := int(prefix[i])
		hash = ((hash << 5) - hash) + char
		hash = hash & hash
	}
	return "prefix_" + itoa36(hash) + "_" + itoa(len(prefix))
}

func (c *PrefixCache) indexPrefix(prefix string, hash string) {
	segments := []string{}
	if len(prefix) >= 100 {
		segments = append(segments, prefix[:100])
	}
	if len(prefix) >= 500 {
		segments = append(segments, prefix[:500])
	}
	if len(prefix) >= 1000 {
		segments = append(segments, prefix[:1000])
	}

	for _, segment := range segments {
		segmentHash := c.hashPrefix(segment)
		if c.prefixIndex[segmentHash] == nil {
			c.prefixIndex[segmentHash] = make(map[string]bool)
		}
		c.prefixIndex[segmentHash][hash] = true
	}
}

func (c *PrefixCache) removeFromIndex(prefix string, hash string) {
	for _, hashes := range c.prefixIndex {
		delete(hashes, hash)
	}
}

func (c *PrefixCache) rebuildIndex() {
	c.prefixIndex = make(map[string]map[string]bool)
}

func (c *PrefixCache) findLongestPrefixMatch(prefix string) *PrefixMatchResult {
	lengths := []int{1000, 500, 100}

	for _, length := range lengths {
		if length > len(prefix) {
			continue
		}
		segment := prefix[:length]
		segmentHash := c.hashPrefix(segment)
		if candidates, ok := c.prefixIndex[segmentHash]; ok {
			for candidateHash := range candidates {
				if entry, ok := c.cache[candidateHash]; ok && !c.isExpired(entry) {
					return &PrefixMatchResult{
						Found:               true,
						Entry:               entry,
						MatchedPrefixLength: length,
						MatchedTokens:       entry.TokenCount * length / len(prefix),
						RemainingTokens:     c.estimateTokens(prefix[length:]),
					}
				}
			}
		}
	}

	return nil
}

func (c *PrefixCache) isExpired(entry *CacheEntry) bool {
	return time.Now().UnixMilli()-entry.CreatedAt > entry.TTL
}

func (c *PrefixCache) ensureCapacity(newTokens int) {
	for len(c.cache) >= c.config.MaxEntries {
		c.evictOne()
	}
	for c.stats.TotalTokens+newTokens > c.config.MaxTokens {
		c.evictOne()
	}
}

func (c *PrefixCache) evictOne() {
	var victimKey string

	switch c.config.EvictionPolicy {
	case PolicyLRU:
		victimKey = c.findLRUVictim()
	case PolicyLFU:
		victimKey = c.findLFUVictim()
	case PolicyTTL:
		victimKey = c.findTTLVictim()
	}

	if victimKey != "" {
		delete(c.cache, victimKey)
		c.stats.Evictions++
	}
}

func (c *PrefixCache) findLRUVictim() string {
	var oldest *CacheEntry
	var oldestKey string

	for key, entry := range c.cache {
		if oldest == nil || entry.LastAccessedAt < oldest.LastAccessedAt {
			oldest = entry
			oldestKey = key
		}
	}

	return oldestKey
}

func (c *PrefixCache) findLFUVictim() string {
	var leastUsed *CacheEntry
	var leastUsedKey string

	for key, entry := range c.cache {
		if leastUsed == nil || entry.AccessCount < leastUsed.AccessCount {
			leastUsed = entry
			leastUsedKey = key
		}
	}

	return leastUsedKey
}

func (c *PrefixCache) findTTLVictim() string {
	var soonestExpiry *CacheEntry
	var soonestKey string

	for key, entry := range c.cache {
		expiryTime := entry.CreatedAt + entry.TTL
		if soonestExpiry == nil || expiryTime < soonestExpiry.CreatedAt+soonestExpiry.TTL {
			soonestExpiry = entry
			soonestKey = key
		}
	}

	return soonestKey
}

func (c *PrefixCache) recordHit(entry *CacheEntry, startTime time.Time) {
	entry.LastAccessedAt = time.Now().UnixMilli()
	entry.AccessCount++
	c.stats.Hits++
	c.recordAccessTime(startTime)
	c.updateHitRate()
}

func (c *PrefixCache) recordMiss(startTime time.Time) {
	c.stats.Misses++
	c.recordAccessTime(startTime)
	c.updateHitRate()
}

func (c *PrefixCache) recordAccessTime(startTime time.Time) {
	accessTime := float64(time.Since(startTime).Milliseconds())
	c.accessTimes = append(c.accessTimes, accessTime)
	if len(c.accessTimes) > 1000 {
		c.accessTimes = c.accessTimes[1:]
	}
	sum := 0.0
	for _, t := range c.accessTimes {
		sum += t
	}
	c.stats.AvgAccessTime = sum / float64(len(c.accessTimes))
}

func (c *PrefixCache) updateHitRate() {
	total := c.stats.Hits + c.stats.Misses
	if total > 0 {
		c.stats.HitRate = float64(c.stats.Hits) / float64(total)
	}
}

func (c *PrefixCache) updateStats() {
	c.stats.TotalEntries = len(c.cache)
	totalTokens := 0
	for _, entry := range c.cache {
		totalTokens += entry.TokenCount
	}
	c.stats.TotalTokens = totalTokens
}

func (c *PrefixCache) estimateTokens(text string) int {
	return (len(text) + 3) / 4
}

// 辅助函数
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func itoa36(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%36]
		n /= 36
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ContextBudgetManager 上下文预算管理器
type ContextBudgetManager struct {
	budget ContextBudget
	mu     sync.RWMutex
}

// NewContextBudgetManager 创建上下文预算管理器
func NewContextBudgetManager(totalTokens int) *ContextBudgetManager {
	if totalTokens <= 0 {
		totalTokens = DefaultContextBudget.TotalTokens
	}

	allocations := make([]*BudgetAllocation, len(DefaultContextBudget.Allocations))
	for i, a := range DefaultContextBudget.Allocations {
		allocations[i] = &BudgetAllocation{
			Category:     a.Category,
			Tokens:       a.Tokens,
			Priority:     a.Priority,
			Compressible: a.Compressible,
		}
	}

	return &ContextBudgetManager{
		budget: ContextBudget{
			TotalTokens:     totalTokens,
			UsedTokens:      0,
			RemainingTokens: totalTokens,
			Allocations:     allocations,
		},
	}
}

// Allocate 分配预算
func (m *ContextBudgetManager) Allocate(category BudgetCategory, tokens int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.budget.RemainingTokens < tokens {
		return false
	}

	var allocation *BudgetAllocation
	for _, a := range m.budget.Allocations {
		if a.Category == category {
			allocation = a
			break
		}
	}

	if allocation != nil {
		allocation.Tokens += tokens
	} else {
		m.budget.Allocations = append(m.budget.Allocations, &BudgetAllocation{
			Category:     category,
			Tokens:       tokens,
			Priority:     2,
			Compressible: true,
		})
	}

	m.budget.UsedTokens += tokens
	m.budget.RemainingTokens -= tokens
	return true
}

// Release 释放预算
func (m *ContextBudgetManager) Release(category BudgetCategory, tokens int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, a := range m.budget.Allocations {
		if a.Category == category {
			released := tokens
			if released > a.Tokens {
				released = a.Tokens
			}
			a.Tokens -= released
			m.budget.UsedTokens -= released
			m.budget.RemainingTokens += released
			return
		}
	}
}

// GetBudget 获取预算
func (m *ContextBudgetManager) GetBudget() ContextBudget {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allocations := make([]*BudgetAllocation, len(m.budget.Allocations))
	for i, a := range m.budget.Allocations {
		allocations[i] = &BudgetAllocation{
			Category:     a.Category,
			Tokens:       a.Tokens,
			Priority:     a.Priority,
			Compressible: a.Compressible,
		}
	}

	return ContextBudget{
		TotalTokens:     m.budget.TotalTokens,
		UsedTokens:      m.budget.UsedTokens,
		RemainingTokens: m.budget.RemainingTokens,
		Allocations:     allocations,
	}
}

// CanAllocate 检查是否可分配
func (m *ContextBudgetManager) CanAllocate(tokens int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.budget.RemainingTokens >= tokens
}

// Compress 压缩预算
func (m *ContextBudgetManager) Compress(targetTokens int) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	toCompress := m.budget.UsedTokens - targetTokens
	if toCompress <= 0 {
		return 0
	}

	// 按优先级排序（高优先级最后压缩）
	compressible := make([]*BudgetAllocation, 0)
	for _, a := range m.budget.Allocations {
		if a.Compressible && a.Tokens > 0 {
			compressible = append(compressible, a)
		}
	}
	// 按优先级降序排序
	for i := 0; i < len(compressible)-1; i++ {
		for j := i + 1; j < len(compressible); j++ {
			if compressible[j].Priority > compressible[i].Priority {
				compressible[i], compressible[j] = compressible[j], compressible[i]
			}
		}
	}

	compressed := 0
	for _, allocation := range compressible {
		if compressed >= toCompress {
			break
		}

		canCompress := allocation.Tokens / 2 // 最多压缩50%
		if canCompress > toCompress-compressed {
			canCompress = toCompress - compressed
		}

		allocation.Tokens -= canCompress
		compressed += canCompress
	}

	m.budget.UsedTokens -= compressed
	m.budget.RemainingTokens += compressed

	return compressed
}

// Reset 重置预算
func (m *ContextBudgetManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	totalTokens := m.budget.TotalTokens

	allocations := make([]*BudgetAllocation, len(DefaultContextBudget.Allocations))
	for i, a := range DefaultContextBudget.Allocations {
		allocations[i] = &BudgetAllocation{
			Category:     a.Category,
			Tokens:       a.Tokens,
			Priority:     a.Priority,
			Compressible: a.Compressible,
		}
	}

	m.budget = ContextBudget{
		TotalTokens:     totalTokens,
		UsedTokens:      0,
		RemainingTokens: totalTokens,
		Allocations:     allocations,
	}
}
