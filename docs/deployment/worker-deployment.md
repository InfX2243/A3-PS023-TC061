# Prototype Deployment Runbook (T-19 / T-21)

This runbook prepares a repeatable two-worker deployment. It does **not** claim that AWS resources have been created or that KVM/guest boot has passed. Complete the instance-specific gate in aws-worker-validation.md before treating deployment as accepted.

## 1. Build artifacts

From a Go 1.23+ development checkout:

```sh
bash scripts/build-linux.sh
cat dist/SHA256SUMS
```

Copy vm-controller and vm-worker-agent to the intended Linux x86-64 hosts. Verify their SHA-256 hashes after transfer. Record Go version, commit SHA, binary hashes, Cloud Hypervisor version/hash, guest image hash, AMI IDs, and kernel versions in a non-secret deployment record.

## 2. Host preparation

On the controller:
- Create a dedicated vm-controller account/group.
- Install the binary at /usr/local/bin/vm-controller.
- Create /etc/vm-controller and /var/lib/vm-controller with restrictive ownership/modes.
- Install controller.json and controller.env; controller.env must be mode 0600 and contain the operator and worker shared secrets as environment assignments.
- Set the database path to /var/lib/vm-controller/controller.db.
- Configure a trusted worker list with each worker's private endpoint and memory cap.

On each worker:
- Complete the KVM/Cloud Hypervisor/guest boot-stop feasibility gate first.
- Create a dedicated vm-controller-worker account and ensure it has only the required /dev/kvm access (usually via the kvm group).
- Install vm-worker-agent at /usr/local/bin/vm-worker-agent and the pinned Cloud Hypervisor binary at the configured absolute path.
- Stage the exact allowlisted kernel/firmware and disk artifacts in a root-owned image directory. Verify checksums before making the directory readable by the worker.
- Install worker.json and worker.env under /etc/vm-controller. worker.env must be mode 0600 and define VM_CONTROLLER_WORKER_SHARED_SECRET.
- Set runtime_dir to a dedicated directory writable only by the worker service account.
- Set listen_addr to the worker's private interface address and controller_heartbeat_url to the controller's private heartbeat URL.

## 3. Controller worker configuration

Add one entry per worker to controller.json. Replace example addresses with actual private IPs and ensure each configured memory cap is no larger than the cap configured on that worker.

```json
{
  "workers": [
    {"id": "worker-a", "name": "Worker A", "endpoint": "http://10.0.1.10:9090", "memory_limit_mib": 7168},
    {"id": "worker-b", "name": "Worker B", "endpoint": "http://10.0.2.10:9090", "memory_limit_mib": 7168}
  ],
  "secure_cookies": true
}
```

Merge these fields into the full controller configuration rather than replacing its other required fields. The addresses above are documentation examples, not deployed endpoints.

## 4. Network policy

- The operator reaches the controller only through an HTTPS reverse proxy or another approved TLS boundary.
- Worker agent TCP/9090 is reachable only from the controller security group/address.
- Cloud Hypervisor is managed only through its local Unix socket; never open its socket or API to TCP.
- SSH access, if needed, is restricted to the operator's approved administrative source.
- Require IMDSv2, encrypted EBS, least-privilege instance roles, resource tags, and a documented budget/teardown owner.

## 5. Install and start services

Install the unit files from deploy/systemd/ into /etc/systemd/system/ after reviewing user/group and writable paths for the actual host. Then run:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now vm-worker-agent.service
sudo systemctl status vm-worker-agent.service
sudo journalctl -u vm-worker-agent.service --since "10 minutes ago"
```

On the controller, after configuring its environment and worker entries:

```sh
sudo systemctl enable --now vm-controller.service
sudo systemctl status vm-controller.service
sudo journalctl -u vm-controller.service --since "10 minutes ago"
curl --fail http://127.0.0.1:8080/healthz
```

The health endpoint is liveness only. Verify the operator UI through the intended TLS endpoint, then verify both worker heartbeats and memory inventory. Do not use a public plain-HTTP endpoint for operator login.

## 6. Acceptance sequence

1. Confirm both configured workers heartbeat as healthy and have fresh available-memory reports.
2. Confirm unauthenticated operator API requests are rejected.
3. Create a small VM using the approved image, then verify the operation and observed state.
4. Start it, verify running state, stop it, and verify stopped state.
5. Repeat the same create request with the same idempotency key and verify no second VM/process appears.
6. Exercise timeout/lost-response behavior and verify unknown operations keep their reservations until reconciliation.
7. Restart the controller and verify unfinished operations are reconciled from worker status.
8. Run the fault and security scenarios listed in docs/tasks.md and docs/security-checklist.md.

Capture timestamps, sanitized command output, worker IDs, image hashes, VM/operation IDs, and results. Never record secrets or public/private addresses that should remain confidential.

## 7. Teardown

- Stop the demo VMs through the UI/API and verify worker processes are reaped.
- Stop the worker and controller services.
- Preserve non-secret logs, checksums, test evidence, and the exact deployment record.
- From the authenticated AWS CLI, list only resources tagged for this prototype and verify their IDs before terminating instances or deleting volumes.
- Terminate temporary test instances and delete disposable volumes/snapshots only after required evidence is preserved.
- Verify the AWS console/CLI shows no remaining billable prototype resources; record the teardown timestamp and result.

Do not run a broad account-wide delete command. Resource IDs and account-specific teardown commands must come from the actual deployment record.
