# Graph image input and independent file delivery validation

Validated on Windows amd64 on 2026-10-17 against the local Acceptance instance.

## Changes

- Freeze parent-issue attachment identities and metadata; authorize existing authenticated downloads and retain referenced input files.
- Require an actual server-verified current-task bundle for `produce_files`; check every marked bundle, including optional artifacts from text nodes.
- Freeze accepted ancestor artifacts into downstream manifests. Download and verify them into each independent attempt; do not inherit another task's working directory or preview process.
- Reject traversal, links, Windows path conflicts, explicit credential paths, oversized archives, altered hashes, and forged bundle markers. Keep ordinary ZIP attachments as ordinary files.
- Require `graph-file-delivery-v1` on all Graph runtimes before admission, with matching start-form upgrade diagnostics. Ordinary task/runtime authentication is unchanged.

## Real model run

- Run: `0ff5fdc5-42be-4eab-8192-14f89c6ce272`, issue `ACCE-49`.
- Version: `6b01f627-bbb3-4cdc-8250-e7cb855c9659`.
- Inputs: the actual 2226 by 1668 TABELA reference PNG and the recovered ordinary source ZIP.
- Producer task: `ebd66880-568e-4e2c-bcb0-d0984b197017`.
- Independent reviewer task: `ccf8b97e-2728-45f5-98e7-94bb622a1ebd`.
- All nodes and the run reached `succeeded`; both providers exited successfully and both delivery receipts were `delivered`.
- Code bundle: `01a148f4-b67f-7cae-8d2e-58a1d6315e95`, 124745 bytes, SHA-256 `24bd451415bb6d28443ded2e2e45fdc6b15948397f14be6adc0e67605048ac84`.
- Reviewer screenshot: `01a148f6-310f-719a-9dd4-5cbfe5602b60`, 245109 bytes, SHA-256 `9d973ab30df62de91994659b7bbbdaf3ad5e5b7876cfd2ca34e058149a7b8db8`.
- Reviewer downloaded and verified the bundle in its own workdir, started its own preview after the producer stopped its service, and captured/inspected a 2226 by 1668 desktop screenshot at DPR 1 and scale 1.
- HTTP page, CSS, JS, fonts and favicon returned 200. Measured scrollWidth/clientWidth were both 2226. The reviewer reported the remaining SVG, illustration and fine visual differences; this run validates delivery and independent page access, not final pixel-perfect product approval.

## Automated verification

- Go Graph, attachment, CLI Graph, bundle and result-contract regressions passed, including a live PostgreSQL state flow, current-task and workspace boundaries, immutable input retention and concurrent deletion.
- `go vet` passed for the affected CLI, graph, bundle, service, handler and daemon packages.
- Shared core Graph tests: 89 passed; shared Graph view tests: 91 passed. Both affected TypeScript packages typechecked.
- Next.js production build passed.
- Full Windows CLI-suite failures were compared against an untouched archive of HEAD `a2d0cb17c`: the same 43 failing test/subtest entries occurred in both trees, with no new entries. They concern pre-existing Windows HOME/profile and path-rendering assumptions. Targeted modified behavior passed; the full Windows suite is not reported as passing.
- Independent code review found no remaining blocking issues after per-artifact verification and cross-platform archive fixes.

The real run used the release-candidate build. Release CI and deployed verification are recorded separately in the release workflow and delivery report; no old frozen run is silently rewritten.
