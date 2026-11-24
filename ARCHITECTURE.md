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
| `internal/sqpack/sestring.go` | 纯 Go FF14 SeString 字节码解析与纯文本格式化器（支持换行、软连字符及样式控制标签清理）。 |
| `internal/sqpack/texture.go` / `bc7.go` | 纯 Go `.tex` 贴图解码器（支持 BGRA8、BGRA4444、BGRA5551、L8/A8、DXT1/3/5、BC4/5 及 BC7 模式 0..7）与有界图像缓存。 |
| `internal/gamedata/types.go` | 定义道具、装备词条、食品药效、制作配方树、采集/垂钓/刺鱼、NPC 商店、雇员探险与成就称号全量业务模型。 |
| `internal/gamedata/eorzea_time.go` | 艾欧泽亚时间 (ET) 实时换算器与限时采集点开放窗口及现实剩余倒计时评估工具。 |
| `internal/gamedata/shop_loader.go` | 解析 `GilShop`、`SpecialShop`、`GCScripShopItem`、`FccShop` 及多跳 `ENpcBase` 引用链与 `Level` 3D 坐标。 |
| `internal/gamedata/retainer_loader.go` | 解析雇员探险定向筹集委托（狩猎/采矿/园艺/捕鱼）及 5 档属性门槛与产出数量索引。 |
| `internal/gamedata/loader.go` | 并发加载全量核心游戏表并构建内存正反向索引，原生支持**仅国服**、**仅国际服**与**双服联查**三种运行模式。 |
| `internal/gamedata/service.go` | 核心查询业务层：多语言搜索与筛选、装备/HQ/自动攻击/药效计算、递归配方树、3D→2D 地图坐标换算与成就检索。 |
| `internal/backup/backup.go` | 启动版本检查、应用数据目录 `backups/` 本地快照持久化与跨版本道具/成就字段级 Diff 计算服务。 |
| `internal/exporter/schema.go` | 解析内嵌 EXDSchema YAML 结构并按 byte offset 升序与 EXH 列动态对齐，从 `exd/root.exl` 读取 7,900+ 全量表清单。 |
| `internal/exporter/exporter.go` | 生成带 UTF-8 BOM 的 RFC 4180 / SaintCoinach 三行表头 CSV，支持本地目录清理重解包与 Zip 打包导出（含实时进度与中断）。 |
| `internal/uiapp/tokens.go` | 参考 Godwit 客户端构建 Tailwind v4 OKLCH 中性浅色/深色主题体系、浮起面板 (`kitPanel`)、卡片、侧栏项、分段滑块、拖拽分隔条及统一矢量图标库。 |
| `internal/uiapp/icons.go` | 游戏图标与地图贴图的后台异步解码及 MyGo GPU `*ui.Bitmap` 有界缓存。 |
| `internal/uiapp/app.go` | 客户端顶层状态机，协调首次启动引导 (`modeSetup`)、异步解析加载 (`modeLoading`)、主题持久化切换、导航历史与主工作台 (`modeReady`)。 |
| `internal/uiapp/view_setup.go` | Godwit 风格左右分栏首次引导页与居中「设置」中心（含外观主题预览卡、客户端路径配置、CSV 导出入口与运行状态）。 |
| `internal/uiapp/view_workbench.go` | 融合可拖拽顶栏（品牌菜单 + 折叠侧栏 + 上下文搜索）、精简左侧导航（【数据】成就/物品、【对比】版本 Diff、【设置】）及物品双浮起面板（内置制作物品/采集/商店/探险/职业与高级筛选抽屉）。 |
| `internal/uiapp/view_detail.go` | 物品阅读器风格悬浮详情面板（最近足迹、NQ/HQ 滑块、装备性能与附加特性、药效、成就奖励、NPC 商店、递归配方树、采集捕鱼限时倒计时、雇员探险与下游用途）。 |
| `internal/uiapp/view_achievements.go` | 悬浮面板成就与称号检索视图（分段过滤、大类筛选、男女称号展示与奖励道具一键跳转）。 |
| `internal/uiapp/view_diff.go` | 悬浮面板版本快照对比视图（基准/目标快照下拉、内联变更摘要、红绿语义差异芯片与手动刷新备份）。 |
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
[internal/sqpack] ──────────(纯 Go Archive/GameReader, EXH/EXD/EXL, SeString, .tex/BC7 解码)
        │
        ├──► [internal/gamedata] ──(loader / shop / retainer / service / eorzea_time)
        ├──► [internal/backup]   ──(应用数据目录 backups/ 快照备份与跨版本 Diff)
        └──► [internal/exporter] ──(内嵌 schemas/*.yml 对齐、本地解包与 Zip 导出)
                    │
                    ▼
[internal/uiapp] (MyGo 原生 GPU UI, 1366×768 Framed-Sheet 工作台)
  ├──► view_setup.go        (首次启动路径引导 + 设置中心 + 系统文件夹选择器)
  ├──► view_workbench.go    (L型外框 + 顶栏检索 + 四栏 KPI + 筛选 + 道具列表)
  ├──► view_detail.go       (道具完整详情 + 商店 + 配方树 + 采集倒计时 + 雇员探险)
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
3. **严格只读访问与内嵌 Schema 资源**：
   - 所有游戏文件仅通过 `os.Open` 只读打开，所有用户配置、版本快照均写入系统标准应用数据目录（`mygo.PathUserData`）；全部 1,198 份 `xivdev/EXDSchema` YAML 定义通过 `//go:embed` 内嵌进可执行文件，实现零外部文件依赖的绿色便携分发。
4. **原生 GPU 异步贴图解码与无头 UI 自动化测试**：
   - 图标和地图 `.tex` 在后台受限并发池解码后通过 `Window.Update` 安全投递到主线程上传 GPU，确保滚动列表与切换详情零卡顿；并通过 `ui.NewTester` 在 CI 中以 `1366 × 768` 无头方式自动验证引导流程、标签切换与弹窗交互。
