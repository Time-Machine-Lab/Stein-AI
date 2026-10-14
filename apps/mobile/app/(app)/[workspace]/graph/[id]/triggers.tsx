import { useLocalSearchParams } from "expo-router";
import { GraphTriggersScreen } from "@/components/graphs/triggers-screen";
export default function GraphTriggers() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <GraphTriggersScreen id={id} />;
}
