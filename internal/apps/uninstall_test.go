package apps

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/StephenSHorton/mead/internal/bottles"
)

func TestUninstall_RunsWineUninstallerRemove(t *testing.T) {
	m, _, bottleID := newTestStack(t)
	proc, err := m.Uninstall(context.Background(), bottleID, "Battle.net")
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	<-proc.Done()
	data, err := os.ReadFile(proc.LogPath())
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "ran uninstaller --remove Battle.net") {
		t.Errorf("argv wrong: %q", data)
	}
}

func TestUninstall_KeyRequired(t *testing.T) {
	m, _, bottleID := newTestStack(t)
	if _, err := m.Uninstall(context.Background(), bottleID, "   "); !errors.Is(err, ErrUninstallKeyRequired) {
		t.Fatalf("err = %v, want ErrUninstallKeyRequired", err)
	}
}

func TestUninstall_UnknownBottle(t *testing.T) {
	m, _, _ := newTestStack(t)
	_, err := m.Uninstall(context.Background(), "00000000-0000-0000-0000-000000000000", "Battle.net")
	if !errors.Is(err, bottles.ErrBottleNotFound) {
		t.Fatalf("err = %v, want bottles.ErrBottleNotFound", err)
	}
}
