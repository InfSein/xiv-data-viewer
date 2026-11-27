package uiapp

import (
	"fmt"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/egoist/mygo/ui"
)

var snapMgrFilterModes = []string{"all", "downloaded", "not_downloaded"}

var diffFilterModes = []string{"all", "added", "modified", "removed"}

var diffTextFilterModes = []string{"all", "modified", "removed"}

func (a *App) diffFilterLabels() []string {
	return []string{
		a.t("diff.filter_all"),
		a.t("diff.filter_added"),
		a.t("diff.filter_modified"),
		a.t("diff.filter_removed"),
	}
}

func (a *App) diffTextFilterLabels() []string {
	return []string{
		a.t("diff.filter_all"),
		a.t("diff.filter_modified"),
		a.t("diff.filter_removed"),
	}
}

var diffEntityModes = []string{"all", "item", "quest", "leve", "achievement", "bgm", "icon", "text"}

func (a *App) diffEntityLabels() []string {
	return []string{
		a.t("diff.entity_all"),
		a.t("diff.entity_item"),
		a.t("diff.entity_quest"),
		a.t("diff.entity_leve"),
		a.t("diff.entity_achievement"),
		a.t("diff.entity_bgm"),
		a.t("diff.entity_icon"),
		a.t("diff.entity_text"),
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
	case "item", "quest", "leve", "achievement", "bgm", "icon":
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
	case "bgm":
		a.inspectBGMFromOtherTab(entry.ID)
	case "icon":
		a.inspectIconFromOtherTab(entry.ID)
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

	// 首次进入版本对比页面时自动静默同步 GitHub 快照目录，以便映射人类可读版本号（如 7.58）
	if a.win != nil && a.backupSvc != nil && !a.snapCatalogTried && !a.snapCatalogBusy {
		a.refreshSnapshotCatalog(false)
	}

	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		// 1. 顶部版本选择与 GitHub 快照管理栏
		ui.Row(c).FillWidth().Padding(12, 16).Gap(10).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, a.t("diff.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				// 同时配置了国服与国际服时，提供同服对比切换器（国服对国服、国际服对国际服）
				if a.versionStatus != nil && a.versionStatus.HasCN && a.versionStatus.HasGlobal {
					regModes := []string{snapshot.RegionCN, snapshot.RegionGlobal}
					regLabels := []string{a.t("shell.server_cn"), a.t("shell.server_global")}
					curIdx := 0
					if a.diffRegion == snapshot.RegionGlobal {
						curIdx = 1
					}
					if nextIdx, chg := kitSegmentedTabs(c, "diff-region-switch", curIdx, regLabels); chg {
						a.switchDiffRegion(regModes[nextIdx])
					}
				}
				if a.versionStatus != nil {
					if curLabel := a.versionStatus.CurrentClientLabelForRegion(a.diffRegion); curLabel != "" {
						drawPill(c, curLabel, pillOutline)
					}
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				var backups []backup.VersionBackupMeta
				if a.versionStatus != nil {
					backups = a.versionStatus.BackupsForRegion(a.diffRegion)
				}
				ui.Text(c, a.t("diff.old_version")).FontSize(t.text.xs).TextColor(t.muted)
				a.renderVersionSelect(c, "diff-from", &a.diffFromVer, backups)
				ui.Icon(c, icoArrowRight).Size(iconSm, iconSm).TextColor(t.muted)
				ui.Text(c, a.t("diff.new_version")).FontSize(t.text.xs).TextColor(t.muted)
				a.renderVersionSelect(c, "diff-to", &a.diffToVer, backups)

				mgrLbl := a.t("diff.manage_snapshots")
				if a.snapDownID != "" {
					mgrLbl = a.t("diff.snap_downloading")
				}
				if kitButton(c, "diff-open-snap-mgr", mgrLbl, btnOpts{variant: btnOutline, size: btnSizeSm, icon: icoDownload}) {
					a.openSnapshotManager()
				}
			})
		})

		// 若尚未从 GitHub 下载任何历史快照，直接展示引导下载面板
		if !a.diffBusy && (a.diffRes == nil || a.diffRes.NeedsDownload) {
			a.renderDiffNeedsDownloadView(c)
			return
		}

		// 2. 紧凑内联变更摘要条 + 筛选器
		ui.Column(c).FillWidth().Padding(10, 16).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			if a.diffError != "" {
				kitAlert(c, toneDanger, a.diffError)
			} else if a.diffNotice != "" {
				kitAlert(c, toneSuccess, a.diffNotice)
			}

			// 仅在实体总览视图展示非零汇总项，避免零值指标刷屏与图标/文本独立分区重复堆叠两层统计条
			if a.diffRes != nil && a.diffEntity != "icon" && a.diffEntity != "text" {
				sum := a.diffRes.Summary
				colTotal := sum.NewCollectableTurnInItems + sum.ModifiedCollectableItems
				type diffStatDef struct {
					label string
					val   int
					text  string
					col   ui.Color
				}
				allStats := []diffStatDef{
					{a.t("diff.stat_added_items"), sum.AddedItems, "+" + formatInt(sum.AddedItems), t.successText},
					{a.t("diff.stat_modified_items"), sum.ModifiedItems, formatInt(sum.ModifiedItems), t.warningText},
					{a.t("diff.stat_collectables"), colTotal, formatInt(colTotal), t.infoText},
					{a.t("diff.stat_added_quests"), sum.AddedQuests, "+" + formatInt(sum.AddedQuests), t.successText},
					{a.t("diff.stat_added_leves"), sum.AddedLeves, "+" + formatInt(sum.AddedLeves), t.successText},
					{a.t("diff.stat_added_recipes"), sum.AddedRecipes, "+" + formatInt(sum.AddedRecipes), t.foreground},
					{a.t("diff.stat_added_achievements"), sum.AddedAchievements, "+" + formatInt(sum.AddedAchievements), t.infoText},
					{a.t("diff.stat_added_titles"), sum.AddedTitles, "+" + formatInt(sum.AddedTitles), t.foreground},
					{a.t("diff.stat_added_bgms"), sum.AddedBGMs, "+" + formatInt(sum.AddedBGMs), t.successText},
					{a.t("diff.stat_modified_bgms"), sum.ModifiedBGMs, formatInt(sum.ModifiedBGMs), t.warningText},
				}
				var visibleStats []diffStatDef
				for _, st := range allStats {
					if st.val > 0 {
						visibleStats = append(visibleStats, st)
					}
				}
				if len(visibleStats) > 0 {
					kitBlock(c, ui.Row(c).FillWidth().Padding(8, 12).Gap(16).AlignItems(ui.Center).Wrap(), toneCard).Children(func() {
						for _, st := range visibleStats {
							drawCompactDiffStat(c, st.label, st.text, st.col)
						}
					})
				}
			}

			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).Children(func() {
					searchPH := a.t("search.diff_placeholder")
					if a.diffEntity == "text" {
						searchPH = a.t("diff.text_search_placeholder")
					}
					ui.Box(c).Grow(1).MinWidth(220).MaxWidth(380).Children(func() {
						a.renderPageSearchInput(c, "diff-page-search", &a.diffSearch, searchPH, a.t("search.diff_label"), func() {
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

				if a.diffRes != nil && !a.diffBusy {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
						numText(c, a.tf("diff.total_filtered", "count", formatInt(a.diffRes.TotalFiltered))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
						if kitButton(c, "diff-open-export", a.t("diff.export_btn"), btnOpts{
							variant:  btnOutline,
							size:     btnSizeSm,
							icon:     icoDownload,
							disabled: a.diffRes.TotalFiltered == 0 || a.diffExporterSvc == nil,
						}) {
							a.openDiffExportModal()
						}
					})
				}
			})

			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
					activeFltModes := diffFilterModes
					activeFltLabels := a.diffFilterLabels()
					fltScope := "df-flt"
					if a.diffEntity == "text" {
						activeFltModes = diffTextFilterModes
						activeFltLabels = a.diffTextFilterLabels()
						fltScope = "df-flt-text"
					}
					fltIdx := 0
					for i, m := range activeFltModes {
						if m == a.diffFilter {
							fltIdx = i
							break
						}
					}
					if nextIdx, chg := kitSegmentedTabs(c, fltScope, fltIdx, activeFltLabels); chg {
						a.diffFilter = activeFltModes[nextIdx]
						a.diffPage = 1
						a.refreshDiff()
					}

					kitSeparator(c, true)

					entityLabels := a.diffEntityLabels()
					for i, m := range diffEntityModes {
						if m == "icon" {
							kitSeparator(c, true)
						}
						chipCount := ""
						if m == "icon" && a.diffRes != nil {
							tot := a.diffRes.Summary.AddedIcons + a.diffRes.Summary.ModifiedIcons + a.diffRes.Summary.RemovedIcons
							if tot > 0 {
								chipCount = formatInt(tot)
							}
						} else if m == "text" && a.diffRes != nil {
							tot := a.diffRes.Summary.ModifiedTexts + a.diffRes.Summary.RemovedTexts
							if tot > 0 {
								chipCount = formatInt(tot)
							}
						}
						if kitToggleChip(c, "df-ent-"+m, entityLabels[i], chipCount, a.diffEntity == m, nil) {
							a.diffEntity = m
							if m == "text" && a.diffFilter == "added" {
								a.diffFilter = "all"
							}
							a.diffPage = 1
							a.diffScroll.Y = 0
							a.refreshDiff()
						}
					}
				})
			})
		})

		// 后台首次解析历史 .xivsnap 快照或扫描文本期间展示平滑加载指示
		if a.diffBusy {
			kitLoading(c, a.t("diff.computing_title"), a.t("diff.computing_desc"))
			return
		}

		// 3 & 4. 图标、文本与成就使用专属视图，常规实体使用四列表格
		if a.diffEntity == "icon" {
			a.renderIconDiffPartition(c)
		} else if a.diffEntity == "text" {
			a.renderTextDiffPartition(c)
		} else if a.diffEntity == "achievement" {
			a.renderAchievementDiffTable(c)
		} else {
			ui.Row(c).FillWidth().Padding(8, 16).Background(t.card).
				BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Gap(diffColGap).Children(func() {
				ui.Text(c, a.t("diff.col_id")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffColIDW).Shrink(0)
				ui.Text(c, a.t("diff.col_name")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffColNameW).Shrink(0)
				ui.Text(c, a.t("diff.col_type")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffColTypeW).Shrink(0).TextAlign(ui.Center)
				ui.Text(c, a.t("diff.col_changes")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Grow(1).Shrink(1).MinWidth(diffColChgMin)
			})

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
									ui.Column(c).Width(diffColIDW).Shrink(0).AlignItems(ui.Start).Gap(3).Children(func() {
										numTextf(c, "#%d", entry.ID).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
										switch entry.EntityType {
										case "achievement":
											drawPill(c, a.t("diff.tag_achievement"), pillAccent)
										case "quest":
											drawPill(c, a.t("diff.tag_quest"), pillWarning)
										case "leve":
											drawPill(c, a.t("diff.tag_leve"), pillPositive)
										case "bgm":
											drawPill(c, a.t("diff.tag_bgm"), pillAccent)
										default:
											if entry.HasCollectableChange {
												drawPill(c, a.t("diff.tag_collectable"), pillAccent)
											}
										}
									})

									ui.Row(c).Width(diffColNameW).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
										if entry.EntityType == "bgm" && entry.IconID <= 0 {
											ui.Box(c).Size(26, 26).Radius(t.radius.md).Background(t.tint(1)).Border(hairline, t.border).Center().Shrink(0).Children(func() {
												ui.Icon(c, icoMusic).Size(14, 14).TextColor(t.subdued)
											})
										} else {
											a.drawGameIcon(c, entry.IconID, false, 26, t.radius.md)
										}
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

								ui.Row(c).Grow(1).Shrink(1).MinWidth(diffColChgMin).Gap(10).Wrap().AlignItems(ui.Center).Children(func() {
									if entry.ChangeType == "added" && entry.EntityType == "achievement" {
										a.renderAddedAchievementInlineSummary(c, entry)
										return
									}
									if entry.ChangeType != "modified" {
										return
									}
									renderDiffFieldChanges(c, entry.Changes)
								})
							})
						}
					})
				})
			}
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

// diffEntryToAchievement 将版本对比中的成就差异记录转换为成就表格展示模型。
func diffEntryToAchievement(entry backup.DiffItemEntry) gamedata.AchievementEntry {
	ach := gamedata.AchievementEntry{
		ID:               entry.ID,
		NameCn:           entry.NameCn,
		NameEn:           entry.NameEn,
		NameJa:           entry.NameJa,
		DescriptionCn:    entry.DescriptionCn,
		Points:           entry.Points,
		IconID:           entry.IconID,
		KindName:         entry.AchKindName,
		CategoryName:     entry.AchCategoryName,
		TitleID:          entry.TitleID,
		TitleMasculineCn: entry.TitleMasculineCn,
		TitleFeminineCn:  entry.TitleFeminineCn,
		TitleNameJa:      entry.TitleNameJa,
		TitleNameEn:      entry.TitleNameEn,
		RewardItemID:     entry.RewardItemID,
		RewardItemNameCn: entry.RewardItemNameCn,
		RewardItemIconID: entry.RewardItemIconID,
	}
	if ach.KindName == "" && ach.CategoryName == "" {
		ach.CategoryName = entry.CategoryName
	}
	if ach.TitleID == 0 && entry.TitleNameCn != "" {
		ach.TitleID = 1
		ach.TitleMasculineCn = entry.TitleNameCn
	}
	return ach
}

// renderAddedAchievementInlineSummary 在「全部」视图的变更内容列为新增成就展示点数、称号与奖励道具摘要。
func (a *App) renderAddedAchievementInlineSummary(c *ui.Context, entry backup.DiffItemEntry) {
	t := themeOf(c)
	ach := diffEntryToAchievement(entry)
	if ach.Points > 0 {
		numText(c, a.tf("backup.ach_points", "pts", formatInt(ach.Points))).FontSize(t.text.micro).FontWeight(600).TextColor(t.subdued)
	}
	titleTxt := ach.TitleMasculineCn
	if titleTxt == "" {
		titleTxt = ach.TitleFeminineCn
	} else if ach.TitleFeminineCn != "" && ach.TitleFeminineCn != ach.TitleMasculineCn {
		titleTxt += " / " + ach.TitleFeminineCn
	}
	if titleTxt != "" {
		ui.Text(c, a.tf("achievements.title_format", "title", titleTxt)).FontSize(t.text.xs).FontWeight(500).TextColor(t.warningText).SingleLine()
	}
	if ach.RewardItemID > 0 && ach.RewardItemNameCn != "" {
		btn := ui.ButtonBase(c.Key(fmt.Sprintf("df-ach-rew-%d", ach.ID))).Label(a.tf("common.view_item", "name", ach.RewardItemNameCn)).
			Padding(2, 6).Radius(t.radius.md).Background(t.card).
			Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
		if btn.Hovered() {
			btn.Background(t.tint(1.5))
		}
		btn.Children(func() {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				a.drawGameIcon(c, ach.RewardItemIconID, false, 16, 3)
				ui.Text(c, ach.RewardItemNameCn).FontSize(t.text.micro).FontWeight(500).TextColor(t.infoText).SingleLine()
			})
		})
		if btn.Clicked() {
			a.inspectItemFromOtherTab(ach.RewardItemID)
		}
	}
	if ach.DescriptionCn != "" {
		ui.Text(c, ach.DescriptionCn).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(1)
	}
}

const (
	diffAchColIDW   float32 = 58
	diffAchColNameW float32 = 190
)

// renderAchievementDiffTable 在版本对比「成就」分类下复用成就页面一致的表格结构，直接展示分类、点数、达成条件、称号与奖励道具。
func (a *App) renderAchievementDiffTable(c *ui.Context) {
	t := themeOf(c)
	ui.Row(c).FillWidth().Padding(8, 16).Background(t.card).
		BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.t("achievements.col_id")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffAchColIDW).Shrink(0)
		ui.Text(c, a.t("achievements.col_name")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffAchColNameW).Shrink(0)
		ui.Text(c, a.t("achievements.col_category")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(achColCatW).Shrink(0)
		ui.Text(c, a.t("achievements.col_points")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(achColPointsW).Shrink(0).TextAlign(ui.Center)
		ui.Text(c, a.t("achievements.col_condition")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Grow(1).Shrink(1).MinWidth(achColCondMin)
		ui.Text(c, a.t("achievements.col_reward")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(achColRewardW).Shrink(0)
		ui.Box(c).Width(achColFavW).Shrink(0).Center().Children(func() {
			ui.Icon(c, icoStar).Size(iconSm, iconSm).TextColor(t.muted)
		})
	})

	if a.diffRes == nil || len(a.diffRes.Items) == 0 {
		kitEmpty(c, icoTrophy, a.t("diff.empty_title"), a.t("diff.empty_desc"), nil)
		return
	}

	ui.Scroll(c.Key("diff-ach-table-scroll")).TrackScroll(&a.diffScroll).Grow(1).Shrink(1).Children(func() {
		ui.Column(c).FillWidth().Selectable().Children(func() {
			for i, entry := range a.diffRes.Items {
				if i > 0 {
					kitSeparator(c, false).Margin(0, 16)
				}
				ach := diffEntryToAchievement(entry)
				row := ui.Row(c.Key(fmt.Sprintf("df-ach-row-%d", ach.ID))).FillWidth().Padding(10, 16).Gap(10).
					AlignItems(ui.Center).Transition(colorTransition(motionFast))
				if row.Hovered() {
					row.Background(t.tint(0.85))
				}
				row.Children(func() {
					// 1. ID 与变更类型徽章
					ui.Column(c).Width(diffAchColIDW).Shrink(0).AlignItems(ui.Start).Gap(3).Children(func() {
						numTextf(c, "#%d", ach.ID).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
						switch entry.ChangeType {
						case "added":
							drawPill(c, a.t("diff.change_added"), pillPositive)
						case "modified":
							drawPill(c, a.t("diff.change_modified"), pillWarning)
						default:
							drawPill(c, a.t("diff.change_removed"), pillNegative)
						}
					})

					// 2. 图标与成就名称（含副语言，非移除条目支持点击跳转成就页）
					ui.Row(c).Width(diffAchColNameW).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
						a.drawGameIcon(c, ach.IconID, false, 28, t.radius.md)
						ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
							ui.Text(c, ach.NameCn).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground).SingleLine()
							for _, sec := range a.secondaryDataLines(ach.NameCn, ach.NameJa, ach.NameEn) {
								ui.Text(c, sec.text).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
							}
						})
					})

					// 3. 分类
					ui.Column(c).Width(achColCatW).Shrink(0).Gap(1).Children(func() {
						if ach.KindName != "" {
							ui.Text(c, ach.KindName).FontSize(t.text.xs).TextColor(t.subdued).SingleLine()
						}
						if ach.CategoryName != "" {
							ui.Text(c, ach.CategoryName).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
						}
					})

					// 4. 点数
					numTextf(c, "%d", ach.Points).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).
						Width(achColPointsW).Shrink(0).TextAlign(ui.Center).SingleLine()

					// 5. 达成条件（变更条目同时附加字段变更明细）
					ui.Column(c).Grow(1).Shrink(1).MinWidth(achColCondMin).Gap(5).Children(func() {
						desc := ach.DescriptionCn
						if desc == "" {
							desc = "–"
						}
						ui.Text(c, desc).FontSize(t.text.xs).TextColor(t.subdued)
						if entry.ChangeType == "modified" && len(entry.Changes) > 0 {
							ui.Row(c).FillWidth().Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
								renderDiffFieldChanges(c, entry.Changes)
							})
						}
					})

					// 6. 奖励称号与奖励道具（直接复用成就页单元格，展示称号、副语言与可直跳道具按钮）
					a.renderAchievementRewardCell(c, ach)

					// 7. 星标收藏按钮（已移除条目不提供收藏）
					ui.Box(c).Width(achColFavW).Shrink(0).Center().Children(func() {
						if entry.ChangeType == "removed" {
							return
						}
						fav := a.isFavorite(config.RecordKindAchievement, ach.ID)
						tip := a.tf("favorites.add_tip", "name", ach.NameCn)
						if fav {
							tip = a.tf("favorites.remove_tip", "name", ach.NameCn)
						}
						if kitFavoriteStarIconButton(c, fmt.Sprintf("df-ach-fav-%d", ach.ID), tip, fav) {
							a.toggleFavorite(config.RecordKindAchievement, ach.ID)
						}
					})
				})
			}
		})
	})
}

// renderDiffNeedsDownloadView 在用户尚未下载任何 GitHub 历史快照时展示清晰引导与一键下载入口。
func (a *App) renderDiffNeedsDownloadView(c *ui.Context) {
	t := themeOf(c)
	ui.Column(c).Grow(1).FillWidth().Center().Padding(32, 24).Gap(14).Children(func() {
		ui.Icon(c, icoDownload).Size(24, 24).TextColor(t.idle)
		ui.Column(c).AlignItems(ui.Center).Gap(6).MaxWidth(440).Children(func() {
			ui.Text(c, a.t("diff.need_download_title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).TextAlign(ui.Center)
			ui.Text(c, a.t("diff.need_download_desc")).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.muted).TextAlign(ui.Center)
		})
		if a.snapMgrError != "" {
			ui.Box(c).MaxWidth(460).FillWidth().Children(func() {
				kitAlert(c, toneDanger, a.snapMgrError)
			})
		}
		if a.snapDownID != "" {
			ui.Box(c).MaxWidth(460).FillWidth().Children(func() {
				a.renderSnapshotDownloadProgressCard(c)
			})
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if kitButton(c, "diff-empty-open-mgr", a.t("diff.open_manager_btn"), btnOpts{variant: btnDefault, size: btnSizeMd, icon: icoDownload}) {
				a.openSnapshotManager()
			}
			refLbl := a.t("diff.snap_refresh_catalog")
			if a.snapCatalogBusy {
				refLbl = a.t("diff.snap_refreshing")
			}
			if kitButton(c, "diff-empty-refresh-cat", refLbl, btnOpts{variant: btnOutline, size: btnSizeMd, icon: icoRefresh, disabled: a.snapCatalogBusy}) {
				a.refreshSnapshotCatalog(true)
			}
		})
	})
}

const (
	snapMgrModalW      float32 = 660
	snapMgrModalH      float32 = 520
	snapMgrColSizeW    float32 = 96
	snapMgrColStatusW  float32 = 88
	snapMgrColActionsW float32 = 68
)

// renderSnapshotManagerModal 渲染 GitHub 统一版本快照下载与本地缓存管理弹窗（固定高度、原生表格布局，切换栏目不抖动）。
func (a *App) renderSnapshotManagerModal(c *ui.Context) {
	if !a.snapMgrOpen {
		return
	}
	t := themeOf(c)
	ui.DialogBase(c.Key("snap-mgr-modal"), &a.snapMgrOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(t.scrim)
		// 固定宽高并撑满内部纵向布局，确保切换全部/已下载/未下载或加载空态时弹窗高度始终恒定
		applyOverlaySurface(c, panel.Width(snapMgrModalW).Height(snapMgrModalH).Radius(t.radius.x2l).Clip(), shadowDialog)

		var allItems []backup.VersionBackupMeta
		if a.versionStatus != nil {
			allItems = a.versionStatus.RemoteVersions
		}
		var downloadedCnt int
		var downloadedBytes int64
		for _, it := range allItems {
			if it.Downloaded {
				downloadedCnt++
				downloadedBytes += it.Size
			}
		}

		ui.Column(c).Fill().Children(func() {
			// 1. 紧凑单行顶栏：标题 + 已识别客户端范围，右侧刷新与关闭
			ui.Row(c).FillWidth().Shrink(0).Padding(14, 20).
				BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
				Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					ui.Text(c, a.t("diff.snap_mgr_title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					if summary := a.detectedClientsSummaryText(); summary != "" {
						numText(c, a.tf("diff.snap_client_scope", "clients", summary)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
					}
				})
				ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
					refLbl := a.t("diff.snap_refresh_catalog")
					if a.snapCatalogBusy {
						refLbl = a.t("diff.snap_refreshing")
					}
					if kitButton(c, "snap-mgr-refresh", refLbl, btnOpts{variant: btnOutline, size: btnSizeSm, icon: icoRefresh, disabled: a.snapCatalogBusy}) {
						a.refreshSnapshotCatalog(true)
					}
					if kitIconButton(c, "snap-mgr-close", icoClose, a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
						a.snapMgrOpen = false
					}
				})
			})

			// 2. 筛选工具条：左侧分段切换（全部 / 已下载 / 未下载），右侧本地缓存汇总
			ui.Row(c).FillWidth().Shrink(0).Padding(10, 20).Gap(12).
				BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
				Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
				curTabIdx := 0
				for i, mode := range snapMgrFilterModes {
					if mode == a.snapMgrFilter {
						curTabIdx = i
						break
					}
				}
				tabLabels := []string{
					a.t("diff.snap_tab_all"),
					a.t("diff.snap_tab_downloaded"),
					a.t("diff.snap_tab_not_downloaded"),
				}
				if nextIdx, changed := kitSegmentedTabs(c, "snap-mgr-filter", curTabIdx, tabLabels); changed {
					a.snapMgrFilter = snapMgrFilterModes[nextIdx]
					a.snapMgrScroll.Y = 0
				}

				if len(allItems) > 0 {
					var storageText string
					if downloadedCnt > 0 {
						storageText = a.tf("diff.snap_storage_summary",
							"downloaded", formatInt(downloadedCnt),
							"total", formatInt(len(allItems)),
							"size", formatSnapshotSize(downloadedBytes),
						)
					} else {
						storageText = a.tf("diff.snap_storage_summary_empty", "total", formatInt(len(allItems)))
					}
					numText(c, storageText).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				}
			})

			// 3. 固定表头行（对齐版本、大小、状态与操作列）
			ui.Row(c).FillWidth().Shrink(0).Padding(7, 20).Gap(12).Background(t.card).
				BorderWidth(0, 0, hairline, 0).BorderColor(t.border).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.t("diff.snap_col_version")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Grow(1).Shrink(1).MinWidth(0)
				ui.Text(c, a.t("diff.snap_col_size")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(snapMgrColSizeW).Shrink(0).TextAlign(ui.End)
				ui.Text(c, a.t("diff.snap_col_status")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(snapMgrColStatusW).Shrink(0)
				ui.Text(c, a.t("diff.snap_col_actions")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(snapMgrColActionsW).Shrink(0).TextAlign(ui.End)
			})

			// 4. 固定伸缩主体区域：加载、空态与滚动列表共享同一容器高度
			ui.Box(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).Children(func() {
				if a.snapCatalogBusy && len(allItems) == 0 {
					kitLoading(c, a.t("diff.snap_refreshing"), "")
					return
				}
				if len(allItems) == 0 {
					kitEmpty(c, icoDownload, a.t("diff.snap_empty_catalog"), a.t("diff.snap_empty_catalog_desc"), nil)
					return
				}

				items := make([]backup.VersionBackupMeta, 0, len(allItems))
				for _, it := range allItems {
					switch a.snapMgrFilter {
					case "downloaded":
						if !it.Downloaded {
							continue
						}
					case "not_downloaded":
						if it.Downloaded {
							continue
						}
					}
					items = append(items, it)
				}
				if len(items) == 0 {
					emptyTitle := a.t("diff.snap_empty_filtered_downloaded")
					if a.snapMgrFilter == "not_downloaded" {
						emptyTitle = a.t("diff.snap_empty_filtered_not_downloaded")
					}
					kitEmpty(c, icoDownload, emptyTitle, "", nil)
					return
				}

				st := a.versionStatus
				ui.Scroll(c.Key("snap-mgr-scroll")).TrackScroll(&a.snapMgrScroll).Fill().Children(func() {
					ui.Column(c).FillWidth().Children(func() {
						for i, item := range items {
							if i > 0 {
								kitSeparator(c, false).Margin(0, 20)
							}
							sameAsClient := false
							canDownload := false
							if st != nil {
								if st.HasCN {
									if st.CNVersion != "" && item.CNVersion == st.CNVersion {
										sameAsClient = true
									}
									if item.HasRemoteCN {
										canDownload = true
									}
								}
								if st.HasGlobal {
									if st.GlobalVersion != "" && item.GlobalVersion == st.GlobalVersion {
										sameAsClient = true
									}
									if item.HasRemoteGlobal {
										canDownload = true
									}
								}
							}
							isDownloading := a.snapDownID == item.Patch

							row := ui.Row(c.Key("snap-item-"+item.Patch)).FillWidth().Padding(10, 20).Gap(12).
								AlignItems(ui.Center).Transition(colorTransition(motionFast))
							if row.Hovered() {
								row.Background(t.tint(0.85))
							}
							row.Children(func() {
								// 列 1：版本号、当前客户端同版标注与构建号/日期
								ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
									ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
										numText(c, item.DisplayName).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground).SingleLine()
										if sameAsClient {
											drawPill(c, a.t("diff.snap_same_as_client"), pillAccent)
										}
									})
									var subParts []string
									if item.Version != "" {
										subParts = append(subParts, item.Version)
									}
									if len(item.CreatedAt) >= 10 {
										subParts = append(subParts, item.CreatedAt[:10])
									}
									if len(subParts) > 0 {
										numText(c, strings.Join(subParts, " · ")).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
									}
								})

								// 列 2：快照体积（等宽右对齐）
								ui.Column(c).Width(snapMgrColSizeW).Shrink(0).AlignItems(ui.End).Gap(2).Children(func() {
									sizeStr := "–"
									if item.Size > 0 {
										sizeStr = formatSnapshotSize(item.Size)
									}
									numText(c, sizeStr).FontSize(t.text.xs).TextColor(t.subdued).SingleLine()
									if item.NeedsSupplement && item.DownloadSize > 0 {
										numText(c, a.tf("diff.snap_remaining_size", "size", formatSnapshotSize(item.DownloadSize))).
											FontSize(t.text.micro).TextColor(t.warningText).SingleLine()
									}
								})

								// 列 3：状态（克制状态点 + 文案，避免整列彩色胶囊堆砌）
								ui.Row(c).Width(snapMgrColStatusW).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
									switch {
									case item.Downloaded:
										drawStatusDot(c, t.success)
										ui.Text(c, a.t("diff.snap_downloaded")).FontSize(t.text.xs).TextColor(t.successText).SingleLine()
									case item.NeedsSupplement:
										drawStatusDot(c, t.attention)
										ui.Text(c, a.t("diff.snap_needs_supplement")).FontSize(t.text.xs).TextColor(t.warningText).SingleLine()
									default:
										drawStatusDot(c, t.idle)
										ui.Text(c, a.t("diff.snap_status_not_downloaded")).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
									}
								})

								// 列 4：行级操作（克制描边图标下载按钮 + 红色删除图标按钮）
								ui.Row(c).Width(snapMgrColActionsW).Shrink(0).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
									if item.Downloaded {
										if kitIconButton(c, "snap-del-"+item.Patch, icoTrash, a.t("diff.snap_delete"), btnOpts{
											variant:  btnDanger,
											size:     btnSizeSm,
											disabled: a.snapDownID != "",
										}) {
											a.deleteLocalSnapshot(item.Patch, item.DisplayName)
										}
									} else if item.NeedsSupplement {
										dlTip := a.t("diff.snap_supplement")
										if isDownloading {
											dlTip = a.t("diff.snap_downloading")
										}
										if kitIconButton(c, "snap-dl-"+item.Patch, icoDownload, dlTip, btnOpts{
											variant:  btnOutline,
											size:     btnSizeSm,
											disabled: a.snapDownID != "" || !canDownload,
										}) {
											a.startSnapshotDownload(item.Patch, item.DisplayName)
										}
										if kitIconButton(c, "snap-del-"+item.Patch, icoTrash, a.t("diff.snap_delete"), btnOpts{
											variant:  btnDanger,
											size:     btnSizeSm,
											disabled: a.snapDownID != "",
										}) {
											a.deleteLocalSnapshot(item.Patch, item.DisplayName)
										}
									} else {
										dlTip := a.t("diff.snap_download")
										if isDownloading {
											dlTip = a.t("diff.snap_downloading")
										}
										if kitIconButton(c, "snap-dl-"+item.Patch, icoDownload, dlTip, btnOpts{
											variant:  btnOutline,
											size:     btnSizeSm,
											disabled: a.snapDownID != "" || !canDownload,
										}) {
											a.startSnapshotDownload(item.Patch, item.DisplayName)
										}
									}
								})
							})
						}
					})
				})
			})

			// 5. 固定底栏：统一承载下载进度、错误/完成反馈或静态说明与关闭按钮，避免插入横幅导致列表跳动
			ui.Row(c).FillWidth().Shrink(0).Padding(10, 20).Gap(12).Background(t.sidebar).
				BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).
				Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					switch {
					case a.snapDownID != "":
						a.renderSnapshotFooterDownloadStatus(c)
					case a.snapMgrError != "":
						ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, icoAlertCircle).Size(iconSm, iconSm).Shrink(0).TextColor(t.danger)
							ui.Text(c, a.snapMgrError).FontSize(t.text.xs).TextColor(t.dangerText).SingleLine().Grow(1).Shrink(1)
						})
					case a.snapMgrNotice != "":
						ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
							drawStatusDot(c, t.success)
							ui.Text(c, a.snapMgrNotice).FontSize(t.text.xs).TextColor(t.successText).SingleLine().Grow(1).Shrink(1)
						})
					default:
						ui.Text(c, a.t("diff.snap_footer_hint")).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
					}
				})
				ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.snapDownID != "" {
						if kitButton(c, "snap-dl-cancel-footer", a.t("diff.snap_cancel_download"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.cancelSnapshotDownload()
						}
					}
					if kitButton(c, "snap-mgr-done", a.t("common.close"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
						a.snapMgrOpen = false
					}
				})
			})
		})
	})
}

func (a *App) renderSnapshotFooterDownloadStatus(c *ui.Context) {
	t := themeOf(c)
	var pct float32
	if a.snapDownTotal > 0 {
		pct = min(max(float32(a.snapDownBytes)*100/float32(a.snapDownTotal), 2), 100)
	} else {
		pct = 15
	}
	sizeInfo := formatSnapshotSize(a.snapDownBytes)
	if a.snapDownTotal > 0 {
		sizeInfo = fmt.Sprintf("%s / %s (%.0f%%)", formatSnapshotSize(a.snapDownBytes), formatSnapshotSize(a.snapDownTotal), pct)
	}
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Spinner(c).FontSize(12).TextColor(t.primary)
				ui.Text(c, a.tf("diff.snap_downloading_title", "name", a.snapDownName)).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
			})
			numText(c, sizeInfo).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
		})
		kitProgressBar(c, pct)
	})
}

func (a *App) detectedClientsSummaryText() string {
	if a.versionStatus == nil {
		return ""
	}
	var parts []string
	if a.versionStatus.HasCN {
		ver := a.versionStatus.CNPatch
		if ver == "" {
			ver = a.versionStatus.CNVersion
		}
		if ver == "" {
			ver = a.t("diff.live_label")
		}
		parts = append(parts, a.tf("diff.snap_client_item", "region", a.t("shell.server_cn"), "ver", ver))
	}
	if a.versionStatus.HasGlobal {
		ver := a.versionStatus.GlobalPatch
		if ver == "" {
			ver = a.versionStatus.GlobalVersion
		}
		if ver == "" {
			ver = a.t("diff.live_label")
		}
		parts = append(parts, a.tf("diff.snap_client_item", "region", a.t("shell.server_global"), "ver", ver))
	}
	return strings.Join(parts, " · ")
}

func (a *App) renderSnapshotDownloadProgressCard(c *ui.Context) {
	t := themeOf(c)
	var pct float32
	if a.snapDownTotal > 0 {
		pct = min(max(float32(a.snapDownBytes)*100/float32(a.snapDownTotal), 2), 100)
	} else {
		pct = 15
	}
	sizeInfo := formatSnapshotSize(a.snapDownBytes)
	if a.snapDownTotal > 0 {
		sizeInfo = fmt.Sprintf("%s / %s (%.0f%%)", formatSnapshotSize(a.snapDownBytes), formatSnapshotSize(a.snapDownTotal), pct)
	}

	kitBlock(c, ui.Column(c).FillWidth().Padding(10, 12).Gap(8), toneCard).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Spinner(c).FontSize(14).TextColor(t.primary)
				ui.Text(c, a.tf("diff.snap_downloading_title", "name", a.snapDownName)).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
				numText(c, sizeInfo).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
				if kitButton(c, "snap-dl-cancel", a.t("diff.snap_cancel_download"), btnOpts{variant: btnGhost, size: btnSizeXs}) {
					a.cancelSnapshotDownload()
				}
			})
		})
		kitProgressBar(c, pct)
	})
}

// renderDiffFieldChanges 绘制结构化字段前后变更明细（供通用实体与成就表格复用）。
func renderDiffFieldChanges(c *ui.Context, changes []backup.ItemFieldChange) {
	t := themeOf(c)
	for _, chg := range changes {
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
}

func formatSnapshotSize(sz int64) string {
	switch {
	case sz >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(sz)/(1024*1024))
	case sz >= 1024:
		return fmt.Sprintf("%.0f KB", float64(sz)/1024)
	case sz > 0:
		return fmt.Sprintf("%d B", sz)
	default:
		return "0 B"
	}
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
	curLabel := ""
	for _, b := range backups {
		if b.ID == *val {
			curLabel = b.DisplayName
			break
		}
	}
	if curLabel == "" {
		if *val != "" && *val != backup.LiveVersionID {
			curLabel = *val
		} else {
			curLabel = a.t("diff.select_snapshot")
		}
	}

	trig := sel.Trigger.Height(28).Width(204).Padding(0, 8, 0, 10).Radius(t.radius.md).
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
		applyOverlaySurface(c, panel.Margin(4, 0, 0, 0).Padding(4).Radius(t.radius.lg).MaxHeight(280).Width(284), shadowOverlay)
		ui.Scroll(c.Key("vsel-scr-" + key)).Shrink(1).Children(func() {
			for _, b := range backups {
				isCur := b.ID == *val
				row := sel.Item(b.ID).MinHeight(34).Padding(4, 8).Radius(t.radius.md).
					Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6)
				if row.Highlighted() {
					row.Background(t.tint(1.25))
				}
				row.Children(func() {
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							numText(c, b.DisplayName).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
							if b.IsLive {
								drawPill(c, a.t("diff.snap_current_client_tag"), pillPositive)
							}
						})
						if b.Version != "" {
							numText(c, b.Version).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
						}
					})
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
