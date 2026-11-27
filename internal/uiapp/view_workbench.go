package uiapp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type itemScopeDef struct {
	mode  string
	label string
}

// itemScopeFilters 返回物品界面内的快捷数据视图筛选选项。
func (a *App) itemScopeFilters() []itemScopeDef {
	return []itemScopeDef{
		{mode: "all", label: a.t("items.scope_all")},
		{mode: "craftable", label: a.t("items.scope_craftable")},
		{mode: "gatherable", label: a.t("items.scope_gatherable")},
		{mode: "gil_shop", label: a.t("items.scope_gil_shop")},
		{mode: "special_shop", label: a.t("items.scope_special_shop")},
		{mode: "retainer_venture", label: a.t("items.scope_retainer_venture")},
		{mode: "collectable_turnin", label: a.t("items.scope_collectable_turnin")},
		{mode: "equipment", label: a.t("items.scope_equipment")},
		{mode: "consumable", label: a.t("items.scope_consumable")},
	}
}

func (a *App) itemSortOptions() []selectOption[string] {
	return []selectOption[string]{
		{value: "id_asc", label: a.t("items.sort_id_asc")},
		{value: "id_desc", label: a.t("items.sort_id_desc")},
		{value: "ilvl_desc", label: a.t("items.sort_ilvl_desc")},
		{value: "ilvl_asc", label: a.t("items.sort_ilvl_asc")},
		{value: "level_desc", label: a.t("items.sort_level_desc")},
	}
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
	if bmp := appLogoBitmap(); bmp != nil {
		ui.Image(c, bmp).Size(iconLg, iconLg).Fit(ui.Contain).Shrink(0)
	} else {
		ui.Icon(c, icoAppLogo).Size(iconMd, iconMd).TextColor(t.primary)
	}
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
			ui.Row(c).Gap(7).AlignItems(ui.Center).Shrink(0).Children(func() {
				a.renderBrandCaption(c)
			})
		}
		ui.Spacer(c).MinWidth(12)
		a.renderTitleLanguageButton(c)
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

			sbTip := a.tf("shell.collapse_sidebar", "shortcut", shortcutLabel("B"))
			if a.sidebarHidden {
				sbTip = a.tf("shell.expand_sidebar", "shortcut", shortcutLabel("B"))
			}
			if kitIconButton(c, "tb-toggle-sb", icoPanelLeft, sbTip, btnOpts{variant: btnGhost, size: btnSizeSm}) {
				a.sidebarHidden = !a.sidebarHidden
			}
			if kitIconButton(c, "tb-back", icoArrowLeft, a.t("shell.back"), btnOpts{variant: btnGhost, size: btnSizeSm, disabled: len(a.tabHistory) == 0}) {
				a.goBack()
			}
			if kitIconButton(c, "tb-fwd", icoArrowRight, a.t("shell.forward"), btnOpts{variant: btnGhost, size: btnSizeSm, disabled: len(a.tabAhead) == 0}) {
				a.goForward()
			}
		})

		ui.Spacer(c).MinWidth(12)

		// 居中上下文全局检索栏（按当前视图智能绑定物品 / 成就 / Diff 检索）
		a.renderTitleSearch(c)

		ui.Spacer(c).MinWidth(12)

		// 右侧：全局语言切换按钮（全界面随时切换）
		a.renderTitleLanguageButton(c)
	})
}

// renderTitleLanguageButton 在窗口右上角渲染语言切换按钮及浮层菜单，支持在任意页面随时切换。
func (a *App) renderTitleLanguageButton(c *ui.Context) {
	t := themeOf(c)
	curLang := a.Language()
	curOpt := i18n.OptionOf(curLang)

	btn := ui.ButtonBase(c.Key("title-lang-btn")).Label(a.t("lang.switch_tip")).
		Height(28).Padding(0, 8).Radius(t.radius.md).Gap(5).
		AlignItems(ui.Center).Shrink(0).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
	if a.langPopoverOpen || btn.Hovered() {
		btn.Background(t.tint(1))
	}
	kitTooltip(c, btn, a.t("lang.switch_tip"))
	btn.Children(func() {
		fg := t.subdued
		if a.langPopoverOpen || btn.Hovered() {
			fg = t.foreground
		}
		if a.langSwitching {
			ui.Spinner(c).FontSize(iconSm).TextColor(t.primary)
		} else {
			ui.Icon(c, icoGlobe).Size(iconSm, iconSm).Shrink(0).TextColor(fg)
		}
		ui.Text(c, curOpt.Autonym).FontSize(t.text.xs).FontWeight(500).TextColor(fg).SingleLine()
		ui.Icon(c, icoChevronDown).Size(iconXs, iconXs).Shrink(0).TextColor(t.muted)
	})
	if btn.Clicked() {
		a.langPopoverOpen = !a.langPopoverOpen
	}

	ui.PopoverBase(c.Key("title-lang-popover"), btn, &a.langPopoverOpen, func(panel ui.Element) {
		applyOverlaySurface(c, panel.AttachTo(btn, ui.AnchorBottomEnd, ui.AnchorTopEnd).
			Margin(4, 0, 0, 0).Width(156).Padding(4).Radius(t.radius.lg), shadowOverlay)
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			for _, opt := range i18n.SupportedLangs {
				active := curLang == opt.Code
				row := ui.ButtonBase(c.Key("title-lang-opt-"+string(opt.Code))).Label(opt.Autonym).
					FillWidth().Height(28).Padding(0, 8).Radius(t.radius.md).
					Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).
					Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
				if row.Hovered() {
					row.Background(t.tint(1.25))
				}
				row.Children(func() {
					fw := 400
					if active {
						fw = 600
					}
					ui.Text(c, opt.Autonym).FontSize(t.text.xs).FontWeight(fw).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
					if active {
						ui.Icon(c, icoCheck).Size(iconXs, iconXs).Shrink(0).TextColor(t.primary)
					}
				})
				if row.Clicked() {
					a.langPopoverOpen = false
					if !active {
						a.SetLanguage(opt.Code)
					}
				}
			}
		})
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
		if m.Item(a.t("menu.export_csv")).Chosen() {
			a.openExportModal()
		}
		if m.Item(a.t("menu.snapshot_manager")).Disabled(a.backupSvc == nil).Chosen() {
			a.navigateTab(tabDiff)
			a.openSnapshotManager()
		}
		m.Separator()
		if m.Item(a.t("menu.theme_light")).Checked(a.cfg.Theme == "light").Chosen() {
			a.SetThemeMode("light")
		}
		if m.Item(a.t("menu.theme_dark")).Checked(a.cfg.Theme == "dark").Chosen() {
			a.SetThemeMode("dark")
		}
		if m.Item(a.t("menu.theme_system")).Checked(a.cfg.Theme == "" || a.cfg.Theme == "system").Chosen() {
			a.SetThemeMode("system")
		}
		m.Separator()
		if m.Item(a.t("menu.settings")).Chosen() {
			a.navigateTab(tabSettings)
		}
		m.Separator()
		if m.Item(a.t("menu.check_update")).Chosen() {
			a.openManualUpdateCheck()
		}
	})
	b.Children(func() {
		a.renderBrandCaption(c)
	})
}

type globalJumpTarget struct {
	tab   activeTab
	label string
	icon  *ui.SVG
}

func (a *App) globalJumpTargets() []globalJumpTarget {
	return []globalJumpTarget{
		{tab: tabWorkbench, label: a.t("search.global_module_item"), icon: icoPackage},
		{tab: tabQuests, label: a.t("search.global_module_quest"), icon: icoScroll},
		{tab: tabLeves, label: a.t("search.global_module_leve"), icon: icoClipboard},
		{tab: tabAchievements, label: a.t("search.global_module_achievement"), icon: icoTrophy},
		{tab: tabMusic, label: a.t("search.global_module_music"), icon: icoMusic},
		{tab: tabIcons, label: a.t("search.global_module_icon"), icon: icoImage},
		{tab: tabDiff, label: a.t("search.global_module_diff"), icon: icoCompare},
		{tab: tabTextSearch, label: a.t("search.global_module_text_search"), icon: icoTextSearch},
	}
}

func (a *App) defaultGlobalSearchIdx() int {
	if len(a.globalSearchRes.Hits) > 0 {
		return 0
	}
	targets := a.globalJumpTargets()
	for i, jt := range targets {
		if jt.tab == a.tab {
			return i
		}
	}
	return 0
}

func (a *App) executeGlobalSearchAction(idx int) {
	hits := a.globalSearchRes.Hits
	targets := a.globalJumpTargets()
	total := len(hits) + len(targets)
	if total == 0 {
		return
	}
	if idx < 0 || idx >= total {
		idx = a.defaultGlobalSearchIdx()
	}
	if idx < len(hits) {
		a.openGlobalSearchHit(hits[idx])
		return
	}
	a.jumpToModuleSearch(targets[idx-len(hits)].tab)
}

func (a *App) globalHitBadgeAndIcon(kind gamedata.GlobalSearchHitKind) (string, pillKind, *ui.SVG) {
	switch kind {
	case gamedata.GlobalHitQuest:
		return a.t("search.global_module_quest"), pillWarning, icoScroll
	case gamedata.GlobalHitLeve:
		return a.t("search.global_module_leve"), pillPositive, icoClipboard
	case gamedata.GlobalHitAchievement:
		return a.t("search.global_module_achievement"), pillAccent, icoTrophy
	case gamedata.GlobalHitMusic:
		return a.t("search.global_module_music"), pillAccent, icoMusic
	default:
		return a.t("search.global_module_item"), pillNeutral, icoPackage
	}
}

func (a *App) formatGlobalHitMeta(hit gamedata.GlobalSearchHit) string {
	parts := []string{fmt.Sprintf("#%d", hit.ID)}
	if hit.Category != "" {
		parts = append(parts, hit.Category)
	}
	switch hit.Kind {
	case gamedata.GlobalHitItem:
		if hit.ItemLevel > 0 {
			parts = append(parts, fmt.Sprintf("iLv %d", hit.ItemLevel))
		}
	case gamedata.GlobalHitQuest, gamedata.GlobalHitLeve:
		if hit.Level > 0 {
			parts = append(parts, fmt.Sprintf("Lv.%d", hit.Level))
		}
	case gamedata.GlobalHitAchievement:
		if hit.Points > 0 {
			parts = append(parts, a.tf("search.global_ach_points", "pts", strconv.Itoa(hit.Points)))
		}
	}
	return strings.Join(parts, " · ")
}

// renderPageSearchInput 绘制各模块页面内统一的内置搜索栏，保持一致的尺寸、图标、清零与 Ctrl+F 聚焦体验。
func (a *App) renderPageSearchInput(c *ui.Context, key string, val *string, placeholder, label string, onChanged func()) {
	inp := kitInput(c, key, val, kitInputOpts{
		placeholder: placeholder,
		label:       label,
		size:        btnSizeSm,
		tintBg:      true,
		leading:     icoSearch,
		shortcut:    shortcutLabel("F"),
		clearable:   true,
	})
	if a.focusPageSearch {
		inp.Focus()
		a.focusPageSearch = false
	}
	if inp.Changed() && onChanged != nil {
		onChanged()
	}
}

func (a *App) renderTitleSearch(c *ui.Context) {
	t := themeOf(c)
	sc := shortcutLabel("K")
	ui.Box(c).Width(420).MinWidth(200).Shrink(1).Children(func() {
		inp, anchor := kitInputWithContainer(c, "title-search-global", &a.globalSearchQ, kitInputOpts{
			placeholder: a.t("search.global_placeholder"),
			label:       a.t("search.global_label"),
			size:        btnSizeSm,
			tintBg:      true,
			leading:     icoSearch,
			shortcut:    sc,
			clearable:   true,
		})

		if a.focusSearch {
			inp.Focus()
			a.focusSearch = false
			if strings.TrimSpace(a.globalSearchQ) != "" {
				a.refreshGlobalSearch()
				a.globalSearchOpen = true
				a.globalSearchIdx = a.defaultGlobalSearchIdx()
			}
		}

		if inp.Changed() {
			a.globalSearchScroll.Y = 0
			if strings.TrimSpace(a.globalSearchQ) == "" {
				a.globalSearchOpen = false
				a.globalSearchRes = gamedata.GlobalSearchResult{}
				a.globalSearchIdx = 0
			} else {
				a.refreshGlobalSearch()
				a.globalSearchOpen = true
				a.globalSearchIdx = a.defaultGlobalSearchIdx()
			}
		} else if inp.Clicked() && !a.globalSearchOpen && strings.TrimSpace(a.globalSearchQ) != "" {
			a.globalSearchScroll.Y = 0
			a.refreshGlobalSearch()
			a.globalSearchOpen = true
			a.globalSearchIdx = a.defaultGlobalSearchIdx()
		}

		hits := a.globalSearchRes.Hits
		targets := a.globalJumpTargets()
		totalActions := len(hits) + len(targets)
		if totalActions > 0 && (a.globalSearchIdx < 0 || a.globalSearchIdx >= totalActions) {
			a.globalSearchIdx = a.defaultGlobalSearchIdx()
		}

		if totalActions > 0 {
			if a.globalSearchOpen {
				if inp.Shortcut(0, ui.KeyDown) {
					a.globalSearchIdx = (a.globalSearchIdx + 1) % totalActions
					a.globalSearchScrollTo = true
				}
				if inp.Shortcut(0, ui.KeyUp) {
					a.globalSearchIdx = (a.globalSearchIdx - 1 + totalActions) % totalActions
					a.globalSearchScrollTo = true
				}
			} else if strings.TrimSpace(a.globalSearchQ) != "" && inp.Shortcut(0, ui.KeyDown) {
				a.refreshGlobalSearch()
				a.globalSearchOpen = true
				a.globalSearchIdx = a.defaultGlobalSearchIdx()
				a.globalSearchScrollTo = true
			}
		}

		if inp.Submitted() && strings.TrimSpace(a.globalSearchQ) != "" {
			if !a.globalSearchOpen {
				a.refreshGlobalSearch()
				hits = a.globalSearchRes.Hits
				a.globalSearchIdx = a.defaultGlobalSearchIdx()
			}
			a.executeGlobalSearchAction(a.globalSearchIdx)
		}

		ui.PopoverBase(c.Key("global-search-popover"), anchor, &a.globalSearchOpen, func(panel ui.Element) {
			panel.KeepFocus()
			// 固定高度并内置结果区滚动、底部跳转区常驻，避免搜索结果数变化导致面板高度抖动
			applyOverlaySurface(c, panel.AttachTo(anchor, ui.AnchorBottomStart, ui.AnchorTopStart).
				Margin(6, 0, 0, 0).Width(480).Height(448).Clip().Radius(t.radius.xl), shadowOverlay)

			ui.Column(c).Fill().Padding(6).Gap(0).Children(func() {
				// 1. 顶部结果摘要栏（固定高度）
				ui.Row(c).FillWidth().Shrink(0).Padding(4, 8, 4, 8).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.t("search.global_matches_header")).FontSize(t.text.micro).FontWeight(500).TextColor(t.muted).SingleLine()
					if a.globalSearchRes.TotalMatched > len(hits) {
						numText(c, a.tf("search.global_matches_capped",
							"shown", formatInt(len(hits)),
							"count", formatInt(a.globalSearchRes.TotalMatched),
						)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
					} else if a.globalSearchRes.TotalMatched > 0 {
						numText(c, a.tf("search.global_matches_count", "count", formatInt(a.globalSearchRes.TotalMatched))).
							FontSize(t.text.micro).TextColor(t.muted).SingleLine()
					}
				})

				// 2. 中部匹配结果区（占满剩余弹性高度并内部滚动）
				ui.Scroll(c.Key("global-search-scroll")).TrackScroll(&a.globalSearchScroll).Grow(1).Shrink(1).FillWidth().Children(func() {
					if len(hits) == 0 {
						ui.Column(c).FillWidth().Padding(28, 12).Center().Children(func() {
							ui.Text(c, a.t("search.global_no_matches")).FontSize(t.text.xs).TextColor(t.muted).TextAlign(ui.Center)
						})
					} else {
						ui.Column(c).FillWidth().Gap(2).Children(func() {
							for i, hit := range hits {
								active := a.globalSearchIdx == i
								modLabel, pillKind, fallbackIco := a.globalHitBadgeAndIcon(hit.Kind)
								btn := ui.ButtonBase(c.Key(fmt.Sprintf("gs-hit-%s-%d", hit.Kind, hit.ID))).
									Label(hit.Title).FillWidth().Padding(6, 8).Gap(8).Radius(t.radius.md).
									Justify(ui.SpaceBetween).AlignItems(ui.Center).
									Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
								if btn.Hovered() || active {
									btn.Background(t.tint(1.25))
								}
								if active && a.globalSearchScrollTo {
									btn.ScrollIntoView()
									a.globalSearchScrollTo = false
								}
								btn.Children(func() {
									ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
										if hit.IconID > 0 {
											a.drawGameIcon(c, hit.IconID, false, 28, t.radius.md)
										} else {
											ui.Box(c).Size(28, 28).Shrink(0).Radius(t.radius.md).Background(t.card).Border(hairline, t.border).Center().Children(func() {
												ui.Icon(c, fallbackIco).Size(iconSm, iconSm).TextColor(t.muted)
											})
										}
										ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
											ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Baseline).Children(func() {
												fw := 500
												if active {
													fw = 600
												}
												ui.Text(c, hit.Title).FontSize(t.text.sm).FontWeight(fw).TextColor(t.foreground).SingleLine().Shrink(1)
												if hit.MatchedAlias != "" {
													ui.Text(c, hit.MatchedAlias).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(1)
												}
											})
											numText(c, a.formatGlobalHitMeta(hit)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
										})
									})
									ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
										if a.isNewGlobalHit(hit) {
											a.drawLatestBadge(c)
										}
										drawPill(c, modLabel, pillKind)
									})
								})
								if btn.Clicked() {
									a.openGlobalSearchHit(hit)
								}
							}
						})
					}
				})

				kitSeparator(c, false).Margin(4, 6)

				// 3. 底部固定跳转搜索区（始终显示，不随上方结果滚动消失）
				ui.Column(c).FillWidth().Shrink(0).Gap(1).Children(func() {
					ui.Row(c).FillWidth().Padding(2, 8, 2, 8).Children(func() {
						ui.Text(c, a.t("search.global_jump_header")).FontSize(t.text.micro).FontWeight(500).TextColor(t.muted).SingleLine()
					})

					trimmedQ := strings.TrimSpace(a.globalSearchQ)
					for j, jt := range targets {
						actionIdx := len(hits) + j
						active := a.globalSearchIdx == actionIdx
						isCurrent := jt.tab == a.tab
						actionLabel := a.tf("search.global_jump_action", "module", jt.label)
						row := ui.ButtonBase(c.Key(fmt.Sprintf("gs-jump-%d", jt.tab))).
							Label(actionLabel).FillWidth().Height(28).Padding(0, 8).Gap(8).Radius(t.radius.md).
							Justify(ui.SpaceBetween).AlignItems(ui.Center).
							Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
						if row.Hovered() || active {
							row.Background(t.tint(1.25))
						}
						if active && a.globalSearchScrollTo {
							a.globalSearchScrollTo = false
						}
						row.Children(func() {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
								icoCol := t.muted
								if active || row.Hovered() {
									icoCol = t.primary
								}
								ui.Icon(c, jt.icon).Size(iconSm, iconSm).Shrink(0).TextColor(icoCol)
								ui.Text(c, actionLabel).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine().Shrink(0)
								if trimmedQ != "" {
									ui.Text(c, formatQuotedQuery(a.Language(), trimmedQ)).FontSize(t.text.xs).TextColor(t.muted).SingleLine().Grow(1).Shrink(1)
								}
							})
							ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
								if isCurrent {
									drawPill(c, a.t("search.global_current_page"), pillOutline)
								}
								ui.Icon(c, icoArrowRight).Size(iconXs, iconXs).TextColor(t.muted)
							})
						})
						if row.Clicked() {
							a.jumpToModuleSearch(jt.tab)
						}
					}
				})
			})
		})
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
				renderSplitterGrip(c, "sb-grip", &a.sidebarW, sidebarMinWidth, sidebarMaxWidth, edgeGap, a.t("shell.resize_sidebar"))
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
			case tabMusic:
				a.renderMusicView(c)
			case tabIcons:
				a.renderIconsView(c)
			case tabRecent:
				a.renderRecentView(c)
			case tabFavorites:
				a.renderFavoritesView(c)
			case tabDiff:
				a.renderDiffView(c)
			case tabTextSearch:
				a.renderTextSearchView(c)
			case tabChangelog:
				a.renderChangelogView(c)
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
				ui.Text(c, a.t("nav.section.data")).FontSize(t.text.micro).FontWeight(500).TextColor(t.muted).Padding(4, 10, 4, 10)

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
				if kitSidebarItem(c, "nav-items", icoPackage, icoPackageFill, a.t("nav.items"), itemCnt, a.tab == tabWorkbench, false) {
					a.navigateTab(tabWorkbench)
				}
				if kitSidebarItem(c, "nav-quests", icoScroll, icoScrollFill, a.t("nav.quests"), questCnt, a.tab == tabQuests, false) {
					a.navigateTab(tabQuests)
				}
				if kitSidebarItem(c, "nav-leves", icoClipboard, icoClipboardFill, a.t("nav.leves"), leveCnt, a.tab == tabLeves, false) {
					a.navigateTab(tabLeves)
				}
				if kitSidebarItem(c, "nav-achievements", icoTrophy, icoTrophyFill, a.t("nav.achievements"), achCnt, a.tab == tabAchievements, false) {
					a.navigateTab(tabAchievements)
				}

				ui.Text(c, a.t("nav.section.resources")).FontSize(t.text.micro).FontWeight(500).TextColor(t.muted).Padding(14, 10, 4, 10)
				musicCnt := ""
				if a.status != nil && a.status.Stats.TotalMusic > 0 {
					musicCnt = formatInt(a.status.Stats.TotalMusic)
				}
				if kitSidebarItem(c, "nav-music", icoMusic, icoMusicFill, a.t("nav.music"), musicCnt, a.tab == tabMusic, false) {
					a.navigateTab(tabMusic)
				}
				iconCnt := ""
				if a.status != nil && a.status.Stats.TotalIcons > 0 {
					iconCnt = formatInt(a.status.Stats.TotalIcons)
				}
				if kitSidebarItem(c, "nav-icons", icoImage, icoImageFill, a.t("nav.icons"), iconCnt, a.tab == tabIcons, false) {
					a.navigateTab(tabIcons)
				}

				ui.Text(c, a.t("nav.section.history")).FontSize(t.text.micro).FontWeight(500).TextColor(t.muted).Padding(14, 10, 4, 10)
				recentCnt := ""
				if cnt := len(a.records.Recent); cnt > 0 {
					recentCnt = formatInt(cnt)
				}
				favCnt := ""
				if cnt := len(a.records.Favorites); cnt > 0 {
					favCnt = formatInt(cnt)
				}
				if kitSidebarItem(c, "nav-recent", icoClock, icoClockFill, a.t("nav.recent"), recentCnt, a.tab == tabRecent, false) {
					a.navigateTab(tabRecent)
				}
				if kitSidebarItem(c, "nav-favorites", icoStar, icoStarFill, a.t("nav.favorites"), favCnt, a.tab == tabFavorites, false) {
					a.navigateTab(tabFavorites)
				}

				ui.Text(c, a.t("nav.section.tools")).FontSize(t.text.micro).FontWeight(500).TextColor(t.muted).Padding(14, 10, 4, 10)

				if kitSidebarItem(c, "nav-diff", icoCompare, icoCompare, a.t("nav.diff"), "", a.tab == tabDiff, false) {
					a.navigateTab(tabDiff)
				}
				if kitSidebarItem(c, "nav-text-search", icoTextSearch, icoTextSearchFill, a.t("nav.text_search"), "", a.tab == tabTextSearch, false) {
					a.navigateTab(tabTextSearch)
				}
			})
		})

		ui.Column(c).FillWidth().Gap(6).Padding(8, 0, 0, 0).Children(func() {
			if a.updateState == updateReady {
				a.renderSidebarAppRestartCard(c)
			}

			kitSeparator(c, false).Margin(0, 6)

			ui.Column(c).FillWidth().Gap(2).Children(func() {
				if kitSidebarItem(c, "nav-changelog", icoChangelog, icoChangelogFill, a.t("nav.changelog"), "", a.tab == tabChangelog, false) {
					a.navigateTab(tabChangelog)
				}

				footRow := ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(4)
				footRow.Children(func() {
					ui.Box(c).Grow(1).Shrink(1).Children(func() {
						if kitSidebarItem(c, "nav-settings", icoSettings, icoSettings, a.t("nav.settings"), "", a.tab == tabSettings, false) {
							a.navigateTab(tabSettings)
						}
					})

					et := gamedata.GetCurrentEorzeaTime(time.Now())
					stBtn := ui.ButtonBase(c.Key("sb-status-btn")).Label(a.t("shell.status_btn_label")).
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
							ui.Text(c, a.t("shell.client_status")).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground)
							ui.Text(c, a.t("common.readonly_mode")).FontSize(t.text.micro).TextColor(t.successText)
						})
						kitSeparator(c, false)
						if a.status != nil {
							if a.status.HasCN {
								ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
									ui.Text(c, a.t("shell.server_cn")).FontSize(t.text.xs).TextColor(t.muted)
									numText(c, "v"+a.status.CNVersion).FontSize(t.text.xs).TextColor(t.foreground)
								})
							}
							if a.status.HasGlobal {
								ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
									ui.Text(c, a.t("shell.server_global")).FontSize(t.text.xs).TextColor(t.muted)
									numText(c, "v"+a.status.GlobalVersion).FontSize(t.text.xs).TextColor(t.foreground)
								})
							}
							ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
								ui.Text(c, a.t("shell.load_time")).FontSize(t.text.xs).TextColor(t.muted)
								numTextf(c, "%d ms", a.status.Stats.LoadTimeMs).FontSize(t.text.xs).TextColor(t.muted)
							})
						}
					})
				})
			})
		})
	})
}

// renderSidebarAppRestartCard 在左下角显示应用新版本已就绪提醒与一键重启按钮。
func (a *App) renderSidebarAppRestartCard(c *ui.Context) {
	t := themeOf(c)
	card := kitBlock(c, ui.Column(c.Key("sb-app-restart-card")).FillWidth().Padding(10).Gap(8), toneInfo)
	card.Children(func() {
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				drawStatusDot(c, t.primary)
				ui.Text(c, a.t("update.sidebar_ready_title")).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
			})
			if ver := formatAppVersionTag(a.updateReadyVer); ver != "" {
				numText(c, ver).FontSize(t.text.micro).FontWeight(500).TextColor(t.infoText).SingleLine().Shrink(0)
			}
		})

		if kitButton(c, "sb-app-restart-btn", a.t("update.sidebar_restart_btn"), btnOpts{
			variant:   btnDefault,
			size:      btnSizeSm,
			icon:      icoRefresh,
			fillWidth: true,
		}) {
			a.restartForUpdate()
		}
	})
}

// ---------------------------------------------------------------------------
// 【物品】主工作台：左侧物品检索列表 + 右侧详情阅读区
// ---------------------------------------------------------------------------

func (a *App) renderWorkbenchView(c *ui.Context) {
	kitResizer(c, "wb-split", &a.listW, itemListMinWidth, itemListMaxWidth, 8, a.t("shell.resize_item_list"),
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
		a.itemQuery.NewOnly ||
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
					ui.Text(c, a.t("items.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					numText(c, a.tf("common.items_count", "count", formatInt(a.itemRes.Total))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				})

				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.hasAnyItemFilter() {
						if kitButton(c, "wb-quick-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.resetItemFilters()
						}
					}

					if kitSelect(c, "wb-sort-select", &a.itemQuery.Sort, a.itemSortOptions(), 126, 184, true) {
						a.itemQuery.Page = 1
						a.refreshItemList()
					}

					advCnt := a.countActiveAdvFilters()
					fltLbl := a.t("items.filter")
					if advCnt > 0 {
						fltLbl = a.tf("items.filter_count", "count", strconv.Itoa(advCnt))
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

			// 页面内置搜索栏
			a.renderPageSearchInput(c, "wb-page-search", &a.itemQuery.Q, a.t("search.item_placeholder"), a.t("search.item_label"), func() {
				a.itemQuery.Page = 1
				a.refreshItemList()
			})

			// 途径筛选条（去掉冗余千位数字，保持两行内清爽排列；有上一版本快照时附带最新版本新增筛选）
			ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
				for _, f := range a.itemScopeFilters() {
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
				if a.renderLatestFilterChip(c, "scope-latest-new", a.latestItemCount(), a.itemQuery.NewOnly) {
					a.itemQuery.NewOnly = !a.itemQuery.NewOnly
					a.itemQuery.Page = 1
					a.refreshItemList()
				}
			})

			// 上下文展开：能工巧匠职业条（同平面 1px 顶部分割，避免卡片套卡片）
			if (a.itemQuery.Filter == "craftable" || a.itemQuery.CraftTypeID != nil) && a.status != nil && len(a.status.CraftTypes) > 0 {
				ui.Column(c).FillWidth().Padding(8, 0, 0, 0).Gap(6).
					BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
						ui.Text(c, a.t("items.craft_jobs")).FontSize(t.text.micro).FontWeight(500).TextColor(t.muted)
						if a.itemQuery.CraftTypeID != nil {
							if kitButton(c, "craft-clear", a.t("items.all_jobs"), btnOpts{variant: btnGhost, size: btnSizeXs}) {
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
			kitEmpty(c, icoSearch, a.t("items.empty_title"), a.t("items.empty_desc"), func() {
				if kitButton(c, "wb-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.resetItemFilters()
				}
			})
		} else {
			ui.Scroll(c.Key("item-list-scroll")).TrackScroll(&a.itemListScroll).Grow(1).Shrink(1).Children(func() {
				ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
					for _, item := range a.itemRes.Items {
						isSel := item.ID == a.selectedItemID
						fav := a.isFavorite(config.RecordKindItem, item.ID)
						if a.renderItemListRow(c, fmt.Sprintf("item-row-%d", item.ID), item, isSel, fav, &a.scrollItemListToID) {
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

// renderItemListRow 绘制单条物品列表行（供「物品」工作台与「收藏」物品标签页复用）。
func (a *App) renderItemListRow(c *ui.Context, key string, item gamedata.ItemSummary, isSel, favorited bool, scrollToID *int) bool {
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
	row.Children(func() {
		a.drawGameIcon(c, item.IconID, false, 34, t.radius.md)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
				fw := 500
				if isSel {
					fw = 600
				}
				ui.Text(c, item.NameCn).FontSize(t.text.sm).FontWeight(fw).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.isNewItem(item.ID) {
						a.drawLatestBadge(c)
					}
					if favorited {
						ui.Icon(c, icoStarFill).Size(11, 11).TextColor(t.attention)
					}
					numTextf(c, "#%d", item.ID).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
				})
			})
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
				sub := nonEmptyStr(item.CategoryName, a.t("common.other"))
				ui.Text(c, sub).FontSize(t.text.xs).TextColor(t.muted).SingleLine().Grow(1).Shrink(1)

				metaParts := buildItemRowMeta(item)
				numText(c, metaParts).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
			})
		})
	})
	if scrollToID != nil && item.ID == *scrollToID {
		row.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter})
		*scrollToID = 0
	}
	return row.Clicked()
}

// buildItemRowMeta 生成物品列表行右下角的品级文本。
func buildItemRowMeta(item gamedata.ItemSummary) string {
	return fmt.Sprintf("iLv %d", item.ItemLevel)
}

func (a *App) renderAdvancedFilterDrawer(c *ui.Context) {
	t := themeOf(c)
	ui.Column(c).FillWidth().Padding(8, 0, 2, 0).Gap(8).
		BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
		catOpts := []selectOption[int]{{value: 0, label: a.t("items.all_categories")}}
		if a.status != nil {
			catOpts[0].label = a.tf("items.all_categories_count", "count", strconv.Itoa(len(a.status.Categories)))
			for _, cat := range a.status.Categories {
				catOpts = append(catOpts, selectOption[int]{
					value: cat.ID,
					label: fmt.Sprintf("%s (%d)", cat.Name, cat.Count),
				})
			}
		}
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, a.t("items.category_label")).FontSize(t.text.xs).TextColor(t.muted).Shrink(0)
			if kitSelect(c, "wb-cat-select", &a.itemQuery.CategoryID, catOpts, 200, 240, true) {
				a.itemQuery.Page = 1
				a.refreshItemList()
			}
		})

		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, a.t("items.ilvl_range")).FontSize(t.text.xs).TextColor(t.muted).Shrink(0)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(74).Children(func() {
					if kitInput(c, "wb-min-ilvl", &a.itemMinIlvlStr, kitInputOpts{
						placeholder: a.t("items.ilvl_min"), label: a.t("items.ilvl_min_label"), size: btnSizeSm,
					}).Changed() {
						a.itemQuery.Page = 1
						a.refreshItemList()
					}
				})
				ui.Text(c, "–").FontSize(t.text.xs).TextColor(t.muted)
				ui.Box(c).Width(74).Children(func() {
					if kitInput(c, "wb-max-ilvl", &a.itemMaxIlvlStr, kitInputOpts{
						placeholder: a.t("items.ilvl_max"), label: a.t("items.ilvl_max_label"), size: btnSizeSm,
					}).Changed() {
						a.itemQuery.Page = 1
						a.refreshItemList()
					}
				})
			})
		})

		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if kitSwitch(c, "wb-tradable", &a.itemQuery.TradableOnly, a.t("items.tradable_label")) {
					a.itemQuery.Page = 1
					a.refreshItemList()
				}
				ui.Text(c, a.t("items.tradable_only")).FontSize(t.text.xs).TextColor(t.subdued)
			})
			if kitButton(c, "wb-reset-btn", a.t("common.reset_filters"), btnOpts{variant: btnGhost, size: btnSizeXs, disabled: !a.hasAnyItemFilter()}) {
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
			if kitButton(c, keyPrefix+"-first", a.t("common.first_page"), btnOpts{variant: btnGhost, size: btnSizeXs, disabled: page <= 1}) {
				onGo(1)
			}
			if kitButton(c, keyPrefix+"-prev", a.t("common.prev_page"), btnOpts{variant: btnOutline, size: btnSizeXs, disabled: page <= 1}) {
				onGo(max(1, page-1))
			}
		})

		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(46).Children(func() {
				inp := kitInput(c, keyPrefix+"-input", pageInput, kitInputOpts{
					label: a.t("common.jump_page"), size: btnSizeXs,
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
			if kitButton(c, keyPrefix+"-next", a.t("common.next_page"), btnOpts{variant: btnOutline, size: btnSizeXs, disabled: page >= totalPages}) {
				onGo(min(totalPages, page+1))
			}
			if kitButton(c, keyPrefix+"-last", a.t("common.last_page"), btnOpts{variant: btnGhost, size: btnSizeXs, disabled: page >= totalPages}) {
				onGo(totalPages)
			}
		})
	})
}
