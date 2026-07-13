# CodeFlow 目录结构说明

> 最后更新：2026-07-13  
> 版本：2.1（M0.6 docs IA）

## 📁 项目结构概览

```
CodeFlow/
├── apps/                      # 应用层（Monorepo Apps）
│   └── workbench/            # 工作台应用（React + Tauri；G01 自 desktop 更名）
│       ├── src/              # 前端源码
│       ├── src-tauri/        # Tauri 配置与 Rust 代码
│       ├── components/       # UI 组件
│       ├── hooks/            # React Hooks
│       ├── adapters/         # 适配器
│       └── package.json      # @codeflow/workbench
│
├── packages/                  # 共享包层（Monorepo Packages）
│   ├── core/                 # 核心逻辑层
│   │   ├── src/
│   │   │   ├── adapters/    # LLM 适配器（Claude/Gemini/Codex）
│   │   │   ├── hooks/       # Hook 事件总线
│   │   │   ├── memory/      # 向量化记忆
│   │   │   ├── samg/        # 结构化原子图谱
│   │   │   ├── commander/   # 指挥官模式
│   │   │   ├── config/      # 配置管理
│   │   │   ├── storage/     # 存储抽象
│   │   │   └── ...          # 20+ 核心模块
│   │   └── package.json     # @codeflow/core
│   │
│   ├── cli/                  # CLI 工具
│   │   ├── src/
│   │   └── package.json     # @codeflow/cli
│   │
│   ├── ui-components/        # UI 组件库（历史资产）
│   │   ├── src/
│   │   └── package.json     # @codeflow/ui-components
│   │
│   └── shared/               # 共享工具库
│       ├── src/
│       └── package.json     # @codeflow/shared
│
├── backend/                   # Go 后端服务
│   ├── cmd/
│   │   └── codeflow-server/ # 主入口
│   ├── internal/             # 内部模块（35+ 子模块）
│   │   ├── adapters/        # LLM 适配器
│   │   ├── api/             # RESTful API
│   │   ├── hooks/           # Hook 系统
│   │   ├── memory/          # 向量存储
│   │   ├── commander/       # 指挥官模式
│   │   ├── blackboard/      # 黑板协作
│   │   ├── websocket/       # WebSocket 服务
│   │   └── ...
│   ├── pkg/                  # 公共库
│   ├── go.mod
│   └── Makefile
│
├── docs/                      # 文档（IA 见 docs/README.md）
│   ├── README.md             # 文档索引与三分法约定
│   ├── adr/                  # 架构决策记录
│   ├── design/               # 设计正文 + early/ 历史
│   ├── plans/                # 实施计划
│   ├── requirements/         # 需求文档
│   ├── frontend-mainline-decision.md
│   ├── FRONTEND_FEATURES.md
│   ├── PROJECT_OVERVIEW.md
│   └── DIRECTORY_STRUCTURE.md # 本文件
│
├── scripts/                   # 构建与工具脚本
│   ├── test-all-e2e.mjs
│   ├── check-repo-hygiene.mjs
│   ├── smoke-embed.mjs
│   ├── check-garbled-comments.mjs
│   ├── check-api-contracts.mjs
│   └── generate-openapi-types.mjs
│
├── issues/                    # Issue 管理（CSV 格式）
├── outputs/                   # 构建输出
│
├── package.json               # 根 package.json（Monorepo 配置）
├── pnpm-workspace.yaml        # pnpm Workspace 配置
├── nx.json                    # Nx 构建配置
├── tsconfig.base.json         # TypeScript 基础配置
└── README.md                  # 项目说明
```

> **M0.6 起**：根目录 `plan/` 与 `archive/docs/early-design/` 已清空迁入 `docs/`；`backend/docs/` 仅保留 `openapi.yaml`（+ README 指针）。

---

## 🎯 目录职责说明

### 应用层（apps/）

**apps/workbench** - 工作台应用（React + Tauri）
- **技术栈**：React 19 + TypeScript 5.8 + Tauri 2 + Vite 6
- **职责**：CodeFlow 桌面客户端，支持浏览器与 Tauri 双模式
- **包名**：`@codeflow/workbench`
- **启动命令**：
  ```bash
  pnpm dev:desktop      # Web 开发模式
  pnpm tauri:dev        # Tauri 开发模式
  pnpm tauri:build      # 构建桌面应用
  ```

---

### 共享包层（packages/）

#### packages/core - 核心逻辑层
- **职责**：CodeFlow 核心业务逻辑，所有应用共享
- **包名**：`@codeflow/core`
- **主要模块**：
  - `adapters/` - LLM 适配器（Claude/Gemini/Codex）
  - `hooks/` - Hook 事件总线
  - `memory/` - 向量化记忆（Episodic Memory）
  - `samg/` - 结构化原子图谱（Semantic Memory）
  - `commander/` - 指挥官模式（多智能体协作）
  - `config/` - 多层级配置管理
  - `storage/` - 存储抽象层
  - `retriever/` - 混合检索引擎
  - `hotswap/` - 模型热切换
  - `isolation/` - 沙箱隔离
  - `privacy/` - 隐私保护与加密
  - `audit/` - 审计日志

#### packages/cli - 命令行工具
- **职责**：CodeFlow CLI 工具
- **包名**：`@codeflow/cli`
- **依赖**：`@codeflow/core`、`commander`

#### packages/ui-components - UI 组件库（历史资产）
- **职责**：历史 GUI 组件库，不再作为默认前端
- **包名**：`@codeflow/ui-components`
- **状态**：维护模式，新功能在 `apps/workbench` 开发

#### packages/shared - 共享工具库
- **职责**：跨包共享的工具函数与类型定义
- **包名**：`@codeflow/shared`

---

### 后端服务（backend/）

**Go 1.23+ 后端服务**
- **框架**：Gin Web Framework
- **数据库**：SQLite（会话存储）
- **通信**：RESTful API + WebSocket
- **端口**：8080（默认）
- **启动命令**：
  ```bash
  cd backend
  make build    # 构建
  make run      # 运行
  make test     # 测试
  ```

---

### 归档目录（.archive/）

**隐藏目录，存放历史代码与临时文件**
- `.archive/legacy/` - 历史代码归档（原 `archive/`）
- `.archive/reviews/` - Code Review 临时文件（原 `.tmp_codeflow_review*`）

---

## 🔄 重构历史

### 2026-05-27 重构（v2.0）

**变更内容**：
1. ✅ 新增 `apps/` 目录，区分"应用"与"包"
2. ✅ `codeflow_template` → `apps/desktop` → `apps/workbench`（G01；明确工作台身份）
3. ✅ `packages/gui` → `packages/ui-components`（明确组件库定位）
4. ✅ `archive` → `.archive/legacy`（隐藏归档）
5. ✅ 清理 5 个 `.tmp_codeflow_review*` → `.archive/reviews/`
6. ✅ 更新 `pnpm-workspace.yaml` 纳入 `apps/*`
7. ✅ 更新 `nx.json` 添加 `workspaceLayout`
8. ✅ 更新根 `package.json` 添加桌面应用快捷脚本

**动机**：
- 符合标准 Monorepo 最佳实践（apps/ + packages/）
- 命名更清晰，职责更明确
- 统一依赖管理，减少重复安装
- 便于 Nx 缓存和增量构建

---

## 📦 包依赖关系

```
apps/workbench
  └── @codeflow/core
      ├── @anthropic-ai/sdk
      ├── @google/generative-ai
      ├── openai
      └── better-sqlite3

packages/cli
  └── @codeflow/core
      └── commander

packages/ui-components
  ├── @codeflow/core
  ├── react
  └── react-dom
```

---

## 🚀 常用命令

### 根目录命令

```bash
# 安装依赖
pnpm install

# 构建所有包
pnpm build

# 类型检查
pnpm typecheck

# 运行测试
pnpm test

# 代码检查
pnpm lint

# 格式化代码
pnpm format
```

### 桌面应用命令

```bash
# Web 开发模式
pnpm dev:desktop

# Tauri 开发模式
pnpm tauri:dev

# 构建桌面应用
pnpm tauri:build

# 预览构建
pnpm --filter @codeflow/workbench preview
```

### 后端命令

```bash
cd backend

# 构建
make build

# 运行
make run

# 开发模式（热重载）
make dev

# 测试
make test

# 代码检查
make lint
```

---

## 📝 开发规范

### 新增应用

在 `apps/` 下创建新目录，package.json 命名为 `@codeflow/<app-name>`：

```json
{
  "name": "@codeflow/web",
  "version": "0.1.0",
  "dependencies": {
    "@codeflow/core": "workspace:*"
  }
}
```

### 新增共享包

在 `packages/` 下创建新目录，package.json 命名为 `@codeflow/<package-name>`：

```json
{
  "name": "@codeflow/utils",
  "version": "0.1.0",
  "main": "./dist/index.js",
  "types": "./dist/index.d.ts"
}
```

### 跨包引用

使用 `workspace:*` 协议引用本地包：

```json
{
  "dependencies": {
    "@codeflow/core": "workspace:*",
    "@codeflow/shared": "workspace:*"
  }
}
```

---

## ⚠️ 注意事项

1. **better-sqlite3 依赖**：需要 Python 3.6+ 和 node-gyp 环境
2. **Go 后端 CGO**：需要 GCC 编译器（go-sqlite3 依赖）
3. **Tauri 构建**：需要 Rust 工具链
4. **归档目录**：`.archive/` 已加入 `.gitignore`，不会提交到 Git

---

## 📊 目录统计

| 类型 | 数量 | 说明 |
|------|------|------|
| 应用（apps） | 1 | desktop |
| 共享包（packages） | 4 | core, cli, ui-components, shared |
| Go 后端模块 | 35+ | backend/internal/* |
| TypeScript 核心模块 | 20+ | packages/core/src/* |

---

**维护者**：CodeFlow Team  
**最后更新**：2026-05-27
