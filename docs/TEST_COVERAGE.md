# 测试覆盖率报告

> 当前目标：核心模块覆盖率 ≥ 80%。Go 后端与 TypeScript 前端分别生成覆盖率报告，根目录提供统一入口。

## 命令入口

| 范围 | 命令 | 输出 |
|------|------|------|
| 前端 / TS | `pnpm test:coverage` | Vitest coverage 目录 |
| 后端 / Go | `pnpm test:coverage:backend` | `backend/coverage.out`、`backend/coverage.html`、`backend/coverage.txt` |
| 全量 | `pnpm test:coverage:all` | 先运行前端覆盖率，再运行后端覆盖率 |

## 后端参数

后端覆盖率命令由根 `package.json` 直接调用 Go 工具链，避免在 Windows 环境依赖 `make`：

```bash
pnpm test:coverage:backend
```

`backend/Makefile` 仍保留 `test-coverage` 目标，供 Unix/CI 或已安装 `make` 的环境使用：

```bash
make test-coverage COVERAGE_OUT=coverage.out COVERAGE_HTML=coverage.html COVERAGE_PKGS=./internal/snapshot
```

## 环境要求

> Go 后端使用 `github.com/mattn/go-sqlite3`。运行包含 SQLite 持久化测试的覆盖率时需要启用 CGO 并安装 C 编译器（Windows 下通常需要 GCC 或 Visual Studio Build Tools）。

## CI 建议

1. 安装 pnpm 与 Go 工具链。
2. 前端执行 `pnpm test:coverage`。
3. 后端执行 `pnpm test:coverage:backend`。
4. 上传 `coverage` 目录与 `backend/coverage.html` / `backend/coverage.txt` 作为构建产物。
5. 对核心模块逐步增加覆盖率门槛，目标为 ≥ 80%。
