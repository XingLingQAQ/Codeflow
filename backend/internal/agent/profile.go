package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// This file is the T4.04.a contract for "which configuration does a run start
// with". It is deliberately a pure, I/O-free surface: it reads no database, no
// global config and no clock, so T4.04.b (freezing the resolved config into a
// Run) and T1.04 (scheduler dispatch) can call it with exactly the revision
// snapshot they pinned.
//
// Resolution order, and nothing else may be invented:
//
//  1. asset revision defaults - ModelPolicy, falling back to the legacy
//     Binding fields for model/temperature/max_tokens (an asset written
//     before T4.04.a has no ModelPolicy, so its Binding is the default);
//  2. project model policy override - model policy fields only, and
//     AllowedModels may only narrow;
//  3. this run's selection - the selected model must be inside the allowed
//     set that survived steps 1-2.
//
// backend, system_prompt and mounts come from the revision and only from the
// revision. There is no project override surface for them by type design
// (ProjectModelOverride has no such fields) and the raw-map entry point
// (ValidateProjectOverrideMap / ProjectModelOverrideFromMap) refuses any key
// outside the model policy, so changing them requires a new revision instead
// of a silent replacement.

// ModelPolicy is the model half of an agent revision: which model the agent
// uses by default and which models a run may pick instead.
//
// Empty AllowedModels means "only DefaultModel": a legacy single-model agent
// (and the legacy role config adapter) does not have to enumerate its one
// model. AllowedModels is a ceiling - a project override may shrink it, never
// grow it.
type ModelPolicy struct {
	DefaultModel  string   `json:"default_model,omitempty"`
	AllowedModels []string `json:"allowed_models,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty"`
	MaxTokens     *int     `json:"max_tokens,omitempty"`
}

// Fit declares where an agent is suitable. Stages and TaskTypes are opaque,
// lower-case-by-convention tags (bugfix/test/refactor/docs/feature for task
// types); nothing in this package constrains their vocabulary, the
// recommendation layer (T8.01) and the caller own the meaning.
type Fit struct {
	Stages    []string `json:"stages,omitempty"`
	TaskTypes []string `json:"task_types,omitempty"`
}

// Resolution source labels attached to every resolved field (T4.04.b returns
// them to the UI instead of letting it assemble a final model itself).
const (
	// ResolutionAssetRevision: the value came from the pinned agent revision,
	// either from its ModelPolicy or from its structural fields.
	ResolutionAssetRevision = "asset_revision"
	// ResolutionLegacyBinding: the field was absent from ModelPolicy, so the
	// pre-T4.04.a Binding value is the default.
	ResolutionLegacyBinding = "legacy_binding"
	// ResolutionProjectOverride: the project model policy override supplied
	// the value.
	ResolutionProjectOverride = "project_override"
	// ResolutionRunSelection: this run's explicit selection supplied the value.
	ResolutionRunSelection = "run_selection"
)

// Field names used as ResolvedAgentConfig.Sources keys and in
// ResolutionError.Field.
const (
	ResolveFieldBackend      = "backend"
	ResolveFieldSystemPrompt = "system_prompt"
	ResolveFieldMounts       = "mounts"
	ResolveFieldModel        = "model"
	ResolveFieldTemperature  = "temperature"
	ResolveFieldMaxTokens    = "max_tokens"
)

// The only keys a project override document may carry. They are exactly the
// JSON names of the ModelPolicy value fields: Backend/SystemPrompt/Mounts have
// no key here on purpose.
const (
	OverrideKeyDefaultModel  = "default_model"
	OverrideKeyAllowedModels = "allowed_models"
	OverrideKeyTemperature   = "temperature"
	OverrideKeyMaxTokens     = "max_tokens"
)

// Resolution refusal codes, carried in *ResolutionError.Code. They are stable
// strings because T4.04.b maps them onto HTTP responses and shows the reason to
// the user.
const (
	// CodeAgentDisabled: the asset revision is disabled; new runs are refused.
	CodeAgentDisabled = "agent_disabled"
	// CodeInvalidRevision: the caller passed no usable revision identity.
	CodeInvalidRevision = "invalid_revision"
	// CodeInvalidBackend: the revision carries a backend name that violates the
	// opaque backend name rule.
	CodeInvalidBackend = "invalid_backend"
	// CodeInvalidModelPolicy: the revision's own ModelPolicy is internally
	// inconsistent (for example a default outside its allowed list); the fix is
	// a new revision, not a resolution trick.
	CodeInvalidModelPolicy = "invalid_model_policy"
	// CodeModelRequired: after all three steps no model is left.
	CodeModelRequired = "model_required"
	// CodeModelNotAllowed: a selection (or a project default) points outside
	// the allowed set in force.
	CodeModelNotAllowed = "model_not_allowed"
	// CodeAllowedModelsWidened: the project override tried to add models the
	// revision does not allow. Narrowing only.
	CodeAllowedModelsWidened = "allowed_models_widened"
	// CodeReservedFieldOverride: an override document carried a field a project
	// may not override (backend/system_prompt/mounts/...); refused, never
	// silently ignored.
	CodeReservedFieldOverride = "reserved_field_override"
	// CodeInvalidOverrideValue: an override value has the wrong JSON type.
	CodeInvalidOverrideValue = "invalid_override_value"
)

// Sentinels so callers can branch with errors.Is; the precise reason is in
// *ResolutionError.Code.
var (
	// ErrModelNotAllowed covers CodeModelNotAllowed and
	// CodeAllowedModelsWidened: both mean "the model policy ceiling was
	// violated".
	ErrModelNotAllowed = errors.New("model not allowed by agent model policy")
	// ErrReservedOverrideField means an override document tried to change a
	// field only a revision may change.
	ErrReservedOverrideField = errors.New("project override may not change agent revision fields")
	// ErrInvalidBackendName means the backend string is not a legal opaque
	// backend name.
	ErrInvalidBackendName = errors.New("invalid execution backend name")
	// ErrInvalidModelPolicy means a ModelPolicy is internally inconsistent.
	ErrInvalidModelPolicy = errors.New("invalid model policy")
)

// ResolutionError explains why a run configuration was refused. Retrieve it
// with errors.As to read Code, or errors.Is to match a sentinel (for example
// errors.Is(err, ErrAgentDisabled) for CodeAgentDisabled).
type ResolutionError struct {
	// Code is one of the Code* constants above.
	Code string
	// Field names the offending field, or "" for whole-asset refusals.
	Field string
	// Detail is a human-readable explanation; it never contains secrets.
	Detail string
	// err is the sentinel this refusal unwraps to, when there is one.
	err error
}

// Error implements error.
func (e *ResolutionError) Error() string {
	if e == nil {
		return "agent run config resolution failed"
	}
	msg := "agent run config resolution failed (" + e.Code
	if e.Field != "" {
		msg += ", field=" + e.Field
	}
	return msg + "): " + e.Detail
}

// Unwrap exposes the sentinel so errors.Is works.
func (e *ResolutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func resolutionError(code, field, detail string, sentinel error) *ResolutionError {
	return &ResolutionError{Code: code, Field: field, Detail: detail, err: sentinel}
}

// ProjectModelOverride is everything a project may change about an agent's
// frozen configuration: model policy fields and nothing else. Backend,
// system_prompt and mounts are absent from the type on purpose - a project
// wanting different ones must publish a new agent revision.
type ProjectModelOverride struct {
	// DefaultModel replaces the revision's default model. It still has to be
	// inside the revision's allowed set.
	DefaultModel string `json:"default_model,omitempty"`
	// AllowedModels narrows the revision's allowed set. Every entry must
	// already be allowed by the revision; adding a model is refused.
	AllowedModels []string `json:"allowed_models,omitempty"`
	// Temperature / MaxTokens replace the revision's sampling values.
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
}

// RunSelection is what this one run asked for. Only the model can be selected;
// everything else is a property of the agent revision.
type RunSelection struct {
	Model string `json:"model,omitempty"`
}

// ResolvedAgentConfig is the effective configuration of one run, with the
// origin of every field recorded in Sources.
//
// Sources keys are the ResolveField* constants: backend/system_prompt/mounts
// always carry ResolutionAssetRevision, model always carries one of the four
// sources, and temperature/max_tokens carry a source only when a value was
// resolved (an absent numeric value has no rule to name).
type ResolvedAgentConfig struct {
	AgentID      string   `json:"agent_id"`
	Revision     int      `json:"revision"`
	Backend      string   `json:"backend"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Mounts       Mounts   `json:"mounts"`
	Model        string   `json:"model"`
	Temperature  *float64 `json:"temperature,omitempty"`
	MaxTokens    *int     `json:"max_tokens,omitempty"`
	// Sources is never nil, so a caller may index it without a nil check.
	Sources map[string]string `json:"sources"`
}

// ResolveRunConfig resolves the effective configuration of a run pinned to
// (rev, revision).
//
// It is a pure function: no I/O, no global configuration, no clock. The caller
// owns loading the revision (sqliteAgentStore.revisionAsset) and the project
// override; this function owns the order and the refusals.
//
// Refusals:
//   - rev.Enabled false -> CodeAgentDisabled wrapping ErrAgentDisabled, the
//     same gate as AssertRunnable (T1.03.b): a disabled asset never starts a
//     new run, while existing runs keep their pinned revision;
//   - an empty id or a revision below 1 -> CodeInvalidRevision;
//   - an illegal backend name or an inconsistent ModelPolicy on the revision
//     -> CodeInvalidBackend / CodeInvalidModelPolicy (fixed by a new revision);
//   - no model anywhere -> CodeModelRequired;
//   - a project override that adds models, or a selection outside the allowed
//     set -> CodeAllowedModelsWidened / CodeModelNotAllowed.
func ResolveRunConfig(
	rev AgentAsset,
	revision int,
	project *ProjectModelOverride,
	sel RunSelection,
) (ResolvedAgentConfig, error) {
	if !rev.Enabled {
		return ResolvedAgentConfig{}, resolutionError(
			CodeAgentDisabled, "",
			fmt.Sprintf("agent %q revision %d is disabled; new runs are refused (existing runs keep their pinned revision)", rev.ID, revision),
			ErrAgentDisabled,
		)
	}
	if strings.TrimSpace(rev.ID) == "" || revision < 1 {
		return ResolvedAgentConfig{}, resolutionError(
			CodeInvalidRevision, "revision",
			fmt.Sprintf("resolution needs a loaded revision: id=%q revision=%d", rev.ID, revision),
			nil,
		)
	}
	if err := ValidateBackendName(rev.Backend); err != nil {
		return ResolvedAgentConfig{}, resolutionError(CodeInvalidBackend, ResolveFieldBackend, err.Error(), ErrInvalidBackendName)
	}
	if err := ValidateModelPolicy(rev.ModelPolicy); err != nil {
		return ResolvedAgentConfig{}, resolutionError(CodeInvalidModelPolicy, ResolveFieldModel, err.Error(), ErrInvalidModelPolicy)
	}

	sources := make(map[string]string, 6)
	out := ResolvedAgentConfig{
		AgentID:      rev.ID,
		Revision:     revision,
		Backend:      rev.Backend,
		SystemPrompt: rev.SystemPrompt,
		Mounts: Mounts{
			MCPTools: copyStringSlice(rev.Mounts.MCPTools),
			Skills:   copyStringSlice(rev.Mounts.Skills),
		},
		Sources: sources,
	}
	// Structural fields: the revision is their only source, empty or not.
	sources[ResolveFieldBackend] = ResolutionAssetRevision
	sources[ResolveFieldSystemPrompt] = ResolutionAssetRevision
	sources[ResolveFieldMounts] = ResolutionAssetRevision

	// Step 1: revision defaults, with the legacy Binding fallback.
	var model string
	switch {
	case rev.ModelPolicy != nil && strings.TrimSpace(rev.ModelPolicy.DefaultModel) != "":
		model = strings.TrimSpace(rev.ModelPolicy.DefaultModel)
		sources[ResolveFieldModel] = ResolutionAssetRevision
	case strings.TrimSpace(rev.Binding.Model) != "":
		model = strings.TrimSpace(rev.Binding.Model)
		sources[ResolveFieldModel] = ResolutionLegacyBinding
	}
	allowed := make([]string, 0, 4)
	if rev.ModelPolicy != nil {
		allowed = append(allowed, rev.ModelPolicy.AllowedModels...)
	}
	if len(allowed) == 0 && model != "" {
		// Documented empty-list rule: only the default model is allowed.
		allowed = []string{model}
	}
	var temperature *float64
	switch {
	case rev.ModelPolicy != nil && rev.ModelPolicy.Temperature != nil:
		temperature = copyFloat(rev.ModelPolicy.Temperature)
		sources[ResolveFieldTemperature] = ResolutionAssetRevision
	case rev.Binding.Temperature != nil:
		temperature = copyFloat(rev.Binding.Temperature)
		sources[ResolveFieldTemperature] = ResolutionLegacyBinding
	}
	var maxTokens *int
	switch {
	case rev.ModelPolicy != nil && rev.ModelPolicy.MaxTokens != nil:
		maxTokens = copyInt(rev.ModelPolicy.MaxTokens)
		sources[ResolveFieldMaxTokens] = ResolutionAssetRevision
	case rev.Binding.MaxTokens != nil:
		maxTokens = copyInt(rev.Binding.MaxTokens)
		sources[ResolveFieldMaxTokens] = ResolutionLegacyBinding
	}

	// Step 2: project model policy override.
	if project != nil {
		if strings.TrimSpace(project.DefaultModel) != "" {
			model = strings.TrimSpace(project.DefaultModel)
			sources[ResolveFieldModel] = ResolutionProjectOverride
		}
		if len(project.AllowedModels) > 0 {
			narrowed := copyStringSlice(project.AllowedModels)
			for _, m := range narrowed {
				if !containsModel(allowed, m) {
					return ResolvedAgentConfig{}, resolutionError(
						CodeAllowedModelsWidened, OverrideKeyAllowedModels,
						fmt.Sprintf("project override allows model %q which the agent revision does not allow (allowed: %s); a project may only narrow the allowed set", m, joinModels(allowed)),
						ErrModelNotAllowed,
					)
				}
			}
			allowed = narrowed
		}
		if project.Temperature != nil {
			temperature = copyFloat(project.Temperature)
			sources[ResolveFieldTemperature] = ResolutionProjectOverride
		}
		if project.MaxTokens != nil {
			maxTokens = copyInt(project.MaxTokens)
			sources[ResolveFieldMaxTokens] = ResolutionProjectOverride
		}
	}

	// Step 3: this run's selection.
	if selected := strings.TrimSpace(sel.Model); selected != "" {
		if !containsModel(allowed, selected) {
			return ResolvedAgentConfig{}, resolutionError(
				CodeModelNotAllowed, ResolveFieldModel,
				fmt.Sprintf("run selection %q is not in the allowed models (%s)", selected, joinModels(allowed)),
				ErrModelNotAllowed,
			)
		}
		model = selected
		sources[ResolveFieldModel] = ResolutionRunSelection
	}

	if model == "" {
		return ResolvedAgentConfig{}, resolutionError(
			CodeModelRequired, ResolveFieldModel,
			fmt.Sprintf("agent %q revision %d resolves to no model: the revision declares none and neither the project override nor the run selection supplied one", rev.ID, revision),
			nil,
		)
	}
	// Final invariant: whatever path produced the model, it must be inside the
	// allowed set in force. This catches a revision or override that is
	// internally inconsistent (for example a new project default that the
	// narrowed set does not contain).
	if !containsModel(allowed, model) {
		return ResolvedAgentConfig{}, resolutionError(
			CodeModelNotAllowed, ResolveFieldModel,
			fmt.Sprintf("resolved model %q is outside the allowed models (%s)", model, joinModels(allowed)),
			ErrModelNotAllowed,
		)
	}

	out.Model = model
	out.Temperature = temperature
	out.MaxTokens = maxTokens
	return out, nil
}

// ValidateBackendName checks the opaque execution backend name stored on an
// agent revision (Backend field). The name is the same string
// execbackend.Backend.Name() returns ("fake", "claude_code", ...); the agent
// package must not import the execution layer, so the rule lives here as data:
//
//   - "" is allowed: an asset that never declared a backend leaves the choice
//     to the caller, and the asset model must not invent one;
//   - otherwise the name is at most 64 bytes and every byte is in
//     [a-z0-9._:-] (lower-case, because backend names are opaque identifiers
//     used in run records, not display text).
func ValidateBackendName(name string) error {
	if name == "" {
		return nil
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: %q is blank", ErrInvalidBackendName, name)
	}
	if len(name) > 64 {
		return fmt.Errorf("%w: %q is %d bytes, limit is 64", ErrInvalidBackendName, name, len(name))
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == ':' || c == '-':
		default:
			return fmt.Errorf("%w: %q contains %q, allowed characters are [a-z0-9._:-]", ErrInvalidBackendName, name, string(c))
		}
	}
	return nil
}

// ValidateModelPolicy checks that a ModelPolicy is internally consistent before
// it is written into a revision (and again when a revision is resolved).
//
// Rules: no blank or whitespace-padded model ids, no duplicates in
// AllowedModels, a non-empty DefaultModel must be inside a non-empty
// AllowedModels, and a non-nil MaxTokens must be positive. A nil policy is
// valid - it means "fall back to the legacy Binding".
func ValidateModelPolicy(p *ModelPolicy) error {
	if p == nil {
		return nil
	}
	if p.DefaultModel != "" {
		if strings.TrimSpace(p.DefaultModel) == "" {
			return fmt.Errorf("%w: default_model is blank", ErrInvalidModelPolicy)
		}
		if p.DefaultModel != strings.TrimSpace(p.DefaultModel) {
			return fmt.Errorf("%w: default_model %q carries surrounding whitespace", ErrInvalidModelPolicy, p.DefaultModel)
		}
	}
	seen := make(map[string]struct{}, len(p.AllowedModels))
	for _, m := range p.AllowedModels {
		if strings.TrimSpace(m) == "" {
			return fmt.Errorf("%w: allowed_models contains a blank entry", ErrInvalidModelPolicy)
		}
		if m != strings.TrimSpace(m) {
			return fmt.Errorf("%w: allowed model %q carries surrounding whitespace", ErrInvalidModelPolicy, m)
		}
		if _, dup := seen[m]; dup {
			return fmt.Errorf("%w: allowed model %q is listed twice", ErrInvalidModelPolicy, m)
		}
		seen[m] = struct{}{}
	}
	if len(p.AllowedModels) > 0 && p.DefaultModel != "" {
		if _, ok := seen[p.DefaultModel]; !ok {
			return fmt.Errorf("%w: default_model %q is not in allowed_models", ErrInvalidModelPolicy, p.DefaultModel)
		}
	}
	if p.MaxTokens != nil && *p.MaxTokens <= 0 {
		return fmt.Errorf("%w: max_tokens must be positive, got %d", ErrInvalidModelPolicy, *p.MaxTokens)
	}
	return nil
}

// ValidateProjectOverrideMap reports whether a raw decoded override document
// (for example a legacy SessionConfig-shaped map read from project settings)
// carries only model policy keys.
//
// It exists so "a project tried to change the backend/prompt/mounts" is a
// refusal instead of a silent drop: every key outside
// {default_model, allowed_models, temperature, max_tokens} is an error, and the
// key set is closed on purpose (fail closed - an unknown key is not assumed
// harmless).
func ValidateProjectOverrideMap(raw map[string]any) error {
	bad := make([]string, 0, len(raw))
	for k := range raw {
		switch k {
		case OverrideKeyDefaultModel, OverrideKeyAllowedModels, OverrideKeyTemperature, OverrideKeyMaxTokens:
		default:
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	// Sorted so the refusal is deterministic for a map with several bad keys.
	sort.Strings(bad)
	return resolutionError(
		CodeReservedFieldOverride, bad[0],
		fmt.Sprintf("project override key %q is not a model policy field (refused keys: %s); backend, system_prompt and mounts can only change through a new agent revision", bad[0], strings.Join(bad, ", ")),
		ErrReservedOverrideField,
	)
}

// ProjectModelOverrideFromMap validates raw and decodes it into a
// *ProjectModelOverride. It returns (nil, nil) when raw carries no model policy
// value, so the caller can pass the result straight to ResolveRunConfig.
//
// Any reserved key is refused (ValidateProjectOverrideMap); a value of the
// wrong JSON type is refused with CodeInvalidOverrideValue.
func ProjectModelOverrideFromMap(raw map[string]any) (*ProjectModelOverride, error) {
	if err := ValidateProjectOverrideMap(raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	out := &ProjectModelOverride{}
	used := false
	if v, ok := raw[OverrideKeyDefaultModel]; ok {
		s, err := overrideString(OverrideKeyDefaultModel, v)
		if err != nil {
			return nil, err
		}
		out.DefaultModel = s
		used = true
	}
	if v, ok := raw[OverrideKeyAllowedModels]; ok {
		list, err := overrideStringList(OverrideKeyAllowedModels, v)
		if err != nil {
			return nil, err
		}
		out.AllowedModels = list
		used = true
	}
	if v, ok := raw[OverrideKeyTemperature]; ok {
		f, err := overrideFloat(OverrideKeyTemperature, v)
		if err != nil {
			return nil, err
		}
		out.Temperature = &f
		used = true
	}
	if v, ok := raw[OverrideKeyMaxTokens]; ok {
		n, err := overrideInt(OverrideKeyMaxTokens, v)
		if err != nil {
			return nil, err
		}
		out.MaxTokens = &n
		used = true
	}
	if !used {
		return nil, nil
	}
	return out, nil
}

func overrideString(key string, v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", resolutionError(CodeInvalidOverrideValue, key,
			fmt.Sprintf("%s must be a string, got %T", key, v), nil)
	}
	return s, nil
}

func overrideStringList(key string, v any) ([]string, error) {
	switch list := v.(type) {
	case []string:
		return copyStringSlice(list), nil
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return nil, resolutionError(CodeInvalidOverrideValue, key,
					fmt.Sprintf("%s must be a list of strings, got an element of type %T", key, item), nil)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, resolutionError(CodeInvalidOverrideValue, key,
			fmt.Sprintf("%s must be a list of strings, got %T", key, v), nil)
	}
}

func overrideFloat(key string, v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, resolutionError(CodeInvalidOverrideValue, key,
				fmt.Sprintf("%s is not a number: %v", key, err), nil)
		}
		return f, nil
	default:
		return 0, resolutionError(CodeInvalidOverrideValue, key,
			fmt.Sprintf("%s must be a number, got %T", key, v), nil)
	}
}

func overrideInt(key string, v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		if n != float64(int(n)) {
			return 0, resolutionError(CodeInvalidOverrideValue, key,
				fmt.Sprintf("%s must be a whole number, got %v", key, n), nil)
		}
		return int(n), nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, resolutionError(CodeInvalidOverrideValue, key,
				fmt.Sprintf("%s must be a whole number: %v", key, err), nil)
		}
		return int(i), nil
	default:
		return 0, resolutionError(CodeInvalidOverrideValue, key,
			fmt.Sprintf("%s must be a whole number, got %T", key, v), nil)
	}
}

func containsModel(list []string, model string) bool {
	for _, m := range list {
		if m == model {
			return true
		}
	}
	return false
}

func joinModels(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
}

func copyStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string{}, in...)
}

func copyFloat(in *float64) *float64 {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func copyInt(in *int) *int {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}
