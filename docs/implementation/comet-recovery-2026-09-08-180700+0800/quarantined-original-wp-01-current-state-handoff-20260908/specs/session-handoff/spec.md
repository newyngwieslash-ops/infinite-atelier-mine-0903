# Session handoff specification — current-state supplement

## Artifact

The repository retains immutable timestamped handoff Markdown files under `docs/implementation`. Each handoff records the worktree and verified conclusions at its own point in time; older handoffs remain historical evidence and are never rewritten to match later implementation.

## Current-state requirements

The current handoff must identify the repository path, branch, HEAD, active work package, exact resume point, Git/data safety state, completed work, incomplete work and reviews, environment limitations, WP-01 remaining tasks, PRD-aligned WP-02–WP-12 work, release blockers, and a copyable resume prompt.

It must state that Task 1–8 are approved, but Task 9 is blocked until trustworthy direct production-Wails evidence exists for `/assets`, `/canvas`, `/director`, and `/config`. It must not substitute source route registration, blind coordinate input, test-only behavior, or unowned-process manipulation for native route rendering evidence.

## Safety

The handoff itself must not claim product completion, change implementation/dependencies/user data/secrets/media/Provider configuration, or perform Git history-changing/cleanup operations. The host race compilation problem must remain recorded as an environment failure: the available 32-bit MinGW GCC cannot compile amd64 CGO.

## Resumption

The resume prompt must require re-reading the latest handoff and mandatory repository documentation, re-checking Git, preserving the dirty worktree, and completing only Task 9 through serial implementer → independent spec reviewer → independent quality reviewer gates. Task 10 cannot begin before both Task 9 reviews approve. WP-02 cannot begin automatically after WP-01.
