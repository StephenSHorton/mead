package meadcore

import (
	"strings"
	"testing"
)

// setDLLOverride is pure string munging — the right place to be paranoid
// about edge cases, because Wine's WINEDLLOVERRIDES syntax is
// notoriously fiddly and incorrect strings just silently get ignored.

func TestSetDLLOverride_AddsToEmpty(t *testing.T) {
	got := setDLLOverride("", "d3dx9_43", "native,builtin")
	want := "d3dx9_43=native,builtin"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSetDLLOverride_AppendsWhenAbsent(t *testing.T) {
	got := setDLLOverride("dnsapi=builtin", "d3dx9_43", "native")
	if !strings.Contains(got, "dnsapi=builtin") {
		t.Errorf("lost existing entry: %q", got)
	}
	if !strings.Contains(got, "d3dx9_43=native") {
		t.Errorf("did not add new entry: %q", got)
	}
}

func TestSetDLLOverride_ReplacesExisting(t *testing.T) {
	got := setDLLOverride("d3dx9_43=builtin;dnsapi=builtin", "d3dx9_43", "native")
	if !strings.Contains(got, "d3dx9_43=native") {
		t.Errorf("did not replace: %q", got)
	}
	if strings.Contains(got, "d3dx9_43=builtin") {
		t.Errorf("kept old value: %q", got)
	}
	if !strings.Contains(got, "dnsapi=builtin") {
		t.Errorf("clobbered sibling: %q", got)
	}
}

func TestSetDLLOverride_EmptyModeRemoves(t *testing.T) {
	got := setDLLOverride("d3dx9_43=native;dnsapi=builtin", "d3dx9_43", "")
	if strings.Contains(got, "d3dx9_43") {
		t.Errorf("did not remove: %q", got)
	}
	if !strings.Contains(got, "dnsapi=builtin") {
		t.Errorf("clobbered sibling on remove: %q", got)
	}
}

func TestSetDLLOverride_DisabledMaps(t *testing.T) {
	// "disabled" → dll= (empty value), which tells Wine to disable
	// the DLL outright.
	got := setDLLOverride("", "msvcp140", "disabled")
	if got != "msvcp140=" {
		t.Errorf("got %q want %q", got, "msvcp140=")
	}
}

func TestSetDLLOverride_CaseInsensitiveDLLMatch(t *testing.T) {
	// Wine itself matches DLL names case-insensitively; our replace
	// logic must too, otherwise we double-set under different casings.
	got := setDLLOverride("d3dx9_43=native", "D3DX9_43", "builtin")
	if strings.Count(got, "=") != 1 {
		t.Errorf("expected one entry, got %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "=builtin") {
		t.Errorf("did not update under different casing: %q", got)
	}
}

func TestSetDLLOverride_PreservesMalformed(t *testing.T) {
	// If we see entries we can't parse, preserve them verbatim — better
	// to leave odd state alone than to silently drop something the user
	// (or a previous Mead version) set.
	got := setDLLOverride("weird-entry-no-equals;d3dx9_43=native", "d3dx9_43", "builtin")
	if !strings.Contains(got, "weird-entry-no-equals") {
		t.Errorf("dropped malformed entry: %q", got)
	}
}
