package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// emit must never wedge the transport read loop before a consumer exists.
// Regression: a resumed conversation replays hundreds of session/update
// notifications while newACPSession is still in handshake, and the engine
// does not start draining s.events until newACPSession returns.
func TestEmit_DoesNotBlockBeforeConsumerReady(t *testing.T) {
	s, _, _ := newTestSession(t, nil)
	s.consumerReady.Store(false)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5000; i++ {
			s.emit(core.Event{Type: core.EventText, Content: fmt.Sprintf("replay %d", i)})
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("emit blocked with no consumer draining events")
	}
}

// Once the consumer is live, streaming noise is shed rather than allowed
// to stall the reader; lifecycle events keep their blocking guarantee.
func TestEmit_ShedsNoiseWhenChannelFull(t *testing.T) {
	s, _, _ := newTestSession(t, nil)
	s.consumerReady.Store(true)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5000; i++ {
			s.emit(core.Event{Type: core.EventText, Content: fmt.Sprintf("chunk %d", i)})
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("emit blocked on a full channel for a non-critical event")
	}
}

// End-to-end shape of the production incident: the backend answers
// session/load only after replaying more notifications than the events
// channel can hold, and the reply does not echo sessionId back.
func TestHandshake_SurvivesLargeReplayDuringLoad(t *testing.T) {
	s, wResp, rReq := newTestSession(t, nil)
	s.consumerReady.Store(false) // pre-handshake: engine has no consumer yet
	s.acpSessID = ""

	const replayCount = 400 // comfortably over the events channel capacity

	go func() {
		sc := bufio.NewScanner(rReq)
		enc := json.NewEncoder(wResp)

		if !sc.Scan() { // initialize
			return
		}
		_ = enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"protocolVersion":   1,
				"agentCapabilities": map[string]any{"loadSession": true},
			},
		})

		if !sc.Scan() { // session/load
			return
		}
		for i := 0; i < replayCount; i++ {
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"method":  "session/update",
				"params": map[string]any{
					"sessionId": "resumed-session",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": fmt.Sprintf("line %d", i)},
					},
				},
			})
		}
		// codebuddy-shaped reply: models/modes/configOptions, no sessionId.
		_ = enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"result": map[string]any{
				"models": map[string]any{},
				"modes":  map[string]any{},
			},
		})
	}()

	done := make(chan error, 1)
	go func() { done <- s.handshake("resumed-session", "") }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("handshake failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handshake wedged: read loop blocked on a full events channel during replay")
	}

	if got := s.currentACPSessionID(); got != "resumed-session" {
		t.Fatalf("session id = %q, want %q: a session/load reply without sessionId must keep the resumed id",
			got, "resumed-session")
	}
}
