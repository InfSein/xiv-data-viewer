# Changelog

## [Unreleased]

## [2.5.0] - 2026-11-26

### Added
- **Version Diff Export**: Added an "Export" feature in Version Diff to export all entries matching the current filters (across all pages). In the "Icons" partition, batch-export icons as PNGs preferring HD assets when available (named `<IconID>.png`, e.g., `026290.png`, with an option to include previous images for modified icons); in the "Items" category, batch-export filtered item icons named `<ItemID>_<ItemName>.png` (also supported for quests, leves, achievements, and All); in "BGM", batch-export `.ogg` / `.scd` audio files; and across every category, export full filtered results as CSV spreadsheets, JSON files, or formatted Markdown reports, with both local folder and `.zip` archive options for batch resource exports.
- **Full-Text Search & Side-by-Side Translations**: Added a new "Text Search" page under the "Tools" sidebar group to search across all game data sheets, UI strings, and story dialogue. Supports ordered fuzzy matching by default (e.g., `AD` matches `ABCD`) and contiguous exact matching when wrapped in double quotes (e.g., `"AB"`), with relevance ranking, side-by-side multilingual translations in the table whenever available, pagination, match highlighting, scope and sheet filtering, one-click text copy, and direct jumps to linked items, quests, leves, achievements, and music.
- **Text Partition in Version Diff**: Added a dedicated "Texts" comparison partition in Version Diff (kept separate from the "All" view) focused exclusively on modified and removed game texts (excluding newly added entries), featuring inline character-level diff highlights, search and filters, pagination, and direct links to related entries.

### Changed
- **Online Version Snapshots from GitHub**: Version Diff no longer generates automatic local backups on startup; instead, it downloads historical snapshots directly from GitHub by patch version (such as `7.58` or `7.55`), automatically fetching data matching your configured CN and/or Global client(s), recognizing both clients in dual-server setups, and switching seamlessly between CN-to-CN and Global-to-Global comparisons. Added a Snapshot Manager dialog for browsing, downloading with progress, and deleting local snapshots, making app startup faster and reducing disk usage.
- **Sidebar Group Renamed**: Renamed the "Compare" sidebar group to "Tools" to house both "Version Diff" and "Text Search".

## [2.4.0] - 2026-11-26

### Added
- **Icons Partition in Version Diff**: Added a dedicated "Icons" comparison partition in Version Diff (kept separate from the "All" view), rendering added and removed icons as tiled grids and displaying old and new icons side-by-side for modified entries.
- **Game Icon Browser & Export**: Added an "Icons" page under the "Resources" sidebar group organizing icons in 1,000-ID groups while skipping empty groups and missing IDs, supporting searches by ID, group, or referencing items/quests/achievements, prioritizing HD previews in the details panel with single-click or double-click large image preview (with quick resolution and directory variant switching inside the lightbox), jumping to related entries, and exporting individual PNGs or bulk local folders and `.zip` archives matching SaintCoinach's directory layout (with all available variants pre-selected).
- **Quest & Music Cross-Links**: Added linked cutscenes and BGM tracks inside Quest details alongside a referencing Quests section in Music details (covering cutscenes and quest scripts, including Orchestrion tracks sharing the same audio file), with one-click jumping both ways plus a "With Cutscenes" quest filter and Version Diff tracking.
- **Music Browser & Built-In Player**: Added a new "Music" page under the "Resources" sidebar group combining game BGM and Orchestrion tracks with zone, duty, mount, event, and ceremony usages (auto-collapsed when exceeding 50 items, click to expand), built-in playback with seek, volume, and loop controls, `.ogg` / `.scd` export, global quick-search support, and full Recent history and Favorites tracking.
- **BGM Tracking in Version Diff**: Added BGM stats and filter chips to Version Diff to highlight newly added BGMs and detect content changes in existing BGM tracks, with click-to-inspect navigation.

### Changed
- **Version Diff Filter Divider**: Added a vertical divider before the "Icons" filter button in Version Diff to visually separate it from the standard "All" summary and categories.

### Fixed
- **Export & Update Progress Bars**: Fixed progress bars not filling properly in the export and update dialogs.

## [2.3.0] - 2026-11-25

### Added
- **Recent History & Favorites**: Added a "History" section in the sidebar with "Recent" and "Favorites" pages. "Recent" tracks recently opened items, quests, and leves (up to 500 entries), while "Favorites" lets you star items, quests, leves, and achievements from their pages and browse them by category tabs.

### Changed
- **Item Detail Header**: Removed the old inline recent items bar from the item detail header (now consolidated into the "Recent" page) and permanently display the current item's ID, category, and item level instead.
- **New App Icon**: Updated the application with a brand-new icon across the app window, taskbar, setup screen, update dialog, and top-left brand bar.

### Fixed
- **Quest List Ordering & Search**: Sorted quests by their in-game journal section, category, chapter, and storyline order so quests across expansion transitions or sharing the same level (such as *Shadowbringers*) no longer get pushed out of order or appear missing; exact title matches now appear first when searching quests.

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
