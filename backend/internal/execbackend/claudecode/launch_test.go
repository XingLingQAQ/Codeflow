package claudecode

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/execbackend"
)

// launchSpec 返回一个"典型"合法 spec，供各处派生。
func launchSpec(t *testing.T) LaunchSpec {
	t.Helper()
	dir := t.TempDir()
	return LaunchSpec{
		Executable:   filepath.Join(dir, "claude.exe"),
		SettingsPath: filepath.Join(dir, "settings.json"),
		Tools:        []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash", "PowerShell", "NotebookEdit"},
		SessionID:    "3f2a1c4e-5b6d-4e8f-9a0b-1c2d3e4f5a6b",
	}
}

// legalSpecs 返回多个合法 spec 变体：禁用参数与硬闸门的断言要对它们全部成立。
func legalSpecs(t *testing.T) map[string]LaunchSpec {
	t.Helper()
	base := launchSpec(t)
	dir := t.TempDir()

	withModel := base
	withModel.Model = "claude-opus-5-5[1m]"

	withDirs := base
	withDirs.AddDirs = []string{filepath.Join(dir, "a"), filepath.Join(dir, "b")}

	withFlags := base
	withFlags.IncludeHookEvents = true
	withFlags.IncludePartialMessages = true

	persist := base
	persist.Persist = true

	full := base
	full.Model = "sonnet"
	full.AddDirs = []string{filepath.Join(dir, "a")}
	full.IncludeHookEvents = true
	full.IncludePartialMessages = true

	return map[string]LaunchSpec{
		"minimal":      base,
		"with_model":   withModel,
		"with_dirs":    withDirs,
		"with_flags":   withFlags,
		"persist":      persist,
		"persist_full": full,
	}
}

func TestLaunchSpecValidate(t *testing.T) {
	spec := launchSpec(t)
	if err := spec.Validate(); err != nil {
		t.Fatalf("typical spec must be valid: %v", err)
	}
	for name, candidate := range legalSpecs(t) {
		if err := candidate.Validate(); err != nil {
			t.Errorf("%s spec must be valid: %v", name, err)
		}
	}
}

func TestLaunchSpecArgsExactOrder(t *testing.T) {
	dir := t.TempDir()
	spec := LaunchSpec{
		Executable:             filepath.Join(dir, "claude.exe"),
		SettingsPath:           filepath.Join(dir, "settings.json"),
		Tools:                  []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash", "PowerShell", "NotebookEdit"},
		SessionID:              "3f2a1c4e-5b6d-4e8f-9a0b-1c2d3e4f5a6b",
		Model:                  "sonnet",
		AddDirs:                []string{filepath.Join(dir, "a"), filepath.Join(dir, "b")},
		IncludeHookEvents:      true,
		IncludePartialMessages: true,
	}
	want := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--restricted",
		"--permission-mode", "dontAsk",
		"--permission-prompts", "none",
		"--settings", filepath.Join(dir, "settings.json"),
		"--strict-mcp-config",
		"--tools", "Read,Edit,Write,Glob,Grep,Bash,PowerShell,NotebookEdit",
		"--session-id", "3f2a1c4e-5b6d-4e8f-9a0b-1c2d3e4f5a6b",
		"--model", "sonnet",
		"--add-dir", filepath.Join(dir, "a"),
		"--add-dir", filepath.Join(dir, "b"),
		"--include-hook-events",
		"--include-partial-messages",
		"--no-session-persistence",
	}
	got := spec.Args()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestLaunchSpecArgsMinimal(t *testing.T) {
	spec := launchSpec(t)
	wantPrefix := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--restricted",
		"--permission-mode", "dontAsk",
		"--permission-prompts", "none",
	}
	got := spec.Args()
	if !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("Args() prefix = %#v, want %#v", got[:len(wantPrefix)], wantPrefix)
	}
	for _, absent := range []string{"--model", "--add-dir", "--include-hook-events", "--include-partial-messages"} {
		for _, arg := range got {
			if arg == absent {
				t.Errorf("minimal spec must not emit %s: %#v", absent, got)
			}
		}
	}
	if got[len(got)-1] != "--no-session-persistence" {
		t.Fatalf("Persist=false must emit --no-session-persistence last, got %#v", got)
	}
}

func TestLaunchSpecPersist(t *testing.T) {
	spec := launchSpec(t)
	spec.Persist = true
	for _, arg := range spec.Args() {
		if arg == "--no-session-persistence" {
			t.Fatal("Persist=true must not emit --no-session-persistence")
		}
	}

	spec.Persist = false
	found := false
	for _, arg := range spec.Args() {
		if arg == "--no-session-persistence" {
			found = true
		}
	}
	if !found {
		t.Fatal("Persist=false must emit --no-session-persistence")
	}
}

// TestLaunchSpecArgsFlagsExistInHelp 是"CLI 升级后某 flag 消失"的护栏：
// Args 输出的每个 flag 都必须在 fixture help 的解析结果里存在。
func TestLaunchSpecArgsFlagsExistInHelp(t *testing.T) {
	flags := fixtureFlags(t)
	for name, spec := range legalSpecs(t) {
		if err := spec.Validate(); err != nil {
			t.Fatalf("%s spec must be valid: %v", name, err)
		}
		for _, arg := range spec.Args() {
			if !strings.HasPrefix(arg, "-") {
				continue
			}
			if !flags.Has(arg) {
				t.Errorf("%s: flag %s is not in `claude --help`", name, arg)
			}
		}
	}
}

// TestLaunchSpecArgsHaveNoPositionalArgs 断言 argv 里没有位置参数：
// 每个不以 '-' 开头的元素都必须紧跟在需要取值的 flag 之后。
func TestLaunchSpecArgsHaveNoPositionalArgs(t *testing.T) {
	flags := fixtureFlags(t)
	for name, spec := range legalSpecs(t) {
		args := spec.Args()
		for i, arg := range args {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			if i == 0 {
				t.Fatalf("%s: argv starts with a non-flag element %q", name, arg)
			}
			prev := args[i-1]
			fact, ok := flags.Lookup(prev)
			if !ok {
				t.Fatalf("%s: %q follows unknown flag %q", name, arg, prev)
			}
			if fact.Value == "" {
				t.Fatalf("%s: %q follows boolean flag %q (would be a positional argument)", name, arg, prev)
			}
		}
	}
}

func TestLaunchSpecBannedArgsNeverEmitted(t *testing.T) {
	banned := BannedArgs()
	if len(banned) == 0 {
		t.Fatal("banned args table must not be empty")
	}
	for _, entry := range banned {
		if entry.Name == "" || entry.Reason == "" {
			t.Fatalf("banned arg entry must have name and reason: %+v", entry)
		}
	}
	for name, spec := range legalSpecs(t) {
		args := spec.Args()
		for _, entry := range banned {
			for _, arg := range args {
				if arg == entry.Name {
					t.Errorf("%s: banned arg %s (%s) appeared in argv", name, entry.Name, entry.Reason)
				}
			}
		}
	}
}

// TestLaunchSpecBannedArgRejectedAsValue 覆盖“禁用参数以取值身份混进 argv”的路径：
// 任何一个禁用参数名作为模型名都必须被 Validate 拒绝，否则 Args 会输出
// `--model --dangerously-skip-permissions` 这样的序列。
func TestLaunchSpecBannedArgRejectedAsValue(t *testing.T) {
	for _, banned := range BannedArgs() {
		spec := launchSpec(t)
		spec.Model = banned.Name
		if err := spec.Validate(); err == nil {
			t.Errorf("model %q (a banned argument) must be rejected; Args would emit %v", banned.Name, spec.Args())
		}
	}
}

func TestLaunchSpecHardGatesAlwaysPresent(t *testing.T) {
	for name, spec := range legalSpecs(t) {
		args := spec.Args()
		joined := strings.Join(args, " ")
		for _, want := range []string{
			"--restricted",
			"--permission-mode dontAsk",
			"--permission-prompts none",
			"--strict-mcp-config",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s: argv must contain %q, got %v", name, want, args)
			}
		}
	}
}

func TestLaunchSpecPermissionModeOnlyDontAsk(t *testing.T) {
	for name, spec := range legalSpecs(t) {
		args := spec.Args()
		for i, arg := range args {
			if arg != "--permission-mode" {
				continue
			}
			if i+1 >= len(args) {
				t.Fatalf("%s: --permission-mode without value", name)
			}
			if args[i+1] != "dontAsk" || AllowedPermissionMode != "dontAsk" {
				t.Fatalf("%s: --permission-mode %q is not allowed", name, args[i+1])
			}
		}
	}
}

func TestLaunchSpecValidateExecutable(t *testing.T) {
	spec := launchSpec(t)
	spec.Executable = "claude"
	assertInvalidRequest(t, spec.Validate(), "executable")

	spec = launchSpec(t)
	spec.Executable = ""
	assertInvalidRequest(t, spec.Validate(), "executable")
}

func TestLaunchSpecValidateSettingsPath(t *testing.T) {
	spec := launchSpec(t)
	spec.SettingsPath = "settings.json"
	assertInvalidRequest(t, spec.Validate(), "settings_path")

	spec = launchSpec(t)
	spec.SettingsPath = ""
	assertInvalidRequest(t, spec.Validate(), "settings_path")
}

func TestLaunchSpecValidateAddDirs(t *testing.T) {
	spec := launchSpec(t)
	spec.AddDirs = []string{filepath.Join(t.TempDir(), "ok"), "relative/dir"}
	assertInvalidRequest(t, spec.Validate(), "add_dirs")

	spec = launchSpec(t)
	spec.AddDirs = []string{""}
	assertInvalidRequest(t, spec.Validate(), "add_dirs")
}

func TestLaunchSpecValidateTools(t *testing.T) {
	cases := map[string][]string{
		"nil":             nil,
		"empty slice":     {},
		"empty name":      {"Read", ""},
		"unknown":         {"Read", "Teleport"},
		"default preset":  {"default"},
		"permission rule": {"Bash(git *)"},
		"read rule":       {"Read(~/secrets/**)"},
		"wildcard":        {"mcp__*"},
		"case mismatch":   {"read"},
		"duplicate":       {"Read", "Read"},
	}
	for name, tools := range cases {
		t.Run(name, func(t *testing.T) {
			spec := launchSpec(t)
			spec.Tools = tools
			assertInvalidRequest(t, spec.Validate(), "tools")
		})
	}
}

func TestLaunchSpecValidateToolsAcceptsAllowlist(t *testing.T) {
	for _, tool := range AllowedTools() {
		spec := launchSpec(t)
		spec.Tools = []string{tool}
		if err := spec.Validate(); err != nil {
			t.Errorf("tool %s must be accepted: %v", tool, err)
		}
	}
	all := launchSpec(t)
	all.Tools = AllowedTools()
	if err := all.Validate(); err != nil {
		t.Fatalf("full allowlist must validate: %v", err)
	}
	want := []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash", "PowerShell", "NotebookEdit"}
	if got := AllowedTools(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedTools() = %v, want %v", got, want)
	}
}

func TestAllowedToolsReturnsCopy(t *testing.T) {
	tools := AllowedTools()
	tools[0] = "Teleport"
	if AllowedTools()[0] != "Read" {
		t.Fatal("AllowedTools must return a copy: callers must not mutate the allowlist")
	}
}

func TestLaunchSpecValidateSessionID(t *testing.T) {
	bad := []string{
		"",
		"not-a-uuid",
		"3f2a1c4e5b6d4e8f9a0b1c2d3e4f5a6b",
		"3f2a1c4e-5b6d-4e8f-9a0b-1c2d3e4f5a6",
		"3f2a1c4e-5b6d-4e8f-9a0b-1c2d3e4f5a6bz",
		"3f2a1c4e-5b6d-4e8f-9a0b-1c2d3e4f5a6b-",
		"{3f2a1c4e-5b6d-4e8f-9a0b-1c2d3e4f5a6b}",
	}
	for _, id := range bad {
		spec := launchSpec(t)
		spec.SessionID = id
		assertInvalidRequest(t, spec.Validate(), "session_id")
	}
	good := []string{
		"3f2a1c4e-5b6d-4e8f-9a0b-1c2d3e4f5a6b",
		"3F2A1C4E-5B6D-4E8F-9A0B-1C2D3E4F5A6B",
		"00000000-0000-0000-0000-000000000000",
	}
	for _, id := range good {
		spec := launchSpec(t)
		spec.SessionID = id
		if err := spec.Validate(); err != nil {
			t.Errorf("session id %q must be accepted: %v", id, err)
		}
	}
}

func TestLaunchSpecValidateModel(t *testing.T) {
	good := []string{"sonnet", "claude-opus-5-5[1m]", "claude-fable-5", "claude-opus-4-1:beta", "us.anthropic.claude-3"}
	for _, model := range good {
		spec := launchSpec(t)
		spec.Model = model
		if err := spec.Validate(); err != nil {
			t.Errorf("model %q must be accepted: %v", model, err)
		}
	}
	bad := []string{
		"claude opus",
		" sonnet",
		"sonnet ",
		"sonnet;rm -rf /",
		"$(whoami)",
		"`id`",
		"sonnet&&id",
		"sonnet|id",
		"sonnet>out",
		`"sonnet"`,
		"sonnet\n",
		"sonnet*",
		// 以 - 开头的取值会把禁用参数原样送进 argv（--model 后面紧跟一个 flag 形状的参数）。
		"-p",
		"-",
		"--resume",
		"--dangerously-skip-permissions",
	}
	for _, model := range bad {
		spec := launchSpec(t)
		spec.Model = model
		assertInvalidRequest(t, spec.Validate(), "model")
	}
}

// assertInvalidRequest 断言 err 是 Code=invalid_request 且 Field 匹配的领域错误。
func assertInvalidRequest(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want invalid_request for field %s, got nil", field)
	}
	var domainErr *execbackend.Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("want *execbackend.Error, got %T: %v", err, err)
	}
	if domainErr.Code != execbackend.CodeInvalidRequest {
		t.Fatalf("Code = %s, want %s", domainErr.Code, execbackend.CodeInvalidRequest)
	}
	if !errors.Is(err, execbackend.ErrInvalidRequest) {
		t.Fatalf("error %v must match ErrInvalidRequest", err)
	}
	if domainErr.Field != field {
		t.Fatalf("Field = %q, want %q", domainErr.Field, field)
	}
}
