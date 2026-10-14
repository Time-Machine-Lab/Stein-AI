"use client";

import { Plus, Trash2 } from "lucide-react";
import { asSchema, type JsonObject } from "@multica/core/graphs";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { useT } from "../i18n";
import { Field } from "./shared";

export function GraphSchemaForm({
  schema,
  value,
  onChange,
  disabled,
  depth = 0,
}: {
  schema: JsonObject;
  value: JsonObject;
  onChange: (value: JsonObject) => void;
  disabled?: boolean;
  depth?: number;
}) {
  const { t } = useT("graphs");
  const required = Array.isArray(schema.required) ? schema.required : [];
  if (depth > 8) return null;
  const properties = asSchema(schema.properties);
  const extras = Object.entries(value).filter(([key]) => !(key in properties));
  return (
    <div className="space-y-4">
      {Object.entries(properties).map(([key, item]) => {
        const field = asSchema(item),
          title = typeof field.title === "string" ? field.title : key;
        return (
          <Field
            key={key}
            label={`${title}${required.includes(key) ? " *" : ""}`}
          >
            {(id) => (
              <div className="space-y-1.5">
                <SchemaValue
                  id={id}
                  schema={field}
                  value={value[key]}
                  disabled={disabled}
                  depth={depth}
                  onChange={(next) => onChange({ ...value, [key]: next })}
                />
                {typeof field.description === "string" && (
                  <p className="text-caption text-muted-foreground">
                    {field.description}
                  </p>
                )}
              </div>
            )}
          </Field>
        );
      })}
      {extras.map(([key, item]) => (
        <div key={key} className="space-y-2 rounded-lg border p-3">
          <div className="flex gap-2">
            <Input
              aria-label={t(($) => $.production.field_name)}
              disabled={disabled}
              defaultValue={key}
              onBlur={(event) => {
                const next = event.target.value.trim();
                if (!next || (next !== key && next in value)) {
                  event.target.value = key;
                  return;
                }
                if (next !== key) {
                  const copy = { ...value };
                  delete copy[key];
                  copy[next] = item;
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
                const copy = { ...value };
                delete copy[key];
                onChange(copy);
              }}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
          {typeof schema.additionalProperties === "object" ? (
            <SchemaValue
              id={`extra-${key}`}
              schema={asSchema(schema.additionalProperties)}
              value={item}
              onChange={(next) => onChange({ ...value, [key]: next })}
              disabled={disabled}
              depth={depth + 1}
            />
          ) : (
            <GraphValueInput
              value={item}
              onChange={(next) => onChange({ ...value, [key]: next })}
              disabled={disabled}
              depth={depth + 1}
            />
          )}
        </div>
      ))}
      {!Object.keys(properties).length && !extras.length && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.production.no_input_fields)}
        </p>
      )}
      {schema.additionalProperties !== false && (
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={disabled || Object.keys(value).length >= 128}
          onClick={() => {
            let index = 1;
            while (`field_${index}` in value || `field_${index}` in properties)
              index++;
            onChange({ ...value, [`field_${index}`]: "" });
          }}
        >
          <Plus className="size-3.5" />
          {t(($) => $.production.add_field)}
        </Button>
      )}
    </div>
  );
}

function SchemaValue({
  id,
  schema,
  value,
  onChange,
  disabled,
  depth,
}: {
  id: string;
  schema: JsonObject;
  value: unknown;
  onChange: (value: unknown) => void;
  disabled?: boolean;
  depth: number;
}) {
  const { t } = useT("graphs");
  if (Array.isArray(schema.enum))
    return (
      <NativeSelect
        id={id}
        value={value === undefined ? "" : JSON.stringify(value)}
        disabled={disabled}
        onChange={(event) => {
          onChange(
            event.target.value ? JSON.parse(event.target.value) : undefined,
          );
        }}
        className="w-full"
      >
        <option value="">{t(($) => $.production.choose_value)}</option>
        {schema.enum.map((item, index) => (
          <option key={index} value={JSON.stringify(item)}>
            {String(item)}
          </option>
        ))}
      </NativeSelect>
    );
  if (schema.type === "object")
    return (
      <div className="rounded-lg border p-3">
        <GraphSchemaForm
          schema={schema}
          value={asSchema(value)}
          onChange={onChange}
          disabled={disabled}
          depth={depth + 1}
        />
      </div>
    );
  if (schema.type === "array") {
    const values = Array.isArray(value) ? value : [];
    return (
      <div className="space-y-2">
        {values.map((item, index) => (
          <div className="flex items-start gap-2" key={index}>
            <div className="min-w-0 flex-1">
              <SchemaValue
                id={`${id}-${index}`}
                schema={asSchema(schema.items)}
                value={item}
                onChange={(next) =>
                  onChange(values.map((old, i) => (i === index ? next : old)))
                }
                disabled={disabled}
                depth={depth + 1}
              />
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              disabled={disabled}
              aria-label={t(($) => $.production.remove_field)}
              onClick={() => onChange(values.filter((_, i) => i !== index))}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
        ))}
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={
            disabled ||
            values.length >=
              (typeof schema.maxItems === "number"
                ? Math.min(schema.maxItems, 1000)
                : 1000)
          }
          onClick={() =>
            onChange([...values, initialValue(asSchema(schema.items))])
          }
        >
          <Plus className="size-3.5" />
          {t(($) => $.production.add_item)}
        </Button>
      </div>
    );
  }
  if (schema.type === "boolean")
    return (
      <NativeSelect
        id={id}
        className="w-full"
        disabled={disabled}
        value={value === undefined ? "" : String(value)}
        onChange={(event) =>
          onChange(
            event.target.value === ""
              ? undefined
              : event.target.value === "true",
          )
        }
      >
        <option value="">{t(($) => $.production.choose_value)}</option>
        <option value="true">{t(($) => $.production.yes)}</option>
        <option value="false">{t(($) => $.production.no)}</option>
      </NativeSelect>
    );
  if (schema.type === "integer" || schema.type === "number")
    return (
      <Input
        id={id}
        type="number"
        step={schema.type === "integer" ? 1 : "any"}
        value={typeof value === "number" ? value : ""}
        disabled={disabled}
        min={typeof schema.minimum === "number" ? schema.minimum : undefined}
        max={typeof schema.maximum === "number" ? schema.maximum : undefined}
        onChange={(event) =>
          onChange(
            event.target.value === "" ? undefined : Number(event.target.value),
          )
        }
      />
    );
  return (
    <Textarea
      id={id}
      disabled={disabled}
      className="min-h-20"
      value={typeof value === "string" ? value : ""}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

export function GraphSchemaBuilder({
  schema,
  onChange,
  disabled,
  depth = 0,
}: {
  schema: JsonObject;
  onChange: (schema: JsonObject) => void;
  disabled?: boolean;
  depth?: number;
}) {
  const { t } = useT("graphs");
  const properties = asSchema(schema.properties),
    required = Array.isArray(schema.required) ? schema.required : [];
  const update = (key: string, next: JsonObject) =>
    onChange({
      ...schema,
      type: "object",
      properties: { ...properties, [key]: next },
    });
  return (
    <div className="space-y-3">
      {Object.entries(properties).map(([key, item]) => {
        const field = asSchema(item);
        return (
          <div key={key} className="space-y-3 rounded-lg border p-3">
            <div className="flex gap-2">
              <Input
                aria-label={t(($) => $.production.field_name)}
                defaultValue={key}
                disabled={disabled}
                onBlur={(event) => {
                  const next = event.target.value.trim();
                  if (!next || (next !== key && properties[next])) {
                    event.target.value = key;
                    return;
                  }
                  if (next !== key) {
                    const copy = { ...properties };
                    delete copy[key];
                    copy[next] = field;
                    onChange({
                      ...schema,
                      properties: copy,
                      required: required.map((name) =>
                        name === key ? next : name,
                      ),
                    });
                  }
                }}
              />
              <NativeSelect
                aria-label={t(($) => $.production.field_type)}
                value={String(field.type ?? "string")}
                disabled={disabled}
                onChange={(event) =>
                  update(key, {
                    type: event.target.value,
                    ...(field.description
                      ? { description: field.description }
                      : {}),
                    ...(field.title ? { title: field.title } : {}),
                    ...(event.target.value === "array"
                      ? { items: { type: "string" } }
                      : {}),
                  })
                }
              >
                {[
                  "string",
                  "number",
                  "integer",
                  "boolean",
                  "object",
                  "array",
                ].map((type) => (
                  <option key={type} value={type}>
                    {type}
                  </option>
                ))}
              </NativeSelect>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.production.remove_field)}
                disabled={disabled}
                onClick={() => {
                  const copy = { ...properties };
                  delete copy[key];
                  onChange({
                    ...schema,
                    properties: copy,
                    required: required.filter((name) => name !== key),
                  });
                }}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
            <Input
              aria-label={t(($) => $.production.field_description)}
              placeholder={t(($) => $.production.field_description)}
              value={String(field.description ?? "")}
              disabled={disabled}
              onChange={(event) =>
                update(key, { ...field, description: event.target.value })
              }
            />
            <label className="flex items-center gap-2 text-caption">
              <input
                type="checkbox"
                disabled={disabled}
                checked={required.includes(key)}
                onChange={(event) =>
                  onChange({
                    ...schema,
                    required: event.target.checked
                      ? [...required, key]
                      : required.filter((name) => name !== key),
                  })
                }
              />
              {t(($) => $.production.required)}
            </label>
            <SchemaConstraints
              schema={field}
              onChange={(next) => update(key, next)}
              disabled={disabled}
            />
            {field.type === "object" && depth < 8 && (
              <GraphSchemaBuilder
                schema={field}
                disabled={disabled}
                onChange={(next) => update(key, next)}
                depth={depth + 1}
              />
            )}
            {field.type === "array" && (
              <Field label={t(($) => $.production.item_type)}>
                {(id) => (
                  <NativeSelect
                    id={id}
                    disabled={disabled}
                    value={String(asSchema(field.items).type ?? "string")}
                    onChange={(event) =>
                      update(key, {
                        ...field,
                        items: { type: event.target.value },
                      })
                    }
                  >
                    {["string", "number", "integer", "boolean", "object"].map(
                      (type) => (
                        <option key={type}>{type}</option>
                      ),
                    )}
                  </NativeSelect>
                )}
              </Field>
            )}
            {field.type === "array" &&
              asSchema(field.items).type === "object" &&
              depth < 8 && (
                <GraphSchemaBuilder
                  schema={asSchema(field.items)}
                  disabled={disabled}
                  onChange={(items) => update(key, { ...field, items })}
                  depth={depth + 1}
                />
              )}
          </div>
        );
      })}
      <Button
        type="button"
        size="sm"
        variant="outline"
        disabled={disabled || Object.keys(properties).length >= 128}
        onClick={() => {
          let index = Object.keys(properties).length + 1;
          while (properties[`field_${index}`]) index++;
          onChange({
            ...schema,
            type: "object",
            properties: {
              ...properties,
              [`field_${index}`]: { type: "string" },
            },
          });
        }}
      >
        <Plus className="size-3.5" />
        {t(($) => $.production.add_field)}
      </Button>
    </div>
  );
}

/** Typed values for open schemas and constant bindings; nested values never become JSON strings. */
export function GraphValueInput({
  value,
  onChange,
  disabled,
  optional,
  depth = 0,
}: {
  value: unknown;
  onChange: (value: unknown) => void;
  disabled?: boolean;
  optional?: boolean;
  depth?: number;
}) {
  const { t } = useT("graphs");
  const type =
    value === null
      ? "null"
      : value === undefined
        ? "unset"
        : Array.isArray(value)
          ? "array"
          : typeof value;
  const labels: Record<string, string> = {
    string: t(($) => $.production.text_value),
    number: t(($) => $.production.number_value),
    boolean: t(($) => $.production.boolean_value),
    object: t(($) => $.production.object),
    array: t(($) => $.production.array_value),
    null: t(($) => $.production.null_value),
    unset: t(($) => $.production.no_default),
  };
  if (depth > 8)
    return (
      <pre className="overflow-auto whitespace-pre-wrap break-all text-caption">
        {JSON.stringify(value)}
      </pre>
    );
  return (
    <div className="space-y-2">
      <NativeSelect
        className="w-full"
        aria-label={t(($) => $.production.value_type)}
        disabled={disabled}
        value={type}
        onChange={(event) => {
          const next = event.target.value;
          onChange(
            next === "object"
              ? {}
              : next === "array"
                ? []
                : next === "boolean"
                  ? false
                  : next === "number"
                    ? 0
                    : next === "null"
                      ? null
                      : next === "unset"
                        ? undefined
                        : "",
          );
        }}
      >
        {[
          ...(optional ? ["unset"] : []),
          "string",
          "number",
          "boolean",
          "object",
          "array",
          "null",
        ].map((kind) => (
          <option key={kind} value={kind}>
            {labels[kind]}
          </option>
        ))}
      </NativeSelect>
      {type === "object" ? (
        <GraphSchemaForm
          schema={{ type: "object" }}
          value={asSchema(value)}
          onChange={onChange}
          disabled={disabled}
          depth={depth + 1}
        />
      ) : type === "array" ? (
        <div className="space-y-3">
          {(value as unknown[]).map((item, index) => (
            <div className="flex gap-2 rounded-lg border p-2" key={index}>
              <div className="min-w-0 flex-1">
                <GraphValueInput
                  value={item}
                  onChange={(next) =>
                    onChange(
                      (value as unknown[]).map((old, i) =>
                        i === index ? next : old,
                      ),
                    )
                  }
                  disabled={disabled}
                  depth={depth + 1}
                />
              </div>
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                aria-label={t(($) => $.production.remove_field)}
                disabled={disabled}
                onClick={() =>
                  onChange((value as unknown[]).filter((_, i) => i !== index))
                }
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled || (value as unknown[]).length >= 1000}
            onClick={() => onChange([...(value as unknown[]), ""])}
          >
            {t(($) => $.production.add_item)}
          </Button>
        </div>
      ) : type === "boolean" ? (
        <NativeSelect
          className="w-full"
          aria-label={t(($) => $.production.value)}
          disabled={disabled}
          value={String(value)}
          onChange={(event) => onChange(event.target.value === "true")}
        >
          <option value="true">{t(($) => $.production.yes)}</option>
          <option value="false">{t(($) => $.production.no)}</option>
        </NativeSelect>
      ) : type === "number" || type === "string" ? (
        <Input
          aria-label={t(($) => $.production.value)}
          disabled={disabled}
          type={type === "number" ? "number" : "text"}
          value={value as string | number}
          onChange={(event) =>
            onChange(
              type === "number"
                ? event.target.value === ""
                  ? undefined
                  : Number(event.target.value)
                : event.target.value,
            )
          }
        />
      ) : null}
    </div>
  );
}

function initialValue(schema: JsonObject): unknown {
  if (schema.default !== undefined) return schema.default;
  return schema.type === "object"
    ? {}
    : schema.type === "array"
      ? []
      : schema.type === "boolean"
        ? false
        : ["number", "integer"].includes(String(schema.type))
          ? 0
          : "";
}
function SchemaConstraints({
  schema,
  onChange,
  disabled,
}: {
  schema: JsonObject;
  onChange: (value: JsonObject) => void;
  disabled?: boolean;
}) {
  const { t } = useT("graphs");
  const minimum =
    schema.type === "string"
      ? "minLength"
      : schema.type === "array"
        ? "minItems"
        : "minimum";
  const maximum =
    schema.type === "string"
      ? "maxLength"
      : schema.type === "array"
        ? "maxItems"
        : "maximum";
  return (
    <details className="rounded-lg bg-muted/30 p-3">
      <summary className="cursor-pointer text-caption font-medium">
        {t(($) => $.production.constraints)}
      </summary>
      <div className="mt-3 space-y-3">
        <label className="flex items-center gap-2 text-caption">
          <input
            type="checkbox"
            checked={schema.default !== undefined}
            disabled={disabled}
            onChange={(event) =>
              onChange({
                ...schema,
                default: event.target.checked
                  ? initialValue(schema)
                  : undefined,
              })
            }
          />
          {t(($) => $.production.default_value)}
        </label>
        {schema.default !== undefined && (
          <Field label={t(($) => $.production.default_value)}>
            {(id) => (
              <SchemaValue
                id={id}
                schema={{ ...schema, enum: undefined }}
                value={schema.default}
                onChange={(value) => onChange({ ...schema, default: value })}
                disabled={disabled}
                depth={0}
              />
            )}
          </Field>
        )}
        {["string", "number", "integer", "array"].includes(
          String(schema.type),
        ) && (
          <div className="grid grid-cols-2 gap-2">
            <Field label={t(($) => $.production.minimum)}>
              {(id) => (
                <Input
                  id={id}
                  type="number"
                  value={
                    typeof schema[minimum] === "number"
                      ? (schema[minimum] as number)
                      : ""
                  }
                  disabled={disabled}
                  onChange={(event) =>
                    onChange({
                      ...schema,
                      [minimum]:
                        event.target.value === ""
                          ? undefined
                          : Number(event.target.value),
                    })
                  }
                />
              )}
            </Field>
            <Field label={t(($) => $.production.maximum)}>
              {(id) => (
                <Input
                  id={id}
                  type="number"
                  value={
                    typeof schema[maximum] === "number"
                      ? (schema[maximum] as number)
                      : ""
                  }
                  disabled={disabled}
                  onChange={(event) =>
                    onChange({
                      ...schema,
                      [maximum]:
                        event.target.value === ""
                          ? undefined
                          : Number(event.target.value),
                    })
                  }
                />
              )}
            </Field>
          </div>
        )}
        {["string", "number", "integer", "boolean"].includes(
          String(schema.type),
        ) && (
          <div className="space-y-2">
            <p className="text-caption font-medium">
              {t(($) => $.production.allowed_values)}
            </p>
            {(Array.isArray(schema.enum) ? schema.enum : []).map(
              (value, index) => (
                <div className="flex gap-2" key={index}>
                  <div className="min-w-0 flex-1">
                    <SchemaValue
                      id={`enum-${index}`}
                      schema={{
                        ...schema,
                        title: `${t(($) => $.production.allowed_values)} ${index + 1}`,
                        enum: undefined,
                      }}
                      value={value}
                      onChange={(next) =>
                        onChange({
                          ...schema,
                          enum: (schema.enum as unknown[]).map((old, i) =>
                            i === index ? next : old,
                          ),
                        })
                      }
                      disabled={disabled}
                      depth={0}
                    />
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t(($) => $.production.remove_field)}
                    disabled={disabled}
                    onClick={() => {
                      const values = (schema.enum as unknown[]).filter(
                        (_, i) => i !== index,
                      );
                      onChange({
                        ...schema,
                        enum: values.length ? values : undefined,
                      });
                    }}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              ),
            )}
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={disabled}
              onClick={() =>
                onChange({
                  ...schema,
                  enum: [
                    ...(Array.isArray(schema.enum) ? schema.enum : []),
                    initialValue({ type: schema.type }),
                  ],
                })
              }
            >
              {t(($) => $.production.add_item)}
            </Button>
          </div>
        )}
      </div>
    </details>
  );
}
