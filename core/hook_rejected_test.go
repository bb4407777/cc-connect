package core

import (
	"testing"
	"time"
)

// TestProcessInteractiveEvents_DropsStopHookRejectedDraft reproduces the
// Claude Code 2.1.272 stream captured in
// stream-json output (captured with a Stop hook that blocks once):
// assistant "AAA" -> user "Stop hook feedback: ..." -> assistant "BBB" -> result "BBB".
func TestProcessInteractiveEvents_DropsStopHookRejectedDraft(t *testing.T) {
	p := &stubPlatformEngine{n: "feishu"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	e.SetDisplayConfig(DisplayCfg{Mode: "quiet", ToolMessages: false})
	sessionKey := "feishu:hook-rejected"
	session := e.sessions.GetOrCreateActive(sessionKey)
	agentSession := newControllableSession("hook-rejected")
	state := &interactiveState{
		agentSession: agentSession,
		platform:     p,
		replyCtx:     "ctx-hook-rejected",
	}
	e.interactiveStates[sessionKey] = state

	agentSession.events <- Event{Type: EventText, Content: "AAA"}
	agentSession.events <- Event{Type: EventType("hook_rejected")}
	agentSession.events <- Event{Type: EventText, Content: "BBB"}
	agentSession.events <- Event{Type: EventResult, Content: "BBB", Done: true}

	e.processInteractiveEvents(state, session, e.sessions, sessionKey, "m-hook-rejected", time.Now(), nil, nil, state.replyCtx, 0)

	if got := p.getSent(); len(got) != 1 || got[0] != "BBB" {
		t.Fatalf("final reply = %#v, want only rewritten text %q", got, "BBB")
	}
	if got := session.GetHistory(0); len(got) != 1 || got[0].Content != "BBB" {
		t.Fatalf("history = %#v, want only rewritten text %q", got, "BBB")
	}
}

func TestProcessInteractiveEvents_NoHookRejectionKeepsTextAcrossToolBoundary(t *testing.T) {
	p := &stubPlatformEngine{n: "feishu"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	e.SetDisplayConfig(DisplayCfg{Mode: "quiet", ToolMessages: false})
	sessionKey := "feishu:no-hook-rejection"
	session := e.sessions.GetOrCreateActive(sessionKey)
	agentSession := newControllableSession("no-hook-rejection")
	state := &interactiveState{
		agentSession: agentSession,
		platform:     p,
		replyCtx:     "ctx-no-hook-rejection",
	}
	e.interactiveStates[sessionKey] = state

	agentSession.events <- Event{Type: EventText, Content: "AAA"}
	agentSession.events <- Event{Type: EventToolUse, ToolName: "Bash", ToolInput: "true"}
	agentSession.events <- Event{Type: EventToolResult, ToolName: "Bash", ToolResult: "ok"}
	agentSession.events <- Event{Type: EventText, Content: "BBB"}
	agentSession.events <- Event{Type: EventResult, Content: "BBB", Done: true}

	e.processInteractiveEvents(state, session, e.sessions, sessionKey, "m-no-hook-rejection", time.Now(), nil, nil, state.replyCtx, 0)

	// 2026-10-06 高律师反馈「bot 的 Q/A 之间没有分行」：跨隐藏工具边界的文本段
	// 拼接改为 joinTextParts——段间无换行时补空行，Q/A 这类首行不再被顶进上段句中。
	if got := p.getSent(); len(got) != 1 || got[0] != "AAA\n\nBBB" {
		t.Fatalf("final reply = %#v, want paragraph break across hidden tool boundary %q", got, "AAA\n\nBBB")
	}
}
