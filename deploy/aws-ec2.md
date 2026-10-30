# EC2 test-node provisioning

The console can create one explicitly tagged EC2 test instance using the AWS CLI. The controller process must have the AWS CLI installed and configured. Credentials are resolved by the AWS CLI standard chain: environment variables, shared credentials/configuration, or an attached instance role. Do not put credentials in browser forms, JSON configuration, tags, or user data.

## Controller environment

Set these in the protected controller `.env` / service environment:

```dotenv
AWS_REGION=ap-south-1
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...
# Optional for temporary credentials:
AWS_SESSION_TOKEN=...
```

Prefer an IAM role when the controller itself runs on AWS. Grant only `ec2:RunInstances`, `ec2:DescribeInstances`, `ec2:StartInstances`, `ec2:StopInstances`, and `ec2:CreateTags` for the intended account/region and permitted AMI, subnet, security groups, and instance types. Restrict instance-profile pass-role permission to the exact worker role if one is used.

## Console launch requirements

Provide a region, approved AMI ID, subnet ID, one or more existing security-group IDs, a name, and either `t3.micro` or `t3.small`. The controller deliberately does not create or modify security groups. Configure inbound rules before launch: allow the worker-agent port only from the controller's fixed source address/security group; allow SSH only from a trusted admin CIDR if required. Never use `0.0.0.0/0` for SSH or the agent API.

The initial integration creates a tagged EC2 instance only. It does not install a worker agent, register a worker, or claim that the instance is compute-ready. Install the agent using the deployment procedure and register it separately after network reachability and KVM capability are verified. A `t3.micro` is a controller/connectivity test profile, not a KVM/Cloud Hypervisor host.

EC2 lifecycle status is read from EC2, not inferred from worker heartbeat. Start and stop are explicit. Termination is intentionally not exposed in the initial console workflow; terminate instances through a separately reviewed AWS operation. Removing a worker registration never changes EC2 resources.

## Verification

Unit tests cover input validation. A real launch incurs AWS charges and requires an account, IAM permissions, a compatible AMI, subnet, and security group; no live AWS instance is created by CI.
