package daemon

import (
	"encoding/json"
	"testing"
)

func TestNormalizeGraphMcpConfigAlwaysHasServerRecord(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"absent":         {in: "", want: `{"mcpServers":{}}`},
		"null":           {in: "null", want: `{"mcpServers":{}}`},
		"empty object":   {in: `{}`, want: `{"mcpServers":{}}`},
		"extra keys":     {in: `{"other":1}`, want: `{"mcpServers":{},"other":1}`},
		"servers kept":   {in: `{"mcpServers":{"a":{"command":"x"}}}`, want: `{"mcpServers":{"a":{"command":"x"}}}`},
		"empty servers":  {in: `{"mcpServers":{}}`, want: `{"mcpServers":{}}`},
		"invalid passes": {in: `[1,2]`, want: `[1,2]`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(normalizeGraphMcpConfig(json.RawMessage(tc.in)))
			if got != tc.want {
				t.Fatalf("normalizeGraphMcpConfig(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}
