---
name: release
description: >-
  当用户发送“发版：x.y.z”（或“发版: x.y.z”）或要求更新子模块并校验、整理 diff、更新版本号与 CHANGELOG.md、提交推送发版 Commit 时使用此技能。
---

# 项目发版自动化流程 (`release`)

收到 `发版：<x.y.z>` 指令或发版请求后，按以下步骤顺序执行（Windows 环境统一确保 UTF-8 输出，并传入 `GOFLAGS=-tags=mygo_nowebview`）：

## 1. 解析与校验版本号

1. 从用户消息提取目标版本号 `x.y.z`（若带前缀 `v` 则去除）。
2. 校验格式须匹配语义化版本正则 `^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$`（与 `.github/workflows/ci.yml` 一致）。

## 2. 更新所有 Submodule 并跑自动化测试确认

1. 同步配置并拉取项目中所有依赖的 Git Submodule（含 `.gitmodules` 指定的跟踪分支最新提交，如 `schemas`）：
   - `git submodule sync --recursive`
   - `git submodule update --init --remote --recursive`
2. 检查 Submodule 变更情况：
   - 执行 `git submodule status --recursive` 与 `git diff --submodule` 确认各子模块状态。
   - 若 Submodule 指针有更新（如 `schemas`），用于后续写入发版 Commit 正文（无需向面向普通用户的更新日志写入底层工程细节）。
3. 在项目根目录立即跑一遍自动化测试确认无兼容性问题（Windows 下先设置 `$env:GOFLAGS = "-tags=mygo_nowebview"`）：
   - `go test -count=1 . ./internal/...`（非 Windows 或开启 CGO 的环境可附带 `-race`）
   - 若测试未通过，立即中止发版并定位修复或向用户汇报失败原因，严禁带着失败测试继续修改版本号或发版。

## 3. 盘点当前改动

1. 执行 `git status`、`git diff` 与未跟踪文件检查，结合代码内容及本轮 Submodule 变更梳理本次尚未发布的所有真实改动。
2. 读取根目录 `CHANGELOG.md` 以及 `docs/CHANGELOG.en.md`、`docs/CHANGELOG.ja.md` 的 `## [Unreleased]` 区块，将其与工作区未归档的用户向改动去重合并，按中/英/日三语对应分类（`新增`/`优化与调整`/`修复`、`Added`/`Changed`/`Fixed`、`新規追加`/`改善・調整`/`修正`）整理为通俗、简练的用户向汇总（避免写入内部代码符号、文件路径或测试细节），并提炼出对应极简英文要点（用于 Commit 正文）。

## 4. 同步更新版本号与文档

1. 更新以下三处版本号为 `x.y.z`：
   - `package.json` 中的 `"version"`
   - `mygo.json` 中的 `"version"`（`.github/workflows/release.yml` 依赖此字段生成 Release 标签与安装包名称）
   - `main.go` 中的 `var appVersion = "..."`
2. 同步更新 `CHANGELOG.md`、`docs/CHANGELOG.en.md` 与 `docs/CHANGELOG.ja.md`：
   - 将三份文件中 `[Unreleased]` 下积累的内容与工作区本次用户向改动对齐合并写入 `## [x.y.z] - YYYY-MM-DD`（使用当前本地日期），保持面向普通玩家的简练风格；
   - 在三份文件顶部均保留空的 `## [Unreleased]` 区块，供后续任务继续自动记录。
3. 若本次 diff 新增或删除了功能模块且尚未同步到 `ARCHITECTURE.md`，一并补充完整。

## 5. 发版前全量校验

版本号与文档同步完成后，在项目根目录执行以下命令（Windows 下设置 `$env:GOFLAGS = "-tags=mygo_nowebview"`，任一步失败则先修复再重跑）：

1. 格式检查与自动修复：`gofmt -w main.go schemas_embed.go internal` 后确认 `gofmt -l main.go schemas_embed.go internal` 输出为空；
2. 静态检查：`go vet . ./internal/...` 与 `go tool mygo vet .`；
3. 自动化测试：`go test -count=1 . ./internal/...`（确保最终提交前的代码、版本号改动与内嵌资源全量通过）。

## 6. 提交并推送至远程仓库

1. 执行 `git add -A` 暂存全部变更（含已更新的 Submodule 指针），用 `git status` 确认无误且未混入 `.env`、构建产物或临时文件。
2. 创建 Commit：
   - **标题**：严格为 `Version x.y.z`（例如 `Version 2.0.1`）
   - **正文**：使用**极简英文**，每条更改以 `* `（星号加空格）开头，一行一条概括核心改动，不要中文或长句。示例：
     ```text
     Version 2.0.1

     * Updated i18n
     * Updated EXDSchema submodule
     ```
   - Windows PowerShell 下建议将 UTF-8（无 BOM）提交信息写入 `.git/RELEASE_COMMIT_MSG`，执行 `git commit -F .git/RELEASE_COMMIT_MSG` 后删除 `.git/RELEASE_COMMIT_MSG`。
3. 推送至当前分支对应的远端分支（如 `git push origin master`）：
   - 注意：`.github/workflows/release.yml` 会在 `master` 收到推送时自动构建 Windows 安装包、便携包并创建/更新 `vx.y.z` GitHub Release；**切勿**同时手动执行 `git push origin vx.y.z`，避免触发两次并发 Release 工作流。

## 7. 结果汇报

向用户简明汇报：
- 已发布版本号与 Commit 哈希/分支；
- Submodule 更新情况；
- 检查与自动化测试通过情况；
- 本次变更汇总要点（无需展开底层实现细节）。
