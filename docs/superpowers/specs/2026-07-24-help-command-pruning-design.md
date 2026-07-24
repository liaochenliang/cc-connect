# /help 命令精简设计

## 目标

精简聊天中的 `/help` 内容，只隐藏当前项目不希望公开展示的命令，同时保留这些命令原有的注册和执行行为。

## 展示范围

纯文本帮助与卡片帮助保持一致，移除以下命令条目：

- `/usage`
- `/upgrade`
- `/status`
- `/version`
- `/provider`
- `/model`
- `/allow`
- `/reasoning`
- `/mode`
- `/lang`
- `/tts`
- `/shell`
- `/show`
- `/dir`
- `/bind`
- `/workspace`

同时移除纯文本帮助末尾的权限模式说明：`default / edit / plan / yolo`。

`/restart` 保留在帮助中，但帮助描述中的产品名从 `cc-connect` 改为 `connect`。五种语言的纯文本和卡片描述同步修改。

## 行为边界

- 不删除、不禁用任何命令。
- 不修改命令路由、权限判断或处理逻辑。
- 不修改 Telegram 等平台的原生命令菜单注册。
- 不修改 `/restart` 执行时的“正在重启”和“重启成功”消息。
- 保留已有的 `/user` 与动态 Shared Workspace 帮助条目。

## 实现

- 从 `MsgHelp` 的五种语言静态文本中删除指定条目和权限模式尾注，并更新 `/restart` 描述。
- 从帮助卡片分组中删除对应条目，并更新 `MsgBuiltinCmdRestart` 的五种语言描述。
- 不增加新的配置、类型或帮助渲染抽象。

## 测试

- 纯文本帮助不包含任何被移除的命令，并保留 `/restart`。
- 卡片帮助的 Agent、Tools、System 分组不包含相应命令。
- 纯文本与卡片中的 `/restart` 描述使用 `connect`。
- 已有 `/user`、`/medialab` 帮助可见性测试继续通过。
