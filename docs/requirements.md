# Requirements — Minimal Two-Node Cloud Hypervisor VM Controller

## 1. Purpose

Build a minimal, usable control plane for provisioning and managing Cloud Hypervisor virtual machines across two KVM-capable worker hosts. The controller must include a web interface so an operator can provision and inspect VMs without manually issuing host commands.

This is a prototype/MVP, not a production-grade cloud platform.

## 2. Problem statement

Managing VMs on separate hosts requires operators to coordinate host choice, resource capacity, lifecycle operations, and runtime state. Blind retries, concurrent allocations, or controller/worker failures can leave metadata inconsistent with actual VM processes or leak reservations.

The MVP centralizes these operations, selects a host using safe memory capacity, persists state, and verifies outcomes before retrying uncertain operations.

## 3. Target users and primary user journey

**Primary user:** developer or operator running a small two-host prototype.

1. Open the controller web UI.
2. View worker health and reported memory capacity.
3. Enter a VM name, memory allocation, vCPU count, and approved guest image.
4. Submit the provisioning request.
5. See the selected host and operation status.
6. Start, stop, and inspect the VM from the UI.
7. View clear errors when capacity is insufficient or a worker is unreachable.

## 4. MVP scope

### In scope

- One controller service with a lightweight web UI and JSON API.
- Exactly two registered worker agents for the target demonstration.
- Cloud Hypervisor on each worker, using a local Unix-domain management socket.
- VM create/prepare, start, stop, and list/status operations.
- Memory-aware host selection and atomic reservation accounting.
- Persistent controller metadata and operation records using SQLite on durable storage.
- Worker health and capacity reporting.
- Operation IDs and idempotency keys for create requests.
- Explicit handling of uncertain outcomes and basic reconciliation after restart.
- Input validation, authentication, private worker communication, structured logs, and actionable errors.
- A repeatable AWS prototype deployment, subject to the KVM feasibility gate.

### Out of scope

- Live migration, autoscaling, multi-tenant billing, quotas per organization, and scheduling beyond two hosts.
- High availability or automatic controller failover.
- Distributed consensus or a multi-controller architecture.
- Kubernetes/OpenStack integration.
- Arbitrary user-uploaded images, arbitrary host paths, arbitrary shell commands, or user-supplied Cloud Hypervisor arguments.
- Advanced networking, virtual network management, storage orchestration, snapshots, backups, and GPU passthrough.
- Production SLA, compliance certification, or multi-region deployment.

## 5. Functional requirements

| ID | Requirement | Acceptance evidence |
|---|---|---|
| FR-01 | The UI shall show controller health, both workers, and each worker's latest capacity/heartbeat. | UI test and API response show both workers and stale/unreachable status. |
| FR-02 | An authenticated operator shall submit a VM definition containing name, vCPU count, memory, and an approved image identifier. | Valid request accepted; malformed or unsupported input rejected. |
| FR-03 | The controller shall select an eligible host using safe memory capacity. | Unit tests cover eligible, ineligible, tie, and no-capacity cases. |
| FR-04 | The controller shall reserve requested memory atomically before dispatch. | Concurrent tests demonstrate that reservations cannot exceed configured limits. |
| FR-05 | The worker shall prepare/create, start, stop, and report the state of a VM through Cloud Hypervisor. | End-to-end lifecycle succeeds on a validated worker. |
| FR-06 | The UI shall list VMs with ID, name, host, requested resources, and current/last-known state. | UI and API display consistent records. |
| FR-07 | Repeating a create request with the same idempotency key shall not create a second VM. | Integration test proves one VM record/process for the same key. |
| FR-08 | Timeouts or lost worker responses shall be represented as uncertain until reconciled. | Fault-injection test confirms no blind duplicate dispatch and reservation retained while uncertain. |
| FR-09 | Controller metadata and operation records shall persist across controller restarts. | Restart test confirms records remain and pending operations are reconciled. |
| FR-10 | Worker management APIs shall require authentication and be reachable only on the intended private path. | Network and authorization checks pass. |
| FR-11 | The UI shall display understandable success, progress, and failure messages. | Manual acceptance checklist completed. |
| FR-12 | VM images and host paths shall be selected from server-side allowlisted configuration. | Attempts to supply arbitrary paths are rejected. |

## 6. Non-functional requirements

- **Safety:** Never claim a VM is running solely because a start request was accepted.
- **Consistency:** Use explicit desired state, observed state, operation state, and reservations.
- **Recoverability:** Preserve uncertain operations and reconcile them after restarts.
- **Security:** Authenticate UI/API access; keep worker and VMM endpoints private; do not log secrets.
- **Simplicity:** Prefer one controller process, one SQLite database, and identical worker binaries/configuration.
- **Usability:** Core provisioning and lifecycle operations are available through the web UI.
- **Testability:** Placement and state transitions are unit-testable without a real hypervisor; end-to-end tests run on KVM-capable hosts.
- **Observability:** Structured logs include request/operation/VM/worker IDs and outcome.
- **Cost control:** AWS resources are tagged, sized for a prototype, and stopped or deleted when not in use.

## 7. MVP success criteria

The MVP is acceptable when all are true:

1. The web UI can create, start, stop, and list VMs.
2. The controller can dispatch to either of two workers.
3. Placement respects the configured memory safety reserve and reservation limit.
4. Concurrent requests cannot oversubscribe the controller's tracked memory budget.
5. A repeated idempotent create request does not duplicate the VM.
6. Lost responses and worker restarts do not cause blind retries or silently released reservations.
7. Controller restart preserves metadata and triggers reconciliation.
8. Access controls prevent unauthenticated operations and direct public access to worker/VMM endpoints.
9. A documented demo and test evidence can be repeated by another team member.

## 8. Constraints and assumptions

- Both worker hosts must support Linux KVM and expose usable `/dev/kvm`.
- Cloud Hypervisor version and API behavior must be pinned and verified against the actual installed version.
- The MVP uses a pre-provisioned guest image and a server-side image allowlist.
- Memory capacity is an admission-control estimate, not a guarantee of all host resources; CPU, disk, and device scheduling are outside the first MVP unless required by validation.
- The controller database is single-writer SQLite on durable storage. Do not place it on an unsuitable shared filesystem.
- AWS worker feasibility must be tested before selecting instance types or estimating final costs.

## 9. Open decisions to resolve during the first tasks

- Which AWS instance type/region supports the required KVM access for worker hosts?
- Which guest image and Cloud Hypervisor version will be used?
- Will the controller and workers all run in AWS, or will the controller run in AWS with local KVM workers if AWS virtualization is unavailable?
- Which authentication approach is simplest for the prototype: one operator credential plus worker shared secrets, or mTLS?
- What maximum vCPU/memory values and safety reserve will be configured for the demo?
