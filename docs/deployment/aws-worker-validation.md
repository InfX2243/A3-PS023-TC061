# AWS Worker Feasibility Gate (T-01 / T-02)

Status: **candidate selected; runtime feasibility not yet proven**. This document deliberately does not claim that any EC2 instance has been launched or that a guest has booted.

## Decisions for the first short-lived feasibility run

| Item | Selected candidate | Reason / boundary |
|---|---|---|
| AWS Region | `ap-south-1` (Mumbai) | Selected as the initial region; confirm quota, AZ capacity, and current pricing before launch. |
| Worker instance type | `c8i.xlarge` | AWS lists C8i among nested-virtualization-capable families and lists C8i in Mumbai. This is a candidate, not proof that a launched instance exposes working KVM. |
| Worker OS / architecture | Ubuntu Server 24.04.5 LTS, x86-64 | LTS host with a current supported kernel line; record the actual AMI ID and booted kernel from the launched instance. |
| Cloud Hypervisor | **v54.0** | Pin this release for the prototype; use the matching official x86-64 release artifact and record its SHA-256. Do not use an unpinned `latest` download. |
| Test guest disk | Ubuntu Server 24.04 LTS cloud image, x86-64, release build dated **2026-10-17** | Use the versioned artifact `ubuntu-24.04-server-cloudimg-amd64.img` from the official release directory, not the mutable `noble/current` alias. Verify the artifact against the release's signed `SHA256SUMS`, then record the exact SHA-256 in the run evidence. |
| Guest sizing for smoke test | 1 vCPU, 1024 MiB RAM | Small validation guest only; does not establish production sizing or final capacity defaults. |
| AWS spend policy | One temporary worker at a time; no long-lived worker pair until pass | Tag resources for the hackathon and terminate the temporary instance and delete temporary volumes after evidence is captured. |

References:
- AWS nested virtualization (global AWS documentation): https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nested-virtualization.html
- AWS nested virtualization (China documentation also reviewed): https://docs.amazonaws.cn/en_us/AWSEC2/latest/UserGuide/amazon-ec2-nested-virtualization.html
- AWS instance availability by Region: https://docs.aws.amazon.com/ec2/latest/instancetypes/ec2-instance-regions.html
- Cloud Hypervisor releases: https://github.com/cloud-hypervisor/cloud-hypervisor/releases
- Ubuntu pinned cloud image build: https://cloud-images.ubuntu.com/releases/releases/noble/release-20261017/

## Preflight: confirm instance capability and budget

Run these commands from an authenticated AWS CLI environment. They query metadata only; they do not create resources.

```bash
export AWS_REGION=ap-south-1
aws sts get-caller-identity
aws ec2 describe-instance-types \
  --region "$AWS_REGION" \
  --instance-types c8i.xlarge \
  --query 'InstanceTypes[0].{Type:InstanceType,VCPUs:VCpuInfo.DefaultVCpus,MemoryMiB:MemoryInfo.SizeInMiB,Features:ProcessorInfo.SupportedFeatures}' \
  --output json
aws ec2 describe-availability-zones --region "$AWS_REGION" \
  --query 'AvailabilityZones[].{Zone:ZoneName,State:State}' --output table
aws ec2 describe-images --region "$AWS_REGION" \
  --owners 099720109477 \
  --filters 'Name=name,Values=ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*' 'Name=state,Values=available' \
  --query 'Images | sort_by(@,&CreationDate)[-5:].[*].[ImageId,Name,CreationDate,Architecture]' \
  --output table
```

Select a current official Ubuntu AMI from that result and record its exact AMI ID, creation date, architecture, and availability zone. Check the current On-Demand price and service quotas before launch; do not assume a quota or capacity reservation exists.

## Launch and host evidence

Launch **one temporary** `c8i.xlarge` worker with nested virtualization explicitly enabled (`--cpu-options "NestedVirtualization=enabled"`), Ubuntu x86-64, encrypted EBS, IMDSv2 required, a restrictive security group, and a time-limited resource tag. Do not put SSH keys, tokens, or other secrets in this document. Do not open the Cloud Hypervisor socket or worker API to the public Internet.

On the instance, capture the outputs (remove public IPs, account IDs, and other sensitive data before committing):

```bash
date -Is
uname -a
cat /etc/os-release
uname -r
lscpu
grep -E -m1 -o 'vmx|svm' /proc/cpuinfo || true
ls -l /dev/kvm
test -r /dev/kvm && test -w /dev/kvm
cloud-hypervisor --version
sha256sum /usr/local/bin/cloud-hypervisor
```

A missing `/dev/kvm`, missing CPU virtualization flags, or failed read/write check is a **failure**, not a reason to continue to the guest test. Capture the actual error and stop/terminate the temporary instance if the candidate is unusable.

## Guest-image integrity and boot/stop acceptance

1. Download only the versioned image and `SHA256SUMS` / `SHA256SUMS.gpg` from the 2026-10-17 Ubuntu release directory.
2. Verify the checksum file's GPG signature using Ubuntu's documented signing-key verification procedure, then verify the disk image against the matching SHA-256 entry.
3. Record the full image URL, filename, file size, SHA-256, and verification result. Use that SHA-256 as the image identifier in the prototype allowlist; `ubuntu-demo` may be retained only as a human-readable alias to this exact artifact.
4. Use the v54.0 API/CLI and the selected disk image with a documented, reproducible configuration. Capture the exact command/configuration with secrets removed.
5. Prove all of the following on the same instance: Cloud Hypervisor process starts; API status reports the VM running; the guest reaches its console/login or a defined guest-ready check; a stop request completes; a subsequent status query reports stopped/exited; the process is cleaned up.
6. Record the instance ID, instance type, region/AZ, AMI ID, kernel, VMM version and binary SHA-256, guest image SHA-256, commands, timestamps, status outputs, and cleanup result in a non-secret evidence note.

Do not mark T-01 complete based only on metadata, unit tests, or a successful Cloud Hypervisor process start. A boot and stop cycle must pass.

## T-02 lock criteria

The candidate choices above are the prototype's selected baseline. T-02 remains incomplete until:
- the selected instance and host kernel pass T-01;
- Cloud Hypervisor v54.0's official binary checksum is recorded and the adapter's expected API endpoints are checked against v54.0 documentation/schema;
- the guest disk's signed release checksum is recorded and the guest boots/stops successfully;
- the final image allowlist uses an immutable digest/identifier rather than a mutable alias;
- CPU, memory, safety reserve, auth, and network defaults are reviewed against the successful host test.

If C8i.xlarge cannot provide usable KVM, do not proceed with a second worker. Reassess a supported EC2 bare-metal type or a different AWS nested-virtualization-capable type, then repeat the same gate.
