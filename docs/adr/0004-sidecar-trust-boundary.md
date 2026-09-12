# ADR 0004: Sidecar 信任边界

- 状态：Accepted
- 日期：2026-08-18
- 关联：`docs/plans/2026-08-18-backend-product-hardening-plan.md` B1

## 背景

CodeFlow 后端由桌面应用作为本地 sidecar 启动。此前服务默认监听所有网卡，HTTP CORS 与 WebSocket Origin 接受任意来源，API 与主题订阅也没有启动身份。这意味着同机网页、局域网设备或错误配置的客户端都可能读取项目、修改工作区或订阅其他会话事件。

## 决策

1. sidecar 默认仅绑定 `127.0.0.1`。非 loopback 地址必须显式启用 `CODEFLOW_REMOTE_MODE=true`，并提供独立的 `CODEFLOW_REMOTE_TOKEN`；远程模式不复用 sidecar 默认令牌。
2. 每次未显式提供令牌的本地启动生成 256-bit 随机令牌。令牌只通过一行受控启动握手输出：`CODEFLOW_HANDSHAKE:{...}`，内容包括协议版本、host、实际 port、token 与进程级过期语义。普通 HTTP 响应、日志和错误不返回令牌。
3. 除 `/health` 和静态资源外，`/ready`、`/metrics` 与所有 `/api/v1` 路由都要求 `Authorization: Bearer <token>`。显式令牌至少 32 个字符，只允许 URL-safe 的字母、数字、`-`、`_`。
4. CORS 与 WebSocket Origin 仅做精确白名单匹配，拒绝 `*`、前缀匹配和任意回调。默认桌面/开发 Origin 为 `http://localhost:3000`、`http://localhost:5173`、`tauri://localhost`、`https://tauri.localhost`。
5. 原生 WebSocket 客户端可使用 Bearer；浏览器客户端同时提供 `codeflow.v1` 与 `codeflow.token.<token>` 子协议，服务端只回显稳定的 `codeflow.v1`。
6. WebSocket 资源范围由升级路由确定，客户端消息不能改绑。会话流不能订阅主题；辩论流只允许 `debate:<id>`；项目流只允许 `flow:project:<id>`。不存在默认开放的全局客户端主题。
7. 测试使用显式测试令牌或测试侧请求包装，不在生产配置中提供无鉴权开关。

## 备选方案

- 仅依赖 loopback：同机恶意网页仍可请求服务，不能替代启动身份与 Origin 检查。
- 把 token 放进 URL query：会进入历史、代理和诊断日志，因此拒绝。
- 长期固定本地 token：无法保证应用重启后旧令牌失效，因此拒绝。
- 所有 WebSocket 共用全局主题：会让一个资源页收到其他项目或会话事件，因此拒绝。

## 后果

- 桌面启动器必须解析握手，并只在进程内保存 token；前端不得写入 localStorage 或日志。
- 未来前端 HTTP 客户端统一附加 Bearer；浏览器 WebSocket 使用子协议认证。
- 每次后端独立启动产生的新令牌会使旧客户端立即失效并需要重新握手。
- 远程/手机伴侣模式仍需后续增加 TLS、配对和短期 ticket；本 ADR 只允许显式远程配置，不宣称公网部署已完成。
