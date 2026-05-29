package meadcore

import (
	"encoding/json"
	"testing"
)

// These pin the JSON wire contract for the methods added alongside the
// clone/uninstall/registry work. A rename or retag would silently break
// the MCP surface the agent calls, so the field names are tested
// explicitly rather than left to reflection.

func TestBottlesCloneParams_Decode(t *testing.T) {
	var p bottlesCloneParams
	if err := json.Unmarshal([]byte(`{"source_id":"abc","name":"Clone of X"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.SourceID != "abc" || p.Name != "Clone of X" {
		t.Errorf("decoded wrong: %+v", p)
	}
}

func TestAppsUninstallParams_Decode(t *testing.T) {
	var p appsUninstallParams
	if err := json.Unmarshal([]byte(`{"bottle_id":"b1","key":"Battle.net"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.BottleID != "b1" || p.Key != "Battle.net" {
		t.Errorf("decoded wrong: %+v", p)
	}
}

func TestRegistryGetParams_Decode(t *testing.T) {
	var p registryGetParams
	if err := json.Unmarshal([]byte(`{"bottle_id":"b1","key":"HKCU\\Software\\Wine","value":"csmt"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.BottleID != "b1" || p.Key != `HKCU\Software\Wine` || p.Value != "csmt" {
		t.Errorf("decoded wrong: %+v", p)
	}
}

func TestRegistrySetParams_Decode(t *testing.T) {
	const raw = `{"bottle_id":"b1","key":"HKCU\\Software\\Wine\\Direct3D","value":"csmt","type":"REG_DWORD","data":"1"}`
	var p registrySetParams
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Key != `HKCU\Software\Wine\Direct3D` || p.Value != "csmt" || p.Type != "REG_DWORD" || p.Data != "1" {
		t.Errorf("decoded wrong: %+v", p)
	}
}
