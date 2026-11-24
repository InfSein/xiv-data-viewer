package uiapp

import (
	"fmt"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

var achFilterModes = []string{"all", "with_title", "with_item"}
var achFilterLabels = []string{"全部", "含称号", "含道具"}

const (
	achColIDW     float32 = 50
	achColNameW   float32 = 228
	achColCatW    float32 = 112
	achColPointsW float32 = 64
	achColCondMin float32 = 88
	achColRewardW float32 = 164
)

func (a *App) hasAchievementFilter() bool {
	return strings.TrimSpace(a.achQuery.Q) != "" ||
		a.achQuery.KindID != 0 ||
		a.achQuery.CategoryID != 0 ||
		a.achQuery.Filter != "all"
}

func (a *App) resetAchievementFilters() {
	a.achQuery = gamedata.AchievementSearchQuery{Filter: "all", Page: 1, PageSize: 30}
	a.refreshAchievements()
}

// allAchievementCategories 返回当前加载状态下的全部有效二级分类（兼容仅填充大类或扁平列表的测试数据）。
func (a *App) allAchievementCategories() []gamedata.AchievementCategoryInfo {
	if a.status == nil {
		return nil
	}
	if len(a.status.AchievementCategories) > 0 {
		return a.status.AchievementCategories
	}
	var out []gamedata.AchievementCategoryInfo
	for _, k := range a.status.AchievementKinds {
		out = append(out, k.Categories...)
	}
	return out
}

func (a *App) buildAchievementKindOptions() []selectOption[int] {
	opts := []selectOption[int]{{value: 0, label: "全部大类"}}
	if a.status == nil {
		return opts
	}
	opts[0].label = fmt.Sprintf("全部大类 (%d)", len(a.status.AchievementKinds))
	for _, k := range a.status.AchievementKinds {
		opts = append(opts, selectOption[int]{
			value: k.ID,
			label: fmt.Sprintf("%s (%d)", k.Name, k.Count),
		})
	}
	return opts
}

func (a *App) buildAchievementCategoryOptions() []selectOption[int] {
	opts := []selectOption[int]{{value: 0, label: "全部二级分类"}}
	allCats := a.allAchievementCategories()
	if len(allCats) == 0 {
		return opts
	}

	var matched []gamedata.AchievementCategoryInfo
	for _, cat := range allCats {
		if a.achQuery.KindID == 0 || cat.KindID == a.achQuery.KindID {
			matched = append(matched, cat)
		}
	}
	opts[0].label = fmt.Sprintf("全部二级分类 (%d)", len(matched))
	for _, cat := range matched {
		label := fmt.Sprintf("%s (%d)", cat.Name, cat.Count)
		// 未锁定大类时加上大类前缀，避免“整体”“战斗”等重名二级分类难以区分
		if a.achQuery.KindID == 0 && cat.KindName != "" {
			label = fmt.Sprintf("%s · %s (%d)", cat.KindName, cat.Name, cat.Count)
		}
		opts = append(opts, selectOption[int]{
			value: cat.ID,
			label: label,
		})
	}
	return opts
}

func (a *App) onAchievementKindChanged() {
	if a.achQuery.KindID == 0 {
		a.achQuery.CategoryID = 0
	} else if a.achQuery.CategoryID > 0 {
		belongs := false
		for _, cat := range a.allAchievementCategories() {
			if cat.ID == a.achQuery.CategoryID && cat.KindID == a.achQuery.KindID {
				belongs = true
				break
			}
		}
		if !belongs {
			a.achQuery.CategoryID = 0
		}
	}
	a.achQuery.Page = 1
	a.refreshAchievements()
}

func (a *App) onAchievementCategoryChanged() {
	if a.achQuery.CategoryID > 0 {
		for _, cat := range a.allAchievementCategories() {
			if cat.ID == a.achQuery.CategoryID && cat.KindID > 0 {
				a.achQuery.KindID = cat.KindID
				break
			}
		}
	}
	a.achQuery.Page = 1
	a.refreshAchievements()
}

func (a *App) renderAchievementRewardCell(c *ui.Context, ach gamedata.AchievementEntry) {
	t := themeOf(c)
	hasTitle := ach.TitleID > 0 && (ach.TitleMasculineCn != "" || ach.TitleFeminineCn != "")
	hasItem := ach.RewardItemID > 0 && ach.RewardItemNameCn != ""

	ui.Column(c).Width(achColRewardW).Shrink(0).Gap(4).Children(func() {
		if !hasTitle && !hasItem {
			ui.Text(c, "–").FontSize(t.text.xs).TextColor(t.idle)
			return
		}
		if hasTitle {
			titleTxt := ach.TitleMasculineCn
			if titleTxt == "" {
				titleTxt = ach.TitleFeminineCn
			} else if ach.TitleFeminineCn != "" && ach.TitleFeminineCn != ach.TitleMasculineCn {
				titleTxt += " / " + ach.TitleFeminineCn
			}
			ui.Text(c, "《"+titleTxt+"》").FontSize(t.text.xs).FontWeight(500).TextColor(t.warningText).SingleLine()
			if ach.TitleNameEn != "" && ach.TitleNameEn != titleTxt {
				ui.Text(c, ach.TitleNameEn).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
			}
		}
		if hasItem {
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
		}
	})
}

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

				kindOpts := a.buildAchievementKindOptions()
				if kitSelect(c, "ach-kind-select", &a.achQuery.KindID, kindOpts, 152, 204, true) {
					a.onAchievementKindChanged()
				}

				catOpts := a.buildAchievementCategoryOptions()
				if kitSelect(c, "ach-cat-select", &a.achQuery.CategoryID, catOpts, 176, 244, true) {
					a.onAchievementCategoryChanged()
				}

				if a.hasAchievementFilter() {
					if kitButton(c, "ach-reset", "重置", btnOpts{variant: btnGhost, size: btnSizeSm}) {
						a.resetAchievementFilters()
					}
				}
			})
		})

		// 2. 表头（紧凑列宽以兼容最小 1024px 窗口，避免挤占达成条件列）
		ui.Row(c).FillWidth().Padding(8, 16).Background(t.card).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Gap(10).Children(func() {
			ui.Text(c, "ID").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(achColIDW).Shrink(0).TextAlign(ui.End)
			ui.Text(c, "成就名称").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(achColNameW).Shrink(0)
			ui.Text(c, "分类").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(achColCatW).Shrink(0)
			ui.Text(c, "点数").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(achColPointsW).Shrink(0).TextAlign(ui.Center)
			ui.Text(c, "达成条件").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Grow(1).Shrink(1).MinWidth(achColCondMin)
			ui.Text(c, "奖励称号 / 道具").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(achColRewardW).Shrink(0)
		})

		// 3. 成就列表行
		if len(a.achRes.Items) == 0 {
			kitEmpty(c, icoTrophy, "没有匹配的成就或称号", "尝试在顶部搜索框调整关键词，或切换筛选分类。", func() {
				if kitButton(c, "ach-empty-reset", "重置筛选", btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.resetAchievementFilters()
				}
			})
		} else {
			ui.Scroll(c.Key("ach-table-scroll")).TrackScroll(&a.achScroll).Grow(1).Shrink(1).Children(func() {
				// 允许用户在表格数据区内自由框选与复制文字（不影响奖励道具按钮点击）
				ui.Column(c).FillWidth().Selectable().Children(func() {
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
							numTextf(c, "#%d", ach.ID).FontSize(t.text.micro).TextColor(t.muted).Width(achColIDW).Shrink(0).TextAlign(ui.End).SingleLine()

							ui.Row(c).Width(achColNameW).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
								a.drawGameIcon(c, ach.IconID, false, 28, t.radius.md)
								ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
									ui.Text(c, ach.NameCn).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground).SingleLine()
									if ach.NameJa != "" {
										ui.Text(c, ach.NameJa).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
									}
									if ach.NameEn != "" && ach.NameEn != ach.NameCn {
										ui.Text(c, ach.NameEn).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
									}
								})
							})

							// 分类：清爽双行纯文本，不再每行贴灰色药丸
							ui.Column(c).Width(achColCatW).Shrink(0).Gap(1).Children(func() {
								ui.Text(c, ach.KindName).FontSize(t.text.xs).TextColor(t.subdued).SingleLine()
								ui.Text(c, ach.CategoryName).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
							})

							numTextf(c, "%d", ach.Points).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).Width(achColPointsW).Shrink(0).TextAlign(ui.Center).SingleLine()

							desc := ach.DescriptionCn
							if desc == "" {
								desc = "–"
							}
							ui.Text(c, desc).FontSize(t.text.xs).TextColor(t.subdued).Grow(1).Shrink(1).MinWidth(achColCondMin)

							a.renderAchievementRewardCell(c, ach)
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
