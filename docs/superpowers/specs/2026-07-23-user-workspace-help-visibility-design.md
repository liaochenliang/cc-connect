# user-workspace 命令帮助可见性

## 目标

让已启用 Shared Workspace 的 `user-workspace` 项目能够在聊天 `/help` 中看到实际可用的 `/user` 和 `/<共享名称>` 命令，并在终端 `cc-connect --help` 中看到这类消息命令的通用说明。

## 根因

`/user` 和 `/<共享名称>` 根据项目配置动态识别，不属于全局 `builtinCommands`。当前纯文本帮助、卡片帮助和 CLI 帮助均来自静态内容，因此命令可以执行却不会显示。

## 聊天帮助

- 仅当项目处于 `user-workspace` 模式且至少配置一个 Shared Workspace 时显示工作区切换命令。
- 纯文本 `/help` 追加一个本地化的“工作区切换”段落。
- 卡片 `/help` 在现有“系统”页追加同一组命令，不新增帮助页签。
- `/user` 排在首位；Shared Workspace 命令按名称排序，保证输出稳定。
- 展示实际配置名称，例如 `/medialab`，而不是占位符。
- 命令说明使用现有五种语言：EN、ZH、ZH-TW、JA、ES。

## CLI 帮助

`cc-connect --help` 不加载配置。帮助文本增加一段静态说明，列出：

- `/user`：切回 User Workspace。
- `/<shared-workspace>`：切换到已配置的 Shared Workspace。
- `/medialab`：仅作为共享命令示例。

这样保持 `--help` 无配置依赖、无文件创建等副作用，也避免多项目配置下无法确定应展示哪个项目的问题。

## 测试

- 纯文本聊天帮助在启用 Shared Workspace 后包含 `/user` 和实际共享命令。
- 卡片帮助的“系统”页包含 `/user` 和实际共享命令，且顺序稳定。
- CLI 帮助包含 `/user`、`/<shared-workspace>` 和 `/medialab` 示例。
- 未配置 Shared Workspace 的聊天帮助保持现状。

## 非目标

- 不把动态工作区命令加入全局 `builtinCommands`。
- 不改变 Telegram、Discord 等平台的原生命令菜单注册。
- 不让 CLI 帮助读取或校验 `config.toml`。
- 不改变工作区切换行为、权限或状态模型。
