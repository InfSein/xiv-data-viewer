package uiapp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/egoist/mygo/ui"
)

// renderSetupView 渲染初次启动引导页（左简明说明 + 右路径配置面板，去除营销话术与冗余重复）。
func (a *App) renderSetupView(c *ui.Context) {
	t := themeOf(c)
	a.renderBareTitleBar(c, false)

	ui.Row(c).Grow(1).FillWidth().AlignItems(ui.Stretch).Gap(8).Padding(0, 8, 8, 8).Children(func() {
		// 左侧说明栏
		ui.Column(c).Width(368).Shrink(0).Padding(36, 28, 28, 28).Gap(26).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(34, 34).Radius(t.radius.lg).Background(t.background).Border(hairline, t.border).Center().Children(func() {
					ui.Icon(c, icoAppLogo).Size(19, 19).TextColor(t.primary)
				})
				ui.Column(c).Gap(1).Children(func() {
					ui.Text(c, "XIV Data Viewer").FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
					numText(c, "v"+a.appVersion).FontSize(t.text.micro).TextColor(t.muted)
				})
			})

			ui.Column(c).Gap(8).Children(func() {
				ui.Text(c, "欢迎使用艾欧泽亚数据工作台").FontSize(t.text.x3l).FontWeight(600).LetterSpacing(-0.3).TextColor(t.foreground)
				ui.Text(c, "本地直接查询 FF14 物品、配方、采集坐标、成就称号与版本变更。").
					FontSize(t.text.sm).LineHeight(lineProse).TextColor(t.muted)
			})

			ui.Column(c).Gap(14).Children(func() {
				drawOnboardingPoint(c, icoShield, "只读读取游戏目录", "仅解析本地 SqPack 数据，不改动任何游戏文件。")
				drawOnboardingPoint(c, icoLayers, "兼容单服与双服", "国服或国际服填写一项即可；两项同配可对照中英日名。")
				drawOnboardingPoint(c, icoLock, "完全离线本地运行", "配置与版本快照均保存在本机用户目录。")
			})

			ui.Spacer(c)
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, "配置保存位置").FontSize(t.text.micro).TextColor(t.muted)
				ui.Text(c, config.ConfigFilePath(a.dataDir)).FontSize(t.text.micro).TextColor(t.subdued).Selectable()
			})
		})

		// 右侧主配置面板
		kitPanel(c, a.panelRadius(c)).Grow(1).Shrink(1).Children(func() {
			ui.Scroll(c.Key("setup-scroll")).Grow(1).Children(func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(40, 32).Children(func() {
					ui.Column(c).FillWidth().MaxWidth(520).Gap(20).Children(func() {
						ui.Column(c).Gap(4).Children(func() {
							ui.Text(c, "选择本地客户端目录").FontSize(t.text.xl).FontWeight(600).TextColor(t.foreground)
							ui.Text(c, "请选择包含 game\\sqpack 的客户端根目录（或直接选 game 目录），国服与国际服至少填一项。").
								FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.muted)
						})

						drawSettingsSection(c, "", "",
							func() {
								ui.Column(c).FillWidth().Padding(14, 16).Children(func() {
									a.renderSinglePathInput(c, "setup-cn", "国服客户端 (CN)", "提供简体中文与国服数据", "例如：D:\\Final Fantasy\\CN\\最终幻想XIV", &a.draftCNPath, "选择 FF14 国服客户端目录")
								})
							},
							func() {
								ui.Column(c).FillWidth().Padding(14, 16).Children(func() {
									a.renderSinglePathInput(c, "setup-global", "国际服客户端 (Global)", "提供英/日/德/法多语言数据", "例如：D:\\Final Fantasy\\GLOBAL\\Game", &a.draftGlobalPath, "选择 FF14 国际服客户端目录")
								})
							},
						)

						if a.draftError != "" {
							kitAlert(c, toneDanger, a.draftError)
						}

						ui.Row(c).FillWidth().Justify(ui.End).Padding(4, 0, 0, 0).Children(func() {
							canSubmit := strings.TrimSpace(a.draftCNPath) != "" || strings.TrimSpace(a.draftGlobalPath) != ""
							if kitButton(c, "setup-start-btn", "验证并进入工作台", btnOpts{variant: btnDefault, size: btnSizeMd, disabled: !canSubmit}) {
								a.SaveAndReloadPaths(a.draftCNPath, a.draftGlobalPath)
							}
						})
					})
				})
			})
		})
	})
}

func drawOnboardingPoint(c *ui.Context, ico *ui.SVG, title, desc string) {
	t := themeOf(c)
	ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, ico).Size(iconMd, iconMd).Margin(2, 0, 0, 0).Shrink(0).TextColor(t.muted)
		ui.Column(c).Gap(2).Shrink(1).Children(func() {
			ui.Text(c, title).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
			ui.Text(c, desc).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.muted)
		})
	})
}

// renderSettingsView 渲染居中设置页：外观主题、客户端路径、CSV 导出与运行状态。
func (a *App) renderSettingsView(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		ui.Scroll(c.Key("settings-scroll")).Grow(1).Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(28, 32, 36, 32).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(720).Gap(22).Children(func() {
					// 页面主标题（去掉重复下面目录的冗余介绍句）
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Baseline).Gap(12).Children(func() {
						ui.Text(c, "设置").FontSize(t.text.x3l).FontWeight(600).LetterSpacing(-0.3).TextColor(t.foreground)
						numText(c, "v"+a.appVersion).FontSize(t.text.xs).TextColor(t.muted)
					})

					// 1. 外观主题设置
					drawSettingsSection(c, "外观", "",
						func() {
							ui.Row(c).FillWidth().Padding(14, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
								ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
									ui.Text(c, "界面主题").FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
									ui.Text(c, "选择浅色、深色或自动跟随系统").FontSize(t.text.xs).TextColor(t.muted)
								})
								ui.Row(c).Gap(10).Shrink(0).Children(func() {
									a.renderThemeTile(c, "system", "跟随系统")
									a.renderThemeTile(c, "light", "浅色")
									a.renderThemeTile(c, "dark", "深色")
								})
							})
						},
					)

					// 2. FF14 游戏客户端路径配置
					pathDirty := strings.TrimSpace(a.draftCNPath) != strings.TrimSpace(a.cfg.CNPath) ||
						strings.TrimSpace(a.draftGlobalPath) != strings.TrimSpace(a.cfg.GlobalPath)
					drawSettingsSection(c, "FF14 游戏客户端", "保存后将重新读取本地数据；所有游戏文件保持只读。",
						func() {
							ui.Column(c).FillWidth().Padding(14, 16).Children(func() {
								a.renderSinglePathInput(c, "settings-cn", "国服客户端路径 (CN)", "提供简体中文与国服数据", "例如：D:\\Final Fantasy\\CN\\最终幻想XIV", &a.draftCNPath, "选择 FF14 国服客户端目录")
							})
						},
						func() {
							ui.Column(c).FillWidth().Padding(14, 16).Children(func() {
								a.renderSinglePathInput(c, "settings-global", "国际服客户端路径 (Global)", "提供英/日/德/法多语言数据", "例如：D:\\Final Fantasy\\GLOBAL\\Game", &a.draftGlobalPath, "选择 FF14 国际服客户端目录")
							})
						},
						func() {
							ui.Column(c).FillWidth().Padding(12, 16).Gap(10).Children(func() {
								if a.draftError != "" {
									kitAlert(c, toneDanger, a.draftError)
								}
								if a.draftNotice != "" {
									kitAlert(c, toneSuccess, a.draftNotice)
								}
								ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
									if kitButton(c, "settings-revert-btn", "撤销修改", btnOpts{variant: btnGhost, size: btnSizeSm, disabled: !pathDirty}) {
										a.draftCNPath = a.cfg.CNPath
										a.draftGlobalPath = a.cfg.GlobalPath
										a.draftError = ""
										a.draftNotice = "已恢复当前生效的路径配置。"
									}
									canSave := pathDirty && (strings.TrimSpace(a.draftCNPath) != "" || strings.TrimSpace(a.draftGlobalPath) != "")
									if kitButton(c, "settings-save-btn", "保存并重新加载", btnOpts{variant: btnDefault, size: btnSizeSm, disabled: !canSave}) {
										a.SaveAndReloadPaths(a.draftCNPath, a.draftGlobalPath)
									}
								})
							})
						},
					)

					// 3. 数据工具
					drawSettingsSection(c, "数据工具", "",
						func() {
							ui.Row(c).FillWidth().Padding(14, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
								ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
									ui.Text(c, "导出全量游戏 CSV").FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
									ui.Text(c, "将全部游戏数据表导出为标准 CSV 文件或 Zip 压缩包").FontSize(t.text.xs).TextColor(t.muted)
								})
								if kitButton(c, "settings-open-export", "导出游戏 CSV", btnOpts{variant: btnOutline, size: btnSizeSm, icon: icoDownload}) {
									a.openExportModal()
								}
							})
						},
					)

					// 4. 运行状态与本地目录
					cnState := "未配置"
					if a.status != nil && a.status.HasCN {
						cnState = fmt.Sprintf("已加载 · v%s", a.status.CNVersion)
					}
					glState := "未配置"
					if a.status != nil && a.status.HasGlobal {
						glState = fmt.Sprintf("已加载 · v%s", a.status.GlobalVersion)
					}
					statsText := "–"
					if a.status != nil {
						statsText = fmt.Sprintf("%s 物品 · %s 配方 · %s 采集 · %s 成就（%d ms）",
							formatInt(a.status.Stats.TotalItems),
							formatInt(a.status.Stats.TotalRecipes),
							formatInt(a.status.Stats.GatherableItems),
							formatInt(a.status.Stats.TotalAchievements),
							a.status.Stats.LoadTimeMs,
						)
					}
					drawSettingsSection(c, "运行状态与存储", "",
						func() { drawSettingKVRow(c, "国服客户端", cnState) },
						func() { drawSettingKVRow(c, "国际服客户端", glState) },
						func() { drawSettingKVRow(c, "数据概览", statsText) },
						func() { drawSettingKVRow(c, "配置文件路径", config.ConfigFilePath(a.dataDir)) },
						func() { drawSettingKVRow(c, "版本快照目录", filepath.Join(a.dataDir, "backups")) },
					)
				})
			})
		})
	})
}

// renderThemeTile 绘制外观缩略图选择卡片（76×48 迷你桌面预览窗）。
func (a *App) renderThemeTile(c *ui.Context, mode, label string) {
	t := themeOf(c)
	cur := a.cfg.Theme
	if cur == "" {
		cur = "system"
	}
	selected := cur == mode

	btn := ui.ButtonBase(c.Key("theme-tile-" + mode)).Label(label).
		AlignItems(ui.Center).Gap(5).Cursor(ui.CursorPointer)
	btn.Children(func() {
		ui.Column(c).AlignItems(ui.Center).Gap(5).Children(func() {
			frame := ui.Row(c).Size(76, 48).Radius(t.radius.lg).Clip().Transition(colorTransition(motionFast))
			if selected {
				frame.Border(2, t.primary)
			} else if btn.Hovered() {
				frame.Border(strokeWidth, t.input)
			} else {
				frame.Border(strokeWidth, t.border)
			}

			drawHalf := func(dark bool) {
				var winBg, panelBg, lineCol ui.Color
				if dark {
					winBg = pSlate800.color()
					panelBg = pSlate950.color()
					lineCol = pSlate700.color()
				} else {
					winBg = pSlate100.color()
					panelBg = pWhite.color()
					lineCol = pSlate300.color()
				}
				ui.Row(c).Grow(1).FillHeight().Background(winBg).Padding(5).Gap(4).Children(func() {
					ui.Column(c).Width(14).FillHeight().Gap(3).Padding(2, 0).Children(func() {
						ui.Box(c).FillWidth().Height(3).Radius(2).Background(lineCol)
						ui.Box(c).FillWidth().Height(3).Radius(2).Background(lineCol.Alpha(0.6))
						ui.Box(c).FillWidth().Height(3).Radius(2).Background(lineCol.Alpha(0.6))
					})
					ui.Column(c).Grow(1).FillHeight().Radius(4).Background(panelBg).Padding(5).Gap(4).Children(func() {
						ui.Box(c).FillWidth().Height(4).Radius(2).Background(lineCol)
						ui.Box(c).Width(20).Height(3).Radius(2).Background(lineCol.Alpha(0.55))
					})
				})
			}

			frame.Children(func() {
				switch mode {
				case "light":
					drawHalf(false)
				case "dark":
					drawHalf(true)
				default:
					ui.Row(c).Fill().Children(func() {
						ui.Row(c).Grow(1).FillHeight().Background(pSlate100.color()).Padding(5, 0, 5, 5).Children(func() {
							ui.Column(c).Fill().Radius(4, 0, 0, 4).Background(pWhite.color()).Padding(5).Gap(4).Children(func() {
								ui.Box(c).FillWidth().Height(4).Radius(2).Background(pSlate300.color())
								ui.Box(c).Width(16).Height(3).Radius(2).Background(pSlate200.color())
							})
						})
						ui.Row(c).Grow(1).FillHeight().Background(pSlate800.color()).Padding(5, 5, 5, 0).Children(func() {
							ui.Column(c).Fill().Radius(0, 4, 4, 0).Background(pSlate950.color()).Padding(5).Gap(4).Children(func() {
								ui.Box(c).FillWidth().Height(4).Radius(2).Background(pSlate700.color())
								ui.Box(c).Width(16).Height(3).Radius(2).Background(pSlate800.color())
							})
						})
					})
				}
			})

			fg := t.muted
			fw := 400
			if selected {
				fg = t.foreground
				fw = 500
			}
			ui.Text(c, label).FontSize(t.text.xs).FontWeight(fw).TextColor(fg).SingleLine()
		})
	})
	if btn.Clicked() {
		a.SetThemeMode(mode)
	}
}

func drawSettingKVRow(c *ui.Context, label, value string) {
	t := themeOf(c)
	ui.Row(c).FillWidth().MinHeight(42).Padding(10, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
		ui.Text(c, label).FontSize(t.text.sm).TextColor(t.subdued).Shrink(0).SingleLine()
		ui.Text(c, value).FontSize(t.text.xs).TextColor(t.muted).FontFeatures("tnum").Selectable().TextAlign(ui.End).SingleLine().Grow(1).Shrink(1)
	})
}

func (a *App) renderSinglePathInput(
	c *ui.Context,
	keyPrefix string,
	title string,
	hint string,
	placeholder string,
	val *string,
	dialogTitle string,
) {
	t := themeOf(c)
	ui.Column(c.Key(keyPrefix + "-wrap")).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Column(c).Gap(1).Grow(1).Shrink(1).Children(func() {
				ui.Text(c, title).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
				ui.Text(c, hint).FontSize(t.text.xs).TextColor(t.muted)
			})
			a.renderPathValidationPill(c, *val)
		})

		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Grow(1).Shrink(1).Children(func() {
				if kitInput(c, keyPrefix+"-input", val, kitInputOpts{
					placeholder: placeholder, label: title, size: btnSizeMd, clearable: true,
				}).Changed() {
					a.draftError = ""
					a.draftNotice = ""
				}
			})
			if kitButton(c, keyPrefix+"-browse", "浏览...", btnOpts{variant: btnOutline, size: btnSizeMd, icon: icoFolder}) {
				cur := *val
				a.pickDirectoryAsync(dialogTitle, cur, func(picked string) {
					*val = picked
					a.draftError = ""
					a.draftNotice = ""
				})
			}
		})
	})
}

func (a *App) renderPathValidationPill(c *ui.Context, rawPath string) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		drawPill(c, "未填写", pillOutline)
		return
	}
	info, err := config.ValidateClientPath(trimmed, "")
	if err != nil || info == nil {
		drawPill(c, "未找到 game\\sqpack", pillNegative)
		return
	}
	drawPill(c, "有效 · v"+info.Version, pillPositive)
}
