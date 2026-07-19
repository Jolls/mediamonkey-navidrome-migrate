# MediaMonkey 5 → Navidrome Migration Tool — Design

## 1. Goal
- One-shot migration of per-track data from **MediaMonkey 5** into **Navidrome**.
- Assumption: both point at the **same music files** on the same machine.
- Migrates: ratings, play counts, last-played dates. (Playlists = phase 2.)

## 2. Stack
- **Go**, single static binary per OS (Windows/Linux).
- **Local web UI**: binary serves an embedded frontend (`embed.FS`) on `127.0.0.1`, opens browser.
- **SQLite**: `modernc.org/sqlite` (pure Go, **no cgo** → trivial cross-compile).
- HTTP: stdlib `net/http` (+ `chi` router optional). No heavy framework.

## 3. Write strategy — API-first, direct DB only as fallback
| Data | Path | Notes |
|---|---|---|
| Rating | **Subsonic API** `setRating` | Convert MM 0–100 → 1–5 (`round(r/20)`); half-stars lost either way |
| Favorite / starred | **Subsonic API** `star` (optional, off by default) | MM has no true "favorite"; opt-in mapping |
| Play count + last-played | **Direct write** to `annotation` | API `scrobble` can't set exact counts or backdate — unavoidable. **Set (overwrite), never add** → keeps re-runs idempotent |
| Playlists | Phase 2 | Native `.m3u` auto-import already covers this |

## 4. Track matching
- Matching key = normalized (lowercase, forward-slash) **library-relative path**.
- **Navidrome side:** `media_file.path` is *already* library-relative (verified against a real DB) → just normalize; **do not strip a root**. Library root lives in the separate `library` table.
- **MM side:** `Songs.SongPath` is an absolute Windows path → user picks the **MM music root**; strip it to get the relative key.
- The two align because both libraries point at the same files, so the sub-tree under each root is identical.
- Fallback signal: MusicBrainz recording ID (`mbz_recording_id`) when present.
- Output: matched / ambiguous / unmatched buckets for UI review + dry-run.

## 5. Data sources
- **MM5.DB** (read-only, schema verified): `Songs` — `SongPath`, `Rating`, `PlayCounter`, `LastTimePlayed`.
  - `SongPath`: absolute Windows path with the **drive letter blanked** (`:\My Music\Artist\...`); the root chosen for stripping is in this stored form.
  - `Rating`: 0–100, `-1`/NULL = unrated (`mm.FromMMRating`).
  - `LastTimePlayed`/`DateAdded`: **TDateTime float** = days since 1899-12-30, `0` = never (`mm.FromMMDate`). Not Unix time.
  - No MBID column (buried in `ExtendedTags`) → MM-side MBID matching deferred.
  - `Played(IDSong, PlayDate, UTCOffset)`: per-play history — future source for backdated per-scrobble fidelity.
- **navidrome.db** (schema verified against a real DB):
  - read-only: `media_file(id, path [library-relative], mbz_recording_id, missing)`, `user(id, user_name)`, `library(id, path)`.
  - read-write: `annotation(user_id, item_id, item_type='media_file', play_count, play_date, rating, starred, starred_at, rated_at)`. PK = `(user_id, item_id, item_type)`.
  - Play-count upsert touches **only** `play_count`/`play_date` so an API-set `rating`/`starred` on the same row survives.

## 6. Config (collected in UI)
- Navidrome server URL + username/password (Subsonic API).
- Path to `navidrome.db` (matching + play-count writes).
- Path to `MM5.DB`.
- Shared music root folder.
- Target Navidrome **user** (annotations are per-user).

## 7. Safety rails (direct DB writes)
- Refuse to run while Navidrome is up (detect DB lock / WAL).
- Timestamped backup of `navidrome.db` before first write.
- Single transaction; commit only after dry-run approval.

## 8. Scope-based workflow
- **Scope path** = any subfolder under the shared root (default: a single album folder). Every dry-run and commit operates *only* on tracks whose relative path falls under the current scope.
- Lets the user verify one album end-to-end before widening to the whole library.

1. Configure sources + pick user.
2. Pick a **scope** (album subfolder to start).
3. Scan & match within scope → review buckets.
4. Choose fields to migrate.
5. **Dry-run** that scope → user examines full report.
6. **Commit** that scope (API writes + direct-DB writes).
7. Widen scope (another subfolder, or whole root) and repeat.
- Commits are idempotent (re-running a scope overwrites, doesn't double-count) so overlapping/nested scopes are safe.

## 9. Module layout
```
/cmd/app          main: start server, open browser
/internal/mm      read MM5.DB → []Track
/internal/nav     navidrome.db read + annotation writes
/internal/subsonic Subsonic API client (setRating, star)
/internal/match   matching strategies + scoring
/internal/api     JSON handlers (scan, preview, dry-run, commit)
/web              embedded frontend
```

## 10. Fixtures & test data (local, git-ignored under `/local/`)
- `local/masters/` — pristine real DBs (`MM5.DB`, `navidrome.db`), chmod 444, never written.
- `local/work/` — throwaway copies the app/tests point at.
- `scripts/reset-fixtures.sh` — refresh `work/` from `masters/` before each run, so every dry-run/real-run starts clean. (`navidrome.db` is the only one we mutate.)
- Real scale: ~20.3k MM songs vs ~20.3k Navidrome files — counts align, so a real dry-run is a meaningful matching test.

## 11. Open items
- ~~Confirm Navidrome `media_file.path` abs vs relative~~ → **library-relative** (resolved).
- Multi-library: `media_file.library_id` + `library` table exist; single-library assumed for v1.
- Rating granularity: half-stars unavoidably lost (Navidrome rating is 0–5 int).
- Multi-user handling beyond a single target user (defer).
```
