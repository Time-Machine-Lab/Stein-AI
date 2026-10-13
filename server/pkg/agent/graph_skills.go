package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

type codexGraphSkill struct {
	Path    string `json:"path"`
	Enabled *bool  `json:"enabled"`
}

func codexGraphPath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// enforceCodexGraphSkills accounts for admin and bundled native skills that HOME
// isolation alone cannot remove. Unknown capabilities fail before a model turn.
func enforceCodexGraphSkills(ctx context.Context, request func(context.Context, string, any) (json.RawMessage, error), cwd string, paths []string) error {
	allowed := make(map[string]bool, len(paths))
	for _, path := range paths {
		allowed[codexGraphPath(path)] = true
	}
	list := func() ([]codexGraphSkill, error) {
		raw, err := request(ctx, "skills/list", map[string]any{"cwds": []string{cwd}, "forceReload": true})
		if err != nil {
			return nil, fmt.Errorf("Graph requires Codex skill inventory support: %w", err)
		}
		var inventory struct {
			Data []struct {
				Cwd    string            `json:"cwd"`
				Skills []codexGraphSkill `json:"skills"`
				Errors []json.RawMessage `json:"errors"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &inventory); err != nil || len(inventory.Data) != 1 || codexGraphPath(inventory.Data[0].Cwd) != codexGraphPath(cwd) || len(inventory.Data[0].Errors) != 0 || len(inventory.Data[0].Skills) > 256 {
			return nil, fmt.Errorf("Graph could not verify Codex skill inventory")
		}
		for _, skill := range inventory.Data[0].Skills {
			if skill.Path == "" || skill.Enabled == nil {
				return nil, fmt.Errorf("Graph requires explicit Codex skill paths and enablement")
			}
		}
		return inventory.Data[0].Skills, nil
	}
	skills, err := list()
	if err != nil {
		return err
	}
	for _, skill := range skills {
		if allowed[codexGraphPath(skill.Path)] || !*skill.Enabled {
			continue
		}
		if _, err := request(ctx, "skills/config/write", map[string]any{"path": skill.Path, "enabled": false}); err != nil {
			return fmt.Errorf("Graph could not disable ambient Codex skill: %w", err)
		}
	}
	skills, err = list()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, skill := range skills {
		if !*skill.Enabled {
			continue
		}
		path := codexGraphPath(skill.Path)
		if !allowed[path] {
			return fmt.Errorf("Graph Codex skill inventory contains an unfrozen skill")
		}
		seen[path] = true
	}
	if len(seen) != len(allowed) {
		return fmt.Errorf("Graph Codex skill inventory is missing a frozen skill")
	}
	return nil
}
