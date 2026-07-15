package skill

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InMemoryRegistry is the skill registry (optional SQLite durability via store).
type InMemoryRegistry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
	store  *sqliteSkillStore // optional
}

// NewInMemoryRegistry creates a registry with built-in skills (memory only).
func NewInMemoryRegistry() *InMemoryRegistry {
	r := &InMemoryRegistry{skills: make(map[string]*Skill)}
	r.seedBuiltins()
	return r
}

// NewSQLiteRegistry opens a durable registry at dbPath.
// Loads existing rows; seeds builtins only when the database is empty.
func NewSQLiteRegistry(dbPath string) (*InMemoryRegistry, error) {
	store, err := openSQLiteSkillStore(dbPath)
	if err != nil {
		return nil, err
	}
	r := &InMemoryRegistry{skills: make(map[string]*Skill), store: store}
	loaded, err := store.loadAll()
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	for _, sk := range loaded {
		r.skills[sk.ID] = sk
	}
	if len(r.skills) == 0 {
		r.seedBuiltins()
		for _, sk := range r.skills {
			if err := store.put(sk); err != nil {
				_ = store.Close()
				return nil, err
			}
		}
	}
	return r, nil
}

func (r *InMemoryRegistry) seedBuiltins() {
	now := time.Now().UTC()
	builtins := []Skill{
		{
			ID: "builtin-commit-hygiene", Name: "Commit Hygiene", Version: "1.0.0",
			Source: SourceBuiltin, Enabled: true,
			Description: "Conventional commits and small diffs",
			Body:        "Prefer conventional commits (feat/fix/docs/refactor). Keep diffs focused; avoid drive-by refactors.",
			Triggers:    []string{"commit", "git commit", "changelog"},
			StageTags:   []string{"submit", "coding"},
			CreatedAt:   now, UpdatedAt: now,
		},
		{
			ID: "builtin-test-first", Name: "Test First Touch", Version: "1.0.0",
			Source: SourceBuiltin, Enabled: true,
			Description: "Encourage tests near behavior changes",
			Body:        "When changing behavior, add or update unit tests in the same change set. Name failure scenarios explicitly.",
			Triggers:    []string{"test", "unit test", "regression"},
			StageTags:   []string{"coding", "review"},
			CreatedAt:   now, UpdatedAt: now,
		},
	}
	for i := range builtins {
		s := builtins[i]
		r.skills[s.ID] = &s
	}
}

// Create adds a skill.
func (r *InMemoryRegistry) Create(ctx context.Context, req *CreateRequest) (*Skill, error) {
	if req == nil {
		return nil, fmt.Errorf("name and body are required")
	}
	applyFrontmatter(req)
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Body) == "" {
		return nil, fmt.Errorf("name and body are required")
	}
	now := time.Now().UTC()
	src := req.Source
	if src == "" {
		src = SourceUser
	}
	ver := req.Version
	if ver == "" {
		ver = "0.1.0"
	}
	s := &Skill{
		ID:          uuid.New().String(),
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		Version:     ver,
		Source:      src,
		Body:        req.Body,
		Triggers:    append([]string(nil), req.Triggers...),
		StageTags:   append([]string(nil), req.StageTags...),
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	r.mu.Lock()
	r.skills[s.ID] = cloneSkill(s)
	if r.store != nil {
		if err := r.store.put(s); err != nil {
			delete(r.skills, s.ID)
			r.mu.Unlock()
			return nil, err
		}
	}
	r.mu.Unlock()
	return cloneSkill(s), nil
}

// Get returns a skill by id.
func (r *InMemoryRegistry) Get(ctx context.Context, id string) (*Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[id]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", id)
	}
	return cloneSkill(s), nil
}

// List returns all skills.
func (r *InMemoryRegistry) List(ctx context.Context) ([]*Skill, error) {
	return r.ListFiltered(ctx, "", true)
}

// ListFiltered returns skills optionally filtered by stage tag.
// includeDisabled controls whether disabled skills appear.
func (r *InMemoryRegistry) ListFiltered(ctx context.Context, stage string, includeDisabled bool) ([]*Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stage = strings.ToLower(strings.TrimSpace(stage))
	out := make([]*Skill, 0, len(r.skills))
	for _, s := range r.skills {
		if !includeDisabled && !s.Enabled {
			continue
		}
		if stage != "" && len(s.StageTags) > 0 {
			ok := false
			for _, t := range s.StageTags {
				if strings.ToLower(t) == stage {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		out = append(out, cloneSkill(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Update patches a skill.
func (r *InMemoryRegistry) Update(ctx context.Context, id string, req *UpdateRequest) (*Skill, error) {
	if req == nil {
		return nil, fmt.Errorf("update request is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.skills[id]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", id)
	}
	if req.Name != nil {
		s.Name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		s.Description = *req.Description
	}
	if req.Version != nil {
		s.Version = *req.Version
	}
	if req.Body != nil {
		s.Body = *req.Body
	}
	if req.Triggers != nil {
		s.Triggers = append([]string(nil), req.Triggers...)
	}
	if req.StageTags != nil {
		s.StageTags = append([]string(nil), req.StageTags...)
	}
	if req.Enabled != nil {
		s.Enabled = *req.Enabled
	}
	s.UpdatedAt = time.Now().UTC()
	if r.store != nil {
		if err := r.store.put(s); err != nil {
			return nil, err
		}
	}
	return cloneSkill(s), nil
}

// Delete removes a skill.
func (r *InMemoryRegistry) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.skills[id]
	if !ok {
		return fmt.Errorf("skill not found: %s", id)
	}
	if s.Source == SourceBuiltin {
		return fmt.Errorf("cannot delete builtin skill: %s", id)
	}
	if r.store != nil {
		if err := r.store.delete(id); err != nil {
			return err
		}
	}
	delete(r.skills, id)
	return nil
}

// Match scores enabled skills by trigger hits and stage tags.
func (r *InMemoryRegistry) Match(ctx context.Context, req *MatchRequest) ([]MatchResult, error) {
	if req == nil {
		req = &MatchRequest{}
	}
	text := strings.ToLower(req.Text)
	stage := strings.ToLower(strings.TrimSpace(req.StageType))
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make([]MatchResult, 0)
	for _, s := range r.skills {
		if !s.Enabled {
			continue
		}
		if stage != "" && len(s.StageTags) > 0 {
			ok := false
			for _, t := range s.StageTags {
				if strings.ToLower(t) == stage {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		hits := make([]string, 0)
		score := 0.0
		if text != "" {
			for _, tr := range s.Triggers {
				tr = strings.ToLower(strings.TrimSpace(tr))
				if tr == "" {
					continue
				}
				if strings.Contains(text, tr) {
					hits = append(hits, tr)
					score += float64(len(tr))
				}
			}
		} else if stage != "" {
			// stage-only listing of applicable skills
			score = 1
		}
		if score <= 0 && text != "" {
			continue
		}
		if score <= 0 && text == "" && stage == "" {
			// no filter: include with zero score for list-like match
			score = 0.1
		}
		results = append(results, MatchResult{Skill: *cloneSkill(s), Score: score, Hits: hits})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Skill.Name < results[j].Skill.Name
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// RenderInjection joins matched skill bodies for prompt mounting.
func (r *InMemoryRegistry) RenderInjection(ctx context.Context, req *MatchRequest) (string, error) {
	matches, err := r.Match(ctx, req)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("## Active Skills\n")
	for _, m := range matches {
		b.WriteString("\n### ")
		b.WriteString(m.Skill.Name)
		b.WriteString(" (")
		b.WriteString(m.Skill.Version)
		b.WriteString(")\n")
		b.WriteString(m.Skill.Body)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// Close releases the optional durable store.
func (r *InMemoryRegistry) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store == nil {
		return nil
	}
	err := r.store.Close()
	r.store = nil
	return err
}

func cloneSkill(s *Skill) *Skill {
	if s == nil {
		return nil
	}
	cp := *s
	cp.Triggers = append([]string(nil), s.Triggers...)
	cp.StageTags = append([]string(nil), s.StageTags...)
	return &cp
}

// --- global ---

var (
	defaultReg Registry
	regMu      sync.RWMutex
)

// GetRegistry returns the process-wide skill registry.
func GetRegistry() Registry {
	regMu.RLock()
	r := defaultReg
	regMu.RUnlock()
	if r != nil {
		return r
	}
	regMu.Lock()
	defer regMu.Unlock()
	if defaultReg == nil {
		defaultReg = NewInMemoryRegistry()
	}
	return defaultReg
}

// SetRegistry sets the process-wide registry.
func SetRegistry(r Registry) {
	regMu.Lock()
	defer regMu.Unlock()
	defaultReg = r
}
