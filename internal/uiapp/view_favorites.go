package uiapp

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

type favoriteTabCounts struct {
	total       int
	item        int
	quest       int
	leve        int
	achievement int
}

func (a *App) favoriteItemsAll() []gamedata.ItemSummary {
	if a.service == nil || a.service.Store() == nil {
		return nil
	}
	byID := a.service.Store().ItemSummaryByID
	var out []gamedata.ItemSummary
	for _, rec := range a.records.Favorites {
		if rec.Kind != config.RecordKindItem {
			continue
		}
		if it, ok := byID[rec.ID]; ok {
			out = append(out, it)
		}
	}
	return out
}

func (a *App) favoriteQuestsAll() []gamedata.QuestEntry {
	if a.service == nil {
		return nil
	}
	var out []gamedata.QuestEntry
	for _, rec := range a.records.Favorites {
		if rec.Kind != config.RecordKindQuest {
			continue
		}
		if q := a.service.GetQuestDetail(rec.ID); q != nil {
			out = append(out, *q)
		}
	}
	return out
}

func (a *App) favoriteLevesAll() []gamedata.LeveEntry {
	if a.service == nil {
		return nil
	}
	var out []gamedata.LeveEntry
	for _, rec := range a.records.Favorites {
		if rec.Kind != config.RecordKindLeve {
			continue
		}
		if lv := a.service.GetLeveDetail(rec.ID); lv != nil {
			out = append(out, *lv)
		}
	}
	return out
}

func (a *App) favoriteAchievementsAll() []gamedata.AchievementEntry {
	if a.service == nil {
		return nil
	}
	var out []gamedata.AchievementEntry
	for _, rec := range a.records.Favorites {
		if rec.Kind != config.RecordKindAchievement {
			continue
		}
		if ach := a.service.GetAchievementDetail(rec.ID); ach != nil {
			out = append(out, *ach)
		}
	}
	return out
}

func (a *App) favoriteCounts() favoriteTabCounts {
	items := a.favoriteItemsAll()
	quests := a.favoriteQuestsAll()
	leves := a.favoriteLevesAll()
	achs := a.favoriteAchievementsAll()
	return favoriteTabCounts{
		total:       len(items) + len(quests) + len(leves) + len(achs),
		item:        len(items),
		quest:       len(quests),
		leve:        len(leves),
		achievement: len(achs),
	}
}

func (a *App) filteredFavoriteItems() ([]gamedata.ItemSummary, int) {
	all := a.favoriteItemsAll()
	qRaw := strings.TrimSpace(a.favItemQ)
	qLower := strings.ToLower(qRaw)
	numID := parseSearchNumericID(qRaw)

	filtered := make([]gamedata.ItemSummary, 0, len(all))
	for _, it := range all {
		if !matchRecordSearch(qLower, numID, it.ID, it.NameCn, it.NameZh, it.NameJa, it.NameEn, it.CategoryName) {
			continue
		}
		filtered = append(filtered, it)
	}

	switch a.favItemSort {
	case "id_asc":
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
	case "id_desc":
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].ID > filtered[j].ID })
	case "ilvl_desc":
		sort.SliceStable(filtered, func(i, j int) bool {
			if filtered[i].ItemLevel != filtered[j].ItemLevel {
				return filtered[i].ItemLevel > filtered[j].ItemLevel
			}
			return filtered[i].ID < filtered[j].ID
		})
	}
	return filtered, len(all)
}

func (a *App) filteredFavoriteQuests() ([]gamedata.QuestEntry, int) {
	all := a.favoriteQuestsAll()
	qRaw := strings.TrimSpace(a.favQuestQ)
	qLower := strings.ToLower(qRaw)
	numID := parseSearchNumericID(qRaw)
	filter := a.favQuestFilter
	if filter == "" {
		filter = "all"
	}

	filtered := make([]gamedata.QuestEntry, 0, len(all))
	for _, q := range all {
		switch filter {
		case "msq":
			if !q.IsMSQ {
				continue
			}
		case "feature":
			if !q.IsFeature {
				continue
			}
		case "repeatable":
			if !q.IsRepeatable {
				continue
			}
		case "with_item":
			if len(q.RewardItems) == 0 {
				continue
			}
		}
		cleanTitle := gamedata.CleanQuestTitle(q.NameCn)
		if !matchRecordSearch(qLower, numID, q.ID, cleanTitle, q.NameCn, q.NameZh, q.NameJa, q.NameEn, q.GenreName, q.SectionName, q.InternalID) {
			continue
		}
		filtered = append(filtered, q)
	}
	return filtered, len(all)
}

func (a *App) filteredFavoriteLeves() ([]gamedata.LeveEntry, int) {
	all := a.favoriteLevesAll()
	qRaw := strings.TrimSpace(a.favLeveQ)
	qLower := strings.ToLower(qRaw)
	numID := parseSearchNumericID(qRaw)
	kind := a.favLeveKind
	if kind == "" {
		kind = "all"
	}

	filtered := make([]gamedata.LeveEntry, 0, len(all))
	for _, lv := range all {
		if kind != "all" && lv.LeveKind != kind {
			continue
		}
		if !matchRecordSearch(qLower, numID, lv.ID, lv.NameCn, lv.NameZh, lv.NameJa, lv.NameEn, lv.AssignmentTypeName, lv.TownName, lv.ClientName) {
			continue
		}
		filtered = append(filtered, lv)
	}
	return filtered, len(all)
}

func (a *App) filteredFavoriteAchievements() ([]gamedata.AchievementEntry, int) {
	all := a.favoriteAchievementsAll()
	qRaw := strings.TrimSpace(a.favAchQ)
	qLower := strings.ToLower(qRaw)
	numID := parseSearchNumericID(qRaw)
	filter := a.favAchFilter
	if filter == "" {
		filter = "all"
	}

	filtered := make([]gamedata.AchievementEntry, 0, len(all))
	for _, ach := range all {
		if filter == "with_title" && ach.TitleID <= 0 {
			continue
		}
		if filter == "with_item" && ach.RewardItemID <= 0 {
			continue
		}
		if !matchRecordSearch(
			qLower, numID, ach.ID,
			ach.NameCn, ach.NameZh, ach.NameJa, ach.NameEn,
			ach.DescriptionCn,
			ach.TitleMasculineCn, ach.TitleFeminineCn, ach.TitleNameJa, ach.TitleNameEn,
			ach.RewardItemNameCn,
		) {
			continue
		}
		filtered = append(filtered, ach)
	}
	return filtered, len(all)
}

func (a *App) selectFavoriteItem(itemID int, record bool) {
	if a.service == nil || itemID <= 0 {
		return
	}
	detail := a.service.GetItemDetail(itemID)
	if detail == nil {
		return
	}
	a.favSelectedItemID = itemID
	a.favItemDetail = detail
	a.favItemRecipeIdx = 0
	a.favItemDetailScroll.Y = 0
	if record {
		a.recordRecent(config.RecordKindItem, itemID)
	}
}

func (a *App) selectFavoriteQuest(questID int, record bool) {
	if a.service == nil || questID <= 0 {
		return
	}
	detail := a.service.GetQuestDetail(questID)
	if detail == nil {
		return
	}
	a.favSelectedQuestID = questID
	a.favQuest = detail
	a.favQuestSummaryOpen = false
	a.favQuestDialogueOpen = false
	a.favQuestText = nil
	a.favQuestDetailScroll.Y = 0
	if record {
		a.recordRecent(config.RecordKindQuest, questID)
	}
	a.loadQuestTextAsync(
		questID,
		&a.favQuestTextSeq,
		&a.favQuestTextLoading,
		func() bool { return a.favSelectedQuestID == questID },
		func(txt *gamedata.QuestTextData) { a.favQuestText = txt },
	)
}

func (a *App) selectFavoriteLeve(leveID int, record bool) {
	if a.service == nil || leveID <= 0 {
		return
	}
	detail := a.service.GetLeveDetail(leveID)
	if detail == nil {
		return
	}
	a.favSelectedLeveID = leveID
	a.favLeve = detail
	a.favLeveDetailScroll.Y = 0
	if record {
		a.recordRecent(config.RecordKindLeve, leveID)
	}
}

func (a *App) syncFavoritesSelectionAfterRemoval(kind config.RecordKind, id int) {
	switch kind {
	case config.RecordKindItem:
		if a.favSelectedItemID == id {
			a.favSelectedItemID = 0
			a.favItemDetail = nil
		}
	case config.RecordKindQuest:
		if a.favSelectedQuestID == id {
			a.favSelectedQuestID = 0
			a.favQuest = nil
			a.favQuestText = nil
			a.favQuestTextLoading = false
		}
	case config.RecordKindLeve:
		if a.favSelectedLeveID == id {
			a.favSelectedLeveID = 0
			a.favLeve = nil
		}
	}
	a.ensureFavoriteSelection()
}

func (a *App) ensureFavoriteSelection() {
	if a.service == nil {
		return
	}
	switch a.favTab {
	case "quest":
		filtered, total := a.filteredFavoriteQuests()
		if total == 0 {
			a.favSelectedQuestID = 0
			a.favQuest = nil
			a.favQuestText = nil
			a.favQuestTextLoading = false
			return
		}
		if a.favSelectedQuestID > 0 && a.isFavorite(config.RecordKindQuest, a.favSelectedQuestID) {
			if a.favQuest == nil {
				a.selectFavoriteQuest(a.favSelectedQuestID, false)
			}
			return
		}
		if len(filtered) > 0 {
			a.selectFavoriteQuest(filtered[0].ID, false)
			return
		}
		all := a.favoriteQuestsAll()
		if len(all) > 0 {
			a.selectFavoriteQuest(all[0].ID, false)
		}

	case "leve":
		filtered, total := a.filteredFavoriteLeves()
		if total == 0 {
			a.favSelectedLeveID = 0
			a.favLeve = nil
			return
		}
		if a.favSelectedLeveID > 0 && a.isFavorite(config.RecordKindLeve, a.favSelectedLeveID) {
			if a.favLeve == nil {
				a.selectFavoriteLeve(a.favSelectedLeveID, false)
			}
			return
		}
		if len(filtered) > 0 {
			a.selectFavoriteLeve(filtered[0].ID, false)
			return
		}
		all := a.favoriteLevesAll()
		if len(all) > 0 {
			a.selectFavoriteLeve(all[0].ID, false)
		}

	case "achievement":
		return

	default:
		filtered, total := a.filteredFavoriteItems()
		if total == 0 {
			a.favSelectedItemID = 0
			a.favItemDetail = nil
			return
		}
		if a.favSelectedItemID > 0 && a.isFavorite(config.RecordKindItem, a.favSelectedItemID) {
			if a.favItemDetail == nil {
				a.selectFavoriteItem(a.favSelectedItemID, false)
			}
			return
		}
		if len(filtered) > 0 {
			a.selectFavoriteItem(filtered[0].ID, false)
			return
		}
		all := a.favoriteItemsAll()
		if len(all) > 0 {
			a.selectFavoriteItem(all[0].ID, false)
		}
	}
}

func (a *App) renderFavoritesView(c *ui.Context) {
	if a.favTab == "" {
		a.favTab = "item"
	}
	a.ensureFavoriteSelection()

	counts := a.favoriteCounts()

	ui.Column(c).Fill().Gap(8).Children(func() {
		a.renderFavoritesTabHeader(c, counts)

		ui.Column(c).Grow(1).Shrink(1).MinHeight(0).FillWidth().Children(func() {
			switch a.favTab {
			case "quest":
				a.renderFavoritesQuestsTab(c)
			case "leve":
				a.renderFavoritesLevesTab(c)
			case "achievement":
				a.renderFavoritesAchievementsTab(c)
			default:
				a.renderFavoritesItemsTab(c)
			}
		})
	})
}

func (a *App) renderFavoritesTabHeader(c *ui.Context, counts favoriteTabCounts) {
	t := themeOf(c)
	type favTabDef struct {
		id      string
		label   string
		a11y    string
		count   int
		icoLine *ui.SVG
		icoFill *ui.SVG
	}
	tabs := []favTabDef{
		{"item", a.t("favorites.tab_item"), a.t("favorites.tab_item_a11y"), counts.item, icoPackage, icoPackageFill},
		{"quest", a.t("favorites.tab_quest"), a.t("favorites.tab_quest_a11y"), counts.quest, icoScroll, icoScrollFill},
		{"leve", a.t("favorites.tab_leve"), a.t("favorites.tab_leve_a11y"), counts.leve, icoClipboard, icoClipboardFill},
		{"achievement", a.t("favorites.tab_achievement"), a.t("favorites.tab_ach_a11y"), counts.achievement, icoTrophy, icoTrophyFill},
	}

	kitPanel(c, a.panelRadius(c)).FillWidth().Shrink(0).Padding(8, 16).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(10).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icoStarFill).Size(iconMd, iconMd).Shrink(0).TextColor(t.attention)
				ui.Text(c, a.t("favorites.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				numText(c, a.tf("favorites.summary", "count", formatInt(counts.total))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
			})

			ui.Row(c.Key("fav-tabs-track")).Padding(3).Gap(2).Radius(t.radius.lg).Background(t.tint(1.1)).AlignItems(ui.Center).Wrap().Children(func() {
				for _, tb := range tabs {
					active := a.favTab == tb.id
					btn := ui.ButtonBase(c.Key("fav-tab-" + tb.id)).Role(ui.RoleTab).Label(tb.a11y).Selected(active).
						Height(28).PaddingX(10).Radius(t.radius.md).Gap(6).
						Justify(ui.Center).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
					if active {
						btn.Background(t.background).Border(hairline, t.border)
					} else if btn.Hovered() {
						btn.Background(t.tint(0.75))
					}
					btn.Children(func() {
						ico := tb.icoLine
						icoCol := t.muted
						fg := t.muted
						fw := 500
						cntCol := t.muted
						if active {
							ico = tb.icoFill
							icoCol = t.primary
							fg = t.foreground
							fw = 600
							cntCol = t.infoText
						} else if btn.Hovered() {
							fg = t.subdued
						}
						ui.Icon(c, ico).Size(iconSm, iconSm).Shrink(0).TextColor(icoCol)
						ui.Text(c, tb.label).FontSize(t.text.xs).FontWeight(fw).TextColor(fg).SingleLine()
						numText(c, formatInt(tb.count)).FontSize(t.text.micro).FontWeight(500).TextColor(cntCol).SingleLine()
					})
					if btn.Clicked() && !active {
						a.favTab = tb.id
						a.focusPageSearch = false
						a.ensureFavoriteSelection()
					}
				}
			})
		})
	})
}

func (a *App) favItemSortOptions() []selectOption[string] {
	return []selectOption[string]{
		{value: "added", label: a.t("favorites.sort_added")},
		{value: "id_asc", label: a.t("items.sort_id_asc")},
		{value: "id_desc", label: a.t("items.sort_id_desc")},
		{value: "ilvl_desc", label: a.t("items.sort_ilvl_desc")},
	}
}

func (a *App) hasFavItemFilter() bool {
	return strings.TrimSpace(a.favItemQ) != "" || (a.favItemSort != "" && a.favItemSort != "added")
}

func (a *App) resetFavItemFilters() {
	a.favItemQ = ""
	a.favItemSort = "added"
	a.favItemPage = 1
	a.favItemPageStr = "1"
	a.favItemScroll.Y = 0
}

func (a *App) renderFavoritesItemsTab(c *ui.Context) {
	t := themeOf(c)
	filtered, totalAll := a.filteredFavoriteItems()
	page, totalPages, start, end := paginateListRange(len(filtered), a.favItemPage, 30)
	if page != a.favItemPage {
		a.favItemPage = page
		a.favItemPageStr = strconv.Itoa(page)
	}
	pageItems := filtered[start:end]

	kitResizer(c, "fav-item-split", &a.listW, itemListMinWidth, itemListMaxWidth, 8, a.t("shell.resize_item_list"),
		func() {
			kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
				ui.Column(c).FillWidth().Padding(10, 12, 10, 12).Gap(8).
					BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Baseline).Grow(1).Shrink(1).Children(func() {
							ui.Text(c, a.t("items.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
							numText(c, a.tf("common.items_count", "count", formatInt(len(filtered)))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
						})
						ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
							if a.hasFavItemFilter() {
								if kitButton(c, "fav-item-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
									a.resetFavItemFilters()
								}
							}
							if kitSelect(c, "fav-item-sort", &a.favItemSort, a.favItemSortOptions(), 126, 176, true) {
								a.favItemPage = 1
								a.favItemPageStr = "1"
								a.favItemScroll.Y = 0
							}
						})
					})

					a.renderPageSearchInput(c, "fav-item-search", &a.favItemQ, a.t("search.item_placeholder"), a.t("search.item_label"), func() {
						a.favItemPage = 1
						a.favItemPageStr = "1"
						a.favItemScroll.Y = 0
					})
				})

				if totalAll == 0 {
					kitEmpty(c, icoStar, a.t("favorites.empty_item_title"), a.t("favorites.empty_item_desc"), func() {
						if kitButton(c, "fav-go-items", a.t("favorites.browse_items"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
							a.navigateTab(tabWorkbench)
						}
					})
				} else if len(pageItems) == 0 {
					kitEmpty(c, icoSearch, a.t("items.empty_title"), a.t("items.empty_desc"), func() {
						if kitButton(c, "fav-item-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
							a.resetFavItemFilters()
						}
					})
				} else {
					ui.Scroll(c.Key("fav-item-list-scroll")).TrackScroll(&a.favItemScroll).Grow(1).Shrink(1).Children(func() {
						ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
							for _, item := range pageItems {
								isSel := item.ID == a.favSelectedItemID
								if a.renderItemListRow(c, fmt.Sprintf("fav-item-row-%d", item.ID), item, isSel, true, nil) {
									a.selectFavoriteItem(item.ID, true)
								}
							}
						})
					})
				}

				a.renderPaginationBar(
					c,
					"fav-item-pg",
					page,
					totalPages,
					&a.favItemPageStr,
					func(next int) {
						a.favItemPage = next
						a.favItemPageStr = strconv.Itoa(next)
						a.favItemScroll.Y = 0
					},
				)
			})
		},
		func() {
			a.renderItemDetailWithOpts(c, itemDetailPaneOpts{
				keyPrefix:       "fav-item",
				showOpenInMain:  true,
				detail:          a.favItemDetail,
				activeRecipeIdx: &a.favItemRecipeIdx,
				scroll:          &a.favItemDetailScroll,
				onJumpItem:      a.inspectItemFromOtherTab,
				onJumpQuest:     a.inspectQuestFromOtherTab,
				onJumpLeve:      a.inspectLeveFromOtherTab,
			})
		},
	)
}

func (a *App) hasFavQuestFilter() bool {
	return strings.TrimSpace(a.favQuestQ) != "" || (a.favQuestFilter != "" && a.favQuestFilter != "all")
}

func (a *App) resetFavQuestFilters() {
	a.favQuestQ = ""
	a.favQuestFilter = "all"
	a.favQuestPage = 1
	a.favQuestPageStr = "1"
	a.favQuestScroll.Y = 0
}

func (a *App) renderFavoritesQuestsTab(c *ui.Context) {
	t := themeOf(c)
	filtered, totalAll := a.filteredFavoriteQuests()
	page, totalPages, start, end := paginateListRange(len(filtered), a.favQuestPage, 30)
	if page != a.favQuestPage {
		a.favQuestPage = page
		a.favQuestPageStr = strconv.Itoa(page)
	}
	pageItems := filtered[start:end]

	kitResizer(c, "fav-quest-split", &a.listW, itemListMinWidth, itemListMaxWidth, 8, a.t("shell.resize_quest_list"),
		func() {
			kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
				ui.Column(c).FillWidth().Padding(10, 12, 10, 12).Gap(8).
					BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Baseline).Grow(1).Shrink(1).Children(func() {
							ui.Text(c, a.t("quests.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
							numText(c, a.tf("common.items_count", "count", formatInt(len(filtered)))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
						})
						if a.hasFavQuestFilter() {
							if kitButton(c, "fav-quest-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
								a.resetFavQuestFilters()
							}
						}
					})

					a.renderPageSearchInput(c, "fav-quest-search", &a.favQuestQ, a.t("search.quest_placeholder"), a.t("search.quest_label"), func() {
						a.favQuestPage = 1
						a.favQuestPageStr = "1"
						a.favQuestScroll.Y = 0
					})

					ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
						for _, f := range a.questScopeFilters() {
							active := a.favQuestFilter == f.mode
							if kitToggleChip(c, "fav-q-scope-"+f.mode, f.label, "", active, nil) {
								a.favQuestFilter = f.mode
								a.favQuestPage = 1
								a.favQuestPageStr = "1"
								a.favQuestScroll.Y = 0
							}
						}
					})
				})

				if totalAll == 0 {
					kitEmpty(c, icoStar, a.t("favorites.empty_quest_title"), a.t("favorites.empty_quest_desc"), func() {
						if kitButton(c, "fav-go-quests", a.t("favorites.browse_quests"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
							a.navigateTab(tabQuests)
						}
					})
				} else if len(pageItems) == 0 {
					kitEmpty(c, icoSearch, a.t("quests.empty_title"), a.t("quests.empty_desc"), func() {
						if kitButton(c, "fav-quest-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
							a.resetFavQuestFilters()
						}
					})
				} else {
					ui.Scroll(c.Key("fav-quest-list-scroll")).TrackScroll(&a.favQuestScroll).Grow(1).Shrink(1).Children(func() {
						ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
							for _, q := range pageItems {
								isSel := q.ID == a.favSelectedQuestID
								if a.renderQuestListRow(c, fmt.Sprintf("fav-quest-row-%d", q.ID), q, isSel, true, nil) {
									a.selectFavoriteQuest(q.ID, true)
								}
							}
						})
					})
				}

				a.renderPaginationBar(
					c,
					"fav-quest-pg",
					page,
					totalPages,
					&a.favQuestPageStr,
					func(next int) {
						a.favQuestPage = next
						a.favQuestPageStr = strconv.Itoa(next)
						a.favQuestScroll.Y = 0
					},
				)
			})
		},
		func() {
			a.renderQuestDetailWithOpts(c, questDetailPaneOpts{
				keyPrefix:      "fav-quest",
				showOpenInMain: true,
				quest:          a.favQuest,
				questText:      a.favQuestText,
				textLoading:    a.favQuestTextLoading,
				summaryOpen:    &a.favQuestSummaryOpen,
				dialogueOpen:   &a.favQuestDialogueOpen,
				scroll:         &a.favQuestDetailScroll,
				onJumpItem:     a.inspectItemFromOtherTab,
				onJumpQuest:    a.inspectQuestFromOtherTab,
			})
		},
	)
}

func (a *App) hasFavLeveFilter() bool {
	return strings.TrimSpace(a.favLeveQ) != "" || (a.favLeveKind != "" && a.favLeveKind != "all")
}

func (a *App) resetFavLeveFilters() {
	a.favLeveQ = ""
	a.favLeveKind = "all"
	a.favLevePage = 1
	a.favLevePageStr = "1"
	a.favLeveScroll.Y = 0
}

func (a *App) renderFavoritesLevesTab(c *ui.Context) {
	t := themeOf(c)
	filtered, totalAll := a.filteredFavoriteLeves()
	page, totalPages, start, end := paginateListRange(len(filtered), a.favLevePage, 30)
	if page != a.favLevePage {
		a.favLevePage = page
		a.favLevePageStr = strconv.Itoa(page)
	}
	pageItems := filtered[start:end]

	kitResizer(c, "fav-leve-split", &a.listW, itemListMinWidth, itemListMaxWidth, 8, a.t("shell.resize_leve_list"),
		func() {
			kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
				ui.Column(c).FillWidth().Padding(10, 12, 10, 12).Gap(8).
					BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Baseline).Grow(1).Shrink(1).Children(func() {
							ui.Text(c, a.t("leves.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
							numText(c, a.tf("common.items_count", "count", formatInt(len(filtered)))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
						})
						if a.hasFavLeveFilter() {
							if kitButton(c, "fav-leve-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
								a.resetFavLeveFilters()
							}
						}
					})

					a.renderPageSearchInput(c, "fav-leve-search", &a.favLeveQ, a.t("search.leve_placeholder"), a.t("search.leve_label"), func() {
						a.favLevePage = 1
						a.favLevePageStr = "1"
						a.favLeveScroll.Y = 0
					})

					ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
						for _, f := range a.leveScopeFilters() {
							active := a.favLeveKind == f.mode
							if kitToggleChip(c, "fav-l-scope-"+f.mode, f.label, "", active, nil) {
								a.favLeveKind = f.mode
								a.favLevePage = 1
								a.favLevePageStr = "1"
								a.favLeveScroll.Y = 0
							}
						}
					})
				})

				if totalAll == 0 {
					kitEmpty(c, icoStar, a.t("favorites.empty_leve_title"), a.t("favorites.empty_leve_desc"), func() {
						if kitButton(c, "fav-go-leves", a.t("favorites.browse_leves"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
							a.navigateTab(tabLeves)
						}
					})
				} else if len(pageItems) == 0 {
					kitEmpty(c, icoSearch, a.t("leves.empty_title"), a.t("leves.empty_desc"), func() {
						if kitButton(c, "fav-leve-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
							a.resetFavLeveFilters()
						}
					})
				} else {
					ui.Scroll(c.Key("fav-leve-list-scroll")).TrackScroll(&a.favLeveScroll).Grow(1).Shrink(1).Children(func() {
						ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
							for _, lv := range pageItems {
								isSel := lv.ID == a.favSelectedLeveID
								if a.renderLeveListRow(c, fmt.Sprintf("fav-leve-row-%d", lv.ID), lv, isSel, true, nil) {
									a.selectFavoriteLeve(lv.ID, true)
								}
							}
						})
					})
				}

				a.renderPaginationBar(
					c,
					"fav-leve-pg",
					page,
					totalPages,
					&a.favLevePageStr,
					func(next int) {
						a.favLevePage = next
						a.favLevePageStr = strconv.Itoa(next)
						a.favLeveScroll.Y = 0
					},
				)
			})
		},
		func() {
			a.renderLeveDetailWithOpts(c, leveDetailPaneOpts{
				keyPrefix:      "fav-leve",
				showOpenInMain: true,
				leve:           a.favLeve,
				scroll:         &a.favLeveDetailScroll,
				onJumpItem:     a.inspectItemFromOtherTab,
			})
		},
	)
}

func (a *App) hasFavAchFilter() bool {
	return strings.TrimSpace(a.favAchQ) != "" || (a.favAchFilter != "" && a.favAchFilter != "all")
}

func (a *App) resetFavAchFilters() {
	a.favAchQ = ""
	a.favAchFilter = "all"
	a.favAchPage = 1
	a.favAchPageStr = "1"
	a.favAchScroll.Y = 0
}

func (a *App) renderFavoritesAchievementsTab(c *ui.Context) {
	t := themeOf(c)
	filtered, totalAll := a.filteredFavoriteAchievements()
	page, totalPages, start, end := paginateListRange(len(filtered), a.favAchPage, 30)
	if page != a.favAchPage {
		a.favAchPage = page
		a.favAchPageStr = strconv.Itoa(page)
	}
	pageItems := filtered[start:end]

	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(12, 16, 10, 16).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Baseline).Children(func() {
					ui.Text(c, a.t("achievements.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					numText(c, a.tf("common.items_count", "count", formatInt(len(filtered)))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				})

				if a.hasFavAchFilter() {
					if kitButton(c, "fav-ach-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
						a.resetFavAchFilters()
					}
				}
			})

			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Box(c).Grow(1).MinWidth(220).MaxWidth(380).Children(func() {
					a.renderPageSearchInput(c, "fav-ach-search", &a.favAchQ, a.t("search.ach_placeholder"), a.t("search.ach_label"), func() {
						a.favAchPage = 1
						a.favAchPageStr = "1"
						a.favAchScroll.Y = 0
					})
				})

				curIdx := 0
				for i, m := range achFilterModes {
					if m == a.favAchFilter {
						curIdx = i
						break
					}
				}
				if nextIdx, chg := kitSegmentedTabs(c, "fav-ach-flt", curIdx, a.achFilterLabels()); chg {
					a.favAchFilter = achFilterModes[nextIdx]
					a.favAchPage = 1
					a.favAchPageStr = "1"
					a.favAchScroll.Y = 0
				}
			})
		})

		a.renderAchievementTableHeader(c)

		if totalAll == 0 {
			kitEmpty(c, icoStar, a.t("favorites.empty_ach_title"), a.t("favorites.empty_ach_desc"), func() {
				if kitButton(c, "fav-go-ach", a.t("favorites.browse_achievements"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.navigateTab(tabAchievements)
				}
			})
		} else if len(pageItems) == 0 {
			kitEmpty(c, icoTrophy, a.t("achievements.empty_title"), a.t("achievements.empty_desc"), func() {
				if kitButton(c, "fav-ach-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					a.resetFavAchFilters()
				}
			})
		} else {
			a.renderAchievementTableRows(c, "fav-ach", pageItems, &a.favAchScroll, nil)
		}

		a.renderPaginationBar(
			c,
			"fav-ach-pg",
			page,
			totalPages,
			&a.favAchPageStr,
			func(next int) {
				a.favAchPage = next
				a.favAchPageStr = strconv.Itoa(next)
				a.favAchScroll.Y = 0
			},
		)
	})
}
