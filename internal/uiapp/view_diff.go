package uiapp

import (
	"fmt"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/egoist/mygo/ui"
)

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
					drawCompactDiffStat(c, a.t("diff.stat_added_bgms"), "+"+formatInt(sum.AddedBGMs), t.successText)
					drawCompactDiffStat(c, a.t("diff.stat_modified_bgms"), formatInt(sum.ModifiedBGMs), t.warningText)
				})
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
			ui.Column(c).Grow(1).FillWidth().Center().Padding(32, 24).Gap(10).Children(func() {
				ui.Spinner(c).FontSize(22).TextColor(t.primary)
				ui.Text(c, a.t("diff.computing_title")).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
				ui.Text(c, a.t("diff.computing_desc")).FontSize(t.text.xs).TextColor(t.muted).TextAlign(ui.Center).MaxWidth(420)
			})
			return
		}

		// 3 & 4. 图标与文本独立分区使用专属视图，常规实体使用四列表格
		if a.diffEntity == "icon" {
			if a.diffRes == nil || len(a.diffRes.Items) == 0 {
				kitEmpty(c, icoImage, a.t("diff.empty_title"), a.t("diff.empty_desc"), nil)
			} else {
				a.renderIconDiffPartition(c)
			}
		} else if a.diffEntity == "text" {
			a.renderTextDiffPartition(c)
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

// renderDiffNeedsDownloadView 在用户尚未下载任何 GitHub 历史快照时展示清晰引导与一键下载入口。
func (a *App) renderDiffNeedsDownloadView(c *ui.Context) {
	t := themeOf(c)
	ui.Column(c).Grow(1).FillWidth().Center().Padding(32, 24).Gap(16).Children(func() {
		ui.Box(c).Size(48, 48).Radius(t.radius.xl).Background(t.tint(1.2)).Border(hairline, t.border).Center().Children(func() {
			ui.Icon(c, icoDownload).Size(22, 22).TextColor(t.primary)
		})
		ui.Column(c).AlignItems(ui.Center).Gap(6).MaxWidth(460).Children(func() {
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

// renderSnapshotManagerModal 渲染 GitHub 统一版本快照下载与本地缓存管理弹窗（按版本号统一下载，不区分国服/国际服）。
func (a *App) renderSnapshotManagerModal(c *ui.Context) {
	if !a.snapMgrOpen {
		return
	}
	t := themeOf(c)
	ui.DialogBase(c.Key("snap-mgr-modal"), &a.snapMgrOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(t.scrim)
		applyOverlaySurface(c, panel.Width(640).MaxHeight(580).Radius(t.radius.x2l).Clip(), shadowDialog)

		// 1. 弹窗标题栏
		ui.Row(c).FillWidth().Padding(16, 20, 12, 20).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Start).Gap(12).Children(func() {
			ui.Column(c).Gap(3).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, a.t("diff.snap_mgr_title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				ui.Text(c, a.t("diff.snap_mgr_subtitle")).FontSize(t.text.xs).TextColor(t.muted)
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

		// 2. 已识别的本地客户端状态、下载进度与提示
		ui.Column(c).FillWidth().Padding(10, 20).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			if summary := a.detectedClientsSummaryText(); summary != "" {
				numText(c, a.tf("diff.snap_client_scope", "clients", summary)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
			}
			if a.snapMgrError != "" {
				kitAlert(c, toneDanger, a.snapMgrError)
			} else if a.snapMgrNotice != "" {
				kitAlert(c, toneSuccess, a.snapMgrNotice)
			}
			if a.snapDownID != "" {
				a.renderSnapshotDownloadProgressCard(c)
			}
		})

		// 3. 统一版本条目列表（每个版本号仅呈现 1 行，自动按已配置的客户端下载所需区服数据）
		var items []backup.VersionBackupMeta
		if a.versionStatus != nil {
			items = a.versionStatus.RemoteVersions
		}
		if a.snapCatalogBusy && len(items) == 0 {
			ui.Column(c).FillWidth().Height(220).Center().Gap(10).Children(func() {
				ui.Spinner(c).FontSize(20).TextColor(t.primary)
				ui.Text(c, a.t("diff.snap_refreshing")).FontSize(t.text.xs).TextColor(t.muted)
			})
			return
		}
		if len(items) == 0 {
			ui.Box(c).FillWidth().Height(220).Children(func() {
				kitEmpty(c, icoDownload, a.t("diff.snap_empty_catalog"), a.t("diff.snap_empty_catalog_desc"), nil)
			})
			return
		}

		st := a.versionStatus
		ui.Scroll(c.Key("snap-mgr-scroll")).TrackScroll(&a.snapMgrScroll).MaxHeight(340).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(8, 20, 14, 20).Gap(8).Children(func() {
				for _, item := range items {
					sameAsClient := false
					canDownload := false
					hasAnyLocal := false
					if st != nil {
						if st.HasCN {
							if st.CNVersion != "" && item.CNVersion == st.CNVersion {
								sameAsClient = true
							}
							if item.HasRemoteCN {
								canDownload = true
							}
							if item.DownloadedCN {
								hasAnyLocal = true
							}
						}
						if st.HasGlobal {
							if st.GlobalVersion != "" && item.GlobalVersion == st.GlobalVersion {
								sameAsClient = true
							}
							if item.HasRemoteGlobal {
								canDownload = true
							}
							if item.DownloadedGlobal {
								hasAnyLocal = true
							}
						}
					}
					isDownloading := a.snapDownID == item.Patch
					kitBlock(c, ui.Row(c.Key("snap-item-"+item.Patch)).FillWidth().Padding(10, 12).Gap(10).
						Justify(ui.SpaceBetween).AlignItems(ui.Center), toneCard).Children(func() {
						ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
							ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
								numText(c, item.DisplayName).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground).SingleLine()
								if item.Downloaded {
									drawPill(c, a.t("diff.snap_downloaded"), pillPositive)
								} else if item.NeedsSupplement {
									drawPill(c, a.t("diff.snap_needs_supplement"), pillWarning)
								}
								if sameAsClient {
									drawPill(c, a.t("diff.snap_same_as_client"), pillAccent)
								}
							})
							var metaParts []string
							if item.Version != "" {
								metaParts = append(metaParts, a.tf("diff.snap_meta_build", "ver", item.Version))
							}
							if item.Size > 0 {
								metaParts = append(metaParts, formatSnapshotSize(item.Size))
							}
							if item.NeedsSupplement && item.DownloadSize > 0 {
								metaParts = append(metaParts, a.tf("diff.snap_remaining_size", "size", formatSnapshotSize(item.DownloadSize)))
							}
							if len(item.CreatedAt) >= 10 {
								metaParts = append(metaParts, item.CreatedAt[:10])
							}
							numText(c, strings.Join(metaParts, " · ")).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
						})

						ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
							if item.Downloaded {
								if kitButton(c, "snap-use-"+item.Patch, a.t("diff.snap_use_compare"), btnOpts{variant: btnOutline, size: btnSizeSm, disabled: !hasAnyLocal}) {
									a.selectSnapshotAsFrom(item.Patch)
								}
								if kitButton(c, "snap-del-"+item.Patch, a.t("diff.snap_delete"), btnOpts{variant: btnGhost, size: btnSizeSm, disabled: a.snapDownID != ""}) {
									a.deleteLocalSnapshot(item.Patch, item.DisplayName)
								}
							} else if item.NeedsSupplement {
								btnLbl := a.t("diff.snap_supplement")
								if isDownloading {
									btnLbl = a.t("diff.snap_downloading")
								}
								if kitButton(c, "snap-dl-"+item.Patch, btnLbl, btnOpts{
									variant:  btnDefault,
									size:     btnSizeSm,
									icon:     icoDownload,
									disabled: a.snapDownID != "" || !canDownload,
								}) {
									a.startSnapshotDownload(item.Patch, item.DisplayName)
								}
								if hasAnyLocal {
									if kitButton(c, "snap-use-"+item.Patch, a.t("diff.snap_use_compare"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
										a.selectSnapshotAsFrom(item.Patch)
									}
								}
								if kitButton(c, "snap-del-"+item.Patch, a.t("diff.snap_delete"), btnOpts{variant: btnGhost, size: btnSizeSm, disabled: a.snapDownID != ""}) {
									a.deleteLocalSnapshot(item.Patch, item.DisplayName)
								}
							} else {
								btnLbl := a.t("diff.snap_download")
								if isDownloading {
									btnLbl = a.t("diff.snap_downloading")
								}
								if kitButton(c, "snap-dl-"+item.Patch, btnLbl, btnOpts{
									variant:  btnDefault,
									size:     btnSizeSm,
									icon:     icoDownload,
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
		ui.Row(c).FillWidth().Height(4).Radius(t.radius.full).Background(t.border).Clip().AlignItems(ui.Stretch).Children(func() {
			ui.Box(c).Grow(pct).FillHeight().Background(t.primary)
			ui.Box(c).Grow(max(0, 100-pct)).FillHeight()
		})
	})
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
