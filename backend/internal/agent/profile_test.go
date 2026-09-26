package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/dbx"
)

// T4.04.a resolution tests: pure ResolveRunConfig behavior, the model policy
// rules, the override surface (which does not exist for
// backend/system_prompt/mounts), and the byte stability of the asset document
// when the new fields are empty.

func ptrFloat(v float64) *float64 { return &v }
func ptrInt(v int) *int           { return &v }

// profileTestAsset is a fully populated revision of the kind T4.04.b will pin.
func profileTestAsset() AgentAsset {
	return AgentAsset{
		ID: "user-runner", Name: "Runner", Version: "1.0.0",
		Source: SourceUser, RoleBase: RoleBaseCoder,
		Purpose:      "implement tasks under guard constraints",
		Backend:      "claude_code",
		SystemPrompt: "You implement scoped tasks.",
		Binding:      Binding{Model: "legacy-model", Channel: "default", Temperature: ptrFloat(0.2), MaxTokens: ptrInt(1024)},
		Mounts:       Mounts{MCPTools: []string{"fs.read"}, Skills: []string{"review"}},
		ModelPolicy: &ModelPolicy{
			DefaultModel:  "claude-3-5-sonnet-20241022",
			AllowedModels: []string{"claude-3-5-sonnet-20241022", "claude-3-5-haiku-20241022"},
			Temperature:   ptrFloat(0.4),
			MaxTokens:     ptrInt(4096),
		},
		Fit:       &Fit{Stages: []string{"coding"}, TaskTypes: []string{"bugfix"}},
		Enabled:   true,
		CreatedAt: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
	}
}

// Step 1 only: every field comes from the revision, ModelPolicy wins over the
// legacy Binding, and the source map says so.
func TestResolveRunConfigAssetRevisionDefaults(t *testing.T) {
	rev := profileTestAsset()
	got, err := ResolveRunConfig(rev, 3, nil, RunSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentID != rev.ID || got.Revision != 3 {
		t.Fatalf("identity=%s/%d want %s/3", got.AgentID, got.Revision, rev.ID)
	}
	if got.Model != "claude-3-5-sonnet-20241022" {
		t.Fatalf("model=%q want the ModelPolicy default", got.Model)
	}
	if got.Backend != "claude_code" || got.SystemPrompt != rev.SystemPrompt {
		t.Fatalf("structural fields=%q/%q", got.Backend, got.SystemPrompt)
	}
	if !reflect.DeepEqual(got.Mounts, rev.Mounts) {
		t.Fatalf("mounts=%+v want %+v", got.Mounts, rev.Mounts)
	}
	if got.Temperature == nil || *got.Temperature != 0.4 {
		t.Fatalf("temperature=%v want 0.4 (ModelPolicy wins over Binding 0.2)", got.Temperature)
	}
	if got.MaxTokens == nil || *got.MaxTokens != 4096 {
		t.Fatalf("max_tokens=%v want 4096 (ModelPolicy wins over Binding 1024)", got.MaxTokens)
	}
	want := map[string]string{
		ResolveFieldBackend:      ResolutionAssetRevision,
		ResolveFieldSystemPrompt: ResolutionAssetRevision,
		ResolveFieldMounts:       ResolutionAssetRevision,
		ResolveFieldModel:        ResolutionAssetRevision,
		ResolveFieldTemperature:  ResolutionAssetRevision,
		ResolveFieldMaxTokens:    ResolutionAssetRevision,
	}
	if !reflect.DeepEqual(got.Sources, want) {
		t.Fatalf("sources=%v want %v", got.Sources, want)
	}
}

// An asset written before T4.04.a has no ModelPolicy: model/temperature/
// max_tokens fall back to the legacy Binding and are labeled legacy_binding,
// while the structural fields stay asset_revision.
func TestResolveRunConfigLegacyBindingFallback(t *testing.T) {
	rev := AgentAsset{
		ID: "builtin-legacy", Name: "Legacy", Version: "1.0.0",
		Source: SourceBuiltin, RoleBase: RoleBaseMain, Enabled: true,
		SystemPrompt: "legacy prompt",
		Binding:      Binding{Model: "gpt-5", Channel: "default", Temperature: ptrFloat(0.7), MaxTokens: ptrInt(2048)},
		Mounts:       Mounts{MCPTools: []string{"orchestrator"}},
	}
	got, err := ResolveRunConfig(rev, 1, nil, RunSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt-5" {
		t.Fatalf("model=%q want gpt-5", got.Model)
	}
	if got.Temperature == nil || *got.Temperature != 0.7 || got.MaxTokens == nil || *got.MaxTokens != 2048 {
		t.Fatalf("numeric fallback=%v/%v", got.Temperature, got.MaxTokens)
	}
	want := map[string]string{
		ResolveFieldBackend:      ResolutionAssetRevision,
		ResolveFieldSystemPrompt: ResolutionAssetRevision,
		ResolveFieldMounts:       ResolutionAssetRevision,
		ResolveFieldModel:        ResolutionLegacyBinding,
		ResolveFieldTemperature:  ResolutionLegacyBinding,
		ResolveFieldMaxTokens:    ResolutionLegacyBinding,
	}
	if !reflect.DeepEqual(got.Sources, want) {
		t.Fatalf("sources=%v want %v", got.Sources, want)
	}
}

// Documented empty-list rule: AllowedModels empty means "only DefaultModel".
func TestResolveRunConfigEmptyAllowedModelsMeansOnlyDefault(t *testing.T) {
	rev := profileTestAsset()
	rev.ModelPolicy.AllowedModels = nil
	if _, err := ResolveRunConfig(rev, 1, nil, RunSelection{Model: "claude-3-5-haiku-20241022"}); err == nil {
		t.Fatal("expected refusal: AllowedModels empty allows only DefaultModel")
	} else {
		assertResolutionCode(t, err, CodeModelNotAllowed, ErrModelNotAllowed)
	}
	got, err := ResolveRunConfig(rev, 1, nil, RunSelection{Model: "claude-3-5-sonnet-20241022"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "claude-3-5-sonnet-20241022" || got.Sources[ResolveFieldModel] != ResolutionRunSelection {
		t.Fatalf("model=%q sources=%v", got.Model, got.Sources)
	}
}

// Steps 2 and 3 on top of step 1: the override wins over the revision and the
// selection wins over both, and each field names its origin.
func TestResolveRunConfigOverrideAndSelectionSources(t *testing.T) {
	rev := profileTestAsset()
	project := &ProjectModelOverride{
		DefaultModel: "claude-3-5-haiku-20241022",
		Temperature:  ptrFloat(0.9),
	}
	got, err := ResolveRunConfig(rev, 2, project, RunSelection{Model: "claude-3-5-sonnet-20241022"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "claude-3-5-sonnet-20241022" {
		t.Fatalf("model=%q want the run selection", got.Model)
	}
	if got.Temperature == nil || *got.Temperature != 0.9 {
		t.Fatalf("temperature=%v want the override 0.9", got.Temperature)
	}
	if got.MaxTokens == nil || *got.MaxTokens != 4096 {
		t.Fatalf("max_tokens=%v want the revision 4096", got.MaxTokens)
	}
	want := map[string]string{
		ResolveFieldBackend:      ResolutionAssetRevision,
		ResolveFieldSystemPrompt: ResolutionAssetRevision,
		ResolveFieldMounts:       ResolutionAssetRevision,
		ResolveFieldModel:        ResolutionRunSelection,
		ResolveFieldTemperature:  ResolutionProjectOverride,
		ResolveFieldMaxTokens:    ResolutionAssetRevision,
	}
	if !reflect.DeepEqual(got.Sources, want) {
		t.Fatalf("sources=%v want %v", got.Sources, want)
	}

	// Without a selection the override's default is what resolves.
	got2, err := ResolveRunConfig(rev, 2, project, RunSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if got2.Model != "claude-3-5-haiku-20241022" || got2.Sources[ResolveFieldModel] != ResolutionProjectOverride {
		t.Fatalf("model=%q sources=%v", got2.Model, got2.Sources)
	}
}

// A project may narrow the allowed set, and the narrowing applies to the run
// selection (a model the project removed is no longer selectable).
func TestResolveRunConfigProjectNarrowsAllowedModels(t *testing.T) {
	rev := profileTestAsset()
	project := &ProjectModelOverride{AllowedModels: []string{"claude-3-5-haiku-20241022"}}
	got, err := ResolveRunConfig(rev, 1, project, RunSelection{Model: "claude-3-5-haiku-20241022"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "claude-3-5-haiku-20241022" {
		t.Fatalf("model=%q", got.Model)
	}
	if _, err := ResolveRunConfig(rev, 1, project, RunSelection{Model: "claude-3-5-sonnet-20241022"}); err == nil {
		t.Fatal("expected refusal: the model was narrowed away by the project")
	} else {
		assertResolutionCode(t, err, CodeModelNotAllowed, ErrModelNotAllowed)
	}
}

// A project can never grant a model the revision does not allow.
func TestResolveRunConfigProjectCannotWidenAllowedModels(t *testing.T) {
	rev := profileTestAsset()
	_, err := ResolveRunConfig(rev, 1, &ProjectModelOverride{AllowedModels: []string{"gpt-6"}}, RunSelection{})
	if err == nil {
		t.Fatal("expected refusal: widening the allowed set")
	}
	assertResolutionCode(t, err, CodeAllowedModelsWidened, ErrModelNotAllowed)
	var re *ResolutionError
	if !errors.As(err, &re) {
		t.Fatalf("error type=%T want *ResolutionError", err)
	}
	if re.Field != OverrideKeyAllowedModels || !strings.Contains(re.Error(), "gpt-6") {
		t.Fatalf("refusal=%+v want field allowed_models naming gpt-6", re)
	}

	// A project default outside the revision's ceiling is refused too.
	_, err = ResolveRunConfig(rev, 1, &ProjectModelOverride{DefaultModel: "gpt-6"}, RunSelection{})
	if err == nil {
		t.Fatal("expected refusal: project default outside the allowed set")
	}
	assertResolutionCode(t, err, CodeModelNotAllowed, ErrModelNotAllowed)
}

// backend/system_prompt/mounts are revision-only. The override type has no
// field for them, the raw map entry point refuses the keys, and a resolved
// config therefore always reports the revision's values.
func TestResolveRunConfigBackendPromptMountsAreRevisionOnly(t *testing.T) {
	rev := profileTestAsset()

	// The raw map form (what a legacy SessionConfig-shaped document looks like)
	// is refused, key by key.
	for _, key := range []string{"backend", "system_prompt", "mounts", "prompt", "mcp_tools", "skills", "channel", "session_id"} {
		raw := map[string]any{key: "whatever"}
		if err := ValidateProjectOverrideMap(raw); err == nil {
			t.Fatalf("override key %q must be refused", key)
		} else {
			assertResolutionCode(t, err, CodeReservedFieldOverride, ErrReservedOverrideField)
		}
		if _, err := ProjectModelOverrideFromMap(raw); err == nil {
			t.Fatalf("override key %q must be refused by the decoder too", key)
		}
	}
	// The full legacy SessionConfig shape: only the model policy keys survive.
	err := ValidateProjectOverrideMap(map[string]any{
		"session_id": "s-1", "mode": "development", "override_model": "gpt-6", "backend": "fake",
	})
	if err == nil {
		t.Fatal("expected refusal for a legacy session override document")
	}
	assertResolutionCode(t, err, CodeReservedFieldOverride, ErrReservedOverrideField)
	var re *ResolutionError
	_ = errors.As(err, &re)
	if re == nil || re.Field != "backend" {
		t.Fatalf("refusal=%+v want the first bad key in sorted order (backend)", re)
	}

	// A model-policy-only override resolves, and the structural fields still
	// come from the revision untouched.
	project := &ProjectModelOverride{DefaultModel: "claude-3-5-haiku-20241022", Temperature: ptrFloat(0.1)}
	got, err := ResolveRunConfig(rev, 1, project, RunSelection{Model: "claude-3-5-sonnet-20241022"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Backend != rev.Backend || got.SystemPrompt != rev.SystemPrompt || !reflect.DeepEqual(got.Mounts, rev.Mounts) {
		t.Fatalf("structural fields changed: %+v", got)
	}
	if got.Sources[ResolveFieldBackend] != ResolutionAssetRevision ||
		got.Sources[ResolveFieldSystemPrompt] != ResolutionAssetRevision ||
		got.Sources[ResolveFieldMounts] != ResolutionAssetRevision {
		t.Fatalf("structural sources=%v", got.Sources)
	}
}

// A disabled asset revision never resolves into a run configuration, exactly
// like AssertRunnable gates new runs (T1.03.b).
func TestResolveRunConfigDisabledAsset(t *testing.T) {
	rev := profileTestAsset()
	rev.Enabled = false
	_, err := ResolveRunConfig(rev, 4, nil, RunSelection{})
	if err == nil {
		t.Fatal("expected refusal for a disabled asset")
	}
	if !errors.Is(err, ErrAgentDisabled) {
		t.Fatalf("err=%v want ErrAgentDisabled", err)
	}
	assertResolutionCode(t, err, CodeAgentDisabled, ErrAgentDisabled)
}

// Structural refusals: no revision identity, no model, a bad backend name and
// an inconsistent model policy.
func TestResolveRunConfigRefusals(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*AgentAsset)
		revision int
		wantCode string
	}{
		{"revision zero", func(a *AgentAsset) {}, 0, CodeInvalidRevision},
		{"empty id", func(a *AgentAsset) { a.ID = "  " }, 1, CodeInvalidRevision},
		{"no model anywhere", func(a *AgentAsset) {
			a.ModelPolicy = nil
			a.Binding = Binding{}
		}, 1, CodeModelRequired},
		{"bad backend", func(a *AgentAsset) { a.Backend = "Fake Backend" }, 1, CodeInvalidBackend},
		{"policy default outside allowed", func(a *AgentAsset) {
			a.ModelPolicy.DefaultModel = "gpt-6"
		}, 1, CodeInvalidModelPolicy},
		{"policy duplicate allowed", func(a *AgentAsset) {
			a.ModelPolicy.AllowedModels = []string{"m", "m"}
			a.ModelPolicy.DefaultModel = "m"
		}, 1, CodeInvalidModelPolicy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rev := profileTestAsset()
			tc.mutate(&rev)
			_, err := ResolveRunConfig(rev, tc.revision, nil, RunSelection{})
			if err == nil {
				t.Fatalf("expected refusal (%s)", tc.wantCode)
			}
			var re *ResolutionError
			if !errors.As(err, &re) {
				t.Fatalf("error type=%T want *ResolutionError", err)
			}
			if re.Code != tc.wantCode {
				t.Fatalf("code=%q want %q (%v)", re.Code, tc.wantCode, err)
			}
		})
	}
}

// The resolved config must not alias the caller's revision or override memory:
// it is a snapshot handed to a run record.
func TestResolveRunConfigNoAliasing(t *testing.T) {
	rev := profileTestAsset()
	project := &ProjectModelOverride{Temperature: ptrFloat(0.5), AllowedModels: []string{"claude-3-5-sonnet-20241022"}}
	got, err := ResolveRunConfig(rev, 1, project, RunSelection{})
	if err != nil {
		t.Fatal(err)
	}
	*got.Temperature = 9.9
	got.MaxTokens = ptrInt(1)
	got.Mounts.MCPTools[0] = "shell.exec"
	got.Sources[ResolveFieldModel] = "hacked"
	if *project.Temperature != 0.5 {
		t.Fatalf("override temperature aliased: %v", *project.Temperature)
	}
	if *rev.ModelPolicy.MaxTokens != 4096 || *rev.ModelPolicy.Temperature != 0.4 {
		t.Fatalf("revision pointers aliased: %v/%v", *rev.ModelPolicy.MaxTokens, *rev.ModelPolicy.Temperature)
	}
	if rev.Mounts.MCPTools[0] != "fs.read" {
		t.Fatalf("revision mounts aliased: %v", rev.Mounts.MCPTools)
	}
}

// Determinism: the same inputs produce the same value and the same source map,
// so a run record can be rebuilt from the pinned revision.
func TestResolveRunConfigDeterministic(t *testing.T) {
	rev := profileTestAsset()
	project := &ProjectModelOverride{DefaultModel: "claude-3-5-haiku-20241022", MaxTokens: ptrInt(512)}
	first, err := ResolveRunConfig(rev, 7, project, RunSelection{Model: "claude-3-5-sonnet-20241022"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveRunConfig(rev, 7, project, RunSelection{Model: "claude-3-5-sonnet-20241022"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("not deterministic:\n%+v\n%+v", first, second)
	}
}

func TestValidateBackendName(t *testing.T) {
	long := strings.Repeat("a", 65)
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty allowed", "", false},
		{"fake", "fake", false},
		{"claude-code", "claude-code", false},
		{"dotted colon", "custom.cli:1", false},
		{"underscore", "my_backend", false},
		{"64 bytes", strings.Repeat("a", 64), false},
		{"65 bytes", long, true},
		{"upper case", "Fake", true},
		{"space inside", "fake backend", true},
		{"slash", "bin/fake", true},
		{"blank", "   ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateBackendName(tc.in)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateBackendName(%q) = nil, want error", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateBackendName(%q) = %v, want nil", tc.in, err)
			}
			if err != nil && !errors.Is(err, ErrInvalidBackendName) {
				t.Fatalf("err=%v want ErrInvalidBackendName", err)
			}
		})
	}
}

func TestValidateModelPolicy(t *testing.T) {
	cases := []struct {
		name    string
		in      *ModelPolicy
		wantErr bool
	}{
		{"nil is the legacy fallback", nil, false},
		{"default only", &ModelPolicy{DefaultModel: "m"}, false},
		{"allowed only", &ModelPolicy{AllowedModels: []string{"m"}}, false},
		{"default in allowed", &ModelPolicy{DefaultModel: "m", AllowedModels: []string{"m", "n"}}, false},
		{"default not in allowed", &ModelPolicy{DefaultModel: "x", AllowedModels: []string{"m"}}, true},
		{"duplicate allowed", &ModelPolicy{AllowedModels: []string{"m", "m"}}, true},
		{"blank allowed entry", &ModelPolicy{AllowedModels: []string{"  "}}, true},
		{"padded default", &ModelPolicy{DefaultModel: " m "}, true},
		{"non positive max tokens", &ModelPolicy{DefaultModel: "m", MaxTokens: ptrInt(0)}, true},
		{"temperature may be zero", &ModelPolicy{DefaultModel: "m", Temperature: ptrFloat(0)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateModelPolicy(tc.in)
			if tc.wantErr && err == nil {
				t.Fatal("want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err != nil && !errors.Is(err, ErrInvalidModelPolicy) {
				t.Fatalf("err=%v want ErrInvalidModelPolicy", err)
			}
		})
	}
}

func TestValidateProjectOverrideMap(t *testing.T) {
	cases := []struct {
		name    string
		raw     map[string]any
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty", map[string]any{}, false},
		{"model policy only", map[string]any{"default_model": "m", "allowed_models": []any{"m"}, "temperature": 0.2, "max_tokens": float64(10)}, false},
		{"backend", map[string]any{"backend": "fake"}, true},
		{"system_prompt", map[string]any{"system_prompt": "x"}, true},
		{"mounts", map[string]any{"mounts": map[string]any{}}, true},
		{"prompt alias", map[string]any{"prompt": "x"}, true},
		{"unknown key fails closed", map[string]any{"whatever": 1}, true},
		{"model policy plus backend", map[string]any{"default_model": "m", "backend": "fake"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProjectOverrideMap(tc.raw)
			if tc.wantErr && err == nil {
				t.Fatal("want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestProjectModelOverrideFromMap(t *testing.T) {
	got, err := ProjectModelOverrideFromMap(map[string]any{})
	if err != nil || got != nil {
		t.Fatalf("empty map = %+v, %v; want nil, nil", got, err)
	}
	got, err = ProjectModelOverrideFromMap(map[string]any{
		"default_model":  "m",
		"allowed_models": []any{"m", "n"},
		"temperature":    json.Number("0.25"),
		"max_tokens":     2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultModel != "m" || !reflect.DeepEqual(got.AllowedModels, []string{"m", "n"}) ||
		got.Temperature == nil || *got.Temperature != 0.25 ||
		got.MaxTokens == nil || *got.MaxTokens != 2048 {
		t.Fatalf("decoded=%+v", got)
	}
	// Wrong value types are refused with a code, not coerced.
	bad := []map[string]any{
		{"default_model": 7},
		{"allowed_models": "m"},
		{"allowed_models": []any{"m", 3}},
		{"temperature": "hot"},
		{"max_tokens": 1.5},
	}
	for _, raw := range bad {
		_, err := ProjectModelOverrideFromMap(raw)
		if err == nil {
			t.Fatalf("expected refusal for %v", raw)
		}
		assertResolutionCode(t, err, CodeInvalidOverrideValue, nil)
	}
}

func TestResolutionErrorFormattingAndSentinels(t *testing.T) {
	err := &ResolutionError{Code: CodeModelNotAllowed, Field: ResolveFieldModel, Detail: "boom", err: ErrModelNotAllowed}
	if !strings.Contains(err.Error(), CodeModelNotAllowed) || !strings.Contains(err.Error(), ResolveFieldModel) {
		t.Fatalf("message=%q", err.Error())
	}
	if !errors.Is(err, ErrModelNotAllowed) {
		t.Fatal("sentinel not reachable")
	}
	var nilErr *ResolutionError
	if nilErr.Error() == "" || nilErr.Unwrap() != nil {
		t.Fatal("nil receiver must be safe")
	}
}

// --- T4.04.a statistics separation: agent_revision_stats ------------------

// statsRow is one row of agent_revision_stats, in scan order.
type statsRow struct {
	agentID   string
	revision  int64
	usage     int64
	score     float64
	sample    int64
	updatedAt int64
}

func dumpStatsRows(t *testing.T, db *sql.DB) []statsRow {
	t.Helper()
	rows, err := db.Query(
		`SELECT agent_id, revision, usage_count, score, sample_size, updated_at
FROM agent_revision_stats ORDER BY agent_id, revision`)
	if err != nil {
		t.Fatalf("dump stats: %v", err)
	}
	defer rows.Close()
	out := make([]statsRow, 0)
	for rows.Next() {
		var r statsRow
		if err := rows.Scan(&r.agentID, &r.revision, &r.usage, &r.score, &r.sample, &r.updatedAt); err != nil {
			t.Fatalf("scan stats row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("dump stats: %v", err)
	}
	return out
}

func statsRowsOf(t *testing.T, db *sql.DB, agentID string) []statsRow {
	t.Helper()
	out := make([]statsRow, 0)
	for _, r := range dumpStatsRows(t, db) {
		if r.agentID == agentID {
			out = append(out, r)
		}
	}
	return out
}

// The acceptance assertion for requirement 2: 100 statistic updates leave the
// revision number and the frozen_config bytes of every revision untouched, and
// the counters really landed in agent_revision_stats.
func TestStatsUpdatesNeverTouchRevisions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_stats_separation.db")
	reg := openWiringRegistry(t, dbPath)
	defer reg.Close()
	ctx := context.Background()

	created, err := reg.Create(ctx, &CreateAgentRequest{
		Name: "Metered", RoleBase: RoleBaseCoder, SystemPrompt: "prompt",
		Binding: &Binding{Model: "gpt-5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// One configuration edit so there are two revisions whose bytes are pinned.
	prompt2 := "prompt v2"
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{SystemPrompt: &prompt2}); err != nil {
		t.Fatal(err)
	}

	headBefore, err := reg.store.headRevision(created.ID)
	if err != nil || headBefore != 2 {
		t.Fatalf("head before stats=%d err=%v want 2", headBefore, err)
	}
	revsBefore := revisionsOf(t, reg, created.ID)
	if len(revsBefore) != 2 {
		t.Fatalf("revisions before stats=%d want 2", len(revsBefore))
	}
	frozenBefore := make(map[int64]string, len(revsBefore))
	for _, r := range revsBefore {
		frozenBefore[r.revision] = r.frozen
	}
	headsBefore := dumpHeadRows(t, reg.store.db)

	const n = 100
	for i := 0; i < n; i++ {
		if err := reg.IncrementUsage(ctx, created.ID); err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
	}
	if err := reg.SetScore(ctx, created.ID, 4.5); err != nil {
		t.Fatal(err)
	}

	// Revision number and frozen bytes are exactly where they were.
	if head, err := reg.store.headRevision(created.ID); err != nil || head != headBefore {
		t.Fatalf("head after %d stats updates=%d err=%v want %d", n, head, err, headBefore)
	}
	revsAfter := revisionsOf(t, reg, created.ID)
	if len(revsAfter) != len(revsBefore) {
		t.Fatalf("revisions after stats=%d want %d", len(revsAfter), len(revsBefore))
	}
	for _, r := range revsAfter {
		if r.frozen != frozenBefore[r.revision] {
			t.Fatalf("frozen_config of revision %d changed by statistics", r.revision)
		}
		if r.source != revisionSourceUpdate {
			t.Fatalf("revision %d source=%q want %q", r.revision, r.source, revisionSourceUpdate)
		}
	}
	if got := dumpHeadRows(t, reg.store.db); !reflect.DeepEqual(headsBefore, got) {
		t.Fatalf("head rows changed: before=%+v after=%+v", headsBefore, got)
	}

	// The counters are in the stats table, on the current head revision. Both
	// revisions have a row because appending a revision records its counters.
	rows := statsRowsOf(t, reg.store.db, created.ID)
	if len(rows) != 2 {
		t.Fatalf("stats rows=%d want 2 (one per revision, carried forward): %+v", len(rows), rows)
	}
	if rows[0].revision != 1 || rows[0].usage != 0 || rows[0].score != 0 {
		t.Fatalf("revision 1 stats=%+v want zero (written before the increments)", rows[0])
	}
	if rows[1].revision != 2 || rows[1].usage != n || rows[1].score != 4.5 {
		t.Fatalf("revision 2 stats=%+v want usage %d score 4.5", rows[1], n)
	}
	if rows[1].updatedAt == 0 {
		t.Fatal("stats updated_at must be recorded")
	}

	// Reads project the table, and the reported live value equals it.
	live, err := reg.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if live.Stats.UsageCount != n || live.Stats.Score != 4.5 {
		t.Fatalf("live stats=%+v want usage %d score 4.5", live.Stats, n)
	}
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}

	// A restart still projects the counters from the table, not from the
	// frozen documents.
	reg2 := openWiringRegistry(t, dbPath)
	defer reg2.Close()
	reopened, err := reg2.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Stats.UsageCount != n || reopened.Stats.Score != 4.5 {
		t.Fatalf("stats after reopen=%+v want usage %d score 4.5", reopened.Stats, n)
	}
	if head, err := reg2.store.headRevision(created.ID); err != nil || head != headBefore {
		t.Fatalf("head after reopen=%d err=%v want %d", head, err, headBefore)
	}
}

// A revision with no stats row reads as the zero value, and a telemetry write
// for an agent that has no revisions at all is a no-op rather than an error.
func TestStatsMissingRowReadsZero(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_stats_missing.db")
	reg := openWiringRegistry(t, dbPath)
	defer reg.Close()
	ctx := context.Background()

	// An agent row written without any revision (the pre-revision shape).
	orphan := &AgentAsset{
		ID: "user-orphan", Name: "Orphan", Version: "0.1.0",
		Source: SourceUser, RoleBase: RoleBaseCoder, Enabled: true,
		Stats:     Stats{UsageCount: 9, Score: 9},
		UpdatedAt: time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC),
	}
	putAgentsRowOnly(t, reg.store, orphan)
	if head, err := reg.store.headRevision(orphan.ID); err != nil || head != 0 {
		t.Fatalf("orphan head=%d err=%v want 0", head, err)
	}

	reg2 := openWiringRegistry(t, dbPath)
	defer reg2.Close()
	got, ok, err := reg2.store.revisionStats(orphan.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ok || got != (RevisionStats{}) {
		t.Fatalf("stats of a revision without a row=(%+v, %v) want zero, false", got, ok)
	}
	loaded, err := reg2.Get(ctx, orphan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Stats.UsageCount != 0 || loaded.Stats.Score != 0 {
		t.Fatalf("orphan read stats=%+v want zero (no stats row)", loaded.Stats)
	}
	// Telemetry for an agent with no revisions cannot attach to anything and
	// must not fail.
	if err := reg2.IncrementUsage(ctx, orphan.ID); err != nil {
		t.Fatalf("increment on revision-less agent: %v", err)
	}
	if rows := statsRowsOf(t, reg2.store.db, orphan.ID); len(rows) != 0 {
		t.Fatalf("revision-less agent got stats rows: %+v", rows)
	}
}

// The FK on (agent_id, revision) is real: a stats row cannot describe a
// revision that does not exist.
func TestStatsRowRequiresRevision(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_stats_fk.db")
	reg := openWiringRegistry(t, dbPath)
	defer reg.Close()

	tx, err := reg.store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = reg.store.writeRevisionStatsTx(tx, "builtin-flow-conductor", 99, RevisionStats{UsageCount: 1})
	_ = tx.Rollback()
	if err == nil {
		t.Fatal("expected the composite foreign key to reject a row for revision 99")
	}
	if !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("err=%v want a foreign key violation", err)
	}
}

// The migration backfills the counters an old database kept inside its
// documents exactly once. Reopening the database must not add a row or raise a
// counter.
func TestStatsBackfillFromLegacyDBIsOneTime(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_stats_backfill.db")
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	assets := []*AgentAsset{
		{
			ID: "legacy-metric", Name: "Metric", Version: "1.0.0",
			Source: SourceUser, RoleBase: RoleBaseCoder, Enabled: true,
			Stats:     Stats{UsageCount: 7, Score: 3.5},
			CreatedAt: base, UpdatedAt: base,
		},
		{
			ID: "legacy-live", Name: "Live", Version: "1.0.0",
			Source: SourceUser, RoleBase: RoleBaseSub, Enabled: true,
			Stats:     Stats{UsageCount: 11, Score: 2.25},
			CreatedAt: base, UpdatedAt: base,
		},
		{
			ID: "legacy-clean", Name: "Clean", Version: "1.0.0",
			Source: SourceUser, RoleBase: RoleBaseMain, Enabled: true,
			CreatedAt: base, UpdatedAt: base,
		},
	}
	// The legacy fixture is a pre-revision database (user_version 0, agents
	// table only), so migration 1 creates the revision snapshots from the
	// payloads - stats included - and migration 2 has to lift those counters
	// into the new table.
	createLegacyAgentsDB(t, dbPath, assets)
	store, err := openSQLiteAgentStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := readUserVersion(t, store.db); got != agentSchemaVersion {
		t.Fatalf("user_version=%d want %d", got, agentSchemaVersion)
	}
	if !tableExists(t, store.db, "agent_revision_stats") {
		t.Fatal("agent_revision_stats missing after migration")
	}
	first := dumpStatsRows(t, store.db)
	if len(first) != 2 {
		t.Fatalf("backfilled rows=%d want 2: %+v", len(first), first)
	}
	want := map[string]statsRow{
		"legacy-metric": {usage: 7, score: 3.5},
		"legacy-live":   {usage: 11, score: 2.25},
	}
	for _, r := range first {
		w, ok := want[r.agentID]
		if !ok {
			t.Fatalf("unexpected backfilled row %+v", r)
		}
		if r.revision != 1 || r.usage != w.usage || r.score != w.score {
			t.Fatalf("backfilled %s = %+v want revision 1 usage %d score %v", r.agentID, r, w.usage, w.score)
		}
		if r.sample != 0 || r.updatedAt == 0 {
			t.Fatalf("backfilled %s = %+v want sample 0 and a timestamp", r.agentID, r)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening three times must not add a row or raise a counter.
	prev := first
	for i := 0; i < 3; i++ {
		storeN, err := openSQLiteAgentStore(dbPath)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		got := dumpStatsRows(t, storeN.db)
		if !reflect.DeepEqual(prev, got) {
			storeN.Close()
			t.Fatalf("reopen %d changed the backfill: before=%+v after=%+v", i, prev, got)
		}
		prev = got
		if err := storeN.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// The real entry point serves the backfilled counters.
	reg, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	ctx := context.Background()
	for _, a := range assets {
		got, err := reg.Get(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Stats.UsageCount != a.Stats.UsageCount || got.Stats.Score != a.Stats.Score {
			t.Fatalf("agent %s read stats=%+v want %+v", a.ID, got.Stats, a.Stats)
		}
	}
	if got, err := reg.Get(ctx, "legacy-clean"); err != nil {
		t.Fatal(err)
	} else if got.Stats.UsageCount != 0 || got.Stats.Score != 0 {
		t.Fatalf("clean agent stats=%+v want zero", got.Stats)
	}
}

// A head revision whose payload counters are newer than its frozen document
// wins the backfill: the newer number is what the installation shows today.
// This is the shape a pre-T4.04.a database really had, because statistic
// updates only ever rewrote the agents payload.
func TestStatsBackfillPrefersNewerPayloadCounters(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_stats_payload_wins.db")
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	legacy := &AgentAsset{
		ID: "legacy-incremented", Name: "Incremented", Version: "1.0.0",
		Source: SourceUser, RoleBase: RoleBaseCoder, Enabled: true,
		Stats:     Stats{UsageCount: 3, Score: 1.5},
		CreatedAt: base, UpdatedAt: base,
	}
	createLegacyAgentsDB(t, dbPath, []*AgentAsset{legacy})

	// Apply migration 1 by hand and stop there: revision tables exist, the
	// stats table does not, and the revision document carries stale counters
	// while the payload carries the live ones.
	db, err := dbx.Open(dbPath, dbx.WithMaxOpenConns(1))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range agentSchemaMigrations[0].statements {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatalf("apply migration 1: %v", err)
		}
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`UPDATE agent_revisions SET frozen_config = ? WHERE agent_id = ? AND revision = 1`,
		`{"id":"legacy-incremented","stats":{"usage_count":0,"score":0}}`, legacy.ID,
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := openSQLiteAgentStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := readUserVersion(t, store.db); got != agentSchemaVersion {
		t.Fatalf("user_version=%d want %d", got, agentSchemaVersion)
	}
	rows := statsRowsOf(t, store.db, legacy.ID)
	if len(rows) != 1 {
		t.Fatalf("backfilled rows=%d want 1: %+v", len(rows), rows)
	}
	if rows[0].revision != 1 || rows[0].usage != 3 || rows[0].score != 1.5 {
		t.Fatalf("backfilled=%+v want the newer payload counters {3, 1.5}", rows[0])
	}
}

// A configuration edit carries the cumulative counters onto the revision it
// appends, sample size included: the telemetry of the live asset must not reset
// just because the configuration moved on.
func TestStatsCarryForwardOnRevisionAppend(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_stats_carry.db")
	reg := openWiringRegistry(t, dbPath)
	defer reg.Close()
	ctx := context.Background()

	created, err := reg.Create(ctx, &CreateAgentRequest{
		Name: "Carried", RoleBase: RoleBaseCoder, SystemPrompt: "one",
		Binding: &Binding{Model: "gpt-5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetRevisionStats(ctx, created.ID, RevisionStats{
		UsageCount: 12, Score: 4.25, SampleSize: 30,
	}); err != nil {
		t.Fatal(err)
	}
	head, err := reg.store.headRevision(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if head != 1 {
		t.Fatalf("head=%d want 1", head)
	}
	rows := statsRowsOf(t, reg.store.db, created.ID)
	if len(rows) != 1 || rows[0].usage != 12 || rows[0].score != 4.25 || rows[0].sample != 30 {
		t.Fatalf("revision 1 stats=%+v want {12, 4.25, sample 30}", rows)
	}

	prompt2 := "two"
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{SystemPrompt: &prompt2}); err != nil {
		t.Fatal(err)
	}
	rows = statsRowsOf(t, reg.store.db, created.ID)
	if len(rows) != 2 {
		t.Fatalf("stats rows after edit=%d want 2: %+v", len(rows), rows)
	}
	if rows[1].revision != 2 || rows[1].usage != 12 || rows[1].score != 4.25 || rows[1].sample != 30 {
		t.Fatalf("revision 2 stats=%+v want the carried counters {12, 4.25, sample 30}", rows[1])
	}
	// The revision number of the lineage advanced, but the counters are the
	// same numbers: the edit did not reset the telemetry.
	live, err := reg.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if live.Stats.UsageCount != 12 || live.Stats.Score != 4.25 {
		t.Fatalf("live stats after edit=%+v want {12, 4.25}", live.Stats)
	}
	// The frozen document of the new revision still records the counters as a
	// historical echo; the read path ignores them in favour of the table.
	revs := revisionsOf(t, reg, created.ID)
	if len(revs) != 2 {
		t.Fatalf("revisions=%d want 2", len(revs))
	}
	var frozen AgentAsset
	if err := json.Unmarshal([]byte(revs[1].frozen), &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.Stats.UsageCount != 12 || frozen.Stats.Score != 4.25 {
		t.Fatalf("frozen echo of revision 2=%+v want {12, 4.25}", frozen.Stats)
	}
}

// The override surface is model policy only, and that is a property of the
// types, not only of the raw-map validator: a field added to
// ProjectModelOverride or RunSelection for backend/system_prompt/mounts would
// be a silent widening of what a project (or a run) may choose, so this test
// pins the field sets. ResolveRunConfig must be the only place the structural
// fields are written, and it writes them from the revision.
func TestOverrideSurfaceIsModelPolicyOnly(t *testing.T) {
	// json tags of ProjectModelOverride: exactly the model policy keys.
	override := reflect.TypeOf(ProjectModelOverride{})
	gotOverride := make([]string, 0, override.NumField())
	for i := 0; i < override.NumField(); i++ {
		tag := override.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Fatalf("ProjectModelOverride.%s has no json tag", override.Field(i).Name)
		}
		gotOverride = append(gotOverride, strings.Split(tag, ",")[0])
	}
	sort.Strings(gotOverride)
	wantOverride := []string{OverrideKeyAllowedModels, OverrideKeyDefaultModel, OverrideKeyMaxTokens, OverrideKeyTemperature}
	sort.Strings(wantOverride)
	if !reflect.DeepEqual(gotOverride, wantOverride) {
		t.Fatalf("ProjectModelOverride json keys=%v want %v; backend/system_prompt/mounts must not be overrideable", gotOverride, wantOverride)
	}

	// RunSelection: the model and nothing else.
	sel := reflect.TypeOf(RunSelection{})
	if sel.NumField() != 1 || sel.Field(0).Name != "Model" {
		names := make([]string, 0, sel.NumField())
		for i := 0; i < sel.NumField(); i++ {
			names = append(names, sel.Field(i).Name)
		}
		t.Fatalf("RunSelection fields=%v want exactly [Model]", names)
	}

	// And every field of ResolvedAgentConfig that comes from a revision-only
	// source is still labelled asset_revision when a project override and a run
	// selection are both present.
	rev := profileTestAsset()
	project := &ProjectModelOverride{DefaultModel: "claude-3-5-sonnet-20241022", Temperature: ptrFloat(0.9)}
	got, err := ResolveRunConfig(rev, 3, project, RunSelection{Model: "claude-3-5-sonnet-20241022"})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{ResolveFieldBackend, ResolveFieldSystemPrompt, ResolveFieldMounts} {
		if got.Sources[field] != ResolutionAssetRevision {
			t.Fatalf("source of %s=%q want %q", field, got.Sources[field], ResolutionAssetRevision)
		}
	}
}

// The exact shape the dispatch names: a legacy database that already has the
// revision tables (user_version 1) and whose frozen_config carries non-zero
// Stats. Opening it backfills the stats table once; repeated opens neither add
// a row nor raise a counter.
func TestStatsBackfillFromRevisionOnlyLegacyDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_stats_revonly.db")
	base := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	legacy := &AgentAsset{
		ID: "legacy-frozen", Name: "Frozen", Version: "1.0.0",
		Source: SourceUser, RoleBase: RoleBaseCoder, Enabled: true,
		CreatedAt: base, UpdatedAt: base,
	}
	createLegacyAgentsDB(t, dbPath, []*AgentAsset{legacy})

	// Build the pre-T4.04.a shape by hand: migration 1 applied and stamped, the
	// frozen document holding counters (as an old revision snapshot did), the
	// agents payload holding the same ones.
	db, err := dbx.Open(dbPath, dbx.WithMaxOpenConns(1))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range agentSchemaMigrations[0].statements {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatalf("apply migration 1: %v", err)
		}
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	frozen := `{"id":"legacy-frozen","stats":{"usage_count":5,"score":2.5}}`
	if _, err := db.Exec(
		`UPDATE agent_revisions SET frozen_config = ? WHERE agent_id = ? AND revision = 1`,
		frozen, legacy.ID,
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Four consecutive opens: the backfill runs on the first and is invisible
	// on the rest.
	var first []statsRow
	for i := 0; i < 4; i++ {
		store, err := openSQLiteAgentStore(dbPath)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		got := statsRowsOf(t, store.db, legacy.ID)
		if i == 0 {
			if len(got) != 1 {
				store.Close()
				t.Fatalf("backfilled rows=%d want 1: %+v", len(got), got)
			}
			if got[0].revision != 1 || got[0].usage != 5 || got[0].score != 2.5 {
				store.Close()
				t.Fatalf("backfilled=%+v want {revision 1, usage 5, score 2.5}", got[0])
			}
			first = got
		} else if !reflect.DeepEqual(first, got) {
			store.Close()
			t.Fatalf("open %d changed the backfill: before=%+v after=%+v", i, first, got)
		}
		// The frozen document is never rewritten, not even by the migration.
		var stored string
		if err := store.db.QueryRow(
			`SELECT frozen_config FROM agent_revisions WHERE agent_id = ? AND revision = 1`,
			legacy.ID,
		).Scan(&stored); err != nil {
			store.Close()
			t.Fatal(err)
		}
		if stored != frozen {
			store.Close()
			t.Fatalf("open %d rewrote frozen_config: got=%s want=%s", i, stored, frozen)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// assertResolutionCode checks the concrete error is a *ResolutionError with the
// wanted code, and (when sentinel is non-nil) that errors.Is matches it.
func assertResolutionCode(t *testing.T, err error, code string, sentinel error) {
	t.Helper()
	var re *ResolutionError
	if !errors.As(err, &re) {
		t.Fatalf("error type=%T (%v) want *ResolutionError", err, err)
	}
	if re.Code != code {
		t.Fatalf("code=%q want %q (%v)", re.Code, code, err)
	}
	if sentinel != nil && !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(%v, %v) = false", err, sentinel)
	}
}

// preT404AgentAsset is the field set and JSON encoding an AgentAsset had before
// T4.04.a, frozen here on purpose: it is the golden encoder the stability test
// compares against.
type preT404AgentAsset struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Avatar       string      `json:"avatar,omitempty"`
	Description  string      `json:"description,omitempty"`
	Version      string      `json:"version"`
	Source       AgentSource `json:"source"`
	RoleBase     RoleBase    `json:"role_base"`
	SystemPrompt string      `json:"system_prompt,omitempty"`
	Binding      Binding     `json:"binding"`
	Mounts       Mounts      `json:"mounts"`
	StageTags    []string    `json:"stage_tags,omitempty"`
	Stats        Stats       `json:"stats"`
	Enabled      bool        `json:"enabled"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

func toPreT404(a *AgentAsset) preT404AgentAsset {
	return preT404AgentAsset{
		ID: a.ID, Name: a.Name, Avatar: a.Avatar, Description: a.Description,
		Version: a.Version, Source: a.Source, RoleBase: a.RoleBase,
		SystemPrompt: a.SystemPrompt, Binding: a.Binding, Mounts: a.Mounts,
		StageTags: a.StageTags, Stats: a.Stats, Enabled: a.Enabled,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

// An asset whose new fields are empty must encode to exactly the bytes the
// pre-T4.04.a model produced: the HTTP responses of the existing Agent API
// cannot change (T4.04.a boundary). Every builtin is checked, and the small
// testdata file pins the literal bytes of one asset that carries every legacy
// field.
func TestAgentAssetJSONByteStability(t *testing.T) {
	for _, a := range builtinAgents() {
		pinned := *a
		pinned.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		pinned.UpdatedAt = pinned.CreatedAt
		if pinned.Purpose != "" || pinned.Backend != "" || pinned.ModelPolicy != nil || pinned.Fit != nil {
			t.Fatalf("builtin %s must not carry the new fields yet (T4.04.b fills them): %+v", a.ID, pinned)
		}
		want, err := json.Marshal(toPreT404(&pinned))
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(&pinned)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("asset %s encoding changed:\nwant=%s\ngot =%s", a.ID, want, got)
		}
		for _, key := range []string{`"purpose"`, `"backend"`, `"model_policy"`, `"fit"`} {
			if strings.Contains(string(got), key) {
				t.Fatalf("asset %s leaked empty new key %s", a.ID, key)
			}
		}
	}

	// Literal golden: a user asset with every legacy field populated, encoded
	// exactly as the pre-T4.04.a model encoded it (captured from the b712d99
	// tree). This is the strongest form of the "responses do not change"
	// assertion, because the expected bytes are not derived from the current
	// struct.
	const golden = `{"id":"user-golden","name":"Golden \"Asset\"","avatar":"A","description":"every legacy field populated","version":"2.1.0","source":"user","role_base":"coder","system_prompt":"line one\nline two","binding":{"model":"gpt-5","channel":"default","temperature":0.25,"max_tokens":2048},"mounts":{"mcp_tools":["fs.read","shell.exec"],"skills":["review"]},"stage_tags":["coding","review"],"stats":{"usage_count":3,"score":4.75},"enabled":true,"created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T04:05:06Z"}`
	temp := 0.25
	maxTok := 2048
	asset := AgentAsset{
		ID: "user-golden", Name: "Golden \"Asset\"", Avatar: "A",
		Description: "every legacy field populated",
		Version:     "2.1.0", Source: SourceUser, RoleBase: RoleBaseCoder,
		SystemPrompt: "line one\nline two",
		Binding:      Binding{Model: "gpt-5", Channel: "default", Temperature: &temp, MaxTokens: &maxTok},
		Mounts:       Mounts{MCPTools: []string{"fs.read", "shell.exec"}, Skills: []string{"review"}},
		StageTags:    []string{"coding", "review"},
		Stats:        Stats{UsageCount: 3, Score: 4.75},
		Enabled:      true,
		CreatedAt:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 1, 2, 4, 5, 6, 0, time.UTC),
	}
	got, err := json.Marshal(&asset)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != golden {
		t.Fatalf("golden mismatch:\nwant=%s\ngot =%s", golden, got)
	}
	// And it round-trips back into the same asset: the decoder ignores nothing
	// and invents nothing.
	var back AgentAsset
	if err := json.Unmarshal([]byte(golden), &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&back, &asset) {
		t.Fatalf("golden round trip mismatch:\ngot =%+v\nwant=%+v", back, asset)
	}
}

// The new pointer fields are deep-copied by the registry read path, so a caller
// cannot mutate a stored revision through a returned asset.
func TestCloneAgentDeepCopiesNewFields(t *testing.T) {
	rev := profileTestAsset()
	r := &InMemoryAgentRegistry{agents: map[string]*AgentAsset{rev.ID: &rev}}
	got, err := r.Get(nil, rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	got.ModelPolicy.AllowedModels[0] = "hacked"
	*got.ModelPolicy.Temperature = 9.9
	*got.ModelPolicy.MaxTokens = 1
	got.Fit.Stages[0] = "hacked"

	again, err := r.Get(nil, rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.ModelPolicy.AllowedModels[0] != "claude-3-5-sonnet-20241022" ||
		*again.ModelPolicy.Temperature != 0.4 || *again.ModelPolicy.MaxTokens != 4096 ||
		again.Fit.Stages[0] != "coding" {
		t.Fatalf("stored asset mutated: %+v", again)
	}
}
