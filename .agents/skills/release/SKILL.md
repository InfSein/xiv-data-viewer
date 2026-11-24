---
name: release
description: >-
  当用户发送“发版：x.y.z”（或“发版: x.y.z”）或要求整理 diff、更新版本号与 CHANGELOG.md、完成检查并提交推送发版 Commit 时使用此技能。
---

# 项目发版自动化流程 (`release`)

收到 `发版：<x.y.z>` 指令或发版请求后，按以下步骤顺序执行（Windows 环境统一确保 UTF-8 输出，并传入 `GOFLAGS=-tags=mygo_nowebview`）：

## 1. 解析与校验版本号

1. 从用户消息提取目标版本号 `x.y.z`（若带前缀 `v` 则去除）。
2. 校验格式须匹配语义化版本正则 `^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$`（与 `.github/workflows/ci.yml` 一致）。

## 2. 盘点当前改动

1. 执行 `git status`、`git diff` 与未跟踪文件检查，结合代码内容梳理本次尚未发布的所有真实变更。
2. 读取根目录 `CHANGELOG.md` 的 `## [Unreleased]` 区块，将其与工作区未归档改动去重合并，按 `新增`、`优化与调整`、`修复`、`工程与测试` 分类整理为结构化中文汇总。

## 3. 同步更新版本号与文档

1. 更新以下三处版本号为 `x.y.z`：
   - `package.json` 中的 `"version"`
   - `mygo.json` 中的 `"version"`（`.github/workflows/release.yml` 依赖此字段生成 Release 标签与安装包名称）
   - `main.go` 中的 `var appVersion = "..."`
2. 更新 `CHANGELOG.md`：
   - 将 `[Unreleased]` 下积累的内容与工作区本次改动合并写入 `## [x.y.z] - YYYY-MM-DD`（使用当前本地日期）；
   - 在顶部保留空的 `## [Unreleased]` 区块，供后续任务继续自动记录。
3. 若本次 diff 新增或删除了功能模块且尚未同步到 `ARCHITECTURE.md`，一并补充完整。

## 4. 发版前全量校验

在项目根目录执行以下命令（Windows 下设置 `$env:GOFLAGS = "-tags=mygo_nowebview"`，任一步失败则先修复再重跑）：

1. 格式检查与自动修复：`gofmt -w main.go schemas_embed.go internal` 后确认 `gofmt -l main.go schemas_embed.go internal` 输出为空；
2. 静态检查：`go vet . ./internal/...` 与 `go tool mygo vet .`；
3. 自动化测试：`go test -count=1 . ./internal/...`（若当前系统环境允许竞争检测可附带 `-race`，Windows 默认无 CGO 时省略 `-race`）。

## 5. 提交并推送至远程仓库

1. 执行 `git add -A` 暂存全部变更，用 `git status` 确认无误且未混入 `.env`、构建产物或临时文件。
2. 创建 Commit（标题严格为 `Version x.y.z`，正文为本版本改动汇总）：
   - 为避免 Windows PowerShell 中文编码乱码，将 UTF-8（无 BOM）提交信息写入 `.git/RELEASE_COMMIT_MSG`，执行 `git commit -F .git/RELEASE_COMMIT_MSG` 后删除 `.git/RELEASE_COMMIT_MSG`。
3. 推送至当前分支对应的远端分支（如 `git push origin master`）：
   - 注意：`.github/workflows/release.yml` 会在 `master` 收到推送时自动构建 Windows 安装包、便携包并创建/更新 `vx.y.z` GitHub Release；**切勿**同时手动执行 `git push origin vx.y.z`，避免触发两次并发 Release 工作流。

## 6. 结果汇报

向用户简明汇报：
- 已发布版本号与 Commit 哈希/分支；
- 检查与测试通过情况；
- 本次变更汇总要点（无需展开底层实现细节）。
