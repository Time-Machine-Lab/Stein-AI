import type { JsonObject } from "./types";

export const asSchema = (value: unknown): JsonObject =>
  value && typeof value === "object" && !Array.isArray(value)
    ? (value as JsonObject)
    : {};
export function schemaDefaults(schema: JsonObject): JsonObject {
  return Object.fromEntries(
    Object.entries(asSchema(schema.properties)).flatMap(([key, value]) => {
      const field = asSchema(value);
      if (field.default !== undefined) return [[key, field.default]];
      if (field.type === "object") {
        const nested = schemaDefaults(field);
        return Object.keys(nested).length ? [[key, nested]] : [];
      }
      return [];
    }),
  );
}
export interface GraphInputError {
  path: string;
  code: string;
}
export function validateGraphInput(
  schema: JsonObject,
  value: unknown,
  path = "",
): GraphInputError[] {
  const errors: GraphInputError[] = [];
  const add = (code: string) => errors.push({ path, code });
  if (
    Array.isArray(schema.enum) &&
    !schema.enum.some((item) => JSON.stringify(item) === JSON.stringify(value))
  )
    add("enum");
  if (schema.type === "object") {
    if (!value || typeof value !== "object" || Array.isArray(value))
      return [{ path, code: "object" }];
    const object = value as JsonObject;
    for (const key of Array.isArray(schema.required) ? schema.required : [])
      if (
        typeof key === "string" &&
        (object[key] === undefined || object[key] === "")
      )
        errors.push({ path: path ? `${path}.${key}` : key, code: "required" });
    for (const [key, item] of Object.entries(asSchema(schema.properties)))
      if (object[key] !== undefined)
        errors.push(
          ...validateGraphInput(
            asSchema(item),
            object[key],
            path ? `${path}.${key}` : key,
          ),
        );
  } else if (schema.type === "array") {
    if (!Array.isArray(value)) return [{ path, code: "array" }];
    if (typeof schema.minItems === "number" && value.length < schema.minItems)
      add("minimum");
    if (typeof schema.maxItems === "number" && value.length > schema.maxItems)
      add("maximum");
    value.forEach((item, index) =>
      errors.push(
        ...validateGraphInput(
          asSchema(schema.items),
          item,
          `${path}[${index + 1}]`,
        ),
      ),
    );
  } else if (schema.type === "string") {
    if (typeof value !== "string") add("string");
    else {
      if (
        typeof schema.minLength === "number" &&
        value.length < schema.minLength
      )
        add("minimum");
      if (
        typeof schema.maxLength === "number" &&
        value.length > schema.maxLength
      )
        add("maximum");
    }
  } else if (schema.type === "number" || schema.type === "integer") {
    if (
      typeof value !== "number" ||
      !Number.isFinite(value) ||
      (schema.type === "integer" && !Number.isInteger(value))
    )
      add(String(schema.type));
    else {
      if (typeof schema.minimum === "number" && value < schema.minimum)
        add("minimum");
      if (typeof schema.maximum === "number" && value > schema.maximum)
        add("maximum");
    }
  } else if (schema.type === "boolean" && typeof value !== "boolean")
    add("boolean");
  return errors;
}
export function schemaFields(
  schema: JsonObject,
  path = "",
  depth = 0,
): { path: string; type: string; title: string }[] {
  if (depth > 12) return [];
  const fields = [
    {
      path,
      type: typeof schema.type === "string" ? schema.type : "unknown",
      title: typeof schema.title === "string" ? schema.title : path || "/",
    },
  ];
  for (const [key, value] of Object.entries(asSchema(schema.properties)))
    fields.push(
      ...schemaFields(
        asSchema(value),
        `${path}/${key.replaceAll("~", "~0").replaceAll("/", "~1")}`,
        depth + 1,
      ),
    );
  return fields;
}
