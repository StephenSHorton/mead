package apps

import (
	"fmt"
	"sync/atomic"
	"time"
)

// logSeq is bumped per call to uniqueLogName so two installs fired in
// the same nanosecond can't collide on the log filename.
var logSeq atomic.Uint64

// uniqueLogName returns a filename suitable for a per-process log
// inside the bottle's logs/ dir. Format: <unix_ns>-<seq>.log. Sorted
// ordering matches creation order; seq disambiguates same-ns calls.
func uniqueLogName() string {
	return fmt.Sprintf("%d-%d.log", time.Now().UnixNano(), logSeq.Add(1))
}
