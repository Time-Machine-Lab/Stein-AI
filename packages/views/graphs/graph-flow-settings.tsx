"use client";

import { useState } from "react";
import { toast } from "sonner";
import {
  exportGraphDefinition,
  importGraphDefinition,
  type GraphDefinition,
} from "@multica/core/graphs";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { copyText } from "@multica/ui/lib/clipboard";
import { downloadBlob } from "../editor/utils/mermaid-export";
import { useT } from "../i18n";
import { Field } from "./shared";
import { GraphSchemaBuilder } from "./graph-schema-fields";
import { GraphBindings } from "./graph-bindings";
import { objectSchema } from "./graph-definition-helpers";

export type GraphFlowSettingsTab = "input" | "output" | "more";

/**
 * Flow-level settings: what a run asks for, what it hands back, and the rest
 * (concurrency, JSON import/export, archive). Shown in the drawer when no step
 * is selected, and opened on the matching tab from the start and end steps.
 */
export function GraphFlowSettings({
  scope,
  onScopeChange,
  root,
  onRootChange,
  isRoot,
  inLoop,
  disabled,
  tab,
  onTabChange,
  onImported,
  onError,
  onArchive,
  canArchive,
}: {
  scope: GraphDefinition;
  onScopeChange: (definition: GraphDefinition) => void;
  root: GraphDefinition;
  onRootChange: (definition: GraphDefinition) => void;
  isRoot: boolean;
  inLoop: boolean;
  disabled: boolean;
  tab: GraphFlowSettingsTab;
  onTabChange: (tab: GraphFlowSettingsTab) => void;
  onImported: () => void;
  onError: (error: Error) => void;
  onArchive?: () => void;
  canArchive?: boolean;
}) {
  const { t } = useT("graphs");
  const [json, setJson] = useState("");

  async function exportDefinition(download: boolean) {
    const text = exportGraphDefinition(root);
    setJson(text);
    if (download)
      downloadBlob(
        new Blob([text + "\n"], { type: "application/json" }),
        "graph-definition.json",
      );
    else if (await copyText(text)) toast.success(t(($) => $.editor.copied));
  }

  return (
    <Tabs
      value={tab}
      onValueChange={(value) => onTabChange(value as GraphFlowSettingsTab)}
      className="gap-4"
    >
      <TabsList className="w-full">
        <TabsTrigger value="input">{t(($) => $.studio.ux.tab_input)}</TabsTrigger>
        <TabsTrigger value="output">
          {t(($) => $.studio.ux.tab_output)}
        </TabsTrigger>
        <TabsTrigger value="more">{t(($) => $.studio.ux.tab_more)}</TabsTrigger>
      </TabsList>
      <TabsContent value="input" className="space-y-3">
        <p className="text-caption leading-5 text-muted-foreground">
          {t(($) => $.studio.ux.input_hint)}
        </p>
        <GraphSchemaBuilder
          schema={scope.inputSchema}
          onChange={(inputSchema) => onScopeChange({ ...scope, inputSchema })}
          disabled={disabled}
        />
      </TabsContent>
      <TabsContent value="output" className="space-y-4">
        <p className="text-caption leading-5 text-muted-foreground">
          {t(($) => $.studio.ux.output_hint)}
        </p>
        <GraphSchemaBuilder
          schema={scope.outputSchema ?? objectSchema}
          onChange={(outputSchema) => onScopeChange({ ...scope, outputSchema })}
          disabled={disabled}
        />
        <GraphBindings
          definition={scope}
          values={scope.outputs ?? {}}
          onChange={(outputs) => onScopeChange({ ...scope, outputs })}
          disabled={disabled}
          inLoop={inLoop}
        />
      </TabsContent>
      <TabsContent value="more" className="space-y-6">
        {isRoot && (
          <Field label={t(($) => $.production.max_concurrency)}>
            {(id) => (
              <Input
                id={id}
                className="max-w-32"
                type="number"
                min={1}
                max={32}
                disabled={disabled}
                value={root.maxConcurrency ?? 4}
                onChange={(event) =>
                  onRootChange({
                    ...root,
                    schemaVersion: 2,
                    maxConcurrency: Number(event.target.value),
                  })
                }
              />
            )}
          </Field>
        )}
        <section className="space-y-3">
          <Field label={t(($) => $.editor.definition_json)}>
            {(id) => (
              <Textarea
                id={id}
                value={json}
                onChange={(event) => setJson(event.target.value)}
                className="min-h-32 font-mono text-caption"
              />
            )}
          </Field>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={disabled || !json.trim()}
              onClick={() => {
                try {
                  onRootChange(importGraphDefinition(json));
                  onImported();
                } catch (error) {
                  onError(
                    error instanceof Error ? error : new Error(String(error)),
                  );
                }
              }}
            >
              {t(($) => $.editor.import_json)}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => void exportDefinition(false)}
            >
              {t(($) => $.editor.copy_json)}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => void exportDefinition(true)}
            >
              {t(($) => $.editor.download_json)}
            </Button>
          </div>
        </section>
        {onArchive && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="text-destructive"
            disabled={!canArchive}
            onClick={onArchive}
          >
            {t(($) => $.editor.archive)}
          </Button>
        )}
      </TabsContent>
    </Tabs>
  );
}
