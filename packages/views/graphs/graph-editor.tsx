"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  bindingToWire, createSerialDefinition, exportGraphDefinition, importGraphDefinition, orderedAgentNodes, parseJsonObject,
  useArchiveGraph, usePublishGraph, useSaveGraph, useValidateGraph,
  type GraphNodeDefinition, type GraphRecord,
} from "@multica/core/graphs";
import { GraphInputBindingSchema } from "@multica/core/graphs/schemas";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { copyText } from "@multica/ui/lib/clipboard";
import { downloadBlob } from "../editor/utils/mermaid-export";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { Field, GraphError, useGraphActor, useGraphCommandKey } from "./shared";

interface StepDraft {
  node: GraphNodeDefinition;
  bindings: string;
  outputSchema: string;
}

function stepDraft(node: GraphNodeDefinition): StepDraft {
  return { node, bindings: JSON.stringify(Object.fromEntries(Object.entries(node.inputs ?? {}).map(([key, value]) => [key, bindingToWire(value)])), null, 2), outputSchema: JSON.stringify(node.outputSchema ?? { type: "object" }, null, 2) };
}

function newStep(nodeKey: string): StepDraft {
  return stepDraft({ nodeKey, type: "agent", agentId: "", instructions: "", executionMode: "none", maxAttempts: 2, timeoutSeconds: 3600, inputs: { context: { source: "run_input", path: "" } }, outputSchema: { type: "object", additionalProperties: true } });
}

export function GraphEditor({ wsId, graph }: { wsId: string; graph?: GraphRecord }) {
  const { t } = useT("graphs");
  const actor = useGraphActor(wsId);
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const agents = useQuery(agentListOptions(wsId));
  const members = useQuery(memberListOptions(wsId));
  const [name, setName] = useState(graph?.definition.name ?? "");
  const [steps, setSteps] = useState<StepDraft[]>(() => graph ? orderedAgentNodes(graph.definition).map(stepDraft) : [newStep("implement")]);
  const [approverId, setApproverId] = useState(graph?.definition.nodes.find((node) => node.type === "approval")?.approverId ?? "");
  const [inputSchema, setInputSchema] = useState(JSON.stringify(graph?.definition.inputSchema ?? { type: "object", additionalProperties: true }, null, 2));
  const [controlKeys, setControlKeys] = useState(() => ({
    start: graph?.definition.nodes.find((node) => node.type === "start")?.nodeKey ?? "start",
    approval: graph?.definition.nodes.find((node) => node.type === "approval")?.nodeKey ?? "approval",
    end: graph?.definition.nodes.find((node) => node.type === "end")?.nodeKey ?? "end",
  }));
  const [definitionJSON, setDefinitionJSON] = useState("");
  const [dirty, setDirty] = useState(false);
  const [localError, setLocalError] = useState<Error | null>(null);
  const [savedRevision, setSavedRevision] = useState(graph?.revision ?? 0);
  const save = useSaveGraph(wsId);
  const validate = useValidateGraph();
  const publish = usePublishGraph(wsId);
  const archive = useArchiveGraph(wsId);
  const commandKey = useGraphCommandKey();
  const admin = actor.role === "admin" || actor.role === "owner";
  const canEdit = !graph || admin || graph.createdBy === actor.userId;
  const supported = !graph || (graph.definition.schemaVersion === 1 && graph.definition.nodes.every((node) => ["start", "agent", "approval", "end"].includes(node.type)));
  const pending = save.isPending || validate.isPending || publish.isPending || archive.isPending;
  const disabled = pending || !canEdit || !supported || Boolean(graph?.archivedAt);
  const change = (update: () => void) => { update(); setDirty(true); validate.reset(); publish.reset(); setLocalError(null); };
  const patchStep = (index: number, update: Partial<GraphNodeDefinition>) => change(() => setSteps((current) => current.map((step, i) => i === index ? { ...step, node: { ...step.node, ...update } } : step)));
  const reorder = (index: number, direction: number) => change(() => setSteps((current) => {
    const next = [...current];
    [next[index], next[index + direction]] = [next[index + direction]!, next[index]!];
    return next;
  }));

  function currentDefinition() {
    return createSerialDefinition(name.trim(), steps.map((step) => ({
        ...step.node,
        inputs: Object.fromEntries(Object.entries(parseJsonObject(step.bindings)).map(([key, value]) => [key, GraphInputBindingSchema.parse(value)])),
        outputSchema: parseJsonObject(step.outputSchema),
      })), approverId, parseJsonObject(inputSchema), controlKeys);
  }

  function importDefinition() {
    try {
      const definition = importGraphDefinition(definitionJSON);
      change(() => {
        setName(definition.name);
        setSteps(orderedAgentNodes(definition).map(stepDraft));
        setApproverId(definition.nodes.find((node) => node.type === "approval")!.approverId!);
        setInputSchema(JSON.stringify(definition.inputSchema, null, 2));
        setControlKeys({
          start: definition.nodes.find((node) => node.type === "start")!.nodeKey,
          approval: definition.nodes.find((node) => node.type === "approval")!.nodeKey,
          end: definition.nodes.find((node) => node.type === "end")!.nodeKey,
        });
      });
      toast.success(t(($) => $.editor.imported));
    } catch (error) {
      setLocalError(new Error(`${t(($) => $.editor.import_failed)} ${error instanceof Error ? error.message : String(error)}`));
    }
  }

  async function exportDefinition(download: boolean) {
    try {
      const text = exportGraphDefinition(currentDefinition());
      setDefinitionJSON(text);
      setLocalError(null);
      if (download) downloadBlob(new Blob([text + "\n"], { type: "application/json" }), "graph-definition.json");
      else if (await copyText(text)) toast.success(t(($) => $.editor.copied));
      else setLocalError(new Error(t(($) => $.editor.copy_failed)));
    } catch (error) {
      setLocalError(error instanceof Error ? error : new Error(String(error)));
    }
  }

  async function saveDraft() {
    try {
      const definition = currentDefinition();
      const request = { definition, id: graph?.id, expectedRevision: savedRevision };
      const result = await save.mutateAsync({ ...request, idempotencyKey: commandKey(request) });
      setSavedRevision(result.revision);
      setDirty(false);
      validate.reset();
      toast.success(t(($) => $.page.saved));
      if (!graph) navigation.replace(paths.graphDetail(result.id));
    } catch (error) {
      if (error instanceof SyntaxError || (error instanceof Error && error.name === "ZodError")) setLocalError(new Error(t(($) => $.editor.invalid_json)));
      else setLocalError(error instanceof Error ? error : new Error(String(error)));
    }
  }

  return <form className="space-y-6" onSubmit={(event) => { event.preventDefault(); void saveDraft(); }}>
    <GraphError error={localError ?? save.error ?? validate.error ?? publish.error ?? archive.error ?? agents.error ?? members.error} />
    {!supported && <p role="alert" className="text-body text-muted-foreground">{t(($) => $.editor.unsupported)}</p>}
    {!canEdit && <p className="text-body text-muted-foreground">{t(($) => $.editor.permission)}</p>}
    <details className="space-y-3 rounded-xl border p-4"><summary className="cursor-pointer text-body font-medium">{t(($) => $.editor.import_export)}</summary>
      <p className="text-caption text-muted-foreground">{t(($) => $.editor.import_help)}</p>
      <Field label={t(($) => $.editor.definition_json)}>{(id) => <Textarea id={id} className="min-h-48 font-mono text-caption" value={definitionJSON} onChange={(event) => setDefinitionJSON(event.target.value)} />}</Field>
      <div className="flex flex-wrap gap-2"><Button type="button" variant="outline" disabled={disabled || !definitionJSON.trim()} onClick={importDefinition}>{t(($) => $.editor.import_json)}</Button><Button type="button" variant="outline" disabled={pending || !supported} onClick={() => void exportDefinition(false)}>{t(($) => $.editor.copy_json)}</Button><Button type="button" variant="outline" disabled={pending || !supported} onClick={() => void exportDefinition(true)}>{t(($) => $.editor.download_json)}</Button></div>
    </details>
    <fieldset disabled={disabled} className="space-y-6">
      <Field label={t(($) => $.editor.name)}>{(id) => <Input id={id} required maxLength={200} value={name} placeholder={t(($) => $.editor.name_placeholder)} onChange={(event) => change(() => setName(event.target.value))} />}</Field>
      <div className="space-y-2"><h2 className="text-title font-medium">{t(($) => $.editor.steps)}</h2><p className="text-caption text-muted-foreground">{t(($) => $.editor.chain)}</p></div>
      {steps.map((step, index) => <section key={index} className="space-y-4 rounded-xl border p-4">
        <div className="flex items-center gap-2"><h3 className="flex-1 text-body font-medium">{t(($) => $.editor.step, { number: index + 1 })}</h3>
          <Button type="button" variant="ghost" size="icon-sm" disabled={index === 0} aria-label={t(($) => $.editor.up)} onClick={() => reorder(index, -1)}><ArrowUp className="size-4" /></Button>
          <Button type="button" variant="ghost" size="icon-sm" disabled={index === steps.length - 1} aria-label={t(($) => $.editor.down)} onClick={() => reorder(index, 1)}><ArrowDown className="size-4" /></Button>
          <Button type="button" variant="ghost" size="icon-sm" disabled={steps.length === 1} aria-label={t(($) => $.editor.remove)} onClick={() => change(() => setSteps((current) => current.filter((_, i) => i !== index)))}><Trash2 className="size-4" /></Button>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t(($) => $.editor.key)}>{(id) => <Input id={id} required pattern="[a-zA-Z][a-zA-Z0-9_-]*" value={step.node.nodeKey} onChange={(event) => patchStep(index, { nodeKey: event.target.value })} />}</Field>
          <Field label={t(($) => $.editor.agent)}>{(id) => <NativeSelect id={id} className="w-full" required value={step.node.agentId ?? ""} onChange={(event) => patchStep(index, { agentId: event.target.value })}><option value="">{t(($) => $.editor.choose_agent)}</option>{(agents.data ?? []).map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}</NativeSelect>}</Field>
        </div>
        <Field label={t(($) => $.editor.instructions)}>{(id) => <Textarea id={id} required className="min-h-28" value={step.node.instructions ?? ""} placeholder={t(($) => $.editor.instructions_placeholder)} onChange={(event) => patchStep(index, { instructions: event.target.value })} />}</Field>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label={t(($) => $.editor.mode)}>{(id) => <NativeSelect id={id} className="w-full" value={step.node.executionMode ?? "none"} onChange={(event) => patchStep(index, { executionMode: event.target.value })}><option value="none">{t(($) => $.editor.none)}</option><option value="produce_commit">{t(($) => $.editor.produce_commit)}</option><option value="verify_commit">{t(($) => $.editor.verify_commit)}</option></NativeSelect>}</Field>
          <Field label={t(($) => $.editor.attempts)}>{(id) => <Input id={id} type="number" min={1} max={3} required value={step.node.maxAttempts ?? 1} onChange={(event) => patchStep(index, { maxAttempts: Number(event.target.value) })} />}</Field>
          <Field label={t(($) => $.editor.timeout)}>{(id) => <Input id={id} type="number" min={1} max={86400} required value={step.node.timeoutSeconds ?? 3600} onChange={(event) => patchStep(index, { timeoutSeconds: Number(event.target.value) })} />}</Field>
        </div>
        <details className="space-y-3"><summary className="cursor-pointer text-body text-muted-foreground">{t(($) => $.editor.advanced)}</summary>
          <Field label={t(($) => $.editor.bindings)}>{(id) => <Textarea id={id} className="min-h-32 font-mono text-caption" value={step.bindings} onChange={(event) => change(() => setSteps((current) => current.map((item, i) => i === index ? { ...item, bindings: event.target.value } : item)))} />}</Field>
          <p className="text-caption text-muted-foreground">{t(($) => $.editor.bindings_help)}</p>
          <Field label={t(($) => $.editor.output_schema)}>{(id) => <Textarea id={id} className="min-h-28 font-mono text-caption" value={step.outputSchema} onChange={(event) => change(() => setSteps((current) => current.map((item, i) => i === index ? { ...item, outputSchema: event.target.value } : item)))} />}</Field>
        </details>
      </section>)}
      <Button type="button" variant="outline" disabled={steps.length >= 16} onClick={() => change(() => setSteps((current) => [...current, newStep(`step_${crypto.randomUUID().slice(0, 8)}`)]))}><Plus className="size-4" />{t(($) => $.editor.add)}</Button>
      <Field label={t(($) => $.editor.approver)}>{(id) => <NativeSelect id={id} className="w-full" required value={approverId} onChange={(event) => change(() => setApproverId(event.target.value))}><option value="">{t(($) => $.editor.choose_approver)}</option>{(members.data ?? []).map((member) => <option key={member.id} value={member.id}>{member.name || member.email}</option>)}</NativeSelect>}</Field>
      <details className="space-y-3"><summary className="cursor-pointer text-body text-muted-foreground">{t(($) => $.editor.input_schema)}</summary><Textarea aria-label={t(($) => $.editor.input_schema)} className="min-h-28 font-mono text-caption" value={inputSchema} onChange={(event) => change(() => setInputSchema(event.target.value))} /></details>
    </fieldset>
    {dirty && <p className="text-caption text-muted-foreground">{t(($) => $.editor.dirty)}</p>}
    {validate.data && <div role="status" className="space-y-2 rounded-lg border p-3">{validate.data.valid === true ? t(($) => $.editor.valid) : validate.data.diagnostics.map((diagnostic, index) => <p key={index} className="text-body text-destructive">{[diagnostic.nodeKey, diagnostic.path, diagnostic.message].filter(Boolean).join(": ")}</p>)}</div>}
    <div className="flex flex-wrap gap-2">
      <Button type="submit" disabled={disabled || Boolean(graph && !dirty)}>{t(($) => $.editor.save)}</Button>
      {graph && <><Button type="button" variant="outline" disabled={pending || dirty} onClick={() => validate.mutate(graph.id)}>{t(($) => $.editor.validate)}</Button><Button type="button" variant="outline" disabled={pending || dirty || !admin || graph.archivedAt != null || validate.data?.valid !== true} onClick={() => {
        const request = { id: graph.id, expectedRevision: savedRevision };
        publish.mutate({ ...request, idempotencyKey: commandKey(request) }, { onSuccess: () => toast.success(t(($) => $.page.published)) });
      }}>{t(($) => $.editor.publish)}</Button><Button type="button" variant="outline" disabled={pending || dirty || !canEdit || Boolean(graph.archivedAt)} onClick={() => {
        const request = { id: graph.id, expectedRevision: savedRevision };
        archive.mutate({ ...request, idempotencyKey: commandKey({ ...request, action: "archive" }) }, { onSuccess: () => toast.success(t(($) => $.page.archived)) });
      }}>{t(($) => $.editor.archive)}</Button></>}
    </div>
  </form>;
}
