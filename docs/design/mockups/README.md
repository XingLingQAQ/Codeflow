# CodeFlow 3.0 控制台界面初稿

> 状态：Draft · 2026-09-06  
> 关联：`../codeflow-3.0/codeflow-3.0-redesign.md` §12 前端信息架构  
> 直接用浏览器打开 `.html` 即可查看，无需构建。

## 文件

| 文件 | 内容 |
|---|---|
| `shared.css` | 所有页面共用：令牌、基础、控件（按钮/药丸/芯片/分段）、导轨、顶栏件、流线签名元素。新页面只写自己的布局。 |
| `home.html` | 首页：问候 + 提示栏、等你处理、正在运行、项目、右栏用量/守卫/记忆/最近合入。 |
| `workbench.html` | 工作台，五个状态可切换（状态栏中间的演示步进器、← → 键、或 `?state=idle|typed|prompt|flow|agents`）：普通工作台 → 输入需求 → AI 建议进入工作流（弹窗）→ 工作流模式（Flow Rail + 阶段画布）→ Agent 看板（原始输出流）。 |
| `home-b-minimal.html` | 已否决的 B 方案（台账式极简），独立样式，仅存档。 |
| `*.png` | 各状态截图（最近一轮改版前的旧图，仅供比对）。 |

## 视觉体系

调性参考 [beautifului.dev](https://www.beautifului.dev)（AI 原生界面组件集）的浅色令牌：

- **字**：Inter + Noto Sans SC，13px 基准，界面文字 500、正文 400，字距 −0.01em，标题 600；等宽 JetBrains Mono。
- **色**：墨色为蓝灰 `#262A33`（非纯黑）；三级灰 `#6B7280 / #9CA3AF / #C4C8CF`；页面 `#FAFAFB`、面板白、内嵌区 `#F5F6F8`；唯一强调蓝 `#3B82F6` 配淡底 `#EFF6FF`；绿/橙/红各配淡底。流线渐变（蓝→青）只用于 Logo 与进度线。
- **面**：卡片不用 border，用 `box-shadow: 0 0 0 1px line` 发丝环 + 极浅投影；圆角控件 8px、容器 14px、状态与卡内动作用药丸。
- **件**：主动作蓝色药丸、次动作灰药丸、静默动作无底；状态药丸淡底彩字；工具值放内嵌等宽框；任务行为白色胶囊 + 圆形编号/勾；提示栏底部是 `+`、选择器、黑色圆形发送；分段控件白色活动片。

## 约定

- 状态门控用 `body[data-state]` 与 `.normal-only / .flow-only / .stage-only / .agents-only / .idle-only / .not-idle` 类；不要用 JS 手动显隐。
- 导轨为共享组件：首页展开、工作台收起（`body.rail-collapsed`），底部按钮可切换。
- 中栏使用容器查询（`@container center`）在变窄时把看板降为单列。
- 页面内避免复用 `.row / .run / .flow` 这类过泛的类名，历史上已撞车两次。
