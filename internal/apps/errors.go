package apps

import "errors"

// ErrInstallerPathRequired is returned by Install when the supplied
// installer path is empty or whitespace-only.
var ErrInstallerPathRequired = errors.New("installer path is required")

// ErrExePathRequired is returned by Launch when the supplied
// executable path is empty or whitespace-only.
var ErrExePathRequired = errors.New("executable path is required")

// ErrWinetricksVerbRequired is returned by RunWinetricks when the
// supplied verb is empty.
var ErrWinetricksVerbRequired = errors.New("winetricks verb is required")

// ErrWinetricksLocatorNotWired is returned by RunWinetricks when the
// Manager was constructed without a winetricks.Locator (only useful
// in tests where we don't always need the dep).
var ErrWinetricksLocatorNotWired = errors.New("winetricks locator not wired")

// ErrUninstallKeyRequired is returned by Uninstall when the supplied
// uninstaller key (from `wine uninstaller --list`) is empty.
var ErrUninstallKeyRequired = errors.New("uninstaller key is required (see `wine uninstaller --list`)")
