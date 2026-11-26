package uiapp

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	diffTextColSourceW  float32 = 212
	diffTextColTypeW    float32 = 76
	diffTextColActionsW float32 = 76
	diffTextColDiffMin  float32 = 240
	diffTextSnipLimit   int     = 220
)

// renderTextDiffPartition 绘制「版本对比 → 文本」独立分区：仅呈现变更与移除记录（不含新增），
// 支持旧/新文本字符级差异高亮、长文本折叠展开、一键复制与关联实体详情直跳。
func (a *App) renderTextDiffPartition(c *ui.Context) {
	t := themeOf(c)
	if a.diffRes == nil {
		kitEmpty(c, icoTextSearch, a.t("diff.empty_title"), a.t("diff.empty_desc"), nil)
		return
	}
	sum := a.diffRes.Summary

	if !sum.TextDiffAvailable {
		kitEmpty(c, icoTextSearch, a.t("diff.text_missing_title"), a.t("diff.text_missing_desc"), func() {
			if kitButton(c, "df-txt-open-mgr", a.t("diff.open_manager_btn"), btnOpts{variant: btnOutline, size: btnSizeSm, icon: icoDownload}) {
				a.openSnapshotManager()
			}
		})
		return
	}

	// 分区统计说明条（明确告知仅追踪变更与移除）
	ui.Box(c).FillWidth().Padding(8, 16).BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Children(func() {
		kitBlock(c, ui.Row(c).FillWidth().Padding(8, 12).Gap(16).AlignItems(ui.Center).Wrap(), toneCard).Children(func() {
			ui.Text(c, a.t("diff.text_partition_note")).FontSize(t.text.xs).FontWeight(500).TextColor(t.subdued)
			drawCompactDiffStat(c, a.t("diff.change_modified"), formatInt(sum.ModifiedTexts), t.warningText)
			drawCompactDiffStat(c, a.t("diff.change_removed"), "-"+formatInt(sum.RemovedTexts), t.dangerText)
		})
	})

	// 表头
	ui.Row(c).FillWidth().Padding(8, 16).Background(t.card).
		BorderWidth(0, 0, hairline, 0).BorderColor(t.border).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.t("diff.text_col_source")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffTextColSourceW).Shrink(0)
		ui.Text(c, a.t("diff.text_col_type")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffTextColTypeW).Shrink(0).TextAlign(ui.Center)
		ui.Text(c, a.t("diff.text_col_diff")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Grow(1).Shrink(1).MinWidth(diffTextColDiffMin)
		ui.Text(c, a.t("text_search.col_actions")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Width(diffTextColActionsW).Shrink(0).TextAlign(ui.End)
	})

	if len(a.diffRes.Items) == 0 {
		kitEmpty(c, icoTextSearch, a.t("diff.empty_title"), a.t("diff.empty_desc"), nil)
		return
	}

	now := time.Now()
	ui.Scroll(c.Key("diff-text-scroll")).TrackScroll(&a.diffScroll).Grow(1).Shrink(1).Children(func() {
		ui.Column(c).FillWidth().Selectable().Children(func() {
			for i, entry := range a.diffRes.Items {
				if i > 0 {
					kitSeparator(c, false).Margin(0, 16)
				}
				entryKey := fmt.Sprintf("%s#%d.%d:%d", entry.TextSheet, entry.ID, entry.TextSubRow, entry.TextColIdx)
				row := ui.Row(c.Key("df-txt-row-"+entryKey)).FillWidth().Padding(10, 16).Gap(10).
					AlignItems(ui.Start).Transition(colorTransition(motionFast))
				if row.Hovered() {
					row.Background(t.tint(0.85))
				}
				row.Children(func() {
					// 1. 来源表 / 行号 / 列名
					ui.Column(c).Width(diffTextColSourceW).Shrink(0).Gap(3).Children(func() {
						ui.Text(c, entry.TextSheet).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).SingleLine()
						ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
							numText(c, gamedata.FormatTextRowKey(entry.ID, entry.TextSubRow)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
							if entry.TextColName != "" {
								drawPill(c, entry.TextColName, pillOutline)
							}
						})
					})

					// 2. 变更 / 移除徽章
					ui.Row(c).Width(diffTextColTypeW).Shrink(0).Center().Padding(2, 0, 0, 0).Children(func() {
						if entry.ChangeType == "modified" {
							drawPill(c, a.t("diff.change_modified"), pillWarning)
						} else {
							drawPill(c, a.t("diff.change_removed"), pillNegative)
						}
					})

					// 3. 文本变动对比详情（支持行内差异高亮与长文本展开）
					expanded := a.diffTextExpanded[entryKey]
					ui.Column(c).Grow(1).Shrink(1).MinWidth(diffTextColDiffMin).Gap(6).Children(func() {
						truncated := false
						if entry.ChangeType == "modified" {
							bStart, bEnd, aStart, aEnd := backup.ComputeInlineTextDiffBounds(entry.TextBefore, entry.TextAfter)
							var bSpans, aSpans []gamedata.TextMatchSpan
							if bEnd > bStart {
								bSpans = []gamedata.TextMatchSpan{{Start: bStart, End: bEnd}}
							}
							if aEnd > aStart {
								aSpans = []gamedata.TextMatchSpan{{Start: aStart, End: aEnd}}
							}

							dispB, dispBSpans, truncB := sliceTextSnippet(entry.TextBefore, bSpans, diffTextSnipLimit, expanded)
							dispA, dispASpans, truncA := sliceTextSnippet(entry.TextAfter, aSpans, diffTextSnipLimit, expanded)
							truncated = truncB || truncA

							renderTextDiffBlock(c, a.t("diff.text_before_label"), dispB, dispBSpans, true, t)
							renderTextDiffBlock(c, a.t("diff.text_after_label"), dispA, dispASpans, false, t)
						} else {
							dispB, _, truncB := sliceTextSnippet(entry.TextBefore, nil, diffTextSnipLimit, expanded)
							truncated = truncB
							renderTextDiffBlock(c, a.t("diff.text_removed_label"), dispB, nil, true, t)
						}

						if truncated || expanded {
							lbl := a.t("text_search.expand_text")
							if expanded {
								lbl = a.t("text_search.collapse_text")
							}
							ui.Row(c).Justify(ui.Start).Children(func() {
								if kitButton(c, "df-txt-exp-"+entryKey, lbl, btnOpts{variant: btnGhost, size: btnSizeXs}) {
									a.diffTextExpanded[entryKey] = !expanded
								}
							})
						}
					})

					// 4. 操作列：复制与直跳关联实体详情
					ui.Row(c).Width(diffTextColActionsW).Shrink(0).Justify(ui.End).AlignItems(ui.Center).Gap(4).Children(func() {
						copied := a.diffTextCopiedKey == entryKey && now.Before(a.diffTextCopiedEnd)
						copyTip := a.t("text_search.copy_text")
						copyIco := icoCopy
						if copied {
							copyTip = a.t("text_search.copied")
							copyIco = icoCheck
						}
						if kitIconButton(c, "df-txt-cp-"+entryKey, copyIco, copyTip, btnOpts{variant: btnGhost, size: btnSizeSm}) {
							copyContent := entry.TextAfter
							if entry.ChangeType == "removed" || copyContent == "" {
								copyContent = entry.TextBefore
							}
							if a.win != nil {
								mygo.Clipboard.WriteText(copyContent)
							}
							a.diffTextCopiedKey = entryKey
							a.diffTextCopiedEnd = time.Now().Add(1500 * time.Millisecond)
						}

						if entry.TextJumpKind != "" && entry.TextJumpID > 0 {
							jumpTip := a.textSearchJumpTooltip(entry.TextJumpKind)
							if kitIconButton(c, "df-txt-jmp-"+entryKey, icoExternalLink, jumpTip, btnOpts{variant: btnGhost, size: btnSizeSm}) {
								a.inspectTextSearchHit(gamedata.TextSearchHit{
									JumpKind: entry.TextJumpKind,
									JumpID:   entry.TextJumpID,
								})
							}
						}
					})
				})
			}
		})
	})
}

func renderTextDiffBlock(c *ui.Context, label, text string, diffSpans []gamedata.TextMatchSpan, isDeletion bool, t uiTokens) {
	boxBg := t.insBg.Alpha(0.45)
	badgeKind := pillPositive
	hlBg := t.insBg
	hlFg := t.insText
	if isDeletion {
		boxBg = t.delBg.Alpha(0.60)
		badgeKind = pillNegative
		hlBg = t.delBg
		hlFg = t.delText
	}

	ui.Row(c).FillWidth().Padding(6, 9).Radius(t.radius.md).Background(boxBg).Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Padding(1, 0, 0, 0).Shrink(0).Children(func() {
			drawPill(c, label, badgeKind)
		})
		ui.Box(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			if len(diffSpans) == 0 || !utf8.ValidString(text) {
				txtEl := ui.Text(c, text).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.foreground).Selectable().FillWidth()
				if isDeletion {
					txtEl.TextColor(t.subdued)
				}
				return
			}
			uiSpans := make([]ui.Span, 0, len(diffSpans)*2+1)
			cursor := 0
			for _, sp := range diffSpans {
				if sp.Start < cursor || sp.End <= sp.Start || sp.End > len(text) {
					continue
				}
				if sp.Start > cursor {
					uiSpans = append(uiSpans, ui.Span{Text: text[cursor:sp.Start]})
				}
				uiSpans = append(uiSpans, ui.Span{
					Text:       text[sp.Start:sp.End],
					Weight:     600,
					Color:      hlFg,
					Background: hlBg,
					Radius:     t.radius.xs,
				})
				cursor = sp.End
			}
			if cursor < len(text) {
				uiSpans = append(uiSpans, ui.Span{Text: text[cursor:]})
			}
			ui.RichText(c, uiSpans...).FontSize(t.text.xs).FontWeight(400).TextColor(t.foreground).
				LineHeight(lineProse).Selectable().FillWidth()
		})
	})
}
