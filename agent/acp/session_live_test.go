package acp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// Live checks against the real WorkBuddy ACP bridge. Skipped unless
// CC_ACP_LIVE=1 because they spawn a real agent process.
//
//	CC_ACP_LIVE=1 go test ./agent/acp/ -run TestLive -v -timeout 300s
func liveConfig(t *testing.T, resumeSessionID string, mode string) acpSessionConfig {
	t.Helper()
	return acpSessionConfig{
		command:         "/Users/gao/.npm-global/bin/node",
		args:            []string{"/Users/gao/skills/cc-connect/scripts/workbuddy-acp-clean.mjs"},
		workDir:         "/Users/gao",
		resumeSessionID: resumeSessionID,
		initialMode:     mode,
		extraEnv: []string{
			"HOME=/Users/gao",
			"WORKBUDDY_MODEL=Deepseek-V4.1-Flash",
			"WORKBUDDY_MCP_CONFIG=/Users/gao/.cc-connect/wai-mcp.json",
			"WORKBUDDY_APPEND_SYSTEM_FILE=/Users/gao/Library/Mobile Documents/iCloud~md~obsidian/Documents/claude-obsidian/CLAUDE.md",
		},
	}
}

func requireLive(t *testing.T) {
	t.Helper()
	if os.Getenv("CC_ACP_LIVE") != "1" {
		t.Skip("set CC_ACP_LIVE=1 to run against the real ACP bridge")
	}
}

// Resuming a long conversation makes the backend replay hundreds of
// session/update notifications before it answers session/load. Before
// the fix this wedged the read loop and newACPSession never returned,
// which is why every inbound WeChat message went unanswered.
func TestLive_ResumeLargeSession(t *testing.T) {
	requireLive(t)
	sid := os.Getenv("CC_ACP_LIVE_SESSION")
	if sid == "" {
		sid = "b34c755a-1921-490b-9fc2-2864932e9620"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	t0 := time.Now()
	type result struct {
		s   *acpSession
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := newACPSession(ctx, liveConfig(t, sid, "bypassPermissions"))
		ch <- result{s, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("newACPSession(resume %s) failed after %s: %v", sid, time.Since(t0), r.err)
		}
		defer r.s.Close()
		t.Logf("handshake completed in %s", time.Since(t0))
		if got := r.s.currentACPSessionID(); got != sid {
			t.Errorf("resumed session id = %q, want %q (load reply carries no sessionId)", got, sid)
		}
	case <-time.After(120 * time.Second):
		t.Fatalf("newACPSession(resume %s) never returned — handshake wedged on replay", sid)
	}
}

// Fresh session + prompt: exercises the full turn loop the WeChat channel
// depends on. Uses session/new so no existing conversation is touched.
func TestLive_FreshSessionPrompt(t *testing.T) {
	requireLive(t)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	t0 := time.Now()
	s, err := newACPSession(ctx, liveConfig(t, "", "bypassPermissions"))
	if err != nil {
		t.Fatalf("newACPSession(new) failed after %s: %v", time.Since(t0), err)
	}
	defer s.Close()
	t.Logf("fresh session %s in %s", s.currentACPSessionID(), time.Since(t0))

	var sb strings.Builder
	gotResult := make(chan struct{})
	go func() {
		defer close(gotResult)
		for ev := range s.Events() {
			switch ev.Type {
			case core.EventText:
				sb.WriteString(ev.Content)
			case core.EventError:
				t.Logf("event error: %v", ev.Error)
			case core.EventResult:
				return
			}
		}
	}()

	if err := s.Send("只回复两个字：收到", "live-test-1", nil, nil); err != nil {
		t.Fatalf("Send failed after %s: %v", time.Since(t0), err)
	}

	select {
	case <-gotResult:
		t.Logf("turn finished in %s; text=%q", time.Since(t0), sb.String())
		if strings.TrimSpace(sb.String()) == "" {
			t.Error("no text streamed back")
		}
	case <-time.After(120 * time.Second):
		t.Fatal("prompt never produced a result event")
	}
}
