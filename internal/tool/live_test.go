package tool

import (
	"os"
	"testing"
	"time"
)

// TestLiveBackends requires all four CLIs and a local conversation in each
// store. It is opt-in and prints counts, never transcript content.
func TestLiveBackends(t *testing.T) {
	if os.Getenv("CL_LIVE_BACKENDS") != "1" {
		t.Skip("set CL_LIVE_BACKENDS=1 on a machine with all four agent stores")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	detected := Detect()
	for _, kind := range order {
		t.Run(kind, func(t *testing.T) {
			if Binary(detected, kind) == "" {
				t.Fatal("agent not on PATH")
			}
			start := time.Now()
			sessions, err := ListStore(kind, StoreDir(kind, home))
			if err != nil {
				t.Fatal(err)
			}
			if len(sessions) == 0 {
				t.Fatal("no resumable conversations in local store")
			}
			var resumable int
			for _, session := range sessions {
				if session.ID != "" && session.Title != "" && !session.Missing {
					resumable++
				}
			}
			if resumable == 0 {
				t.Fatal("no named session with usable working directory")
			}
			t.Logf("%d listed, %d usable, %s", len(sessions), resumable, time.Since(start))
		})
	}
}
