# ARCHITECTURE.md — 项目架构说明

## 1. 模块与文件职责

| 文件 / 目录路径 | 职责说明 |
| --- | --- |
| `main.go` | MyGo 桌面客户端主入口，注册 `zh-Hans`/`ja` 原生控件语言包，使用 `//go:embed` 内嵌三语更新日志 (`CHANGELOG.md`、`docs/CHANGELOG.en.md`、`docs/CHANGELOG.ja.md`)，设置窗口/Dock 图标并按配置或系统语言与主题初始化原生 GPU 渲染窗口。 |
| `schemas_embed.go` | 使用 Go `//go:embed` 将 Git Submodule `schemas/*.yml`（`xivdev/EXDSchema`）直接内嵌到单文件客户端二进制中。 |
| `mygo.json` | MyGo 应用构建清单，定义软件名称、标识符、版本号、应用图标 (`resources/icon.png`) 及 Windows 安装包配置。 |
| `package.json` | 零依赖 npm 快捷脚本入口，提供跨平台一键本地调试 (`npm run dev`)、Windows 构建 (`npm run build`)、静态检查与自动化测试 (`npm test`)。 |
| `resources/icon.png` / `resources/embed.go` | 1024×1024 透明背景应用图标与 `//go:embed` 资源包；构建时由 MyGo 自动生成 Windows `.exe` 内嵌图标及 NSIS 安装包图标，同时供运行时窗口、任务栏与界面品牌徽标复用。 |
| `internal/config/config.go` | 管理应用持久化配置（`config.json`），支持界面语言（`language`）、主题与国服/国际服路径只读校验及版本号读取。 |
| `internal/config/records.go` | 管理持久化最近浏览记录（物品/任务/理符/音乐，上限 500 条）与收藏名单（物品/任务/理符/成就/音乐，区分 `bgm` 与 `orchestrion`），序列化存储于应用数据目录 `history.json`。 |
| `internal/i18n/` | 内嵌中文/日语/英语 JSON 词条表与 `{key}` 占位符格式化，提供跨平台非阻塞系统语言自动探测、MyGo BCP 47 映射及副语言展示规则。 |
| `internal/changelog/` | Keep a Changelog 格式 Markdown 结构化解析器，提取版本号、发布日期、中/英/日语义分类、加粗标题/正文、二级子条目及行内代码与链接。 |
| `internal/sqpack/hash.go` | 实现 FF14 SqPack 使用的 JamCRC32 哈希及 `.index` / `.index2` 路径哈希计算。 |
| `internal/sqpack/sqpack.go` | 纯 Go 并发安全只读 SqPack 归档读取器，支持 `.index` / `.index2` 查表、`.dat0..N` 标准/贴图文件流式解压及按压缩分块跨度快速计算资源 MD5 摘要。 |
| `internal/sqpack/excel.go` | 纯 Go EXH / EXD / EXL 解析器，支持普通表（Variant=1）与子行表（Variant=2）按分页并发读取及全数据类型解包。 |
| `internal/sqpack/schema.go` | 基于 `schemas/*.yml`（`xivdev/EXDSchema`）将 EXH 原始列按 byte offset 升序动态对齐字段名，支持按需列裁剪读取 `SchemaSheet` / `SchemaSubrowSheet`。 |
| `internal/sqpack/sestring.go` | 纯 Go FF14 SeString 字节码解析与纯文本格式化器（支持换行、软连字符、表引用展开、按目标语言格式化玩家称谓与占位符及私用区字形清理）。 |
| `internal/sqpack/texture.go` / `bc7.go` | 纯 Go `.tex` 贴图解码器（支持 BGRA8、BGRA4444、BGRA5551、L8/A8、DXT1/3/5、BC4/5 及 BC7 模式 0..7）与有界图像缓存。 |
| `internal/sqpack/scd.go` | 纯 Go 只读 `.scd` 音频容器解析器，支持 Ogg Vorbis 加密块（v2/v3 XOR）还原、`MARK` / Vorbis 标签循环点与采样率提取及未知编码兼容识别。 |
| `internal/sqpack/cutscene.go` | 纯 Go 只读 `.cutb` 过场动画时间线音乐轨道 (`C070`) 提取与 `.luab` (Lua 5.1) 任务脚本过场/BGM 调用解析器。 |
| `internal/sqpack/icon.go` | 纯 Go `060000` 图标索引与 `.dat` 资源存在性扫描、变体路径映射（含 `_hr1`/`hq`/`chs`/`en`/`ja`/`de`/`fr`）、区域采样缩略图缩放及 PNG 无损解码。 |
| `internal/sqpack/icon_digest.go` | 按物理磁盘偏移顺序扫描 `060000` 图标跨版本指纹并生成紧凑缩略图载荷（`'T'+zlib .tex` / `'P'+PNG`）与解码还原。 |
| `internal/gamedata/types.go` | 定义道具（含 `NameZh`/`NameJa`/`NameEn`）、装备词条、食品药效、制作配方树、采集/垂钓/刺鱼、NPC 商店、雇员探险、任务、理符、收藏品交易、成就称号与音乐 BGM 全量业务模型。 |
| `internal/gamedata/eorzea_time.go` | 艾欧泽亚时间 (ET) 实时换算器与按界面语言格式化的限时采集点开放窗口及现实剩余倒计时评估工具。 |
| `internal/gamedata/shop_loader.go` | 解析 `GilShop`、`SpecialShop`、`GCScripShopItem`、`FccShop`、`CollectablesShop` 关联、多跳 `ENpcBase` 引用链与 `Level` 3D 坐标。 |
| `internal/gamedata/retainer_loader.go` | 解析雇员探险定向筹集委托（狩猎/采矿/园艺/捕鱼）及 5 档属性门槛与产出数量索引。 |
| `internal/gamedata/quest_loader.go` | 解析 `Quest`、任务类型图标映射 (`061431` 系列)、详情横幅/专属徽章、前置条件、NPC 坐标、任务解锁及 `QuestClassJobReward`/`BeastRankBonus` 奖励。 |
| `internal/gamedata/cutscene_loader.go` | 关联 `Quest`、`CompleteJournal`、`Cutscene` 与 `EventSystemDefine`，提供任务过场动画与音乐双向反查（含按需解包 `.cutb`/`.luab` 与乐谱标题匹配）。 |
| `internal/gamedata/quest_text.go` | 按需从 SqPack 只读解析 `quest/xxx/Id` 文本表，按当前主语言提取任务概要、任务目的与剧情对话流并自动匹配说话人名称（带内存有界缓存）。 |
| `internal/gamedata/leve_loader.go` | 解析 `Leve` 及其四类子表（`CraftLeve`、`GatheringLeve`、`BattleLeve`、`CompanyLeve`）、交纳物品、委托目标与 `LeveRewardItem` 概率奖励池。 |
| `internal/gamedata/collectables_loader.go` | 解析 `CollectablesShop` 收藏品交易、`SatisfactionNpc`/`SatisfactionSupply` 老主顾及 `HWDCrafterSupply` 苍天街供货三档价值与工票双向索引。 |
| `internal/gamedata/music_loader.go` | 解析 `BGM`、`BGMSituation`、`BGMSwitch`、`BGMSystemDefine`、`WeddingBGM` 与 `Orchestrion*` 乐谱，联动区域、副本、坐骑、FATE/GATE、理符与乐谱物品，并提供并发 BGM 摘要计算。 |
| `internal/gamedata/icon_loader.go` | 构建并合并国服/国际服有效图标总索引与每 1000 编号分组摘要（自动隐藏空分组并跳过空编号），关联物品/任务/成就/理符反向引用。 |
| `internal/gamedata/loader.go` | 并发加载全量核心游戏表与 Schema 对齐扩展表并构建内存正反向索引，支持按目标语言 (`zh`/`ja`/`en`) 动态切换主语言数据源，兼容**仅国服**、**仅国际服**与**双服联查**三种运行模式。 |
| `internal/gamedata/service.go` | 核心查询业务层：跨模块名称包含检索与多模块均衡排序 (`SearchGlobal`)、分页定位、中/日/英三语联合搜索与筛选、装备/HQ/自动攻击/药效计算、递归配方树、3D→2D 地图坐标换算、任务/理符、成就与音乐试听查询。 |
| `internal/backup/backup.go` / `icon_diff.go` | 启动版本检查、应用数据目录快照持久化（含按内容寻址去重 `icon_blobs.dat`/`icon_blobs.idx` 图标缩略图包，支持按语言隔离目录）与按当前语言输出字段级 Diff 的对比服务（图标差异划归独立分区，不在「全部」中混入，并平滑兼容无图标/BGM的历史旧快照）。 |
| `internal/exporter/schema.go` | 复用 `sqpack` 的 EXDSchema YAML 对齐能力，并从 `exd/root.exl` 读取 7,900+ 全量表清单。 |
| `internal/exporter/exporter.go` | 生成带 UTF-8 BOM 的 RFC 4180 / SaintCoinach 三行表头 CSV，支持本地目录清理重解包与 Zip 打包导出（含实时进度与中断）。 |
| `internal/exporter/icon_exporter.go` | 按 SaintCoinach 路径分布（`ui/icon/000000/[chs/hq/]000000[_hr1].png`）提供多线程无损 PNG 解码到本地目录与流式 Zip 压缩包导出器。 |
| `internal/updater/updater.go` | 基于 GitHub Release（含限流重定向回退与 `SHA256SUMS.txt` 校验）的应用内置自更新客户端，支持静默后台下载、可回滚原子文件替换与旧版备份清理。 |
| `internal/uiapp/fonts.go` | 使用 `//go:embed` 内嵌 Google Sans (`Regular`/`Medium`/`Bold`) 字体文件并在应用初始化时通过 `ui.RegisterFont` 注册为默认西文与数字字体。 |
| `internal/uiapp/tokens.go` | 参考 Godwit 客户端构建 Tailwind v4 OKLCH 中性浅色/深色主题体系、按界面语言切换的字体回退栈、浮起面板 (`kitPanel`)、卡片、侧栏项、分段滑块、拖拽分隔条及统一矢量图标库。 |
| `internal/uiapp/icons.go` | 游戏图标与地图贴图的后台异步解码及 MyGo GPU `*ui.Bitmap` 有界缓存。 |
| `internal/uiapp/app.go` | 客户端顶层状态机，协调首次启动系统语言探测、语言切换与数据源热重载、`modeSetup` 引导、异步加载 (`modeLoading`)、主题持久化、GitHub Release 启动静默/主动更新流程与主工作台 (`modeReady`)。 |
| `internal/uiapp/view_setup.go` | 单画布聚焦式首次引导选目录页（含欢迎页三语实时切换、区服模式切换、拖拽路径识别与纠错）、三阶段实时加载进度页及居中「设置」中心（含语言切换、主题、路径配置与导出入口）。 |
| `internal/uiapp/view_workbench.go` | 融合可拖拽顶栏（品牌菜单含导出/快照/主题/设置/检查更新 + 折叠侧栏 + 全局跨模块名称检索浮层与页面跳转 + 右上角全局语言切换）、左侧导航（【数据】物品/任务/理符/成就、【资源】音乐/图标、【历史】最近/收藏、【对比】版本对比、更新日志、【设置】及左下角更新就绪重启提醒）及带页内搜索栏的物品双浮起面板。 |
| `internal/uiapp/view_changelog.go` | 结构化多语言更新日志页面，按版本卡、发布状态徽章、变更分类标签、标题/正文分层与行内代码高亮展示 `CHANGELOG.md` 及 `docs/CHANGELOG.{en,ja}.md`，支持版本筛选、分类过滤、页内搜索与版本折叠。 |
| `internal/uiapp/view_detail.go` | 物品阅读器风格悬浮详情面板（支持收藏切换、按当前语言规则展示副语言名称、收藏品交易与老主顾交纳明细、任务/理符关联、成就奖励、商店、配方树、采集倒计时与雇员探险）。 |
| `internal/uiapp/view_quests.go` | 任务检索与阅读工作台（含内置页内搜索栏、收藏切换、按规则展示副语言名称、横幅插图、前置跳转、关联过场动画与 BGM 直跳及默认折叠防剧透的概要与对话文本）。 |
| `internal/uiapp/view_leves.go` | 理符检索与阅读工作台（含内置页内搜索栏、收藏切换、按规则展示副语言名称、四分栏筛选、发行点与目标地图坐标及奖励道具池）。 |
| `internal/uiapp/view_achievements.go` | 悬浮面板成就与称号检索视图（含内置页内搜索栏、表格行内星标收藏切换、按规则展示成就与称号副语言名称、分类联动筛选与奖励道具跳转）。 |
| `internal/uiapp/audio_player.go` | 基于 `mygo/audio` 与 `audio/vorbis` 的纯 Go Ogg Vorbis 音频流播放器，支持播放/暂停/停止、拖拽进度、循环区间及音量与静音控制。 |
| `internal/uiapp/view_music.go` | 「资源 / 音乐」双栏工作台（汇集游戏 BGM 与管弦乐琴曲目、超 50 条自动折叠的使用场景与关联任务直跳、内置试听播放器、循环控制、收藏切换及 `.ogg`/`.scd` 导出，支持复用展示详情）。 |
| `internal/uiapp/view_icons.go` | 「资源 / 图标」三栏工作台与导出弹窗（左侧每 1000 编号分组且隐藏空组、中央虚拟化图标网格自动跳过空编号、右侧高清/多语言变体属性预览与反向引用跳转，并按 SaintCoinach 规范导出单个 PNG、本地目录或 Zip 压缩包）。 |
| `internal/uiapp/view_recent.go` | 「最近」浏览记录工作台：汇总最近打开过的物品、任务、理符与音乐（上限 500 条），支持类型筛选、页内搜索、分页与右侧复用对应详情阅读或试听面板。 |
| `internal/uiapp/view_favorites.go` | 「收藏」工作台：顶部提供物品、任务、理符、成就与音乐五个切换标签页，并分别展示与对应原页面一致的双栏详情/试听或全宽表格界面。 |
| `internal/uiapp/view_diff.go` / `view_diff_icons.go` | 悬浮面板版本快照对比视图（含内置页内搜索栏、常规实体明细表、独立「图标」平铺与新旧对照对比分区及详情一键直跳）。 |
| `internal/uiapp/view_modals.go` | Godwit 对话框规范地图贴图预览弹窗（带坐标大头针）、全量 CSV 导出弹窗（原生目录/保存对话框 + 细进度条 + 取消控制）及 GitHub Release 检查更新弹窗。 |
| `.github/workflows/ci.yml` | PR 与非主分支推送自动检查：`mygo.json` 校验、`gofmt`、`go vet`、`mygo vet`、单元测试（含无头 UI 测试）及 Windows `amd64/arm64` 构建验证。 |
| `.github/workflows/release.yml` | `master` 分支更新或推送 `v*` 标签时自动验证并构建 Windows `amd64/arm64` NSIS 安装包与便携 ZIP，生成 `SHA256SUMS.txt` 并创建/更新 GitHub Release。 |
| `.github/workflows/security.yml` | 每周定时运行 `govulncheck` 依赖漏洞扫描。 |

---

## 2. 模块间调用关系

```text
[本地 FF14 客户端 SqPack (国服 / 国际服，至少配置其一，严格只读)]
        │
        ▼
[internal/i18n]   ──────────(三语内嵌词条、系统语言探测、副语言展示规则)
        │
        ├──► [internal/config]   (路径校验、config.json 持久化 language/theme、history.json 持久化最近与收藏、ffxivgame.ver)
        ├──► [internal/sqpack]   (Archive/GameReader, EXH/EXD/EXL, Schema 对齐, SeString, .tex 解码)
        ├──► [internal/gamedata] (按语言切换主数据、保留三语检索、eorzea_time、各类解析器)
        ├──► [internal/backup]   (按语言隔离 backups/ 快照备份与本地化版本 Diff)
        └──► [internal/exporter] (内嵌 schemas/*.yml 对齐、本地解包与 Zip 导出)
                    │
                    ▼
[internal/uiapp] (MyGo 原生 GPU UI, 1366×768 Framed-Sheet 工作台)
  ├──► view_setup.go        (首次启动路径与语言引导 + 设置中心语言/主题/路径调节)
  ├──► view_workbench.go    (L型外框 + 顶栏检索 + 筛选 + 道具列表)
  ├──► view_detail.go       (道具完整详情 + 副语言展示 + 收藏品 + 配方树 + 采集倒计时)
  ├──► view_quests.go       (任务检索与详情 + 副语言展示 + 前置任务与奖励跳转)
  ├──► view_leves.go        (理符分类检索 + 副语言展示 + 交纳委托与奖励池)
  ├──► view_achievements.go (成就与称号一览 + 副语言展示 + 星标收藏)
  ├──► view_music.go        (BGM 与管弦乐琴检索、场景出处、audio_player 试听、收藏与导出)
  ├──► view_icons.go        (每 1000 编号图标分组、虚拟化网格、变体预览、引用反查与 SaintCoinach 导出)
  ├──► view_recent.go       (最近打开的物品/任务/理符/音乐工作台，上限 500 条)
  ├──► view_favorites.go    (分标签页展示收藏的物品/任务/理符/成就/音乐)
  ├──► view_diff.go         (版本快照对比；图标差异独立分区与平铺/新旧对照由 view_diff_icons.go 承接)
  ├──► view_changelog.go    (三语结构化更新日志展示)
  └──► view_modals.go       (原生贴图地图预览 + CSV 导出弹窗)
                    │
                    ▼
[main.go + schemas_embed.go + mygo.json] ──► 单文件 Windows 客户端 & NSIS 安装包
```

---

## 3. 关键设计决定与原因

1. **采用 `egoist/mygo` 纯 Go 原生 UI + 纯 Go 后端引擎**：
   - 完全摆脱 Node.js / Bun 运行时与 WebView2 依赖（Windows 构建传入 `GOFLAGS=-tags=mygo_nowebview`），最终单文件客户端仅约 **12 MB**、NSIS 安装包仅约 **4 MB**，启动瞬时响应且低内存占用，同时支持在 Ubuntu CI 节点直接无 cgo 交叉编译 `windows/amd64` 与 `windows/arm64`。
2. **初次启动引导 + 灵活单服/双服兼容**：
   - 将原先强依赖 `.env` 且必须同时具备双服的设计升级为图形化配置管理：首次启动自动进入路径引导页，只要求用户提供国服或国际服至少其一即可完整使用，并支持随时在「设置」中调整路径并热重载数据；单国服/单国际服模式下自动降级多语言展示与 CSV 可选导出语言。
3. **严格只读访问与内嵌资源**：
   - 所有游戏文件仅通过 `os.Open` 只读打开，所有用户配置、版本快照均写入系统标准应用数据目录（`mygo.PathUserData`）；全部 1,198 份 `xivdev/EXDSchema` YAML 定义与默认西文字体 Google Sans（`Regular` / `Medium` / `Bold`）均通过 `//go:embed` 内嵌进可执行文件，实现零外部文件依赖与跨机器一致的英文及数字排版效果。
4. **原生 GPU 异步贴图解码与无头 UI 自动化测试**：
   - 图标和地图 `.tex` 在后台受限并发池解码后通过 `Window.Update` 安全投递到主线程上传 GPU，确保滚动列表与切换详情零卡顿；并通过 `ui.NewTester` 在 CI 中以 `1366 × 768` 与 `1024 × 640` 无头方式自动验证引导流程、语言切换、标签切换与弹窗交互。
5. **三语国际化与主/副语言数据联动设计**：
   - `internal/i18n` 采用 `//go:embed locales/*.json` 配合单元测试校验全量键对齐与 `{key}` 占位符一致性，并在首次未保存语言偏好时通过各平台非阻塞 API（Windows `GetUserPreferredUILanguages` 等、macOS `AppleLocale`、`LC_ALL`/`LANG`）自动推断系统语言，欢迎页与「设置」均可随时切换并持久化。
   - 切换为日语或英语且已挂载国际服客户端时，`gamedata` 会将主数据列切换为对应的 `ja` / `en` 读取并独立保存于 `backups/ja` 与 `backups/en`，同时始终保留 `NameZh` / `NameJa` / `NameEn` 三语联合检索；物品、任务、理符与成就/称号详情按 `SecondaryDataLangs` 统一展示副语言（中文→日语+英语，日语→仅英语，英语→仅日语）。
6. **纯 Go 音乐解析、按需试听与无明文哈希的版本音频对比**：
   - `internal/sqpack/scd.go` 直接从只读 `.scd` 容器拆解 Ogg Vorbis 加密块（v2/v3 XOR）并交由 `mygo/audio/vorbis` 零 cgo 播放，同时针对空占位或非 Ogg（如 CRI HCA）轨道平稳降级并提供 `.scd` 原始容器导出。
   - 为避免启动时全量解压近 4 GB BGM 带来的数十秒延迟，`ArchiveReader.ReadFileDigest` 直接按 SqPack `.index`/`.dat` 的连续压缩分块区间流式计算 MD5（同一版本冷启动后的二次启动直接复用已有版本快照中的 BGM 摘要），并在「版本对比」界面严格仅向玩家呈现“内容有变化”而隐藏新旧 MD5 明文，同时对不含 `bgms` 字段的历史旧版快照平滑跳过，不误报全量新增。
7. **纯 Go 图标扫描索引与 SaintCoinach 兼容导出**：
   - `internal/sqpack/icon.go` 基于 `060000.win32.index`/`.index2` 与 `.dat` 头的按偏移序读校验（识别并剔除 `fileType == fileTypeEmpty` 或体积为 0 的空占位编号，约 1.1 秒完成 ~19.5 万有效文件扫描），仅显示包含至少 1 个有效图标的非空 1,000 号段分组，在网格中跳过无资源编号并支持按 `maxThumbSide = 128` 区域采样缩略图缓存。
   - `internal/exporter/icon_exporter.go` 支持多线程解码并以 SaintCoinach `ui/icon/{分组}000/[chs/en/ja/de/fr/hq/]{6位编号}[_hr1].png` 目录结构导出无损 RGBA PNG 到本地目录或流式 Zip 压缩包，弹窗默认全选当前可用变体并提供细粒度过滤。
   - `internal/backup/icon_diff.go` 在版本切换创建快照时对全量图标仅做按偏移顺序的单次 4KB 头与块指纹校验（重复版本秒级复用），并通过内容寻址的 `icon_blobs.dat` / `icon_blobs.idx` 去重存储旧版代表图以支撑「版本对比 → 图标」独立分区中的新增/移除平铺与新旧图标并排对比，且平滑兼容缺少图标记录的历史快照。
