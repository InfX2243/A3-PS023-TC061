# EC2 test-node provisioning

The authenticated controller console provisions one explicitly tagged EC2 instance through the **AWS SDK for Go v2**. The AWS CLI is not used or required. The controller process must have outbound HTTPS access to the selected AWS region's EC2 API.

## Controller environment and credentials

Set the region in the protected controller environment:

```dotenv
AWS_REGION=ap-south-1
# Optional: AWS_PROFILE=your-local-profile
# Optional for explicit or temporary credentials:
# AWS_ACCESS_KEY_ID=...
# AWS_SECRET_ACCESS_KEY=...
# AWS_SESSION_TOKEN=...
```

The SDK uses its standard credential provider chain through `config.LoadDefaultConfig`: environment credentials, shared AWS config/credentials profiles, and supported workload/instance-role providers. Prefer an attached IAM role for an AWS-hosted controller. Never submit AWS credentials through browser forms, JSON configuration, tags, or user data. Do not commit secrets.

Grant least-privilege permissions for `ec2:RunInstances`, `ec2:DescribeInstances`, `ec2:StartInstances`, and `ec2:StopInstances`, constrained to the intended account, region, approved AMIs, subnets, security groups, and instance types where supported. Launch also creates the three fixed instance tags (Name, ManagedBy, Role); grant the required tag-on-create permission using `ec2:CreateTags` conditioned on `ec2:RunInstances`. Restrict `iam:PassRole` to the exact worker role only if an instance profile is selected.

## Console launch requirements

Provide a region, approved AMI ID, subnet ID, one or more existing security-group IDs, a name, and either `t3.micro` or `t3.small`. The controller deliberately does not create or modify security groups. Configure inbound rules before launch: allow the worker-agent port only from the controller's fixed source address/security group; allow SSH only from a trusted admin CIDR if required. Never use `0.0.0.0/0` for SSH or the agent API.

The launch request always sets MinCount=1 and MaxCount=1, applies managed tags, and supplies a stable client token for idempotency. SDK calls use the incoming HTTP request context, so client cancellation and server deadlines are propagated.

## Scope and lifecycle

The initial integration creates a tagged EC2 instance only. It does not install a worker agent, register a worker, or claim that the instance is compute-ready. Install the agent using the deployment procedure and register it separately after network reachability and KVM capability are verified. A `t3.micro` is a controller/connectivity test profile, not a KVM/Cloud Hypervisor host.

EC2 lifecycle status is read from EC2, not inferred from worker heartbeat. Start and stop are explicit SDK operations. Termination is intentionally not exposed in the controller workflow; terminate instances through a separately reviewed AWS operation. Removing a worker registration never changes EC2 resources.

## Build and verification

The AWS SDK modules are Go dependencies and are downloaded by Go's module system; no AWS CLI installation is needed.

```sh
go mod tidy
go test ./...
go run ./cmd/controller -config config.example.json
```

Unit tests inject a narrow EC2 API interface and verify request construction, validation, instance mapping, and lifecycle operations without making live AWS calls. A real launch incurs AWS charges and requires an account, IAM permissions, a compatible AMI, subnet, and security group.
