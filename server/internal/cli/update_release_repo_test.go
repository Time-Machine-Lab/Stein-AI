package cli

import "testing"

func TestReleaseAPIURLUsesConfiguredRepo(t *testing.T) {
	t.Setenv("MULTICA_RELEASE_REPO", "")
	if got, want := releaseAPIURL("releases/latest"), "https://api.github.com/repos/"+DefaultReleaseRepo+"/releases/latest"; got != want {
		t.Fatalf("default: got %q, want %q", got, want)
	}

	t.Setenv("MULTICA_RELEASE_REPO", " Time-Machine-Lab/Stein-AI ")
	if got, want := releaseAPIURL("releases/tags/v0.1.2"), "https://api.github.com/repos/Time-Machine-Lab/Stein-AI/releases/tags/v0.1.2"; got != want {
		t.Fatalf("override: got %q, want %q", got, want)
	}
}
