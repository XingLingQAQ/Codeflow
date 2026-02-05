---
mode: plan
cwd: D:\project\CodeFlow
task: Tauri + Go 后端桌面应用方案 - 将 CodeFlow 打包为 Windows 桌面应用
complexity: complex
planning_method: builtin
created_at: 2026-02-05T23:05:58
---

# Plan: Tauri + Go 后端桌面应用方案

🎯 任务概述
将 CodeFlow 打包为 Windows 桌面应用（Tauri），内嵌 Go 后端进程。采用 Tauri 2.x 作为桌面框架，通过 Sidecar 机制管理 Go 后端服务。

## 架构设计

```
┌─────────────────────────────────────────────────────┐
│              CodeFlow.exe (Tauri App)               │
├─────────────────────────────────────────────────────┤
│  ┌───────────────────────────────────────────────┐  │
│  │           Tauri Shell (Rust)                  │  │
│  │  - 窗口管理                                    │  │
│  │  - 系统托盘                                    │  │
│  │  - 进程管理（启动/停止 Go 后端）                │  │
│  │  - 资源嵌入（Go exe + 前端资源）               │  │
│  └───────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────┐  │
│  │           Frontend (WebView)                  │  │
│  │  - React SPA (codeflow_template)              │  │
│  │  - 通过 HTTP 与 Go 后端通信                    │  │
│  └───────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────┐  │
│  │           Go Backend (Sidecar)                │  │
│  │  - codeflow-server.exe                        │  │
│  │  - REST API (/api/v1/*)                       │  │
│  │  - WebSocket                                  │  │
│  │  - 端口: 动态分配或固定 8080                   │  │
│  └───────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

## 通信流程
```
Frontend (WebView) ──HTTP/WS──> Go Backend (localhost:8080)
         │
         └──Tauri Commands──> Rust Shell (进程管理/系统功能)
```

📋 执行计划

### Phase 1: Tauri 项目初始化
1. 在 `codeflow_template` 目录安装 Tauri CLI 和 API 依赖
2. 执行 `npx tauri init` 初始化 Tauri 项目结构
3. 配置 `tauri.conf.json`（窗口、安全策略、资源嵌入）

### Phase 2: Go 后端 Sidecar 集成
1. 创建 `src-tauri/binaries/` 目录存放 Go 可执行文件
2. 编写 Go 后端构建命令（交叉编译 Windows x64）
3. 实现 Rust 端 Sidecar 管理逻辑（启动、监控、日志）
4. 配置 `Cargo.toml` 添加 `tauri-plugin-shell` 依赖

### Phase 3: 前端适配
1. 修改 `vite.config.ts` 适配 Tauri 构建要求
2. 创建 API 基础路径配置文件
3. 确保前端正确连接本地 Go 后端

### Phase 4: 构建与打包
1. 创建 Windows 构建脚本 `scripts/build-tauri.ps1`
2. 配置 MSI/NSIS 安装包生成
3. 准备应用图标资源

### Phase 5: 验证与测试
1. 开发模式验证（`npm run tauri dev`）
2. 生产构建验证
3. 功能完整性测试

## 关键文件清单

| 操作 | 文件路径 | 说明 |
|------|---------|------|
| 新建 | `codeflow_template/src-tauri/tauri.conf.json` | Tauri 配置 |
| 新建 | `codeflow_template/src-tauri/Cargo.toml` | Rust 依赖 |
| 新建 | `codeflow_template/src-tauri/src/main.rs` | Tauri 入口 |
| 新建 | `codeflow_template/src-tauri/binaries/` | Go 后端存放目录 |
| 修改 | `codeflow_template/vite.config.ts` | Tauri 兼容配置 |
| 修改 | `codeflow_template/package.json` | 添加 Tauri 依赖 |
| 新建 | `scripts/build-tauri.ps1` | Windows 构建脚本 |

## 目录结构

```
codeflow_template/
├── src-tauri/
│   ├── binaries/
│   │   └── codeflow-server-x86_64-pc-windows-msvc.exe
│   ├── icons/
│   │   └── icon.ico
│   ├── src/
│   │   └── main.rs
│   ├── Cargo.toml
│   └── tauri.conf.json
├── src/
│   └── ... (React 前端)
├── package.json
└── vite.config.ts
```

## 核心配置参考

### tauri.conf.json
```json
{
  "$schema": "https://schema.tauri.app/config/2",
  "productName": "CodeFlow",
  "version": "0.1.0",
  "identifier": "com.codeflow.app",
  "build": {
    "beforeBuildCommand": "npm run build",
    "devUrl": "http://localhost:3000",
    "frontendDist": "../dist"
  },
  "app": {
    "windows": [{
      "title": "CodeFlow",
      "width": 1280,
      "height": 800,
      "resizable": true
    }],
    "security": { "csp": null }
  },
  "bundle": {
    "active": true,
    "targets": ["msi", "nsis"],
    "icon": ["icons/icon.ico"],
    "resources": ["binaries/*"],
    "externalBin": ["binaries/codeflow-server"]
  }
}
```

### main.rs (Sidecar 管理)
```rust
use tauri::Manager;
use tauri_plugin_shell::ShellExt;

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .setup(|app| {
            let sidecar = app.shell().sidecar("codeflow-server").unwrap();
            let (mut rx, _child) = sidecar.spawn().expect("Failed to spawn sidecar");
            tauri::async_runtime::spawn(async move {
                while let Some(event) = rx.recv().await {
                    if let tauri_plugin_shell::process::CommandEvent::Stdout(line) = event {
                        println!("[Go Backend] {}", String::from_utf8_lossy(&line));
                    }
                }
            });
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
```

⚠️ 风险与注意事项
- **Rust 工具链依赖**：需要安装 `rustup` + `stable` toolchain + Windows SDK
- **Go 交叉编译**：确保 Go 后端正确编译为 Windows x64 目标
- **端口冲突**：Go 后端使用 8080 端口，需处理端口占用情况
- **进程生命周期**：确保 Tauri 关闭时 Go 后端进程正确退出
- **安全策略**：CSP 设置为 null 以允许本地 HTTP 通信，生产环境需评估

📎 参考
- Tauri 2.x 文档: https://v2.tauri.app/
- Tauri Sidecar 指南: https://v2.tauri.app/develop/sidecar/
- `backend/cmd/codeflow-server` - Go 后端入口
- `codeflow_template/` - 前端项目目录

## 预期输出
- Windows 安装包: `src-tauri/target/release/bundle/msi/CodeFlow_0.1.0_x64.msi`
- 便携版: `src-tauri/target/release/CodeFlow.exe`
- 预计大小: ~30-50MB（含 Go 后端）

## 前置条件
1. **Rust 工具链**: `rustup` + `stable` toolchain
2. **Node.js**: 18+
3. **Go**: 1.21+
4. **Windows SDK**: Visual Studio Build Tools
