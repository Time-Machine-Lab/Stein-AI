package service

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
)

func graphPluginSkillFixture(t *testing.T) (pluginruntime.CompiledEntry, map[string]db.PluginArtifactFile) {
	t.Helper()
	entry := pluginruntime.CompiledEntry{
		PluginID: "10000000-0000-0000-0000-000000000001", InstallationID: "10000000-0000-0000-0000-000000000002",
		ReleaseID: "10000000-0000-0000-0000-000000000003", ContributionID: "10000000-0000-0000-0000-000000000004",
		ArtifactFileID: "10000000-0000-0000-0000-000000000005", ContributionKey: "review", ContributionType: plugincontract.ContributionAgentSkillV1,
		EntryPath: "skills/review/SKILL.md",
	}
	file := pluginSkillArtifact(entry.ArtifactFileID, entry.ReleaseID, entry.EntryPath, "# Immutable review")
	entry.EntryDigest = file.Digest
	files := map[string]db.PluginArtifactFile{entry.ArtifactFileID: file}
	skill, err := materializePinnedPluginSkill(entry, files)
	if err != nil {
		t.Fatal(err)
	}
	_, refs := BuildAgentSkillBundles([]AgentSkillData{skill})
	entry.SkillBundleHash, entry.SkillSizeBytes, entry.SkillFileCount = refs[0].Hash, refs[0].SizeBytes, refs[0].FileCount
	return entry, files
}

func TestGraphPluginSkillsUsePinnedMaterialization(t *testing.T) {
	entry, files := graphPluginSkillFixture(t)
	skills, pins, err := graphMaterializePluginSkills([]pluginruntime.CompiledEntry{entry}, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Content != "# Immutable review" || len(pins) != 1 || pins[0].ReleaseID != entry.ReleaseID || pins[0].SkillBundleHash != entry.SkillBundleHash {
		t.Fatalf("invalid frozen plugin result: skills=%#v pins=%#v", skills, pins)
	}
	file := files[entry.ArtifactFileID]
	file.Content = "changed content"
	files[entry.ArtifactFileID] = file
	if _, _, err = graphMaterializePluginSkills([]pluginruntime.CompiledEntry{entry}, files); err == nil {
		t.Fatal("accepted artifact content drift")
	}
}

func TestGraphPluginSkillsRejectHashDriftAndUnsupportedContributions(t *testing.T) {
	entry, files := graphPluginSkillFixture(t)
	entry.SkillBundleHash = "sha256:wrong"
	if _, _, err := graphMaterializePluginSkills([]pluginruntime.CompiledEntry{entry}, files); err == nil {
		t.Fatal("accepted bundle hash drift")
	}
	for _, kind := range []string{plugincontract.ContributionRemoteMCPV1, "unknown"} {
		entry.ContributionType = kind
		if _, _, err := graphMaterializePluginSkills([]pluginruntime.CompiledEntry{entry}, files); err == nil {
			t.Fatalf("silently accepted unsupported contribution %s", kind)
		}
	}
}
