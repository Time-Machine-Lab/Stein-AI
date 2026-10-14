import { useEffect, useState } from "react";
import { Alert, Switch, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import * as Clipboard from "expo-clipboard";
import type {
  GraphRecord,
  GraphTrigger,
  GraphTriggerWrite,
  GraphTriggerConfig,
} from "@multica/core/graphs/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import {
  graphDetailOptions,
  graphTriggersOptions,
  graphTriggerEventsOptions,
} from "@/data/queries/graphs";
import { projectListOptions } from "@/data/queries/projects";
import {
  useSaveGraphTrigger,
  useDeleteGraphTrigger,
  useRotateGraphTriggerSecret,
} from "@/data/mutations/graphs";
import {
  useGraphDraftStore,
  graphDraftIdentity,
} from "@/data/stores/graph-draft-store";
import {
  GraphScreen,
  GraphSection,
  GraphText,
  GraphPick,
  GraphDisclosure,
  GraphError,
  GraphStatus,
  useGraphContext,
  useGraphCommandKey,
} from "./shared";
import { GraphStartFields } from "./start-screen";
import { emptyGraph } from "./editor-screen";
function TriggerEvents({ id }: { id: string }) {
  const { wsId, slug } = useGraphContext(),
    query = useQuery(graphTriggerEventsOptions(wsId, id));
  return (
    <View className="gap-3">
      <GraphError error={query.error} retry={() => void query.refetch()} />
      {query.data?.items.map((event) => (
        <View key={event.id} className="gap-1 border-l border-border pl-3">
          <Text className="text-xs text-muted-foreground">
            {new Date(event.createdAt).toLocaleString()}
          </Text>
          <GraphStatus state={event.state} />
          {event.error && (
            <Text className="text-destructive">{event.error}</Text>
          )}
          {event.runId && (
            <Button
              variant="outline"
              onPress={() => {
                void import("expo-router").then(({ router }) =>
                  router.push(`/${slug}/graph/run/${event.runId}`),
                );
              }}
            >
              <Text>Open run</Text>
            </Button>
          )}
        </View>
      ))}
    </View>
  );
}
function TriggerEditor({
  graph,
  trigger,
  onDone,
  onSecret,
}: {
  graph: GraphRecord;
  trigger?: GraphTrigger;
  onDone: () => void;
  onSecret: (secret: string, url: string) => void;
}) {
  const { wsId, userId } = useGraphContext(),
    save = useSaveGraphTrigger(wsId, graph.id),
    projects = useQuery(projectListOptions(wsId)),
    commandKey = useGraphCommandKey(),
    key = graphDraftIdentity(
      wsId,
      userId,
      `trigger-${graph.id}-${trigger?.id ?? "new"}-${trigger?.revision ?? 0}`,
    ),
    doc = useGraphDraftStore((s) => s.documents[key]);
  useEffect(() => {
    const s = useGraphDraftStore.getState();
    s.open(key, emptyGraph(), 0);
    if (!useGraphDraftStore.getState().documents[key]?.values.initialized)
      s.field(key, ["values"], {
        initialized: true,
        name: trigger?.name ?? "",
        kind: trigger?.kind ?? "schedule",
        enabled: trigger?.enabled ?? true,
        config: trigger?.config ?? {
          cron: "0 9 * * 1-5",
          timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
          rootTitle: graph.name,
          events: ["status_changed"],
          statuses: ["todo"],
        },
      });
  }, [key, trigger, graph.name]);
  if (!doc) return null;
  const kind = String(doc.values.kind) as GraphTriggerWrite["kind"],
    config = doc.values.config as GraphTriggerConfig,
    set = (name: string, value: unknown) =>
      useGraphDraftStore.getState().field(key, ["values", name], value),
    patch = (name: string, value: unknown) =>
      useGraphDraftStore
        .getState()
        .field(key, ["values", "config", name], value);
  return (
    <GraphSection title={trigger ? "Edit trigger" : "New trigger"}>
      <GraphError error={save.error ?? projects.error} />
      <GraphText
        label="Trigger name"
        value={String(doc.values.name ?? "")}
        onChange={(v) => set("name", v)}
        disabled={save.isPending}
      />
      <GraphPick
        documentKey={key}
        path={["values", "kind"]}
        label="Trigger type"
        value={kind}
        options={[
          { value: "schedule", label: "Schedule" },
          { value: "webhook", label: "Webhook" },
          { value: "task_event", label: "Task event" },
        ]}
        disabled={save.isPending}
      />
      <View className="flex-row justify-between">
        <Text>Enabled</Text>
        <Switch
          value={doc.values.enabled === true}
          disabled={save.isPending}
          onValueChange={(v) => set("enabled", v)}
        />
      </View>
      {kind === "schedule" && (
        <>
          <GraphText
            label="Schedule (5-field cron)"
            value={config.cron ?? ""}
            onChange={(v) => patch("cron", v)}
            disabled={save.isPending}
          />
          <GraphText
            label="IANA timezone"
            value={config.timezone ?? ""}
            onChange={(v) => patch("timezone", v)}
            disabled={save.isPending}
          />
        </>
      )}
      {kind === "task_event" ? (
        <>
          <GraphPick
            documentKey={key}
            path={["values", "config", "projectId"]}
            label="Project"
            value={config.projectId ?? ""}
            options={(projects.data ?? []).map((p) => ({
              value: p.id,
              label: p.title,
            }))}
            disabled={save.isPending}
          />
          {[
            ["created", "Task created"],
            ["status_changed", "Task status changed"],
          ].map(([value, label]) => (
            <View key={value} className="flex-row items-center justify-between">
              <Text>{label}</Text>
              <Switch
                value={config.events?.includes(value) ?? false}
                disabled={save.isPending}
                onValueChange={(v) =>
                  patch(
                    "events",
                    v
                      ? [...(config.events ?? []), value]
                      : config.events?.filter((e) => e !== value),
                  )
                }
              />
            </View>
          ))}
          <Text className="font-medium">Run only for these task states</Text>
          {[
            "backlog",
            "todo",
            "in_progress",
            "in_review",
            "done",
            "cancelled",
          ].map((value) => (
            <View key={value} className="flex-row items-center justify-between">
              <Text>{value.replaceAll("_", " ")}</Text>
              <Switch
                value={config.statuses?.includes(value) ?? false}
                disabled={save.isPending}
                onValueChange={(v) =>
                  patch(
                    "statuses",
                    v
                      ? [...(config.statuses ?? []), value]
                      : config.statuses?.filter((e) => e !== value),
                  )
                }
              />
            </View>
          ))}
        </>
      ) : (
        <>
          <GraphText
            label="New parent task title"
            value={config.rootTitle ?? ""}
            onChange={(v) => patch("rootTitle", v)}
            disabled={save.isPending}
          />
          <GraphText
            label="New task description"
            value={config.rootDescription ?? ""}
            onChange={(v) => patch("rootDescription", v)}
            disabled={save.isPending}
            multiline
          />
        </>
      )}
      <Text className="text-sm text-muted-foreground">
        This trigger is pinned to the selected published version. Publishing
        another version will not upgrade it automatically.
      </Text>
      <GraphStartFields
        graph={graph}
        formId={trigger?.id ?? "new-trigger"}
        configuration={
          trigger
            ? {
                versionId: trigger.versionId,
                input: trigger.input,
                runtimeBindings: trigger.runtimeBindings,
                repository: trigger.repository,
              }
            : undefined
        }
        pending={save.isPending}
        submitLabel="Save trigger"
        onConfigure={async (execution) => {
          if (
            !String(doc.values.name ?? "").trim() ||
            (kind !== "task_event" && !config.rootTitle?.trim()) ||
            (kind === "task_event" &&
              (!config.projectId ||
                !config.events?.length ||
                !config.statuses?.length))
          )
            throw new Error(
              "Enter a name, parent task title, or task event project and filters.",
            );
          const selected =
            kind === "task_event"
              ? {
                  projectId: config.projectId,
                  events: config.events,
                  statuses: config.statuses,
                }
              : kind === "schedule"
                ? {
                    cron: config.cron,
                    timezone: config.timezone,
                    rootTitle: config.rootTitle,
                    rootDescription: config.rootDescription,
                  }
                : {
                    rootTitle: config.rootTitle,
                    rootDescription: config.rootDescription,
                  };
          const request: GraphTriggerWrite = {
            name: String(doc.values.name).trim(),
            kind,
            enabled: doc.values.enabled === true,
            versionId: execution.versionId,
            input: execution.input,
            runtimeBindings: execution.runtimeBindings,
            repository: execution.repository,
            config: selected,
            expectedRevision: trigger?.revision,
          };
          const result = await save.mutateAsync({
            triggerId: trigger?.id,
            request: { ...request, idempotencyKey: commandKey(request) },
          });
          if (result.secret) onSecret(result.secret, result.webhookUrl ?? "");
          save.reset();
          useGraphDraftStore.getState().field(key, ["values"], {});
          onDone();
        }}
      />
      <Button variant="outline" disabled={save.isPending} onPress={onDone}>
        <Text>Cancel</Text>
      </Button>
    </GraphSection>
  );
}
export function GraphTriggersScreen({ id }: { id: string }) {
  const { wsId, admin } = useGraphContext(),
    graph = useQuery(graphDetailOptions(wsId, id)),
    triggers = useQuery(graphTriggersOptions(wsId, id)),
    save = useSaveGraphTrigger(wsId, id),
    remove = useDeleteGraphTrigger(wsId, id),
    rotate = useRotateGraphTriggerSecret(),
    commandKey = useGraphCommandKey();
  const [editing, setEditing] = useState<string | null>(null),
    [secret, setSecret] = useState<{ secret: string; url: string } | null>(
      null,
    ),
    [error, setError] = useState<unknown>(null);
  const pending = save.isPending || remove.isPending || rotate.isPending;
  const showSecret = (value: string, url: string) =>
    setSecret({
      secret: value,
      url: new URL(url, process.env.EXPO_PUBLIC_API_URL).toString(),
    });
  const invoke = async (fn: () => Promise<unknown>) => {
    setError(null);
    try {
      await fn();
    } catch (e) {
      setError(e);
    }
  };
  return (
    <GraphScreen loading={graph.isLoading || triggers.isLoading}>
      <GraphError
        error={
          error ??
          graph.error ??
          triggers.error ??
          save.error ??
          remove.error ??
          rotate.error
        }
        retry={() => void triggers.refetch()}
      />
      {secret && (
        <GraphSection title="Save this webhook secret">
          <Text>
            This secret is shown only once. Rotation immediately revokes the
            previous secret.
          </Text>
          <Text selectable>{secret.url}</Text>
          <Text selectable>{secret.secret}</Text>
          <Button
            variant="outline"
            onPress={() => void Clipboard.setStringAsync(secret.secret)}
          >
            <Text>Copy secret</Text>
          </Button>
          <Text className="text-sm">
            POST JSON {`{"input":{...}}`} to this URL with Authorization: Bearer
            &lt;secret&gt; and an Idempotency-Key event identifier (1–128 ASCII
            characters). Repeated event keys cannot change the payload.
          </Text>
          <Button
            onPress={() => {
              setSecret(null);
              rotate.reset();
              save.reset();
            }}
          >
            <Text>Done</Text>
          </Button>
        </GraphSection>
      )}
      {graph.data && editing !== null ? (
        <TriggerEditor
          graph={graph.data}
          trigger={triggers.data?.items.find((t) => t.id === editing)}
          onDone={() => setEditing(null)}
          onSecret={showSecret}
        />
      ) : (
        <>
          {admin && (
            <Button
              disabled={
                pending ||
                !graph.data?.versions.length ||
                !!graph.data?.archivedAt
              }
              onPress={() => setEditing("new")}
            >
              <Text>Add trigger</Text>
            </Button>
          )}
          {triggers.data?.items.map((trigger) => (
            <GraphSection key={trigger.id} title={trigger.name}>
              <Text>
                {trigger.kind.replaceAll("_", " ")} ·{" "}
                {trigger.enabled ? "Enabled" : "Disabled"}
              </Text>
              {trigger.nextFireAt && (
                <Text>
                  Next: {new Date(trigger.nextFireAt).toLocaleString()}
                </Text>
              )}
              {trigger.lastError && (
                <Text className="text-destructive">{trigger.lastError}</Text>
              )}
              {admin && (
                <>
                  <Button
                    variant="outline"
                    disabled={pending}
                    onPress={() => setEditing(trigger.id)}
                  >
                    <Text>Edit</Text>
                  </Button>
                  <Button
                    variant="outline"
                    disabled={pending}
                    onPress={() =>
                      void invoke(() =>
                        save.mutateAsync({
                          triggerId: trigger.id,
                          request: {
                            ...trigger,
                            enabled: !trigger.enabled,
                            expectedRevision: trigger.revision,
                            idempotencyKey: commandKey({
                              id: trigger.id,
                              revision: trigger.revision,
                              enabled: !trigger.enabled,
                            }),
                          },
                        }),
                      )
                    }
                  >
                    <Text>{trigger.enabled ? "Disable" : "Enable"}</Text>
                  </Button>
                  {trigger.kind === "webhook" && (
                    <Button
                      variant="outline"
                      disabled={pending}
                      onPress={() =>
                        Alert.alert(
                          "Rotate webhook secret?",
                          "The existing secret stops working immediately.",
                          [
                            { text: "Cancel", style: "cancel" },
                            {
                              text: "Rotate",
                              onPress: () =>
                                void invoke(async () => {
                                  const request = {
                                    triggerId: trigger.id,
                                    expectedRevision: trigger.revision,
                                  };
                                  const result = await rotate.mutateAsync({
                                    ...request,
                                    idempotencyKey: commandKey(request),
                                  });
                                  showSecret(result.secret, result.webhookUrl);
                                  rotate.reset();
                                  void triggers.refetch();
                                }),
                            },
                          ],
                        )
                      }
                    >
                      <Text>Rotate secret</Text>
                    </Button>
                  )}
                  <Button
                    variant="destructive"
                    disabled={pending}
                    onPress={() =>
                      Alert.alert(
                        "Delete trigger?",
                        "Future events will no longer start this workflow.",
                        [
                          { text: "Cancel", style: "cancel" },
                          {
                            text: "Delete",
                            style: "destructive",
                            onPress: () =>
                              void invoke(() =>
                                remove.mutateAsync({
                                  triggerId: trigger.id,
                                  expectedRevision: trigger.revision,
                                  idempotencyKey: commandKey({
                                    id: trigger.id,
                                    revision: trigger.revision,
                                    action: "delete",
                                  }),
                                }),
                              ),
                          },
                        ],
                      )
                    }
                  >
                    <Text>Delete trigger</Text>
                  </Button>
                </>
              )}
              <GraphDisclosure title="Recent trigger events">
                <TriggerEvents id={trigger.id} />
              </GraphDisclosure>
            </GraphSection>
          ))}
        </>
      )}
    </GraphScreen>
  );
}
