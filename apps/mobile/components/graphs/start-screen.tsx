import { useEffect, useRef, useState, useDeferredValue } from "react";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type {
  GraphRecord,
  GraphStartRequest,
} from "@multica/core/graphs/types";
import { executionNodes } from "@multica/core/graphs/definition";
import {
  asSchema,
  schemaDefaults,
  validateGraphInput,
} from "@multica/core/graphs/input-schema";
import { isGraphRuntimeSupported } from "@multica/core/graphs/controls";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import {
  graphDetailOptions,
  graphIssueSearchOptions,
  graphStartOptions,
  graphCommitsOptions,
  graphRunOptions,
} from "@/data/queries/graphs";
import { issueDetailOptions } from "@/data/queries/issues";
import { runtimeListOptions } from "@/data/queries/runtimes";
import { agentListOptions } from "@/data/queries/agents";
import { useStartGraphRun } from "@/data/mutations/graphs";
import {
  graphDraftIdentity,
  useGraphDraftStore,
} from "@/data/stores/graph-draft-store";
import {
  GraphScreen,
  GraphSection,
  GraphPick,
  GraphText,
  GraphError,
  useGraphContext,
  useGraphCommandKey,
} from "./shared";
import { GraphInputFields } from "./fields";
export function GraphStartScreen({
  id,
  previousRunId,
  rootIssueId,
}: {
  id: string;
  previousRunId?: string;
  rootIssueId?: string;
}) {
  const { wsId } = useGraphContext(),
    graph = useQuery(graphDetailOptions(wsId, id));
  return (
    <GraphScreen loading={graph.isLoading}>
      <GraphError error={graph.error} retry={() => void graph.refetch()} />
      {graph.data && (
        <GraphStartFields
          graph={graph.data}
          previousRunId={previousRunId}
          rootIssueId={rootIssueId}
        />
      )}
    </GraphScreen>
  );
}
export function GraphStartFields({
  graph,
  previousRunId,
  rootIssueId,
  configuration,
  onConfigure,
  submitLabel = "Start run",
  pending = false,
  formId = "",
}: {
  graph: GraphRecord;
  previousRunId?: string;
  rootIssueId?: string;
  configuration?: Partial<GraphStartRequest>;
  onConfigure?: (
    request: Omit<GraphStartRequest, "idempotencyKey">,
  ) => Promise<void>;
  submitLabel?: string;
  pending?: boolean;
  formId?: string;
}) {
  const { wsId, userId, slug } = useGraphContext(),
    key = graphDraftIdentity(
      wsId,
      userId,
      `start-${graph.id}-${previousRunId ?? ""}-${configuration?.versionId ?? ""}-${formId}`,
    ),
    doc = useGraphDraftStore((s) => s.documents[key]);
  const [error, setError] = useState<unknown>(null),
    [issueQuery, setIssueQuery] = useState(""),
    [ref, setRef] = useState(""),
    [cursors, setCursors] = useState<string[]>([]);
  const prior = useQuery(graphRunOptions(wsId, previousRunId ?? "")),
    agents = useQuery(agentListOptions(wsId)),
    runtimes = useQuery(runtimeListOptions(wsId)),
    start = useStartGraphRun(wsId),
    commandKey = useGraphCommandKey();
  const versions = [...graph.versions].sort((a, b) => b.version - a.version),
    version =
      versions.find((v) => v.id === doc?.values.versionId) ?? versions[0];
  useEffect(() => {
    if (!version || !userId) return;
    const store = useGraphDraftStore.getState();
    store.open(key, version.definition, 0);
    if (
      !store.documents[key]?.values.initialized &&
      !useGraphDraftStore.getState().documents[key]?.values.initialized
    )
      store.field(key, ["values"], {
        initialized: true,
        versionId: configuration?.versionId ?? version.id,
        issueId: rootIssueId ?? "",
        input:
          configuration?.input ??
          schemaDefaults(version.definition.inputSchema),
        bindings: configuration?.runtimeBindings ?? {},
        repositoryId: configuration?.repository?.repositoryId ?? "",
        baseSha: configuration?.repository?.baseSha ?? "",
      });
  }, [key, userId, version, configuration, rootIssueId]);
  const restored = useRef(false);
  useEffect(() => {
    if (
      prior.data &&
      doc &&
      !restored.current &&
      !doc.values.inputTouched &&
      !doc.values.issueTouched
    ) {
      restored.current = true;
      const store = useGraphDraftStore.getState();
      store.field(key, ["values"], {
        ...doc.values,
        input: prior.data.input,
        issueId: prior.data.rootIssueId,
        versionId: prior.data.versionId,
      });
    }
  }, [prior.data, doc, key]);
  const issueId = String(doc?.values.issueId ?? ""),
    repositoryId = String(doc?.values.repositoryId ?? ""),
    baseSha = String(doc?.values.baseSha ?? "");
  const oldIssue = useRef(issueId),
    oldRepository = useRef(repositoryId);
  useEffect(() => {
    if (oldIssue.current !== issueId) {
      oldIssue.current = issueId;
      useGraphDraftStore.getState().field(key, ["values", "repositoryId"], "");
      useGraphDraftStore.getState().field(key, ["values", "baseSha"], "");
      setCursors([]);
    }
  }, [issueId, key]);
  useEffect(() => {
    if (oldRepository.current !== repositoryId) {
      oldRepository.current = repositoryId;
      useGraphDraftStore.getState().field(key, ["values", "baseSha"], "");
      setCursors([]);
    }
  }, [repositoryId, key]);
  const steps = version ? executionNodes(version.definition) : [],
    needsRepository = steps.some(
      ({ node }) => node.executionMode && node.executionMode !== "none",
    );
  const issues = useQuery(
      graphIssueSearchOptions(wsId, useDeferredValue(issueQuery)),
    ),
    selectedIssue = useQuery(issueDetailOptions(wsId, issueId));
  const repositories = useQuery({
      ...graphStartOptions(wsId, issueId),
      enabled: needsRepository && !!(issueId || onConfigure),
    }),
    repo = repositories.data?.repositories.find((r) => r.id === repositoryId);
  const commits = useQuery({
    ...graphCommitsOptions(wsId, repositoryId, {
      q: useDeferredValue(ref),
      ref: repo?.defaultBranch,
      cursor: cursors[cursors.length - 1],
    }),
    enabled:
      !!repositoryId && (repo?.commitsSupported ?? repo?.provider === "github"),
  });
  if (!version)
    return <Text>Publish a version before starting this workflow.</Text>;
  if (!doc) return null;
  const set = (name: string, value: unknown) =>
      useGraphDraftStore.getState().field(key, ["values", name], value),
    input = asSchema(doc.values.input),
    bindings = asSchema(doc.values.bindings);
  const supported = (runtimes.data ?? []).filter(
    (r) => r.status === "online" && isGraphRuntimeSupported(r),
  );
  const runtimeFor = (path: string, agentId?: string, script = false) => {
    const value = String(
      bindings[path] ??
        agents.data?.find((a) => a.id === agentId)?.runtime_id ??
        "",
    );
    return supported.some(
      (r) =>
        r.id === value && isGraphRuntimeSupported(r, script || !!r.profile_id),
    )
      ? value
      : "";
  };
  const issueOptions = [
    ...(selectedIssue.data ? [selectedIssue.data] : []),
    ...(issues.data?.issues ?? []),
  ]
    .filter(
      (item, index, all) =>
        !item.graph_node_run_id &&
        all.findIndex((i) => i.id === item.id) === index,
    )
    .map((i) => ({ value: i.id, label: `${i.identifier} · ${i.title}` }));
  const submit = async () => {
    setError(null);
    try {
      const errors = validateGraphInput(version.definition.inputSchema, input);
      if (errors.length)
        throw new Error(
          "Check input: " + errors.map((e) => e.path || "/").join(", "),
        );
      if (!issueId && !onConfigure) throw new Error("Choose a parent task.");
      if (
        steps.some(
          ({ node, path }) =>
            !runtimeFor(path, node.agentId, node.type === "script"),
        )
      )
        throw new Error(
          "Every agent or script step requires a compatible online runtime.",
        );
      if (
        needsRepository &&
        (!repositoryId || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/i.test(baseSha))
      )
        throw new Error("Choose a repository and exact commit.");
      const request = {
        versionId: version.id,
        rootIssueId: issueId,
        input,
        runtimeBindings: Object.fromEntries(
          steps.map(({ node, path }) => [
            path,
            runtimeFor(path, node.agentId, node.type === "script"),
          ]),
        ),
        repository: needsRepository
          ? { repositoryId, baseSha, commitSha: baseSha }
          : undefined,
        previousRunId,
      };
      if (onConfigure) await onConfigure(request);
      else {
        const run = await start.mutateAsync({
          ...request,
          idempotencyKey: commandKey(request),
        });
        router.push(`/${slug}/graph/run/${run.id}`);
      }
    } catch (e) {
      setError(e);
    }
  };
  const disabled = pending || start.isPending || !!graph.archivedAt;
  return (
    <>
      <Text className="text-lg font-semibold">{graph.name}</Text>
      <Text className="text-sm text-muted-foreground">
        The published version, inputs and runtime bindings are frozen when the
        run starts.
      </Text>
      <GraphError
        error={
          error ??
          start.error ??
          issues.error ??
          repositories.error ??
          commits.error ??
          runtimes.error ??
          agents.error ??
          prior.error
        }
      />
      <GraphPick
        documentKey={key}
        path={["values", "versionId"]}
        label="Published version"
        value={version.id}
        options={versions.map((v) => ({
          value: v.id,
          label: `Version ${v.version}`,
        }))}
        disabled={disabled}
      />
      {!onConfigure && (
        <>
          <GraphText
            label="Find parent task"
            value={issueQuery}
            onChange={setIssueQuery}
            disabled={disabled}
          />
          <GraphPick
            documentKey={key}
            path={["values", "issueId"]}
            label="Parent task"
            value={issueId}
            options={issueOptions}
            disabled={disabled}
          />
        </>
      )}
      <GraphSection title="Run inputs">
        <GraphInputFields
          documentKey={key}
          path={["values", "input"]}
          schema={version.definition.inputSchema}
          value={input}
          disabled={disabled}
        />
      </GraphSection>
      {steps.map(({ node, path }) => (
        <GraphPick
          key={path}
          documentKey={key}
          path={["values", "bindings", path]}
          label={`${node.label || node.nodeKey} · runtime`}
          value={runtimeFor(path, node.agentId, node.type === "script")}
          options={supported
            .filter((r) =>
              isGraphRuntimeSupported(
                r,
                node.type === "script" || !!r.profile_id,
              ),
            )
            .map((r) => ({
              value: r.id,
              label: r.name || r.id,
              description: r.provider,
            }))}
          disabled={disabled}
        />
      ))}
      {needsRepository && (
        <GraphSection title="Repository snapshot">
          <GraphPick
            documentKey={key}
            path={["values", "repositoryId"]}
            label="Repository"
            value={repositoryId}
            options={(repositories.data?.repositories ?? []).map((r) => ({
              value: r.id,
              label: r.name,
              description: r.url,
            }))}
            disabled={disabled}
          />
          {(repo?.commitsSupported ?? repo?.provider === "github") ? (
            <>
              <GraphText
                label="Branch, tag or commit SHA"
                value={ref}
                onChange={(v) => {
                  setRef(v);
                  setCursors([]);
                }}
                disabled={disabled}
              />
              <GraphPick
                documentKey={key}
                path={["values", "baseSha"]}
                label="Base commit"
                value={baseSha}
                options={(commits.data?.commits ?? []).map((c) => ({
                  value: c.sha,
                  label: `${c.sha.slice(0, 8)} · ${c.message}`,
                  description: c.author,
                }))}
                disabled={disabled}
              />
              <Button
                variant="outline"
                disabled={!cursors.length || commits.isFetching}
                onPress={() => setCursors((c) => c.slice(0, -1))}
              >
                <Text>Previous commits</Text>
              </Button>
              <Button
                variant="outline"
                disabled={!commits.data?.nextCursor || commits.isFetching}
                onPress={() =>
                  setCursors((c) => [...c, commits.data!.nextCursor!])
                }
              >
                <Text>More commits</Text>
              </Button>
            </>
          ) : (
            <>
              <GraphText
                label="Exact commit SHA"
                value={baseSha}
                onChange={(v) => set("baseSha", v.trim())}
                disabled={disabled}
              />
              <Text className="text-sm text-muted-foreground">
                Connect this repository’s provider to browse commits, or enter
                its full 40 or 64 character SHA.
              </Text>
            </>
          )}
        </GraphSection>
      )}
      <Button disabled={disabled} onPress={() => void submit()}>
        <Text>{start.isPending ? "Starting…" : submitLabel}</Text>
      </Button>
    </>
  );
}
