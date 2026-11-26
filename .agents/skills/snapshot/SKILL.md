---
name: snapshot
description: >-
  当用户发送“建立快照：x.y”（或“建立快照: x.y”，例如“建立快照：7.58”或“建立快照：7.58 国服”）或要求从本地 FF14 客户端打包 `.xivsnap` 版本快照、上传到 GitHub 快照仓库 `InfSein/xiv-data-snapshots` 并更新 `catalog.json` 时使用此技能。
---

# 版本数据快照打包与发布自动化流程 (`snapshot`)

收到 `建立快照：<patch>`（可选附带区服 `国服`/`国际服`）指令后，严格按照以下步骤顺序执行（Windows PowerShell 统一先执行 `chcp 65001` 与 UTF-8 编码输出，对游戏客户端目录**严格只读**，且**严禁提交或推送主代码仓库**）：

## 1. 解析与校验参数

1. 从用户消息提取目标玩家版本号 `<patch>`（如 `7.58`、`7.55`、`8.0`；若带前缀 `v` 则去除）。
2. 校验版本号非空且仅含安全字符（匹配 `^[0-9]+\.[0-9]+([a-zA-Z0-9._-]*)?$`）。
3. 判断目标区服 `-region`：
   - 若用户指定「国服」/ `cn`：`-region cn`；
   - 若用户指定「国际服」/ `global`：`-region global`；
   - 否则默认 `-region all`（自动检测根目录 `.env` 中已配置的 `FFXIV_CN_PATH` 与 `FFXIV_GLOBAL_PATH`）。
4. 检查 `gh auth status` 确认已登录具备 `InfSein/xiv-data-snapshots` 写入权限的账号；若未登录则立即中止并提示用户登录。

## 2. 确认 EXDSchema 与本地客户端状态

1. 检查根目录 `.env` 中目标客户端的 `game/ffxivgame.ver`，读取客户端内部版本号（如 `2026.11.03.0000.0000`）。
   - 若同时配置了国服与国际服且 `-region all`，**先比对两者的 `ffxivgame.ver`**：如果国服与国际服当前实际处于不同版本（例如国际服已更新而国服尚未更新），先向用户确认本次是只打包其中一个区服还是分别指定版本号，避免把旧版本误标成新版本号！
2. 确认 `schemas/` 子模块已检出且非空（如尚未初始化则执行 `git submodule update --init --recursive`，注意不要擅自在主仓库做 `git commit`）。

## 3. 拉取快照仓库并准备上一版基准快照（提取变动旧图标）

1. 在系统临时目录创建独立工作目录（例如 `$env:TEMP\xiv-snap-pub-<随机ID>`），克隆 `https://github.com/InfSein/xiv-data-snapshots.git` 的 `main` 分支，读取现有 `catalog.json`（统一版本号 `versions` 结构）。
2. 在本地维护者缓存目录（`$env:LOCALAPPDATA\xiv-data-viewer-packer\prev-snapshots`）中准备各目标区服的「上一版本快照」（即 `catalog.json` 的 `versions` 中 `patch != <patch>` 且包含对应 `cn`/`global` 资产的最新条目）：
   - 若上一版本快照已存在于 `prev-snapshots`，直接复用；
   - 否则按 `asset.downloadUrl`（或 `gh release download`）下载到本地缓存，供本次打包器传入 `-prev-cn` / `-prev-global`，从而仅抽取变动/移除的旧图标缩略图封入新快照。
3. 维护者本地去重图标缓存目录默认位于 `$env:LOCALAPPDATA\xiv-data-viewer-packer\icon-cache`。若是首次运行且该缓存目录为空、同时检测到 `%APPDATA%\xiv-data-viewer\backups\icon_blobs.dat` 存在，可传入 `-seed-blobs "$env:APPDATA\xiv-data-viewer\backups"` 一次性迁移历史图标缩略图。

## 4. 只读打包 `.xivsnap` 快照文件

在主仓库根目录执行打包工具（输出至临时目录 `<tmpOut>`，不要污染主仓库工作区）：

```powershell
chcp 65001 > $null
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
go run ./cmd/pack-snapshot `
  -region <cn|global|all> `
  -patch <patch> `
  -out <tmpOut> `
  -manifest-out <tmpOut>\version-entry.json `
  [-prev-cn <prevCnSnapPath>] `
  [-prev-global <prevGlobalSnapPath>] `
  [-seed-blobs <legacyBackupsDir>]
```

- 打包过程对游戏客户端目录严格只读：
  - 国服资产（`xivsnap-<patch>-cn.xivsnap`）：全量 EXD（`chs` + 语言无关）+ 配套 `EXDSchema` + BGM 音频指纹 + 全量图标指纹 + 变动旧图标缩略图；
  - 国际服资产（`xivsnap-<patch>-global.xivsnap`）：全量 EXD（`ja` + `en` 双语合一 + 语言无关，剔除 `de`/`fr`）+ 配套 `EXDSchema` + BGM 音频指纹 + 全量图标指纹 + 变动旧图标缩略图。
- 校验 `<tmpOut>\version-entry.json` 与对应 `.xivsnap`、`SHA256SUMS.txt` 均已生成。

## 5. 发布统一版本 GitHub Release (`v<patch>`) 并更新 `catalog.json`

1. 将生成的 `.xivsnap` 文件复制一份留存到 `$env:LOCALAPPDATA\xiv-data-viewer-packer\prev-snapshots\`，便于下次发新补丁时秒级复用。
2. 所有同版本区服资产统一挂载在同一个 GitHub Release 标签 **`v<patch>`**（例如 `v7.58`）下：
   - 先检测 `gh release view v<patch> -R InfSein/xiv-data-snapshots`：
     - 已存在（例如先前发过国际服，本次补发国服）：使用 `gh release upload v<patch> <snapFiles...> <sumPath> --clobber -R InfSein/xiv-data-snapshots` 追加/覆盖上传，并更新 Release 说明；
     - 不存在：使用 `gh release create v<patch> <snapFiles...> <sumPath> -R InfSein/xiv-data-snapshots --title "Patch <patch>" --notes "<统计摘要与 SHA256>"`。
3. 使用 Python（优先项目 `.venv/Scripts/python.exe`）将 `<tmpOut>\version-entry.json` 合并写入临时克隆仓库的 `catalog.json`（UTF-8 无 BOM）：
   - 在 `versions` 中按 `patch` 查重并合并（若同 `patch` 已存在，保留本次未重新打包的另一区服 `cn`/`global` 字段，仅替换本次打包的区服字段！）；
   - 将 `schemaVersion` 保持为 `1`，`updatedAt` 更新为当前 UTC RFC3339 时间；
   - `versions` 按主版本号倒序排列（最新版本号在最前）。
4. 在临时克隆的 `InfSein/xiv-data-snapshots` 仓库中提交并推送 `catalog.json`：
   - `git add catalog.json`
   - `git commit -m "Publish snapshot <patch> (<regions>)"`
   - `git push origin main`
5. 清理本次创建的临时发布目录（保留 `prev-snapshots` 与 `icon-cache` 维护者本地缓存），并确认主代码仓库 `git status` 未残留任何临时产物。

## 6. 结果汇报

用简洁易懂的中文向用户汇报：
- 已发布的统一版本号（如 `7.58`）、对应客户端内部版本号与包含区服；
- 各区服资产体积、包含的数据表/音乐/图标/变动旧图数量；
- GitHub Release 地址与 `catalog.json` 更新状态。
