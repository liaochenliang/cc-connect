# WeCom Streaming Turn Design

## Goal

In WeCom intelligent-bot WebSocket mode, use `aibot_respond_msg` to keep one quoted reply updated with the current model thinking, tool calls, tool results, and final answer. Enable this only for `[display].mode = "full"`.

## Scope

- The WeCom WebSocket adapter opts into a new optional core capability.
- The WeCom Webhook adapter, proactive messages without an inbound `req_id`, Slack, DingTalk, and all other platforms keep their current behavior.
- No new configuration or dependency is added.
- The WeCom implementation lives in `platform/wecom/streaming_turn.go`.

## Core Contract

Core defines a neutral optional `StreamingTurnPlatform` capability. A supporting platform creates a `StreamingTurn` from the inbound reply context. The turn accepts full-replacement Markdown updates, finalizes the response, and reports permanent failure so the engine can use its existing send path.

The engine creates the turn only when display mode is `full`. It maintains the visible turn Markdown from existing formatted events:

1. `EventThinking` updates the thinking section after applying `thinking_max_len`.
2. `EventToolUse` appends a numbered tool-call section after applying `tool_max_len` and the existing tool-input formatting.
3. `EventToolResult` appends the existing localized tool-result fallback after applying `tool_max_len`.
4. `EventText` updates the answer section in real time while preserving the existing `NO_REPLY` hold behavior.
5. Terminal `EventResult` replaces the streamed answer with the final normalized answer and finalizes the turn.

The format is ordinary Markdown with stable process and answer sections. It does not use undocumented WeCom fields or claim native collapsible-thinking support.

## WeCom Protocol

Every update is an `aibot_respond_msg` frame with the original callback `headers.req_id`, `body.msgtype = "stream"`, and a generated `stream.id`. Updates to one segment reuse that stream ID and set `finish = false`; finalization sets `finish = true`.

Each frame waits for the existing WebSocket ACK path. A non-zero ACK, timeout, local write error, or canceled context permanently fails the turn so core can use its fallback.

`stream.content` must not exceed 20,480 UTF-8 bytes. If the complete turn exceeds that limit, the adapter finalizes the current segment and continues with the same callback `req_id` and a new stream ID. Segments preserve all process content and the complete answer in order. Each segment is therefore a quoted response associated with the triggering inbound message.

If a later full-replacement update changes content that was already finalized, the adapter first finalizes any open segment, then starts fresh quoted continuation streams containing the corrected complete snapshot. This favors complete, finalized output over leaving an unfinishable stale stream.

## Failure Behavior

- Creation failure leaves the turn disabled and core uses the existing message path.
- Update failure first tries to finish any previously acknowledged open segment, then permanently fails the turn; core stops streaming process events and sends the final answer through the existing path.
- Overflow advances to a new stream ID only after the current segment is finalized successfully.
- If corrected content shrinks exactly to the finalized prefix, the adapter clears the open tail with an empty full-replacement frame and `finish = true`.
- Stop, timeout, agent error, or unexpected channel close finalizes any active turn with the visible content accumulated so far.
- Context cancellation or a disconnected WebSocket stops further writes and returns the error.
- Bare and trailing partial `NO_REPLY` markers are never rendered. A silent turn finalizes any already-created stream without the marker.

## Tests

- WeCom protocol tests assert same `req_id`, ACK failures, stable stream ID for updates, `finish` transitions, UTF-8 byte limits, corrected overflow finalization, and new stream IDs for overflow.
- Core tests assert full-mode event order and content, final-answer reconciliation, bare/trailing `NO_REPLY` suppression, abnormal finalization, failure fallback, and that compact/quiet modes do not create a streaming turn.
- Existing WeCom, core CUJ, full Go tests, race-relevant package tests, and build are run before completion.
