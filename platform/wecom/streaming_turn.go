package wecom

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/chenhg5/cc-connect/core"
)

const wecomStreamingTurnContentLimit = 20480

type streamingTurn struct {
	platform  *WSPlatform
	reqID     string
	streamID  string
	committed string
	active    string
	open      bool
	failed    atomic.Bool
}

func (p *WSPlatform) CreateStreamingTurn(_ context.Context, replyCtx any) (core.StreamingTurn, error) {
	rc, ok := replyCtx.(wsReplyContext)
	if !ok {
		return nil, fmt.Errorf("wecom-ws: invalid streaming turn context type %T", replyCtx)
	}
	if rc.reqID == "" {
		return nil, fmt.Errorf("wecom-ws: streaming turn requires callback req_id")
	}
	return &streamingTurn{
		platform: p,
		reqID:    rc.reqID,
		streamID: p.generateReqID("stream"),
	}, nil
}

func (t *streamingTurn) Update(ctx context.Context, content string) error {
	return t.write(ctx, content, false)
}

func (t *streamingTurn) Finalize(ctx context.Context, content string) error {
	return t.write(ctx, content, true)
}

func (t *streamingTurn) Failed() bool {
	return t.failed.Load()
}

// FormatStreamingTurnContent keeps intermediate agent activity inside WeCom's
// native collapsible thinking block while leaving the final answer visible.
func (p *WSPlatform) FormatStreamingTurnContent(progress []string, answer string) string {
	if len(progress) == 0 {
		return answer
	}
	content := "<think>\n" + strings.Join(progress, "\n\n---\n\n") + "\n</think>"
	if answer != "" {
		content += "\n\n" + answer
	}
	return content
}

func (t *streamingTurn) write(ctx context.Context, content string, final bool) error {
	if t.Failed() {
		return fmt.Errorf("wecom-ws: streaming turn already failed")
	}
	if err := ctx.Err(); err != nil {
		return t.fail(err)
	}
	if !strings.HasPrefix(content, t.committed) {
		if err := t.restart(ctx); err != nil {
			return t.fail(err)
		}
	}

	remaining := content[len(t.committed):]
	if remaining == "" {
		if !t.open {
			return nil
		}
		if err := t.send(ctx, "", final); err != nil {
			return t.failAndClose(ctx, err)
		}
		t.active = ""
		t.open = !final
		return nil
	}
	chunks := splitByBytes(remaining, wecomStreamingTurnContentLimit)
	for i, chunk := range chunks {
		finish := final || i < len(chunks)-1
		if err := t.send(ctx, chunk, finish); err != nil {
			return t.failAndClose(ctx, err)
		}
		t.open = !finish
		if finish {
			t.active = ""
		} else {
			t.active = chunk
		}
		if i < len(chunks)-1 {
			t.committed += chunk
			t.streamID = t.platform.generateReqID("stream")
		}
	}
	return nil
}

func (t *streamingTurn) restart(ctx context.Context) error {
	if t.open {
		if err := t.send(ctx, t.active, true); err != nil {
			return err
		}
	}
	t.committed = ""
	t.active = ""
	t.open = false
	t.streamID = t.platform.generateReqID("stream")
	return nil
}

func (t *streamingTurn) send(ctx context.Context, content string, finish bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	frame := map[string]any{
		"cmd":     "aibot_respond_msg",
		"headers": map[string]string{"req_id": t.reqID},
		"body": map[string]any{
			"msgtype": "stream",
			"stream": map[string]any{
				"id":      t.streamID,
				"finish":  finish,
				"content": content,
			},
		},
	}
	return t.platform.writeAndWaitAckStrict(ctx, frame, t.reqID, wsAckTimeout)
}

func (t *streamingTurn) fail(err error) error {
	t.failed.Store(true)
	return err
}

func (t *streamingTurn) failAndClose(ctx context.Context, cause error) error {
	if t.open && ctx.Err() == nil {
		if err := t.send(ctx, t.active, true); err != nil {
			cause = fmt.Errorf("%w; close active stream: %v", cause, err)
		} else {
			t.active = ""
			t.open = false
		}
	}
	return t.fail(cause)
}
