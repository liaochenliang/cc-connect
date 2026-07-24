# 纯文本 /help 尾部顺序设计

## 目标

只调整纯文本 `/help` 的输出顺序，让工作区切换命令出现在通用说明之前。

## 最终顺序

纯文本帮助依次输出：

1. 现有命令列表，以 `/help` 和 `Show this help` 结束。
2. 启用 Shared Workspace 时的 `Workspace switching`、`/user`、`/<shared-workspace>`。
3. 现有四段通用说明：`Tip`、`Custom commands`、`Command aliases`、`Agent skills`。

因此 `/user` 和 `/medialab` 不再位于消息最底部，四段通用说明成为最终尾部。

## 实现

- 从五语言 `MsgHelp` 中取出四段通用说明，放入一个五语言 `MsgHelpFooter`。
- 非卡片平台按 `MsgHelp + userWorkspaceHelpText() + MsgHelpFooter` 输出。
- 未启用 Shared Workspace 时，帮助内容保持原有文字和相对顺序。

## 非目标

- 不修改任何帮助文案。
- 不修改卡片帮助。
- 不修改命令注册、菜单、路由、权限或执行行为。
- 不修改 workspace 选择或帮助可见性条件。

## 测试

- 配置 `medialab` 后，纯文本中的 `/medialab` 位于 `Tip` 之前。
- 四段通用说明全部保留，并位于消息尾部。
- 未配置 Shared Workspace 时，纯文本帮助仍包含相同内容。
- 卡片帮助现有测试继续通过。
