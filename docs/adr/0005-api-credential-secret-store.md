# ADR 0005: API 凭据与配置分离存储

- 状态：Accepted
- 日期：2026-08-19
- 关联：`docs/plans/2026-08-18-backend-product-hardening-plan.md` B2

## 背景

原有 `config.APIChannel.APIKey` 可被 JSON/YAML 序列化，配置 API 会回显完整配置，SQLite 的 `global_config.config_json` 也直接保存 API Key。运行时再从 `ResolvedConfig` 把明文复制到 adapter。这使密钥可能进入响应、普通配置备份、错误转储和日志。

CodeFlow 是桌面 sidecar，但本机进程边界不等于密钥存储边界。配置元数据需要可备份、可查看，凭据必须由独立的受保护存储管理。

## 决策

1. 引入 `SecretStore` 接口。普通配置只保存不可用作认证的 `secret_ref`、`secret_status`、掩码、版本和更新时间，不保存明文。
2. Windows 使用当前登录用户范围的 DPAPI 保护每个 secret。密文写入独立的 `config.db.secrets.json`，采用临时文件、同步和原子替换；密文文件不包含主密钥。
3. 非 Windows 使用 AES-256-GCM，主密钥必须通过独立的 `CODEFLOW_SECRET_MASTER_KEY` 环境来源提供。服务不在配置目录生成或保存主密钥，避免主密钥与密文同库同源。后续可用系统 Keychain/Secret Service 实现替换该 provider，不改变 `SecretStore` 契约。
4. 配置 API 使用三种模型：写入 DTO 接受只写 `api_key` 和显式 `delete_secret`；内部模型只携带 transient 写入值及 secret 元数据；公开 DTO 永不包含 `api_key`。省略密钥表示保留，删除必须显式请求。
5. `ResolvedConfig` 不携带明文。`commander.BuildAgentFromResolved` 在构造新 adapter 时通过 context 和 secret ref 从 `SecretStore` 取值。已运行的请求持有原 adapter，不受轮换影响；新构造使用新 ref。
6. 启动时先初始化 audit，再初始化配置。旧 `config_json.api_key` 先写入 SecretStore，成功后清除普通数据库中的字段；任何加密、写入、解密或引用校验失败都拒绝配置服务启动。保存数据库失败会删除本次新建的孤立 secret；启动和成功保存后会清理未被引用的旧 ref。
7. create、rotate、delete、legacy migrate 写审计事件。审计只记录 channel ID、secret ref 和版本，不记录明文或可逆内容。

## 备选方案

- 继续依赖 JSON `omitempty`：拒绝。字段仍能被内部响应、YAML 和数据库路径泄露。
- 将主密钥和密文放入同一个 SQLite：拒绝。备份或文件读取会同时取得两者。
- 仅使用进程环境变量保存所有 API Key：拒绝。无法支持用户持久化配置、轮换和重启恢复。
- 本轮新增第三方跨平台 keyring 依赖：暂不采用。Windows 已有稳定 DPAPI；当前仓库没有已验证的 macOS/Linux keyring 运行基线。非 Windows 明确要求外部主密钥，保持 fail-closed。

## 后果

- 配置响应、普通 SQLite 和配置导出可以安全包含通道元数据，但不能恢复 API Key。
- 复制 `config.db` 不再同时复制可解密凭据；Windows 密文还绑定当前 OS 用户。
- Windows 用户迁移到另一账户或机器时需要重新录入凭据。非 Windows 部署必须单独管理 `CODEFLOW_SECRET_MASTER_KEY`。
- 本决策不防御已完全控制当前 OS 用户会话的攻击者，也不把掩码或 secret ref 当作认证凭据。
- 旧明文迁移失败会阻止启动，而不是继续使用或回显旧值；修复 secret provider 后可幂等重试迁移。
