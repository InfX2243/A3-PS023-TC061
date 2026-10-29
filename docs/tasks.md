# Task Plan — One Verifiable Task at a Time

## How to use this file
Implement tasks in dependency order and keep scope/task status explicit. The user has requested an implementation-first pass: continue implementing the remaining software tasks without waiting for local test execution or AWS deployment. Add tests with the code and record all checks that could not be run as pending; do not mark acceptance complete until verification evidence is available. Deployment-only gates must not block implementation of dependent software contracts.

- [ ] means not yet verified.
- [x] means completed and supported by a commit, test output, or deployment evidence.
- Do not mark a task complete because code was generated; tests and acceptance criteria must pass.
- Keep tasks small enough for one focused AI-assisted change and one review.
- Add task IDs to commits/PRs, e.g. `T-03: add memory placement policy`.

## Execution note (2026-10-29)

The user selected AWS EC2 and explicitly accepted AWS's official nested-virtualization documentation as the platform-support evidence: the linked AWS page lists C8i as supported, KVM as a supported L1 hypervisor, and `NestedVirtualization=enabled` as the launch option. This resolves the **architecture/platform support question**, not the instance-specific guest boot test. The working candidate remains one temporary `c8i.xlarge` in `ap-south-1`, Ubuntu Server 24.04.5 LTS x86-64 host, Cloud Hypervisor v54.0, and the versioned Ubuntu 24.04 LTS cloud image build dated 2026-10-17. The candidate choices and runtime evidence procedure are in [AWS worker feasibility gate](./deployment/aws-worker-validation.md). Per the user's decision, T-07 adapter-contract implementation may proceed against the pinned v54.0 OpenAPI contract; actual KVM/guest boot-stop, artifact checksum capture, and immutable image allowlisting remain mandatory deployment gates before two-worker rollout. No AWS instance has been launched and no guest has booted.

## Implementation-first execution policy (2026-10-29)\n\nPer explicit user direction, complete the software implementation across the task plan before pausing for AWS deployment or a full local verification session. Continue through code tasks in dependency order even if a test command or real-host check cannot currently run. Add focused tests alongside features, keep unexecuted checks unchecked, and do not claim they pass. T-01/T-02 runtime evidence remains required for deployment acceptance, not a prerequisite to write the remaining worker/controller/UI code. Once implementation is complete, run the full test and integration pass, fix failures, and then perform real-host/deployment validation.\n\n## Phase 0 — Feasibility and repository baseline

### T-00 — Inspect repository and record baseline
- [x] Inspect current files, language, build/test commands, and existing implementation.
- [x] Record the existing repository conventions; do not overwrite existing code without understanding it.
- [x] Add or update a short development setup section.
- Acceptance: a clean checkout can be inspected and current build/test status is recorded.
- Evidence: README development setup section added in commit `f5663a78c2fda96bc288f550e39794cab83adad1`. Inspected `README.md`, `src/README.md`, and this task plan on `docs/mvp-requirements-design-task-plan`. The inspected source directory contains only a placeholder; no application source, build manifest, or test configuration was found. No build/test command could be run, and this is explicitly recorded as unavailable rather than passing. KVM/Cloud Hypervisor runtime verification remains T-01.
- Depends on: none.

### T-01 — Verify KVM and Cloud Hypervisor feasibility
- [x] Confirm platform-level nested virtualization support from AWS's official documentation: C8i is listed as supported, KVM is a supported nested hypervisor, and launch-time enablement uses `NestedVirtualization=enabled` ([AWS documentation](https://docs.amazonaws.cn/en_us/AWSEC2/latest/UserGuide/amazon-ec2-nested-virtualization.html)). This is accepted as architecture feasibility evidence per user direction.
- [x] Select a specific AWS candidate for the short-lived feasibility run: one `c8i.xlarge` in `ap-south-1` (Mumbai).
- [ ] Record actual EC2 AMI ID, OS/kernel, CPU virtualization flags, `/dev/kvm` permissions, Cloud Hypervisor v54.0 binary checksum, and exact guest-image SHA-256. Required commands and redaction guidance are in [AWS worker feasibility gate](./deployment/aws-worker-validation.md).
- [ ] Boot and stop the pinned Ubuntu cloud image on the candidate instance. Record reproducible config/commands, running and stopped status queries, guest-ready evidence, and process cleanup.
- [ ] Before two-worker rollout, validate the selected candidate using one temporary instance. Do not provision the long-lived worker pair until `/dev/kvm` is usable and the exact guest completes a boot/stop cycle.
- Status: **platform support confirmed; instance-level smoke test pending**. No AWS instance has been launched and no host or guest evidence exists.
- Acceptance: AWS platform support is documented, and before deployment the candidate instance exposes usable KVM and the pinned guest boots, reports running, stops, and reports stopped/exited; otherwise record a fallback decision and test that path.
- Depends on: T-00.

### T-02 — Lock MVP decisions
- [x] Confirm language/toolchain and minimal UI approach: Go 1.23+; controller-served server-rendered HTML or small embedded frontend (documented in `docs/design.md`).
- [x] Select Cloud Hypervisor release candidate **v54.0** and guest image candidate **Ubuntu Server 24.04 LTS x86-64 cloud image build dated 2026-10-17**. The pinned artifact URLs and checksum verification process are documented in [AWS worker feasibility gate](./deployment/aws-worker-validation.md); checksums and boot compatibility still require verification.
- [x] Document provisional vCPU/memory limits, safety reserve, image alias, and prototype auth approach: `config.example.json` sets max 8 vCPUs, max 8192 MiB, 1024 MiB reserve, and human-readable image alias `ubuntu-demo`; worker and operator bearer-token secrets are environment-configured. These defaults are provisional pending host evidence; final allowlist identity must be immutable (SHA-256).
- [ ] Complete T-02 evidence closure: record verified Cloud Hypervisor v54.0 binary SHA-256, guest disk SHA-256 from the signed official checksum list, confirm actual worker kernel/available resources, and update the allowlist so `ubuntu-demo` resolves to the immutable image digest. Revisit resource limits after the smoke test.
- Acceptance: all MVP choices that affect API/image identity and host feasibility are pinned, documented, and validated on the candidate environment; no critical implementation blocker remains.
- Status: **implementation decisions selected; deployment evidence pending**. The adapter can target the pinned API release now. Runtime artifact checksums, guest boot, and immutable image allowlisting remain required before two-worker rollout.
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
- Scope re-confirmed by user: use AWS's official nested-virtualization documentation as platform-support evidence and proceed with adapter implementation against the selected Cloud Hypervisor v54.0 API contract. Keep instance boot/stop proof as a mandatory deployment gate.
- Versioned API source and adapter contract documented in [Cloud Hypervisor v54.0 API contract](./cloudhypervisor-api-v54.md): `vm.create`, `vm.boot`, `vm.shutdown`, `vm.info`, and `vmm.ping` method/path/status choices checked against the v54.0 OpenAPI specification.
- Fixed the macOS Unix-socket path-length issue in fake-socket tests by using a short socket path under `/tmp`; the full suite now passes in CI.
- [x] Verify endpoint methods, paths, and success statuses against the pinned v54.0 OpenAPI specification.
- [x] Implement typed calls for VM prepare/create, start, stop, and status.
- [x] Add bounded timeouts and structured errors.
- [x] Align memory size to v54.0 schema (integer bytes), reject non-positive configured memory, and accept all documented VM states including `BreakPoint`.
- [x] CI ran `gofmt` and `go test ./...`; fake Unix-socket tests passed on commit `7132fb4b1da7eb1df1b02d3ddd5663b8259d5217` ([workflow run](https://github.com/InfX2243/A3-PS023-TC061/actions/runs/40933395022)).
- [ ] Run a real Cloud Hypervisor v54.0 smoke test on an AWS candidate host before deployment acceptance.
- Acceptance: adapter tests pass against the fake API; host smoke test is recorded before two-worker rollout.
- Status: **implementation and automated tests verified; host runtime smoke test pending**. CI tests do not establish that `/dev/kvm` or the selected guest image works on a real EC2 instance.
- Depends on: T-01 platform support decision, T-02 selected API/image candidates, T-03.

### T-08 — Implement worker VM lifecycle and process ownership
- [x] Add worker manager with VM/operation identifiers, resource bounds, and configured image-ID resolution.
- [x] Enforce server-side image allowlist and fixed per-VM runtime/socket paths; API requests cannot supply host filesystem paths or VMM arguments.
- [x] Add authenticated worker endpoints for prepare/create, start, stop, and status with bounded JSON payloads and stable error categories.
- [x] Add Cloud Hypervisor process launch/readiness handling, graceful stop confirmation, and child-process kill/reap cleanup.
- [x] Make repeated create/start/stop requests state-aware and avoid launching a second VMM for an existing VM.
- [x] Add a runnable `cmd/worker-agent` entry point and credential-free `worker.example.json` template.
- [x] CI formatting, `go test ./...`, `go vet ./...`, and both binary builds passed on commit `136211af60d7fcac848743b1bac782a7951440ea` ([workflow run](https://github.com/InfX2243/A3-PS023-TC061/actions/runs/40932420786)).
- [x] Define safe worker-agent restart semantics: graceful shutdown kills/reaps owned VMMs; a fresh manager does not assume old in-memory VM records still exist, and controller reconciliation preserves uncertainty when worker status is missing. Durable VMM reattachment is intentionally deferred because the MVP does not yet define cross-process child ownership recovery.
- Acceptance: create/start/stop/status work on one host; repeated commands do not spawn duplicate VM processes; invalid image/path inputs are rejected; child processes are reaped.
- Evidence: implementation files are present at `internal/workeragent/manager.go`, `internal/workeragent/http.go`, `internal/workeragent/manager_test.go`, `internal/workeragent/integration_test.go`, `cmd/worker-agent/main.go`, and `worker.example.json`; CI passed on commit `136211af60d7fcac848743b1bac782a7951440ea` ([workflow run](https://github.com/InfX2243/A3-PS023-TC061/actions/runs/40932420786)).
- Status: **automated tests pass; real Cloud Hypervisor runtime and worker-agent restart behavior remain pending**. The tests use simulated VMMs, not an AWS-backed guest.
- Depends on: T-06, T-07 implementation (T-07 automated tests now pass; real host smoke test remains pending).

### T-09 — Add worker integration tests
- [x] CI ran the workeragent tests covering VMM unavailable, invalid image, timeout, process exit, and orderly worker restart cleanup.
- [x] Tests verify actionable worker errors and that timed-out starts are not reported as running.
- [x] Decide durable worker-side inventory/reattachment is not required for this MVP: controller reconciliation preserves unknown state when worker state is missing rather than claiming success.
- Implementation present: focused worker lifecycle tests are in `internal/workeragent/manager_test.go` and `internal/workeragent/integration_test.go`.
- Acceptance: worker tests pass; failures return actionable, non-secret errors.
- Status: **automated worker lifecycle tests pass; real worker restart/runtime validation pending**.
- Depends on: T-08.

### T-10 — Implement controller API contracts
- [x] CI passed controller API contract tests for authentication, request validation, idempotency-key requirements, stable error payloads, and not-found paths.
- [x] CI-built controller wires the versioned API and heartbeat routes without registration conflicts.
- Implementation present: `internal/controllerapi/handler.go` defines versioned worker, VM, operation, create, start, and stop routes with bounded JSON bodies, request IDs, bearer auth, and stable errors; `cmd/controller/main.go` wires the routes.
- Acceptance: API contract tests cover success, validation, auth, and not-found paths.
- Status: **API implementation and automated contract tests pass**.
- Depends on: T-04, T-05.

### T-11 — Implement memory placement and atomic reservations
- [x] CI passed placement tests for byte arithmetic, stale/unhealthy workers, insufficient capacity, deterministic tie-breaks, and concurrent requests.
- [x] Transactional reservation and concurrency tests pass; stopped VMs release capacity and restart reservations are pinned to the existing worker.
- Implementation present: `internal/placement/placement.go` performs healthy/fresh worker filtering, safe-capacity calculation, deterministic selection, and reservation insert/reactivation inside the caller's SQL transaction. Reservations created after the latest heartbeat are subtracted from the observed available-memory snapshot; all pending/active reservations are subtracted from the configured cap.
- Acceptance: unit and concurrency tests prove no tracked over-reservation under simultaneous create requests.
- Status: **placement implementation and automated concurrency tests pass**.
- Depends on: T-05, T-06, T-10.

### T-12 — Connect controller to workers
- [x] CI passed controller-service tests for persisted create/start/stop, worker authentication, heartbeat inventory, timeout/error handling, and same-worker restart.
- [x] Service tests enforce exact controller/worker memory-cap matching and worker endpoints come only from trusted configuration.
- Implementation present: `internal/controller/worker_client.go` and `internal/controller/service.go` persist VM/operation/reservation outcomes and dispatch authenticated worker commands with bounded HTTP timeouts. Controller startup registers configured workers; worker heartbeat inventory is persisted by the heartbeat handler. The worker agent can report host memory and KVM/VMM availability.
- Acceptance: one VM can be provisioned through the controller and worker end-to-end.
- Status: **controller/worker integration implementation and simulated-worker tests pass**.
- Depends on: T-08, T-10, T-11.

### T-13 — Implement idempotency and uncertain outcomes
- [x] CI passed repeated-create tests with matching and conflicting payloads.
- [x] CI passed timeout/reconciliation tests; uncertain outcomes retain reservations.
- Implementation present: create requires an idempotency key; same-key/same-payload requests return the original VM/operation without redispatch; same-key/different-payload returns conflict. Worker transport timeouts are represented as unknown and do not release reservations.
- Acceptance: repeated create cannot create a duplicate; lost-response test leaves a recoverable state.
- Status: **idempotency and uncertain-outcome tests pass in CI**.
- Depends on: T-05, T-12.

### T-14 — Add reconciliation and restart recovery
- [x] CI passed status-based reconciliation tests for confirmed VM state and preserves unknown state/reservations when a worker returns no status.
- [x] Reconciliation leaves unresolved worker responses uncertain and preserves their reservations.
- Implementation present: `internal/controller/reconcile.go` queries the assigned worker for unfinished operations and only updates metadata from a concrete status response. `cmd/controller/main.go` runs reconciliation on startup and periodically.
- Acceptance: controller restart and worker response-loss tests do not silently corrupt state or free uncertain reservations.
- Status: **reconciliation implementation and simulated status-recovery tests pass**.
- Depends on: T-13.

### T-15 — Build minimal web UI shell
- [x] CI passed UI authentication/session tests; controller routes the UI.
- [ ] Verify secure-cookie behavior behind HTTPS and review the local-development setting.
- Implementation present: `internal/webui/handler.go` provides a server-rendered login/dashboard shell with signed expiring sessions, operator-token login, security headers, and worker/VM views.
- Acceptance: UI loads over the intended protected endpoint and shows both workers.
- Status: **UI shell and authentication/session tests pass in CI**.
- Depends on: T-10.

### T-16 — Add VM provisioning form and VM list
- [x] CI passed UI create/idempotency tests and API image allowlist/validation tests.
- [ ] Verify the selected worker, resource values, observed state, and latest operation are visible.
- Implementation present: the UI includes name, vCPU, memory, and approved image fields; form submissions carry a stable idempotency key; the dashboard shows VM ID/name/host/resources/state and latest operation.
- Acceptance: operator can create a VM from the browser and observe operation progress.
- Status: **provisioning form/list implemented; verification pending**.
- Depends on: T-12, T-15.

### T-17 — Add start/stop/status UI actions
- [x] CI passed UI start-action/CSRF tests and verifies unknown operations trigger dashboard refresh without exposing start/stop actions.
- [ ] Verify invalid actions are not shown and pending/unknown operations refresh without being presented as success.
- Implementation present: start/stop controls are shown for compatible observed states, action results return to the dashboard, and the dashboard refreshes while operations remain pending/dispatched/unknown.
- Acceptance: complete create/start/stop/list journey is usable without manual API calls.
- Status: **lifecycle UI implemented; verification pending**.
- Depends on: T-13, T-14, T-16.

### T-18 — Harden security and operational defaults
- [x] CI passed automated auth, validation, CSRF, configuration, and lifecycle security tests. Linux host permission/network inspection remains a deployment gate.
- [ ] Verify private network ingress, TLS boundary, KVM device access, and socket permissions in deployment.
- Implementation present: worker URL/config validation, constant-time bearer checks, bounded strict JSON parsing, image allowlists, safe argument-array process launch, 0700 runtime directories, 0600 VMM sockets, bounded UI forms, HMAC sessions, configurable Secure cookies, and [security checklist](./security-checklist.md).
- Acceptance: security checklist passes; worker/VMM APIs are not public.
- Status: **code controls implemented; host/network checks pending**.
- Depends on: T-08, T-10, T-15.

### T-19 — Deploy and register two workers
- [ ] Build and install the controller and worker agent on the selected hosts.
- [ ] Register two configured private worker endpoints and verify healthy heartbeat/capacity.
- [ ] Validate the approved guest image on both hosts.
- Implementation present: Linux build script, systemd unit templates, trusted worker configuration, and [deployment runbook](./deployment/worker-deployment.md) are committed.
- Acceptance: both workers are healthy and can run the test guest.
- Status: **deployment automation/docs present; no AWS resources have been launched and deployment remains unverified**.
- Depends on: T-01, T-14, T-18.

### T-20 — Run end-to-end and fault tests
- [ ] Run `gofmt`, `go test ./...`, `go vet ./...`, and build both binaries locally or through CI.
- [ ] Run simulated two-worker placement/lifecycle tests and all fault cases.
- [ ] Run real KVM-backed create/start/stop on each validated host and capture evidence.
- Implementation present: Go CI workflow runs formatting, tests, vet, and builds; controller service tests cover idempotency, timeout uncertainty, and reconciliation; a two-simulated-worker API journey is in `internal/controller/e2e_test.go`.
- Acceptance: all required scenarios pass or have a clearly documented blocker with no false success claims.
- Status: **CI passed formatting, module-file verification, `go test ./...`, `go vet ./...`, and both builds on commit `7132fb4b1da7eb1df1b02d3ddd5663b8259d5217` ([workflow run](https://github.com/InfX2243/A3-PS023-TC061/actions/runs/40933395022)). Real KVM-backed tests are still pending.
- Depends on: T-17, T-19.

### T-21 — Package demo and teardown
- [ ] Follow the demo runbook in a real environment and capture sanitized screenshots/log evidence.
- [ ] Record exact versions, checksums, deployment configuration, and known limitations.
- [ ] Verify teardown removes billable prototype resources without deleting required evidence.
- Implementation present: [demo and teardown runbook](./demo-runbook.md) describes API/UI demo steps, fault scenarios, evidence collection, and cautious AWS teardown.
- Acceptance: another team member can reproduce the demo and shut down the environment safely.
- Status: **runbook present; demo and teardown not executed**.
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
