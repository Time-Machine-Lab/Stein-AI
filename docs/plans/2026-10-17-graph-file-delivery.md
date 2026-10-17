# Graph input images and file delivery implementation plan

> **For Claude:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task.

**Goal:** Read parent-task images and deliver implementation files to independent review attempts, then verify the real flow before releasing and replacing the local daemon.

**Architecture:** Keep existing runtime authentication, attachment storage, task tokens, and per-attempt directories. Add an explicit `produce_files` execution contract backed by a verified attachment bundle, and task-scoped CLI commands to package outputs and materialize accepted inputs. Review starts its own preview from downloaded files; process-local paths and localhost URLs are never durable delivery evidence.

**Tech Stack:** Go, PostgreSQL, existing attachment API, ZIP, shared TypeScript Graph editor, GitHub Actions.

The user has authorized execution, real implementation-to-review verification, push, deployment, release, and local replacement. Continue directly in the existing checkout containing the reviewed image-input fix; do not pause for another execution choice.

### Task 1: Freeze readable input images

Files: `server/internal/service/graph_input_attachment.go`, `graph_run.go`, handler `graph_scope.go`, migration `380_graph_input_attachment_retention.*.sql`, corresponding service/handler tests.

1. Keep the existing failing-then-passing image regressions: freeze identity/metadata, scoped download, decoding, retention, concurrent deletion.
2. Reuse existing authenticated attachment download; never freeze signed URLs or read later comments.
3. Run `go test ./internal/service ./internal/handler -run 'TestGraph.*Attachment|TestGraphInputImage|TestGraphPostgresFreez' -count=1` against isolated local test fixtures.

### Task 2: Enforce actual file delivery

Files: `server/internal/graph/{definition,result}.go`, service `graph_task.go`, CLI `cmd_graph*.go`, shared Graph editor, authoring grammar and builtin Graph instructions.

1. Add a failing result test: a `produce_files` node with a text output and no current-task bundle must be rejected.
2. Add `produce_files`, require a nonempty current-task file bundle artifact on pass, and expose accepted artifacts in task context.
3. Add `graph node deliver <directory>` using existing attachment upload, with bounded archives and symlink/private-path rejection.
4. Add `graph node download-inputs -o <directory>`: authenticated download, verify hash/size, safe extraction beneath a dedicated artifact directory, no archive traversal or link overwrite.
5. Test text-only rejection, forged/missing uploads, roundtrip, independent directories, corrupted bytes, archive traversal, empty bundle, stale/foreign task access, and preservation of ordinary text nodes.

### Task 3: Integrate authoring and runtime instructions

1. Make file-creation workflows use `produce_files` unless the user selected Git delivery; explain the mode in the shared editor.
2. Direct reviewers to materialize input bundles and start their own local preview, and producers to provide bundle artifacts before submitting.
3. Update built-in skill references and Graph design documentation; test authoring/runtime contracts.

### Task 4: Verify the full path

1. Run Go Graph/attachment tests, CLI tests, static checks, relevant shared frontend tests and production build.
2. Run a bounded real-agent Graph with a parent PNG and explicit producer bundle, then an independent review that downloads it, opens the page at the desktop viewport, and uploads screenshot evidence. Record run/task/artifact identities and actual results; no automatic human approval.
3. Obtain independent code review and fix blocking findings.

### Task 5: Release and replace

1. Commit only intended changes and push main through proxy `http://127.0.0.1:7890`.
2. Verify main CI, create the next patch tag, wait for release and production readiness.
3. Verify official Windows archive checksum; idle-check local daemons, back up executables, replace and restart preserving identity/config/arguments.
4. Verify deployed readiness, local version and runtime capabilities, then repeat a real flow against the release if needed to establish deployed behavior.
