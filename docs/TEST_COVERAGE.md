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

## CI 集成

`.github/workflows/nx-ci.yml` 已生成并上传覆盖率产物：

| 产物 | 来源命令 | Artifact |
|------|----------|----------|
| 前端覆盖率 | `pnpm run test:coverage` | `frontend-coverage` |
| 后端覆盖率 | `go test -race -coverprofile=coverage.out ./...` + `go tool cover` | `backend-coverage` |

## 覆盖率目标与徽章

核心模块覆盖率目标为 ≥ 80%。当前可使用目标徽章标识质量门槛：

```md
![core coverage target](https://img.shields.io/badge/core%20coverage%20target-%E2%89%A580%25-brightgreen)
```

GitHub Actions 可使用 `nx-ci.yml` 的 workflow badge 展示覆盖率报告生成状态；动态覆盖率百分比 badge 需要接入 Codecov、Coveralls 或 GitHub Pages 后，由 `backend/coverage.txt` 与前端 coverage summary 自动生成。

当前仓库先保留本地命令、CI artifact 与目标徽章入口，避免在未配置外部覆盖率服务时生成无法自动更新的静态百分比。
