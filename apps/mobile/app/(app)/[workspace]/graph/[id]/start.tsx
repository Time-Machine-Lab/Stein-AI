import { useLocalSearchParams } from "expo-router";
import { GraphStartScreen } from "@/components/graphs/start-screen";
export default function StartGraph() {
  const { id, previous_run, issue } = useLocalSearchParams<{
    id: string;
    previous_run?: string;
    issue?: string;
  }>();
  return (
    <GraphStartScreen
      id={id}
      previousRunId={previous_run}
      rootIssueId={issue}
    />
  );
}
