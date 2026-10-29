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

## 🧑‍💻 Development setup and baseline

This branch is currently a planning/documentation baseline, not yet a buildable application. The repository files inspected for this baseline include the root README, the MVP task plan, and `src/README.md`; the latter is a placeholder instructing contributors to add implementation files.

### Inspect a clean checkout

```sh
git clone https://github.com/InfX2243/A3-PS023-TC061.git
cd A3-PS023-TC061
git status --short
find . -maxdepth 3 -type f -not -path './.git/*' | sort
```

For the current planning branch, check it out after cloning:

```sh
git fetch origin
git switch docs/mvp-requirements-design-task-plan
```

### Build and test status

- **Language/toolchain:** Not established in the inspected application files; no application source or dependency manifest is present in this baseline.
- **Build command:** Not available yet; no build configuration was found in the inspected files.
- **Test command:** Not available yet; no test configuration or test suite was found in the inspected files.
- **Runtime checks:** Not run. This documentation-only inspection does not establish KVM or Cloud Hypervisor availability.
- **Next step:** Follow T-01 in `docs/tasks.md` to collect worker-environment feasibility evidence before locking implementation decisions.

Do not treat the absence of build/test commands as a passing build or test result. Add the appropriate toolchain, build, and test commands when the implementation foundation is selected.
