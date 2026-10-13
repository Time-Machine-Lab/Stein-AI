"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { isGraphRuntimeSupported, orderedAgentNodes, parseJsonObject, useStartGraphRun, type GraphRecord } from "@multica/core/graphs";
import { runtimeListOptions } from "@multica/core/runtimes";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { Button } from "@multica/ui/components/ui/button";
import { buildRuntimeMachines, runtimeRowLabel } from "../runtimes/components/runtime-machines";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { Field, GraphError, useGraphCommandKey } from "./shared";

export function GraphStartForm({ wsId, graph }: { wsId: string; graph: GraphRecord }) {
  const { t } = useT("graphs");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const runtimes = useQuery(runtimeListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const [versionId, setVersionId] = useState("");
  const [rootIssueId, setRootIssueId] = useState(navigation.searchParams.get("issue") ?? "");
  const [input, setInput] = useState("{}");
  const [bindings, setBindings] = useState<Record<string, string>>({});
  const [repositoryId, setRepositoryId] = useState("");
  const [baseSha, setBaseSha] = useState("");
  const [deadline, setDeadline] = useState("");
  const [localError, setLocalError] = useState<Error | null>(null);
  const start = useStartGraphRun(wsId);
  const commandKey = useGraphCommandKey();
  const version = graph.versions.find((candidate) => candidate.id === versionId) ?? [...graph.versions].sort((a, b) => b.version - a.version)[0];
  const steps = version ? orderedAgentNodes(version.definition) : [];
  const needsRepository = steps.some((node) => node.executionMode !== "none");
  const supportedRuntimes = useMemo(() => (runtimes.data ?? []).filter(isGraphRuntimeSupported), [runtimes.data]);
  const machines = useMemo(() => buildRuntimeMachines(supportedRuntimes, { now: Date.now() }), [supportedRuntimes]);
  const runtimeFor = (nodeKey: string, agentId?: string) => {
    const candidate = bindings[nodeKey] ?? agents.data?.find((agent) => agent.id === agentId)?.runtime_id ?? "";
    return supportedRuntimes.some((runtime) => runtime.id === candidate) ? candidate : "";
  };
  if (!version) return <p className="text-body text-muted-foreground">{t(($) => $.start.no_version)}</p>;

  async function submit() {
    if (!version) return;
    setLocalError(null);
    try {
      const request = {
        versionId: version.id, rootIssueId: rootIssueId.trim(), input: parseJsonObject(input),
        runtimeBindings: Object.fromEntries(steps.map((node) => [node.nodeKey, runtimeFor(node.nodeKey, node.agentId)])),
        repository: needsRepository ? { repositoryId: repositoryId.trim(), baseSha: baseSha.trim().toLowerCase(), commitSha: baseSha.trim().toLowerCase() } : undefined,
        deadline: deadline ? new Date(deadline).toISOString() : undefined,
      };
      const run = await start.mutateAsync({ ...request, idempotencyKey: commandKey(request) });
      navigation.push(paths.graphRun(run.id));
    } catch (error) {
      setLocalError(error instanceof SyntaxError ? new Error(t(($) => $.editor.invalid_json)) : error instanceof Error ? error : new Error(String(error)));
    }
  }

  return <form className="space-y-4 rounded-xl border p-4" onSubmit={(event) => { event.preventDefault(); void submit(); }}>
    <h2 className="text-title font-medium">{t(($) => $.start.title)}</h2>
    <p className="text-caption text-muted-foreground">{t(($) => $.start.fixed)}</p>
    <p className="text-caption text-muted-foreground">{t(($) => $.start.runtime_support)}</p>
    <GraphError error={localError ?? start.error ?? runtimes.error} />
    <fieldset disabled={start.isPending || Boolean(graph.archivedAt)} className="space-y-4">
      <Field label={t(($) => $.start.version)}>{(id) => <NativeSelect id={id} className="w-full" required value={version.id} onChange={(event) => setVersionId(event.target.value)}>{graph.versions.map((item) => <option key={item.id} value={item.id}>{t(($) => $.page.version, { version: item.version })}</option>)}</NativeSelect>}</Field>
      <Field label={t(($) => $.start.issue)}>{(id) => <Input id={id} required value={rootIssueId} placeholder={t(($) => $.start.issue_placeholder)} onChange={(event) => setRootIssueId(event.target.value)} />}</Field>
      <Field label={t(($) => $.start.input)}>{(id) => <Textarea id={id} className="min-h-28 font-mono text-caption" value={input} onChange={(event) => setInput(event.target.value)} />}</Field>
      {steps.map((node) => <Field key={node.nodeKey} label={t(($) => $.start.runtime, { node: node.nodeKey })}>{(id) => <NativeSelect id={id} className="w-full" required value={runtimeFor(node.nodeKey, node.agentId)} onChange={(event) => setBindings((current) => ({ ...current, [node.nodeKey]: event.target.value }))}><option value="">{t(($) => $.start.choose_runtime)}</option>{machines.map((machine) => <optgroup key={machine.id} label={machine.title}>{machine.runtimes.map((runtime) => <option key={runtime.id} value={runtime.id}>{runtimeRowLabel(runtime, machine.title)}</option>)}</optgroup>)}</NativeSelect>}</Field>)}
      {needsRepository && <><Field label={t(($) => $.start.repository)}>{(id) => <Input id={id} required value={repositoryId} onChange={(event) => setRepositoryId(event.target.value)} />}</Field><Field label={t(($) => $.start.base_sha)}>{(id) => <Input id={id} required pattern="[0-9a-fA-F]{40}([0-9a-fA-F]{24})?" value={baseSha} onChange={(event) => setBaseSha(event.target.value)} />}</Field></>}
      <Field label={t(($) => $.start.deadline)}>{(id) => <Input id={id} type="datetime-local" value={deadline} onChange={(event) => setDeadline(event.target.value)} />}</Field>
      <Button type="submit">{t(($) => $.start.submit)}</Button>
    </fieldset>
  </form>;
}
