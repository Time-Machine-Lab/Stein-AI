"use client";

import { useId, useRef, type ReactNode } from "react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Label } from "@multica/ui/components/ui/label";
import { useCurrentMember } from "@multica/core/permissions";
import type { GraphActor } from "@multica/core/graphs";

export function Field({ label, children }: { label: string; children: (id: string) => ReactNode }) {
  const id = useId();
  return <div className="grid min-w-0 gap-2"><Label htmlFor={id}>{label}</Label>{children(id)}</div>;
}

export function JsonDetails({ label, value, open = false }: { label: string; value: unknown; open?: boolean }) {
  return <details className="min-w-0 rounded-lg border p-3" open={open}>
    <summary className="cursor-pointer text-body font-medium">{label}</summary>
    <pre className="mt-3 max-h-96 overflow-auto whitespace-pre-wrap break-all text-caption text-muted-foreground">{JSON.stringify(value, null, 2)}</pre>
  </details>;
}

export function GraphState({ state }: { state: string }) {
  return <Badge variant="outline" className="max-w-full break-all">{state}</Badge>;
}

export function GraphError({ error }: { error: unknown }) {
  if (!error) return null;
  return <p role="alert" className="break-words rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-body text-destructive">{error instanceof Error ? error.message : String(error)}</p>;
}

export function useGraphActor(wsId: string): GraphActor {
  const membership = useCurrentMember(wsId);
  return { userId: membership.userId ?? "", memberId: membership.member?.id, role: membership.role ?? undefined };
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
