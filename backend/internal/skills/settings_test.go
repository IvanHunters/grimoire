package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsStore_SetAndGet(t *testing.T) {
	dir := t.TempDir()
	store := NewSettingsStore(filepath.Join(dir, "settings.json"))

	if state, _ := store.GetState("missing"); state != OverrideOn {
		t.Errorf("default state should be on, got %q", state)
	}

	if err := store.SetState("deploy", OverrideOff); err != nil {
		t.Fatalf("SetState off: %v", err)
	}
	if state, _ := store.GetState("deploy"); state != OverrideOff {
		t.Errorf("after off, got %q", state)
	}

	if err := store.SetState("deploy", OverrideOn); err != nil {
		t.Fatalf("SetState on: %v", err)
	}
	if state, _ := store.GetState("deploy"); state != OverrideOn {
		t.Errorf("after re-enable, got %q", state)
	}
}

func TestSettingsStore_PreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	initial := map[string]any{
		"theme":          "dark",
		"otherSetting":   42.0,
		"skillOverrides": map[string]any{"existing": "name-only"},
	}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewSettingsStore(path)
	if err := store.SetState("new-skill", OverrideOff); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var got map[string]any
	json.Unmarshal(raw, &got)

	if got["theme"] != "dark" {
		t.Errorf("theme lost: %v", got["theme"])
	}
	if got["otherSetting"] != 42.0 {
		t.Errorf("otherSetting lost: %v", got["otherSetting"])
	}
	overrides := got["skillOverrides"].(map[string]any)
	if overrides["existing"] != "name-only" {
		t.Errorf("existing override lost: %v", overrides)
	}
	if overrides["new-skill"] != "off" {
		t.Errorf("new override not written: %v", overrides)
	}
}

func TestSettingsStore_GetAll(t *testing.T) {
	dir := t.TempDir()
	store := NewSettingsStore(filepath.Join(dir, "settings.json"))
	store.SetState("a", OverrideOff)
	store.SetState("b", OverrideNameOnly)
	all, err := store.GetAll()
	if err != nil {
		t.Fatal(err)
	}
	if all["a"] != OverrideOff || all["b"] != OverrideNameOnly {
		t.Errorf("GetAll mismatch: %v", all)
	}
}
