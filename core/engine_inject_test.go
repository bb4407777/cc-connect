package core

import (
	"strings"
	"testing"
)

// TestBuildSenderPrompt_InjectsBrief: the prompt.build hook output lands ahead
// of the user's text (after the sender header), so the model reads the recalled
// context first and the actual request last.
func TestBuildSenderPrompt_InjectsBrief(t *testing.T) {
	e := newTestEngine()
	e.SetInjectSender(true)
	inject := true
	e.SetHooks(NewHookManager("test", []HookConfig{{
		Event:   string(HookEventPromptBuild),
		Type:    "command",
		Command: `printf 'BRIEF'`,
		Inject:  &inject,
	}}, "sh", "-c", ""))

	got := e.buildSenderPrompt("问题", "user1", "Alice", "weixin", "weixin:ch:user1", "", 0)
	want := "[cc-connect sender_id=user1 sender_name=\"Alice\" platform=weixin chat_id=ch]\nBRIEF\n\n问题"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBuildSenderPrompt_NoInjectHookKeepsPromptStable guards existing behaviour
// for installs without injectors.
func TestBuildSenderPrompt_NoInjectHookKeepsPromptStable(t *testing.T) {
	e := newTestEngine()
	e.SetInjectSender(true)

	got := e.buildSenderPrompt("hello", "user1", "Alice", "feishu", "feishu:ch:user1", "", 0)
	if strings.Contains(got, "BRIEF") || !strings.HasSuffix(got, "hello") {
		t.Fatalf("prompt unexpectedly changed: %q", got)
	}
}
