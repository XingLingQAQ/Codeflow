package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/guard"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/skill"
)

// ContributionType identifies a typed contribution-point category.
type ContributionType string

const (
	ContribFlowTemplate ContributionType = "flow_template"
	ContribGuardRule    ContributionType = "guard_rule"
	ContribSkill        ContributionType = "skill"
)

// FlowTemplateContrib is one flow-template contribution (JSON CustomTemplate).
type FlowTemplateContrib struct {
	TemplateJSON json.RawMessage `json:"template"`
}

// GuardRuleContrib is one guard-rule contribution (severity override or glob addition).
type GuardRuleContrib struct {
	RuleID              guard.RuleID   `json:"rule_id,omitempty"`
	Severity            guard.Severity `json:"severity,omitempty"`
	DeniedPathGlobs     []string       `json:"denied_path_globs,omitempty"`
	DeprecatedPathGlobs []string       `json:"deprecated_path_globs,omitempty"`
}

// SkillContrib is one skill contribution.
type SkillContrib struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	Body        string   `json:"body"`
	Triggers    []string `json:"triggers,omitempty"`
	StageTags   []string `json:"stage_tags,omitempty"`
}

// ContributionManifest is the typed contributions section of a plugin manifest.
type ContributionManifest struct {
	FlowTemplates []FlowTemplateContrib `json:"flow_templates,omitempty"`
	GuardRules    []GuardRuleContrib    `json:"guard_rules,omitempty"`
	Skills        []SkillContrib        `json:"skills,omitempty"`
}

// appliedRecord tracks what was applied for rollback.
type appliedRecord struct {
	contribType ContributionType
	id          string // template id / skill id / "guard_config"
}

// ContributionRegistry manages typed contribution-point registration with
// all-or-nothing semantics and per-plugin rollback tracking.
type ContributionRegistry struct {
	mu      sync.Mutex
	applied map[string][]appliedRecord // pluginID -> records

	guardSnap map[string]guard.Config // pluginID -> config snapshot before apply
}

// NewContributionRegistry creates an empty registry.
func NewContributionRegistry() *ContributionRegistry {
	return &ContributionRegistry{
		applied:   make(map[string][]appliedRecord),
		guardSnap: make(map[string]guard.Config),
	}
}

// RegisterContributions validates and applies all contributions from a manifest
// for the given plugin. Semantics are all-or-nothing: if any contribution
// fails, earlier ones are rolled back.
func (r *ContributionRegistry) RegisterContributions(pluginID string, m ContributionManifest) error {
	return r.RegisterContributionsContext(context.Background(), pluginID, m)
}

// RegisterContributionsContext enforces the plugin boundary before applying
// any contribution. The legacy method delegates here for API compatibility.
func (r *ContributionRegistry) RegisterContributionsContext(ctx context.Context, pluginID string, m ContributionManifest) error {
	if strings.TrimSpace(pluginID) == "" {
		return fmt.Errorf("plugin id required")
	}
	if decision := policy.EvaluateBoundary(ctx, policy.Request{Operation: policy.OperationPluginRegister, Resource: pluginID, PluginID: pluginID}); !decision.Allowed {
		return policy.DenialError(decision)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.applied[pluginID]; exists {
		return fmt.Errorf("contributions already registered for plugin %s; unregister first", pluginID)
	}

	records := make([]appliedRecord, 0)
	rollback := func() {
		r.undoLocked(pluginID, records)
	}

	// --- skills (applied first so template failures can roll them back) ---
	for _, sc := range m.Skills {
		sk, err := skill.GetRegistry().Create(context.Background(), &skill.CreateRequest{
			Name:        sc.Name,
			Body:        sc.Body,
			Triggers:    sc.Triggers,
			StageTags:   sc.StageTags,
			Source:      skill.SourcePlugin,
			Version:     sc.Version,
			Description: sc.Description,
		})
		if err != nil {
			rollback()
			return fmt.Errorf("skill %q: %w", sc.Name, err)
		}
		records = append(records, appliedRecord{contribType: ContribSkill, id: sk.ID})
	}

	// --- guard rules (severity overrides + glob additions) ---
	if len(m.GuardRules) > 0 {
		gSvc, ok := guard.GetService().(*guard.Engine)
		if !ok {
			rollback()
			return fmt.Errorf("guard service is not a configurable engine")
		}
		snap := gSvc.Config()
		r.guardSnap[pluginID] = snap

		cfg := gSvc.Config()
		for _, gr := range m.GuardRules {
			if gr.RuleID != "" && gr.Severity != "" {
				if cfg.Rules == nil {
					cfg.Rules = make(map[guard.RuleID]guard.RuleConfig)
				}
				rc := cfg.Rules[gr.RuleID]
				rc.Severity = gr.Severity
				cfg.Rules[gr.RuleID] = rc
			}
			if len(gr.DeniedPathGlobs) > 0 {
				cfg.DeniedPathGlobs = append(cfg.DeniedPathGlobs, gr.DeniedPathGlobs...)
			}
			if len(gr.DeprecatedPathGlobs) > 0 {
				cfg.DeprecatedPathGlobs = append(cfg.DeprecatedPathGlobs, gr.DeprecatedPathGlobs...)
			}
		}
		gSvc.ApplyConfig(cfg)
		records = append(records, appliedRecord{contribType: ContribGuardRule, id: "guard_config"})
	}

	// --- flow templates ---
	for _, ft := range m.FlowTemplates {
		tid, err := floweng.ImportTemplateJSON(ft.TemplateJSON)
		if err != nil {
			rollback()
			return fmt.Errorf("flow_template: %w", err)
		}
		records = append(records, appliedRecord{contribType: ContribFlowTemplate, id: string(tid)})
	}

	r.applied[pluginID] = records
	return nil
}

// UnregisterContributions removes all contributions for a plugin.
func (r *ContributionRegistry) UnregisterContributions(pluginID string) error {
	return r.UnregisterContributionsContext(context.Background(), pluginID)
}

func (r *ContributionRegistry) UnregisterContributionsContext(ctx context.Context, pluginID string) error {
	if decision := policy.EvaluateBoundary(ctx, policy.Request{Operation: policy.OperationPluginRegister, Resource: pluginID, PluginID: pluginID}); !decision.Allowed {
		return policy.DenialError(decision)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	records, ok := r.applied[pluginID]
	if !ok {
		return fmt.Errorf("no contributions registered for plugin %s", pluginID)
	}
	r.undoLocked(pluginID, records)
	delete(r.applied, pluginID)
	return nil
}

// ListApplied returns the contribution types registered for a plugin.
func (r *ContributionRegistry) ListApplied(pluginID string) []appliedRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	recs := r.applied[pluginID]
	out := make([]appliedRecord, len(recs))
	copy(out, recs)
	return out
}

// undoLocked rolls back contributions in reverse order. Caller holds r.mu.
func (r *ContributionRegistry) undoLocked(pluginID string, records []appliedRecord) {
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		switch rec.contribType {
		case ContribFlowTemplate:
			_ = floweng.UnregisterTemplate(floweng.TemplateID(rec.id))
		case ContribGuardRule:
			if snap, ok := r.guardSnap[pluginID]; ok {
				if gSvc, ok := guard.GetService().(*guard.Engine); ok {
					gSvc.ApplyConfig(snap)
				}
				delete(r.guardSnap, pluginID)
			}
		case ContribSkill:
			_ = skill.GetRegistry().Delete(context.Background(), rec.id)
		}
	}
}
