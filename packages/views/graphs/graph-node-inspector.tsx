"use client";

import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import {
  connectGraphNodes,
  renameGraphNode,
  type GraphDefinition,
  type GraphNodeDefinition,
} from "@multica/core/graphs";
import type { Agent, MemberWithUser } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { useT } from "../i18n";
import { Field } from "./shared";
import { GraphActorPicker } from "./graph-actor-picker";
import {
  defaultCondition,
  GraphBindingField,
  GraphBindings,
  GraphConditionEditor,
} from "./graph-bindings";
import { GraphSchemaBuilder } from "./graph-schema-fields";
import { useGraphNodeLabels } from "./graph-canvas";

export function GraphNodeInspector({
  definition,
  node,
  onChange,
  onSelect,
  onEnter,
  onError,
  agents,
  members,
  disabled,
  inLoop,
}: {
  definition: GraphDefinition;
  node: GraphNodeDefinition;
  onChange: (definition: GraphDefinition) => void;
  onSelect: (key: string) => void;
  onEnter: (key: string) => void;
  onError: (error: Error) => void;
  agents: Agent[];
  members: MemberWithUser[];
  disabled?: boolean;
  inLoop?: boolean;
}) {
  const { t } = useT("graphs"),
    labels = useGraphNodeLabels();
  const [target, setTarget] = useState("");
  const [port, setPort] = useState("true");
  const patch = (next: Partial<GraphNodeDefinition>) =>
    onChange({
      ...definition,
      schemaVersion: 2,
      nodes: definition.nodes.map((candidate) =>
        candidate.nodeKey === node.nodeKey
          ? { ...candidate, ...next }
          : candidate,
      ),
    });
  const agentOptions = agents
    .filter((agent) => !agent.archived_at || agent.id === node.agentId)
    .map((agent) => ({
      value: agent.id,
      label: agent.name,
      avatarUrl: agent.avatar_url,
    }));
  const approverIds =
    node.approval?.approverIds ?? (node.approverId ? [node.approverId] : []);
  const approval = node.approval ?? { approverIds, mode: "all" };
  return (
    <section className="min-w-0 overflow-hidden rounded-xl border bg-card">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-3">
        <h2 className="text-body font-semibold">
          {labels[node.type] ?? node.type}
        </h2>
        {!["start", "end"].includes(node.type) && (
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            aria-label={t(($) => $.production.delete_node)}
            disabled={disabled}
            onClick={() => {
              onChange({
                ...definition,
                nodes: definition.nodes.filter(
                  (candidate) => candidate.nodeKey !== node.nodeKey,
                ),
                edges: definition.edges.filter(
                  (edge) =>
                    edge.from !== node.nodeKey && edge.to !== node.nodeKey,
                ),
                ...(definition.layout
                  ? {
                      layout: {
                        ...definition.layout,
                        nodes: Object.fromEntries(
                          Object.entries(definition.layout.nodes).filter(
                            ([key]) => key !== node.nodeKey,
                          ),
                        ),
                      },
                    }
                  : {}),
              });
              onSelect("start");
            }}
          >
            <Trash2 className="size-4" />
          </Button>
        )}
      </div>
      <fieldset disabled={disabled} className="space-y-5 p-4">
        <Field label={t(($) => $.production.node_name)}>
          {(id) => (
            <Input
              id={id}
              value={node.label ?? ""}
              placeholder={labels[node.type] ?? node.type}
              onChange={(event) => patch({ label: event.target.value })}
            />
          )}
        </Field>
        <details>
          <summary className="cursor-pointer text-caption text-muted-foreground">
            {t(($) => $.production.raw_details)}
          </summary>
          <div className="mt-3">
            <Field label={t(($) => $.editor.key)}>
              {(id) => (
                <Input
                  id={id}
                  defaultValue={node.nodeKey}
                  onBlur={(event) => {
                    const key = event.target.value.trim();
                    if (key === node.nodeKey) return;
                    try {
                      onChange(renameGraphNode(definition, node.nodeKey, key));
                      onSelect(key);
                    } catch (error) {
                      onError(error as Error);
                      event.target.value = node.nodeKey;
                    }
                  }}
                />
              )}
            </Field>
          </div>
        </details>
        {["agent", "script"].includes(node.type) && (
          <Field label={t(($) => $.editor.agent)}>
            {(id) => (
              <GraphActorPicker
                id={id}
                label={t(($) => $.editor.agent)}
                isAgent
                disabled={disabled}
                options={agentOptions}
                value={node.agentId ?? ""}
                onChange={(agentId) => patch({ agentId })}
                placeholder={t(($) => $.editor.choose_agent)}
              />
            )}
          </Field>
        )}
        {node.type === "agent" && (
          <>
            <Field label={t(($) => $.editor.instructions)}>
              {(id) => (
                <Textarea
                  id={id}
                  className="min-h-36"
                  value={node.instructions ?? ""}
                  onChange={(event) =>
                    patch({ instructions: event.target.value })
                  }
                />
              )}
            </Field>
            <Field label={t(($) => $.editor.mode)}>
              {(id) => (
                <NativeSelect
                  id={id}
                  className="w-full"
                  value={node.executionMode ?? "none"}
                  onChange={(event) =>
                    patch({ executionMode: event.target.value })
                  }
                >
                  <option value="none">{t(($) => $.editor.none)}</option>
                  <option value="produce_commit">
                    {t(($) => $.editor.produce_commit)}
                  </option>
                  <option value="verify_commit">
                    {t(($) => $.editor.verify_commit)}
                  </option>
                </NativeSelect>
              )}
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label={t(($) => $.editor.attempts)}>
                {(id) => (
                  <Input
                    id={id}
                    type="number"
                    min={1}
                    max={3}
                    value={node.maxAttempts ?? 1}
                    onChange={(event) =>
                      patch({ maxAttempts: Number(event.target.value) })
                    }
                  />
                )}
              </Field>
              <Field label={t(($) => $.editor.timeout)}>
                {(id) => (
                  <Input
                    id={id}
                    type="number"
                    min={1}
                    max={86400}
                    value={node.timeoutSeconds ?? 3600}
                    onChange={(event) =>
                      patch({ timeoutSeconds: Number(event.target.value) })
                    }
                  />
                )}
              </Field>
            </div>
          </>
        )}
        {node.type === "branch" && (
          <GraphConditionEditor
            definition={definition}
            nodeKey={node.nodeKey}
            value={node.condition ?? defaultCondition()}
            onChange={(condition) => patch({ condition })}
            disabled={disabled}
            inLoop={inLoop}
          />
        )}
        {node.type === "approval" && (
          <>
            <Field label={t(($) => $.production.approval_mode)}>
              {(id) => (
                <NativeSelect
                  id={id}
                  className="w-full"
                  value={approval.mode}
                  onChange={(event) =>
                    patch({
                      approverId: undefined,
                      approval: {
                        ...approval,
                        mode: event.target.value,
                        quorum: event.target.value === "quorum" ? 1 : undefined,
                      },
                    })
                  }
                >
                  <option value="all">
                    {t(($) => $.production.approval_all)}
                  </option>
                  <option value="any">
                    {t(($) => $.production.approval_any)}
                  </option>
                  <option value="quorum">
                    {t(($) => $.production.approval_quorum)}
                  </option>
                </NativeSelect>
              )}
            </Field>
            {approval.mode === "quorum" && (
              <Field label={t(($) => $.production.quorum)}>
                {(id) => (
                  <Input
                    id={id}
                    type="number"
                    min={1}
                    max={Math.max(1, approverIds.length)}
                    value={approval.quorum ?? 1}
                    onChange={(event) =>
                      patch({
                        approverId: undefined,
                        approval: {
                          ...approval,
                          quorum: Number(event.target.value),
                        },
                      })
                    }
                  />
                )}
              </Field>
            )}
            <div className="space-y-2">
              {approverIds.map((memberId) => (
                <div
                  key={memberId}
                  className="flex items-center gap-2 rounded-lg bg-muted px-3 py-2"
                >
                  <span className="min-w-0 flex-1 truncate text-caption">
                    {members.find((member) => member.id === memberId)?.name ??
                      memberId}
                  </span>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t(($) => $.production.remove_field)}
                    onClick={() =>
                      patch({
                        approverId: undefined,
                        approval: {
                          ...approval,
                          approverIds: approverIds.filter(
                            (id) => id !== memberId,
                          ),
                        },
                      })
                    }
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              ))}
            </div>
            <Field label={t(($) => $.editor.approver)}>
              {(id) => (
                <GraphActorPicker
                  id={id}
                  label={t(($) => $.editor.approver)}
                  value=""
                  onChange={(memberId) =>
                    patch({
                      approverId: undefined,
                      approval: {
                        ...approval,
                        approverIds: [...approverIds, memberId],
                      },
                    })
                  }
                  options={members
                    .filter((member) => !approverIds.includes(member.id))
                    .map((member) => ({
                      value: member.id,
                      label: member.name || member.email,
                      avatarUrl: member.avatar_url,
                    }))}
                  placeholder={t(($) => $.editor.choose_approver)}
                  disabled={disabled || approverIds.length >= 32}
                />
              )}
            </Field>
          </>
        )}
        {node.delay && (
          <Field label={t(($) => $.production.seconds)}>
            {(id) => (
              <Input
                id={id}
                type="number"
                min={1}
                max={86400}
                value={node.delay!.seconds}
                onChange={(event) =>
                  patch({ delay: { seconds: Number(event.target.value) } })
                }
              />
            )}
          </Field>
        )}
        {node.script && (
          <>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.production.script_help)}
            </p>
            <Field label={t(($) => $.production.language)}>
              {(id) => (
                <NativeSelect
                  id={id}
                  value={node.script!.language}
                  onChange={(event) =>
                    patch({
                      script: { ...node.script!, language: event.target.value },
                    })
                  }
                >
                  <option value="javascript">
                    {t(($) => $.production.javascript)}
                  </option>
                  <option value="python">
                    {t(($) => $.production.python)}
                  </option>
                </NativeSelect>
              )}
            </Field>
            <Field label={t(($) => $.production.script_code)}>
              {(id) => (
                <Textarea
                  id={id}
                  className="min-h-48 font-mono text-caption"
                  value={node.script!.code}
                  onChange={(event) =>
                    patch({
                      script: { ...node.script!, code: event.target.value },
                    })
                  }
                />
              )}
            </Field>
            <Field label={t(($) => $.editor.timeout)}>
              {(id) => (
                <Input
                  id={id}
                  type="number"
                  min={1}
                  max={3600}
                  value={node.script!.timeoutSeconds}
                  onChange={(event) =>
                    patch({
                      script: {
                        ...node.script!,
                        timeoutSeconds: Number(event.target.value),
                      },
                    })
                  }
                />
              )}
            </Field>
          </>
        )}
        {node.http && (
          <>
            <div className="grid grid-cols-[100px_minmax(0,1fr)] gap-2">
              <Field label={t(($) => $.production.method)}>
                {(id) => (
                  <NativeSelect
                    id={id}
                    value={node.http!.method}
                    onChange={(event) =>
                      patch({
                        http: { ...node.http!, method: event.target.value },
                      })
                    }
                  >
                    {["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"].map(
                      (method) => (
                        <option key={method}>{method}</option>
                      ),
                    )}
                  </NativeSelect>
                )}
              </Field>
              <Field label={t(($) => $.production.url)}>
                {(id) => (
                  <Input
                    id={id}
                    type="url"
                    value={node.http!.url}
                    placeholder="https://"
                    onChange={(event) =>
                      patch({
                        http: { ...node.http!, url: event.target.value },
                      })
                    }
                  />
                )}
              </Field>
            </div>
            <Field label={t(($) => $.editor.timeout)}>
              {(id) => (
                <Input
                  id={id}
                  type="number"
                  min={1}
                  max={300}
                  value={node.http!.timeoutSeconds}
                  onChange={(event) =>
                    patch({
                      http: {
                        ...node.http!,
                        timeoutSeconds: Number(event.target.value),
                      },
                    })
                  }
                />
              )}
            </Field>
            <Field label={t(($) => $.production.response_limit)}>
              {(id) => (
                <Input
                  id={id}
                  type="number"
                  min={1}
                  max={262144}
                  value={node.http!.maxResponseBytes}
                  onChange={(event) =>
                    patch({
                      http: {
                        ...node.http!,
                        maxResponseBytes: Number(event.target.value),
                      },
                    })
                  }
                />
              )}
            </Field>
            <details>
              <summary className="cursor-pointer text-caption font-medium">
                {t(($) => $.production.headers)}
              </summary>
              <div className="mt-3">
                <GraphBindings
                  definition={definition}
                  nodeKey={node.nodeKey}
                  values={node.http.headers ?? {}}
                  onChange={(headers) =>
                    patch({ http: { ...node.http!, headers } })
                  }
                  disabled={disabled}
                  inLoop={inLoop}
                />
              </div>
            </details>
            {node.http.method !== "GET" && node.http.method !== "HEAD" && (
              <Field label={t(($) => $.production.body)}>
                {() => (
                  <GraphBindingField
                    definition={definition}
                    nodeKey={node.nodeKey}
                    value={node.http!.body ?? { source: "run_input", path: "" }}
                    onChange={(body) =>
                      patch({ http: { ...node.http!, body } })
                    }
                    disabled={disabled}
                    inLoop={inLoop}
                  />
                )}
              </Field>
            )}
          </>
        )}
        {(node.loop || node.subgraph) && (
          <Button
            type="button"
            className="w-full"
            variant="outline"
            onClick={() => onEnter(node.nodeKey)}
          >
            {t(($) => $.production.nested_edit)}
          </Button>
        )}
        {node.loop && (
          <>
            <Field label={t(($) => $.production.max_iterations)}>
              {(id) => (
                <Input
                  id={id}
                  type="number"
                  min={1}
                  max={100}
                  value={node.loop!.maxIterations}
                  onChange={(event) =>
                    patch({
                      loop: {
                        ...node.loop!,
                        maxIterations: Number(event.target.value),
                      },
                    })
                  }
                />
              )}
            </Field>
            <Field label={t(($) => $.production.continue_condition)}>
              {() => (
                <GraphConditionEditor
                  definition={node.loop!.body}
                  value={node.loop!.condition}
                  onChange={(condition) =>
                    patch({ loop: { ...node.loop!, condition } })
                  }
                  disabled={disabled}
                  inLoop
                />
              )}
            </Field>
            <Field label={t(($) => $.production.next_input)}>
              {() => (
                <GraphBindings
                  definition={node.loop!.body}
                  values={node.loop!.next}
                  onChange={(next) => patch({ loop: { ...node.loop!, next } })}
                  disabled={disabled}
                  inLoop
                />
              )}
            </Field>
          </>
        )}
        {["agent", "script", "loop", "subgraph"].includes(node.type) && (
          <details open>
            <summary className="cursor-pointer text-caption font-medium">
              {t(($) => $.editor.bindings)}
            </summary>
            <div className="mt-3">
              <GraphBindings
                definition={definition}
                nodeKey={node.nodeKey}
                values={node.inputs ?? {}}
                onChange={(inputs) => patch({ inputs })}
                disabled={disabled}
                inLoop={inLoop}
              />
            </div>
          </details>
        )}
        {["agent", "script", "loop", "subgraph", "http"].includes(
          node.type,
        ) && (
          <details>
            <summary className="cursor-pointer text-caption font-medium">
              {t(($) => $.editor.output_schema)}
            </summary>
            <div className="mt-3">
              <GraphSchemaBuilder
                schema={node.outputSchema ?? { type: "object" }}
                onChange={(outputSchema) => patch({ outputSchema })}
                disabled={disabled}
              />
            </div>
          </details>
        )}
        {node.type !== "end" && (
          <details open>
            <summary className="cursor-pointer text-caption font-medium">
              {t(($) => $.production.connections)}
            </summary>
            <div className="mt-3 space-y-2">
              {definition.edges
                .filter((edge) => edge.from === node.nodeKey)
                .map((edge) => (
                  <div
                    key={`${edge.to}:${edge.port}`}
                    className="flex items-center gap-2 rounded-lg bg-muted px-3 py-2"
                  >
                    <span className="min-w-0 flex-1 truncate text-caption">
                      {edge.port ? `${edge.port} → ` : ""}
                      {definition.nodes.find((item) => item.nodeKey === edge.to)
                        ?.label || edge.to}
                    </span>
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="ghost"
                      aria-label={t(($) => $.production.remove_field)}
                      onClick={() =>
                        onChange({
                          ...definition,
                          edges: definition.edges.filter(
                            (item) => item !== edge,
                          ),
                        })
                      }
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                ))}
              {node.type === "branch" && (
                <NativeSelect
                  aria-label={t(($) => $.production.branch)}
                  value={port}
                  onChange={(event) => setPort(event.target.value)}
                >
                  <option value="true">
                    {t(($) => $.production.true_path)}
                  </option>
                  <option value="false">
                    {t(($) => $.production.false_path)}
                  </option>
                </NativeSelect>
              )}
              <NativeSelect
                className="w-full"
                aria-label={t(($) => $.production.target_node)}
                value={target}
                onChange={(event) => setTarget(event.target.value)}
              >
                <option value="">{t(($) => $.production.choose_node)}</option>
                {definition.nodes
                  .filter(
                    (candidate) =>
                      candidate.nodeKey !== node.nodeKey &&
                      candidate.type !== "start",
                  )
                  .map((candidate) => (
                    <option key={candidate.nodeKey} value={candidate.nodeKey}>
                      {candidate.label || candidate.nodeKey}
                    </option>
                  ))}
              </NativeSelect>
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={!target}
                onClick={() => {
                  try {
                    onChange(
                      connectGraphNodes(definition, {
                        from: node.nodeKey,
                        to: target,
                        ...(node.type === "branch" ? { port } : {}),
                      }),
                    );
                    setTarget("");
                  } catch (error) {
                    onError(error as Error);
                  }
                }}
              >
                <Plus className="size-3.5" />
                {t(($) => $.production.connect)}
              </Button>
            </div>
          </details>
        )}
      </fieldset>
    </section>
  );
}
