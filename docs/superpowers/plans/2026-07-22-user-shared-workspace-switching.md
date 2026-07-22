# user-workspace 共享工作区切换 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在企业微信 WebSocket 的 `user-workspace` 模式中，为每个 UserID 提供 `/user` 和预配置的 `/medialab` 等共享工作区切换命令，同时保留各 workspace 的独立会话。

**Architecture:** 扩展现有 user-workspace 路由，不增加新 Router。Engine 保存 `UserID -> shared workspace name` 的内存选择，并继续复用 workspace agent pool、WorkspaceBindingManager 和每 workspace SessionManager；服务重启后选择自然清空。

**Tech Stack:** Go、标准库、现有 TOML 配置、现有 core Engine/SessionManager、Go testing

**Design:** `docs/superpowers/specs/2026-07-22-user-shared-workspace-switching-design.md`

---

## 文件结构

- `config/config.go`：声明并校验 `shared_workspaces` 的配置语法和适用模式。
- `config/config_test.go`：覆盖合法名称、保留名、重复名和错误模式。
- `core/engine.go`：保存并发安全的用户选择状态，在 alias 和普通命令前分派 workspace 选择命令，并本地化运行时目录失效错误。
- `core/user_workspace.go`：验证共享目录、解析 UserID 的当前 workspace、维护派生 binding、执行切换和跨会话忙碌检查。
- `core/user_workspace_test.go`：覆盖路径安全、用户隔离、跨聊天共享选择、忙碌拒绝、会话恢复和重启回退。
- `core/i18n.go`：增加切换相关 MsgKey 及五种语言翻译；中文部署只显示中文。
- `cmd/cc-connect/main.go`：把项目配置装配到 Engine，配置错误时拒绝启动。
- `config.example.toml`、`docs/usage.md`、`docs/usage.zh-CN.md`：记录配置和命令。
- `core/cuj_test.go`：增加用户视角的跨群聊切换旅程。

## 预计排期

| 阶段 | 内容 | 预计时间 |
| --- | --- | ---: |
| Task 1 | 配置字段与纯配置校验 | 30 分钟 |
| Task 2 | 共享目录注册、选择状态与安全解析 | 75 分钟 |
| Task 3 | 动态命令、忙碌检查、i18n 与会话回归 | 90 分钟 |
| Task 4 | 启动装配和使用文档 | 30 分钟 |
| Task 5 | CUJ、全量验证和差异审查 | 60 分钟 |
| 合计 | 不含代码审查反馈返工 | 约 4 小时 45 分钟 |

---

### Task 1: 配置字段与名称校验

**Files:**
- Modify: `config/config.go:470-490`
- Modify: `config/config.go:1038-1065`
- Test: `config/config_test.go:192-270`

- [ ] **Step 1: 先写失败的配置测试**

在 `TestValidateUserWorkspace` 中增加以下子测试：

```go
func TestValidateUserSharedWorkspaces(t *testing.T) {
	newProject := func() ProjectConfig {
		p := validProject("demo")
		p.Mode = "user-workspace"
		p.BaseDir = "/tmp/workspaces"
		delete(p.Agent.Options, "work_dir")
		p.Platforms[0] = PlatformConfig{Type: "wecom", Options: map[string]any{"mode": "websocket"}}
		return p
	}

	t.Run("accepts lowercase names", func(t *testing.T) {
		p := newProject()
		p.SharedWorkspaces = []string{"medialab", "design-lab", "lab_2"}
		if err := (&Config{Projects: []ProjectConfig{p}}).validate(); err != nil {
			t.Fatalf("validate() error = %v", err)
		}
	})

	for _, tc := range []struct {
		name  string
		mode  string
		items []string
		want  string
	}{
		{name: "requires user workspace mode", mode: "multi-workspace", items: []string{"medialab"}, want: "only valid in user-workspace mode"},
		{name: "rejects uppercase", mode: "user-workspace", items: []string{"MediaLab"}, want: "invalid shared workspace name"},
		{name: "rejects path separators", mode: "user-workspace", items: []string{"team/lab"}, want: "invalid shared workspace name"},
		{name: "rejects user", mode: "user-workspace", items: []string{"user"}, want: "reserved"},
		{name: "rejects duplicates", mode: "user-workspace", items: []string{"medialab", "medialab"}, want: "duplicate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProject()
			p.Mode = tc.mode
			p.SharedWorkspaces = tc.items
			err := (&Config{Projects: []ProjectConfig{p}}).validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate() error = %v, want %q", err, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试，确认字段尚不存在**

Run: `go test ./config/ -run TestValidateUserSharedWorkspaces -v`

Expected: FAIL，错误包含 `p.SharedWorkspaces undefined`。

- [ ] **Step 3: 增加最小配置字段和校验函数**

在 `ProjectConfig` 的 `BaseDir` 后增加：

```go
SharedWorkspaces []string `toml:"shared_workspaces,omitempty"`
```

在 `config/config.go` 增加并调用：

```go
var userSharedWorkspaceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func validateUserSharedWorkspaces(prefix, mode string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	if mode != "user-workspace" {
		return fmt.Errorf("config: %s.shared_workspaces is only valid in user-workspace mode", prefix)
	}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if !userSharedWorkspaceNamePattern.MatchString(name) {
			return fmt.Errorf("config: %s has invalid shared workspace name %q", prefix, name)
		}
		if name == "user" {
			return fmt.Errorf("config: %s shared workspace name %q is reserved", prefix, name)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("config: %s has duplicate shared workspace name %q", prefix, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}
```

在每个 project 的校验尾部、`validateRunAsUser` 前调用：

```go
if err := validateUserSharedWorkspaces(prefix, proj.Mode, proj.SharedWorkspaces); err != nil {
	return err
}
```

- [ ] **Step 4: 运行配置测试**

Run: `go test ./config/ -run 'TestValidateUserWorkspace|TestValidateUserSharedWorkspaces' -v`

Expected: PASS。

- [ ] **Step 5: 提交配置改动**

```bash
git add config/config.go config/config_test.go
git commit -m "feat(config): add user shared workspaces"
```

---

### Task 2: 共享目录注册与 UserID 路由

**Files:**
- Modify: `core/engine.go:420-445`
- Modify: `core/user_workspace.go:1-125`
- Test: `core/user_workspace_test.go:146-385`

- [ ] **Step 1: 先写共享目录和路由失败测试**

在 `core/user_workspace_test.go` 增加：

```go
func configureUserSharedWorkspace(t *testing.T, e *Engine, name string) string {
	t.Helper()
	path := filepath.Join(e.baseDir, name)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := e.SetUserSharedWorkspaces([]string{name}); err != nil {
		t.Fatal(err)
	}
	return normalizeWorkspacePath(path)
}

func TestSetUserSharedWorkspacesValidatesDirectoriesAndCommands(t *testing.T) {
	baseDir := t.TempDir()
	e := NewEngine("test", nil, nil, "", LangChinese)
	e.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))

	if err := e.SetUserSharedWorkspaces([]string{"missing"}); err == nil {
		t.Fatal("missing shared workspace unexpectedly accepted")
	}
	if err := os.Mkdir(filepath.Join(baseDir, "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := e.SetUserSharedWorkspaces([]string{"new"}); err == nil {
		t.Fatal("built-in command collision unexpectedly accepted")
	}
	if err := os.WriteFile(filepath.Join(baseDir, "filelab"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.SetUserSharedWorkspaces([]string{"filelab"}); err == nil {
		t.Fatal("ordinary file unexpectedly accepted")
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(baseDir, "linklab")); err != nil {
		t.Fatal(err)
	}
	if err := e.SetUserSharedWorkspaces([]string{"linklab"}); err == nil {
		t.Fatal("symlink unexpectedly accepted")
	}
}

func TestUserSharedWorkspaceSelectionUsesUserIDAcrossChats(t *testing.T) {
	baseDir := t.TempDir()
	e := NewEngine("test", nil, nil, "", LangChinese)
	e.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	shared := configureUserSharedWorkspace(t, e, "medialab")
	e.setUserWorkspaceSelection("alice", "medialab")

	for _, sessionKey := range []string{"wecom:group-1:alice", "wecom:private-2:alice"} {
		msg := &Message{Platform: "wecom", SessionKey: sessionKey, UserID: "alice"}
		got, err := e.prepareUserWorkspace(msg)
		if err != nil || got != shared {
			t.Fatalf("prepareUserWorkspace(%q) = %q, %v; want %q", sessionKey, got, err, shared)
		}
	}

	bob := &Message{Platform: "wecom", SessionKey: "wecom:group-1:bob", UserID: "bob"}
	bobWorkspace, err := e.prepareUserWorkspace(bob)
	if err != nil {
		t.Fatal(err)
	}
	if bobWorkspace == shared {
		t.Fatal("bob unexpectedly inherited alice's shared workspace")
	}
}

func TestUserSharedWorkspaceSelectionClearsWhenDirectoryDisappears(t *testing.T) {
	baseDir := t.TempDir()
	e := NewEngine("test", nil, nil, "", LangChinese)
	e.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	shared := configureUserSharedWorkspace(t, e, "medialab")
	e.setUserWorkspaceSelection("alice", "medialab")
	if err := os.Remove(shared); err != nil {
		t.Fatal(err)
	}
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice"}
	_, err := e.prepareUserWorkspace(msg)
	var unavailable *userSharedWorkspaceUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Name != "medialab" {
		t.Fatalf("prepare error = %v, want medialab unavailable error", err)
	}
	if got := e.selectedUserSharedWorkspace("alice"); got != "" {
		t.Fatalf("selection = %q, want cleared", got)
	}
}

func TestUserSharedWorkspaceSelectionDoesNotRestoreFromBinding(t *testing.T) {
	baseDir := t.TempDir()
	storePath := filepath.Join(t.TempDir(), "bindings.json")
	shared := filepath.Join(baseDir, "medialab")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}

	first := NewEngine("test", nil, nil, "", LangChinese)
	t.Cleanup(first.cancel)
	first.SetUserWorkspace(baseDir, storePath)
	if err := first.SetUserSharedWorkspaces([]string{"medialab"}); err != nil {
		t.Fatal(err)
	}
	first.setUserWorkspaceSelection("alice", "medialab")
	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice"}
	if got, err := first.prepareUserWorkspace(msg); err != nil || got != normalizeWorkspacePath(shared) {
		t.Fatalf("first workspace = %q, %v", got, err)
	}

	restarted := NewEngine("test", nil, nil, "", LangChinese)
	t.Cleanup(restarted.cancel)
	restarted.SetUserWorkspace(baseDir, storePath)
	if err := restarted.SetUserSharedWorkspaces([]string{"medialab"}); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.prepareUserWorkspace(msg)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ensureUserWorkspaceDir(baseDir, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("workspace after restart = %q, want /user workspace %q", got, want)
	}
}
```

- [ ] **Step 2: 运行测试，确认新 API 缺失**

Run: `go test ./core/ -run 'TestSetUserShared|TestUserSharedWorkspaceSelection' -v`

Expected: FAIL，错误包含 `SetUserSharedWorkspaces undefined`。

- [ ] **Step 3: 在 Engine 增加最小状态字段**

在 `Engine` 的 user-workspace 字段旁增加：

```go
userWorkspaceMu         sync.RWMutex
userSharedWorkspaces    map[string]string // lower-case command name -> validated path
userWorkspaceSelections map[string]string // WeCom UserID -> shared workspace name; missing means /user
```

不要创建新的 Router 类型或持久化 store。

- [ ] **Step 4: 在现有 user_workspace.go 实现注册与选择解析**

先用完整初始化替换现有 `SetUserWorkspace`：

```go
func (e *Engine) SetUserWorkspace(baseDir, bindingStorePath string) {
	e.SetMultiWorkspace(baseDir, bindingStorePath)
	e.userWorkspace = true
	e.userWorkspaceMu.Lock()
	e.userSharedWorkspaces = make(map[string]string)
	e.userWorkspaceSelections = make(map[string]string)
	e.userWorkspaceMu.Unlock()
}
```

随后加入以下实现：

```go
type userSharedWorkspaceUnavailableError struct {
	Name string
	Err  error
}

func (e *userSharedWorkspaceUnavailableError) Error() string {
	return fmt.Sprintf("user-workspace: shared workspace %q unavailable: %v", e.Name, e.Err)
}

func (e *userSharedWorkspaceUnavailableError) Unwrap() error { return e.Err }

func (e *Engine) SetUserSharedWorkspaces(names []string) error {
	if !e.userWorkspace {
		return fmt.Errorf("user-workspace: shared workspaces require user-workspace mode")
	}
	workspaces := make(map[string]string, len(names))
	for _, name := range names {
		name = strings.ToLower(name)
		if name == "user" || matchPrefix(name, builtinCommands) != "" {
			return fmt.Errorf("user-workspace: shared workspace name %q conflicts with a command", name)
		}
		path := filepath.Join(e.baseDir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("user-workspace: inspect shared workspace %q: %w", name, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("user-workspace: shared workspace %q must be a real directory", name)
		}
		workspaces[name] = normalizeWorkspacePath(path)
	}
	e.userWorkspaceMu.Lock()
	e.userSharedWorkspaces = workspaces
	e.userWorkspaceSelections = make(map[string]string)
	e.userWorkspaceMu.Unlock()
	return nil
}

func (e *Engine) selectedUserSharedWorkspace(userID string) string {
	e.userWorkspaceMu.RLock()
	defer e.userWorkspaceMu.RUnlock()
	return e.userWorkspaceSelections[userID]
}

func (e *Engine) setUserWorkspaceSelection(userID, name string) {
	e.userWorkspaceMu.Lock()
	defer e.userWorkspaceMu.Unlock()
	if name == "" {
		delete(e.userWorkspaceSelections, userID)
		return
	}
	e.userWorkspaceSelections[userID] = name
}

func (e *Engine) resolveSelectedUserWorkspace(userID string) (string, error) {
	e.userWorkspaceMu.RLock()
	name := e.userWorkspaceSelections[userID]
	path := e.userSharedWorkspaces[name]
	e.userWorkspaceMu.RUnlock()
	if name == "" {
		return ensureUserWorkspaceDir(e.baseDir, userID)
	}
	info, err := os.Lstat(path)
	if err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	if err == nil {
		err = fmt.Errorf("path is not a real directory")
	}
	e.userWorkspaceMu.Lock()
	if e.userWorkspaceSelections[userID] == name {
		delete(e.userWorkspaceSelections, userID)
	}
	e.userWorkspaceMu.Unlock()
	return "", &userSharedWorkspaceUnavailableError{Name: name, Err: err}
}
```

修改 `prepareUserWorkspace` 和 `resolveWorkspaceForSessionKey`，把直接调用 `ensureUserWorkspaceDir` 的位置替换为：

```go
workspace, err := e.resolveSelectedUserWorkspace(msg.UserID)
```

以及：

```go
workspace, err := e.resolveSelectedUserWorkspace(userID)
```

保留后续 exact project binding 校验；binding 仍是派生路由数据，不从 binding 恢复选择。

- [ ] **Step 5: 运行 user-workspace 核心测试**

Run: `go test ./core/ -run 'TestEnsureUserWorkspace|TestUserWorkspace|TestSetUserShared' -v`

Expected: PASS。已有 strict resolver、UID 隔离和路径权限测试必须继续通过。

- [ ] **Step 6: 提交路由状态改动**

```bash
git add core/engine.go core/user_workspace.go core/user_workspace_test.go
git commit -m "feat(core): route user shared workspaces"
```

---

### Task 3: 动态切换命令、忙碌保护与 i18n

**Files:**
- Modify: `core/engine.go:2493-2515`
- Modify: `core/engine.go:6442-6575`
- Modify: `core/user_workspace.go`
- Modify: `core/i18n.go:620-645`
- Modify: `core/i18n.go:3980-4140`
- Test: `core/user_workspace_test.go`
- Test: `core/i18n_test.go`

- [ ] **Step 1: 先写命令行为失败测试**

增加以下测试；测试通过真实 `handleCommand`/`cmdNew` 路径，不直接修改 Session 内容：

```go
func TestUserWorkspaceSelectionCommandsAreCaseInsensitiveAndPerUser(t *testing.T) {
	e, platform, aliceUserWorkspace, _ := newUserWorkspaceExecutionEngine(t)
	shared := configureUserSharedWorkspace(t, e, "medialab")
	alice := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice", ReplyCtx: "ctx"}

	if !e.handleCommand(platform, alice, "/MediaLab") {
		t.Fatal("/MediaLab was not consumed")
	}
	if got := e.selectedUserSharedWorkspace("alice"); got != "medialab" {
		t.Fatalf("alice selection = %q", got)
	}
	otherChat := &Message{Platform: "wecom", SessionKey: "wecom:private-2:alice", UserID: "alice"}
	if got, err := e.prepareUserWorkspace(otherChat); err != nil || got != shared {
		t.Fatalf("alice other chat workspace = %q, %v; want %q", got, err, shared)
	}
	bob := &Message{Platform: "wecom", SessionKey: "wecom:group-1:bob", UserID: "bob"}
	if got, err := e.prepareUserWorkspace(bob); err != nil || got == shared {
		t.Fatalf("bob workspace = %q, %v; must remain private", got, err)
	}
	if !e.handleCommand(platform, alice, "/USER") {
		t.Fatal("/USER was not consumed")
	}
	if got, err := e.prepareUserWorkspace(alice); err != nil || got != aliceUserWorkspace {
		t.Fatalf("alice /user workspace = %q, %v; want %q", got, err, aliceUserWorkspace)
	}
}

func TestUserWorkspaceSelectionRejectsBusySessionInAnotherChat(t *testing.T) {
	e, platform, workspace, _ := newUserWorkspaceExecutionEngine(t)
	e.i18n = NewI18n(LangChinese)
	configureUserSharedWorkspace(t, e, "medialab")
	_, sessions, err := e.getOrCreateWorkspaceAgent(workspace)
	if err != nil {
		t.Fatal(err)
	}
	busyKey := "wecom:group-2:alice"
	busy := sessions.GetOrCreateActive(busyKey)
	if !busy.TryLock() {
		t.Fatal("failed to mark session busy")
	}
	defer busy.Unlock()

	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice", ReplyCtx: "ctx"}
	e.handleCommand(platform, msg, "/medialab")
	if got := e.selectedUserSharedWorkspace("alice"); got != "" {
		t.Fatalf("selection changed while another chat was busy: %q", got)
	}
	if sent := strings.Join(platform.getSent(), "\n"); !strings.Contains(sent, "请先执行 /stop") {
		t.Fatalf("busy reply = %q", sent)
	}
}

func TestUserSharedWorkspaceMissingConsumesCurrentMessage(t *testing.T) {
	e, platform, _, starts := newUserWorkspaceExecutionEngine(t)
	e.i18n = NewI18n(LangChinese)
	shared := configureUserSharedWorkspace(t, e, "medialab")
	e.setUserWorkspaceSelection("alice", "medialab")
	if err := os.Remove(shared); err != nil {
		t.Fatal(err)
	}
	before := *starts
	e.handleMessage(platform, &Message{
		Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice",
		Content: "must not run in /user", ReplyCtx: "ctx",
	})
	if *starts != before {
		t.Fatalf("agent starts = %d, want unchanged %d", *starts, before)
	}
	if sent := strings.Join(platform.getSent(), "\n"); !strings.Contains(sent, "已切回 `/user`") {
		t.Fatalf("missing workspace reply = %q", sent)
	}
}

func TestUserWorkspaceSelectionCommandsBypassAliasAndPreserveSessions(t *testing.T) {
	e, platform, userWorkspace, _ := newUserWorkspaceExecutionEngine(t)
	shared := configureUserSharedWorkspace(t, e, "medialab")
	e.AddAlias("/medialab", "/new")
	if got := e.resolveAlias("/medialab"); got != "/medialab" {
		t.Fatalf("resolveAlias = %q, want workspace command unchanged", got)
	}

	msg := &Message{Platform: "wecom", SessionKey: "wecom:group-1:alice", UserID: "alice", ReplyCtx: "ctx"}
	e.handleCommand(platform, msg, "/medialab")
	_, sharedSessions, err := e.getOrCreateWorkspaceAgent(shared)
	if err != nil {
		t.Fatal(err)
	}
	sharedBefore := sharedSessions.GetOrCreateActive(msg.SessionKey).ID
	e.cmdNew(platform, msg, nil)
	sharedAfter := sharedSessions.GetOrCreateActive(msg.SessionKey).ID
	if sharedAfter == sharedBefore {
		t.Fatal("/new did not rotate the medialab session")
	}

	e.handleCommand(platform, msg, "/user")
	_, userSessions, err := e.getOrCreateWorkspaceAgent(userWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if got := sharedSessions.GetOrCreateActive(msg.SessionKey).ID; got != sharedAfter {
		t.Fatalf("switching to /user changed medialab session from %q to %q", sharedAfter, got)
	}
	userBefore := userSessions.GetOrCreateActive(msg.SessionKey).ID
	e.cmdNew(platform, msg, nil)
	if got := userSessions.GetOrCreateActive(msg.SessionKey).ID; got == userBefore {
		t.Fatal("/new did not rotate the user workspace session")
	}
}
```

在 `core/i18n_test.go` 同步先写完整性测试：

```go
func TestI18n_UserWorkspaceSwitchMessagesHaveAllTranslations(t *testing.T) {
	keys := []MsgKey{
		MsgUserWsSwitchedShared,
		MsgUserWsSwitchedUser,
		MsgUserWsAlreadyShared,
		MsgUserWsAlreadyUser,
		MsgUserWsSwitchBusy,
		MsgUserWsSharedUnavailable,
		MsgUserWsSwitchUsage,
	}
	languages := []Language{LangEnglish, LangChinese, LangTraditionalChinese, LangJapanese, LangSpanish}
	for _, key := range keys {
		for _, lang := range languages {
			if messages[key][lang] == "" {
				t.Errorf("message key %q missing %s translation", key, lang)
			}
		}
	}
}
```

- [ ] **Step 2: 运行测试，确认命令尚未被识别**

Run: `go test ./core/ -run 'TestUserWorkspaceSelectionCommands' -v`

Expected: FAIL；`/MediaLab` 不会更新选择，忙碌提示不存在。

- [ ] **Step 3: 增加动态命令匹配和 alias 优先级保护**

在 `core/user_workspace.go` 增加：

```go
func (e *Engine) matchUserWorkspaceSelectionCommand(cmd string) (string, bool) {
	if !e.userWorkspace {
		return "", false
	}
	cmd = strings.ToLower(strings.TrimPrefix(cmd, "/"))
	e.userWorkspaceMu.RLock()
	defer e.userWorkspaceMu.RUnlock()
	if len(e.userSharedWorkspaces) == 0 {
		return "", false
	}
	if cmd == "user" {
		return "", true
	}
	_, ok := e.userSharedWorkspaces[cmd]
	return cmd, ok
}
```

在 `resolveAlias` 读取 alias map 前保护 bare workspace command：

```go
first, _, _ := strings.Cut(content, " ")
if strings.HasPrefix(first, "/") {
	if _, ok := e.matchUserWorkspaceSelectionCommand(first); ok {
		return content
	}
}
```

在 `handleCommand` 解析 `cmd` 和 `args` 后、调用 `matchPrefix` 前增加：

```go
if e.handleUserWorkspaceSelectionCommand(p, msg, cmd, args) {
	return true
}
```

- [ ] **Step 4: 实现跨聊天忙碌检查和切换处理**

在 `core/user_workspace.go` 的 import 中增加标准库 `errors`，然后增加：

```go
func (e *Engine) userHasBusyWorkspaceSession(userID string) bool {
	if e.workspacePool == nil {
		return false
	}
	// ponytail: workspace/session counts are small; add an index only if this scan is measured hot.
	for _, state := range e.workspacePool.All() {
		state.mu.Lock()
		sessions := state.sessions
		state.mu.Unlock()
		if sessions == nil {
			continue
		}
		idToKey, activeIDs := sessions.SessionKeyMap()
		for sessionID, sessionKey := range idToKey {
			if !activeIDs[sessionID] || userIDFromWeComSessionKey(sessionKey) != userID {
				continue
			}
			if session := sessions.FindByID(sessionID); session != nil && session.Busy() {
				return true
			}
		}
	}
	return false
}

func (e *Engine) handleUserWorkspaceSelectionCommand(p Platform, msg *Message, cmd string, args []string) bool {
	target, ok := e.matchUserWorkspaceSelectionCommand(cmd)
	if !ok {
		return false
	}
	if len(args) != 0 {
		e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgUserWsSwitchUsage, "/"+cmd))
		return true
	}
	current := e.selectedUserSharedWorkspace(msg.UserID)
	if current == target {
		if target == "" {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgUserWsAlreadyUser))
		} else {
			e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgUserWsAlreadyShared, target))
		}
		return true
	}
	if e.userHasBusyWorkspaceSession(msg.UserID) {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgUserWsSwitchBusy))
		return true
	}

	e.setUserWorkspaceSelection(msg.UserID, target)
	if _, err := e.prepareUserWorkspace(msg); err != nil {
		var unavailable *userSharedWorkspaceUnavailableError
		if errors.As(err, &unavailable) {
			e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgUserWsSharedUnavailable, unavailable.Name))
		} else {
			e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgWsResolutionError, err))
		}
		return true
	}
	if target == "" {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgUserWsSwitchedUser))
	} else {
		e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgUserWsSwitchedShared, target))
	}
	return true
}
```

在 `handleMessage` 的 `prepareUserWorkspace` 错误分支中优先处理运行时失效并终止当前消息：

```go
var unavailable *userSharedWorkspaceUnavailableError
if errors.As(err, &unavailable) {
	e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgUserWsSharedUnavailable, unavailable.Name))
	return
}
```

- [ ] **Step 5: 增加 MsgKey 和五种语言翻译**

定义以下键：

```go
MsgUserWsSwitchedShared   MsgKey = "user_ws_switched_shared"
MsgUserWsSwitchedUser     MsgKey = "user_ws_switched_user"
MsgUserWsAlreadyShared    MsgKey = "user_ws_already_shared"
MsgUserWsAlreadyUser      MsgKey = "user_ws_already_user"
MsgUserWsSwitchBusy       MsgKey = "user_ws_switch_busy"
MsgUserWsSharedUnavailable MsgKey = "user_ws_shared_unavailable"
MsgUserWsSwitchUsage      MsgKey = "user_ws_switch_usage"
```

翻译表使用以下实际文案：

```go
MsgUserWsSwitchedShared: {
	LangEnglish: "Switched to shared workspace: `%s`",
	LangChinese: "已切换到共享工作区：`%s`",
	LangTraditionalChinese: "已切換到共享工作區：`%s`",
	LangJapanese: "共有ワークスペースに切り替えました：`%s`",
	LangSpanish: "Se cambió al workspace compartido: `%s`",
},
MsgUserWsSwitchedUser: {
	LangEnglish: "Switched to your user workspace.",
	LangChinese: "已切换到用户工作区。",
	LangTraditionalChinese: "已切換到使用者工作區。",
	LangJapanese: "ユーザーワークスペースに切り替えました。",
	LangSpanish: "Se cambió a tu workspace de usuario.",
},
MsgUserWsAlreadyShared: {
	LangEnglish: "Already using shared workspace: `%s`",
	LangChinese: "当前已在共享工作区：`%s`",
	LangTraditionalChinese: "目前已在共享工作區：`%s`",
	LangJapanese: "すでに共有ワークスペースを使用中です：`%s`",
	LangSpanish: "Ya estás usando el workspace compartido: `%s`",
},
MsgUserWsAlreadyUser: {
	LangEnglish: "Already using your user workspace.",
	LangChinese: "当前已在用户工作区。",
	LangTraditionalChinese: "目前已在使用者工作區。",
	LangJapanese: "すでにユーザーワークスペースを使用中です。",
	LangSpanish: "Ya estás usando tu workspace de usuario.",
},
MsgUserWsSwitchBusy: {
	LangEnglish: "A task is still running. Run `/stop` before switching workspaces.",
	LangChinese: "当前任务正在运行，请先执行 `/stop`。",
	LangTraditionalChinese: "目前任務正在執行，請先執行 `/stop`。",
	LangJapanese: "タスクを実行中です。ワークスペースを切り替える前に `/stop` を実行してください。",
	LangSpanish: "Hay una tarea en ejecución. Ejecuta `/stop` antes de cambiar de workspace.",
},
MsgUserWsSharedUnavailable: {
	LangEnglish: "Shared workspace `%s` is unavailable. Switched back to `/user`; resend your message.",
	LangChinese: "共享工作区 `%s` 不可用，已切回 `/user`；请重新发送消息。",
	LangTraditionalChinese: "共享工作區 `%s` 不可用，已切回 `/user`；請重新傳送訊息。",
	LangJapanese: "共有ワークスペース `%s` は利用できません。`/user` に戻りました。メッセージを再送してください。",
	LangSpanish: "El workspace compartido `%s` no está disponible. Se volvió a `/user`; reenvía el mensaje.",
},
MsgUserWsSwitchUsage: {
	LangEnglish: "Usage: `%s`",
	LangChinese: "用法：`%s`",
	LangTraditionalChinese: "用法：`%s`",
	LangJapanese: "使い方：`%s`",
	LangSpanish: "Uso: `%s`",
},
```

- [ ] **Step 6: 运行命令、i18n 和 user-workspace 测试**

Run: `go test ./core/ -run 'TestUserWorkspace|TestI18n' -v`

Expected: PASS。

- [ ] **Step 7: 提交命令改动**

```bash
git add core/engine.go core/user_workspace.go core/user_workspace_test.go core/i18n.go core/i18n_test.go
git commit -m "feat(core): add user workspace switch commands"
```

---

### Task 4: 启动装配和配置文档

**Files:**
- Modify: `cmd/cc-connect/main.go:454-505`
- Modify: `config.example.toml:682-710`
- Modify: `docs/usage.md:961-990`
- Modify: `docs/usage.zh-CN.md:874-905`

- [ ] **Step 1: 在启动路径装配共享 workspace**

紧跟 `engine.SetUserWorkspace(baseDir, bindingStore)` 增加：

```go
engine.SetUserWorkspace(baseDir, bindingStore)
if err := engine.SetUserSharedWorkspaces(proj.SharedWorkspaces); err != nil {
	slog.Error("invalid user shared workspace configuration", "project", proj.Name, "error", err)
	os.Exit(1)
}
```

只在 `proj.Mode == "user-workspace"` 分支调用。目录不存在、是文件、是符号链接或命令名冲突时整个进程退出，不能 `continue` 后半启动。

- [ ] **Step 2: 更新示例配置**

在 `user-workspace` 示例或多工作区说明旁加入：

```toml
# mode = "user-workspace"
# base_dir = "/data/workspaces"
# shared_workspaces = ["medialab", "designlab"]
#
# Shared directories must already exist as real directories:
#   /data/workspaces/medialab
#   /data/workspaces/designlab
# Users switch with /medialab and return with /user.
```

- [ ] **Step 3: 更新中英文使用文档**

`docs/usage.md` 增加：

```markdown
### Shared workspaces in user-workspace mode

Set `shared_workspaces = ["medialab"]` on a WeCom WebSocket `user-workspace` project. The directory `base_dir/medialab` must already exist. `/medialab` switches every chat for the sending UserID to that shared directory; `/user` switches the user back. Each chat keeps a separate agent session inside each workspace. Selections reset to `/user` after restart.
```

`docs/usage.zh-CN.md` 增加：

```markdown
### user-workspace 共享工作区

在企业微信 WebSocket 的 `user-workspace` 项目中配置 `shared_workspaces = ["medialab"]`。目录 `base_dir/medialab` 必须预先存在。用户执行 `/medialab` 后，该 UserID 的所有群聊和私聊都切换到共享目录；执行 `/user` 切回用户目录。每个聊天在每个 workspace 内仍保留独立 Agent 会话。服务重启后默认回到 `/user`。
```

- [ ] **Step 4: 格式化并验证装配代码**

Run: `gofmt -w config/config.go config/config_test.go core/engine.go core/user_workspace.go core/user_workspace_test.go core/i18n.go core/i18n_test.go cmd/cc-connect/main.go`

Run: `go test ./config/ ./cmd/cc-connect/ ./core/ -run 'TestValidateUserSharedWorkspaces|TestUserWorkspace'`

Expected: PASS。

Run: `go build ./cmd/cc-connect`

Expected: PASS。

- [ ] **Step 5: 提交装配和文档**

```bash
git add cmd/cc-connect/main.go config.example.toml docs/usage.md docs/usage.zh-CN.md
git commit -m "docs: configure user shared workspaces"
```

---

### Task 5: Critical User Journey 和完成验证

**Files:**
- Modify: `core/cuj_test.go`

- [ ] **Step 1: 写跨群聊 CUJ**

在 H 组增加以下测试，使用真实 Engine 和 SessionManager，只 mock Agent 进程与平台：

```go
func TestCUJ_H4_UserSharedWorkspaceSelection(t *testing.T) {
	agentName := "cuj-user-shared-" + strings.ReplaceAll(t.Name(), "/", "-")
	starts := 0
	RegisterAgent(agentName, func(opts map[string]any) (Agent, error) {
		workDir, _ := opts["work_dir"].(string)
		return &userWorkspacePathAgent{name: agentName, workDir: workDir, starts: &starts}, nil
	})

	platform := &userWorkspaceTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "wecom"}}
	baseDir := t.TempDir()
	sharedDir := filepath.Join(baseDir, "medialab")
	if err := os.Mkdir(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	baseAgent := &userWorkspacePathAgent{name: agentName, workDir: "global", starts: &starts}
	engine := NewEngine("test", baseAgent, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangChinese)
	t.Cleanup(engine.cancel)
	engine.SetUserWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	if err := engine.SetUserSharedWorkspaces([]string{"medialab"}); err != nil {
		t.Fatal(err)
	}

	send := func(chatID, userID, content string) {
		t.Helper()
		engine.ReceiveMessage(platform, &Message{
			Platform: "wecom", SessionKey: "wecom:" + chatID + ":" + userID,
			MessageID: "msg-" + chatID + "-" + userID + "-" + content,
			UserID: userID, UserName: userID, Content: content, ReplyCtx: chatID,
		})
	}
	waitFor := func(needle string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(strings.Join(platform.getSent(), "\n"), needle) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("reply missing %q: %v", needle, platform.getSent())
	}

	send("group-1", "alice", "/medialab")
	waitFor("已切换到共享工作区")
	platform.clearSent()

	send("group-2", "alice", "shared task")
	waitFor(normalizeWorkspacePath(sharedDir))
	platform.clearSent()

	send("group-1", "bob", "bob task")
	bobDir, err := ensureUserWorkspaceDir(baseDir, "bob")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(bobDir)
	platform.clearSent()

	send("group-2", "alice", "/user")
	waitFor("已切换到用户工作区")
	platform.clearSent()

	send("group-1", "alice", "private task")
	aliceDir, err := ensureUserWorkspaceDir(baseDir, "alice")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(aliceDir)
	platform.clearSent()

	send("group-1", "alice", "/medialab")
	waitFor("已切换到共享工作区")
	platform.clearSent()
	send("group-2", "alice", "/history 10")
	waitFor("shared task")
}
```

- [ ] **Step 2: 运行新增 CUJ**

Run: `go test ./core/ -run TestCUJ_H4_UserSharedWorkspaceSelection -v`

Expected: PASS，平台回复证明跨群聊选择、用户隔离、`/user` 回退和共享会话恢复全部成立。

- [ ] **Step 3: 运行所有 CUJ**

Run: `go test ./core/ -run TestCUJ -v`

Expected: PASS。

- [ ] **Step 4: 运行全量测试、race 和构建**

Run: `go test ./...`

Expected: PASS。

Run: `go test -race ./core/ ./config/`

Expected: PASS，无 data race。

Run: `go build ./...`

Expected: PASS。

- [ ] **Step 5: 检查差异范围和硬编码**

Run: `git diff --check`

Expected: 无输出。

Run: `git diff --stat 3686d88..HEAD`

Expected: 只包含本计划列出的配置、core、cmd、文档和测试文件。

Run: `rg -n 'feishu|telegram|discord|slack' core/user_workspace.go`

Expected: 无输出；user-workspace 仅允许已有的通用 core 能力和 WeCom session key 解析，不向 core 添加其他平台名称。

- [ ] **Step 6: 提交 CUJ**

```bash
git add core/cuj_test.go
git commit -m "test(core): cover user shared workspace journey"
```

- [ ] **Step 7: 请求代码审查并处理发现**

使用 `requesting-code-review` skill，审查范围为设计提交 `3686d88` 到当前 HEAD。重点检查：

```text
1. UserID 选择是否会泄漏到其他用户
2. /new、/history 和恢复是否始终使用当前 workspace 的 SessionManager
3. 共享目录失效时原消息是否被终止
4. 忙碌检查和选择 map 是否存在 data race
5. 启动失败是否真的拒绝半启动
```

修复审查发现后，重新执行 Step 2-5 的全部验证，再进入 `finishing-a-development-branch`。
