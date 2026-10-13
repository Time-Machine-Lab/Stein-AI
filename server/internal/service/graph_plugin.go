package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/multica-ai/multica/server/internal/graph"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// GraphPluginPin identifies an immutable Skill and its live authorization owner.
// Skill content is copied into AgentConfig; these references contain no secrets.
type GraphPluginPin struct {
	PluginID        string                       `json:"plugin_id"`
	InstallationID  string                       `json:"installation_id"`
	ReleaseID       string                       `json:"release_id"`
	ContributionID  string                       `json:"contribution_id"`
	SkillBundleHash string                       `json:"skill_bundle_hash"`
	Entry           *pluginruntime.CompiledEntry `json:"entry,omitempty"`
}

// GraphPluginEntries are the immutable, credential-free entries copied into
// the ordinary task plugin manifest by the Graph enqueue trigger.
func GraphPluginEntries(pins []GraphPluginPin) []pluginruntime.CompiledEntry {
	entries := make([]pluginruntime.CompiledEntry, 0, len(pins))
	for _, pin := range pins {
		if pin.Entry != nil {
			entries = append(entries, *pin.Entry)
		}
	}
	return entries
}

// SnapshotGraphSkills uses the same scope matching and contribution ordering as
// pin_plugin_execution_manifest. Publication copies every selected Skill; it
// never resolves a plugin's current content during claim or retry.
func (s *PluginService) SnapshotGraphSkills(ctx context.Context, tx pgx.Tx, workspaceID, agentID pgtype.UUID) ([]AgentSkillData, []GraphPluginPin, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT jsonb_agg(entry ORDER BY
 COALESCE((entry->>'ordinal')::integer,0),entry->>'plugin_key',entry->>'contribution_key')
 FROM jsonb_array_elements(snapshot.compiled_entries) entry
 WHERE (entry->>'scope_type'='agent' AND entry->>'scope_id'=$2::uuid::text)
 OR (entry->>'scope_type'='workspace' AND NOT COALESCE(entry->'excluded_agent_ids','[]'::jsonb) ? $2::uuid::text)), '[]'::jsonb)
 FROM plugin_workspace_capability_state state
 JOIN plugin_capability_snapshot snapshot ON snapshot.id=state.active_snapshot_id AND snapshot.workspace_id=state.workspace_id
 WHERE state.workspace_id=$1`, workspaceID, agentID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		var enabled bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plugin_installation WHERE workspace_id=$1 AND enabled AND uninstalled_at IS NULL)`, workspaceID).Scan(&enabled); err != nil {
			return nil, nil, err
		}
		if enabled {
			return nil, nil, graphError("snapshot_invalid", "enabled plugins have no compiled capability snapshot")
		}
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	entries, err := pluginruntime.ParseEntries(raw)
	if err != nil {
		return nil, nil, graphError("snapshot_invalid", "plugin capability snapshot is invalid")
	}
	ids := make([]pgtype.UUID, 0)
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.ContributionType == plugincontract.ContributionRemoteMCPV1 {
			continue
		}
		if entry.ContributionType != plugincontract.ContributionAgentSkillV1 {
			return nil, nil, graphError("snapshot_invalid", "unsupported plugin contribution")
		}
		fileIDs := []string{entry.ArtifactFileID}
		for _, file := range entry.SkillFiles {
			fileIDs = append(fileIDs, file.ArtifactFileID)
		}
		for _, id := range fileIDs {
			if seen[id] {
				continue
			}
			parsed, parseErr := util.ParseUUID(id)
			if parseErr != nil {
				return nil, nil, graphError("snapshot_invalid", "plugin artifact reference is invalid")
			}
			seen[id] = true
			ids = append(ids, parsed)
		}
	}
	filesByID := make(map[string]db.PluginArtifactFile, len(ids))
	if len(ids) > 0 {
		files, err := s.Queries.WithTx(tx).ListPluginArtifactFilesByIDs(ctx, ids)
		if err != nil {
			return nil, nil, err
		}
		for _, file := range files {
			filesByID[util.UUIDToString(file.ID)] = file
		}
	}
	skills, pins, err := graphMaterializePluginSkills(entries, filesByID)
	if err != nil {
		return nil, nil, graphError("snapshot_invalid", err.Error())
	}
	if err = s.ValidateGraphPluginPins(ctx, tx, workspaceID, agentID, pins); err != nil {
		return nil, nil, err
	}
	return skills, pins, nil
}

func graphMaterializePluginSkills(entries []pluginruntime.CompiledEntry, files map[string]db.PluginArtifactFile) ([]AgentSkillData, []GraphPluginPin, error) {
	skills := make([]AgentSkillData, 0, len(entries))
	pins := make([]GraphPluginPin, 0, len(entries))
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.ContributionType != plugincontract.ContributionAgentSkillV1 && entry.ContributionType != plugincontract.ContributionRemoteMCPV1 {
			return nil, nil, fmt.Errorf("unsupported plugin contribution type %q", entry.ContributionType)
		}
		if seen[entry.ContributionID] {
			return nil, nil, fmt.Errorf("plugin snapshot contains duplicate Skill contributions")
		}
		seen[entry.ContributionID] = true
		for _, id := range []string{entry.PluginID, entry.InstallationID, entry.ReleaseID, entry.ContributionID} {
			if _, err := util.ParseUUID(id); err != nil {
				return nil, nil, fmt.Errorf("plugin snapshot contains an invalid resource reference")
			}
		}
		pin := GraphPluginPin{PluginID: entry.PluginID, InstallationID: entry.InstallationID, ReleaseID: entry.ReleaseID, ContributionID: entry.ContributionID, SkillBundleHash: entry.SkillBundleHash, Entry: &entry}
		if entry.ContributionType == plugincontract.ContributionRemoteMCPV1 {
			digest, err := remotemcp.ToolSetDigest(entry.ApprovedTools)
			if err != nil || digest != entry.ToolSchemaDigest || len(entry.ApprovedTools) == 0 || entry.ConfigID == "" || entry.ConfigRevision < 1 || entry.Endpoint == "" || entry.Transport != "streamable-http" || (entry.FailurePolicy != "required" && entry.FailurePolicy != "optional") {
				return nil, nil, fmt.Errorf("invalid pinned Remote MCP configuration")
			}
			pins = append(pins, pin)
			continue
		}
		skill, err := materializePinnedPluginSkill(entry, files)
		if err != nil {
			return nil, nil, err
		}
		bundles, refs := BuildAgentSkillBundles([]AgentSkillData{skill})
		if refs[0].Hash != entry.SkillBundleHash || refs[0].SizeBytes != entry.SkillSizeBytes || refs[0].FileCount != entry.SkillFileCount {
			return nil, nil, fmt.Errorf("pinned plugin Skill failed bundle hash validation")
		}
		skills = append(skills, bundles[0])
		pins = append(pins, pin)
	}
	return skills, pins, nil
}

// ValidateGraphPluginPins rechecks authorization without following mutable
// active-release pointers. An upgrade preserves a published Graph's content;
// disable, uninstall, release revocation and grant/binding withdrawal stop new
// execution. Existing processes retain their already delivered Skill text.
func (s *PluginService) ValidateGraphPluginPins(ctx context.Context, tx pgx.Tx, workspaceID, agentID pgtype.UUID, pins []GraphPluginPin) error {
	for _, pin := range pins {
		kind, capability := plugincontract.ContributionAgentSkillV1, plugincontract.CapabilityAgentSkillContribute
		if pin.Entry != nil && pin.Entry.ContributionType == plugincontract.ContributionRemoteMCPV1 {
			kind, capability = plugincontract.ContributionRemoteMCPV1, plugincontract.CapabilityRemoteMCPConnect
		}
		for _, id := range []string{pin.PluginID, pin.InstallationID, pin.ReleaseID, pin.ContributionID} {
			if _, err := util.ParseUUID(id); err != nil {
				return graphError("authorization_revoked", "published plugin reference is invalid")
			}
		}
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM plugin_installation installation
 JOIN plugin_identity identity ON identity.id=installation.plugin_id AND identity.retired_at IS NULL
 JOIN plugin_release release ON release.id=$5 AND release.plugin_id=identity.id AND release.revocation_status='active'
 JOIN plugin_contribution contribution ON contribution.id=$6 AND contribution.release_id=release.id AND contribution.type=$7
 WHERE installation.workspace_id=$1 AND installation.id=$3 AND installation.plugin_id=$4
 AND installation.enabled AND installation.uninstalled_at IS NULL
 AND (SELECT decision FROM plugin_grant WHERE installation_id=installation.id AND capability=$8 ORDER BY grant_revision DESC LIMIT 1)='granted'
 AND COALESCE(
 (SELECT enabled FROM plugin_binding WHERE installation_id=installation.id AND scope_type='agent' AND scope_id=$2 ORDER BY binding_revision DESC LIMIT 1),
 (SELECT enabled FROM plugin_binding WHERE installation_id=installation.id AND scope_type='workspace' AND scope_id=$1 ORDER BY binding_revision DESC LIMIT 1),false)
)`, workspaceID, agentID, pin.InstallationID, pin.PluginID, pin.ReleaseID, pin.ContributionID, kind, capability).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return graphError("authorization_revoked", "a published plugin contribution is disabled, revoked or outside this agent's scope")
		}
	}
	return nil
}

// ValidateGraphTaskRemoteMCPAuthorization is called for every Graph broker
// request, including auth=none contributions. Revoked grants/bindings and a
// stopped or superseded attempt must not keep reaching upstream tools.
func (s *PluginService) ValidateGraphTaskRemoteMCPAuthorization(ctx context.Context, taskID pgtype.UUID, contributionID string) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	manifest, err := q.GetPluginExecutionManifestByTask(ctx, taskID)
	if err != nil {
		return err
	}
	if manifest.ComposerVersion != "graph-plugin-composer-v1" {
		return graphError("authorization_revoked", "Graph plugin manifest is unavailable")
	}
	attempt, err := q.GetGraphTaskManifest(ctx, db.GetGraphTaskManifestParams{TaskID: taskID, WorkspaceID: manifest.WorkspaceID})
	if err != nil {
		return err
	}
	if attempt.CurrentTaskID != taskID || attempt.StoppedAt.Valid || (attempt.RunState != string(graph.RunActive) && attempt.RunState != string(graph.RunWaitingApproval)) || (attempt.ControlState != string(graph.ControlRun) && attempt.ControlState != string(graph.ControlPause)) {
		return graphError("authorization_revoked", "Graph attempt is no longer allowed to call tools")
	}
	entries, err := pluginruntime.ParseEntries(manifest.OrderedContributions)
	if err != nil {
		return err
	}
	var execution struct {
		NodeSpec struct {
			AgentConfig struct {
				Entries []pluginruntime.CompiledEntry `json:"plugin_entries"`
			} `json:"agent_config"`
		} `json:"node_spec"`
	}
	if err = json.Unmarshal(attempt.Manifest, &execution); err != nil {
		return err
	}
	expected, _ := json.Marshal(execution.NodeSpec.AgentConfig.Entries)
	actual, _ := json.Marshal(entries)
	if string(expected) != string(actual) {
		return graphError("authorization_revoked", "Graph plugin manifest differs from its immutable attempt")
	}
	for _, entry := range entries {
		if entry.ContributionType != plugincontract.ContributionRemoteMCPV1 || entry.ContributionID != contributionID {
			continue
		}
		pin := GraphPluginPin{PluginID: entry.PluginID, InstallationID: entry.InstallationID, ReleaseID: entry.ReleaseID, ContributionID: entry.ContributionID, Entry: &entry}
		if err = s.ValidateGraphPluginPins(ctx, tx, manifest.WorkspaceID, manifest.AgentID, []GraphPluginPin{pin}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	return graphError("authorization_revoked", "Graph contribution is outside this attempt")
}
