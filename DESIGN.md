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
| Rating | **Subsonic API** `setRating` | MM 0–100 → 0-10 half-star step (`mm.ToRatingStep`) → 0–5 via user-configurable `Config.RatingMap` (UI presets: round down/up, or a custom per-step table) |
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
  - `Rating`: 0–100, `-1`/NULL = unrated (`mm.ToRatingStep`).
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
/internal/listenbrainz  ListenBrainz submit-listens client (§11, independent of the pipeline above)
/web              embedded frontend
```

## 10. Fixtures & test data (local, git-ignored under `/local/`)
- `local/masters/` — pristine real DBs (`MM5.DB`, `navidrome.db`), chmod 444, never written.
- `local/work/` — throwaway copies the app/tests point at.
- `scripts/reset-fixtures.sh` — refresh `work/` from `masters/` before each run, so every dry-run/real-run starts clean. (`navidrome.db` is the only one we mutate.)
- Real scale: ~20.3k MM songs vs ~20.3k Navidrome files — counts align, so a real dry-run is a meaningful matching test.

## 11. Play History view + ListenBrainz backfill (independent path)
- **Not part of the pipeline above.** MediaMonkey's `Played(IDSong, PlayDate, UTCOffset)` table (per-play history) has no Navidrome equivalent — `annotation.play_date` only stores the single most recent play — and MediaMonkey itself has no UI to browse it. This path needs only `MM5.DB`: no music root, no Navidrome server/db, no scope.
- **Timestamps are real UTC instants**, unlike `Songs.LastTimePlayed`: `Played.UTCOffset` carries a genuine per-row offset (days, local-minus-UTC), so `UTC = mmEpoch + PlayDate - UTCOffset` (`mm.FromMMPlayDate`). `Songs.Artist/SongTitle/Album` cover display metadata directly — no join to `Artists`/`ArtistsSongs` needed.
- **Play History view**: a read-only, searchable/paginated table (`/api/history/plays`) — also doubles as a preview of what ListenBrainz submission would send.
- **ListenBrainz submission**: `POST https://api.listenbrainz.org/1/submit-listens` (`internal/listenbrainz`), `Authorization: Token <user token>`, `listen_type: "import"`, batched to ≤1000 listens/request. Direct API, not a CSV/file import — matches how ListenBrainz's own importers and third-party tools (e.g. `juho05/export-to-listenbrainz`, which also reads from Navidrome) work under the hood. Track metadata is text-only for v1 (artist/track/release name); no MusicBrainz `recording_mbid` (would require parsing `Songs.ExtendedTags`, deferred).
- **Idempotency**: ListenBrainz dedups server-side by (timestamp, track name, user), so re-submitting the same export is safe in the common case — a best-effort guarantee, not a hard one, so the UI offers a small test-batch submit (N most recent plays) to verify against a real account before trusting it with the full run, rather than a silent full resubmission.
- **This is a one-time backfill**, not live scrobbling. Navidrome has its own native forward-looking ListenBrainz integration (point it at the same account, configured in Navidrome itself) for everything from the migration point onward — this tool doesn't attempt to keep syncing new plays.

## 12. Open items
- ~~Confirm Navidrome `media_file.path` abs vs relative~~ → **library-relative** (resolved).
- Multi-library: `media_file.library_id` + `library` table exist; single-library assumed for v1.
- Rating granularity: half-stars unavoidably lost (Navidrome rating is 0–5 int).
- Multi-user handling beyond a single target user (defer).
```
