package skill

import (
	"context"
	"strings"
	"testing"
)

func TestCRUDAndMatch(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()

	// builtins present
	list, err := r.List(ctx)
	if err != nil || len(list) < 2 {
		t.Fatalf("builtins list=%d err=%v", len(list), err)
	}

	created, err := r.Create(ctx, &CreateRequest{
		Name:     "API Error Shape",
		Body:     "Return structured {error, code} JSON on failures.",
		Triggers: []string{"api error", "handler"},
		StageTags: []string{"coding"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.Get(ctx, created.ID)
	if err != nil || got.Name != "API Error Shape" {
		t.Fatalf("get=%+v err=%v", got, err)
	}

	name := "API Errors"
	updated, err := r.Update(ctx, created.ID, &UpdateRequest{Name: &name})
	if err != nil || updated.Name != name {
		t.Fatalf("update=%+v err=%v", updated, err)
	}

	matches, err := r.Match(ctx, &MatchRequest{Text: "please fix api error in handler", StageType: "coding"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range matches {
		if m.Skill.ID == created.ID && m.Score > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected match, got %+v", matches)
	}

	inj, err := r.RenderInjection(ctx, &MatchRequest{Text: "api error", StageType: "coding"})
	if err != nil || !strings.Contains(inj, "Active Skills") {
		t.Fatalf("inject=%q err=%v", inj, err)
	}

	if err := r.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, created.ID); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestStageFilter(t *testing.T) {
	r := NewInMemoryRegistry()
	// commit hygiene has stage submit/coding
	matches, err := r.Match(context.Background(), &MatchRequest{Text: "git commit message", StageType: "submit"})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("expected commit skill on submit stage")
	}
}

func TestGetSetRegistry(t *testing.T) {
	prev := GetRegistry()
	t.Cleanup(func() { SetRegistry(prev) })
	custom := NewInMemoryRegistry()
	SetRegistry(custom)
	if GetRegistry() != custom {
		t.Fatal("expected custom registry")
	}
}
