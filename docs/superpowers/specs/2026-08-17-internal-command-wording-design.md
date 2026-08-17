# Internal Command Wording Design

## Goal

Describe unknown slash commands as not being internal commands, without exposing the `cc-connect` product name in that notice.

## Behavior

- An unknown slash command still notifies the user and then falls through to the Agent unchanged.
- Simplified Chinese uses `` `%s` 不是内部命令，已转发给 Agent 处理... ``.
- English, Traditional Chinese, Japanese, and Spanish use equivalent “internal command” wording.
- Existing quiet-mode acknowledgements remain unchanged:
  - Idle message: `⏳ 处理中...`
  - Busy message: `📬 消息已收到，将在当前任务完成后处理。`

The WeCom special deployment continues to obtain these acknowledgements through `[display].mode = "quiet"`. No platform-specific branch, configuration, dependency, or new abstraction is added.

## Testing

- Update the unknown slash command regression test to require the new “internal command” wording while confirming the original request still reaches the Agent.
- Run the existing quiet idle and busy-message regression tests.
- Run the affected core and release-local test packages.
