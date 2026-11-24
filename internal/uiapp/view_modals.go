package uiapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/exporter"
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
				numTextf(c, "%s · 坐标 (%.1f, %.1f) · %s", m.Title, m.CoordX, m.CoordY, m.Subtitle).
					FontSize(t.text.xs).TextColor(t.muted).SingleLine()
			})
			if kitIconButton(c, "map-close-btn", icoClose, "关闭", btnOpts{variant: btnGhost, size: btnSizeSm}) {
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
							ui.Spinner(c).FontSize(20).TextColor(t.primary).Label("加载地图")
							ui.Text(c, "正在读取游戏地图...").FontSize(t.text.xs).TextColor(t.muted)
						} else {
							ui.Text(c, "该区域暂无可用地图").FontSize(t.text.sm).TextColor(t.muted)
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
				ui.Text(c, "游戏全量 CSV 解包与本地导出").FontSize(t.text.base).FontWeight(600).TextColor(t.foreground)
				numTextf(c, "共 %s 张数据表 · 将导出 %s 个 CSV 文件", formatInt(totalSheets), formatInt(totalTasks)).
					FontSize(t.text.xs).TextColor(t.muted)
			})
			if kitIconButton(c, "exp-close-top", icoClose, "关闭", btnOpts{variant: btnGhost, size: btnSizeSm, disabled: isRunning}) {
				a.exportOpen = false
			}
		})

		// 主体内容（限高可滚动，保证在 640px 窗口高度下底部操作条始终可见）
		ui.Scroll(c.Key("exp-body-scroll")).FillWidth().MaxHeight(440).Children(func() {
			ui.Column(c).FillWidth().Padding(4, 20, 16, 20).Gap(16).Children(func() {
				// 语言选择网格
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					ui.Text(c, "导出语言").FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
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
									srv := lang.Server
									if !avail {
										srv = "未配置"
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
					ui.Text(c, "本地输出目录").FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground)
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Grow(1).Shrink(1).Children(func() {
							kitInput(c, "exp-dir-input", &a.exportDirDraft, kitInputOpts{
								placeholder: "选择或输入本地目录，如 D:\\FFXIV-CSV-Export",
								label:       "本地输出目录",
								size:        btnSizeMd,
								disabled:    isRunning,
								clearable:   true,
							})
						})
						if kitButton(c, "exp-dir-browse", "浏览...", btnOpts{variant: btnOutline, size: btnSizeMd, icon: icoFolder, disabled: isRunning}) {
							cur := a.exportDirDraft
							a.pickDirectoryAsync("选择 CSV 输出目录", cur, func(picked string) {
								a.exportDirDraft = picked
								a.persistExportDir(picked)
							})
						}
					})
					ui.Text(c, "解包到本地目录时，会先清空目标目录下对应的旧语言子目录（如 chs、ja）再重新生成。").
						FontSize(t.text.micro).TextColor(t.muted)
				})

				// 实时进度面板
				if a.exportProgress != nil {
					p := a.exportProgress
					kitBlock(c, ui.Column(c).FillWidth().Padding(14).Gap(8), toneCard).Children(func() {
						ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
							statusTxt := "等待开始"
							statusCol := t.muted
							switch {
							case p.IsRunning:
								statusTxt = fmt.Sprintf("正在导出 %s/%s...", p.CurrentLang, p.CurrentSheet)
								statusCol = t.foreground
							case p.IsCompleted:
								statusTxt = fmt.Sprintf("导出完成：%s", p.OutputPath)
								statusCol = t.successText
							case p.IsCancelled:
								statusTxt = "已取消导出"
								statusCol = t.warningText
							case p.ErrorMessage != "":
								statusTxt = "导出失败：" + p.ErrorMessage
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
							numTextf(c, "已处理 %s / %s 个文件", formatInt(p.ProcessedTasks), formatInt(p.TotalTasks)).FontSize(t.text.micro).TextColor(t.muted)
							if p.ErrorCount > 0 {
								numTextf(c, "跳过 %d 张表", p.ErrorCount).FontSize(t.text.micro).TextColor(t.dangerText)
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
				if kitButton(c, "exp-cancel-btn", "取消任务", btnOpts{variant: btnOutline, size: btnSizeSm}) {
					if a.exporterSvc != nil {
						a.exporterSvc.Cancel()
					}
				}
			} else {
				if kitButton(c, "exp-close-bottom", "关闭", btnOpts{variant: btnGhost, size: btnSizeSm}) {
					a.exportOpen = false
				}
			}

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if kitButton(c, "exp-zip-btn", "导出为 Zip...", btnOpts{variant: btnOutline, size: btnSizeSm, disabled: isRunning || len(selectedCodes) == 0}) {
					a.startSaveZipFlow(selectedCodes)
				}
				canLocal := !isRunning && len(selectedCodes) > 0 && strings.TrimSpace(a.exportDirDraft) != ""
				if kitButton(c, "exp-local-btn", "解包到本地目录", btnOpts{variant: btnDefault, size: btnSizeSm, disabled: !canLocal}) {
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
		a.exportError = "请先选择或输入有效的本地输出目录"
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
			Title:       "保存全量 CSV Zip 压缩包",
			DefaultPath: defaultName,
			Filters:     []mygo.FileFilter{{Name: "Zip 压缩包", Extensions: []string{"zip"}}},
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
