package uiapp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

type itemScopeDef struct {
	mode  string
	label string
}

// itemScopeFilters 物品界面内的快捷数据视图筛选。
var itemScopeFilters = []itemScopeDef{
	{mode: "all", label: "全部"},
	{mode: "craftable", label: "制作物品"},
	{mode: "gatherable", label: "采集物品"},
	{mode: "gil_shop", label: "金币商店"},
	{mode: "special_shop", label: "特殊兑换"},
	{mode: "retainer_venture", label: "雇员探险"},
	{mode: "collectable_turnin", label: "收藏品交纳"},
	{mode: "equipment", label: "武器装备"},
	{mode: "consumable", label: "食品药水"},
}

var itemSortOptions = []selectOption[string]{
	{value: "id_asc", label: "ID 升序"},
	{value: "id_desc", label: "ID 降序"},
	{value: "ilvl_desc", label: "品级降序"},
	{value: "ilvl_asc", label: "品级升序"},
	{value: "level_desc", label: "装备等级降序"},
}

const appBrandTitle = "XIV Data Viewer"

// formatAppVersionTag 将应用版本号规范化为带小写 v 前缀的展示标签。
func formatAppVersionTag(ver string) string {
	v := strings.TrimSpace(ver)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		return "v" + v[1:]
	}
	return "v" + v
}

// renderBrandCaption 绘制左上角品牌图标、名称与右侧不加粗小字版本号。
func (a *App) renderBrandCaption(c *ui.Context) {
	t := themeOf(c)
	ui.Icon(c, icoAppLogo).Size(iconMd, iconMd).TextColor(t.primary)
	ui.Row(c).Gap(6).AlignItems(ui.Baseline).Children(func() {
		ui.Text(c, appBrandTitle).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).SingleLine()
		if ver := formatAppVersionTag(a.appVersion); ver != "" {
			numText(c, ver).FontSize(t.text.micro).FontWeight(400).TextColor(t.muted).SingleLine()
		}
	})
}

// renderBareTitleBar 用于引导页与加载页的简洁可拖拽顶栏；showCaption 控制是否绘制应用名称。
func (a *App) renderBareTitleBar(c *ui.Context, showCaption bool) {
	tb := c.TitleBar()
	h := max(titleBarHeight, tb.Height)
	padL := tb.Left + 14
	padR := tb.Right + 10
	ui.Row(c).Height(h).FillWidth().Padding(0, padR, 0, padL).AlignItems(ui.Center).DragWindow().Children(func() {
		if showCaption {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				a.renderBrandCaption(c)
			})
		}
	})
}

// renderMainShell 渲染桌面端主外壳：融合标题栏 + 可折叠左侧导航 + 悬浮内容面板。
func (a *App) renderMainShell(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		a.renderTitleBar(c)
		a.renderShellBody(c)
	})
}

// renderTitleBar 绘制 40px 融合标题栏（支持拖拽移动窗口与系统控制按钮安全留白）。
func (a *App) renderTitleBar(c *ui.Context) {
	tb := c.TitleBar()
	h := max(titleBarHeight, tb.Height)
	padL := tb.Left + 12
	padR := tb.Right + 10

	ui.Row(c).Height(h).FillWidth().Padding(0, padR, 0, padL).AlignItems(ui.Center).DragWindow().Children(func() {
		// 左侧：品牌下拉菜单 + 侧栏折叠 + 前进后退
		ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
			a.renderBrandMenu(c)
			kitSeparator(c, true).Margin(0, 4)

			sbTip := fmt.Sprintf("收起侧边栏 (%s)", shortcutLabel("B"))
			if a.sidebarHidden {
				sbTip = fmt.Sprintf("展开侧边栏 (%s)", shortcutLabel("B"))
			}
			if kitIconButton(c, "tb-toggle-sb", icoPanelLeft, sbTip, btnOpts{variant: btnGhost, size: btnSizeSm}) {
				a.sidebarHidden = !a.sidebarHidden
			}
			if kitIconButton(c, "tb-back", icoArrowLeft, "后退", btnOpts{variant: btnGhost, size: btnSizeSm, disabled: len(a.tabHistory) == 0}) {
				a.goBack()
			}
			if kitIconButton(c, "tb-fwd", icoArrowRight, "前进", btnOpts{variant: btnGhost, size: btnSizeSm, disabled: len(a.tabAhead) == 0}) {
				a.goForward()
			}
		})

		ui.Spacer(c).MinWidth(12)

		// 居中上下文全局检索栏（按当前视图智能绑定物品 / 成就 / Diff 检索）
		a.renderTitleSearch(c)

		ui.Spacer(c).MinWidth(12)
	})
}

// renderBrandMenu 左上角可点击品牌菜单，收拢全局辅助工具。
func (a *App) renderBrandMenu(c *ui.Context) {
	t := themeOf(c)
	b := ui.ButtonBase(c.Key("brand-menu-btn")).Label(appBrandTitle).
		Height(28).Padding(0, 8, 0, 6).Radius(t.radius.md).Gap(7).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
	if b.Hovered() {
		b.Background(t.tint(1))
	}
	b.Menu(func(m *ui.Menu) {
		if m.Item("导出游戏 CSV...").Chosen() {
			a.openExportModal()
		}
		if m.Item("刷新版本快照").Disabled(a.backupSvc == nil || a.diffBusy).Chosen() {
			a.refreshVersionBackup()
		}
		m.Separator()
		if m.Item("浅色外观").Checked(a.cfg.Theme == "light").Chosen() {
			a.SetThemeMode("light")
		}
		if m.Item("深色外观").Checked(a.cfg.Theme == "dark").Chosen() {
			a.SetThemeMode("dark")
		}
		if m.Item("跟随系统外观").Checked(a.cfg.Theme == "" || a.cfg.Theme == "system").Chosen() {
			a.SetThemeMode("system")
		}
		m.Separator()
		if m.Item("设置").Chosen() {
			a.navigateTab(tabSettings)
		}
	})
	b.Children(func() {
		a.renderBrandCaption(c)
	})
}

func (a *App) renderTitleSearch(c *ui.Context) {
	sc := shortcutLabel("K")
	ui.Box(c).Width(420).MinWidth(200).Shrink(1).Children(func() {
		switch a.tab {
		case tabQuests:
			inp := kitInput(c, "title-search-quest", &a.questQuery.Q, kitInputOpts{
				placeholder: "搜索任务名、区域、NPC 或 ID...",
				label:       "搜索任务",
				size:        btnSizeSm,
				tintBg:      true,
				leading:     icoSearch,
				shortcut:    sc,
				clearable:   true,
			})
			if a.focusSearch {
				inp.Focus()
				a.focusSearch = false
			}
			if inp.Changed() {
				a.questQuery.Page = 1
				a.refreshQuests()
			}
		case tabLeves:
			inp := kitInput(c, "title-search-leve", &a.leveQuery.Q, kitInputOpts{
				placeholder: "搜索理符名、委托道具、发布地或 ID...",
				label:       "搜索理符",
				size:        btnSizeSm,
				tintBg:      true,
				leading:     icoSearch,
				shortcut:    sc,
				clearable:   true,
			})
			if a.focusSearch {
				inp.Focus()
				a.focusSearch = false
			}
			if inp.Changed() {
				a.leveQuery.Page = 1
				a.refreshLeves()
			}
		case tabAchievements:
			inp := kitInput(c, "title-search-ach", &a.achQuery.Q, kitInputOpts{
				placeholder: "搜索成就、称号或 ID...",
				label:       "搜索成就",
				size:        btnSizeSm,
				tintBg:      true,
				leading:     icoSearch,
				shortcut:    sc,
				clearable:   true,
			})
			if a.focusSearch {
				inp.Focus()
				a.focusSearch = false
			}
			if inp.Changed() {
				a.achQuery.Page = 1
				a.refreshAchievements()
			}
		case tabDiff:
			inp := kitInput(c, "title-search-diff", &a.diffSearch, kitInputOpts{
				placeholder: "搜索差异中的物品、任务、理符、成就或 ID...",
				label:       "搜索版本差异",
				size:        btnSizeSm,
				tintBg:      true,
				leading:     icoSearch,
				shortcut:    sc,
				clearable:   true,
			})
			if a.focusSearch {
				inp.Focus()
				a.focusSearch = false
			}
			if inp.Changed() {
				a.diffPage = 1
				a.refreshDiff()
			}
		default:
			inp := kitInput(c, "title-search-item", &a.itemQuery.Q, kitInputOpts{
				placeholder: "搜索物品名（中/英/日）或 ID...",
				label:       "全局物品搜索",
				size:        btnSizeSm,
				tintBg:      true,
				leading:     icoSearch,
				shortcut:    sc,
				clearable:   true,
			})
			if a.focusSearch {
				inp.Focus()
				a.focusSearch = false
			}
			if inp.Changed() {
				if a.tab != tabWorkbench {
					a.navigateTab(tabWorkbench)
				}
				a.itemQuery.Page = 1
				a.refreshItemList()
			}
		}
	})
}

// renderShellBody 绘制主体容器：左通透可折叠侧栏 + 右主面板内容区。
func (a *App) renderShellBody(c *ui.Context) {
	const edgeGap float32 = 8
	row := ui.Row(c).Grow(1).Shrink(1).FillWidth().AlignItems(ui.Stretch).Padding(0, edgeGap, edgeGap, edgeGap)

	target := a.sidebarW
	if a.sidebarHidden {
		target = 0
	}
	visibleW := row.Animate("sb-width", target, motionBase)

	row.Children(func() {
		if visibleW > 1 {
			ui.Row(c).Width(visibleW).FillHeight().Shrink(0).ClipX().Children(func() {
				ui.Column(c).Width(a.sidebarW).FillHeight().Shrink(0).Translate(visibleW-a.sidebarW, 0).
					Disabled(a.sidebarHidden).Children(func() {
					a.renderSidebar(c)
				})
			})

			if !a.sidebarHidden {
				renderSplitterGrip(c, "sb-grip", &a.sidebarW, sidebarMinWidth, sidebarMaxWidth, edgeGap, "调整侧边栏宽度")
			}
		}

		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).FillHeight().Children(func() {
			switch a.tab {
			case tabQuests:
				a.renderQuestsView(c)
			case tabLeves:
				a.renderLevesView(c)
			case tabAchievements:
				a.renderAchievementsView(c)
			case tabDiff:
				a.renderDiffView(c)
			case tabSettings:
				a.renderSettingsView(c)
			default:
				a.renderWorkbenchView(c)
			}
		})
	})
}

// renderSidebar 绘制精简左侧菜单：默认主入口「物品」置首，底部固定设置与 ET 状态。
func (a *App) renderSidebar(c *ui.Context) {
	t := themeOf(c)
	ui.Column(c).Fill().Padding(6, 4, 4, 4).Justify(ui.SpaceBetween).Children(func() {
		ui.Scroll(c.Key("sidebar-scroll")).Grow(1).Shrink(1).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				ui.Text(c, "数据").FontSize(t.text.micro).FontWeight(500).TextColor(t.muted).Padding(4, 10, 4, 10)

				achCnt := ""
				itemCnt := ""
				questCnt := ""
				leveCnt := ""
				if a.status != nil {
					achCnt = formatInt(a.status.Stats.TotalAchievements)
					itemCnt = formatInt(a.status.Stats.TotalItems)
					questCnt = formatInt(a.status.Stats.TotalQuests)
					leveCnt = formatInt(a.status.Stats.TotalLeves)
				}
				if kitSidebarItem(c, "nav-items", icoPackage, icoPackageFill, "物品", itemCnt, a.tab == tabWorkbench, false) {
					a.navigateTab(tabWorkbench)
				}
				if kitSidebarItem(c, "nav-quests", icoScroll, icoScrollFill, "任务", questCnt, a.tab == tabQuests, false) {
					a.navigateTab(tabQuests)
				}
				if kitSidebarItem(c, "nav-leves", icoClipboard, icoClipboardFill, "理符", leveCnt, a.tab == tabLeves, false) {
					a.navigateTab(tabLeves)
				}
				if kitSidebarItem(c, "nav-achievements", icoTrophy, icoTrophyFill, "成就", achCnt, a.tab == tabAchievements, false) {
					a.navigateTab(tabAchievements)
				}

				ui.Text(c, "对比").FontSize(t.text.micro).FontWeight(500).TextColor(t.muted).Padding(14, 10, 4, 10)

				hasUpdate := a.versionStatus != nil && a.versionStatus.HasVersionUpdate
				diffMeta := ""
				if hasUpdate {
					diffMeta = "有更新"
				}
				if kitSidebarItem(c, "nav-diff", icoCompare, icoCompare, "版本对比", diffMeta, a.tab == tabDiff, hasUpdate) {
					a.navigateTab(tabDiff)
				}
			})
		})

		ui.Column(c).FillWidth().Gap(6).Padding(8, 0, 0, 0).Children(func() {
			if a.versionStatus != nil && a.versionStatus.HasVersionUpdate && a.tab != tabDiff && !a.dismissUpdateBanner {
				a.renderSidebarUpdateCard(c)
			}

			kitSeparator(c, false).Margin(0, 6)

			footRow := ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(4)
			footRow.Children(func() {
				ui.Box(c).Grow(1).Shrink(1).Children(func() {
					if kitSidebarItem(c, "nav-settings", icoSettings, icoSettings, "设置", "", a.tab == tabSettings, false) {
						a.navigateTab(tabSettings)
					}
				})

				et := gamedata.GetCurrentEorzeaTime(time.Now())
				stBtn := ui.ButtonBase(c.Key("sb-status-btn")).Label("客户端状态与艾欧泽亚时间").
					Height(28).Padding(0, 8).Radius(t.radius.md).Gap(6).Shrink(0).
					AlignItems(ui.Center).Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
				if a.statusPopoverOpen || stBtn.Hovered() {
					stBtn.Background(t.tint(1.25))
				}
				stBtn.Children(func() {
					drawStatusDot(c, t.success)
					numText(c, "ET "+et.Formatted).FontSize(t.text.micro).FontWeight(500).TextColor(t.muted).SingleLine()
				})
				if stBtn.Clicked() {
					a.statusPopoverOpen = !a.statusPopoverOpen
				}
			})

			ui.PopoverBase(c.Key("sb-status-popover"), footRow, &a.statusPopoverOpen, func(panel ui.Element) {
				applyOverlaySurface(c, panel.AttachTo(footRow, ui.AnchorTopStart, ui.AnchorBottomStart).
					Margin(0, 0, 6, 0).Width(236).Padding(12).Radius(t.radius.xl), shadowOverlay)
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "客户端状态").FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground)
						ui.Text(c, "只读模式").FontSize(t.text.micro).TextColor(t.successText)
					})
					kitSeparator(c, false)
					if a.status != nil {
						if a.status.HasCN {
							ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
								ui.Text(c, "国服").FontSize(t.text.xs).TextColor(t.muted)
								numText(c, "v"+a.status.CNVersion).FontSize(t.text.xs).TextColor(t.foreground)
							})
						}
						if a.status.HasGlobal {
							ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
								ui.Text(c, "国际服").FontSize(t.text.xs).TextColor(t.muted)
								numText(c, "v"+a.status.GlobalVersion).FontSize(t.text.xs).TextColor(t.foreground)
							})
						}
						ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
							ui.Text(c, "加载耗时").FontSize(t.text.xs).TextColor(t.muted)
							numTextf(c, "%d ms", a.status.Stats.LoadTimeMs).FontSize(t.text.xs).TextColor(t.muted)
						})
					}
				})
			})
		})
	})
}

// renderSidebarUpdateCard 检测到客户端版本更新时低调提醒，支持点击直达版本对比或关闭提醒。
func (a *App) renderSidebarUpdateCard(c *ui.Context) {
	t := themeOf(c)
	ui.Row(c.Key("sb-update-card")).FillWidth().Padding(8, 8, 8, 10).Radius(t.radius.lg).Gap(6).
		Background(t.background).Border(hairline, t.border).AlignItems(ui.Center).Children(func() {
		nav := ui.ButtonBase(c.Key("sb-update-go")).Label("查看版本更新对比").
			Grow(1).Shrink(1).Cursor(ui.CursorPointer)
		nav.Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					drawStatusDot(c, t.success)
					ui.Text(c, "检测到版本更新").FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
				})
				numText(c, a.versionStatus.CurrentVersion).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
			})
		})
		if nav.Clicked() {
			a.navigateTab(tabDiff)
		}

		if kitIconButton(c, "sb-update-dismiss", icoClose, "暂不提醒", btnOpts{variant: btnGhost, size: btnSizeXs}) {
			a.dismissUpdateBanner = true
		}
	})
}

// ---------------------------------------------------------------------------
// 【物品】主工作台：左侧物品检索列表 + 右侧详情阅读区
// ---------------------------------------------------------------------------

func (a *App) renderWorkbenchView(c *ui.Context) {
	kitResizer(c, "wb-split", &a.listW, itemListMinWidth, itemListMaxWidth, 8, "调整物品列表宽度",
		func() { a.renderItemListPane(c) },
		func() { a.renderItemDetailPane(c) },
	)
}

func (a *App) countActiveAdvFilters() int {
	n := 0
	if a.itemQuery.CategoryID > 0 {
		n++
	}
	if strings.TrimSpace(a.itemMinIlvlStr) != "" || strings.TrimSpace(a.itemMaxIlvlStr) != "" {
		n++
	}
	if a.itemQuery.TradableOnly {
		n++
	}
	return n
}

func (a *App) hasAnyItemFilter() bool {
	return strings.TrimSpace(a.itemQuery.Q) != "" ||
		a.itemQuery.Filter != "all" ||
		a.itemQuery.CraftTypeID != nil ||
		a.itemQuery.Sort != "id_asc" ||
		a.countActiveAdvFilters() > 0
}

func (a *App) renderItemListPane(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		// 1. 列表头部：标题 + 结果数 + 重置/排序/筛选 + 途径过滤 + 按需展开职业/高级条件
		ui.Column(c).FillWidth().Padding(10, 12, 10, 12).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Baseline).Grow(1).Shrink(1).Children(func() {
					ui.Text(c, "物品").FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					numTextf(c, "%s 项", formatInt(a.itemRes.Total)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				})

				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.hasAnyItemFilter() {
						if kitButton(c, "wb-quick-reset", "重置", btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.resetItemFilters()
						}
					}

					if kitSelect(c, "wb-sort-select", &a.itemQuery.Sort, itemSortOptions, 112, 148, true) {
						a.itemQuery.Page = 1
						a.refreshItemList()
					}

					advCnt := a.countActiveAdvFilters()
					fltLbl := "筛选"
					if advCnt > 0 {
						fltLbl = fmt.Sprintf("筛选 (%d)", advCnt)
					}
					v := btnOutline
					if a.itemAdvFilter || advCnt > 0 {
						v = btnSecondary
					}
					if kitButton(c, "wb-adv-toggle", fltLbl, btnOpts{variant: v, size: btnSizeSm, icon: icoFilter}) {
						a.itemAdvFilter = !a.itemAdvFilter
					}
				})
			})

			// 途径筛选条（去掉冗余千位数字，保持两行内清爽排列）
			ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
				for _, f := range itemScopeFilters {
					active := a.itemQuery.Filter == f.mode
					if kitToggleChip(c, "scope-"+f.mode, f.label, "", active, nil) {
						a.itemQuery.Filter = f.mode
						if f.mode != "craftable" {
							a.itemQuery.CraftTypeID = nil
						}
						a.itemQuery.Page = 1
						a.refreshItemList()
					}
				}
			})

			// 上下文展开：能工巧匠职业条（同平面 1px 顶部分割，避免卡片套卡片）
			if (a.itemQuery.Filter == "craftable" || a.itemQuery.CraftTypeID != nil) && a.status != nil && len(a.status.CraftTypes) > 0 {
				ui.Column(c).FillWidth().Padding(8, 0, 0, 0).Gap(6).
					BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "能工巧匠职业").FontSize(t.text.micro).FontWeight(500).TextColor(t.muted)
						if a.itemQuery.CraftTypeID != nil {
							if kitButton(c, "craft-clear", "全部职业", btnOpts{variant: btnGhost, size: btnSizeXs}) {
								a.itemQuery.CraftTypeID = nil
								a.itemQuery.Page = 1
								a.refreshItemList()
							}
						}
					})
					ui.Row(c).FillWidth().Gap(4).Wrap().Children(func() {
						for _, ct := range a.status.CraftTypes {
							isSel := a.itemQuery.CraftTypeID != nil && *a.itemQuery.CraftTypeID == ct.ID
							bmp := a.bitmaps.GetIcon(a.service, a.win, ct.IconID, false)
							if kitToggleChip(c, fmt.Sprintf("craft-%d", ct.ID), ct.JobName, "", isSel, bmp) {
								if isSel {
									a.itemQuery.CraftTypeID = nil
								} else {
									id := ct.ID
									a.itemQuery.Filter = "craftable"
									a.itemQuery.CraftTypeID = &id
								}
								a.itemQuery.Page = 1
								a.refreshItemList()
							}
						}
					})
				})
			}

			// 按需展开：高级筛选区（同平面细线分隔）
			if a.itemAdvFilter {
				a.renderAdvancedFilterDrawer(c)
			}
		})

		// 2. 物品列表主体（清爽无胶囊堆砌：左下角仅显示类型，右下角仅显示品级）
		if len(a.itemRes.Items) == 0 {
			kitEmpty(c, icoSearch, "没有匹配的物品", "尝试更换关键词，或重置筛选条件。", func() {
				if kitButton(c, "wb-empty-reset", "重置筛选", btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.resetItemFilters()
				}
			})
		} else {
			ui.Scroll(c.Key("item-list-scroll")).TrackScroll(&a.itemListScroll).Grow(1).Shrink(1).Children(func() {
				ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
					for _, item := range a.itemRes.Items {
						isSel := item.ID == a.selectedItemID
						row := ui.ButtonBase(c.Key(fmt.Sprintf("item-row-%d", item.ID))).
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
						row.Children(func() {
							a.drawGameIcon(c, item.IconID, false, 34, t.radius.md)
							ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
								ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
									fw := 500
									if isSel {
										fw = 600
									}
									ui.Text(c, item.NameCn).FontSize(t.text.sm).FontWeight(fw).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
									numTextf(c, "#%d", item.ID).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
								})
								ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
									sub := nonEmptyStr(item.CategoryName, "其他")
									ui.Text(c, sub).FontSize(t.text.xs).TextColor(t.muted).SingleLine().Grow(1).Shrink(1)

									metaParts := buildItemRowMeta(item)
									numText(c, metaParts).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
								})
							})
						})
						if row.Clicked() {
							a.selectItem(item.ID)
						}
					}
				})
			})
		}

		// 3. 底部分页条
		a.renderPaginationBar(
			c,
			"item-pg",
			a.itemRes.Page,
			a.itemRes.TotalPages,
			&a.itemPageStr,
			func(next int) {
				a.itemQuery.Page = next
				a.refreshItemList()
			},
		)
	})
}

// buildItemRowMeta 生成物品列表行右下角的品级文本。
func buildItemRowMeta(item gamedata.ItemSummary) string {
	return fmt.Sprintf("iLv %d", item.ItemLevel)
}

func (a *App) renderAdvancedFilterDrawer(c *ui.Context) {
	t := themeOf(c)
	ui.Column(c).FillWidth().Padding(8, 0, 2, 0).Gap(8).
		BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
		catOpts := []selectOption[int]{{value: 0, label: "全部分类"}}
		if a.status != nil {
			catOpts[0].label = fmt.Sprintf("全部分类 (%d)", len(a.status.Categories))
			for _, cat := range a.status.Categories {
				catOpts = append(catOpts, selectOption[int]{
					value: cat.ID,
					label: fmt.Sprintf("%s (%d)", cat.Name, cat.Count),
				})
			}
		}
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "物品分类").FontSize(t.text.xs).TextColor(t.muted).Shrink(0)
			if kitSelect(c, "wb-cat-select", &a.itemQuery.CategoryID, catOpts, 200, 240, true) {
				a.itemQuery.Page = 1
				a.refreshItemList()
			}
		})

		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "品级区间").FontSize(t.text.xs).TextColor(t.muted).Shrink(0)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(74).Children(func() {
					if kitInput(c, "wb-min-ilvl", &a.itemMinIlvlStr, kitInputOpts{
						placeholder: "最低", label: "最低品级", size: btnSizeSm,
					}).Changed() {
						a.itemQuery.Page = 1
						a.refreshItemList()
					}
				})
				ui.Text(c, "–").FontSize(t.text.xs).TextColor(t.muted)
				ui.Box(c).Width(74).Children(func() {
					if kitInput(c, "wb-max-ilvl", &a.itemMaxIlvlStr, kitInputOpts{
						placeholder: "最高", label: "最高品级", size: btnSizeSm,
					}).Changed() {
						a.itemQuery.Page = 1
						a.refreshItemList()
					}
				})
			})
		})

		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if kitSwitch(c, "wb-tradable", &a.itemQuery.TradableOnly, "仅可交易") {
					a.itemQuery.Page = 1
					a.refreshItemList()
				}
				ui.Text(c, "仅显示可交易").FontSize(t.text.xs).TextColor(t.subdued)
			})
			if kitButton(c, "wb-reset-btn", "重置筛选", btnOpts{variant: btnGhost, size: btnSizeXs, disabled: !a.hasAnyItemFilter()}) {
				a.resetItemFilters()
			}
		})
	})
}

func (a *App) resetItemFilters() {
	a.itemQuery = gamedata.ItemSearchQuery{
		Filter:   "all",
		Sort:     "id_asc",
		Page:     1,
		PageSize: 30,
	}
	a.itemMinIlvlStr = ""
	a.itemMaxIlvlStr = ""
	a.refreshItemList()
}

func (a *App) renderPaginationBar(
	c *ui.Context,
	keyPrefix string,
	page, totalPages int,
	pageInput *string,
	onGo func(int),
) {
	t := themeOf(c)
	ui.Row(c).FillWidth().Height(40).Padding(0, 12).
		BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).
		Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Row(c).Gap(4).Children(func() {
			if kitButton(c, keyPrefix+"-first", "首页", btnOpts{variant: btnGhost, size: btnSizeXs, disabled: page <= 1}) {
				onGo(1)
			}
			if kitButton(c, keyPrefix+"-prev", "上一页", btnOpts{variant: btnOutline, size: btnSizeXs, disabled: page <= 1}) {
				onGo(max(1, page-1))
			}
		})

		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(46).Children(func() {
				inp := kitInput(c, keyPrefix+"-input", pageInput, kitInputOpts{
					label: "跳转页码", size: btnSizeXs,
				})
				if inp.Submitted() {
					if p, err := strconv.Atoi(strings.TrimSpace(*pageInput)); err == nil {
						p = max(1, min(totalPages, p))
						*pageInput = strconv.Itoa(p)
						if p != page {
							onGo(p)
						}
					} else {
						*pageInput = strconv.Itoa(page)
					}
				}
			})
			numTextf(c, "/ %d", totalPages).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
		})

		ui.Row(c).Gap(4).Children(func() {
			if kitButton(c, keyPrefix+"-next", "下一页", btnOpts{variant: btnOutline, size: btnSizeXs, disabled: page >= totalPages}) {
				onGo(min(totalPages, page+1))
			}
			if kitButton(c, keyPrefix+"-last", "末页", btnOpts{variant: btnGhost, size: btnSizeXs, disabled: page >= totalPages}) {
				onGo(totalPages)
			}
		})
	})
}
