package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// We test against /bin/sh because it's universally available on macOS
// and Linux, behaves identically across the host machines we care
// about, and lets us script exit codes + stdout/stderr deterministically.

func TestRun_CapturesStdoutAndStderr(t *testing.T) {
	r := New()
	res, err := r.Run(context.Background(), Spec{
		Argv: []string{"/bin/sh", "-c", "echo out; echo err >&2"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := string(res.Output)
	if !strings.Contains(out, "out") || !strings.Contains(out, "err") {
		t.Errorf("missing stdout/stderr: %q", out)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d; want 0", res.ExitCode)
	}
}

func TestRun_NonZeroExitReturnsError(t *testing.T) {
	r := New()
	res, err := r.Run(context.Background(), Spec{
		Argv: []string{"/bin/sh", "-c", "echo failing; exit 7"},
	})
	if err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}
	if res == nil {
		t.Fatal("expected Result alongside error, got nil")
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d; want 7", res.ExitCode)
	}
	if !strings.Contains(string(res.Output), "failing") {
		t.Errorf("output missing pre-exit content: %q", res.Output)
	}
}

func TestRun_EmptyArgvRejected(t *testing.T) {
	r := New()
	_, err := r.Run(context.Background(), Spec{Argv: nil})
	if err == nil {
		t.Error("expected error for empty argv")
	}
}

func TestRun_LogPathTeesOutput(t *testing.T) {
	r := New()
	logPath := filepath.Join(t.TempDir(), "out.log")
	res, err := r.Run(context.Background(), Spec{
		Argv:    []string{"/bin/sh", "-c", "echo hello-to-file"},
		LogPath: logPath,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(string(res.Output), "hello-to-file") {
		t.Errorf("in-memory output missing content: %q", res.Output)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "hello-to-file") {
		t.Errorf("on-disk log missing content: %q", data)
	}
}

func TestRun_EnvOverridesPassThrough(t *testing.T) {
	r := New()
	res, err := r.Run(context.Background(), Spec{
		Argv: []string{"/bin/sh", "-c", "echo $MEAD_TEST_VAR"},
		Env:  map[string]string{"MEAD_TEST_VAR": "hello"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(string(res.Output), "hello") {
		t.Errorf("env var didn't reach the process: %q", res.Output)
	}
}

func TestCappedBuffer_TruncatesWithoutError(t *testing.T) {
	// Direct unit test of the cap behavior. We don't drive this
	// through a subprocess because writing >1 MiB of binary into a
	// pipe whose reader has stopped reading is platform-dependent
	// (the OS may send SIGPIPE to the writer regardless of how
	// faithfully we lie about consuming the bytes). The contract
	// the cap enforces is purely in-process: never grow past Cap,
	// never return an error.
	cb := &cappedBuffer{Cap: 100}
	for i := 0; i < 20; i++ {
		n, err := cb.Write(make([]byte, 50))
		if err != nil {
			t.Fatalf("Write #%d: %v", i, err)
		}
		if n != 50 {
			t.Errorf("Write #%d: n=%d want 50", i, n)
		}
	}
	if got := len(cb.Bytes()); got != 100 {
		t.Errorf("buffer grew to %d; expected cap %d", got, 100)
	}
}

func TestComposeEnv_OverridesWin(t *testing.T) {
	base := []string{"PATH=/usr/bin", "FOO=base"}
	overrides := map[string]string{"FOO": "override", "BAR": "new"}
	got := composeEnv(base, overrides)

	want := map[string]string{}
	for _, kv := range got {
		eq := strings.IndexByte(kv, '=')
		want[kv[:eq]] = kv[eq+1:]
	}
	if want["PATH"] != "/usr/bin" {
		t.Errorf("PATH lost: %v", want)
	}
	if want["FOO"] != "override" {
		t.Errorf("FOO=%q; want override", want["FOO"])
	}
	if want["BAR"] != "new" {
		t.Errorf("BAR=%q; want new", want["BAR"])
	}
}
