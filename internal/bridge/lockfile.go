package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Per-process lock layout: <lockDir>/<pid>.lock
//
// Multiple Mead instances can run side-by-side; each writes its own
// pid-named lock so external MCP servers can enumerate them. On startup
// we prune any lock files whose pid is no longer alive (force-kill /
// crash leaves them behind).
//
// The lock directory is configurable so this package stays domain-free —
// see Config and the New(cfg) constructor. Default lives at
// $HOME/.<AppName>/mcp/, overridable via <UPPER_APP_NAME>_MCP_LOCK_DIR.

// Config is what the bridge needs from its host application to write
// per-pid lockfiles. AppName seeds the default lockdir ($HOME/.<AppName>/mcp)
// and the env-var name (<UPPER_APP_NAME>_MCP_LOCK_DIR) so two apps using
// this bridge package can't accidentally write to each other's directory.
type Config struct {
	AppName string
}

func (b *Bridge) lockDir() string {
	envVar := strings.ToUpper(b.cfg.AppName) + "_MCP_LOCK_DIR"
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to cwd if we somehow can't find HOME — the bridge
		// should still work even if multi-instance enumeration breaks.
		home = "."
	}
	return filepath.Join(home, "."+b.cfg.AppName, "mcp")
}

func (b *Bridge) ownLockPath() string {
	return filepath.Join(b.lockDir(), strconv.Itoa(os.Getpid())+".lock")
}

type lockFile struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	Token     string `json:"token"`
	StartedAt string `json:"started_at"`
}

func (b *Bridge) writeLockfile(port int, token string) error {
	b.pruneStaleLocks()

	path := b.ownLockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create lockdir: %w", err)
	}

	doc := lockFile{
		PID:       os.Getpid(),
		Port:      port,
		Token:     token,
		StartedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}
	return nil
}

func (b *Bridge) deleteLockfile() {
	_ = os.Remove(b.ownLockPath())
}

func (b *Bridge) pruneStaleLocks() {
	dir := b.lockDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		// ENOENT is fine; nothing to prune.
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".lock")
		pid, err := strconv.Atoi(stem)
		if err != nil {
			continue
		}
		if !pidAlive(pid) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
