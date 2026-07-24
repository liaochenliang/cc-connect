# /help 命令精简 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 从纯文本和卡片 `/help` 隐藏指定命令，移除权限模式尾注，并把 `/restart` 的帮助描述改为 `connect`。

**Architecture:** 复用现有 `MsgHelp` 多语言文本和 `helpCardGroups()`，不增加配置或抽象。只改帮助数据源，保留命令注册、执行行为以及当前工作区已有的动态 `/user`、`/<shared-workspace>` 帮助。

**Tech Stack:** Go、标准库 `strings`、现有 `testing`。

---

### Task 1: 用失败测试固定帮助行为

**Files:**
- Modify: `core/engine_test.go:3692`
- Modify: `core/engine_test.go:6316`

- [ ] **Step 1: 扩展纯文本帮助测试**

在 `TestCmdHelp_UsesLegacyTextOnPlatformWithoutCardSupport` 的现有断言后加入：

```go
for _, command := range []string{
	"/usage", "/upgrade", "/status", "/version",
	"/provider [list|add|remove|switch|clear]",
	"/model [switch <名称>]", "/allow <工具名>",
	"/reasoning [级别]", "/mode [名称]",
	"/lang [en|zh|zh-TW|ja|es|auto]", "/tts [always|voice_only]",
	"/shell [--timeout <秒>] <命令>", "/show <引用>",
	"/dir [路径|reset]", "/bind [项目名|remove]", "/workspace [init]",
} {
	if strings.Contains(p.sent[0], "\n"+command+"\n") {
		t.Errorf("help text contains hidden command %q", command)
	}
}
if strings.Contains(p.sent[0], "权限模式：default / edit / plan / yolo") {
	t.Error("help text contains permission mode footer")
}
if !strings.Contains(p.sent[0], "/restart\n  重启 connect 服务") {
	t.Errorf("help text missing connect restart description: %q", p.sent[0])
}
```

- [ ] **Step 2: 更新卡片帮助测试**

把 `TestHandleCardNav_HelpSwitchesTabs` 替换为：

```go
func TestHandleCardNav_HelpHidesConfiguredCommands(t *testing.T) {
	e := NewEngine("test", &stubAgent{}, []Platform{&stubPlatformEngine{n: "test"}}, "", LangChinese)
	tests := []struct {
		group  string
		keep   string
		hidden []string
	}{
		{"agent", "/memory", []string{"/model", "/reasoning", "/mode", "/lang", "/provider", "/allow", "/tts"}},
		{"tools", "/cron", []string{"/shell", "/show"}},
		{"system", "/restart", []string{"/status", "/usage", "/bind", "/workspace", "/dir", "/version", "/upgrade"}},
	}
	for _, tt := range tests {
		t.Run(tt.group, func(t *testing.T) {
			card := e.handleCardNav("nav:/help "+tt.group, "test:user1")
			if card == nil {
				t.Fatal("expected help nav card")
			}
			text := card.RenderText()
			if !strings.Contains(text, "**"+tt.keep+"**") {
				t.Errorf("%s help text missing %s: %q", tt.group, tt.keep, text)
			}
			for _, command := range tt.hidden {
				if strings.Contains(text, "**"+command+"**") {
					t.Errorf("%s help text contains hidden command %s: %q", tt.group, command, text)
				}
			}
		})
	}
	systemText := e.handleCardNav("nav:/help system", "test:user1").RenderText()
	if !strings.Contains(systemText, "**/restart**  重启 connect 服务") {
		t.Errorf("system help missing connect restart description: %q", systemText)
	}
}
```

- [ ] **Step 3: 增加五语言描述测试**

```go
func TestHelpRestartDescriptionUsesConnect(t *testing.T) {
	tests := []struct {
		lang        Language
		text        string
		description string
	}{
		{LangEnglish, "/restart\n  Restart connect service", "Restart connect service"},
		{LangChinese, "/restart\n  重启 connect 服务", "重启 connect 服务"},
		{LangTraditionalChinese, "/restart\n  重啟 connect 服務", "重啟 connect 服務"},
		{LangJapanese, "/restart\n  connect サービスを再起動", "connect サービスを再起動"},
		{LangSpanish, "/restart\n  Reiniciar el servicio connect", "Reiniciar el servicio connect"},
	}
	for _, tt := range tests {
		t.Run(string(tt.lang), func(t *testing.T) {
			i18n := NewI18n(tt.lang)
			if help := i18n.T(MsgHelp); !strings.Contains(help, tt.text) {
				t.Errorf("MsgHelp missing %q: %q", tt.text, help)
			}
			if got := i18n.T(MsgBuiltinCmdRestart); got != tt.description {
				t.Errorf("MsgBuiltinCmdRestart = %q, want %q", got, tt.description)
			}
		})
	}
}
```

- [ ] **Step 4: 确认 RED**

Run: `go test ./core -run 'TestCmdHelp_UsesLegacyTextOnPlatformWithoutCardSupport|TestHandleCardNav_HelpHidesConfiguredCommands|TestHelpRestartDescriptionUsesConnect' -count=1`

Expected: FAIL，原因是旧帮助仍展示命令或仍使用 `cc-connect`，不能是编译错误。

### Task 2: 最小修改帮助数据

**Files:**
- Modify: `core/i18n.go:988`
- Modify: `core/i18n.go:3727`
- Modify: `core/engine.go:9295`

- [ ] **Step 1: 精简五语言 `MsgHelp`**

从 EN、ZH、ZH-TW、JA、ES 五段中删除 `/usage`、`/upgrade`、`/status`、`/version`、`/provider`、`/model`、`/allow`、`/reasoning`、`/mode`、`/lang`、`/tts`、`/shell`、`/show`、`/dir`、`/bind`、`/workspace` 的完整“命令 + 描述”拼接，并删除五语言的权限模式尾注。

保留 `/restart`，五语言描述精确改为：

```text
Restart connect service
重启 connect 服务
重啟 connect 服務
connect サービスを再起動
Reiniciar el servicio connect
```

- [ ] **Step 2: 精简 `helpCardGroups()`**

Agent 页只保留 `/memory`、`/quiet`；Tools 页只删除 `/shell`、`/show`；System 页只保留 `/doctor`、`/config`、`/restart`。不要修改 `renderHelpGroupCard()` 追加 `e.userWorkspaceHelpItems()` 的逻辑。

- [ ] **Step 3: 更新卡片描述**

```go
MsgBuiltinCmdRestart: {
	LangEnglish:            "Restart connect service",
	LangChinese:            "重启 connect 服务",
	LangTraditionalChinese: "重啟 connect 服務",
	LangJapanese:           "connect サービスを再起動",
	LangSpanish:            "Reiniciar el servicio connect",
},
```

保持 `MsgRestarting` 与 `MsgRestartSuccess` 原样。

- [ ] **Step 4: 确认 GREEN**

Run: `gofmt -w core/engine.go core/engine_test.go core/i18n.go`

Run: `go test ./core -run 'TestCmdHelp_UsesLegacyTextOnPlatformWithoutCardSupport|TestHandleCardNav_HelpHidesConfiguredCommands|TestHelpRestartDescriptionUsesConnect|TestUserWorkspaceHelpListsSelectionCommands' -count=1`

Expected: PASS。

- [ ] **Step 5: 用 code-simplifier 复查**

只复查本次测试和帮助数据，删除重复或无意义代码；不重构 `MsgHelp`，不改其他未提交功能。

### Task 3: 验证、审查与交付

**Files:**
- Verify: `core/engine.go`
- Verify: `core/engine_test.go`
- Verify: `core/i18n.go`
- Verify: `core/user_workspace.go`

- [ ] **Step 1: 运行核心验证**

Run: `go test ./core -count=1`

Run: `go test ./core -run TestCUJ -count=1`

Expected: PASS。

- [ ] **Step 2: 运行完整验证**

Run: `go test ./... -count=1`

Run: `go build ./...`

Run: `git diff --check`

Expected: 全部退出码为 0。

- [ ] **Step 3: 本地代码审查**

确认 diff 没有修改 `builtinCommands`、命令处理器、Telegram 注册、`MsgRestarting` 或 `MsgRestartSuccess`。按 code-review 目标检查 bug、边界回归和缺失测试；当前没有 PR，因此不发布 GitHub 评论。

- [ ] **Step 4: 提交功能代码**

仅暂存本功能与已批准的 `/user`、`/medialab` 帮助文件，明确排除 `AGENTS.md`；提交前逐行核对 staged diff：

```bash
git add -- core/engine.go core/engine_test.go core/i18n.go core/i18n_test.go core/user_workspace.go core/user_workspace_test.go cmd/cc-connect/main.go cmd/cc-connect/main_test.go docs/superpowers/plans/2026-07-23-user-workspace-help-visibility.md
git diff --cached --check
git diff --cached
git commit -m "feat(core): tailor workspace help commands"
```

- [ ] **Step 5: 更新部署并上线验证**

把新源码提交写入 `k8s-agent/k8s-codex/cc-connect/source.commit`，同步两份 README 的 commit，运行构建脚本和 `k8s-codex/tests/test_cc_connect.sh`，再构建、推送、发布镜像。上线后在企业微信验证 `/user`、`/medialab`、`/restart` 可见，被隐藏条目不可见，且 `/restart` 描述为“重启 connect 服务”。
