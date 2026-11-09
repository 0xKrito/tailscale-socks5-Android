package tsproxy

import (
	"context"
	"strings"
	"testing"
)

// Lifecycle state must be observable without starting real Tailscale sockets.
func TestStartupState(t *testing.T) {
	mu.Lock()
	oldRunning, oldStarting, oldError := running, starting, startupError
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		running, starting, startupError = oldRunning, oldStarting, oldError
		mu.Unlock()
	})
	for _, tt := range []struct {
		name          string
		run, start    bool
		failure, want string
	}{
		{"stopped", false, false, "", "STOPPED"},
		{"initializing", false, true, "", "STARTING"},
		{"running", true, false, "", "RUNNING"},
		{"failed", false, false, "tsnet error", "ERROR: tsnet error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mu.Lock()
			running, starting, startupError = tt.run, tt.start, tt.failure
			mu.Unlock()
			if got := GetStartupState(); got != tt.want {
				t.Fatalf("GetStartupState = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestRepeatedStartIsGuarded(t *testing.T) {
	mu.Lock()
	oldRunning, oldStarting := running, starting
	running, starting = false, true
	mu.Unlock()
	t.Cleanup(func() { mu.Lock(); running, starting = oldRunning, oldStarting; mu.Unlock() })
	if got := Start("127.0.0.1:1080", "host", "ignored"); got != "STARTING" {
		t.Fatalf("duplicate Start = %q; want STARTING", got)
	}
	mu.Lock()
	running, starting = true, false
	mu.Unlock()
	if got := Start("127.0.0.1:1080", "host", "ignored"); got != "RUNNING" {
		t.Fatalf("running Start = %q; want RUNNING", got)
	}
}

func TestStopCancelsPendingLogin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mu.Lock()
	oldStarting, oldCancel := starting, startCancel
	starting, startCancel = true, cancel
	mu.Unlock()
	t.Cleanup(func() { mu.Lock(); starting, startCancel = oldStarting, oldCancel; mu.Unlock() })
	Stop()
	if err := ctx.Err(); err != context.Canceled {
		t.Fatalf("cancel pending login: %v", err)
	}
	if !strings.Contains(GetLogs(), "Canceling pending Tailscale startup") {
		t.Fatal("missing cancellation log")
	}
}
