1. Think Before Coding: don't assume/hide confusion. State assumptions; if multiple interpretations, present don't pick; suggest simpler approach if exists, push back when warranted; if unclear, stop and ask.

2. Simplicity First: minimum code, nothing speculative. No unrequested features/abstractions/flexibility/error-handling for impossible cases. If 200 lines could be 50, rewrite. Test: "would a senior engineer call this overcomplicated?"

3. Surgical Changes: touch only what's needed. Don't improve/refactor/reformat adjacent code; match existing style; mention unrelated dead code, don't delete it. Remove imports/vars/funcs YOUR change orphaned; don't remove pre-existing dead code unless asked. Every changed line should trace to the request.

4. Goal-Driven Execution: define verifiable success criteria, loop until met (e.g. bug fix → repro test → make pass; feature → tests for invalid inputs → pass). For multi-step tasks state brief plan with verify step per item.

5. Tests after bug fixes/features: suggest a regression test when it'd meaningfully catch breakage (non-obvious edge cases, silent-break logic) — briefly, and only write if user agrees. Skip for trivial/UI-only/well-covered changes.

6. Token-Efficient Messages: terse, no preamble/restating/unrequested trailing summary, no just-in-case caveats. Alternatives welcome (standard/idiomatic ones) but skip esoteric ones unless asked. Prefer short direct statements over headers/bullets unless content has genuinely distinct parts.

7. Model Selection: default Sonnet.

# MediaMonkey → Navidrome Migration Tool

Go tool to migrate library data (ratings, play counts, playlists, etc.) from MediaMonkey (`MM5.DB`, SQLite) into Navidrome (`navidrome.db`, SQLite).

See [DESIGN.md](DESIGN.md) for architecture/approach and [HANDOFF.md](HANDOFF.md) for current status.

## Layout
- `cmd/` — entrypoint(s)
- `internal/` — core migration logic
- `web/` — any web UI assets
- `scripts/` — helper scripts
- `local/` — local working copies of source/target `.db` files (gitignored, not test fixtures — don't treat as reference data)

## Plans
Save implementation plans (Plan Mode, issue-tied) to `docs/plans/<issue-id>-<description-stem>.md`.

## Branching
Never commit to main. Before first commit in session, check current branch; if on main, create the branch yourself using the standard below — don't ask for a name. Naming: `feature/<issue-id>-<short-slug>` when the work maps to a GitHub issue, else `feature/<short-description>`. Push branch + open PR, never push main directly.

## Pre-commit sequence
1. go build/vet/test pass.
2. Present multi-select question for review passes to run (recommend based on diff risk, mark "(Recommended)"), then run chosen ones in order, summarize findings:
   - `/code-review low` — cheap high-confidence pass, no agent spawn. Default for small/low-risk diffs.
   - `/code-review medium` — broader pass. Default for normal feature branches. Spawn agents only if token-efficient to do so.
   - `/code-review high`/Opus-High single agent — deep review. Default for large/risky/cross-cutting changes.
   - `/simplify` — quality-only cleanup (reuse/simplification/efficiency/altitude), no bug-hunting. Combine with a code-review pass or standalone for cleanup diffs.
   - Skip review.
3. Pause for user manual testing.
4. On approval, commit. Never commit without the user's explicit go-ahead, even mid-task or when the diff looks done.

## Changelog
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Update CHANGELOG.md once per PR/merge (not per commit); all branch changes under one version entry, grouped under `### Added`/`### Changed`/`### Fixed`/`### Removed`/`### Security`/`### Deprecated` subheadings (only include the subheadings you need). Format:
```
## [0.1.X] - YYYY-MM-DD
### Added
- <one-line summary>
```

## What NOT to touch
`local/*.db` files are working copies of real user data — never overwrite/delete without confirmation, never treat as test fixtures to commit.
