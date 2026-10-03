// Risk classification for approval requests (T2.02.b, plan §28 T2.02.b).
//
// The plan's rule: "风险表按可信项目配置和实际命令效果判断：读取可低风险，
// 测试/安装默认视为能执行脚本" — classify by what the action will actually do,
// not by what the tool is called. The consequence the table is built around is
// the one a reviewer cares about: a command that can run project scripts (a
// test runner, an installer, a build) executes arbitrary code out of the
// repository, so it is high risk and must be authorized explicitly by a human.
// A command that only reads is low risk and never interrupts the user.
//
// This file is deliberately pure: no database, no clock, no policy. It answers
// one question — given a tool action, how dangerous is it — and returns the
// reasons, so a reviewer and an audit row can see why the table answered what
// it did. The service computes the risk itself and never accepts one from a
// caller (service.go): a caller that could name its own risk could downgrade a
// shell command to "low" and have it auto-approved.
//
// The failure direction is fixed: anything the table does not positively
// recognize as a plain read is high. A false "high" costs a human click; a
// false "low" auto-approves an unreviewed action.
package approval

import (
	"path"
	"strings"
)

// RiskConfig is the project's trusted-command configuration, consulted by
// ClassifyToolRisk.
//
// The zero value trusts nothing, which is the fail-closed default: a project
// that has not declared a command as trusted gets the table's own answer.
type RiskConfig struct {
	// TrustedCommands are argv prefixes the project declares trustworthy —
	// "pnpm test" in a project whose scripts are pinned and reviewed. A command
	// whose argv starts with one of these prefixes is classified medium instead
	// of high: still never auto-approved (medium never is), but the reviewer
	// sees that the project vouched for it.
	//
	// Trust never reaches a compound command: "npm test && rm -rf ." is high
	// even when "npm test" is trusted, because the trusted declaration is about
	// one invocation, not about what a shell does around it.
	//
	// An empty prefix is ignored rather than matching everything.
	TrustedCommands [][]string
}

// Risk reasons. They are stable strings because they are written into the
// approval's audit trail; a reviewer reading "shell:test_or_install" learns
// more than "high". The reason list is returned alongside the risk, so the
// classification is explainable without re-deriving it.
const (
	reasonReadOnlyTool   = "read_only_tool: the action only reads"
	reasonWriteTool      = "workspace_write: the action writes inside the workspace"
	reasonKnownReadOnly  = "shell:known_read_only_command"
	reasonUnsafeOption   = "shell:a read-only program with an option that writes a file or runs another program (fail closed)"
	reasonShellCompound  = "shell:composes other commands (pipeline, redirect, chaining, substitution), so it is not read-only"
	reasonTestOrInstall  = "shell:test_or_install, which can execute project scripts"
	reasonTrustedCommand = "shell:trusted_command declared by the project"
	reasonUnknownCommand = "shell:unknown_command (fail closed)"
	reasonUnknownTool    = "tool:not a recognised reader or workspace writer (fail closed)"
)

// ClassifyToolRisk returns the risk of one tool action and the reasons for it.
//
// The table, in the order the cases are decided:
//
//  1. No command at all: the action is whatever the tool name says. A name that
//     reads as reading (read_file, list_dir, grep, glob, ...) is low; a name
//     that writes/edits/patches inside the workspace is medium; anything else is
//     high, because an unrecognised tool may do anything. This is the
//     fail-closed default, not a guess.
//  2. A command is present. A command that is exactly one simple command (no
//     `|`, `&`, `;`, `<`, `>`, `$(`, backtick) whose argv[0] is a known
//     read-only program is low. A pipeline, a redirect or a chain is not
//     read-only whatever it contains: what runs is then decided by the shell,
//     not by the one program the table recognized.
//  3. A command that composes others is high, before trust and before the
//     runner table (§2's "一律不算"): the project trusted "npm test", not
//     "npm test && rm -rf .", and a compound command is never the invocation
//     anybody declared. Fail closed.
//  4. A command whose argv starts with one of RiskConfig.TrustedCommands is
//     medium. Never low, never for a compound: "trusted by the project" is
//     exactly the promise that the project's own scripts run, and the plan
//     keeps a human in that loop.
//  5. A known read-only program given an option that writes a file or runs
//     another program (`git log --output=…`, `git diff --ext-diff`,
//     `rg --pre …`; see readOnlyUnsafeOptions) is high: the option, not the
//     program name, decides what the command does.
//  6. A command whose argv[0] is a known test/install/build runner — npm, pnpm,
//     yarn, go, cargo, make, pytest, pip, mvn, gradle, bundle, composer, ... —
//     is high, because those programs run scripts out of the repository:
//     `npm test` and `npm install` execute package.json scripts, `go test`
//     compiles and runs project code, `make` runs the Makefile.
//  7. Every other command is high as well (fail closed).
//
// The returned slice is never empty: every path names at least one reason.
func ClassifyToolRisk(action ToolAction, cfg RiskConfig) (Risk, []string) {
	if len(action.Command) == 0 {
		return classifyByName(action)
	}
	if isSimpleReadOnlyCommand(action.Command) {
		return RiskLow, []string{reasonKnownReadOnly}
	}
	if !isSimpleCommand(action.Command) {
		return RiskHigh, []string{reasonShellCompound}
	}
	if trustedCommand(action.Command, cfg.TrustedCommands) {
		return RiskMedium, []string{reasonTrustedCommand}
	}
	if isReadOnlyProgram(action.Command) {
		// A recognized reader that isSimpleReadOnlyCommand still refused: one of
		// its arguments is an option that writes or executes.
		return RiskHigh, []string{reasonUnsafeOption}
	}
	if isTestOrInstallRunner(action.Command[0]) {
		return RiskHigh, []string{reasonTestOrInstall}
	}
	return RiskHigh, []string{reasonUnknownCommand}
}

// classifyByName handles an action with no command: the tool name is the only
// evidence there is.
func classifyByName(action ToolAction) (Risk, []string) {
	name := normalizeToolName(action.Tool)
	if name != "" {
		if isReadOnlyToolName(name) {
			return RiskLow, []string{reasonReadOnlyTool}
		}
		if isWriteToolName(name) {
			return RiskMedium, []string{reasonWriteTool}
		}
	}
	return RiskHigh, []string{reasonUnknownTool}
}

// readOnlyToolNames are tool names that only read the workspace when no command
// is given. Matching is on a normalized name (lowercase, separators removed), so
// "read_file", "ReadFile", "read-file" and "readFile" are one name.
//
// Names deliberately left out although they "read", because the read is not of
// the workspace — they fall through to the fail-closed default:
//
//	webfetch, websearch, httpget, fetch   a request carries data out (in its
//	                                      URL or query), so it is egress, not
//	                                      a read; auto-approving it would let
//	                                      any secret the agent has seen leave
//	                                      without anybody asked
//	readresource                          an MCP resource read runs an external
//	                                      server's code
var readOnlyToolNames = map[string]bool{
	"read": true, "readfile": true, "readfiles": true, "readmultiplefiles": true,
	"readtext": true, "view": true,
	"list": true, "listdir": true, "listdirectory": true, "listfiles": true,
	"ls": true, "dir": true, "tree": true,
	"search": true, "searchfiles": true, "grep": true, "grepsearch": true, "ripgrep": true, "rg": true,
	"glob": true, "find": true, "findfiles": true, "filesearch": true,
	"cat": true, "head": true, "tail": true, "stat": true, "fileinfo": true,
	"metadata": true, "getmetadata": true,
	"gitstatus": true, "gitdiff": true, "gitlog": true, "gitshow": true,
}

// writeToolNames are tool names that change the workspace without running a
// command. They are medium: reversible inside the workspace, no script
// execution.
var writeToolNames = map[string]bool{
	"write": true, "writefile": true, "writefiles": true,
	"edit": true, "editfile": true, "multiedit": true, "strreplace": true,
	"patch": true, "applypatch": true, "replace": true, "insert": true, "notebookedit": true,
	"create": true, "createfile": true, "mkdir": true, "touch": true,
	"delete": true, "deletefile": true, "remove": true, "rm": true,
	"move": true, "rename": true, "copy": true, "cp": true,
}

// readOnlyCommands are the programs whose own operation is to print a read of
// their input and that cannot run another program or write a file. They are
// matched by base name, so "/bin/ls" is "ls".
//
// Programs deliberately left out although they read, because they can also
// execute or write — and this table is a whitelist, so leaving one out is the
// safe direction:
//
//	find, fd      -exec / -delete runs programs and removes files
//	env           `env rm -rf /` runs a program
//	less, more     `!cmd` runs a program from inside the pager
//	sort          `-o file` writes a file
//	uniq          `uniq IN OUT` writes OUT
//	tree          `-o file` writes a file
//	file          `-C` compiles and writes a magic file
//	yq            `-i` edits files in place
//	xargs         runs a program
//	awk, sed      `-i` writes files, `system()` runs programs
//	tee           writes files
//	mktemp        creates a file
//	date          `-s` sets the system clock
//	hostname      `hostname NAME` sets the host name
//	printenv      prints the environment, where credentials live; that is
//	              not a read of the workspace
var readOnlyCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "pwd": true,
	"wc": true, "stat": true, "du": true, "df": true,
	"which": true, "whoami": true, "id": true,
	"uname": true, "echo": true,
	"grep": true, "rg": true, "egrep": true, "fgrep": true,
	"cut": true, "tr": true, "diff": true, "cmp": true,
	"basename": true, "dirname": true, "realpath": true, "readlink": true,
	"sha256sum": true, "md5sum": true, "shasum": true, "cksum": true,
	"jq": true, "column": true,
}

// readOnlyUnsafeOptions are the options that turn a whitelisted reader into a
// writer or a runner. An argument equal to one of them, or spelled
// "option=value", takes the command off the low tier (rule 5). The match is
// exact so "--output-indicator-new" or "--pre-glob" (which only print or
// filter) are not caught.
//
//	git --output        writes the diff/log into a file
//	git --ext-diff      runs the repository's configured external diff program
//	git --textconv      runs the repository's configured conversion filter
//	rg  --pre           runs the given program on every file searched
//
// What argv cannot show is not covered here: a repository whose configuration
// was changed to run a program on a plain `git status` (core.fsmonitor) or
// `git diff` (diff.external, a textconv driver selected by .gitattributes).
// Changing that configuration is itself a write into the repository, which is
// never low; the Guard has to treat writes under .git/ accordingly.
var readOnlyUnsafeOptions = map[string][]string{
	"git": {"--output", "--ext-diff", "--textconv"},
	"rg":  {"--pre"},
}

// gitReadOnlySubcommands are the git subcommands that only read. Anything else
// ("git config", "git clean", "git checkout", "git reset", "git stash", and
// any hook-invoking command) is not on the list, so it falls through to the
// fail-closed default.
var gitReadOnlySubcommands = map[string]bool{
	"status": true, "diff": true, "log": true, "show": true,
}

// testOrInstallRunners are the programs the plan names as "能执行脚本": their
// normal operation runs code that lives in the repository (package scripts,
// Makefiles, test files, install hooks, build plugins, interpreted sources).
// Reaching this table is high whatever the subcommand says, because the
// program itself is the thing that runs project code.
var testOrInstallRunners = map[string]bool{
	"npm": true, "npx": true, "pnpm": true, "pnpx": true, "yarn": true, "bun": true, "bunx": true,
	"go": true, "pytest": true, "py.test": true, "tox": true, "nox": true, "poetry": true,
	"pip": true, "pip3": true, "uv": true, "uvx": true, "conda": true, "mamba": true,
	"make": true, "cmake": true, "ninja": true, "meson": true, "bazel": true, "buck": true,
	"cargo": true, "rustup": true, "mvn": true, "mvnw": true, "gradle": true, "gradlew": true,
	"ant": true, "bundle": true, "bundler": true, "rake": true, "composer": true,
	"dotnet": true, "msbuild": true, "nuget": true, "swift": true, "xcodebuild": true,
	"gcc": true, "g++": true, "clang": true, "clang++": true, "cc": true, "ld": true,
	"tsc": true, "esbuild": true, "webpack": true, "vite": true, "rollup": true,
	"jest": true, "vitest": true, "mocha": true, "playwright": true, "cypress": true,
	"deno": true, "node": true, "python": true, "python3": true, "ruby": true, "perl": true,
	"docker": true, "docker-compose": true, "kubectl": true, "terraform": true, "ansible": true,
	"sh": true, "bash": true, "zsh": true, "dash": true, "cmd": true, "powershell": true, "pwsh": true,
}

// isSimpleReadOnlyCommand reports whether argv is a single, plainly read-only
// command: a recognized reader whose arguments add no write and no execution.
//
// "Single" is enforced structurally (isSimpleCommand): any metacharacter that
// hands control to another command or redirects output makes the whole thing
// "not read-only", because what runs is then no longer only the program this
// table recognized. Quotes are not metacharacters here — an argument that
// contains `|` is refused whether or not it was quoted, so treating the quote
// itself as a composition character would only produce false highs for
// `grep "a b" src`.
func isSimpleReadOnlyCommand(argv []string) bool {
	return isSimpleCommand(argv) && isReadOnlyProgram(argv) && !hasUnsafeReadOnlyOption(argv)
}

// isReadOnlyProgram reports whether argv names a whitelisted reader — for git,
// a read-only subcommand in argv[1] — regardless of its other arguments.
func isReadOnlyProgram(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := commandBase(argv[0])
	if base == "git" {
		return len(argv) >= 2 && gitReadOnlySubcommands[argv[1]]
	}
	return readOnlyCommands[base]
}

// hasUnsafeReadOnlyOption reports whether an argument after the program name is
// one of the program's readOnlyUnsafeOptions, alone or as "option=value".
func hasUnsafeReadOnlyOption(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	unsafe := readOnlyUnsafeOptions[commandBase(argv[0])]
	for _, arg := range argv[1:] {
		for _, option := range unsafe {
			if arg == option || strings.HasPrefix(arg, option+"=") {
				return true
			}
		}
	}
	return false
}

// shellMetacharacters are the characters that compose, redirect or substitute:
// with one of them present, the command that runs is decided by a shell rather
// than by argv. They are refused in *every* argv element, including argv[0].
//
// A backslash, a quote, a tab or a space inside an element is not one of them:
// those are argument content, and a shell would have consumed them before argv
// existed. Nothing is lost by leaving them out — an escaped metacharacter is
// still the metacharacter (`\|` contains `|`).
const shellMetacharacters = "|&;<>`$\n\r"

// isSimpleCommand reports whether argv is one plain command with no composing
// or redirecting character anywhere in it.
func isSimpleCommand(argv []string) bool {
	for _, arg := range argv {
		if strings.ContainsAny(arg, shellMetacharacters) {
			return false
		}
	}
	return true
}

// commandBase returns the last path element of a program name, so "/bin/ls"
// and "ls" are the same program. A Windows-style separator is normalized too,
// because a project on Windows may pass "C:\\tools\\bin\\foo.exe".
func commandBase(program string) string {
	p := strings.TrimSpace(program)
	p = strings.ReplaceAll(p, `\`, "/")
	return path.Base(p)
}

// trustedCommand reports whether argv starts with one of the configured
// prefixes, element by element.
//
// The comparison is on argv elements, never on a joined string: the prefix
// "npm test" must not match "npm testing", and a trusted "ls" must not bless
// "lsblk". An empty prefix is skipped rather than matching everything, and the
// program name is compared by base name — exactly as classification compares
// it — so a trusted "ls" matches "/bin/ls".
func trustedCommand(argv []string, prefixes [][]string) bool {
	for _, prefix := range prefixes {
		if len(prefix) == 0 || len(prefix) > len(argv) {
			continue
		}
		match := true
		for i, want := range prefix {
			want = strings.TrimSpace(want)
			if want == "" {
				match = false
				break
			}
			got := argv[i]
			if i == 0 {
				got, want = commandBase(got), commandBase(want)
			}
			if got != want {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// isTestOrInstallRunner reports whether the program is one that runs project
// code.
func isTestOrInstallRunner(program string) bool {
	return testOrInstallRunners[commandBase(program)]
}

// normalizeToolName lowercases a tool name and drops the separators, so the
// name tables can be written once per concept instead of once per spelling.
func normalizeToolName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch r {
		case '_', '-', '.', ' ', '/', ':':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isReadOnlyToolName reports whether a normalized tool name is a reader.
func isReadOnlyToolName(name string) bool { return readOnlyToolNames[name] }

// isWriteToolName reports whether a normalized tool name is a workspace writer.
func isWriteToolName(name string) bool { return writeToolNames[name] }
