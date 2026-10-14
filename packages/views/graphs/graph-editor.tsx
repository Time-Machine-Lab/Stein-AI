"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Check,
  ChevronRight,
  Download,
  GitBranch,
  LayoutGrid,
  Loader2,
  Plus,
  Redo2,
  Sparkles,
  Undo2,
} from "lucide-react";
import { toast } from "sonner";
import {
  createSerialDefinition,
  exportGraphDefinition,
  graphDefinitionChanges,
  graphDraftKey,
  graphGenerationOptions,
  importGraphDefinition,
  layoutGraph,
  useArchiveGraph,
  useGenerateGraph,
  useGraphDraftStore,
  usePublishGraph,
  useSaveGraph,
  useValidateGraph,
  type GraphDefinition,
  type GraphGenerationRequest,
  type GraphNodeDefinition,
  type GraphRecord,
} from "@multica/core/graphs";
import { errorCode } from "@multica/core/api/client";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { useWorkspacePaths, useWorkspaceSlug } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { copyText } from "@multica/ui/lib/clipboard";
import { downloadBlob } from "../editor/utils/mermaid-export";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { GraphValueView } from "./graph-result-view";
import { Field, GraphError, useGraphActor, useGraphCommandKey } from "./shared";
import { GraphAiComposer } from "./graph-ai-composer";
import { GraphCanvas, useGraphNodeLabels } from "./graph-canvas";
import { GraphNodeInspector } from "./graph-node-inspector";
import { GraphSchemaBuilder } from "./graph-schema-fields";
import { defaultCondition, GraphBindings } from "./graph-bindings";

const objectSchema = { type: "object", additionalProperties: true };
function blankDefinition(name = ""): GraphDefinition {
  return {
    ...createSerialDefinition(
      name,
      [
        {
          nodeKey: "implement",
          type: "agent",
          agentId: "",
          instructions: "",
          inputs: { context: { source: "run_input", path: "" } },
          outputSchema: objectSchema,
          executionMode: "none",
          maxAttempts: 2,
          timeoutSeconds: 3600,
        },
      ],
      "",
    ),
    schemaVersion: 2,
    maxConcurrency: 4,
  };
}
function emptyBody(name: string): GraphDefinition {
  return {
    schemaVersion: 2,
    name,
    inputSchema: objectSchema,
    outputSchema: objectSchema,
    outputs: {},
    nodes: [
      { nodeKey: "start", type: "start" },
      { nodeKey: "end", type: "end" },
    ],
    edges: [{ from: "start", to: "end" }],
  };
}
function newNode(type: string, nodeKey: string): GraphNodeDefinition {
  const base = { nodeKey, type };
  switch (type) {
    case "agent":
      return { ...blankDefinition().nodes[1]!, nodeKey };
    case "approval":
      return { ...base, approval: { approverIds: [], mode: "all" } };
    case "branch":
      return { ...base, condition: defaultCondition() };
    case "delay":
      return { ...base, delay: { seconds: 60 } };
    case "http":
      return {
        ...base,
        outputSchema: objectSchema,
        http: {
          method: "GET",
          url: "",
          timeoutSeconds: 30,
          maxResponseBytes: 65536,
        },
      };
    case "script":
      return {
        ...base,
        agentId: "",
        inputs: {},
        outputSchema: objectSchema,
        script: { language: "javascript", code: "", timeoutSeconds: 60 },
      };
    case "loop":
      return {
        ...base,
        inputs: {},
        outputSchema: objectSchema,
        loop: {
          body: emptyBody(nodeKey),
          maxIterations: 3,
          condition: {
            op: "eq",
            left: { source: "constant", value: false },
            right: { source: "constant", value: true },
          },
          next: {},
        },
      };
    case "subgraph":
      return {
        ...base,
        inputs: {},
        outputSchema: objectSchema,
        subgraph: { definition: emptyBody(nodeKey) },
      };
    default:
      return base;
  }
}
function getScope(
  definition: GraphDefinition,
  path: string[],
): GraphDefinition {
  for (const key of path) {
    const node = definition.nodes.find((item) => item.nodeKey === key);
    if (node?.loop) definition = node.loop.body;
    else if (node?.subgraph) definition = node.subgraph.definition;
    else break;
  }
  return definition;
}
function replaceScope(
  definition: GraphDefinition,
  path: string[],
  updated: GraphDefinition,
): GraphDefinition {
  if (!path.length) return updated;
  return {
    ...definition,
    schemaVersion: 2,
    nodes: definition.nodes.map((node) => {
      if (node.nodeKey !== path[0]) return node;
      if (node.loop) {
        const body = replaceScope(node.loop.body, path.slice(1), updated);
        return {
          ...node,
          outputSchema: body.outputSchema,
          loop: { ...node.loop, body },
        };
      }
      if (node.subgraph) {
        const child = replaceScope(
          node.subgraph.definition,
          path.slice(1),
          updated,
        );
        return {
          ...node,
          outputSchema: child.outputSchema,
          subgraph: { definition: child },
        };
      }
      return node;
    }),
  };
}

export function GraphEditor({
  wsId,
  graph,
}: {
  wsId: string;
  graph?: GraphRecord;
}) {
  const { t } = useT("graphs"),
    labels = useGraphNodeLabels();
  const workspaceSlug = useWorkspaceSlug();
  const actor = useGraphActor(wsId),
    navigation = useNavigation(),
    paths = useWorkspacePaths();
  const agents = useQuery(agentListOptions(wsId)),
    members = useQuery(memberListOptions(wsId));
  const generationOptions = useQuery(graphGenerationOptions(wsId));
  const generate = useGenerateGraph(wsId),
    save = useSaveGraph(wsId),
    validate = useValidateGraph(),
    publish = usePublishGraph(wsId),
    archive = useArchiveGraph(wsId);
  const initial = useMemo(
    () => graph?.definition ?? blankDefinition(),
    [graph?.definition],
  );
  const draftKey = graphDraftKey(wsId, actor.userId, graph?.id);
  const draft = useGraphDraftStore((state) => state.documents[draftKey]);
  const definition = draft?.definition ?? initial;
  const [narrowPane, setNarrowPane] = useState<"canvas" | "inspector">(
    "canvas",
  );
  const [scopePath, setScopePath] = useState<string[]>([]),
    [selectedKey, setSelectedKey] = useState("implement");
  const [mode, setMode] = useState<"ai" | "editor">(graph ? "editor" : "ai");
  const [localError, setLocalError] = useState<Error | null>(null),
    [definitionJSON, setDefinitionJSON] = useState("");
  const [prompt, setPrompt] = useState(""),
    [generationAgentId, setGenerationAgentId] = useState(""),
    [generationApproverId, setGenerationApproverId] = useState("");
  const [messages, setMessages] = useState<
    NonNullable<GraphGenerationRequest["messages"]>
  >([]);
  const [proposal, setProposal] = useState<{
    definition: GraphDefinition;
    base: string;
    summary?: string;
  } | null>(null);
  const [showComparison, setShowComparison] = useState(false);
  const [generating, setGenerating] = useState(false),
    [nodeType, setNodeType] = useState("agent");
  const commandKey = useGraphCommandKey();
  const admin = actor.role === "admin" || actor.role === "owner",
    canEdit =
      Boolean(actor.userId) &&
      (!graph || admin || graph.createdBy === actor.userId);
  const supported = [1, 2].includes(definition.schemaVersion);
  const pending =
    save.isPending ||
    validate.isPending ||
    publish.isPending ||
    archive.isPending;
  const disabled =
    pending || !canEdit || !supported || Boolean(graph?.archivedAt);
  const scope = getScope(definition, scopePath),
    selected =
      scope.nodes.find((node) => node.nodeKey === selectedKey) ??
      scope.nodes[0];
  const inLoop = scopePath.some(
    (key, index) =>
      getScope(definition, scopePath.slice(0, index)).nodes.find(
        (node) => node.nodeKey === key,
      )?.type === "loop",
  );
  const generationAgents = (agents.data ?? []).filter(
    (agent) => !agent.archived_at,
  );
  const selectedGenerationAgent =
    generationAgentId ||
    (generationAgents.length === 1 ? generationAgents[0]!.id : "");
  const selectedGenerationApprover =
    generationApproverId ||
    (members.data?.some((member) => member.id === actor.memberId)
      ? actor.memberId!
      : "");
  useEffect(() => {
    if (actor.userId)
      useGraphDraftStore
        .getState()
        .open(
          draftKey,
          initial,
          graph?.revision ?? 0,
          workspaceSlug ?? undefined,
        );
  }, [actor.userId, draftKey, initial, graph?.revision, workspaceSlug]);
  useEffect(() => {
    if (!draft?.dirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      if (draft.storageError || draft.storageConflict) event.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [draft?.dirty, draft?.storageError, draft?.storageConflict]);
  const change = (next: GraphDefinition) => {
    useGraphDraftStore.getState().edit(draftKey, next);
    validate.reset();
    publish.reset();
    setLocalError(null);
  };
  const changeScope = (next: GraphDefinition) =>
    change(replaceScope(definition, scopePath, { ...next, schemaVersion: 2 }));
  async function saveDraft() {
    if (disabled || draft?.conflict || draft?.storageConflict) return;
    try {
      const request = {
        definition,
        id: graph?.id,
        expectedRevision: draft?.baseRevision ?? graph?.revision ?? 0,
      };
      const result = await save.mutateAsync({
        ...request,
        idempotencyKey: commandKey(request),
      });
      useGraphDraftStore
        .getState()
        .saved(draftKey, definition, result.revision);
      validate.reset();
      toast.success(t(($) => $.page.saved));
      if (!graph) navigation.replace(paths.graphDetail(result.id));
    } catch (error) {
      setLocalError(error instanceof Error ? error : new Error(String(error)));
    }
  }
  async function generateProposal() {
    if (generating || disabled || generationOptions.data?.enabled !== true)
      return;
    setGenerating(true);
    setLocalError(null);
    const current =
      useGraphDraftStore.getState().documents[draftKey]?.definition ??
      definition;
    try {
      const conversation = [
        ...messages,
        { role: "user" as const, content: prompt.trim() },
      ].slice(-20);
      const result = await generate.mutateAsync({
        prompt: prompt.trim(),
        agentId: selectedGenerationAgent,
        approverId: selectedGenerationApprover,
        ...(current.name.trim() || draft?.dirty
          ? { currentDefinition: current }
          : {}),
        messages: conversation,
      });
      setMessages([
        ...conversation,
        {
          role: "assistant",
          content: result.summary ?? result.definition.name,
        },
      ]);
      setProposal({
        definition: result.definition,
        base: JSON.stringify(current),
        summary: result.summary,
      });
    } catch (error) {
      const code = errorCode(error),
        errors: Record<string, string> = {
          generation_unavailable: t(($) => $.studio.unavailable),
          generation_busy: t(($) => $.studio.error_busy),
          generation_timeout: t(($) => $.studio.error_timeout),
          generation_failed: t(($) => $.studio.error_failed),
          generation_invalid: t(($) => $.studio.error_invalid),
        };
      setLocalError(
        new Error(
          code && errors[code]
            ? errors[code]
            : error instanceof Error
              ? error.message
              : String(error),
        ),
      );
    } finally {
      setGenerating(false);
    }
  }
  async function exportDefinition(download: boolean) {
    const text = exportGraphDefinition(definition);
    setDefinitionJSON(text);
    if (download)
      downloadBlob(
        new Blob([text + "\n"], { type: "application/json" }),
        "graph-definition.json",
      );
    else if (await copyText(text)) toast.success(t(($) => $.editor.copied));
  }
  function addNode() {
    let index = scope.nodes.filter((node) => node.type === nodeType).length + 1,
      key = `${nodeType}_${index}`;
    while (scope.nodes.some((node) => node.nodeKey === key))
      key = `${nodeType}_${++index}`;
    const next = {
        ...newNode(nodeType, key),
        label: `${labels[nodeType] ?? nodeType} ${index}`,
      },
      after =
        selected?.type !== "end"
          ? selected
          : scope.nodes.find((node) => node.type === "start");
    const outgoing = scope.edges.filter((edge) => edge.from === after?.nodeKey);
    const canInsert =
      after &&
      outgoing.length === 1 &&
      !["branch", "parallel"].includes(after.type) &&
      !["branch", "parallel", "join"].includes(nodeType);
    changeScope({
      ...scope,
      nodes: [...scope.nodes, next],
      edges: canInsert
        ? [
            ...scope.edges.filter((edge) => edge !== outgoing[0]),
            { from: after.nodeKey, to: key },
            { from: key, to: outgoing[0]!.to },
          ]
        : scope.edges,
      layout: {
        ...scope.layout,
        nodes: {
          ...scope.layout?.nodes,
          [key]: {
            x: (scope.layout?.nodes[after?.nodeKey ?? ""]?.x ?? 0) + 300,
            y: (scope.layout?.nodes[after?.nodeKey ?? ""]?.y ?? 0) + 160,
          },
        },
      },
    });
    setSelectedKey(key);
    setNarrowPane("inspector");
  }
  const proposalChanges = proposal
    ? graphDefinitionChanges(definition, proposal.definition)
    : null;
  const proposalStale = proposal?.base !== JSON.stringify(definition);
  return (
    <div
      className="flex min-h-0 flex-1 flex-col"
      onKeyDown={(event) => {
        if (
          disabled ||
          !(event.metaKey || event.ctrlKey) ||
          ["INPUT", "TEXTAREA", "SELECT"].includes(
            (event.target as HTMLElement).tagName,
          )
        )
          return;
        if (event.key.toLowerCase() === "z") {
          event.preventDefault();
          useGraphDraftStore
            .getState()
            [event.shiftKey ? "redo" : "undo"](draftKey);
        }
        if (event.key.toLowerCase() === "y") {
          event.preventDefault();
          useGraphDraftStore.getState().redo(draftKey);
        }
      }}
    >
      <div className="min-h-0 flex-1 overflow-auto">
        <div className="space-y-2 px-5 pt-4">
          <GraphError
            error={
              localError ??
              save.error ??
              validate.error ??
              publish.error ??
              archive.error ??
              agents.error ??
              members.error ??
              generationOptions.error
            }
          />
          {!supported && (
            <p role="alert" className="text-body text-muted-foreground">
              {t(($) => $.production.unsupported)}
            </p>
          )}
          {draft?.recovered && (
            <p role="status" className="text-caption text-muted-foreground">
              {t(($) => $.production.draft_recovered)}
            </p>
          )}
          {draft?.storageError && (
            <p role="alert" className="text-caption text-destructive">
              {t(($) => $.production.storage_error)}
            </p>
          )}
          {(draft?.conflict || draft?.storageConflict) && (
            <div
              role="alert"
              className="space-y-3 rounded-lg border border-warning/40 bg-warning/5 p-3"
            >
              <p className="text-body">
                {draft.storageConflict
                  ? t(($) => $.production.storage_conflict)
                  : t(($) => $.production.draft_conflict)}
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => void exportDefinition(true)}
                >
                  <Download className="size-3.5" />
                  {t(($) => $.editor.download_json)}
                </Button>
                <Button
                  size="sm"
                  onClick={() =>
                    useGraphDraftStore
                      .getState()
                      .keepLocal(draftKey, graph?.revision ?? 0)
                  }
                >
                  {t(($) => $.production.keep_local)}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() =>
                    useGraphDraftStore
                      .getState()
                      .discard(draftKey, initial, graph?.revision ?? 0)
                  }
                >
                  {t(($) => $.production.discard_local)}
                </Button>
              </div>
            </div>
          )}
        </div>
        {mode === "ai" ? (
          <>
            <GraphAiComposer
              prompt={prompt}
              onPromptChange={setPrompt}
              agentId={selectedGenerationAgent}
              onAgentChange={setGenerationAgentId}
              approverId={selectedGenerationApprover}
              onApproverChange={setGenerationApproverId}
              agents={generationAgents}
              members={members.data ?? []}
              enabled={generationOptions.data?.enabled === true && !disabled}
              loading={generationOptions.isPending}
              pending={generating}
              hasDraft={Boolean(definition.name.trim())}
              onGenerate={() => void generateProposal()}
              onManual={() => setMode("editor")}
            />
            {messages.length > 0 && (
              <section className="mx-auto mb-5 max-w-5xl space-y-3 px-5">
                <h3 className="text-body font-semibold">
                  {t(($) => $.production.conversation)}
                </h3>
                {messages.map((message, index) => (
                  <p
                    key={index}
                    className={
                      message.role === "user"
                        ? "rounded-lg bg-muted p-3 text-body"
                        : "px-3 text-body text-muted-foreground"
                    }
                  >
                    {message.content}
                  </p>
                ))}
              </section>
            )}
          </>
        ) : (
          <div className="space-y-5 px-5 py-5">
            <div className="flex flex-wrap items-center gap-3">
              <GitBranch className="size-5 shrink-0 text-muted-foreground" />
              <Input
                aria-label={t(($) => $.editor.name)}
                className="min-w-48 flex-1 border-transparent bg-transparent text-title font-semibold shadow-none"
                value={definition.name}
                disabled={disabled}
                maxLength={200}
                placeholder={t(($) => $.editor.name_placeholder)}
                onChange={(event) =>
                  change({ ...definition, name: event.target.value })
                }
              />
              <Button
                type="button"
                variant="outline"
                disabled={disabled}
                onClick={() => setMode("ai")}
              >
                <Sparkles className="size-4" />
                {t(($) => $.studio.ai)}
              </Button>
            </div>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <nav className="flex min-w-0 flex-wrap items-center gap-1 text-caption">
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setScopePath([]);
                    setSelectedKey("start");
                  }}
                >
                  {t(($) => $.production.root_flow)}
                </Button>
                {scopePath.map((key, index) => (
                  <span key={key} className="flex items-center">
                    <ChevronRight className="size-3" />
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() => {
                        setScopePath(scopePath.slice(0, index + 1));
                        setSelectedKey("start");
                      }}
                    >
                      {key}
                    </Button>
                  </span>
                ))}
              </nav>
              <div className="flex flex-wrap items-center gap-1">
                <Button
                  size="icon-sm"
                  variant="outline"
                  aria-label={t(($) => $.production.undo)}
                  disabled={disabled || !draft?.past.length}
                  onClick={() => useGraphDraftStore.getState().undo(draftKey)}
                >
                  <Undo2 className="size-4" />
                </Button>
                <Button
                  size="icon-sm"
                  variant="outline"
                  aria-label={t(($) => $.production.redo)}
                  disabled={disabled || !draft?.future.length}
                  onClick={() => useGraphDraftStore.getState().redo(draftKey)}
                >
                  <Redo2 className="size-4" />
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={disabled}
                  onClick={() => changeScope(layoutGraph(scope))}
                >
                  <LayoutGrid className="size-3.5" />
                  {t(($) => $.production.layout)}
                </Button>
                <NativeSelect
                  aria-label={t(($) => $.production.add_node)}
                  value={nodeType}
                  disabled={disabled}
                  onChange={(event) => setNodeType(event.target.value)}
                >
                  {[
                    "agent",
                    "approval",
                    "branch",
                    "parallel",
                    "join",
                    "loop",
                    "subgraph",
                    "delay",
                    "http",
                    "script",
                  ].map((type) => (
                    <option key={type} value={type}>
                      {labels[type]}
                    </option>
                  ))}
                </NativeSelect>
                <Button
                  size="sm"
                  disabled={disabled || scope.nodes.length >= 128}
                  onClick={addNode}
                >
                  <Plus className="size-3.5" />
                  {t(($) => $.production.add_node)}
                </Button>
              </div>
            </div>
            <div className="flex gap-2 xl:hidden">
              <Button
                variant={narrowPane === "canvas" ? "default" : "outline"}
                size="sm"
                onClick={() => setNarrowPane("canvas")}
              >
                {t(($) => $.studio.flow)}
              </Button>
              <Button
                variant={narrowPane === "inspector" ? "default" : "outline"}
                size="sm"
                onClick={() => setNarrowPane("inspector")}
              >
                {t(($) => $.studio.step_settings)}
              </Button>
            </div>
            <div className="grid min-h-80 items-start gap-4 xl:h-[min(720px,75vh)] xl:grid-cols-[minmax(0,1fr)_360px]">
              <div
                className={
                  narrowPane === "canvas"
                    ? "h-full min-w-0"
                    : "hidden h-full min-w-0 xl:block"
                }
              >
                <GraphCanvas
                  key={scopePath.join("/")}
                  definition={scope}
                  onChange={changeScope}
                  selectedKey={selected?.nodeKey}
                  onSelect={setSelectedKey}
                  disabled={disabled}
                  onError={setLocalError}
                />
              </div>
              <div
                className={`${narrowPane === "inspector" ? "" : "hidden xl:block"} min-w-0 space-y-4 xl:max-h-full xl:overflow-y-auto`}
              >
                {selected && (
                  <GraphNodeInspector
                    key={`${scopePath.join("/")}:${selected.nodeKey}`}
                    definition={scope}
                    node={selected}
                    onChange={changeScope}
                    onSelect={setSelectedKey}
                    onEnter={(key) => {
                      setScopePath([...scopePath, key]);
                      setSelectedKey("start");
                    }}
                    onError={setLocalError}
                    agents={agents.data ?? []}
                    members={members.data ?? []}
                    disabled={disabled}
                    inLoop={inLoop}
                  />
                )}
              </div>
            </div>
            <div className="grid gap-4 lg:grid-cols-2">
              <details className="rounded-xl border p-4">
                <summary className="cursor-pointer text-body font-medium">
                  {t(($) => $.editor.input_schema)}
                </summary>
                <div className="mt-4">
                  <GraphSchemaBuilder
                    schema={scope.inputSchema}
                    onChange={(inputSchema) =>
                      changeScope({ ...scope, inputSchema })
                    }
                    disabled={disabled}
                  />
                </div>
              </details>
              <details className="rounded-xl border p-4">
                <summary className="cursor-pointer text-body font-medium">
                  {t(($) => $.production.output)}
                </summary>
                <div className="mt-4 space-y-4">
                  <GraphSchemaBuilder
                    schema={scope.outputSchema ?? objectSchema}
                    onChange={(outputSchema) =>
                      changeScope({ ...scope, outputSchema })
                    }
                    disabled={disabled}
                  />
                  <GraphBindings
                    definition={scope}
                    values={scope.outputs ?? {}}
                    onChange={(outputs) => changeScope({ ...scope, outputs })}
                    disabled={disabled}
                    inLoop={inLoop}
                  />
                </div>
              </details>
            </div>
            {!scopePath.length && (
              <Field label={t(($) => $.production.max_concurrency)}>
                {(id) => (
                  <Input
                    id={id}
                    className="max-w-40"
                    type="number"
                    min={1}
                    max={32}
                    disabled={disabled}
                    value={definition.maxConcurrency ?? 4}
                    onChange={(event) =>
                      change({
                        ...definition,
                        schemaVersion: 2,
                        maxConcurrency: Number(event.target.value),
                      })
                    }
                  />
                )}
              </Field>
            )}
            <details className="rounded-xl border p-4">
              <summary className="cursor-pointer text-caption font-medium text-muted-foreground">
                {t(($) => $.editor.import_export)}
              </summary>
              <div className="mt-4 space-y-3">
                <Field label={t(($) => $.editor.definition_json)}>
                  {(id) => (
                    <Textarea
                      id={id}
                      value={definitionJSON}
                      onChange={(event) =>
                        setDefinitionJSON(event.target.value)
                      }
                      className="min-h-40 font-mono text-caption"
                    />
                  )}
                </Field>
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={disabled || !definitionJSON.trim()}
                    onClick={() => {
                      try {
                        change(importGraphDefinition(definitionJSON));
                        setScopePath([]);
                        setSelectedKey("start");
                      } catch (error) {
                        setLocalError(error as Error);
                      }
                    }}
                  >
                    {t(($) => $.editor.import_json)}
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => void exportDefinition(false)}
                  >
                    {t(($) => $.editor.copy_json)}
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => void exportDefinition(true)}
                  >
                    {t(($) => $.editor.download_json)}
                  </Button>
                </div>
              </div>
            </details>
            {graph && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={
                  pending ||
                  draft?.dirty ||
                  !canEdit ||
                  Boolean(graph.archivedAt)
                }
                onClick={() => {
                  const request = {
                    id: graph.id,
                    expectedRevision: draft?.baseRevision ?? graph.revision,
                  };
                  archive.mutate({
                    ...request,
                    idempotencyKey: commandKey({
                      ...request,
                      action: "archive",
                    }),
                  });
                }}
              >
                {t(($) => $.editor.archive)}
              </Button>
            )}
          </div>
        )}
        {proposal && (
          <section className="mx-5 mb-6 space-y-4 rounded-2xl border border-primary/30 bg-primary/[0.025] p-5">
            <div className="flex items-center gap-2">
              <Sparkles className="size-4" />
              <h2 className="text-title font-semibold">
                {t(($) => $.production.proposal)}
              </h2>
            </div>
            {proposal.summary && (
              <p className="text-body">{proposal.summary}</p>
            )}
            <h3 className="text-body font-medium">
              {proposal.definition.name}
            </h3>
            <div className="grid gap-3 sm:grid-cols-3">
              {[
                [t(($) => $.production.added), proposalChanges!.added],
                [t(($) => $.production.changed), proposalChanges!.changed],
                [t(($) => $.production.removed), proposalChanges!.removed],
              ].map(([label, keys]) => (
                <div
                  key={String(label)}
                  className="rounded-lg bg-background p-3"
                >
                  <p className="text-caption text-muted-foreground">{label}</p>
                  <p className="mt-1 break-words text-body">
                    {(keys as string[]).join(", ") || "—"}
                  </p>
                </div>
              ))}
            </div>
            <div className="flex flex-wrap gap-2 text-caption text-muted-foreground">
              {proposalChanges?.renamed && (
                <span>{t(($) => $.production.name_changed)}</span>
              )}
              {proposalChanges?.connections && (
                <span>{t(($) => $.production.connections_changed)}</span>
              )}
              {proposalChanges?.inputs && (
                <span>{t(($) => $.production.inputs_changed)}</span>
              )}
            </div>
            <details
              className="rounded-xl border bg-background p-4"
              onToggle={(event) => setShowComparison(event.currentTarget.open)}
            >
              <summary className="cursor-pointer text-caption font-medium">
                {t(($) => $.production.compare_changes)}
              </summary>
              {showComparison && (
                <div className="mt-4 grid gap-4 sm:grid-cols-2">
                  <section className="min-w-0 space-y-3">
                    <h4 className="text-body font-medium">
                      {t(($) => $.production.before)}
                    </h4>
                    <GraphValueView value={definition} />
                  </section>
                  <section className="min-w-0 space-y-3">
                    <h4 className="text-body font-medium">
                      {t(($) => $.production.after)}
                    </h4>
                    <GraphValueView value={proposal.definition} />
                  </section>
                </div>
              )}
            </details>
            <GraphCanvas definition={proposal.definition} disabled />
            {proposalStale && (
              <p role="alert" className="text-caption text-destructive">
                {t(($) => $.production.proposal_stale)}
              </p>
            )}
            <div className="flex flex-wrap gap-2">
              <Button
                disabled={disabled || proposalStale}
                onClick={() => {
                  change(proposal.definition);
                  setProposal(null);
                  setMode("editor");
                  setScopePath([]);
                  setSelectedKey("start");
                  setPrompt("");
                }}
              >
                {t(($) => $.production.apply_proposal)}
              </Button>
              <Button variant="outline" onClick={() => setProposal(null)}>
                {t(($) => $.production.discard_proposal)}
              </Button>
            </div>
          </section>
        )}
      </div>
      {mode === "editor" && (
        <footer className="pe-chat-launcher shrink-0 space-y-3 border-t bg-background/95 px-5 py-3 backdrop-blur">
          {validate.data && (
            <div role="status" className="text-caption">
              {validate.data.valid === true ? (
                <span className="flex items-center gap-2 text-success">
                  <Check className="size-3.5" />
                  {t(($) => $.editor.valid)}
                </span>
              ) : (
                validate.data.diagnostics.map((diagnostic, index) => (
                  <button
                    key={index}
                    type="button"
                    className="block text-left text-destructive underline-offset-4 hover:underline"
                    onClick={() => {
                      if (diagnostic.nodeKey) {
                        setScopePath([]);
                        setSelectedKey(diagnostic.nodeKey);
                      }
                    }}
                  >
                    {[diagnostic.nodeKey, diagnostic.path, diagnostic.message]
                      .filter(Boolean)
                      .join(": ")}
                  </button>
                ))
              )}
            </div>
          )}
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-caption text-muted-foreground">
              {draft?.dirty
                ? draft.storageError || draft.storageConflict
                  ? t(($) => $.studio.unsaved)
                  : t(($) => $.production.local_saved)
                : t(($) => $.studio.saved)}
            </p>
            <div className="flex flex-wrap items-center gap-2">
              {draft?.dirty && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() =>
                    useGraphDraftStore
                      .getState()
                      .discard(draftKey, initial, graph?.revision ?? 0)
                  }
                >
                  {t(($) => $.production.discard_local)}
                </Button>
              )}
              {graph && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={pending || draft?.dirty}
                  onClick={() => validate.mutate(graph.id)}
                >
                  {t(($) => $.editor.validate)}
                </Button>
              )}
              <Button
                type="button"
                variant={graph ? "outline" : "default"}
                disabled={
                  disabled ||
                  draft?.conflict ||
                  draft?.storageConflict ||
                  Boolean(graph && !draft?.dirty)
                }
                onClick={() => void saveDraft()}
              >
                {save.isPending && <Loader2 className="size-4 animate-spin" />}
                {t(($) => $.editor.save)}
              </Button>
              {graph && (
                <Button
                  type="button"
                  disabled={
                    pending ||
                    draft?.dirty ||
                    !admin ||
                    Boolean(graph.archivedAt) ||
                    validate.data?.valid !== true
                  }
                  onClick={() => {
                    const request = {
                      id: graph.id,
                      expectedRevision: draft?.baseRevision ?? graph.revision,
                    };
                    publish.mutate(
                      { ...request, idempotencyKey: commandKey(request) },
                      {
                        onSuccess: () =>
                          toast.success(t(($) => $.page.published)),
                      },
                    );
                  }}
                >
                  {t(($) => $.editor.publish)}
                </Button>
              )}
            </div>
          </div>
        </footer>
      )}
    </div>
  );
}
