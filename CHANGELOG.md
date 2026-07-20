# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
