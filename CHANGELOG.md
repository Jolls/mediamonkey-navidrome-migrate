# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
