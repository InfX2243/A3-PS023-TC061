# Controller deployment and worker onboarding

This deployment is intentionally split into two planes:

- **Controller**: a small Linux EC2 instance (a t3.micro can host the Go controller, SQLite metadata, and UI for a prototype). It does not run guest VMs.
- **Worker**: a separate KVM-capable Linux host running the worker agent and Cloud Hypervisor. A worker needs usable /dev/kvm, enough RAM/CPU, a staged allowlisted guest image, and network reachability to the controller. Do not assume a t3.micro is suitable as a virtualization worker.

## 1. Configure secrets

The repository intentionally does not contain a populated .env because it would expose operator and worker credentials. For local development:

```sh
cp .env.example .env
chmod 600 .env
# Generate independent values for the API/session token, worker shared secret, and operator password:
openssl rand -hex 32
```

Do not source an untrusted env file. For a server, install the values in /etc/aiori/controller.env, owned by root and mode 0600 (the service manager reads it before dropping privileges). Generate distinct random values for VM_CONTROLLER_OPERATOR_TOKEN and VM_CONTROLLER_WORKER_SHARED_SECRET, and set VM_CONTROLLER_OPERATOR_USERNAME plus a different VM_CONTROLLER_OPERATOR_PASSWORD of at least 16 characters. The username/password pair protects browser sign-in; the operator token continues to authenticate the API and sign session/CSRF tokens. Rotate exposed credentials.

## 2. Prepare controller configuration

Create /etc/aiori/config.json from config.example.json and set:

- listen_addr: 127.0.0.1:8080 when using the included Nginx TLS proxy.
- database_path: /var/lib/aiori/controller.db so inventory and VM metadata survive service restarts and binary upgrades.
- secure_cookies: true when the UI is only accessed over HTTPS.
- workers: empty initially. Worker registrations must not depend on a static list baked into the deployment.

The SQLite database is the durable source of truth. Back up the database consistently and keep backups outside the instance. For a production deployment, use encrypted durable EBS storage, snapshots, and a tested restore procedure. Do not put the DB on ephemeral instance storage.

## 3. Build and install

Build for the target architecture from a trusted checkout/release artifact:

```sh
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o aiori-controller ./cmd/controller
```

Install the binary at /usr/local/bin/aiori-controller, the systemd unit at /etc/systemd/system/aiori-controller.service, and the config/secrets in /etc/aiori/. Create a dedicated aiori service user and group; make /var/lib/aiori owned by that account. Keep config and secrets readable only by root/service group as appropriate. Then:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now aiori-controller
sudo systemctl status aiori-controller
curl --fail http://127.0.0.1:8080/healthz
```

Install and configure Nginx using deploy/nginx/aiori-controller.conf, obtain a valid TLS certificate, and restrict the EC2 security group to HTTPS from intended operator IPs. Do not expose port 8080, the worker API, SSH broadly, or any Cloud Hypervisor socket to the public internet. The included Nginx file is a template; replace the example hostname and certificate paths.

## 4. Onboard a worker

For EC2-managed workers, configure the checksum-pinned bootstrap environment and IAM roles described in [EC2 inventory and bootstrap](./aws-ec2.md). Launch through `/aws`; cloud-init installs the worker agent, retrieves its shared secret from SSM, and starts the systemd service. The first authenticated heartbeat advertises the private endpoint and auto-enrolls the node in SQLite. Confirm a recent heartbeat and a healthy `/dev/kvm` + Cloud Hypervisor probe in Worker inventory before scheduling VMs.

For an existing non-EC2 worker, the manual onboarding path remains available. Verify `/dev/kvm`, install the worker agent, configure the same worker shared secret, stage the allowlisted guest artifacts, ensure private controller-to-worker reachability, and register the stable ID, endpoint, and memory cap through the UI/API. Do not expose the worker API or Cloud Hypervisor socket publicly.

## 5. State semantics and cloud lifecycle

The controller can reliably show **agent healthy**, **stale heartbeat**, or **unreachable** from worker reports. A stopped worker cannot send a heartbeat, so unreachable does not prove the EC2 instance is stopped or terminated. The authenticated AWS EC2 console uses the AWS SDK for Go v2 to display authoritative EC2 lifecycle states (pending, running, stopping, stopped, shutting-down, terminated) and issue explicit start/stop operations. Configure the SDK credential chain and least-privilege permissions; do not infer EC2 states from network failure. Cloud lifecycle actions must be explicit, auditable, and guarded against accidental termination.

## 6. Persistence and deletion

Worker registration metadata is controller-owned and should persist across restarts. Removing a registration should not terminate or delete the cloud instance. Before deregistration, ensure no VM/reservation references that worker. Cloud resource termination is a separate destructive action and must require a separate confirmation. Keep periodic database backups and verify restore before relying on the controller for durable inventory.
