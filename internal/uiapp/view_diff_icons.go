package uiapp

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/egoist/mygo/ui"
)

func (a *App) loadDiffIconBitmap(blobKey string, thumbPNG []byte) *ui.Bitmap {
	if a.diffIconCache == nil {
		a.diffIconCache = make(map[string]*ui.Bitmap)
	}
	var cacheKey string
	switch {
	case blobKey != "":
		cacheKey = "blob:" + blobKey
	case len(thumbPNG) > 0:
		sum := md5.Sum(thumbPNG)
		cacheKey = "png:" + hex.EncodeToString(sum[:8])
	default:
		return nil
	}
	if bmp, ok := a.diffIconCache[cacheKey]; ok {
		return bmp
	}
	if len(a.diffIconCache) > 800 {
		clear(a.diffIconCache)
	}

	var bmp *ui.Bitmap
	if len(thumbPNG) > 0 {
		if rgba, err := sqpack.DecodeIconBlobToRGBA(thumbPNG); err == nil && rgba != nil {
			bmp = ui.NewBitmap(rgba)
		}
	} else if blobKey != "" && a.backupSvc != nil {
		if rgba, err := a.backupSvc.ReadIconBlobImage(blobKey); err == nil && rgba != nil {
			bmp = ui.NewBitmap(rgba)
		}
	}
	a.diffIconCache[cacheKey] = bmp
	return bmp
}

func (a *App) loadDiffNewIconBitmap(entry backup.DiffItemEntry) *ui.Bitmap {
	if bmp := a.loadDiffIconBitmap(entry.NewBlobKey, entry.NewThumbPNG); bmp != nil {
		return bmp
	}
	if a.service != nil && entry.IconID >= 0 {
		if detail := a.service.GetIconDetail(entry.IconID); detail != nil {
			if bmp, ok := a.iconTileCache.Get(a.service, a.win, string(a.Language()), detail.ID, detail.DefaultVariant, detail.DefaultHR1); ok && bmp != nil {
				return bmp
			}
		}
	}
	return nil
}

// renderIconDiffPartition 以独立分区平铺展示图标新增/移除及新旧对比卡片（不复用逐行文字表格）。
func (a *App) renderIconDiffPartition(c *ui.Context) {
	t := themeOf(c)
	sum := a.diffRes.Summary

	var modified, added, removed []backup.DiffItemEntry
	for _, entry := range a.diffRes.Items {
		switch entry.ChangeType {
		case "modified":
			modified = append(modified, entry)
		case "added":
			added = append(added, entry)
		case "removed":
			removed = append(removed, entry)
		}
	}

	ui.Scroll(c.Key("diff-icon-scroll")).TrackScroll(&a.diffScroll).Grow(1).Shrink(1).Children(func() {
		ui.Column(c).FillWidth().Padding(14, 16).Gap(16).Children(func() {
			// 分区概览统计横幅
			kitBlock(c, ui.Row(c).FillWidth().Padding(10, 14).Gap(16).AlignItems(ui.Center).Wrap(), toneCard).Children(func() {
				ui.Text(c, a.t("diff.icon_partition_note")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
				drawCompactDiffStat(c, a.t("diff.change_modified"), formatInt(sum.ModifiedIcons), t.warningText)
				drawCompactDiffStat(c, a.t("diff.change_added"), "+"+formatInt(sum.AddedIcons), t.successText)
				drawCompactDiffStat(c, a.t("diff.change_removed"), "-"+formatInt(sum.RemovedIcons), t.dangerText)
			})

			// 1. 变更图标：同时展示旧图与新图
			if len(modified) > 0 {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
						ui.Text(c, a.t("diff.icon_section_modified")).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
						numText(c, formatInt(len(modified))).FontSize(t.text.xs).TextColor(t.muted)
					})
					ui.Row(c).FillWidth().Gap(10).Wrap().Children(func() {
						for _, entry := range modified {
							a.renderModifiedIconDiffCard(c, entry)
						}
					})
				})
			}

			// 2. 新增图标：直接平铺网格
			if len(added) > 0 {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
						ui.Text(c, a.t("diff.icon_section_added")).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
						numText(c, formatInt(len(added))).FontSize(t.text.xs).TextColor(t.muted)
					})
					ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
						for _, entry := range added {
							a.renderTiledIconDiffCard(c, entry, true)
						}
					})
				})
			}

			// 3. 移除图标：直接平铺网格（展示从历史备份还原的旧图）
			if len(removed) > 0 {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
						ui.Text(c, a.t("diff.icon_section_removed")).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
						numText(c, formatInt(len(removed))).FontSize(t.text.xs).TextColor(t.muted)
					})
					ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
						for _, entry := range removed {
							a.renderTiledIconDiffCard(c, entry, false)
						}
					})
				})
			}
		})
	})
}

func (a *App) renderModifiedIconDiffCard(c *ui.Context, entry backup.DiffItemEntry) {
	t := themeOf(c)
	code := fmt.Sprintf("%06d", entry.IconID)
	card := ui.ButtonBase(c.Key(fmt.Sprintf("df-ic-mod-%d", entry.ID))).
		Label("#"+code).Width(212).Padding(10).Radius(t.radius.lg).
		Background(t.card).Border(hairline, t.border).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
	if card.Hovered() {
		card.Background(t.accentSoft).Border(hairline, t.accentBorder)
	}

	oldBmp := a.loadDiffIconBitmap(entry.OldBlobKey, entry.OldThumbPNG)
	newBmp := a.loadDiffNewIconBitmap(entry)

	card.Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
				numTextf(c, "#%s", code).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).SingleLine()
				drawPill(c, entry.IconVariantSummary, pillWarning)
			})

			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).Children(func() {
				// 旧图标框
				ui.Column(c).Gap(3).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(52, 52).Radius(t.radius.md).Background(t.delBg).
						Border(hairline, t.dangerBorder).Center().Clip().Children(func() {
						if oldBmp != nil {
							ui.Image(c, oldBmp).Size(44, 44).Fit(ui.Contain)
						} else {
							ui.Icon(c, icoImage).Size(18, 18).TextColor(t.muted)
						}
					})
					ui.Text(c, a.t("diff.icon_prev_label")).FontSize(t.text.micro).TextColor(t.delText)
				})

				ui.Icon(c, icoArrowRight).Size(14, 14).TextColor(t.muted)

				// 新图标框
				ui.Column(c).Gap(3).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(52, 52).Radius(t.radius.md).Background(t.insBg).
						Border(hairline, t.successBorder).Center().Clip().Children(func() {
						if newBmp != nil {
							ui.Image(c, newBmp).Size(44, 44).Fit(ui.Contain)
						} else {
							ui.Icon(c, icoImage).Size(18, 18).TextColor(t.muted)
						}
					})
					ui.Text(c, a.t("diff.icon_new_label")).FontSize(t.text.micro).FontWeight(500).TextColor(t.insText)
				})
			})
		})
	})

	if card.Clicked() {
		a.inspectIconFromOtherTab(entry.ID)
	}
}

func (a *App) renderTiledIconDiffCard(c *ui.Context, entry backup.DiffItemEntry, isAdded bool) {
	t := themeOf(c)
	code := fmt.Sprintf("%06d", entry.IconID)
	keyPrefix := "df-ic-rem-"
	borderCol := t.dangerBorder
	tagTxt := "-" + a.t("diff.change_removed")
	tagCol := t.dangerText
	var bmp *ui.Bitmap
	if isAdded {
		keyPrefix = "df-ic-add-"
		borderCol = t.successBorder
		tagTxt = "+" + a.t("diff.change_added")
		tagCol = t.successText
		bmp = a.loadDiffNewIconBitmap(entry)
	} else {
		bmp = a.loadDiffIconBitmap(entry.OldBlobKey, entry.OldThumbPNG)
	}

	tile := ui.ButtonBase(c.Key(fmt.Sprintf("%s%d", keyPrefix, entry.ID))).
		Label("#"+code).Width(86).Padding(8, 6).Radius(t.radius.md).
		Background(t.card).Border(hairline, borderCol).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
	if tile.Hovered() {
		tile.Background(t.accentSoft).Border(hairline, t.accentBorder)
	}

	tile.Children(func() {
		ui.Column(c).FillWidth().Gap(3).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(48, 48).Radius(t.radius.sm).Background(t.background).
				Border(hairline, t.border).Center().Clip().Children(func() {
				if bmp != nil {
					ui.Image(c, bmp).Size(42, 42).Fit(ui.Contain)
				} else {
					ui.Icon(c, icoImage).Size(18, 18).TextColor(t.muted)
				}
			})
			numText(c, code).FontSize(t.text.micro).FontWeight(600).TextColor(t.foreground).SingleLine()
			ui.Text(c, tagTxt).FontSize(t.text.micro).TextColor(tagCol).SingleLine()
		})
	})

	if tile.Clicked() {
		if isAdded {
			a.inspectIconFromOtherTab(entry.ID)
		} else {
			a.openIconBlobLightbox("#"+code, a.t("diff.change_removed")+" · "+entry.CategoryName, entry.OldBlobKey, entry.OldThumbPNG)
		}
	}
}
