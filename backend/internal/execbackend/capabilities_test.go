package execbackend

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 本文件覆盖 CapabilityReport 的证据语义与 CheckRequired（§27.7）：
// 缺证据的 capability=false；未证明支持的能力调用返回 capability_unavailable，
// Missing 列出缺失项；errors.Is/errors.As 均可用。

// TestCapabilityReportHasRequiresNonEmptyEvidence 缺证据或空证据一律 false。
func TestCapabilityReportHasRequiresNonEmptyEvidence(t *testing.T) {
	report := CapabilityReport{
		Backend:        "claude-code",
		ExecutablePath: `C:\tools\claude.exe`,
		Version:        "1.2.3",
		Source:         "help output: claude --help",
		ProbedAt:       time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC),
		Evidence: map[Capability]string{
			CapabilityNonInteractive: "help output: --print",
			CapabilityJSONStream:     "help output: --output-format stream-json",
			CapabilityPTY:            "",    // 显式空串：未证明
			CapabilitySandbox:        "   ", // 空白：未证明
		},
	}

	cases := []struct {
		name string
		cap  Capability
		want bool
	}{
		{"有非空证据", CapabilityNonInteractive, true},
		{"有非空证据（第二项）", CapabilityJSONStream, true},
		{"Evidence 里显式空串", CapabilityPTY, false},
		{"Evidence 里空白串", CapabilitySandbox, false},
		{"Evidence 缺 key", CapabilityMCP, false},
		{"完全未知能力", Capability("teleport"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := report.Has(tc.cap); got != tc.want {
				t.Errorf("Has(%q) = %v, want %v", tc.cap, got, tc.want)
			}
		})
	}
}

// TestCapabilityReportHasRequiresSource §27.7：能力证据必须有来源（文档/本机 help
// 输出）。Source 为空的报告不得声称任何能力已证明。
func TestCapabilityReportHasRequiresSource(t *testing.T) {
	report := CapabilityReport{
		Backend:  "custom",
		Evidence: map[Capability]string{CapabilityNonInteractive: "self-declared"},
	}
	if report.Has(CapabilityNonInteractive) {
		t.Fatal("Source 为空时不得声称能力已证明")
	}
	if got := report.EvidenceFor(CapabilityNonInteractive); got != "" {
		t.Errorf("EvidenceFor 无来源时应返回空串, got %q", got)
	}

	report.Source = "docs: https://example.invalid/backend"
	if !report.Has(CapabilityNonInteractive) {
		t.Fatal("补齐来源后应承认该能力")
	}
	if got := report.EvidenceFor(CapabilityNonInteractive); got != "self-declared" {
		t.Errorf("EvidenceFor = %q, want self-declared", got)
	}
}

// TestCapabilityReportNilAndZeroValue 零值与 nil 报告等价：一切能力都未证明。
func TestCapabilityReportNilAndZeroValue(t *testing.T) {
	var nilReport CapabilityReport
	for _, c := range AllCapabilities() {
		if nilReport.Has(c) {
			t.Errorf("零值报告 Has(%q) = true, want false", c)
		}
	}
	required := []Capability{CapabilityNonInteractive, CapabilityMCP}
	if got := nilReport.Missing(required); !reflect.DeepEqual(got, required) {
		t.Errorf("零值报告 Missing = %v, want %v", got, required)
	}
	if err := CheckRequired(nilReport, required); err == nil {
		t.Fatal("零值报告不满足任何能力要求，应返回错误")
	}
}

// TestCapabilityReportMissingAndSupported 顺序保持、空输入返回 nil。
func TestCapabilityReportMissingAndSupported(t *testing.T) {
	report := CapabilityReport{
		Source: "help output: --help",
		Evidence: map[Capability]string{
			CapabilityNonInteractive: "help output: --print",
			CapabilityUsageTokens:    "docs: usage section",
		},
	}
	required := []Capability{
		CapabilityMCP,
		CapabilityNonInteractive,
		CapabilityPTY,
		CapabilityUsageTokens,
	}

	wantMissing := []Capability{CapabilityMCP, CapabilityPTY}
	if got := report.Missing(required); !reflect.DeepEqual(got, wantMissing) {
		t.Errorf("Missing = %v, want %v", got, wantMissing)
	}
	wantSupported := []Capability{CapabilityNonInteractive, CapabilityUsageTokens}
	if got := report.Supported(required); !reflect.DeepEqual(got, wantSupported) {
		t.Errorf("Supported = %v, want %v", got, wantSupported)
	}
	if got := report.Missing(nil); got != nil {
		t.Errorf("Missing(nil) = %v, want nil", got)
	}
	if got := report.Supported(nil); got != nil {
		t.Errorf("Supported(nil) = %v, want nil", got)
	}
}

// TestCheckRequiredMissingTwo 缺两项时错误列出两项，且 errors.Is/As 均可用。
func TestCheckRequiredMissingTwo(t *testing.T) {
	report := CapabilityReport{
		Source:   "help output: --help",
		Evidence: map[Capability]string{CapabilityNonInteractive: "help output: --print"},
	}
	required := []Capability{CapabilityNonInteractive, CapabilityApprovalHook, CapabilityPTY}

	err := CheckRequired(report, required)
	if err == nil {
		t.Fatal("缺两项能力应返回错误")
	}

	var domainErr *Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("错误类型 = %T, want *execbackend.Error", err)
	}
	if domainErr.Code != CodeCapabilityUnavailable {
		t.Errorf("Code = %q, want %q", domainErr.Code, CodeCapabilityUnavailable)
	}
	wantMissing := []Capability{CapabilityApprovalHook, CapabilityPTY}
	if !reflect.DeepEqual(domainErr.Missing, wantMissing) {
		t.Errorf("Missing = %v, want %v", domainErr.Missing, wantMissing)
	}
	if !errors.Is(err, ErrCapabilityUnavailable) {
		t.Error("errors.Is(err, ErrCapabilityUnavailable) = false, want true")
	}
	if errors.Is(err, ErrInvalidRequest) {
		t.Error("capability_unavailable 不应匹配 invalid_request 哨兵")
	}
	// 多缺失时 Capability 单值字段保持空（避免只报一项误导调用方）。
	if domainErr.Capability != "" {
		t.Errorf("多缺失时 Capability = %q, want 空", domainErr.Capability)
	}
}

// TestCheckRequiredMissingSingle 单缺失时同时填 Capability 字段，便于 UI 直接提示。
func TestCheckRequiredMissingSingle(t *testing.T) {
	report := CapabilityReport{
		Source:   "help output: --help",
		Evidence: map[Capability]string{CapabilityNonInteractive: "help output: --print"},
	}
	err := CheckRequired(report, []Capability{CapabilityNonInteractive, CapabilityResumeCheckpoint})

	var domainErr *Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("错误类型 = %T, want *execbackend.Error", err)
	}
	if domainErr.Capability != CapabilityResumeCheckpoint {
		t.Errorf("Capability = %q, want %q", domainErr.Capability, CapabilityResumeCheckpoint)
	}
	if !reflect.DeepEqual(domainErr.Missing, []Capability{CapabilityResumeCheckpoint}) {
		t.Errorf("Missing = %v, want [resume_checkpoint]", domainErr.Missing)
	}
	if !strings.Contains(domainErr.Error(), "resume_checkpoint") {
		t.Errorf("Error() 应包含缺失能力名: %q", domainErr.Error())
	}
}

// TestCheckRequiredSatisfied 全部满足返回 nil，包括空 required。
func TestCheckRequiredSatisfied(t *testing.T) {
	report := CapabilityReport{
		Source: "help output: --help",
		Evidence: map[Capability]string{
			CapabilityNonInteractive: "help output: --print",
			CapabilityJSONStream:     "help output: --output-format stream-json",
		},
	}
	if err := CheckRequired(report, []Capability{CapabilityNonInteractive, CapabilityJSONStream}); err != nil {
		t.Fatalf("全部满足应返回 nil, got %v", err)
	}
	if err := CheckRequired(report, nil); err != nil {
		t.Fatalf("空 required 应返回 nil, got %v", err)
	}
}

// TestCheckRequiredDeduplicatesMissing required 里重复的能力只在 Missing 里出现一次
// （StartRequest.Validate 已拒绝重复 required，但 CheckRequired 被各适配器直接复用，
// 不能因为上游漏校验就返回重复项）。
func TestCheckRequiredDeduplicatesMissing(t *testing.T) {
	report := CapabilityReport{Source: "help output: --help"}
	err := CheckRequired(report, []Capability{CapabilityPTY, CapabilityMCP, CapabilityPTY})

	var domainErr *Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("错误类型 = %T, want *execbackend.Error", err)
	}
	wantMissing := []Capability{CapabilityPTY, CapabilityMCP}
	if !reflect.DeepEqual(domainErr.Missing, wantMissing) {
		t.Errorf("Missing = %v, want %v（去重且保持首次出现顺序）", domainErr.Missing, wantMissing)
	}
}

// TestCheckRequiredUnknownCapability 未知枚举值一律视为缺失，不会被当成已支持。
func TestCheckRequiredUnknownCapability(t *testing.T) {
	report := CapabilityReport{
		Source:   "help output: --help",
		Evidence: map[Capability]string{Capability("teleport"): "wishful thinking"},
	}
	err := CheckRequired(report, []Capability{Capability("teleport")})
	var domainErr *Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("未知能力应被当作缺失并返回错误, got %v", err)
	}
	if domainErr.Code != CodeCapabilityUnavailable {
		t.Errorf("Code = %q, want %q", domainErr.Code, CodeCapabilityUnavailable)
	}
}

// TestNewCapabilityUnavailableNoArgs 无参数构造（"该后端整体未证明支持该操作"）合法。
func TestNewCapabilityUnavailableNoArgs(t *testing.T) {
	err := NewCapabilityUnavailable()
	if err.Code != CodeCapabilityUnavailable {
		t.Errorf("Code = %q, want %q", err.Code, CodeCapabilityUnavailable)
	}
	if len(err.Missing) != 0 {
		t.Errorf("Missing = %v, want 空", err.Missing)
	}
	if !errors.Is(err, ErrCapabilityUnavailable) {
		t.Error("errors.Is(err, ErrCapabilityUnavailable) = false, want true")
	}
}
