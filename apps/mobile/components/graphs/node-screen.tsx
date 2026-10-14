import { useState } from "react";
import { Alert, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type {
  GraphDefinition,
  GraphNodeDefinition,
} from "@multica/core/graphs/types";
import {
  connectGraphNodes,
  renameGraphNode,
} from "@multica/core/graphs/definition";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { agentListOptions } from "@/data/queries/agents";
import { useGraphDraftStore } from "@/data/stores/graph-draft-store";
import {
  GraphScreen,
  GraphSection,
  GraphText,
  GraphPick,
  GraphError,
  GraphDisclosure,
  GraphWorkflowTree,
  NODE_LABELS,
  useGraphContext,
} from "./shared";
import {
  graphPathValue,
  GraphSchemaFields,
  GraphBindingsFields,
  GraphConditionFields,
  GraphBindingFields,
} from "./fields";
import { createGraphNode } from "./editor-screen";
export function GraphNodeScreen() {
  const {
    document: key,
    path: rawPath,
    readonly,
  } = useLocalSearchParams<{
    document: string;
    path: string;
    readonly?: string;
  }>();
  const context = useGraphContext(),
    agents = useQuery(agentListOptions(context.wsId)),
    doc = useGraphDraftStore((s) => s.documents[key]);
  const [error, setError] = useState<unknown>(null),
    [identifier, setIdentifier] = useState("");
  let path: (string | number)[] = [];
  try {
    const parsed: unknown = JSON.parse(rawPath);
    if (
      Array.isArray(parsed) &&
      parsed.every((p) => typeof p === "string" || typeof p === "number")
    )
      path = parsed;
  } catch {}
  const node = graphPathValue(doc, path) as GraphNodeDefinition | undefined,
    scopePath = path.slice(0, -2),
    definition = graphPathValue(doc, scopePath) as GraphDefinition | undefined;
  if (!doc || !node?.nodeKey || !definition?.nodes)
    return (
      <GraphScreen>
        <Text>This step is no longer available. Open the workflow again.</Text>
      </GraphScreen>
    );
  const disabled = readonly === "1",
    field = (suffix: (string | number)[], value: unknown) =>
      useGraphDraftStore.getState().field(key, [...path, ...suffix], value);
  const prefix = path.join("."),
    approvers =
      node.approval?.approverIds ?? (node.approverId ? [node.approverId] : []),
    approval = node.approval ?? { mode: "all", approverIds: approvers };
  const pick = (name: string) => String(doc.values[prefix + "." + name] ?? ""),
    vp = (name: string) => ["values", prefix + "." + name];
  const number = (
    label: string,
    value: number,
    suffix: (string | number)[],
  ) => (
    <GraphText
      label={label}
      value={String(value)}
      onChange={(v) => field(suffix, Number(v))}
      disabled={disabled}
    />
  );
  return (
    <GraphScreen>
      <Text className="text-xl font-semibold">
        {node.label || NODE_LABELS[node.type] || node.nodeKey}
      </Text>
      <GraphError error={error ?? agents.error ?? context.members.error} />
      <GraphText
        label="Step name"
        value={node.label ?? ""}
        onChange={(v) => field(["label"], v)}
        disabled={disabled}
      />
      {["agent", "script"].includes(node.type) && (
        <GraphPick
          documentKey={key}
          path={[...path, "agentId"]}
          label="Agent"
          value={node.agentId ?? ""}
          options={(agents.data ?? [])
            .filter((a) => !a.archived_at || a.id === node.agentId)
            .map((a) => ({ value: a.id, label: a.name }))}
          disabled={disabled}
        />
      )}
      {node.type === "agent" && (
        <>
          <GraphText
            label="Instructions and required evidence"
            multiline
            value={node.instructions ?? ""}
            onChange={(v) => field(["instructions"], v)}
            disabled={disabled}
          />
          <GraphPick
            documentKey={key}
            path={[...path, "executionMode"]}
            label="Code delivery"
            value={node.executionMode ?? "none"}
            options={[
              { value: "none", label: "No code artifact" },
              { value: "produce_commit", label: "Produce an isolated commit" },
              { value: "verify_commit", label: "Verify a prior commit" },
            ]}
            disabled={disabled}
          />
          {number("Maximum attempts (1–3)", node.maxAttempts ?? 2, [
            "maxAttempts",
          ])}
          {number("Timeout in seconds", node.timeoutSeconds ?? 3600, [
            "timeoutSeconds",
          ])}
        </>
      )}
      {node.type === "approval" && (
        <GraphSection title="Review policy">
          <GraphPick
            documentKey={key}
            path={[...path, "approval", "mode"]}
            label="Approval rule"
            value={approval.mode}
            options={[
              { value: "all", label: "All reviewers" },
              { value: "any", label: "Any reviewer" },
              { value: "quorum", label: "Required number of reviewers" },
            ]}
            disabled={disabled}
          />
          {approval.mode === "quorum" &&
            number("Required approvals", approval.quorum ?? 1, [
              "approval",
              "quorum",
            ])}
          {approvers.map((id) => (
            <View className="flex-row items-center justify-between" key={id}>
              <Text>
                {context.members.data?.find((m) => m.id === id)?.name ?? id}
              </Text>
              <Button
                variant="ghost"
                disabled={disabled}
                onPress={() => {
                  field(["approval"], {
                    ...approval,
                    approverIds: approvers.filter((a) => a !== id),
                  });
                  field(["approverId"], undefined);
                }}
              >
                <Text>Remove</Text>
              </Button>
            </View>
          ))}
          <GraphPick
            documentKey={key}
            path={vp("reviewer")}
            label="Add reviewer"
            value={pick("reviewer")}
            options={(context.members.data ?? [])
              .filter((m) => !approvers.includes(m.id))
              .map((m) => ({ value: m.id, label: m.name || m.email }))}
            disabled={disabled}
          />
          <Button
            variant="outline"
            disabled={
              disabled ||
              !pick("reviewer") ||
              approvers.includes(pick("reviewer"))
            }
            onPress={() => {
              field(["approval"], {
                ...approval,
                approverIds: [...approvers, pick("reviewer")],
              });
              field(["approverId"], undefined);
            }}
          >
            <Text>Add reviewer</Text>
          </Button>
        </GraphSection>
      )}
      {node.type === "branch" && (
        <GraphConditionFields
          documentKey={key}
          path={[...path, "condition"]}
          condition={node.condition ?? { op: "exists" }}
          definition={definition}
          disabled={disabled}
        />
      )}
      {node.delay &&
        number("Wait in seconds (1–86400)", node.delay.seconds, [
          "delay",
          "seconds",
        ])}
      {node.script && (
        <>
          <GraphPick
            documentKey={key}
            path={[...path, "script", "language"]}
            label="Language"
            value={node.script.language}
            options={[
              { value: "javascript", label: "JavaScript" },
              { value: "python", label: "Python" },
            ]}
            disabled={disabled}
          />
          <Text className="text-sm text-muted-foreground">
            JavaScript: read input and return a JSON value. Python: read input
            and assign output. Write logs to stderr.
          </Text>
          <GraphText
            label="Script"
            multiline
            value={node.script.code}
            onChange={(v) => field(["script", "code"], v)}
            disabled={disabled}
          />
          {number("Timeout in seconds (1–3600)", node.script.timeoutSeconds, [
            "script",
            "timeoutSeconds",
          ])}
        </>
      )}
      {node.http && (
        <>
          <GraphPick
            documentKey={key}
            path={[...path, "http", "method"]}
            label="HTTP method"
            value={node.http.method}
            options={["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"].map(
              (v) => ({ value: v, label: v }),
            )}
            disabled={disabled}
          />
          <GraphText
            label="HTTPS URL"
            value={node.http.url}
            onChange={(v) => field(["http", "url"], v)}
            disabled={disabled}
          />
          {number("Timeout in seconds (1–300)", node.http.timeoutSeconds, [
            "http",
            "timeoutSeconds",
          ])}
          {number("Maximum response bytes", node.http.maxResponseBytes, [
            "http",
            "maxResponseBytes",
          ])}
          <GraphDisclosure title="Request headers">
            <GraphBindingsFields
              documentKey={key}
              path={[...path, "http", "headers"]}
              bindings={node.http.headers ?? {}}
              definition={definition}
              disabled={disabled}
            />
          </GraphDisclosure>
          {!["GET", "HEAD"].includes(node.http.method) && (
            <GraphBindingFields
              documentKey={key}
              path={[...path, "http", "body"]}
              binding={node.http.body ?? { source: "run_input", path: "" }}
              definition={definition}
              disabled={disabled}
            />
          )}
        </>
      )}
      {node.loop && (
        <>
          {number("Maximum iterations (1–100)", node.loop.maxIterations, [
            "loop",
            "maxIterations",
          ])}
          <GraphSection title="Continue after each iteration">
            <GraphConditionFields
              documentKey={key}
              path={[...path, "loop", "condition"]}
              condition={node.loop.condition}
              definition={node.loop.body}
              disabled={disabled}
            />
          </GraphSection>
          <GraphSection title="Next iteration input">
            <GraphBindingsFields
              documentKey={key}
              path={[...path, "loop", "next"]}
              bindings={node.loop.next}
              definition={node.loop.body}
              disabled={disabled}
            />
          </GraphSection>
        </>
      )}
      {(node.loop || node.subgraph) && (
        <GraphSection title="Internal workflow">
          <GraphWorkflowTree
            definition={node.loop?.body ?? node.subgraph!.definition}
            onNode={(child) =>
              router.push({
                pathname: `/${context.slug}/graph/node`,
                params: {
                  document: key,
                  path: JSON.stringify([
                    ...path,
                    node.loop ? "loop" : "subgraph",
                    node.loop ? "body" : "definition",
                    ...child,
                  ]),
                  readonly,
                },
              })
            }
          />
          <GraphPick
            documentKey={key}
            path={vp("type")}
            label="New internal step"
            value={pick("type") || "agent"}
            options={Object.entries(NODE_LABELS)
              .filter(([v]) => !["start", "end"].includes(v))
              .map(([value, label]) => ({ value, label }))}
            disabled={disabled}
          />
          <Button
            variant="outline"
            disabled={disabled}
            onPress={() => {
              const body = node.loop?.body ?? node.subgraph!.definition,
                type = pick("type") || "agent";
              let index = 1;
              while (body.nodes.some((n) => n.nodeKey === `${type}_${index}`))
                index++;
              const next = createGraphNode(type, `${type}_${index}`);
              field(
                [
                  node.loop ? "loop" : "subgraph",
                  node.loop ? "body" : "definition",
                ],
                { ...body, nodes: [...body.nodes, next] },
              );
            }}
          >
            <Text>Add internal step</Text>
          </Button>
          <GraphSchemaFields
            documentKey={key}
            path={[
              ...path,
              node.loop ? "loop" : "subgraph",
              node.loop ? "body" : "definition",
              "inputSchema",
            ]}
            schema={(node.loop?.body ?? node.subgraph!.definition).inputSchema}
            disabled={disabled}
          />
          <GraphBindingsFields
            documentKey={key}
            path={[
              ...path,
              node.loop ? "loop" : "subgraph",
              node.loop ? "body" : "definition",
              "outputs",
            ]}
            bindings={
              (node.loop?.body ?? node.subgraph!.definition).outputs ?? {}
            }
            definition={node.loop?.body ?? node.subgraph!.definition}
            disabled={disabled}
          />
        </GraphSection>
      )}
      {["agent", "script", "loop", "subgraph"].includes(node.type) && (
        <GraphDisclosure title="Step inputs">
          <GraphBindingsFields
            documentKey={key}
            path={[...path, "inputs"]}
            bindings={node.inputs ?? {}}
            definition={definition}
            disabled={disabled}
          />
        </GraphDisclosure>
      )}
      {["agent", "script", "loop", "subgraph", "http"].includes(node.type) && (
        <GraphDisclosure title="Output fields">
          <GraphSchemaFields
            documentKey={key}
            path={[...path, "outputSchema"]}
            schema={node.outputSchema ?? { type: "object" }}
            disabled={disabled}
          />
        </GraphDisclosure>
      )}
      {node.type !== "end" && (
        <GraphSection title="Next steps">
          {definition.edges
            .filter((e) => e.from === node.nodeKey)
            .map((e) => (
              <View className="gap-2" key={e.to + e.port}>
                <Text>
                  {e.port
                    ? e.port === "true"
                      ? "If true → "
                      : "If false → "
                    : "→ "}
                  {definition.nodes.find((n) => n.nodeKey === e.to)?.label ||
                    e.to}
                </Text>
                <Button
                  variant="ghost"
                  disabled={disabled}
                  onPress={() =>
                    useGraphDraftStore.getState().field(
                      key,
                      [...scopePath, "edges"],
                      definition.edges.filter((item) => item !== e),
                    )
                  }
                >
                  <Text>Remove connection</Text>
                </Button>
              </View>
            ))}
          {node.type === "branch" && (
            <GraphPick
              documentKey={key}
              path={vp("port")}
              label="Outcome"
              value={pick("port") || "true"}
              options={[
                { value: "true", label: "True" },
                { value: "false", label: "False" },
              ]}
              disabled={disabled}
            />
          )}
          <GraphPick
            documentKey={key}
            path={vp("target")}
            label="Connect to step"
            value={pick("target")}
            options={definition.nodes
              .filter((n) => n.nodeKey !== node.nodeKey && n.type !== "start")
              .map((n) => ({ value: n.nodeKey, label: n.label || n.nodeKey }))}
            disabled={disabled}
          />
          <Button
            variant="outline"
            disabled={disabled || !pick("target")}
            onPress={() => {
              try {
                useGraphDraftStore
                  .getState()
                  .field(
                    key,
                    scopePath,
                    connectGraphNodes(definition, {
                      from: node.nodeKey,
                      to: pick("target"),
                      ...(node.type === "branch"
                        ? { port: pick("port") || "true" }
                        : {}),
                    }),
                  );
                setError(null);
              } catch (e) {
                setError(e);
              }
            }}
          >
            <Text>Connect steps</Text>
          </Button>
        </GraphSection>
      )}
      <GraphDisclosure title="Technical identifier">
        <GraphText
          label="Step identifier"
          value={identifier || node.nodeKey}
          onChange={setIdentifier}
          disabled={disabled}
        />
        <Button
          variant="outline"
          disabled={disabled || !identifier}
          onPress={() => {
            try {
              useGraphDraftStore
                .getState()
                .field(
                  key,
                  scopePath,
                  renameGraphNode(definition, node.nodeKey, identifier),
                );
              setIdentifier("");
            } catch (e) {
              setError(e);
            }
          }}
        >
          <Text>Rename identifier and references</Text>
        </Button>
      </GraphDisclosure>
      {!["start", "end"].includes(node.type) && (
        <Button
          variant="destructive"
          disabled={disabled}
          onPress={() =>
            Alert.alert(
              "Delete this step?",
              "Its connections will be removed. Validate references before publishing.",
              [
                { text: "Cancel", style: "cancel" },
                {
                  text: "Delete",
                  style: "destructive",
                  onPress: () => {
                    useGraphDraftStore
                      .getState()
                      .field(key, scopePath, {
                        ...definition,
                        nodes: definition.nodes.filter(
                          (n) => n.nodeKey !== node.nodeKey,
                        ),
                        edges: definition.edges.filter(
                          (e) =>
                            e.from !== node.nodeKey && e.to !== node.nodeKey,
                        ),
                      });
                    router.back();
                  },
                },
              ],
            )
          }
        >
          <Text>Delete step</Text>
        </Button>
      )}
      <Button variant="outline" onPress={() => router.back()}>
        <Text>Done</Text>
      </Button>
    </GraphScreen>
  );
}
