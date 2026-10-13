"use client";

import { useQuery } from "@tanstack/react-query";
import { GitBranch } from "lucide-react";
import {
  canDetachGraphRun,
  graphRunOptions,
  isGraphManagedIssue,
  useDetachGraphIssue,
} from "@multica/core/graphs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Issue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { AppLink } from "../navigation";
import { useT } from "../i18n";
import {
  GraphError,
  GraphState,
  useGraphActor,
  useGraphCommandKey,
} from "./shared";

export function GraphIssueCard({ issue }: { issue: Issue }) {
  const { t } = useT("graphs");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const run = useQuery(graphRunOptions(wsId, issue.graph_run_id ?? ""));
  const actor = useGraphActor(wsId);
  const detach = useDetachGraphIssue(wsId);
  const commandKey = useGraphCommandKey();
  const managed = isGraphManagedIssue(issue);
  return (
    <div className="mb-5 space-y-2 rounded-lg border p-3 text-body">
      <div className="flex flex-wrap items-center gap-2">
        <GitBranch className="size-4" />
        <span className="font-medium">{t(($) => $.page.title)}</span>
        {run.data && <GraphState state={run.data.state} />}
        <AppLink
          className="ml-auto text-caption underline underline-offset-4"
          href={
            issue.graph_run_id
              ? paths.graphRun(issue.graph_run_id)
              : `${paths.graphs()}?issue=${encodeURIComponent(issue.id)}`
          }
        >
          {managed ? t(($) => $.run.open_run) : t(($) => $.start.title)}
        </AppLink>
      </div>
      {managed && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.run.graph_managed)}
        </p>
      )}
      <GraphError error={detach.error} />
      {managed &&
        !issue.graph_node_run_id &&
        canDetachGraphRun(run.data, actor) && (
          <Button
            size="sm"
            variant="outline"
            disabled={detach.isPending}
            onClick={() => {
              const request = { issueId: issue.id, runId: issue.graph_run_id };
              detach.mutate(
                { issueId: issue.id, idempotencyKey: commandKey(request) },
                { onSuccess: () => toast.success(t(($) => $.run.detached)) },
              );
            }}
          >
            {t(($) => $.run.detach)}
          </Button>
        )}
    </div>
  );
}
