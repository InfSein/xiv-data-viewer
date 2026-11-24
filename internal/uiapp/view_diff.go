package uiapp

import (
	"fmt"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/egoist/mygo/ui"
)

var diffFilterModes = []string{"all", "added", "modified", "removed"}
var diffFilterLabels = []string{"全部", "新增", "变更", "移除"}

var diffEntityModes = []string{"all", "item", "achievement"}
var diffEntityLabels = []string{"全部", "物品", "成就"}

func (a *App) renderDiffView(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		// 1. 顶部版本选择与快照控制栏
		ui.Row(c).FillWidth().Padding(12, 16).Gap(10).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "版本变更对比").FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				if a.versionStatus != nil && a.versionStatus.HasVersionUpdate {
					drawPill(c, "有新版本", pillPositive)
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				var backups []backup.VersionBackupMeta
				if a.versionStatus != nil {
					backups = a.versionStatus.Backups
				}
				ui.Text(c, "旧版本").FontSize(t.text.xs).TextColor(t.muted)
				a.renderVersionSelect(c, "diff-from", &a.diffFromVer, backups)
				ui.Icon(c, icoArrowRight).Size(iconSm, iconSm).TextColor(t.muted)
				ui.Text(c, "新版本").FontSize(t.text.xs).TextColor(t.muted)
				a.renderVersionSelect(c, "diff-to", &a.diffToVer, backups)

				btnLbl := "刷新快照"
				if a.diffBusy {
					btnLbl = "刷新中..."
				}
				if kitButton(c, "diff-refresh-backup", btnLbl, btnOpts{variant: btnOutline, size: btnSizeSm, icon: icoRefresh, disabled: a.diffBusy}) {
					a.refreshVersionBackup()
				}
			})
		})

		// 2. 紧凑内联变更摘要条 + 筛选器（取代占地的四栏大数字卡片，把空间留给对比表）
		ui.Column(c).FillWidth().Padding(10, 16).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			if a.diffError != "" {
				kitAlert(c, toneDanger, a.diffError)
			} else if a.diffNotice != "" {
				kitAlert(c, toneSuccess, a.diffNotice)
			}

			if a.diffRes != nil {
				sum := a.diffRes.Summary
				kitBlock(c, ui.Row(c).FillWidth().Padding(8, 12).Gap(16).AlignItems(ui.Center).Wrap(), toneCard).Children(func() {
					drawCompactDiffStat(c, "新增物品", "+"+formatInt(sum.AddedItems), t.successText)
					drawCompactDiffStat(c, "物品变更", formatInt(sum.ModifiedItems), t.warningText)
					drawCompactDiffStat(c, "新增配方", "+"+formatInt(sum.AddedRecipes), t.foreground)
					drawCompactDiffStat(c, "可采集", "+"+formatInt(sum.NewGatherableItems), t.foreground)
					drawCompactDiffStat(c, "新增成就", "+"+formatInt(sum.AddedAchievements), t.infoText)
					drawCompactDiffStat(c, "新增称号", "+"+formatInt(sum.AddedTitles), t.foreground)
				})
			}

			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
					fltIdx := 0
					for i, m := range diffFilterModes {
						if m == a.diffFilter {
							fltIdx = i
							break
						}
					}
					if nextIdx, chg := kitSegmentedTabs(c, "df-flt", fltIdx, diffFilterLabels); chg {
						a.diffFilter = diffFilterModes[nextIdx]
						a.diffPage = 1
						a.refreshDiff()
					}

					kitSeparator(c, true)

					for i, m := range diffEntityModes {
						if kitToggleChip(c, "df-ent-"+m, diffEntityLabels[i], "", a.diffEntity == m, nil) {
							a.diffEntity = m
							a.diffPage = 1
							a.refreshDiff()
						}
					}
				})

				if a.diffRes != nil {
					numTextf(c, "共 %s 条变更", formatInt(a.diffRes.TotalFiltered)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				}
			})
		})

		// 3. 变更表头
		ui.Row(c).FillWidth().Padding(8, 16).Background(t.card).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Gap(10).Children(func() {
			ui.Text(c, "ID").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(54).TextAlign(ui.End)
			ui.Text(c, "名称").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(190)
			ui.Text(c, "品级/点数").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(62).TextAlign(ui.End)
			ui.Text(c, "类型").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(72)
			ui.Text(c, "变更内容").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Grow(1).MinWidth(140)
			ui.Text(c, "操作").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(74).TextAlign(ui.End)
		})

		// 4. 变更明细行
		if a.diffRes == nil || len(a.diffRes.Items) == 0 {
			kitEmpty(c, icoCompare, "当前条件下没有版本变更", "可在上方切换对比版本，或清空搜索关键词与筛选条件。", nil)
		} else {
			ui.Scroll(c.Key("diff-table-scroll")).TrackScroll(&a.diffScroll).Grow(1).Shrink(1).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for i, entry := range a.diffRes.Items {
						if i > 0 {
							kitSeparator(c, false).Margin(0, 16)
						}
						isAch := entry.EntityType == "achievement"
						row := ui.Row(c.Key(fmt.Sprintf("df-row-%s-%d", entry.EntityType, entry.ID))).
							FillWidth().Padding(10, 16).Gap(10).
							AlignItems(ui.Center).Transition(colorTransition(motionFast))
						if row.Hovered() {
							row.Background(t.tint(0.85))
						}
						row.Children(func() {
							numTextf(c, "#%d", entry.ID).FontSize(t.text.micro).TextColor(t.muted).Width(54).TextAlign(ui.End).SingleLine()

							ui.Row(c).Width(190).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
								a.drawGameIcon(c, entry.IconID, false, 26, t.radius.md)
								ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
									ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
										ui.Text(c, entry.NameCn).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
										if isAch {
											drawPill(c, "成就", pillAccent)
										}
									})
									ui.Text(c, entry.CategoryName).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
								})
							})

							lvTxt := fmt.Sprintf("%d", entry.ItemLevel)
							if isAch {
								lvTxt = fmt.Sprintf("%d 点", entry.Points)
							}
							numText(c, lvTxt).FontSize(t.text.xs).TextColor(t.foreground).Width(62).TextAlign(ui.End).SingleLine()

							ui.Box(c).Width(72).Shrink(0).Children(func() {
								switch entry.ChangeType {
								case "added":
									drawPill(c, "新增", pillPositive)
								case "modified":
									drawPill(c, "变更", pillWarning)
								default:
									drawPill(c, "已移除", pillNegative)
								}
							})

							// 无外层套娃框的清爽增删变更项
							ui.Row(c).Grow(1).Shrink(1).MinWidth(140).Gap(10).Wrap().AlignItems(ui.Center).Children(func() {
								for _, chg := range entry.Changes {
									ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
										ui.Text(c, chg.Label).FontSize(t.text.micro).TextColor(t.muted)
										ui.Box(c).Padding(1, 4).Radius(t.radius.xs).Background(t.delBg).Children(func() {
											numText(c, chg.Before).FontSize(t.text.micro).TextColor(t.delText).Strikethrough()
										})
										ui.Text(c, "→").FontSize(t.text.micro).TextColor(t.muted)
										ui.Box(c).Padding(1, 4).Radius(t.radius.xs).Background(t.insBg).Children(func() {
											numText(c, chg.After).FontSize(t.text.micro).FontWeight(500).TextColor(t.insText)
										})
									})
								}
							})

							ui.Row(c).Width(74).Shrink(0).Justify(ui.End).Children(func() {
								if !isAch && entry.ChangeType != "removed" {
									if kitButton(c, fmt.Sprintf("df-view-item-%d", entry.ID), "详情", btnOpts{variant: btnOutline, size: btnSizeXs}) {
										a.inspectItemFromOtherTab(entry.ID)
									}
								} else if isAch && entry.RewardItemID > 0 {
									if kitButton(c, fmt.Sprintf("df-view-rew-%d", entry.ID), "奖励", btnOpts{variant: btnOutline, size: btnSizeXs}) {
										a.inspectItemFromOtherTab(entry.RewardItemID)
									}
								}
							})
						})
					}
				})
			})
		}

		// 5. 底部分页栏
		if a.diffRes != nil {
			a.renderPaginationBar(
				c,
				"diff-pg",
				a.diffRes.Page,
				a.diffRes.TotalPages,
				&a.diffPageStr,
				func(next int) {
					a.diffPage = next
					a.refreshDiff()
				},
			)
		}
	})
}

func drawCompactDiffStat(c *ui.Context, label, value string, valColor ui.Color) {
	t := themeOf(c)
	ui.Row(c).Gap(5).AlignItems(ui.Baseline).Children(func() {
		ui.Text(c, label).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
		numText(c, value).FontSize(t.text.sm).FontWeight(600).TextColor(valColor).SingleLine()
	})
}

func (a *App) renderVersionSelect(c *ui.Context, key string, val *string, backups []backup.VersionBackupMeta) {
	t := themeOf(c)
	sel := ui.SelectBase(c.Key(key), val)
	curLabel := *val
	if curLabel == "" {
		curLabel = "选择快照"
	}

	trig := sel.Trigger.Height(28).Width(178).Padding(0, 8, 0, 10).Radius(t.radius.md).
		Background(t.background).Border(strokeWidth, t.input).
		Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
	if trig.Hovered() {
		trig.Background(t.hover)
	}
	trig.Children(func() {
		numText(c, curLabel).FontSize(t.text.xs).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
		ui.Icon(c, icoChevronDown).Size(iconSm, iconSm).Shrink(0).TextColor(t.muted)
	})

	sel.Popup(func(panel ui.Element) {
		applyOverlaySurface(c, panel.Margin(4, 0, 0, 0).Padding(4).Radius(t.radius.lg).MaxHeight(280).Width(244), shadowOverlay)
		ui.Scroll(c.Key("vsel-scr-" + key)).Shrink(1).Children(func() {
			for _, b := range backups {
				isCur := b.Version == *val
				tag := "本地快照"
				if b.IsBaseline {
					tag = "历史基准"
				}
				row := sel.Item(b.Version).Height(28).Padding(0, 8).Radius(t.radius.md).
					Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6)
				if row.Highlighted() {
					row.Background(t.tint(1.25))
				}
				row.Children(func() {
					numTextf(c, "%s (%s)", b.Version, tag).FontSize(t.text.xs).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
					if isCur {
						ui.Icon(c, icoCheck).Size(iconXs, iconXs).Shrink(0).TextColor(t.primary)
					}
				})
			}
		})
	})

	if sel.Trigger.Changed() {
		a.diffPage = 1
		a.refreshDiff()
	}
}
