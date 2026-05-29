package meadcore

import (
	"encoding/json"
	"testing"
)

// The apps.launch wire contract must mirror apps.install: both accept an
// "args" string array forwarded to the spawned process. These tests pin
// the JSON field name + decoding so a rename/retag can't silently break
// the MCP contract the agent relies on to pass Chromium/CEF flags.

func TestAppsLaunchParams_DecodesArgs(t *testing.T) {
	raw := `{"bottle_id":"b1","exe_path":"windows/notepad.exe","args":["--single-process","--in-process-gpu"]}`
	var p appsLaunchParams
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.BottleID != "b1" || p.ExePath != "windows/notepad.exe" {
		t.Errorf("bottle/exe decoded wrong: %+v", p)
	}
	if len(p.Args) != 2 || p.Args[0] != "--single-process" || p.Args[1] != "--in-process-gpu" {
		t.Errorf("args decoded wrong: %#v", p.Args)
	}
}

func TestAppsLaunchParams_OmittedArgsIsNil(t *testing.T) {
	// A launch with no args must decode to a nil/empty slice so the
	// variadic spread degrades to the prior no-extra-args behavior.
	var p appsLaunchParams
	if err := json.Unmarshal([]byte(`{"bottle_id":"b1","exe_path":"x.exe"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(p.Args) != 0 {
		t.Errorf("expected no args, got %#v", p.Args)
	}
}

func TestAppsLaunchParams_ArgsFieldMatchesInstall(t *testing.T) {
	// Same JSON "args" key feeds both surfaces — keep them in lockstep.
	const raw = `{"bottle_id":"b","exe_path":"x","installer_path":"y","args":["--flag"]}`
	var l appsLaunchParams
	var i appsInstallParams
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		t.Fatalf("launch unmarshal: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &i); err != nil {
		t.Fatalf("install unmarshal: %v", err)
	}
	if len(l.Args) != 1 || len(i.Args) != 1 || l.Args[0] != i.Args[0] {
		t.Errorf("launch/install args disagree: launch=%#v install=%#v", l.Args, i.Args)
	}
}
