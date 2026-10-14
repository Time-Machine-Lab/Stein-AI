import { useLocalSearchParams } from "expo-router";
import { GraphEditorScreen } from "@/components/graphs/editor-screen";
export default function GraphDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <GraphEditorScreen id={id} />;
}
