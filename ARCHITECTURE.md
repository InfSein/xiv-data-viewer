# ARCHITECTURE.md — 项目架构说明

## 1. 模块与文件职责

| 文件 / 目录路径 | 职责说明 |
| --- | --- |
| `main.go` | MyGo 桌面客户端主入口，按配置初始化系统/浅色/深色主题、启用 Windows Mica / macOS Sidebar 融合标题栏并创建默认 `1366 × 768` 原生 GPU 渲染窗口。 |
| `schemas_embed.go` | 使用 Go `//go:embed` 将 Git Submodule `schemas/*.yml`（`xivdev/EXDSchema`）直接内嵌到单文件客户端二进制中。 |
| `mygo.json` | MyGo 应用构建清单，定义软件名称、标识符、版本号、应用图标 (`resources/icon.png`) 及 Windows 安装包配置。 |
| `package.json` | 零依赖 npm 快捷脚本入口，提供跨平台一键本地调试 (`npm run dev`)、Windows 构建 (`npm run build`)、静态检查与自动化测试 (`npm test`)。 |
| `resources/icon.png` | 1024×1024 应用图标，构建时由 MyGo 自动生成 Windows `.exe` 内嵌图标及 NSIS 安装包图标。 |
| `internal/config/config.go` | 管理应用持久化配置（`config.json`），支持国服/国际服路径只读校验（二者至少其一，兼容根目录或 `game` 目录）与版本号读取。 |
| `internal/sqpack/hash.go` | 实现 FF14 SqPack 使用的 JamCRC32 哈希及 `.index` / `.index2` 路径哈希计算。 |
| `internal/sqpack/sqpack.go` | 纯 Go 并发安全只读 SqPack 归档读取器，支持 `.index` / `.index2` 查表及 `.dat0..N` 标准文件与贴图文件流式解压。 |
| `internal/sqpack/excel.go` | 纯 Go EXH / EXD / EXL 解析器，支持普通表（Variant=1）与子行表（Variant=2）按分页并发读取及全数据类型解包。 |
| `internal/sqpack/schema.go` | 基于 `schemas/*.yml`（`xivdev/EXDSchema`）将 EXH 原始列按 byte offset 升序动态对齐字段名，支持按需列裁剪读取 `SchemaSheet` / `SchemaSubrowSheet`。 |
| `internal/sqpack/sestring.go` | 纯 Go FF14 SeString 字节码解析与纯文本格式化器（支持换行、软连字符、表引用展开、玩家占位符可读化及私用区字形清理）。 |
| `internal/sqpack/texture.go` / `bc7.go` | 纯 Go `.tex` 贴图解码器（支持 BGRA8、BGRA4444、BGRA5551、L8/A8、DXT1/3/5、BC4/5 及 BC7 模式 0..7）与有界图像缓存。 |
| `internal/gamedata/types.go` | 定义道具、装备词条、食品药效、制作配方树、采集/垂钓/刺鱼、NPC 商店、雇员探险、任务（含详情插图与概要/对话文本）、理符、收藏品交易与成就称号全量业务模型。 |
| `internal/gamedata/eorzea_time.go` | 艾欧泽亚时间 (ET) 实时换算器与限时采集点开放窗口及现实剩余倒计时评估工具。 |
| `internal/gamedata/shop_loader.go` | 解析 `GilShop`、`SpecialShop`、`GCScripShopItem`、`FccShop`、`CollectablesShop` 关联、多跳 `ENpcBase` 引用链与 `Level` 3D 坐标。 |
| `internal/gamedata/retainer_loader.go` | 解析雇员探险定向筹集委托（狩猎/采矿/园艺/捕鱼）及 5 档属性门槛与产出数量索引。 |
| `internal/gamedata/quest_loader.go` | 解析 `Quest`、任务类型图标映射 (`061431` 系列)、详情横幅/专属徽章、前置条件、NPC 坐标、任务解锁及 `QuestClassJobReward`/`BeastRankBonus` 奖励。 |
| `internal/gamedata/quest_text.go` | 按需从 SqPack 只读解析 `quest/xxx/Id` 文本表，提取任务概要、任务目的与剧情对话流并自动匹配说话人名称（带内存有界缓存）。 |
| `internal/gamedata/leve_loader.go` | 解析 `Leve` 及其四类子表（`CraftLeve`、`GatheringLeve`、`BattleLeve`、`CompanyLeve`）、交纳物品、委托目标与 `LeveRewardItem` 概率奖励池。 |
| `internal/gamedata/collectables_loader.go` | 解析 `CollectablesShop` 收藏品交易、`SatisfactionNpc`/`SatisfactionSupply` 老主顾及 `HWDCrafterSupply` 苍天街供货三档价值与工票双向索引。 |
| `internal/gamedata/loader.go` | 并发加载全量核心游戏表与 Schema 对齐扩展表并构建内存正反向索引，原生支持**仅国服**、**仅国际服**与**双服联查**三种运行模式。 |
| `internal/gamedata/service.go` | 核心查询业务层：多语言搜索与筛选、装备/HQ/自动攻击/药效计算、递归配方树、3D→2D 地图坐标换算、任务/理符检索及成就查询。 |
| `internal/backup/backup.go` | 启动版本检查、应用数据目录 `backups/` 本地快照持久化与跨版本道具（含收藏品交纳/回报）、任务、理符、成就字段级 Diff 计算服务。 |
| `internal/exporter/schema.go` | 复用 `sqpack` 的 EXDSchema YAML 对齐能力，并从 `exd/root.exl` 读取 7,900+ 全量表清单。 |
| `internal/exporter/exporter.go` | 生成带 UTF-8 BOM 的 RFC 4180 / SaintCoinach 三行表头 CSV，支持本地目录清理重解包与 Zip 打包导出（含实时进度与中断）。 |
| `internal/uiapp/fonts.go` | 使用 `//go:embed` 内嵌 Google Sans (`Regular`/`Medium`/`Bold`) 字体文件并在应用初始化时通过 `ui.RegisterFont` 注册为默认西文与数字字体。 |
| `internal/uiapp/tokens.go` | 参考 Godwit 客户端构建 Tailwind v4 OKLCH 中性浅色/深色主题体系、默认字体栈 (`Google Sans` + 系统中文回退)、浮起面板 (`kitPanel`)、卡片、侧栏项、分段滑块、拖拽分隔条及统一矢量图标库。 |
| `internal/uiapp/icons.go` | 游戏图标与地图贴图的后台异步解码及 MyGo GPU `*ui.Bitmap` 有界缓存。 |
| `internal/uiapp/app.go` | 客户端顶层状态机，协调首次启动引导 (`modeSetup`)、异步解析加载 (`modeLoading`)、主题持久化切换、导航历史与主工作台 (`modeReady`)。 |
| `internal/uiapp/view_setup.go` | 单画布聚焦式首次引导选目录页（区服模式切换、拖拽路径识别与纠错）、三阶段实时加载进度页及居中「设置」中心（外观主题、路径配置、CSV 导出入口与运行状态）。 |
| `internal/uiapp/view_workbench.go` | 融合可拖拽顶栏（品牌菜单 + 折叠侧栏 + 上下文搜索）、左侧导航（【数据】物品/任务/理符/成就、【对比】版本 Diff、【设置】）及物品双浮起面板（含制作物品/采集/商店/探险/收藏品交纳等筛选）。 |
| `internal/uiapp/view_detail.go` | 物品阅读器风格悬浮详情面板（收藏品交易与老主顾交纳三档明细、任务/理符关联跳转、成就奖励、NPC 商店、递归配方树、采集捕鱼限时倒计时、雇员探险与下游用途）。 |
| `internal/uiapp/view_quests.go` | 任务检索与阅读工作台（任务类型图标、横幅详情图片与专属徽章、报酬与前置跳转，以及默认折叠防剧透的任务概要、目的与对话文本）。 |
| `internal/uiapp/view_leves.go` | 理符检索与阅读工作台（制作/采集/佣兵/军队四分栏筛选、发行点与目标地图坐标、交纳数量与重复次数、随机奖励道具池）。 |
| `internal/uiapp/view_achievements.go` | 悬浮面板成就与称号检索视图（分段过滤、大类筛选、男女称号展示与奖励道具一键跳转）。 |
| `internal/uiapp/view_diff.go` | 悬浮面板版本快照对比视图（支持物品/收藏品、任务、理符、成就筛选过滤、内联变更摘要与详情一键直跳）。 |
| `internal/uiapp/view_modals.go` | Godwit 对话框规范地图贴图预览弹窗（带坐标大头针）与全量 CSV 导出弹窗（原生目录/保存对话框 + 细进度条 + 取消控制）。 |
| `.github/workflows/ci.yml` | PR 与非主分支推送自动检查：`mygo.json` 校验、`gofmt`、`go vet`、`mygo vet`、单元测试（含无头 UI 测试）及 Windows `amd64/arm64` 构建验证。 |
| `.github/workflows/release.yml` | `master` 分支更新或推送 `v*` 标签时自动验证并构建 Windows `amd64/arm64` NSIS 安装包与便携 ZIP，生成 `SHA256SUMS.txt` 并创建/更新 GitHub Release。 |
| `.github/workflows/security.yml` | 每周定时运行 `govulncheck` 依赖漏洞扫描。 |

---

## 2. 模块间调用关系

```text
[本地 FF14 客户端 SqPack (国服 / 国际服，至少配置其一，严格只读)]
        │
        ▼
[internal/config] ──────────(路径校验、config.json 持久化、ffxivgame.ver)
        │
        ▼
[internal/sqpack] ──────────(纯 Go Archive/GameReader, EXH/EXD/EXL, Schema 对齐, SeString, .tex/BC7 解码)
        │
        ├──► [internal/gamedata] ──(loader / shop / retainer / quest / quest_text / leve / collectables / service / eorzea_time)
        ├──► [internal/backup]   ──(应用数据目录 backups/ 快照备份与跨版本 Diff)
        └──► [internal/exporter] ──(内嵌 schemas/*.yml 对齐、本地解包与 Zip 导出)
                    │
                    ▼
[internal/uiapp] (MyGo 原生 GPU UI, 1366×768 Framed-Sheet 工作台)
  ├──► view_setup.go        (首次启动路径引导 + 设置中心 + 系统文件夹选择器)
  ├──► view_workbench.go    (L型外框 + 顶栏检索 + 筛选 + 道具列表)
  ├──► view_detail.go       (道具完整详情 + 收藏品交纳 + 任务/理符来源 + 商店 + 配方树 + 采集倒计时 + 雇员探险)
  ├──► view_quests.go       (任务检索与细节详情 + 前置任务与奖励跳转)
  ├──► view_leves.go        (理符分类检索 + 交纳委托 + 随机奖励池)
  ├──► view_achievements.go (成就与称号一览)
  ├──► view_diff.go         (版本快照对比)
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
   - 图标和地图 `.tex` 在后台受限并发池解码后通过 `Window.Update` 安全投递到主线程上传 GPU，确保滚动列表与切换详情零卡顿；并通过 `ui.NewTester` 在 CI 中以 `1366 × 768` 无头方式自动验证引导流程、标签切换与弹窗交互。
