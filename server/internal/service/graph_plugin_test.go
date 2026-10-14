package service

import (
	"encoding/json"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
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

func TestGraphPluginRemoteMCPIsPinnedWithoutCredentialMaterial(t *testing.T) {
	entry, _ := graphPluginSkillFixture(t)
	entry.ContributionType = plugincontract.ContributionRemoteMCPV1
	entry.ConfigID = "10000000-0000-0000-0000-000000000006"
	entry.ConfigRevision = 2
	entry.Endpoint = "https://tools.example.com/mcp"
	entry.Transport = "streamable-http"
	entry.AuthType = "bearer"
	entry.SecretRef = "10000000-0000-0000-0000-000000000007"
	entry.FailurePolicy = "required"
	entry.ApprovedTools = []pluginruntime.RemoteMCPTool{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	entry.ToolSchemaDigest, _ = remotemcp.ToolSetDigest(entry.ApprovedTools)
	skills, pins, err := graphMaterializePluginSkills([]pluginruntime.CompiledEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 || len(pins) != 1 {
		t.Fatalf("unexpected snapshot: %v %v", skills, pins)
	}
	entries := GraphPluginEntries(pins)
	if len(entries) != 1 || entries[0].Endpoint != entry.Endpoint || entries[0].SecretRef != entry.SecretRef {
		t.Fatalf("lost pinned connection: %#v", entries)
	}
	entry.ToolSchemaDigest = "sha256:wrong"
	if _, _, err = graphMaterializePluginSkills([]pluginruntime.CompiledEntry{entry}, nil); err == nil {
		t.Fatal("accepted tool schema drift")
	}
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
