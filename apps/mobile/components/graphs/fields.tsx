import { useState } from "react";
import { Switch, View } from "react-native";
import type {
  GraphDefinition,
  GraphInputBinding,
  GraphCondition,
  JsonObject,
} from "@multica/core/graphs/types";
import { asSchema, schemaDefaults } from "@multica/core/graphs/input-schema";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import { useGraphDraftStore } from "@/data/stores/graph-draft-store";
import {
  GraphField,
  GraphPick,
  GraphText,
  GraphDisclosure,
  GraphError,
} from "./shared";

type Path = (string | number)[];
export const graphPathValue = (value: unknown, path: Path): unknown =>
  path.reduce<unknown>(
    (item, key) =>
      item && typeof item === "object"
        ? (item as Record<string, unknown>)[String(key)]
        : undefined,
    value,
  );
const set = (key: string, path: Path, value: unknown) =>
  useGraphDraftStore.getState().field(key, path, value);
export function GraphJSONField({
  label,
  value,
  onChange,
  disabled,
}: {
  label: string;
  value: unknown;
  onChange: (value: unknown) => void;
  disabled?: boolean;
}) {
  const [text, setText] = useState<string | null>(null),
    [error, setError] = useState<Error | null>(null);
  return (
    <GraphDisclosure title={label}>
      <GraphText
        label={label}
        value={text ?? JSON.stringify(value, null, 2)}
        onChange={setText}
        multiline
        disabled={disabled}
      />
      <GraphError error={error} />
      <Button
        variant="outline"
        disabled={disabled || text === null}
        onPress={() => {
          try {
            onChange(JSON.parse(text!));
            setText(null);
            setError(null);
          } catch (e) {
            setError(e as Error);
          }
        }}
      >
        <Text>Apply changes</Text>
      </Button>
    </GraphDisclosure>
  );
}
export function GraphInputFields({
  documentKey,
  path,
  schema,
  value,
  disabled,
  depth = 0,
}: {
  documentKey: string;
  path: Path;
  schema: JsonObject;
  value: unknown;
  disabled?: boolean;
  depth?: number;
}) {
  const [extraName, setExtraName] = useState("");
  if (depth > 8)
    return (
      <GraphJSONField
        label="Advanced value"
        value={value}
        disabled={disabled}
        onChange={(v) => set(documentKey, path, v)}
      />
    );
  if (Array.isArray(schema.enum))
    return (
      <GraphPick
        documentKey={documentKey}
        path={path}
        label={String(schema.title || schema.description || "Value")}
        value={typeof value === "string" ? value : JSON.stringify(value)}
        options={schema.enum.map((v) => ({
          value: typeof v === "string" ? v : JSON.stringify(v),
          label: String(v),
          data: v,
        }))}
        disabled={disabled}
      />
    );
  if (schema.type === "boolean")
    return (
      <Switch
        accessibilityLabel={String(schema.description || "Enabled")}
        value={value === true}
        disabled={disabled}
        onValueChange={(v) => set(documentKey, path, v)}
      />
    );
  if (schema.type === "object" || schema.properties) {
    const properties = asSchema(schema.properties),
      current = asSchema(value),
      required = Array.isArray(schema.required) ? schema.required : [];
    const keys = [
      ...new Set([...Object.keys(properties), ...Object.keys(current)]),
    ];
    return (
      <View className="gap-3">
        {keys.map((key) => {
          const child = asSchema(properties[key]);
          return (
            <GraphField
              key={key}
              label={
                String(child.title || child.description || key) +
                (required.includes(key) ? " *" : "")
              }
            >
              <GraphInputFields
                documentKey={documentKey}
                path={[...path, key]}
                schema={
                  Object.keys(child).length ? child : inferSchema(current[key])
                }
                value={current[key]}
                disabled={disabled}
                depth={depth + 1}
              />
              {!required.includes(key) && current[key] !== undefined && (
                <Button
                  variant="ghost"
                  disabled={disabled}
                  onPress={() => {
                    const next = { ...current };
                    delete next[key];
                    set(documentKey, path, next);
                  }}
                >
                  <Text>Remove</Text>
                </Button>
              )}
            </GraphField>
          );
        })}
        {schema.additionalProperties !== false && (
          <View className="gap-2">
            <TextField
              accessibilityLabel="New field name"
              placeholder="Additional field name"
              value={extraName}
              onChangeText={setExtraName}
              editable={!disabled}
            />
            <Button
              variant="outline"
              disabled={disabled || !extraName.trim() || extraName in current}
              onPress={() => {
                set(documentKey, path, { ...current, [extraName.trim()]: "" });
                setExtraName("");
              }}
            >
              <Text>Add field</Text>
            </Button>
          </View>
        )}
      </View>
    );
  }
  if (schema.type === "array") {
    const values = Array.isArray(value) ? value : [],
      items = asSchema(schema.items);
    return (
      <View className="gap-3">
        {values.map((v, i) => (
          <View key={i} className="gap-2 border-l border-border pl-3">
            <GraphInputFields
              documentKey={documentKey}
              path={[...path, i]}
              value={v}
              schema={items}
              disabled={disabled}
              depth={depth + 1}
            />
            <Button
              variant="ghost"
              disabled={disabled}
              onPress={() =>
                set(
                  documentKey,
                  path,
                  values.filter((_, index) => index !== i),
                )
              }
            >
              <Text>Remove item {i + 1}</Text>
            </Button>
          </View>
        ))}
        <Button
          variant="outline"
          disabled={
            disabled || values.length >= Number(schema.maxItems ?? 1000)
          }
          onPress={() =>
            set(documentKey, path, [...values, initialValue(items)])
          }
        >
          <Text>Add item</Text>
        </Button>
      </View>
    );
  }
  return (
    <TextField
      accessibilityLabel={String(schema.description || "Value")}
      editable={!disabled}
      value={value === undefined || value === null ? "" : String(value)}
      keyboardType={
        ["number", "integer"].includes(String(schema.type))
          ? "numbers-and-punctuation"
          : "default"
      }
      onChangeText={(v) =>
        set(
          documentKey,
          path,
          ["number", "integer"].includes(String(schema.type))
            ? v.trim()
              ? Number(v)
              : undefined
            : v,
        )
      }
    />
  );
}
function inferSchema(value: unknown): JsonObject {
  return {
    type: Array.isArray(value)
      ? "array"
      : value === null
        ? "string"
        : typeof value === "object"
          ? "object"
          : typeof value === "number"
            ? "number"
            : typeof value === "boolean"
              ? "boolean"
              : "string",
  };
}
function initialValue(schema: JsonObject): unknown {
  return (
    schema.default ??
    (schema.type === "object"
      ? schemaDefaults(schema)
      : schema.type === "array"
        ? []
        : schema.type === "boolean"
          ? false
          : ["number", "integer"].includes(String(schema.type))
            ? 0
            : "")
  );
}
export function GraphSchemaFields({
  documentKey,
  path,
  schema,
  disabled,
  depth = 0,
}: {
  documentKey: string;
  path: Path;
  schema: JsonObject;
  disabled?: boolean;
  depth?: number;
}) {
  const properties = asSchema(schema.properties),
    required = Array.isArray(schema.required)
      ? (schema.required as string[])
      : [];
  const [name, setName] = useState("");
  return (
    <View className="gap-4">
      {Object.entries(properties).map(([key, raw]) => {
        const field = asSchema(raw),
          p = [...path, "properties", key];
        return (
          <View key={key} className="gap-3 rounded-xl border border-border p-3">
            <Text className="font-medium">{key}</Text>
            <GraphText
              label="Field description"
              value={String(field.description ?? "")}
              onChange={(v) => set(documentKey, [...p, "description"], v)}
              disabled={disabled}
            />
            <GraphPick
              documentKey={documentKey}
              path={[...p, "type"]}
              label="Value type"
              value={String(field.type ?? "string")}
              options={[
                "string",
                "number",
                "integer",
                "boolean",
                "object",
                "array",
              ].map((v) => ({ value: v, label: v }))}
              disabled={disabled}
            />
            <View className="flex-row items-center justify-between">
              <Text>Required</Text>
              <Switch
                value={required.includes(key)}
                disabled={disabled}
                onValueChange={(v) =>
                  set(
                    documentKey,
                    [...path, "required"],
                    v ? [...required, key] : required.filter((k) => k !== key),
                  )
                }
              />
            </View>
            {depth < 4 && field.type === "object" && (
              <GraphSchemaFields
                documentKey={documentKey}
                path={p}
                schema={field}
                disabled={disabled}
                depth={depth + 1}
              />
            )}
            <Button
              variant="ghost"
              disabled={disabled}
              onPress={() => {
                const next = { ...properties };
                delete next[key];
                set(documentKey, path, {
                  ...schema,
                  properties: next,
                  required: required.filter((k) => k !== key),
                });
              }}
            >
              <Text>Remove field</Text>
            </Button>
          </View>
        );
      })}
      <TextField
        accessibilityLabel="New schema field"
        placeholder="Field key"
        value={name}
        onChangeText={setName}
        editable={!disabled}
      />
      <Button
        variant="outline"
        disabled={disabled || !name.trim() || name in properties}
        onPress={() => {
          set(documentKey, path, {
            ...schema,
            type: "object",
            properties: {
              ...properties,
              [name.trim()]: { type: "string", description: name.trim() },
            },
          });
          setName("");
        }}
      >
        <Text>Add input field</Text>
      </Button>
      <GraphJSONField
        label="Advanced schema constraints"
        value={schema}
        disabled={disabled}
        onChange={(v) => set(documentKey, path, asSchema(v))}
      />
    </View>
  );
}
export function GraphBindingFields({
  documentKey,
  path,
  binding,
  definition,
  disabled,
  depth = 0,
}: {
  documentKey: string;
  path: Path;
  binding: GraphInputBinding;
  definition: GraphDefinition;
  disabled?: boolean;
  depth?: number;
}) {
  const source = binding.source || "run_input";
  const literalKey = "literal." + path.join("."),
    literalType = useGraphDraftStore(
      (s) => s.documents[documentKey]?.values[literalKey],
    );
  return (
    <View className="gap-3">
      <GraphPick
        documentKey={documentKey}
        path={[...path, "source"]}
        label="Value source"
        value={source}
        options={[
          { value: "run_input", label: "Workflow input" },
          { value: "node_output", label: "Earlier step output" },
          { value: "constant", label: "Fixed value" },
          { value: "iteration_index", label: "Loop iteration" },
        ]}
        disabled={disabled}
      />
      {source === "node_output" && (
        <GraphPick
          documentKey={documentKey}
          path={[...path, "nodeKey"]}
          label="Source step"
          value={binding.nodeKey ?? ""}
          options={definition.nodes
            .filter((n) => n.outputSchema)
            .map((n) => ({ value: n.nodeKey, label: n.label || n.nodeKey }))}
          disabled={disabled}
        />
      )}{" "}
      {(source === "run_input" || source === "node_output") && (
        <GraphText
          label="Field path (empty means all fields)"
          value={binding.path ?? ""}
          onChange={(v) => set(documentKey, [...path, "path"], v)}
          disabled={disabled}
        />
      )}{" "}
      {source === "constant" && (
        <>
          <GraphPick
            documentKey={documentKey}
            path={["values", literalKey]}
            label="Fixed value type"
            value={String(literalType ?? inferSchema(binding.value).type)}
            options={["string", "number", "boolean", "object", "array"].map(
              (value) => ({ value, label: value }),
            )}
            disabled={disabled}
          />
          <GraphInputFields
            documentKey={documentKey}
            path={[...path, "value"]}
            schema={{ type: literalType ?? inferSchema(binding.value).type }}
            value={binding.value}
            disabled={disabled}
            depth={depth}
          />
        </>
      )}
      <GraphDisclosure title="Optional value and fallback">
        <View className="flex-row items-center justify-between">
          <Text>Allow a missing value</Text>
          <Switch
            value={binding.optional === true}
            disabled={disabled}
            onValueChange={(v) => set(documentKey, [...path, "optional"], v)}
          />
        </View>
        <GraphJSONField
          label="Fallback value"
          value={binding.default ?? null}
          disabled={disabled}
          onChange={(v) => set(documentKey, [...path, "default"], v)}
        />
      </GraphDisclosure>
    </View>
  );
}
export function GraphBindingsFields({
  documentKey,
  path,
  bindings,
  definition,
  disabled,
}: {
  documentKey: string;
  path: Path;
  bindings: Record<string, GraphInputBinding>;
  definition: GraphDefinition;
  disabled?: boolean;
}) {
  const [name, setName] = useState("");
  return (
    <View className="gap-4">
      {Object.entries(bindings).map(([key, b]) => (
        <View className="gap-2 rounded-xl border border-border p-3" key={key}>
          <Text className="font-medium">{key}</Text>
          <GraphBindingFields
            documentKey={documentKey}
            path={[...path, key]}
            binding={b}
            definition={definition}
            disabled={disabled}
          />
          <Button
            variant="ghost"
            disabled={disabled}
            onPress={() => {
              const next = { ...bindings };
              delete next[key];
              set(documentKey, path, next);
            }}
          >
            <Text>Remove binding</Text>
          </Button>
        </View>
      ))}
      <TextField
        accessibilityLabel="New input binding"
        placeholder="Input name"
        value={name}
        onChangeText={setName}
        editable={!disabled}
      />
      <Button
        variant="outline"
        disabled={disabled || !name.trim() || name in bindings}
        onPress={() => {
          set(documentKey, path, {
            ...bindings,
            [name.trim()]: { source: "run_input", path: "" },
          });
          setName("");
        }}
      >
        <Text>Add input binding</Text>
      </Button>
    </View>
  );
}
export function GraphConditionFields({
  documentKey,
  path,
  condition,
  definition,
  disabled,
  depth = 0,
}: {
  documentKey: string;
  path: Path;
  condition: GraphCondition;
  definition: GraphDefinition;
  disabled?: boolean;
  depth?: number;
}) {
  const compound = ["all", "any", "not"].includes(condition.op);
  return (
    <View className="gap-3">
      <GraphPick
        documentKey={documentKey}
        path={[...path, "op"]}
        label="Condition"
        value={condition.op}
        options={Object.entries({
          eq: "Equals",
          ne: "Does not equal",
          gt: "Greater than",
          gte: "At least",
          lt: "Less than",
          lte: "At most",
          exists: "Has a value",
          all: "All conditions",
          any: "Any condition",
          not: "Not",
        }).map(([value, label]) => ({ value, label }))}
        disabled={disabled}
      />
      {compound && depth < 6 ? (
        <>
          {(condition.conditions ?? []).map((c, i) => (
            <GraphConditionFields
              key={i}
              documentKey={documentKey}
              path={[...path, "conditions", i]}
              condition={c}
              definition={definition}
              disabled={disabled}
              depth={depth + 1}
            />
          ))}
          {(condition.op !== "not" || !condition.conditions?.length) && (
            <Button
              variant="outline"
              disabled={disabled}
              onPress={() =>
                set(
                  documentKey,
                  [...path, "conditions"],
                  [
                    ...(condition.conditions ?? []),
                    { op: "exists", left: { source: "run_input", path: "" } },
                  ],
                )
              }
            >
              <Text>Add condition</Text>
            </Button>
          )}
        </>
      ) : (
        <>
          <GraphBindingFields
            documentKey={documentKey}
            path={[...path, "left"]}
            binding={condition.left ?? { source: "run_input", path: "" }}
            definition={definition}
            disabled={disabled}
          />
          {condition.op !== "exists" && (
            <GraphBindingFields
              documentKey={documentKey}
              path={[...path, "right"]}
              binding={condition.right ?? { source: "constant", value: true }}
              definition={definition}
              disabled={disabled}
            />
          )}
        </>
      )}
    </View>
  );
}
