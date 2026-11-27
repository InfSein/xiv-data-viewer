package uiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const musicAutoCollapseLimit = 50

type musicAudioSlot struct {
	detail        *gamedata.MusicAudioDetail
	key           string
	seq           uint64
	loading       bool
	err           string
	autoPlayKey   string
	seekPos       float64
	exportMsg     string
	exportIsErr   bool
	quests        []gamedata.MusicQuestRef
	questsKey     string
	questsSvc     *gamedata.Service
	questsSeq     uint64
	questsLoading bool
	usagesOpen    bool
	usagesInitKey string
	questsOpen    bool
	questsInitKey string
}

type musicDetailPaneOpts struct {
	keyPrefix      string
	showOpenInMain bool
	entry          *gamedata.MusicEntry
	slot           *musicAudioSlot
	scroll         *ui.ScrollState
	isStillCurrent func() bool
	onJumpQuest    func(int)
}

func (a *App) refreshMusicList() {
	if a.service == nil {
		return
	}
	a.musicRes = a.service.SearchMusic(a.musicQuery)
	a.musicQuery.Page = a.musicRes.Page
	a.musicPageStr = strconv.Itoa(a.musicRes.Page)
	a.musicListScroll.Y = 0
}

func (a *App) hasMusicFilter() bool {
	return strings.TrimSpace(a.musicQuery.Q) != "" ||
		(a.musicQuery.Kind != "" && a.musicQuery.Kind != "all") ||
		a.musicQuery.ExpansionID >= 0 ||
		a.musicQuery.CategoryID > 0 ||
		a.musicQuery.WithUsageOnly ||
		a.musicQuery.NewOnly
}

func (a *App) resetMusicFilters() {
	pageSize := a.musicQuery.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	a.musicQuery = gamedata.MusicSearchQuery{
		Kind:        "all",
		ExpansionID: -1,
		CategoryID:  0,
		Page:        1,
		PageSize:    pageSize,
	}
	a.refreshMusicList()
}

func (a *App) selectMusic(kind string, id int) {
	a.selectMusicInternal(kind, id, true)
}

func (a *App) selectMusicInternal(kind string, id int, record bool) {
	if a.service == nil || id <= 0 {
		return
	}
	detail := a.service.GetMusicDetail(kind, id)
	if detail == nil {
		return
	}
	a.selectedMusicKey = detail.Key
	a.selectedMusicKind = detail.Kind
	a.selectedMusicID = detail.ID
	a.musicDetail = detail
	a.musicDetailScroll.Y = 0
	if record {
		a.recordRecent(config.MusicRecordKind(detail.Kind), detail.ID)
	}
	targetKey := detail.Key
	a.prepareMusicDetailSlot(&a.musicAudioSlot, detail, func() bool {
		return a.selectedMusicKey == targetKey
	})
}

func (a *App) prepareMusicDetailSlot(slot *musicAudioSlot, entry *gamedata.MusicEntry, isStillSelected func() bool) {
	if slot == nil || entry == nil {
		return
	}
	a.prepareMusicAudioSlot(slot, entry, isStillSelected)
	a.prepareMusicQuestsSlot(slot, entry, isStillSelected)
}

func (a *App) prepareMusicQuestsSlot(slot *musicAudioSlot, entry *gamedata.MusicEntry, isStillSelected func() bool) {
	if slot == nil || entry == nil {
		return
	}
	if slot.questsKey == entry.Key && slot.questsSvc == a.service {
		return
	}
	slot.questsSeq++
	seq := slot.questsSeq
	reqKey := entry.Key
	svc := a.service
	slot.questsKey = reqKey
	slot.questsSvc = svc
	slot.quests = nil
	slot.questsInitKey = ""

	if svc == nil || entry.IsPlaceholder {
		slot.questsLoading = false
		return
	}
	if a.win == nil {
		slot.questsLoading = false
		slot.quests = svc.GetMusicQuests(entry.Kind, entry.ID)
		return
	}

	slot.questsLoading = true
	kind := entry.Kind
	id := entry.ID
	go func() {
		refs := svc.GetMusicQuests(kind, id)
		a.win.Update(func() {
			still := isStillSelected == nil || isStillSelected()
			if slot.questsSeq != seq || !still || a.service != svc {
				return
			}
			slot.questsLoading = false
			slot.quests = refs
		})
	}()
}

func (a *App) ensureSyncMusicQuestsIfHeadless(slot *musicAudioSlot, m *gamedata.MusicEntry) {
	if slot == nil || m == nil || m.IsPlaceholder || a.service == nil || a.win != nil {
		return
	}
	if !slot.questsLoading && (slot.questsKey != m.Key || slot.questsSvc != a.service) {
		slot.quests = a.service.GetMusicQuests(m.Kind, m.ID)
		slot.questsKey = m.Key
		slot.questsSvc = a.service
		slot.questsInitKey = ""
	}
}

func (a *App) prepareMusicAudioSlot(slot *musicAudioSlot, entry *gamedata.MusicEntry, isStillSelected func() bool) {
	if slot == nil || entry == nil {
		return
	}
	slot.exportMsg = ""
	slot.exportIsErr = false

	// 如果播放器当前正在播放另一首曲目以外的新选择仅按需解包容器，不打断当前播放，直到点击播放
	if a.musicPlayer != nil {
		snap := a.musicPlayer.Snapshot()
		if snap.TrackKey == entry.Key && snap.HasTrack {
			slot.seekPos = snap.Position.Seconds()
		} else {
			slot.seekPos = 0
		}
	}

	if slot.key == entry.Key && slot.detail != nil {
		return
	}
	a.loadMusicAudioSlotAsync(slot, entry, isStillSelected, false)
}

func (a *App) loadMusicAudioSlotAsync(slot *musicAudioSlot, entry *gamedata.MusicEntry, isStillSelected func() bool, autoPlay bool) {
	if slot == nil || entry == nil {
		return
	}
	slot.seq++
	seq := slot.seq
	reqKey := entry.Key
	if autoPlay {
		slot.autoPlayKey = reqKey
	} else {
		slot.autoPlayKey = ""
	}
	slot.key = reqKey
	slot.detail = nil
	slot.err = ""

	svc := a.service
	if svc == nil || entry.File == "" || !entry.FileExists || entry.IsPlaceholder {
		slot.loading = false
		return
	}

	if a.win == nil {
		slot.loading = false
		detail, err := svc.LoadMusicAudio(entry.Kind, entry.ID)
		if err != nil {
			slot.err = err.Error()
			return
		}
		slot.detail = detail
		return
	}

	slot.loading = true
	kind := entry.Kind
	id := entry.ID
	go func() {
		detail, err := svc.LoadMusicAudio(kind, id)
		a.win.Update(func() {
			still := isStillSelected == nil || isStillSelected()
			if slot.seq != seq || !still {
				return
			}
			slot.loading = false
			if err != nil {
				slot.err = err.Error()
				slot.autoPlayKey = ""
				return
			}
			slot.detail = detail
			if slot.autoPlayKey == reqKey {
				slot.autoPlayKey = ""
				a.startMusicSlotPlayback(slot, entry, isStillSelected)
			}
		})
	}()
}

func (a *App) startMusicSlotPlayback(slot *musicAudioSlot, m *gamedata.MusicEntry, isStillSelected func() bool) {
	if slot == nil || m == nil || a.musicPlayer == nil {
		return
	}
	if !m.FileExists || m.IsPlaceholder {
		return
	}
	if slot.detail == nil || slot.key != m.Key {
		a.loadMusicAudioSlotAsync(slot, m, isStillSelected, true)
		return
	}
	if !slot.detail.Playable {
		return
	}
	snap := a.musicPlayer.Snapshot()
	if snap.TrackKey == m.Key && snap.HasTrack {
		a.musicPlayer.TogglePlayPause(a.win)
		return
	}
	if err := a.musicPlayer.PlayDetail(m.Key, slot.detail, a.win); err != nil {
		slot.err = err.Error()
		return
	}
	slot.seekPos = 0
}

func (a *App) locateMusicInList(kind string, id int) {
	if a.service == nil || id <= 0 {
		return
	}
	res, ok := a.service.LocateMusicPage(a.musicQuery, kind, id)
	if !ok {
		pageSize := a.musicQuery.PageSize
		if pageSize <= 0 {
			pageSize = 30
		}
		a.musicQuery = gamedata.MusicSearchQuery{
			Kind:        "all",
			ExpansionID: -1,
			CategoryID:  0,
			Page:        1,
			PageSize:    pageSize,
		}
		res, ok = a.service.LocateMusicPage(a.musicQuery, kind, id)
	}
	if ok {
		if a.musicQuery.Page != res.Page {
			a.musicListScroll.Y = 0
		}
		a.musicQuery.Page = res.Page
		a.musicPageStr = strconv.Itoa(res.Page)
		a.musicRes = res
		a.scrollMusicListKey = kind + ":" + strconv.Itoa(id)
	}
}

func (a *App) jumpToMusic(kind string, id int) {
	a.jumpToMusicInternal(kind, id, true)
}

func (a *App) jumpToMusicInternal(kind string, id int, record bool) {
	if a.service == nil || id <= 0 {
		return
	}
	if detail := a.service.GetMusicDetail(kind, id); detail != nil {
		a.selectMusicInternal(detail.Kind, detail.ID, record)
		a.locateMusicInList(detail.Kind, detail.ID)
	}
}

func (a *App) inspectMusicFromOtherTab(kind string, id int) {
	a.jumpToMusic(kind, id)
	a.navigateTab(tabMusic)
}

func (a *App) inspectBGMFromOtherTab(bgmID int) {
	a.inspectMusicFromOtherTab(gamedata.MusicKindBGM, bgmID)
}

func (a *App) exportMusicSlotAudio(slot *musicAudioSlot, entry *gamedata.MusicEntry, asOGG bool, isStillSelected func() bool) {
	if slot == nil || entry == nil || slot.detail == nil || a.win == nil {
		return
	}
	var data []byte
	var ext, filterName, title string
	if asOGG {
		if !slot.detail.Playable || len(slot.detail.OggData) == 0 {
			return
		}
		data = slot.detail.OggData
		ext = "ogg"
		filterName = "Ogg Vorbis"
		title = a.t("music.save_ogg_title")
	} else {
		if len(slot.detail.RawSCD) == 0 {
			return
		}
		data = slot.detail.RawSCD
		ext = "scd"
		filterName = "FFXIV SCD Audio"
		title = a.t("music.save_scd_title")
	}

	stem := strings.TrimSpace(entry.FileStem)
	if stem == "" {
		stem = fmt.Sprintf("music_%d", entry.ID)
	}
	defaultName := stem + "." + ext
	payload := append([]byte(nil), data...)

	go func() {
		outPath, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:            a.win,
			Title:             title,
			DefaultPath:       defaultName,
			CreateDirectories: true,
			Filters: []mygo.FileFilter{{
				Name:       filterName,
				Extensions: []string{ext},
			}},
		})
		if err != nil {
			a.win.Update(func() {
				if isStillSelected == nil || isStillSelected() {
					slot.exportMsg = a.tf("music.export_fail", "err", err.Error())
					slot.exportIsErr = true
				}
			})
			return
		}
		outPath = strings.TrimSpace(outPath)
		if outPath == "" {
			return
		}
		if filepath.Ext(outPath) == "" {
			outPath += "." + ext
		}
		writeErr := os.WriteFile(outPath, payload, 0o644)
		a.win.Update(func() {
			if isStillSelected != nil && !isStillSelected() {
				return
			}
			if writeErr != nil {
				slot.exportMsg = a.tf("music.export_fail", "err", writeErr.Error())
				slot.exportIsErr = true
				return
			}
			slot.exportMsg = a.tf("music.export_success", "path", outPath)
			slot.exportIsErr = false
		})
	}()
}

func (a *App) renderMusicView(c *ui.Context) {
	kitResizer(c, "music-split", &a.listW, itemListMinWidth, itemListMaxWidth, 8, a.t("shell.resize_music_list"),
		func() { a.renderMusicListPane(c) },
		func() { a.renderMusicDetailPane(c) },
	)
}

func (a *App) renderMusicListPane(c *ui.Context) {
	t := themeOf(c)
	bgmCount := 0
	orchCount := 0
	if a.status != nil {
		bgmCount = a.status.Stats.TotalBGMs
		orchCount = a.status.Stats.TotalOrchestrions
	}
	totalAll := bgmCount + orchCount

	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(10, 12, 10, 12).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Baseline).Grow(1).Shrink(1).Children(func() {
					ui.Text(c, a.t("music.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					numText(c, a.tf("common.items_count", "count", formatInt(a.musicRes.Total))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				})
				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.hasMusicFilter() {
						if kitButton(c, "music-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.resetMusicFilters()
						}
					}
					expOpts := []selectOption[int]{{value: -1, label: a.t("music.exp_all")}}
					if a.status != nil {
						for _, exp := range a.status.MusicExpansions {
							expOpts = append(expOpts, selectOption[int]{value: exp.ID, label: exp.Name})
						}
					}
					if kitSelect(c, "music-exp-sel", &a.musicQuery.ExpansionID, expOpts, 128, 180, true) {
						a.musicQuery.Page = 1
						a.refreshMusicList()
					}
				})
			})

			a.renderPageSearchInput(c, "music-search", &a.musicQuery.Q, a.t("search.music_placeholder"), a.t("search.music_label"), func() {
				a.musicQuery.Page = 1
				a.refreshMusicList()
			})

			kindIdx := 0
			switch a.musicQuery.Kind {
			case gamedata.MusicKindBGM:
				kindIdx = 1
			case gamedata.MusicKindOrchestrion:
				kindIdx = 2
			}
			kindLabels := []string{
				a.tf("music.filter_all", "count", formatInt(totalAll)),
				a.t("music.filter_bgm"),
				a.t("music.filter_orch"),
			}
			if next, changed := kitSegmentedTabs(c, "music-kind-tabs", kindIdx, kindLabels); changed {
				switch next {
				case 1:
					a.musicQuery.Kind = gamedata.MusicKindBGM
					a.musicQuery.CategoryID = 0
				case 2:
					a.musicQuery.Kind = gamedata.MusicKindOrchestrion
				default:
					a.musicQuery.Kind = "all"
				}
				a.musicQuery.Page = 1
				a.refreshMusicList()
			}

			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Wrap().Children(func() {
					if kitToggleChip(c, "music-usage-only", a.t("music.filter_usage_only"), "", a.musicQuery.WithUsageOnly, nil) {
						a.musicQuery.WithUsageOnly = !a.musicQuery.WithUsageOnly
						a.musicQuery.Page = 1
						a.refreshMusicList()
					}
					if a.renderLatestFilterChip(c, "music-latest-new", a.latestMusicCount(), a.musicQuery.NewOnly) {
						a.musicQuery.NewOnly = !a.musicQuery.NewOnly
						a.musicQuery.Page = 1
						a.refreshMusicList()
					}
				})
				if a.musicQuery.Kind != gamedata.MusicKindBGM && a.status != nil && len(a.status.MusicCategories) > 0 {
					catOpts := []selectOption[int]{{value: 0, label: a.t("music.cat_all")}}
					for _, cat := range a.status.MusicCategories {
						catOpts = append(catOpts, selectOption[int]{value: cat.ID, label: cat.Name})
					}
					if kitSelect(c, "music-cat-sel", &a.musicQuery.CategoryID, catOpts, 128, 190, true) {
						if a.musicQuery.CategoryID > 0 {
							a.musicQuery.Kind = gamedata.MusicKindOrchestrion
						}
						a.musicQuery.Page = 1
						a.refreshMusicList()
					}
				}
			})
		})

		if len(a.musicRes.Items) == 0 {
			kitEmpty(c, icoMusic, a.t("music.empty_title"), a.t("music.empty_desc"), func() {
				if a.hasMusicFilter() {
					if kitButton(c, "music-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
						a.resetMusicFilters()
					}
				}
			})
		} else {
			var snap musicPlayerSnapshot
			if a.musicPlayer != nil {
				snap = a.musicPlayer.Snapshot()
			}
			ui.Scroll(c.Key("music-list-scroll")).TrackScroll(&a.musicListScroll).Grow(1).Shrink(1).Children(func() {
				ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
					for _, m := range a.musicRes.Items {
						isSel := m.Key == a.selectedMusicKey
						isPlaying := snap.TrackKey == m.Key && snap.Playing
						fav := a.isFavorite(config.MusicRecordKind(m.Kind), m.ID)
						if a.renderMusicListRow(c, "music-row-"+m.Key, m, isSel, isPlaying, fav, &a.scrollMusicListKey) {
							a.selectMusic(m.Kind, m.ID)
						}
					}
				})
			})
		}

		a.renderPaginationBar(
			c,
			"music-pg",
			a.musicRes.Page,
			a.musicRes.TotalPages,
			&a.musicPageStr,
			func(p int) {
				a.musicQuery.Page = p
				a.refreshMusicList()
			},
		)
	})
}

func (a *App) renderMusicListRow(c *ui.Context, key string, m gamedata.MusicEntry, isSel, isPlaying, favorited bool, scrollToKey *string) bool {
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
		if m.ItemIconID > 0 {
			a.drawGameIcon(c, m.ItemIconID, false, 32, t.radius.md)
		} else {
			icoCol := t.muted
			bg := t.card
			if isSel || isPlaying {
				icoCol = t.primary
				bg = t.accentSoft
			}
			ui.Box(c).Size(32, 32).Radius(t.radius.md).Background(bg).Border(hairline, t.border).Center().Shrink(0).Children(func() {
				ui.Icon(c, icoMusic).Size(15, 15).TextColor(icoCol)
			})
		}

		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
				fw := 500
				if isSel {
					fw = 600
				}
				title := nonEmptyMusicText(m.Title, m.FileStem, fmt.Sprintf("#%d", m.ID))
				ui.Text(c, title).FontSize(t.text.sm).FontWeight(fw).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.isNewMusic(m.Kind, m.ID) {
						a.drawLatestBadge(c)
					}
					if favorited {
						ui.Icon(c, icoStarFill).Size(11, 11).TextColor(t.attention)
					}
					if isPlaying {
						drawStatusDot(c, t.primary)
					}
					numText(c, fmt.Sprintf("#%d", m.ID)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
				})
			})
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
				sub := a.formatMusicRowSubtitle(m)
				ui.Text(c, sub).FontSize(t.text.xs).TextColor(t.muted).SingleLine().Grow(1).Shrink(1)
				kindLbl := a.t("music.kind_bgm")
				kindCol := t.muted
				if m.Kind == gamedata.MusicKindOrchestrion {
					kindLbl = a.t("music.kind_orch")
					kindCol = t.infoText
				}
				ui.Text(c, kindLbl).FontSize(t.text.micro).TextColor(kindCol).SingleLine().Shrink(0)
			})
		})
	})

	if scrollToKey != nil && *scrollToKey != "" && m.Key == *scrollToKey {
		row.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter})
		*scrollToKey = ""
	}
	return row.Clicked()
}

func (a *App) formatMusicRowSubtitle(m gamedata.MusicEntry) string {
	var parts []string
	if m.ExpansionName != "" {
		parts = append(parts, m.ExpansionName)
	}
	if m.CategoryName != "" {
		parts = append(parts, m.CategoryName)
	}
	if m.UsageSummary != "" {
		parts = append(parts, m.UsageSummary)
	} else if m.ItemName != "" {
		parts = append(parts, m.ItemName)
	} else if m.File != "" {
		parts = append(parts, m.File)
	}
	return strings.Join(parts, " · ")
}

func (a *App) renderMusicDetailPane(c *ui.Context) {
	targetKey := a.selectedMusicKey
	a.renderMusicDetailWithOpts(c, musicDetailPaneOpts{
		keyPrefix:      "wb-music",
		entry:          a.musicDetail,
		slot:           &a.musicAudioSlot,
		scroll:         &a.musicDetailScroll,
		isStillCurrent: func() bool { return a.selectedMusicKey == targetKey },
		onJumpQuest:    a.inspectQuestFromOtherTab,
	})
}

func (a *App) renderMusicDetailWithOpts(c *ui.Context, opts musicDetailPaneOpts) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Grow(1).Shrink(1).FillHeight().Children(func() {
		m := opts.entry
		if m == nil {
			kitEmpty(c, icoMusic, a.t("music.detail_empty_title"), a.t("music.detail_empty_desc"), nil)
			return
		}

		scroll := opts.scroll
		if scroll == nil {
			scroll = &a.musicDetailScroll
		}
		slot := opts.slot
		if slot == nil {
			slot = &a.musicAudioSlot
		}

		ui.Row(c).FillWidth().Height(42).Padding(0, 16).Gap(10).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				numText(c, a.tf("music.id_header", "id", fmt.Sprintf("%d", m.ID))).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				if opts.showOpenInMain {
					if kitIconButton(c, opts.keyPrefix+"-open-main", icoExternalLink, a.t("recent.open_in_music"), btnOpts{variant: btnGhost, size: btnSizeXs}) {
						a.inspectMusicFromOtherTab(m.Kind, m.ID)
					}
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
				recKind := config.MusicRecordKind(m.Kind)
				fav := a.isFavorite(recKind, m.ID)
				if kitFavoriteToggleButton(c, opts.keyPrefix+"-fav-btn", a.t("favorites.add_btn"), a.t("favorites.added_btn"), fav) {
					a.toggleFavorite(recKind, m.ID)
				}
				if m.Kind == gamedata.MusicKindOrchestrion {
					drawPill(c, a.t("music.kind_orch"), pillAccent)
				} else {
					drawPill(c, a.t("music.kind_bgm"), pillNeutral)
				}
			})
		})

		ui.Scroll(c.Key(opts.keyPrefix + "-scroll")).TrackScroll(scroll).Grow(1).Shrink(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(20, 24, 32, 24).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(880).Gap(18).Children(func() {
					a.ensureSyncMusicQuestsIfHeadless(slot, m)
					a.renderMusicHero(c, slot, m)
					a.renderMusicPlayerSection(c, opts.keyPrefix, slot, m, opts.isStillCurrent)
					if m.Kind == gamedata.MusicKindOrchestrion && (m.ItemID > 0 || m.Description != "") {
						a.renderMusicOrchestrionSection(c, opts.keyPrefix, m)
					}
					a.renderMusicMetadataSection(c, slot, m)
					a.renderMusicUsagesSection(c, opts.keyPrefix, slot, m)
					if !m.IsPlaceholder {
						a.renderMusicQuestsSection(c, opts.keyPrefix, slot, m, opts.onJumpQuest)
					}
				})
			})
		})
	})
}

func (a *App) renderMusicHero(c *ui.Context, slot *musicAudioSlot, m *gamedata.MusicEntry) {
	t := themeOf(c)
	var snap musicPlayerSnapshot
	if a.musicPlayer != nil {
		snap = a.musicPlayer.Snapshot()
	}
	isPlaying := snap.TrackKey == m.Key && snap.Playing

	title := nonEmptyMusicText(m.Title, m.FileStem, fmt.Sprintf("#%d", m.ID))
	ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Start).Children(func() {
		if m.ItemIconID > 0 {
			a.drawGameIcon(c, m.ItemIconID, false, 52, t.radius.xl)
		} else {
			bg := t.tint(1)
			icoCol := t.subdued
			if isPlaying {
				icoCol = t.primary
				bg = t.tint(2)
			}
			ui.Box(c).Size(52, 52).Radius(t.radius.xl).Background(bg).Border(hairline, t.border).Center().Shrink(0).Children(func() {
				ui.Icon(c, icoMusicFill).Size(24, 24).TextColor(icoCol)
			})
		}

		ui.Column(c).Gap(6).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Baseline).Wrap().Children(func() {
				ui.Text(c, title).FontSize(t.text.xl).FontWeight(600).LetterSpacing(-0.3).TextColor(t.foreground).Selectable()
				if a.isNewMusic(m.Kind, m.ID) {
					a.drawLatestBadge(c)
				}
				numText(c, fmt.Sprintf("#%d", m.ID)).FontSize(t.text.xs).TextColor(t.muted).Selectable()
			})
			if m.FileStem != "" && m.FileStem != title {
				ui.Text(c, m.FileStem).FontSize(t.text.xs).TextColor(t.subdued).Selectable()
			}
			if secLines := a.secondaryDataLines(title, m.NameJa, m.NameEn); len(secLines) > 0 {
				ui.Row(c).Gap(12).Wrap().Children(func() {
					for _, sec := range secLines {
						ui.Textf(c, "%s  %s", sec.badge, sec.text).FontSize(t.text.xs).TextColor(t.muted).Selectable()
					}
				})
			}

			// 内联排版元数据替代多枚彩色胶囊堆叠，胶囊仅保留给异常或特殊状态
			var metaParts []string
			if m.ExpansionName != "" {
				metaParts = append(metaParts, m.ExpansionName)
			}
			if m.CategoryName != "" {
				metaParts = append(metaParts, m.CategoryName)
			}
			if len(m.Usages) > 0 {
				metaParts = append(metaParts, a.tf("music.usage_count", "count", formatInt(len(m.Usages))))
			}
			if slot != nil && !slot.questsLoading && slot.questsKey == m.Key && len(slot.quests) > 0 {
				metaParts = append(metaParts, a.tf("music.quest_count", "count", formatInt(len(slot.quests))))
			}
			if len(metaParts) > 0 || m.IsPlaceholder || !m.FileExists {
				ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
					if len(metaParts) > 0 {
						numText(c, strings.Join(metaParts, " · ")).FontSize(t.text.xs).TextColor(t.subdued)
					}
					if m.IsPlaceholder {
						drawPill(c, a.t("music.audio_placeholder"), pillNeutral)
					} else if !m.FileExists {
						drawPill(c, a.t("music.audio_missing"), pillWarning)
					}
				})
			}
		})
	})
}

func (a *App) renderMusicPlayerSection(c *ui.Context, keyPrefix string, slot *musicAudioSlot, m *gamedata.MusicEntry, isStillSelected func() bool) {
	t := themeOf(c)
	var snap musicPlayerSnapshot
	if a.musicPlayer != nil {
		snap = a.musicPlayer.Snapshot()
	}
	isCurrentTrack := snap.TrackKey == m.Key && snap.HasTrack
	audioReady := slot.detail != nil && slot.key == m.Key
	if isCurrentTrack && snap.Playing {
		slot.seekPos = snap.Position.Seconds()
	}

	drawDetailSection(c, a.t("music.player_section"), m.File, nil, func() {
		if m.IsPlaceholder {
			kitAlert(c, toneInfo, a.t("music.audio_placeholder"))
			return
		}
		if !m.FileExists {
			kitAlert(c, toneWarning, a.t("music.audio_missing"))
			return
		}
		if slot.loading && slot.key == m.Key {
			kitAlert(c, toneInfo, a.t("music.audio_loading"))
			return
		}
		if slot.err != "" && slot.key == m.Key {
			kitAlert(c, toneDanger, a.tf("music.audio_error", "err", slot.err))
			return
		}

		kitBlock(c, ui.Column(c).FillWidth().Padding(14, 16).Gap(12), toneCard).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
					canPlay := !audioReady || slot.detail.Playable
					playLabel := a.t("music.btn_play")
					playIcon := icoPlay
					if isCurrentTrack && snap.Playing {
						playLabel = a.t("music.btn_pause")
						playIcon = icoPause
					} else if isCurrentTrack && snap.Paused {
						playLabel = a.t("music.btn_resume")
					}
					if kitButton(c, keyPrefix+"-play-btn", playLabel, btnOpts{
						variant:  btnDefault,
						size:     btnSizeSm,
						icon:     playIcon,
						disabled: !canPlay,
					}) {
						a.startMusicSlotPlayback(slot, m, isStillSelected)
					}
					if kitButton(c, keyPrefix+"-stop-btn", a.t("music.btn_stop"), btnOpts{
						variant:  btnOutline,
						size:     btnSizeSm,
						icon:     icoStop,
						disabled: !isCurrentTrack,
					}) {
						a.musicPlayer.Stop(a.win)
						slot.seekPos = 0
					}

					loopVar := btnOutline
					loopLabel := a.t("music.loop_off")
					if snap.Loop {
						loopVar = btnSecondary
						loopLabel = a.t("music.loop_on")
					}
					if kitButton(c, keyPrefix+"-loop-btn", loopLabel, btnOpts{
						variant: loopVar,
						size:    btnSizeSm,
						icon:    icoRepeat,
					}) {
						if a.musicPlayer != nil {
							a.musicPlayer.ToggleLoop()
						}
					}
					if audioReady && slot.detail.HasLoop {
						drawPill(c, a.t("music.loop_point_tag"), pillPositive)
					}
				})

				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if kitButton(c, keyPrefix+"-export-ogg", a.t("music.export_ogg"), btnOpts{
						variant:  btnOutline,
						size:     btnSizeSm,
						icon:     icoDownload,
						disabled: !audioReady || !slot.detail.Playable,
					}) {
						a.exportMusicSlotAudio(slot, m, true, isStillSelected)
					}
					if kitButton(c, keyPrefix+"-export-scd", a.t("music.export_scd"), btnOpts{
						variant:  btnOutline,
						size:     btnSizeSm,
						icon:     icoDownload,
						disabled: !audioReady || len(slot.detail.RawSCD) == 0,
					}) {
						a.exportMusicSlotAudio(slot, m, false, isStillSelected)
					}
				})
			})

			if audioReady && !slot.detail.Playable {
				kitAlert(c, toneInfo, a.tf("music.audio_unsupported", "format", slot.detail.FormatName))
			} else {
				maxSec := 1.0
				if isCurrentTrack && snap.Duration > 0 {
					maxSec = snap.Duration.Seconds()
				} else if audioReady && slot.detail.DurationSec > 0 {
					maxSec = slot.detail.DurationSec
				}
				if slot.seekPos < 0 {
					slot.seekPos = 0
				}
				if slot.seekPos > maxSec {
					slot.seekPos = maxSec
				}

				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					curLabel := formatAudioSec(slot.seekPos)
					durLabel := formatAudioSec(maxSec)
					if !audioReady {
						durLabel = "--:--"
					}
					numText(c, curLabel).FontSize(t.text.xs).TextColor(t.subdued).Width(42).Shrink(0)

					seekSlider := ui.SliderBase(c.Key(keyPrefix+"-seek-slider"), &slot.seekPos, 0, maxSec).
						Grow(1).Shrink(1).Height(22).Padding(0, 8)
					if isCurrentTrack {
						seekSlider.Cursor(ui.CursorPointer)
					}
					seekSlider.Draw(func(p *ui.Painter, r ui.Rect) {
						trackW := r.W - 16
						if trackW < 1 {
							return
						}
						ratio := float32(slot.seekPos / maxSec)
						if ratio < 0 {
							ratio = 0
						}
						if ratio > 1 {
							ratio = 1
						}
						trackY := r.Y + (r.H-4)/2
						p.Fill(ui.Rect{X: r.X + 8, Y: trackY, W: trackW, H: 4}, t.border, 2)
						if ratio > 0 {
							p.Fill(ui.Rect{X: r.X + 8, Y: trackY, W: trackW * ratio, H: 4}, t.primary, 2)
						}
						if isCurrentTrack {
							thumbX := r.X + 8 + trackW*ratio - 6
							p.Fill(ui.Rect{X: thumbX, Y: r.Y + (r.H-12)/2, W: 12, H: 12}, t.primary, 6)
						}
					})
					if seekSlider.Changed() && isCurrentTrack && a.musicPlayer != nil {
						a.musicPlayer.Seek(time.Duration(slot.seekPos * float64(time.Second)))
					}

					numText(c, durLabel).FontSize(t.text.xs).TextColor(t.muted).Width(42).TextAlign(ui.End).Shrink(0)

					volIco := icoVolume
					volTip := a.t("music.mute")
					if snap.Muted || a.musicVolume <= 0 {
						volIco = icoVolumeMute
						volTip = a.t("music.unmute")
					}
					if kitIconButton(c, keyPrefix+"-mute-btn", volIco, volTip, btnOpts{variant: btnGhost, size: btnSizeXs}) {
						if a.musicPlayer != nil {
							a.musicPlayer.ToggleMute()
						}
					}
					volSlider := ui.SliderBase(c.Key(keyPrefix+"-vol-slider"), &a.musicVolume, 0, 100).
						Width(86).Height(22).Padding(0, 7).Cursor(ui.CursorPointer)
					volSlider.Draw(func(p *ui.Painter, r ui.Rect) {
						trackW := r.W - 14
						if trackW < 1 {
							return
						}
						effPct := a.musicVolume
						if snap.Muted {
							effPct = 0
						}
						ratio := float32(effPct / 100.0)
						if ratio < 0 {
							ratio = 0
						}
						if ratio > 1 {
							ratio = 1
						}
						trackY := r.Y + (r.H-4)/2
						p.Fill(ui.Rect{X: r.X + 7, Y: trackY, W: trackW, H: 4}, t.border, 2)
						if ratio > 0 {
							p.Fill(ui.Rect{X: r.X + 7, Y: trackY, W: trackW * ratio, H: 4}, t.primary, 2)
						}
						thumbX := r.X + 7 + trackW*ratio - 5
						p.Fill(ui.Rect{X: thumbX, Y: r.Y + (r.H-10)/2, W: 10, H: 10}, t.primary, 5)
					})
					if volSlider.Changed() && a.musicPlayer != nil {
						a.musicPlayer.SetVolume(a.musicVolume / 100.0)
					}
					dispPct := int(a.musicVolume + 0.5)
					if snap.Muted {
						dispPct = 0
					}
					numText(c, fmt.Sprintf("%d%%", dispPct)).FontSize(t.text.micro).TextColor(t.muted).Width(34).TextAlign(ui.End).Shrink(0)
				})
			}

			if slot.exportMsg != "" {
				tone := toneSuccess
				if slot.exportIsErr {
					tone = toneDanger
				}
				kitAlert(c, tone, slot.exportMsg)
			}
		})
	})
}

func (a *App) renderMusicOrchestrionSection(c *ui.Context, keyPrefix string, m *gamedata.MusicEntry) {
	t := themeOf(c)
	drawDetailSection(c, a.t("music.orch_section"), "", nil, func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			if m.Description != "" {
				kitBlock(c, ui.Box(c).FillWidth().Padding(10, 14), toneCard).Children(func() {
					ui.Text(c, m.Description).FontSize(t.text.sm).TextColor(t.subdued).LineHeight(lineProse).Selectable()
				})
			}
			if m.ItemID > 0 {
				row := kitLinkSurface(c, ui.ButtonBase(c.Key(keyPrefix+"-orch-item")).
					FillWidth().Padding(8, 12).Gap(10).Radius(t.radius.lg).AlignItems(ui.Center))
				row.Children(func() {
					a.drawGameIcon(c, m.ItemIconID, false, 28, t.radius.md)
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, a.t("music.orch_roll_item")).FontSize(t.text.micro).TextColor(t.muted)
						ui.Text(c, m.ItemName).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground).SingleLine().Shrink(1)
					})
					ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
						ui.Text(c, a.t("music.orch_open_item")).FontSize(t.text.xs).TextColor(t.primary)
						ui.Icon(c, icoArrowRight).Size(12, 12).TextColor(t.primary)
					})
				})
				if row.Clicked() {
					a.inspectItemFromOtherTab(m.ItemID)
				}
			}
		})
	})
}

func (a *App) renderMusicMetadataSection(c *ui.Context, slot *musicAudioSlot, m *gamedata.MusicEntry) {
	t := themeOf(c)
	type metaKV struct {
		label string
		val   string
	}
	var kvs []metaKV
	if m.File != "" {
		kvs = append(kvs, metaKV{a.t("music.meta_file"), m.File})
	}
	audio := slot.detail
	if audio != nil && slot.key == m.Key {
		fmtLabel := audio.FormatName
		if audio.OggVersion > 0 && audio.Format == sqpack.ScdFormatOggVorbis {
			fmtLabel = fmt.Sprintf("%s (v%d)", audio.FormatName, audio.OggVersion)
		}
		kvs = append(kvs, metaKV{a.t("music.meta_format"), fmtLabel})
		if audio.SampleRate > 0 && audio.Channels > 0 {
			kvs = append(kvs, metaKV{
				a.t("music.meta_sample_rate"),
				fmt.Sprintf("%s Hz · %s", formatInt(audio.SampleRate), a.formatMusicChannels(audio.Channels)),
			})
		}
		if audio.DurationSec > 0 {
			kvs = append(kvs, metaKV{a.t("music.meta_duration"), formatAudioSec(audio.DurationSec)})
		}
		if audio.FileSize > 0 {
			kvs = append(kvs, metaKV{a.t("music.meta_size"), formatMusicBytes(uint32(audio.FileSize))})
		}
		if audio.HasLoop {
			kvs = append(kvs, metaKV{
				a.t("music.meta_loop_range"),
				fmt.Sprintf("%s ~ %s", formatInt(int(audio.LoopStartSample)), formatInt(int(audio.LoopEndSample))),
			})
		}
	}
	if m.Kind == gamedata.MusicKindBGM && m.Priority > 0 {
		kvs = append(kvs, metaKV{a.t("music.meta_priority"), formatInt(m.Priority)})
	}
	var flags []string
	if m.DisableRestart {
		flags = append(flags, a.t("music.flag_disable_restart"))
	}
	if m.PassEnd {
		flags = append(flags, a.t("music.flag_pass_end"))
	}
	if len(flags) > 0 {
		kvs = append(kvs, metaKV{a.t("music.meta_flags"), strings.Join(flags, " · ")})
	}
	if len(kvs) == 0 {
		return
	}

	drawDetailSection(c, a.t("music.meta_section"), "", nil, func() {
		ui.Column(c).FillWidth().Children(func() {
			for i, kv := range kvs {
				if i > 0 {
					kitSeparator(c, false)
				}
				ui.Row(c).FillWidth().Padding(8, 2).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, kv.label).FontSize(t.text.xs).TextColor(t.muted).Shrink(0)
					numText(c, kv.val).FontSize(t.text.xs).TextColor(t.foreground).Selectable()
				})
			}
		})
	})
}

func syncMusicFoldState(initKey *string, open *bool, trackKey string, count int) {
	if *initKey != trackKey {
		*initKey = trackKey
		*open = count <= musicAutoCollapseLimit
	}
}

func (a *App) renderMusicMaybeCollapsibleSection(c *ui.Context, foldKey, title string, itemCount int, open *bool, body func()) {
	if itemCount > musicAutoCollapseLimit {
		drawCollapsibleDetailSection(
			c,
			foldKey,
			title,
			"",
			open,
			a.tf("music.collapse_hint_collapsed", "limit", formatInt(musicAutoCollapseLimit)),
			a.t("music.collapse_hint_expanded"),
			body,
		)
		return
	}
	drawDetailSection(c, title, "", nil, body)
}

func (a *App) renderMusicUsagesSection(c *ui.Context, keyPrefix string, slot *musicAudioSlot, m *gamedata.MusicEntry) {
	t := themeOf(c)
	countStr := formatInt(len(m.Usages))
	title := a.tf("music.usages_section", "count", countStr)

	renderBody := func() {
		if len(m.Usages) == 0 {
			ui.Text(c, a.t("music.usages_empty")).FontSize(t.text.xs).TextColor(t.muted).LineHeight(lineProse)
			return
		}

		ui.Column(c).FillWidth().Children(func() {
			for i, u := range m.Usages {
				if i > 0 {
					kitSeparator(c, false)
				}
				rowKey := fmt.Sprintf("%s-usage-%s-%d-%d", keyPrefix, u.Kind, u.RefID, i)
				canJumpLeve := u.Kind == "leve" && u.RefID > 0
				if canJumpLeve {
					btn := ui.ButtonBase(c.Key(rowKey)).
						FillWidth().Padding(8, 4).Gap(8).Radius(t.radius.md).
						AlignItems(ui.Center).Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
					if btn.Hovered() {
						btn.Background(t.tint(1))
					}
					btn.Children(func() {
						a.renderMusicUsageRowContent(c, u, true)
					})
					if btn.Clicked() {
						a.inspectLeveFromOtherTab(u.RefID)
					}
				} else {
					ui.Row(c).FillWidth().Padding(8, 4).Gap(8).AlignItems(ui.Center).Children(func() {
						a.renderMusicUsageRowContent(c, u, false)
					})
				}
			}
		})
	}

	if slot != nil {
		syncMusicFoldState(&slot.usagesInitKey, &slot.usagesOpen, m.Key, len(m.Usages))
		a.renderMusicMaybeCollapsibleSection(c, fmt.Sprintf("%s-usages-fold-%s", keyPrefix, m.Key), title, len(m.Usages), &slot.usagesOpen, renderBody)
		return
	}
	drawDetailSection(c, title, "", nil, renderBody)
}

func (a *App) renderMusicQuestsSection(c *ui.Context, keyPrefix string, slot *musicAudioSlot, m *gamedata.MusicEntry, onJumpQuest func(int)) {
	t := themeOf(c)
	loading := slot != nil && slot.questsLoading && slot.questsKey == m.Key
	if loading {
		drawDetailSection(c, a.t("music.quests_title_loading"), "", nil, func() {
			ui.Row(c).FillWidth().Padding(4, 0).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).FontSize(13).TextColor(t.primary).Label(a.t("music.quests_loading"))
				ui.Text(c, a.t("music.quests_loading")).FontSize(t.text.xs).TextColor(t.muted)
			})
		})
		return
	}

	var quests []gamedata.MusicQuestRef
	if slot != nil && slot.questsKey == m.Key {
		quests = slot.quests
	}
	title := a.tf("music.quests_section", "count", formatInt(len(quests)))
	renderBody := func() {
		if len(quests) == 0 {
			ui.Text(c, a.t("music.quests_empty")).FontSize(t.text.xs).TextColor(t.muted).LineHeight(lineProse)
			return
		}

		ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
			for idx, qr := range quests {
				qTitle := gamedata.CleanQuestTitle(qr.NameCn)
				if qTitle == "" {
					qTitle = fmt.Sprintf("#%d", qr.QuestID)
				}
				btn := kitLinkSurface(c, ui.ButtonBase(c.Key(fmt.Sprintf("%s-mq-%d-%d", keyPrefix, qr.QuestID, idx))).
					Label(fmt.Sprintf("%s #%d", qTitle, qr.QuestID)).
					MinWidth(260).Grow(1).Shrink(1).Padding(8, 12).Radius(t.radius.lg).
					Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8))
				btn.Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						if qr.TypeIconID > 0 {
							a.drawGameIcon(c, qr.TypeIconID, false, 24, t.radius.sm)
						}
						ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
							ui.Row(c).Gap(6).AlignItems(ui.Center).FillWidth().Children(func() {
								ui.Text(c, qTitle).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground).SingleLine().Shrink(1)
								numTextf(c, "#%d", qr.QuestID).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
							})
							if sub := a.formatMusicQuestSource(qr); sub != "" {
								numText(c, sub).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
							}
						})
					})
					ui.Icon(c, icoArrowRight).Size(12, 12).Shrink(0).TextColor(t.primary)
				})
				if btn.Clicked() && onJumpQuest != nil && qr.QuestID > 0 {
					onJumpQuest(qr.QuestID)
				}
			}
		})
	}

	if slot != nil {
		syncMusicFoldState(&slot.questsInitKey, &slot.questsOpen, m.Key, len(quests))
		a.renderMusicMaybeCollapsibleSection(c, fmt.Sprintf("%s-quests-fold-%s", keyPrefix, m.Key), title, len(quests), &slot.questsOpen, renderBody)
		return
	}
	drawDetailSection(c, title, "", nil, renderBody)
}

func (a *App) formatMusicQuestSource(q gamedata.MusicQuestRef) string {
	var parts []string
	if q.Level > 0 {
		parts = append(parts, fmt.Sprintf("Lv.%d", q.Level))
	}
	if q.IsMSQ {
		parts = append(parts, a.t("quests.type_msq"))
	} else if q.GenreName != "" {
		parts = append(parts, q.GenreName)
	} else if q.SectionName != "" {
		parts = append(parts, q.SectionName)
	} else if q.IsFeature {
		parts = append(parts, a.t("quests.type_feature"))
	} else if q.IsRepeatable {
		parts = append(parts, a.t("quests.type_repeatable"))
	}
	if len(q.CutsceneNames) == 1 {
		parts = append(parts, a.tf("music.quest_src_cutscene", "name", q.CutsceneNames[0]))
	} else if len(q.CutsceneNames) > 1 {
		parts = append(parts, a.tf("music.quest_src_cutscenes", "name", q.CutsceneNames[0], "count", formatInt(len(q.CutsceneNames))))
	} else if q.ViaScript {
		parts = append(parts, a.t("music.quest_src_script"))
	}
	return strings.Join(parts, " · ")
}

func (a *App) renderMusicUsageRowContent(c *ui.Context, u gamedata.MusicUsage, jumpable bool) {
	t := themeOf(c)
	kindLabel, pillK := a.musicUsageKindBadge(u.Kind)
	drawPill(c, kindLabel, pillK)
	if u.IconID > 0 {
		a.drawGameIcon(c, u.IconID, false, 20, t.radius.sm)
	}
	ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
		ui.Text(c, u.Label).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground).SingleLine().Shrink(1).Selectable()
		var sub []string
		if u.Role != "" {
			sub = append(sub, u.Role)
		}
		if u.SubLabel != "" {
			sub = append(sub, u.SubLabel)
		}
		if len(sub) > 0 {
			ui.Text(c, strings.Join(sub, " · ")).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(1)
		}
	})
	if jumpable {
		ui.Icon(c, icoArrowRight).Size(12, 12).TextColor(t.primary)
	} else if u.RefID > 0 {
		numText(c, fmt.Sprintf("#%d", u.RefID)).FontSize(t.text.micro).TextColor(t.muted).Shrink(0)
	}
}

func (a *App) musicUsageKindBadge(kind string) (string, pillKind) {
	switch kind {
	case "territory":
		return a.t("music.usage_territory"), pillPositive
	case "duty":
		return a.t("music.usage_duty"), pillWarning
	case "mount":
		return a.t("music.usage_mount"), pillAccent
	case "fate":
		return a.t("music.usage_fate"), pillWarning
	case "gfate":
		return a.t("music.usage_gfate"), pillAccent
	case "wedding":
		return a.t("music.usage_wedding"), pillAccent
	case "leve":
		return a.t("music.usage_leve"), pillNeutral
	case "switch":
		return a.t("music.usage_switch"), pillNeutral
	default:
		return kind, pillNeutral
	}
}

func (a *App) formatMusicChannels(ch int) string {
	switch ch {
	case 1:
		return a.t("music.ch_mono")
	case 2:
		return a.t("music.ch_stereo")
	default:
		return a.tf("music.ch_multi", "ch", strconv.Itoa(ch))
	}
}

func formatAudioSec(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	total := int(sec + 0.5)
	if total < 0 {
		total = 0
	}
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

func formatMusicBytes(sz uint32) string {
	switch {
	case sz >= 1024*1024:
		return fmt.Sprintf("%.2f MB", float64(sz)/(1024*1024))
	case sz >= 1024:
		return fmt.Sprintf("%.1f KB", float64(sz)/1024)
	default:
		return fmt.Sprintf("%d B", sz)
	}
}

func nonEmptyMusicText(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
