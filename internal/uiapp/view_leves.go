package uiapp

import (
	"fmt"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

func (a *App) leveScopeFilters() []itemScopeDef {
	return []itemScopeDef{
		{mode: "all", label: a.t("leves.scope_all")},
		{mode: "craft", label: a.t("leves.scope_craft")},
		{mode: "gathering", label: a.t("leves.scope_gathering")},
		{mode: "battle", label: a.t("leves.scope_battle")},
		{mode: "company", label: a.t("leves.scope_company")},
	}
}

func (a *App) renderLevesView(c *ui.Context) {
	kitResizer(c, "leve-split", &a.listW, itemListMinWidth, itemListMaxWidth, 8, a.t("shell.resize_leve_list"),
		func() { a.renderLeveListPane(c) },
		func() { a.renderLeveDetailPane(c) },
	)
}

func (a *App) hasLeveFilter() bool {
	return strings.TrimSpace(a.leveQuery.Q) != "" ||
		a.leveQuery.Kind != "all" ||
		a.leveQuery.AssignmentTypeID > 0 ||
		a.leveQuery.NewOnly
}

func (a *App) resetLeveFilters() {
	a.leveQuery = gamedata.LeveSearchQuery{
		Kind:     "all",
		Page:     1,
		PageSize: 30,
	}
	a.refreshLeves()
}

func (a *App) renderLeveListPane(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(10, 12, 10, 12).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Baseline).Grow(1).Shrink(1).Children(func() {
					ui.Text(c, a.t("leves.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					numText(c, a.tf("common.items_count", "count", formatInt(a.leveRes.Total))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				})
				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.hasLeveFilter() {
						if kitButton(c, "leve-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.resetLeveFilters()
						}
					}
					typeOpts := []selectOption[int]{{value: 0, label: a.t("leves.all_roles")}}
					if a.status != nil {
						for _, at := range a.status.LeveAssignmentTypes {
							typeOpts = append(typeOpts, selectOption[int]{
								value: at.ID,
								label: fmt.Sprintf("%s (%d)", at.Name, at.Count),
							})
						}
					}
					if kitSelect(c, "leve-assign-select", &a.leveQuery.AssignmentTypeID, typeOpts, 136, 200, true) {
						a.leveQuery.Page = 1
						a.refreshLeves()
					}
				})
			})

			// 页面内置搜索栏
			a.renderPageSearchInput(c, "leve-page-search", &a.leveQuery.Q, a.t("search.leve_placeholder"), a.t("search.leve_label"), func() {
				a.leveQuery.Page = 1
				a.refreshLeves()
			})

			ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
				for _, f := range a.leveScopeFilters() {
					active := a.leveQuery.Kind == f.mode
					if kitToggleChip(c, "l-scope-"+f.mode, f.label, "", active, nil) {
						a.leveQuery.Kind = f.mode
						a.leveQuery.Page = 1
						a.refreshLeves()
					}
				}
				if a.renderLatestFilterChip(c, "l-scope-latest-new", a.latestLeveCount(), a.leveQuery.NewOnly) {
					a.leveQuery.NewOnly = !a.leveQuery.NewOnly
					a.leveQuery.Page = 1
					a.refreshLeves()
				}
			})
		})

		if len(a.leveRes.Items) == 0 {
			kitEmpty(c, icoClipboard, a.t("leves.empty_title"), a.t("leves.empty_desc"), func() {
				if kitButton(c, "leve-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.resetLeveFilters()
				}
			})
		} else {
			ui.Scroll(c.Key("leve-list-scroll")).TrackScroll(&a.leveListScroll).Grow(1).Shrink(1).Children(func() {
				ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
					for _, lv := range a.leveRes.Items {
						isSel := lv.ID == a.selectedLeveID
						fav := a.isFavorite(config.RecordKindLeve, lv.ID)
						if a.renderLeveListRow(c, fmt.Sprintf("leve-row-%d", lv.ID), lv, isSel, fav, &a.scrollLeveListToID) {
							a.selectLeve(lv.ID)
						}
					}
				})
			})
		}

		a.renderPaginationBar(
			c,
			"leve-pg",
			a.leveRes.Page,
			a.leveRes.TotalPages,
			&a.levePageStr,
			func(next int) {
				a.leveQuery.Page = next
				a.refreshLeves()
			},
		)
	})
}

func leveKindPillKind(kind string) pillKind {
	switch kind {
	case "craft":
		return pillWarning
	case "gathering":
		return pillPositive
	case "company":
		return pillAccent
	default:
		return pillNeutral
	}
}

// renderLeveListRow 绘制单条理符列表行（供「理符」页面与「收藏」理符标签页复用）。
func (a *App) renderLeveListRow(c *ui.Context, key string, lv gamedata.LeveEntry, isSel, favorited bool, scrollToID *int) bool {
	t := themeOf(c)
	row := ui.ButtonBase(c.Key(key)).
		FillWidth().Padding(8, 10).Gap(10).Radius(t.radius.lg).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
	switch {
	case isSel && row.Hovered():
		row.Background(t.tint(2.25))
	case isSel:
		row.Background(t.tint(1.75))
	case row.Hovered():
		row.Background(t.tint(1))
	}
	iconID := lv.AssignmentTypeIconID
	if iconID <= 0 {
		iconID = lv.IconCityState
	}
	row.Children(func() {
		a.drawGameIcon(c, iconID, false, 32, t.radius.md)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
				fw := 500
				if isSel {
					fw = 600
				}
				ui.Text(c, lv.NameCn).FontSize(t.text.sm).FontWeight(fw).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.isNewLeve(lv.ID) {
						a.drawLatestBadge(c)
					}
					if favorited {
						ui.Icon(c, icoStarFill).Size(11, 11).TextColor(t.attention)
					}
					numTextf(c, "#%d", lv.ID).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
				})
			})
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
				sub := nonEmptyStr(lv.AssignmentTypeName, nonEmptyStr(lv.ClassJobCategoryName, lv.LeveKindLabel))
				ui.Text(c, sub).FontSize(t.text.xs).TextColor(t.muted).SingleLine().Grow(1).Shrink(1)
				meta := ""
				if lv.ClassJobLevel > 0 {
					meta = fmt.Sprintf("Lv.%d", lv.ClassJobLevel)
				}
				numText(c, meta).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
			})
		})
	})
	if scrollToID != nil && lv.ID == *scrollToID {
		row.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter})
		*scrollToID = 0
	}
	return row.Clicked()
}

type leveDetailPaneOpts struct {
	keyPrefix      string
	showOpenInMain bool
	leve           *gamedata.LeveEntry
	scroll         *ui.ScrollState
	onJumpItem     func(int)
}

func (a *App) renderLeveDetailPane(c *ui.Context) {
	a.renderLeveDetailWithOpts(c, leveDetailPaneOpts{
		keyPrefix:  "wb-leve",
		leve:       a.selectedLeve,
		scroll:     &a.leveDetailScroll,
		onJumpItem: a.inspectItemFromOtherTab,
	})
}

func (a *App) renderLeveDetailWithOpts(c *ui.Context, opts leveDetailPaneOpts) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Grow(1).Shrink(1).FillHeight().Children(func() {
		lv := opts.leve
		if lv == nil {
			kitEmpty(c, icoClipboard, a.t("leves.detail_empty_title"), a.t("leves.detail_empty_desc"), nil)
			return
		}

		onJumpItem := opts.onJumpItem
		if onJumpItem == nil {
			onJumpItem = a.inspectItemFromOtherTab
		}
		scroll := opts.scroll
		if scroll == nil {
			scroll = &a.leveDetailScroll
		}

		ui.Row(c).FillWidth().Height(42).Padding(0, 16).Gap(10).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				numText(c, a.tf("leves.id_header", "id", fmt.Sprintf("%d", lv.ID))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				if opts.showOpenInMain {
					if kitIconButton(c, opts.keyPrefix+"-open-main", icoExternalLink, a.t("recent.open_in_leves"), btnOpts{variant: btnGhost, size: btnSizeXs}) {
						a.inspectLeveFromOtherTab(lv.ID)
					}
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
				fav := a.isFavorite(config.RecordKindLeve, lv.ID)
				if kitFavoriteToggleButton(c, opts.keyPrefix+"-fav-btn", a.t("favorites.add_btn"), a.t("favorites.added_btn"), fav) {
					a.toggleFavorite(config.RecordKindLeve, lv.ID)
				}
				drawPill(c, lv.LeveKindLabel, leveKindPillKind(lv.LeveKind))
			})
		})

		ui.Scroll(c.Key(opts.keyPrefix + "-scroll")).TrackScroll(scroll).Grow(1).Shrink(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(20, 24, 32, 24).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(880).Gap(20).Children(func() {
					iconID := lv.AssignmentTypeIconID
					if iconID <= 0 {
						iconID = lv.IconCityState
					}
					ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Start).Children(func() {
						a.drawGameIcon(c, iconID, false, 52, t.radius.xl)
						ui.Column(c).Gap(6).Grow(1).Shrink(1).MinWidth(0).Children(func() {
							ui.Row(c).Gap(8).AlignItems(ui.Baseline).Wrap().Children(func() {
								ui.Text(c, lv.NameCn).FontSize(t.text.xl).FontWeight(600).LetterSpacing(-0.3).TextColor(t.foreground).Selectable()
								if a.isNewLeve(lv.ID) {
									a.drawLatestBadge(c)
								}
								numTextf(c, "#%d", lv.ID).FontSize(t.text.xs).TextColor(t.muted).Selectable()
							})
							if secLines := a.secondaryDataLines(lv.NameCn, lv.NameJa, lv.NameEn); len(secLines) > 0 {
								ui.Row(c).Gap(12).Wrap().Children(func() {
									for _, sec := range secLines {
										ui.Textf(c, "%s  %s", sec.badge, sec.text).FontSize(t.text.xs).TextColor(t.muted).Selectable()
									}
								})
							}
							var meta []string
							meta = append(meta, lv.AssignmentTypeName)
							if lv.ClassJobLevel > 0 {
								meta = append(meta, fmt.Sprintf("Lv.%d", lv.ClassJobLevel))
							}
							if lv.ClassJobCategoryName != "" {
								meta = append(meta, lv.ClassJobCategoryName)
							}
							if lv.TownName != "" {
								meta = append(meta, a.tf("leves.town", "name", lv.TownName))
							}
							if lv.ClientName != "" {
								meta = append(meta, a.tf("leves.client", "name", lv.ClientName))
							}
							numText(c, strings.Join(meta, " · ")).FontSize(t.text.xs).TextColor(t.subdued)
						})
					})

					if lv.DescriptionCn != "" {
						kitBlock(c, ui.Box(c).FillWidth().Padding(10, 14), toneCard).Children(func() {
							ui.Text(c, lv.DescriptionCn).FontSize(t.text.sm).LineHeight(lineProse).TextColor(t.subdued).Selectable()
						})
					}

					expStr := a.tf("common.exp_amount", "amount", formatInt(lv.ExpReward))
					if lv.ExpReward <= 0 {
						expStr = a.t("common.none")
					}
					gilStr := a.tf("common.gil_amount", "amount", formatInt(lv.GilReward))
					if lv.GilReward <= 0 {
						gilStr = a.t("common.none")
					}
					limitStr := a.tf("leves.allowance", "count", fmt.Sprintf("%d", max(1, lv.AllowanceCost)))
					if lv.TimeLimit > 0 {
						limitStr += a.tf("leves.time_limit", "min", fmt.Sprintf("%d", lv.TimeLimit))
					}
					kitBlock(c, ui.Row(c).FillWidth().Clip(), toneCard).Children(func() {
						drawMetricSplitCell(c, a.t("leves.base_exp"), expStr, false)
						drawMetricSplitCell(c, a.t("leves.gil_reward"), gilStr, true)
						drawMetricSplitCell(c, a.t("leves.cost_limit"), limitStr, true)
					})

					// 接取与执行地点
					if lv.PlaceNameIssued != "" || lv.PlaceNameStart != "" || lv.LevemeteLocation != nil || lv.StartLocation != nil || lv.GatheringTargetLocation != nil {
						drawDetailSection(c, a.t("leves.locations_title"), "", nil, func() {
							ui.Column(c).FillWidth().Gap(8).Children(func() {
								if lv.PlaceNameIssued != "" || lv.LevemeteLocation != nil {
									ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
										ui.Text(c, a.tf("leves.issued_at", "name", nonEmptyStr(lv.PlaceNameIssued, a.t("leves.issued_fallback")))).FontSize(t.text.xs).TextColor(t.foreground)
										if lv.LevemeteLocation != nil {
											a.renderMapLocationBtn(c, fmt.Sprintf("lv-issued-map-%d", lv.ID), lv.NameCn, a.t("leves.map_issued_sub"), lv.LevemeteLocation)
										}
									})
								}
								if lv.PlaceNameStart != "" || lv.StartLocation != nil {
									ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
										zone := lv.PlaceNameStart
										if lv.PlaceNameStartZone != "" && lv.PlaceNameStartZone != zone {
											zone += " · " + lv.PlaceNameStartZone
										}
										ui.Text(c, a.tf("leves.target_zone", "name", nonEmptyStr(zone, a.t("leves.start_fallback")))).FontSize(t.text.xs).TextColor(t.foreground)
										if lv.StartLocation != nil {
											a.renderMapLocationBtn(c, fmt.Sprintf("lv-start-map-%d", lv.ID), lv.NameCn, a.t("leves.map_start_sub"), lv.StartLocation)
										}
									})
								}
								if lv.GatheringTargetLocation != nil {
									ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
										ui.Text(c, a.t("leves.gather_area")).FontSize(t.text.xs).TextColor(t.foreground)
										a.renderMapLocationBtn(c, fmt.Sprintf("lv-gath-map-%d", lv.ID), lv.NameCn, a.t("leves.map_gather_sub"), lv.GatheringTargetLocation)
									})
								}
							})
						})
					}

					// 制作理符交纳物品
					if len(lv.TurnInItems) > 0 {
						meta := ""
						if lv.CraftRepeats > 0 {
							meta = a.tf("leves.turnin_repeats", "repeats", fmt.Sprintf("%d", lv.CraftRepeats), "total", fmt.Sprintf("%d", 1+lv.CraftRepeats))
						}
						drawDetailSection(c, a.t("leves.turnin_title"), meta, nil, func() {
							ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
								for idx, ti := range lv.TurnInItems {
									btn := kitLinkSurface(c, ui.ButtonBase(c.Key(fmt.Sprintf("lv-turn-%d-%d", lv.ID, idx))).Label(a.tf("common.view_item", "name", ti.ItemName)).
										MinWidth(240).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
										Justify(ui.SpaceBetween).AlignItems(ui.Center))
									btn.Children(func() {
										ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
											a.drawGameIcon(c, ti.IconID, false, 26, t.radius.md)
											ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
												ui.Text(c, ti.ItemName).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
												if lv.CraftRepeats > 0 {
													numText(c, a.tf("leves.turnin_round_total", "per", fmt.Sprintf("%d", ti.Count), "total", fmt.Sprintf("%d", ti.Count*(1+lv.CraftRepeats)))).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
												}
											})
										})
										numTextf(c, "×%d", ti.Count).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine().Shrink(0)
									})
									if btn.Clicked() {
										onJumpItem(ti.ItemID)
									}
								}
							})
						})
					}

					// 采集/佣兵/军队目标明细
					hasDetails := lv.RuleName != "" || lv.FishingSpotName != "" || lv.SpearfishingPlaceName != "" ||
						len(lv.Objectives) > 0 || len(lv.CombatTargets) > 0 || len(lv.RequiredObjectiveItems) > 0
					if hasDetails {
						drawDetailSection(c, a.t("leves.rules_title"), lv.RuleName, nil, func() {
							ui.Column(c).FillWidth().Gap(8).Children(func() {
								if lv.FishingSpotName != "" {
									ui.Text(c, a.tf("leves.fishing_spot", "name", lv.FishingSpotName)).FontSize(t.text.xs).TextColor(t.subdued)
								}
								if lv.SpearfishingPlaceName != "" {
									ui.Text(c, a.tf("leves.spearfishing_spot", "name", lv.SpearfishingPlaceName)).FontSize(t.text.xs).TextColor(t.subdued)
								}
								for _, obj := range lv.Objectives {
									ui.Text(c, "• "+obj).FontSize(t.text.xs).TextColor(t.subdued)
								}
								if len(lv.CombatTargets) > 0 {
									ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
										for idx, ct := range lv.CombatTargets {
											ui.Row(c.Key(fmt.Sprintf("lv-bnpc-%d-%d", lv.ID, idx))).Padding(5, 9).Radius(t.radius.md).
												Background(t.card).Gap(6).AlignItems(ui.Center).Children(func() {
												ui.Text(c, ct.BNpcName).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground)
												if ct.Level > 0 {
													numTextf(c, "Lv.%d", ct.Level).FontSize(t.text.micro).TextColor(t.muted)
												}
												if ct.Count > 0 {
													numTextf(c, "×%d", ct.Count).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText)
												}
											})
										}
									})
								}
								if len(lv.RequiredObjectiveItems) > 0 {
									ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
										for idx, oi := range lv.RequiredObjectiveItems {
											ui.Row(c.Key(fmt.Sprintf("lv-objitem-%d-%d", lv.ID, idx))).Padding(5, 9).Radius(t.radius.md).
												Background(t.card).Gap(6).AlignItems(ui.Center).Children(func() {
												ui.Text(c, oi.ItemName).FontSize(t.text.xs).TextColor(t.foreground)
												numTextf(c, "×%d", oi.Count).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText)
												if oi.DropRate > 0 {
													numTextf(c, "%d%%", oi.DropRate).FontSize(t.text.micro).TextColor(t.muted)
												}
											})
										}
									})
								}
							})
						})
					}

					// 概率奖励道具池
					if len(lv.RewardGroups) > 0 {
						drawDetailSection(c, a.t("leves.reward_pool_title"), a.tf("leves.reward_pool_meta", "count", fmt.Sprintf("%d", len(lv.RewardGroups))), nil, func() {
							ui.Column(c).FillWidth().Gap(10).Children(func() {
								for gIdx, grp := range lv.RewardGroups {
									if gIdx > 0 {
										kitSeparator(c, false)
									}
									ui.Column(c.Key(fmt.Sprintf("lv-rgrp-%d-%d", lv.ID, gIdx))).FillWidth().Gap(6).Children(func() {
										numText(c, a.tf("leves.drop_prob", "pct", fmt.Sprintf("%d", grp.ProbabilityPercent))).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground)
										ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
											for iIdx, ri := range grp.Items {
												btn := kitLinkSurface(c, ui.ButtonBase(c.Key(fmt.Sprintf("lv-ritem-%d-%d-%d", lv.ID, gIdx, iIdx))).Label(a.tf("common.view_item", "name", ri.ItemName)).
													Padding(4, 8).Radius(t.radius.md))
												btn.Children(func() {
													ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
														a.drawGameIcon(c, ri.IconID, ri.IsHQ, 18, 3)
														ui.Text(c, ri.ItemName).FontSize(t.text.xs).TextColor(t.foreground).SingleLine()
														if ri.IsHQ {
															drawPill(c, "HQ", pillWarning)
														}
														numTextf(c, "×%d", ri.Count).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine()
													})
												})
												if btn.Clicked() {
													onJumpItem(ri.ItemID)
												}
											}
										})
									})
								}
							})
						})
					}
				})
			})
		})
	})
}

func nonEmptyStr(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
