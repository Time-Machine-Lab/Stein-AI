package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func TestGraphCodexSkillsDisableAmbientBeforeFirstTurn(t *testing.T) {
	cwd := t.TempDir()
	allowed := filepath.Join(t.TempDir(), "frozen", "SKILL.md")
	ambient := filepath.Join(t.TempDir(), "ambient", "SKILL.md")
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprint(broken), func(t *testing.T) {
			disabled, lists := false, 0
			request := func(_ context.Context, method string, params any) (json.RawMessage, error) {
				switch method {
				case "skills/list":
					lists++
					return json.Marshal(map[string]any{"data": []any{map[string]any{"cwd": cwd, "skills": []any{map[string]any{"path": allowed, "enabled": true}, map[string]any{"path": ambient, "enabled": !disabled || broken}}, "errors": []any{}}}})
				case "skills/config/write":
					p := params.(map[string]any)
					if p["path"] != ambient || p["enabled"] != false {
						t.Fatalf("unexpected disable: %v", p)
					}
					disabled = true
					return json.RawMessage(`{}`), nil
				default:
					return nil, fmt.Errorf("unexpected method %s", method)
				}
			}
			err := enforceCodexGraphSkills(context.Background(), request, cwd, []string{allowed})
			if (err != nil) != broken || !disabled || lists != 2 {
				t.Fatalf("err=%v disabled=%v lists=%d", err, disabled, lists)
			}
		})
	}
}

func TestGraphCodexSkillsFailClosedOnMissingOrUnobservableInventory(t *testing.T) {
	for _, reply := range []string{`{}`, `{"data":[]}`, `{"data":[{"cwd":"work","skills":[],"errors":[]}]}`, `{"data":[{"cwd":"work","skills":[],"errors":[{"message":"unreadable"}]}]}`} {
		request := func(context.Context, string, any) (json.RawMessage, error) { return json.RawMessage(reply), nil }
		if err := enforceCodexGraphSkills(context.Background(), request, "work", []string{filepath.Join(t.TempDir(), "SKILL.md")}); err == nil {
			t.Fatalf("accepted %s", reply)
		}
	}
}
