"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CalendarClock, KeyRound, Plus, Webhook, Zap } from "lucide-react";
import { api } from "@multica/core/api";
import {
  graphTriggersOptions,
  graphTriggerEventsOptions,
  useSaveGraphTrigger,
  useDeleteGraphTrigger,
  useRotateGraphTriggerSecret,
  type GraphRecord,
  type GraphTrigger,
  type GraphTriggerConfig,
  type GraphTriggerWrite,
} from "@multica/core/graphs";
import { projectListOptions } from "@multica/core/projects/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import { copyText } from "@multica/ui/lib/clipboard";
import { toast } from "sonner";
import { AppLink } from "../navigation";
import { useT } from "../i18n";
import {
  Field,
  GraphError,
  GraphState,
  useGraphActor,
  useGraphCommandKey,
} from "./shared";
import { GraphStartForm } from "./graph-start-form";
import { GraphResourcePicker } from "./graph-resource-picker";

function TriggerEvents({
  wsId,
  triggerId,
}: {
  wsId: string;
  triggerId: string;
}) {
  const { t } = useT("graphs"),
    paths = useWorkspacePaths();
  const events = useQuery(graphTriggerEventsOptions(wsId, triggerId));
  return (
    <div className="space-y-2">
      <GraphError error={events.error} />
      {events.isPending && (
        <p className="text-caption">{t(($) => $.page.loading)}</p>
      )}
      {events.data?.items.map((event) => (
        <div
          key={event.id}
          className="flex flex-wrap items-center gap-2 rounded-lg bg-muted/30 p-3"
        >
          <GraphState state={event.state} />
          <span className="min-w-0 flex-1 text-caption text-muted-foreground">
            {new Date(event.createdAt).toLocaleString()}
          </span>
          {event.runId && (
            <AppLink
              className="text-caption underline"
              href={paths.graphRun(event.runId)}
            >
              {t(($) => $.triggers.view_run)}
            </AppLink>
          )}
          {event.error && (
            <p className="w-full break-words text-caption text-destructive">
              {event.error}
            </p>
          )}
        </div>
      ))}
    </div>
  );
}
function TriggerEditor({
  wsId,
  graph,
  trigger,
  onSaved,
  onCancel,
}: {
  wsId: string;
  graph: GraphRecord;
  trigger?: GraphTrigger;
  onSaved: (trigger: GraphTrigger) => void;
  onCancel: () => void;
}) {
  const { t } = useT("graphs");
  const { t: ti } = useT("issues");
  const statusLabels: Record<string, string> = {
    backlog: ti(($) => $.status.backlog),
    todo: ti(($) => $.status.todo),
    in_progress: ti(($) => $.status.in_progress),
    in_review: ti(($) => $.status.in_review),
    done: ti(($) => $.status.done),
    cancelled: ti(($) => $.status.cancelled),
  };
  const [name, setName] = useState(trigger?.name ?? ""),
    [kind, setKind] = useState<GraphTriggerWrite["kind"]>(
      trigger?.kind ?? "schedule",
    );
  const [config, setConfig] = useState<GraphTriggerConfig>(
    trigger?.config ?? {
      cron: "0 9 * * 1-5",
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      rootTitle: graph.name,
      events: ["status_changed"],
      statuses: ["todo"],
    },
  );
  const [enabled, setEnabled] = useState(trigger?.enabled ?? true),
    [localError, setLocalError] = useState<Error | null>(null);
  const projects = useQuery(projectListOptions(wsId)),
    save = useSaveGraphTrigger(wsId, graph.id),
    commandKey = useGraphCommandKey();
  const patch = (value: Partial<GraphTriggerConfig>) =>
    setConfig((current) => ({ ...current, ...value }));
  return (
    <div className="space-y-5 rounded-2xl border bg-background p-5">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-title font-semibold">
          {trigger ? t(($) => $.triggers.edit) : t(($) => $.triggers.add)}
        </h2>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          disabled={save.isPending}
          onClick={onCancel}
        >
          {t(($) => $.production.cancel)}
        </Button>
      </div>
      <GraphError error={localError ?? save.error} />
      <Field label={t(($) => $.editor.name)}>
        {(id) => (
          <Input
            id={id}
            value={name}
            onChange={(event) => setName(event.target.value)}
            maxLength={200}
            disabled={save.isPending}
          />
        )}
      </Field>
      <Field label={t(($) => $.triggers.kind)}>
        {(id) => (
          <NativeSelect
            id={id}
            className="w-full"
            value={kind}
            disabled={save.isPending || Boolean(trigger)}
            onChange={(event) =>
              setKind(event.target.value as GraphTriggerWrite["kind"])
            }
          >
            <option value="schedule">{t(($) => $.triggers.schedule)}</option>
            <option value="webhook">{t(($) => $.triggers.webhook)}</option>
            <option value="task_event">
              {t(($) => $.triggers.task_event)}
            </option>
          </NativeSelect>
        )}
      </Field>
      {kind === "schedule" && (
        <div className="space-y-3">
          <Field label={t(($) => $.triggers.cron)}>
            {(id) => (
              <Input
                id={id}
                value={config.cron ?? ""}
                onChange={(event) => patch({ cron: event.target.value })}
                disabled={save.isPending}
              />
            )}
          </Field>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.triggers.cron_help)}
          </p>
          <Field label={t(($) => $.triggers.timezone)}>
            {(id) => (
              <Input
                id={id}
                value={config.timezone ?? ""}
                onChange={(event) => patch({ timezone: event.target.value })}
                disabled={save.isPending}
              />
            )}
          </Field>
        </div>
      )}
      {kind === "task_event" ? (
        <>
          <Field label={t(($) => $.triggers.project)}>
            {(id) => (
              <GraphResourcePicker
                id={id}
                label={t(($) => $.triggers.project)}
                value={config.projectId ?? ""}
                onChange={(projectId) => patch({ projectId })}
                options={(projects.data ?? []).map((project) => ({
                  value: project.id,
                  label: project.title,
                }))}
                placeholder={t(($) => $.triggers.choose_project)}
                loading={projects.isPending}
                disabled={save.isPending}
              />
            )}
          </Field>
          <fieldset className="space-y-2">
            <legend className="mb-2 text-caption font-medium">
              {t(($) => $.triggers.events)}
            </legend>
            {["created", "status_changed"].map((event) => (
              <label key={event} className="flex items-center gap-2 text-body">
                <input
                  type="checkbox"
                  checked={config.events?.includes(event) ?? false}
                  disabled={save.isPending}
                  onChange={(change) =>
                    patch({
                      events: change.target.checked
                        ? [...(config.events ?? []), event]
                        : config.events?.filter((item) => item !== event),
                    })
                  }
                />
                {event === "created"
                  ? t(($) => $.triggers.created)
                  : t(($) => $.triggers.status_changed)}
              </label>
            ))}
          </fieldset>
          <fieldset className="space-y-2">
            <legend className="mb-2 text-caption font-medium">
              {t(($) => $.triggers.statuses)}
            </legend>
            <div className="flex flex-wrap gap-3">
              {[
                "backlog",
                "todo",
                "in_progress",
                "in_review",
                "done",
                "cancelled",
              ].map((status) => (
                <label
                  key={status}
                  className="flex items-center gap-2 text-caption"
                >
                  <input
                    type="checkbox"
                    checked={config.statuses?.includes(status) ?? false}
                    disabled={save.isPending}
                    onChange={(event) =>
                      patch({
                        statuses: event.target.checked
                          ? [...(config.statuses ?? []), status]
                          : config.statuses?.filter((item) => item !== status),
                      })
                    }
                  />
                  {statusLabels[status] ?? status}
                </label>
              ))}
            </div>
          </fieldset>
        </>
      ) : (
        <>
          <Field label={t(($) => $.triggers.root_title)}>
            {(id) => (
              <Input
                id={id}
                value={config.rootTitle ?? ""}
                onChange={(event) => patch({ rootTitle: event.target.value })}
                disabled={save.isPending}
              />
            )}
          </Field>
          <Field label={t(($) => $.triggers.root_description)}>
            {(id) => (
              <Textarea
                id={id}
                value={config.rootDescription ?? ""}
                onChange={(event) =>
                  patch({ rootDescription: event.target.value })
                }
                disabled={save.isPending}
              />
            )}
          </Field>
        </>
      )}
      <label className="flex items-center gap-2 text-body">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(event) => setEnabled(event.target.checked)}
          disabled={save.isPending}
        />
        {t(($) => $.triggers.enabled)}
      </label>
      <p className="rounded-lg bg-muted p-3 text-caption leading-5 text-muted-foreground">
        {t(($) => $.triggers.fixed_version)}
      </p>
      <GraphStartForm
        wsId={wsId}
        graph={graph}
        configuration={trigger}
        externalPending={save.isPending}
        submitLabel={t(($) => $.triggers.save)}
        onConfigure={async (execution) => {
          setLocalError(null);
          if (
            !name.trim() ||
            (kind !== "task_event" && !config.rootTitle?.trim()) ||
            (kind === "task_event" &&
              (!config.projectId ||
                !config.events?.length ||
                !config.statuses?.length))
          ) {
            const error = new Error(t(($) => $.triggers.required));
            setLocalError(error);
            throw error;
          }
          const selectedConfig: GraphTriggerConfig =
            kind === "schedule"
              ? {
                  cron: config.cron,
                  timezone: config.timezone,
                  rootTitle: config.rootTitle,
                  rootDescription: config.rootDescription,
                }
              : kind === "webhook"
                ? {
                    rootTitle: config.rootTitle,
                    rootDescription: config.rootDescription,
                  }
                : {
                    projectId: config.projectId,
                    statuses: config.statuses,
                    events: config.events,
                  };
          const request: GraphTriggerWrite = {
            name: name.trim(),
            kind,
            enabled,
            versionId: execution.versionId,
            config: selectedConfig,
            input: execution.input,
            runtimeBindings: execution.runtimeBindings,
            repository: execution.repository,
            expectedRevision: trigger?.revision,
          };
          const result = await save.mutateAsync({
            request: { ...request, idempotencyKey: commandKey(request) },
            triggerId: trigger?.id,
          });
          onSaved(result);
        }}
      />
    </div>
  );
}

export function GraphTriggers({
  wsId,
  graph,
}: {
  wsId: string;
  graph: GraphRecord;
}) {
  const { t } = useT("graphs"),
    actor = useGraphActor(wsId),
    commandKey = useGraphCommandKey();
  const triggers = useQuery(graphTriggersOptions(wsId, graph.id)),
    save = useSaveGraphTrigger(wsId, graph.id),
    remove = useDeleteGraphTrigger(wsId, graph.id),
    rotate = useRotateGraphTriggerSecret();
  const [editing, setEditing] = useState<GraphTrigger | "new" | null>(null),
    [expanded, setExpanded] = useState("");
  const [confirmation, setConfirmation] = useState<{
    action: "delete" | "rotate";
    trigger: GraphTrigger;
  } | null>(null);
  const [revealed, setRevealed] = useState<{
    secret: string;
    webhookUrl: string;
  } | null>(null);
  const canManage =
    !graph.archivedAt &&
    (actor.role === "owner" ||
      actor.role === "admin" ||
      graph.createdBy === actor.userId);
  const pending = save.isPending || remove.isPending || rotate.isPending;
  const kinds = {
    schedule: t(($) => $.triggers.schedule),
    webhook: t(($) => $.triggers.webhook),
    task_event: t(($) => $.triggers.task_event),
  };
  const webhookURL = (path: string) => {
    try {
      return new URL(path, api.getBaseUrl() || window.location.origin).href;
    } catch {
      return path;
    }
  };
  return (
    <div className="mx-auto max-w-4xl space-y-5 px-5 py-7">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-title font-semibold">
          {t(($) => $.triggers.title)}
        </h2>
        <Button
          size="sm"
          disabled={!canManage || !graph.versions.length || Boolean(editing)}
          onClick={() => setEditing("new")}
        >
          <Plus className="size-4" />
          {t(($) => $.triggers.add)}
        </Button>
      </div>
      <GraphError
        error={triggers.error ?? save.error ?? remove.error ?? rotate.error}
      />
      {revealed && (
        <section className="space-y-3 rounded-xl border border-warning/30 bg-warning/5 p-4">
          <p className="text-body font-medium">
            {t(($) => $.triggers.secret_once)}
          </p>
          <code className="block break-all rounded-lg bg-background p-3 text-caption">
            {revealed.secret}
          </code>
          <code className="block break-all text-caption">
            {webhookURL(revealed.webhookUrl)}
          </code>
          <p className="text-caption leading-5 text-muted-foreground">
            {t(($) => $.triggers.webhook_help)}
          </p>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={async () => {
                if (await copyText(revealed.secret))
                  toast.success(t(($) => $.editor.copied));
              }}
            >
              {t(($) => $.triggers.copy_secret)}
            </Button>
            <Button
              size="sm"
              onClick={() => {
                setRevealed(null);
                rotate.reset();
              }}
            >
              {t(($) => $.production.done)}
            </Button>
          </div>
        </section>
      )}
      {editing && (
        <TriggerEditor
          key={editing === "new" ? "new" : editing.id}
          wsId={wsId}
          graph={graph}
          trigger={editing === "new" ? undefined : editing}
          onCancel={() => setEditing(null)}
          onSaved={(trigger) => {
            setEditing(null);
            if (trigger.secret && trigger.webhookUrl)
              setRevealed({
                secret: trigger.secret,
                webhookUrl: trigger.webhookUrl,
              });
          }}
        />
      )}
      {triggers.isPending ? (
        <p>{t(($) => $.page.loading)}</p>
      ) : !triggers.data?.items.length && !editing ? (
        <p className="rounded-xl border border-dashed p-8 text-center text-body text-muted-foreground">
          {t(($) => $.triggers.empty)}
        </p>
      ) : (
        triggers.data?.items.map((trigger) => {
          const Icon =
            trigger.kind === "schedule"
              ? CalendarClock
              : trigger.kind === "webhook"
                ? Webhook
                : Zap;
          return (
            <section
              key={trigger.id}
              className="space-y-4 rounded-xl border bg-card p-4"
            >
              <div className="flex flex-wrap items-start gap-3">
                <span className="rounded-lg bg-muted p-2">
                  <Icon className="size-4 text-muted-foreground" />
                </span>
                <div className="min-w-0 flex-1">
                  <h3 className="truncate text-body font-semibold">
                    {trigger.name}
                  </h3>
                  <p className="mt-1 text-caption text-muted-foreground">
                    {kinds[trigger.kind]} ·{" "}
                    {t(($) => $.page.version, {
                      version:
                        graph.versions.find(
                          (version) => version.id === trigger.versionId,
                        )?.version ?? "—",
                    })}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={!canManage || pending}
                  onClick={() => {
                    const request = {
                      ...trigger,
                      enabled: !trigger.enabled,
                      expectedRevision: trigger.revision,
                    };
                    save.mutate({
                      triggerId: trigger.id,
                      request: {
                        ...request,
                        idempotencyKey: commandKey(request),
                      },
                    });
                  }}
                >
                  {trigger.enabled
                    ? t(($) => $.triggers.disable)
                    : t(($) => $.triggers.enable)}
                </Button>
              </div>
              {trigger.kind === "schedule" && (
                <p className="text-caption text-muted-foreground">
                  {trigger.config.cron} · {trigger.config.timezone}
                  {trigger.nextFireAt
                    ? ` · ${t(($) => $.triggers.next_fire)}: ${new Date(trigger.nextFireAt).toLocaleString()}`
                    : ""}
                </p>
              )}
              {trigger.webhookUrl && (
                <code className="block break-all text-caption text-muted-foreground">
                  {webhookURL(trigger.webhookUrl)}
                </code>
              )}
              {trigger.lastError && (
                <p role="status" className="text-caption text-destructive">
                  {trigger.lastError}
                </p>
              )}
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={!canManage || pending}
                  onClick={() => setEditing(trigger)}
                >
                  {t(($) => $.triggers.edit)}
                </Button>
                {trigger.kind === "webhook" && (
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={!canManage || pending}
                    onClick={() =>
                      setConfirmation({ action: "rotate", trigger })
                    }
                  >
                    <KeyRound className="size-3.5" />
                    {t(($) => $.triggers.rotate)}
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={!canManage || pending}
                  onClick={() => setConfirmation({ action: "delete", trigger })}
                >
                  {t(($) => $.triggers.remove)}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() =>
                    setExpanded(expanded === trigger.id ? "" : trigger.id)
                  }
                >
                  {t(($) => $.triggers.recent_events)}
                </Button>
              </div>
              {expanded === trigger.id && (
                <TriggerEvents wsId={wsId} triggerId={trigger.id} />
              )}
            </section>
          );
        })
      )}
      <AlertDialog
        open={Boolean(confirmation)}
        onOpenChange={(open) => {
          if (!open) setConfirmation(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirmation?.action === "rotate"
                ? t(($) => $.triggers.rotate)
                : t(($) => $.triggers.remove)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirmation?.action === "rotate"
                ? t(($) => $.triggers.rotate_help)
                : t(($) => $.triggers.remove_help)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.production.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={pending}
              onClick={() => {
                if (!confirmation) return;
                const request = {
                  triggerId: confirmation.trigger.id,
                  expectedRevision: confirmation.trigger.revision,
                };
                if (confirmation.action === "delete")
                  remove.mutate(
                    {
                      ...request,
                      idempotencyKey: commandKey({
                        ...request,
                        action: "delete",
                      }),
                    },
                    { onSuccess: () => setConfirmation(null) },
                  );
                else
                  rotate.mutate(
                    {
                      ...request,
                      idempotencyKey: commandKey({
                        ...request,
                        action: "rotate",
                      }),
                    },
                    {
                      onSuccess: (result) => {
                        setRevealed(result);
                        setConfirmation(null);
                        void triggers.refetch();
                      },
                    },
                  );
              }}
            >
              {t(($) => $.run.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
