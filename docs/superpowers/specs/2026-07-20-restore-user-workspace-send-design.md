# 恢复 user-workspace 的 /send

## 目标

在 `user-workspace` 模式下恢复本地 Unix Socket API 的 `POST /send` 原有行为。

## 行为

- `/send` 不再经过 `blockForUserWorkspaces`，继续使用现有 `handleSend`。
- `/sessions`、cron、timer、relay 和 webhook 在 `user-workspace` 下继续返回 `403`。
- `session_key`、`work_dir`、附件和 TTS 的处理保持现状。
- 本次不支持将十六进制 user-workspace 目录名转换为企业微信 session key。

## 风险

调用方接受共享 Socket 上的进程可以指定其他用户 `session_key` 的跨用户发送风险。本次不增加鉴权或调用方身份校验。

## 验证

- 回归测试证明 `user-workspace` 下 `/send` 可以发送消息。
- 回归测试证明其余本地 API 仍被拦截。
- 原有非 `user-workspace` API 测试继续通过。
