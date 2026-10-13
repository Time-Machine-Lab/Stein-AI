package agent

import "testing"

func TestGraphExecutionSuppressesProviderWholeRunRetry(t *testing.T) {
	for _, result := range []Result{{codexInitializeRetrySafe: true}, {codexStartupRefreshRetrySafe: true}} {
		if !codexWholeRunRetryAllowed(ExecOptions{}, result, 1) {
			t.Fatal("ordinary safe startup retry was lost")
		}
		if codexWholeRunRetryAllowed(ExecOptions{DisableWholeRunRetry: true}, result, 1) {
			t.Fatal("Graph attempt restarted inside provider adapter")
		}
		if codexWholeRunRetryAllowed(ExecOptions{}, result, 2) {
			t.Fatal("ordinary adapter exceeded its restart budget")
		}
	}
	if codexWholeRunRetryAllowed(ExecOptions{}, Result{}, 1) {
		t.Fatal("unproven retry was allowed")
	}
}
