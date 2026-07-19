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
- User selects **one shared music root folder**.
- Read-only from `navidrome.db` → `media_file(id, path)`; read `MM5.DB` → `Songs(SongPath, ...)`.
- Compute **path relative to shared root** on both sides; match on that.
- Fallback signal: MusicBrainz recording ID (`mbz_recording_id`) when present.
- Output: matched / ambiguous / unmatched buckets for UI review + dry-run.

## 5. Data sources
- **MM5.DB** (read-only): `Songs` — `SongPath`, `Rating` (0–100), `PlayCounter`, `LastTimePlayed`.
- **navidrome.db**:
  - read-only: `media_file(id, path, mbz_recording_id)`, `user(id, user_name)`.
  - read-write: `annotation(user_id, item_id, item_type='media_file', play_count, play_date, rating, starred, starred_at)`.

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

## 10. Open items
- Confirm Navidrome `media_file.path` is absolute vs library-relative (affects root-strip logic).
- Rating granularity: half-stars unavoidably lost (Navidrome rating is 0–5 int).
- Multi-user handling beyond a single target user (defer).
```
