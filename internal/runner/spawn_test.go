package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpawn_DetachedCompletesOnItsOwn(t *testing.T) {
	r := New()
	logPath := filepath.Join(t.TempDir(), "out.log")
	p, err := r.Spawn(context.Background(), Spec{
		Argv:    []string{"/bin/sh", "-c", "echo done; exit 0"},
		LogPath: logPath,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if p.ID() == "" {
		t.Fatal("empty RunID")
	}
	if p.PID() == 0 {
		t.Fatal("PID = 0; process didn't start")
	}

	// Wait for the reaper.
	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("process didn't exit within 2s")
	}

	if !p.Exited() {
		t.Error("Exited() = false after Done() closed")
	}
	if p.ExitCode() != 0 {
		t.Errorf("ExitCode = %d; want 0", p.ExitCode())
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "done") {
		t.Errorf("log content: %q", data)
	}
}

func TestSpawn_RegisteredInList(t *testing.T) {
	r := New()
	logPath := filepath.Join(t.TempDir(), "log")
	p, err := r.Spawn(context.Background(), Spec{
		Argv:    []string{"/bin/sh", "-c", "true"},
		LogPath: logPath,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	got, ok := r.Get(p.ID())
	if !ok {
		t.Fatal("Get returned ok=false for fresh process")
	}
	if got.ID() != p.ID() {
		t.Errorf("Get returned different process")
	}
	all := r.List()
	if len(all) != 1 || all[0].ID() != p.ID() {
		t.Errorf("List did not include the spawned process: %v", all)
	}
	<-p.Done()
}

func TestSpawn_BottleIDTracked(t *testing.T) {
	r := New()
	logPath := filepath.Join(t.TempDir(), "log")
	const bottleID = "test-bottle-id"
	p, err := r.Spawn(context.Background(), Spec{
		Argv:     []string{"/bin/sh", "-c", "true"},
		LogPath:  logPath,
		BottleID: bottleID,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if p.BottleID() != bottleID {
		t.Errorf("BottleID = %q; want %q", p.BottleID(), bottleID)
	}
	<-p.Done()
}

func TestSpawn_KillTerminatesRunning(t *testing.T) {
	r := New()
	logPath := filepath.Join(t.TempDir(), "log")
	// Sleep long enough that we definitely need to kill it.
	p, err := r.Spawn(context.Background(), Spec{
		Argv:    []string{"/bin/sh", "-c", "sleep 30"},
		LogPath: logPath,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Give the OS a moment to set up the process group.
	time.Sleep(50 * time.Millisecond)

	if err := p.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if !p.Exited() {
		t.Error("Exited() = false after Kill returned")
	}
}

func TestSpawn_LogsReadIncrementally(t *testing.T) {
	r := New()
	logPath := filepath.Join(t.TempDir(), "log")
	p, err := r.Spawn(context.Background(), Spec{
		Argv: []string{"/bin/sh", "-c", `
echo line1
sleep 0.2
echo line2
sleep 0.2
echo line3
`},
		LogPath: logPath,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Poll until we see line1, capture the offset, then read from
	// there once the process finishes.
	var offset int64
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, next, _, err := p.Logs(offset, 0)
		if err != nil {
			t.Fatalf("Logs: %v", err)
		}
		if strings.Contains(string(data), "line1") {
			offset = next
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if offset == 0 {
		t.Fatal("never saw line1 in log")
	}

	// Wait for completion.
	<-p.Done()

	// Read remaining lines from the captured offset.
	data, _, exited, err := p.Logs(offset, 0)
	if err != nil {
		t.Fatalf("Logs (post-exit): %v", err)
	}
	if !exited {
		t.Error("exited=false after Done() closed")
	}
	rest := string(data)
	if !strings.Contains(rest, "line2") || !strings.Contains(rest, "line3") {
		t.Errorf("incremental read missed content: %q", rest)
	}
	if strings.Contains(rest, "line1") {
		t.Errorf("incremental read re-included line1: %q", rest)
	}
}

func TestSpawn_LogPathRequired(t *testing.T) {
	r := New()
	_, err := r.Spawn(context.Background(), Spec{
		Argv: []string{"/bin/sh", "-c", "true"},
	})
	if err == nil {
		t.Error("expected error when LogPath is empty")
	}
}

func TestSpawn_LogsAvailableAfterExit(t *testing.T) {
	r := New()
	logPath := filepath.Join(t.TempDir(), "log")
	p, err := r.Spawn(context.Background(), Spec{
		Argv:    []string{"/bin/sh", "-c", "echo posthumous-message"},
		LogPath: logPath,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	<-p.Done()

	// 100ms after the reaper closed Done, the log file should still
	// be readable.
	time.Sleep(100 * time.Millisecond)
	data, _, exited, err := p.Logs(0, 0)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if !exited {
		t.Error("exited=false")
	}
	if !strings.Contains(string(data), "posthumous-message") {
		t.Errorf("log content: %q", data)
	}
}

func TestForget(t *testing.T) {
	r := New()
	logPath := filepath.Join(t.TempDir(), "log")
	p, err := r.Spawn(context.Background(), Spec{
		Argv:    []string{"/bin/sh", "-c", "true"},
		LogPath: logPath,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	<-p.Done()

	r.Forget(p.ID())
	if _, ok := r.Get(p.ID()); ok {
		t.Error("Get returned ok=true after Forget")
	}
	if len(r.List()) != 0 {
		t.Error("List still includes forgotten process")
	}
}
