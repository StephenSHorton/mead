package runner

import "errors"

// ErrDetachUnsupported is returned by Run when Spec.Detach=true. v0.1
// only supports the synchronous (wait + capture) case; detached
// launching (apps.launch + process.logs streaming) lands in v0.2.
var ErrDetachUnsupported = errors.New("runner: detached spec not yet supported")
