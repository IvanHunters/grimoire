package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SkillOverrideState mirrors the values understood by Claude Code's
// `skillOverrides` map. An absent entry behaves like "on".
type SkillOverrideState string

const (
	OverrideOn                SkillOverrideState = "on"
	OverrideNameOnly          SkillOverrideState = "name-only"
	OverrideUserInvocableOnly SkillOverrideState = "user-invocable-only"
	OverrideOff               SkillOverrideState = "off"
)

// SettingsStore reads and writes the `skillOverrides` field of
// ~/.claude/settings.json. Other fields in the file are preserved.
type SettingsStore struct {
	path string
	mu   sync.Mutex
}

// NewSettingsStore returns a SettingsStore pointing at the given file.
func NewSettingsStore(path string) *SettingsStore {
	return &SettingsStore{path: path}
}

// DefaultSettingsPath returns ~/.claude/settings.json.
func DefaultSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func (s *SettingsStore) read() (map[string]any, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("settings.json: %w", err)
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func (s *SettingsStore) write(root map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// GetState returns the override state for a skill. Missing entries return "on".
func (s *SettingsStore) GetState(name string) (SkillOverrideState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.read()
	if err != nil {
		return OverrideOn, err
	}
	overrides, _ := root["skillOverrides"].(map[string]any)
	if overrides == nil {
		return OverrideOn, nil
	}
	v, ok := overrides[name].(string)
	if !ok {
		return OverrideOn, nil
	}
	return SkillOverrideState(v), nil
}

// GetAll returns a copy of the entire skillOverrides map.
func (s *SettingsStore) GetAll() (map[string]SkillOverrideState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.read()
	if err != nil {
		return nil, err
	}
	overrides, _ := root["skillOverrides"].(map[string]any)
	out := map[string]SkillOverrideState{}
	for k, v := range overrides {
		if s, ok := v.(string); ok {
			out[k] = SkillOverrideState(s)
		}
	}
	return out, nil
}

// SetState writes name=state into skillOverrides. Passing OverrideOn removes
// the entry since the default state is already "on".
func (s *SettingsStore) SetState(name string, state SkillOverrideState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.read()
	if err != nil {
		return err
	}
	overrides, _ := root["skillOverrides"].(map[string]any)
	if overrides == nil {
		overrides = map[string]any{}
	}
	if state == OverrideOn {
		delete(overrides, name)
	} else {
		overrides[name] = string(state)
	}
	if len(overrides) == 0 {
		delete(root, "skillOverrides")
	} else {
		root["skillOverrides"] = overrides
	}
	return s.write(root)
}
