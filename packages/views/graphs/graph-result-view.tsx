"use client";
import { useState } from "react";
import { toast } from "sonner";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  FileText,
  GitCommitHorizontal,
} from "lucide-react";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
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

/** A string is short enough to sit in a key–value row beside its label. */
function isShortScalar(value: unknown): boolean {
  if (typeof value === "number" || typeof value === "boolean") return true;
  return (
    typeof value === "string" &&
    value.length <= 60 &&
    !value.includes("\n") &&
    !parseGraphUrl(value)
  );
}

/** Copies a value: text as itself, anything else as indented JSON. */
export function CopyValueButton({
  value,
  className,
}: {
  value: unknown;
  className?: string;
}) {
  const { t } = useT("graphs");
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      aria-label={t(($) => $.studio.ux.copy)}
      title={t(($) => $.studio.ux.copy)}
      className={cn(
        "inline-flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
        className,
      )}
      onClick={async () => {
        const text =
          typeof value === "string" ? value : JSON.stringify(value, null, 2);
        if (await copyText(text)) {
          setDone(true);
          toast.success(t(($) => $.editor.copied));
          window.setTimeout(() => setDone(false), 1500);
        }
      }}
    >
      {done ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
    </button>
  );
}

/**
 * Agent prose, rendered as markdown. Anything longer than a few lines opens
 * folded with a fade, so one long field cannot push everything else off the
 * pane; the reader unfolds what they came for.
 */
function LongText({ text }: { text: string }) {
  const { t } = useT("graphs");
  const long = text.length > 320 || text.split("\n").length > 6;
  const [open, setOpen] = useState(!long);
  return (
    <div className="min-w-0">
      <div
        className={cn(
          "min-w-0 overflow-hidden",
          !open && "max-h-40 [mask-image:linear-gradient(to_bottom,black_55%,transparent)]",
        )}
      >
        <ReadonlyContent
          content={text}
          className="min-w-0 break-words text-body leading-7"
        />
      </div>
      {long && (
        <button
          type="button"
          className="mt-1 inline-flex items-center gap-1 text-caption font-medium text-muted-foreground hover:text-foreground"
          onClick={() => setOpen((current) => !current)}
        >
          <ChevronDown
            className={cn("size-3.5 transition-transform", open && "rotate-180")}
          />
          {open ? t(($) => $.value.collapse) : t(($) => $.studio.ux.show_all)}
        </button>
      )}
    </div>
  );
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
        <ul className="min-w-0 space-y-2">
          {shown.map((item, index) => (
            <li
              key={index}
              className="relative min-w-0 ps-4 before:absolute before:left-0 before:top-[0.7em] before:size-1.5 before:rounded-full before:bg-muted-foreground/50"
            >
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
    return <LongText text={value} />;
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
  const properties = asSchema(schema.properties);
  // Short facts read as a compact key–value block; prose and collections get
  // a section each, with their label above and a copy button beside it.
  const facts = entries.filter(([, item]) => isShortScalar(item));
  const sections = entries.filter(([, item]) => !isShortScalar(item));
  return (
    <div className="min-w-0 space-y-5">
      {facts.length > 0 && (
        <dl className="grid min-w-0 grid-cols-[minmax(4rem,38%)_minmax(0,1fr)] gap-x-4 gap-y-2 rounded-lg bg-muted/40 px-3 py-2.5">
          {facts.map(([key, item]) => (
            <div key={key} className="contents">
              <dt className="line-clamp-2 break-words text-caption leading-5 text-muted-foreground">
                {fieldLabel(key, asSchema(properties[key]), labels)}
              </dt>
              <dd className="min-w-0 break-words text-body tabular-nums">
                {String(item)}
              </dd>
            </div>
          ))}
        </dl>
      )}
      {sections.map(([key, item]) => {
        const field = asSchema(properties[key]);
        return (
          <section key={key} className="group/field min-w-0">
            <div className="mb-1.5 flex items-center gap-2">
              <h4
                className={cn(
                  "min-w-0 flex-1 break-words font-medium",
                  depth === 0
                    ? "text-body text-foreground"
                    : "text-caption text-muted-foreground",
                )}
              >
                {fieldLabel(key, field, labels)}
              </h4>
              <CopyValueButton
                value={item}
                className="invisible group-hover/field:visible focus-visible:visible"
              />
            </div>
            <GraphValueView value={item} schema={field} depth={depth + 1} />
          </section>
        );
      })}
    </div>
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
