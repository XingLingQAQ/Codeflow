package skill

import (
	"context"
	"testing"
)

// TestFrontmatterBOMPrefixed ensures a leading UTF-8 BOM is stripped so the
// frontmatter block is still recognized end-to-end through Create.
func TestFrontmatterBOMPrefixed(t *testing.T) {
	raw := "\uFEFF---\nname: BomSkill\ntriggers: [bom]\n---\nBody after BOM.\n"
	body, overlay, ok := parseFrontmatter(raw)
	if !ok {
		t.Fatal("BOM-prefixed frontmatter should parse")
	}
	if overlay.Name != "BomSkill" {
		t.Fatalf("name=%q", overlay.Name)
	}
	if body == "" || body[0] == '-' {
		t.Fatalf("body should be stripped of frontmatter: %q", body)
	}

	reg := NewInMemoryRegistry()
	sk, err := reg.Create(context.Background(), &CreateRequest{Body: raw})
	if err != nil {
		t.Fatal(err)
	}
	if sk.Name != "BomSkill" || !containsStr(sk.Triggers, "bom") {
		t.Fatalf("create from BOM frontmatter: name=%q triggers=%v", sk.Name, sk.Triggers)
	}
}

// TestFrontmatterAlternateStageKeys checks stage-tags and stages both map to StageTags.
func TestFrontmatterAlternateStageKeys(t *testing.T) {
	_, hyphen, ok := parseFrontmatter("---\nstage-tags: coding, review\n---\nx")
	if !ok || !containsStr(hyphen.StageTags, "coding") || !containsStr(hyphen.StageTags, "review") {
		t.Fatalf("stage-tags alt key: ok=%v tags=%v", ok, hyphen.StageTags)
	}
	_, plural, ok := parseFrontmatter("---\nstages: submit, plan\n---\nx")
	if !ok || !containsStr(plural.StageTags, "submit") || !containsStr(plural.StageTags, "plan") {
		t.Fatalf("stages alt key: ok=%v tags=%v", ok, plural.StageTags)
	}
	_, snake, ok := parseFrontmatter("---\nstage_tags: coding\n---\nx")
	if !ok || !containsStr(snake.StageTags, "coding") {
		t.Fatalf("stage_tags key: ok=%v tags=%v", ok, snake.StageTags)
	}
}

// TestFrontmatterTriggersBracketVsCSV checks both bracket-list and CSV trigger forms.
func TestFrontmatterTriggersBracketVsCSV(t *testing.T) {
	_, bracket, ok := parseFrontmatter("---\ntriggers: [alpha, beta]\n---\nx")
	if !ok || len(bracket.Triggers) != 2 || bracket.Triggers[0] != "alpha" || bracket.Triggers[1] != "beta" {
		t.Fatalf("bracket triggers: ok=%v got=%v", ok, bracket.Triggers)
	}
	_, csv, ok := parseFrontmatter("---\ntriggers: gamma, delta\n---\nx")
	if !ok || len(csv.Triggers) != 2 || csv.Triggers[0] != "gamma" || csv.Triggers[1] != "delta" {
		t.Fatalf("csv triggers: ok=%v got=%v", ok, csv.Triggers)
	}
}

// TestFrontmatterUnknownKeysIgnored checks unrecognized keys are skipped without error.
func TestFrontmatterUnknownKeysIgnored(t *testing.T) {
	body, overlay, ok := parseFrontmatter("---\nname: Known\ncolor: blue\nweird_key: 123\n---\nreal body\n")
	if !ok {
		t.Fatal("frontmatter with unknown keys should still parse")
	}
	if overlay.Name != "Known" {
		t.Fatalf("known key lost: %q", overlay.Name)
	}
	if body != "real body\n" {
		t.Fatalf("body should follow frontmatter: %q", body)
	}
}

// TestFrontmatterEmptyBlock documents the behavior for degenerate frontmatter.
// A key with an empty value parses (ok) but sets no fields; a bare open/close
// with no content is not recognized as frontmatter and the raw text is kept.
func TestFrontmatterEmptyBlock(t *testing.T) {
	body, overlay, ok := parseFrontmatter("---\nname:\n---\nbody text")
	if !ok {
		t.Fatal("frontmatter with empty value should still parse")
	}
	if overlay.Name != "" {
		t.Fatalf("empty value must not set a field: %q", overlay.Name)
	}
	if body != "body text" {
		t.Fatalf("body=%q", body)
	}

	rawBody, rawOverlay, rawOK := parseFrontmatter("---\n---\nbody")
	if rawOK {
		t.Fatal("bare ---/--- with no content is not recognized as frontmatter")
	}
	if rawOverlay.Name != "" {
		t.Fatalf("no field should be set for unrecognized block: %q", rawOverlay.Name)
	}
	if rawBody != "---\n---\nbody" {
		t.Fatalf("raw text should be preserved when unrecognized: %q", rawBody)
	}
}
