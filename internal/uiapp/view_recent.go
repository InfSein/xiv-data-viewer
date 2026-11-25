package uiapp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

type recentCounts struct {
	all   int
	item  int
	quest int
	leve  int
}

type resolvedRecentEntry struct {
	kind     config.RecordKind
	id       int
	title    string
	subtitle string
	meta     string
	iconID   int
	aliases  []string
}

func paginateListRange(total, reqPage, pageSize int) (page, totalPages, start, end int) {
	if pageSize <= 0 {
		pageSize = 30
	}
	totalPages = max(1, (total+pageSize-1)/pageSize)
	page = reqPage
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start = (page - 1) * pageSize
	end = min(total, start+pageSize)
	return
}

func matchRecordSearch(qLower string, numericID int, id int, fields ...string) bool {
	if qLower == "" {
		return true
	}
	if numericID > 0 && id == numericID {
		return true
	}
	if strings.Contains(strconv.Itoa(id), strings.TrimPrefix(qLower, "#")) && strings.HasPrefix(qLower, "#") {
		return true
	}
	for _, f := range fields {
		if f != "" && strings.Contains(strings.ToLower(f), qLower) {
			return true
		}
	}
	return false
}

func parseSearchNumericID(q string) int {
	raw := strings.TrimPrefix(strings.TrimSpace(q), "#")
	if raw == "" {
		return -1
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return -1
		}
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return -1
	}
	return v
}

func (a *App) resolveHistoryEntry(rec config.HistoryRecord) (resolvedRecentEntry, bool) {
	if a.service == nil || a.service.Store() == nil || rec.ID <= 0 {
		return resolvedRecentEntry{}, false
	}
	store := a.service.Store()
	switch rec.Kind {
	case config.RecordKindItem:
		it, ok := store.ItemSummaryByID[rec.ID]
		if !ok {
			return resolvedRecentEntry{}, false
		}
		sub := nonEmptyStr(it.CategoryName, a.t("common.other"))
		return resolvedRecentEntry{
			kind:     rec.Kind,
			id:       rec.ID,
			title:    it.NameCn,
			subtitle: sub,
			meta:     buildItemRowMeta(it),
			iconID:   it.IconID,
			aliases:  []string{it.NameZh, it.NameJa, it.NameEn, sub},
		}, true

	case config.RecordKindQuest:
		q := a.service.GetQuestDetail(rec.ID)
		if q == nil {
			return resolvedRecentEntry{}, false
		}
		title := gamedata.CleanQuestTitle(q.NameCn)
		if title == "" {
			title = q.NameCn
		}
		sub := a.formatQuestRowType(*q)
		return resolvedRecentEntry{
			kind:     rec.Kind,
			id:       rec.ID,
			title:    title,
			subtitle: sub,
			meta:     formatQuestRowMeta(*q),
			iconID:   q.IconID,
			aliases:  []string{q.NameCn, q.NameZh, q.NameJa, q.NameEn, q.GenreName, q.SectionName, q.InternalID, sub},
		}, true

	case config.RecordKindLeve:
		lv := a.service.GetLeveDetail(rec.ID)
		if lv == nil {
			return resolvedRecentEntry{}, false
		}
		iconID := lv.AssignmentTypeIconID
		if iconID <= 0 {
			iconID = lv.IconCityState
		}
		sub := nonEmptyStr(lv.AssignmentTypeName, nonEmptyStr(lv.ClassJobCategoryName, lv.LeveKindLabel))
		meta := ""
		if lv.ClassJobLevel > 0 {
			meta = fmt.Sprintf("Lv.%d", lv.ClassJobLevel)
		}
		return resolvedRecentEntry{
			kind:     rec.Kind,
			id:       rec.ID,
			title:    lv.NameCn,
			subtitle: sub,
			meta:     meta,
			iconID:   iconID,
			aliases:  []string{lv.NameZh, lv.NameJa, lv.NameEn, lv.TownName, lv.ClientName, sub},
		}, true
	}
	return resolvedRecentEntry{}, false
}

func (a *App) filteredRecentEntries() ([]resolvedRecentEntry, recentCounts) {
	var counts recentCounts
	resolved := make([]resolvedRecentEntry, 0, len(a.records.Recent))
	for _, rec := range a.records.Recent {
		entry, ok := a.resolveHistoryEntry(rec)
		if !ok {
			continue
		}
		counts.all++
		switch entry.kind {
		case config.RecordKindItem:
			counts.item++
		case config.RecordKindQuest:
			counts.quest++
		case config.RecordKindLeve:
			counts.leve++
		}
		resolved = append(resolved, entry)
	}

	kindFilter := a.recentFilter
	if kindFilter == "" {
		kindFilter = "all"
	}
	qRaw := strings.TrimSpace(a.recentQueryQ)
	qLower := strings.ToLower(qRaw)
	numID := parseSearchNumericID(qRaw)

	out := make([]resolvedRecentEntry, 0, len(resolved))
	for _, e := range resolved {
		if kindFilter != "all" && string(e.kind) != kindFilter {
			continue
		}
		if qLower != "" {
			fields := append([]string{e.title, e.subtitle}, e.aliases...)
			if !matchRecordSearch(qLower, numID, e.id, fields...) {
				continue
			}
		}
		out = append(out, e)
	}
	return out, counts
}

func (a *App) selectRecentEntry(kind config.RecordKind, id int) {
	if a.service == nil || id <= 0 {
		return
	}
	a.recentSelectedKind = kind
	a.recentSelectedID = id
	a.recentItemDetail = nil
	a.recentQuest = nil
	a.recentQuestText = nil
	a.recentQuestTextLoading = false
	a.recentLeve = nil

	switch kind {
	case config.RecordKindItem:
		a.recentItemDetail = a.service.GetItemDetail(id)
		a.recentItemRecipeIdx = 0
		a.recentItemScroll.Y = 0
	case config.RecordKindQuest:
		a.recentQuest = a.service.GetQuestDetail(id)
		a.recentQuestSummaryOpen = false
		a.recentQuestDialogueOpen = false
		a.recentQuestScroll.Y = 0
		a.loadQuestTextAsync(
			id,
			&a.recentQuestTextSeq,
			&a.recentQuestTextLoading,
			func() bool { return a.recentSelectedKind == config.RecordKindQuest && a.recentSelectedID == id },
			func(txt *gamedata.QuestTextData) { a.recentQuestText = txt },
		)
	case config.RecordKindLeve:
		a.recentLeve = a.service.GetLeveDetail(id)
		a.recentLeveScroll.Y = 0
	}
}

func (a *App) ensureRecentSelection() {
	if a.service == nil {
		return
	}
	filtered, counts := a.filteredRecentEntries()
	if counts.all == 0 {
		a.recentSelectedKind = ""
		a.recentSelectedID = 0
		a.recentItemDetail = nil
		a.recentQuest = nil
		a.recentQuestText = nil
		a.recentQuestTextLoading = false
		a.recentLeve = nil
		return
	}

	// 若当前选中项依然存在且对应详情可用，保持选中
	if a.recentSelectedID > 0 && a.recentSelectedKind != "" {
		for _, rec := range a.records.Recent {
			if rec.Kind == a.recentSelectedKind && rec.ID == a.recentSelectedID {
				switch rec.Kind {
				case config.RecordKindItem:
					if a.recentItemDetail != nil {
						return
					}
				case config.RecordKindQuest:
					if a.recentQuest != nil {
						return
					}
				case config.RecordKindLeve:
					if a.recentLeve != nil {
						return
					}
				}
				a.selectRecentEntry(rec.Kind, rec.ID)
				return
			}
		}
	}

	if len(filtered) > 0 {
		a.selectRecentEntry(filtered[0].kind, filtered[0].id)
		return
	}
	for _, rec := range a.records.Recent {
		if _, ok := a.resolveHistoryEntry(rec); ok {
			a.selectRecentEntry(rec.Kind, rec.ID)
			return
		}
	}
}

func (a *App) refreshRecentAndFavoriteDetails() {
	if a.service == nil {
		return
	}
	if a.recentSelectedID > 0 && a.recentSelectedKind != "" {
		a.selectRecentEntry(a.recentSelectedKind, a.recentSelectedID)
	}
	if a.favSelectedItemID > 0 {
		a.selectFavoriteItem(a.favSelectedItemID, false)
	}
	if a.favSelectedQuestID > 0 {
		a.selectFavoriteQuest(a.favSelectedQuestID, false)
	}
	if a.favSelectedLeveID > 0 {
		a.selectFavoriteLeve(a.favSelectedLeveID, false)
	}
}

func (a *App) hasRecentFilter() bool {
	return strings.TrimSpace(a.recentQueryQ) != "" || (a.recentFilter != "" && a.recentFilter != "all")
}

func (a *App) resetRecentFilters() {
	a.recentQueryQ = ""
	a.recentFilter = "all"
	a.recentPage = 1
	a.recentPageStr = "1"
	a.recentListScroll.Y = 0
}

func (a *App) recentKindTag(kind config.RecordKind) (string, pillKind) {
	switch kind {
	case config.RecordKindQuest:
		return a.t("recent.tag_quest"), pillWarning
	case config.RecordKindLeve:
		return a.t("recent.tag_leve"), pillPositive
	default:
		return a.t("recent.tag_item"), pillNeutral
	}
}

func (a *App) renderRecentView(c *ui.Context) {
	a.ensureRecentSelection()
	kitResizer(c, "recent-split", &a.listW, itemListMinWidth, itemListMaxWidth, 8, a.t("shell.resize_recent_list"),
		func() { a.renderRecentListPane(c) },
		func() { a.renderRecentDetailPane(c) },
	)
}

func (a *App) renderRecentListPane(c *ui.Context) {
	t := themeOf(c)
	filtered, counts := a.filteredRecentEntries()
	page, totalPages, start, end := paginateListRange(len(filtered), a.recentPage, 30)
	if page != a.recentPage {
		a.recentPage = page
		a.recentPageStr = strconv.Itoa(page)
	}
	pageItems := filtered[start:end]

	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(10, 12, 10, 12).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Baseline).Grow(1).Shrink(1).Children(func() {
					ui.Text(c, a.t("recent.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					cntTxt := a.tf("recent.count_cap", "count", formatInt(counts.all), "max", strconv.Itoa(config.MaxRecentRecords))
					if a.hasRecentFilter() {
						cntTxt = a.tf("common.items_count", "count", formatInt(len(filtered)))
					}
					numText(c, cntTxt).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				})

				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.hasRecentFilter() {
						if kitButton(c, "recent-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.resetRecentFilters()
						}
					}
					if counts.all > 0 {
						if kitButton(c, "recent-clear", a.t("recent.clear_all"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
							a.clearRecent()
						}
					}
				})
			})

			a.renderPageSearchInput(c, "recent-page-search", &a.recentQueryQ, a.t("search.recent_placeholder"), a.t("search.recent_label"), func() {
				a.recentPage = 1
				a.recentPageStr = "1"
				a.recentListScroll.Y = 0
			})

			filters := []struct {
				mode  string
				label string
				cnt   int
			}{
				{"all", a.t("recent.filter_all"), counts.all},
				{"item", a.t("recent.filter_item"), counts.item},
				{"quest", a.t("recent.filter_quest"), counts.quest},
				{"leve", a.t("recent.filter_leve"), counts.leve},
			}
			ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
				for _, f := range filters {
					active := a.recentFilter == f.mode
					cntStr := ""
					if f.cnt > 0 {
						cntStr = formatInt(f.cnt)
					}
					if kitToggleChip(c, "rec-flt-"+f.mode, f.label, cntStr, active, nil) {
						a.recentFilter = f.mode
						a.recentPage = 1
						a.recentPageStr = "1"
						a.recentListScroll.Y = 0
					}
				}
			})
		})

		if counts.all == 0 {
			kitEmpty(c, icoClock, a.t("recent.empty_title"), a.t("recent.empty_desc"), nil)
		} else if len(pageItems) == 0 {
			kitEmpty(c, icoSearch, a.t("recent.empty_filter_title"), a.t("recent.empty_filter_desc"), func() {
				if kitButton(c, "recent-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.resetRecentFilters()
				}
			})
		} else {
			ui.Scroll(c.Key("recent-list-scroll")).TrackScroll(&a.recentListScroll).Grow(1).Shrink(1).Children(func() {
				ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
					for _, e := range pageItems {
						isSel := e.kind == a.recentSelectedKind && e.id == a.recentSelectedID
						fav := a.isFavorite(e.kind, e.id)
						row := ui.ButtonBase(c.Key(fmt.Sprintf("recent-row-%s-%d", e.kind, e.id))).
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
							a.drawGameIcon(c, e.iconID, false, 32, t.radius.md)
							ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
								ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
									fw := 500
									if isSel {
										fw = 600
									}
									ui.Text(c, e.title).FontSize(t.text.sm).FontWeight(fw).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
									ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
										if fav {
											ui.Icon(c, icoStarFill).Size(11, 11).TextColor(t.attention)
										}
										numTextf(c, "#%d", e.id).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
									})
								})
								ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
									tagLbl, tagKind := a.recentKindTag(e.kind)
									ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
										drawPill(c, tagLbl, tagKind)
										ui.Text(c, e.subtitle).FontSize(t.text.xs).TextColor(t.muted).SingleLine().Grow(1).Shrink(1)
									})
									if e.meta != "" {
										numText(c, e.meta).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
									}
								})
							})
						})
						if row.Clicked() {
							a.selectRecentEntry(e.kind, e.id)
						}
					}
				})
			})
		}

		a.renderPaginationBar(
			c,
			"recent-pg",
			page,
			totalPages,
			&a.recentPageStr,
			func(next int) {
				a.recentPage = next
				a.recentPageStr = strconv.Itoa(next)
				a.recentListScroll.Y = 0
			},
		)
	})
}

func (a *App) renderRecentDetailPane(c *ui.Context) {
	switch a.recentSelectedKind {
	case config.RecordKindItem:
		a.renderItemDetailWithOpts(c, itemDetailPaneOpts{
			keyPrefix:       "recent-item",
			showOpenInMain:  true,
			detail:          a.recentItemDetail,
			activeRecipeIdx: &a.recentItemRecipeIdx,
			scroll:          &a.recentItemScroll,
			onJumpItem:      a.inspectItemFromOtherTab,
			onJumpQuest:     a.inspectQuestFromOtherTab,
			onJumpLeve:      a.inspectLeveFromOtherTab,
		})
	case config.RecordKindQuest:
		a.renderQuestDetailWithOpts(c, questDetailPaneOpts{
			keyPrefix:      "recent-quest",
			showOpenInMain: true,
			quest:          a.recentQuest,
			questText:      a.recentQuestText,
			textLoading:    a.recentQuestTextLoading,
			summaryOpen:    &a.recentQuestSummaryOpen,
			dialogueOpen:   &a.recentQuestDialogueOpen,
			scroll:         &a.recentQuestScroll,
			onJumpItem:     a.inspectItemFromOtherTab,
			onJumpQuest:    a.inspectQuestFromOtherTab,
		})
	case config.RecordKindLeve:
		a.renderLeveDetailWithOpts(c, leveDetailPaneOpts{
			keyPrefix:      "recent-leve",
			showOpenInMain: true,
			leve:           a.recentLeve,
			scroll:         &a.recentLeveScroll,
			onJumpItem:     a.inspectItemFromOtherTab,
		})
	default:
		kitPanel(c, a.panelRadius(c)).Grow(1).Shrink(1).FillHeight().Children(func() {
			kitEmpty(c, icoClock, a.t("recent.detail_empty_title"), a.t("recent.detail_empty_desc"), nil)
		})
	}
}
