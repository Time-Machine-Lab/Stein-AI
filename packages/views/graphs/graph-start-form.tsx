"use client";

import { useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitCommitHorizontal, Play } from "lucide-react";
import {
  executionNodes,
  graphCommitsOptions,
  graphIssueSearchOptions,
  graphRunOptions,
  graphStartOptions,
  isGraphRuntimeSupported,
  parseJsonObject,
  schemaDefaults,
  validateGraphInput,
  useStartGraphRun,
  type GraphRecord,
  type GraphStartRequest,
  type JsonObject,
} from "@multica/core/graphs";
import { runtimeListOptions } from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { Button } from "@multica/ui/components/ui/button";
import {
  buildRuntimeMachines,
  runtimeRowLabel,
} from "../runtimes/components/runtime-machines";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";
import { Field, GraphError, useGraphCommandKey } from "./shared";
import { GraphResourcePicker } from "./graph-resource-picker";
import { GraphSchemaForm } from "./graph-schema-fields";

export function GraphStartForm({
  wsId,
  graph,
  configuration,
  onConfigure,
  submitLabel,
  externalPending = false,
}: {
  wsId: string;
  graph: GraphRecord;
  configuration?: Partial<GraphStartRequest>;
  onConfigure?: (
    request: Omit<GraphStartRequest, "idempotencyKey">,
  ) => Promise<void>;
  submitLabel?: string;
  externalPending?: boolean;
}) {
  const { t } = useT("graphs"),
    navigation = useNavigation(),
    paths = useWorkspacePaths();
  const runtimes = useQuery(runtimeListOptions(wsId)),
    agents = useQuery(agentListOptions(wsId));
  const [versionId, setVersionId] = useState(configuration?.versionId ?? "");
  const [rootIssueId, setRootIssueId] = useState(
    navigation.searchParams.get("issue") ?? "",
  );
  const [commitCursors, setCommitCursors] = useState<string[]>([]);
  const [issueQuery, setIssueQuery] = useState(""),
    [commitQuery, setCommitQuery] = useState("");
  const [bindings, setBindings] = useState<Record<string, string>>(
    configuration?.runtimeBindings ?? {},
  );
  const [repositoryId, setRepositoryId] = useState(
      configuration?.repository?.repositoryId ?? "",
    ),
    [baseSha, setBaseSha] = useState(configuration?.repository?.baseSha ?? "");
  const [deadline, setDeadline] = useState(""),
    [localError, setLocalError] = useState<Error | null>(null);
  const start = useStartGraphRun(wsId),
    commandKey = useGraphCommandKey();
  const version =
    graph.versions.find((candidate) => candidate.id === versionId) ??
    [...graph.versions].sort((a, b) => b.version - a.version)[0];
  const [input, setInput] = useState<JsonObject>(
    () =>
      configuration?.input ??
      schemaDefaults(version?.definition.inputSchema ?? {}),
  );
  const [rawInput, setRawInput] = useState<string | null>(null);
  const previousRunId = onConfigure
    ? ""
    : (navigation.searchParams.get("previous_run") ?? "");
  const previous = useQuery(graphRunOptions(wsId, previousRunId));
  const restored = useRef(false),
    inputEdited = useRef(false);
  useEffect(() => {
    if (previous.data && !restored.current && !inputEdited.current) {
      restored.current = true;
      setInput(previous.data.input);
      setRootIssueId(previous.data.rootIssueId);
      setVersionId(previous.data.versionId);
    }
  }, [previous.data]);
  const steps = version ? executionNodes(version.definition) : [];

  const needsRepository = steps.some(
    ({ node }) =>
      node.type === "agent" &&
      ["produce_commit", "verify_commit"].includes(node.executionMode ?? ""),
  );
  const repositories = useQuery({
    ...graphStartOptions(wsId, onConfigure ? undefined : rootIssueId),
    enabled: needsRepository && Boolean(rootIssueId || onConfigure),
  });
  const selectedRepository = repositories.data?.repositories.find(
    (repo) => repo.id === repositoryId,
  );
  const canBrowseCommits =
    selectedRepository?.commitsSupported ??
    selectedRepository?.provider === "github";
  const commits = useQuery({
    ...graphCommitsOptions(wsId, repositoryId, {
      q: useDeferredValue(commitQuery),
      ref: selectedRepository?.defaultBranch,
      cursor: commitCursors.at(-1),
    }),
    enabled: Boolean(repositoryId && canBrowseCommits),
  });
  const issues = useQuery(
    graphIssueSearchOptions(wsId, useDeferredValue(issueQuery)),
  );
  const selectedIssue = useQuery(issueDetailOptions(wsId, rootIssueId));
  const issueOptions = [
    ...(selectedIssue.data ? [selectedIssue.data] : []),
    ...(issues.data?.issues ?? []),
  ]
    .filter(
      (issue, index, all) =>
        all.findIndex((candidate) => candidate.id === issue.id) === index &&
        !issue.graph_node_run_id,
    )
    .map((issue) => ({
      value: issue.id,
      label: `${issue.identifier} · ${issue.title}`,
      description: issue.status,
    }));
  const machines = useMemo(
    () => buildRuntimeMachines(runtimes.data ?? [], { now: Date.now() }),
    [runtimes.data],
  );
  const runtimeReason = (
    runtime: AgentRuntime | undefined,
    requireV2 = false,
  ) => {
    if (!runtime) return t(($) => $.start.runtime_missing);
    if (runtime.status !== "online") return t(($) => $.start.runtime_offline);
    if (
      !isGraphRuntimeSupported(
        runtime,
        requireV2 || Boolean(runtime.profile_id),
        true,
      )
    )
      return t(($) => $.start.runtime_upgrade);
    return "";
  };
  const runtimeFor = (path: string, agentId?: string, requireV2 = false) => {
    const agent = agents.data?.find((agent) => agent.id === agentId);
    if (!agent || agent.archived_at) return "";
    const candidate = agent.runtime_id || bindings[path] || "";
    return runtimeReason(
      runtimes.data?.find((runtime) => runtime.id === candidate),
      requireV2,
    )
      ? ""
      : candidate;
  };
  if (!version)
    return (
      <p className="text-body text-muted-foreground">
        {t(($) => $.start.no_version)}
      </p>
    );
  async function submit() {
    if (!version) return;
    setLocalError(null);
    try {
      const parsed = rawInput === null ? input : parseJsonObject(rawInput);
      const errors = validateGraphInput(version.definition.inputSchema, parsed);
      if (errors.length)
        throw new Error(
          t(($) => $.production.input_invalid, {
            fields: errors.map((error) => error.path || "/").join(", "),
          }),
        );
      if (!rootIssueId && !onConfigure)
        throw new Error(t(($) => $.production.search_issue));
      const unavailable = steps.find(
        ({ node, path }) =>
          !runtimeFor(path, node.agentId, node.type === "script"),
      );
      if (unavailable) {
        const agent = agents.data?.find(
          (agent) => agent.id === unavailable.node.agentId,
        );
        throw new Error(
          t(($) => $.start.runtime_node_error, {
            node: unavailable.node.label || unavailable.path,
            agent: agent?.name || unavailable.node.agentId,
            reason:
              agent && !agent.archived_at
                ? runtimeReason(
                    runtimes.data?.find(
                      (runtime) =>
                        runtime.id ===
                        (agent.runtime_id || bindings[unavailable.path]),
                    ),
                    unavailable.node.type === "script",
                  )
                : t(($) => $.start.agent_unavailable),
          }),
        );
      }
      if (
        needsRepository &&
        (!repositoryId || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/i.test(baseSha))
      )
        throw new Error(t(($) => $.production.choose_commit));
      const request = {
        versionId: version.id,
        rootIssueId,
        input: parsed,
        runtimeBindings: Object.fromEntries(
          steps.map(({ node, path }) => [
            path,
            runtimeFor(path, node.agentId, node.type === "script"),
          ]),
        ),
        repository: needsRepository
          ? { repositoryId, baseSha, commitSha: baseSha }
          : undefined,
        deadline: deadline ? new Date(deadline).toISOString() : undefined,
        previousRunId: previousRunId || undefined,
      };
      if (onConfigure) await onConfigure(request);
      else {
        const run = await start.mutateAsync({
          ...request,
          idempotencyKey: commandKey(request),
        });
        navigation.push(paths.graphRun(run.id));
      }
    } catch (error) {
      setLocalError(error instanceof Error ? error : new Error(String(error)));
    }
  }
  return (
    <form
      className="space-y-6 rounded-2xl border bg-card p-5 sm:p-6"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <div>
        <h2 className="text-title font-semibold">
          {submitLabel ?? t(($) => $.start.title)}
        </h2>
        <p className="mt-2 text-caption leading-5 text-muted-foreground">
          {t(($) => $.start.fixed)}
        </p>
      </div>
      <GraphError
        error={
          localError ??
          start.error ??
          runtimes.error ??
          agents.error ??
          repositories.error ??
          commits.error ??
          issues.error ??
          previous.error
        }
      />
      {previousRunId && (
        <AppLink
          href={paths.graphRun(previousRunId)}
          className="text-caption underline"
        >
          {t(($) => $.production.previous_run)}
        </AppLink>
      )}
      <fieldset
        disabled={
          start.isPending || externalPending || Boolean(graph.archivedAt)
        }
        className="space-y-5"
      >
        <div className="grid gap-4 md:grid-cols-2">
          <Field label={t(($) => $.start.version)}>
            {(id) => (
              <NativeSelect
                id={id}
                className="w-full"
                required
                value={version.id}
                onChange={(event) => {
                  setVersionId(event.target.value);
                  const next = graph.versions.find(
                    (item) => item.id === event.target.value,
                  );
                  if (next && !inputEdited.current)
                    setInput(schemaDefaults(next.definition.inputSchema));
                }}
              >
                {graph.versions.map((item) => (
                  <option key={item.id} value={item.id}>
                    {t(($) => $.page.version, { version: item.version })}
                  </option>
                ))}
              </NativeSelect>
            )}
          </Field>
          {!onConfigure && (
            <Field label={t(($) => $.start.issue)}>
              {(id) => (
                <GraphResourcePicker
                  id={id}
                  label={t(($) => $.start.issue)}
                  value={rootIssueId}
                  onChange={(value) => {
                    setRootIssueId(value);
                    setRepositoryId("");
                    setBaseSha("");
                    setCommitCursors([]);
                  }}
                  options={issueOptions}
                  placeholder={t(($) => $.production.search_issue)}
                  onSearch={setIssueQuery}
                  loading={issues.isFetching}
                />
              )}
            </Field>
          )}
        </div>
        <section className="space-y-4 rounded-xl bg-muted/20 p-4">
          <h3 className="text-body font-medium">{t(($) => $.start.input)}</h3>
          <GraphSchemaForm
            schema={version.definition.inputSchema}
            value={input}
            onChange={(next) => {
              inputEdited.current = true;
              setInput(next);
              setRawInput(null);
            }}
            disabled={start.isPending}
          />
          <details>
            <summary className="cursor-pointer text-caption text-muted-foreground">
              {t(($) => $.production.raw_details)}
            </summary>
            <Textarea
              className="mt-3 min-h-28 font-mono text-caption"
              aria-label={t(($) => $.start.input)}
              value={rawInput ?? JSON.stringify(input, null, 2)}
              onChange={(event) => {
                inputEdited.current = true;
                setRawInput(event.target.value);
              }}
            />
          </details>
        </section>
        {steps.map(({ node, path }) => {
          const agent = agents.data?.find((agent) => agent.id === node.agentId);
          const bound = agent?.runtime_id;
          const machine = machines.find((machine) =>
            machine.runtimes.some((runtime) => runtime.id === bound),
          );
          const runtime = machine?.runtimes.find(
            (runtime) => runtime.id === bound,
          );
          const reason =
            !agents.isPending && (!agent || agent.archived_at)
              ? t(($) => $.start.agent_unavailable)
              : bound
                ? runtimeReason(runtime, node.type === "script")
                : "";
          return (
            <Field
              key={path}
              label={t(($) => $.start.runtime, {
                node: `${node.label || path}${agents.data?.find((agent) => agent.id === node.agentId)?.name ? ` · ${agents.data.find((agent) => agent.id === node.agentId)!.name}` : ""}`,
              })}
            >
              {(id) => (
                <div className="space-y-2">
                  {bound ? (
                    <>
                      <output
                        id={id}
                        className="block rounded-lg border bg-muted/20 px-3 py-2 text-body"
                      >
                        {machine && runtime
                          ? `${machine.title} · ${runtimeRowLabel(runtime, machine.title)}`
                          : t(($) => $.start.runtime_missing)}
                      </output>
                      <p className="text-caption text-muted-foreground">
                        {t(($) => $.start.runtime_bound)}
                      </p>
                    </>
                  ) : (
                    <NativeSelect
                      id={id}
                      className="w-full"
                      required
                      value={runtimeFor(
                        path,
                        node.agentId,
                        node.type === "script",
                      )}
                      onChange={(event) =>
                        setBindings((current) => ({
                          ...current,
                          [path]: event.target.value,
                        }))
                      }
                    >
                      <option value="">
                        {t(($) => $.start.choose_runtime)}
                      </option>
                      {machines.map((machine) => (
                        <optgroup key={machine.id} label={machine.title}>
                          {machine.runtimes.map((runtime) => (
                            <option
                              key={runtime.id}
                              value={runtime.id}
                              disabled={Boolean(
                                runtimeReason(runtime, node.type === "script"),
                              )}
                            >
                              {runtimeRowLabel(runtime, machine.title)}
                              {runtimeReason(runtime, node.type === "script")
                                ? ` · ${runtimeReason(runtime, node.type === "script")}`
                                : ""}
                            </option>
                          ))}
                        </optgroup>
                      ))}
                    </NativeSelect>
                  )}
                  {reason && (
                    <p role="alert" className="text-caption text-destructive">
                      {t(($) => $.start.runtime_node_error, {
                        node: node.label || path,
                        agent: agent?.name || node.agentId,
                        reason,
                      })}
                    </p>
                  )}
                </div>
              )}
            </Field>
          );
        })}
        {steps.length > 0 && (
          <details className="rounded-lg border p-3 text-caption">
            <summary className="cursor-pointer">
              {t(($) => $.start.runtime_inventory, { count: machines.length })}
            </summary>
            <ul className="mt-3 space-y-3">
              {machines.map((machine) => (
                <li key={machine.id}>
                  <p className="font-medium">{machine.title}</p>
                  <ul className="mt-1 space-y-1 text-muted-foreground">
                    {machine.runtimes.map((runtime) => (
                      <li key={runtime.id}>
                        {runtimeRowLabel(runtime, machine.title)} ·{" "}
                        {runtimeReason(
                          runtime,
                          steps.some(({ node }) => node.type === "script"),
                        ) || t(($) => $.start.runtime_ready)}
                      </li>
                    ))}
                  </ul>
                </li>
              ))}
            </ul>
          </details>
        )}
        {needsRepository && (
          <section className="space-y-4 rounded-xl border p-4">
            <Field label={t(($) => $.start.repository)}>
              {(id) => (
                <GraphResourcePicker
                  id={id}
                  label={t(($) => $.start.repository)}
                  value={repositoryId}
                  onChange={(value) => {
                    setRepositoryId(value);
                    setBaseSha("");
                    setCommitQuery("");
                    setCommitCursors([]);
                  }}
                  options={(repositories.data?.repositories ?? []).map(
                    (repo) => ({
                      value: repo.id,
                      label: repo.name,
                      description: repo.url,
                    }),
                  )}
                  placeholder={t(($) => $.production.search_repository)}
                  loading={repositories.isFetching}
                />
              )}
            </Field>
            <Field label={t(($) => $.start.base_sha)}>
              {(id) =>
                canBrowseCommits || !selectedRepository ? (
                  <GraphResourcePicker
                    id={id}
                    label={t(($) => $.start.base_sha)}
                    value={baseSha}
                    onChange={setBaseSha}
                    options={(commits.data?.commits ?? []).map((commit) => ({
                      value: commit.sha,
                      label: `${commit.sha.slice(0, 8)} · ${commit.message}`,
                      description: [
                        commit.author,
                        commit.createdAt
                          ? new Date(commit.createdAt).toLocaleDateString()
                          : "",
                      ]
                        .filter(Boolean)
                        .join(" · "),
                    }))}
                    placeholder={t(($) => $.production.search_commit)}
                    onSearch={(value) => {
                      setCommitQuery(value);
                      setCommitCursors([]);
                    }}
                    loading={commits.isFetching}
                    disabled={!repositoryId}
                  />
                ) : (
                  <div className="space-y-2">
                    <Input
                      id={id}
                      value={baseSha}
                      onChange={(event) =>
                        setBaseSha(event.target.value.trim())
                      }
                      placeholder={t(($) => $.production.manual_sha)}
                      pattern="([a-fA-F0-9]{40}|[a-fA-F0-9]{64})"
                      required
                    />
                    <p className="text-caption text-muted-foreground">
                      {t(($) => $.production.manual_sha_help)}
                    </p>
                  </div>
                )
              }
            </Field>
            {canBrowseCommits &&
              (commits.data?.nextCursor || commitCursors.length > 0) && (
                <div className="flex justify-end gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={!commitCursors.length || commits.isFetching}
                    onClick={() =>
                      setCommitCursors((current) => current.slice(0, -1))
                    }
                  >
                    {t(($) => $.production.previous)}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={!commits.data?.nextCursor || commits.isFetching}
                    onClick={() => {
                      if (commits.data?.nextCursor)
                        setCommitCursors((current) => [
                          ...current,
                          commits.data!.nextCursor!,
                        ]);
                    }}
                  >
                    {t(($) => $.production.next)}
                  </Button>
                </div>
              )}
            {baseSha && (
              <p className="flex items-center gap-2 break-all font-mono text-caption text-muted-foreground">
                <GitCommitHorizontal className="size-3.5 shrink-0" />
                {baseSha}
              </p>
            )}
          </section>
        )}
        {!onConfigure && (
          <Field label={t(($) => $.start.deadline)}>
            {(id) => (
              <Input
                id={id}
                type="datetime-local"
                value={deadline}
                onChange={(event) => setDeadline(event.target.value)}
              />
            )}
          </Field>
        )}
        <Button
          type="submit"
          disabled={
            (!rootIssueId && !onConfigure) ||
            start.isPending ||
            externalPending ||
            steps.some(
              ({ node, path }) =>
                !runtimeFor(path, node.agentId, node.type === "script"),
            )
          }
        >
          <Play className="size-4" />
          {submitLabel ?? t(($) => $.start.submit)}
        </Button>
      </fieldset>
    </form>
  );
}
