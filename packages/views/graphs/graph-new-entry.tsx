"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowUp, Copy, Plus } from "lucide-react";
import {
  graphGenerationMode,
  graphGenerationOptions,
  graphSearchOptions,
  type GraphDefinition,
} from "@multica/core/graphs";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";
import { GraphNodeIcon } from "./graph-node-catalog";

/**
 * Where "New graph" lands: describe it, copy an existing one, or start blank.
 * All three continue into the same studio; nothing here asks the user to pick
 * between an AI mode and a manual mode first.
 */
export function GraphNewEntry({
  wsId,
  onDescribe,
  onCopy,
  onBlank,
}: {
  wsId: string;
  onDescribe: (message: string) => void;
  onCopy: (definition: GraphDefinition) => void;
  onBlank: () => void;
}) {
  const { t } = useT("graphs");
  const [text, setText] = useState("");
  const generation = useQuery(graphGenerationOptions(wsId));
  const recent = useQuery(graphSearchOptions(wsId, { limit: 3 }));
  const aiAvailable = graphGenerationMode(generation.data) === "builder";
  const examples = [
    t(($) => $.studio.example_research),
    t(($) => $.studio.example_release),
    t(($) => $.studio.example_content),
  ];
  const submit = () => {
    if (text.trim()) onDescribe(text.trim());
  };
  const copies = (recent.data?.items ?? []).filter(
    (graph) => graph.definition?.nodes?.length,
  );

  return (
    <main className="flex min-h-0 flex-1 justify-center overflow-y-auto px-5 py-12">
      <div className="w-full max-w-2xl">
        <h2 className="text-center text-title-lg font-semibold">
          {t(($) => $.studio.ux.entry_title)}
        </h2>
        <p className="mx-auto mt-2 max-w-lg text-center text-body leading-6 text-muted-foreground">
          {aiAvailable
            ? t(($) => $.studio.ux.entry_description)
            : t(($) => $.studio.ux.ai_unavailable)}
        </p>
        {aiAvailable && (
          <>
            <div className="mt-8 rounded-2xl border bg-card p-3 shadow-sm focus-within:ring-2 focus-within:ring-ring/30">
              <Textarea
                aria-label={t(($) => $.studio.prompt)}
                value={text}
                autoFocus
                onChange={(event) => setText(event.target.value)}
                onKeyDown={(event) => {
                  if (
                    event.key === "Enter" &&
                    !event.shiftKey &&
                    !event.nativeEvent.isComposing
                  ) {
                    event.preventDefault();
                    submit();
                  }
                }}
                placeholder={t(($) => $.studio.placeholder)}
                className="min-h-24 resize-none border-0 bg-transparent p-1 text-body shadow-none focus-visible:ring-0"
              />
              <div className="flex justify-end">
                <Button
                  type="button"
                  size="icon"
                  className="rounded-full"
                  aria-label={t(($) => $.studio.ux.entry_send)}
                  disabled={!text.trim()}
                  onClick={submit}
                >
                  <ArrowUp className="size-4" />
                </Button>
              </div>
            </div>
            <div className="mt-3 flex flex-wrap gap-2">
              {examples.map((example) => (
                <button
                  key={example}
                  type="button"
                  onClick={() => setText(example)}
                  className="rounded-full border bg-background px-3 py-1.5 text-left text-caption transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {example}
                </button>
              ))}
            </div>
          </>
        )}
        <p className="mt-10 text-caption font-medium text-muted-foreground">
          {t(($) => $.studio.ux.entry_or)}
        </p>
        <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {copies.map((graph) => (
            <button
              key={graph.id}
              type="button"
              onClick={() =>
                onCopy({
                  ...graph.definition,
                  name: t(($) => $.studio.ux.copy_name, { name: graph.name }),
                })
              }
              className="group flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-4 text-left transition-colors hover:border-primary/40 hover:bg-accent/30"
            >
              <span className="flex gap-1">
                {graph.definition.nodes
                  .filter((node) => !["start", "end"].includes(node.type))
                  .slice(0, 5)
                  .map((node) => (
                    <GraphNodeIcon
                      key={node.nodeKey}
                      type={node.type}
                      className="size-5"
                    />
                  ))}
              </span>
              <span className="min-w-0">
                <span className="block truncate text-body font-medium">
                  {graph.name}
                </span>
                <span className="mt-0.5 flex items-center gap-1 text-caption text-muted-foreground">
                  <Copy className="size-3" />
                  {t(($) => $.studio.ux.copy_from)}
                </span>
              </span>
            </button>
          ))}
          <button
            type="button"
            onClick={onBlank}
            className="flex min-h-24 items-center justify-center gap-2 rounded-xl border border-dashed text-body text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground"
          >
            <Plus className="size-4" />
            {t(($) => $.studio.ux.blank)}
          </button>
        </div>
      </div>
    </main>
  );
}
