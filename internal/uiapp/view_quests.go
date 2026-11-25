package uiapp

import (
	"fmt"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

func (a *App) questScopeFilters() []itemScopeDef {
	return []itemScopeDef{
		{mode: "all", label: a.t("quests.scope_all")},
		{mode: "msq", label: a.t("quests.scope_msq")},
		{mode: "feature", label: a.t("quests.scope_feature")},
		{mode: "repeatable", label: a.t("quests.scope_repeatable")},
		{mode: "with_item", label: a.t("quests.scope_with_item")},
	}
}

func (a *App) renderQuestsView(c *ui.Context) {
	kitResizer(c, "quest-split", &a.listW, itemListMinWidth, itemListMaxWidth, 8, a.t("shell.resize_quest_list"),
		func() { a.renderQuestListPane(c) },
		func() { a.renderQuestDetailPane(c) },
	)
}

func (a *App) hasQuestFilter() bool {
	return strings.TrimSpace(a.questQuery.Q) != "" ||
		a.questQuery.Filter != "all" ||
		a.questQuery.SectionID >= 0 ||
		a.questQuery.ExpansionID >= 0
}

func (a *App) resetQuestFilters() {
	a.questQuery = gamedata.QuestSearchQuery{
		SectionID:   -1,
		ExpansionID: -1,
		Filter:      "all",
		Page:        1,
		PageSize:    30,
	}
	a.refreshQuests()
}

func (a *App) renderQuestListPane(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(10, 12, 10, 12).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Baseline).Grow(1).Shrink(1).Children(func() {
					ui.Text(c, a.t("quests.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					numText(c, a.tf("common.items_count", "count", formatInt(a.questRes.Total))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				})
				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.hasQuestFilter() {
						if kitButton(c, "quest-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.resetQuestFilters()
						}
					}
					secOpts := []selectOption[int]{{value: -1, label: a.t("quests.all_sections")}}
					if a.status != nil {
						for _, sec := range a.status.QuestSections {
							secOpts = append(secOpts, selectOption[int]{
								value: sec.ID,
								label: fmt.Sprintf("%s (%d)", sec.Name, sec.Count),
							})
						}
					}
					if kitSelect(c, "quest-sec-select", &a.questQuery.SectionID, secOpts, 136, 220, true) {
						a.questQuery.Page = 1
						a.refreshQuests()
					}
				})
			})

			// 页面内置搜索栏
			a.renderPageSearchInput(c, "quest-page-search", &a.questQuery.Q, a.t("search.quest_placeholder"), a.t("search.quest_label"), func() {
				a.questQuery.Page = 1
				a.refreshQuests()
			})

			ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
				for _, f := range a.questScopeFilters() {
					active := a.questQuery.Filter == f.mode
					if kitToggleChip(c, "q-scope-"+f.mode, f.label, "", active, nil) {
						a.questQuery.Filter = f.mode
						a.questQuery.Page = 1
						a.refreshQuests()
					}
				}
			})
		})

		if len(a.questRes.Items) == 0 {
			kitEmpty(c, icoScroll, a.t("quests.empty_title"), a.t("quests.empty_desc"), func() {
				if kitButton(c, "quest-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.resetQuestFilters()
				}
			})
		} else {
			ui.Scroll(c.Key("quest-list-scroll")).TrackScroll(&a.questListScroll).Grow(1).Shrink(1).Children(func() {
				ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
					for _, q := range a.questRes.Items {
						isSel := q.ID == a.selectedQuestID
						row := ui.ButtonBase(c.Key(fmt.Sprintf("quest-row-%d", q.ID))).
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
						title := gamedata.CleanQuestTitle(q.NameCn)
						if title == "" {
							title = q.NameCn
						}
						row.Children(func() {
							a.drawGameIcon(c, q.IconID, false, 32, t.radius.md)
							ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
								ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
									fw := 500
									if isSel {
										fw = 600
									}
									ui.Text(c, title).FontSize(t.text.sm).FontWeight(fw).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
									numTextf(c, "#%d", q.ID).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
								})
								ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
									ui.Text(c, a.formatQuestRowType(q)).FontSize(t.text.xs).TextColor(t.muted).SingleLine().Grow(1).Shrink(1)
									numText(c, formatQuestRowMeta(q)).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
								})
							})
						})
						if row.Clicked() {
							a.selectQuest(q.ID)
						}
						if q.ID == a.scrollQuestListToID {
							row.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter})
							a.scrollQuestListToID = 0
						}
					}
				})
			})
		}

		a.renderPaginationBar(
			c,
			"quest-pg",
			a.questRes.Page,
			a.questRes.TotalPages,
			&a.questPageStr,
			func(next int) {
				a.questQuery.Page = next
				a.refreshQuests()
			},
		)
	})
}

// formatQuestRowType 生成任务列表行左下角的任务类型文本。
func (a *App) formatQuestRowType(q gamedata.QuestEntry) string {
	if q.IsMSQ {
		return a.t("quests.type_msq")
	}
	if q.GenreName != "" {
		return q.GenreName
	}
	if q.SectionName != "" {
		return q.SectionName
	}
	if q.IsFeature {
		return a.t("quests.type_feature")
	}
	if q.IsRepeatable {
		if q.RepeatIntervalLabel == a.t("gamedata.quest.repeat_daily") || q.RepeatIntervalLabel == "每日可重复" {
			return a.t("quests.type_daily")
		}
		if q.RepeatIntervalLabel == a.t("gamedata.quest.repeat_weekly") || q.RepeatIntervalLabel == "每周可重复" {
			return a.t("quests.type_weekly")
		}
		return a.t("quests.type_repeatable")
	}
	return a.t("quests.type_side")
}

// formatQuestRowMeta 生成任务列表行右下角的等级文本。
func formatQuestRowMeta(q gamedata.QuestEntry) string {
	if q.Level > 0 {
		return fmt.Sprintf("Lv.%d", q.Level)
	}
	return ""
}

func (a *App) renderQuestDetailPane(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Grow(1).Shrink(1).FillHeight().Children(func() {
		q := a.selectedQuest
		if q == nil {
			kitEmpty(c, icoScroll, a.t("quests.detail_empty_title"), a.t("quests.detail_empty_desc"), nil)
			return
		}

		ui.Row(c).FillWidth().Height(42).Padding(0, 16).Gap(10).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			numText(c, a.tf("quests.id_header", "id", fmt.Sprintf("%d", q.ID), "internal", q.InternalID)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
			if q.ExpansionName != "" {
				drawPill(c, q.ExpansionName, pillNeutral)
			}
		})

		ui.Scroll(c.Key("quest-detail-scroll")).TrackScroll(&a.questDetailScroll).Grow(1).Shrink(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(20, 24, 32, 24).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(880).Gap(18).Children(func() {
					// 1. 头部摘要（类型图标 + 中/日/英标题 + 元数据 + 任务专属徽章）
					a.renderQuestDetailHero(c, q)

					// 2. 任务详情图片（1128×360 横幅插图，参考网页排版紧随标题下方）
					a.renderQuestBannerImage(c, q)

					// 3. 报酬与解锁（数值概览 + 道具奖励 + 解锁奖励）
					a.renderQuestRewardsSection(c, q)

					// 4. 任务概要与目的（成功解析时显示，默认折叠防剧透）
					a.renderQuestSummarySection(c, q)

					// 5. 接取与交付 NPC 及地点坐标
					a.renderQuestNPCsSection(c, q)

					// 6. 接取条件与前置要求
					a.renderQuestRequirementsSection(c, q)

					// 7. 任务对话文本（成功解析时显示，默认折叠防剧透）
					a.renderQuestDialogueSection(c, q)
				})
			})
		})
	})
}

func (a *App) renderQuestDetailHero(c *ui.Context, q *gamedata.QuestEntry) {
	t := themeOf(c)
	title := gamedata.CleanQuestTitle(q.NameCn)
	if title == "" {
		title = q.NameCn
	}

	ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Start).Children(func() {
		a.drawGameIcon(c, q.IconID, false, 52, t.radius.xl)

		ui.Column(c).Gap(6).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Baseline).Wrap().Children(func() {
				ui.Text(c, title).FontSize(t.text.xl).FontWeight(600).LetterSpacing(-0.3).TextColor(t.foreground).Selectable()
				numTextf(c, "#%d", q.ID).FontSize(t.text.xs).TextColor(t.muted).Selectable()
			})
			if secLines := a.secondaryDataLines(title, q.NameJa, q.NameEn); len(secLines) > 0 {
				ui.Row(c).Gap(12).Wrap().Children(func() {
					for _, sec := range secLines {
						ui.Textf(c, "%s  %s", sec.badge, sec.text).FontSize(t.text.xs).TextColor(t.muted).Selectable()
					}
				})
			}

			var meta []string
			if q.SectionName != "" {
				meta = append(meta, q.SectionName)
			}
			if q.CategoryName != "" && q.CategoryName != q.SectionName {
				meta = append(meta, q.CategoryName)
			}
			if q.GenreName != "" {
				meta = append(meta, q.GenreName)
			}
			if q.Level > 0 {
				lvlStr := fmt.Sprintf("Lv.%d", q.Level)
				if q.LevelMax > q.Level {
					lvlStr = fmt.Sprintf("Lv.%d~%d", q.Level, q.LevelMax)
				}
				meta = append(meta, lvlStr)
			}
			if q.ClassJobCategoryName != "" {
				meta = append(meta, q.ClassJobCategoryName)
			}
			if q.PlaceName != "" {
				meta = append(meta, q.PlaceName)
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
				if len(meta) > 0 {
					numText(c, strings.Join(meta, " · ")).FontSize(t.text.xs).TextColor(t.subdued)
				}
				if q.IsMSQ {
					drawPill(c, a.t("quests.type_msq"), pillWarning)
				}
				if q.IsFeature {
					drawPill(c, a.t("quests.type_feature"), pillAccent)
				}
				if q.IsRepeatable {
					lbl := q.RepeatIntervalLabel
					if lbl == "" {
						lbl = a.t("quests.badge_repeatable")
					}
					drawPill(c, lbl, pillPositive)
				}
				if q.HasLevelSync {
					drawPill(c, a.t("quests.badge_level_sync"), pillNeutral)
				}
				if q.IsJobFixed {
					drawPill(c, a.t("quests.badge_job_fixed"), pillNeutral)
				}
			})
		})

		if q.SpecialIconID > 0 {
			if bmp := a.bitmaps.GetIcon(a.service, a.win, q.SpecialIconID, false); bmp != nil {
				ui.Box(c).Size(60, 74).Shrink(0).Center().Children(func() {
					ui.Image(c, bmp).Size(60, 74).Fit(ui.Contain)
				})
			}
		}
	})
}

func (a *App) renderQuestBannerImage(c *ui.Context, q *gamedata.QuestEntry) {
	if q.BannerIconID <= 0 {
		return
	}
	t := themeOf(c)
	bmp := a.bitmaps.GetIcon(a.service, a.win, q.BannerIconID, false)
	if bmp == nil {
		return
	}
	ui.Box(c).FillWidth().MaxWidth(564).AspectRatio(1128.0/360.0).
		Radius(t.radius.lg).Border(hairline, t.border).Background(t.card).Clip().Children(func() {
		ui.Image(c, bmp).Fill().Fit(ui.Contain)
	})
}

func (a *App) renderQuestRewardsSection(c *ui.Context, q *gamedata.QuestEntry) {
	t := themeOf(c)

	gilStr := a.t("common.none")
	if q.GilReward > 0 {
		gilStr = a.tf("common.gil_amount", "amount", formatInt(q.GilReward))
	}
	expStr := a.t("common.none")
	if q.ExpReward > 0 {
		expStr = a.tf("common.exp_amount", "amount", formatInt(q.ExpReward))
	} else if q.ExpFactor > 0 {
		expStr = a.tf("quests.exp_factor", "factor", fmt.Sprintf("%d", q.ExpFactor))
	}
	extraLabel := a.t("quests.extra_currency_rep")
	var extraParts []string
	if q.CurrencyRewardName != "" && q.CurrencyRewardCount > 0 {
		extraParts = append(extraParts, fmt.Sprintf("%s ×%s", q.CurrencyRewardName, formatInt(q.CurrencyRewardCount)))
	}
	if q.TomestoneName != "" && q.TomestoneCount > 0 {
		extraParts = append(extraParts, fmt.Sprintf("%s ×%s", q.TomestoneName, formatInt(q.TomestoneCount)))
	}
	if q.ReputationReward > 0 {
		extraParts = append(extraParts, a.tf("quests.reputation_plus", "rep", formatInt(q.ReputationReward)))
	}
	extraVal := a.t("common.none")
	if len(extraParts) > 0 {
		extraVal = strings.Join(extraParts, " · ")
	}

	kitBlock(c, ui.Row(c).FillWidth().Clip(), toneCard).Children(func() {
		drawMetricSplitCell(c, a.t("quests.gil_reward"), gilStr, false)
		drawMetricSplitCell(c, a.t("quests.exp_reward"), expStr, true)
		drawMetricSplitCell(c, extraLabel, extraVal, true)
	})

	if len(q.RewardItems) > 0 {
		drawDetailSection(c, a.t("quests.item_rewards"), a.tf("common.items_count", "count", fmt.Sprintf("%d", len(q.RewardItems))), nil, func() {
			ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
				for rIdx, ri := range q.RewardItems {
					btn := ui.ButtonBase(c.Key(fmt.Sprintf("qrew-%d-%d-%d", q.ID, ri.ItemID, rIdx))).Label(a.tf("common.view_item", "name", ri.ItemName)).
						MinWidth(230).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
						Background(t.card).Border(hairline, t.border).Justify(ui.SpaceBetween).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
					if btn.Hovered() {
						btn.Background(t.tint(1.5))
					}
					btn.Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
							a.drawGameIcon(c, ri.IconID, ri.IsHQ, 26, t.radius.md)
							ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
								ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
									ui.Text(c, ri.ItemName).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine().Shrink(1)
									if ri.IsHQ {
										drawPill(c, "HQ", pillWarning)
									}
								})
								sub := ri.GroupLabel
								if ri.ClassJobCategory != "" {
									sub += " · " + ri.ClassJobCategory
								}
								ui.Text(c, sub).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
							})
						})
						numTextf(c, "×%d", ri.Count).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine().Shrink(0)
					})
					if btn.Clicked() {
						a.inspectItemFromOtherTab(ri.ItemID)
					}
				}
			})
		})
	}

	if len(q.Unlocks) > 0 {
		drawDetailSection(c, a.t("quests.unlocks_title"), a.tf("common.items_count", "count", fmt.Sprintf("%d", len(q.Unlocks))), nil, func() {
			ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
				for uIdx, u := range q.Unlocks {
					ui.Row(c.Key(fmt.Sprintf("qunl-%d-%d", q.ID, uIdx))).Padding(6, 10).Radius(t.radius.md).
						Background(t.card).Border(hairline, t.border).Gap(6).AlignItems(ui.Center).Children(func() {
						if u.IconID > 0 {
							a.drawGameIcon(c, u.IconID, false, 20, t.radius.sm)
						}
						ui.Text(c, u.Label).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground)
					})
				}
			})
		})
	}
}

func (a *App) renderQuestNPCsSection(c *ui.Context, q *gamedata.QuestEntry) {
	if q.IssuerName == "" && q.IssuerLocation == nil && q.TargetName == "" && q.TargetLocation == nil {
		return
	}
	t := themeOf(c)
	drawDetailSection(c, a.t("quests.npcs_title"), "", nil, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			if q.IssuerName != "" || q.IssuerLocation != nil {
				ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						drawPill(c, a.t("quests.issuer_badge"), pillAccent)
						lbl := q.IssuerName
						if lbl == "" {
							lbl = fmt.Sprintf("NPC #%d", q.IssuerNpcID)
						}
						ui.Text(c, lbl).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
					})
					if q.IssuerLocation != nil {
						a.renderMapLocationBtn(c, fmt.Sprintf("q-issuer-map-%d", q.ID), gamedata.CleanQuestTitle(q.NameCn), a.tf("quests.issuer_point", "name", q.IssuerName), q.IssuerLocation)
					}
				})
			}
			if q.TargetName != "" || q.TargetLocation != nil {
				ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						drawPill(c, a.t("quests.target_badge"), pillPositive)
						lbl := q.TargetName
						if lbl == "" {
							lbl = fmt.Sprintf("NPC #%d", q.TargetNpcID)
						}
						ui.Text(c, lbl).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
					})
					if q.TargetLocation != nil {
						a.renderMapLocationBtn(c, fmt.Sprintf("q-target-map-%d", q.ID), gamedata.CleanQuestTitle(q.NameCn), a.tf("quests.target_point", "name", q.TargetName), q.TargetLocation)
					}
				})
			}
		})
	})
}

func (a *App) renderQuestRequirementsSection(c *ui.Context, q *gamedata.QuestEntry) {
	hasReqs := len(q.PreviousQuestIDs) > 0 || len(q.QuestLockIDs) > 0 || len(q.InstanceContentNames) > 0 ||
		q.ClassJobRequiredName != "" || q.GrandCompanyName != "" || q.BeastTribeName != "" || q.MountRequiredName != "" || q.IsHouseRequired
	if !hasReqs {
		return
	}
	t := themeOf(c)
	drawDetailSection(c, a.t("quests.reqs_title"), "", nil, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			if len(q.PreviousQuestIDs) > 0 {
				ui.Row(c).FillWidth().Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
					lbl := a.t("quests.prev_all")
					if q.PreviousQuestJoin == 1 {
						lbl = a.t("quests.prev_any")
					}
					ui.Text(c, lbl).FontSize(t.text.xs).TextColor(t.muted)
					for idx, pqID := range q.PreviousQuestIDs {
						pqName := gamedata.CleanQuestTitle(q.PreviousQuestNames[idx])
						btn := ui.ButtonBase(c.Key(fmt.Sprintf("pq-%d-%d", q.ID, pqID))).Label(a.tf("common.view_quest", "name", pqName)).
							Padding(4, 9).Radius(t.radius.md).Background(t.card).Border(hairline, t.border).
							Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
						if btn.Hovered() {
							btn.Background(t.tint(1.5))
						}
						btn.Children(func() {
							ui.Text(c, pqName).FontSize(t.text.xs).FontWeight(500).TextColor(t.infoText)
						})
						if btn.Clicked() {
							a.jumpToQuest(pqID)
						}
					}
				})
			}
			if len(q.QuestLockIDs) > 0 {
				ui.Row(c).FillWidth().Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.t("quests.lock_quests")).FontSize(t.text.xs).TextColor(t.muted)
					for idx, lID := range q.QuestLockIDs {
						lName := gamedata.CleanQuestTitle(q.QuestLockNames[idx])
						if kitButton(c, fmt.Sprintf("qlock-%d-%d", q.ID, lID), lName, btnOpts{variant: btnOutline, size: btnSizeXs}) {
							a.jumpToQuest(lID)
						}
					}
				})
			}
			var extraConds []string
			if q.ClassJobRequiredName != "" {
				extraConds = append(extraConds, a.tf("quests.req_job", "name", q.ClassJobRequiredName))
			}
			if q.GrandCompanyName != "" {
				extraConds = append(extraConds, a.tf("quests.req_gc", "name", q.GrandCompanyName))
			}
			if q.BeastTribeName != "" {
				s := a.tf("quests.req_tribe", "name", q.BeastTribeName)
				if q.BeastReputationRank != "" {
					s += " · " + q.BeastReputationRank
				}
				extraConds = append(extraConds, s)
			}
			if q.MountRequiredName != "" {
				extraConds = append(extraConds, a.tf("quests.req_mount", "name", q.MountRequiredName))
			}
			if q.IsHouseRequired {
				extraConds = append(extraConds, a.t("quests.req_house"))
			}
			for _, inst := range q.InstanceContentNames {
				extraConds = append(extraConds, a.tf("quests.req_instance", "name", inst))
			}
			if len(extraConds) > 0 {
				ui.Text(c, strings.Join(extraConds, "   ·   ")).FontSize(t.text.xs).TextColor(t.subdued)
			}
		})
	})
}

// renderQuestCollapsibleSection 绘制防剧透默认折叠区块（仅保留清晰顶部细线与可点击标题行）。
func (a *App) renderQuestCollapsibleSection(c *ui.Context, key, title, countMeta string, open *bool, body func()) {
	t := themeOf(c)
	ui.Column(c).FillWidth().Padding(14, 0, 0, 0).Gap(10).
		BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
		trig := ui.ButtonBase(c.Key(key)).Label(title).
			FillWidth().Padding(6, 10).Radius(t.radius.lg).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
		if *open {
			trig.Background(t.card)
		} else if trig.Hovered() {
			trig.Background(t.tint(1.25))
		} else {
			trig.Background(t.tint(0.6))
		}
		trig.Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).Children(func() {
				ico := icoChevronRight
				if *open {
					ico = icoChevronDown
				}
				ui.Icon(c, ico).Size(iconSm, iconSm).Shrink(0).TextColor(t.muted)
				ui.Text(c, title).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				if countMeta != "" {
					numText(c, countMeta).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				}
			})
			hint := a.t("quests.spoiler_collapsed")
			if *open {
				hint = a.t("quests.spoiler_expanded")
			}
			ui.Text(c, hint).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
		})
		if trig.Clicked() {
			*open = !*open
		}

		if *open && body != nil {
			body()
		}
	})
}

func (a *App) renderQuestSummarySection(c *ui.Context, q *gamedata.QuestEntry) {
	qt := a.selectedQuestText
	if qt == nil {
		if a.questTextLoading {
			t := themeOf(c)
			ui.Row(c).FillWidth().Padding(14, 0, 0, 0).Gap(8).AlignItems(ui.Center).
				BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
				ui.Spinner(c).FontSize(13).TextColor(t.primary).Label(a.t("quests.loading_text"))
				ui.Text(c, a.t("quests.loading_text_desc")).FontSize(t.text.xs).TextColor(t.muted)
			})
		}
		return
	}
	if len(qt.Journals) == 0 && len(qt.Objectives) == 0 {
		return
	}

	t := themeOf(c)
	var metaParts []string
	if len(qt.Journals) > 0 {
		metaParts = append(metaParts, a.tf("quests.summary_journals_meta", "count", fmt.Sprintf("%d", len(qt.Journals))))
	}
	if len(qt.Objectives) > 0 {
		metaParts = append(metaParts, a.tf("quests.summary_objectives_meta", "count", fmt.Sprintf("%d", len(qt.Objectives))))
	}

	a.renderQuestCollapsibleSection(c, fmt.Sprintf("q-summary-fold-%d", q.ID), a.t("quests.summary_title"), strings.Join(metaParts, " · "), &a.questSummaryOpen, func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			if len(qt.Journals) > 0 {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					ui.Text(c, a.t("quests.journals_heading")).FontSize(t.text.xs).FontWeight(600).TextColor(t.subdued)
					for idx, jText := range qt.Journals {
						kitBlock(c, ui.Column(c).FillWidth().Padding(10, 14).Gap(6), toneCard).Children(func() {
							if len(qt.Journals) > 1 {
								lbl := a.tf("quests.journal_step", "step", fmt.Sprintf("%d", idx+1))
								if idx == 0 {
									lbl = a.t("quests.journal_start")
								}
								drawPill(c, lbl, pillNeutral)
							}
							ui.Text(c, jText).FontSize(t.text.sm).LineHeight(lineProse).TextColor(t.foreground).Selectable()
						})
					}
				})
			}

			if len(qt.Objectives) > 0 {
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					ui.Text(c, a.t("quests.objectives_heading")).FontSize(t.text.xs).FontWeight(600).TextColor(t.subdued)
					kitBlock(c, ui.Column(c).FillWidth().Padding(10, 14).Gap(6), toneCard).Children(func() {
						for idx, obj := range qt.Objectives {
							ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
								numTextf(c, "%d.", idx+1).FontSize(t.text.xs).FontWeight(600).TextColor(t.muted).Width(18).Shrink(0)
								ui.Text(c, obj).FontSize(t.text.sm).LineHeight(lineProse).TextColor(t.foreground).Grow(1).Shrink(1).Selectable()
							})
						}
					})
				})
			}
		})
	})
}

func (a *App) renderQuestDialogueSection(c *ui.Context, q *gamedata.QuestEntry) {
	qt := a.selectedQuestText
	if qt == nil || len(qt.Dialogues) == 0 {
		return
	}
	t := themeOf(c)
	meta := a.tf("quests.dialogue_meta", "count", fmt.Sprintf("%d", len(qt.Dialogues)))

	a.renderQuestCollapsibleSection(c, fmt.Sprintf("q-dialogue-fold-%d", q.ID), a.t("quests.dialogue_title"), meta, &a.questDialogueOpen, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			for _, line := range qt.Dialogues {
				padL := float32(14)
				if line.Kind == "answer" {
					padL = 28
				}
				kitBlock(c, ui.Column(c).FillWidth().Padding(9, 14, 9, padL).Gap(5), toneCard).Children(func() {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						switch line.Kind {
						case "system":
							drawPill(c, line.Speaker, pillAccent)
						case "question":
							drawPill(c, line.Speaker, pillWarning)
						case "answer":
							drawPill(c, line.Speaker, pillOutline)
						case "battle":
							drawPill(c, line.Speaker, pillNegative)
						default:
							ui.Text(c, line.Speaker).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).SingleLine()
						}
					})
					ui.Text(c, line.Text).FontSize(t.text.sm).LineHeight(lineProse).TextColor(t.subdued).Selectable()
				})
			}
		})
	})
}
