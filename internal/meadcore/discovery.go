package meadcore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
)

// adoptExistingProcesses scans every bottle's logs/ dir for sidecar
// JSON files (left behind by previous Mead sessions) and re-registers
// each one with the runner. Returns the first scan error encountered;
// individual sidecar parse failures are logged but don't abort the
// whole scan (one corrupt file shouldn't lose the rest).
//
// Walks `<store-root>/bottles/<id>/logs/*.json`. Skips the .tmp files
// writeSidecar leaves mid-write.
func adoptExistingProcesses(s *store.Store, r *runner.Runner) error {
	if s == nil || r == nil {
		return nil
	}
	bottlesDir := filepath.Join(s.Root(), "bottles")
	entries, err := os.ReadDir(bottlesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read bottles dir: %w", err)
	}

	var adopted, failed int
	for _, e := range entries {
		if !e.IsDir() || !store.UUIDIsh(e.Name()) {
			continue
		}
		logsDir := filepath.Join(bottlesDir, e.Name(), "logs")
		logs, err := os.ReadDir(logsDir)
		if err != nil {
			continue // bottle has no logs/ yet — fine.
		}
		for _, le := range logs {
			name := le.Name()
			if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".tmp") {
				continue
			}
			path := filepath.Join(logsDir, name)
			if _, err := r.Adopt(path); err != nil {
				failed++
				continue
			}
			adopted++
		}
	}
	if adopted > 0 || failed > 0 {
		fmt.Fprintf(os.Stderr, "meadcore: adopted %d process sidecar(s), %d failed\n", adopted, failed)
	}
	return nil
}
