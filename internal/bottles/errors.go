package bottles

import "errors"

var errNotImplemented = errors.New("not implemented yet")

// ErrNotImplemented is the exported alias of the package's stub sentinel.
var ErrNotImplemented = errNotImplemented

// ErrBottleNotFound is returned by Get/Delete when the supplied ID isn't
// registered. Distinct from a real I/O error so the MCP handler can map
// it to a 404-flavoured response.
var ErrBottleNotFound = errors.New("bottle not found")

// ErrBottleNameConflict is returned by Create when the supplied display
// name is already in use. (IDs are server-generated so they never
// collide; names are user-supplied so they can.)
var ErrBottleNameConflict = errors.New("bottle name already in use")
