"use client";

import { useId, useRef, type ReactNode } from "react";
import { useT } from "../i18n";
import { Badge } from "@multica/ui/components/ui/badge";
import { Label } from "@multica/ui/components/ui/label";
import { useCurrentMember } from "@multica/core/permissions";
import type { GraphActor } from "@multica/core/graphs";

export function Field({
  label,
  children,
}: {
  label: string;
  children: (id: string) => ReactNode;
}) {
  const id = useId();
  return (
    <div className="grid min-w-0 gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children(id)}
    </div>
  );
}

export function JsonDetails({
  label,
  value,
  open = false,
}: {
  label: string;
  value: unknown;
  open?: boolean;
}) {
  return (
    <details className="min-w-0 rounded-lg border p-3" open={open}>
      <summary className="cursor-pointer text-body font-medium">
        {label}
      </summary>
      <pre className="mt-3 max-h-96 overflow-auto whitespace-pre-wrap break-all text-caption text-muted-foreground">
        {JSON.stringify(value, null, 2)}
      </pre>
    </details>
  );
}

export function useGraphStateLabels(): Record<string, string> {
  const { t } = useT("graphs");
  return {
    active: t(($) => $.states.active),
    running: t(($) => $.states.running),
    waiting_approval: t(($) => $.states.waiting_approval),
    blocked: t(($) => $.states.blocked),
    succeeded: t(($) => $.states.succeeded),
    failed: t(($) => $.states.failed),
    rejected: t(($) => $.states.rejected),
    cancelled: t(($) => $.states.cancelled),
    pending: t(($) => $.states.pending),
    ready: t(($) => $.states.ready),
    skipped: t(($) => $.states.skipped),
    approved: t(($) => $.states.approved),
    run: t(($) => $.states.run),
    pause: t(($) => $.states.paused),
    paused: t(($) => $.states.paused),
    pause_requested: t(($) => $.states.pause_requested),
    cancel_requested: t(($) => $.states.cancel_requested),
    dispatched: t(($) => $.states.dispatched),
    queued: t(($) => $.states.queued),
    started: t(($) => $.states.started),
    completed: t(($) => $.states.completed),
    retry_wait: t(($) => $.states.retry_wait),
  };
}

export function GraphState({ state }: { state: string }) {
  const labels = useGraphStateLabels();
  return (
    <Badge variant="outline" className="max-w-full break-all">
      {labels[state] ?? state}
    </Badge>
  );
}

export function GraphError({ error }: { error: unknown }) {
  if (!error) return null;
  return (
    <p
      role="alert"
      className="break-words rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-body text-destructive"
    >
      {error instanceof Error ? error.message : String(error)}
    </p>
  );
}

export function useGraphActor(wsId: string): GraphActor {
  const membership = useCurrentMember(wsId);
  return {
    userId: membership.userId ?? "",
    memberId: membership.member?.id,
    role: membership.role ?? undefined,
  };
}

/** A retry of an unchanged command reuses its key after an ambiguous network failure. */
export function useGraphCommandKey() {
  const keys = useRef(new Map<string, string>());
  return (request: unknown) => {
    const fingerprint = JSON.stringify(request);
    let key = keys.current.get(fingerprint);
    if (!key) {
      key = crypto.randomUUID();
      keys.current.set(fingerprint, key);
    }
    return key;
  };
}
