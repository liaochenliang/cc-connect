# user-workspace 共享工作区切换

## 目标

在企业微信 WebSocket 的 `user-workspace` 模式中，允许每个用户独立地在自己的 User Workspace 与预配置的 Shared Workspace 之间切换。

首期标准命令为 `/user` 和由配置生成的 `/<共享名称>`，例如 `/medialab`。选择以 UserID 为范围：同一用户的所有企业微信群聊和私聊一起切换，其他用户不受影响。

## 配置

在现有项目配置中增加 `shared_workspaces`：

```toml
[[projects]]
name = "wecom-users"
mode = "user-workspace"
base_dir = "/data/workspaces"
shared_workspaces = ["medialab", "designlab"]

[projects.agent]
type = "codex"

[[projects.platforms]]
type = "wecom"

[projects.platforms.options]
mode = "websocket"
```

每个名称直接映射到 `base_dir/<名称>`，不支持单独配置绝对路径。

启动时执行以下校验：

- `shared_workspaces` 只允许用于 `user-workspace` 模式。
- 名称只能包含小写字母、数字、`-` 和 `_`，且必须以字母或数字开头。
- `user`、内置命令以及忽略大小写后的重复名称不可使用。
- 对应路径必须已经存在，必须是真实目录，不能是符号链接。

共享目录不会自动创建，也不会被 cc-connect 修改权限。

## 状态模型

Engine 保存一个并发安全的内存映射：

```text
UserID -> selected shared workspace name
```

没有映射时，该用户使用现有规则生成的 User Workspace：`base_dir/<编码后的 UserID>`。User Workspace 继续按现有逻辑自动创建并设置为 `0700`。

选择不新增持久化格式。服务重启后映射为空，所有用户默认回到 User Workspace。现有 `workspace_bindings.json` 只作为运行时路由的派生数据，不作为用户选择的恢复来源。

## 命令行为

### `/medialab`

1. 按配置名称忽略大小写匹配命令；标准展示始终使用小写名称。
2. 如果用户已经选择 medialab，回复当前已在该工作区，不改变状态。
3. 如果用户当前 Agent Session 正忙，拒绝切换并提示先执行 `/stop`。
4. 否则只更新该 UserID 的选择并回复 `已切换到共享工作区：medialab`。

### `/user`

1. 如果用户已经在 User Workspace，回复当前已在用户工作区，不改变状态。
2. 如果用户当前 Agent Session 正忙，拒绝切换并提示先执行 `/stop`。
3. 否则删除该 UserID 的共享选择并回复 `已切换到用户工作区`。

所有通过现有企业微信访问控制的用户都可以执行这些切换命令，不增加管理员权限要求。`/workspace` 在 `user-workspace` 模式中继续不可用。

共享工作区选择命令优先于同名自定义命令、alias 或 agent skill；配置校验仍提前拒绝与内置命令冲突，避免覆盖核心命令。

## 会话行为

Workspace 先于 Agent Session 解析。切换命令只改变用户后续消息的 workspace，不创建、不删除、也不清空 Agent Session。

现有 SessionManager 已经按 workspace 分开存储，并在 workspace 内按消息 `SessionKey` 区分用户。因此：

- 同一用户在 User Workspace 与每个 Shared Workspace 中分别保留会话。
- 同一用户的 Workspace Selection 跨群聊和私聊共享，但每个消息 `SessionKey` 的 Agent Session 仍然独立。
- 切回某个 workspace 时恢复该用户在该 workspace 的上次会话。
- `/new` 只在用户当前选择的 workspace 内创建新会话。
- 其他用户的 workspace 选择和 Agent Session 均不受影响。

## 消息处理流程

每条企业微信消息进入 Engine 后：

1. 根据 UserID 读取内存中的 workspace 选择。
2. 没有共享选择时，复用现有逻辑创建或解析 User Workspace。
3. 有共享选择时，从启动时验证过的配置表取得目录；不使用消息文本直接拼接路径。
4. 将得到的目录作为该消息的有效 workspace，继续复用现有 workspace agent pool、SessionManager 和交互状态处理。
5. 命令处理使用同一个有效 workspace，因此 `/new`、`/list`、`/history` 等命令自然作用于当前选择。

不新增通用 Workspace Router、额外接口或第三方依赖。

## 异常与并发

- **任务忙碌时切换**：拒绝切换，保留原选择，回复 `当前任务正在运行，请先执行 /stop`。
- **共享目录运行时消失**：清除该用户的共享选择，回复共享目录不存在且已回到 `/user`，并终止当前消息；不得把原消息改在 User Workspace 执行。
- **多个用户同时使用共享目录**：按已确认的产品选择允许并发，不增加 workspace 级锁、拒绝或队列。调用方接受同时修改同一目录产生冲突的风险。
- **同一用户并发切换**：用户选择映射使用互斥锁保护；现有每会话忙碌状态继续保护正在执行的 Agent Session。

新增用户可见回复全部通过 i18n MsgKey 提供所有受支持语言的翻译。部署设置 `language = "zh"` 时，企业微信仅显示中文回复。

## 测试

### 配置测试

- `shared_workspaces` 仅接受 `user-workspace` 模式。
- 接受合法小写名称，拒绝大写、路径分隔符、`user`、内置命令和重复名称。
- 启动装配拒绝缺失目录、普通文件和符号链接。

### Engine 回归测试

- 默认消息仍进入发送者的 User Workspace。
- `/medialab` 只切换发送者，另一个用户保持原 workspace。
- 同一用户从另一个群聊或私聊发送消息时使用同一 workspace 选择。
- `/user` 切回用户目录。
- 命令输入大小写不敏感，回复使用配置中的小写名称。
- 忙碌时拒绝切换且不改变选择。
- 切换 workspace 后恢复各自原有会话；`/new` 只重置当前 workspace 的会话。
- 服务重启后不恢复共享选择。
- 共享目录运行时消失时回退 `/user`，且当前消息不会发送给 Agent。
- 不同用户可以同时在同一个 Shared Workspace 发起任务。

### CUJ

增加一个用户视角的多步骤场景：用户 A 在群聊 1 执行 `/medialab` 并对话，在群聊 2 的下一条消息也进入 medialab；用户 A 执行 `/user` 后两个群聊都回到 User Workspace。用户 B 始终留在 B 自己的 User Workspace。再次进入 medialab 时，各消息 `SessionKey` 分别恢复自己的原会话。断言平台实际收到的回复，不读取 Engine 内部字段。

### 完成验证

```bash
go test ./core/ -run TestCUJ -v
go test ./...
go test -race ./core/ ./config/
go build ./...
```

## 非目标

- 不支持企业微信 WebSocket 之外的平台。
- 不支持按群聊或私聊分别保存同一用户的 Workspace Selection。
- 不持久化用户的 workspace 选择。
- 不支持任意路径、运行时增删 Shared Workspace 或自动创建共享目录。
- 不增加共享目录的串行执行、排队或冲突处理。
- 不改变 `/workspace`、Web 管理后台或 Management API。
- 不把 Shared Workspace 等同于共享 Agent Session。
