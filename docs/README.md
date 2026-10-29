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

## MVP north star

Deliver a working web interface and authenticated API that can create, start, stop, and list VMs across two worker hosts, using Cloud Hypervisor and KVM, with memory-aware placement and persistent operation tracking.

## Important feasibility gate

Cloud Hypervisor requires access to KVM. Before committing to an AWS instance family, prove that the chosen worker instance exposes usable `/dev/kvm` and can run the target guest image. If this is not supported on the selected AWS configuration, stop and choose a supported bare-metal / virtualization-capable configuration or use local KVM hosts for the workers while keeping the controller on AWS. Do not assume ordinary EC2 virtual instances provide nested KVM.

## Current status

These documents define the proposed MVP and implementation sequence. They do not imply that implementation, AWS deployment, or acceptance tests have already completed. Update task checkboxes and evidence links only after verification.
