package wine

import "errors"

// ErrWineNotFound is returned by Locator.Path when none of the
// resolution-chain candidates produced a usable Wine binary. The UI
// maps this to a setup-required state.
var ErrWineNotFound = errors.New("wine binary not found")
