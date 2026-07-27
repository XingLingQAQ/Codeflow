package skill

import (
	"context"
	"testing"
)

// TestMatchWhitespaceTriggerNeverMatches ensures blank/whitespace-only triggers
// are ignored and never produce a match.
func TestMatchWhitespaceTriggerNeverMatches(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	sk, err := r.Create(ctx, &CreateRequest{
		Name:     "Whitespace Trigger",
		Body:     "body",
		Triggers: []string{"   ", "\t"},
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := r.Match(ctx, &MatchRequest{Text: "please do the thing now"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if m.Skill.ID == sk.ID {
			t.Fatalf("whitespace-only triggers must not match: %+v", m)
		}
	}
}

// TestRenderInjectionZeroMatchesEmpty ensures RenderInjection returns "" when
// nothing matches.
func TestRenderInjectionZeroMatchesEmpty(t *testing.T) {
	r := NewInMemoryRegistry()
	out, err := r.RenderInjection(context.Background(), &MatchRequest{Text: "xyzzy_no_such_trigger_zzz"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("expected empty injection for zero matches, got %q", out)
	}
}

// TestMatchLimitHonored ensures the result set is capped at MatchRequest.Limit.
func TestMatchLimitHonored(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	for _, name := range []string{"Zeb A", "Zeb B", "Zeb C"} {
		if _, err := r.Create(ctx, &CreateRequest{
			Name: name, Body: "body", Triggers: []string{"zebrakw"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := r.Match(ctx, &MatchRequest{Text: "the zebrakw appears", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected limit of 2, got %d", len(matches))
	}
}

// TestMatchExcludesDisabled ensures disabled skills never appear in Match results.
func TestMatchExcludesDisabled(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	sk, err := r.Create(ctx, &CreateRequest{
		Name: "Disabled Match", Body: "body", Triggers: []string{"disabledkw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Present while enabled.
	matches, err := r.Match(ctx, &MatchRequest{Text: "trigger disabledkw here"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSkill(matches, sk.ID) {
		t.Fatal("enabled skill should match before disabling")
	}
	off := false
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	matches, err = r.Match(ctx, &MatchRequest{Text: "trigger disabledkw here"})
	if err != nil {
		t.Fatal(err)
	}
	if hasSkill(matches, sk.ID) {
		t.Fatal("disabled skill must be excluded from Match")
	}
}

func hasSkill(matches []MatchResult, id string) bool {
	for _, m := range matches {
		if m.Skill.ID == id {
			return true
		}
	}
	return false
}
