# AIORI CloudFormation deployment

This directory provisions the AWS foundation for an AIORI controller and its worker nodes. It is intended as the repeatable starting point for a test deployment; it does not claim that nested virtualization or guest boot has been validated in your AWS account.

## What the stack creates

- A dedicated VPC, public subnet, internet gateway, and route table.
- A controller EC2 instance with an instance profile, AWS Systems Manager access, and the permissions needed to discover and launch managed worker instances.
- A worker security group and worker instance profile. TCP 9090 is accepted **only from the controller security group**.
- A private SSM parameter path for the worker shared secret. The controller creates the secret on first boot; it is never passed as a CloudFormation parameter or placed in EC2 user data.
- CloudWatch log groups, CloudWatch Agent configuration on the controller, and a CloudWatch dashboard with EC2 CPU/network plus CloudWatch Agent memory/disk metrics and log access.
- SSM parameters containing the stack's subnet, security-group, instance-profile, and secret-parameter names so controller automation can discover its environment without a large manually maintained `.env`.

The controller instance is public so the operator can reach the initial console. Its security group restricts the application port to `AdminCIDR`; no SSH port is opened. Use Systems Manager Session Manager for administration.

## Deploy

1. The template automatically resolves the latest Canonical Ubuntu 24.04 amd64 AMI for the selected region through its public SSM parameter.
2. Open **CloudFormation → Create stack → With new resources (standard)**.
3. Upload `aiori-compute.yaml`.
4. Supply:
   - Stack name, e.g. `aiori-dev`.
   - `AdminCIDR`: your current public IPv4 address with `/32` (do not use `0.0.0.0/0`).
   - `KeyName` is optional; SSM is the recommended access path.
   - `GitRef`: a branch, tag, or commit that contains the controller source. Use the deployment branch until its changes are merged.
5. Acknowledge the IAM resource creation capability and create the stack.
6. Wait for `CREATE_COMPLETE`. Open the `ControllerURL` output and sign in with the generated operator token. Retrieve it from the controller's SSM parameter using the read command below.

You can also deploy/update from a machine with AWS CLI credentials:

```sh
chmod +x infra/deploy.sh
AWS_REGION=ap-south-1 ./infra/deploy.sh aiori-dev YOUR.PUBLIC.IP/32 feat/cloudformation-infrastructure-monitoring
```

The template uses a public HTTP endpoint for this initial restricted test setup. It is **not a production TLS deployment**. Do not widen `AdminCIDR`. Before production or use over untrusted networks, put the controller behind HTTPS (for example, an ALB with ACM and a domain) and enable secure cookies. Worker API port 9090 is never public.

## Retrieve the initial operator token

Use an AWS identity that is allowed to read the stack's operator-token parameter:

```sh
aws ssm get-parameter \
  --name "/aiori/<stack-name>/controller/operator-token" \
  --with-decryption \
  --query 'Parameter.Value' --output text
```

The worker shared secret is deliberately not exposed as a stack output. The controller instance role creates and reads it; workers receive only permission to retrieve that one SecureString parameter.

## Connect a worker

The stack exports the values required by the existing `/aws` provisioning screen:

- `WorkerSubnetId`
- `WorkerSecurityGroupId`
- `WorkerInstanceProfileName`
- `WorkerSecretParameterName`
- `ControllerPrivateIP`
- `ControllerSecurityGroupId`

The controller and workers are placed in the same VPC. The worker security group accepts TCP 9090 only from the controller security group. Worker heartbeats reach the controller over the private VPC address. The worker role can read only the one shared-secret parameter and publish CloudWatch Agent telemetry/logs.

**Worker bootstrap caveat:** the current application still needs immutable, checksum-verified Cloud Hypervisor and guest-disk artifacts before it can fully bootstrap a KVM worker. The CloudFormation stack intentionally does not invent or silently trust those artifacts. Publish/version those assets and wire their exact URLs and SHA-256 values into the controller's bootstrap configuration before launching a compute worker. The worker-agent release workflow publishes its own versioned binary and checksum manifest.

## Monitoring and logs

Open the CloudWatch dashboard named `<stack-name>-AIORI-Compute` in the stack's region. It includes EC2 CPU/network and CloudWatch Agent memory/disk metrics for instances in the account/region. Log groups are:

- `/aiori/<stack-name>/controller`
- `/aiori/<stack-name>/worker`
- `/aiori/<stack-name>/system`

The controller bootstrap log is also available at `/var/log/aiori-controller-bootstrap.log` on the instance. CloudWatch metrics can take several minutes to appear after the agent starts. The EC2 basic CPU/network metrics and the CloudWatch Agent guest metrics are separate data sources.

## Costs and teardown

EC2, EBS, CloudWatch logs/metrics, and data transfer incur AWS charges. This template intentionally avoids a NAT Gateway; the controller and worker instances use public-subnet egress, while security groups restrict inbound access. Review the instance type, EBS size, log retention, and CloudWatch dashboard before deployment.

Delete the CloudFormation stack when finished. Confirm that no worker/VM data or EBS volumes need retention first. The controller creates SSM parameters at boot; delete the stack-specific `/aiori/<stack-name>/` parameters if they remain after stack deletion.
