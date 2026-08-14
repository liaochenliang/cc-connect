# WeCom Streaming Turn Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stream WeCom WebSocket full-mode thinking, tool activity, and final answers through quoted `aibot_respond_msg` replies.

**Architecture:** Add one neutral optional core streaming-turn capability and implement it only on `wecom.WSPlatform`. Core builds localized Markdown from existing event formatting; the adapter owns WeCom frame IDs, byte limits, overflow, and failure state.

**Tech Stack:** Go, existing Gorilla WebSocket transport, standard library testing.

---

### Task 1: WeCom Streaming Protocol

**Files:**
- Create: `platform/wecom/streaming_turn_test.go`
- Create: `platform/wecom/streaming_turn.go`

- [ ] **Step 1: Write failing protocol tests**

Add tests that create a turn with `wsReplyContext{reqID: "callback-1"}`, capture outgoing frames, and assert:

```go
turn, err := p.CreateStreamingTurn(ctx, wsReplyContext{reqID: "callback-1"})
if err != nil { t.Fatal(err) }
if err := turn.Update(ctx, "thinking"); err != nil { t.Fatal(err) }
if err := turn.Finalize(ctx, "thinking\n\nanswer"); err != nil { t.Fatal(err) }
```

The two frames must reuse `callback-1` and one stream ID, with `finish=false` then `finish=true`. Add an overflow case containing multibyte text that reconstructs exactly, keeps each `content` at or below 20,480 bytes, reuses the callback ID, and uses a new stream ID per completed segment. Add invalid reply-context and write-failure cases.

- [ ] **Step 2: Verify the tests fail**

Run: `go test ./platform/wecom -run 'TestStreamingTurn' -count=1 -v`

Expected: FAIL because `CreateStreamingTurn` and the protocol implementation do not exist.

- [ ] **Step 3: Implement the minimum adapter**

Create `streaming_turn.go` with the protocol limit, an active stream ID, sent-prefix tracking, and a permanent failed flag. Reuse `WSPlatform.generateReqID`, `WSPlatform.writeJSON`, and `splitByBytes`; do not add a transport wrapper or dependency.

- [ ] **Step 4: Verify the protocol tests pass**

Run: `go test ./platform/wecom -run 'TestStreamingTurn' -count=1 -v`

Expected: PASS.

### Task 2: Core Event Routing

**Files:**
- Modify: `core/interfaces.go`
- Modify: `core/engine.go`
- Modify: `core/engine_test.go`

- [ ] **Step 1: Write failing engine tests**

Add a recording platform implementing the wished-for capability and feed one turn through the real event loop:

```go
events <- Event{Type: EventThinking, Content: "plan"}
events <- Event{Type: EventToolUse, ToolName: "Bash", ToolInput: "pwd"}
events <- Event{Type: EventToolResult, ToolName: "Bash", ToolResult: "/tmp"}
events <- Event{Type: EventText, Content: "draft"}
events <- Event{Type: EventResult, Content: "final", Done: true}
```

Assert the updates preserve that order, the final payload contains `final` rather than stale `draft`, and no ordinary platform message is sent. Add table cases proving compact and quiet modes do not create the turn, plus cases for update failure fallback and bare `NO_REPLY` suppression.

- [ ] **Step 2: Verify the engine tests fail**

Run: `go test ./core -run 'TestProcessInteractiveEvents_StreamingTurn' -count=1 -v`

Expected: FAIL because the optional interface and event routing do not exist.

- [ ] **Step 3: Add the optional capability and minimal routing**

Define:

```go
type StreamingTurn interface {
    Update(context.Context, string) error
    Finalize(context.Context, string) error
    Failed() bool
}

type StreamingTurnPlatform interface {
    CreateStreamingTurn(context.Context, any) (StreamingTurn, error)
}
```

In `processInteractiveEvents`, create it only for `display.Mode == "full"`, route the four requested event kinds into stable Markdown, and retain the existing branches whenever the turn is absent or failed. Finalize with the normalized terminal answer and retain existing silent-reply behavior.

- [ ] **Step 4: Verify engine tests and CUJs pass**

Run: `go test ./core -run 'TestProcessInteractiveEvents_StreamingTurn' -count=1 -v`

Run: `go test ./core -run TestCUJ -count=1`

Expected: PASS.

### Task 3: Complete Verification and Review

**Files:**
- Review only the files changed by Tasks 1 and 2.

- [ ] **Step 1: Format and run focused tests**

Run: `gofmt -w core/interfaces.go core/engine.go core/engine_test.go platform/wecom/streaming_turn.go platform/wecom/streaming_turn_test.go`

Run: `go test ./core ./platform/wecom -count=1`

Expected: PASS.

- [ ] **Step 2: Run full repository verification**

Run: `go test ./... -count=1`

Run: `go build ./...`

Expected: both commands exit 0.

- [ ] **Step 3: Inspect impact and simplify**

Run GitNexus change detection, inspect `git diff --check`, and review only newly changed code for unnecessary abstractions, duplicated formatting, protocol errors, concurrency hazards, and missing failure paths. Apply only fixes required by the approved design, then repeat focused and full verification.

- [ ] **Step 4: Commit the implementation**

```bash
git add docs/superpowers/specs/2026-08-14-wecom-streaming-turn-design.md \
  docs/superpowers/plans/2026-08-14-wecom-streaming-turn.md \
  core/interfaces.go core/engine.go core/engine_test.go \
  platform/wecom/streaming_turn.go platform/wecom/streaming_turn_test.go
git commit -m "feat(wecom): stream full turn replies"
```
