# User Workspace Help Visibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让聊天 `/help` 和终端 `cc-connect --help` 能发现 user-workspace 切换命令。

**Architecture:** 复用 Engine 已验证的 `userSharedWorkspaces` 配置作为聊天帮助的唯一数据源；CLI 帮助保持静态，不读取配置。只扩展现有帮助渲染路径，不改命令注册和执行路径。

**Tech Stack:** Go 标准库、现有 `testing` 测试。

---

### Task 1: 用失败测试固定帮助行为

**Files:**
- Modify: `core/user_workspace_test.go`
- Modify: `core/i18n_test.go`
- Modify: `cmd/cc-connect/main_test.go`

- [x] 在 `core/user_workspace_test.go` 增加 `TestUserWorkspaceHelpListsSelectionCommands`：配置 `zlab`、`medialab` 后调用纯文本和卡片帮助，断言包含 `/user`、`/medialab`、`/zlab` 且共享名称排序。
- [x] 将新的帮助消息键加入现有五语言完整性测试。
- [x] 在 `cmd/cc-connect/main_test.go` 增加 `TestPrintUsage_ListsUserWorkspaceCommands`：断言 CLI 帮助包含 `/user`、`/<shared-workspace>` 和 `/medialab`。
- [x] 运行 `go test ./core -run TestUserWorkspaceHelpListsSelectionCommands -count=1` 与 `go test ./cmd/cc-connect -run TestPrintUsage_ListsUserWorkspaceCommands -count=1`，预期因帮助内容缺失而失败。

### Task 2: 最小实现并验证

**Files:**
- Modify: `core/user_workspace.go`
- Modify: `core/engine.go`
- Modify: `core/i18n.go`
- Modify: `cmd/cc-connect/main.go`

- [x] 在 `core/user_workspace.go` 从锁保护的配置复制并排序命令，生成现有帮助卡片条目和纯文本段落。
- [x] 在 `core/engine.go` 让纯文本帮助追加动态段落，并让卡片“系统”页追加动态条目。
- [x] 在 `core/i18n.go` 增加工作区帮助标题、User Workspace 和 Shared Workspace 描述的五语言文本。
- [x] 在 `cmd/cc-connect/main.go` 的静态 Usage 中增加消息工作区命令段落。
- [x] 运行 `gofmt`、两个回归测试、`go test ./core ./cmd/cc-connect`、`go test ./core -run TestCUJ`、`go build ./...` 和 `git diff --check`。
