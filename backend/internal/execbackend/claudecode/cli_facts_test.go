package claudecode

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixtureDir 是 T1.13.a 抓下来的本机 CLI 输出（`claude --version` / `claude --help` 原样）。
const fixtureDir = "testdata/cli/2.1.283"

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func fixtureVersionText(t *testing.T) string { return readFixture(t, "version.txt") }

func fixtureHelpText(t *testing.T) string { return readFixture(t, "help.txt") }

func fixtureVersion(t *testing.T) Version {
	t.Helper()
	v, err := ParseVersion(fixtureVersionText(t))
	if err != nil {
		t.Fatalf("ParseVersion(fixture): %v", err)
	}
	return v
}

func fixtureFlags(t *testing.T) HelpFlags {
	t.Helper()
	flags, err := ParseHelp(fixtureHelpText(t))
	if err != nil {
		t.Fatalf("ParseHelp(fixture): %v", err)
	}
	return flags
}

func TestParseVersionFixture(t *testing.T) {
	v := fixtureVersion(t)
	if !v.Valid() {
		t.Fatal("fixture version must be valid")
	}
	if got, want := v.String(), "2.1.283"; got != want {
		t.Fatalf("Version.String() = %q, want %q", got, want)
	}
	if v.Major() != 2 || v.Minor() != 1 || v.Patch() != 283 {
		t.Fatalf("Version parts = %d.%d.%d, want 2.1.283", v.Major(), v.Minor(), v.Patch())
	}
}

func TestParseVersionTrims(t *testing.T) {
	for _, in := range []string{
		"2.1.283 (Claude Code)\n",
		"  2.1.283 (Claude Code)  \n",
		"2.1.283 (Claude Code)\r\n",
	} {
		v, err := ParseVersion(in)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", in, err)
		}
		if v.String() != "2.1.283" {
			t.Fatalf("ParseVersion(%q) = %q, want 2.1.283", in, v.String())
		}
	}
}

func TestParseVersionRejects(t *testing.T) {
	cases := map[string]string{
		"beta suffix":      "2.1.283-beta (Claude Code)",
		"empty":            "",
		"brand first":      "Claude Code 2.1.283",
		"missing brand":    "2.1.283",
		"other brand":      "2.1.283 (Codex CLI)",
		"sandbox error":    "Error: command not found",
		"two lines":        "2.1.283 (Claude Code)\n2.1.284 (Claude Code)",
		"two component":    "2.1 (Claude Code)",
		"build metadata":   "2.1.283+build (Claude Code)",
		"leading v":        "v2.1.283 (Claude Code)",
		"only whitespace":  "   \n",
		"package json-ish": `{"version":"2.1.283"}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := ParseVersion(in)
			if err == nil {
				t.Fatalf("ParseVersion(%q) = %v, want error", in, v)
			}
			if !errors.Is(err, ErrVersionUnparsable) {
				t.Fatalf("error %v does not wrap ErrVersionUnparsable", err)
			}
			if v.Valid() {
				t.Fatalf("rejected version must not be valid: %v", v)
			}
		})
	}
}

func TestVersionCompareAndAtLeast(t *testing.T) {
	cases := []struct {
		a, b Version
		want int
	}{
		{NewVersion(2, 1, 259), NewVersion(2, 1, 259), 0},
		{NewVersion(2, 1, 258), NewVersion(2, 1, 259), -1},
		{NewVersion(2, 1, 260), NewVersion(2, 1, 259), 1},
		{NewVersion(2, 2, 0), NewVersion(2, 1, 999), 1},
		{NewVersion(3, 0, 0), NewVersion(2, 9, 9), 1},
		{Version{}, NewVersion(2, 1, 259), -1},
	}
	for _, tc := range cases {
		if got := tc.a.Compare(tc.b); got != tc.want {
			t.Fatalf("Compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	if !NewVersion(2, 1, 259).AtLeast(minSupportedVersion) {
		t.Fatal("MinSupportedVersion must be inclusive (>=): 2.1.259 must be supported")
	}
	if NewVersion(2, 1, 258).AtLeast(minSupportedVersion) {
		t.Fatal("2.1.258 must be below the minimum")
	}
	if (Version{}).Valid() {
		t.Fatal("zero Version must be invalid")
	}
	if (Version{}).String() != "" {
		t.Fatalf("zero Version.String() = %q, want empty", (Version{}).String())
	}
}

// requiredFlags 是 §28 T1.13.a 要求本机 help 必须提供的 flag。
var requiredFlags = []string{
	"-p", "--print",
	"--output-format",
	"--verbose",
	"--permission-mode",
	"--permission-prompts",
	"--restricted",
	"--settings",
	"--strict-mcp-config",
	"--tools",
	"--session-id",
	"--no-session-persistence",
	"--include-hook-events",
	"--include-partial-messages",
	"--model",
	"--add-dir",
	"--mcp-config",
	"--allowedTools", "--allowed-tools",
}

func TestParseHelpFixtureFlags(t *testing.T) {
	flags := fixtureFlags(t)
	for _, name := range requiredFlags {
		if !flags.Has(name) {
			t.Errorf("fixture help is missing flag %s", name)
		}
	}
}

func TestParseHelpAliasesAndValues(t *testing.T) {
	flags := fixtureFlags(t)

	printFlag, ok := flags.Lookup("--print")
	if !ok {
		t.Fatal("--print not found")
	}
	if !printFlag.HasName("-p") || !printFlag.HasName("--print") {
		t.Fatalf("--print aliases = %v, want -p and --print", printFlag)
	}
	if printFlag.Value != "" {
		t.Fatalf("-p/--print is a boolean switch, got value %q", printFlag.Value)
	}

	allowed, ok := flags.Lookup("--allowedTools")
	if !ok {
		t.Fatal("--allowedTools not found")
	}
	if !allowed.HasName("--allowed-tools") {
		t.Fatalf("--allowedTools aliases = %v, want --allowed-tools", allowed)
	}
	if allowed.Value != "<tools...>" {
		t.Fatalf("--allowedTools value = %q, want <tools...>", allowed.Value)
	}

	tools, ok := flags.Lookup("--tools")
	if !ok {
		t.Fatal("--tools not found")
	}
	if tools.Value != "<tools...>" {
		t.Fatalf("--tools value = %q, want <tools...>", tools.Value)
	}

	addDir, ok := flags.Lookup("--add-dir")
	if !ok {
		t.Fatal("--add-dir not found")
	}
	if addDir.Value != "<directories...>" {
		t.Fatalf("--add-dir value = %q, want <directories...>", addDir.Value)
	}
}

func TestParseHelpChoices(t *testing.T) {
	flags := fixtureFlags(t)

	outputFormat, ok := flags.Lookup("--output-format")
	if !ok {
		t.Fatal("--output-format not found")
	}
	if !outputFormat.HasChoice("stream-json") {
		t.Fatalf("--output-format choices = %v, want stream-json", outputFormat.Choices)
	}

	// --permission-mode 的 choices 在 help 里跨三行，必须拼起来才完整。
	mode, ok := flags.Lookup("--permission-mode")
	if !ok {
		t.Fatal("--permission-mode not found")
	}
	for _, want := range []string{"acceptEdits", "auto", "bypassPermissions", "manual", "dontAsk", "plan"} {
		if !mode.HasChoice(want) {
			t.Errorf("--permission-mode choices %v missing %q", mode.Choices, want)
		}
	}
	if len(mode.Choices) != 6 {
		t.Fatalf("--permission-mode choices = %v, want exactly 6 entries", mode.Choices)
	}

	// (choices: "host", "none", default: "host")：default 说明不能被当成 choice。
	prompts, ok := flags.Lookup("--permission-prompts")
	if !ok {
		t.Fatal("--permission-prompts not found")
	}
	if got, want := prompts.Choices, []string{"host", "none"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("--permission-prompts choices = %v, want %v", got, want)
	}

	// preset: "true" 同理不是 choice。
	suggestions, ok := flags.Lookup("--prompt-suggestions")
	if !ok {
		t.Fatal("--prompt-suggestions not found")
	}
	if suggestions.HasChoice("preset") {
		t.Fatalf("--prompt-suggestions choices = %v, must not contain preset markers", suggestions.Choices)
	}
}

func TestParseHelpExcludesCommands(t *testing.T) {
	flags := fixtureFlags(t)
	for _, name := range []string{"update", "upgrade", "doctor", "auth", "mcp", "plugin"} {
		if flags.Has(name) {
			t.Errorf("command %q must not be parsed as a flag", name)
		}
	}
	for _, primary := range flags.Names() {
		if !strings.HasPrefix(primary, "-") {
			t.Errorf("flag primary name %q does not start with '-'", primary)
		}
	}
	// Commands 段在 Options 段之后，`update|upgrade` 必须整条被排除。
	if flags.Has("--help") == false {
		t.Error("--help should still be present as a flag")
	}
}

func TestParseHelpCRLFMatchesLF(t *testing.T) {
	// 干净检出里 fixture 本身就是 CRLF，先归一化再重新造 CRLF，保证本测试在任何
	// 检出形态下都真的在比较"LF vs CRLF"。
	lf := normalizeNewlines(fixtureHelpText(t))
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	if crlf == lf {
		t.Fatal("fixture has no newline to convert")
	}
	fromLF, err := ParseHelp(lf)
	if err != nil {
		t.Fatalf("ParseHelp(LF): %v", err)
	}
	fromCRLF, err := ParseHelp(crlf)
	if err != nil {
		t.Fatalf("ParseHelp(CRLF): %v", err)
	}
	if !reflect.DeepEqual(fromLF, fromCRLF) {
		t.Fatal("CRLF help must parse exactly like LF help")
	}
	if !fromCRLF.HasChoice("--permission-mode", "dontAsk") {
		t.Fatal("CRLF parse lost dontAsk (newline normalization is missing)")
	}
}

func TestParseHelpStripsANSI(t *testing.T) {
	// 先归一化换行：本测试只关心 ANSI 剥离，CRLF 由 TestParseHelpCRLFMatchesLF 单独覆盖。
	// 干净检出（core.autocrlf=true）里 fixture 是 CRLF，逐行拼 ANSI 时行尾 \r 会留在
	// 转义序列里，让颜色包裹变成"改结构"。
	plain := normalizeNewlines(fixtureHelpText(t))
	var colored strings.Builder
	for _, line := range strings.Split(plain, "\n") {
		if line == "" {
			colored.WriteString(line)
		} else {
			colored.WriteString("\x1b[36m" + line + "\x1b[39m")
		}
		colored.WriteString("\n")
	}
	fromPlain, err := ParseHelp(plain)
	if err != nil {
		t.Fatalf("ParseHelp(plain): %v", err)
	}
	fromColored, err := ParseHelp(colored.String())
	if err != nil {
		t.Fatalf("ParseHelp(colored): %v", err)
	}
	if !reflect.DeepEqual(fromPlain, fromColored) {
		t.Fatal("ANSI-colored help must parse exactly like plain help")
	}
}

func TestParseHelpUnparsable(t *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"prose only":         "Claude Code help is unavailable\n",
		"commands only":      "Commands:\n  update|upgrade  Check for updates\n",
		"arguments only":     "Arguments:\n  prompt   Your prompt\n",
		"options header out": "options:\n  --print  Print response\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			flags, err := ParseHelp(in)
			if err == nil {
				t.Fatalf("ParseHelp(%q) = %v, want error", in, flags)
			}
			if !errors.Is(err, ErrHelpUnparsable) {
				t.Fatalf("error %v does not wrap ErrHelpUnparsable", err)
			}
		})
	}
}

func TestParseHelpOnlyOptionsSection(t *testing.T) {
	// Options 段结束于下一个顶格非空行：Commands 段里的 --flag 形状文本不能被收进来。
	text := "Usage: claude\n\nOptions:\n  --print  Print response\n\nCommands:\n  --sneaky  Not a flag\n"
	flags, err := ParseHelp(text)
	if err != nil {
		t.Fatalf("ParseHelp: %v", err)
	}
	if !flags.Has("--print") {
		t.Fatal("--print should be parsed")
	}
	if flags.Has("--sneaky") {
		t.Fatal("flags after the Options section must not be parsed")
	}
}

func TestHelpFlagsDuplicateAliases(t *testing.T) {
	text := "Options:\n  --tools <tools...>  a\n  --tools <tools...>  b\n"
	flags, err := ParseHelp(text)
	if err != nil {
		t.Fatalf("ParseHelp: %v", err)
	}
	if len(flags.Names()) != 2 {
		t.Fatalf("Names() = %v, want both entries in order", flags.Names())
	}
	if _, ok := flags.Lookup("--tools"); !ok {
		t.Fatal("--tools must still be looked up")
	}
}
