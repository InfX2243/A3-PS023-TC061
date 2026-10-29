# Task Plan — One Verifiable Task at a Time

## How to use this file
Work strictly from top to bottom unless a dependency requires a documented change. Select exactly one unchecked task, implement it, run its acceptance checks, and update this file with evidence before starting the next task.

- [ ] means not yet verified.
- [x] means completed and supported by a commit, test output, or deployment evidence.
- Do not mark a task complete because code was generated; tests and acceptance criteria must pass.
- Keep tasks small enough for one focused AI-assisted change and one review.
- Add task IDs to commits/PRs, e.g. `T-03: add memory placement policy`.

## Execution note (2026-10-29)

The user explicitly authorized continuing without waiting for environment verification. T-01 feasibility checks are therefore deferred, not passed; no KVM/AWS runtime evidence is available. The existing design proposes Go, a controller-served lightweight UI, SQLite, and systemd, so a provisional Go foundation was added while the feasibility/version/image decisions remain open. T-03 remains unchecked until a Go build/test run verifies the new scaffold.

## Phase 0 — Feasibility and repository baseline

### T-00 — Inspect repository and record baseline
- [x] Inspect current files, language, build/test commands, and existing implementation.
- [x] Record the existing repository conventions; do not overwrite existing code without understanding it.
- [x] Add or update a short development setup section.
- Acceptance: a clean checkout can be inspected and current build/test status is recorded.
- Evidence: README development setup section added in commit `f5663a78c2fda96bc288f550e39794cab83adad1`. Inspected `README.md`, `src/README.md`, and this task plan on `docs/mvp-requirements-design-task-plan`. The inspected source directory contains only a placeholder; no application source, build manifest, or test configuration was found. No build/test command could be run, and this is explicitly recorded as unavailable rather than passing. KVM/Cloud Hypervisor runtime verification remains T-01.
- Depends on: none.

### T-01 — Verify KVM and Cloud Hypervisor feasibility
- [ ] Choose candidate worker environment(s). **Blocked:** the intended worker target (specific existing Linux hosts vs AWS instance type/region) has not been supplied or selected.
- [ ] Record OS/kernel, CPU virtualization support, `/dev/kvm` permissions, Cloud Hypervisor version, and guest image. On each proposed Linux worker, capture `uname -a`, `lscpu`, `ls -l /dev/kvm`, `cloud-hypervisor --version`, and the exact guest-image identifier/checksum.
- [ ] Boot and stop the exact guest image on each intended worker. Record the exact command/config, successful status query, and shutdown result; unit tests do not substitute for this.
- [ ] Test AWS candidate instance type before creating long-lived resources. Do not launch long-lived resources until a short-lived candidate proves usable `/dev/kvm` and boots the chosen guest.
- Status: **not verified**. No worker host, AWS instance type/region, KVM device, hypervisor version, or guest-boot evidence has been provided. macOS development-machine tests do not establish Linux worker feasibility.
- Acceptance: evidence demonstrates the intended deployment path works, or a documented fallback is selected.
- Depends on: T-00.

### T-02 — Lock MVP decisions
- [x] Confirm language/toolchain and minimal UI approach from repository state: Go 1.23+; UI will be served by the controller using server-rendered HTML or a small embedded frontend (documented in `docs/design.md`).
- [ ] Pin Cloud Hypervisor and guest image versions. This is blocked on T-01 selecting and testing the worker environment; no release/image has been verified.
- [x] Document provisional vCPU/memory limits, safety reserve, allowed image IDs, and prototype auth approach: `config.example.json` sets max 8 vCPUs, max 8192 MiB, 1024 MiB reserve, image ID `ubuntu-demo`; worker and operator bearer-token secrets are environment-configured. These are prototype defaults, not host-validated final limits.
- Acceptance: configuration defaults and decisions are documented; no critical unknown blocks implementation.
- Status: **incomplete** because T-01 feasibility and pinned Cloud Hypervisor/guest-image versions remain unresolved. Do not advance to dependent implementation tasks until resolved or explicitly re-scoped with the user.
- Depends on: T-00, T-01.

## Phase 1 — Foundation

### T-03 — Establish project layout and configuration
- Evidence: user-reported local `go test ./...` passed for controller, config, domain, store, and workerhealth packages before the T-07 test was added. User also verified that the controller starts with `config.example.json` and `GET /healthz` returns HTTP 200 with `{"status":"ok"}`. Configuration validation is covered by passing config tests.
- [x] Add config loading, validation, logging, and graceful startup/shutdown.
- [x] Add example non-secret configuration and ignore real secrets.
- [x] Add `/healthz` and startup validation.
- Acceptance: service starts with valid config, fails clearly with invalid config, and health check passes.
- Status: **complete for the current foundation** based on user-provided test and smoke-test evidence.
- Depends on: T-02.

### T-04 — Define domain models and state transitions
- Evidence: user-reported local `go test ./...` passed `internal/domain` tests.
- [x] Define VM, worker, reservation, and operation models.
- [x] Encode allowed lifecycle transitions.
- [x] Add unit tests for valid and invalid transitions.
- Acceptance: state transition tests pass and invalid transitions return explicit errors.
- Status: **complete** based on the reported passing test suite.
- Depends on: T-03.

### T-05 — Add SQLite persistence and migrations
- Evidence: user-reported local `go test ./...` passed `internal/store` tests, covering migration/restart persistence, uniqueness, rollback, and serialized concurrent transactions.
- [x] Add tables and unique constraints for VMs, workers, operations, and reservations.
- [x] Add migration/versioning strategy and transaction helpers.
- [x] Test restart persistence and concurrent reservation transaction behavior.
- Acceptance: tests show records survive restart and constraints prevent duplicate idempotency keys.
- Status: **complete for the current schema and store tests**. The controller does not yet use this store at runtime; integration is a later task.
- Depends on: T-04.

## Phase 2 — Worker vertical slice

### T-06 — Implement worker health and inventory
- Evidence: user-reported local `go test ./...` passed `internal/workerhealth` tests. This verifies the in-memory registry and authenticated HTTP handler; it does not verify a deployed worker agent, probe loop, or persistent worker inventory.
- [x] Report worker ID, heartbeat, available memory, configured cap, and Cloud Hypervisor health.
- [x] Add worker authentication.
- [x] Mark stale heartbeat as unavailable using a documented threshold (30 seconds).
- Acceptance: controller can distinguish healthy, stale, and unreachable workers.
- Status: **complete for the registry/HTTP-handler scope**. Persistent integration and a real worker agent remain future work.
- Depends on: T-03, T-04.

### T-07 — Implement local Cloud Hypervisor adapter
- Implementation was started prematurely before its T-01/T-02 dependencies were resolved. Existing code is retained, but **no further T-07 work should proceed until the feasibility and version/image decisions are resolved**.
- Test failure reported by user: `TestLifecycleEndpointsUseExpectedMethodsAndPaths` could not bind the Unix socket because macOS's temporary directory path exceeded the platform's Unix-socket path limit. A fix now creates the test socket under `/tmp` with a short path; it has **not yet been rerun by the user**.
- [ ] Verify API endpoints and Unix-socket behavior against a selected, pinned Cloud Hypervisor release.
- [x] Implement typed calls for VM prepare/create, start, stop, and status (implementation exists; tests currently not yet passing on the reported run).
- [x] Add bounded timeouts and structured errors (implementation exists; tests currently not yet passing on the reported run).
- Acceptance: adapter tests pass against a fake API and actual host smoke test succeeds.
- Status: **blocked by T-01/T-02 and adapter test verification**. No host runtime evidence exists.
- Depends on: T-01, T-02, T-03.

### T-08 — Implement worker VM lifecycle and process ownership
- [ ] Accept authenticated, validated commands with operation and VM IDs.
- [ ] Enforce server-side image allowlist and fixed storage locations.
- [ ] Ensure child processes are supervised and reaped.
- [ ] Make duplicate commands safe by checking existing VM/operation state.
- Acceptance: create/start/stop/status work on one host; repeated commands do not spawn duplicate VM processes.
- Depends on: T-06, T-07.

### T-09 — Add worker integration tests
- [ ] Test unavailable VMM, invalid image, timeout, process exit, and worker restart cases.
- [ ] Record host-specific test steps.
- Acceptance: worker tests pass; failures return actionable, non-secret errors.
- Depends on: T-08.

## Phase 3 — Controller and placement

### T-10 — Implement controller API contracts
- [ ] Add versioned endpoints for workers, VMs, operations, create, start, and stop.
- [ ] Add request validation, auth middleware, request IDs, and stable error payloads.
- Acceptance: API contract tests cover success, validation, auth, and not-found paths.
- Depends on: T-04, T-05.

### T-11 — Implement memory placement and atomic reservations
- [ ] Implement safe-capacity calculation in bytes.
- [ ] Reject stale/unhealthy workers and insufficient capacity.
- [ ] Select eligible worker with greatest safe capacity and deterministic tie-break.
- [ ] Reserve capacity inside a transaction before dispatch.
- Acceptance: unit and concurrency tests prove no tracked over-reservation under simultaneous create requests.
- Depends on: T-05, T-06, T-10.

### T-12 — Connect controller to workers
- [ ] Add authenticated worker client with bounded timeouts.
- [ ] Persist operation before dispatch.
- [ ] Dispatch create/start/stop and persist confirmed outcomes.
- Acceptance: one VM can be provisioned through the controller and worker end-to-end.
- Depends on: T-08, T-10, T-11.

### T-13 — Implement idempotency and uncertain outcomes
- [ ] Require idempotency key for VM create.
- [ ] Persist operation identity and dispatch state.
- [ ] On timeout, mark outcome unknown and reconcile before retry.
- [ ] Do not release reservations until failure/non-execution is established.
- Acceptance: a repeated create request cannot create a duplicate; lost-response test leaves a recoverable state.
- Depends on: T-05, T-12.

### T-14 — Add reconciliation and restart recovery
- [ ] On startup, inspect unfinished operations.
- [ ] Query workers for actual VM/process state.
- [ ] Repair metadata only when evidence supports the transition; flag unresolved cases.
- Acceptance: controller restart and worker response-loss tests do not silently corrupt state or free uncertain reservations.
- Depends on: T-13.

## Phase 4 — Provisioning interface

### T-15 — Build minimal web UI shell
- [ ] Serve UI from controller.
- [ ] Add login/auth flow consistent with API auth.
- [ ] Show controller health, worker health, and capacity.
- Acceptance: UI loads over the intended protected endpoint and shows both workers.
- Depends on: T-10.

### T-16 — Add VM provisioning form and VM list
- [ ] Form fields: name, vCPU, memory, approved image ID.
- [ ] Show validation errors and selected host.
- [ ] List VM ID/name/host/resources/state and operation status.
- Acceptance: operator can create a VM from the browser and observe operation progress.
- Depends on: T-12, T-15.

### T-17 — Add start/stop/status UI actions
- [ ] Add lifecycle controls with disabled states for invalid transitions.
- [ ] Poll or refresh operation state safely.
- [ ] Display failure and uncertain states clearly.
- Acceptance: complete create/start/stop/list journey is usable without manual API calls.
- Depends on: T-13, T-14, T-16.

## Phase 5 — Security and two-node validation

### T-18 — Harden security and operational defaults
- [ ] Restrict worker network ingress to controller.
- [ ] Restrict VMM socket permissions.
- [ ] Check config/secret file permissions, input bounds, log redaction, and safe process invocation.
- Acceptance: security checklist passes; worker/VMM APIs are not public.
- Depends on: T-08, T-10, T-15.

### T-19 — Deploy and register two workers
- [ ] Deploy controller and two agents in the chosen environment.
- [ ] Register both workers and verify heartbeat/capacity.
- [ ] Validate approved guest image on both hosts.
- Acceptance: both workers are healthy and can run the test guest.
- Depends on: T-01, T-14, T-18.

### T-20 — Run end-to-end and fault tests
- [ ] Test create/start/stop/list across both hosts.
- [ ] Test insufficient memory, concurrent requests, repeated idempotency key, worker outage, lost response, and controller restart.
- [ ] Capture commands, logs, test results, and known limitations.
- Acceptance: all required scenarios pass or have a clearly documented blocker with no false success claims.
- Depends on: T-17, T-19.

### T-21 — Package demo and teardown
- [ ] Write repeatable demo steps and screenshots/log evidence.
- [ ] Record exact versions and deployment configuration without secrets.
- [ ] Verify teardown removes billable prototype resources without deleting required evidence.
- Acceptance: another team member can reproduce the demo and shut down the environment safely.
- Depends on: T-20.

## Definition of done for every task

- Scope is limited to the selected task.
- Code is formatted and relevant tests pass.
- New behavior has tests; failures and edge cases are handled.
- No secrets, private keys, or real credentials are committed.
- README/docs are updated when behavior or commands change.
- The task is reviewed against its acceptance criteria.
- Commit/PR references and evidence are added here.
- The next task is not started until this one is complete or explicitly split.
