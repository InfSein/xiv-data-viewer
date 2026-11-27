package uiapp

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/exporter"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

type cachedIconPreview struct {
	bmp     *ui.Bitmap
	hdr     sqpack.TextureHeader
	rawSize int
	loading bool
	err     string
}

// iconTileBitmapCache 为「图标」网格提供按多变体/分辨率索引的异步有界贴图缓存。
type iconTileBitmapCache struct {
	mu      sync.Mutex
	maxSize int
	order   []string
	bitmaps map[string]*ui.Bitmap
	missing map[string]bool
	pending map[string]bool
	sem     chan struct{}
}

func newIconTileBitmapCache(maxSize int) *iconTileBitmapCache {
	if maxSize <= 0 {
		maxSize = 600
	}
	return &iconTileBitmapCache{
		maxSize: maxSize,
		bitmaps: make(map[string]*ui.Bitmap, maxSize),
		missing: make(map[string]bool),
		pending: make(map[string]bool),
		sem:     make(chan struct{}, 6),
	}
}

func (c *iconTileBitmapCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order = c.order[:0]
	clear(c.bitmaps)
	clear(c.missing)
	clear(c.pending)
}

func (c *iconTileBitmapCache) Get(svc *gamedata.Service, win *mygo.Window, langTag string, iconID int, variant string, hr1 bool) (*ui.Bitmap, bool) {
	if c == nil || svc == nil || iconID < 0 {
		return nil, false
	}
	key := fmt.Sprintf("%s|%d|%s|%v", langTag, iconID, variant, hr1)

	c.mu.Lock()
	if bmp, ok := c.bitmaps[key]; ok {
		c.mu.Unlock()
		return bmp, true
	}
	if c.missing[key] || c.pending[key] {
		c.mu.Unlock()
		return nil, false
	}
	// 无窗口的测试上下文跳过后台协程解码，保持渲染帧确定且无并发残留
	if win == nil {
		c.mu.Unlock()
		return nil, false
	}
	c.pending[key] = true
	c.mu.Unlock()

	go func() {
		c.sem <- struct{}{}
		img, _, err := svc.GetIconVariantImage(iconID, variant, hr1, 128)
		<-c.sem

		var bmp *ui.Bitmap
		if err == nil && img != nil {
			bmp = ui.NewBitmap(img)
		}

		c.mu.Lock()
		delete(c.pending, key)
		if bmp == nil {
			c.missing[key] = true
		} else {
			if _, exists := c.bitmaps[key]; !exists {
				for len(c.order) >= c.maxSize && len(c.order) > 0 {
					evict := c.order[0]
					c.order = c.order[1:]
					delete(c.bitmaps, evict)
				}
				c.order = append(c.order, key)
			}
			c.bitmaps[key] = bmp
		}
		c.mu.Unlock()

		win.Update(func() {})
	}()

	return nil, false
}

func (a *App) hasIconGroup(groupID int) bool {
	if a.service == nil || groupID < 0 {
		return false
	}
	target := sqpack.IconGroupID(groupID)
	for _, g := range a.service.GetIconGroups() {
		if g.GroupID == target {
			return true
		}
	}
	return false
}

func (a *App) refreshIcons(resetScroll bool) {
	if a.service == nil {
		a.iconRes = gamedata.IconSearchResponse{}
		a.iconGridSelIdx = -1
		a.selectedIconID = -1
		a.selectedIconDetail = nil
		return
	}
	a.iconRes = a.service.SearchIcons(a.iconQuery)
	items := a.iconRes.Items

	matchedIdx := -1
	if a.selectedIconID >= 0 {
		for i, ic := range items {
			if ic.ID == a.selectedIconID {
				matchedIdx = i
				break
			}
		}
	}

	switch {
	case matchedIdx >= 0:
		a.iconGridSelIdx = matchedIdx
		a.selectIconEntry(items[matchedIdx], true)
	case len(items) > 0:
		a.iconGridSelIdx = 0
		a.selectIconEntry(items[0], false)
	default:
		a.iconGridSelIdx = -1
		a.selectedIconID = -1
		a.selectedIconDetail = nil
	}

	if resetScroll {
		a.iconGridState.ScrollTo(0, ui.Start)
	}
}

func (a *App) selectIconGroup(groupID int) {
	if groupID >= 0 {
		groupID = sqpack.IconGroupID(groupID)
	} else {
		groupID = -1
	}
	if a.iconQuery.GroupID == groupID {
		return
	}
	a.iconQuery.GroupID = groupID
	a.iconExportTaskCountKey = ""
	a.refreshIcons(true)
}

func (a *App) selectIconEntry(entry gamedata.IconEntry, keepVariant bool) {
	prevID := a.selectedIconID
	cp := entry
	a.selectedIconID = entry.ID
	a.selectedIconDetail = &cp
	if prevID != entry.ID {
		a.iconInspectorScroll.Y = 0
		a.iconSingleExportMsg = ""
		a.iconSingleExportIsErr = false
	}
	a.syncIconInspectorSelection(keepVariant)
	a.ensureSelectedIconPreviewLoaded()
}

func (a *App) syncIconInspectorSelection(keepVariant bool) {
	detail := a.selectedIconDetail
	if detail == nil || len(detail.Variants) == 0 {
		a.iconInspectorVariant = ""
		a.iconInspectorHR1 = false
		return
	}
	picked := detail.Variants[0]
	found := false
	if keepVariant {
		for _, v := range detail.Variants {
			if v.Variant == a.iconInspectorVariant {
				picked = v
				found = true
				break
			}
		}
	}
	if !found {
		for _, v := range detail.Variants {
			if v.Variant == detail.DefaultVariant {
				picked = v
				found = true
				break
			}
		}
	}
	a.iconInspectorVariant = picked.Variant
	if !keepVariant {
		// 详情页默认优先展示高清分辨率（_hr1）；仅存在标准分辨率时回退标准图
		a.iconInspectorHR1 = picked.HasHR1
	}
	if a.iconInspectorHR1 && !picked.HasHR1 {
		a.iconInspectorHR1 = false
	}
	if !a.iconInspectorHR1 && !picked.HasStd && picked.HasHR1 {
		a.iconInspectorHR1 = true
	}
}

func (a *App) selectedIconPreviewKey() string {
	return fmt.Sprintf("%s|%d|%s|%v", a.Language(), a.selectedIconID, a.iconInspectorVariant, a.iconInspectorHR1)
}

func (a *App) ensureSelectedIconPreviewLoaded() {
	if a.service == nil || a.selectedIconID < 0 {
		return
	}
	if a.iconPreviewCache == nil {
		a.iconPreviewCache = make(map[string]*cachedIconPreview)
	}
	key := a.selectedIconPreviewKey()
	if cur, ok := a.iconPreviewCache[key]; ok && cur != nil {
		return
	}

	svc := a.service
	iconID := a.selectedIconID
	variant := a.iconInspectorVariant
	hr1 := a.iconInspectorHR1

	decodePreview := func() *cachedIconPreview {
		raw, hdr, err := svc.ReadIconVariantRaw(iconID, variant, hr1)
		if err != nil {
			return &cachedIconPreview{hdr: hdr, err: err.Error()}
		}
		if len(raw) == 0 {
			return &cachedIconPreview{hdr: hdr}
		}
		img, decErr := sqpack.DecodeTexToRGBA(raw)
		if decErr != nil {
			return &cachedIconPreview{hdr: hdr, rawSize: len(raw), err: decErr.Error()}
		}
		return &cachedIconPreview{
			bmp:     ui.NewBitmap(img),
			hdr:     hdr,
			rawSize: len(raw),
		}
	}

	if a.win == nil {
		a.iconPreviewCache[key] = decodePreview()
		return
	}

	a.iconPreviewCache[key] = &cachedIconPreview{loading: true}
	go func() {
		res := decodePreview()
		a.dispatchUI(func() {
			if a.service != svc {
				return
			}
			a.iconPreviewCache[key] = res
		})
	}()
}

func (a *App) iconVariantLabel(variant string) string {
	switch strings.Trim(strings.ToLower(strings.TrimSpace(variant)), "/") {
	case "":
		return a.t("icons.variant_default")
	case "hq":
		return a.t("icons.variant_hq")
	case "chs":
		return a.t("icons.variant_chs")
	case "en":
		return a.t("icons.variant_en")
	case "ja":
		return a.t("icons.variant_ja")
	case "de":
		return a.t("icons.variant_de")
	case "fr":
		return a.t("icons.variant_fr")
	case "cht":
		return a.t("icons.variant_cht")
	case "ko":
		return a.t("icons.variant_ko")
	case "tc":
		return a.t("icons.variant_tc")
	default:
		return variant + "/"
	}
}

func (a *App) iconSourceLabel(src gamedata.IconReaderSource) string {
	switch src {
	case gamedata.IconSourceCN:
		return a.t("icons.source_cn")
	case gamedata.IconSourceGlobal:
		return a.t("icons.source_global")
	default:
		return a.t("icons.source_primary")
	}
}

func (a *App) iconCellMetaTag(ic gamedata.IconEntry) string {
	switch {
	case ic.HasHR1:
		return a.t("icons.badge_hr1")
	case ic.HasHQ:
		return a.t("icons.badge_hq")
	case ic.HasLocalized:
		return a.t("icons.badge_loc")
	default:
		return a.t("icons.badge_std")
	}
}

func formatIconBytes(size int) string {
	switch {
	case size <= 0:
		return "--"
	case size < 1024:
		return fmt.Sprintf("%d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024.0)
	default:
		return fmt.Sprintf("%.2f MB", float64(size)/(1024.0*1024.0))
	}
}

// renderIconsView 绘制【资源 -> 图标】浏览、属性预览与 SaintCoinach 规范导出工作台。
func (a *App) renderIconsView(c *ui.Context) {
	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		a.renderIconsHeader(c)

		// 若后台全量图标索引仍在静默扫描中（或失败），向用户展示清晰加载状态提示
		if a.isIconIndexPending() {
			if a.iconIndexError != "" && !a.iconIndexLoading {
				kitEmpty(c, icoAlertCircle, a.t("icons.title"), a.tf("icons.loading_error", "err", a.iconIndexError), func() {
					if kitButton(c, "icons-retry-load-btn", a.t("icons.retry_load"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
						a.startBackgroundIconIndexLoad()
					}
				})
			} else {
				kitLoading(c, a.t("icons.loading_title"), a.t("icons.loading_desc"))
			}
			return
		}

		ui.Row(c).Grow(1).Shrink(1).MinHeight(0).FillWidth().Children(func() {
			kitResizer(c, "icons-group-split", &a.iconGroupListW, iconGroupListMinWidth, iconGroupListMaxWidth, 0, a.t("shell.resize_icon_groups"),
				func() { a.renderIconGroupPane(c) },
				func() {
					ui.Row(c).Fill().Children(func() {
						a.renderIconGridPane(c)
						a.renderIconInspectorPane(c)
					})
				},
			)
		})
	})
}

func (a *App) renderIconsHeader(c *ui.Context) {
	t := themeOf(c)
	pending := a.isIconIndexPending()
	totalIcons := 0
	totalGroups := 0
	if a.status != nil {
		totalIcons = a.status.Stats.TotalIcons
		totalGroups = a.status.Stats.TotalIconGroups
	}

	ui.Column(c).FillWidth().Padding(12, 16, 10, 16).Gap(8).
		BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
		// 顶栏：页面标题、有效总量统计、导出弹窗入口
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(12).Wrap().Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Baseline).Wrap().Children(func() {
				ui.Text(c, a.t("icons.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				if pending {
					ui.Text(c, a.t("icons.loading_badge")).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				} else {
					numText(c, a.tf("icons.summary_stats",
						"groups", formatInt(totalGroups),
						"icons", formatInt(totalIcons),
					)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if a.iconQuery.GroupID >= 0 {
					if kitButton(c, "icons-export-group-btn", a.t("icons.export_group_btn"), btnOpts{
						variant:  btnOutline,
						size:     btnSizeSm,
						icon:     icoFolder,
						disabled: pending,
					}) {
						a.openIconExportModal(true)
					}
				}
				if kitButton(c, "icons-export-all-btn", a.t("icons.export_btn"), btnOpts{
					variant:  btnDefault,
					size:     btnSizeSm,
					icon:     icoDownload,
					disabled: pending,
				}) {
					a.openIconExportModal(false)
				}
			})
		})

		// 次栏：编号/名称检索框 + 变体特征筛选胶囊
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Box(c).Grow(1).Shrink(1).MinWidth(220).Children(func() {
				a.renderPageSearchInput(c, "icons-page-search", &a.iconQuery.Q,
					a.t("icons.search_placeholder"), a.t("icons.search_label"), func() {
						a.refreshIcons(true)
					})
			})

			ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
				filters := []struct {
					id    string
					label string
				}{
					{id: "all", label: a.t("icons.filter_all")},
					{id: "hr1", label: a.t("icons.filter_hr1")},
					{id: "hq", label: a.t("icons.filter_hq")},
					{id: "localized", label: a.t("icons.filter_localized")},
				}
				for _, flt := range filters {
					active := a.iconQuery.VariantFilter == flt.id || (a.iconQuery.VariantFilter == "" && flt.id == "all")
					if kitToggleChip(c, "icons-flt-"+flt.id, flt.label, "", active, nil) {
						a.iconQuery.VariantFilter = flt.id
						a.refreshIcons(true)
					}
				}
				if a.renderLatestFilterChip(c, "icons-flt-latest-new", a.latestIconCount(), a.iconQuery.NewOnly) {
					a.iconQuery.NewOnly = !a.iconQuery.NewOnly
					a.refreshIcons(true)
				}
			})
		})
	})
}

func (a *App) renderIconGroupPane(c *ui.Context) {
	t := themeOf(c)
	var groups []gamedata.IconGroupInfo
	totalIcons := 0
	if a.service != nil {
		groups = a.service.GetIconGroups()
	}
	if a.status != nil {
		totalIcons = a.status.Stats.TotalIcons
	}

	ui.Column(c).Fill().Background(t.sidebar).BorderWidth(0, hairline, 0, 0).BorderColor(t.border).Children(func() {
		ui.Column(c).FillWidth().Padding(10, 12).Gap(6).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Text(c, a.t("icons.groups_title")).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).SingleLine()
					numText(c, a.tf("icons.groups_count", "count", formatInt(len(groups)))).
						FontSize(t.text.micro).TextColor(t.muted).SingleLine()
				})
				ui.Text(c, a.t("icons.groups_subtitle")).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
			})
			grpInp := kitInput(c, "icons-group-filter", &a.iconGroupFilterQ, kitInputOpts{
				placeholder: a.t("icons.groups_filter_placeholder"),
				label:       a.t("icons.groups_filter_placeholder"),
				size:        btnSizeXs,
				tintBg:      true,
				clearable:   true,
			})
			if grpInp.Changed() {
				a.iconGroupScroll.Y = 0
			}
		})

		grpQ := strings.TrimSpace(strings.TrimPrefix(a.iconGroupFilterQ, "#"))
		ui.Scroll(c.Key("icons-group-scroll")).TrackScroll(&a.iconGroupScroll).Grow(1).Shrink(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(6, 8).Gap(2).Children(func() {
				if grpQ == "" {
					allSelected := a.iconQuery.GroupID < 0
					if a.renderIconGroupRow(c, "ic-grp-all", a.t("icons.group_all"), "", formatInt(totalIcons), allSelected) {
						a.selectIconGroup(-1)
					}
				}
				for _, g := range groups {
					if grpQ != "" && !strings.Contains(g.Code, grpQ) && !strings.Contains(g.RangeLabel, grpQ) {
						continue
					}
					sel := a.iconQuery.GroupID == g.GroupID
					if a.renderIconGroupRow(c, "ic-grp-"+g.Code, g.Code, g.RangeLabel, formatInt(g.Count), sel) {
						a.selectIconGroup(g.GroupID)
					}
				}
			})
		})
	})
}

func (a *App) renderIconGroupRow(c *ui.Context, key, title, subtitle, countText string, selected bool) bool {
	t := themeOf(c)
	row := ui.ButtonBase(c.Key(key)).FillWidth().Padding(7, 10).Radius(t.radius.lg).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
	switch {
	case selected && row.Hovered():
		row.Background(t.tint(2.25))
	case selected:
		row.Background(t.tint(1.75))
	case row.Hovered():
		row.Background(t.tint(1))
	}
	titleCol := t.subdued
	fw := 500
	if selected {
		titleCol = t.foreground
		fw = 600
	} else if row.Hovered() {
		titleCol = t.foreground
	}
	row.Children(func() {
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Column(c).Gap(1).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				numText(c, title).FontSize(t.text.xs).FontWeight(fw).TextColor(titleCol).SingleLine()
				if subtitle != "" {
					numText(c, subtitle).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
				}
			})
			numText(c, countText).FontSize(t.text.micro).FontWeight(500).TextColor(t.muted).SingleLine().Shrink(0)
		})
	})
	return row.Clicked()
}

func (a *App) renderIconGridPane(c *ui.Context) {
	t := themeOf(c)
	items := a.iconRes.Items

	ui.Column(c).Grow(1).Shrink(1).MinWidth(0).FillHeight().Children(func() {
		// 当前展示状态条
		banner := a.tf("icons.grid_banner_all", "shown", formatInt(len(items)))
		if a.iconQuery.GroupID >= 0 {
			code := sqpack.IconGroupCode(a.iconQuery.GroupID)
			rangeText := fmt.Sprintf("%s – %06d", code, a.iconQuery.GroupID+sqpack.IconGroupSize-1)
			banner = a.tf("icons.grid_banner_group",
				"group", code,
				"range", rangeText,
				"shown", formatInt(len(items)),
			)
		}
		ui.Row(c).FillWidth().Padding(8, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			numText(c, banner).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Grow(1).Shrink(1)
		})

		if len(items) == 0 {
			kitEmpty(c, icoImage, a.t("icons.empty_title"), a.t("icons.empty_desc"), func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if a.iconQuery.GroupID >= 0 {
						if kitButton(c, "icons-empty-show-all", a.t("icons.empty_show_all"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
							a.selectIconGroup(-1)
						}
					}
					if a.iconQuery.Q != "" || (a.iconQuery.VariantFilter != "" && a.iconQuery.VariantFilter != "all") || a.iconQuery.NewOnly {
						if kitButton(c, "icons-empty-reset", a.t("icons.empty_reset"), btnOpts{variant: btnDefault, size: btnSizeSm}) {
							a.iconQuery.Q = ""
							a.iconQuery.VariantFilter = "all"
							a.iconQuery.NewOnly = false
							a.refreshIcons(true)
						}
					}
				})
			})
			return
		}

		a.iconGridState.Selected = &a.iconGridSelIdx
		a.iconGridState.Key = func(i int) any {
			if i >= 0 && i < len(items) {
				return items[i].ID
			}
			return i
		}
		a.iconGridState.Label = func(i int) string {
			if i >= 0 && i < len(items) {
				return items[i].Code
			}
			return ""
		}

		langTag := string(a.Language())
		ui.Box(c).Grow(1).Shrink(1).FillWidth().MinHeight(0).Padding(8, 12).Children(func() {
			grid := ui.GridView(c, &a.iconGridState, len(items), 82, 98, func(i int) {
				if i < 0 || i >= len(items) {
					return
				}
				ic := items[i]
				isSel := a.selectedIconID == ic.ID
				codeCol := t.foreground
				codeWeight := 500
				if isSel {
					codeCol = t.primary
					codeWeight = 600
				}

				ui.Column(c).Fill().Padding(6, 4).Gap(3).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					box := ui.Box(c).Size(48, 48).Radius(t.radius.md).
						Background(t.card).Border(hairline, t.border).Center().Clip()
					if isSel {
						box.Border(strokeWidth, t.primary).Background(t.accentSoft)
					}
					box.Children(func() {
						if bmp, ok := a.iconTileCache.Get(a.service, a.win, langTag, ic.ID, ic.DefaultVariant, ic.DefaultHR1); ok && bmp != nil {
							ui.Image(c, bmp).Size(42, 42).Fit(ui.Contain)
						} else {
							ui.Icon(c, icoImage).Size(18, 18).TextColor(t.muted)
						}
					})

					numText(c, ic.Code).FontSize(t.text.micro).FontWeight(codeWeight).TextColor(codeCol).SingleLine()
					if a.isNewIcon(ic.ID) {
						ui.Text(c, a.latestCompactBadgeLabel()).FontSize(t.text.micro).FontWeight(600).TextColor(t.successText).SingleLine()
					} else {
						numText(c, a.iconCellMetaTag(ic)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
					}
				})
			}).Fill()

			if grid.Changed() && a.iconGridSelIdx >= 0 && a.iconGridSelIdx < len(items) {
				a.selectIconEntry(items[a.iconGridSelIdx], false)
			}
			if grid.Submitted() && a.iconGridSelIdx >= 0 && a.iconGridSelIdx < len(items) {
				a.selectIconEntry(items[a.iconGridSelIdx], false)
				a.openIconInspectorLightbox()
			}
		})
	})
}

func (a *App) renderIconInspectorPane(c *ui.Context) {
	t := themeOf(c)
	ui.Column(c).Width(292).Shrink(0).FillHeight().Background(t.background).
		BorderWidth(0, 0, 0, hairline).BorderColor(t.border).Children(func() {
		detail := a.selectedIconDetail
		if detail == nil || a.selectedIconID < 0 {
			kitEmpty(c, icoImage, a.t("icons.title"), a.t("icons.inspector_empty"), nil)
			return
		}

		ui.Scroll(c.Key("icons-inspector-scroll")).TrackScroll(&a.iconInspectorScroll).Fill().Children(func() {
			ui.Column(c).FillWidth().Padding(14, 16).Gap(12).Children(func() {
				// 顶部横向概览：左侧高清/标准大图预览（点击放大）+ 右侧编号、相对路径与单图保存按钮
				relPath := sqpack.IconExportRelPath(detail.ID, a.iconInspectorVariant, a.iconInspectorHR1)
				a.ensureSelectedIconPreviewLoaded()
				var prev *cachedIconPreview
				if a.iconPreviewCache != nil {
					prev = a.iconPreviewCache[a.selectedIconPreviewKey()]
				}

				ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					previewBtn := ui.ButtonBase(c.Key("icon-insp-preview-zoom")).Label(a.t("icons.preview_zoom_tip")).
						Size(92, 92).Radius(t.radius.lg).Background(t.card).
						Border(hairline, t.border).Center().Shrink(0).Clip().
						Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
					if previewBtn.Hovered() {
						previewBtn.Border(strokeWidth, t.primary).Background(t.accentSoft)
					}
					previewBtn.Children(func() {
						switch {
						case prev != nil && prev.bmp != nil:
							ui.Image(c, prev.bmp).Size(84, 84).Fit(ui.Contain)
						case prev != nil && prev.err != "":
							ui.Text(c, a.tf("icons.insp_preview_err", "err", prev.err)).
								FontSize(t.text.micro).TextColor(t.dangerText)
						case prev != nil && prev.loading:
							ui.Text(c, a.t("icons.insp_preview_loading")).
								FontSize(t.text.micro).TextColor(t.muted)
						default:
							ui.Icon(c, icoImage).Size(28, 28).TextColor(t.muted)
						}
					})
					if previewBtn.Clicked() {
						a.openIconInspectorLightbox()
					}
					kitTooltip(c, previewBtn, a.t("icons.preview_zoom_tip"))

					ui.Column(c).Gap(5).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
							numTextf(c, "#%s", detail.Code).FontSize(t.text.xl).FontWeight(600).TextColor(t.foreground).SingleLine()
							if a.isNewIcon(detail.ID) {
								a.drawLatestBadge(c)
							}
							if a.iconInspectorHR1 {
								drawPill(c, "HD _hr1", pillAccent)
							} else {
								drawPill(c, "PNG", pillNeutral)
							}
						})
						ui.Text(c, relPath).FontSize(t.text.micro).TextColor(t.muted).Selectable()
						if kitButton(c, "icon-save-single-png", a.t("icons.export_single_btn"), btnOpts{
							variant: btnDefault,
							size:    btnSizeXs,
							icon:    icoDownload,
						}) {
							a.exportSingleIconPNG(detail.ID, a.iconInspectorVariant, a.iconInspectorHR1)
						}
					})
				})

				if a.iconSingleExportMsg != "" {
					tone := toneSuccess
					if a.iconSingleExportIsErr {
						tone = toneDanger
					}
					kitAlert(c, tone, a.iconSingleExportMsg)
				}

				// 清晰度与目录变体切换（仅显示当前图标实际存在的资源）
				curVar, hasCurVar := findIconVariantInfo(detail, a.iconInspectorVariant)
				a.renderIconVariantAndResolutionControls(c, "icon", detail, &a.iconInspectorVariant, &a.iconInspectorHR1, func() {
					a.iconSingleExportMsg = ""
					a.ensureSelectedIconPreviewLoaded()
				})

				// 贴图与文件路径元数据
				dimText := "--"
				fmtText := "--"
				mipText := "--"
				rawSizeText := "--"
				if prev != nil {
					if prev.hdr.Width > 0 && prev.hdr.Height > 0 {
						dimText = fmt.Sprintf("%d × %d", prev.hdr.Width, prev.hdr.Height)
					}
					if prev.hdr.Format != 0 {
						fmtText = sqpack.TexFormatName(prev.hdr.Format)
					}
					if prev.hdr.MipLevels > 0 {
						mipText = fmt.Sprintf("%d", prev.hdr.MipLevels)
					}
					rawSizeText = formatIconBytes(prev.rawSize)
				}
				srcLabel := a.iconSourceLabel(gamedata.IconSourcePrimary)
				if hasCurVar {
					if a.iconInspectorHR1 {
						srcLabel = a.iconSourceLabel(curVar.HR1Source)
					} else {
						srcLabel = a.iconSourceLabel(curVar.StdSource)
					}
				}

				drawDetailSection(c, a.t("icons.meta_title"), "", nil, func() {
					kitBlock(c, ui.Column(c).FillWidth().Padding(10, 12).Gap(8), toneCard).Children(func() {
						ui.Grid(c).FillWidth().Columns(2).Gap(8).Children(func() {
							a.renderIconMetaCell(c, a.t("icons.meta_id"), detail.Code, true)
							a.renderIconMetaCell(c, a.t("icons.meta_group"), sqpack.IconGroupCode(detail.GroupID), true)
							a.renderIconMetaCell(c, a.t("icons.meta_dimensions"), dimText, true)
							a.renderIconMetaCell(c, a.t("icons.meta_format"), fmtText, true)
							a.renderIconMetaCell(c, a.t("icons.meta_mips"), mipText, true)
							a.renderIconMetaCell(c, a.t("icons.meta_size"), rawSizeText, true)
						})
						a.renderIconMetaRow(c, a.t("icons.meta_source"), srcLabel, false)
					})
				})

				// 关联游戏数据反查
				usages := a.service.GetIconUsages(detail.ID)
				usageMeta := ""
				if len(usages) > 0 {
					usageMeta = a.tf("icons.usages_count", "count", formatInt(len(usages)))
				}
				drawDetailSection(c, a.t("icons.usages_title"), usageMeta, nil, func() {
					if len(usages) == 0 {
						ui.Text(c, a.t("icons.usages_empty")).
							FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.muted)
						return
					}
					const maxVisibleUsages = 40
					showCount := min(len(usages), maxVisibleUsages)
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						for idx := 0; idx < showCount; idx++ {
							u := usages[idx]
							kindLabel, kindPill := a.iconUsageKindBadge(u.Kind)
							btn := ui.ButtonBase(c.Key(fmt.Sprintf("icon-usage-%s-%d-%d", u.Kind, u.ID, idx))).
								FillWidth().Padding(6, 8).Radius(t.radius.md).
								Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
							if btn.Hovered() {
								btn.Background(t.card)
							}
							btn.Children(func() {
								ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
									drawPill(c, kindLabel, kindPill)
									ui.Text(c, u.Name).FontSize(t.text.xs).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
									numTextf(c, "#%d", u.ID).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
								})
							})
							if btn.Clicked() {
								a.openIconUsageRef(u)
							}
						}
						if len(usages) > maxVisibleUsages {
							numText(c, a.tf("icons.usages_more", "count", formatInt(len(usages)-maxVisibleUsages))).
								FontSize(t.text.micro).TextColor(t.muted).Padding(4, 0, 0, 0)
						}
					})
				})
			})
		})
	})
}

func (a *App) renderIconMetaCell(c *ui.Context, label, value string, numeric bool) {
	t := themeOf(c)
	ui.Column(c).FillWidth().Gap(1).Children(func() {
		ui.Text(c, label).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
		var valElem ui.Element
		if numeric {
			valElem = numText(c, value)
		} else {
			valElem = ui.Text(c, value)
		}
		valElem.FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).Selectable()
	})
}

func (a *App) renderIconMetaRow(c *ui.Context, label, value string, numeric bool) {
	t := themeOf(c)
	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Baseline).Gap(10).Children(func() {
		ui.Text(c, label).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
		var valElem ui.Element
		if numeric {
			valElem = numText(c, value)
		} else {
			valElem = ui.Text(c, value)
		}
		valElem.FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).Selectable()
	})
}

func (a *App) iconUsageKindBadge(kind string) (string, pillKind) {
	switch kind {
	case "item":
		return a.t("icons.usage_kind_item"), pillNeutral
	case "quest":
		return a.t("icons.usage_kind_quest"), pillAccent
	case "achievement":
		return a.t("icons.usage_kind_achievement"), pillPositive
	case "leve":
		return a.t("icons.usage_kind_leve"), pillWarning
	default:
		return kind, pillOutline
	}
}

func (a *App) openIconUsageRef(u gamedata.IconUsageRef) {
	switch u.Kind {
	case "item":
		a.inspectItemFromOtherTab(u.ID)
	case "quest":
		a.inspectQuestFromOtherTab(u.ID)
	case "achievement":
		a.inspectAchievementFromOtherTab(u.ID, 0)
	case "leve":
		a.inspectLeveFromOtherTab(u.ID)
	}
}

func (a *App) exportSingleIconPNG(iconID int, variant string, hr1 bool) {
	if a.service == nil || iconID < 0 {
		return
	}
	raw, _, err := a.service.ReadIconVariantRaw(iconID, variant, hr1)
	if err != nil {
		a.iconSingleExportMsg = a.tf("icons.export_single_err", "err", err.Error())
		a.iconSingleExportIsErr = true
		return
	}
	if len(raw) == 0 {
		a.iconSingleExportMsg = a.tf("icons.export_single_err", "err", "empty texture data")
		a.iconSingleExportIsErr = true
		return
	}
	pngBytes, decErr := sqpack.DecodeTexToPNGWithCompression(raw, png.DefaultCompression)
	if decErr != nil {
		a.iconSingleExportMsg = a.tf("icons.export_single_err", "err", decErr.Error())
		a.iconSingleExportIsErr = true
		return
	}
	if a.win == nil {
		return
	}

	suffix := ""
	if hr1 {
		suffix = "_hr1"
	}
	defaultName := fmt.Sprintf("%06d%s.png", iconID, suffix)
	title := a.t("icons.export_single_title")
	filterName := a.t("icons.export_single_filter")

	go func() {
		savePath, dlgErr := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:            a.win,
			Title:             title,
			DefaultPath:       defaultName,
			CreateDirectories: true,
			Filters:           []mygo.FileFilter{{Name: filterName, Extensions: []string{"png"}}},
		})
		if dlgErr != nil || strings.TrimSpace(savePath) == "" {
			return
		}
		wErr := os.WriteFile(savePath, pngBytes, 0o644)
		a.dispatchUI(func() {
			if a.selectedIconID != iconID {
				return
			}
			if wErr != nil {
				a.iconSingleExportMsg = a.tf("icons.export_single_err", "err", wErr.Error())
				a.iconSingleExportIsErr = true
			} else {
				a.iconSingleExportMsg = a.tf("icons.export_single_ok", "path", filepath.Base(savePath))
				a.iconSingleExportIsErr = false
			}
		})
	}()
}

// syncAvailableIconExportVariants 同步客户端实际存在的图标子目录变体并默认全选。
func (a *App) syncAvailableIconExportVariants() {
	if a.iconExportVariants == nil {
		a.iconExportVariants = make(map[string]bool)
	}
	if a.status == nil {
		return
	}
	avail := a.status.AvailableIconVariants
	availSet := make(map[string]bool, len(avail))
	for _, v := range avail {
		availSet[v] = true
	}
	for k := range a.iconExportVariants {
		if !availSet[k] {
			delete(a.iconExportVariants, k)
		}
	}
	anySelected := false
	for _, v := range avail {
		if sel, exists := a.iconExportVariants[v]; exists {
			if sel {
				anySelected = true
			}
		} else {
			a.iconExportVariants[v] = true
			anySelected = true
		}
	}
	if !anySelected {
		for _, v := range avail {
			a.iconExportVariants[v] = true
		}
	}
	a.iconExportTaskCountKey = ""
}

func (a *App) openIconExportModal(scopeCurrentGroup bool) {
	a.syncAvailableIconExportVariants()
	if !a.iconExportIncludeStd && !a.iconExportIncludeHR1 {
		a.iconExportIncludeStd = true
		a.iconExportIncludeHR1 = true
	}
	a.iconExportScopeGroup = scopeCurrentGroup && a.iconQuery.GroupID >= 0
	a.iconExportError = ""
	a.iconExportTaskCountKey = ""
	if strings.TrimSpace(a.iconExportDirDraft) == "" && strings.TrimSpace(a.cfg.IconExportOutputDir) != "" {
		a.iconExportDirDraft = a.cfg.IconExportOutputDir
	}
	a.iconExportOpen = true
}

func (a *App) selectedIconExportVariants() []string {
	if a.status == nil {
		return nil
	}
	var out []string
	for _, v := range a.status.AvailableIconVariants {
		if a.iconExportVariants[v] {
			out = append(out, v)
		}
	}
	return out
}

func (a *App) currentIconExportOptions() (exporter.IconExportOptions, bool) {
	selectedVars := a.selectedIconExportVariants()
	if len(selectedVars) == 0 || (!a.iconExportIncludeStd && !a.iconExportIncludeHR1) {
		return exporter.IconExportOptions{GroupID: -1}, false
	}
	groupID := -1
	if a.iconExportScopeGroup && a.iconQuery.GroupID >= 0 {
		groupID = a.iconQuery.GroupID
	}
	return exporter.IconExportOptions{
		IncludeStandard: a.iconExportIncludeStd,
		IncludeHR1:      a.iconExportIncludeHR1,
		Variants:        selectedVars,
		GroupID:         groupID,
	}, true
}

func (a *App) countIconExportTasksCached() int {
	opts, valid := a.currentIconExportOptions()
	if !valid || a.iconExporterSvc == nil {
		a.iconExportTaskCount = 0
		a.iconExportTaskCountKey = "none"
		return 0
	}
	varsSorted := append([]string(nil), opts.Variants...)
	sort.Strings(varsSorted)
	cacheKey := fmt.Sprintf("%s|%v|%v|%d|%s", a.Language(), opts.IncludeStandard, opts.IncludeHR1, opts.GroupID, strings.Join(varsSorted, ","))
	if a.iconExportTaskCountKey == cacheKey {
		return a.iconExportTaskCount
	}
	cnt := a.iconExporterSvc.CountMatchingTasks(opts)
	a.iconExportTaskCountKey = cacheKey
	a.iconExportTaskCount = cnt
	return cnt
}

func (a *App) persistIconExportDir(dir string) {
	a.cfg.IconExportOutputDir = strings.TrimSpace(dir)
	if strings.TrimSpace(a.dataDir) != "" {
		_ = config.SaveAppConfig(a.dataDir, a.cfg)
	}
}

func (a *App) startIconLocalExportFlow() {
	if a.iconExporterSvc == nil {
		return
	}
	dir := strings.TrimSpace(a.iconExportDirDraft)
	if dir == "" {
		a.iconExportError = a.t("icon_export.err_no_dir")
		return
	}
	opts, valid := a.currentIconExportOptions()
	if !valid {
		return
	}
	a.iconExportError = ""
	a.persistIconExportDir(dir)

	onUpdate := func(p exporter.IconExportProgress) {
		cp := p
		a.dispatchUI(func() {
			a.iconExportProgress = &cp
		})
	}
	if err := a.iconExporterSvc.StartExportToDirectory(dir, opts, onUpdate); err != nil {
		a.iconExportError = err.Error()
	}
}

func (a *App) startIconZipExportFlow() {
	if a.iconExporterSvc == nil || a.win == nil {
		return
	}
	opts, valid := a.currentIconExportOptions()
	if !valid {
		return
	}
	a.iconExportError = ""

	scopeTag := "all"
	if opts.GroupID >= 0 {
		scopeTag = sqpack.IconGroupCode(opts.GroupID)
	}
	defaultName := fmt.Sprintf("ffxiv-icons-%s-%s.zip", scopeTag, time.Now().Format("20060102-150405"))
	title := a.t("icon_export.zip_dialog_title")
	filterName := a.t("icon_export.zip_filter_name")

	go func() {
		savePath, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:            a.win,
			Title:             title,
			DefaultPath:       defaultName,
			CreateDirectories: true,
			Filters:           []mygo.FileFilter{{Name: filterName, Extensions: []string{"zip"}}},
		})
		if err != nil || strings.TrimSpace(savePath) == "" {
			return
		}
		a.dispatchUI(func() {
			onUpdate := func(p exporter.IconExportProgress) {
				cp := p
				a.dispatchUI(func() {
					a.iconExportProgress = &cp
				})
			}
			if expErr := a.iconExporterSvc.StartExportToZipFile(savePath, opts, onUpdate); expErr != nil {
				a.iconExportError = expErr.Error()
			}
		})
	}()
}

// renderIconExportModal 绘制遵循 SaintCoinach 规范的批量图标导出弹窗（本地目录或 Zip 压缩包）。
func (a *App) renderIconExportModal(c *ui.Context) {
	isRunning := a.iconExportProgress != nil && a.iconExportProgress.IsRunning
	if !a.iconExportOpen && !isRunning {
		return
	}
	t := themeOf(c)
	totalTasks := a.countIconExportTasksCached()
	var availableVars []string
	if a.status != nil {
		availableVars = a.status.AvailableIconVariants
	}

	ui.DialogBase(c.Key("icon-export-modal"), &a.iconExportOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(t.scrim)
		applyOverlaySurface(c, panel.Width(580).Radius(t.radius.x2l).Clip(), shadowDialog)

		// 标题栏
		ui.Row(c).FillWidth().Padding(16, 20, 12, 20).Justify(ui.SpaceBetween).AlignItems(ui.Start).Gap(12).Children(func() {
			ui.Column(c).Gap(3).Grow(1).Shrink(1).Children(func() {
				ui.Text(c, a.t("icon_export.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground)
				ui.Text(c, a.t("icon_export.subtitle")).FontSize(t.text.xs).TextColor(t.muted)
			})
			if kitIconButton(c, "icon-exp-close-top", icoClose, a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm, disabled: isRunning}) {
				a.iconExportOpen = false
			}
		})

		// 可滚动设置区（保证 640px 窗口高度下底栏不裁切）
		ui.Scroll(c.Key("icon-exp-body-scroll")).FillWidth().MaxHeight(430).Children(func() {
			if a.isIconIndexPending() {
				kitLoading(c, a.t("icons.loading_title"), a.t("icons.loading_desc"))
				return
			}
			ui.Column(c).FillWidth().Padding(4, 20, 16, 20).Gap(14).Children(func() {
				// 1. 导出范围
				if a.iconQuery.GroupID >= 0 {
					ui.Column(c).FillWidth().Gap(6).Children(func() {
						ui.Text(c, a.t("icon_export.scope_label")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
						grpCode := sqpack.IconGroupCode(a.iconQuery.GroupID)
						grpCount := 0
						if a.service != nil {
							for _, g := range a.service.GetIconGroups() {
								if g.GroupID == a.iconQuery.GroupID {
									grpCount = g.Count
									break
								}
							}
						}
						ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
							if kitToggleChip(c, "icon-exp-scope-all", a.t("icon_export.scope_all"), "", !a.iconExportScopeGroup, nil) && !isRunning {
								a.iconExportScopeGroup = false
								a.iconExportTaskCountKey = ""
							}
							if kitToggleChip(c, "icon-exp-scope-grp",
								a.tf("icon_export.scope_group", "group", grpCode, "count", formatInt(grpCount)),
								"", a.iconExportScopeGroup, nil) && !isRunning {
								a.iconExportScopeGroup = true
								a.iconExportTaskCountKey = ""
							}
						})
					})
				}

				// 2. 分辨率勾选（标准 .png / 高清 _hr1.png）
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					ui.Text(c, a.t("icon_export.res_label")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
					ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
						if kitToggleChip(c, "icon-exp-res-std", a.t("icon_export.res_std"), "", a.iconExportIncludeStd, nil) && !isRunning {
							a.iconExportIncludeStd = !a.iconExportIncludeStd
							a.iconExportTaskCountKey = ""
						}
						if kitToggleChip(c, "icon-exp-res-hr1", a.t("icon_export.res_hr1"), "", a.iconExportIncludeHR1, nil) && !isRunning {
							a.iconExportIncludeHR1 = !a.iconExportIncludeHR1
							a.iconExportTaskCountKey = ""
						}
					})
				})

				// 3. 子目录变体勾选（默认全选可用目录，支持切换）
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
						ui.Text(c, a.t("icon_export.variants_label")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
						if kitButton(c, "icon-exp-var-all", a.t("icon_export.select_all_variants"), btnOpts{
							variant:  btnGhost,
							size:     btnSizeXs,
							disabled: isRunning || len(availableVars) == 0,
						}) {
							for _, v := range availableVars {
								a.iconExportVariants[v] = true
							}
							a.iconExportTaskCountKey = ""
						}
					})
					ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
						for _, v := range availableVars {
							active := a.iconExportVariants[v]
							if kitToggleChip(c, "icon-exp-var-"+v, a.iconVariantLabel(v), "", active, nil) && !isRunning {
								a.iconExportVariants[v] = !active
								a.iconExportTaskCountKey = ""
							}
						}
					})
				})

				// 4. 输出文件预估总数
				kitBlock(c, ui.Row(c).FillWidth().Padding(10, 12).Justify(ui.SpaceBetween).AlignItems(ui.Center), toneCard).Children(func() {
					ui.Text(c, a.t("icon_export.summary_label")).FontSize(t.text.xs).TextColor(t.subdued)
					numText(c, a.tf("icon_export.summary_value", "count", formatInt(totalTasks))).
						FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
				})

				// 5. 本地输出目录选择
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					ui.Text(c, a.t("icon_export.output_dir")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Grow(1).Shrink(1).Children(func() {
							kitInput(c, "icon-exp-dir-input", &a.iconExportDirDraft, kitInputOpts{
								placeholder: a.t("icon_export.output_placeholder"),
								label:       a.t("icon_export.output_dir"),
								size:        btnSizeMd,
								disabled:    isRunning,
								clearable:   true,
							})
						})
						if kitButton(c, "icon-exp-dir-browse", a.t("common.browse_short"), btnOpts{
							variant:  btnOutline,
							size:     btnSizeMd,
							icon:     icoFolder,
							disabled: isRunning,
						}) {
							cur := a.iconExportDirDraft
							a.pickDirectoryAsync(a.t("icon_export.dialog_dir_title"), cur, func(picked string) {
								a.iconExportDirDraft = picked
								a.persistIconExportDir(picked)
							})
						}
					})
					ui.Text(c, a.t("icon_export.dir_hint")).FontSize(t.text.micro).TextColor(t.muted)
				})

				if a.iconExportError != "" {
					kitAlert(c, toneDanger, a.iconExportError)
				}
			})
		})

		// 实时导出进度条固定在滚动区外、底栏上方，确保任务执行期间始终可见且不被滚动折叠
		if a.iconExportProgress != nil {
			p := a.iconExportProgress
			ui.Column(c).FillWidth().Padding(10, 20).Gap(6).Background(t.card).
				BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
				ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
					statusTxt := a.t("icon_export.status_idle")
					statusCol := t.muted
					switch {
					case p.IsRunning:
						statusTxt = a.t("icon_export.status_running")
						statusCol = t.primary
					case p.IsCancelled:
						statusTxt = a.t("icon_export.status_cancelled")
						statusCol = t.warningText
					case p.IsCompleted:
						statusTxt = a.t("icon_export.status_done")
						statusCol = t.successText
					}
					ui.Text(c, statusTxt).FontSize(t.text.xs).FontWeight(600).TextColor(statusCol)
					ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
						numText(c, a.tf("icon_export.progress_count",
							"done", formatInt(p.ProcessedTasks),
							"total", formatInt(p.TotalTasks),
						)).FontSize(t.text.xs).TextColor(t.subdued)
						numTextf(c, "%d%%", p.Percent).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground)
					})
				})

				pct := p.Percent
				if pct < 0 {
					pct = 0
				}
				if pct > 100 {
					pct = 100
				}
				done := float32(pct)
				if p.IsRunning && done < 2 {
					done = 2
				}
				kitProgressBar(c, done)

				if p.CurrentFile != "" {
					ui.Text(c, p.CurrentFile).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
				}
				if p.ErrorCount > 0 {
					numText(c, a.tf("icon_export.progress_errors", "count", formatInt(p.ErrorCount))).
						FontSize(t.text.micro).TextColor(t.warningText)
				}
				if p.ErrorMessage != "" {
					ui.Text(c, p.ErrorMessage).FontSize(t.text.xs).TextColor(t.dangerText)
				}
			})
		}

		// 底部按钮栏
		ui.Row(c).FillWidth().Padding(12, 20).Background(t.sidebar).
			BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			if isRunning {
				if kitButton(c, "icon-exp-cancel-btn", a.t("icon_export.cancel_task"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
					if a.iconExporterSvc != nil {
						a.iconExporterSvc.Cancel()
					}
				}
			} else {
				if kitButton(c, "icon-exp-close-bottom", a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
					a.iconExportOpen = false
				}
			}

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				canExport := !isRunning && totalTasks > 0
				if kitButton(c, "icon-exp-zip-btn", a.t("icon_export.zip_btn"), btnOpts{
					variant:  btnOutline,
					size:     btnSizeSm,
					disabled: !canExport,
				}) {
					a.startIconZipExportFlow()
				}
				canLocal := canExport && strings.TrimSpace(a.iconExportDirDraft) != ""
				if kitButton(c, "icon-exp-local-btn", a.t("icon_export.local_btn"), btnOpts{
					variant:  btnDefault,
					size:     btnSizeSm,
					disabled: !canLocal,
				}) {
					a.startIconLocalExportFlow()
				}
			})
		})
	})

	if isRunning && !a.iconExportOpen {
		a.iconExportOpen = true
	}
}

func findIconVariantInfo(detail *gamedata.IconEntry, variant string) (gamedata.IconVariantInfo, bool) {
	if detail == nil || len(detail.Variants) == 0 {
		return gamedata.IconVariantInfo{}, false
	}
	for _, v := range detail.Variants {
		if v.Variant == variant {
			return v, true
		}
	}
	return detail.Variants[0], true
}

// renderIconVariantAndResolutionControls 在详情栏与大图预览弹窗间共享清晰度（标准 / _hr1）与目录变体快捷切换控件。
func (a *App) renderIconVariantAndResolutionControls(
	c *ui.Context,
	keyPrefix string,
	detail *gamedata.IconEntry,
	variant *string,
	hr1 *bool,
	onChanged func(),
) {
	if detail == nil {
		return
	}
	t := themeOf(c)
	curVar, hasCurVar := findIconVariantInfo(detail, *variant)
	if hasCurVar && (curVar.HasStd || curVar.HasHR1) {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Text(c, a.t("icons.insp_resolution_label")).FontSize(t.text.xs).FontWeight(600).TextColor(t.subdued)
			ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
				if curVar.HasStd {
					if kitToggleChip(c, keyPrefix+"-res-std", a.t("icons.insp_res_std"), "", !*hr1, nil) {
						*hr1 = false
						if onChanged != nil {
							onChanged()
						}
					}
				}
				if curVar.HasHR1 {
					if kitToggleChip(c, keyPrefix+"-res-hr1", a.t("icons.insp_res_hr1"), "", *hr1, nil) {
						*hr1 = true
						if onChanged != nil {
							onChanged()
						}
					}
				}
			})
		})
	}

	if len(detail.Variants) > 0 {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Text(c, a.t("icons.insp_variants_label")).FontSize(t.text.xs).FontWeight(600).TextColor(t.subdued)
			ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
				for _, v := range detail.Variants {
					active := v.Variant == *variant
					if kitToggleChip(c, keyPrefix+"-var-"+v.Variant, a.iconVariantLabel(v.Variant), "", active, nil) {
						*variant = v.Variant
						if *hr1 && !v.HasHR1 {
							*hr1 = false
						} else if !*hr1 && !v.HasStd && v.HasHR1 {
							*hr1 = true
						}
						if onChanged != nil {
							onChanged()
						}
					}
				}
			})
		})
	}
}

func (a *App) openIconInspectorLightbox() {
	if a.selectedIconDetail == nil || a.selectedIconID < 0 {
		return
	}
	a.iconLightboxID = a.selectedIconID
	a.iconLightboxVariant = a.iconInspectorVariant
	a.iconLightboxHR1 = a.iconInspectorHR1
	a.iconLightboxTitle = "#" + a.selectedIconDetail.Code
	a.iconLightboxSubtitle = ""
	a.iconLightboxBlobKey = ""
	a.iconLightboxThumbPNG = nil
	a.iconLightboxOpen = true
}

func (a *App) openIconBlobLightbox(title, subtitle, blobKey string, thumbPNG []byte) {
	a.iconLightboxID = -1
	a.iconLightboxVariant = ""
	a.iconLightboxHR1 = false
	a.iconLightboxTitle = title
	a.iconLightboxSubtitle = subtitle
	a.iconLightboxBlobKey = blobKey
	a.iconLightboxThumbPNG = thumbPNG
	a.iconLightboxOpen = true
}

func (a *App) ensureIconLightboxPreviewLoaded(iconID int, variant string, hr1 bool) *cachedIconPreview {
	if a.service == nil || iconID < 0 {
		return nil
	}
	if a.iconPreviewCache == nil {
		a.iconPreviewCache = make(map[string]*cachedIconPreview)
	}
	key := fmt.Sprintf("%s|%d|%s|%v", a.Language(), iconID, variant, hr1)
	if cur, ok := a.iconPreviewCache[key]; ok && cur != nil {
		return cur
	}
	svc := a.service
	decodePreview := func() *cachedIconPreview {
		raw, hdr, err := svc.ReadIconVariantRaw(iconID, variant, hr1)
		if err != nil {
			return &cachedIconPreview{hdr: hdr, err: err.Error()}
		}
		if len(raw) == 0 {
			return &cachedIconPreview{hdr: hdr}
		}
		img, decErr := sqpack.DecodeTexToRGBA(raw)
		if decErr != nil {
			return &cachedIconPreview{hdr: hdr, rawSize: len(raw), err: decErr.Error()}
		}
		return &cachedIconPreview{
			bmp:     ui.NewBitmap(img),
			hdr:     hdr,
			rawSize: len(raw),
		}
	}
	if a.win == nil {
		res := decodePreview()
		a.iconPreviewCache[key] = res
		return res
	}
	placeholder := &cachedIconPreview{loading: true}
	a.iconPreviewCache[key] = placeholder
	go func() {
		res := decodePreview()
		a.dispatchUI(func() {
			if a.service != svc {
				return
			}
			a.iconPreviewCache[key] = res
		})
	}()
	return placeholder
}

// renderIconLightboxModal 绘制图标大图预览弹窗（由图标列表双击或详情预览点击触发，亦兼容版本对比已移除图标预览）。
func (a *App) renderIconLightboxModal(c *ui.Context) {
	if !a.iconLightboxOpen {
		return
	}
	t := themeOf(c)

	var detail *gamedata.IconEntry
	if a.iconLightboxID >= 0 && a.service != nil {
		detail = a.service.GetIconDetail(a.iconLightboxID)
	}

	var prev *cachedIconPreview
	var blobBmp *ui.Bitmap
	subtitle := a.iconLightboxSubtitle
	if detail != nil {
		prev = a.ensureIconLightboxPreviewLoaded(detail.ID, a.iconLightboxVariant, a.iconLightboxHR1)
		subtitle = sqpack.IconExportRelPath(detail.ID, a.iconLightboxVariant, a.iconLightboxHR1)
	} else if a.iconLightboxBlobKey != "" || len(a.iconLightboxThumbPNG) > 0 {
		blobBmp = a.loadDiffIconBitmap(a.iconLightboxBlobKey, a.iconLightboxThumbPNG)
	}

	ui.DialogBase(c.Key("icon-lightbox-modal"), &a.iconLightboxOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(t.scrim)
		applyOverlaySurface(c, panel.Width(580).Radius(t.radius.x2l).Clip(), shadowDialog)

		// 标题栏
		ui.Row(c).FillWidth().Padding(16, 20, 12, 20).Justify(ui.SpaceBetween).AlignItems(ui.Start).Gap(12).Children(func() {
			ui.Column(c).Gap(3).Grow(1).Shrink(1).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
					numText(c, a.iconLightboxTitle).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground)
					if detail != nil {
						if a.iconLightboxHR1 {
							drawPill(c, "HD _hr1", pillAccent)
						} else {
							drawPill(c, "PNG", pillNeutral)
						}
					}
					if prev != nil && prev.hdr.Width > 0 && prev.hdr.Height > 0 {
						numTextf(c, "%d × %d · %s", prev.hdr.Width, prev.hdr.Height, sqpack.TexFormatName(prev.hdr.Format)).
							FontSize(t.text.xs).TextColor(t.muted)
					}
				})
				if subtitle != "" {
					ui.Text(c, subtitle).FontSize(t.text.xs).TextColor(t.muted).Selectable()
				}
			})
			if kitIconButton(c, "icon-lb-close-top", icoClose, a.t("common.close"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
				a.iconLightboxOpen = false
			}
		})

		// 大图展示区
		ui.Box(c).FillWidth().Padding(16, 20).Background(t.card).Center().Children(func() {
			ui.Box(c).FillWidth().Height(260).Radius(t.radius.lg).Background(t.background).
				Border(hairline, t.border).Center().Padding(16).Clip().Children(func() {
				switch {
				case prev != nil && prev.bmp != nil:
					ui.Image(c, prev.bmp).Size(440, 228).Fit(ui.Contain)
				case blobBmp != nil:
					ui.Image(c, blobBmp).Size(440, 228).Fit(ui.Contain)
				case prev != nil && prev.err != "":
					ui.Text(c, a.tf("icons.insp_preview_err", "err", prev.err)).FontSize(t.text.sm).TextColor(t.dangerText)
				case prev != nil && prev.loading:
					ui.Text(c, a.t("icons.insp_preview_loading")).FontSize(t.text.sm).TextColor(t.muted)
				default:
					ui.Icon(c, icoImage).Size(48, 48).TextColor(t.muted)
				}
			})
		})

		// 快捷切换当前图标的清晰度（标准 / _hr1）与资源目录变体（默认 / hq / chs / en / ja 等）
		if detail != nil {
			ui.Column(c).FillWidth().Padding(12, 20).Gap(10).Background(t.background).
				BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
				a.renderIconVariantAndResolutionControls(c, "icon-lb", detail, &a.iconLightboxVariant, &a.iconLightboxHR1, func() {
					if a.selectedIconID == detail.ID {
						a.iconInspectorVariant = a.iconLightboxVariant
						a.iconInspectorHR1 = a.iconLightboxHR1
					}
				})
			})
		}

		// 底部按钮
		ui.Row(c).FillWidth().Padding(12, 20).Background(t.sidebar).
			BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).
			Justify(ui.End).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
			if detail != nil {
				if kitButton(c, "icon-lb-save", a.t("icons.export_single_btn"), btnOpts{
					variant: btnOutline,
					size:    btnSizeSm,
					icon:    icoDownload,
				}) {
					a.exportSingleIconPNG(detail.ID, a.iconLightboxVariant, a.iconLightboxHR1)
				}
			}
			if kitButton(c, "icon-lb-close-btn", a.t("common.close"), btnOpts{variant: btnDefault, size: btnSizeSm}) {
				a.iconLightboxOpen = false
			}
		})
	})
}
