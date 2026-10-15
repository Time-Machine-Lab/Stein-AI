"use client";
import { useState } from "react";
import { ChevronRight, FileText, GitCommitHorizontal } from "lucide-react";
import {
  asSchema,
  type JsonObject,
  type GraphRepositoryOption,
} from "@multica/core/graphs";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../navigation";
import { ReadonlyContent } from "../editor/readonly-content";
import { useT } from "../i18n";
import { JsonDetails } from "./shared";

export {
  graphArtifactLink,
  parseGraphUrl,
} from "@multica/core/graphs/artifacts";
import { graphArtifactLink, parseGraphUrl } from "@multica/core/graphs/artifacts";

/**
 * A collection longer than this is folded. Fifty-seven sources are context, not
 * the answer, and rendering each as its own bordered row is what turned a run
 * page into ninety screens. A few leading entries stay visible so the reader
 * can see what kind of thing the list holds.
 */
const FOLD_AT = 8;
const FOLD_PREVIEW = 3;

function isPlainObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * A list of objects sharing a few keys is a table, not a stack of cards. Run
 * output is mostly this shape — findings, claims, metrics — so this one rule
 * removes the bulk of a long page.
 */
function tableColumns(items: unknown[]): string[] | null {
  if (items.length < 3) return null;
  const keys: string[] = [];
  for (const item of items) {
    if (!isPlainObject(item)) return null;
    for (const key of Object.keys(item)) {
      if (!keys.includes(key)) keys.push(key);
    }
  }
  return keys.length >= 2 && keys.length <= 6 ? keys : null;
}

/**
 * Scalars before collections, order otherwise preserved. A report is read
 * headline-first: the conclusion and the caveats are what the reader came for,
 * and the evidence lists are the appendix. Agents emit these keys in whatever
 * order they composed them, which is how a conclusion ends up below four
 * thousand pixels of URLs.
 */
function orderEntries(entries: [string, unknown][]): [string, unknown][] {
  const rank = (value: unknown) => (isPlainObject(value) || Array.isArray(value) ? 1 : 0);
  return [...entries].sort(([, a], [, b]) => rank(a) - rank(b));
}

function useResultLabels(): Record<string, string> {
  const { t } = useT("graphs");
  return {
    summary: t(($) => $.result_labels.summary),
    results: t(($) => $.result_labels.results),
    findings: t(($) => $.result_labels.findings),
    evidence: t(($) => $.result_labels.evidence),
    recommendations: t(($) => $.result_labels.recommendations),
    details: t(($) => $.result_labels.details),
    input: t(($) => $.result_labels.input),
    output: t(($) => $.result_labels.output),
  };
}

function fieldLabel(
  key: string,
  field: JsonObject,
  labels: Record<string, string>,
): string {
  if (typeof field.title === "string" && field.title) return field.title;
  if (typeof field.description === "string" && field.description)
    return field.description;
  return labels[key] ?? key.replaceAll("_", " ");
}

function Disclosure({
  open,
  count,
  onToggle,
}: {
  open: boolean;
  count: number;
  onToggle: () => void;
}) {
  const { t } = useT("graphs");
  return (
    <button
      type="button"
      className="inline-flex items-center gap-1 text-caption text-muted-foreground hover:text-foreground"
      onClick={onToggle}
    >
      <ChevronRight
        className={`size-3 transition-transform ${open ? "rotate-90" : ""}`}
      />
      {open
        ? t(($) => $.value.collapse)
        : t(($) => $.value.expand, { count })}
    </button>
  );
}

/** A cell has to survive being one row of a table: bounded, never a paragraph wall. */
function CellValue({ value }: { value: unknown }) {
  if (value === null || value === undefined)
    return <span className="text-muted-foreground">—</span>;
  if (
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  )
    return (
      <span className="line-clamp-6 whitespace-pre-wrap break-words">
        {String(value)}
      </span>
    );
  return (
    <code className="line-clamp-4 break-all font-mono text-caption text-muted-foreground">
      {JSON.stringify(value)}
    </code>
  );
}

function ValueTable({
  items,
  columns,
  schema,
}: {
  items: unknown[];
  columns: string[];
  schema: JsonObject;
}) {
  const labels = useResultLabels();
  const properties = asSchema(schema.properties);
  return (
    <div className="min-w-0 overflow-x-auto rounded-lg border">
      <table className="w-full border-collapse text-body">
        <thead>
          <tr className="border-b bg-muted/40">
            {columns.map((key) => (
              <th
                key={key}
                scope="col"
                className="px-3 py-2 text-left align-bottom text-caption font-medium text-muted-foreground"
              >
                {fieldLabel(key, asSchema(properties[key]), labels)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {items.map((item, index) => (
            <tr key={index} className="border-b align-top last:border-b-0">
              {columns.map((key) => (
                <td key={key} className="px-3 py-2">
                  <CellValue value={asSchema(item)[key]} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Objects that are not uniform enough for a table still read better as records than as cards. */
function ValueRecords({
  items,
  schema,
  depth,
}: {
  items: unknown[];
  schema: JsonObject;
  depth: number;
}) {
  return (
    <ol className="divide-y rounded-lg border">
      {items.map((item, index) => (
        <li key={index} className="px-3 py-2.5">
          <GraphValueView
            value={item}
            schema={asSchema(schema.items)}
            depth={depth + 2}
          />
        </li>
      ))}
    </ol>
  );
}

function ValueList({
  value,
  schema,
  depth,
}: {
  value: unknown[];
  schema: JsonObject;
  depth: number;
}) {
  const [open, setOpen] = useState(false);
  const folded = value.length > FOLD_AT;
  const shown = folded && !open ? value.slice(0, FOLD_PREVIEW) : value;
  // Columns come from the whole list, not the preview: a key that first appears
  // in a hidden row would otherwise lose its column and its values with it.
  const columns = tableColumns(value);
  return (
    <div className="min-w-0 space-y-2">
      {columns ? (
        <ValueTable items={shown} columns={columns} schema={schema} />
      ) : shown.every((item) => !isPlainObject(item) && !Array.isArray(item)) ? (
        <ul className="min-w-0 space-y-1">
          {shown.map((item, index) => (
            <li key={index} className="min-w-0">
              <GraphValueView
                value={item}
                schema={asSchema(schema.items)}
                depth={depth + 1}
              />
            </li>
          ))}
        </ul>
      ) : (
        <ValueRecords items={shown} schema={schema} depth={depth} />
      )}
      {folded && (
        <Disclosure
          open={open}
          count={value.length}
          onToggle={() => setOpen((current) => !current)}
        />
      )}
    </div>
  );
}

export function GraphValueView({
  value,
  schema = {},
  depth = 0,
}: {
  value: unknown;
  schema?: JsonObject;
  depth?: number;
}) {
  const labels = useResultLabels();
  if (value === null || value === undefined)
    return <span className="text-caption text-muted-foreground">—</span>;
  if (typeof value === "string") {
    const url = parseGraphUrl(value);
    if (url)
      return (
        <a
          href={url.toString()}
          target="_blank"
          rel="noopener noreferrer"
          className="break-all text-body underline underline-offset-4"
        >
          {value}
        </a>
      );
    return (
      <ReadonlyContent
        content={value}
        className="min-w-0 break-words text-body"
      />
    );
  }
  if (typeof value === "number" || typeof value === "boolean")
    return (
      <span className="break-words text-body tabular-nums">
        {String(value)}
      </span>
    );
  if (depth >= 8)
    return (
      <pre className="overflow-auto whitespace-pre-wrap break-all text-caption">
        {JSON.stringify(value, null, 2)}
      </pre>
    );
  if (Array.isArray(value)) {
    if (!value.length)
      return <span className="text-caption text-muted-foreground">—</span>;
    return <ValueList value={value} schema={schema} depth={depth} />;
  }
  const entries = orderEntries(Object.entries(asSchema(value)));
  if (!entries.length)
    return <span className="text-caption text-muted-foreground">—</span>;
  return (
    <dl className="space-y-4">
      {entries.map(([key, item]) => {
        const field = asSchema(asSchema(schema.properties)[key]);
        return (
          <div key={key} className="grid min-w-0 gap-1.5">
            <dt className="break-words text-caption font-medium text-muted-foreground">
              {fieldLabel(key, field, labels)}
            </dt>
            <dd className="min-w-0">
              <GraphValueView value={item} schema={field} depth={depth + 1} />
            </dd>
          </div>
        );
      })}
    </dl>
  );
}

export function GraphArtifactCards({
  artifacts,
  repositories = [],
}: {
  artifacts: JsonObject[];
  repositories?: GraphRepositoryOption[];
}) {
  const { t } = useT("graphs"),
    paths = useWorkspacePaths();
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      {artifacts.map((artifact, index) => {
        const isCommit = artifact.type === "git_commit",
          Icon = isCommit ? GitCommitHorizontal : FileText;
        const href = isCommit
          ? graphArtifactLink(artifact, repositories)
          : typeof artifact.id === "string" &&
              artifact.type === "attachment_ref"
            ? paths.attachmentPreview(artifact.id)
            : undefined;
        const title =
          typeof artifact.name === "string"
            ? artifact.name
            : isCommit
              ? `${String(artifact.commit_sha ?? "").slice(0, 12)}`
              : String(artifact.media_type ?? artifact.type ?? "");
        return (
          <article
            key={String(artifact.id ?? index)}
            className="space-y-3 rounded-xl border bg-background p-4"
          >
            <div className="flex items-center gap-2">
              <Icon className="size-4 shrink-0 text-muted-foreground" />
              <h3 className="min-w-0 flex-1 break-all text-body font-medium">
                {title}
              </h3>
            </div>
            {isCommit && (
              <p className="break-all text-caption text-muted-foreground">
                {repositories.find((repo) => repo.id === artifact.repository_id)
                  ?.name ?? String(artifact.managed_ref ?? "")}
              </p>
            )}
            {typeof artifact.size === "number" && (
              <p className="text-caption text-muted-foreground">
                {(artifact.size / 1024).toLocaleString(undefined, {
                  maximumFractionDigits: 1,
                })}{" "}
                KB
              </p>
            )}
            {href &&
              (isCommit ? (
                <a
                  className="inline-block text-caption underline underline-offset-4"
                  href={href}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {t(($) => $.production.open_artifact)}
                </a>
              ) : (
                <AppLink
                  href={href}
                  className="inline-block text-caption underline underline-offset-4"
                >
                  {t(($) => $.production.open_artifact)}
                </AppLink>
              ))}
            <JsonDetails
              label={t(($) => $.production.raw_details)}
              value={artifact}
            />
          </article>
        );
      })}
    </div>
  );
}
