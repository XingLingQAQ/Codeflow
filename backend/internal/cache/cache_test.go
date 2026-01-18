package cache

import (
	"sync"
	"testing"
	"time"
)

func TestPrefixCache_SetGet(t *testing.T) {
	cache := NewPrefixCache(nil)

	prefix := string(make([]byte, 200)) // 200字符满足最小长度要求
	cache.Set(prefix, "test value", 100, 0)

	result := cache.Get(prefix)
	if !result.Found {
		t.Error("Expected to find cached entry")
	}
	if result.Entry == nil {
		t.Error("Expected non-nil entry")
	}
	if result.Entry.TokenCount != 100 {
		t.Errorf("Expected token count 100, got %d", result.Entry.TokenCount)
	}
}

func TestPrefixCache_Has(t *testing.T) {
	cache := NewPrefixCache(nil)

	prefix := string(make([]byte, 200))
	cache.Set(prefix, "value", 50, 0)

	if !cache.Has(prefix) {
		t.Error("Expected Has to return true")
	}

	if cache.Has("nonexistent" + string(make([]byte, 200))) {
		t.Error("Expected Has to return false for nonexistent key")
	}
}

func TestPrefixCache_Delete(t *testing.T) {
	cache := NewPrefixCache(nil)

	prefix := string(make([]byte, 200))
	cache.Set(prefix, "value", 50, 0)

	if !cache.Delete(prefix) {
		t.Error("Expected Delete to return true")
	}

	if cache.Has(prefix) {
		t.Error("Expected entry to be deleted")
	}
}

func TestPrefixCache_Clear(t *testing.T) {
	cache := NewPrefixCache(nil)

	for i := 0; i < 5; i++ {
		prefix := string(make([]byte, 200+i))
		cache.Set(prefix, i, 50, 0)
	}

	stats := cache.GetStats()
	if stats.TotalEntries == 0 {
		t.Error("Expected entries before clear")
	}

	cache.Clear()

	stats = cache.GetStats()
	if stats.TotalEntries != 0 {
		t.Errorf("Expected 0 entries after clear, got %d", stats.TotalEntries)
	}
}

func TestPrefixCache_Stats(t *testing.T) {
	cache := NewPrefixCache(nil)

	prefix := string(make([]byte, 200))
	cache.Set(prefix, "value", 100, 0)

	// Hit
	cache.Get(prefix)
	// Miss
	cache.Get("nonexistent" + string(make([]byte, 200)))

	stats := cache.GetStats()
	if stats.Hits != 1 {
		t.Errorf("Expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses)
	}
	if stats.HitRate != 0.5 {
		t.Errorf("Expected hit rate 0.5, got %f", stats.HitRate)
	}
}

func TestPrefixCache_Eviction_LRU(t *testing.T) {
	config := &PrefixCacheConfig{
		MaxEntries:      2,
		MaxTokens:       500000,
		DefaultTTL:      3600000,
		MinPrefixLength: 100,
		EvictionPolicy:  PolicyLRU,
	}
	cache := NewPrefixCache(config)

	prefix1 := "prefix1" + string(make([]byte, 200))
	prefix2 := "prefix2" + string(make([]byte, 200))
	prefix3 := "prefix3" + string(make([]byte, 200))

	cache.Set(prefix1, "value1", 50, 0)
	cache.Set(prefix2, "value2", 50, 0)

	// 验证初始状态
	stats := cache.GetStats()
	if stats.TotalEntries != 2 {
		t.Errorf("Expected 2 entries, got %d", stats.TotalEntries)
	}

	// 添加第三个条目 - 应触发淘汰
	cache.Set(prefix3, "value3", 50, 0)

	stats = cache.GetStats()
	if stats.TotalEntries != 2 {
		t.Errorf("Expected 2 entries after eviction, got %d", stats.TotalEntries)
	}
	if stats.Evictions != 1 {
		t.Errorf("Expected 1 eviction, got %d", stats.Evictions)
	}

	// 新条目应该存在
	if !cache.Has(prefix3) {
		t.Error("Expected prefix3 to exist")
	}
}

func TestPrefixCache_Eviction_LFU(t *testing.T) {
	config := &PrefixCacheConfig{
		MaxEntries:      2,
		MaxTokens:       500000,
		DefaultTTL:      3600000,
		MinPrefixLength: 100,
		EvictionPolicy:  PolicyLFU,
	}
	cache := NewPrefixCache(config)

	prefix1 := "prefix1" + string(make([]byte, 200))
	prefix2 := "prefix2" + string(make([]byte, 200))
	prefix3 := "prefix3" + string(make([]byte, 200))

	cache.Set(prefix1, "value1", 50, 0)
	cache.Set(prefix2, "value2", 50, 0)

	// Access prefix1 multiple times
	cache.Get(prefix1)
	cache.Get(prefix1)
	cache.Get(prefix1)

	// Add third entry - should evict prefix2 (least frequently accessed)
	cache.Set(prefix3, "value3", 50, 0)

	if cache.Has(prefix2) {
		t.Error("Expected prefix2 to be evicted")
	}
	if !cache.Has(prefix1) {
		t.Error("Expected prefix1 to still exist")
	}
}

func TestPrefixCache_TTL_Expiration(t *testing.T) {
	cache := NewPrefixCache(nil)

	prefix := string(make([]byte, 200))
	cache.Set(prefix, "value", 50, 50) // 50ms TTL

	if !cache.Has(prefix) {
		t.Error("Expected entry to exist initially")
	}

	time.Sleep(100 * time.Millisecond)

	if cache.Has(prefix) {
		t.Error("Expected entry to be expired")
	}
}

func TestPrefixCache_Prune(t *testing.T) {
	cache := NewPrefixCache(nil)

	prefix1 := "prefix1" + string(make([]byte, 200))
	prefix2 := "prefix2" + string(make([]byte, 200))

	cache.Set(prefix1, "value1", 50, 50) // 50ms TTL
	cache.Set(prefix2, "value2", 50, 0)  // Default TTL

	time.Sleep(100 * time.Millisecond)

	pruned := cache.Prune()
	if pruned != 1 {
		t.Errorf("Expected 1 pruned entry, got %d", pruned)
	}

	if cache.Has(prefix1) {
		t.Error("Expected prefix1 to be pruned")
	}
	if !cache.Has(prefix2) {
		t.Error("Expected prefix2 to still exist")
	}
}

func TestPrefixCache_MinPrefixLength(t *testing.T) {
	cache := NewPrefixCache(nil)

	shortPrefix := "short" // Less than 100 chars
	cache.Set(shortPrefix, "value", 50, 0)

	if cache.Has(shortPrefix) {
		t.Error("Expected short prefix to not be cached")
	}
}

func TestPrefixCache_Concurrent(t *testing.T) {
	cache := NewPrefixCache(nil)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			prefix := string(make([]byte, 200+n))
			cache.Set(prefix, n, 50, 0)
		}(i)

		go func(n int) {
			defer wg.Done()
			prefix := string(make([]byte, 200+n))
			cache.Get(prefix)
		}(i)
	}

	wg.Wait()

	stats := cache.GetStats()
	if stats.TotalEntries == 0 {
		t.Error("Expected some entries after concurrent operations")
	}
}

func TestContextBudgetManager_Allocate(t *testing.T) {
	mgr := NewContextBudgetManager(128000)

	if !mgr.Allocate(CategoryHistory, 1000) {
		t.Error("Expected allocation to succeed")
	}

	budget := mgr.GetBudget()
	if budget.UsedTokens != 1000 {
		t.Errorf("Expected used tokens 1000, got %d", budget.UsedTokens)
	}
	if budget.RemainingTokens != 127000 {
		t.Errorf("Expected remaining tokens 127000, got %d", budget.RemainingTokens)
	}
}

func TestContextBudgetManager_Release(t *testing.T) {
	mgr := NewContextBudgetManager(128000)

	mgr.Allocate(CategoryHistory, 1000)
	mgr.Release(CategoryHistory, 500)

	budget := mgr.GetBudget()
	if budget.UsedTokens != 500 {
		t.Errorf("Expected used tokens 500, got %d", budget.UsedTokens)
	}
}

func TestContextBudgetManager_CanAllocate(t *testing.T) {
	mgr := NewContextBudgetManager(1000)

	if !mgr.CanAllocate(500) {
		t.Error("Expected to be able to allocate 500")
	}

	mgr.Allocate(CategoryHistory, 800)

	if mgr.CanAllocate(500) {
		t.Error("Expected to not be able to allocate 500 after using 800")
	}
}

func TestContextBudgetManager_Compress(t *testing.T) {
	mgr := NewContextBudgetManager(10000)

	// 分配一些预算
	mgr.Allocate(CategoryHistory, 5000)  // compressible, priority 3
	mgr.Allocate(CategoryContext, 3000)  // compressible, priority 2

	// 尝试压缩到6000
	compressed := mgr.Compress(6000)

	if compressed <= 0 {
		t.Error("Expected some compression")
	}

	budget := mgr.GetBudget()
	if budget.UsedTokens > 6000 {
		t.Errorf("Expected used tokens <= 6000 after compression, got %d", budget.UsedTokens)
	}
}

func TestContextBudgetManager_Reset(t *testing.T) {
	mgr := NewContextBudgetManager(128000)

	mgr.Allocate(CategoryHistory, 5000)
	mgr.Reset()

	budget := mgr.GetBudget()
	if budget.UsedTokens != 0 {
		t.Errorf("Expected used tokens 0 after reset, got %d", budget.UsedTokens)
	}
	if budget.RemainingTokens != 128000 {
		t.Errorf("Expected remaining tokens 128000 after reset, got %d", budget.RemainingTokens)
	}
}

func TestContextBudgetManager_AllocateNewCategory(t *testing.T) {
	mgr := NewContextBudgetManager(128000)

	// 分配到一个不在默认列表中的类别
	// 实际上所有类别都在默认列表中，但可以增加tokens
	if !mgr.Allocate(CategoryHistory, 1000) {
		t.Error("Expected allocation to history to succeed")
	}

	budget := mgr.GetBudget()
	var historyAlloc *BudgetAllocation
	for _, a := range budget.Allocations {
		if a.Category == CategoryHistory {
			historyAlloc = a
			break
		}
	}

	if historyAlloc == nil {
		t.Error("Expected history allocation to exist")
	}
	// 默认history是50000，加上1000应该是51000
	if historyAlloc.Tokens != 50000+1000 {
		t.Errorf("Expected history tokens 51000, got %d", historyAlloc.Tokens)
	}
}

func TestContextBudgetManager_Concurrent(t *testing.T) {
	mgr := NewContextBudgetManager(128000)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			mgr.Allocate(CategoryHistory, 100)
		}()
		go func() {
			defer wg.Done()
			mgr.Release(CategoryHistory, 50)
		}()
	}

	wg.Wait()

	budget := mgr.GetBudget()
	if budget.TotalTokens != 128000 {
		t.Errorf("Expected total tokens to remain 128000, got %d", budget.TotalTokens)
	}
}

func TestItoa36(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{35, "z"},
		{36, "10"},
		{-1, "-1"},
	}

	for _, tt := range tests {
		got := itoa36(tt.input)
		if got != tt.want {
			t.Errorf("itoa36(%d) = %s, want %s", tt.input, got, tt.want)
		}
	}
}
