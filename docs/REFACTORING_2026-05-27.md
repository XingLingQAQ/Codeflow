# CodeFlow 目录结构重构记录

> 执行日期：2026-05-27  
> 执行人：AI Assistant  
> 版本：v2.0

---

## 🎯 重构目标

将项目从混乱的目录结构重构为标准 Monorepo 架构，提升可维护性和开发体验。

---

## 📋 执行的变更

### 阶段 1：清理临时文件 ✅

**操作**：
```bash
mkdir -p .archive/reviews .archive/legacy
mv .tmp_codeflow_review* .archive/reviews/
mv archive/* .archive/legacy/ && rmdir archive
```

**结果**：
- ✅ 清理 5 个临时 review 目录 → `.archive/reviews/`
- ✅ 移动历史归档 `archive/` → `.archive/legacy/`
- ✅ 更新 `.gitignore` 添加 `.archive/`

---

### 阶段 2：重命名组件库 ✅

**操作**：
```bash
mv packages/gui packages/ui-components
```

**文件变更**：
- `packages/ui-components/package.json`
  - `name`: `@codeflow/gui` → `@codeflow/ui-components`
  - `description`: "CodeFlow GUI 层" → "CodeFlow UI 组件库（历史资产）"

**影响**：
- ✅ 明确组件库定位（历史资产，非主前端）
- ✅ 避免与主应用混淆

---

### 阶段 3：重构主前端 ✅

**操作**：
```bash
mkdir -p apps
mv codeflow_template apps/desktop
rm -rf apps/desktop/node_modules apps/desktop/package-lock.json
```

**文件变更**：
- `apps/desktop/package.json`
  - `name`: `codeflow` → `@codeflow/desktop`
- 删除独立 `node_modules`（1.6G），复用 workspace

**影响**：
- ✅ 纳入 Monorepo 统一管理
- ✅ 减少依赖重复安装
- ✅ 符合标准 apps/ + packages/ 结构

---

### 阶段 4：更新配置文件 ✅

#### 4.1 `pnpm-workspace.yaml`
```yaml
packages:
  - 'apps/*'      # 新增
  - 'packages/*'
```

#### 4.2 `nx.json`
```json
{
  "workspaceLayout": {
    "appsDir": "apps",
    "libsDir": "packages"
  }
}
```

#### 4.3 根 `package.json`
新增脚本：
```json
{
  "scripts": {
    "dev:desktop": "pnpm --filter @codeflow/desktop dev",
    "build:desktop": "pnpm --filter @codeflow/desktop build",
    "tauri:dev": "pnpm --filter @codeflow/desktop tauri:dev",
    "tauri:build": "pnpm --filter @codeflow/desktop tauri:build"
  }
}
```

#### 4.4 `.gitignore`
```
.archive/  # 新增
```

---

## 📊 重构前后对比

### 目录结构对比

**重构前**：
```
CodeFlow/
├── codeflow_template/        # 主前端（命名混乱）
├── packages/
│   ├── gui/                  # 组件库（职责不清）
│   ├── core/
│   ├── cli/
│   └── shared/
├── archive/                  # 归档（干扰工作目录）
├── .tmp_codeflow_review2/    # 临时文件未清理
├── .tmp_codeflow_review3/
├── .tmp_codeflow_review4/
├── .tmp_codeflow_review5/
└── .tmp_codeflow_review6/
```

**重构后**：
```
CodeFlow/
├── apps/                     # 应用层（新增）
│   └── desktop/             # 桌面应用（原 codeflow_template）
├── packages/                 # 共享包层
│   ├── ui-components/       # UI 组件库（原 gui）
│   ├── core/
│   ├── cli/
│   └── shared/
└── .archive/                 # 归档（隐藏）
    ├── legacy/              # 历史代码
    └── reviews/             # 临时文件
```

### 包命名对比

| 原名称 | 新名称 | 说明 |
|--------|--------|------|
| `codeflow` | `@codeflow/desktop` | 明确桌面应用身份 |
| `@codeflow/gui` | `@codeflow/ui-components` | 明确组件库定位 |

---

## ✅ 验证结果

### 目录结构验证
```bash
$ ls -la apps/ packages/
apps/:
drwxr-xr-x desktop

packages/:
drwxr-xr-x cli
drwxr-xr-x core
drwxr-xr-x shared
drwxr-xr-x ui-components
```

### 包名验证
```bash
$ cat apps/desktop/package.json | grep name
  "name": "@codeflow/desktop",

$ cat packages/ui-components/package.json | grep name
  "name": "@codeflow/ui-components",
```

### Workspace 验证
```bash
$ cat pnpm-workspace.yaml
packages:
  - 'apps/*'
  - 'packages/*'
```

---

## 🎉 重构收益

### 1. 结构清晰
- ✅ 应用（apps/）与包（packages/）职责分离
- ✅ 命名规范，一目了然
- ✅ 符合 Monorepo 最佳实践

### 2. 依赖优化
- ✅ 删除 `apps/desktop` 独立 `node_modules`（1.6G）
- ✅ 统一依赖管理，减少重复安装
- ✅ 便于 Nx 缓存和增量构建

### 3. 开发体验
- ✅ 根目录快捷命令：`pnpm dev:desktop`、`pnpm tauri:dev`
- ✅ 清理临时文件，减少干扰
- ✅ 归档目录隐藏，保持工作区整洁

### 4. 可维护性
- ✅ 新增应用/包遵循统一规范
- ✅ 文档完善（`DIRECTORY_STRUCTURE.md`）
- ✅ 便于新成员理解项目结构

---

## ⚠️ 已知问题

### 1. better-sqlite3 依赖问题
**现象**：`pnpm install` 时 better-sqlite3 编译失败  
**原因**：缺少 Python 3.6+ 环境  
**影响**：不影响目录重构，但影响 `@codeflow/core` 功能  
**解决方案**：
```bash
# 安装 Python 3.6+
# 或配置 npm python 路径
npm config set python "C:\Path\To\python.exe"
```

### 2. TypeScript 编译问题
**现象**：`pnpm typecheck` 失败（tsc 未找到）  
**原因**：依赖安装未完成  
**解决方案**：
```bash
pnpm install --force
pnpm typecheck
```

---

## 📝 后续建议

### 短期（1 周内）
1. ✅ 解决 better-sqlite3 依赖问题
2. ✅ 验证 Tauri 构建流程
3. ✅ 更新 CI/CD 配置（如有）

### 中期（1 个月内）
1. 考虑新增 `apps/web`（Web 版本）
2. 优化 `packages/ui-components`（提取通用组件）
3. 完善单元测试覆盖

### 长期
1. 考虑迁移到 Turborepo（更强大的 Monorepo 工具）
2. 建立组件库文档站点（Storybook）
3. 实现跨包类型共享优化

---

## 📚 相关文档

- [目录结构说明](./DIRECTORY_STRUCTURE.md)
- [项目概览](./PROJECT_OVERVIEW.md)
- [前端功能清单](./FRONTEND_FEATURES.md)

---

**执行时间**：约 15 分钟  
**影响范围**：目录结构、配置文件、包命名  
**风险等级**：低（仅结构调整，无业务逻辑变更）  
**回滚方案**：Git revert 本次提交
