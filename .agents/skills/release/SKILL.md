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

## 5. 发版前全量校验（与 CI/Release 对齐）

版本号与文档同步完成后，推送 `master` 前必须在项目根目录完成以下全部校验（Windows 下设置 `$env:GOFLAGS = "-tags=mygo_nowebview"`，任一步失败则立即修复并重跑）：

1. **版本与清单一致性校验**：
   - 验证 `mygo.json` 包含非空 `name`、`slug`、`identifier`、`version`，且 `version` 匹配 `^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$`；
   - 验证 `package.json`（`version`）、`mygo.json`（`version`）与 `main.go`（`appVersion`）三处版本号完全一致为 `x.y.z`；
   - 验证 `CHANGELOG.md`、`docs/CHANGELOG.en.md` 与 `docs/CHANGELOG.ja.md` 顶部均保留空的 `## [Unreleased]` 且包含 `## [x.y.z] - YYYY-MM-DD`。
2. **格式与模块依赖校验**：
   - 执行 `gofmt -w main.go schemas_embed.go resources internal cmd` 后确认 `gofmt -l main.go schemas_embed.go resources internal cmd` 输出为空；
   - 执行 `go mod verify` 确认模块校验通过。
3. **静态分析与生产编译预检**：
   - 执行 `go vet ./...` 与 `go tool mygo vet .`；
   - 执行无安装包生产构建预检：`MYGO_LDFLAGS="-X main.appVersion=x.y.z"` 环境下运行 `go tool mygo build -skip-installer -platform windows/amd64`，确认可执行文件能正常编译通过，随后清理临时生成的 `build/` 目录（严禁将构建产物提交进仓库）。
4. **全量测试与 `-race` 竞态检测（强制过门）**：
   - **优先本机 `-race`**：若本机具备 CGO 编译环境（有 `gcc`/`clang`）或可用的 WSL/Docker Linux Go 环境，必须运行 `go test -race -count=1 . ./internal/...`；
   - **本机无 `-race` 环境时的 CI 预检兜底**（例如 Windows 未安装 C 编译器）：
     1. 先在本地运行 `go test -count=1 ./...` 确认全部通过，并重点排查本次涉及 goroutine / 并发 worker / 测试回调的代码是否存在未同步共享状态；
     2. 创建或保留提交后，**先推送到临时验证分支**（例如 `git push origin HEAD:refs/heads/ci-check-vx.y.z`）以触发 `.github/workflows/ci.yml`（在 Ubuntu 上执行 `go test -race` 及 Windows 双架构打包构建）；
     3. 使用 `gh run watch` 等待该 CI 运行全部通过后，立即删除远端临时分支（`git push origin --delete ci-check-vx.y.z`）；若失败则修复问题并 `git commit --amend` 后重新预检，**严禁在 `-race` 未通过前将提交推入 `master`**。

## 6. 提交并推送至远程仓库

1. 执行 `git add -A` 暂存全部变更（含已更新的 Submodule 指针），用 `git status` 确认无误且未混入 `.env`、`build/` 构建产物或临时文件。
2. 创建 Commit（若已通过临时验证分支预检且工作区无新修改，直接复用已验证的 Commit 即可）：
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
4. **推送后流水线确认**：
   - 若本机已安装并登录 `gh` CLI，推送 `master` 后执行 `gh run list --limit 3` / `gh run watch` 确认对应的 `Release Windows Client` 工作流成功启动并通过（如遇异常及时定位汇报）。

## 7. 结果汇报

向用户简明汇报：
- 已发布版本号与 Commit 哈希/分支；
- Submodule 更新情况；
- 检查与自动化测试通过情况；
- 本次变更汇总要点（无需展开底层实现细节）。
