# Task Management

- Use Linear as the single source of truth for tasks in this repository.
- Tag all `wtx` repository tasks with the Linear issue label `wtx`.
- Prefer creating/updating/closing Linear issues instead of maintaining a local task list for active work.

# Testing Notes

- `local-e2e` tests are local-only and must be run manually with `make local-e2e` (or `go test -tags local_e2e ./e2e` with `WTX_LOCAL_E2E=1`).
- Keep `local-e2e` scenarios isolated from this repo by creating and using temporary test repositories.
- For any code changes in this repository, run `make local-e2e` before reporting completion (unless the user explicitly asks to skip it).
