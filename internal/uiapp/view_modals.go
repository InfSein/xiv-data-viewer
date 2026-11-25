package uiapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/exporter"
	"github.com/InfSein/xiv-data-viewer/internal/updater"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// renderMapModal 渲染原生游戏贴图坐标预览弹窗（含边界钳制，防止贴边标点被裁剪）。
func (a *App) renderMapModal(c *ui.Context) {
	if !a.mapOpen || a.mapMarker == nil {
		return
	}
	t := themeOf(c)
	m := a.mapMarker
	ui.DialogBase(c.Key("map-modal"), &a.mapOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(t.scrim)
		applyOverlaySurface(c, panel.Width(580).Radius(t.radius.x2l).Clip(), shadowDialog)

		// 标题栏
		ui.Row(c).FillWidth().Padding(14, 18).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Start).Gap(12).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				area := fmt.Sprintf("%s · %s", m.RegionName, m.ZoneName)
				if m.PlaceName != "" && m.PlaceName != m.ZoneName {
					area += fmt.Sprintf("（%s）", m.PlaceName)
				}
				ui.Text(c, area).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				numText(c, a.tf("map.subtitle_coords",
					"title", m.Title,
					"x", fmt.Sprintf("%.1f", m.CoordX),
					"y", fmt.Sprintf("%.1f", m.CoordY),
					"sub", m.Subtitle,
				)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
			})
			if kitIconButton(c, "map-close-btn", icoClose, a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
				a.mapOpen = false
			}
		})

		// 地图贴图与坐标定位标记（520×520，适配最小 640px 窗口高度）
		const mapSize float32 = 520
		ui.Box(c).FillWidth().Padding(16).Center().Background(t.card).Children(func() {
			ui.Box(c).Size(mapSize, mapSize).Radius(t.radius.lg).Background(t.background).Border(hairline, t.border).Clip().Children(func() {
				bmp, loading := a.bitmaps.GetMap(a.service, a.win, m.MapPath)
				if bmp != nil {
					ui.Image(c, bmp).Size(mapSize, mapSize).Fit(ui.Cover)
				} else {
					ui.Column(c).Fill().Center().Gap(8).Children(func() {
						if loading {
							ui.Spinner(c).FontSize(20).TextColor(t.primary).Label(a.t("map.loading"))
							ui.Text(c, a.t("map.loading_desc")).FontSize(t.text.xs).TextColor(t.muted)
						} else {
							ui.Text(c, a.t("map.unavailable")).FontSize(t.text.sm).TextColor(t.muted)
						}
					})
				}

				// 钳制图钉与坐标标签位置，避免贴近边缘时被裁切出画布
				cx := min(max(float32(m.NormX)*mapSize, 16), mapSize-16)
				cy := min(max(float32(m.NormY)*mapSize, 16), mapSize-16)
				ui.Box(c).Absolute().Left(cx-13).Top(cy-13).Size(26, 26).Radius(13).
					Background(t.background).Border(2, t.primary).Center().Children(func() {
					ui.Icon(c, icoMapPin).Size(14, 14).TextColor(t.danger)
				})

				const labelW float32 = 88
				lblX := min(max(cx-labelW/2, 8), mapSize-labelW-8)
				lblY := cy + 17
				if cy > mapSize-48 {
					lblY = cy - 39
				}
				ui.Box(c).Absolute().Left(lblX).Top(lblY).Width(labelW).Padding(2, 6).Radius(t.radius.sm).
					Background(t.foreground).Center().Children(func() {
					numTextf(c, "%.1f, %.1f", m.CoordX, m.CoordY).FontSize(t.text.micro).FontWeight(600).TextColor(t.background).SingleLine()
				})
			})
		})
	})
}

// renderExportModal 渲染全量 CSV 导出弹窗（内置滚动保护，适配最小 640px 窗口高度）。
func (a *App) renderExportModal(c *ui.Context) {
	if !a.exportOpen {
		return
	}
	t := themeOf(c)
	isRunning := a.exportProgress != nil && a.exportProgress.IsRunning
	selectedCodes := a.selectedExportLangCodes()
	totalSheets := a.exportTotalShts
	if totalSheets <= 0 {
		totalSheets = 7900
	}
	totalTasks := totalSheets * len(selectedCodes)

	ui.DialogBase(c.Key("export-modal"), &a.exportOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(t.scrim)
		applyOverlaySurface(c, panel.Width(600).Radius(t.radius.x2l).Clip(), shadowDialog)

		// 标题栏
		ui.Row(c).FillWidth().Padding(16, 20, 12, 20).
			Justify(ui.SpaceBetween).AlignItems(ui.Start).Gap(12).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
				ui.Text(c, a.t("export.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground)
				numText(c, a.tf("export.subtitle", "sheets", formatInt(totalSheets), "tasks", formatInt(totalTasks))).
					FontSize(t.text.xs).TextColor(t.muted)
			})
			if kitIconButton(c, "exp-close-top", icoClose, a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm, disabled: isRunning}) {
				a.exportOpen = false
			}
		})

		// 主体内容（限高可滚动，保证在 640px 窗口高度下底部操作条始终可见）
		ui.Scroll(c.Key("exp-body-scroll")).FillWidth().MaxHeight(440).Children(func() {
			ui.Column(c).FillWidth().Padding(4, 20, 16, 20).Gap(16).Children(func() {
				// 语言选择网格
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					ui.Text(c, a.t("export.langs_label")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
					ui.Grid(c).FillWidth().Columns(5).Gap(8).Children(func() {
						for _, lang := range exporter.AllSupportedLangs {
							avail := a.exporterSvc != nil && a.exporterSvc.IsLangAvailable(lang.Code)
							selected := avail && a.exportLangs[lang.Code]
							btn := ui.ButtonBase(c.Key("exp-lang-"+lang.Code)).Label(lang.Label).Padding(8, 6).Radius(t.radius.lg).
								Disabled(!avail || isRunning).DimDisabled(false).
								Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
							switch {
							case !avail:
								btn.Background(t.card).Border(hairline, t.border).Opacity(0.55)
							case selected:
								btn.Background(t.accentSoft).Border(strokeWidth, t.primary)
							case btn.Hovered():
								btn.Background(t.hover).Border(hairline, t.border)
							default:
								btn.Background(t.background).Border(hairline, t.border)
							}
							btn.Children(func() {
								ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(2).Children(func() {
									fg := t.subdued
									if selected {
										fg = t.infoText
									}
									ui.Text(c, lang.Code+"/").FontSize(t.text.xs).FontWeight(600).TextColor(fg)
									ui.Text(c, lang.Label).FontSize(t.text.xs).TextColor(t.foreground)
									srv := lang.ServerLabel(a.Language())
									if !avail {
										srv = a.t("export.server_unset")
									}
									ui.Text(c, srv).FontSize(t.text.micro).TextColor(t.muted)
								})
							})
							if avail && !isRunning && btn.Clicked() {
								if !(selected && len(selectedCodes) <= 1) {
									a.exportLangs[lang.Code] = !selected
								}
							}
						}
					})
				})

				// 本地解包输出目录
				kitBlock(c, ui.Column(c).FillWidth().Padding(14).Gap(8), toneCard).Children(func() {
					ui.Text(c, a.t("export.output_dir")).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground)
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Grow(1).Shrink(1).Children(func() {
							kitInput(c, "exp-dir-input", &a.exportDirDraft, kitInputOpts{
								placeholder: a.t("export.output_placeholder"),
								label:       a.t("export.output_dir"),
								size:        btnSizeMd,
								disabled:    isRunning,
								clearable:   true,
							})
						})
						if kitButton(c, "exp-dir-browse", a.t("common.browse_short"), btnOpts{variant: btnOutline, size: btnSizeMd, icon: icoFolder, disabled: isRunning}) {
							cur := a.exportDirDraft
							a.pickDirectoryAsync(a.t("export.dialog_dir_title"), cur, func(picked string) {
								a.exportDirDraft = picked
								a.persistExportDir(picked)
							})
						}
					})
					ui.Text(c, a.t("export.dir_hint")).
						FontSize(t.text.micro).TextColor(t.muted)
				})

				// 实时进度面板
				if a.exportProgress != nil {
					p := a.exportProgress
					kitBlock(c, ui.Column(c).FillWidth().Padding(14).Gap(8), toneCard).Children(func() {
						ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
							statusTxt := a.t("export.status_idle")
							statusCol := t.muted
							switch {
							case p.IsRunning:
								statusTxt = a.tf("export.status_running", "lang", p.CurrentLang, "sheet", p.CurrentSheet)
								statusCol = t.foreground
							case p.IsCompleted:
								statusTxt = a.tf("export.status_done", "path", p.OutputPath)
								statusCol = t.successText
							case p.IsCancelled:
								statusTxt = a.t("export.status_cancelled")
								statusCol = t.warningText
							case p.ErrorMessage != "":
								statusTxt = a.tf("export.status_failed", "err", p.ErrorMessage)
								statusCol = t.dangerText
							}
							ui.Text(c, statusTxt).FontSize(t.text.xs).FontWeight(500).TextColor(statusCol).SingleLine().Grow(1).Shrink(1)
							numTextf(c, "%d%%", p.Percent).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).Shrink(0)
						})
						done := float32(p.Percent)
						ui.Row(c).FillWidth().Height(4).Radius(t.radius.full).Background(t.border).Clip().Children(func() {
							ui.Box(c).Grow(done).Background(t.primary)
							ui.Box(c).Grow(max(0, 100-done))
						})
						ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
							numText(c, a.tf("export.processed", "done", formatInt(p.ProcessedTasks), "total", formatInt(p.TotalTasks))).FontSize(t.text.micro).TextColor(t.muted)
							if p.ErrorCount > 0 {
								numText(c, a.tf("export.skipped", "count", fmt.Sprintf("%d", p.ErrorCount))).FontSize(t.text.micro).TextColor(t.dangerText)
							}
						})
					})
				}

				if a.exportError != "" {
					kitAlert(c, toneDanger, a.exportError)
				}
			})
		})

		// 底部操作条
		ui.Row(c).FillWidth().Padding(12, 20).Background(t.sidebar).
			BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			if isRunning {
				if kitButton(c, "exp-cancel-btn", a.t("export.cancel_task"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					if a.exporterSvc != nil {
						a.exporterSvc.Cancel()
					}
				}
			} else {
				if kitButton(c, "exp-close-bottom", a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
					a.exportOpen = false
				}
			}

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if kitButton(c, "exp-zip-btn", a.t("export.zip_btn"), btnOpts{variant: btnOutline, size: btnSizeSm, disabled: isRunning || len(selectedCodes) == 0}) {
					a.startSaveZipFlow(selectedCodes)
				}
				canLocal := !isRunning && len(selectedCodes) > 0 && strings.TrimSpace(a.exportDirDraft) != ""
				if kitButton(c, "exp-local-btn", a.t("export.local_btn"), btnOpts{variant: btnDefault, size: btnSizeSm, disabled: !canLocal}) {
					a.startLocalUnpackFlow(selectedCodes)
				}
			})
		})
	})

	if isRunning && !a.exportOpen {
		a.exportOpen = true
	}
}

func (a *App) selectedExportLangCodes() []string {
	var out []string
	for _, lang := range exporter.AllSupportedLangs {
		if a.exportLangs[lang.Code] && a.exporterSvc != nil && a.exporterSvc.IsLangAvailable(lang.Code) {
			out = append(out, lang.Code)
		}
	}
	return out
}

func (a *App) persistExportDir(dir string) {
	a.cfg.ExportOutputDir = strings.TrimSpace(dir)
	_ = config.SaveAppConfig(a.dataDir, a.cfg)
}

func (a *App) startLocalUnpackFlow(langs []string) {
	if a.exporterSvc == nil {
		return
	}
	dir := strings.TrimSpace(a.exportDirDraft)
	if dir == "" {
		a.exportError = a.t("export.err_no_dir")
		return
	}
	a.exportError = ""
	a.persistExportDir(dir)

	onUpdate := func(p exporter.ExportProgress) {
		cp := p
		if a.win != nil {
			a.win.Update(func() {
				a.exportProgress = &cp
			})
		} else {
			a.exportProgress = &cp
		}
	}

	if err := a.exporterSvc.StartUnpackToLocalDirectory(dir, langs, nil, onUpdate); err != nil {
		a.exportError = err.Error()
	}
}

func (a *App) startSaveZipFlow(langs []string) {
	if a.exporterSvc == nil || a.win == nil {
		return
	}
	a.exportError = ""
	defaultName := fmt.Sprintf("ffxiv-csv-all-%s.zip", time.Now().Format("20060102-150405"))
	langsCopy := append([]string(nil), langs...)
	go func() {
		savePath, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      a.win,
			Title:       a.t("export.zip_dialog_title"),
			DefaultPath: defaultName,
			Filters:     []mygo.FileFilter{{Name: a.t("export.zip_filter_name"), Extensions: []string{"zip"}}},
		})
		if err != nil || strings.TrimSpace(savePath) == "" {
			return
		}
		a.win.Update(func() {
			onUpdate := func(p exporter.ExportProgress) {
				cp := p
				a.win.Update(func() {
					a.exportProgress = &cp
				})
			}
			if err := a.exporterSvc.StartExportToZipFile(savePath, langsCopy, nil, onUpdate); err != nil {
				a.exportError = err.Error()
			}
		})
	}()
}

// renderUpdateModal 渲染用户主动检查更新、下载进度与重启提醒弹窗。
func (a *App) renderUpdateModal(c *ui.Context) {
	if !a.updateModalOpen {
		return
	}
	t := themeOf(c)
	isDownloading := a.updateState == updateDownloading
	curVer := strings.TrimPrefix(strings.TrimSpace(a.appVersion), "v")
	if curVer == "" {
		curVer = "0.0.0"
	}

	canSelfUpdate := false
	var disableReason updater.SelfUpdateDisableReason
	if a.updaterClient != nil {
		canSelfUpdate, disableReason = a.updaterClient.CanSelfUpdate()
	}

	openDialog := true
	ui.DialogBase(c.Key("update-modal"), &openDialog, func(backdrop, panel ui.Element) {
		backdrop.Background(t.scrim)
		applyOverlaySurface(c, panel.Width(500).Radius(t.radius.x2l).Clip(), shadowDialog)

		// 标题栏
		ui.Row(c).FillWidth().Padding(16, 20, 12, 20).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Start).Gap(12).Children(func() {
			ui.Column(c).Gap(3).Grow(1).Shrink(1).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.t("update.modal_title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground)
					switch a.updateState {
					case updateAvailable:
						drawPill(c, a.t("update.available_badge"), pillAccent)
					case updateReady:
						drawPill(c, a.t("update.ready_badge"), pillPositive)
					}
				})
				numText(c, a.tf("update.current_version", "version", curVer)).FontSize(t.text.xs).TextColor(t.muted)
			})
			if kitIconButton(c, "upd-close-top", icoClose, a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm, disabled: isDownloading}) {
				a.closeUpdateModal()
			}
		})

		// 主体内容
		ui.Scroll(c.Key("upd-body-scroll")).FillWidth().MaxHeight(360).Children(func() {
			ui.Column(c).FillWidth().Padding(16, 20).Gap(12).Children(func() {
				switch a.updateState {
				case updateChecking:
					kitBlock(c, ui.Row(c).FillWidth().Padding(16).Gap(12).AlignItems(ui.Center), toneCard).Children(func() {
						ui.Spinner(c).FontSize(18).TextColor(t.primary).Label(a.t("update.checking"))
						ui.Column(c).Gap(3).Grow(1).Shrink(1).Children(func() {
							ui.Text(c, a.t("update.checking")).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
							ui.Text(c, a.t("update.checking_hint")).FontSize(t.text.xs).TextColor(t.muted)
						})
					})

				case updateUpToDate:
					kitBlock(c, ui.Row(c).FillWidth().Padding(14).Gap(10).AlignItems(ui.Start), toneSuccess).Children(func() {
						ui.Icon(c, icoCheck).Size(iconMd, iconMd).Margin(2, 0, 0, 0).Shrink(0)
						ui.Column(c).Gap(3).Grow(1).Shrink(1).Children(func() {
							ui.Text(c, a.t("update.up_to_date_title")).FontSize(t.text.sm).FontWeight(600)
							numText(c, a.tf("update.up_to_date_desc", "version", curVer)).FontSize(t.text.xs)
						})
					})
					if a.updateRelease != nil && a.updateRelease.Version != "" {
						kitBlock(c, ui.Row(c).FillWidth().Padding(10, 12).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8), toneCard).Children(func() {
							numText(c, a.tf("update.latest_tag_meta", "version", a.updateRelease.Version)).FontSize(t.text.xs).TextColor(t.subdued)
							if !a.updateRelease.PublishedAt.IsZero() {
								numText(c, a.tf("update.published_at", "date", a.updateRelease.PublishedAt.Local().Format("2006-01-02"))).FontSize(t.text.xs).TextColor(t.muted)
							}
						})
					}

				case updateAvailable:
					if rel := a.updateRelease; rel != nil {
						kitBlock(c, ui.Column(c).FillWidth().Padding(14).Gap(6), toneCard).Children(func() {
							ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
								ui.Text(c, a.tf("update.available_title", "version", rel.Version)).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground).Grow(1).Shrink(1)
								numText(c, a.tf("update.version_arrow", "from", curVer, "to", rel.Version)).FontSize(t.text.xs).FontWeight(500).TextColor(t.infoText).Shrink(0)
							})
							var metaParts []string
							if rel.AssetSize > 0 {
								metaParts = append(metaParts, a.tf("update.package_size", "size", formatByteSize(rel.AssetSize)))
							}
							if !rel.PublishedAt.IsZero() {
								metaParts = append(metaParts, a.tf("update.published_at", "date", rel.PublishedAt.Local().Format("2006-01-02")))
							}
							if len(metaParts) > 0 {
								numText(c, strings.Join(metaParts, " · ")).FontSize(t.text.xs).TextColor(t.muted)
							}
						})

						notes := updater.FormatReleaseNotes(rel.Notes)
						if notes == "" {
							notes = a.t("update.notes_empty")
						}
						kitBlock(c, ui.Column(c).FillWidth().Padding(12, 14).Gap(6), toneOutline).Children(func() {
							ui.Text(c, a.t("update.release_notes")).FontSize(t.text.xs).FontWeight(600).TextColor(t.subdued)
							ui.Scroll(c.Key("upd-notes-scroll")).FillWidth().MaxHeight(168).Children(func() {
								ui.Text(c, notes).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.foreground).Selectable()
							})
						})

						if !canSelfUpdate {
							var msg string
							switch disableReason {
							case updater.DisableReasonDev:
								msg = a.t("update.disabled_dev")
							case updater.DisableReasonPlatform:
								msg = a.t("update.disabled_platform")
							default:
								msg = a.t("update.disabled_permission")
							}
							kitAlert(c, toneWarning, msg)
						}
					}

				case updateDownloading:
					targetVer := ""
					if a.updateRelease != nil {
						targetVer = a.updateRelease.Version
					}
					phaseText := a.updatePhaseLabel()
					pct := 0
					if a.updateDownTotal > 0 {
						pct = int(min(100, (a.updateDownBytes*100)/a.updateDownTotal))
					} else if a.updatePhase == updater.PhaseExtracting {
						pct = 88
					} else if a.updatePhase == updater.PhaseReplacing {
						pct = 96
					}

					kitBlock(c, ui.Column(c).FillWidth().Padding(14).Gap(10), toneCard).Children(func() {
						ui.Text(c, a.tf("update.downloading_title", "version", targetVer)).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
						ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, phaseText).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued).SingleLine().Grow(1).Shrink(1)
							numTextf(c, "%d%%", pct).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).Shrink(0)
						})
						done := float32(max(4, pct))
						ui.Row(c).FillWidth().Height(4).Radius(t.radius.full).Background(t.border).Clip().Children(func() {
							ui.Box(c).Grow(done).Background(t.primary)
							ui.Box(c).Grow(max(0, 100-done))
						})
						if a.updateDownTotal > 0 {
							numText(c, a.tf("update.download_progress_bytes",
								"done", formatByteSize(a.updateDownBytes),
								"total", formatByteSize(a.updateDownTotal),
							)).FontSize(t.text.micro).TextColor(t.muted)
						}
					})

				case updateReady:
					kitBlock(c, ui.Row(c).FillWidth().Padding(14).Gap(10).AlignItems(ui.Start), toneSuccess).Children(func() {
						ui.Icon(c, icoCheck).Size(iconMd, iconMd).Margin(2, 0, 0, 0).Shrink(0)
						ui.Column(c).Gap(3).Grow(1).Shrink(1).Children(func() {
							ui.Text(c, a.tf("update.ready_title", "version", a.updateReadyVer)).FontSize(t.text.sm).FontWeight(600)
							ui.Text(c, a.t("update.ready_desc")).FontSize(t.text.xs).LineHeight(lineProse)
						})
					})
					if a.updateRelease != nil && strings.TrimSpace(a.updateRelease.Notes) != "" {
						kitBlock(c, ui.Column(c).FillWidth().Padding(12, 14).Gap(6), toneOutline).Children(func() {
							ui.Text(c, a.t("update.release_notes")).FontSize(t.text.xs).FontWeight(600).TextColor(t.subdued)
							ui.Scroll(c.Key("upd-ready-notes-scroll")).FillWidth().MaxHeight(148).Children(func() {
								ui.Text(c, updater.FormatReleaseNotes(a.updateRelease.Notes)).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.foreground).Selectable()
							})
						})
					}

				case updateError:
					kitAlert(c, toneDanger, a.t("update.error_title")+" — "+a.t("update.error_desc"))
					if strings.TrimSpace(a.updateErrorMsg) != "" {
						kitBlock(c, ui.Column(c).FillWidth().Padding(12).Gap(4), toneCard).Children(func() {
							ui.Text(c, a.tf("update.error_detail", "err", a.updateErrorMsg)).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.muted).Selectable()
						})
					}
				}
			})
		})

		// 底部操作条
		ui.Row(c).FillWidth().Padding(12, 20).Background(t.sidebar).
			BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
			if a.updateState == updateAvailable || a.updateState == updateUpToDate || a.updateState == updateError {
				if kitButton(c, "upd-open-rel", a.t("update.btn_open_release"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					c.OpenURL(a.releasePageURL())
				}
			} else {
				ui.Box(c)
			}

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				switch a.updateState {
				case updateChecking:
					if kitButton(c, "upd-cancel-chk", a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
						a.closeUpdateModal()
					}
				case updateUpToDate:
					if kitButton(c, "upd-recheck", a.t("update.btn_check_again"), btnOpts{variant: btnOutline, size: btnSizeSm, icon: icoRefresh}) {
						a.startManualUpdateCheck()
					}
					if kitButton(c, "upd-ok-latest", a.t("common.close"), btnOpts{variant: btnDefault, size: btnSizeSm}) {
						a.closeUpdateModal()
					}
				case updateAvailable:
					if canSelfUpdate {
						if kitButton(c, "upd-later", a.t("update.btn_later"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.closeUpdateModal()
						}
						if kitButton(c, "upd-install", a.t("update.btn_download_install"), btnOpts{variant: btnDefault, size: btnSizeSm, icon: icoDownload}) {
							a.startManualUpdateInstall()
						}
					} else {
						if kitButton(c, "upd-close-disabled", a.t("common.close"), btnOpts{variant: btnDefault, size: btnSizeSm}) {
							a.closeUpdateModal()
						}
					}
				case updateDownloading:
					// 下载与文件替换中禁止误关弹窗
				case updateReady:
					if kitButton(c, "upd-restart-later", a.t("update.btn_restart_later"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
						a.closeUpdateModal()
					}
					if kitButton(c, "upd-restart-now", a.t("update.btn_restart_now"), btnOpts{variant: btnDefault, size: btnSizeSm, icon: icoRefresh}) {
						a.restartForUpdate()
					}
				case updateError:
					if kitButton(c, "upd-err-close", a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
						a.closeUpdateModal()
					}
					if kitButton(c, "upd-retry", a.t("update.btn_retry"), btnOpts{variant: btnDefault, size: btnSizeSm, icon: icoRefresh}) {
						a.startManualUpdateCheck()
					}
				}
			})
		})
	})

	if !openDialog && !isDownloading {
		a.closeUpdateModal()
	}
}

func (a *App) updatePhaseLabel() string {
	switch a.updatePhase {
	case updater.PhaseVerifying:
		return a.t("update.phase_verifying")
	case updater.PhaseExtracting:
		return a.t("update.phase_extracting")
	case updater.PhaseReplacing:
		return a.t("update.phase_replacing")
	default:
		return a.t("update.phase_downloading")
	}
}

func formatByteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}
