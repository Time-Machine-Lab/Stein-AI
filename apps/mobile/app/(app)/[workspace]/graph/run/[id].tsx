import { useLocalSearchParams } from "expo-router";
import { GraphRunScreen } from "@/components/graphs/run-screen";
export default function GraphRun() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <GraphRunScreen id={id} />;
}
