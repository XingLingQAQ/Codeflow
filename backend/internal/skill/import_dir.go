package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ImportMarkdownDir loads *.md files as skills (frontmatter optional).
// Existing skills with the same id/name are updated by name match when forceReplace is true.
func (r *InMemoryRegistry) ImportMarkdownDir(ctx context.Context, dir string) (int, error) {
	if strings.TrimSpace(dir) == "" {
		return 0, fmt.Errorf("dir is required")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		req := &CreateRequest{
			Body: string(data),
		}
		applyFrontmatter(req)
		// default name from filename if frontmatter lacks name
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if strings.TrimSpace(req.Name) == "" {
			req.Name = base
		}
		if strings.TrimSpace(req.Body) == "" {
			continue
		}
		// replace existing by name (never overwrite builtins)
		if existing := r.findByName(req.Name); existing != nil {
			if existing.Source == SourceBuiltin {
				return count, fmt.Errorf("cannot import over builtin skill: %s", existing.Name)
			}
			_, err = r.Update(ctx, existing.ID, &UpdateRequest{
				Description: &req.Description,
				Version:     &req.Version,
				Body:        &req.Body,
				Triggers:    req.Triggers,
				StageTags:   req.StageTags,
			})
			if err != nil {
				return count, err
			}
			count++
			continue
		}
		if _, err := r.Create(ctx, req); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (r *InMemoryRegistry) findByName(name string) *Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.skills {
		if strings.EqualFold(s.Name, name) {
			return cloneSkill(s)
		}
	}
	return nil
}
