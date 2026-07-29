# Quiet Instant Reply Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** quiet 模式在空闲会话收到普通消息后立即发送本地化的 `MsgStarting`，忙碌会话保持现有排队提示。

**Architecture:** 在 `handleMessage` 成功取得空闲会话锁后检测当前消息语言，并复用 `e.send` 和 `MsgStarting`；busy 分支在此之前返回。消息、排队项和交互状态保存“已发送即时回复”快照，避免 mode 切换或 queued turn 造成重复；cron、timer 和非 quiet 的 `[instant_reply]` 行为保持不变。

**Tech Stack:** Go、标准库 `testing`、现有 Engine 测试桩

**预计耗时:** 约 10 分钟

---

## 文件范围

- Modify: `core/message.go`：保存当前入站消息是否已发送即时回复。
- Modify: `core/engine.go`：空闲任务入口发送 quiet 提示，并将快照传入事件处理。
- Test: `core/engine_test.go`：覆盖 quiet 空闲和 busy 两种用户可见行为。

### Task 1: 用回归测试固定 quiet 即时回复行为

**Files:**
- Test: `core/engine_test.go:13770`

- [x] **Step 1: 写空闲、mode 切换和 busy 回归测试**

在现有 instant reply 测试旁新增：

```go
func TestHandleMessage_QuietSendsStartingForStreamingCardPlatform(t *testing.T) {
	p := &stubStreamingCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "dingtalk"}}
	agentSession := newResultAgentSession("agent reply")
	agent := &resultAgent{session: agentSession}
	e := NewEngine("test", agent, []Platform{p}, "", LangAuto)
	e.SetDisplayConfig(DisplayCfg{Mode: "quiet"})

	e.handleMessage(p, &Message{
		SessionKey: "dingtalk:user1",
		Platform:   "dingtalk",
		UserID:     "u1",
		UserName:   "user",
		Content:    "请处理这个任务",
		ReplyCtx:   "ctx",
	})

	sent := waitForPlatformSend(&p.stubPlatformEngine, 1, time.Second)
	want := messages[MsgStarting][LangChinese]
	if len(sent) != 1 || sent[0] != want {
		t.Fatalf("sent = %v, want only %q", sent, want)
	}
}

func TestHandleMessage_QuietBusySessionKeepsQueuedReply(t *testing.T) {
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangChinese)
	e.SetDisplayConfig(DisplayCfg{Mode: "quiet"})

	key := "test:user1"
	session := e.sessions.GetOrCreateActive(key)
	if !session.TryLock() {
		t.Fatal("expected session lock")
	}
	defer session.Unlock()

	e.interactiveMu.Lock()
	e.interactiveStates[key] = &interactiveState{
		agentSession: newControllableSession("running"),
		platform:     p,
		replyCtx:     "ctx-running",
	}
	e.interactiveMu.Unlock()

	e.handleMessage(p, &Message{
		SessionKey: key,
		Platform:   "test",
		UserID:     "u1",
		UserName:   "user",
		Content:    "second task",
		ReplyCtx:   "ctx-queued",
	})

	sent := p.getSent()
	if len(sent) != 1 || sent[0] != e.i18n.T(MsgMessageQueued) {
		t.Fatalf("sent = %v, want only %q", sent, e.i18n.T(MsgMessageQueued))
	}
}
```

另新增 `TestHandleMessage_QuietAcknowledgementSurvivesModeChange`，用阻塞的 `StartSession` 在提示发送后切换到 full，并断言配置的即时回复不会重复出现。

- [x] **Step 2: 运行测试并确认 RED**

Run:

```bash
go test ./core -run 'TestHandleMessage_Quiet(SendsStartingForStreamingCardPlatform|BusySessionKeepsQueuedReply)$' -count=1 -v
```

Expected: `TestHandleMessage_QuietSendsStartingForStreamingCardPlatform` 因没有收到 `MsgStarting` 而失败；busy 测试通过。

### Task 2: 在空闲任务入口发送 quiet 提示

**Files:**
- Modify: `core/message.go:239`
- Modify: `core/engine.go:3175`
- Modify: `core/engine.go:4849`
- Modify: `core/engine.go:6008`

- [x] **Step 1: 写最小实现**

在 `go e.processInteractiveMessageWith(...)` 之前检测语言、发送提示并记录消息快照：

```go
if e.display.Mode == "quiet" {
	e.i18n.DetectAndSet(msg.Content)
	e.send(p, msg.ReplyCtx, e.i18n.T(MsgStarting))
	msg.instantReplySent = true
}
```

将快照复制到 `interactiveState`，并让 `processInteractiveEvents` 根据快照决定是否执行旧机制：

```go
if !instantReplySent && e.instantReply.Enabled && streamCard == nil {
```

queued turn 使用入队时保存的 quiet 快照；quiet busy 消息保持单一排队提示，非 quiet 继续使用旧即时回复行为。

- [x] **Step 2: 格式化并确认 GREEN**

Run:

```bash
gofmt -w core/engine.go core/engine_test.go
go test ./core -run 'TestHandleMessage_Quiet(SendsStartingForStreamingCardPlatform|BusySessionKeepsQueuedReply)$' -count=1 -v
```

Expected: 两个测试均 PASS。

- [x] **Step 3: 验证相邻即时回复行为**

Run:

```bash
go test ./core -run 'TestHandleMessage_(Quiet|InstantReply)' -count=1 -v
```

Expected: quiet、显式 instant reply、流式卡片和 Slash 命令测试全部 PASS。

### Task 3: 完整验证与审查

**Files:**
- Verify: `core/engine.go`
- Verify: `core/engine_test.go`

- [x] **Step 1: 运行 core CUJ**

Run:

```bash
go test ./core/ -run TestCUJ -count=1
```

Expected: PASS。

Result: CUJ 功能断言通过；A3/A5 在整包运行时存在既有异步 `TempDir` 清理偶发失败，单独运行通过。

- [x] **Step 2: 运行仓库验证**

Run:

```bash
go test ./...
go build ./...
```

Expected: 两条命令退出码均为 0。

Result: `go build ./...` 通过；`go test ./...` 仍有基线已存在的 Codex 偶发、macOS 临时路径和 launchd 环境失败。

- [x] **Step 3: 检查改动影响与代码质量**

运行 GitNexus `detect_changes(scope: "all")`，再按 `code-review` 和 `code-simplifier` skill 检查 diff。Expected: 只影响 quiet 首次任务回复及对应测试，无新依赖、配置或抽象。

- [ ] **Step 4: 提交实现**

```bash
git add core/engine.go core/engine_test.go docs/superpowers/plans/2026-07-29-quiet-instant-reply.md
git commit -m "feat(core): acknowledge tasks in quiet mode"
```
