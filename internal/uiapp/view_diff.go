package uiapp

import (
	"fmt"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

var diffFilterModes = []string{"all", "added", "modified", "removed"}

func (a *App) diffFilterLabels() []string {
	return []string{
		a.t("diff.filter_all"),
		a.t("diff.filter_added"),
		a.t("diff.filter_modified"),
		a.t("diff.filter_removed"),
	}
}

var diffEntityModes = []string{"all", "item", "quest", "leve", "achievement"}

func (a *App) diffEntityLabels() []string {
	return []string{
		a.t("diff.entity_all"),
		a.t("diff.entity_item"),
		a.t("diff.entity_quest"),
		a.t("diff.entity_leve"),
		a.t("diff.entity_achievement"),
	}
}

const (
	diffColGap    float32 = 10
	diffColIDW    float32 = 68
	diffColNameW  float32 = 240
	diffColTypeW  float32 = 84
	diffColChgMin float32 = 140
)

func canInspectDiffEntry(entry backup.DiffItemEntry) bool {
	if entry.ChangeType == "removed" {
		return false
	}
	switch entry.EntityType {
	case "item", "quest", "leve", "achievement":
		return true
	default:
		return false
	}
}

func (a *App) inspectDiffEntry(entry backup.DiffItemEntry) {
	if !canInspectDiffEntry(entry) {
		return
	}
	switch entry.EntityType {
	case "item":
		a.inspectItemFromOtherTab(entry.ID)
	case "quest":
		a.inspectQuestFromOtherTab(entry.ID)
	case "leve":
		a.inspectLeveFromOtherTab(entry.ID)
	case "achievement":
		a.inspectAchievementFromOtherTab(entry.ID, entry.RewardItemID)
	}
}

func (a *App) hasDiffFilter() bool {
	return strings.TrimSpace(a.diffSearch) != "" ||
		a.diffFilter != "all" ||
		a.diffEntity != "all"
}

func (a *App) resetDiffFilters() {
	a.diffSearch = ""
	a.diffFilter = "all"
	a.diffEntity = "all"
	a.diffPage = 1
	a.refreshDiff()
}

func (a *App) renderDiffView(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		// 1. 顶部版本选择与快照控制栏
		ui.Row(c).FillWidth().Padding(12, 16).Gap(10).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.t("diff.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				if a.versionStatus != nil && a.versionStatus.HasVersionUpdate {
					drawPill(c, a.t("diff.has_new_version"), pillPositive)
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				var backups []backup.VersionBackupMeta
				if a.versionStatus != nil {
					backups = a.versionStatus.Backups
				}
				ui.Text(c, a.t("diff.old_version")).FontSize(t.text.xs).TextColor(t.muted)
				a.renderVersionSelect(c, "diff-from", &a.diffFromVer, backups)
				ui.Icon(c, icoArrowRight).Size(iconSm, iconSm).TextColor(t.muted)
				ui.Text(c, a.t("diff.new_version")).FontSize(t.text.xs).TextColor(t.muted)
				a.renderVersionSelect(c, "diff-to", &a.diffToVer, backups)

				btnLbl := a.t("diff.refresh_snapshot")
				if a.diffBusy {
					btnLbl = a.t("diff.refreshing")
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
					drawCompactDiffStat(c, a.t("diff.stat_added_items"), "+"+formatInt(sum.AddedItems), t.successText)
					drawCompactDiffStat(c, a.t("diff.stat_modified_items"), formatInt(sum.ModifiedItems), t.warningText)
					colTotal := sum.NewCollectableTurnInItems + sum.ModifiedCollectableItems
					drawCompactDiffStat(c, a.t("diff.stat_collectables"), formatInt(colTotal), t.infoText)
					drawCompactDiffStat(c, a.t("diff.stat_added_quests"), "+"+formatInt(sum.AddedQuests), t.successText)
					drawCompactDiffStat(c, a.t("diff.stat_added_leves"), "+"+formatInt(sum.AddedLeves), t.successText)
					drawCompactDiffStat(c, a.t("diff.stat_added_recipes"), "+"+formatInt(sum.AddedRecipes), t.foreground)
					drawCompactDiffStat(c, a.t("diff.stat_added_achievements"), "+"+formatInt(sum.AddedAchievements), t.infoText)
					drawCompactDiffStat(c, a.t("diff.stat_added_titles"), "+"+formatInt(sum.AddedTitles), t.foreground)
				})
			}

			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).Children(func() {
					ui.Box(c).Grow(1).MinWidth(220).MaxWidth(380).Children(func() {
						a.renderPageSearchInput(c, "diff-page-search", &a.diffSearch, a.t("search.diff_placeholder"), a.t("search.diff_label"), func() {
							a.diffPage = 1
							a.refreshDiff()
						})
					})
					if a.hasDiffFilter() {
						if kitButton(c, "diff-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.resetDiffFilters()
						}
					}
				})

				if a.diffRes != nil {
					numText(c, a.tf("diff.total_filtered", "count", formatInt(a.diffRes.TotalFiltered))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				}
			})

			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
					fltIdx := 0
					for i, m := range diffFilterModes {
						if m == a.diffFilter {
							fltIdx = i
							break
						}
					}
					if nextIdx, chg := kitSegmentedTabs(c, "df-flt", fltIdx, a.diffFilterLabels()); chg {
						a.diffFilter = diffFilterModes[nextIdx]
						a.diffPage = 1
						a.refreshDiff()
					}

					kitSeparator(c, true)

					entityLabels := a.diffEntityLabels()
					for i, m := range diffEntityModes {
						if kitToggleChip(c, "df-ent-"+m, entityLabels[i], "", a.diffEntity == m, nil) {
							a.diffEntity = m
							a.diffPage = 1
							a.refreshDiff()
						}
					}
				})
			})
		})

		// 3. 变更表头
		ui.Row(c).FillWidth().Padding(8, 16).Background(t.card).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Gap(diffColGap).Children(func() {
			ui.Text(c, a.t("diff.col_id")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffColIDW).Shrink(0)
			ui.Text(c, a.t("diff.col_name")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffColNameW).Shrink(0)
			ui.Text(c, a.t("diff.col_type")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffColTypeW).Shrink(0).TextAlign(ui.Center)
			ui.Text(c, a.t("diff.col_changes")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Grow(1).Shrink(1).MinWidth(diffColChgMin)
		})

		// 4. 变更明细行
		if a.diffRes == nil || len(a.diffRes.Items) == 0 {
			kitEmpty(c, icoCompare, a.t("diff.empty_title"), a.t("diff.empty_desc"), nil)
		} else {
			ui.Scroll(c.Key("diff-table-scroll")).TrackScroll(&a.diffScroll).Grow(1).Shrink(1).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for i, entry := range a.diffRes.Items {
						if i > 0 {
							kitSeparator(c, false).Margin(0, 16)
						}
						row := ui.Row(c.Key(fmt.Sprintf("df-row-%s-%d", entry.EntityType, entry.ID))).
							FillWidth().Padding(10, 16).Gap(diffColGap).
							AlignItems(ui.Center).Transition(colorTransition(motionFast))
						if row.Hovered() {
							row.Background(t.tint(0.85))
						}
						row.Children(func() {
							dispName := entry.NameCn
							if entry.EntityType == "quest" {
								dispName = gamedata.CleanQuestTitle(dispName)
							}
							canJump := canInspectDiffEntry(entry)
							renderIDAndName := func(hovered bool) {
								// ID 列左对齐，并将类型 tag 收纳在 ID 下方，避免单独占列或挤压名称
								ui.Column(c).Width(diffColIDW).Shrink(0).AlignItems(ui.Start).Gap(3).Children(func() {
									numTextf(c, "#%d", entry.ID).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
									switch entry.EntityType {
									case "achievement":
										drawPill(c, a.t("diff.tag_achievement"), pillAccent)
									case "quest":
										drawPill(c, a.t("diff.tag_quest"), pillWarning)
									case "leve":
										drawPill(c, a.t("diff.tag_leve"), pillPositive)
									default:
										if entry.HasCollectableChange {
											drawPill(c, a.t("diff.tag_collectable"), pillAccent)
										}
									}
								})

								ui.Row(c).Width(diffColNameW).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
									a.drawGameIcon(c, entry.IconID, false, 26, t.radius.md)
									ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
										nameCol := t.foreground
										if hovered {
											nameCol = t.primary
										}
										ui.Text(c, dispName).FontSize(t.text.xs).FontWeight(500).TextColor(nameCol).SingleLine()
										ui.Text(c, entry.CategoryName).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
									})
								})
							}

							jumpW := diffColIDW + diffColGap + diffColNameW
							if canJump {
								jumpBtn := ui.ButtonBase(c.Key(fmt.Sprintf("df-jump-%s-%d", entry.EntityType, entry.ID))).
									Label(a.tf("common.view_item", "name", dispName)).Width(jumpW).Shrink(0).Gap(diffColGap).Padding(0).
									Justify(ui.Start).AlignItems(ui.Center).
									Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
								jumpBtn.Children(func() {
									renderIDAndName(jumpBtn.Hovered())
								})
								if jumpBtn.Clicked() {
									a.inspectDiffEntry(entry)
								}
							} else {
								ui.Row(c).Width(jumpW).Shrink(0).Gap(diffColGap).AlignItems(ui.Center).Children(func() {
									renderIDAndName(false)
								})
							}

							// 类型列使用 Row 居中，防止 Column 默认 Stretch 拉宽胶囊产生多余空白
							ui.Row(c).Width(diffColTypeW).Shrink(0).Center().Children(func() {
								switch entry.ChangeType {
								case "added":
									drawPill(c, a.t("diff.change_added"), pillPositive)
								case "modified":
									drawPill(c, a.t("diff.change_modified"), pillWarning)
								default:
									drawPill(c, a.t("diff.change_removed"), pillNegative)
								}
							})

							// 仅“变更”条目显示具体字段变化；新增与已移除条目保持空白
							ui.Row(c).Grow(1).Shrink(1).MinWidth(diffColChgMin).Gap(10).Wrap().AlignItems(ui.Center).Children(func() {
								if entry.ChangeType != "modified" {
									return
								}
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
		curLabel = a.t("diff.select_snapshot")
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
		applyOverlaySurface(c, panel.Margin(4, 0, 0, 0).Padding(4).Radius(t.radius.lg).MaxHeight(280).Width(264), shadowOverlay)
		ui.Scroll(c.Key("vsel-scr-" + key)).Shrink(1).Children(func() {
			for _, b := range backups {
				isCur := b.Version == *val
				tag := a.t("diff.snapshot_local")
				if b.IsBaseline {
					tag = a.t("diff.snapshot_baseline")
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
