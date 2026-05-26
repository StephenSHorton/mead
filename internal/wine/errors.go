package wine

import "errors"

var errNotImplemented = errors.New("not implemented yet")

// ErrNotImplemented is the exported alias of the package's stub sentinel.
var ErrNotImplemented = errNotImplemented

// ErrWineNotFound is returned by Locator.Path when no usable Wine binary
// can be located. The UI maps this to a setup-required state.
var ErrWineNotFound = errors.New("wine binary not found")
