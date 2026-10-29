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

## Deployment and infrastructure onboarding

See [controller deployment and worker onboarding](deploy/README.md) for the EC2 controller setup, systemd unit, TLS reverse-proxy example, durable SQLite storage, secret handling, and bring-your-own-worker checklist. A small `t3.micro` is a controller candidate only; worker hosts must independently satisfy KVM/Cloud Hypervisor requirements.

The controller now supports a persistent worker registry in the console at `/workers`. Registrations are stored in SQLite and survive controller restarts. Removing a registration removes controller metadata only; it does not terminate cloud infrastructure. Agent health (healthy, stale, unreachable) is distinct from authoritative cloud lifecycle state. AWS EC2 lifecycle status and start/stop/terminate actions require a separately configured AWS integration and least-privilege IAM permissions; they must not be inferred from heartbeat loss.

Copy `.env.example` to a local `.env`, replace both secret placeholders with different random values, and run `chmod 600 .env`. The controller loads a local `.env` without shell evaluation and does not override environment variables already set by the process. Never commit `.env` or real credentials. Production deployments should supply secrets through the service manager's protected environment file or a secret manager.

## Development setup

The controller and worker agent use Go 1.23 or newer. The worker agent is configured separately from the controller.

```sh
go test ./...
go run ./cmd/controller -config config.example.json
```

The controller example config binds to `127.0.0.1:8080` and uses local relative paths. It contains no credentials. The controller exposes `GET /healthz`, the authenticated versioned API under `/api/v1`, and a server-rendered UI at `/`. `worker.example.json` is a separate, credential-free worker-agent template; replace the illustrative absolute image paths with files staged and verified on each worker.

Runtime secrets are read from environment variables, not JSON config:

- `VM_CONTROLLER_OPERATOR_TOKEN`
- `VM_CONTROLLER_WORKER_SHARED_SECRET`

The operator token protects the UI and controller API; the worker shared secret protects heartbeats and controller-to-worker lifecycle calls. Configure trusted worker endpoints in the controller JSON before expecting placement to succeed. The worker agent requires `VM_CONTROLLER_WORKER_SHARED_SECRET` to authorize lifecycle requests. Start it with `go run ./cmd/worker-agent -config worker.example.json` after staging the configured Cloud Hypervisor binary and allowlisted image artifacts. Never expose the worker API or Cloud Hypervisor Unix socket publicly. Do not commit real credentials. Runtime database files under `data/` are ignored by Git.

## Current verification status

- The user reported that `go test ./...` passed for the controller, config, domain, store, and workerhealth packages, and verified `GET /healthz` returns HTTP 200 with `{"status":"ok"}`.
- The Cloud Hypervisor adapter test initially failed because the Unix-socket test path under macOS's `t.TempDir()` was too long. The test now creates its socket in a short `/tmp` path; the fix still needs a fresh local `go test ./...` run. Worker lifecycle tests have been added but are also pending execution.
- KVM/Cloud Hypervisor host feasibility, the pinned Cloud Hypervisor release, and the guest image remain unverified. Do not treat a passing unit suite as evidence that a real VM can boot.
- The implementation-first workflow continues through remaining tasks while execution and deployment checks are recorded as pending. See [the task plan](docs/tasks.md) for per-task status and evidence.
