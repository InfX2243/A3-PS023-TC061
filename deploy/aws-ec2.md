# EC2 inventory, worker bootstrap, and lifecycle operations

The authenticated controller console at `/aws` lists every EC2 instance tagged `ManagedBy=aiori-controller` in the selected region. Select an instance to open its dedicated AWS-style detail page with lifecycle, networking, AMI, placement, security-group, and worker-agent information. The detail page plots CPU, memory, root-disk utilization, and network counters directly in Aiori; it does not query CloudWatch. Start/stop actions are explicit, and termination is available only through a separate irreversible confirmation. All lifecycle actions re-check the controller management tag before calling AWS.

The controller uses the AWS SDK for Go v2. The AWS CLI is not required on the controller. The worker bootstrap installs AWS CLI on the worker only to retrieve its SecureString bootstrap secret through the worker's instance role. A small Python collector (not the CloudWatch Agent) reads Linux /proc, df, and interface counters every 60 seconds and sends authenticated samples to the controller. The controller keeps the latest 24 hours / 1,440 samples per instance in memory; a controller restart clears this telemetry history.

## 1. Controller AWS permissions and region

Set `AWS_REGION` (for example `ap-south-1`) and configure the standard AWS SDK credential chain. Prefer an attached IAM role on an EC2-hosted controller.

The controller role needs least-privilege permissions for:

- `ec2:RunInstances`, `ec2:DescribeInstances`, `ec2:StartInstances`, `ec2:StopInstances`
- `ec2:CreateTags` for the fixed `Name`, `ManagedBy`, and `Role` tags on launch
- `ec2:AuthorizeSecurityGroupIngress` for the worker-agent port on the worker security group
- `iam:PassRole` restricted to the exact worker instance role

Restrict these permissions to the intended account, region, AMIs, subnet/security groups, instance types, and role wherever AWS supports resource-level conditions. The inventory only returns resources tagged `ManagedBy=aiori-controller`; it does not list unrelated EC2 instances.

## 2. Automatic worker bootstrap configuration

The UI shows a generated user-data preview when all bootstrap settings are present. The launch payload is a cloud-init script that:

1. Installs the worker's bootstrap dependencies.
2. Downloads the worker-agent, Cloud Hypervisor, and guest disk from HTTPS artifact URLs, and installs the Ubuntu OVMF package for UEFI guest firmware.
3. Verifies every downloaded artifact against a required SHA-256 digest before installing it.
4. Uses IMDSv2 to obtain the instance ID, private IPv4 address, and region.
5. Retrieves the worker shared secret from SSM Parameter Store as a SecureString using the instance profile; the secret value is not embedded in user data.
6. Writes a restricted worker config and environment file, installs a systemd service, and starts the agent.
7. Sends authenticated heartbeats to the controller. On first heartbeat, the controller durably enrolls the node using its EC2 instance ID and private agent endpoint.
8. Starts `aiori-metrics-collector.service`, which sends CPU, memory, root filesystem utilization, and network byte counters to `POST /api/v1/worker/metrics` using the same worker secret. The console reads these samples through the authenticated session route and draws the charts locally.

Set these values in the controller's protected environment (see `.env.example` for the complete variable list):

- `VM_CONTROLLER_PUBLIC_URL`: HTTPS base URL reachable by the worker.
- `VM_CONTROLLER_WORKER_AGENT_URL` and `VM_CONTROLLER_WORKER_AGENT_SHA256`.
- `VM_CONTROLLER_CLOUD_HYPERVISOR_URL` and `VM_CONTROLLER_CLOUD_HYPERVISOR_SHA256`.
- `VM_CONTROLLER_WORKER_DISK_URL` and `VM_CONTROLLER_WORKER_DISK_SHA256`.
- `VM_CONTROLLER_WORKER_SECRET_PARAMETER`: absolute SSM parameter path containing the worker shared secret.
- `VM_CONTROLLER_WORKER_IMAGE_ID` and optional `VM_CONTROLLER_WORKER_MEMORY_LIMIT_MIB`.
- `VM_CONTROLLER_SECURITY_GROUP_ID`: the controller's EC2 security group in the same VPC as the workers.

Artifact URLs must be HTTPS URLs without embedded credentials, query strings, or fragments. Every SHA-256 must be exactly 64 hexadecimal characters. Publish immutable/versioned artifacts and verify their provenance before configuring these values. Do not use mutable `latest` artifact URLs.

To publish the worker-agent binary from this repository, create and push a version tag after the source is ready:

```sh
git tag worker-agent-v0.1.0
git push origin worker-agent-v0.1.0
```

The `.github/workflows/release-worker-agent.yml` workflow builds a static Linux amd64 binary and attaches it with a `SHA256SUMS` file to the GitHub release. Use the versioned release asset URL for `VM_CONTROLLER_WORKER_AGENT_URL` and the matching digest for `VM_CONTROLLER_WORKER_AGENT_SHA256`. Publish Cloud Hypervisor and the guest disk as immutable HTTPS artifacts as well.

The EC2 launch form requires an instance profile when bootstrap is enabled. Attach a worker role that grants only `ssm:GetParameter` on the exact configured parameter ARN (and `kms:Decrypt` for the specific customer-managed KMS key if applicable). Do not grant broad SSM read access. Never put the shared secret itself in user data, EC2 tags, AMI metadata, or browser forms.

## 3. Private network requirements

The controller and worker must be in the same VPC for the automatic security-group rule. On launch, the controller adds a TCP rule for the configured worker-agent port (default `9090`) to each worker security group, with the **controller security group as the only source**. It does not open the agent API to `0.0.0.0/0`.

The worker sends heartbeat traffic outbound to `VM_CONTROLLER_PUBLIC_URL`. The controller calls the worker's private IP endpoint over the VPC. Select a worker subnet and security group in the controller VPC. If the controller security group is in a different VPC, launch/bootstrap will fail to configure the source-group rule; do not bypass this by opening the worker API publicly.

The current worker-agent HTTP API uses bearer authentication over the private VPC path; it is not independently TLS-terminated. Keep that path inside a trusted private network and do not route it over the public internet. A production deployment should add mutual TLS or a private encrypted overlay before using untrusted network paths.


## 3. Local testing through ngrok

For local development, run the controller on `127.0.0.1:8080` and expose that port with an HTTPS ngrok URL. Set `VM_CONTROLLER_PUBLIC_URL` to the exact public HTTPS origin (for example `https://your-name.ngrok-free.app`) before starting the controller. Keep the URL current whenever ngrok assigns a new hostname, then restart the controller and launch a new EC2 instance so its user data contains the current URL. Existing instances do not receive updated user data when the environment changes.

The worker collector and heartbeat make outbound HTTPS requests, so this setup supports telemetry uploads without exposing your laptop or worker-agent port. The instance detail page can therefore display telemetry while the controller is running locally behind ngrok. Important limitation: the existing worker lifecycle API also expects the controller to reach the worker-agent's private endpoint. A laptop reached through ngrok is not inside the EC2 VPC, so this local arrangement does not make private worker-agent RPC reachable. For VM start/stop orchestration, run the controller in the same VPC or establish a private network overlay; do not open port 9090 to the public internet.

Check `/var/log/aiori-worker-bootstrap.log` and `systemctl status aiori-worker-agent aiori-metrics-collector` on the instance if no metrics arrive. The first sample is sent after the collector's initial 60-second interval.

## 4. Inventory and health semantics

EC2 state and agent health are separate authoritative signals:

- **EC2 state** comes from AWS `DescribeInstances`: pending, running, stopping, stopped, shutting-down, terminated, and other AWS lifecycle values.
- **Agent state** comes from authenticated heartbeats: healthy, stale, or unavailable. A healthy heartbeat also requires a successful /dev/kvm and Cloud Hypervisor probe.
- A running EC2 instance with no heartbeat is not a healthy worker. A running instance with an unhealthy hypervisor probe remains unavailable.
- A stopped or unreachable instance eventually becomes stale in worker inventory; heartbeat loss alone does not prove an EC2 lifecycle state.

The Worker inventory refreshes every 15 seconds and shows endpoint, last heartbeat, available memory, and configured memory cap. The AWS inventory refreshes every 30 seconds and supports explicit start/stop. Removing a worker registration never stops or terminates its EC2 resource.

## 5. Instance type and compute readiness

The launch form exposes `t3.micro` and `t3.small` as connectivity/bootstrap test profiles only. Their agent can enroll and report telemetry, but they are not KVM/Cloud Hypervisor compute hosts and must not receive VM workloads.

The `c8i.xlarge` profile explicitly sends `CpuOptions.NestedVirtualization=enabled` through the upgraded AWS SDK. This is the intended first compute-worker candidate from the AWS feasibility gate. It is still a candidate until the launched AMI/kernel exposes usable `/dev/kvm`, the agent's KVM/VMM probe succeeds, and a real guest boot/stop cycle passes. Check region capacity, service quota, and current On-Demand price before launch. Do not infer virtualization readiness from a successful EC2 launch or heartbeat alone.

## 6. Connect and operate

- Use **Connect in AWS console** on the inspected instance to open the AWS instance details page.
- For an Ubuntu AMI with an EC2 key pair, the details page shows an SSH command template. The private key remains on your machine; never upload it.
- Use **Start** and **Stop** only when the instance is in a compatible AWS state. Refresh to confirm the resulting state.
- **Terminate** is irreversible and deletes the EC2 instance; use it only after checking attached EBS volume retention and any workload/data impact. The controller only offers this action for instances tagged as managed by this project.
- Use Worker inventory for agent health and the controller VM dashboard for guest VM operations. EC2 running is not equivalent to a running guest VM.

## 7. Build and verify

```sh
go test ./...
go run ./cmd/controller -config config.example.json
```

Unit tests use a narrow EC2 API fake and do not make live AWS calls. A real launch incurs AWS charges and requires valid IAM permissions, a compatible Linux AMI, an instance profile, artifact URLs/checksums, SSM parameter, and same-VPC security groups. Cloud-init logs are written to `/var/log/aiori-worker-bootstrap.log` on the worker; inspect them through your approved SSH access path when bootstrap does not complete. Collector service output is available through `journalctl -u aiori-metrics-collector`.
