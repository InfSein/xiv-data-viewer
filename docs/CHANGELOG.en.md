# Changelog

## [Unreleased]

## [2.5.1] - 2026-11-27

### Added
- **New-Patch Badges & Filter**: When a historical snapshot is downloaded, newly added entries are marked across all lists and global search with a one-click filter.

### Changed
- **Faster Startup**: Sped up initial loading and moved full icon indexing to the background with per-version caching.
- **Cross-Language Text Search**: Search across Chinese, Japanese, and English simultaneously with match highlights, and simplified the header and actions column.
- **Version Diff & Snapshot Manager**: Redesigned the Snapshot Manager as a fixed-height table with filter tabs; upgraded Version Diff achievements to a full table and texts to a side-by-side layout.
- **Visual & Layout Polish**: Unified the Icons page framing and list styles, trimmed redundant cards and badges, improved Changelog section hierarchy, and made version headers clickable to fold.

### Fixed
- **Inline Text Diff Highlighting**: Fixed multiple changes in the same text block highlighting unchanged text in between.

## [2.5.0] - 2026-11-26

### Added
- **Version Diff Export**: Export all filtered diff results as CSV, JSON, or Markdown, or batch-export PNG icons and audio files to a folder or Zip.
- **Full-Text Search**: Added a Text Search page for fuzzy and exact searches across game tables, UI strings, and dialogue with translations and detail jumps.
- **Text Diff Partition**: Added a dedicated Texts partition in Version Diff tracking modified and removed texts with inline highlights.

### Changed
- **Online Version Snapshots**: Replaced automatic local backups with on-demand GitHub patch snapshots and dual-server switching for faster startup and lower disk use.
- **Sidebar Grouping**: Renamed the Compare group to Tools to house both Version Diff and Text Search.

## [2.4.0] - 2026-11-26

### Added
- **Game Icon Browser & Export**: Added an Icons page with 1,000-ID grouping, HD lightbox previews, usage reverse-lookup, and batch PNG / Zip export.
- **Music Browser & Player**: Added a Music page for BGM and Orchestrion playback, usage lookup, audio export, Recent history, and Favorites.
- **Quest & Music Cross-Links**: Linked cutscenes and BGM bidirectionally between Quest and Music details, plus a "With Cutscenes" quest filter.
- **Expanded Version Diff**: Added a dedicated Icons diff partition and BGM change tracking in Version Diff.

### Changed
- **Filter Bar Divider**: Added a vertical divider separating standard Version Diff categories from the standalone Icons partition.

### Fixed
- **Dialog Progress Bars**: Fixed progress bars not filling properly in export and update dialogs.

## [2.3.0] - 2026-11-25

### Added
- **Recent & Favorites**: Added a History sidebar group tracking recently viewed entries and letting you favorite items, quests, leves, and achievements.

### Changed
- **Item Header & App Icon**: Replaced the item detail recent strip with persistent ID, category, and item level info, and updated the app icon throughout.

### Fixed
- **Quest Ordering & Search**: Sorted quests by in-game journal order to fix cross-expansion misplacement, and prioritized exact matches in search.

## [2.2.0] - 2026-11-25

### Added
- **Changelog & Auto-Updates**: Added a Changelog page and "Check for Updates..." to check and download new releases automatically.
- **Global & In-Page Search**: Added `Ctrl+K` / `⌘K` global search and `Ctrl+F` / `⌘F` in-page search with auto-scroll and highlighting on jump.
- **Multilingual Support**: Added Simplified Chinese, Japanese, and English UI with OS language detection and secondary game text display.

### Changed
- **Language Switching & Compact Layout**: Switching languages keeps your current view, with improved Japanese/English typography and `1024 × 640` layout.

## [2.1.0] - 2026-11-24

### Added
- **Quests & Leves Views**: Added dedicated Quests and Leves pages featuring rewards, prerequisites, NPC coordinates, spoiler-protected story dialogue, turn-in items, and reward pools.
- **Collectable Exchange**: Integrated Collectable Appraiser, Custom Deliveries, and Firmament Supply turn-in tiers and scrip rewards into item details.
- **Expanded Version Diff**: Added quest, leve, and collectable tracking to Version Diff with entity filters and one-click detail jumps.

### Changed
- **Cleaner Game Text & Setup**: Expanded inline game references and removed unreadable special glyphs; streamlined first-launch folder selection and loading progress feedback.
- **Typography & UI Polish**: Embedded crisp Latin/numeric fonts and refined item list metadata and achievement category filters.

### Fixed
- **Icon Edge Halos**: Fixed bright white halos around transparent game icons on light and dark backgrounds.
- **App Data Directory**: Standardized config and snapshot storage under the `xiv-data-viewer` folder in your system app data directory.

## [2.0.0] - 2026-11-24

### Added
- First desktop release supporting read-only browsing of CN and Global clients, including items, recipe trees, timed gathering countdowns, shops, retainer ventures, achievements, version diffs, and full CSV export.
