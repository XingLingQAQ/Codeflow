# 插件系统详细设计（Plugin System）

> 状态：Active / 目标态设计（**本切片 M6.1 落地**：贡献点注册表 + 贡献清单验证 + 回滚语义）  
> 模块：`backend/internal/plugin`（扩展现有 Service / Store / types）  
> 关联：`internal/floweng`（模板贡献点）、`internal/guard`（规则贡献点）、`internal/skill`（Skill 贡献点）、`internal/isolation`（沙箱运行时）  
> 里程碑：M6  
> 纳入跟踪：2026-07-13（M0.6）；注册表切片 2026-07-27（M6.1）

---

## 1. 设计原则

- **核心功能即贡献点**：插件系统比 VS Code 更深——模板、阶段类型、Gate 校验器、守卫规则、Skill 等核心功能本身也是贡献点，可被插件替换或扩展（roadmap §1）
- **能力范围权限**：每个插件声明所需权限（贡献点类型 + 操作范围），注册时校验清单与权限匹配；运行时由 isolation 沙箱约束
- **安全失败注册**：贡献清单以全有或全无（all-or-nothing）语义注册——若清单中第 N 项注册失败，前 N-1 项自动回滚；卸载亦原子移除所有贡献

---

## 2. 贡献点模型

类型化注册表（ContributionRegistry）管理五类贡献点，每类映射到已有后端接缝：

| 贡献类型 | ContributionType | 后端接缝 | 当前状态 |
|---|---|---|---|
| 流程模板 | `flow_template` | `floweng.ImportTemplateJSON` / `UnregisterTemplate` | ✅ 已实现 |
| 守卫规则 | `guard_rule` | `guard.Engine.Config()` + `ApplyConfig`（severity override + glob additions） | ✅ 已实现（仅 severity/globs） |
| Skill 资产 | `skill` | `skill.Registry.Create` with `SourcePlugin` + `Delete` | ✅ 已实现 |
| 阶段类型 | `stage_type` | 后置：需 `floweng` 扩展 stage dispatcher 注册 | ❌ 缺 stage type 注册器 |
| Gate 校验器 | `gate_validator` | 后置：需 `floweng` 扩展 gate evaluator 注册 | ❌ 缺 gate validator 注册器 |

### 2.1 flow_template

插件提供 JSON 格式的 `CustomTemplate`（与 `floweng.ExportTemplateJSON` 输出等价）。注册时调用 `ImportTemplateJSON`；卸载时 `UnregisterTemplate(id)`。模板 ID 须全局唯一且不得与 builtin 冲突——`RegisterTemplate` 已有此校验。

### 2.2 guard_rule

限于安全的配置层操作：severity override（将已有 RuleID 的 severity 设为 error/warn/off）和 denied/deprecated path glob 追加。通过 `ApplyConfig` 叠加；卸载时恢复注册前的 Config 快照。**不支持**注入新的规则评估逻辑——自定义评估器为后续切片。

### 2.3 skill

插件以 `SourcePlugin` 身份创建 Skill（Create with Source:"plugin"），触发 frontmatter 解析。卸载时按 `pluginID + skillID` 跟踪关系删除。

### 2.4 stage_type（后置）

需 floweng 新增 `RegisterStageType(typeName, handler)` 接口。当前 stage dispatcher 硬编码阶段类型；扩展须重构 dispatch 为注册表查找。列入 M6.2 或后续。

### 2.5 gate_validator（后置）

需 floweng 新增 `RegisterGateValidator(kind, evaluator)` 接口。当前 gate 评估逻辑在 `evaluateGates` 中硬编码 `human_approval / agent_check / auto`；扩展须重构为注册表。列入 M6.2 或后续。

---

## 3. 插件清单（Manifest）

```yaml
id: "com.example.my-plugin"
name: "My Plugin"
version: "1.0.0"
permissions:
  - flow_template
  - guard_rule
  - skill
contributions:
  flow_templates:
    - template: <JSON | path>
  guard_rules:
    - rule_id: "binary_exec_write"
      severity: "error"
    - denied_path_globs: ["**/*.bak"]
    - deprecated_path_globs: ["legacy/**"]
  skills:
    - name: "Plugin Skill"
      body: "Skill body markdown"
      triggers: ["plugin", "example"]
```

`ContributionManifest`（Go 结构体）是清单中 `contributions` 部分的类型化表示，由 `RegisterContributions` 消费。

---

## 4. 生命周期

```
discover → validate → register → enable/disable → unregister
```

1. **Discover**：扫描插件目录或从市场获取清单（现有 `Service.ListMarketplace`）
2. **Validate**：校验清单完整性（id/name/version 非空）、权限声明与贡献类型匹配、贡献条目基础格式
3. **Register**：`ContributionRegistry.RegisterContributions(pluginID, manifest)` 逐条应用，全有或全无
4. **Enable/Disable**：通过现有 `Service.Toggle` 控制运行时活跃状态（hook 层面）
5. **Unregister**：`ContributionRegistry.UnregisterContributions(pluginID)` 原子回滚所有已注册贡献

**回滚语义**：注册过程中，已成功应用的贡献记录在 `appliedContributions[pluginID]` 中。若后续条目失败，按倒序调用每条贡献的 undo 操作（UnregisterTemplate / Config restore / skill Delete）。

---

## 5. 沙箱与信任

- **isolation 集成**：`internal/isolation` 提供进程/WASM 级沙箱运行时（M6.2）。贡献注册在沙箱外执行（声明式），但插件的运行时行为（hook handler、自定义 gate evaluator）须在沙箱内执行
- **签名验证**：现有 `integration.Signature` 已携带算法、签名值和验证标志。`配置数字签名`（agent-quality-system.md §4）为后续增强——本切片信任已验证签名的 `Verified=true` 标志
- **权限执行**：注册时校验清单声明的 `permissions` 覆盖所有 `contributions` 中使用的类型；未声明的类型拒绝注册

---

## 6. API 面草案

本切片为域层（storage + interface），API handler 由后续切片添加。预期路由：

| 方法 | 路由 | 说明 |
|---|---|---|
| POST | `/api/v1/plugins/:id/contributions` | 注册贡献清单 |
| DELETE | `/api/v1/plugins/:id/contributions` | 卸载全部贡献 |
| GET | `/api/v1/plugins/:id/contributions` | 列出已注册贡献 |

现有 `/api/v1/plugins` CRUD / marketplace / toggle 路由不变。

---

## 7. M6 切片范围

### M6.1（本切片）

- `ContributionRegistry` + `ContributionManifest` 类型
- `RegisterContributions` / `UnregisterContributions` with all-or-nothing semantics
- 三类贡献实现：`flow_template`、`guard_rule`（severity + globs）、`skill`
- In-memory applied-contributions tracking per plugin
- 测试：round-trip、rollback、double-register 拒绝、unknown type

### 后续切片

- M6.2：`stage_type` + `gate_validator` 贡献点（需 floweng dispatcher 重构）
- M6.2：isolation 沙箱运行时强化（hook handler 在沙箱内执行）
- M6.3：Live Preview + 检查器桥（workspace + guard 联动）
- M6.4：插件市场 UI + 数字签名验证链
- SQLite 持久化 applied-contributions（本切片 in-memory）
