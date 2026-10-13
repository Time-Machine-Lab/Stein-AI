import { useLocalSearchParams } from "expo-router";
import { GraphListScreen } from "@/components/graphs/list-screen";
export default function GraphRuns() {
  const { graph } = useLocalSearchParams<{ graph?: string }>();
  return <GraphListScreen runs graphId={graph} />;
}
