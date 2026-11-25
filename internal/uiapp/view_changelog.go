package uiapp

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/changelog"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type changelogCategoryFilter struct {
	key  string
	kind changelog.CategoryKind
}

var changelogCategoryDefs = []changelogCategoryFilter{
	{key: "added", kind: changelog.CategoryAdded},
	{key: "changed", kind: changelog.CategoryChanged},
	{key: "fixed", kind: changelog.CategoryFixed},
	{key: "engineering", kind: changelog.CategoryEngineering},
}

func (a *App) resolveChangelogFS() fs.FS {
	if a.changelogFS != nil {
		return a.changelogFS
	}
	// 单元测试场景下未通过 main 注入时，尝试从仓库根目录只读加载。
	for _, cand := range []string{".", filepath.Join("..", "..")} {
		if _, err := os.Stat(filepath.Join(cand, "CHANGELOG.md")); err == nil {
			return os.DirFS(cand)
		}
	}
	return nil
}

func (a *App) loadCurrentChangelog() (changelog.Document, bool) {
	lang := a.Language()
	if a.changelogDocs == nil {
		a.changelogDocs = make(map[i18n.Lang]changelog.Document)
	}
	if doc, ok := a.changelogDocs[lang]; ok {
		return doc, true
	}
	fsys := a.resolveChangelogFS()
	if fsys == nil {
		a.changelogErr = "changelog source not configured"
		return changelog.Document{}, false
	}
	doc, err := changelog.LoadForLang(fsys, lang)
	if err != nil {
		a.changelogErr = err.Error()
		return changelog.Document{}, false
	}
	a.changelogErr = ""
	a.changelogDocs[lang] = doc
	return doc, true
}

func (a *App) hasChangelogFilter() bool {
	return strings.TrimSpace(a.changelogSearch) != "" ||
		(a.changelogCatFilter != "" && a.changelogCatFilter != "all") ||
		a.changelogVerFilter != ""
}

func (a *App) resetChangelogFilters() {
	a.changelogSearch = ""
	a.changelogCatFilter = "all"
	a.changelogVerFilter = ""
	a.changelogScroll.Y = 0
}

func (a *App) changelogCategoryLabel(kind changelog.CategoryKind) string {
	switch kind {
	case changelog.CategoryAdded:
		return a.t("changelog.cat_added")
	case changelog.CategoryChanged:
		return a.t("changelog.cat_changed")
	case changelog.CategoryFixed:
		return a.t("changelog.cat_fixed")
	case changelog.CategoryEngineering:
		return a.t("changelog.cat_engineering")
	default:
		return a.t("common.other")
	}
}

func changelogCategoryPillKind(kind changelog.CategoryKind) pillKind {
	switch kind {
	case changelog.CategoryAdded:
		return pillPositive
	case changelog.CategoryChanged:
		return pillAccent
	case changelog.CategoryFixed:
		return pillWarning
	case changelog.CategoryEngineering:
		return pillNeutral
	case changelog.CategoryRemoved:
		return pillNegative
	default:
		return pillOutline
	}
}

func changelogCategoryDotColor(t uiTokens, kind changelog.CategoryKind) ui.Color {
	switch kind {
	case changelog.CategoryAdded:
		return t.success
	case changelog.CategoryChanged:
		return t.primary
	case changelog.CategoryFixed:
		return t.attention
	case changelog.CategoryRemoved:
		return t.danger
	default:
		return t.muted
	}
}

func filterChangelogReleases(releases []changelog.Release, verFilter, catFilter, query string) []changelog.Release {
	q := strings.ToLower(strings.TrimSpace(query))
	var wantKind changelog.CategoryKind
	hasCatFilter := false
	for _, def := range changelogCategoryDefs {
		if def.key == catFilter {
			wantKind = def.kind
			hasCatFilter = true
			break
		}
	}

	out := make([]changelog.Release, 0, len(releases))
	for _, rel := range releases {
		if verFilter != "" && rel.Version != verFilter {
			continue
		}
		verMatched := q != "" && (strings.Contains(strings.ToLower(rel.Version), q) ||
			strings.Contains(strings.ToLower(rel.Date), q) ||
			strings.Contains(strings.ToLower(rel.RawHeading), q))

		var filteredSecs []changelog.Section
		total := 0
		for _, sec := range rel.Sections {
			if hasCatFilter && sec.Kind != wantKind {
				continue
			}
			var items []changelog.ChangeItem
			if q == "" || verMatched {
				items = sec.Items
			} else {
				for _, it := range sec.Items {
					if strings.Contains(strings.ToLower(it.PlainText), q) ||
						strings.Contains(strings.ToLower(sec.Heading), q) {
						items = append(items, it)
					}
				}
			}
			if len(items) == 0 {
				continue
			}
			filteredSecs = append(filteredSecs, changelog.Section{
				Kind:    sec.Kind,
				Heading: sec.Heading,
				Items:   items,
			})
			total += len(items)
		}
		if len(filteredSecs) == 0 {
			continue
		}
		rel.Sections = filteredSecs
		rel.TotalItems = total
		out = append(out, rel)
	}
	return out
}

func (a *App) renderChangelogView(c *ui.Context) {
	t := themeOf(c)
	doc, ok := a.loadCurrentChangelog()
	releases := doc.NonEmptyReleases()

	if a.changelogCatFilter == "" {
		a.changelogCatFilter = "all"
	}

	// 计算总条目数与各分类总数（不受分类筛选影响，用于筛选芯片角标）
	totalEntries := 0
	catCounts := make(map[changelog.CategoryKind]int, 4)
	latestReleasedVer := ""
	for _, rel := range releases {
		if !rel.IsUnreleased && latestReleasedVer == "" {
			latestReleasedVer = rel.Version
		}
		totalEntries += rel.TotalItems
		for _, sec := range rel.Sections {
			catCounts[sec.Kind] += len(sec.Items)
		}
	}

	filtered := filterChangelogReleases(releases, a.changelogVerFilter, a.changelogCatFilter, a.changelogSearch)

	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		// 1. 统一页头：标题 + 当前应用版本 + 版本选择 + 搜索框 + 分类筛选
		ui.Column(c).FillWidth().Padding(12, 16, 10, 16).Gap(8).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
					ui.Text(c, a.t("changelog.title")).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
					if cleanVer := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(a.appVersion), "v"), "V"); cleanVer != "" {
						drawPill(c, a.tf("changelog.current_ver_badge", "version", cleanVer), pillAccent)
					}
					if ok {
						numText(c, a.tf("changelog.summary",
							"releases", formatInt(len(releases)),
							"items", formatInt(totalEntries),
						)).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
					}
				})

				ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
					if a.hasChangelogFilter() {
						if kitButton(c, "cl-reset", a.t("common.reset"), btnOpts{variant: btnGhost, size: btnSizeSm}) {
							a.resetChangelogFilters()
						}
					}

					verOpts := []selectOption[string]{{value: "", label: a.t("changelog.all_versions")}}
					for _, rel := range releases {
						lbl := "v" + rel.Version
						if rel.IsUnreleased {
							lbl = a.t("changelog.unreleased_title")
						} else if rel.Date != "" {
							lbl += " (" + rel.Date + ")"
						}
						verOpts = append(verOpts, selectOption[string]{value: rel.Version, label: lbl})
					}
					if kitSelect(c, "cl-ver-select", &a.changelogVerFilter, verOpts, 186, 236, true) {
						a.changelogScroll.Y = 0
					}
				})
			})

			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
				ui.Box(c).Grow(1).MinWidth(200).MaxWidth(360).Children(func() {
					a.renderPageSearchInput(c, "changelog-page-search", &a.changelogSearch, a.t("changelog.search_placeholder"), a.t("changelog.search_label"), func() {
						a.changelogScroll.Y = 0
					})
				})

				ui.Row(c).Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
					if kitToggleChip(c, "cl-cat-all", a.t("changelog.cat_all"), formatInt(totalEntries), a.changelogCatFilter == "all", nil) {
						a.changelogCatFilter = "all"
						a.changelogScroll.Y = 0
					}
					for _, def := range changelogCategoryDefs {
						cnt := catCounts[def.kind]
						if cnt == 0 && a.changelogCatFilter != def.key {
							continue
						}
						if kitToggleChip(c, "cl-cat-"+def.key, a.changelogCategoryLabel(def.kind), formatInt(cnt), a.changelogCatFilter == def.key, nil) {
							a.changelogCatFilter = def.key
							a.changelogScroll.Y = 0
						}
					}
				})
			})
		})

		// 2. 主内容区
		if !ok {
			kitEmpty(c, icoChangelog, a.t("changelog.load_error_title"), a.changelogErr, nil)
			return
		}
		if len(filtered) == 0 {
			kitEmpty(c, icoChangelog, a.t("changelog.empty_title"), a.t("changelog.empty_desc"), func() {
				if a.hasChangelogFilter() {
					if kitButton(c, "cl-empty-reset", a.t("common.reset_filters"), btnOpts{variant: btnOutline, size: btnSizeSm}) {
						a.resetChangelogFilters()
					}
				}
			})
			return
		}

		cleanAppVer := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(a.appVersion), "v"), "V")

		ui.Scroll(c.Key("changelog-scroll")).TrackScroll(&a.changelogScroll).Grow(1).Shrink(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(20, 24, 32, 24).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(880).Gap(16).Children(func() {
					if !a.hasChangelogFilter() && len(doc.IntroSpans) > 0 {
						kitBlock(c, ui.Row(c).FillWidth().Padding(10, 14).Gap(10).AlignItems(ui.Center), toneCard).Children(func() {
							ui.Icon(c, icoInfo).Size(iconSm, iconSm).Shrink(0).TextColor(t.primary)
							ui.Box(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
								renderChangelogRichText(c, doc.IntroSpans, t.text.xs, 400, t.subdued, lineProse, t)
							})
						})
					}

					for _, rel := range filtered {
						a.renderChangelogReleaseCard(c, t, rel, cleanAppVer, latestReleasedVer)
					}
				})
			})
		})
	})
}

func (a *App) renderChangelogReleaseCard(c *ui.Context, t uiTokens, rel changelog.Release, cleanAppVer, latestReleasedVer string) {
	collapsed := a.changelogCollapsed[rel.Version]
	card := ui.Column(c.Key("cl-rel-"+rel.Version)).FillWidth().Radius(t.radius.xl).
		Background(t.background).Border(hairline, t.border).Clip()

	card.Children(func() {
		// 版本卡头栏
		hdr := ui.Row(c).FillWidth().Padding(13, 18).Gap(12).Background(t.card).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap()
		if !collapsed {
			hdr.BorderWidth(0, 0, hairline, 0).BorderColor(t.border)
		}
		hdr.Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Grow(1).Shrink(1).Children(func() {
				if rel.IsUnreleased {
					ui.Text(c, a.t("changelog.unreleased_title")).FontSize(t.text.xl).FontWeight(600).
						LetterSpacing(-0.3).TextColor(t.foreground).SingleLine()
					drawPill(c, a.t("changelog.badge_unreleased"), pillWarning)
				} else {
					numText(c, "v"+rel.Version).FontSize(t.text.xl).FontWeight(600).
						LetterSpacing(-0.3).TextColor(t.foreground).SingleLine()
					if cleanAppVer != "" && rel.Version == cleanAppVer {
						drawPill(c, a.t("changelog.badge_current"), pillPositive)
					} else if latestReleasedVer != "" && rel.Version == latestReleasedVer {
						drawPill(c, a.t("changelog.badge_latest"), pillAccent)
					}
				}
				if rel.Date != "" {
					numText(c, rel.Date).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				}
			})

			ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Shrink(0).Children(func() {
				for _, sec := range rel.Sections {
					cntStr := formatInt(len(sec.Items))
					switch sec.Kind {
					case changelog.CategoryAdded:
						drawPill(c, a.tf("changelog.short_added", "count", cntStr), pillPositive)
					case changelog.CategoryChanged:
						drawPill(c, a.tf("changelog.short_changed", "count", cntStr), pillAccent)
					case changelog.CategoryFixed:
						drawPill(c, a.tf("changelog.short_fixed", "count", cntStr), pillWarning)
					case changelog.CategoryEngineering:
						drawPill(c, a.tf("changelog.short_engineering", "count", cntStr), pillNeutral)
					}
				}

				toggleIco := icoChevronDown
				toggleTip := a.t("changelog.collapse_tip")
				if collapsed {
					toggleIco = icoChevronRight
					toggleTip = a.t("changelog.expand_tip")
				}
				if kitIconButton(c, "cl-toggle-"+rel.Version, toggleIco, toggleTip, btnOpts{variant: btnGhost, size: btnSizeSm}) {
					if a.changelogCollapsed == nil {
						a.changelogCollapsed = make(map[string]bool)
					}
					a.changelogCollapsed[rel.Version] = !collapsed
				}
			})
		})

		if collapsed {
			return
		}

		// 版本正文区
		ui.Column(c).FillWidth().Padding(16, 18, 18, 18).Gap(16).Children(func() {
			for secIdx, sec := range rel.Sections {
				if secIdx > 0 {
					kitSeparator(c, false)
				}
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							heading := sec.Heading
							if heading == "" {
								heading = a.changelogCategoryLabel(sec.Kind)
							}
							drawPill(c, heading, changelogCategoryPillKind(sec.Kind))
							numText(c, a.tf("changelog.section_count", "count", formatInt(len(sec.Items)))).
								FontSize(t.text.xs).TextColor(t.muted).SingleLine()
						})
					})

					ui.Column(c).FillWidth().Gap(12).Children(func() {
						dotCol := changelogCategoryDotColor(t, sec.Kind)
						for _, item := range sec.Items {
							renderChangelogItem(c, t, item, dotCol)
						}
					})
				})
			}
		})
	})
}

func renderChangelogItem(c *ui.Context, t uiTokens, item changelog.ChangeItem, dotCol ui.Color) {
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Start).Children(func() {
		// 左侧语义状态圆点，与首行文本中线垂直对齐
		ui.Box(c).Width(10).Height(20).Shrink(0).Center().Children(func() {
			drawStatusDot(c, dotCol)
		})

		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			hasTitle := len(item.TitleSpans) > 0
			if hasTitle {
				renderChangelogRichText(c, item.TitleSpans, t.text.sm, 600, t.foreground, lineUI, t)
			}
			if len(item.BodySpans) > 0 {
				bodyCol := t.subdued
				if !hasTitle {
					bodyCol = t.foreground
				}
				renderChangelogRichText(c, item.BodySpans, t.text.sm, 400, bodyCol, lineProse, t)
			}
			if len(item.SubItems) > 0 {
				ui.Column(c).FillWidth().Padding(4, 0, 0, 2).Gap(6).Children(func() {
					for _, sub := range item.SubItems {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
							ui.Box(c).Width(10).Height(19).Shrink(0).Center().Children(func() {
								ui.Box(c).Size(5, 5).Radius(t.radius.full).Border(strokeWidth, t.muted)
							})
							ui.Box(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
								renderChangelogRichText(c, sub.Spans, t.text.xs, 400, t.subdued, lineProse, t)
							})
						})
					}
				})
			}
		})
	})
}

func toChangelogUISpan(sp changelog.InlineSpan, fontSize float32, t uiTokens) ui.Span {
	switch sp.Kind {
	case changelog.SpanCode:
		return ui.Span{
			Text:            sp.Text,
			Size:            max(t.text.micro, fontSize-1),
			Weight:          500,
			Color:           t.foreground,
			Background:      t.tint(1.35),
			Radius:          t.radius.sm,
			PaddingX:        4,
			PaddingY:        1,
			NoLetterSpacing: true,
			Features:        "tnum",
		}
	case changelog.SpanBold:
		return ui.Span{
			Text:   sp.Text,
			Weight: 600,
			Color:  t.foreground,
		}
	default:
		return ui.Span{Text: sp.Text}
	}
}

func renderChangelogRichText(c *ui.Context, spans []changelog.InlineSpan, fontSize float32, baseWeight int, baseColor ui.Color, lh float32, t uiTokens) {
	if len(spans) == 0 {
		return
	}
	hasLink := false
	for _, sp := range spans {
		if sp.Kind == changelog.SpanLink {
			hasLink = true
			break
		}
	}
	if !hasLink {
		uiSpans := make([]ui.Span, 0, len(spans))
		for _, sp := range spans {
			uiSpans = append(uiSpans, toChangelogUISpan(sp, fontSize, t))
		}
		ui.RichText(c, uiSpans...).FontSize(fontSize).FontWeight(baseWeight).TextColor(baseColor).
			LineHeight(lh).FontFeatures("tnum").Selectable().FillWidth()
		return
	}

	rt := ui.RichText(c).FontSize(fontSize).FontWeight(baseWeight).TextColor(baseColor).
		LineHeight(lh).FontFeatures("tnum").Selectable().FillWidth()
	rt.Children(func() {
		for _, sp := range spans {
			switch sp.Kind {
			case changelog.SpanLink:
				ui.Link(c, sp.Text, sp.URL).TextColor(t.primary).Underline()
			case changelog.SpanBold:
				ui.Text(c, sp.Text).FontWeight(600).TextColor(t.foreground)
			case changelog.SpanCode:
				ui.RichText(c, toChangelogUISpan(sp, fontSize, t))
			default:
				ui.Text(c, sp.Text)
			}
		}
	})
}
