package approval_test

import (
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/approval"
)

// TestClassifyToolRisk is §28 T2.02.b's risk table, one row per rule. The
// table is the whole point of the function: a low row must never be anything
// else (a false low auto-approves an unreviewed action) and a high row must
// name the reason a reviewer will read.
func TestClassifyToolRisk(t *testing.T) {
	trusted := approval.RiskConfig{TrustedCommands: [][]string{
		{"pnpm", "test"},
		{"git", "push"},
		{"", ""}, // an empty prefix must not match everything
	}}

	cases := []struct {
		name string
		cfg  approval.RiskConfig
		// action is the input; wantRisk is the table's answer.
		action   approval.ToolAction
		wantRisk approval.Risk
		wantFrag string // a fragment of the reason that must be present
	}{
		// Rule 1: no command, classified by the tool name.
		{"read tool", approval.RiskConfig{},
			approval.ToolAction{Tool: "read_file"}, approval.RiskLow, "read_only_tool"},
		{"read tool legacy name", approval.RiskConfig{},
			approval.ToolAction{Tool: "ReadFile"}, approval.RiskLow, "read_only_tool"},
		{"list dir", approval.RiskConfig{},
			approval.ToolAction{Tool: "list_dir"}, approval.RiskLow, "read_only_tool"},
		{"grep tool", approval.RiskConfig{},
			approval.ToolAction{Tool: "grep"}, approval.RiskLow, "read_only_tool"},
		{"glob tool", approval.RiskConfig{},
			approval.ToolAction{Tool: "glob"}, approval.RiskLow, "read_only_tool"},
		// A web fetch sends data out (main agent's acceptance review): it is
		// egress, not a read, so it is never auto-approved.
		{"webfetch tool is not a read", approval.RiskConfig{},
			approval.ToolAction{Tool: "web_fetch"}, approval.RiskHigh, "not a recognised reader"},
		{"write tool", approval.RiskConfig{},
			approval.ToolAction{Tool: "write_file"}, approval.RiskMedium, "workspace_write"},
		{"edit tool", approval.RiskConfig{},
			approval.ToolAction{Tool: "apply_patch"}, approval.RiskMedium, "workspace_write"},
		{"mkdir tool", approval.RiskConfig{},
			approval.ToolAction{Tool: "mkdir"}, approval.RiskMedium, "workspace_write"},
		{"unknown tool", approval.RiskConfig{},
			approval.ToolAction{Tool: "frobnicate"}, approval.RiskHigh, "fail closed"},
		{"empty tool", approval.RiskConfig{},
			approval.ToolAction{}, approval.RiskHigh, "fail closed"},

		// Rule 2: a single known read-only command is low.
		{"ls", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"ls", "-la"}}, approval.RiskLow, "known_read_only_command"},
		{"cat", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"cat", "src/a.go"}}, approval.RiskLow, "known_read_only_command"},
		{"git status", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"git", "status"}}, approval.RiskLow, "known_read_only_command"},
		{"git diff", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"git", "diff", "--cached"}}, approval.RiskLow, "known_read_only_command"},
		{"absolute path reader", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"/bin/ls"}}, approval.RiskLow, "known_read_only_command"},
		{"grep with a quoted space", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"grep", "a b", "src/a.go"}}, approval.RiskLow, "known_read_only_command"},

		// Rule 2 again, from the other side: composition is never read-only.
		{"pipeline", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"cat", "src/a.go |", "sh"}}, approval.RiskHigh, "composes other commands"},
		{"redirect", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"echo", "x", ">", "src/a.go"}}, approval.RiskHigh, "composes other commands"},
		{"chained", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"ls", "&&", "rm", "-rf", "."}}, approval.RiskHigh, "composes other commands"},
		{"substitution", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"echo", "$(rm -rf /)"}}, approval.RiskHigh, "composes other commands"},
		{"git with an unknown subcommand", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"git", "clean", "-fd"}}, approval.RiskHigh, "unknown_command"},
		{"find (excluded from the reader table)", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"find", ".", "-delete"}}, approval.RiskHigh, "unknown_command"},

		// Rule 5: runners that execute project scripts are high by default.
		{"npm test", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"npm", "test"}}, approval.RiskHigh, "execute project scripts"},
		{"npm install", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"npm", "install"}}, approval.RiskHigh, "execute project scripts"},
		{"go test", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"go", "test", "./..."}}, approval.RiskHigh, "execute project scripts"},
		{"pytest", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"pytest", "-q"}}, approval.RiskHigh, "execute project scripts"},
		{"make", approval.RiskConfig{},
			approval.ToolAction{Tool: "run_shell", Command: []string{"make", "build"}}, approval.RiskHigh, "execute project scripts"},

		// Rule 4: a project-trusted prefix drops high to medium, never to low.
		{"trusted pnpm test", trusted,
			approval.ToolAction{Tool: "run_shell", Command: []string{"pnpm", "test"}}, approval.RiskMedium, "trusted_command"},
		{"trusted pnpm test with args", trusted,
			approval.ToolAction{Tool: "run_shell", Command: []string{"pnpm", "test", "--", "src/a.test.ts"}}, approval.RiskMedium, "trusted_command"},
		{"trusted git push", trusted,
			approval.ToolAction{Tool: "run_shell", Command: []string{"git", "push", "origin", "HEAD"}}, approval.RiskMedium, "trusted_command"},
		{"trusted prefix does not match a longer word", trusted,
			approval.ToolAction{Tool: "run_shell", Command: []string{"pnpm", "testing"}}, approval.RiskHigh, "execute project scripts"},
		{"trust never blesses a compound", trusted,
			approval.ToolAction{Tool: "run_shell", Command: []string{"pnpm", "test", "&&", "rm", "-rf", "."}}, approval.RiskHigh, "composes other commands"},
		{"empty trusted prefix is skipped", approval.RiskConfig{TrustedCommands: [][]string{{""}}},
			approval.ToolAction{Tool: "run_shell", Command: []string{"rm", "-rf", "."}}, approval.RiskHigh, "unknown_command"},
		{"a trusted prefix shorter than argv", trusted,
			approval.ToolAction{Tool: "run_shell", Command: []string{"pnpm"}}, approval.RiskHigh, "execute project scripts"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			risk, reasons := approval.ClassifyToolRisk(tc.action, tc.cfg)
			if risk != tc.wantRisk {
				t.Fatalf("ClassifyToolRisk(%v) = %s (%v), want %s",
					tc.action.Command, risk, reasons, tc.wantRisk)
			}
			if len(reasons) == 0 {
				t.Fatal("ClassifyToolRisk returned no reasons; every path must explain itself")
			}
			if !strings.Contains(reasons[0], tc.wantFrag) {
				t.Errorf("reason %q does not contain %q", reasons[0], tc.wantFrag)
			}
		})
	}
}

// TestClassifyToolRiskNeverReturnsLowForUnknownInput is the fail-closed
// direction as its own assertion: an unknown tool with an unknown command, an
// empty action and a command with a newline are all high, so nothing that the
// table does not positively recognize can be auto-approved.
func TestClassifyToolRiskNeverReturnsLowForUnknownInput(t *testing.T) {
	actions := []approval.ToolAction{
		{},
		{Tool: "mystery", Command: []string{"mystery", "--do-things"}},
		{Tool: "run_shell", Command: []string{"ls\nrm -rf /"}},
		{Tool: "read_file", Command: []string{"sh", "-c", "rm -rf /"}},
	}
	for _, action := range actions {
		risk, reasons := approval.ClassifyToolRisk(action, approval.RiskConfig{})
		if risk != approval.RiskHigh {
			t.Errorf("ClassifyToolRisk(%q, %v) = %s (%v), want high", action.Tool, action.Command, risk, reasons)
		}
	}
}
