package debate

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkListDebates(b *testing.B) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		_, err := m.CreateDebate(ctx, &DebateCreateRequest{
			Title:        fmt.Sprintf("debate-%04d", i),
			GeneratorID:  "generator",
			CriticID:     "critic",
			InitialInput: "review this change",
			FlowID:       fmt.Sprintf("flow-%d", i%10),
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	req := &DebateListRequest{FlowID: "flow-3", Limit: 20}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := m.ListDebates(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Debates) != 20 || result.Total != 100 {
			b.Fatalf("unexpected result: len=%d total=%d", len(result.Debates), result.Total)
		}
	}
}

func BenchmarkDetectConflicts(b *testing.B) {
	m := NewInMemoryDebateManager()
	feedback := "This has a critical security vulnerability and an inefficient implementation with a readability problem. The logic is incorrect."

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		conflicts := m.DetectConflicts("generated solution", feedback)
		if len(conflicts) != 4 {
			b.Fatalf("got %d conflicts", len(conflicts))
		}
	}
}
