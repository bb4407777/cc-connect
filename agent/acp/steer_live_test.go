package acp

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// TestLive_SteerMergesIntoRunningTurn exercises the real CodeBuddy ACP backend.
// Requires the local WorkBuddy CLI + wrapper; skipped unless ACP_STEER_LIVE=1
// because it spends model tokens.
//
// Contract being verified:
//   - Steer returns quickly while a turn is running (no sendMu wait);
//   - the steer acknowledgment does NOT produce a second EventResult;
//   - the injected text is folded into the running turn's single reply.
func TestLive_SteerMergesIntoRunningTurn(t *testing.T) {
	if os.Getenv("ACP_STEER_LIVE") != "1" {
		t.Skip("set ACP_STEER_LIVE=1 to run against the real CodeBuddy CLI")
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = "/Users/gao"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	s, err := newACPSession(ctx, acpSessionConfig{
		command:     "/Users/gao/.npm-global/bin/node",
		args:        []string{home + "/skills/cc-connect/scripts/workbuddy-acp-clean.mjs"},
		workDir:     t.TempDir(),
		initialMode: "bypassPermissions",
		extraEnv: []string{
			"HOME=" + home,
			"WORKBUDDY_MODEL=deepseek-v4.1-flash",
			"CODEBUDDY_CLI=/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy",
			"NODE_OPTIONS=--max-old-space-size=2048",
		},
	})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	defer func() { _ = s.Close() }()

	var (
		mu     sync.Mutex
		text   strings.Builder
		result int
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range s.Events() {
			switch ev.Type {
			case core.EventText:
				mu.Lock()
				text.WriteString(ev.Content)
				mu.Unlock()
			case core.EventResult:
				if ev.Done {
					mu.Lock()
					result++
					mu.Unlock()
				}
			}
		}
	}()

	sendErr := make(chan error, 1)
	go func() {
		sendErr <- s.Send("请在 bash 里执行 sleep 8，然后只回复：甲句完成", "m1", nil, nil)
	}()

	time.Sleep(3 * time.Second)

	steerStart := time.Now()
	steerErr := s.Steer("再补一句：乙句收到", "m2", nil, nil)
	steerTook := time.Since(steerStart)
	if steerErr != nil {
		t.Fatalf("Steer: %v", steerErr)
	}
	if steerTook > 5*time.Second {
		t.Fatalf("Steer took %s, want immediate return（不能等整轮）", steerTook)
	}
	t.Logf("Steer returned in %s", steerTook)

	select {
	case err := <-sendErr:
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("Send did not return")
	}

	// Give the last chunks a moment to land in the channel.
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	got := text.String()
	results := result
	mu.Unlock()

	if results != 1 {
		t.Fatalf("EventResult count = %d, want 1（注入确认不能收尾）", results)
	}
	if !strings.Contains(got, "甲句完成") || !strings.Contains(got, "乙句收到") {
		t.Fatalf("reply = %q, want both 甲句完成 and 乙句收到 merged", got)
	}
	t.Logf("merged reply: %q", got)
}
