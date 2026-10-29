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

The initial controller foundation uses Go and the standard library. Use Go 1.23 or newer.

```sh
go test ./...
go run ./cmd/controller -config config.example.json
```

The example config binds to `127.0.0.1:8080` and uses local relative paths. It contains no credentials. The current service exposes `GET /healthz`.

Runtime secrets are read from environment variables, not JSON config:

- `VM_CONTROLLER_OPERATOR_TOKEN`
- `VM_CONTROLLER_WORKER_SHARED_SECRET`

These are reserved for subsequent authenticated API/worker tasks and are not yet used by the health endpoint. Do not commit real credentials. Runtime database files under `data/` are ignored by Git.

## Current verification status

- The user reported that `go test ./...` passed for the controller, config, domain, store, and workerhealth packages, and verified `GET /healthz` returns HTTP 200 with `{"status":"ok"}`.
- The subsequent Cloud Hypervisor adapter test run failed because the Unix-socket test path under macOS's `t.TempDir()` was too long. The test now creates its socket in a short `/tmp` path; this fix still needs a fresh local `go test ./...` run.
- KVM/Cloud Hypervisor host feasibility, the pinned Cloud Hypervisor release, and the guest image remain unverified. Do not treat a passing unit suite as evidence that a real VM can boot.
- See [the task plan](docs/tasks.md) for per-task status and evidence. T-01 and T-02 remain gates before additional T-07 work.
