package uiapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

var textSearchScopeModes = []gamedata.TextSearchScope{
	gamedata.TextScopeAll,
	gamedata.TextScopeCore,
	gamedata.TextScopeStory,
	gamedata.TextScopeUI,
	gamedata.TextScopeOther,
}

var textSearchPageSizeChoices = []int{30, 50, 100}

const (
	textColSourceW   float32 = 212
	textColActionsW  float32 = 52
	textColContentMn float32 = 220
	textColCompareMn float32 = 200
	textSnippetLimit int     = 260
	textTransSnipLim int     = 180
)

func (a *App) textSearchScopeLabels() []string {
	return []string{
		a.t("text_search.scope_all"),
		a.t("text_search.scope_core"),
		a.t("text_search.scope_story"),
		a.t("text_search.scope_ui"),
		a.t("text_search.scope_other"),
	}
}

func (a *App) cancelTextSearchTasks() {
	if a.textSearchIdxCancel != nil {
		a.textSearchIdxCancel()
		a.textSearchIdxCancel = nil
	}
	if a.textSearchRunCancel != nil {
		a.textSearchRunCancel()
		a.textSearchRunCancel = nil
	}
	a.textSearchIdxBusy = false
	a.textSearchBusy = false
	a.textSearchIdxSeq++
	a.textSearchRunSeq++
}

func (a *App) effectiveTextSearchLang() string {
	if a.service == nil {
		return "chs"
	}
	return a.service.DefaultTextSearchLang()
}

// syncTextSearchOnLangChange 在切换应用语言并激活新 Service 时，复用已构建的全部语言全文索引并按新主语言刷新展示。
func (a *App) syncTextSearchOnLangChange() {
	if a.service == nil {
		return
	}
	for code, idx := range a.textIndexCache {
		if idx != nil {
			a.service.SetTextSearchIndex(code, idx)
		}
	}
	a.textSearchSheet = ""
	a.textSearchPage = 1
	a.textSearchPageStr = "1"
	clear(a.textSearchExpanded)
	if a.tab == tabTextSearch {
		a.ensureTextSearchReady()
	}
}

// ensureTextSearchReady 确保当前客户端可用的全部核心语言（中/日/英）全文索引均已就绪；
// 首次访问时在后台异步构建，避免阻塞 UI 线程，完成后输入任意语言均可直接命中。
func (a *App) ensureTextSearchReady() {
	if a.service == nil {
		return
	}
	for code, idx := range a.textIndexCache {
		if idx != nil {
			a.service.SetTextSearchIndex(code, idx)
		}
	}
	primaryLang := a.effectiveTextSearchLang()

	if a.service.HasAllCoreTextSearchIndices() {
		a.textSearchIdxBusy = false
		a.textSearchIdxErr = ""
		if strings.TrimSpace(a.textSearchQ) != "" {
			a.runTextSearch(false)
		} else {
			a.textSearchRes = gamedata.TextSearchResponse{
				LangCode:   primaryLang,
				Page:       1,
				PageSize:   max(1, a.textSearchPageSize),
				TotalPages: 1,
				IndexStats: a.service.CombinedTextIndexStats(),
			}
		}
		return
	}

	// 若正在后台构建跨语言索引，直接继续等待进度推送
	if a.textSearchIdxBusy {
		return
	}

	if a.textSearchIdxCancel != nil {
		a.textSearchIdxCancel()
		a.textSearchIdxCancel = nil
	}
	a.textSearchIdxSeq++
	seq := a.textSearchIdxSeq
	svc := a.service
	a.textSearchIdxBusy = true
	a.textSearchIdxErr = ""
	a.textSearchIdxProg = gamedata.TextIndexProgress{
		LangCode:  primaryLang,
		LangIndex: 1,
		LangTotal: max(1, len(svc.CoreTextSearchLangs())),
		Phase:     "scanning",
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.textSearchIdxCancel = cancel

	applyDone := func(builtMap map[string]*gamedata.TextSearchIndex, err error) {
		cancel()
		if a.textSearchIdxSeq != seq {
			return
		}
		a.textSearchIdxCancel = nil
		a.textSearchIdxBusy = false
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			a.textSearchIdxErr = a.tf("text_search.indexing_failed", "err", err.Error())
			return
		}
		for code, built := range builtMap {
			if built != nil {
				a.textIndexCache[code] = built
				if a.service != nil {
					a.service.SetTextSearchIndex(code, built)
				}
			}
		}
		a.textSearchIdxErr = ""
		a.runTextSearch(false)
	}

	if a.win == nil {
		builtMap, err := svc.BuildAllTextSearchIndices(ctx, func(p gamedata.TextIndexProgress) {
			if a.textSearchIdxSeq == seq {
				a.textSearchIdxProg = p
			}
		})
		applyDone(builtMap, err)
		return
	}

	go func() {
		builtMap, err := svc.BuildAllTextSearchIndices(ctx, func(p gamedata.TextIndexProgress) {
			cp := p
			a.win.Update(func() {
				if a.textSearchIdxSeq == seq {
					a.textSearchIdxProg = cp
				}
			})
		})
		a.win.Update(func() {
			applyDone(builtMap, err)
		})
	}()
}

// runTextSearch 触发跨语言全量文本检索。pagingOnly=true 时命中内部翻页缓存可同帧瞬时返回，
// 新关键词检索在有窗口时后台执行并自动取消陈旧查询，保障连续打字零掉帧。
func (a *App) runTextSearch(pagingOnly bool) {
	if a.service == nil {
		return
	}
	stats := a.service.CombinedTextIndexStats()
	if stats.TotalTexts == 0 {
		return
	}
	lang := a.effectiveTextSearchLang()

	pageSize := a.textSearchPageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	qStr := a.textSearchQ
	parsed := gamedata.ParseTextSearchQuery(qStr)
	if parsed.Empty {
		if a.textSearchRunCancel != nil {
			a.textSearchRunCancel()
			a.textSearchRunCancel = nil
		}
		a.textSearchRunSeq++
		a.textSearchBusy = false
		a.textSearchPage = 1
		a.textSearchPageStr = "1"
		a.textSearchScroll.Y = 0
		clear(a.textSearchExpanded)
		a.textSearchRes = gamedata.TextSearchResponse{
			LangCode:   lang,
			Query:      qStr,
			Mode:       parsed.Mode,
			Page:       1,
			PageSize:   pageSize,
			TotalPages: 1,
			IndexStats: stats,
		}
		return
	}

	query := gamedata.TextSearchQuery{
		LangCode:    lang,
		Q:           qStr,
		Scope:       a.textSearchScope,
		SheetFilter: a.textSearchSheet,
		Page:        a.textSearchPage,
		PageSize:    pageSize,
	}

	if a.textSearchRunCancel != nil {
		a.textSearchRunCancel()
		a.textSearchRunCancel = nil
	}
	a.textSearchRunSeq++
	seq := a.textSearchRunSeq
	svc := a.service

	// 翻页或无头单元测试模式直接同步执行（翻页命中内存缓存耗时 < 0.1ms，无闪烁）
	if a.win == nil || pagingOnly {
		res, err := svc.SearchTexts(context.Background(), query)
		if err == nil {
			a.textSearchBusy = false
			a.textSearchRes = res
			a.textSearchPage = res.Page
			a.textSearchPageStr = strconv.Itoa(res.Page)
			a.textSearchScroll.Y = 0
		}
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.textSearchRunCancel = cancel
	a.textSearchBusy = true

	go func() {
		res, err := svc.SearchTexts(ctx, query)
		a.win.Update(func() {
			cancel()
			if a.textSearchRunSeq != seq {
				return
			}
			a.textSearchRunCancel = nil
			a.textSearchBusy = false
			if err != nil {
				return
			}
			a.textSearchRes = res
			a.textSearchPage = res.Page
			a.textSearchPageStr = strconv.Itoa(res.Page)
			a.textSearchScroll.Y = 0
		})
	}()
}

func (a *App) hasTextSearchFilter() bool {
	return strings.TrimSpace(a.textSearchQ) != "" ||
		a.textSearchScope != gamedata.TextScopeAll ||
		strings.TrimSpace(a.textSearchSheet) != ""
}

func (a *App) resetTextSearchFilters() {
	a.textSearchQ = ""
	a.textSearchScope = gamedata.TextScopeAll
	a.textSearchSheet = ""
	a.textSearchPage = 1
	a.textSearchPageStr = "1"
	clear(a.textSearchExpanded)
	a.runTextSearch(false)
}

func (a *App) inspectTextSearchHit(hit gamedata.TextSearchHit) {
	if hit.JumpID <= 0 {
		return
	}
	switch hit.JumpKind {
	case "item":
		a.inspectItemFromOtherTab(hit.JumpID)
	case "quest":
		a.inspectQuestFromOtherTab(hit.JumpID)
	case "leve":
		a.inspectLeveFromOtherTab(hit.JumpID)
	case "achievement":
		a.inspectAchievementFromOtherTab(hit.JumpID, 0)
	case "bgm":
		a.inspectBGMFromOtherTab(hit.JumpID)
	case "orchestrion":
		a.inspectMusicFromOtherTab("orchestrion", hit.JumpID)
	}
}

func (a *App) textSearchJumpTooltip(kind string) string {
	switch kind {
	case "item":
		return a.t("text_search.jump_item")
	case "quest":
		return a.t("text_search.jump_quest")
	case "leve":
		return a.t("text_search.jump_leve")
	case "achievement":
		return a.t("text_search.jump_achievement")
	case "bgm", "orchestrion":
		return a.t("text_search.jump_music")
	default:
		return ""
	}
}

func (a *App) renderTextSearchView(c *ui.Context) {
	t := themeOf(c)
	var idxStats gamedata.TextIndexStats
	if a.service != nil {
		idxStats = a.service.CombinedTextIndexStats()
	}
	parsed := gamedata.ParseTextSearchQuery(a.textSearchQ)

	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		// 1. 页头、主搜索框、匹配模式指示与范围/表筛选控制区
		ui.Column(c).FillWidth().Padding(12, 16, 10, 16).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			// 第一行：标题 + 索引概览 + 右侧重置/每页条数
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Baseline).Children(func() {
					ui.Text(c, a.t("text_search.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					if idxStats.TotalTexts > 0 {
						numText(c, a.tf("text_search.summary_ready",
							"texts", formatInt(idxStats.TotalTexts),
							"sheets", formatInt(idxStats.TotalSheets),
						)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
					}
				})

				ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
					if a.hasTextSearchFilter() {
						if kitButton(c, "ts-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.resetTextSearchFilters()
						}
					}

					psOpts := make([]selectOption[int], 0, len(textSearchPageSizeChoices))
					for _, sz := range textSearchPageSizeChoices {
						psOpts = append(psOpts, selectOption[int]{
							value: sz,
							label: a.tf("text_search.page_size_opt", "size", strconv.Itoa(sz)),
						})
					}
					if kitSelect(c, "ts-pagesize-select", &a.textSearchPageSize, psOpts, 102, 118, true) {
						a.textSearchPage = 1
						a.textSearchPageStr = "1"
						a.runTextSearch(true)
					}
				})
			})

			// 第二行：宽敞检索输入框 + 实时匹配模式胶囊（模糊 / 精确）+ 耗时与命中数
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(260).Children(func() {
					ui.Box(c).Grow(1).Shrink(1).MinWidth(220).MaxWidth(560).Children(func() {
						inp := kitInput(c, "ts-page-search", &a.textSearchQ, kitInputOpts{
							placeholder: a.t("text_search.placeholder"),
							label:       a.t("text_search.label"),
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
						if inp.Changed() || inp.Submitted() {
							a.textSearchPage = 1
							a.textSearchPageStr = "1"
							clear(a.textSearchExpanded)
							a.runTextSearch(false)
						}
					})

					// 匹配模式实时反馈胶囊（悬停展示语法提示）
					modeLbl := a.t("text_search.mode_fuzzy")
					modeTip := a.t("text_search.mode_fuzzy_tip")
					modeKind := pillNeutral
					if parsed.Mode == gamedata.TextMatchExact {
						modeLbl = a.t("text_search.mode_exact")
						modeTip = a.t("text_search.mode_exact_tip")
						modeKind = pillAccent
					}
					modeBox := ui.Box(c.Key("ts-mode-badge")).Shrink(0)
					kitTooltip(c, modeBox, modeTip)
					modeBox.Children(func() {
						drawPill(c, modeLbl, modeKind)
					})
				})

				ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.textSearchBusy {
						ui.Spinner(c).FontSize(iconSm).TextColor(t.primary)
					}
					if !parsed.Empty && idxStats.TotalTexts > 0 && !a.textSearchIdxBusy {
						msStr := formatElapsedMs(a.textSearchRes.ElapsedMs)
						numText(c, a.tf("text_search.result_stats",
							"count", formatInt(a.textSearchRes.Total),
							"ms", msStr,
						)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
					}
				})
			})

			// 第三行：数据范围分段切换 + 动态数据表下钻筛选
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				curScopeIdx := 0
				for i, sc := range textSearchScopeModes {
					if sc == a.textSearchScope {
						curScopeIdx = i
						break
					}
				}
				if nextIdx, chg := kitSegmentedTabs(c, "ts-scope", curScopeIdx, a.textSearchScopeLabels()); chg {
					a.textSearchScope = textSearchScopeModes[nextIdx]
					a.textSearchSheet = ""
					a.textSearchPage = 1
					a.textSearchPageStr = "1"
					clear(a.textSearchExpanded)
					a.runTextSearch(false)
				}

				// 若有结果表分布或已锁定某张表，提供快捷下拉按表过滤
				if len(a.textSearchRes.TopSheets) > 0 || a.textSearchSheet != "" {
					sheetOpts := a.buildTextSearchSheetOptions()
					if kitSelect(c, "ts-sheet-select", &a.textSearchSheet, sheetOpts, 200, 280, true) {
						a.textSearchPage = 1
						a.textSearchPageStr = "1"
						clear(a.textSearchExpanded)
						a.runTextSearch(false)
					}
				}
			})
		})

		// 2. 主内容区：索引构建态 / 失败态 / 引导态 / 空结果 / 匹配列表
		switch {
		case a.textSearchIdxBusy:
			a.renderTextSearchIndexingState(c)
		case a.textSearchIdxErr != "":
			kitEmpty(c, icoAlertCircle, a.t("text_search.title"), a.textSearchIdxErr, func() {
				if kitButton(c, "ts-idx-retry", a.t("text_search.retry"), btnOpts{variant: btnOutline, size: btnSizeSm, icon: icoRefresh}) {
					a.textSearchIdxErr = ""
					a.ensureTextSearchReady()
				}
			})
		case parsed.Empty:
			kitEmpty(c, icoTextSearch, a.t("text_search.empty_prompt_title"), a.t("text_search.empty_prompt_desc"), nil)
		case len(a.textSearchRes.Items) == 0 && !a.textSearchBusy:
			kitEmpty(c, icoSearch, a.t("text_search.empty_no_result_title"), a.t("text_search.empty_no_result_desc"), func() {
				if kitButton(c, "ts-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.resetTextSearchFilters()
				}
			})
		default:
			showCompare := hasAnyHitTranslation(a.textSearchRes.Items)
			showActions := hasAnyHitJump(a.textSearchRes.Items)
			a.renderTextSearchTableHeader(c, showCompare, showActions)
			a.renderTextSearchRows(c, showCompare, showActions)
			a.renderPaginationBar(
				c,
				"ts-pg",
				a.textSearchRes.Page,
				a.textSearchRes.TotalPages,
				&a.textSearchPageStr,
				func(next int) {
					a.textSearchPage = next
					a.runTextSearch(true)
				},
			)
		}
	})
}

func (a *App) buildTextSearchSheetOptions() []selectOption[string] {
	opts := []selectOption[string]{{value: "", label: a.t("text_search.all_sheets")}}
	if len(a.textSearchRes.TopSheets) > 0 {
		opts[0].label = a.tf("text_search.all_sheets_count", "count", strconv.Itoa(len(a.textSearchRes.TopSheets)))
	}
	hasSelected := a.textSearchSheet == ""
	for _, f := range a.textSearchRes.TopSheets {
		if strings.EqualFold(f.SheetName, a.textSearchSheet) {
			hasSelected = true
		}
		opts = append(opts, selectOption[string]{
			value: f.SheetName,
			label: fmt.Sprintf("%s (%s)", f.SheetName, formatInt(f.Count)),
		})
	}
	if !hasSelected && a.textSearchSheet != "" {
		opts = append(opts, selectOption[string]{
			value: a.textSearchSheet,
			label: a.textSearchSheet,
		})
	}
	return opts
}

func (a *App) renderTextSearchIndexingState(c *ui.Context) {
	t := themeOf(c)
	prog := a.textSearchIdxProg
	langFrac := 0.0
	msg := ""
	if prog.Phase == "indexing" {
		langFrac = 0.88
		msg = a.tf("text_search.indexing_build", "texts", formatInt(prog.IndexedTexts))
	} else {
		if prog.TotalSheets > 0 {
			langFrac = float64(prog.DoneSheets) / float64(prog.TotalSheets) * 0.85
		}
		msg = a.tf("text_search.indexing_scan",
			"done", formatInt(prog.DoneSheets),
			"total", formatInt(prog.TotalSheets),
			"texts", formatInt(prog.IndexedTexts),
		)
	}
	pct := langFrac
	if prog.LangTotal > 1 {
		idx := max(1, min(prog.LangIndex, prog.LangTotal))
		pct = (float64(idx-1) + langFrac) / float64(prog.LangTotal)
		if prog.LangCode != "" {
			msg = fmt.Sprintf("[%d/%d %s] %s", idx, prog.LangTotal, gamedata.TextLangBadge(prog.LangCode), msg)
		}
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}

	ui.Column(c).Grow(1).FillWidth().Center().Padding(32, 24).Gap(12).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).FontSize(iconMd).TextColor(t.primary)
			ui.Text(c, a.t("text_search.indexing_title")).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
		})
		ui.Box(c).Width(360).Children(func() {
			kitProgressBar(c, float32(pct*100))
		})
		numText(c, msg).FontSize(t.text.xs).TextColor(t.subdued).TextAlign(ui.Center)
		ui.Text(c, a.t("text_search.indexing_hint")).FontSize(t.text.xs).TextColor(t.muted).TextAlign(ui.Center).MaxWidth(440)
	})
}

func hasAnyHitTranslation(items []gamedata.TextSearchHit) bool {
	for i := range items {
		if len(items[i].Translations) > 0 {
			return true
		}
	}
	return false
}

func hasAnyHitJump(items []gamedata.TextSearchHit) bool {
	for i := range items {
		if items[i].JumpKind != "" && items[i].JumpID > 0 {
			return true
		}
	}
	return false
}

func (a *App) renderTextSearchTableHeader(c *ui.Context, showCompare, showActions bool) {
	t := themeOf(c)
	ui.Row(c).FillWidth().Padding(8, 16).Background(t.card).
		BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.t("text_search.col_source")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(textColSourceW).Shrink(0)
		ui.Text(c, a.t("text_search.col_content")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Grow(1.15).Shrink(1).MinWidth(textColContentMn)
		if showCompare {
			ui.Text(c, a.t("text_search.col_compare")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Grow(1).Shrink(1).MinWidth(textColCompareMn)
		}
		if showActions {
			ui.Text(c, a.t("text_search.col_actions")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(textColActionsW).Shrink(0).TextAlign(ui.End)
		}
	})
}

func (a *App) renderTextSearchRows(c *ui.Context, showCompare, showActions bool) {
	t := themeOf(c)
	primaryLang := a.textSearchRes.LangCode
	ui.Scroll(c.Key("ts-table-scroll")).TrackScroll(&a.textSearchScroll).Grow(1).Shrink(1).Children(func() {
		ui.Column(c).FillWidth().Selectable().Children(func() {
			for i, hit := range a.textSearchRes.Items {
				if i > 0 {
					kitSeparator(c, false).Margin(0, 16)
				}
				row := ui.Row(c.Key(fmt.Sprintf("ts-row-%d", hit.DocID))).FillWidth().Padding(10, 16).Gap(12).
					AlignItems(ui.Start).Transition(colorTransition(motionFast))
				if row.Hovered() {
					row.Background(t.tint(0.85))
				}
				row.Children(func() {
					// 1. 来源表 / 行号 / 列名（点击表名可快速按该表过滤）
					ui.Column(c).Width(textColSourceW).Shrink(0).Gap(3).Children(func() {
						shBtn := ui.ButtonBase(c.Key(fmt.Sprintf("ts-sh-%d", hit.DocID))).
							Label(a.tf("text_search.filter_by_sheet_tip", "sheet", hit.SheetName)).
							FillWidth().Padding(0).Justify(ui.Start).AlignItems(ui.Center).
							Cursor(ui.CursorPointer)
						kitTooltip(c, shBtn, a.tf("text_search.filter_by_sheet_tip", "sheet", hit.SheetName))
						shBtn.Children(func() {
							col := t.foreground
							if shBtn.Hovered() {
								col = t.primary
							}
							ui.Text(c, hit.SheetName).FontSize(t.text.xs).FontWeight(600).TextColor(col).SingleLine().Grow(1).Shrink(1)
						})
						if shBtn.Clicked() {
							a.textSearchSheet = hit.SheetName
							a.textSearchPage = 1
							a.textSearchPageStr = "1"
							a.runTextSearch(false)
						}

						ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
							numText(c, gamedata.FormatTextRowKey(hit.RowID, hit.SubRowID)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
							if hit.ColName != "" {
								drawPill(c, hit.ColName, pillOutline)
							}
						})
					})

					// 2. 命中文本内容（高亮匹配字符，超长文本默认围绕首命中截取并提供展开/收起）
					expanded := a.textSearchExpanded[hit.DocID]
					anyTransTrunc := false
					if showCompare && !expanded {
						for _, tr := range hit.Translations {
							if utf8.RuneCountInString(tr.Text) > textTransSnipLim {
								anyTransTrunc = true
								break
							}
						}
					}
					ui.Column(c).Grow(1.15).Shrink(1).MinWidth(textColContentMn).Gap(4).Children(func() {
						dispText, dispSpans, truncated := sliceTextSnippet(hit.Text, hit.MatchSpans, textSnippetLimit, expanded)
						if hit.LangCode != "" && primaryLang != "" && hit.LangCode != primaryLang {
							// 主语言无该条目时，标注当前展示的回退语言徽章
							ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Start).Children(func() {
								ui.Box(c).Padding(2, 0, 0, 0).Shrink(0).Children(func() {
									drawPill(c, gamedata.TextLangBadge(hit.LangCode), pillNeutral)
								})
								ui.Box(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
									renderHighlightedSearchText(c, dispText, dispSpans, t)
								})
							})
						} else {
							renderHighlightedSearchText(c, dispText, dispSpans, t)
						}
						if truncated || anyTransTrunc || expanded {
							lbl := a.t("text_search.expand_text")
							if expanded {
								lbl = a.t("text_search.collapse_text")
							}
							ui.Row(c).Justify(ui.Start).Children(func() {
								if kitButton(c, fmt.Sprintf("ts-exp-%d", hit.DocID), lbl, btnOpts{variant: btnGhost, size: btnSizeXs}) {
									a.textSearchExpanded[hit.DocID] = !expanded
								}
							})
						}
					})

					// 3. 多语言对照列（仅在当前页存在对照数据时展示，清晰并排呈现 JA / EN / ZH 译文并高亮命中词）
					if showCompare {
						ui.Column(c).Grow(1).Shrink(1).MinWidth(textColCompareMn).Gap(5).Children(func() {
							if len(hit.Translations) == 0 {
								ui.Text(c, "–").FontSize(t.text.xs).TextColor(t.idle)
								return
							}
							for _, tr := range hit.Translations {
								dispTr, dispTrSpans, _ := sliceTextSnippet(tr.Text, tr.MatchSpans, textTransSnipLim, expanded)
								ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Start).Children(func() {
									ui.Box(c).Padding(1, 0, 0, 0).Shrink(0).Children(func() {
										drawPill(c, tr.Badge, pillNeutral)
									})
									ui.Box(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
										renderHighlightedTranslationText(c, dispTr, dispTrSpans, t)
									})
								})
							}
						})
					}

					// 4. 操作列：若关联内置实体则显示直跳详情按钮
					if showActions {
						ui.Row(c).Width(textColActionsW).Shrink(0).Justify(ui.End).AlignItems(ui.Center).Gap(4).Children(func() {
							if hit.JumpKind != "" && hit.JumpID > 0 {
								jumpTip := a.textSearchJumpTooltip(hit.JumpKind)
								if kitIconButton(c, fmt.Sprintf("ts-jump-%d", hit.DocID), icoExternalLink, jumpTip, btnOpts{variant: btnGhost, size: btnSizeSm}) {
									a.inspectTextSearchHit(hit)
								}
							}
						})
					}
				})
			}
		})
	})
}

// sliceTextSnippet 为超长剧情或说明文本生成围绕首命中位置的紧凑上下文摘录，并同步重映射高亮字节区间。
func sliceTextSnippet(text string, spans []gamedata.TextMatchSpan, maxRunes int, expanded bool) (string, []gamedata.TextMatchSpan, bool) {
	if expanded || maxRunes <= 0 {
		return text, spans, false
	}
	runeCount := utf8.RuneCountInString(text)
	if runeCount <= maxRunes {
		return text, spans, false
	}

	// 定位所有 rune 的字节起点，以首个命中区间为中心构造截取窗口
	starts := make([]int, 0, runeCount+1)
	for i := range text {
		starts = append(starts, i)
	}
	starts = append(starts, len(text))

	anchorRune := 0
	if len(spans) > 0 {
		for rIdx, bOff := range starts {
			if bOff >= spans[0].Start {
				anchorRune = rIdx
				break
			}
		}
	}

	winStart := max(0, anchorRune-maxRunes/4)
	winEnd := winStart + maxRunes
	if winEnd > runeCount {
		winEnd = runeCount
		winStart = max(0, winEnd-maxRunes)
	}

	byteStart := starts[winStart]
	byteEnd := starts[winEnd]

	prefix := ""
	if winStart > 0 {
		prefix = "…"
	}
	suffix := ""
	if winEnd < runeCount {
		suffix = "…"
	}
	outText := prefix + text[byteStart:byteEnd] + suffix
	shift := len(prefix) - byteStart

	var outSpans []gamedata.TextMatchSpan
	for _, sp := range spans {
		if sp.End <= byteStart || sp.Start >= byteEnd {
			continue
		}
		ns := max(sp.Start, byteStart) + shift
		ne := min(sp.End, byteEnd) + shift
		if ne > ns {
			outSpans = append(outSpans, gamedata.TextMatchSpan{Start: ns, End: ne})
		}
	}
	return outText, outSpans, true
}

// renderHighlightedSearchText 将命中的字符区间以语义高亮样式渲染为可框选富文本。
func renderHighlightedSearchText(c *ui.Context, text string, spans []gamedata.TextMatchSpan, t uiTokens) {
	renderHighlightedTextStyled(c, text, spans, t.text.sm, t.foreground, t)
}

// renderHighlightedTranslationText 渲染多语言对照列译文，若该语言译文直接命中搜索词则同步高亮。
func renderHighlightedTranslationText(c *ui.Context, text string, spans []gamedata.TextMatchSpan, t uiTokens) {
	renderHighlightedTextStyled(c, text, spans, t.text.xs, t.subdued, t)
}

func renderHighlightedTextStyled(c *ui.Context, text string, spans []gamedata.TextMatchSpan, fontSize float32, baseColor ui.Color, t uiTokens) {
	if len(spans) == 0 {
		ui.Text(c, text).FontSize(fontSize).LineHeight(lineProse).TextColor(baseColor).Selectable().FillWidth()
		return
	}

	uiSpans := make([]ui.Span, 0, len(spans)*2+1)
	cursor := 0
	for _, sp := range spans {
		if sp.Start < cursor || sp.End <= sp.Start || sp.End > len(text) {
			continue
		}
		if sp.Start > cursor {
			uiSpans = append(uiSpans, ui.Span{Text: text[cursor:sp.Start]})
		}
		uiSpans = append(uiSpans, ui.Span{
			Text:       text[sp.Start:sp.End],
			Weight:     600,
			Color:      t.infoText,
			Background: t.accentSoft,
			Radius:     t.radius.xs,
		})
		cursor = sp.End
	}
	if cursor < len(text) {
		uiSpans = append(uiSpans, ui.Span{Text: text[cursor:]})
	}
	if len(uiSpans) == 0 {
		ui.Text(c, text).FontSize(fontSize).LineHeight(lineProse).TextColor(baseColor).Selectable().FillWidth()
		return
	}
	ui.RichText(c, uiSpans...).FontSize(fontSize).FontWeight(400).TextColor(baseColor).
		LineHeight(lineProse).Selectable().FillWidth()
}

func formatElapsedMs(ms float64) string {
	if ms < 0.05 {
		return "<0.1"
	}
	if ms < 10 {
		return fmt.Sprintf("%.1f", ms)
	}
	return fmt.Sprintf("%.0f", ms)
}
