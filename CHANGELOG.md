# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.12] - 2026-07-22

### Added
- `Songs.DateAdded` migration to `media_file.created_at`, via a new "Date
  added" field checkbox: direct SQLite write (no Subsonic API surface for
  this field), converted from MM's `TDateTime` format like `LastTimePlayed`.
- "Verify" button on the dry-run review step: compares MediaMonkey's Rating,
  Play count, Last played, and Date added against what's actually stored in
  navidrome.db right now, independent of which fields are currently checked
  and usable before or after a commit — surfaces tracks a previous commit
  missed or that got reset by a Navidrome rescan.

## [0.1.11] - 2026-07-22

### Fixed
- Maloja submission no longer loses plays to false-positive "duplicate"
  rejections: MediaMonkey's sub-second PlayDate precision was being
  truncated to Maloja's whole-second timestamps, so two *different* songs
  played less than a second apart could round to the same second — Maloja
  dedups by timestamp alone, so the second song was silently discarded and
  marked submitted. Colliding different-song timestamps are now nudged
  apart before submission; genuine same-song duplicates are left colliding
  so Maloja's own duplicate-timestamp response (already handled) catches
  them correctly.

## [0.1.10] - 2026-07-21

### Added
- Maloja export from the Play History panel: a second, independent
  preview/submit flow (server URL + API key) alongside the existing
  ListenBrainz backfill, with its own submitted-state sidecar file.
- Maloja scrobbles now include album artist and track length (from
  MediaMonkey's `AlbumArtist`/`SongLength`), matching what a live scrobbler
  sends and avoiding a Maloja-side bug triggered by scrobbles missing them.
- Per-service "Submitted" columns (LB / Maloja) and independent "Hide
  already submitted" filters in the Play History table, instead of one
  ListenBrainz-only column/filter.

### Fixed
- A Maloja "duplicate timestamp" response (HTTP 409) no longer aborts the
  rest of a submit batch — it's now recorded as already-submitted and
  skipped, with the rest of the batch continuing.
- `SubmittedStore.MarkSubmitted` (both ListenBrainz and Maloja) now retries
  its atomic rename briefly, to ride out a transient Windows
  antivirus/indexer lock on the just-written sidecar file.

## [0.1.9] - 2026-07-21

### Added
- "Hide already submitted" checkbox on the Play History table, to filter the
  view down to plays not yet confirmed submitted to ListenBrainz.

### Fixed
- MediaMonkey's `Played.PlayDate` is already a UTC instant (confirmed
  against MediaMonkey's own display, which adds `UTCOffset` to get local
  time); `FromMMPlayDate` and the play-history query were incorrectly
  subtracting `UTCOffset` again, shifting play timestamps and their sort
  order.

## [0.1.8] - 2026-07-20

### Added
- ListenBrainz submissions are now tracked locally, in a sidecar JSON file
  next to `MM5.DB` keyed by MediaMonkey's play-history row IDs — re-running
  a submit (or reopening the app later) only sends plays not already
  confirmed submitted, on top of ListenBrainz's own best-effort server-side
  dedup. The Play History preview now also shows how many plays were
  already submitted.
- Play History table now shows each row's ID, to make it easy to cross-check
  against the sidecar submitted-listens file.

### Fixed
- Reopening MM5.DB without re-entering the ListenBrainz token no longer
  hides submitted status for every row — the local submitted-state file is
  local data and doesn't need a token to read; the token is only required
  to actually submit new listens.

## [0.1.7] - 2026-07-20

### Added
- New "Play History" view, independent of the Navidrome migration wizard
  (only needs `MM5.DB`): a searchable, paginated table of every play in
  MediaMonkey's `Played` log, which MediaMonkey itself has no UI to browse.
- One-time backfill of that play history into
  [ListenBrainz](https://listenbrainz.org) (`internal/listenbrainz`), given a
  ListenBrainz user token — submitted directly via ListenBrainz's
  `submit-listens` API in backdated batches, with a small test-batch submit
  to verify against a real account before sending the full history.

## [0.1.6] - 2026-07-20

### Added
- Dry-run review (step 4) now lists unmatched and ambiguous tracks in a
  table above the pending changes, so it's clear what needs attention
  before committing.

## [0.1.5] - 2026-07-19

### Added
- Configurable MM half-star → Navidrome star rating mapping in the UI (round
  down, round up, or a custom per-step table), replacing the previous
  fixed round-down conversion. `Track.Rating` is now `Track.RatingStep`
  (the raw 0-10 MM half-star step); `mm.FromMMRating` is replaced by
  `mm.ToRatingStep` plus the new `Config.RatingMap`/`Config.MapRating`.

## [0.1.4] - 2026-07-19

### Added
- Commit progress logging: `Pipeline.Commit` now logs to the terminal every
  250 changes (plus start/finish and each error), so a large library commit
  no longer looks frozen with no feedback.
- On Linux, if the app is launched with no terminal attached (e.g.
  double-clicked from a file manager), it now relaunches itself inside a
  terminal emulator (`x-terminal-emulator`, `gnome-terminal`, `konsole`,
  `xfce4-terminal`, or `xterm`, tried in that order) so log output —
  including commit progress — is visible, matching how Windows already
  opens a console automatically.

## [0.1.3] - 2026-07-19

### Added
- Quit button in the UI header, backed by a new `POST /api/quit` endpoint,
  to close the app without switching back to the terminal.

### Fixed
- Dry-run preview showed a "Last Played" time shifted by the browser's
  timezone, disagreeing with what actually gets written. MediaMonkey's play
  dates have no reliable real-world offset, so they're treated as literal
  wall-clock digits everywhere; the preview now renders those digits as-is
  instead of running them through `new Date().toLocaleString()`, which
  wrongly treated them as a real UTC instant.

## [0.1.2] - 2026-07-19

### Fixed
- `play_date` writes landed as a bogus near-year-1 date in Navidrome. The
  writer formatted `LastPlayed` at a zero UTC offset, which
  `modernc.org/sqlite` canonicalizes to a trailing `Z` on insert into a
  `datetime`-affinity column — a shape none of Navidrome's own timestamp
  columns use (they're always an explicit local offset). Fixed by
  reinterpreting the same wall-clock reading in the local zone before
  writing, matching Navidrome's own convention.

### Added
- `scripts/build-linux.sh` and a `-Linux` switch on `scripts/build.ps1` to
  cross-compile a Linux binary (`bin/migrate-linux`); the app has no cgo
  dependencies, so no extra toolchain is needed.

## [0.1.1] - 2026-07-19

### Added
- Browse buttons next to the `MM5.DB` and `navidrome.db` path fields in the
  config step, backed by a new `GET /api/browse-file` endpoint that opens a
  native Windows file-picker dialog.
- Terminal logging for the config/users/scan/dry-run/commit steps and for
  every incoming HTTP request, so progress is visible while the app runs.
- Back button on the dry-run review step to return to the scope-pick step.
- `scripts/build.ps1` / `scripts/build.sh` to build the app into
  `bin/migrate.exe`.

## [0.1.0] - 2026-07-19

Initial working version: a local web app that migrates ratings, play counts,
and last-played dates from a MediaMonkey 5 library into Navidrome.

### Added
- Read-only MediaMonkey 5 (`MM5.DB`) reader, converting MM's 0-100 rating and
  `TDateTime` last-played into Navidrome's 0-5 rating and `time.Time`.
- Read-only Navidrome (`navidrome.db`) reader for media files and users.
- Relative-path track matching between the two libraries, with MusicBrainz id
  as a fallback signal, producing matched/ambiguous/unmatched buckets.
- Scoped scan -> dry-run -> commit pipeline; commits are idempotent (set, not
  add) so re-running a scope is safe.
- Subsonic API client for rating and star/unstar writes.
- Direct, idempotent `navidrome.db` writes for exact play counts and
  backdated last-played dates (data the Subsonic API can't express), guarded
  by a liveness check and an automatic timestamped backup before the first
  write.
- Configurable star-rating threshold for mapping MM ratings onto Navidrome's
  boolean "starred" flag.
- Local web UI (config form -> user pick -> scope pick -> scan/dry-run
  review -> commit), served by the app binary on `127.0.0.1`.
