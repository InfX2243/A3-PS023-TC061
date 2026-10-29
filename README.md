# AIORI-3-HACKATHON-PROJECTS

This repository contains the code and resources for projects developed during the AIORI-3 Hackathon.

## Project

**Minimal Two-Node Cloud Hypervisor VM Controller** — a prototype controller for managing VMs across two KVM-capable Linux workers.

**Team:** CloudOpsCrew

- **Abid Shaikh** — Controller Architecture & Integration
- **Umar Ansari** — Worker Agents & VM Lifecycle
- **Mentor:** Dr. Zainab Mirza

[Project presentation](https://youtu.be/x6dSHQatmrY)

## MVP documentation

Start with the [MVP documentation index](docs/README.md), [requirements](docs/requirements.md), [design](docs/design.md), and [task plan](docs/tasks.md).

Cloud Hypervisor requires KVM. Worker environment feasibility remains a deployment gate; this repository does not claim that KVM or an AWS worker configuration has been verified.

## Development setup

The controller and worker agent use Go 1.23 or newer. The worker agent is configured separately from the controller.

```sh
go test ./...
go run ./cmd/controller -config config.example.json
```

The controller example config binds to `127.0.0.1:8080` and uses local relative paths. It contains no credentials. The controller exposes `GET /healthz`. `worker.example.json` is a separate, credential-free worker-agent template; replace the illustrative absolute image paths with files staged and verified on each worker.

Runtime secrets are read from environment variables, not JSON config:

- `VM_CONTROLLER_OPERATOR_TOKEN`
- `VM_CONTROLLER_WORKER_SHARED_SECRET`

The controller currently uses these for worker-heartbeat and operator worker-list endpoints. The worker agent requires `VM_CONTROLLER_WORKER_SHARED_SECRET` to authorize lifecycle requests. Start it with `go run ./cmd/worker-agent -config worker.example.json` after staging the configured Cloud Hypervisor binary and allowlisted image artifacts. Never expose the worker API or Cloud Hypervisor Unix socket publicly. Do not commit real credentials. Runtime database files under `data/` are ignored by Git.

## Current verification status

- The user reported that `go test ./...` passed for the controller, config, domain, store, and workerhealth packages, and verified `GET /healthz` returns HTTP 200 with `{"status":"ok"}`.
- The Cloud Hypervisor adapter test initially failed because the Unix-socket test path under macOS's `t.TempDir()` was too long. The test now creates its socket in a short `/tmp` path; the fix still needs a fresh local `go test ./...` run. Worker lifecycle tests have been added but are also pending execution.
- KVM/Cloud Hypervisor host feasibility, the pinned Cloud Hypervisor release, and the guest image remain unverified. Do not treat a passing unit suite as evidence that a real VM can boot.
- The implementation-first workflow continues through remaining tasks while execution and deployment checks are recorded as pending. See [the task plan](docs/tasks.md) for per-task status and evidence.
