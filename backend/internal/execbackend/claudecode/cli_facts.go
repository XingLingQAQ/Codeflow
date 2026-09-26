package claudecode

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// 版本
// ---------------------------------------------------------------------------

// versionLine 只接受 `X.Y.Z (Claude Code)`。
//
// beta/预发布后缀、缺厂商后缀、品牌名在前（`Claude Code 2.1.283`）一律视为不可解析：
// §27.7 要求能力证据绑定到确切的版本行，不能靠"大概是这个版本"推断能力。
var versionLine = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+) \(Claude Code\)$`)

// ErrVersionUnparsable 表示 `claude --version` 输出不是受支持的格式。
var ErrVersionUnparsable = errors.New("claudecode: version output is not `X.Y.Z (Claude Code)`")

// Version 是解析后的 CLI 版本号。
//
// 零值表示"未知"：Valid 为 false，String 返回空串，Compare 视作低于任何有效版本，
// 因此未解析出版本时任何能力都不会被证明（fail-closed）。
type Version struct {
	major int
	minor int
	patch int
	ok    bool
}

// NewVersion 构造一个有效版本；调用方（含测试）应通过它而不是结构体字面量，
// 因为 Version 依赖内部有效性标记区分"0.0.0"与"未知"。
func NewVersion(major, minor, patch int) Version {
	return Version{major: major, minor: minor, patch: patch, ok: true}
}

// ParseVersion 解析 `claude --version` 的标准输出。
//
// 只接受去掉首尾空白后严格匹配 `^\d+\.\d+\.\d+ \(Claude Code\)$` 的整行；
// 其余输入返回包装了 ErrVersionUnparsable 的错误（错误里只带脱敏截断片段）。
func ParseVersion(stdout string) (Version, error) {
	line := strings.TrimSpace(stdout)
	m := versionLine.FindStringSubmatch(line)
	if m == nil {
		return Version{}, fmt.Errorf("%w (got %s)", ErrVersionUnparsable, quoteSnippet(line))
	}
	major, err1 := strconv.Atoi(m[1])
	minor, err2 := strconv.Atoi(m[2])
	patch, err3 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return Version{}, fmt.Errorf("%w (numeric overflow)", ErrVersionUnparsable)
	}
	return NewVersion(major, minor, patch), nil
}

// Valid 报告版本是否由 ParseVersion/NewVersion 得到。
func (v Version) Valid() bool { return v.ok }

// Major 返回主版本号；无效版本返回 0。
func (v Version) Major() int { return v.major }

// Minor 返回次版本号；无效版本返回 0。
func (v Version) Minor() int { return v.minor }

// Patch 返回修订号；无效版本返回 0。
func (v Version) Patch() int { return v.patch }

// String 返回 `X.Y.Z`；无效版本返回空串（空串表示"未知"，不表示无限制）。
func (v Version) String() string {
	if !v.ok {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

// Compare 按 major/minor/patch 比较：v 小于、等于、大于 o 时分别返回 -1、0、1。
//
// 无效版本（零值）视为最低版本，因此永远不会被判为"达到最低要求"。
func (v Version) Compare(o Version) int {
	switch {
	case v.major != o.major:
		return compareInt(v.major, o.major)
	case v.minor != o.minor:
		return compareInt(v.minor, o.minor)
	default:
		return compareInt(v.patch, o.patch)
	}
}

// AtLeast 报告 v 是否 ≥ o（含端点）。
//
// 边界按 ≥ 处理：MinSupportedVersion 本身是支持的（文档写的是 "requires v2.1.259
// or later"），把边界判成不支持会导致能力被无理由关掉。
func (v Version) AtLeast(o Version) bool { return v.Compare(o) >= 0 }

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// help 解析
// ---------------------------------------------------------------------------

// ansiEscape 匹配 CSI 转义序列（如 \x1b[36m、\x1b[1;32m、\x1b[0m）。
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// flagName 是 help 里合法 flag 名的形状：`-x` 或 `--long-name`（含驼峰别名）。
var flagName = regexp.MustCompile(`^-{1,2}[A-Za-z][A-Za-z0-9_-]*$`)

// ErrHelpUnparsable 表示 `claude --help` 输出里没有可用的 Options 段。
var ErrHelpUnparsable = errors.New("claudecode: help output has no usable `Options:` section")

// FlagFact 是 `claude --help` 里一条 flag 条目解析后的事实。
type FlagFact struct {
	// Long 长别名（保留 "--" 前缀），按 help 中出现顺序；可空。
	Long []string
	// Short 短别名（保留 "-" 前缀），按 help 中出现顺序；可空。
	Short []string
	// Value 取值占位符原文（如 "<format>"、"<directories...>"）；空串表示布尔开关。
	// 多个别名时取最后一个别名上的占位符（commander 的排版把取值写在末尾）。
	Value string
	// Choices 该 flag 声明的 (choices: "a", "b") 列表；未声明时为 nil。
	Choices []string
}

// HasName 报告 fact 是否含该别名（需原样带前缀，如 "--allowed-tools" 或 "-p"）。
func (f FlagFact) HasName(name string) bool {
	for _, n := range f.Long {
		if n == name {
			return true
		}
	}
	for _, n := range f.Short {
		if n == name {
			return true
		}
	}
	return false
}

// HasChoice 报告该 flag 是否声明了取值 choice。
func (f FlagFact) HasChoice(choice string) bool {
	for _, c := range f.Choices {
		if c == choice {
			return true
		}
	}
	return false
}

// primaryName 返回条目主名（第一个长别名，否则第一个短别名）；无别名时返回空串。
func (f FlagFact) primaryName() string {
	if len(f.Long) > 0 {
		return f.Long[0]
	}
	if len(f.Short) > 0 {
		return f.Short[0]
	}
	return ""
}

// HelpFlags 是 `claude --help` 的 Options 段解析结果。
//
// Flags 以主名（第一个长别名）为 key；index 记录全部别名到主名的映射，使 Lookup
// 既能用 "--print" 也能用 "-p" 命中。零值可用：所有查询返回"不存在"。
type HelpFlags struct {
	Flags map[string]FlagFact
	Order []string
	index map[string]string
}

// ParseHelp 解析 `claude --help` 的输出，只取 `Options:` 段。
//
// 设计要点：
//   - 先把 \r\n 归一成 \n（仓库 core.autocrlf=true，干净检出里的 fixture 是 CRLF），
//     再去掉 ANSI 转义。这两步必须在结构识别之前完成：行尾残留的 \r 会混进 flag
//     取值与 (choices: ...) 文本，静默改变解析结果。本文件因此只做"去空格与制表符"
//     的裁剪（trimSpaces），故意不吞掉 \r，让漏归一化立即暴露成解析差异。
//   - 段边界只看顶格行：`Options:` 开始，下一个顶格非空行（`Commands:`）结束。这样
//     `update|upgrade` 这类子命令不会被当成 flag。
//   - 条目行 = 行首恰好两个空格且其后紧跟 '-'；其余缩进行是该条目的续行，拼接后
//     提取 (choices: ...)，因此跨行的 "--permission-mode" choices 也能解析完整。
func ParseHelp(stdout string) (HelpFlags, error) {
	text := ansiEscape.ReplaceAllString(normalizeNewlines(stdout), "")
	body, ok := sectionLines(text, "Options:")
	if !ok {
		return HelpFlags{}, fmt.Errorf("%w (no `Options:` line)", ErrHelpUnparsable)
	}
	out := HelpFlags{Flags: map[string]FlagFact{}, index: map[string]string{}}
	for _, entry := range parseHelpEntries(body) {
		fact, ok := parseFlagEntry(entry.spec)
		if !ok {
			continue
		}
		fact.Choices = extractChoices(strings.Join(entry.parts, " "))
		primary := fact.primaryName()
		if primary == "" {
			continue
		}
		out.Flags[primary] = fact
		out.Order = append(out.Order, primary)
		for _, n := range supportedNames(fact) {
			out.index[n] = primary
		}
	}
	if len(out.Order) == 0 {
		return HelpFlags{}, fmt.Errorf("%w (no flag entries)", ErrHelpUnparsable)
	}
	return out, nil
}

// Lookup 按别名查 flag 事实，长名与短别名都可以。
func (h HelpFlags) Lookup(name string) (FlagFact, bool) {
	key, ok := h.lookupKey(name)
	if !ok {
		return FlagFact{}, false
	}
	return h.Flags[key], true
}

// Has 报告 help 里是否存在该 flag（任意别名）。
func (h HelpFlags) Has(name string) bool {
	_, ok := h.lookupKey(name)
	return ok
}

// HasChoice 报告 flag 是否存在且声明了该 choice；flag 不存在时返回 false。
func (h HelpFlags) HasChoice(name, choice string) bool {
	fact, ok := h.Lookup(name)
	if !ok {
		return false
	}
	return fact.HasChoice(choice)
}

// Names 返回主名列表的副本，按 help 中出现顺序。
func (h HelpFlags) Names() []string { return append([]string(nil), h.Order...) }

func (h HelpFlags) lookupKey(name string) (string, bool) {
	if key, ok := h.index[name]; ok {
		return key, true
	}
	if _, ok := h.Flags[name]; ok {
		return name, true
	}
	return "", false
}

func supportedNames(f FlagFact) []string {
	names := make([]string, 0, len(f.Long)+len(f.Short))
	names = append(names, f.Long...)
	names = append(names, f.Short...)
	return names
}

// helpEntry 是一条 flag 条目：flag 列原文 + 描述行（含续行）。
type helpEntry struct {
	spec  string
	parts []string
}

// sectionLines 返回 header（顶格行，含冒号）到下一个顶格非空行之间的内容行。
func sectionLines(text, header string) ([]string, bool) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if !isTopLevel(line) {
			continue
		}
		if trimSpaces(line) == header {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if isTopLevel(lines[i]) && trimSpaces(lines[i]) != "" {
			end = i
			break
		}
	}
	return lines[start+1 : end], true
}

// isTopLevel 报告一行是否顶格（段头、Usage 行、Commands 段头都属于此类）。
func isTopLevel(line string) bool {
	return line != "" && line[0] != ' ' && line[0] != '\t'
}

// parseHelpEntries 把 Options 段切成条目：条目行开新条目，缩进行并入当前条目。
//
// 段首、没有归属条目的缩进行（例如 Options 段前的说明）会被忽略。
func parseHelpEntries(body []string) []helpEntry {
	var entries []helpEntry
	for _, line := range body {
		if spec, desc, ok := splitEntryLine(line); ok {
			entry := helpEntry{spec: spec}
			if desc != "" {
				entry.parts = append(entry.parts, desc)
			}
			entries = append(entries, entry)
			continue
		}
		if len(entries) == 0 {
			continue
		}
		cont := trimSpaces(line)
		if cont == "" {
			continue
		}
		last := &entries[len(entries)-1]
		last.parts = append(last.parts, cont)
	}
	return entries
}

// splitEntryLine 判断一行是不是新的 flag 条目。
//
// 判据：行首恰好两个空格（第 3 列不是空白），其后紧跟 '-'，且 flag 列本身能解析成
// 合法 flag 名。任何一条不满足就当作续行，避免把缩进的描述行误判成 flag。
func splitEntryLine(line string) (spec, desc string, ok bool) {
	if len(line) < 3 || line[0] != ' ' || line[1] != ' ' || line[2] == ' ' || line[2] == '\t' {
		return "", "", false
	}
	rest := line[2:]
	if !strings.HasPrefix(rest, "-") {
		return "", "", false
	}
	spec = rest
	if i := firstSpaceRun(rest); i >= 0 {
		spec = trimSpaces(rest[:i])
		desc = trimSpaces(rest[i:])
	}
	if !looksLikeFlagSpec(spec) {
		return "", "", false
	}
	return spec, desc, true
}

// looksLikeFlagSpec 报告逗号分隔的每个片段是否都是合法 flag 名。
func looksLikeFlagSpec(spec string) bool {
	seen := false
	for _, token := range strings.Split(spec, ",") {
		token = trimSpaces(token)
		if token == "" {
			continue
		}
		name, _ := splitFlagToken(token)
		if !flagName.MatchString(name) {
			return false
		}
		seen = true
	}
	return seen
}

// parseFlagEntry 解析 flag 列：`--allowedTools, --allowed-tools <tools...>` 这类
// 多别名写法会得到两个别名，取值占位符记在最后一个别名上。
func parseFlagEntry(spec string) (FlagFact, bool) {
	var fact FlagFact
	for _, token := range strings.Split(spec, ",") {
		token = trimSpaces(token)
		if token == "" {
			continue
		}
		name, value := splitFlagToken(token)
		if !flagName.MatchString(name) {
			return FlagFact{}, false
		}
		if strings.HasPrefix(name, "--") {
			fact.Long = append(fact.Long, name)
		} else {
			fact.Short = append(fact.Short, name)
		}
		if value != "" {
			fact.Value = value
		}
	}
	if fact.primaryName() == "" {
		return FlagFact{}, false
	}
	return fact, true
}

// splitFlagToken 把 `--name <value>` 拆成 ("--name", "<value>")。
func splitFlagToken(token string) (name, value string) {
	if i := strings.IndexAny(token, " \t"); i >= 0 {
		return token[:i], trimSpaces(token[i:])
	}
	return token, ""
}

// extractChoices 从拼好的描述里提取 (choices: "a", "b", ...) 的取值列表。
//
// 只接受整段就是双引号字符串的逗号片段，因此 `default: "host"`、`preset: "true"`
// 这类附加说明不会被误当成 choice；取到第一个 ')' 为止。
func extractChoices(desc string) []string {
	const marker = "(choices:"
	i := strings.Index(desc, marker)
	if i < 0 {
		return nil
	}
	rest := desc[i+len(marker):]
	if j := strings.Index(rest, ")"); j >= 0 {
		rest = rest[:j]
	}
	var choices []string
	for _, token := range strings.Split(rest, ",") {
		token = trimSpaces(token)
		if len(token) < 2 || token[0] != '"' || token[len(token)-1] != '"' {
			continue
		}
		body := token[1 : len(token)-1]
		if strings.ContainsAny(body, `"`) {
			continue
		}
		choices = append(choices, body)
	}
	return choices
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

// normalizeNewlines 把 CRLF 归一成 LF。
//
// 这是解析的第一步，不可省略：仓库 core.autocrlf=true，干净检出里的 fixture 是 CRLF。
func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// trimSpaces 只去空格与制表符。
//
// 故意不使用 strings.TrimSpace：它会连 \r 一起去掉，从而掩盖"忘了归一化换行"的缺陷，
// 让 CRLF 泄漏只在别处间接暴露。换行归一化统一由 normalizeNewlines 负责。
func trimSpaces(s string) string { return strings.Trim(s, " \t") }

// firstSpaceRun 返回第一个长度 ≥ 2 的空格/制表符连续段的起始下标；没有则返回 -1。
func firstSpaceRun(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if isSpaceOrTab(s[i]) && isSpaceOrTab(s[i+1]) {
			return i
		}
	}
	return -1
}

func isSpaceOrTab(b byte) bool { return b == ' ' || b == '\t' }

// quoteSnippet 把任意输出片段压成单行并截断，用于错误信息，避免回显大段原文。
func quoteSnippet(s string) string {
	const max = 60
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			b.WriteByte('?')
			continue
		}
		b.WriteByte(c)
		if b.Len() >= max {
			b.WriteString("...")
			break
		}
	}
	if b.Len() == 0 {
		return "<empty>"
	}
	return b.String()
}
