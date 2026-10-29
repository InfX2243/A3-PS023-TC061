# AI-Driven Development Workflow

## 1. Operating principle

Use AI as a pair programmer, not as an unchecked code generator. The workflow is intentionally sequential: one task, one bounded change, one verification pass, then review and commit.

## 2. Task loop

For every task in [tasks.md](./tasks.md):

1. **Read context:** Read requirements, design, the task, related source files, and current test/build commands.
2. **State the plan:** Ask the AI to summarize the task, dependencies, files likely to change, risks, and acceptance tests before editing.
3. **Check assumptions:** Verify version-sensitive Cloud Hypervisor/AWS behavior against official documentation or an executable test. Do not invent API endpoints or environment capabilities.
4. **Implement one task only:** Avoid unrelated refactors, dependency changes, or speculative features.
5. **Test:** Run formatter, unit tests, targeted integration tests, static checks, and build steps appropriate to the task.
6. **Review diff:** Inspect every changed file. Look for unsafe process invocation, path traversal, race conditions, leaked reservations, auth gaps, logging of secrets, and unsupported claims.
7. **Record evidence:** Put test commands/results and commit/PR link beside the task in `tasks.md`.
8. **Commit:** Use a focused commit message such as `T-11: add atomic memory placement`.
9. **Advance:** Start the next task only when acceptance criteria are satisfied.

## 3. Prompt template

Copy and fill this prompt for each task:

> We are implementing task **[TASK-ID and title]** from `docs/tasks.md`.
>
> Read `docs/requirements.md`, `docs/design.md`, `docs/tasks.md`, and the relevant current source files first.
>
> Constraints:
> - Implement this task only; do not start later tasks.
> - Preserve existing repository conventions and avoid unrelated refactors.
> - Confirm version-specific API and AWS assumptions rather than guessing.
> - Add tests for success paths, validation failures, and relevant failure/race conditions.
> - Do not add secrets or arbitrary shell/path execution.
> - Run the relevant formatter, tests, and build; report exact commands and outcomes.
> - If a requirement is ambiguous or blocked, stop and ask a focused question rather than silently changing scope.
>
> Before editing, give a short plan and list expected files. After editing, summarize changes, tests, remaining risks, and the acceptance criteria evidence for this task.

## 4. Repository and branch rules

- Keep this planning branch for documentation until the team decides whether to implement on it or create a dedicated feature branch.
- Use feature branches for meaningful implementation slices; avoid direct commits to `main`.
- Prefer one task per commit and a small pull request when collaboration/review is available.
- Do not mark a task done until tests and acceptance evidence exist.
- Keep generated documentation aligned with actual code. If code and docs conflict, identify the conflict and resolve it deliberately.
- Never paste credentials, SSH private keys, AWS secret keys, or production data into prompts or commits.

## 5. Required engineering checks

### Correctness
- Placement calculations use consistent units (bytes internally).
- Resource reservation and operation creation are atomic.
- A remote timeout is not treated as proof that an operation failed.
- Worker commands are idempotent or guarded by persisted operation identity.
- Unknown states retain reservations until reconciliation.

### Security
- No shell interpolation of user-controlled values.
- No arbitrary image path or host path supplied by browser clients.
- Controller and worker authentication is enforced.
- Cloud Hypervisor management socket remains local and permission-restricted.
- Secrets are supplied via protected runtime configuration or a secret store.
- Logs and API errors do not reveal credentials.

### Quality
- Tests cover invalid transitions, concurrency, restart recovery, and worker timeouts as applicable.
- Errors are actionable and structured.
- Code changes are minimal and formatted.
- A clean build and targeted test run are recorded.

## 6. When AI output fails

1. Capture the exact error, command, environment/version, and relevant logs with secrets removed.
2. Ask for a diagnosis and a minimal fix for the current task only.
3. Do not accept broad rewrites to hide a failing test.
4. Re-run the previously failing test and the relevant regression suite.
5. Update docs if the failure revealed an incorrect assumption.

## 7. Definition of a completed task

A task is complete only when:
- acceptance criteria pass,
- tests are reproducible,
- the diff is reviewed,
- no known critical security/correctness defect is introduced,
- and evidence is recorded in `docs/tasks.md`.

A generated patch by itself is not evidence of completion.
