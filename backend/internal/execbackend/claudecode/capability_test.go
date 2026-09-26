package claudecode

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// fixedClock 是注入式时钟：ProbedAt 必须是注入时钟给出的时间。
type fixedClock struct{ at time.Time }

// Now 实现 Clock。
func (c fixedClock) Now() time.Time { return c.at }

// probeTime 是测试里统一的探测时间。
var probeTime = time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)

// derivableCapabilities 是 T1.13.a 允许由 help 事实推导出来的能力。
func derivableCapabilities() []execbackend.Capability {
	return []execbackend.Capability{
		execbackend.CapabilityNonInteractive,
		execbackend.CapabilityJSONStream,
		execbackend.CapabilityApprovalHook,
		execbackend.CapabilityUsageTokens,
		execbackend.CapabilityUsageCost,
		execbackend.CapabilityMCP,
	}
}

// alwaysFalseCapabilities 是本步永远为 false 的能力（证据只能来自真实进程，归 T1.13.c/T2.04）。
func alwaysFalseCapabilities() []execbackend.Capability {
	return []execbackend.Capability{
		execbackend.CapabilityCancelGraceful,
		execbackend.CapabilityResumeCheckpoint,
		execbackend.CapabilityInject,
		execbackend.CapabilityPTY,
		execbackend.CapabilitySandbox,
	}
}

// fixtureFacts 用 fixture 解析结果拼出探测事实。
func fixtureFacts(t *testing.T) Facts {
	t.Helper()
	return Facts{
		ExecutablePath: `C:\claude\claude.exe`,
		ProbedAt:       probeTime,
		Version:        fixtureVersion(t),
		Flags:          fixtureFlags(t),
	}
}

// problemCodes 返回问题列表的 code 集合，便于断言"包含某 code"。
func problemCodes(problems []Problem) []string {
	codes := make([]string, 0, len(problems))
	for _, p := range problems {
		codes = append(codes, p.Code)
	}
	return codes
}

// findProblem 返回第一个 code 匹配且 Detail 含 want 的问题。
func findProblem(problems []Problem, code, want string) (Problem, bool) {
	for _, p := range problems {
		if p.Code == code && strings.Contains(p.Detail, want) {
			return p, true
		}
	}
	return Problem{}, false
}

func hasProblemCode(problems []Problem, code string) bool {
	_, ok := findProblem(problems, code, "")
	return ok
}

func TestDeriveCapabilitiesFixture(t *testing.T) {
	report, problems := DeriveCapabilities(fixtureFacts(t), derivableCapabilities())
	if len(problems) != 0 {
		t.Fatalf("fixture probe must have no problems, got %v", problems)
	}
	if report.Backend != BackendName {
		t.Fatalf("Backend = %q, want %q", report.Backend, BackendName)
	}
	if report.Backend != "claude_code" {
		t.Fatalf("Backend must match readiness execBackendNames, got %q", report.Backend)
	}
	if report.Version != "2.1.283" {
		t.Fatalf("Version = %q, want 2.1.283", report.Version)
	}
	if report.Source == "" {
		t.Fatal("Source must be non-empty")
	}
	if !report.ProbedAt.Equal(probeTime) {
		t.Fatalf("ProbedAt = %v, want %v", report.ProbedAt, probeTime)
	}
	for _, c := range derivableCapabilities() {
		if !report.Has(c) {
			t.Errorf("capability %s must be proven by the fixture", c)
		}
		if report.EvidenceFor(c) == "" {
			t.Errorf("capability %s must carry non-empty evidence", c)
		}
	}
	for _, c := range alwaysFalseCapabilities() {
		if report.Has(c) {
			t.Errorf("capability %s must stay false in T1.13.a", c)
		}
	}
}

func TestDeriveCapabilitiesEvidenceMentionsSources(t *testing.T) {
	report, _ := DeriveCapabilities(fixtureFacts(t), derivableCapabilities())
	for _, c := range derivableCapabilities() {
		evidence := report.EvidenceFor(c)
		if !strings.Contains(evidence, docsSnapshot) {
			t.Errorf("%s evidence must cite the docs snapshot date %s: %s", c, docsSnapshot, evidence)
		}
		if !strings.Contains(evidence, "code.claude.com/docs") {
			t.Errorf("%s evidence must cite a doc URL: %s", c, evidence)
		}
	}
	cost := report.EvidenceFor(execbackend.CapabilityUsageCost)
	if !strings.Contains(cost, "estimated") {
		t.Errorf("usage_cost evidence must state the estimate quality: %s", cost)
	}
	if !strings.Contains(report.EvidenceFor(execbackend.CapabilityApprovalHook), "dontAsk") {
		t.Error("approval_hook evidence must mention dontAsk")
	}
}

func TestDeriveCapabilitiesImplementedEmpty(t *testing.T) {
	report, problems := DeriveCapabilities(fixtureFacts(t), nil)
	if len(problems) != 0 {
		t.Fatalf("no CLI fact is missing, so there must be no problems: %v", problems)
	}
	for _, c := range execbackend.AllCapabilities() {
		if report.Has(c) {
			t.Errorf("capability %s must be false when implemented is empty", c)
		}
	}
}

func TestDeriveCapabilitiesAlwaysFalseEvenIfImplemented(t *testing.T) {
	report, _ := DeriveCapabilities(fixtureFacts(t), alwaysFalseCapabilities())
	for _, c := range alwaysFalseCapabilities() {
		if report.Has(c) {
			t.Errorf("capability %s must never be derived from read-only facts", c)
		}
	}
	if report.EvidenceFor(execbackend.CapabilitySandbox) != "" {
		t.Error("always-false capabilities must not carry evidence")
	}
}

func TestDeriveCapabilitiesPartialImplemented(t *testing.T) {
	implemented := []execbackend.Capability{execbackend.CapabilityJSONStream}
	report, problems := DeriveCapabilities(fixtureFacts(t), implemented)
	if len(problems) != 0 {
		t.Fatalf("no fact is missing: %v", problems)
	}
	if !report.Has(execbackend.CapabilityJSONStream) {
		t.Fatal("json_stream is implemented and proven, must be true")
	}
	if report.Has(execbackend.CapabilityNonInteractive) {
		t.Fatal("non_interactive is proven by facts but not implemented, must be false")
	}
}

func TestDeriveCapabilitiesMissingPermissionPrompts(t *testing.T) {
	facts := fixtureFacts(t)
	flags, err := ParseHelp(removeHelpEntry(t, fixtureHelpText(t), "--permission-prompts"))
	if err != nil {
		t.Fatalf("ParseHelp(without --permission-prompts): %v", err)
	}
	facts.Flags = flags

	report, problems := DeriveCapabilities(facts, derivableCapabilities())
	if report.Has(execbackend.CapabilityApprovalHook) {
		t.Fatal("approval_hook must be false without --permission-prompts")
	}
	if _, ok := findProblem(problems, ProblemRequiredFlagMissing, "--permission-prompts"); !ok {
		t.Fatalf("want required_flag_missing for --permission-prompts, got %v", problems)
	}
	// 其他能力不受影响：单个事实缺失不能让整份报告归零。
	for _, c := range []execbackend.Capability{
		execbackend.CapabilityNonInteractive,
		execbackend.CapabilityJSONStream,
		execbackend.CapabilityUsageTokens,
		execbackend.CapabilityMCP,
	} {
		if !report.Has(c) {
			t.Errorf("capability %s must stay proven: %v", c, problems)
		}
	}
}

func TestDeriveCapabilitiesMissingDontAskChoice(t *testing.T) {
	facts := fixtureFacts(t)
	help := strings.Replace(fixtureHelpText(t), `"dontAsk", "plan")`, `"plan")`, 1)
	if help == fixtureHelpText(t) {
		t.Fatal("fixture mutation did not apply")
	}
	flags, err := ParseHelp(help)
	if err != nil {
		t.Fatalf("ParseHelp(without dontAsk): %v", err)
	}
	facts.Flags = flags

	report, problems := DeriveCapabilities(facts, derivableCapabilities())
	if report.Has(execbackend.CapabilityApprovalHook) {
		t.Fatal("approval_hook must be false when dontAsk is not a choice")
	}
	if _, ok := findProblem(problems, ProblemRequiredChoiceMissing, "dontAsk"); !ok {
		t.Fatalf("want required_choice_missing for dontAsk, got %v", problems)
	}
	if _, ok := findProblem(problems, ProblemRequiredChoiceMissing, "--permission-mode"); !ok {
		t.Fatalf("required_choice_missing detail must name the flag: %v", problems)
	}
}

func TestDeriveCapabilitiesMissingStreamJSON(t *testing.T) {
	facts := fixtureFacts(t)
	// 只改 --output-format 的 choices，不动 --input-format 的同名取值。
	facts.Flags = dropOutputFormatStreamJSON(t)

	report, problems := DeriveCapabilities(facts, derivableCapabilities())
	for _, c := range []execbackend.Capability{
		execbackend.CapabilityJSONStream,
		execbackend.CapabilityUsageTokens,
		execbackend.CapabilityUsageCost,
	} {
		if report.Has(c) {
			t.Errorf("capability %s needs stream-json", c)
		}
	}
	if _, ok := findProblem(problems, ProblemRequiredChoiceMissing, "stream-json"); !ok {
		t.Fatalf("want required_choice_missing for stream-json, got %v", problems)
	}
	if !report.Has(execbackend.CapabilityNonInteractive) {
		t.Error("non_interactive must be unaffected")
	}
}

func TestDeriveCapabilitiesVersionBoundary(t *testing.T) {
	base := fixtureFacts(t)

	unsupported := base
	unsupported.Version = NewVersion(2, 1, 258)
	report, problems := DeriveCapabilities(unsupported, derivableCapabilities())
	for _, c := range execbackend.AllCapabilities() {
		if report.Has(c) {
			t.Errorf("2.1.258 must prove no capability, got %s", c)
		}
	}
	if !hasProblemCode(problems, ProblemVersionUnsupported) {
		t.Fatalf("want version_unsupported, got %v", problems)
	}
	if len(problems) != 1 {
		t.Fatalf("version gate must short-circuit: %v", problems)
	}

	boundary := base
	boundary.Version = NewVersion(2, 1, 259)
	report, problems = DeriveCapabilities(boundary, derivableCapabilities())
	if len(problems) != 0 {
		t.Fatalf("2.1.259 is the inclusive minimum, no problems expected: %v", problems)
	}
	if !report.Has(execbackend.CapabilityApprovalHook) {
		t.Fatal("2.1.259 must still prove approval_hook")
	}

	newer := base
	newer.Version = NewVersion(2, 2, 0)
	report, _ = DeriveCapabilities(newer, derivableCapabilities())
	if !report.Has(execbackend.CapabilityNonInteractive) {
		t.Fatal("2.2.0 must be supported")
	}
	if report.Version != "2.2.0" {
		t.Fatalf("Version = %q, want 2.2.0", report.Version)
	}
}

func TestDeriveCapabilitiesVersionUnparsable(t *testing.T) {
	base := fixtureFacts(t)
	for name, facts := range map[string]Facts{
		"zero version": func() Facts { f := base; f.Version = Version{}; return f }(),
		"parse error": func() Facts {
			f := base
			f.Version = Version{}
			f.VersionErr = ErrVersionUnparsable
			return f
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			report, problems := DeriveCapabilities(facts, derivableCapabilities())
			for _, c := range execbackend.AllCapabilities() {
				if report.Has(c) {
					t.Errorf("unparsable version must prove nothing, got %s", c)
				}
			}
			if !hasProblemCode(problems, ProblemVersionUnparsable) {
				t.Fatalf("want version_unparsable, got %v", problems)
			}
			if report.Version != "" {
				t.Fatalf("Version = %q, want empty for unparsable output", report.Version)
			}
		})
	}
}

func TestDeriveCapabilitiesHelpUnparsable(t *testing.T) {
	base := fixtureFacts(t)
	empty := base
	empty.Flags = HelpFlags{}
	withErr := base
	withErr.Flags = HelpFlags{}
	withErr.HelpErr = ErrHelpUnparsable

	for name, facts := range map[string]Facts{"empty flags": empty, "parse error": withErr} {
		t.Run(name, func(t *testing.T) {
			report, problems := DeriveCapabilities(facts, derivableCapabilities())
			for _, c := range execbackend.AllCapabilities() {
				if report.Has(c) {
					t.Errorf("unparsable help must prove nothing, got %s", c)
				}
			}
			if !hasProblemCode(problems, ProblemHelpUnparsable) {
				t.Fatalf("want help_unparsable, got %v", problems)
			}
			if len(problems) != 1 {
				t.Fatalf("help gate must short-circuit: %v", problems)
			}
		})
	}
}

func TestCheckRequiredRejectsUnprovenCapabilities(t *testing.T) {
	report, _ := DeriveCapabilities(fixtureFacts(t), derivableCapabilities())

	if err := execbackend.CheckRequired(report, derivableCapabilities()); err != nil {
		t.Fatalf("proven capabilities must pass CheckRequired: %v", err)
	}
	err := execbackend.CheckRequired(report, []execbackend.Capability{execbackend.CapabilitySandbox})
	if err == nil {
		t.Fatal("sandbox is never proven in T1.13.a, CheckRequired must fail")
	}
	if !errors.Is(err, execbackend.ErrCapabilityUnavailable) {
		t.Fatalf("want capability_unavailable, got %v", err)
	}
	var domainErr *execbackend.Error
	if !errors.As(err, &domainErr) || domainErr.Code != execbackend.CodeCapabilityUnavailable {
		t.Fatalf("want Code=%s, got %v", execbackend.CodeCapabilityUnavailable, err)
	}
	if len(domainErr.Missing) != 1 || domainErr.Missing[0] != execbackend.CapabilitySandbox {
		t.Fatalf("Missing = %v, want [sandbox]", domainErr.Missing)
	}
}

func TestDeriveCapabilitiesProblemsHaveNoSecrets(t *testing.T) {
	facts := fixtureFacts(t)
	facts.Version = Version{}
	_, problems := DeriveCapabilities(facts, nil)
	if len(problems) == 0 {
		t.Fatal("want at least one problem")
	}
	for _, p := range problems {
		if p.Detail == "" {
			t.Errorf("problem %s must carry a detail", p.Code)
		}
		if strings.ContainsAny(p.Detail, "\n\r") {
			t.Errorf("problem detail must be single-line: %q", p.Detail)
		}
	}
	if codes := problemCodes(problems); len(codes) == 0 {
		t.Fatal("problemCodes must not be empty")
	}
}

// removeHelpEntry 从 help 输出里删掉一个 flag 条目（含其续行），用于派生负例 fixture。
//
// 只匹配 flag 列（行首恰好两个空格后紧跟 '-'）里出现该 flag 名的行：描述里的提及
// （例如 --restricted 的描述里提到 --tools）不算条目。
func removeHelpEntry(t *testing.T, help, flag string) string {
	t.Helper()
	lines := strings.Split(help, "\n")
	var out []string
	skipping := false
	removed := 0
	for _, line := range lines {
		isEntry := isHelpEntryLine(line)
		if skipping {
			if isEntry || isTopLevel(line) {
				skipping = false
			} else {
				continue
			}
		}
		if isEntry {
			if spec, _, ok := splitEntryLine(line); ok && strings.Contains(spec, flag) {
				skipping = true
				removed++
				continue
			}
		}
		out = append(out, line)
	}
	if removed != 1 {
		t.Fatalf("removeHelpEntry(%s) removed %d entries, want 1", flag, removed)
	}
	return strings.Join(out, "\n")
}

// isHelpEntryLine 报告一行是否可能是 flag 条目行（与实现同判据的粗筛）。
func isHelpEntryLine(line string) bool {
	return len(line) >= 3 && line[0] == ' ' && line[1] == ' ' && line[2] == '-'
}

// dropOutputFormatStreamJSON 从 fixture help 里删掉 --output-format 的 stream-json 取值。
//
// 只动 --output-format 那一条：--input-format 的 choices 里也有同名取值，误伤它就会
// 让负例变成"与预期无关的改动"。
func dropOutputFormatStreamJSON(t *testing.T) HelpFlags {
	t.Helper()
	// 归一化换行后再改：干净检出里 fixture 是 CRLF，行尾 \r 会跟着 choices 一起被改，
	// 让负例混进无关差异。
	lines := strings.Split(normalizeNewlines(fixtureHelpText(t)), "\n")

	// 先定位 --output-format 条目的行区间。
	start, end := -1, -1
	for i, line := range lines {
		if !isHelpEntryLine(line) {
			continue
		}
		spec, _, ok := splitEntryLine(line)
		if ok && strings.Contains(spec, "--output-format") {
			start = i
			continue
		}
		if start >= 0 {
			end = i
			break
		}
	}
	if start < 0 {
		t.Fatal("dropOutputFormatStreamJSON: --output-format entry not found")
	}
	if end < 0 {
		end = len(lines)
	}
	// 从条目末尾往前找：描述散文里也提到 stream-json，choices 在最后一行。
	replaced := 0
	for i := end - 1; i > start; i-- {
		if strings.Contains(lines[i], `"stream-json"`) {
			lines[i] = strings.Replace(lines[i], `"stream-json"`, `"json"`, 1)
			replaced++
			break
		}
	}
	if replaced != 1 {
		t.Fatalf("dropOutputFormatStreamJSON replaced %d lines, want 1", replaced)
	}
	flags, err := ParseHelp(strings.Join(lines, "\n"))
	if err != nil {
		t.Fatalf("ParseHelp(without --output-format stream-json): %v", err)
	}
	return flags
}
