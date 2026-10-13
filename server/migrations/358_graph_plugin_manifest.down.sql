CREATE OR REPLACE FUNCTION pin_plugin_execution_manifest()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    workspace UUID;
    source_manifest plugin_execution_manifest%ROWTYPE;
    active_snapshot plugin_capability_snapshot%ROWTYPE;
    manifest_id UUID;
    contributions JSONB := '[]'::jsonb;
BEGIN
    SELECT agent.workspace_id INTO workspace
    FROM agent
    WHERE agent.id = NEW.agent_id;

    IF workspace IS NULL THEN
        RAISE EXCEPTION 'cannot compose plugin execution manifest without task workspace';
    END IF;

    IF NEW.retry_of_task_id IS NOT NULL THEN
        SELECT plugin_execution_manifest.* INTO source_manifest
        FROM agent_task_queue parent
        JOIN plugin_execution_manifest
          ON plugin_execution_manifest.id = parent.plugin_execution_manifest_id
        WHERE parent.id = NEW.retry_of_task_id;
    END IF;

    IF source_manifest.id IS NOT NULL THEN
        INSERT INTO plugin_execution_manifest (
            task_id, workspace_id, agent_id, runtime_id,
            snapshot_id, snapshot_revision, snapshot_digest,
            composer_version, schema_version,
            ordered_contributions, diagnostics
        ) VALUES (
            NEW.id, workspace, NEW.agent_id, NEW.runtime_id,
            source_manifest.snapshot_id,
            source_manifest.snapshot_revision,
            source_manifest.snapshot_digest,
            source_manifest.composer_version,
            source_manifest.schema_version,
            source_manifest.ordered_contributions,
            source_manifest.diagnostics
        ) RETURNING id INTO manifest_id;
    ELSE
        SELECT snapshot.* INTO active_snapshot
        FROM plugin_workspace_capability_state state
        JOIN plugin_capability_snapshot snapshot
          ON snapshot.id = state.active_snapshot_id
        WHERE state.workspace_id = workspace;

        IF active_snapshot.id IS NOT NULL THEN
            SELECT COALESCE(jsonb_agg(entry ORDER BY
                COALESCE((entry->>'ordinal')::integer, 0),
                entry->>'plugin_key',
                entry->>'contribution_key'
            ), '[]'::jsonb)
            INTO contributions
            FROM jsonb_array_elements(active_snapshot.compiled_entries) AS entry
            WHERE (
                entry->>'scope_type' = 'agent'
                AND entry->>'scope_id' = NEW.agent_id::text
            ) OR (
                entry->>'scope_type' = 'workspace'
                AND NOT COALESCE(entry->'excluded_agent_ids', '[]'::jsonb) ? NEW.agent_id::text
            );
        END IF;

        INSERT INTO plugin_execution_manifest (
            task_id, workspace_id, agent_id, runtime_id,
            snapshot_id, snapshot_revision, snapshot_digest,
            composer_version, schema_version,
            ordered_contributions, diagnostics
        ) VALUES (
            NEW.id, workspace, NEW.agent_id, NEW.runtime_id,
            active_snapshot.id,
            COALESCE(active_snapshot.revision, 0),
            active_snapshot.snapshot_digest,
            'plugin-composer-v1', 1,
            contributions, '[]'::jsonb
        ) RETURNING id INTO manifest_id;
    END IF;

    NEW.plugin_execution_manifest_id = manifest_id;
    RETURN NEW;
END;
$$;
