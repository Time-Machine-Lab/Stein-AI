import { z } from "zod";
import { ApiError } from "../api/client";
import { parseWithFallback } from "../api/schema";

const graphFailureSchema = z.object({
  code: z.string(),
  node_path: z.string().optional().catch(undefined),
  node_label: z.string().optional().catch(undefined),
  agent_name: z.string().optional().catch(undefined),
});

export function graphFailureContext(error: unknown) {
  if (!(error instanceof ApiError) || !error.body) return undefined;
  return parseWithFallback<z.infer<typeof graphFailureSchema> | undefined>(
    error.body,
    graphFailureSchema,
    undefined,
    { endpoint: "graph error" },
  );
}
