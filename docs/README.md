# MVP Implementation Documentation

Project: **PS-023 — Minimal Two-Node Cloud Hypervisor VM Controller**  
Repository: `InfX2243/A3-PS023-TC061`  
Planning branch: `docs/mvp-requirements-design-task-plan`

## Start here

1. [Requirements](./requirements.md) — scope, user stories, functional and non-functional requirements, acceptance criteria.
2. [Design](./design.md) — MVP architecture, API, state model, placement, security, and AWS deployment design.
3. [Task plan](./tasks.md) — ordered, atomic tasks with dependencies and definition of done.
4. [AI-driven development workflow](./ai-development-workflow.md) — how to implement and review one task at a time.
5. [AWS deployment plan](./aws-deployment.md) — prerequisites, network layout, rollout, validation, and cost guardrails.
6. [AWS worker feasibility gate](./deployment/aws-worker-validation.md) — selected EC2 candidate, pinned VMM/guest candidates, and required runtime evidence.
7. [Cloud Hypervisor v54.0 API contract](./cloudhypervisor-api-v54.md) — pinned endpoint and schema contract used by the Go adapter.
8. [Security checklist](./security-checklist.md) — code controls and deployment-only checks.
9. [Worker deployment runbook](./deployment/worker-deployment.md) — systemd setup, worker registration, acceptance sequence, and teardown.

## MVP north star

Deliver a working web interface and authenticated API that can create, start, stop, and list VMs across two worker hosts, using Cloud Hypervisor and KVM, with memory-aware placement and persistent operation tracking.

## Important feasibility gate

Cloud Hypervisor requires access to KVM. The current candidate is one temporary `c8i.xlarge` in `ap-south-1`, with Cloud Hypervisor v54.0 and a pinned Ubuntu 24.04 LTS cloud-image build. AWS documents nested virtualization for supported families, but the candidate remains unverified until `/dev/kvm` and a real guest boot/stop cycle pass. See the [AWS worker feasibility gate](./deployment/aws-worker-validation.md).

## Current status

Implementation is proceeding ahead of full local and AWS verification per user direction. The task plan distinguishes implemented code from unexecuted tests and deployment-only evidence; no pending check should be interpreted as passing.
