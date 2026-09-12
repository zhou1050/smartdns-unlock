package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func NewState() State {
	return State{Version: 2, Routes: map[string]string{}, RouteModes: map[string]string{}, LastChecks: map[string]ProbeResult{}, PrimaryHealthy: true, BackupHealthy: true}
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
	if s.RouteModes == nil {
		s.RouteModes = map[string]string{}
	}
	// Version 1 did not store a mode. Preserve automatic behaviour by default,
	// but retain choices that could only have come from an explicit CLI command:
	// "off", or "backup" on a platform that has no automatic probe.
	if s.Version < 2 {
		for id, route := range s.Routes {
			p, ok := PlatformByID(id)
			if route == "off" || (ok && p.Probe == "" && route == "backup") {
				s.RouteModes[id] = "manual"
			}
		}
		s.Version = 2
	}
	if s.LastChecks == nil {
		s.LastChecks = map[string]ProbeResult{}
	}
	return s, nil
}

func (s State) RouteMode(id string) string {
	if s.RouteModes[id] == "manual" { return "manual" }
	return "auto"
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
