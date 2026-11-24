package uiapp

import (
	"fmt"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

var achFilterModes = []string{"all", "with_title", "with_item"}
var achFilterLabels = []string{"全部", "含称号", "含道具"}

func (a *App) renderAchievementsView(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		// 1. 集成页头与筛选控制条
		ui.Row(c).FillWidth().Padding(12, 16).Gap(10).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, "成就与称号").FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				if a.status != nil {
					numTextf(c, "%s 项成就 · %s 个称号 · 当前 %s 项",
						formatInt(a.status.Stats.TotalAchievements),
						formatInt(a.status.Stats.TotalTitles),
						formatInt(a.achRes.Total),
					).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				curIdx := 0
				for i, m := range achFilterModes {
					if m == a.achQuery.Filter {
						curIdx = i
						break
					}
				}
				if nextIdx, chg := kitSegmentedTabs(c, "ach-flt", curIdx, achFilterLabels); chg {
					a.achQuery.Filter = achFilterModes[nextIdx]
					a.achQuery.Page = 1
					a.refreshAchievements()
				}

				kindOpts := []selectOption[int]{{value: 0, label: "全部大类"}}
				if a.status != nil {
					kindOpts[0].label = fmt.Sprintf("全部大类 (%d)", len(a.status.AchievementKinds))
					for _, k := range a.status.AchievementKinds {
						kindOpts = append(kindOpts, selectOption[int]{
							value: k.ID,
							label: fmt.Sprintf("%s (%d)", k.Name, k.Count),
						})
					}
				}
				if kitSelect(c, "ach-kind-select", &a.achQuery.KindID, kindOpts, 160, 200, true) {
					a.achQuery.Page = 1
					a.refreshAchievements()
				}

				if a.achQuery.Q != "" || a.achQuery.KindID != 0 || a.achQuery.Filter != "all" {
					if kitButton(c, "ach-reset", "重置", btnOpts{variant: btnGhost, size: btnSizeSm}) {
						a.achQuery = gamedata.AchievementSearchQuery{Filter: "all", Page: 1, PageSize: 30}
						a.refreshAchievements()
					}
				}
			})
		})

		// 2. 表头（紧凑列宽以兼容最小 1024px 窗口，避免挤占达成条件列）
		ui.Row(c).FillWidth().Padding(8, 16).Background(t.card).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Gap(10).Children(func() {
			ui.Text(c, "ID").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(50).TextAlign(ui.End)
			ui.Text(c, "成就名称").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(196)
			ui.Text(c, "分类").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(116)
			ui.Text(c, "点数").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(40).TextAlign(ui.End)
			ui.Text(c, "达成条件").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Grow(1).MinWidth(100)
			ui.Text(c, "奖励称号").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(144)
			ui.Text(c, "奖励道具").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(134)
		})

		// 3. 成就列表行
		if len(a.achRes.Items) == 0 {
			kitEmpty(c, icoTrophy, "没有匹配的成就或称号", "尝试在顶部搜索框调整关键词，或切换筛选分类。", func() {
				if kitButton(c, "ach-empty-reset", "重置筛选", btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.achQuery = gamedata.AchievementSearchQuery{Filter: "all", Page: 1, PageSize: 30}
					a.refreshAchievements()
				}
			})
		} else {
			ui.Scroll(c.Key("ach-table-scroll")).TrackScroll(&a.achScroll).Grow(1).Shrink(1).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for i, ach := range a.achRes.Items {
						if i > 0 {
							kitSeparator(c, false).Margin(0, 16)
						}
						row := ui.Row(c.Key(fmt.Sprintf("ach-row-%d", ach.ID))).FillWidth().Padding(10, 16).Gap(10).
							AlignItems(ui.Center).Transition(colorTransition(motionFast))
						if row.Hovered() {
							row.Background(t.tint(0.85))
						}
						row.Children(func() {
							numTextf(c, "#%d", ach.ID).FontSize(t.text.micro).TextColor(t.muted).Width(50).TextAlign(ui.End).SingleLine()

							ui.Row(c).Width(196).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
								a.drawGameIcon(c, ach.IconID, false, 28, t.radius.md)
								ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
									ui.Text(c, ach.NameCn).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground).SingleLine()
									sub := ach.NameEn
									if sub == "" {
										sub = ach.NameJa
									}
									if sub != "" {
										ui.Text(c, sub).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
									}
								})
							})

							// 分类：清爽双行纯文本，不再每行贴灰色药丸
							ui.Column(c).Width(116).Shrink(0).Gap(1).Children(func() {
								ui.Text(c, ach.KindName).FontSize(t.text.xs).TextColor(t.subdued).SingleLine()
								ui.Text(c, ach.CategoryName).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
							})

							numTextf(c, "%d", ach.Points).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).Width(40).TextAlign(ui.End).SingleLine()

							desc := ach.DescriptionCn
							if desc == "" {
								desc = "–"
							}
							ui.Text(c, desc).FontSize(t.text.xs).TextColor(t.subdued).Grow(1).Shrink(1).MinWidth(100)

							// 奖励称号：柔和文字高亮替代成排黄药丸
							ui.Column(c).Width(144).Shrink(0).Gap(2).Children(func() {
								if ach.TitleID > 0 && ach.TitleMasculineCn != "" {
									titleTxt := ach.TitleMasculineCn
									if ach.TitleFeminineCn != "" && ach.TitleFeminineCn != ach.TitleMasculineCn {
										titleTxt += " / " + ach.TitleFeminineCn
									}
									posTxt := "后缀"
									if ach.TitleIsPrefix {
										posTxt = "前缀"
									}
									ui.Text(c, "《"+titleTxt+"》").FontSize(t.text.xs).FontWeight(500).TextColor(t.warningText).SingleLine()
									meta := posTxt
									if ach.TitleNameEn != "" {
										meta += " · " + ach.TitleNameEn
									}
									ui.Text(c, meta).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
								} else {
									ui.Text(c, "–").FontSize(t.text.xs).TextColor(t.idle)
								}
							})

							// 奖励道具（点击跳转物品详情）
							ui.Box(c).Width(134).Shrink(0).Children(func() {
								if ach.RewardItemID > 0 && ach.RewardItemNameCn != "" {
									btn := ui.ButtonBase(c.Key(fmt.Sprintf("ach-rew-%d", ach.ID))).Label("查看 "+ach.RewardItemNameCn).
										FillWidth().Padding(4, 7).Radius(t.radius.md).
										Background(t.card).Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
									if btn.Hovered() {
										btn.Background(t.tint(1.5))
									}
									btn.Children(func() {
										ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
											a.drawGameIcon(c, ach.RewardItemIconID, false, 18, 3)
											ui.Text(c, ach.RewardItemNameCn).FontSize(t.text.xs).FontWeight(500).TextColor(t.infoText).SingleLine().Grow(1).Shrink(1)
										})
									})
									if btn.Clicked() {
										a.inspectItemFromOtherTab(ach.RewardItemID)
									}
								} else {
									ui.Text(c, "–").FontSize(t.text.xs).TextColor(t.idle)
								}
							})
						})
					}
				})
			})
		}

		// 4. 分页底栏
		a.renderPaginationBar(
			c,
			"ach-pg",
			a.achRes.Page,
			a.achRes.TotalPages,
			&a.achPageStr,
			func(next int) {
				a.achQuery.Page = next
				a.refreshAchievements()
			},
		)
	})
}
