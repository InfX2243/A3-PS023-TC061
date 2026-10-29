# AWS MVP Deployment Plan

## 1. Objective

Deploy the smallest useful prototype: a controller with a web provisioning UI and two Cloud Hypervisor worker agents. The plan is conditional on proving that the chosen worker environment supports KVM.

## 2. Mandatory feasibility gate

Cloud Hypervisor depends on KVM. Before building the full AWS environment:

1. Select a candidate AWS region and worker instance configuration.
2. Launch one short-lived test instance.
3. Verify `/dev/kvm` exists and the service user has the necessary access.
4. Install a pinned Cloud Hypervisor release.
5. Boot and shut down the exact approved guest image.
6. Record instance type, AMI/OS, kernel, Cloud Hypervisor version, test commands, and results.
7. Terminate the test instance if the candidate fails.

Do not assume that a normal virtualized EC2 instance supports nested KVM. If the selected configuration cannot run KVM, evaluate a supported bare-metal instance or keep the workers on existing KVM-capable hosts while the controller runs on AWS. Confirm current AWS documentation and cost before choosing.

## 3. Prototype topology

Preferred only after the KVM gate passes:

- **Controller EC2:** controller API and web UI; SQLite on EBS-backed durable storage.
- **Worker A EC2:** worker agent, Cloud Hypervisor, approved guest image.
- **Worker B EC2:** worker agent, Cloud Hypervisor, approved guest image.
- **Network:** VPC with private worker subnets; controller ingress restricted to a known operator IP range or protected access path.
- **Worker communication:** private addressing and security-group rules allowing only controller-to-worker traffic.
- **Secrets:** protected runtime files, AWS Systems Manager Parameter Store/Secrets Manager, or another approved secret mechanism. Do not commit secrets.
- **Observability:** structured service logs; CloudWatch integration is optional for MVP.
- **Persistence:** EBS volume or EBS-backed root volume for controller database and configuration backups.

If the controller is placed in a public subnet, expose only the intended HTTPS entry point and restrict source IPs. Prefer private workers without public IP addresses.

## 4. Network/security group intent

| Component | Inbound access | Explicitly disallow |
|---|---|---|
| Controller | HTTPS from approved operator IPs or a protected access path; SSH only through a restricted admin path if required | Unrestricted admin ports and public worker management endpoints |
| Worker A/B | Worker API port from controller security group only; SSH through restricted admin path if required | Public worker API, public Cloud Hypervisor socket/API |
| SQLite/data disk | Local controller process only | Network exposure of the database |

Use TLS at the operator boundary. Authenticate worker requests at the application layer even inside a private VPC. Network location is not a substitute for authentication.

## 5. IAM and secrets

- Attach a least-privilege instance role only if the instance needs AWS APIs.
- Prefer Systems Manager Session Manager over open SSH where practical.
- Do not create long-lived AWS access keys on the instances.
- Restrict access to any Parameter Store/Secrets Manager values to the required instance role.
- Keep application secrets out of AMIs, user-data logs, Git, and error messages.
- Use separate credentials for operator access and worker-to-controller communication.

## 6. Deployment sequence

1. **KVM proof:** run the feasibility gate on one candidate worker configuration.
2. **Network baseline:** create VPC/subnets/security groups with private worker reachability.
3. **Controller host:** deploy the controller binary/service and durable database directory.
4. **Worker A:** deploy agent, configure local Cloud Hypervisor socket, verify health and guest boot.
5. **Worker B:** repeat and verify identical version/configuration.
6. **Register workers:** configure stable worker IDs/endpoints and credentials.
7. **UI verification:** access the protected controller URL and check worker health/capacity.
8. **Lifecycle smoke test:** provision a small approved guest, start, inspect, stop, and verify cleanup.
9. **Fault tests:** run concurrency, timeout, controller restart, and worker outage scenarios.
10. **Evidence and teardown:** save redacted logs/test results, stop or terminate resources when not in use, and verify remaining billable resources.

## 7. Service operation

For the first MVP, systemd is preferred to reduce deployment complexity. Suggested units:

- `vm-controller.service`: controller UI/API, configured to restart on failure.
- `vm-worker.service`: worker agent, configured to restart on failure.

Set dedicated service users, restrictive file permissions, environment/config files with no secrets in Git, explicit resource limits where practical, and log rotation. Ensure worker restart behavior does not start duplicate VMM processes or forget existing VMs.

## 8. Validation checklist

- [ ] `/healthz` responds only on the intended controller path.
- [ ] Both workers report healthy status and fresh capacity.
- [ ] Worker APIs are not reachable from the public internet.
- [ ] Cloud Hypervisor management sockets are local and permission-restricted.
- [ ] Approved guest image boots and stops on both workers.
- [ ] UI create/start/stop/list path works.
- [ ] Placement respects reservations and safety reserve.
- [ ] Repeated create request does not duplicate the VM.
- [ ] Lost worker response and controller restart are recoverable.
- [ ] Logs do not contain secrets.
- [ ] Teardown instructions are tested.

## 9. Cost guardrails

- Use short-lived instances for feasibility testing.
- Check current regional prices and bare-metal instance pricing before deployment.
- Tag all resources, e.g. `Project=AIORI-PS023`, `Environment=prototype`, `Owner=team`.
- Set a budget alert; alerts may not stop spending automatically.
- Avoid NAT gateways and other persistent paid components unless required; evaluate private access and package download paths carefully.
- Stop instances when possible; terminate temporary resources when no longer needed.
- Remember that stopped instances may still incur EBS and allocated-address charges.
- Before ending the demo, inspect EC2 instances, EBS volumes/snapshots, Elastic IPs, load balancers, NAT gateways, and CloudWatch log retention.

## 10. Rollback and teardown

- Keep previous application binaries/configuration for service rollback.
- Back up the SQLite database before schema changes.
- Roll back application code without deleting VM disk images or blindly releasing reservations.
- Before terminating workers, stop guest VMs and confirm their state if preservation is required.
- Preserve required evidence and redact sensitive details.
- Terminate prototype instances and delete unneeded storage/network resources after confirming the team no longer needs them.

## 11. Deployment record template

Record this in the project notes without secrets:

- AWS region:
- Controller instance type / OS:
- Worker instance type / OS:
- KVM proof result and evidence:
- Cloud Hypervisor version:
- Guest image identifier/version:
- Network access method:
- Deployment commit:
- Smoke test result:
- Known limitations:
- Teardown date/status:
