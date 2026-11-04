"use client";

import type { GraphRecord } from "@multica/core/graphs";
import { GraphDraftNotices, GraphStudio } from "./graph-studio";
import type { GraphPublishFlow } from "./use-graph-publish-flow";

/**
 * The editor body of a graph page. The draft, saving and publishing belong to
 * the page (`useGraphPublishFlow`) so its header can carry those actions; this
 * is the notices about the draft document and the studio below them.
 */
export function GraphEditor({
  wsId,
  graph,
  flow,
  initialMessage,
}: {
  wsId: string;
  graph?: GraphRecord;
  flow: GraphPublishFlow;
  initialMessage?: string;
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <GraphDraftNotices flow={flow} />
      <GraphStudio
        wsId={wsId}
        flow={flow}
        archived={graph ? Boolean(graph.archivedAt) : undefined}
        initialMessage={initialMessage}
      />
    </div>
  );
}
