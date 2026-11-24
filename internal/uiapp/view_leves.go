package uiapp

import (
	"fmt"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

var leveScopeFilters = []itemScopeDef{
	{mode: "all", label: "全部"},
	{mode: "craft", label: "制作"},
	{mode: "gathering", label: "采集"},
	{mode: "battle", label: "佣兵"},
	{mode: "company", label: "军队"},
}

func (a *App) renderLevesView(c *ui.Context) {
	kitResizer(c, "leve-split", &a.listW, itemListMinWidth, itemListMaxWidth, 8, "调整理符列表宽度",
		func() { a.renderLeveListPane(c) },
		func() { a.renderLeveDetailPane(c) },
	)
}

func (a *App) hasLeveFilter() bool {
	return strings.TrimSpace(a.leveQuery.Q) != "" ||
		a.leveQuery.Kind != "all" ||
		a.leveQuery.AssignmentTypeID > 0
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
					ui.Text(c, "理符").FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					numTextf(c, "%s 项", formatInt(a.leveRes.Total)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				})
				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.hasLeveFilter() {
						if kitButton(c, "leve-reset", "重置", btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.resetLeveFilters()
						}
					}
					typeOpts := []selectOption[int]{{value: 0, label: "全部职能"}}
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

			ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
				for _, f := range leveScopeFilters {
					active := a.leveQuery.Kind == f.mode
					if kitToggleChip(c, "l-scope-"+f.mode, f.label, "", active, nil) {
						a.leveQuery.Kind = f.mode
						a.leveQuery.Page = 1
						a.refreshLeves()
					}
				}
			})
		})

		if len(a.leveRes.Items) == 0 {
			kitEmpty(c, icoClipboard, "没有匹配的理符任务", "尝试在顶部搜索框调整关键词，或切换理符类型。", func() {
				if kitButton(c, "leve-empty-reset", "重置筛选", btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.resetLeveFilters()
				}
			})
		} else {
			ui.Scroll(c.Key("leve-list-scroll")).TrackScroll(&a.leveListScroll).Grow(1).Shrink(1).Children(func() {
				ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
					for _, lv := range a.leveRes.Items {
						isSel := lv.ID == a.selectedLeveID
						row := ui.ButtonBase(c.Key(fmt.Sprintf("leve-row-%d", lv.ID))).
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
									numTextf(c, "#%d", lv.ID).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
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
						if row.Clicked() {
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

func (a *App) renderLeveDetailPane(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Grow(1).Shrink(1).FillHeight().Children(func() {
		lv := a.selectedLeve
		if lv == nil {
			kitEmpty(c, icoClipboard, "选择理符查看详情", "点击左侧任意理符，可查看委托交纳物品、坐标与奖励道具池。", nil)
			return
		}

		ui.Row(c).FillWidth().Height(42).Padding(0, 16).Gap(10).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			numTextf(c, "理符 #%d", lv.ID).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
			drawPill(c, lv.LeveKindLabel, leveKindPillKind(lv.LeveKind))
		})

		ui.Scroll(c.Key("leve-detail-scroll")).TrackScroll(&a.leveDetailScroll).Grow(1).Shrink(1).FillWidth().Children(func() {
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
								numTextf(c, "#%d", lv.ID).FontSize(t.text.xs).TextColor(t.muted).Selectable()
							})
							if lv.NameEn != "" || lv.NameJa != "" {
								ui.Row(c).Gap(12).Wrap().Children(func() {
									if lv.NameEn != "" {
										ui.Textf(c, "EN  %s", lv.NameEn).FontSize(t.text.xs).TextColor(t.muted).Selectable()
									}
									if lv.NameJa != "" {
										ui.Textf(c, "JA  %s", lv.NameJa).FontSize(t.text.xs).TextColor(t.muted).Selectable()
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
								meta = append(meta, "所属城市："+lv.TownName)
							}
							if lv.ClientName != "" {
								meta = append(meta, "委托人："+lv.ClientName)
							}
							numText(c, strings.Join(meta, " · ")).FontSize(t.text.xs).TextColor(t.subdued)
						})
					})

					if lv.DescriptionCn != "" {
						kitBlock(c, ui.Box(c).FillWidth().Padding(10, 14), toneCard).Children(func() {
							ui.Text(c, lv.DescriptionCn).FontSize(t.text.sm).LineHeight(lineProse).TextColor(t.subdued).Selectable()
						})
					}

					expStr := fmt.Sprintf("%s EXP", formatInt(lv.ExpReward))
					if lv.ExpReward <= 0 {
						expStr = "无"
					}
					gilStr := fmt.Sprintf("%s 金币", formatInt(lv.GilReward))
					if lv.GilReward <= 0 {
						gilStr = "无"
					}
					limitStr := fmt.Sprintf("限额 %d 张", max(1, lv.AllowanceCost))
					if lv.TimeLimit > 0 {
						limitStr += fmt.Sprintf(" · 限时 %d 分钟", lv.TimeLimit)
					}
					kitBlock(c, ui.Row(c).FillWidth().Clip(), toneCard).Children(func() {
						drawMetricSplitCell(c, "基础经验", expStr, false)
						drawMetricSplitCell(c, "金币报酬", gilStr, true)
						drawMetricSplitCell(c, "消耗与时限", limitStr, true)
					})

					// 接取与执行地点
					if lv.PlaceNameIssued != "" || lv.PlaceNameStart != "" || lv.LevemeteLocation != nil || lv.StartLocation != nil || lv.GatheringTargetLocation != nil {
						drawDetailSection(c, "接取与执行地点", "", nil, func() {
							ui.Column(c).FillWidth().Gap(8).Children(func() {
								if lv.PlaceNameIssued != "" || lv.LevemeteLocation != nil {
									ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
										ui.Textf(c, "理符发布点：%s", nonEmptyStr(lv.PlaceNameIssued, "发行官位置")).FontSize(t.text.xs).TextColor(t.foreground)
										if lv.LevemeteLocation != nil {
											a.renderMapLocationBtn(c, fmt.Sprintf("lv-issued-map-%d", lv.ID), lv.NameCn, "理符发布位置", lv.LevemeteLocation)
										}
									})
								}
								if lv.PlaceNameStart != "" || lv.StartLocation != nil {
									ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
										zone := lv.PlaceNameStart
										if lv.PlaceNameStartZone != "" && lv.PlaceNameStartZone != zone {
											zone += " · " + lv.PlaceNameStartZone
										}
										ui.Textf(c, "任务区域：%s", nonEmptyStr(zone, "起始坐标")).FontSize(t.text.xs).TextColor(t.foreground)
										if lv.StartLocation != nil {
											a.renderMapLocationBtn(c, fmt.Sprintf("lv-start-map-%d", lv.ID), lv.NameCn, "理符执行坐标", lv.StartLocation)
										}
									})
								}
								if lv.GatheringTargetLocation != nil {
									ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
										ui.Text(c, "采集点分布区域").FontSize(t.text.xs).TextColor(t.foreground)
										a.renderMapLocationBtn(c, fmt.Sprintf("lv-gath-map-%d", lv.ID), lv.NameCn, "采集目标分布", lv.GatheringTargetLocation)
									})
								}
							})
						})
					}

					// 制作理符交纳物品
					if len(lv.TurnInItems) > 0 {
						meta := ""
						if lv.CraftRepeats > 0 {
							meta = fmt.Sprintf("可重复追加 %d 次（最多交纳 %d 轮）", lv.CraftRepeats, 1+lv.CraftRepeats)
						}
						drawDetailSection(c, "需要交纳道具", meta, nil, func() {
							ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
								for idx, ti := range lv.TurnInItems {
									btn := ui.ButtonBase(c.Key(fmt.Sprintf("lv-turn-%d-%d", lv.ID, idx))).Label("查看 "+ti.ItemName).
										MinWidth(240).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
										Background(t.card).Border(hairline, t.border).Justify(ui.SpaceBetween).AlignItems(ui.Center).
										Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
									if btn.Hovered() {
										btn.Background(t.tint(1.5))
									}
									btn.Children(func() {
										ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
											a.drawGameIcon(c, ti.IconID, false, 26, t.radius.md)
											ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
												ui.Text(c, ti.ItemName).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
												if lv.CraftRepeats > 0 {
													numTextf(c, "单轮 ×%d · 满交共 ×%d", ti.Count, ti.Count*(1+lv.CraftRepeats)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
												}
											})
										})
										numTextf(c, "×%d", ti.Count).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine().Shrink(0)
									})
									if btn.Clicked() {
										a.inspectItemFromOtherTab(ti.ItemID)
									}
								}
							})
						})
					}

					// 采集/佣兵/军队目标明细
					hasDetails := lv.RuleName != "" || lv.FishingSpotName != "" || lv.SpearfishingPlaceName != "" ||
						len(lv.Objectives) > 0 || len(lv.CombatTargets) > 0 || len(lv.RequiredObjectiveItems) > 0
					if hasDetails {
						drawDetailSection(c, "委托规则与目标", lv.RuleName, nil, func() {
							ui.Column(c).FillWidth().Gap(8).Children(func() {
								if lv.FishingSpotName != "" {
									ui.Textf(c, "关联钓场：%s", lv.FishingSpotName).FontSize(t.text.xs).TextColor(t.subdued)
								}
								if lv.SpearfishingPlaceName != "" {
									ui.Textf(c, "刺鱼渔场：%s", lv.SpearfishingPlaceName).FontSize(t.text.xs).TextColor(t.subdued)
								}
								for _, obj := range lv.Objectives {
									ui.Text(c, "• "+obj).FontSize(t.text.xs).TextColor(t.subdued)
								}
								if len(lv.CombatTargets) > 0 {
									ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
										for idx, ct := range lv.CombatTargets {
											ui.Row(c.Key(fmt.Sprintf("lv-bnpc-%d-%d", lv.ID, idx))).Padding(5, 9).Radius(t.radius.md).
												Background(t.card).Border(hairline, t.border).Gap(6).AlignItems(ui.Center).Children(func() {
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
												Background(t.card).Border(hairline, t.border).Gap(6).AlignItems(ui.Center).Children(func() {
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
						drawDetailSection(c, "随机奖励道具池", fmt.Sprintf("%d 组", len(lv.RewardGroups)), nil, func() {
							ui.Column(c).FillWidth().Gap(10).Children(func() {
								for gIdx, grp := range lv.RewardGroups {
									if gIdx > 0 {
										kitSeparator(c, false)
									}
									ui.Column(c.Key(fmt.Sprintf("lv-rgrp-%d-%d", lv.ID, gIdx))).FillWidth().Gap(6).Children(func() {
										numTextf(c, "掉落概率 %d%%", grp.ProbabilityPercent).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground)
										ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
											for iIdx, ri := range grp.Items {
												btn := ui.ButtonBase(c.Key(fmt.Sprintf("lv-ritem-%d-%d-%d", lv.ID, gIdx, iIdx))).Label("查看 "+ri.ItemName).
													Padding(4, 8).Radius(t.radius.md).Background(t.card).Border(hairline, t.border).
													Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
												if btn.Hovered() {
													btn.Background(t.tint(1.5))
												}
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
													a.inspectItemFromOtherTab(ri.ItemID)
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
