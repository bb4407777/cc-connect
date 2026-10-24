package core

import (
	"strings"
	"testing"
)

// TestInjections_ReturnsStdout covers the prompt.build injector contract:
// stdout is spliced into the prompt, empty output means "nothing to add".
func TestInjections_ReturnsStdout(t *testing.T) {
	inject := true
	hm := NewHookManager("test", []HookConfig{{
		Event:   string(HookEventPromptBuild),
		Type:    "command",
		Command: `printf 'recalled: %s' "$CC_HOOK_CONTENT"`,
		Inject:  &inject,
	}}, "sh", "-c", "")

	got := hm.Injections(HookEvent{
		Event:   HookEventPromptBuild,
		Content: "26民0621",
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 injection, got %d: %v", len(got), got)
	}
	if want := "recalled: 26民0621"; got[0] != want {
		t.Fatalf("injection = %q, want %q", got[0], want)
	}
}

// TestInjections_FailureIsSkipped: a broken injector must never block the turn.
func TestInjections_FailureIsSkipped(t *testing.T) {
	inject := true
	hm := NewHookManager("test", []HookConfig{
		{Event: string(HookEventPromptBuild), Type: "command", Command: "exit 3", Inject: &inject},
		{Event: string(HookEventPromptBuild), Type: "command", Command: "echo ok", Inject: &inject},
	}, "sh", "-c", "")

	got := hm.Injections(HookEvent{Event: HookEventPromptBuild, Content: "x"})
	if len(got) != 1 || strings.TrimSpace(got[0]) != "ok" {
		t.Fatalf("expected only the working injector's output, got %v", got)
	}
}

// TestInjections_IgnoresNonInjectHooks: plain async hooks stay on the Emit path.
func TestInjections_IgnoresNonInjectHooks(t *testing.T) {
	hm := NewHookManager("test", []HookConfig{{
		Event:   string(HookEventPromptBuild),
		Type:    "command",
		Command: "echo should-not-inject",
	}}, "sh", "-c", "")

	if got := hm.Injections(HookEvent{Event: HookEventPromptBuild}); len(got) != 0 {
		t.Fatalf("non-inject hook leaked into prompt: %v", got)
	}
}

// TestValidateHookConfig_InjectRequiresCommand rejects an injector that has no
// stdout to capture (http hooks).
func TestValidateHookConfig_InjectRequiresCommand(t *testing.T) {
	inject := true
	err := validateHookConfig(HookConfig{
		Event:  string(HookEventPromptBuild),
		Type:   "http",
		URL:    "http://127.0.0.1:1/hook",
		Inject: &inject,
	})
	if err == nil {
		t.Fatal("expected validation error for inject=true on an http hook")
	}
}
