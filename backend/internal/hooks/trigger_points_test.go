package hooks

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/run"
)

// ---------------------------------------------------------------------------
// 表本身的不变量
// ---------------------------------------------------------------------------

func TestTriggerPointTableIsWellFormed(t *testing.T) {
	points := TriggerPoints()
	if len(points) == 0 {
		t.Fatal("trigger point table is empty")
	}

	usedCategory := map[TriggerCategory]bool{}
	usedSource := map[ObservationSource]bool{}
	usedPolicy := map[FailurePolicy]bool{}
	usedBehavior := map[CurrentBehavior]bool{}
	existing, pending := 0, 0
	for i, point := range points {
		if err := point.validate(i); err != nil {
			t.Errorf("row %d (%s %s): %v", i, point.Hook, point.Site, err)
		}
		usedCategory[point.Category] = true
		usedSource[point.Source] = true
		usedPolicy[point.Policy] = true
		usedBehavior[point.CurrentBehavior] = true
		switch point.Owner {
		case TriggerPointOwnerExisting:
			existing++
		case TriggerPointOwnerT107B:
			pending++
		}
		if !point.Retained {
			t.Errorf("row %d (%s): Retained=false without an argued retirement in this step", i, point.Hook)
		}
	}

	// Validate() on the exported path must agree with the internal index check.
	for i, point := range points {
		var perr *TriggerPointError
		if err := point.Validate(); err != nil && !errors.As(err, &perr) {
			t.Errorf("row %d: Validate() error is not a *TriggerPointError: %v", i, err)
		}
		if err := point.Validate(); err != nil && !errors.Is(err, ErrInvalidTriggerPoint) {
			t.Errorf("row %d: Validate() error does not wrap ErrInvalidTriggerPoint: %v", i, err)
		}
	}

	// Every closed enum value is exercised by the table: a category or source
	// that nothing uses is either a missing trigger point or a dead enum value.
	for _, category := range TriggerCategories {
		if !usedCategory[category] {
			t.Errorf("category %q is declared but used by no trigger point", category)
		}
	}
	for _, source := range ObservationSources {
		if !usedSource[source] {
			t.Errorf("source %q is declared but used by no trigger point", source)
		}
	}
	for _, policy := range FailurePolicies {
		if !usedPolicy[policy] {
			t.Errorf("policy %q is declared but used by no trigger point", policy)
		}
	}
	for _, behavior := range CurrentBehaviors {
		if !usedBehavior[behavior] {
			t.Errorf("behaviour %q is declared but used by no trigger point", behavior)
		}
	}

	if existing == 0 || pending == 0 {
		t.Fatalf("expected both existing and pending rows, got existing=%d pending=%d", existing, pending)
	}
	if got := len(PendingTriggerPoints()); got != pending {
		t.Errorf("PendingTriggerPoints() = %d rows, want %d", got, pending)
	}
	if got := len(RetainedTriggerPoints()); got != len(points) {
		t.Errorf("RetainedTriggerPoints() = %d rows, want %d (all rows retained in this step)", got, len(points))
	}

	// Table accessors return copies: mutating the result must not change the
	// frozen table.
	copied := TriggerPoints()
	copied[0].Observation = "mutated"
	copied[0].Hook = HookRunFinish
	if again := TriggerPoints(); again[0].Observation == "mutated" || again[0].Hook != points[0].Hook {
		t.Error("TriggerPoints() returned the table itself, not a copy")
	}

	// Site + Hook is the row identity.
	seen := map[string]bool{}
	for i, point := range points {
		key := point.Site + "\x00" + string(point.Hook)
		if seen[key] {
			t.Errorf("row %d: %s (%q) appears twice: Site+Hook must be unique", i, point.Site, point.Hook)
		}
		seen[key] = true
	}
}

func TestTriggerPointObservationsAreUnique(t *testing.T) {
	points := TriggerPoints()
	byObservation := map[string][]TriggerPoint{}
	for _, point := range points {
		byObservation[point.Observation] = append(byObservation[point.Observation], point)
	}
	for observation, owners := range byObservation {
		if len(owners) > 1 {
			details := make([]string, 0, len(owners))
			for _, owner := range owners {
				details = append(details, fmt.Sprintf("%s@%s[%s]", owner.Hook, owner.Site, owner.Source))
			}
			t.Errorf("observation %q is claimed by %d trigger points: %s", observation, len(owners), strings.Join(details, ", "))
		}
	}

	// Retained points (the ones the 3.0 trigger set keeps) must be unique too.
	retained := map[string]TriggerPoint{}
	for _, point := range RetainedTriggerPoints() {
		if other, ok := retained[point.Observation]; ok {
			t.Errorf("retained points %s@%s and %s@%s observe the same fact %q",
				other.Hook, other.Site, point.Hook, point.Site, point.Observation)
		}
		retained[point.Observation] = point
	}

	// No observation is shared across observation sources: one fact belongs to
	// exactly one link of the chain.
	sourceOf := map[string]ObservationSource{}
	for _, point := range points {
		if prev, ok := sourceOf[point.Observation]; ok && prev != point.Source {
			t.Errorf("observation %q is shared by sources %q and %q", point.Observation, prev, point.Source)
		}
		sourceOf[point.Observation] = point.Source
	}
}

// TestNoDoubleCountingLegacyWriteVsCLITool is the "旧 workspace 与新 CLI 观察不能
// 重复计数" invariant (§15 T1.07, §26 I-27).
//
// A server-side write through workspace.FSService triggers HookBeforeWrite only;
// an external CLI backend's write inside its Run working copy is observed as a
// tool call and triggers HookPreToolUse only. The two are different facts and
// different sources, and the runworkspace materialisation path (which copies the
// baseline into the working copy with its own os.OpenFile loop, copy.go:copyFile)
// never calls FSService.Write, so the two can never fire for one write.
func TestNoDoubleCountingLegacyWriteVsCLITool(t *testing.T) {
	writePoints := TriggerPointsForHook(HookBeforeWrite)
	if len(writePoints) != 1 {
		t.Fatalf("HookBeforeWrite has %d trigger points, want exactly 1", len(writePoints))
	}
	writePoint := writePoints[0]
	if writePoint.Category != TriggerCategoryBeforeWrite || writePoint.Source != ObservationSourceLegacyWorkspace {
		t.Errorf("HookBeforeWrite point = %s/%s, want before_write/legacy_workspace", writePoint.Category, writePoint.Source)
	}
	if writePoint.Owner != TriggerPointOwnerExisting || writePoint.Policy != FailurePolicyReject || writePoint.CurrentBehavior != BehaviorRejected {
		t.Errorf("HookBeforeWrite point = owner %s policy %s behaviour %s, want existing/reject/rejected",
			writePoint.Owner, writePoint.Policy, writePoint.CurrentBehavior)
	}

	prePoints := TriggerPointsForHook(HookPreToolUse)
	if len(prePoints) != 1 {
		t.Fatalf("HookPreToolUse has %d trigger points, want exactly 1", len(prePoints))
	}
	prePoint := prePoints[0]
	if prePoint.Category != TriggerCategoryToolPre || prePoint.Source != ObservationSourceCLIBackend {
		t.Errorf("HookPreToolUse point = %s/%s, want tool_pre/cli_backend", prePoint.Category, prePoint.Source)
	}
	if prePoint.Owner != TriggerPointOwnerT107B || prePoint.Policy != FailurePolicyReject {
		t.Errorf("HookPreToolUse point = owner %s policy %s, want T1.07.b/reject", prePoint.Owner, prePoint.Policy)
	}
	if prePoint.Observation == writePoint.Observation {
		t.Errorf("the two write paths share the observation %q: that is double counting", writePoint.Observation)
	}
	if prePoint.Source == writePoint.Source {
		t.Errorf("both write paths claim source %q", writePoint.Source)
	}

	// The CLI's working copy is materialised by runworkspace, not by FSService:
	// no trigger point may point at that package.
	for _, point := range TriggerPoints() {
		if strings.Contains(point.Site, "runworkspace") {
			t.Errorf("trigger point %s@%s claims the run workspace materialisation path; it must be observed as cli_tool_call",
				point.Hook, point.Site)
		}
	}

	// Only one point observes each of these two facts, and the post point is a
	// different fact again.
	if got := TriggerPointsForHook(HookPostToolUse); len(got) != 1 || got[0].Observation == prePoint.Observation {
		t.Errorf("HookPostToolUse must observe a distinct fact from HookPreToolUse: %+v", got)
	}
}

func TestBehaviorGapsAreExplicit(t *testing.T) {
	// The gap list is derived from the table, never hand-written...
	gaps := BehaviorGaps()
	derived := map[string]BehaviorGap{}
	for _, gap := range gaps {
		key := gap.Site + "\x00" + string(gap.Hook)
		if _, dup := derived[key]; dup {
			t.Errorf("gap for %s (%q) appears twice", gap.Site, gap.Hook)
		}
		derived[key] = gap
		if gap.Owner != TriggerPointOwnerT107B {
			t.Errorf("gap %s (%q) has owner %q, want %q", gap.Site, gap.Hook, gap.Owner, TriggerPointOwnerT107B)
		}
		if strings.TrimSpace(gap.Closure) == "" {
			t.Errorf("gap %s (%q) does not say what closes it", gap.Site, gap.Hook)
		}
		if gap.Policy.CompatibleWith(gap.CurrentBehavior) {
			t.Errorf("gap %s (%q) is not actually a gap: policy %s vs behaviour %s", gap.Site, gap.Hook, gap.Policy, gap.CurrentBehavior)
		}
	}

	// ...and it must equal this fixed list. Editing a policy so that it no longer
	// matches the code shows up here instead of being silently accepted.
	want := []BehaviorGap{
		{
			Hook:            HookPostResponse,
			Site:            "internal/adapters/message_conversion.go:notifyAdapterPostResponse",
			Policy:          FailurePolicyWarn,
			CurrentBehavior: BehaviorReturnedToCaller,
			Owner:           TriggerPointOwnerT107B,
		},
		{
			Hook:            HookOnStream,
			Site:            "internal/adapters/message_conversion.go:notifyAdapterStreamChunk",
			Policy:          FailurePolicyWarn,
			CurrentBehavior: BehaviorDiscarded,
			Owner:           TriggerPointOwnerT107B,
		},
		{
			Hook:            HookBeforeTaskExecute,
			Site:            "internal/planner/memory_integration.go:emitGlobalTaskHook",
			Policy:          FailurePolicyWarn,
			CurrentBehavior: BehaviorReturnedToCaller,
			Owner:           TriggerPointOwnerT107B,
		},
		{
			Hook:            HookAfterTaskExecute,
			Site:            "internal/planner/memory_integration.go:emitGlobalTaskHook",
			Policy:          FailurePolicyRetry,
			CurrentBehavior: BehaviorReturnedToCaller,
			Owner:           TriggerPointOwnerT107B,
		},
		{
			Hook:            HookOnTaskFailure,
			Site:            "internal/planner/memory_integration.go:emitGlobalTaskHook",
			Policy:          FailurePolicyRetry,
			CurrentBehavior: BehaviorReturnedToCaller,
			Owner:           TriggerPointOwnerT107B,
		},
		{
			Hook:            HookOnTaskComplete,
			Site:            "internal/planner/memory_integration.go:emitGlobalTaskHook",
			Policy:          FailurePolicyRetry,
			CurrentBehavior: BehaviorReturnedToCaller,
			Owner:           TriggerPointOwnerT107B,
		},
	}
	if len(gaps) != len(want) {
		t.Fatalf("BehaviorGaps() has %d entries, want %d:\n%+v", len(gaps), len(want), gaps)
	}
	for _, expected := range want {
		key := expected.Site + "\x00" + string(expected.Hook)
		got, ok := derived[key]
		if !ok {
			t.Errorf("missing expected gap %s (%q)", expected.Site, expected.Hook)
			continue
		}
		if got.Policy != expected.Policy || got.CurrentBehavior != expected.CurrentBehavior {
			t.Errorf("gap %s (%q) = policy %s / behaviour %s, want %s / %s",
				expected.Site, expected.Hook, got.Policy, got.CurrentBehavior, expected.Policy, expected.CurrentBehavior)
		}
	}
}

// TestFailurePolicyCompatibility pins the semantics of CompatibleWith: which
// declared policy each observed behaviour can satisfy.
func TestFailurePolicyCompatibility(t *testing.T) {
	cases := []struct {
		policy     FailurePolicy
		behavior   CurrentBehavior
		compatible bool
	}{
		{FailurePolicyReject, BehaviorRejected, true},
		{FailurePolicyReject, BehaviorLoggedWarn, false},
		{FailurePolicyReject, BehaviorDiscarded, false},
		{FailurePolicyReject, BehaviorReturnedToCaller, false},
		{FailurePolicyWarn, BehaviorLoggedWarn, true},
		{FailurePolicyWarn, BehaviorRejected, false},
		{FailurePolicyWarn, BehaviorDiscarded, false},
		{FailurePolicyWarn, BehaviorReturnedToCaller, false},
		{FailurePolicyRetry, BehaviorLoggedWarn, true},
		{FailurePolicyRetry, BehaviorRejected, false},
		{FailurePolicyRetry, BehaviorDiscarded, false},
		{FailurePolicyRetry, BehaviorReturnedToCaller, false},
	}
	for _, tc := range cases {
		if got := tc.policy.CompatibleWith(tc.behavior); got != tc.compatible {
			t.Errorf("%s.CompatibleWith(%s) = %v, want %v", tc.policy, tc.behavior, got, tc.compatible)
		}
	}

	// The consequence axis: reject blocks the guarded operation, warn and retry
	// continue it (retry only re-runs the handler).
	for _, policy := range []FailurePolicy{FailurePolicyWarn, FailurePolicyRetry} {
		if got := policy.Consequence(); got != ConsequenceContinue {
			t.Errorf("%s.Consequence() = %s, want continue", policy, got)
		}
	}
	if got := FailurePolicyReject.Consequence(); got != ConsequenceBlock {
		t.Errorf("reject.Consequence() = %s, want block", got)
	}
	if got := BehaviorRejected.Consequence(); got != ConsequenceBlock {
		t.Errorf("rejected.Consequence() = %s, want block", got)
	}
}

// TestAllHookTypesMatchTypesGo parses the HookType constants out of types.go and
// requires them to equal AllHookTypes() one for one, in declaration order. That
// is what makes "the four reserved types exist and nothing else changed" a
// machine-checked fact instead of a claim.
func TestAllHookTypesMatchTypesGo(t *testing.T) {
	root := findBackendRoot(t)
	declared, err := parseHookTypeConstants(filepath.Join(root, "internal", "hooks", "types.go"))
	if err != nil {
		t.Fatalf("parse types.go: %v", err)
	}
	if len(declared) == 0 {
		t.Fatal("no HookType constants found in types.go")
	}

	declaredValues := make([]HookType, 0, len(declared))
	for _, constant := range declared {
		declaredValues = append(declaredValues, constant.value)
	}
	all := AllHookTypes()
	if len(all) != len(declaredValues) {
		t.Fatalf("AllHookTypes() has %d entries, types.go declares %d:\ngot  %v\nwant %v", len(all), len(declaredValues), all, declaredValues)
	}
	for i := range all {
		if all[i] != declaredValues[i] {
			t.Errorf("AllHookTypes()[%d] = %q, types.go declares %q (order matters)", i, all[i], declaredValues[i])
		}
	}

	// Every declared type is reachable from at least one trigger point.
	withPoint := map[HookType]bool{}
	for _, hook := range HookTypesWithTriggerPoint() {
		withPoint[hook] = true
	}
	for _, hook := range all {
		if !withPoint[hook] {
			t.Errorf("HookType %q appears in no trigger point: a hook nobody triggers is not a contract", hook)
		}
	}
	if got := len(HookTypesWithTriggerPoint()); got != len(all) {
		t.Errorf("HookTypesWithTriggerPoint() = %d types, want %d", got, len(all))
	}

	// The four reserved types exist and are owned by T1.07.b.
	reserved := []HookType{HookPreToolUse, HookPostToolUse, HookRunStart, HookRunFinish}
	for _, hook := range reserved {
		if !hook.Valid() {
			t.Errorf("reserved hook %q is not a declared HookType", hook)
		}
		rows := TriggerPointsForHook(hook)
		if len(rows) != 1 {
			t.Errorf("reserved hook %q has %d trigger points, want 1", hook, len(rows))
			continue
		}
		if rows[0].Owner != TriggerPointOwnerT107B {
			t.Errorf("reserved hook %q has owner %q, want %q", hook, rows[0].Owner, TriggerPointOwnerT107B)
		}
	}
	if HookType("hook_not_a_real_type").Valid() {
		t.Error("HookType.Valid accepted an undeclared type")
	}
}

// TestReservedHookTypesHaveNoCallSite proves the wiring has not started: no
// production call site triggers a reserved type, and the sites its rows name do
// not exist yet.
//
// When T1.07.b wires them, this test fails on purpose: the rows must be re-pointed
// to their real Sites and their Owner must change in the same step.
func TestReservedHookTypesHaveNoCallSite(t *testing.T) {
	root := findBackendRoot(t)
	report, err := scanTriggerCalls(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	reserved := map[HookType]bool{
		HookPreToolUse:  true,
		HookPostToolUse: true,
		HookRunStart:    true,
		HookRunFinish:   true,
	}
	for _, call := range report.Calls {
		if reserved[call.Hook] {
			t.Errorf("reserved hook %q is already triggered at %s:%d (%s); its wiring belongs to T1.07.b", call.Hook, call.File, call.Line, call.Site)
		}
	}
	for _, point := range PendingTriggerPoints() {
		if _, isFile := SiteFile(point.Site); isFile {
			t.Errorf("pending row %s (%q) names a real file site", point.Site, point.Hook)
			continue
		}
		rel := strings.TrimSuffix(point.Site, ":<tbd>")
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			t.Errorf("pending row %s (%q) points at a file that already exists; re-point the row instead of leaving it <tbd>", point.Site, point.Hook)
		}
	}
}

// TestTriggerPointSiteFilesExist checks that every wired row points at a file
// that is really there (a moved file fails loudly instead of rotting).
func TestTriggerPointSiteFilesExist(t *testing.T) {
	root := findBackendRoot(t)
	for _, point := range TriggerPoints() {
		rel, isFile := SiteFile(point.Site)
		if !isFile {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(full)
		if err != nil {
			t.Errorf("row %s (%q): %v", point.Site, point.Hook, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("row %s (%q) points at a directory", point.Site, point.Hook)
		}
	}
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

// findBackendRoot walks up from the test's working directory (the hooks package
// directory) to the backend module root and asserts the go.mod module path.
func findBackendRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(candidate); err == nil {
			if !strings.Contains(string(data), "module github.com/codeflow/backend") {
				t.Fatalf("%s is not the codeflow backend module", candidate)
			}
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find the backend module root (no go.mod above the test directory)")
		}
		dir = parent
	}
}

// hookTypeConstant is one HookType constant parsed out of types.go.
type hookTypeConstant struct {
	name  string
	value HookType
}

// parseHookTypeConstants parses every `Name HookType = "value"` constant of the
// given source file, in declaration order.
func parseHookTypeConstants(path string) ([]hookTypeConstant, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
	if err != nil {
		return nil, err
	}
	var out []hookTypeConstant
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := value.Type.(*ast.Ident)
			if !ok || ident.Name != "HookType" {
				continue
			}
			for i, name := range value.Names {
				if i >= len(value.Values) {
					continue
				}
				lit, ok := value.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(lit.Value)
				if err != nil {
					return nil, fmt.Errorf("%s: constant %s: %w", path, name.Name, err)
				}
				out = append(out, hookTypeConstant{name: name.Name, value: HookType(unquoted)})
			}
		}
	}
	return out, nil
}

// runPackageEventType is a compile-time tie between the payload contract and
// T1.02.a's closed event enum: the payload code names these events, and renaming
// one in internal/run must break this file too.
var _ = []run.ExecutionEventType{
	run.EventToolRequested,
	run.EventSchedulerClaimed,
	run.EventRunCompleted,
	run.EventRunFailed,
}

// ---------------------------------------------------------------------------
// AST 扫描器
// ---------------------------------------------------------------------------

// triggerCallNames is the closed set of manager call names the scanner looks
// for: the three generic entry points plus the seven helper methods of
// IHookManager. A new way to trigger a hook must be added here *and* to the
// table, or the binding test fails.
var triggerCallNames = map[string]bool{
	"Trigger":                  true,
	"TriggerAsync":             true,
	"TriggerHook":              true,
	"HookAfterExec":            true,
	"HookRestoreState":         true,
	"HookOnUserInputSubmitted": true,
	"HookBeforeTaskExecute":    true,
	"HookAfterTaskExecute":     true,
	"HookOnTaskFailure":        true,
	"HookOnTaskComplete":       true,
}

// helperHookOf maps a helper method to the hook type it triggers. The three
// generic entries carry the hook type as an argument instead.
var helperHookOf = map[string]HookType{
	"HookAfterExec":            HookAfterExec,
	"HookRestoreState":         HookRestoreState,
	"HookOnUserInputSubmitted": HookOnUserInputSubmitted,
	"HookBeforeTaskExecute":    HookBeforeTaskExecute,
	"HookAfterTaskExecute":     HookAfterTaskExecute,
	"HookOnTaskFailure":        HookOnTaskFailure,
	"HookOnTaskComplete":       HookOnTaskComplete,
}

// triggerCall is one resolved (Site, Hook) occurrence found in the tree.
type triggerCall struct {
	File string
	Line int
	Site string
	Hook HookType
}

// scanProblem is one thing the scanner could not resolve. A non-empty problem
// list fails the binding test: the scanner must never silently drop a call.
type scanProblem struct {
	File   string
	Line   int
	Reason string
}

// Error implements error.
func (p scanProblem) Error() string {
	return fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Reason)
}

// scanReport is what one scan of a tree produced.
type scanReport struct {
	// Calls are the resolved occurrences, in file order.
	Calls []triggerCall
	// Problems are the unresolvable occurrences: an unknown receiver, a
	// forwarding function whose hook argument cannot be attributed, a trigger
	// method used as a value instead of being called.
	Problems []scanProblem
}

// scanTriggerCalls walks root for Go files that import the hooks package, parses
// them and returns every hook trigger call with the code site it belongs to.
//
// Skipped: _test.go files, testdata, vendor, dot-directories and the hooks
// package itself (that is where the table lives). The scanner deliberately
// reports a problem instead of guessing whenever it cannot prove that a
// trigger-shaped call is not a hooks manager call — samg's same-named
// (*SAMGService).HookOnMessageComplete is ignored only because its receiver
// resolves to a non-hooks type.
func scanTriggerCalls(root string) (scanReport, error) {
	var report scanReport

	files, err := collectHookImportingFiles(root)
	if err != nil {
		return report, err
	}

	constants, err := parseHookTypeConstants(filepath.Join(root, "internal", "hooks", "types.go"))
	if err != nil {
		return report, fmt.Errorf("scan %s: %w", root, err)
	}
	constantValues := map[string]HookType{}
	for _, constant := range constants {
		constantValues[constant.name] = constant.value
	}

	byDir := map[string][]*fileInfo{}
	order := []string{}

	for _, rel := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		info, err := parseTriggerFile(rel, full, constantValues)
		if err != nil {
			return report, err
		}
		if info == nil {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		if _, seen := byDir[dir]; !seen {
			order = append(order, dir)
		}
		byDir[dir] = append(byDir[dir], info)
	}

	for _, dir := range order {
		resolveForwardedCalls(byDir[dir], &report)
	}
	for _, dir := range order {
		for _, info := range byDir[dir] {
			report.Calls = append(report.Calls, info.direct...)
			report.Problems = append(report.Problems, info.problems...)
		}
	}
	sort.SliceStable(report.Calls, func(i, j int) bool {
		if report.Calls[i].File != report.Calls[j].File {
			return report.Calls[i].File < report.Calls[j].File
		}
		return report.Calls[i].Line < report.Calls[j].Line
	})
	sort.SliceStable(report.Problems, func(i, j int) bool {
		if report.Problems[i].File != report.Problems[j].File {
			return report.Problems[i].File < report.Problems[j].File
		}
		return report.Problems[i].Line < report.Problems[j].Line
	})
	return report, nil
}

// collectHookImportingFiles returns the slash-separated paths (relative to root)
// of the Go files that may trigger a hook.
func collectHookImportingFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if name == "testdata" || name == "vendor" || name == "node_modules" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			if rel, relErr := filepath.Rel(root, path); relErr == nil && filepath.ToSlash(rel) == "internal/hooks" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !bytes.Contains(data, []byte(`"github.com/codeflow/backend/internal/hooks"`)) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// funcInfo is one function or method of a scanned file.
type funcInfo struct {
	site         string
	file         string
	line         int
	plainName    string
	isMethod     bool
	recvName     string
	recvType     string
	params       []string
	paramIndex   map[string]int
	decl         *ast.FuncDecl
	direct       []triggerCall
	problems     []scanProblem
	indirect     []forwardedCall
	managerNames map[string]bool
}

// forwardedCall is a manager call whose hook type is a variable: it has to be
// attributed to the callers of the function that holds the variable.
type forwardedCall struct {
	file       string
	line       int
	forwarder  string
	paramIndex int
	paramName  string
}

// fileInfo is one parsed file.
type fileInfo struct {
	path     string
	hooksPkg string
	funcs    []*funcInfo
	direct   []triggerCall
	problems []scanProblem
}

// parseTriggerFile parses one file and extracts its trigger calls, its manager
// local variables and its forwarding functions.
func parseTriggerFile(rel, full string, constants map[string]HookType) (*fileInfo, error) {
	src, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, full, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}

	info := &fileInfo{path: rel}
	for _, spec := range file.Imports {
		path, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil || path != "github.com/codeflow/backend/internal/hooks" {
			continue
		}
		if spec.Name != nil {
			info.hooksPkg = spec.Name.Name
		} else {
			info.hooksPkg = "hooks"
		}
	}
	if info.hooksPkg == "" {
		return nil, nil
	}

	position := func(node ast.Node) int {
		return fset.Position(node.Pos()).Line
	}

	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Body == nil {
			continue
		}
		fi := newFuncInfo(rel, funcDecl, position(funcDecl))
		collectManagerLocals(funcDecl, fi, info.hooksPkg)
		scanFuncBody(funcDecl, fi, info.hooksPkg, constants, position)
		info.funcs = append(info.funcs, fi)
		info.direct = append(info.direct, fi.direct...)
		info.problems = append(info.problems, fi.problems...)
	}
	return info, nil
}

func newFuncInfo(file string, decl *ast.FuncDecl, line int) *funcInfo {
	fi := &funcInfo{
		file:         file,
		line:         line,
		decl:         decl,
		managerNames: map[string]bool{},
		paramIndex:   map[string]int{},
	}
	name := decl.Name.Name
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		fi.isMethod = true
		recv := decl.Recv.List[0]
		if len(recv.Names) > 0 {
			fi.recvName = recv.Names[0].Name
		}
		fi.recvType = exprString(recv.Type)
		name = "(" + fi.recvType + ")." + decl.Name.Name
	}
	fi.plainName = decl.Name.Name
	fi.site = file + ":" + name
	if decl.Type.Params != nil {
		index := 0
		for _, field := range decl.Type.Params.List {
			for _, paramName := range field.Names {
				fi.params = append(fi.params, paramName.Name)
				fi.paramIndex[paramName.Name] = index
				index++
			}
			if len(field.Names) == 0 {
				// Unnamed parameter (e.g. an interface method signature).
				fi.params = append(fi.params, "")
				index++
			}
		}
	}
	return fi
}

// collectManagerLocals records the local variables that hold the hooks manager,
// e.g. `mgr := backendhooks.GetHookManager()` or
// `var hookMgr hooks.IHookManager = hooks.GetHookManager()`.
func collectManagerLocals(decl *ast.FuncDecl, fi *funcInfo, hooksPkg string) {
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			for i, rhs := range stmt.Rhs {
				if i >= len(stmt.Lhs) || !isGetHookManagerCall(rhs, hooksPkg) {
					continue
				}
				if ident, ok := stmt.Lhs[i].(*ast.Ident); ok {
					fi.managerNames[ident.Name] = true
				}
			}
		case *ast.ValueSpec:
			for i, value := range stmt.Values {
				if i >= len(stmt.Names) || !isGetHookManagerCall(value, hooksPkg) {
					continue
				}
				fi.managerNames[stmt.Names[i].Name] = true
			}
		}
		return true
	})
}

// scanFuncBody records every trigger call of one function body.
func scanFuncBody(decl *ast.FuncDecl, fi *funcInfo, hooksPkg string, constants map[string]HookType, position func(ast.Node) int) {
	// First pass: which selector expressions are actually called? A trigger
	// method that appears anywhere else (a method value) escapes the scan.
	called := map[ast.Node]bool{}
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && triggerCallNames[selector.Sel.Name] {
			called[selector] = true
		}
		return true
	})

	ast.Inspect(decl.Body, func(node ast.Node) bool {
		switch expr := node.(type) {
		case *ast.SelectorExpr:
			if !triggerCallNames[expr.Sel.Name] || called[expr] {
				return true
			}
			// <hooksPkg>.HookAfterExec is a constant of the hooks package (the
			// runtime allowlist lists them, and a hook argument is exactly that),
			// not a method value on a receiver.
			if qualifier, ok := expr.X.(*ast.Ident); ok && qualifier.Name == hooksPkg {
				return true
			}
			fi.problems = append(fi.problems, scanProblem{
				File:   fi.file,
				Line:   position(expr),
				Reason: fmt.Sprintf("%s.%s is used as a value, not called: the scanner cannot see the call it makes", exprString(expr.X), expr.Sel.Name),
			})
			return true
		case *ast.CallExpr:
			selector, ok := expr.Fun.(*ast.SelectorExpr)
			if !ok || !triggerCallNames[selector.Sel.Name] {
				return true
			}
			fi.recordTriggerCall(expr, selector, hooksPkg, constants, position)
			return true
		}
		return true
	})
}

// recordTriggerCall resolves one trigger-shaped call.
func (fi *funcInfo) recordTriggerCall(call *ast.CallExpr, selector *ast.SelectorExpr, hooksPkg string, constants map[string]HookType, position func(ast.Node) int) {
	isManager, resolved := fi.resolveReceiver(selector.X, hooksPkg)
	if !resolved {
		fi.problems = append(fi.problems, scanProblem{
			File: fi.file,
			Line: position(call),
			Reason: fmt.Sprintf("cannot resolve the receiver of %s.%s: the scanner must not guess whether it is the hooks manager",
				exprString(selector.X), selector.Sel.Name),
		})
		return
	}
	if !isManager {
		return
	}

	name := selector.Sel.Name
	if hook, ok := helperHookOf[name]; ok {
		fi.direct = append(fi.direct, triggerCall{File: fi.file, Line: position(call), Site: fi.site, Hook: hook})
		return
	}

	// Trigger / TriggerAsync carry the hook type as their second argument;
	// TriggerHook takes a hook *name* and is therefore recorded with an empty
	// hook type (the manual by-name trigger point).
	if name == "TriggerHook" {
		fi.direct = append(fi.direct, triggerCall{File: fi.file, Line: position(call), Site: fi.site, Hook: ""})
		return
	}
	if len(call.Args) < 2 {
		fi.problems = append(fi.problems, scanProblem{
			File:   fi.file,
			Line:   position(call),
			Reason: fmt.Sprintf("%s is called with %d arguments: the hook type argument is missing", name, len(call.Args)),
		})
		return
	}
	arg := call.Args[1]
	if hook, ok := resolveHookConstant(arg, hooksPkg, constants); ok {
		fi.direct = append(fi.direct, triggerCall{File: fi.file, Line: position(call), Site: fi.site, Hook: hook})
		return
	}
	if ident, ok := arg.(*ast.Ident); ok {
		if index, isParam := fi.paramIndex[ident.Name]; isParam {
			fi.indirect = append(fi.indirect, forwardedCall{
				file:       fi.file,
				line:       position(call),
				forwarder:  fi.plainName,
				paramIndex: index,
				paramName:  ident.Name,
			})
			return
		}
	}
	fi.problems = append(fi.problems, scanProblem{
		File: fi.file,
		Line: position(call),
		Reason: fmt.Sprintf("%s is called with a hook type the scanner cannot resolve (%s): it must be a hooks.HookX constant or a parameter of a forwarding function",
			name, exprString(arg)),
	})
}

// resolveReceiver reports whether the receiver expression is the hooks manager.
// resolved=false means the scanner could not prove either way, which is a
// problem rather than a silent skip.
func (fi *funcInfo) resolveReceiver(recv ast.Expr, hooksPkg string) (isManager bool, resolved bool) {
	switch expr := recv.(type) {
	case *ast.CallExpr:
		selector, ok := expr.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "GetHookManager" {
			return false, false
		}
		if qualifier, ok := selector.X.(*ast.Ident); ok && qualifier.Name == hooksPkg {
			return true, true
		}
		return false, false
	case *ast.Ident:
		if fi.managerNames[expr.Name] {
			return true, true
		}
		if fi.recvName != "" && expr.Name == fi.recvName {
			// A method receiver of a locally declared type is provably not the
			// hooks manager; a receiver whose type lives in the hooks package is.
			if qualifier, ok := receiverQualifier(fi.recvType); ok && qualifier == hooksPkg {
				return true, true
			}
			return false, true
		}
		return false, false
	case *ast.SelectorExpr:
		return false, false
	default:
		return false, false
	}
}

// resolveForwardedCalls attributes every indirect call in one package directory
// to the constant hook types its callers pass.
func resolveForwardedCalls(files []*fileInfo, report *scanReport) {
	type decl struct {
		fi    *funcInfo
		calls []forwardedCall
	}
	forwarders := map[string][]*funcInfo{}
	for _, file := range files {
		for _, fi := range file.funcs {
			if !fi.isMethod {
				forwarders[fi.plainName] = append(forwarders[fi.plainName], fi)
			}
		}
	}

	for _, file := range files {
		for _, fi := range file.funcs {
			for _, indirect := range fi.indirect {
				candidates := forwarders[indirect.forwarder]
				if len(candidates) == 0 {
					report.Problems = append(report.Problems, scanProblem{
						File:   indirect.file,
						Line:   indirect.line,
						Reason: fmt.Sprintf("hook type argument %q comes from a method parameter: only a package-level forwarding function can be followed", indirect.paramName),
					})
					continue
				}
				attributed := 0
				for _, callerFile := range files {
					for _, caller := range callerFile.funcs {
						for _, call := range forwardedCallSites(caller, indirect.forwarder) {
							if indirect.paramIndex >= len(call.Args) {
								report.Problems = append(report.Problems, scanProblem{
									File:   caller.file,
									Line:   caller.line,
									Reason: fmt.Sprintf("call to forwarding function %s passes %d arguments, fewer than the hook type position %d", indirect.forwarder, len(call.Args), indirect.paramIndex),
								})
								continue
							}
							arg := call.Args[indirect.paramIndex]
							hook, ok := resolveHookConstant(arg, callerFile.hooksPkg, nil)
							if !ok {
								report.Problems = append(report.Problems, scanProblem{
									File:   caller.file,
									Line:   caller.line,
									Reason: fmt.Sprintf("forwarding function %s is called with a non-constant hook type (%s): the trigger cannot be attributed to a hook", indirect.forwarder, exprString(arg)),
								})
								continue
							}
							report.Calls = append(report.Calls, triggerCall{File: caller.file, Line: caller.line, Site: caller.site, Hook: hook})
							attributed++
						}
					}
				}
				if attributed == 0 && len(candidates) > 0 {
					// No call site at all: the forwarder is dead code for the
					// scanner's purposes, which is itself a problem.
					dead := true
					for _, callerFile := range files {
						for _, caller := range callerFile.funcs {
							if len(forwardedCallSites(caller, indirect.forwarder)) > 0 {
								dead = false
							}
						}
					}
					if dead {
						report.Problems = append(report.Problems, scanProblem{
							File:   indirect.file,
							Line:   indirect.line,
							Reason: fmt.Sprintf("forwarding function %s has no call site in its package: the hook type cannot be resolved", indirect.forwarder),
						})
					}
				}
			}
		}
	}
}

// forwardedCallSites returns the calls to a package-level function in one
// function body.
func forwardedCallSites(fi *funcInfo, name string) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(fi.decl.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == name {
			out = append(out, call)
		}
		return true
	})
	return out
}

// resolveHookConstant resolves a hook type expression to its constant value. The
// expression must be a selector of the hooks package (hooks.HookX /
// backendhooks.HookX) whose selector is a declared HookType constant.
func resolveHookConstant(expr ast.Expr, hooksPkg string, constants map[string]HookType) (HookType, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok || hooksPkg == "" || qualifier.Name != hooksPkg {
		return "", false
	}
	if constants == nil {
		// The caller passes nil while resolving forwarded calls: fall back to
		// the declared table, which is the same closed set the scanner parsed.
		for _, hook := range AllHookTypes() {
			if constantNameOf(hook) == selector.Sel.Name {
				return hook, true
			}
		}
		return "", false
	}
	hook, ok := constants[selector.Sel.Name]
	return hook, ok
}

// constantNameOf returns the Go constant name of a HookType, e.g. "HookBeforeSend".
func constantNameOf(hook HookType) string {
	name := string(hook)
	name = strings.TrimPrefix(name, "hook_")
	var out strings.Builder
	for _, part := range strings.Split(name, "_") {
		if part == "" {
			continue
		}
		out.WriteString(strings.ToUpper(part[:1]))
		out.WriteString(part[1:])
	}
	return "Hook" + out.String()
}

// receiverQualifier returns the package qualifier of a receiver type like
// "*hooks.HookManager", if it has one.
func receiverQualifier(recvType string) (string, bool) {
	trimmed := strings.TrimPrefix(recvType, "*")
	dot := strings.Index(trimmed, ".")
	if dot < 0 {
		return "", false
	}
	return trimmed[:dot], true
}

// isGetHookManagerCall reports whether the expression is a call to
// <hooksPkg>.GetHookManager().
func isGetHookManagerCall(expr ast.Expr, hooksPkg string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "GetHookManager" {
		return false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	return ok && qualifier.Name == hooksPkg
}

// exprString renders an expression for a diagnostic message (never evaluated).
func exprString(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return exprString(value.X) + "." + value.Sel.Name
	case *ast.CallExpr:
		return exprString(value.Fun) + "(...)"
	case *ast.StarExpr:
		return "*" + exprString(value.X)
	case *ast.IndexExpr:
		return exprString(value.X) + "[...]"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// ---------------------------------------------------------------------------
// 表 ↔ 代码 绑定
// ---------------------------------------------------------------------------

// compareTriggerTable matches one table against one scan. It returns the calls
// the table does not know about, the existing rows with no call site, and the
// scanner problems. This is the function the fixtures exercise directly.
func compareTriggerTable(table []TriggerPoint, report scanReport) (unregistered []triggerCall, missing []TriggerPoint, problems []scanProblem) {
	problems = report.Problems
	seen := map[string]int{}
	for _, call := range report.Calls {
		key := call.Site + "\x00" + string(call.Hook)
		seen[key]++
		point, ok := findTriggerPoint(table, call.Site, call.Hook)
		if !ok {
			unregistered = append(unregistered, call)
			continue
		}
		if point.Owner != TriggerPointOwnerExisting {
			unregistered = append(unregistered, call)
		}
	}
	for _, point := range table {
		if point.Owner != TriggerPointOwnerExisting {
			continue
		}
		key := point.Site + "\x00" + string(point.Hook)
		if seen[key] == 0 {
			missing = append(missing, point)
		}
	}
	return unregistered, missing, problems
}

// findTriggerPoint looks up one row in an arbitrary table slice.
func findTriggerPoint(table []TriggerPoint, site string, hook HookType) (TriggerPoint, bool) {
	for _, point := range table {
		if point.Site == site && point.Hook == hook {
			return point, true
		}
	}
	return TriggerPoint{}, false
}

// TestTriggerTableIsBoundToCode is the heart of T1.07.a: the table and the real
// tree must agree in both directions.
func TestTriggerTableIsBoundToCode(t *testing.T) {
	root := findBackendRoot(t)
	report, err := scanTriggerCalls(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	if len(report.Problems) > 0 {
		for _, problem := range report.Problems {
			t.Errorf("scanner problem: %v", problem)
		}
		t.Fatalf("the scanner could not resolve %d call(s); a trigger call must never be silently skipped", len(report.Problems))
	}

	table := TriggerPoints()
	unregistered, missing, _ := compareTriggerTable(table, report)
	for _, call := range unregistered {
		t.Errorf("unregistered trigger call at %s:%d: %s (%q) is not in the table (or is not owned by %q)",
			call.File, call.Line, call.Site, call.Hook, TriggerPointOwnerExisting)
	}
	for _, point := range missing {
		t.Errorf("table row %s (%q) has no call site in the tree: the code that triggered it is gone", point.Site, point.Hook)
	}

	// Exactly one occurrence per existing row.
	counts := map[string]int{}
	for _, call := range report.Calls {
		counts[call.Site+"\x00"+string(call.Hook)]++
	}
	existing := 0
	for _, point := range table {
		if point.Owner != TriggerPointOwnerExisting {
			continue
		}
		existing++
		if got := counts[point.Site+"\x00"+string(point.Hook)]; got != 1 {
			t.Errorf("table row %s (%q) is triggered %d times, want exactly 1", point.Site, point.Hook, got)
		}
	}
	if existing != len(report.Calls) {
		t.Errorf("the tree has %d trigger calls but the table has %d existing rows", len(report.Calls), existing)
	}

	// samg's same-named method is not a hooks manager call and must not be
	// counted: no occurrence and no row may point at that package.
	for _, call := range report.Calls {
		if strings.Contains(call.Site, "samg") {
			t.Errorf("(*SAMGService).HookOnMessageComplete was counted as a hooks manager call: %s:%d", call.File, call.Line)
		}
	}
	for _, point := range table {
		if strings.Contains(point.Site, "samg") {
			t.Errorf("row %s (%q) points at samg, whose same-named method is not a hooks trigger", point.Site, point.Hook)
		}
	}
}

// ---------------------------------------------------------------------------
// 扫描器的变异证明：合成 fixture 树
// ---------------------------------------------------------------------------

// fixtureTable is the tiny table the fixture trees are written against.
func fixtureTable() []TriggerPoint {
	return []TriggerPoint{
		{
			Hook:            HookBeforeWrite,
			Category:        TriggerCategoryBeforeWrite,
			Source:          ObservationSourceLegacyWorkspace,
			Observation:     "fixture_write",
			Site:            "internal/aaa/writer.go:Write",
			Policy:          FailurePolicyReject,
			CurrentBehavior: BehaviorRejected,
			Retained:        true,
			Owner:           TriggerPointOwnerExisting,
			Note:            "fixture",
		},
		{
			Hook:            HookBeforeSend,
			Category:        TriggerCategoryModelIO,
			Source:          ObservationSourceLegacySession,
			Observation:     "fixture_send",
			Site:            "internal/aaa/forward.go:CallBeforeSend",
			Policy:          FailurePolicyReject,
			CurrentBehavior: BehaviorRejected,
			Retained:        true,
			Owner:           TriggerPointOwnerExisting,
			Note:            "fixture",
		},
	}
}

// writeFixtureTree writes a small tree that imports the hooks package. Each knob
// introduces exactly one defect the scanner must report.
type fixtureKnobs struct {
	extraUnregisteredCall bool
	dropWriteCall         bool
	badReceiver           bool
	variableForwarderArg  bool
}

func writeFixtureTree(t *testing.T, knobs fixtureKnobs) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("internal/hooks/types.go", "package hooks\n\n"+
		"type HookType string\n\n"+
		"const (\n"+
		"\tHookBeforeWrite HookType = \"hook_before_write\"\n"+
		"\tHookBeforeSend  HookType = \"hook_before_send\"\n"+
		"\tHookAfterExec   HookType = \"hook_after_exec\"\n"+
		")\n\n"+
		"type IHookManager interface {\n"+
		"\tTrigger(ctx any, hookType HookType, payload any) error\n"+
		"}\n\n"+
		"func GetHookManager() IHookManager { return nil }\n")

	writer := "package aaa\n\n" +
		"import backendhooks \"github.com/codeflow/backend/internal/hooks\"\n\n" +
		"func Write(payload any) error {\n"
	if !knobs.dropWriteCall {
		writer += "\treturn backendhooks.GetHookManager().Trigger(nil, backendhooks.HookBeforeWrite, payload)\n"
	}
	writer += "}\n"
	if knobs.extraUnregisteredCall {
		writer += "\nfunc Sneaky(payload any) error {\n" +
			"\treturn backendhooks.GetHookManager().Trigger(nil, backendhooks.HookAfterExec, payload)\n" +
			"}\n"
	}
	write("internal/aaa/writer.go", writer)

	forward := "package aaa\n\n" +
		"import backendhooks \"github.com/codeflow/backend/internal/hooks\"\n\n" +
		"func forwardHook(hookType backendhooks.HookType, payload any) error {\n" +
		"\treturn backendhooks.GetHookManager().Trigger(nil, hookType, payload)\n" +
		"}\n\n" +
		"func CallBeforeSend(payload any) error {\n"
	if knobs.variableForwarderArg {
		forward += "\thookType := backendhooks.HookBeforeSend\n" +
			"\treturn forwardHook(hookType, payload)\n"
	} else {
		forward += "\treturn forwardHook(backendhooks.HookBeforeSend, payload)\n"
	}
	forward += "}\n"
	write("internal/aaa/forward.go", forward)

	if knobs.badReceiver {
		write("internal/aaa/bad.go", "package aaa\n\n"+
			"import backendhooks \"github.com/codeflow/backend/internal/hooks\"\n\n"+
			"func Bad(mgr backendhooks.IHookManager, payload any) error {\n"+
			"\treturn mgr.Trigger(nil, backendhooks.HookBeforeWrite, payload)\n"+
			"}\n")
	}

	return root
}

func TestScannerReportsEveryDefect(t *testing.T) {
	t.Run("clean fixture matches its table", func(t *testing.T) {
		root := writeFixtureTree(t, fixtureKnobs{})
		report, err := scanTriggerCalls(root)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		unregistered, missing, problems := compareTriggerTable(fixtureTable(), report)
		for _, problem := range problems {
			t.Errorf("unexpected problem: %v", problem)
		}
		if len(unregistered) != 0 {
			t.Errorf("unexpected unregistered calls: %+v", unregistered)
		}
		if len(missing) != 0 {
			t.Errorf("unexpected missing rows: %+v", missing)
		}
		if len(report.Calls) != 2 {
			t.Fatalf("clean fixture produced %d calls, want 2: %+v", len(report.Calls), report.Calls)
		}
	})

	t.Run("unregistered call is reported", func(t *testing.T) {
		root := writeFixtureTree(t, fixtureKnobs{extraUnregisteredCall: true})
		report, err := scanTriggerCalls(root)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		unregistered, missing, problems := compareTriggerTable(fixtureTable(), report)
		if len(problems) != 0 {
			t.Fatalf("unexpected problems: %+v", problems)
		}
		if len(missing) != 0 {
			t.Errorf("unexpected missing rows: %+v", missing)
		}
		if len(unregistered) != 1 {
			t.Fatalf("unregistered calls = %+v, want exactly the Sneaky call", unregistered)
		}
		if unregistered[0].Hook != HookAfterExec || !strings.HasSuffix(unregistered[0].Site, ":Sneaky") {
			t.Errorf("unregistered call = %+v, want HookAfterExec at ...:Sneaky", unregistered[0])
		}
	})

	t.Run("deleted call site is reported as a missing row", func(t *testing.T) {
		root := writeFixtureTree(t, fixtureKnobs{dropWriteCall: true})
		report, err := scanTriggerCalls(root)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		_, missing, problems := compareTriggerTable(fixtureTable(), report)
		if len(problems) != 0 {
			t.Fatalf("unexpected problems: %+v", problems)
		}
		if len(missing) != 1 || missing[0].Hook != HookBeforeWrite {
			t.Fatalf("missing rows = %+v, want the HookBeforeWrite row", missing)
		}
	})

	t.Run("unresolvable receiver fails the scan", func(t *testing.T) {
		root := writeFixtureTree(t, fixtureKnobs{badReceiver: true})
		report, err := scanTriggerCalls(root)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(report.Problems) != 1 {
			t.Fatalf("problems = %+v, want exactly the unresolved receiver", report.Problems)
		}
		if !strings.Contains(report.Problems[0].Reason, "cannot resolve the receiver") {
			t.Errorf("problem reason = %q, want an unresolved-receiver message", report.Problems[0].Reason)
		}
	})

	t.Run("forwarder called with a variable hook type is reported", func(t *testing.T) {
		root := writeFixtureTree(t, fixtureKnobs{variableForwarderArg: true})
		report, err := scanTriggerCalls(root)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(report.Problems) != 1 {
			t.Fatalf("problems = %+v, want exactly the unattributable forwarder call", report.Problems)
		}
		if !strings.Contains(report.Problems[0].Reason, "non-constant hook type") {
			t.Errorf("problem reason = %q, want a non-constant hook type message", report.Problems[0].Reason)
		}
	})

	t.Run("test files and testdata are skipped", func(t *testing.T) {
		root := writeFixtureTree(t, fixtureKnobs{})
		// A trigger call hidden in a test file or testdata must not be counted:
		// neither is production wiring.
		extra := "package aaa\n\n" +
			"import backendhooks \"github.com/codeflow/backend/internal/hooks\"\n\n" +
			"func TestThing(t *testing.T) {\n" +
			"\t_ = backendhooks.GetHookManager().Trigger(nil, backendhooks.HookAfterExec, payload)\n" +
			"}\n"
		if err := os.WriteFile(filepath.Join(root, "internal", "aaa", "x_test.go"), []byte(extra), 0o644); err != nil {
			t.Fatalf("write test file: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "internal", "aaa", "testdata"), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "internal", "aaa", "testdata", "gen.go"), []byte(extra), 0o644); err != nil {
			t.Fatalf("write testdata file: %v", err)
		}
		report, err := scanTriggerCalls(root)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(report.Calls) != 2 {
			t.Fatalf("scan counted %d calls, want 2 (test files and testdata must be skipped): %+v", len(report.Calls), report.Calls)
		}
	})

	t.Run("the hooks package itself is not scanned", func(t *testing.T) {
		root := writeFixtureTree(t, fixtureKnobs{})
		// The fixture's internal/hooks/types.go declares a manager and a
		// GetHookManager; a call added there is a definition, not a trigger.
		path := filepath.Join(root, "internal", "hooks", "types.go")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		extra := string(data) + "\nfunc definition() error {\n" +
			"\treturn GetHookManager().Trigger(nil, HookAfterExec, nil)\n}\n"
		if err := os.WriteFile(path, []byte(extra), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		report, err := scanTriggerCalls(root)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(report.Calls) != 2 {
			t.Fatalf("scan counted %d calls, want 2: the hooks package must be skipped", len(report.Calls))
		}
	})
}

// TestScannerIgnoresForeignMethods pins the samg rule on a synthetic tree: a
// method with a trigger name on a non-hooks receiver is not a trigger.
func TestScannerIgnoresForeignMethods(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "hooks", "types.go"),
		[]byte("package hooks\n\ntype HookType string\n\nconst HookOnMessageComplete HookType = \"hook_on_message_complete\"\n"), 0o644); err != nil {
		t.Fatalf("write types: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "samg"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	foreign := "package samg\n\n" +
		"import \"github.com/codeflow/backend/internal/hooks\"\n\n" +
		"type Service struct{}\n\n" +
		"func (s *Service) HookOnMessageComplete(ctx int, content string) error { return nil }\n\n" +
		"func (s *Service) Run(ctx int) error {\n" +
		"\t_ = hooks.HookOnMessageComplete\n" +
		"\treturn s.HookOnMessageComplete(ctx, \"x\")\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(root, "internal", "samg", "service.go"), []byte(foreign), 0o644); err != nil {
		t.Fatalf("write foreign: %v", err)
	}

	report, err := scanTriggerCalls(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(report.Calls) != 0 {
		t.Fatalf("foreign method counted as a trigger: %+v", report.Calls)
	}
	// The bare `hooks.HookOnMessageComplete` reference is a constant read, not a
	// trigger method on a receiver, so it is not a problem either.
	if len(report.Problems) != 0 {
		t.Fatalf("unexpected problems: %+v", report.Problems)
	}
}
