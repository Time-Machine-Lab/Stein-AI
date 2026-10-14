import { useState, useDeferredValue } from "react";
import { FlatList, Pressable, ScrollView, View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import {
  graphSearchOptions,
  graphRunSearchOptions,
} from "@/data/queries/graphs";
import {
  GraphError,
  GraphStatus,
  STATE_LABELS,
  useGraphContext,
} from "./shared";
export function GraphListScreen({
  runs = false,
  graphId,
}: {
  runs?: boolean;
  graphId?: string;
}) {
  const { wsId, slug } = useGraphContext();
  const [q, setQ] = useState(""),
    [state, setState] = useState(""),
    [cursors, setCursors] = useState<string[]>([]);
  const params = {
    q: useDeferredValue(q),
    state: state || undefined,
    graphId,
    cursor: cursors[cursors.length - 1],
    limit: 20,
  };
  const graphs = useQuery({
      ...graphSearchOptions(wsId, {
        q: params.q,
        cursor: params.cursor,
        limit: 20,
      }),
      enabled: !!wsId && !runs,
    }),
    history = useQuery({
      ...graphRunSearchOptions(wsId, params),
      enabled: !!wsId && runs,
    });
  const query = runs ? history : graphs,
    items =
      (runs
        ? history.data?.items.map((run) => ({
            id: run.id,
            title: run.graphName || "Workflow run",
            subtitle: new Date(run.createdAt).toLocaleString(),
            state: run.state,
          }))
        : graphs.data?.items.map((graph) => ({
            id: graph.id,
            title: graph.name,
            subtitle: graph.versions.length
              ? `${graph.versions.length} published version(s)`
              : "Draft",
            state: graph.archivedAt ? "Archived" : "",
          }))) ?? [];
  return (
    <View className="flex-1 bg-background">
      <View className="gap-3 border-b border-border p-4">
        <View className="flex-row gap-3">
          <Button
            variant="outline"
            onPress={() => router.push(`/${slug}/graph/${runs ? "" : "runs"}`)}
          >
            <Text>{runs ? "Workflows" : "All runs"}</Text>
          </Button>
          {!runs && (
            <Button onPress={() => router.push(`/${slug}/graph/new`)}>
              <Text>Create Graph</Text>
            </Button>
          )}
        </View>
        <TextField
          accessibilityLabel="Search workflows"
          placeholder={runs ? "Search runs…" : "Search workflows…"}
          value={q}
          onChangeText={(v) => {
            setQ(v);
            setCursors([]);
          }}
        />
        {runs && (
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={{ gap: 8 }}
          >
            {[
              "",
              "active",
              "waiting_approval",
              "blocked",
              "succeeded",
              "failed",
              "rejected",
              "cancelled",
            ].map((value) => (
              <Button
                key={value}
                size="sm"
                variant={state === value ? "default" : "outline"}
                onPress={() => {
                  setState(value);
                  setCursors([]);
                }}
              >
                <Text>{STATE_LABELS[value] || "All states"}</Text>
              </Button>
            ))}
          </ScrollView>
        )}
        <GraphError error={query.error} retry={() => void query.refetch()} />
        {history.data?.stats && (
          <View className="gap-1">
            <Text>
              {history.data.stats.active} running ·{" "}
              {history.data.stats.waitingApproval} awaiting approval ·{" "}
              {history.data.stats.blocked} blocked
            </Text>
            <Text className="text-xs text-muted-foreground">
              {history.data.stats.totalTokens.toLocaleString()} tokens · $
              {history.data.stats.totalCostUsd.toFixed(2)} recorded
              {history.data.stats.unpricedTasks
                ? ` · ${history.data.stats.unpricedTasks} tasks have no price data`
                : ""}
            </Text>
          </View>
        )}
      </View>
      <FlatList
        data={items}
        keyExtractor={(item) => item.id}
        refreshing={query.isRefetching}
        onRefresh={() => void query.refetch()}
        contentContainerStyle={{ padding: 16, gap: 12, paddingBottom: 40 }}
        renderItem={({ item }) => (
          <Pressable
            accessibilityRole="button"
            className="gap-2 rounded-xl border border-border bg-card p-4 active:bg-secondary"
            onPress={() =>
              router.push(`/${slug}/graph/${runs ? "run/" : ""}${item.id}`)
            }
          >
            <Text className="text-base font-semibold">{item.title}</Text>
            <Text className="text-sm text-muted-foreground">
              {item.subtitle}
            </Text>
            {item.state && <GraphStatus state={item.state} />}
          </Pressable>
        )}
        ListEmptyComponent={
          <Text className="py-8 text-center text-muted-foreground">
            {query.isLoading
              ? "Loading…"
              : q
                ? "No matching workflows."
                : runs
                  ? "No runs yet."
                  : "Create a workflow from an AI proposal or build its steps manually."}
          </Text>
        }
        ListFooterComponent={
          <View className="flex-row justify-between gap-3">
            <Button
              variant="outline"
              disabled={!cursors.length || query.isFetching}
              onPress={() => setCursors((v) => v.slice(0, -1))}
            >
              <Text>Previous</Text>
            </Button>
            <Text>{query.data?.total ?? 0} total</Text>
            <Button
              variant="outline"
              disabled={!query.data?.nextCursor || query.isFetching}
              onPress={() => setCursors((v) => [...v, query.data!.nextCursor!])}
            >
              <Text>Next</Text>
            </Button>
          </View>
        }
      />
    </View>
  );
}
