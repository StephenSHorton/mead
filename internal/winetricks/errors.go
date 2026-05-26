package winetricks

import "errors"

// ErrNotFound is returned by Locator.Path when no winetricks binary
// can be located. The MCP layer maps this to an actionable hint:
// "run `brew install winetricks`".
var ErrNotFound = errors.New("winetricks binary not found")
