# Handoff — implementing the stubs

The skeleton wires the pipeline; the brain (matching, conversions, scope, the
API-first/direct-DB split) is done. What remains is mechanical fill-in, each
marked `TODO(sonnet)` in the code. Do them in the order below. See
[DESIGN.md](DESIGN.md) for the why; this file is the worklist + acceptance bars.

Verified environment: Go 1.26, `go build ./...` and `go vet ./...` are green now
and must stay green after every task.

## Test data & how to run

Real DBs live git-ignored under `/local/` (schemas already verified):
- `local/masters/` — pristine originals, read-only.
- `local/work/` — writable copies; regenerate with `scripts/reset-fixtures.sh`.

**Always** `bash scripts/reset-fixtures.sh` before a run that writes, so
`local/work/navidrome.db` starts clean. `MM5.DB` is only ever read.

Acceptance numbers come from a validated Python prototype of the match (run
against the real data): **20278 / 20290 MM tracks match 1:1 by relative path,
0 ambiguous, 12 unmatched** (one renamed folder), **18138 matched tracks carry a
rating and/or play data**. The Go readers must reproduce these.

---

## Task 1 — SQLite readers (unblocks a real dry-run)

Add `modernc.org/sqlite` (pure Go, no cgo — keep it that way): `go get modernc.org/sqlite`.

### 1a. `internal/mm` — `Open` / `ReadTracks`
- Read-only DSN: `file:<path>?mode=ro&immutable=1`.
- `SELECT SongPath, Rating, PlayCounter, LastTimePlayed FROM Songs`.
- Per row → `model.Track`:
  - `RelPath`: `match.NormalizeRel(SongPath, root)`. Root is in MM's stored form
    with a blanked drive, e.g. `":\My Music"`. Skip rows where `ok == false`
    or `SongPath` is NULL.
  - `OrigPath`: raw `SongPath`.
  - `Rating`: `mm.FromMMRating(Rating)` (already implemented).
  - `PlayCount`: `PlayCounter`.
  - `LastPlayed`: `mm.FromMMDate(LastTimePlayed)` (already implemented).
  - `MBID`: `""` (not in MM5).

### 1b. `internal/nav` — `OpenReader` / `ReadTracks` / `Users`
- Read-only DSN as above.
- `ReadTracks()` (no root): `SELECT id, path, mbz_recording_id FROM media_file
  WHERE COALESCE(missing,0)=0` → `model.NavTrack{ID:id, RelPath:match.Normalize(path),
  MBID:mbz_recording_id}`. **Do not strip a root** — `path` is already
  library-relative.
- `Users()`: `SELECT id, user_name FROM user`.

**Acceptance:** a tiny throwaway `main` (or a test) that builds a `migrate.Pipeline`
with `MMRoot = ":\\My Music"`, `Fields = all`, runs `DryRun(Scope{})`, and prints
`Matched/Ambiguous/Unmatched`. Must report **Matched 20278, Ambiguous 0,
Unmatched 12** against `local/masters`. `len(Changes)` == 20278.

---

## Task 2 — the write side

### 2a. `internal/subsonic` — `Client`
- Subsonic auth: token = `md5(password + salt)`, params `u,t,s,v,c,f=json`.
- `Ping`: `GET rest/ping.view`; parse `subsonic-response.status == "ok"`.
- `SetRating`: `GET rest/setRating.view?id=<navID>&rating=<0-5>`.
- `Star`/unstar: `GET rest/star.view` / `rest/unstar.view?id=<navID>`.
- Surface non-`ok` responses as errors (include the subsonic error code/message).

### 2b. `internal/nav` — `OpenWriter` / `SetAnnotation` + safety
- Read-write DSN (no `mode=ro`).
- Upsert (SQL already in the `OpenWriter` doc): touch **only** `play_count` /
  `play_date` so an API-set `rating`/`starred` on the same row survives. Pass
  `play_date = NULL` when `LastPlayed.IsZero()`.
- `EnsureUnlocked(dbPath)`: fail if Navidrome is live (inspect `-wal`/`-shm` or
  try a `BEGIN EXCLUSIVE`). Wire it + `Backup` into the commit entry point so a
  real run refuses to race a running server and always backs up first.

**Acceptance:** after `reset-fixtures.sh`, a scoped `Commit` on `local/work`
sets the expected rating (via API, needs a running Navidrome pointed at
`local/work/navidrome.db`) and the expected `play_count`/`play_date` (direct);
re-running the same scope is a no-op (idempotent — values unchanged).

---

## Task 3 — the UI

- `cmd/app/main.go`: mount `/api/*` on the existing mux, backed by a
  `migrate.Pipeline` built from posted `model.Config`. Endpoints: `POST
  /api/config`, `GET /api/users`, `GET /api/scan?scope=`, `GET /api/dryrun?scope=`,
  `POST /api/commit`.
- `web/`: replace the placeholder with the real flow (config form → user pick →
  scope pick → scan/dry-run review buckets → commit). Assets embed via
  `web.FS`; add files under `web/` and extend the `//go:embed` pattern.
- Make the star mapping (`migrate.change`, currently `rating >= 5`) a config
  option.

---

## Guardrails
- Keep it **cgo-free** (pure-Go sqlite) so cross-compilation stays trivial.
- Never write `MM5.DB`. Only ever write a `local/work` copy of `navidrome.db`.
- Writes are **set, not add** — re-running any scope must stay idempotent.
