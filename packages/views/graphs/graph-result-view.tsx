"use client";
import { FileText, GitCommitHorizontal } from "lucide-react";
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

export { graphArtifactLink } from "@multica/core/graphs/artifacts";
import { graphArtifactLink } from "@multica/core/graphs/artifacts";

export function GraphValueView({
  value,
  schema = {},
  depth = 0,
}: {
  value: unknown;
  schema?: JsonObject;
  depth?: number;
}) {
  const { t } = useT("graphs");
  const labels: Record<string, string> = {
    summary: t(($) => $.result_labels.summary),
    results: t(($) => $.result_labels.results),
    findings: t(($) => $.result_labels.findings),
    evidence: t(($) => $.result_labels.evidence),
    recommendations: t(($) => $.result_labels.recommendations),
    details: t(($) => $.result_labels.details),
    input: t(($) => $.result_labels.input),
    output: t(($) => $.result_labels.output),
  };
  if (value === null || value === undefined)
    return <span className="text-caption text-muted-foreground">—</span>;
  if (typeof value === "string")
    return (
      <ReadonlyContent
        content={value}
        className="min-w-0 break-words text-body"
      />
    );
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
  if (Array.isArray(value))
    return (
      <ol className="space-y-3">
        {value.map((item, index) => (
          <li key={index} className="rounded-lg border bg-background p-3">
            <GraphValueView
              value={item}
              schema={asSchema(schema.items)}
              depth={depth + 1}
            />
          </li>
        ))}
      </ol>
    );
  return (
    <dl className="space-y-3">
      {Object.entries(asSchema(value)).map(([key, item]) => {
        const field = asSchema(asSchema(schema.properties)[key]);
        const label =
          (typeof field.title === "string" && field.title) ||
          (typeof field.description === "string" && field.description) ||
          labels[key] ||
          key.replaceAll("_", " ");
        return (
          <div key={key} className="grid min-w-0 gap-1.5">
            <dt className="break-words text-caption font-medium text-muted-foreground">
              {label}
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
