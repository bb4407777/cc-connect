package core_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/agent/acp"
	"github.com/chenhg5/cc-connect/core"
)

// capturePlatform records replies so the test can inspect what the user would see.
type capturePlatform struct {
	mu      sync.Mutex
	handler core.MessageHandler
	replies []string
}

func (p *capturePlatform) Name() string { return "test" }
func (p *capturePlatform) Start(h core.MessageHandler) error {
	p.handler = h
	return nil
}
func (p *capturePlatform) Reply(_ context.Context, _ any, content string) error {
	p.add(content)
	return nil
}
func (p *capturePlatform) Send(_ context.Context, _ any, content string) error {
	p.add(content)
	return nil
}
func (p *capturePlatform) Stop() error { return nil }

func (p *capturePlatform) add(s string) {
	p.mu.Lock()
	p.replies = append(p.replies, s)
	p.mu.Unlock()
}

func (p *capturePlatform) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.replies...)
}

// TestLive_EngineSteerBusyMessage drives the whole engine path against the real
// CodeBuddy CLI: a long turn is started, then a second message arrives mid-turn
// and must be injected into that turn (one merged reply) instead of queued.
// Skipped unless ACP_STEER_LIVE=1.
func TestLive_EngineSteerBusyMessage(t *testing.T) {
	if os.Getenv("ACP_STEER_LIVE") != "1" {
		t.Skip("set ACP_STEER_LIVE=1 to run against the real CodeBuddy CLI")
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = "/Users/gao"
	}

	agent, err := acp.New(map[string]any{
		"command":  "/Users/gao/.npm-global/bin/node",
		"args":     []string{home + "/skills/cc-connect/scripts/workbuddy-acp-clean.mjs"},
		"work_dir": t.TempDir(),
		"mode":     "bypassPermissions",
		"env": map[string]string{
			"HOME":            home,
			"WORKBUDDY_MODEL": "deepseek-v4.1-flash",
			"CODEBUDDY_CLI":   "/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy",
			"NODE_OPTIONS":    "--max-old-space-size=2048",
		},
	})
	if err != nil {
		t.Fatalf("acp.New: %v", err)
	}

	p := &capturePlatform{}
	e := core.NewEngine("steer-live", agent, []core.Platform{p}, t.TempDir(), core.LangEnglish)
	e.SetBusyInputMode("steer")
	if err := e.Start(); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	defer func() { _ = e.Stop() }()

	msg := func(id, text string) *core.Message {
		return &core.Message{
			SessionKey: "test:dm:probe",
			Platform:   "test",
			UserID:     "probe",
			UserName:   "probe",
			Content:    text,
			ReplyCtx:   "ctx",
			MessageID:  id,
		}
	}

	p.handler(p, msg("m1", "请在 bash 里执行 sleep 8，然后只回复：甲句完成"))

	time.Sleep(3 * time.Second)

	start := time.Now()
	p.handler(p, msg("m2", "再补一句：乙句收到"))
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("busy handler blocked for %s（注入不能占住入站）", took)
	} else {
		t.Logf("busy handler returned in %s", took)
	}

	deadline := time.Now().Add(90 * time.Second)
	for {
		replies := p.snapshot()
		joined := strings.Join(replies, "\n---\n")
		if strings.Contains(joined, "甲句完成") && strings.Contains(joined, "乙句收到") {
			t.Logf("replies (%d): %q", len(replies), joined)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("replies = %q, want merged 甲句完成 + 乙句收到", joined)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
