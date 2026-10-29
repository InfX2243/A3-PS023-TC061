# AIORI-3-HACKATHON-PROJECTS

This repository contains the code and resources for projects developed during the AIORI-3 Hackathon.

## 🎥 Project Presentation

**Project:** Minimal Two-Node Cloud Hypervisor VM Controller

**Team:** CloudOpsCrew

Watch our project presentation for AIORI Hackathon 3 on YouTube:

[▶️ Watch the Project Presentation](https://youtu.be/x6dSHQatmrY)

## 👥 Team CloudOpsCrew

- **Abid Shaikh** — Controller Architecture & Integration
- **Umar Ansari** — Worker Agents & VM Lifecycle
- **Mentor:** Dr. Zainab Mirza

## 🎯 Project Overview

The Minimal Two-Node Cloud Hypervisor VM Controller aims to provide a lightweight, secure, and reliable solution for managing virtual machines across two KVM-enabled Linux hosts.

The project focuses on:

- Controller architecture and system integration
- Memory-aware host selection and resource reservations
- Worker-agent communication and VM lifecycle management
- Timeout handling, failure recovery, and resource cleanup
- Reliable, resource-aware virtual machine orchestration

Developed as part of **AIORI Hackathon 3**.

## 🛠️ MVP implementation plan

The current planning branch contains the implementation requirements, architecture/design, dependency-ordered task checklist, AI-driven development workflow, and AWS deployment plan.

Start with the [MVP documentation index](docs/README.md) and complete one verified task at a time using [docs/tasks.md](docs/tasks.md).

**AWS prerequisite:** Cloud Hypervisor requires KVM. The deployment plan begins with validating `/dev/kvm` and booting the intended guest on the exact candidate worker configuration before provisioning the full environment.
