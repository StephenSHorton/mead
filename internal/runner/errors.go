package runner

import "errors"

var errNotImplemented = errors.New("not implemented yet")

// ErrNotImplemented is the exported alias of the package's stub sentinel.
var ErrNotImplemented = errNotImplemented
