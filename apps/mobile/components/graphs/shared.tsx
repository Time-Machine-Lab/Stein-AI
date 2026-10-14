import { useState, useRef, type ReactNode } from "react";
import { ActivityIndicator, Pressable, ScrollView, View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type { GraphDefinition, JsonObject } from "@multica/core/graphs/types";
import { asSchema } from "@multica/core/graphs/input-schema";
import { createRequestId } from "@/lib/request-id";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { Markdown } from "@/lib/markdown";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useAuthStore } from "@/data/auth-store";
import { memberListOptions } from "@/data/queries/members";
import { useGraphDraftStore } from "@/data/stores/graph-draft-store";

export const NODE_LABELS: Record<string, string> = {
  start: "Start",
  end: "Finish",
  agent: "Agent task",
  approval: "Approval",
  branch: "Condition",
  parallel: "Parallel paths",
  join: "Join paths",
  loop: "Repeat",
  subgraph: "Subflow",
  delay: "Wait",
  http: "HTTP request",
  script: "Script",
};
export const STATE_LABELS: Record<string, string> = {
  active: "Running",
  pending: "Pending",
  ready: "Ready",
  running: "Running",
  succeeded: "Succeeded",
  failed: "Failed",
  rejected: "Rejected",
  cancelled: "Cancelled",
  waiting_approval: "Awaiting approval",
  blocked: "Needs attention",
  skipped: "Skipped",
  pause: "Paused",
  paused: "Paused",
  run: "Scheduling",
  cancel_requested: "Cancelling",
  approved: "Approved",
  dispatched: "Dispatched",
  completed: "Completed",
  accepted: "Accepted",
  timeout: "Timed out",
};
export function useGraphContext() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId) ?? "";
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug) ?? "";
  const userId = useAuthStore((s) => s.user?.id) ?? "";
  const members = useQuery(memberListOptions(wsId));
  const member = members.data?.find((m) => m.user_id === userId);
  return {
    wsId,
    slug,
    userId,
    members,
    actor: { userId, memberId: member?.id, role: member?.role },
    admin: member?.role === "admin" || member?.role === "owner",
  };
}
export function GraphScreen({
  children,
  loading,
}: {
  children: ReactNode;
  loading?: boolean;
}) {
  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerStyle={{ padding: 16, paddingBottom: 48, gap: 16 }}
      keyboardShouldPersistTaps="handled"
      automaticallyAdjustKeyboardInsets
      contentInsetAdjustmentBehavior="automatic"
    >
      {loading && <ActivityIndicator />}
      {children}
    </ScrollView>
  );
}
export function GraphSection({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <View className="gap-3 rounded-xl border border-border bg-card p-4">
      <Text className="text-base font-semibold">{title}</Text>
      {children}
    </View>
  );
}
export function GraphError({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  return error ? (
    <View
      accessibilityRole="alert"
      className="gap-2 rounded-xl bg-destructive/10 p-3"
    >
      <Text className="text-destructive">
        {error instanceof Error ? error.message : String(error)}
      </Text>
      {retry && (
        <Button variant="outline" onPress={retry}>
          <Text>Try again</Text>
        </Button>
      )}
    </View>
  ) : null;
}
export function GraphField({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <View className="gap-2">
      <Text className="text-sm font-medium">{label}</Text>
      {children}
    </View>
  );
}
export function GraphText({
  label,
  value,
  onChange,
  multiline,
  disabled,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  multiline?: boolean;
  disabled?: boolean;
}) {
  return (
    <GraphField label={label}>
      {multiline ? (
        <AutosizeTextArea
          accessibilityLabel={label}
          value={value}
          onChangeText={onChange}
          editable={!disabled}
          minHeight={96}
          maxHeight={260}
        />
      ) : (
        <TextField
          accessibilityLabel={label}
          value={value}
          onChangeText={onChange}
          editable={!disabled}
        />
      )}
    </GraphField>
  );
}
export function GraphStatus({ state }: { state: string }) {
  return (
    <Text className="self-start rounded-md bg-secondary px-2 py-1 text-xs">
      {STATE_LABELS[state] ?? state.replaceAll("_", " ")}
    </Text>
  );
}
export function GraphDisclosure({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <View className="gap-3">
      <Pressable
        accessibilityRole="button"
        accessibilityState={{ expanded: open }}
        onPress={() => setOpen(!open)}
        className="py-2"
      >
        <Text className="text-sm font-medium">
          {open ? "−" : "+"} {title}
        </Text>
      </Pressable>
      {open && children}
    </View>
  );
}
export function GraphPick({
  documentKey,
  path,
  label,
  value,
  options,
  disabled,
}: {
  documentKey: string;
  path: (string | number)[];
  label: string;
  value: string;
  options: {
    value: string;
    label: string;
    description?: string;
    data?: unknown;
  }[];
  disabled?: boolean;
}) {
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  return (
    <GraphField label={label}>
      <Button
        variant="outline"
        disabled={disabled}
        className="justify-start"
        accessibilityLabel={label}
        onPress={() => {
          useGraphDraftStore.setState({
            picker: { key: documentKey, path, title: label, value, options },
          });
          router.push(`/${slug}/graph/picker`);
        }}
      >
        <Text numberOfLines={1}>
          {options.find((o) => o.value === value)?.label || "Choose…"}
        </Text>
      </Button>
    </GraphField>
  );
}
const resultLabels: Record<string, string> = {
  summary: "Summary",
  findings: "Findings",
  recommendations: "Recommendations",
  results: "Results",
  evidence: "Evidence",
  output: "Output",
  input: "Input",
};
export function GraphValue({
  value,
  schema = {},
  depth = 0,
}: {
  value: unknown;
  schema?: JsonObject;
  depth?: number;
}) {
  if (value === undefined || value === null)
    return <Text className="text-muted-foreground">—</Text>;
  if (typeof value === "string") return <Markdown content={value} />;
  if (typeof value !== "object") return <Text selectable>{String(value)}</Text>;
  if (depth > 8) return <Text selectable>{JSON.stringify(value)}</Text>;
  if (Array.isArray(value))
    return (
      <View className="gap-3">
        {value.map((v, i) => (
          <GraphValue
            key={i}
            value={v}
            schema={asSchema(schema.items)}
            depth={depth + 1}
          />
        ))}
      </View>
    );
  return (
    <View className="gap-3">
      {Object.entries(value).map(([key, v]) => {
        const field = asSchema(asSchema(schema.properties)[key]);
        return (
          <View key={key} className="gap-1">
            <Text className="text-sm text-muted-foreground">
              {String(
                field.title ||
                  field.description ||
                  resultLabels[key] ||
                  key.replaceAll("_", " "),
              )}
            </Text>
            <GraphValue value={v} schema={field} depth={depth + 1} />
          </View>
        );
      })}
    </View>
  );
}
export function GraphWorkflowTree({
  definition,
  onNode,
  path = [],
  depth = 0,
}: {
  definition: GraphDefinition;
  onNode?: (path: (string | number)[]) => void;
  path?: (string | number)[];
  depth?: number;
}) {
  return (
    <View className="gap-2">
      {definition.nodes.map((node, index) => {
        const nodePath = [...path, "nodes", index];
        return (
          <View
            key={node.nodeKey}
            className="gap-2 rounded-xl border border-border p-3"
          >
            <Pressable
              disabled={!onNode}
              accessibilityRole="button"
              onPress={() => onNode?.(nodePath)}
            >
              <Text className="font-medium">
                {node.label || NODE_LABELS[node.type] || node.nodeKey}
              </Text>
              <Text className="text-xs text-muted-foreground">
                {NODE_LABELS[node.type] ?? node.type}
              </Text>
            </Pressable>
            {node.instructions && (
              <Text numberOfLines={3} className="text-sm text-muted-foreground">
                {node.instructions}
              </Text>
            )}
            {definition.edges
              .filter((e) => e.from === node.nodeKey)
              .map((e) => (
                <Text
                  key={e.to + e.port}
                  className="text-xs text-muted-foreground"
                >
                  {e.port
                    ? e.port === "true"
                      ? "If true → "
                      : "If false → "
                    : "→ "}
                  {definition.nodes.find((n) => n.nodeKey === e.to)?.label ||
                    e.to}
                </Text>
              ))}
            {depth < 4 && (node.loop || node.subgraph) && (
              <GraphDisclosure title="Internal steps">
                <GraphWorkflowTree
                  definition={node.loop?.body ?? node.subgraph!.definition}
                  path={[
                    ...nodePath,
                    node.loop ? "loop" : "subgraph",
                    node.loop ? "body" : "definition",
                  ]}
                  onNode={onNode}
                  depth={depth + 1}
                />
              </GraphDisclosure>
            )}
          </View>
        );
      })}
    </View>
  );
}

/** Reuse the operation key after timeout until its concrete request changes. */
export function useGraphCommandKey() {
  const prior = useRef<{ signature: string; key: string } | null>(null);
  return (value: unknown) => {
    const signature = JSON.stringify(value);
    if (prior.current?.signature !== signature)
      prior.current = { signature, key: createRequestId() };
    return prior.current.key;
  };
}
