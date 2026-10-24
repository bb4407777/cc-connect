package config

import (
	"os"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestHooksInjectParses pins the config surface for prompt.build injectors:
// the key must survive a plain unmarshal, and a config without it must leave
// Inject nil so ordinary hooks keep their async semantics.
func TestHooksInjectParses(t *testing.T) {
	data := []byte(`
[[hooks]]
event = "prompt.build"
type = "command"
command = "bash /tmp/brief.sh"
timeout = 4
inject = true

[[hooks]]
event = "message.sent"
type = "command"
command = "echo hi"
`)
	dir := t.TempDir()
	path := dir + "/config.toml"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cfg Config
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	if len(cfg.Hooks) != 2 {
		t.Fatalf("hooks = %d, want 2", len(cfg.Hooks))
	}
	if cfg.Hooks[0].Inject == nil || !*cfg.Hooks[0].Inject {
		t.Fatalf("hooks[0].Inject = %v, want true", cfg.Hooks[0].Inject)
	}
	if cfg.Hooks[1].Inject != nil {
		t.Fatalf("hooks[1].Inject = %v, want nil", cfg.Hooks[1].Inject)
	}
}
