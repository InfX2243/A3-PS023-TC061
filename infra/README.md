# AWS deployment with CloudFormation

This directory is the infrastructure entry point for Aiori Compute. Deploy the stack instead of manually creating a VPC, subnets, security groups, EC2 controller, instance profiles, and the worker IAM boundary.

## What the stack creates

- A dedicated VPC with two public subnets, an internet gateway, and routing.
- An HTTPS Application Load Balancer in front of the controller. The controller security group accepts port 8080 only from the ALB security group.
- A controller EC2 instance with IMDSv2 required, encrypted persistent gp3 root storage, and an instance profile.
- A worker security group whose agent port 9090 is allowed only from the controller security group.
- A worker IAM role/profile with narrowly scoped SSM access to the worker shared-secret parameter and the CloudWatch Agent managed policy.
- Controller IAM permissions for EC2 lifecycle/provisioning, restricted PassRole to the worker role, SSM secret setup, and CloudWatch read operations.
- CloudWatch log groups and a CloudWatch dashboard resource.

The worker-agent port is never opened to the public internet. The ALB is the only public ingress path; restrict its CIDR to your trusted office/VPN range.

## Prerequisites

1. Choose an AWS region and ensure EC2, ALB, ACM, SSM Parameter Store, and CloudWatch are available there.
2. Register a domain name and create an ACM certificate in the same region. Complete DNS validation before deployment.
3. Create a DNS record for the chosen hostname pointing to the ALB DNS name returned by the stack.
4. Publish an immutable Linux amd64 controller binary over HTTPS and calculate its SHA-256:
   ```sh
   sha256sum aiori-controller
   ```
5. Keep release artifacts version-pinned. Do not use mutable `latest` URLs or signed URLs with short expirations.

## Deploy

Validate the template first:

```sh
aws cloudformation validate-template --template-body file://infra/template.yaml --region ap-south-1
```

Deploy (replace values; use the correct region and a trusted ingress CIDR):

```sh
aws cloudformation deploy \
  --template-file infra/template.yaml \
  --stack-name aiori-compute \
  --region ap-south-1 \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides \
    OperatorIngressCidr=YOUR_PUBLIC_IP/32 \
    DomainName=compute.example.com \
    CertificateArn=arn:aws:acm:ap-south-1:ACCOUNT_ID:certificate/CERTIFICATE_ID \
    ControllerArtifactUrl=https://ARTIFACT_HOST/releases/aiori-controller \
    ControllerArtifactSha256=64_CHARACTER_SHA256
```

The current first-cut stack accepts the controller artifact URL and checksum. The worker agent, Cloud Hypervisor, and guest disk artifacts are intentionally separate inputs to the worker-bootstrap configuration; do not launch compute workers until those immutable artifacts and their matching digests are configured. The application must also receive the stack's worker subnet, worker security group, worker instance profile, and controller security group as deployment defaults. These values are available as CloudFormation outputs and are not secrets.

Retrieve outputs:

```sh
aws cloudformation describe-stacks \
  --stack-name aiori-compute --region ap-south-1 \
  --query 'Stacks[0].Outputs' --output table
```

Create the DNS alias/CNAME for `DomainName` using `LoadBalancerDNSName`. Wait for the ALB target to become healthy, then open `ControllerURL`.

Retrieve the initial operator credentials (use a secure terminal and do not paste the values into chat):\n\n\`\`\`sh\naws ssm get-parameter --name /aiori/operator/password --with-decryption --query 'Parameter.Value' --output text --region ap-south-1\naws ssm get-parameter --name /aiori/operator/token --with-decryption --query 'Parameter.Value' --output text --region ap-south-1\n\`\`\`\n\n## Security and operational notes

- The bootstrap generates distinct random operator token, operator password, and worker shared secret. The worker shared secret is stored as an SSM SecureString; it is never put in EC2 user data.
- The generated operator password and token are written as separate SSM SecureString parameters. Retrieve them only from an authorized workstation and keep them out of shell history and source control:
- Controller and worker IAM roles are attached to EC2; do not add long-lived AWS access keys to `.env`.
- IMDSv2 is required on the controller. Keep the worker-agent security group source restricted to the controller security group.
- CloudWatch dashboard and log groups are provisioned by the template. EC2 basic CPU/network/disk-I/O metrics are available automatically; memory and filesystem metrics require the CloudWatch Agent to be installed and configured on each host.
- CloudWatch ingestion, ALB hours, EC2, EBS, and public IPv4 addresses incur AWS charges. Delete the stack when finished testing, after backing up the controller database and reviewing retained EBS volumes.
- The template is a starting point for a hackathon deployment, not a hardened multi-account production landing zone. Review CIDRs, IAM, TLS, backups, retention, and network egress before production use.
