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

See [controller deployment and worker onboarding](deploy/README.md) and the [AWS EC2 test-node guide](deploy/aws-ec2.md). The authenticated console at `/aws` can launch a single tagged `t3.micro` or `t3.small` instance using the AWS SDK for Go v2 (no AWS CLI required). The AWS SDK uses its standard credential provider chain; credentials are never submitted by the browser. The workflow requires an existing AMI, subnet, and preconfigured security group and does not widen network access. It supports explicit start/stop and reads EC2 lifecycle state from AWS. Termination is deliberately not exposed.

The EC2 console now lists controller-managed instances, shows detailed AWS state and connection information, and supports explicit start/stop. Worker bootstrap user data is mandatory for every launch from `/aws`: the controller validates the complete cloud-init script and required worker instance profile before calling AWS `RunInstances`. If configuration is incomplete, the launch is blocked—there is no longer a silent EC2-only launch path. The script installs checksum-verified worker artifacts, retrieves the worker secret from SSM Parameter Store using the instance role, starts the agent, and auto-enrolls it on its first authenticated heartbeat. Configure the required values in `.env` as described in [the mandatory bootstrap setup](deploy/aws-ec2.md). EC2 lifecycle and worker-agent health remain distinct. The `t3.micro`/`t3.small` profiles are connectivity/bootstrap tests only; the `c8i.xlarge` profile explicitly requests AWS nested virtualization but still requires a real `/dev/kvm` and guest boot/stop validation; see [EC2 bootstrap and network requirements](deploy/aws-ec2.md). The instance inventory opens a dedicated AWS-style detail page with in-platform CPU, memory, disk, and network charts. Worker hosts push authenticated telemetry directly to the controller; the CloudFormation stack no longer creates CloudWatch dashboards/log groups or grants CloudWatch agent permissions. For local tests, expose the controller with ngrok HTTPS and set `VM_CONTROLLER_PUBLIC_URL`; telemetry works outbound, while controller-to-worker lifecycle RPC still needs a private VPC path. See [EC2 bootstrap, telemetry, and ngrok testing](deploy/aws-ec2.md).

Copy `.env.example` to a local `.env`, replace the operator token, operator password, and worker shared secret with independent values, set an operator username, and run `chmod 600 .env`. The controller loads a local `.env` without shell evaluation and does not override environment variables already set by the process. For EC2 integration, configure `AWS_REGION` plus the AWS SDK for Go v2 default credential chain, preferably an attached IAM role. Go downloads the SDK modules automatically; no AWS CLI installation is needed. Never commit `.env` or real credentials. Production deployments should supply secrets through the service manager's protected environment file or a secret manager.

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


## Frontend controller simulation

The browser-only controller experience is documented in [docs/frontend-controller-simulation.md](docs/frontend-controller-simulation.md). It explains the two-worker operator workflows, simulated placement and health rules, accessibility expectations, and the intended frontend-to-controller integration boundary. The interactive handbook is also available from the public frontend Documentation page.
