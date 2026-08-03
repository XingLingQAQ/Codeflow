package guard

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkEngineEvaluate(b *testing.B) {
	cfg := Config{Rules: map[RuleID]RuleConfig{
		RuleDuplicateSymbol: {Severity: SeverityOff},
	}}
	e := NewEngine(&cfg, nil)
	path := filepath.Join("workspace", "internal", "service.go")
	content := []byte("package internal\n\nfunc ProcessRequest() {}\n")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decision := e.Evaluate(ctx, path, content)
		if !decision.Allowed {
			b.Fatal("clean write unexpectedly denied")
		}
	}
}

func BenchmarkEngineEvaluateWithExemptions(b *testing.B) {
	cfg := Config{Rules: map[RuleID]RuleConfig{
		RuleDuplicateSymbol: {Severity: SeverityOff},
	}}
	e := NewEngine(&cfg, nil)
	for i := 0; i < 100; i++ {
		if err := e.GrantExemption(Exemption{
			Path:      filepath.Join("other", fmt.Sprintf("file-%03d.go", i)),
			ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			b.Fatal(err)
		}
	}
	path := filepath.Join("workspace", "internal", "service.go")
	content := []byte("package internal\n\nfunc ProcessRequest() {}\n")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decision := e.Evaluate(ctx, path, content)
		if !decision.Allowed {
			b.Fatal("clean write unexpectedly denied")
		}
	}
}
