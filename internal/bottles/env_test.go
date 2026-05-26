package bottles

import (
	"context"
	"errors"
	"testing"
)

func TestSetEnv_PersistsAcrossReloads(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	b, err := m.Create(context.Background(), "env-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := m.SetEnv(b.ID, "DXVK_HUD", "fps"); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}

	// Re-read via the manager's persistence-backed path to confirm it
	// survives a process restart (no actual restart needed — each call
	// reads from disk).
	env, err := m.EnvOverrides(b.ID)
	if err != nil {
		t.Fatalf("EnvOverrides: %v", err)
	}
	if env["DXVK_HUD"] != "fps" {
		t.Errorf("DXVK_HUD = %q; want fps", env["DXVK_HUD"])
	}
}

func TestSetEnv_EmptyValueRemovesKey(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	b, err := m.Create(context.Background(), "env-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.SetEnv(b.ID, "WINEDEBUG", "warn+all"); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}
	if err := m.SetEnv(b.ID, "WINEDEBUG", ""); err != nil {
		t.Fatalf("SetEnv (delete): %v", err)
	}
	env, err := m.EnvOverrides(b.ID)
	if err != nil {
		t.Fatalf("EnvOverrides: %v", err)
	}
	if _, present := env["WINEDEBUG"]; present {
		t.Errorf("WINEDEBUG still present after empty-value SetEnv: %v", env)
	}
}

func TestSetEnv_EmptyKeyRejected(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	b, err := m.Create(context.Background(), "env-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.SetEnv(b.ID, "", "anything"); !errors.Is(err, ErrEnvKeyRequired) {
		t.Errorf("expected ErrEnvKeyRequired, got %v", err)
	}
}

func TestSetEnv_UnknownBottle(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	if err := m.SetEnv("00000000-0000-0000-0000-000000000000", "X", "1"); !errors.Is(err, ErrBottleNotFound) {
		t.Errorf("expected ErrBottleNotFound, got %v", err)
	}
}

func TestEnvOverrides_ReturnsCopy(t *testing.T) {
	// Mutating the returned map must NOT affect the bottle's persisted
	// state — otherwise a misbehaving caller could corrupt the bottle's
	// env without going through SetEnv.
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	b, err := m.Create(context.Background(), "env-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.SetEnv(b.ID, "KEY", "ORIG"); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}
	env, _ := m.EnvOverrides(b.ID)
	env["KEY"] = "MUTATED"
	again, _ := m.EnvOverrides(b.ID)
	if again["KEY"] != "ORIG" {
		t.Errorf("EnvOverrides returned a live reference: %q", again["KEY"])
	}
}
