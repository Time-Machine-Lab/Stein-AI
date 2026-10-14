import { useState } from "react";
import { FlatList, Pressable, View } from "react-native";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { Button } from "@/components/ui/button";
import { useGraphDraftStore } from "@/data/stores/graph-draft-store";
export default function GraphPicker() {
  const picker = useGraphDraftStore((s) => s.picker),
    [query, setQuery] = useState("");
  if (!picker)
    return (
      <View className="flex-1 bg-background p-4">
        <Text>This selection has expired.</Text>
        <Button onPress={() => router.back()}>
          <Text>Back</Text>
        </Button>
      </View>
    );
  const items = picker.options.filter((o) =>
    (o.label + " " + (o.description ?? ""))
      .toLowerCase()
      .includes(query.toLowerCase()),
  );
  return (
    <View className="flex-1 bg-background">
      <View className="gap-3 border-b border-border p-4">
        <Text className="text-lg font-semibold">{picker.title}</Text>
        <TextField
          accessibilityLabel="Search options"
          placeholder="Search…"
          value={query}
          onChangeText={setQuery}
        />
      </View>
      <FlatList
        data={items}
        keyboardShouldPersistTaps="handled"
        automaticallyAdjustKeyboardInsets
        keyExtractor={(o) => o.value}
        contentContainerStyle={{ paddingBottom: 40 }}
        renderItem={({ item }) => (
          <Pressable
            className="gap-1 border-b border-border px-4 py-4 active:bg-secondary"
            accessibilityRole="button"
            accessibilityState={{ selected: item.value === picker.value }}
            onPress={() => {
              useGraphDraftStore
                .getState()
                .field(
                  picker.key,
                  picker.path,
                  Object.prototype.hasOwnProperty.call(item, "data")
                    ? item.data
                    : item.value,
                );
              useGraphDraftStore.setState({ picker: undefined });
              router.back();
            }}
          >
            <Text>
              {item.label}
              {item.value === picker.value ? " ✓" : ""}
            </Text>
            {item.description && (
              <Text className="text-xs text-muted-foreground">
                {item.description}
              </Text>
            )}
          </Pressable>
        )}
        ListEmptyComponent={
          <Text className="p-6 text-muted-foreground">
            No matching options.
          </Text>
        }
      />
    </View>
  );
}
