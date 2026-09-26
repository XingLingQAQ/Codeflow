package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/config"
)

// T4.04.a legacy adapter tests: the three-role configuration
// (config.RoleConfig main/coder/sub) maps onto agent asset fields without
// silently dropping anything, and every unmappable value comes back as a
// diagnostic. Fixtures live in testdata/legacy_roles.

type legacyRoleFixture struct {
	Role    string             `json:"role"`
	Comment string             `json:"comment,omitempty"`
	Config  *config.RoleConfig `json:"config"`
}

func loadLegacyFixture(t *testing.T, name string) legacyRoleFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "legacy_roles", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var f legacyRoleFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return f
}

// The three default fixtures must be the real config.DefaultRoleConfigs
// payloads: a fixture that drifted from the shipped defaults would make every
// assertion below meaningless.
func TestLegacyFixturesMatchDefaultRoleConfigs(t *testing.T) {
	for _, tc := range []struct {
		file string
		role config.RoleType
	}{
		{"role_main.json", config.RoleMain},
		{"role_coder.json", config.RoleCoder},
		{"role_sub.json", config.RoleSub},
	} {
		t.Run(tc.file, func(t *testing.T) {
			f := loadLegacyFixture(t, tc.file)
			if config.RoleType(f.Role) != tc.role {
				t.Fatalf("fixture role=%q want %q", f.Role, tc.role)
			}
			want, ok := config.DefaultRoleConfigs[tc.role]
			if !ok {
				t.Fatalf("no default role config for %q", tc.role)
			}
			if !reflect.DeepEqual(f.Config, want) {
				t.Fatalf("fixture differs from DefaultRoleConfigs[%s]:\ngot =%#v\nwant=%#v", tc.role, f.Config, want)
			}
		})
	}
}

// Table-driven mapping and diagnostics: every fixture is mapped and compared
// against the expected asset fields and the expected diagnostic list.
func TestMapLegacyRoleConfigTable(t *testing.T) {
	zero := 0.0
	cases := []struct {
		file      string
		want      LegacyMapping
		wantDiags []LegacyDiagnostic
	}{
		{
			file: "role_main.json",
			want: LegacyMapping{
				RoleBase:     RoleBaseMain,
				ModelPolicy:  &ModelPolicy{DefaultModel: "claude-3-5-sonnet-20241022", Temperature: ptrFloat(1.0)},
				SystemPrompt: "You are the main AI commander.",
				Mounts:       Mounts{MCPTools: []string{"orchestrator"}},
				Binding:      Binding{Channel: "default"},
			},
		},
		{
			file: "role_coder.json",
			want: LegacyMapping{
				RoleBase:     RoleBaseCoder,
				ModelPolicy:  &ModelPolicy{DefaultModel: "claude-3-5-sonnet-20241022", Temperature: ptrFloat(0.7)},
				SystemPrompt: "You are a code implementation expert.",
				Mounts:       Mounts{MCPTools: []string{"filesystem", "linter"}},
				Binding:      Binding{Channel: "default"},
			},
		},
		{
			file: "role_sub.json",
			want: LegacyMapping{
				RoleBase:     RoleBaseSub,
				ModelPolicy:  &ModelPolicy{DefaultModel: "claude-3-5-haiku-20241022", Temperature: ptrFloat(0.8)},
				SystemPrompt: "You are a research assistant.",
				Mounts:       Mounts{MCPTools: []string{"websearch"}},
				Binding:      Binding{Channel: "default"},
			},
		},
		{
			// Every mapped field plus every unmapped one: the diagnostics list
			// is exactly the four fields with no landing spot, in field order.
			file: "role_coder_full.json",
			want: LegacyMapping{
				RoleBase: RoleBaseCoder,
				ModelPolicy: &ModelPolicy{
					DefaultModel: "claude-3-5-sonnet-20241022",
					Temperature:  ptrFloat(0.3),
				},
				SystemPrompt: "Full legacy coder prompt.",
				Mounts: Mounts{
					MCPTools: []string{"filesystem", "linter", "shell"},
					Skills:   []string{"review", "refactor"},
				},
				Binding: Binding{Channel: "team-channel"},
			},
			wantDiags: []LegacyDiagnostic{
				legacyNoAssetField("TopP"),
				legacyNoAssetField("AnswerStyle"),
				legacyNoAssetField("Capabilities"),
				legacyNoAssetField("AllowedHooks"),
			},
		},
		{
			// An empty legacy config still maps losslessly: role and the
			// non-optional temperature scalar (an explicit 0) survive, and
			// there is nothing to diagnose because nothing carries a value.
			file: "role_main_empty.json",
			want: LegacyMapping{
				RoleBase:    RoleBaseMain,
				ModelPolicy: &ModelPolicy{Temperature: &zero},
				Binding:     Binding{},
			},
		},
		{
			// A role the legacy hierarchy does not define is reported, not
			// guessed: RoleBase stays empty and the caller decides.
			file: "role_unknown.json",
			want: LegacyMapping{
				RoleBase:     "",
				ModelPolicy:  &ModelPolicy{DefaultModel: "claude-3-5-haiku-20241022", Temperature: ptrFloat(0.9)},
				SystemPrompt: "Adversarial reviewer.",
				// The fixture carries an explicit empty list, which is copied
				// faithfully (empty non-nil), not normalized to nil.
				Mounts:  Mounts{MCPTools: []string{}},
				Binding: Binding{Channel: "default"},
			},
			wantDiags: []LegacyDiagnostic{{
				Code:   LegacyCodeRoleUnmapped,
				Field:  "role",
				Reason: `legacy role "critic" has no RoleBase equivalent (main, coder, sub only); the caller must choose one explicitly`,
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			f := loadLegacyFixture(t, tc.file)
			got, diags := MapLegacyRoleConfig(config.RoleType(f.Role), f.Config)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mapping mismatch:\ngot =%#v\nwant=%#v", got, tc.want)
			}
			if len(diags) == 0 && len(tc.wantDiags) == 0 {
				return
			}
			if !reflect.DeepEqual(diags, tc.wantDiags) {
				t.Fatalf("diagnostics mismatch:\ngot =%#v\nwant=%#v", diags, tc.wantDiags)
			}
			for _, d := range diags {
				if d.Field == "" || d.Reason == "" || d.Code == "" {
					t.Fatalf("diagnostic must name field/code/reason: %+v", d)
				}
			}
		})
	}
}

// The three shipped role configs must map without any diagnostic: a caller
// converting an untouched installation has nothing to decide.
func TestMapLegacyDefaultRolesHaveNoDiagnostics(t *testing.T) {
	for role := range config.DefaultRoleConfigs {
		t.Run(string(role), func(t *testing.T) {
			m, diags := MapLegacyRoleConfig(role, config.DefaultRoleConfigs[role])
			if len(diags) != 0 {
				t.Fatalf("default role %s produced diagnostics: %+v", role, diags)
			}
			if m.RoleBase == "" || m.ModelPolicy == nil || m.ModelPolicy.DefaultModel == "" {
				t.Fatalf("default role %s mapped to %#v", role, m)
			}
			// The mapping output must be usable as an agent revision as-is.
			if err := ValidateModelPolicy(m.ModelPolicy); err != nil {
				t.Fatalf("mapped model policy invalid: %v", err)
			}
			// AllowedModels empty means "only DefaultModel": the legacy
			// single-model semantics survive.
			rev := AgentAsset{
				ID: "converted-" + string(role), Name: "converted", Version: "1.0.0",
				Source: SourceUser, RoleBase: m.RoleBase, Enabled: true,
				SystemPrompt: m.SystemPrompt, Binding: m.Binding, Mounts: m.Mounts,
				ModelPolicy: m.ModelPolicy,
			}
			resolved, err := ResolveRunConfig(rev, 1, nil, RunSelection{Model: m.ModelPolicy.DefaultModel})
			if err != nil {
				t.Fatalf("converted revision does not resolve: %v", err)
			}
			if resolved.Model != m.ModelPolicy.DefaultModel {
				t.Fatalf("resolved model=%q want %q", resolved.Model, m.ModelPolicy.DefaultModel)
			}
		})
	}
}

// Nothing is dropped silently: every field of config.RoleConfig is either in
// the documented mapping table or in the documented diagnostic table. A field
// added to the config package later fails this test until it gets a decision.
func TestLegacyMappingCoversEveryRoleConfigField(t *testing.T) {
	typ := reflect.TypeOf(config.RoleConfig{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		_, mapped := legacyMappedFields[name]
		_, diagnosed := legacyUnmappedFields[name]
		switch {
		case mapped && diagnosed:
			t.Fatalf("field %s is both mapped and diagnosed", name)
		case !mapped && !diagnosed:
			t.Fatalf("field %s has neither a mapping nor a diagnostic: add it to legacyMappedFields or legacyUnmappedFields", name)
		}
	}

	// The full fixture exercises every mapped field and every diagnosed field,
	// so the coverage above is backed by actual values.
	f := loadLegacyFixture(t, "role_coder_full.json")
	if f.Config == nil {
		t.Fatal("full fixture has no config")
	}
	m, diags := MapLegacyRoleConfig(config.RoleCoder, f.Config)
	if m.ModelPolicy == nil || m.ModelPolicy.DefaultModel != f.Config.Model ||
		*m.ModelPolicy.Temperature != f.Config.Temperature ||
		m.SystemPrompt != f.Config.SystemPrompt ||
		!reflect.DeepEqual(m.Mounts.MCPTools, f.Config.MCPTools) ||
		!reflect.DeepEqual(m.Mounts.Skills, f.Config.AllowedSkills) ||
		m.Binding.Channel != f.Config.APIChannel {
		t.Fatalf("full fixture mapping lost a documented field: %#v -> %#v", f.Config, m)
	}
	if len(diags) != 4 {
		t.Fatalf("full fixture diagnostics=%d want 4 (%+v)", len(diags), diags)
	}

	// The empty fixture carries no unmapped value, so it must not produce a
	// diagnostic even though the four fields exist.
	empty := loadLegacyFixture(t, "role_main_empty.json")
	if _, diags := MapLegacyRoleConfig(config.RoleMain, empty.Config); len(diags) != 0 {
		t.Fatalf("empty fixture produced diagnostics: %+v", diags)
	}
}

// A nil config is reported instead of panicking, and an unknown role with a nil
// config reports both facts.
func TestMapLegacyRoleConfigNilAndUnknownRole(t *testing.T) {
	m, diags := MapLegacyRoleConfig(config.RoleMain, nil)
	if m.RoleBase != RoleBaseMain || m.ModelPolicy != nil {
		t.Fatalf("nil config mapping=%#v", m)
	}
	if len(diags) != 1 || diags[0].Code != LegacyCodeConfigMissing {
		t.Fatalf("nil config diagnostics=%+v", diags)
	}
	if !strings.Contains(diags[0].Reason, "nil") {
		t.Fatalf("reason=%q", diags[0].Reason)
	}

	_, diags = MapLegacyRoleConfig(config.RoleType("reviewer"), nil)
	if len(diags) != 2 {
		t.Fatalf("unknown role + nil config diagnostics=%+v want 2", diags)
	}
	if diags[0].Code != LegacyCodeRoleUnmapped || diags[1].Code != LegacyCodeConfigMissing {
		t.Fatalf("diagnostic order=%+v", diags)
	}
}

// The mapping copies values verbatim: whitespace or an out-of-range value is
// reported by the downstream validator, not rewritten here.
func TestMapLegacyRoleConfigCopiesVerbatim(t *testing.T) {
	m, _ := MapLegacyRoleConfig(config.RoleCoder, &config.RoleConfig{
		Model: "  padded model  ", Temperature: 0.5,
		MCPTools: []string{" a "}, AllowedSkills: []string{},
	})
	if m.ModelPolicy.DefaultModel != "  padded model  " {
		t.Fatalf("model=%q want the verbatim source value", m.ModelPolicy.DefaultModel)
	}
	if err := ValidateModelPolicy(m.ModelPolicy); err == nil {
		t.Fatal("verbatim garbage must be caught by ValidateModelPolicy")
	}
	if !reflect.DeepEqual(m.Mounts.MCPTools, []string{" a "}) {
		t.Fatalf("mcp tools=%v", m.Mounts.MCPTools)
	}
	if m.Mounts.Skills == nil || len(m.Mounts.Skills) != 0 {
		t.Fatalf("empty non-nil skills list must stay empty non-nil: %#v", m.Mounts.Skills)
	}
}
