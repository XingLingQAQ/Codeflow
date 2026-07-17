package floweng

import "sync"

var (
	defaultEngine Engine
	engineMu      sync.RWMutex
)

// HasEngine reports whether a process-wide engine was explicitly set
// (does not trigger lazy construction).
func HasEngine() bool {
	engineMu.RLock()
	defer engineMu.RUnlock()
	return defaultEngine != nil
}

// GetEngine returns the process-wide engine, creating an in-memory default if needed.
func GetEngine() Engine {
	engineMu.RLock()
	e := defaultEngine
	engineMu.RUnlock()
	if e != nil {
		return e
	}
	engineMu.Lock()
	defer engineMu.Unlock()
	if defaultEngine == nil {
		defaultEngine = NewInMemoryEngine(nil)
	}
	return defaultEngine
}

// SetEngine sets the process-wide engine (bootstrap / tests). nil clears.
func SetEngine(e Engine) {
	engineMu.Lock()
	defer engineMu.Unlock()
	defaultEngine = e
}
