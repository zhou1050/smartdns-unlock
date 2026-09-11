package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func NewState() State {
	return State{Version: 1, Routes: map[string]string{}, LastChecks: map[string]ProbeResult{}, PrimaryHealthy: true, BackupHealthy: true}
}

func LoadState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewState(), nil
		}
		return State{}, err
	}
	s := NewState()
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, err
	}
	if s.Routes == nil {
		s.Routes = map[string]string{}
	}
	if s.LastChecks == nil {
		s.LastChecks = map[string]ProbeResult{}
	}
	return s, nil
}

func SaveState(path string, s State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func SaveRules(path string, r RulesFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadRules(path string) (RulesFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RulesFile{}, err
	}
	var r RulesFile
	err = json.Unmarshal(b, &r)
	return r, err
}

func PlatformByID(id string) (Platform, bool) {
	for _, p := range Platforms {
		if p.ID == id {
			return p, true
		}
	}
	return Platform{}, false
}
