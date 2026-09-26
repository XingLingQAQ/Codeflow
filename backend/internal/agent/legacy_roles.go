package agent

import (
	"fmt"
	"strings"

	"github.com/codeflow/backend/internal/config"
)

// This file adapts the pre-T4.04.a three-role configuration
// (config.RoleConfig for main/coder/sub, the map config.DefaultRoleConfigs
// seeds and config.Manager resolves) onto agent asset fields, so existing
// installations can be turned into agent revisions without losing values.
//
// The mapping is one-way and total where it can be: fields with a landing spot
// are copied, and every field without one produces a LegacyDiagnostic naming
// the field and the reason. Nothing is dropped silently and nothing is
// refused: a caller that wants to keep an unmappable value has to decide where
// it goes (system_prompt, a mount, or nothing) and can see exactly what needs a
// decision.
//
// Mapping table (source field -> asset field):
//
//	Model         -> ModelPolicy.DefaultModel
//	Temperature   -> ModelPolicy.Temperature
//	SystemPrompt  -> SystemPrompt
//	MCPTools      -> Mounts.MCPTools
//	AllowedSkills -> Mounts.Skills
//	APIChannel    -> Binding.Channel
//	<role>        -> RoleBase (main/coder/sub, 1:1)
//
// Fields with no landing spot (diagnostic per non-empty value):
//
//	TopP, AnswerStyle, Capabilities, AllowedHooks

// LegacyDiagnostic codes. A diagnostic is information, not a failure: the
// caller decides whether an unmapped value is acceptable.
const (
	// LegacyCodeRoleUnmapped: the legacy role has no RoleBase equivalent, so
	// the caller must choose one.
	LegacyCodeRoleUnmapped = "legacy_role_unmapped"
	// LegacyCodeNoAssetField: the source field carries a value but the agent
	// asset model has no field for it.
	LegacyCodeNoAssetField = "legacy_no_asset_field"
	// LegacyCodeConfigMissing: the role config is nil, so nothing could be
	// mapped at all.
	LegacyCodeConfigMissing = "legacy_config_missing"
)

// LegacyDiagnostic reports one legacy field that could not be mapped onto the
// agent asset model, with the reason. Field names the source field of
// config.RoleConfig (or "role"/"*" for the two structural cases).
type LegacyDiagnostic struct {
	Code   string `json:"code"`
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// LegacyMapping is the lossless projection of a config.RoleConfig onto agent
// asset fields. It is plain data: the caller writes it into a new AgentAsset
// revision (and must publish that revision - the mapping never mutates an
// existing one).
type LegacyMapping struct {
	RoleBase     RoleBase     `json:"role_base"`
	ModelPolicy  *ModelPolicy `json:"model_policy,omitempty"`
	SystemPrompt string       `json:"system_prompt,omitempty"`
	Mounts       Mounts       `json:"mounts"`
	Binding      Binding      `json:"binding"`
}

// legacyRoleBases is the 1:1 role mapping the legacy config hierarchy defines:
// config has exactly main/coder/sub (config.RoleType), the asset model has more
// role bases, and inventing a mapping for the others would be a guess.
var legacyRoleBases = map[config.RoleType]RoleBase{
	config.RoleMain:  RoleBaseMain,
	config.RoleCoder: RoleBaseCoder,
	config.RoleSub:   RoleBaseSub,
}

// legacyMappedFields documents the mapping table above, keyed by the
// config.RoleConfig field name. The coverage test walks config.RoleConfig with
// reflection and fails when a field is neither here nor in
// legacyUnmappedFields, so a future field cannot be dropped silently.
var legacyMappedFields = map[string]string{
	"Model":         "ModelPolicy.DefaultModel",
	"Temperature":   "ModelPolicy.Temperature",
	"SystemPrompt":  "SystemPrompt",
	"MCPTools":      "Mounts.MCPTools",
	"AllowedSkills": "Mounts.Skills",
	"APIChannel":    "Binding.Channel",
}

// legacyUnmappedFields documents the fields with no landing spot, keyed by the
// config.RoleConfig field name, with the reason reported in the diagnostic.
var legacyUnmappedFields = map[string]string{
	"TopP":         "the agent asset has no sampling top_p field; a run resolves temperature/max_tokens from the model policy only",
	"AnswerStyle":  "the agent asset has no answer style field; system_prompt is its only prompt surface",
	"Capabilities": "the agent asset has no capability list; a capability becomes a mount (MCP tool or skill) or a prompt statement",
	"AllowedHooks": "the agent asset has no hook field; hooks are attached by the hook subsystem, not by the asset",
}

// MapLegacyRoleConfig maps one legacy role configuration onto agent asset
// fields.
//
// A field with a value but no landing spot yields one diagnostic (code
// legacy_no_asset_field) instead of being dropped; an unknown role yields
// legacy_role_unmapped and leaves RoleBase empty; a nil config yields
// legacy_config_missing. The mapping copies values verbatim (no trimming or
// rewriting): ValidateModelPolicy is the downstream gate for values that are
// not usable as-is.
func MapLegacyRoleConfig(role config.RoleType, cfg *config.RoleConfig) (LegacyMapping, []LegacyDiagnostic) {
	diags := make([]LegacyDiagnostic, 0, 5)
	var m LegacyMapping

	if rb, ok := legacyRoleBases[role]; ok {
		m.RoleBase = rb
	} else {
		diags = append(diags, LegacyDiagnostic{
			Code:  LegacyCodeRoleUnmapped,
			Field: "role",
			Reason: fmt.Sprintf("legacy role %q has no RoleBase equivalent (main, coder, sub only); the caller must choose one explicitly",
				string(role)),
		})
	}
	if cfg == nil {
		diags = append(diags, LegacyDiagnostic{
			Code:   LegacyCodeConfigMissing,
			Field:  "*",
			Reason: "role config is nil; no field could be mapped",
		})
		return m, diags
	}

	// config.RoleConfig.Temperature is a non-optional scalar (no pointer, no
	// omitempty), so its value maps 1:1 including an explicit 0.
	temperature := cfg.Temperature
	m.ModelPolicy = &ModelPolicy{
		DefaultModel: cfg.Model,
		Temperature:  &temperature,
	}
	// AllowedModels stays empty: the legacy config had exactly one model, and
	// the documented empty-list rule is "only DefaultModel is allowed".
	m.SystemPrompt = cfg.SystemPrompt
	m.Mounts = Mounts{
		MCPTools: copyStringSlice(cfg.MCPTools),
		Skills:   copyStringSlice(cfg.AllowedSkills),
	}
	m.Binding = Binding{Channel: cfg.APIChannel}

	// Diagnostics in a fixed order so callers and tests see a stable list.
	if cfg.TopP != nil {
		diags = append(diags, legacyNoAssetField("TopP"))
	}
	if strings.TrimSpace(cfg.AnswerStyle) != "" {
		diags = append(diags, legacyNoAssetField("AnswerStyle"))
	}
	if len(cfg.Capabilities) > 0 {
		diags = append(diags, legacyNoAssetField("Capabilities"))
	}
	if len(cfg.AllowedHooks) > 0 {
		diags = append(diags, legacyNoAssetField("AllowedHooks"))
	}
	return m, diags
}

func legacyNoAssetField(field string) LegacyDiagnostic {
	return LegacyDiagnostic{
		Code:   LegacyCodeNoAssetField,
		Field:  field,
		Reason: legacyUnmappedFields[field],
	}
}
