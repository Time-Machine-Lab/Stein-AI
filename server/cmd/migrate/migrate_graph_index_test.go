package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Graph runs, submissions and trigger deliveries depend on these indexes for
// uniqueness as well as performance. Retrying an interrupted concurrent build
// must not mark an invalid index as successfully migrated.
func TestEveryGraphConcurrentBuildHasCleanup(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*_graph_*.up.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("find Graph migrations: paths=%d err=%v", len(paths), err)
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		version := strings.TrimSuffix(filepath.Base(path), ".up.sql")
		for _, match := range concurrentIndexNamePattern.FindAllSubmatch(stripSQLLineComments(body), -1) {
			if got := concurrentIndexCleanups[version]; got != string(match[1]) {
				t.Errorf("%s needs invalid-index cleanup for %s, got %q", version, match[1], got)
			}
		}
	}
}
