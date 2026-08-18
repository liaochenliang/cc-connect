package wecom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testStreamingTurnContentLimit = 20480

type capturedStreamingTurnFrame struct {
	Cmd     string `json:"cmd"`
	Headers struct {
		ReqID string `json:"req_id"`
	} `json:"headers"`
	Body struct {
		MsgType string `json:"msgtype"`
		Stream  struct {
			ID      string `json:"id"`
			Finish  bool   `json:"finish"`
			Content string `json:"content"`
		} `json:"stream"`
	} `json:"body"`
}

func newStreamingTurnCapture(t *testing.T, frameCount int) (*WSPlatform, <-chan capturedStreamingTurnFrame) {
	return newStreamingTurnCaptureWithAck(t, frameCount, 0)
}

func newStreamingTurnCaptureWithAck(t *testing.T, frameCount, ackErrCode int) (*WSPlatform, <-chan capturedStreamingTurnFrame) {
	ackErrCodes := make([]int, frameCount)
	for i := range ackErrCodes {
		ackErrCodes[i] = ackErrCode
	}
	return newStreamingTurnCaptureWithAcks(t, ackErrCodes)
}

func newStreamingTurnCaptureWithAcks(t *testing.T, ackErrCodes []int) (*WSPlatform, <-chan capturedStreamingTurnFrame) {
	t.Helper()

	frameCount := len(ackErrCodes)
	frames := make(chan capturedStreamingTurnFrame, frameCount)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for i := range frameCount {
			var frame capturedStreamingTurnFrame
			if err := conn.ReadJSON(&frame); err != nil {
				return
			}
			frames <- frame
			if err := conn.WriteJSON(map[string]any{
				"headers": map[string]string{"req_id": frame.Headers.ReqID},
				"errcode": ackErrCodes[i],
				"errmsg":  "test ack",
			}); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial capture websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	p := &WSPlatform{conn: conn}
	go func() {
		for range frameCount {
			var ack wsFrame
			if err := conn.ReadJSON(&ack); err != nil {
				return
			}
			p.handleFrame(ack)
		}
	}()
	return p, frames
}

func receiveStreamingTurnFrames(t *testing.T, frames <-chan capturedStreamingTurnFrame, count int) []capturedStreamingTurnFrame {
	t.Helper()

	got := make([]capturedStreamingTurnFrame, 0, count)
	for range count {
		select {
		case frame := <-frames:
			got = append(got, frame)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for streaming turn frame %d", len(got)+1)
		}
	}
	return got
}

func TestStreamingTurn_ReusesCallbackAndStreamIDs(t *testing.T) {
	p, frames := newStreamingTurnCapture(t, 2)
	turn, err := p.CreateStreamingTurn(context.Background(), wsReplyContext{reqID: "callback-1"})
	if err != nil {
		t.Fatalf("create streaming turn: %v", err)
	}

	if err := turn.Update(context.Background(), "## Process\n\nplanning"); err != nil {
		t.Fatalf("update streaming turn: %v", err)
	}
	final := "## Process\n\nplanning\n\n## Answer\n\ndone"
	if err := turn.Finalize(context.Background(), final); err != nil {
		t.Fatalf("finalize streaming turn: %v", err)
	}

	got := receiveStreamingTurnFrames(t, frames, 2)
	for i, frame := range got {
		if frame.Cmd != "aibot_respond_msg" || frame.Headers.ReqID != "callback-1" || frame.Body.MsgType != "stream" {
			t.Fatalf("frame %d has unexpected envelope: %+v", i, frame)
		}
	}
	if got[0].Body.Stream.ID == "" || got[1].Body.Stream.ID != got[0].Body.Stream.ID {
		t.Fatalf("stream IDs = %q, %q; want one stable non-empty ID", got[0].Body.Stream.ID, got[1].Body.Stream.ID)
	}
	if got[0].Body.Stream.Finish || !got[1].Body.Stream.Finish {
		t.Fatalf("finish flags = %v, %v; want false, true", got[0].Body.Stream.Finish, got[1].Body.Stream.Finish)
	}
	if got[0].Body.Stream.Content != "## Process\n\nplanning" || got[1].Body.Stream.Content != final {
		t.Fatalf("unexpected stream content: %q, %q", got[0].Body.Stream.Content, got[1].Body.Stream.Content)
	}
}

func TestStreamingTurnContentUsesThinkBlock(t *testing.T) {
	p := &WSPlatform{}
	if got := p.FormatStreamingTurnContent([]string{"planning", "running tool"}, "answer"); got != "<think>\nplanning\n\n---\n\nrunning tool\n</think>\n\nanswer" {
		t.Fatalf("formatted content = %q", got)
	}
	if got := p.FormatStreamingTurnContent(nil, "answer"); got != "answer" {
		t.Fatalf("pure answer content = %q, want answer", got)
	}
}

func TestStreamingTurn_OverflowUsesQuotedContinuationStreams(t *testing.T) {
	p, frames := newStreamingTurnCapture(t, 2)
	turn, err := p.CreateStreamingTurn(context.Background(), wsReplyContext{reqID: "callback-overflow"})
	if err != nil {
		t.Fatalf("create streaming turn: %v", err)
	}

	content := strings.Repeat("你", 8000)
	if err := turn.Finalize(context.Background(), content); err != nil {
		t.Fatalf("finalize streaming turn: %v", err)
	}

	got := receiveStreamingTurnFrames(t, frames, 2)
	var rebuilt strings.Builder
	for i, frame := range got {
		if frame.Headers.ReqID != "callback-overflow" {
			t.Fatalf("frame %d req_id = %q", i, frame.Headers.ReqID)
		}
		if !frame.Body.Stream.Finish {
			t.Fatalf("frame %d finish = false, want true", i)
		}
		if len(frame.Body.Stream.Content) > testStreamingTurnContentLimit {
			t.Fatalf("frame %d content bytes = %d, limit = %d", i, len(frame.Body.Stream.Content), testStreamingTurnContentLimit)
		}
		rebuilt.WriteString(frame.Body.Stream.Content)
	}
	if got[0].Body.Stream.ID == got[1].Body.Stream.ID {
		t.Fatalf("overflow reused stream ID %q", got[0].Body.Stream.ID)
	}
	if rebuilt.String() != content {
		t.Fatalf("overflow content was not preserved: got %d bytes, want %d", rebuilt.Len(), len(content))
	}
}

func TestStreamingTurn_RejectsMissingCallbackRequestID(t *testing.T) {
	p := &WSPlatform{}
	for _, replyCtx := range []any{"wrong", wsReplyContext{}} {
		if _, err := p.CreateStreamingTurn(context.Background(), replyCtx); err == nil {
			t.Fatalf("CreateStreamingTurn(%#v) error = nil", replyCtx)
		}
	}
}

func TestStreamingTurn_WriteFailureIsPermanent(t *testing.T) {
	p := &WSPlatform{}
	turn, err := p.CreateStreamingTurn(context.Background(), wsReplyContext{reqID: "callback-disconnected"})
	if err != nil {
		t.Fatalf("create streaming turn: %v", err)
	}
	if err := turn.Update(context.Background(), "planning"); err == nil {
		t.Fatal("update error = nil, want disconnected websocket error")
	}
	if !turn.Failed() {
		t.Fatal("Failed() = false after write failure")
	}
}

func TestStreamingTurn_AckErrorIsPermanent(t *testing.T) {
	p, _ := newStreamingTurnCaptureWithAck(t, 1, 40001)
	turn, err := p.CreateStreamingTurn(context.Background(), wsReplyContext{reqID: "callback-ack-error"})
	if err != nil {
		t.Fatalf("create streaming turn: %v", err)
	}
	if err := turn.Update(context.Background(), "planning"); err == nil {
		t.Fatal("update error = nil, want server ack error")
	}
	if !turn.Failed() {
		t.Fatal("Failed() = false after server ack error")
	}
}

func TestStreamingTurn_FinalCorrectionClosesAndRestartsOverflow(t *testing.T) {
	p, frames := newStreamingTurnCapture(t, 5)
	turn, err := p.CreateStreamingTurn(context.Background(), wsReplyContext{reqID: "callback-correction"})
	if err != nil {
		t.Fatalf("create streaming turn: %v", err)
	}

	draft := strings.Repeat("草", 8000)
	if err := turn.Update(context.Background(), draft); err != nil {
		t.Fatalf("update overflow draft: %v", err)
	}
	final := strings.Repeat("终", 8000)
	if err := turn.Finalize(context.Background(), final); err != nil {
		t.Fatalf("finalize corrected overflow: %v", err)
	}

	got := receiveStreamingTurnFrames(t, frames, 5)
	if !got[0].Body.Stream.Finish || got[1].Body.Stream.Finish {
		t.Fatalf("draft finish flags = %v, %v; want true, false", got[0].Body.Stream.Finish, got[1].Body.Stream.Finish)
	}
	if got[2].Body.Stream.ID != got[1].Body.Stream.ID || !got[2].Body.Stream.Finish {
		t.Fatalf("open draft stream was not finalized: second=%+v third=%+v", got[1].Body.Stream, got[2].Body.Stream)
	}
	if got[3].Body.Stream.ID == got[2].Body.Stream.ID || got[4].Body.Stream.ID == got[3].Body.Stream.ID {
		t.Fatalf("corrected overflow did not use fresh continuation IDs: %q, %q, %q", got[2].Body.Stream.ID, got[3].Body.Stream.ID, got[4].Body.Stream.ID)
	}
	if rebuilt := got[3].Body.Stream.Content + got[4].Body.Stream.Content; rebuilt != final {
		t.Fatalf("corrected overflow content was not preserved: got %d bytes, want %d", len(rebuilt), len(final))
	}
}

func TestStreamingTurn_UpdateAckFailureClosesPreviousStream(t *testing.T) {
	p, frames := newStreamingTurnCaptureWithAcks(t, []int{0, 40001, 0})
	turn, err := p.CreateStreamingTurn(context.Background(), wsReplyContext{reqID: "callback-update-error"})
	if err != nil {
		t.Fatalf("create streaming turn: %v", err)
	}
	if err := turn.Update(context.Background(), "planning"); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if err := turn.Update(context.Background(), "planning more"); err == nil {
		t.Fatal("second update error = nil, want server ack error")
	}

	got := receiveStreamingTurnFrames(t, frames, 3)
	if got[2].Body.Stream.ID != got[0].Body.Stream.ID || !got[2].Body.Stream.Finish {
		t.Fatalf("previous stream was not closed after update failure: first=%+v close=%+v", got[0].Body.Stream, got[2].Body.Stream)
	}
	if got[2].Body.Stream.Content != "planning" {
		t.Fatalf("close content = %q, want last acknowledged content", got[2].Body.Stream.Content)
	}
}

func TestStreamingTurn_FinalizeAtCommittedBoundaryClearsOpenStream(t *testing.T) {
	p, frames := newStreamingTurnCapture(t, 3)
	turn, err := p.CreateStreamingTurn(context.Background(), wsReplyContext{reqID: "callback-shrink"})
	if err != nil {
		t.Fatalf("create streaming turn: %v", err)
	}
	if err := turn.Update(context.Background(), strings.Repeat("x", 25000)); err != nil {
		t.Fatalf("overflow update: %v", err)
	}
	if err := turn.Finalize(context.Background(), strings.Repeat("x", testStreamingTurnContentLimit)); err != nil {
		t.Fatalf("finalize at committed boundary: %v", err)
	}

	got := receiveStreamingTurnFrames(t, frames, 3)
	if got[2].Body.Stream.ID != got[1].Body.Stream.ID || !got[2].Body.Stream.Finish || got[2].Body.Stream.Content != "" {
		t.Fatalf("stale open stream was not cleared: open=%+v close=%+v", got[1].Body.Stream, got[2].Body.Stream)
	}
}
