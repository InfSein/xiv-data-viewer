package uiapp

import (
	"fmt"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/exporter"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *App) currentDiffExportQuery() backup.VersionDiffQuery {
	return backup.VersionDiffQuery{
		Region:     a.diffRegion,
		From:       a.diffFromVer,
		To:         a.diffToVer,
		Filter:     a.diffFilter,
		EntityType: a.diffEntity,
		Q:          a.diffSearch,
	}
}

// openDiffExportModal 打开版本对比导出弹窗，并按当前筛选分类智能预选默认导出模式与命名格式。
func (a *App) openDiffExportModal() {
	if a.diffRes == nil || a.diffRes.NeedsDownload || a.diffRes.TotalFiltered == 0 {
		return
	}
	a.langPopoverOpen = false
	a.statusPopoverOpen = false
	a.globalSearchOpen = false

	isRunning := a.diffExportProgress != nil && a.diffExportProgress.IsRunning
	if !isRunning {
		a.diffExportKind = exporter.DefaultDiffExportKind(a.diffEntity)
		a.diffExportIconNaming = exporter.DefaultDiffIconNaming(a.diffEntity)
		a.diffExportPreferHR1 = true
		if a.diffExportAudioFormat == "" {
			a.diffExportAudioFormat = exporter.DiffAudioOGG
		}
		if strings.TrimSpace(a.diffExportDirDraft) == "" {
			if strings.TrimSpace(a.cfg.DiffExportOutputDir) != "" {
				a.diffExportDirDraft = a.cfg.DiffExportOutputDir
			} else if strings.TrimSpace(a.cfg.IconExportOutputDir) != "" {
				a.diffExportDirDraft = a.cfg.IconExportOutputDir
			}
		}
		a.diffExportProgress = nil
		a.diffExportError = ""
	}
	a.diffExportOpen = true
}

func (a *App) persistDiffExportDir(dir string) {
	a.cfg.DiffExportOutputDir = strings.TrimSpace(dir)
	if strings.TrimSpace(a.dataDir) != "" {
		_ = config.SaveAppConfig(a.dataDir, a.cfg)
	}
}

func (a *App) buildDiffExportOptions(outPath string, toZip bool) exporter.DiffExportOptions {
	naming := a.diffExportIconNaming
	if a.diffEntity == "icon" {
		naming = exporter.DiffIconNamingIconID
	} else if naming == "" {
		naming = exporter.DefaultDiffIconNaming(a.diffEntity)
	}
	audioFmt := a.diffExportAudioFormat
	if audioFmt == "" {
		audioFmt = exporter.DiffAudioOGG
	}
	return exporter.DiffExportOptions{
		Query:           a.currentDiffExportQuery(),
		Kind:            a.diffExportKind,
		Lang:            a.Language(),
		PreferHR1:       a.diffExportPreferHR1,
		IconNaming:      naming,
		IncludeOldIcons: a.diffEntity == "icon" && a.diffExportIncludeOldIcon,
		AudioFormat:     audioFmt,
		ToZip:           toZip,
		OutputPath:      outPath,
	}
}

func (a *App) startDiffExportTask(opts exporter.DiffExportOptions) {
	if a.diffExporterSvc == nil {
		return
	}
	a.diffExportError = ""
	onUpdate := func(p exporter.DiffExportProgress) {
		cp := p
		a.dispatchUI(func() {
			a.diffExportProgress = &cp
		})
	}
	if err := a.diffExporterSvc.StartExport(opts, onUpdate); err != nil {
		a.diffExportError = err.Error()
	}
}

func (a *App) startDiffLocalBatchExportFlow() {
	dir := strings.TrimSpace(a.diffExportDirDraft)
	if dir == "" {
		a.diffExportError = a.t("diff_export.err_no_dir")
		return
	}
	a.persistDiffExportDir(dir)
	a.startDiffExportTask(a.buildDiffExportOptions(dir, false))
}

func (a *App) startDiffZipBatchExportFlow() {
	if a.diffExporterSvc == nil || a.win == nil {
		return
	}
	a.diffExportError = ""
	base := exporter.DefaultDiffExportBaseName(a.diffRes, a.diffEntity, a.diffFilter, a.diffExportKind)
	defaultName := base + ".zip"
	title := a.t("diff_export.zip_dialog_title")
	filterName := a.t("icon_export.zip_filter_name")
	optsTemplate := a.buildDiffExportOptions("", true)

	go func() {
		savePath, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:            a.win,
			Title:             title,
			DefaultPath:       defaultName,
			CreateDirectories: true,
			Filters:           []mygo.FileFilter{{Name: filterName, Extensions: []string{"zip"}}},
		})
		if err != nil || strings.TrimSpace(savePath) == "" {
			return
		}
		a.dispatchUI(func() {
			optsTemplate.OutputPath = savePath
			a.startDiffExportTask(optsTemplate)
		})
	}()
}

func (a *App) startDiffSingleFileExportFlow() {
	if a.diffExporterSvc == nil || a.win == nil {
		return
	}
	a.diffExportError = ""
	ext := "csv"
	filterName := a.t("diff_export.filter_csv")
	switch a.diffExportKind {
	case exporter.DiffExportJSON:
		ext = "json"
		filterName = a.t("diff_export.filter_json")
	case exporter.DiffExportMarkdown:
		ext = "md"
		filterName = a.t("diff_export.filter_md")
	}
	base := exporter.DefaultDiffExportBaseName(a.diffRes, a.diffEntity, a.diffFilter, a.diffExportKind)
	defaultName := base + "." + ext
	title := a.t("diff_export.save_dialog_title")
	optsTemplate := a.buildDiffExportOptions("", false)

	go func() {
		savePath, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:            a.win,
			Title:             title,
			DefaultPath:       defaultName,
			CreateDirectories: true,
			Filters:           []mygo.FileFilter{{Name: filterName, Extensions: []string{ext}}},
		})
		if err != nil || strings.TrimSpace(savePath) == "" {
			return
		}
		a.dispatchUI(func() {
			optsTemplate.OutputPath = savePath
			a.startDiffExportTask(optsTemplate)
		})
	}()
}

func (a *App) diffExportEntityLabel(entity string) string {
	switch entity {
	case "item":
		return a.t("diff.entity_item")
	case "quest":
		return a.t("diff.entity_quest")
	case "leve":
		return a.t("diff.entity_leve")
	case "achievement":
		return a.t("diff.entity_achievement")
	case "bgm":
		return a.t("diff.entity_bgm")
	case "icon":
		return a.t("diff.entity_icon")
	case "text":
		return a.t("diff.entity_text")
	default:
		return a.t("diff.entity_all")
	}
}

func (a *App) diffExportFilterLabel(filter string) string {
	switch filter {
	case "added":
		return a.t("diff.filter_added")
	case "modified":
		return a.t("diff.filter_modified")
	case "removed":
		return a.t("diff.filter_removed")
	default:
		return a.t("diff.filter_all")
	}
}

func (a *App) diffExportIconExampleName() string {
	if a.diffEntity == "icon" || a.diffExportIconNaming == exporter.DiffIconNamingIconID {
		if a.diffRes != nil {
			for _, it := range a.diffRes.Items {
				if it.IconID > 0 || (it.EntityType == "icon" && it.IconID >= 0) {
					return fmt.Sprintf("%06d.png", it.IconID)
				}
			}
		}
		return "026290.png"
	}
	if a.diffRes != nil {
		for _, it := range a.diffRes.Items {
			if it.IconID > 0 {
				return exporter.FormatDiffIconFileName(it, a.diffEntity, a.diffExportIconNaming, false)
			}
		}
	}
	if a.diffEntity == "all" {
		return "item_44099_Name.png"
	}
	return "44099_Name.png"
}

func (a *App) diffExportAudioExampleName() string {
	ext := ".ogg"
	if a.diffExportAudioFormat == exporter.DiffAudioSCD {
		ext = ".scd"
	}
	if a.diffRes != nil {
		for _, it := range a.diffRes.Items {
			if it.EntityType == "bgm" && it.ChangeType != "removed" && it.File != "" {
				name := exporter.SanitizeFileName(it.NameCn)
				if name != "" {
					return fmt.Sprintf("%d_%s%s", it.ID, name, ext)
				}
				return fmt.Sprintf("%d%s", it.ID, ext)
			}
		}
	}
	return "1024_BGM" + ext
}

// renderDiffExportModal 绘制「版本对比」统一导出弹窗（支持图标高清批量 PNG、物品图标按编号_名称导出、BGM 音频批量导出与 CSV/JSON/Markdown 报告）。
func (a *App) renderDiffExportModal(c *ui.Context) {
	isRunning := a.diffExportProgress != nil && a.diffExportProgress.IsRunning
	if !a.diffExportOpen && !isRunning {
		return
	}
	t := themeOf(c)

	fromDisp := a.diffFromVer
	toDisp := a.diffToVer
	totalFiltered := 0
	if a.diffRes != nil {
		if a.diffRes.FromDisplayName != "" {
			fromDisp = a.diffRes.FromDisplayName
		}
		if a.diffRes.ToDisplayName != "" {
			toDisp = a.diffRes.ToDisplayName
		}
		totalFiltered = a.diffRes.TotalFiltered
	}

	// 确保当前选中的导出方式对当前分类有效
	if a.diffExportKind == exporter.DiffExportIcons && !exporter.DiffEntitySupportsIcons(a.diffEntity) {
		a.diffExportKind = exporter.DefaultDiffExportKind(a.diffEntity)
	}
	if a.diffExportKind == exporter.DiffExportAudio && !exporter.DiffEntitySupportsAudio(a.diffEntity) {
		a.diffExportKind = exporter.DefaultDiffExportKind(a.diffEntity)
	}
	if a.diffExportKind == "" {
		a.diffExportKind = exporter.DefaultDiffExportKind(a.diffEntity)
	}

	isBatchKind := a.diffExportKind == exporter.DiffExportIcons || a.diffExportKind == exporter.DiffExportAudio

	ui.DialogBase(c.Key("diff-export-modal"), &a.diffExportOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(t.scrim)
		applyOverlaySurface(c, panel.Width(580).Radius(t.radius.x2l).Clip(), shadowDialog)

		// 1. 弹窗标题栏
		ui.Row(c).FillWidth().Padding(16, 20, 12, 20).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Start).Gap(12).Children(func() {
			ui.Column(c).Gap(3).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, a.t("diff_export.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				var sub string
				if q := strings.TrimSpace(a.diffSearch); q != "" {
					sub = a.tf("diff_export.subtitle_with_q",
						"from", fromDisp,
						"to", toDisp,
						"entity", a.diffExportEntityLabel(a.diffEntity),
						"filter", a.diffExportFilterLabel(a.diffFilter),
						"q", q,
						"count", formatInt(totalFiltered),
					)
				} else {
					sub = a.tf("diff_export.subtitle",
						"from", fromDisp,
						"to", toDisp,
						"entity", a.diffExportEntityLabel(a.diffEntity),
						"filter", a.diffExportFilterLabel(a.diffFilter),
						"count", formatInt(totalFiltered),
					)
				}
				numText(c, sub).FontSize(t.text.xs).TextColor(t.muted)
			})
			if kitIconButton(c, "diff-exp-close-top", icoClose, a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm, disabled: isRunning}) {
				a.diffExportOpen = false
			}
		})

		// 2. 可滚动配置区域（兼容 640px 最小窗口高度）
		ui.Scroll(c.Key("diff-exp-body-scroll")).FillWidth().MaxHeight(410).Children(func() {
			ui.Column(c).FillWidth().Padding(14, 20, 16, 20).Gap(14).Children(func() {
				// 导出内容类型选择
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					ui.Text(c, a.t("diff_export.kind_label")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
					ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
						if exporter.DiffEntitySupportsIcons(a.diffEntity) {
							lbl := a.t("diff_export.kind_icons")
							if a.diffEntity == "item" {
								lbl = a.t("diff_export.kind_item_icons")
							}
							if kitToggleChip(c, "diff-exp-kind-icons", lbl, "", a.diffExportKind == exporter.DiffExportIcons, nil) && !isRunning {
								a.diffExportKind = exporter.DiffExportIcons
								a.diffExportError = ""
							}
						}
						if exporter.DiffEntitySupportsAudio(a.diffEntity) {
							if kitToggleChip(c, "diff-exp-kind-audio", a.t("diff_export.kind_audio"), "", a.diffExportKind == exporter.DiffExportAudio, nil) && !isRunning {
								a.diffExportKind = exporter.DiffExportAudio
								a.diffExportError = ""
							}
						}
						if kitToggleChip(c, "diff-exp-kind-csv", a.t("diff_export.kind_csv"), "", a.diffExportKind == exporter.DiffExportCSV, nil) && !isRunning {
							a.diffExportKind = exporter.DiffExportCSV
							a.diffExportError = ""
						}
						if kitToggleChip(c, "diff-exp-kind-json", a.t("diff_export.kind_json"), "", a.diffExportKind == exporter.DiffExportJSON, nil) && !isRunning {
							a.diffExportKind = exporter.DiffExportJSON
							a.diffExportError = ""
						}
						if kitToggleChip(c, "diff-exp-kind-md", a.t("diff_export.kind_markdown"), "", a.diffExportKind == exporter.DiffExportMarkdown, nil) && !isRunning {
							a.diffExportKind = exporter.DiffExportMarkdown
							a.diffExportError = ""
						}
					})
				})

				// 按导出类型展示专属选项
				switch a.diffExportKind {
				case exporter.DiffExportIcons:
					// 清晰度选项：默认优先高清 (HD)，无高清回退标准图
					ui.Column(c).FillWidth().Gap(6).Children(func() {
						ui.Text(c, a.t("diff_export.res_label")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
						ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
							if kitToggleChip(c, "diff-exp-res-hd", a.t("diff_export.res_hd_first"), "", a.diffExportPreferHR1, nil) && !isRunning {
								a.diffExportPreferHR1 = true
							}
							if kitToggleChip(c, "diff-exp-res-std", a.t("diff_export.res_std_only"), "", !a.diffExportPreferHR1, nil) && !isRunning {
								a.diffExportPreferHR1 = false
							}
						})
					})

					// 命名规则与实时示例
					kitBlock(c, ui.Column(c).FillWidth().Padding(10, 12).Gap(8), toneCard).Children(func() {
						ui.Text(c, a.t("diff_export.naming_label")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
						if a.diffEntity != "icon" {
							idNameLbl := a.t("diff_export.naming_id_name")
							if a.diffEntity == "item" {
								idNameLbl = a.t("diff_export.naming_item_id_name")
							}
							ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
								if kitToggleChip(c, "diff-exp-name-idname", idNameLbl, "", a.diffExportIconNaming == exporter.DiffIconNamingEntityIDName, nil) && !isRunning {
									a.diffExportIconNaming = exporter.DiffIconNamingEntityIDName
								}
								if kitToggleChip(c, "diff-exp-name-iconid", a.t("diff_export.naming_icon_id"), "", a.diffExportIconNaming == exporter.DiffIconNamingIconID, nil) && !isRunning {
									a.diffExportIconNaming = exporter.DiffIconNamingIconID
								}
							})
						}
						numText(c, a.tf("diff_export.naming_preview", "example", a.diffExportIconExampleName())).
							FontSize(t.text.xs).TextColor(t.foreground)

						if a.diffEntity == "icon" && (a.diffFilter == "all" || a.diffFilter == "modified") {
							if kitToggleChip(c, "diff-exp-old-icons", a.t("diff_export.include_old_icons"), "", a.diffExportIncludeOldIcon, nil) && !isRunning {
								a.diffExportIncludeOldIcon = !a.diffExportIncludeOldIcon
							}
						}
					})

				case exporter.DiffExportAudio:
					ui.Column(c).FillWidth().Gap(6).Children(func() {
						ui.Text(c, a.t("diff_export.audio_format_label")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
						ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
							if kitToggleChip(c, "diff-exp-aud-ogg", a.t("diff_export.audio_ogg"), "", a.diffExportAudioFormat != exporter.DiffAudioSCD, nil) && !isRunning {
								a.diffExportAudioFormat = exporter.DiffAudioOGG
							}
							if kitToggleChip(c, "diff-exp-aud-scd", a.t("diff_export.audio_scd"), "", a.diffExportAudioFormat == exporter.DiffAudioSCD, nil) && !isRunning {
								a.diffExportAudioFormat = exporter.DiffAudioSCD
							}
						})
					})
					kitBlock(c, ui.Column(c).FillWidth().Padding(10, 12).Gap(4), toneCard).Children(func() {
						numText(c, a.tf("diff_export.audio_naming_hint", "example", a.diffExportAudioExampleName())).
							FontSize(t.text.xs).TextColor(t.muted)
					})

				case exporter.DiffExportCSV:
					kitBlock(c, ui.Column(c).FillWidth().Padding(12).Gap(4), toneCard).Children(func() {
						numText(c, a.tf("diff_export.report_csv_desc", "count", formatInt(totalFiltered))).
							FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.subdued)
					})
				case exporter.DiffExportJSON:
					kitBlock(c, ui.Column(c).FillWidth().Padding(12).Gap(4), toneCard).Children(func() {
						numText(c, a.tf("diff_export.report_json_desc", "count", formatInt(totalFiltered))).
							FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.subdued)
					})
				case exporter.DiffExportMarkdown:
					kitBlock(c, ui.Column(c).FillWidth().Padding(12).Gap(4), toneCard).Children(func() {
						numText(c, a.tf("diff_export.report_md_desc", "count", formatInt(totalFiltered))).
							FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.subdued)
					})
				}

				// 批量资源导出时展示本地输出目录选择
				if isBatchKind {
					ui.Column(c).FillWidth().Gap(6).Children(func() {
						ui.Text(c, a.t("diff_export.output_dir")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Grow(1).Shrink(1).Children(func() {
								kitInput(c, "diff-exp-dir-input", &a.diffExportDirDraft, kitInputOpts{
									placeholder: a.t("diff_export.output_placeholder"),
									label:       a.t("diff_export.output_dir"),
									size:        btnSizeMd,
									disabled:    isRunning,
									clearable:   true,
								})
							})
							if kitButton(c, "diff-exp-dir-browse", a.t("common.browse_short"), btnOpts{
								variant:  btnOutline,
								size:     btnSizeMd,
								icon:     icoFolder,
								disabled: isRunning,
							}) {
								cur := a.diffExportDirDraft
								a.pickDirectoryAsync(a.t("diff_export.dialog_dir_title"), cur, func(picked string) {
									a.diffExportDirDraft = picked
									a.persistDiffExportDir(picked)
								})
							}
						})
						ui.Text(c, a.t("diff_export.dir_hint")).FontSize(t.text.micro).TextColor(t.muted)
					})
				}

				if a.diffExportError != "" {
					kitAlert(c, toneDanger, a.diffExportError)
				}
			})
		})

		// 3. 实时导出进度条（固定于底栏上方，防止被滚动折叠）
		if a.diffExportProgress != nil {
			p := a.diffExportProgress
			ui.Column(c).FillWidth().Padding(10, 20).Gap(6).Background(t.card).
				BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
				ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
					statusTxt := a.t("diff_export.status_idle")
					statusCol := t.muted
					switch {
					case p.IsRunning:
						statusTxt = a.t("diff_export.status_running")
						statusCol = t.primary
					case p.IsCancelled:
						statusTxt = a.t("diff_export.status_cancelled")
						statusCol = t.warningText
					case p.IsCompleted:
						statusTxt = a.tf("diff_export.status_done", "path", p.OutputPath)
						statusCol = t.successText
					case p.ErrorMessage != "":
						statusTxt = p.ErrorMessage
						statusCol = t.dangerText
					}
					ui.Text(c, statusTxt).FontSize(t.text.xs).FontWeight(600).TextColor(statusCol).SingleLine().Grow(1).Shrink(1)
					ui.Row(c).Gap(8).AlignItems(ui.Baseline).Shrink(0).Children(func() {
						if p.TotalTasks > 0 {
							numText(c, a.tf("diff_export.progress_count",
								"done", formatInt(p.ProcessedTasks),
								"total", formatInt(p.TotalTasks),
							)).FontSize(t.text.xs).TextColor(t.subdued)
						}
						numTextf(c, "%d%%", p.Percent).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground)
					})
				})

				pct := min(max(p.Percent, 0), 100)
				done := float32(pct)
				if p.IsRunning && done < 2 {
					done = 2
				}
				ui.Row(c).FillWidth().Height(5).Radius(t.radius.full).Background(t.border).Clip().
					AlignItems(ui.Stretch).Children(func() {
					ui.Box(c).Grow(done).FillHeight().Background(t.primary)
					ui.Box(c).Grow(max(0, 100-done)).FillHeight()
				})

				if p.IsRunning && p.CurrentFile != "" {
					ui.Text(c, p.CurrentFile).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
				}
				if p.IsCompleted || p.IsCancelled {
					if p.SkippedCount > 0 || p.ErrorCount > 0 {
						numText(c, a.tf("diff_export.progress_summary",
							"ok", formatInt(p.SuccessCount),
							"skipped", formatInt(p.SkippedCount),
							"err", formatInt(p.ErrorCount),
						)).FontSize(t.text.micro).TextColor(t.muted)
					}
				}
			})
		}

		// 4. 底部操作条
		ui.Row(c).FillWidth().Padding(12, 20).Background(t.sidebar).
			BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			if isRunning {
				if kitButton(c, "diff-exp-cancel-btn", a.t("diff_export.cancel_task"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					if a.diffExporterSvc != nil {
						a.diffExporterSvc.Cancel()
					}
				}
			} else {
				if kitButton(c, "diff-exp-close-bottom", a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
					a.diffExportOpen = false
				}
			}

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				canExport := !isRunning && totalFiltered > 0 && a.diffExporterSvc != nil
				if isBatchKind {
					if kitButton(c, "diff-exp-zip-btn", a.t("diff_export.zip_btn"), btnOpts{
						variant:  btnOutline,
						size:     btnSizeSm,
						disabled: !canExport,
					}) {
						a.startDiffZipBatchExportFlow()
					}
					canLocal := canExport && strings.TrimSpace(a.diffExportDirDraft) != ""
					if kitButton(c, "diff-exp-local-btn", a.t("diff_export.local_btn"), btnOpts{
						variant:  btnDefault,
						size:     btnSizeSm,
						icon:     icoDownload,
						disabled: !canLocal,
					}) {
						a.startDiffLocalBatchExportFlow()
					}
				} else {
					if kitButton(c, "diff-exp-save-btn", a.t("diff_export.save_file_btn"), btnOpts{
						variant:  btnDefault,
						size:     btnSizeSm,
						icon:     icoDownload,
						disabled: !canExport,
					}) {
						a.startDiffSingleFileExportFlow()
					}
				}
			})
		})
	})

	if isRunning && !a.diffExportOpen {
		a.diffExportOpen = true
	}
}
