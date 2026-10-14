import { useEffect, useState } from "react";
import { Alert, View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type {
  GraphDefinition,
  GraphNodeDefinition,
} from "@multica/core/graphs/types";
import { graphDefinitionChanges } from "@multica/core/graphs/definition";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { agentListOptions } from "@/data/queries/agents";
import {
  graphDetailOptions,
  graphGenerationOptions,
} from "@/data/queries/graphs";
import {
  useGenerateGraph,
  useSaveGraph,
  useValidateGraph,
  usePublishGraph,
  useArchiveGraph,
} from "@/data/mutations/graphs";
import {
  graphDraftIdentity,
  useGraphDraftStore,
} from "@/data/stores/graph-draft-store";
import {
  GraphScreen,
  GraphSection,
  GraphError,
  GraphPick,
  GraphText,
  GraphWorkflowTree,
  GraphDisclosure,
  useGraphContext,
  useGraphCommandKey,
  NODE_LABELS,
} from "./shared";
import { GraphSchemaFields, GraphBindingsFields } from "./fields";

const object = { type: "object", additionalProperties: true };
export function emptyGraph(name = ""): GraphDefinition {
  return {
    schemaVersion: 2,
    name,
    inputSchema: object,
    nodes: [
      { nodeKey: "start", label: "Start", type: "start" },
      { nodeKey: "end", label: "Finish", type: "end" },
    ],
    edges: [{ from: "start", to: "end" }],
    maxConcurrency: 4,
  };
}
export function createGraphNode(
  type: string,
  key: string,
): GraphNodeDefinition {
  const base = { nodeKey: key, label: NODE_LABELS[type] ?? type, type };
  switch (type) {
    case "agent":
      return {
        ...base,
        agentId: "",
        instructions: "",
        inputs: { context: { source: "run_input", path: "" } },
        outputSchema: object,
        executionMode: "none",
        maxAttempts: 2,
        timeoutSeconds: 3600,
      };
    case "approval":
      return { ...base, approval: { mode: "all", approverIds: [] } };
    case "branch":
      return {
        ...base,
        condition: { op: "exists", left: { source: "run_input", path: "" } },
      };
    case "delay":
      return { ...base, delay: { seconds: 60 } };
    case "script":
      return {
        ...base,
        agentId: "",
        inputs: {},
        outputSchema: object,
        script: { language: "javascript", code: "", timeoutSeconds: 60 },
      };
    case "http":
      return {
        ...base,
        outputSchema: object,
        http: {
          method: "GET",
          url: "",
          timeoutSeconds: 30,
          maxResponseBytes: 65536,
        },
      };
    case "loop":
      return {
        ...base,
        inputs: {},
        outputSchema: object,
        loop: {
          body: emptyGraph("Loop body"),
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
        outputSchema: object,
        subgraph: { definition: emptyGraph("Subflow") },
      };
    default:
      return base;
  }
}
export function GraphEditorScreen({ id }: { id?: string }) {
  const { wsId, slug, userId, members, actor, admin } = useGraphContext();
  const graph = useQuery(graphDetailOptions(wsId, id ?? "")),
    agents = useQuery(agentListOptions(wsId)),
    generation = useQuery(graphGenerationOptions(wsId));
  const save = useSaveGraph(wsId),
    generate = useGenerateGraph(wsId),
    validate = useValidateGraph(),
    publish = usePublishGraph(wsId),
    archive = useArchiveGraph(wsId);
  const commandKey = useGraphCommandKey(),
    key = graphDraftIdentity(wsId, userId, id),
    doc = useGraphDraftStore((s) => s.documents[key]);
  const [ready, setReady] = useState(false),
    [prompt, setPrompt] = useState(""),
    [error, setError] = useState<unknown>(null);
  useEffect(() => {
    if (!wsId || !userId || (id && !graph.data)) return;
    useGraphDraftStore
      .getState()
      .open(
        key,
        graph.data?.definition ?? emptyGraph(),
        graph.data?.revision ?? 0,
      );
    setReady(false);
    void useGraphDraftStore
      .getState()
      .recover(key)
      .finally(() => setReady(true));
  }, [key, wsId, userId, id, graph.data]);
  if (id && !graph.isLoading && !graph.data && !graph.error)
    return (
      <GraphScreen>
        <Text>This workflow is unavailable.</Text>
        <Button variant="outline" onPress={() => void graph.refetch()}>
          <Text>Try again</Text>
        </Button>
      </GraphScreen>
    );
  const canEdit =
      !!userId && (!id || admin || graph.data?.createdBy === userId),
    pending =
      save.isPending ||
      generate.isPending ||
      publish.isPending ||
      archive.isPending;
  if (!doc || !ready)
    return (
      <GraphScreen loading={graph.isLoading || !ready}>
        <GraphError error={graph.error} retry={() => void graph.refetch()} />
      </GraphScreen>
    );
  const disabled =
    !canEdit ||
    pending ||
    !!graph.data?.archivedAt ||
    ![1, 2].includes(doc.definition.schemaVersion);
  const agentId = String(
      doc.values.generationAgent ??
        (agents.data?.length === 1 ? agents.data[0]!.id : ""),
    ),
    approverId = String(doc.values.generationApprover ?? actor.memberId ?? "");
  const newType = String(doc.values.nodeType ?? "agent");
  const run = async (action: () => Promise<unknown>) => {
    setError(null);
    try {
      await action();
    } catch (e) {
      setError(e);
    }
  };
  const changes = doc.proposal
    ? graphDefinitionChanges(doc.definition, doc.proposal.definition)
    : undefined;
  return (
    <GraphScreen>
      <GraphError
        error={
          error ??
          graph.error ??
          agents.error ??
          members.error ??
          doc.storageError
        }
      />
      {graph.data?.archivedAt && <Text>Archived workflow</Text>}
      {doc.conflict && (
        <GraphSection title="This workflow changed elsewhere">
          <Text>
            Your edits are preserved. Reload the latest version before saving to
            avoid overwriting other collaborators’ changes.
          </Text>
          <Button
            variant="outline"
            onPress={() =>
              Alert.alert(
                "Discard local changes?",
                "The latest saved version will replace this draft.",
                [
                  { text: "Keep editing", style: "cancel" },
                  {
                    text: "Reload saved version",
                    style: "destructive",
                    onPress: () => {
                      if (graph.data)
                        useGraphDraftStore
                          .getState()
                          .reset(
                            key,
                            graph.data.definition,
                            graph.data.revision,
                          );
                    },
                  },
                ],
              )
            }
          >
            <Text>Reload saved version</Text>
          </Button>
        </GraphSection>
      )}
      <GraphText
        label="Workflow name"
        value={doc.definition.name}
        onChange={(name) =>
          useGraphDraftStore.getState().edit(key, { ...doc.definition, name })
        }
        disabled={disabled}
      />
      <GraphSection title="Create or refine with AI">
        <GraphText
          label="What should this workflow do?"
          value={prompt}
          onChange={setPrompt}
          multiline
          disabled={disabled}
        />
        <GraphPick
          documentKey={key}
          path={["values", "generationAgent"]}
          label="Workflow agent"
          value={agentId}
          options={(agents.data ?? [])
            .filter((a) => !a.archived_at)
            .map((a) => ({ value: a.id, label: a.name }))}
          disabled={disabled}
        />
        <GraphPick
          documentKey={key}
          path={["values", "generationApprover"]}
          label="Default reviewer"
          value={approverId}
          options={(members.data ?? []).map((m) => ({
            value: m.id,
            label: m.name || m.email,
          }))}
          disabled={disabled}
        />
        <Button
          disabled={
            disabled ||
            !generation.data?.enabled ||
            !prompt.trim() ||
            !agentId ||
            !approverId
          }
          onPress={() =>
            void run(async () => {
              const base = doc.definition,
                messages = [
                  ...doc.messages,
                  { role: "user" as const, content: prompt.trim() },
                ].slice(-20);
              const result = await generate.mutateAsync({
                prompt: prompt.trim(),
                agentId,
                approverId,
                ...(base.name.trim() || doc.dirty
                  ? { currentDefinition: base }
                  : {}),
                messages,
              });
              useGraphDraftStore
                .getState()
                .propose(key, result.definition, base, result.summary);
              useGraphDraftStore
                .getState()
                .field(
                  key,
                  ["messages"],
                  [
                    ...messages,
                    {
                      role: "assistant",
                      content: result.summary ?? result.definition.name,
                    },
                  ],
                );
            })
          }
        >
          <Text>
            {generate.isPending ? "Generating…" : "Generate proposal"}
          </Text>
        </Button>
        {generation.data?.enabled === false && (
          <Text className="text-muted-foreground">
            AI generation is unavailable on this server. Manual editing is
            available below.
          </Text>
        )}
        <GraphError
          error={generation.error}
          retry={() => void generation.refetch()}
        />
      </GraphSection>
      {doc.proposal && (
        <GraphSection title="Review proposed changes">
          <Text>{doc.proposal.summary ?? doc.proposal.definition.name}</Text>
          <Text>
            {changes?.added.length} added · {changes?.changed.length} changed ·{" "}
            {changes?.removed.length} removed
          </Text>
          <GraphWorkflowTree definition={doc.proposal.definition} />
          {JSON.stringify(doc.definition) !== doc.proposal.base && (
            <Text className="text-destructive">
              Your draft changed after this proposal. Generate again to preserve
              your edits.
            </Text>
          )}
          <Button
            disabled={
              disabled || JSON.stringify(doc.definition) !== doc.proposal.base
            }
            onPress={() => {
              try {
                useGraphDraftStore.getState().apply(key);
                setPrompt("");
              } catch (e) {
                setError(e);
              }
            }}
          >
            <Text>Apply changes</Text>
          </Button>
          <Button
            variant="outline"
            onPress={() => useGraphDraftStore.getState().discardProposal(key)}
          >
            <Text>Discard proposal</Text>
          </Button>
        </GraphSection>
      )}
      <View className="flex-row gap-3">
        <Button
          variant="outline"
          disabled={disabled || !doc.undo.length}
          onPress={() => useGraphDraftStore.getState().undo(key)}
        >
          <Text>Undo</Text>
        </Button>
        <Button
          variant="outline"
          disabled={disabled || !doc.redo.length}
          onPress={() => useGraphDraftStore.getState().redo(key)}
        >
          <Text>Redo</Text>
        </Button>
      </View>
      <GraphSection title="Workflow steps">
        <GraphWorkflowTree
          definition={doc.definition}
          onNode={(path) =>
            router.push({
              pathname: `/${slug}/graph/node`,
              params: {
                document: key,
                path: JSON.stringify(["definition", ...path]),
                readonly: disabled ? "1" : "0",
              },
            })
          }
        />
        <GraphPick
          documentKey={key}
          path={["values", "nodeType"]}
          label="New step type"
          value={newType}
          options={Object.entries(NODE_LABELS)
            .filter(([value]) => !["start", "end"].includes(value))
            .map(([value, label]) => ({ value, label }))}
          disabled={disabled}
        />
        <Button
          variant="outline"
          disabled={disabled || doc.definition.nodes.length >= 128}
          onPress={() => {
            let n = 1;
            while (
              doc.definition.nodes.some(
                (item) => item.nodeKey === `${newType}_${n}`,
              )
            )
              n++;
            const node = createGraphNode(newType, `${newType}_${n}`),
              nodes = [...doc.definition.nodes, node];
            useGraphDraftStore
              .getState()
              .edit(key, { ...doc.definition, nodes });
            router.push({
              pathname: `/${slug}/graph/node`,
              params: {
                document: key,
                path: JSON.stringify(["definition", "nodes", nodes.length - 1]),
              },
            });
          }}
        >
          <Text>Add step</Text>
        </Button>
      </GraphSection>
      <GraphDisclosure title="Workflow inputs">
        <GraphSchemaFields
          documentKey={key}
          path={["definition", "inputSchema"]}
          schema={doc.definition.inputSchema}
          disabled={disabled}
        />
      </GraphDisclosure>
      <GraphDisclosure title="Workflow outputs">
        <GraphBindingsFields
          documentKey={key}
          path={["definition", "outputs"]}
          bindings={doc.definition.outputs ?? {}}
          definition={doc.definition}
          disabled={disabled}
        />
        <GraphSchemaFields
          documentKey={key}
          path={["definition", "outputSchema"]}
          schema={doc.definition.outputSchema ?? object}
          disabled={disabled}
        />
      </GraphDisclosure>
      <GraphText
        label="Maximum concurrent steps (1–32)"
        value={String(doc.definition.maxConcurrency ?? 4)}
        onChange={(v) =>
          useGraphDraftStore
            .getState()
            .edit(key, { ...doc.definition, maxConcurrency: Number(v) })
        }
        disabled={disabled}
      />
      <Text className="text-sm text-muted-foreground">
        {doc.dirty
          ? "Draft is stored securely on this device."
          : "All changes saved."}
      </Text>
      <Button
        disabled={
          disabled ||
          doc.conflict ||
          !doc.definition.name.trim() ||
          (!!id && !doc.dirty)
        }
        onPress={() =>
          void run(async () => {
            const submitted = doc.definition;
            const result = await save.mutateAsync({
              id,
              definition: submitted,
              expectedRevision: doc.revision,
              idempotencyKey: commandKey({
                id,
                definition: doc.definition,
                revision: doc.revision,
              }),
            });
            useGraphDraftStore
              .getState()
              .saved(key, submitted, result.revision);
            validate.reset();
            if (!id) {
              const store = useGraphDraftStore.getState(),
                latest = store.documents[key],
                nextKey = graphDraftIdentity(wsId, userId, result.id);
              store.open(nextKey, submitted, result.revision);
              if (latest?.dirty) store.edit(nextKey, latest.definition);
              store.reset(key, submitted, result.revision);
              router.replace(`/${slug}/graph/${result.id}`);
            }
          })
        }
      >
        <Text>Save draft</Text>
      </Button>
      {id && (
        <>
          <Button
            variant="outline"
            disabled={pending || doc.dirty}
            onPress={() => validate.mutate(id)}
          >
            <Text>Validate workflow</Text>
          </Button>
          {validate.data && (
            <GraphSection
              title={
                validate.data.valid
                  ? "Workflow is valid"
                  : "Validation needs attention"
              }
            >
              {validate.data.diagnostics.map((d, i) => (
                <Text key={i} className="text-destructive">
                  {d.message}
                </Text>
              ))}
            </GraphSection>
          )}
          <GraphError error={validate.error ?? publish.error} />
          <Button
            disabled={
              disabled || !admin || doc.dirty || validate.data?.valid !== true
            }
            onPress={() =>
              void run(() =>
                publish.mutateAsync({
                  id,
                  expectedRevision: doc.revision,
                  idempotencyKey: commandKey({
                    id,
                    revision: doc.revision,
                    action: "publish",
                  }),
                }),
              )
            }
          >
            <Text>Publish version</Text>
          </Button>
          <Button
            variant="outline"
            disabled={!graph.data?.versions.length || !!graph.data?.archivedAt}
            onPress={() => router.push(`/${slug}/graph/${id}/start`)}
          >
            <Text>Start a run</Text>
          </Button>
          <Button
            variant="outline"
            onPress={() =>
              router.push({
                pathname: `/${slug}/graph/runs`,
                params: { graph: id },
              })
            }
          >
            <Text>Run history</Text>
          </Button>
          <Button
            variant="outline"
            onPress={() => router.push(`/${slug}/graph/${id}/triggers`)}
          >
            <Text>Triggers</Text>
          </Button>
          {admin && !graph.data?.archivedAt && (
            <Button
              variant="ghost"
              disabled={pending}
              onPress={() =>
                Alert.alert(
                  "Archive this workflow?",
                  "Published versions and run history remain available.",
                  [
                    { text: "Cancel", style: "cancel" },
                    {
                      text: "Archive",
                      style: "destructive",
                      onPress: () =>
                        void run(() =>
                          archive.mutateAsync({
                            id,
                            expectedRevision: doc.revision,
                            idempotencyKey: commandKey({
                              id,
                              revision: doc.revision,
                              action: "archive",
                            }),
                          }),
                        ),
                    },
                  ],
                )
              }
            >
              <Text>Archive workflow</Text>
            </Button>
          )}
        </>
      )}
    </GraphScreen>
  );
}
