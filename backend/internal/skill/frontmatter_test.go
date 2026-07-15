package skill

import (
	"context"
	"testing"
)

func TestParseFrontmatterCreate(t *testing.T) {
	raw := `---
name: From Frontmatter
description: demo
version: 2.0.0
triggers: [alpha, beta]
stage_tags: coding, review
---
# Body title

Do the thing.
`
	reg := NewInMemoryRegistry()
	// name intentionally empty — filled by frontmatter
	sk, err := reg.Create(context.Background(), &CreateRequest{
		Body: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sk.Name != "From Frontmatter" {
		t.Fatalf("name=%q", sk.Name)
	}
	if sk.Version != "2.0.0" {
		t.Fatalf("version=%q", sk.Version)
	}
	if len(sk.Triggers) != 2 || sk.Triggers[0] != "alpha" {
		t.Fatalf("triggers=%v", sk.Triggers)
	}
	if !containsStr(sk.StageTags, "coding") {
		t.Fatalf("stages=%v", sk.StageTags)
	}
	if sk.Body == "" || sk.Body[0] == '-' {
		t.Fatalf("body should strip frontmatter: %q", sk.Body)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
