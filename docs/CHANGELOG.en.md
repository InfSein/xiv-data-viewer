# Changelog

## [Unreleased]

## [2.2.0] - 2026-11-25

### Added
- **Changelog Page**: Added a "Changelog" button above "Settings" in the sidebar to browse release notes organized by version and category, with search and filters.
- **Automatic App Updates**: Added "Check for Updates..." to the top-left menu, supporting automatic update checks, downloads, and restart prompts.
- **Global Quick Search**: Press `Ctrl+K` / `⌘K` to search across items, quests, leves, and achievements, then jump straight to details or into a specific page.
- **In-Page Search & Auto-Scroll**: Added `Ctrl+F` / `⌘F` search bars across all data and comparison views; jumping from search or detail links automatically switches pages and highlights the target entry.
- **Multilingual UI & Game Data**: Added Simplified Chinese, Japanese, and English interface support with automatic OS language detection on first launch, an anytime top-right switcher, and matching primary/secondary game text when a Global client is configured.

### Changed
- **Faster Language Switching & Compact Layout**: Switching languages preserves your current view without returning to the loading screen; refined Japanese/English typography and layout at `1024 × 640`.

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
