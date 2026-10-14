"use client";

import { Plus, Trash2 } from "lucide-react";
import {
  graphAncestors,
  schemaFields,
  type GraphCondition,
  type GraphDefinition,
  type GraphInputBinding,
} from "@multica/core/graphs";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { useT } from "../i18n";
import { GraphValueInput } from "./graph-schema-fields";
import { Field } from "./shared";

export function GraphBindingField({
  definition,
  nodeKey,
  value,
  onChange,
  disabled,
  inLoop = false,
  depth = 0,
}: {
  definition: GraphDefinition;
  nodeKey?: string;
  value: GraphInputBinding;
  onChange: (value: GraphInputBinding) => void;
  disabled?: boolean;
  inLoop?: boolean;
  depth?: number;
}) {
  const { t } = useT("graphs");
  const ancestors = nodeKey
    ? graphAncestors(definition, nodeKey)
    : definition.nodes;
  const sourceSchema =
    value.source === "run_input"
      ? definition.inputSchema
      : (ancestors.find((node) => node.nodeKey === value.nodeKey)
          ?.outputSchema ?? {});
  const fields = schemaFields(sourceSchema);
  const reference =
    value.source === "run_input" || value.source === "node_output";
  return (
    <div className="space-y-2">
      <NativeSelect
        className="w-full"
        aria-label={t(($) => $.production.source)}
        disabled={disabled}
        value={value.source}
        onChange={(event) =>
          onChange({
            source: event.target.value,
            ...(event.target.value === "constant" ? { value: "" } : {}),
            ...(event.target.value === "object" ? { fields: {} } : {}),
            ...(["run_input", "node_output"].includes(event.target.value)
              ? { path: "" }
              : {}),
          })
        }
      >
        <option value="run_input">{t(($) => $.start.input)}</option>
        <option value="node_output">
          {t(($) => $.production.node_output)}
        </option>
        <option value="constant">{t(($) => $.production.constant)}</option>
        <option value="object">{t(($) => $.production.object)}</option>
        {inLoop && (
          <option value="iteration_index">
            {t(($) => $.production.iteration)}
          </option>
        )}
      </NativeSelect>
      {value.source === "node_output" && (
        <NativeSelect
          className="w-full"
          aria-label={t(($) => $.production.source_node)}
          disabled={disabled}
          value={value.nodeKey ?? ""}
          onChange={(event) =>
            onChange({ ...value, nodeKey: event.target.value, path: "" })
          }
        >
          <option value="">{t(($) => $.production.choose_node)}</option>
          {ancestors
            .filter((node) => node.outputSchema)
            .map((node) => (
              <option key={node.nodeKey} value={node.nodeKey}>
                {node.label || node.nodeKey}
              </option>
            ))}
        </NativeSelect>
      )}
      {reference && (
        <>
          <NativeSelect
            className="w-full"
            aria-label={t(($) => $.production.source_field)}
            disabled={disabled}
            value={value.path ?? ""}
            onChange={(event) =>
              onChange({ ...value, path: event.target.value })
            }
          >
            {fields.map((field) => (
              <option key={field.path} value={field.path}>
                {field.path || t(($) => $.production.whole_object)} ·{" "}
                {field.type}
              </option>
            ))}
            {value.path &&
              !fields.some((field) => field.path === value.path) && (
                <option value={value.path}>{value.path}</option>
              )}
          </NativeSelect>
          <details className="text-caption">
            <summary className="cursor-pointer text-muted-foreground">
              {t(($) => $.production.missing_value)}
            </summary>
            <div className="mt-2 space-y-2">
              <label className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={value.optional === true}
                  disabled={disabled}
                  onChange={(event) =>
                    onChange({
                      ...value,
                      optional: event.target.checked || undefined,
                    })
                  }
                />
                {t(($) => $.production.optional)}
              </label>
              <Input
                aria-label={t(($) => $.production.custom_path)}
                placeholder={t(($) => $.production.custom_path)}
                value={value.path ?? ""}
                disabled={disabled}
                onChange={(event) =>
                  onChange({ ...value, path: event.target.value })
                }
              />
              <GraphValueInput
                value={value.default}
                onChange={(next) => onChange({ ...value, default: next })}
                disabled={disabled}
                optional
              />
            </div>
          </details>
        </>
      )}
      {value.source === "constant" && (
        <GraphValueInput
          value={value.value}
          onChange={(next) => onChange({ source: "constant", value: next })}
          disabled={disabled}
        />
      )}
      {value.source === "object" && depth < 8 && (
        <GraphBindings
          definition={definition}
          nodeKey={nodeKey}
          values={value.fields ?? {}}
          onChange={(next) => onChange({ source: "object", fields: next })}
          disabled={disabled}
          inLoop={inLoop}
          depth={depth + 1}
        />
      )}
    </div>
  );
}

export function GraphBindings({
  definition,
  nodeKey,
  values,
  onChange,
  disabled,
  inLoop,
  depth = 0,
}: {
  definition: GraphDefinition;
  nodeKey?: string;
  values: Record<string, GraphInputBinding>;
  onChange: (values: Record<string, GraphInputBinding>) => void;
  disabled?: boolean;
  inLoop?: boolean;
  depth?: number;
}) {
  const { t } = useT("graphs");
  return (
    <div className="space-y-3">
      {Object.entries(values).map(([key, value]) => (
        <div
          key={key}
          className="space-y-2 rounded-lg border bg-background p-3"
        >
          <div className="flex gap-2">
            <Input
              aria-label={t(($) => $.production.target_field)}
              disabled={disabled}
              defaultValue={key}
              onBlur={(event) => {
                const next = event.target.value.trim();
                if (!next || (next !== key && values[next])) {
                  event.target.value = key;
                  return;
                }
                if (next !== key) {
                  const copy = { ...values };
                  delete copy[key];
                  copy[next] = value;
                  onChange(copy);
                }
              }}
            />
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              aria-label={t(($) => $.production.remove_field)}
              disabled={disabled}
              onClick={() => {
                const copy = { ...values };
                delete copy[key];
                onChange(copy);
              }}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
          <GraphBindingField
            definition={definition}
            nodeKey={nodeKey}
            value={value}
            onChange={(next) => onChange({ ...values, [key]: next })}
            disabled={disabled}
            inLoop={inLoop}
            depth={depth}
          />
        </div>
      ))}
      <Button
        type="button"
        size="sm"
        variant="outline"
        disabled={disabled || Object.keys(values).length >= 128}
        onClick={() => {
          let index = Object.keys(values).length + 1;
          while (values[`field_${index}`]) index++;
          onChange({
            ...values,
            [`field_${index}`]: { source: "run_input", path: "" },
          });
        }}
      >
        <Plus className="size-3.5" />
        {t(($) => $.production.add_binding)}
      </Button>
    </div>
  );
}

export const defaultCondition = (): GraphCondition => ({
  op: "eq",
  left: { source: "run_input", path: "" },
  right: { source: "constant", value: true },
});
export function GraphConditionEditor({
  definition,
  nodeKey,
  value,
  onChange,
  disabled,
  inLoop,
  depth = 0,
}: {
  definition: GraphDefinition;
  nodeKey?: string;
  value: GraphCondition;
  onChange: (value: GraphCondition) => void;
  disabled?: boolean;
  inLoop?: boolean;
  depth?: number;
}) {
  const { t } = useT("graphs");
  const operators: Record<string, string> = {
    eq: t(($) => $.conditions.eq),
    ne: t(($) => $.conditions.ne),
    gt: t(($) => $.conditions.gt),
    gte: t(($) => $.conditions.gte),
    lt: t(($) => $.conditions.lt),
    lte: t(($) => $.conditions.lte),
    exists: t(($) => $.conditions.exists),
    all: t(($) => $.conditions.all),
    any: t(($) => $.conditions.any),
    not: t(($) => $.conditions.not),
  };
  const compound = ["all", "any", "not"].includes(value.op);
  return (
    <div className="space-y-3 rounded-lg border p-3">
      <Field label={t(($) => $.production.operator)}>
        {(id) => (
          <NativeSelect
            id={id}
            className="w-full"
            value={value.op}
            disabled={disabled}
            onChange={(event) => {
              const op = event.target.value;
              onChange(
                ["all", "any", "not"].includes(op)
                  ? { op, conditions: [defaultCondition()] }
                  : {
                      ...defaultCondition(),
                      op,
                      ...(op === "exists" ? { right: undefined } : {}),
                    },
              );
            }}
          >
            {[
              "eq",
              "ne",
              "gt",
              "gte",
              "lt",
              "lte",
              "exists",
              ...(depth < 6 ? ["all", "any", "not"] : []),
            ].map((op) => (
              <option key={op} value={op}>
                {operators[op] ?? op}
              </option>
            ))}
          </NativeSelect>
        )}
      </Field>
      {compound ? (
        <>
          {(value.conditions ?? []).map((condition, index) => (
            <div className="space-y-2" key={index}>
              <GraphConditionEditor
                definition={definition}
                nodeKey={nodeKey}
                value={condition}
                disabled={disabled}
                inLoop={inLoop}
                depth={depth + 1}
                onChange={(next) =>
                  onChange({
                    ...value,
                    conditions: value.conditions!.map((old, i) =>
                      i === index ? next : old,
                    ),
                  })
                }
              />
              {value.conditions!.length > 1 && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={disabled}
                  onClick={() =>
                    onChange({
                      ...value,
                      conditions: value.conditions!.filter(
                        (_, i) => i !== index,
                      ),
                    })
                  }
                >
                  {t(($) => $.production.remove_condition)}
                </Button>
              )}
            </div>
          ))}
          {value.op !== "not" && (value.conditions?.length ?? 0) < 32 && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={disabled}
              onClick={() =>
                onChange({
                  ...value,
                  conditions: [...(value.conditions ?? []), defaultCondition()],
                })
              }
            >
              {t(($) => $.production.add_condition)}
            </Button>
          )}
        </>
      ) : (
        <>
          <GraphBindingField
            definition={definition}
            nodeKey={nodeKey}
            value={value.left ?? { source: "run_input", path: "" }}
            onChange={(left) => onChange({ ...value, left })}
            disabled={disabled}
            inLoop={inLoop}
          />
          {value.op !== "exists" && (
            <GraphBindingField
              definition={definition}
              nodeKey={nodeKey}
              value={value.right ?? { source: "constant", value: true }}
              onChange={(right) => onChange({ ...value, right })}
              disabled={disabled}
              inLoop={inLoop}
            />
          )}
        </>
      )}
    </div>
  );
}
