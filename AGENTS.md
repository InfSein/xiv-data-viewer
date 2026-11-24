# 项目开发准则 (AGENTS.md)

## 1. 数据安全与环境红线

- **严格只读**：对 FF14 国服/国际服客户端目录仅允许只读解析，严禁写入、修改或删除任何游戏文件。
- **路径配置**：首次启动通过引导页要求用户配置国服或国际服路径（至少其一），持久化于系统应用数据目录（`mygo.PathUserData`）的 `config.json`，禁止在源码硬编码默认客户端路径；本地集成测试可通过根目录 `.env`（参考 `.env.example`）提供 `FFXIV_CN_PATH` / `FFXIV_GLOBAL_PATH`。
- **快照目录**：启动版本检查与快照统一保存在应用数据目录的 `backups/` 下（测试用临时目录）。
- **提交前校验**：须通过 `gofmt`、`go vet ./...`、`go tool mygo vet .` 与 `go test ./...`。

## 2. 技术栈与客户端规范

- **应用框架**：纯 Go 原生桌面应用 [`egoist/mygo`](https://github.com/egoist/mygo)（无 cgo、无 WebView2/Node 运行时，Windows 调试与构建传入 `GOFLAGS=-tags=mygo_nowebview`）。
- **窗口规格**：默认 `1366 × 768`，最小 `1024 × 640`。
- **数据引擎**：纯 Go 解析引擎位于 `internal/sqpack/`，Excel 表结构内嵌 `schemas/*.yml`（Git Submodule `xivdev/EXDSchema`）。

## 3. SqPack 解包与格式扩展指南

当用户提出新的 FF14 客户端文件解包或二进制解析需求（如新增文件格式、`.exh`/`.exd`/`.exl` 字段类型扩展、`.tex` 新贴图格式、`SeString` 新控制标签、`.lgb` 场景布局、模型/音效等）时：

1. **优先从 `@ffcafe/elpis` 获取解析方法**：
   - 严禁凭记忆猜测二进制结构、偏移量或编码规则，须先查阅 [`@ffcafe/elpis`](https://github.com/thewakingsands/elpis) 仓库或对应 npm 包源码，重点参考：
     - `@ffcafe/elpis-sqpack`：SqPack `.index`/`.index2`/`.dat` 索引哈希、分块解压、EXL/EXH/EXD、`.tex` 贴图、`.lgb` 布局与多语言读取；
     - `@ffcafe/elpis-sestring`：SeString 控制字节码与文本格式化；
     - `@ffcafe/elpis-struct`：基础二进制结构与字段类型定义。
2. **纯 Go 只读移植**：
   - 将确认后的解析算法以纯 Go、并发安全、严格只读的方式移植到 `internal/sqpack/`，禁止引入外部运行时依赖。
   - 涉及 Excel 表字段映射时，同步结合内嵌的 `schemas/*.yml`（`xivdev/EXDSchema`）对齐列偏移。
3. **验证与测试**：
   - 为新增解包能力编写 `internal/sqpack/` 单元测试；依赖本机客户端文件的用例从 `.env` 读取路径，未配置时使用 `t.Skip` 跳过，确保无客户端的 CI 环境可通过。

## 4. 新增数据解析接入版本 Diff 规范

当用户要求新增游戏数据解析项目（如任务、理符、收藏品、宠物、坐骑、副本等任何**具有稳定主键、且可能随游戏版本更新产生增删或数值改动**的数据集）时，**只要适合加入版本对比，须同步接入版本 Diff**（纯 UI 辅助映射或无独立变更对比价值的底层资源除外）：

1. **快照结构与兼容性**：在 `internal/backup/backup.go` 的 `VersionBackupMeta` 与 `snapshotPayload` 中补充紧凑快照字段（独立实体使用专属切片，附属于物品或成就的子能力则以可选 `*bool` / 摘要字段挂载于原有紧凑记录上），并在启动快照检查中兼容历史快照平滑升级，避免把老版本已有数据误报为新增。
2. **字段级对比与汇总**：在 `CompareVersions` 与 `DiffSummary` 中实现新增、变更与移除检测及清晰的中文 `ItemFieldChange` 明细；若某功能集成在现有视图（如收藏品交易集成在物品内），Diff 中也归入对应主实体记录并高亮标签，不单独拆出独立菜单。
3. **界面与测试同步**：更新 `internal/uiapp/view_diff.go`（变更摘要统计、筛选开关、详情直跳按钮）与顶部差异检索，补充 `internal/backup/` 及 `internal/uiapp/` 单元测试，并同步更新 `ARCHITECTURE.md`。

## 5. UI 设计规范 (`framed-sheet`)

- **布局结构**：一体式浅色外框（`#f9fbfc`），左侧导航栏 `228px` + 顶部检索栏 `48px`（无分割线），右下内嵌白底工作台画布（`#ffffff`、`1px #e8eaec` 边框、`16px` 圆角、距右/底各 `8px`）。
- **令牌复用**：色彩、胶囊、KPI 带与图标统一复用 `internal/uiapp/tokens.go`（`colGround`~`colPrimary`、`drawPill`、`drawKPIStrip`、`numText`）。
- **排版对齐**：字体栈 `"Google Sans, Microsoft YaHei UI, Microsoft YaHei, PingFang SC, Segoe UI, system-ui"`（西文与数字默认内嵌 Google Sans，中文自动回退系统字体），最小字号 `11px`；所有数字启用 `FontFeatures("tnum")` 等宽特性，表格数值列右对齐（`ui.End`）。
- **视觉禁令**：严禁渐变、发光、毛玻璃（`Blur`/`Vibrancy`）、卡片阴影（`Shadow`）、斑马纹表格、KPI 装饰圆圈以及 `★`/Emoji 符号（星级统一使用 `Lv.100 · 2星` 等纯文本）。

## 6. 变更日志与发版流程规范

1. **任务完成自动维护 `CHANGELOG.md`**：
   - 后续任何 Agent 完成功能开发、体验优化、问题修复或工程配置变更后，在任务收尾前**必须同步更新**根目录 `CHANGELOG.md` 的 `## [Unreleased]` 区块（按 `### 新增`、`### 优化与调整`、`### 修复`、`### 工程与测试` 分类追加简洁准确的中文说明，仅纯文档或注释调整可酌情省略）。
2. **发版指令自动执行（`发版：x.y.z`）**：
   - 当用户发送 `发版：x.y.z`（或 `发版: x.y.z`，例如 `发版：2.1.0`）时，自动读取并执行项目内置技能 [release](.agents/skills/release/SKILL.md)：
     1. 汇总当前 `git diff`、未跟踪文件及 `CHANGELOG.md` 的 `[Unreleased]` 记录；
     2. 同步更新 `package.json`（`version`）、`mygo.json`（`version`）与 `main.go`（`appVersion`）为 `x.y.z`；
     3. 将 `CHANGELOG.md` 归档为 `## [x.y.z] - YYYY-MM-DD` 并保留空的 `## [Unreleased]`；
     4. 通过 `gofmt`、`go vet`、`go tool mygo vet .` 与 `go test` 全量校验（Windows 传入 `GOFLAGS=-tags=mygo_nowebview`）；
     5. 暂存全部变更，提交标题为 `Version x.y.z`、正文为中文改动汇总的 Commit，并推送至当前远端分支（由 `.github/workflows/release.yml` 自动根据 `mygo.json` 发布 GitHub Release，切勿同时手动推送 `vx.y.z` 标签）。
