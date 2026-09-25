package execbackend

import (
	"strings"
	"time"
)

// CapabilityReport 是一次只读能力探测的结果（§27.7）。
//
// 关键规则：
//   - 只记录探测得到的事实；不发送模型请求、不自动登录/安装、不回显凭据。
//   - 缺证据的能力一律为 false：Has 只承认 Evidence 中的非空条目，
//     Source 为空时任何能力都不算已证明。
//   - 每个已证明的能力都必须在 Evidence 里留下证据来源（文档、本机 help 输出等），
//     这样 UI 禁用/启用功能与后续复核都有依据。
//   - ExecutablePath 必须是绝对路径；Version/Source 是探测事实，可为空（表示未知，
//     不表示无限制）。
type CapabilityReport struct {
	// Backend 后端标识（与 Backend.Name 一致）。
	Backend string `json:"backend"`
	// ExecutablePath 被探测可执行文件的绝对路径；空表示尚未定位到。
	ExecutablePath string `json:"executable_path,omitempty"`
	// Version 探测到的版本字符串；空表示未知。
	Version string `json:"version,omitempty"`
	// Source 探测来源说明（如 "docs: <url>"、"help output: <cmdline>"），
	// 用于复核能力证据的确切出处。
	Source string `json:"source,omitempty"`
	// ProbedAt 探测时间。
	ProbedAt time.Time `json:"probed_at"`
	// Evidence 逐能力证据：key 为能力，value 为非空证据字符串才代表"已证明支持"。
	// 显式写入空串或缺失 key 都表示该能力未获证明（capability=false）。
	Evidence map[Capability]string `json:"evidence,omitempty"`
}

// Has 报告能力 c 是否已被证据证明。
//
// 只有三个条件同时成立才返回 true：c 是已知枚举值、Source 非空、Evidence[c] 为非空
// 字符串。只写一个空证据或只在接口里声明能力都不算交付（§27.7）；枚举外的值一律
// 视为未证明，避免某个后端凭空声称一个协议里不存在的能力。
func (r CapabilityReport) Has(c Capability) bool {
	if !c.Valid() {
		return false
	}
	if strings.TrimSpace(r.Source) == "" {
		return false
	}
	return strings.TrimSpace(r.Evidence[c]) != ""
}

// EvidenceFor 返回能力 c 的证据字符串；没有证据时返回空串。
func (r CapabilityReport) EvidenceFor(c Capability) string {
	if !r.Has(c) {
		return ""
	}
	return r.Evidence[c]
}

// Missing 按输入顺序返回 required 中未获证明的能力。
//
// 输入为空时返回 nil（表示无缺失）；nil 接收者的行为与空报告一致（全部缺失）。
// 未知枚举值一律视为缺失。
func (r CapabilityReport) Missing(required []Capability) []Capability {
	var missing []Capability
	for _, c := range required {
		if !r.Has(c) {
			missing = append(missing, c)
		}
	}
	return missing
}

// Supported 按输入顺序返回 required 中已获证明的能力，供 UI 展示"可用项"。
func (r CapabilityReport) Supported(required []Capability) []Capability {
	var supported []Capability
	for _, c := range required {
		if r.Has(c) {
			supported = append(supported, c)
		}
	}
	return supported
}

// CheckRequired 校验 required 中的能力是否都被 report 证明。
//
// 全部满足时返回 nil；有缺失时返回 Code=capability_unavailable 的 *Error，
// Missing 按 required 顺序列出缺失项（去重后仍保持首次出现顺序），
// errors.Is(err, ErrCapabilityUnavailable) 为真。
//
// 该函数是各适配器 Prepare 的共用实现，保证 fake 与真实 adapter 的失败 code 与
// Missing 列表一致。
func CheckRequired(report CapabilityReport, required []Capability) error {
	missing := dedupeCapabilities(report.Missing(required))
	if len(missing) == 0 {
		return nil
	}
	return NewCapabilityUnavailable(missing...)
}

// dedupeCapabilities 按首次出现顺序去重；输入为 nil 时返回 nil。
func dedupeCapabilities(caps []Capability) []Capability {
	if len(caps) == 0 {
		return nil
	}
	seen := make(map[Capability]struct{}, len(caps))
	var out []Capability
	for _, c := range caps {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}
