package runner

import "errors"

// ErrProcessNotFound is returned by callers (typically through the
// MCP layer) when a RunID isn't in the registry. Defined here so the
// handler layer can wrap it consistently.
var ErrProcessNotFound = errors.New("process not found")
