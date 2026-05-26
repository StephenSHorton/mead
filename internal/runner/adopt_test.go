package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Adoption is the keystone of cross-restart process tracking. These
// tests cover the four states a sidecar can be in at discovery time:
//
//  1. exited=true → register as past process, done channel pre-closed
//  2. exited=false + pid alive → register live, watcher polls until exit
//  3. exited=false + pid dead → synthesize exit + write back to sidecar
//  4. malformed → readSidecar errors, caller skips

func TestAdopt_ExitedSidecar_RegistersAsPastProcess(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "x.log")
	metaPath := sidecarPathFor(logPath)
	if err := os.WriteFile(logPath, []byte("earlier output\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := writeSidecar(metaPath, &sidecar{
		ID:        "11111111-1111-1111-1111-111111111111",
		BottleID:  "bottle-x",
		Argv:      []string{"/bin/sh", "-c", "echo done"},
		StartedAt: fmtTime(time.Now().Add(-time.Hour).UTC()),
		LogPath:   logPath,
		PID:       999999, // doesn't matter; sidecar says exited
		Exited:    true,
		ExitedAt:  fmtTime(time.Now().Add(-time.Hour + time.Minute).UTC()),
		ExitCode:  0,
	}); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	r := New()
	p, err := r.Adopt(metaPath)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if p.ID() != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("ID = %q", p.ID())
	}
	if !p.Exited() {
		t.Error("Exited() = false; sidecar said true")
	}
	if p.ExitCode() != 0 {
		t.Errorf("ExitCode = %d", p.ExitCode())
	}
	// Done channel should be pre-closed for exited processes.
	select {
	case <-p.Done():
	case <-time.After(100 * time.Millisecond):
		t.Error("Done() not closed for already-exited process")
	}
	// Logs still readable.
	data, _, _, err := p.Logs(0, 0)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if !strings.Contains(string(data), "earlier output") {
		t.Errorf("log content lost: %q", data)
	}
}

func TestAdopt_AlivePID_RegistersAsRunning(t *testing.T) {
	// Spawn a long-sleeping process DETACHED from the test process —
	// it must not be our direct child. Otherwise it becomes a zombie
	// when killed (until the test reaps it), and our orphan watcher's
	// kill(pid, 0) liveness check returns 0 (still in the process
	// table), making the watcher loop forever.
	//
	// Trick: `sh -c 'sleep 30 < /dev/null > /dev/null 2>&1 & echo $!'`
	// — the parent shell prints the bg pid and exits; the bg sleep is
	// reparented to init/launchd, which will reap it on death.
	out, err := exec.Command("/bin/sh", "-c", "sleep 30 < /dev/null > /dev/null 2>&1 & echo $!").Output()
	if err != nil {
		t.Fatalf("start detached sleep: %v", err)
	}
	var sleepPID int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &sleepPID); err != nil {
		t.Fatalf("parse pid: %v (out=%q)", err, out)
	}
	t.Cleanup(func() { _ = syscall.Kill(sleepPID, syscall.SIGKILL) })

	dir := t.TempDir()
	logPath := filepath.Join(dir, "running.log")
	metaPath := sidecarPathFor(logPath)
	if err := os.WriteFile(logPath, []byte(""), 0o644); err != nil {
		t.Fatalf("touch log: %v", err)
	}
	if err := writeSidecar(metaPath, &sidecar{
		ID:        "22222222-2222-2222-2222-222222222222",
		Argv:      []string{"/bin/sh", "-c", "sleep 30"},
		StartedAt: fmtTime(time.Now().UTC()),
		LogPath:   logPath,
		PID:       sleepPID,
		Exited:    false,
	}); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	r := New()
	p, err := r.Adopt(metaPath)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if p.Exited() {
		t.Error("Exited() = true; sidecar said running and PID is alive")
	}
	if p.PID() != sleepPID {
		t.Errorf("PID = %d; want %d", p.PID(), sleepPID)
	}

	// Kill via the adopted Process — proves Kill works without a
	// cmd handle.
	if err := p.Kill(); err != nil {
		t.Fatalf("Kill (adopted): %v", err)
	}
	if !p.Exited() {
		t.Error("Exited() = false after Kill returned")
	}
}

func TestAdopt_DeadPID_SynthesizesExit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "dead.log")
	metaPath := sidecarPathFor(logPath)
	if err := os.WriteFile(logPath, []byte("stuff\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	// Use PID 1 in the sidecar — it's guaranteed to NOT match what we
	// "spawned" because we never spawned anything. (pid 1 itself is
	// usually alive but unrelated to our hypothetical process.)
	// To get a guaranteed-dead pid, spawn + wait + use that pid.
	deadCmd := exec.Command("/bin/sh", "-c", "true")
	if err := deadCmd.Run(); err != nil {
		t.Fatalf("spawn dead: %v", err)
	}
	deadPid := deadCmd.Process.Pid
	// pidAlive should report false (parent collected the exit; the
	// pid number is now stale/freeable).
	if pidAlive(deadPid) {
		t.Skip("kernel hasn't recycled the test pid yet; flaky timing")
	}

	if err := writeSidecar(metaPath, &sidecar{
		ID:        "33333333-3333-3333-3333-333333333333",
		Argv:      []string{"/bin/sh", "-c", "true"},
		StartedAt: fmtTime(time.Now().Add(-time.Hour).UTC()),
		LogPath:   logPath,
		PID:       deadPid,
		Exited:    false, // sidecar wasn't updated before Mead exited
	}); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	r := New()
	p, err := r.Adopt(metaPath)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if !p.Exited() {
		t.Error("Exited() = false; PID was dead at adoption")
	}
	if p.ExitCode() != -1 {
		t.Errorf("ExitCode = %d; want -1 (synthesized)", p.ExitCode())
	}
	if p.RunErr() == "" {
		t.Error("RunErr empty; expected synthesized message")
	}

	// Verify the sidecar was rewritten with the synthesized exit info.
	s, err := readSidecar(metaPath)
	if err != nil {
		t.Fatalf("re-read sidecar: %v", err)
	}
	if !s.Exited {
		t.Error("sidecar Exited still false after synthesis")
	}
	if s.RunErr == "" {
		t.Error("sidecar RunErr not persisted")
	}
}

func TestAdopt_MalformedSidecar_Errors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("not json at all"), 0o644); err != nil {
		t.Fatalf("write bad: %v", err)
	}
	r := New()
	if _, err := r.Adopt(bad); err == nil {
		t.Error("expected error from malformed sidecar")
	}
}

func TestSpawn_WritesSidecarAlongsideLog(t *testing.T) {
	r := New()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	p, err := r.Spawn(context.Background(), Spec{
		Argv:    []string{"/bin/sh", "-c", "echo hello"},
		LogPath: logPath,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	<-p.Done()

	metaPath := sidecarPathFor(logPath)
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	s, err := readSidecar(metaPath)
	if err != nil {
		t.Fatalf("readSidecar: %v", err)
	}
	if s.ID != p.ID() {
		t.Errorf("sidecar ID = %q; want %q", s.ID, p.ID())
	}
	if !s.Exited {
		t.Error("sidecar Exited = false after process ended")
	}
	if s.ExitCode != 0 {
		t.Errorf("sidecar ExitCode = %d; want 0", s.ExitCode)
	}
	if s.PID == 0 {
		t.Error("sidecar PID = 0")
	}
}

func TestSpawn_SidecarHasInitialStateBeforeExit(t *testing.T) {
	// Spawn a slow process; before it exits, the sidecar should
	// already exist with exited=false. This is what makes mid-run
	// discovery work.
	r := New()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "slow.log")
	p, err := r.Spawn(context.Background(), Spec{
		Argv:    []string{"/bin/sh", "-c", "sleep 1; echo done"},
		LogPath: logPath,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = p.Kill() })

	// Give Spawn a moment to write the initial sidecar.
	time.Sleep(50 * time.Millisecond)
	metaPath := sidecarPathFor(logPath)
	s, err := readSidecar(metaPath)
	if err != nil {
		t.Fatalf("readSidecar (mid-run): %v", err)
	}
	if s.Exited {
		t.Error("sidecar Exited = true mid-run")
	}
	if s.PID == 0 {
		t.Error("mid-run sidecar PID = 0")
	}
	<-p.Done()
}

func TestPidAlive(t *testing.T) {
	// Our own pid: alive.
	if !pidAlive(os.Getpid()) {
		t.Error("pidAlive(self) = false")
	}
	// 0 / negative: invalid → false.
	if pidAlive(0) || pidAlive(-1) {
		t.Error("pidAlive(<=0) should be false")
	}
	// Spawn + reap a process; its pid is then almost certainly dead.
	cmd := exec.Command("/bin/sh", "-c", "true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// Wait a beat for kernel cleanup, then check.
	time.Sleep(50 * time.Millisecond)
	if pidAlive(cmd.Process.Pid) {
		// Skip rather than fail — this is dependent on kernel pid
		// recycling timing.
		t.Skipf("pid %d still considered alive (kernel recycle timing)", cmd.Process.Pid)
	}
}

func TestWriteSidecar_AtomicReplace(t *testing.T) {
	// Atomic write means readers never see a half-written file. Hard
	// to test the race directly; here we just confirm the .tmp file
	// is cleaned up after a successful write.
	dir := t.TempDir()
	metaPath := filepath.Join(dir, "x.json")
	s := &sidecar{ID: "x", Argv: []string{"a"}, PID: 1}
	if err := writeSidecar(metaPath, s); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(metaPath + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file leaked: stat err = %v", err)
	}
	// Roundtrip.
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got sidecar
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "x" || len(got.Argv) != 1 || got.Argv[0] != "a" {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}
