import { useEffect } from "react";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useGraphDraftStore } from "@/data/stores/graph-draft-store";
import { useQueryClient } from "@tanstack/react-query";
import { isGraphManagedIssue } from "@multica/core/graphs/controls";
import { graphKeys } from "@/data/queries/graphs";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

/** Graph events carry identifiers, not a complete multi-table run snapshot. */
export function useGraphsRealtime() {
  const qc = useQueryClient();
  const userId = useAuthStore((s) => s.user?.id), workspaceId = useWorkspaceStore((s) => s.currentWorkspaceId);
  useEffect(() => () => useGraphDraftStore.getState().clear(), [userId, workspaceId]);
  useWSSubscriptions((ws, wsId) => {
    const refresh = () => { void qc.invalidateQueries({ queryKey: graphKeys.all(wsId) }); };
    return [
      ws.on("issue:updated", ({ issue }) => { if (isGraphManagedIssue(issue)) refresh(); }),
      ws.onAny((message) => { if (message.type.startsWith("graph:")) refresh(); }),
      ws.onReconnect(refresh),
    ];
  }, [qc]);
}
