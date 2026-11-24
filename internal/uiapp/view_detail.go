package uiapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// renderItemDetailPane 渲染右侧详情阅读面板（扁平百科式排版：顶部分隔节标题 + 零嵌套卡片 + 克制语义标注）。
func (a *App) renderItemDetailPane(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Grow(1).Shrink(1).FillHeight().Children(func() {
		item := a.selectedDetail
		if item == nil {
			kitEmpty(c, icoPackage, "选择物品查看详情", "点击左侧任意物品，可查看属性、制作配方、采集点与兑换来源。", nil)
			return
		}

		isHQ := item.CanBeHQ && a.showHQ

		// 顶部工具条：最近浏览 + 复制名称 + NQ/HQ 切换
		ui.Row(c).FillWidth().Height(42).Padding(0, 16).Gap(10).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Grow(1).Shrink(1).ClipX().Children(func() {
				if len(a.history) > 1 {
					ui.Text(c, "最近").FontSize(t.text.micro).TextColor(t.muted).Shrink(0).SingleLine()
					maxHist := min(6, len(a.history))
					for _, h := range a.history[:maxHist] {
						isSel := h.id == a.selectedItemID
						chip := ui.ButtonBase(c.Key(fmt.Sprintf("hist-%d", h.id))).Label(h.nameCn).
							Height(22).Padding(0, 7).Radius(t.radius.md).Shrink(0).
							Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
						if isSel {
							chip.Background(t.tint(1.75))
						} else if chip.Hovered() {
							chip.Background(t.tint(1))
						}
						chip.Children(func() {
							fg := t.muted
							fw := 400
							if isSel {
								fg = t.foreground
								fw = 500
							}
							ui.Text(c, h.nameCn).FontSize(t.text.xs).FontWeight(fw).TextColor(fg).SingleLine()
						})
						if chip.Clicked() && !isSel {
							a.selectItem(h.id)
						}
					}
				} else {
					numTextf(c, "物品 #%d", item.ID).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
				copied := a.copiedItemID == item.ID && time.Now().Before(a.copiedUntil)
				copyLabel := "复制名称"
				copyIco := icoCopy
				if copied {
					copyLabel = "已复制"
					copyIco = icoCheck
				}
				if kitButton(c, "copy-item-name", copyLabel, btnOpts{variant: btnOutline, size: btnSizeSm, icon: copyIco}) {
					if a.win != nil {
						mygo.Clipboard.WriteText(item.NameCn)
					}
					a.copiedItemID = item.ID
					a.copiedUntil = time.Now().Add(1500 * time.Millisecond)
				}

				if item.CanBeHQ {
					selIdx := 0
					if a.showHQ {
						selIdx = 1
					}
					if nextIdx, chg := kitSegmentedTabs(c, "hq-toggle", selIdx, []string{"NQ", "HQ"}); chg {
						a.showHQ = nextIdx == 1
					}
				}
			})
		})

		// 主详情滚动阅读区
		ui.Scroll(c.Key("item-detail-scroll")).TrackScroll(&a.detailScroll).Grow(1).Shrink(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(20, 24, 32, 24).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(880).Gap(20).Children(func() {
					hasRecipes := len(item.Recipes) > 0
					hasUsedIn := len(item.UsedInRecipes) > 0
					hasGather := len(item.Gathering.Nodes) > 0 || len(item.Gathering.FishingSpots) > 0 || len(item.Gathering.Spearfishing) > 0
					hasShops := len(item.Shops) > 0
					hasRetainer := len(item.RetainerVentures) > 0
					hasColTurnIn := len(item.CollectableTurnIns) > 0
					hasColReward := len(item.CollectableRewards) > 0
					hasQuestSrc := len(item.RewardedFromQuests) > 0
					hasLeveTurnIn := len(item.TurnInForLeves) > 0
					hasLeveReward := len(item.RewardedFromLeves) > 0
					hasAch := len(item.RewardedFromAchievements) > 0

					a.renderDetailHero(c, item, isHQ, hasAch)

					if item.EquipmentStats != nil {
						a.renderEquipmentStatsSection(c, item.EquipmentStats, isHQ)
					}

					if item.EquipmentAttributes != nil {
						a.renderEquipmentAttrsSection(c, item.EquipmentAttributes)
					}

					if item.ConsumableEffect != nil {
						a.renderConsumableSection(c, item, isHQ)
					}

					if hasColTurnIn {
						a.renderCollectableTurnInsSection(c, item.NameCn, item.CollectableTurnIns)
					}

					if hasColReward {
						a.renderCollectableRewardsSection(c, item.NameCn, item.CollectableRewards)
					}

					if hasAch {
						a.renderRewardAchievementsSection(c, item.RewardedFromAchievements)
					}

					if hasQuestSrc {
						a.renderQuestSourcesSection(c, item.RewardedFromQuests)
					}

					if hasLeveTurnIn {
						a.renderLeveSourcesSection(c, "理符交纳委托", item.TurnInForLeves, true)
					}

					if hasLeveReward {
						a.renderLeveSourcesSection(c, "理符奖励掉落", item.RewardedFromLeves, false)
					}

					if hasShops {
						a.renderShopsSection(c, item.NameCn, item.Shops)
					}

					if hasRecipes {
						a.renderRecipesSection(c, item)
					}

					if hasGather {
						a.renderGatheringSections(c, item.NameCn, item.Gathering)
					}

					if hasRetainer {
						a.renderRetainerSection(c, item.RetainerVentures)
					}

					if hasUsedIn {
						a.renderUsedInRecipesSection(c, item.UsedInRecipes)
					}

					if !hasShops && !hasRecipes && !hasUsedIn && !hasGather && !hasRetainer && !hasAch &&
						!hasColTurnIn && !hasColReward && !hasQuestSrc && !hasLeveTurnIn && !hasLeveReward {
						ui.Row(c).FillWidth().Padding(16, 0, 0, 0).Gap(8).AlignItems(ui.Center).
							BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
							ui.Icon(c, icoInfo).Size(iconSm, iconSm).Shrink(0).TextColor(t.muted)
							ui.Text(c, "暂无常规商店、制作、采集、任务或收藏品来源，可能出自副本掉落或限时活动。").
								FontSize(t.text.xs).TextColor(t.muted).Grow(1).Shrink(1)
						})
					}
				})
			})
		})
	})
}

func (a *App) renderDetailHero(c *ui.Context, item *gamedata.ItemDetail, isHQ bool, hasAch bool) {
	t := themeOf(c)
	ui.Column(c).FillWidth().Gap(14).Children(func() {
		// 图标、主标题、多语言名称与内联元数据行
		ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Start).Children(func() {
			a.drawGameIcon(c, item.IconID, isHQ, 54, t.radius.xl)
			ui.Column(c).Gap(6).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Baseline).Wrap().Children(func() {
					ui.Text(c, item.NameCn).FontSize(t.text.xl).FontWeight(600).LetterSpacing(-0.3).TextColor(t.foreground).Selectable()
					if isHQ {
						drawPill(c, "HQ", pillWarning)
					}
					numTextf(c, "#%d", item.ID).FontSize(t.text.xs).TextColor(t.muted).Selectable()
				})

				if item.NameEn != "" || item.NameJa != "" {
					ui.Row(c).Gap(12).Wrap().Children(func() {
						if item.NameEn != "" {
							ui.Textf(c, "EN  %s", item.NameEn).FontSize(t.text.xs).TextColor(t.muted).Selectable()
						}
						if item.NameJa != "" {
							ui.Textf(c, "JA  %s", item.NameJa).FontSize(t.text.xs).TextColor(t.muted).Selectable()
						}
					})
				}

				// 结构化内联元数据行：用纯排版替代原本 6+ 个无意义彩色胶囊
				metaParts := []string{
					item.CategoryName,
					fmt.Sprintf("品级 %d", item.ItemLevel),
				}
				if item.EquipLevel > 0 {
					metaParts = append(metaParts, fmt.Sprintf("装备 Lv.%d", item.EquipLevel))
				}
				if item.EquipSlotName != "" {
					metaParts = append(metaParts, item.EquipSlotName)
				}
				if item.ClassJobCategoryName != "" {
					metaParts = append(metaParts, item.ClassJobCategoryName)
				}

				// 仅将需要留意的限制/稀有标记作为克制小徽章，常规可交易/可市场静默内联
				type flagTag struct {
					label string
					kind  pillKind
				}
				var specialFlags []flagTag
				if !item.IsTradable {
					specialFlags = append(specialFlags, flagTag{"不可交易", pillNegative})
				}
				if !item.IsMarketable {
					specialFlags = append(specialFlags, flagTag{"不可上架", pillWarning})
				}
				if !item.IsSellable {
					specialFlags = append(specialFlags, flagTag{"不可出售", pillNegative})
				}
				if item.IsUnique {
					specialFlags = append(specialFlags, flagTag{"独占", pillWarning})
				}
				if item.IsCollectable {
					specialFlags = append(specialFlags, flagTag{"收藏品", pillAccent})
				}
				if item.CanAetherialReduce {
					specialFlags = append(specialFlags, flagTag{"可精选", pillNeutral})
				}
				if item.IsTradable && item.IsMarketable {
					metaParts = append(metaParts, "可交易 · 可上架市场")
				} else if item.IsTradable {
					metaParts = append(metaParts, "可交易")
				}

				ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
					numText(c, strings.Join(metaParts, " · ")).FontSize(t.text.xs).TextColor(t.subdued)
					for _, fl := range specialFlags {
						drawPill(c, fl.label, fl.kind)
					}
				})
			})
		})

		// 物品原文本说明（柔和无描边阅读块）
		if item.DescriptionCn != "" {
			kitBlock(c, ui.Box(c).FillWidth().Padding(10, 14), toneCard).Children(func() {
				ui.Text(c, item.DescriptionCn).FontSize(t.text.sm).LineHeight(lineProse).TextColor(t.subdued).Selectable()
			})
		}

		// 三栏核心参数概览条（单层轻底色 + 1px 竖线分隔）
		showRepair := item.IsEquipment && item.RepairClassJobName != ""
		thirdLabel := "获取途径"
		thirdValue := ""
		if showRepair {
			thirdLabel = "修理职业"
			thirdValue = item.RepairClassJobName
			if item.RepairItemName != "" {
				thirdValue += fmt.Sprintf("（%s）", item.RepairItemName)
			}
		} else {
			var srcParts []string
			if item.IsGilShop {
				srcParts = append(srcParts, "金币商店")
			}
			if item.IsSpecialShop {
				srcParts = append(srcParts, "特殊兑换")
			}
			if item.IsCraftable {
				srcParts = append(srcParts, "配方制作")
			}
			if item.IsGatherable {
				srcParts = append(srcParts, "采集/垂钓")
			}
			if item.IsRetainerVenture {
				srcParts = append(srcParts, "雇员探险")
			}
			if len(item.CollectableTurnIns) > 0 {
				srcParts = append(srcParts, "收藏品交纳")
			}
			if len(item.CollectableRewards) > 0 {
				srcParts = append(srcParts, "收藏品回报")
			}
			if len(item.RewardedFromQuests) > 0 {
				srcParts = append(srcParts, "任务奖励")
			}
			if len(item.TurnInForLeves) > 0 || len(item.RewardedFromLeves) > 0 {
				srcParts = append(srcParts, "理符委托")
			}
			if hasAch {
				srcParts = append(srcParts, "成就奖励")
			}
			if len(srcParts) == 0 {
				thirdValue = "副本掉落 / 活动奖励"
			} else {
				thirdValue = strings.Join(srcParts, " · ")
			}
		}

		sellStr := "不可出售"
		if item.IsSellable {
			sellStr = fmt.Sprintf("%s 金币", formatInt(item.PriceLow))
		}

		kitBlock(c, ui.Row(c).FillWidth().Clip(), toneCard).Children(func() {
			drawMetricSplitCell(c, "堆叠上限", fmt.Sprintf("%s 个", formatInt(item.StackSize)), false)
			drawMetricSplitCell(c, "出售价格", sellStr, true)
			drawMetricSplitCell(c, thirdLabel, thirdValue, true)
		})
	})
}

func drawMetricSplitCell(c *ui.Context, label, value string, leftBorder bool) {
	t := themeOf(c)
	col := ui.Column(c).Grow(1).Shrink(1).Basis(0).MinWidth(0).Padding(10, 14).Gap(3)
	if leftBorder {
		col.BorderWidth(0, 0, 0, hairline).BorderColor(t.border)
	}
	col.Children(func() {
		ui.Text(c, label).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
		numText(c, value).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground).SingleLine()
	})
}

func (a *App) renderEquipmentStatsSection(c *ui.Context, st *gamedata.EquipmentStats, isHQ bool) {
	t := themeOf(c)
	meta := ""
	if st.MateriaSlotCount > 0 {
		meld := "不可禁断"
		if st.IsAdvancedMeldingPermitted {
			meld = "可禁断"
		}
		meta = fmt.Sprintf("魔晶石 %d 孔 · %s", st.MateriaSlotCount, meld)
	}
	drawDetailSection(c, "装备属性", meta, nil, func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			type coreStat struct {
				label string
				val   string
				sub   string
				col   ui.Color
			}
			var core []coreStat
			if st.DamagePhys > 0 {
				v, sub := st.DamagePhys, ""
				if isHQ && st.DamagePhysHQ > st.DamagePhys {
					v, sub = st.DamagePhysHQ, fmt.Sprintf("NQ %d", st.DamagePhys)
				}
				core = append(core, coreStat{"物理基本性能", fmt.Sprintf("%d", v), sub, t.foreground})
			}
			if st.DamageMag > 0 {
				v, sub := st.DamageMag, ""
				if isHQ && st.DamageMagHQ > st.DamageMag {
					v, sub = st.DamageMagHQ, fmt.Sprintf("NQ %d", st.DamageMag)
				}
				core = append(core, coreStat{"魔法基本性能", fmt.Sprintf("%d", v), sub, t.infoText})
			}
			if st.AutoAttack > 0 {
				v, sub := st.AutoAttack, ""
				if isHQ && st.AutoAttackHQ > st.AutoAttack {
					v, sub = st.AutoAttackHQ, fmt.Sprintf("NQ %.2f", st.AutoAttack)
				}
				core = append(core, coreStat{"物理自动攻击", fmt.Sprintf("%.2f", v), sub, t.foreground})
			}
			if st.DelaySec > 0 {
				core = append(core, coreStat{"攻击间隔", fmt.Sprintf("%.2fs", st.DelaySec), "", t.foreground})
			}
			if st.BlockRate > 0 {
				v, sub := st.BlockRate, ""
				if isHQ && st.BlockRateHQ > st.BlockRate {
					v, sub = st.BlockRateHQ, fmt.Sprintf("NQ %d", st.BlockRate)
				}
				core = append(core, coreStat{"格挡发动力", fmt.Sprintf("%d", v), sub, t.foreground})
			}
			if st.Block > 0 {
				v, sub := st.Block, ""
				if isHQ && st.BlockHQ > st.Block {
					v, sub = st.BlockHQ, fmt.Sprintf("NQ %d", st.Block)
				}
				core = append(core, coreStat{"格挡性能", fmt.Sprintf("%d", v), sub, t.foreground})
			}
			if st.DefensePhys > 0 {
				v, sub := st.DefensePhys, ""
				if isHQ && st.DefensePhysHQ > st.DefensePhys {
					v, sub = st.DefensePhysHQ, fmt.Sprintf("NQ %d", st.DefensePhys)
				}
				core = append(core, coreStat{"物理防御力", fmt.Sprintf("%d", v), sub, t.foreground})
			}
			if st.DefenseMag > 0 {
				v, sub := st.DefenseMag, ""
				if isHQ && st.DefenseMagHQ > st.DefenseMag {
					v, sub = st.DefenseMagHQ, fmt.Sprintf("NQ %d", st.DefenseMag)
				}
				core = append(core, coreStat{"魔法防御力", fmt.Sprintf("%d", v), sub, t.foreground})
			}

			if len(core) > 0 {
				kitBlock(c, ui.Row(c).FillWidth().Wrap(), toneCard).Children(func() {
					for i, s := range core {
						col := ui.Column(c).MinWidth(130).Grow(1).Shrink(1).Padding(9, 14).Gap(2)
						if i > 0 {
							col.BorderWidth(0, 0, 0, hairline).BorderColor(t.border)
						}
						col.Children(func() {
							ui.Text(c, s.label).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
							ui.Row(c).Gap(6).AlignItems(ui.Baseline).Children(func() {
								numText(c, s.val).FontSize(t.text.base).FontWeight(600).TextColor(s.col)
								if s.sub != "" {
									numText(c, s.sub).FontSize(t.text.micro).TextColor(t.muted)
								}
							})
						})
					}
				})
			}

			if len(st.Bonuses) > 0 {
				ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
					for _, b := range st.Bonuses {
						val := b.ValueNQ
						if isHQ {
							val = b.ValueHQ
						}
						ui.Row(c).MinWidth(176).Grow(1).Shrink(1).Padding(6, 10).
							BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
							Justify(ui.SpaceBetween).AlignItems(ui.Baseline).Gap(8).Children(func() {
							ui.Text(c, b.Name).FontSize(t.text.xs).TextColor(t.subdued).SingleLine()
							ui.Row(c).Gap(6).AlignItems(ui.Baseline).Shrink(0).Children(func() {
								numTextf(c, "+%d", val).FontSize(t.text.sm).FontWeight(600).TextColor(t.infoText).SingleLine()
								if isHQ && b.ValueHQ > b.ValueNQ {
									numTextf(c, "NQ +%d", b.ValueNQ).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
								}
							})
						})
					}
				})
			}
		})
	})
}

func (a *App) renderEquipmentAttrsSection(c *ui.Context, attr *gamedata.EquipmentAttributes) {
	t := themeOf(c)
	drawDetailSection(c, "装备特性", "", nil, func() {
		desynthTxt := "不可分解"
		if attr.CanDesynth {
			desynthTxt = "可分解"
			if attr.DesynthClassJobName != "" {
				desynthTxt = fmt.Sprintf("可分解（%s）", attr.DesynthClassJobName)
			}
		}
		dyeTxt := "不可染色"
		if attr.DyeCount > 0 {
			dyeTxt = fmt.Sprintf("可染色（%d通道）", attr.DyeCount)
		}
		entries := []struct {
			text string
			pos  bool
		}{
			{boolText(attr.CanExtractMateria, "可精制魔晶石", "不可精制"), attr.CanExtractMateria},
			{boolText(attr.IsProjectable, "可投影", "不可投影"), attr.IsProjectable},
			{desynthTxt, attr.CanDesynth},
			{dyeTxt, attr.DyeCount > 0},
			{boolText(attr.IsCrestWorthy, "可印部队徽记", "不可印徽记"), attr.IsCrestWorthy},
			{boolText(attr.CanStoreInGlamourDresser, "可入投影台", "不可入投影台"), attr.CanStoreInGlamourDresser},
			{boolText(attr.CanStoreInArmoire, "可入收藏柜", "不可入收藏柜"), attr.CanStoreInArmoire},
		}

		// 以清爽无框内联清单呈现布尔特性，避免 7 个套娃卡片 + 7 个胶囊的视觉轰炸
		ui.Row(c).FillWidth().Gap(14).Wrap().AlignItems(ui.Center).Children(func() {
			for _, e := range entries {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					if e.pos {
						ui.Icon(c, icoCheck).Size(iconXs, iconXs).Shrink(0).TextColor(t.successText)
						ui.Text(c, e.text).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
					} else {
						ui.Text(c, "–").FontSize(t.text.xs).TextColor(t.idle).SingleLine()
						ui.Text(c, e.text).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
					}
				})
			}
		})
	})
}

func (a *App) renderConsumableSection(c *ui.Context, item *gamedata.ItemDetail, isHQ bool) {
	t := themeOf(c)
	ce := item.ConsumableEffect
	durTxt := fmt.Sprintf("%d秒", ce.DurationSec)
	if ce.DurationSec/60 > 0 {
		durTxt = fmt.Sprintf("%d分", ce.DurationSec/60)
	}
	meta := fmt.Sprintf("持续 %s", durTxt)
	if ce.CooldownSec > 0 {
		cd := ce.CooldownSec
		if isHQ {
			cd = ce.CooldownHQSec
		}
		meta += fmt.Sprintf(" · 复唱 %d秒", cd)
	}
	var trailing func()
	if ce.ExpBonusPercent > 0 {
		expTxt := fmt.Sprintf("经验值 +%d%%", ce.ExpBonusPercent)
		trailing = func() {
			drawPill(c, expTxt, pillPositive)
		}
	}

	drawDetailSection(c, ce.TypeName+"效果", meta, trailing, func() {
		ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
			for _, p := range ce.Params {
				val, mx := p.ValueNQ, p.MaxNQ
				if isHQ {
					val, mx = p.ValueHQ, p.MaxHQ
				}
				mainStr := fmt.Sprintf("+%d", val)
				if p.IsRelative {
					mainStr = fmt.Sprintf("+%d%%（上限 %d）", val, mx)
				}
				kitBlock(c, ui.Column(c).MinWidth(200).Grow(1).Shrink(1).Padding(10, 12).Gap(3), toneCard).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Baseline).Gap(8).Children(func() {
						ui.Text(c, p.Name).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground).SingleLine()
						numText(c, mainStr).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine()
					})
					if item.CanBeHQ {
						alt := fmt.Sprintf("HQ +%d", p.ValueHQ)
						if isHQ {
							alt = fmt.Sprintf("NQ +%d", p.ValueNQ)
							if p.IsRelative {
								alt = fmt.Sprintf("NQ +%d%%（上限 %d）", p.ValueNQ, p.MaxNQ)
							}
						} else if p.IsRelative {
							alt = fmt.Sprintf("HQ +%d%%（上限 %d）", p.ValueHQ, p.MaxHQ)
						}
						numText(c, alt).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
					}
				})
			}
		})
	})
}

func (a *App) renderRewardAchievementsSection(c *ui.Context, achs []gamedata.AchievementEntry) {
	t := themeOf(c)
	drawDetailSection(c, "成就奖励", fmt.Sprintf("%d 项", len(achs)), nil, func() {
		ui.Column(c).FillWidth().Children(func() {
			for i, ach := range achs {
				if i > 0 {
					kitSeparator(c, false)
				}
				ui.Row(c).FillWidth().Padding(9, 2).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						a.drawGameIcon(c, ach.IconID, false, 30, t.radius.md)
						ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
							ui.Row(c).Gap(8).AlignItems(ui.Baseline).Wrap().Children(func() {
								ui.Text(c, ach.NameCn).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
								numTextf(c, "#%d · %d 点", ach.ID, ach.Points).FontSize(t.text.micro).TextColor(t.muted)
							})
							ui.Text(c, ach.DescriptionCn).FontSize(t.text.xs).TextColor(t.muted)
						})
					})
					ui.Textf(c, "%s · %s", ach.KindName, ach.CategoryName).FontSize(t.text.xs).TextColor(t.muted).Shrink(0).SingleLine()
				})
			}
		})
	})
}

func shopTypePillKind(shopType string) pillKind {
	switch shopType {
	case "special":
		return pillAccent
	case "gc":
		return pillWarning
	case "fcc":
		return pillPositive
	default:
		return pillNeutral
	}
}

func (a *App) renderShopsSection(c *ui.Context, itemName string, shops []gamedata.ShopPurchaseSource) {
	t := themeOf(c)
	drawDetailSection(c, "商店兑换", fmt.Sprintf("%d 项", len(shops)), nil, func() {
		ui.Column(c).FillWidth().Children(func() {
			for sIdx, shop := range shops {
				if sIdx > 0 {
					kitSeparator(c, false)
				}
				// 自适应双行紧凑结构：杜绝窄屏下固定列宽溢出的问题
				ui.Column(c.Key(fmt.Sprintf("shop-%d-%d", shop.ShopID, sIdx))).FillWidth().Padding(10, 2).Gap(7).Children(func() {
					// 第一行：商店类型 + 名称 + 产出数量 + 兑换消耗
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
							drawPill(c, shop.ShopTypeLabel, shopTypePillKind(shop.ShopType))
							ui.Text(c, shop.ShopName).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
							numTextf(c, "产出 ×%d", shop.ReceiveAmount).FontSize(t.text.xs).TextColor(t.muted)
							if shop.ReceiveHQ {
								drawPill(c, "HQ", pillWarning)
							}
						})

						ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
							for cIdx, cost := range shop.Costs {
								canJump := cost.ItemID > 1 && cost.ItemID < 100000
								if !canJump {
									// 不可跳转的基础货币（如金币）渲染为纯图文，避免伪按钮困惑
									ui.Row(c.Key(fmt.Sprintf("cost-plain-%d-%d-%d", shop.ShopID, sIdx, cIdx))).
										Padding(2, 4).Gap(5).AlignItems(ui.Center).Children(func() {
										if cost.IconID > 0 {
											a.drawGameIcon(c, cost.IconID, false, 18, 3)
										}
										ui.Text(c, cost.NameCn).FontSize(t.text.xs).TextColor(t.subdued).SingleLine()
										numTextf(c, "×%s", formatInt(cost.Amount)).FontSize(t.text.xs).FontWeight(600).TextColor(t.foreground).SingleLine()
									})
									continue
								}
								btn := ui.ButtonBase(c.Key(fmt.Sprintf("cost-%d-%d-%d", shop.ShopID, sIdx, cIdx))).
									Label("查看 "+cost.NameCn).
									Padding(3, 8).Radius(t.radius.md).Background(t.card).Border(hairline, t.border).
									Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
								if btn.Hovered() {
									btn.Background(t.tint(1.5))
								}
								btn.Children(func() {
									ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
										if cost.IconID > 0 {
											a.drawGameIcon(c, cost.IconID, false, 18, 3)
										}
										ui.Text(c, cost.NameCn).FontSize(t.text.xs).TextColor(t.foreground).SingleLine()
										numTextf(c, "×%s", formatInt(cost.Amount)).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine()
									})
								})
								if btn.Clicked() {
									a.selectItem(cost.ItemID)
								}
							}
						})
					})

					// 第二行：售卖 NPC 与坐标按钮
					a.renderVendorNpcChips(c, fmt.Sprintf("npcmap-%d-%d", shop.ShopID, sIdx), fmt.Sprintf("%s · %s", shop.ShopName, itemName), "通用柜台 / 住宅区或军需官兑换", shop.NPCs)
				})
			}
		})
	})
}

func (a *App) renderMapLocationBtn(c *ui.Context, key, title, subtitle string, loc *gamedata.MapLocationPoint) {
	if loc == nil {
		return
	}
	t := themeOf(c)
	locTxt := loc.ZoneName
	if loc.PlaceName != "" && loc.PlaceName != loc.ZoneName {
		locTxt = loc.ZoneName + " · " + loc.PlaceName
	}
	if loc.MapPath != "" {
		coordLbl := fmt.Sprintf("%s (%.1f, %.1f)", locTxt, loc.CoordX, loc.CoordY)
		btn := ui.ButtonBase(c.Key(key)).Label("查看地图："+coordLbl).
			Height(24).Padding(0, 8).Radius(t.radius.md).Gap(5).
			Background(t.card).Border(hairline, t.border).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
		if btn.Hovered() {
			btn.Background(t.tint(1.5))
		}
		btn.Children(func() {
			ui.Icon(c, icoMapPin).Size(iconXs, iconXs).Shrink(0).TextColor(t.infoText)
			ui.Text(c, locTxt).FontSize(t.text.xs).TextColor(t.subdued).SingleLine()
			numTextf(c, "%.1f, %.1f", loc.CoordX, loc.CoordY).FontSize(t.text.micro).FontWeight(500).TextColor(t.infoText).SingleLine()
		})
		if btn.Clicked() {
			a.openMap(MapMarkerData{
				Title:      title,
				Subtitle:   subtitle,
				RegionName: loc.RegionName,
				ZoneName:   loc.ZoneName,
				PlaceName:  loc.PlaceName,
				MapPath:    loc.MapPath,
				CoordX:     loc.CoordX,
				CoordY:     loc.CoordY,
				NormX:      loc.NormX,
				NormY:      loc.NormY,
			})
		}
		return
	}
	ui.Text(c, locTxt).FontSize(t.text.xs).TextColor(t.muted)
}

func (a *App) renderVendorNpcChips(c *ui.Context, keyPrefix, subtitle, emptyHint string, npcs []gamedata.ShopVendorNpc) {
	t := themeOf(c)
	ui.Row(c).FillWidth().Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
		if len(npcs) == 0 {
			if emptyHint != "" {
				ui.Text(c, emptyHint).FontSize(t.text.xs).TextColor(t.muted)
			}
			return
		}
		maxShow := min(6, len(npcs))
		for _, npc := range npcs[:maxShow] {
			txt := npc.NpcName
			if npc.ZoneName != "" {
				txt += " · " + npc.ZoneName
			}
			if npc.MapPath != "" {
				coordLbl := fmt.Sprintf("%s  (%.1f, %.1f)", txt, npc.CoordX, npc.CoordY)
				btn := ui.ButtonBase(c.Key(fmt.Sprintf("%s-%d", keyPrefix, npc.NpcID))).
					Label("查看地图："+coordLbl).
					Height(24).Padding(0, 8).Radius(t.radius.md).Gap(5).
					Background(t.card).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
				if btn.Hovered() {
					btn.Background(t.tint(1.5))
				}
				btn.Children(func() {
					ui.Icon(c, icoMapPin).Size(iconXs, iconXs).Shrink(0).TextColor(t.infoText)
					ui.Text(c, txt).FontSize(t.text.xs).TextColor(t.subdued).SingleLine()
					numTextf(c, "%.1f, %.1f", npc.CoordX, npc.CoordY).FontSize(t.text.micro).FontWeight(500).TextColor(t.infoText).SingleLine()
				})
				if btn.Clicked() {
					title := npc.NpcName
					if npc.NpcTitle != "" {
						title += " <" + npc.NpcTitle + ">"
					}
					a.openMap(MapMarkerData{
						Title:      title,
						Subtitle:   subtitle,
						RegionName: npc.RegionName,
						ZoneName:   npc.ZoneName,
						PlaceName:  npc.PlaceName,
						MapPath:    npc.MapPath,
						CoordX:     npc.CoordX,
						CoordY:     npc.CoordY,
						NormX:      npc.NormX,
						NormY:      npc.NormY,
					})
				}
			} else {
				ui.Text(c, txt).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
			}
		}
		if len(npcs) > maxShow {
			numTextf(c, "等共 %d 名 NPC", len(npcs)).FontSize(t.text.micro).TextColor(t.muted)
		}
	})
}

func collectableKindPill(kind string) pillKind {
	switch kind {
	case "satisfaction":
		return pillWarning
	case "hwd":
		return pillPositive
	default:
		return pillAccent
	}
}

func (a *App) renderCollectableTurnInsSection(c *ui.Context, itemName string, list []gamedata.CollectableTurnInSource) {
	t := themeOf(c)
	drawDetailSection(c, "收藏品交易交纳", fmt.Sprintf("%d 项", len(list)), nil, func() {
		ui.Column(c).FillWidth().Children(func() {
			for idx, src := range list {
				if idx > 0 {
					kitSeparator(c, false)
				}
				ui.Column(c.Key(fmt.Sprintf("colturn-%d-%d", src.ShopID, idx))).FillWidth().Padding(10, 2).Gap(8).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
							drawPill(c, src.SourceLabel, collectableKindPill(src.SourceKind))
							ui.Text(c, src.ShopName).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
							if src.GroupName != "" {
								ui.Text(c, src.GroupName).FontSize(t.text.xs).TextColor(t.muted)
							}
							if src.IsBonus {
								drawPill(c, "需求奖励加成", pillWarning)
							}
						})
						var metas []string
						if src.LevelMin > 0 && src.LevelMax > src.LevelMin {
							metas = append(metas, fmt.Sprintf("Lv.%d~%d", src.LevelMin, src.LevelMax))
						} else if src.LevelMin > 0 {
							metas = append(metas, fmt.Sprintf("Lv.%d", src.LevelMin))
						}
						if src.Stars > 0 {
							metas = append(metas, fmt.Sprintf("%d星", src.Stars))
						}
						if src.RequiredQuestName != "" {
							metas = append(metas, "前置："+src.RequiredQuestName)
						}
						if len(metas) > 0 {
							numText(c, strings.Join(metas, " · ")).FontSize(t.text.xs).TextColor(t.muted)
						}
					})

					if len(src.Tiers) > 0 {
						kitBlock(c, ui.Row(c).FillWidth().Clip(), toneCard).Children(func() {
							for ti, tier := range src.Tiers {
								cell := ui.Column(c).Grow(1).Shrink(1).Basis(0).MinWidth(0).Padding(8, 10).Gap(4)
								if ti > 0 {
									cell.BorderWidth(0, 0, 0, hairline).BorderColor(t.border)
								}
								cell.Children(func() {
									numTextf(c, "%s · 价值 %d+", tier.Label, tier.MinCollectability).
										FontSize(t.text.micro).TextColor(t.muted).SingleLine()

									if tier.RewardItemID > 0 && tier.RewardAmount > 0 {
										btn := ui.ButtonBase(c.Key(fmt.Sprintf("coltier-%d-%d-%d", src.ShopID, idx, ti))).
											Label("查看 "+tier.RewardItemName).Padding(2, 4).Radius(t.radius.sm).
											Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
										if btn.Hovered() {
											btn.Background(t.tint(1.5))
										}
										btn.Children(func() {
											ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
												if tier.RewardItemIconID > 0 {
													a.drawGameIcon(c, tier.RewardItemIconID, false, 18, 3)
												}
												ui.Text(c, tier.RewardItemName).FontSize(t.text.xs).TextColor(t.foreground).SingleLine().Shrink(1)
												numTextf(c, "×%d", tier.RewardAmount).FontSize(t.text.sm).FontWeight(600).TextColor(t.infoText).SingleLine()
											})
										})
										if btn.Clicked() {
											a.selectItem(tier.RewardItemID)
										}
									}

									var extras []string
									if tier.SecondaryItemID > 0 && tier.SecondaryAmount > 0 {
										extras = append(extras, fmt.Sprintf("%s ×%d", tier.SecondaryItemName, tier.SecondaryAmount))
									}
									if tier.GilReward > 0 {
										extras = append(extras, fmt.Sprintf("%s 金币", formatInt(tier.GilReward)))
									}
									if tier.Satisfaction > 0 {
										extras = append(extras, fmt.Sprintf("信赖度 +%d", tier.Satisfaction))
									}
									if tier.ExpReward > 0 {
										extras = append(extras, fmt.Sprintf("%s EXP", formatInt(tier.ExpReward)))
									} else if tier.ExpRatio > 0 {
										extras = append(extras, fmt.Sprintf("EXP 系数 %d", tier.ExpRatio))
									}
									if tier.Points > 0 {
										extras = append(extras, fmt.Sprintf("技巧点 %d", tier.Points))
									}
									if len(extras) > 0 {
										numText(c, strings.Join(extras, " · ")).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
									}
								})
							}
						})
					}

					a.renderVendorNpcChips(c, fmt.Sprintf("colnpc-%d-%d", src.ShopID, idx), src.ShopName+" · "+itemName, "", src.NPCs)
				})
			}
		})
	})
}

func (a *App) renderCollectableRewardsSection(c *ui.Context, itemName string, list []gamedata.CollectableRewardSource) {
	t := themeOf(c)
	drawDetailSection(c, "通过收藏品交纳获得", fmt.Sprintf("%d 项", len(list)), nil, func() {
		ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
			for idx, rew := range list {
				btn := ui.ButtonBase(c.Key(fmt.Sprintf("colrew-%d-%d", rew.TurnInItemID, idx))).
					Label("查看 "+rew.TurnInItemName).MinWidth(250).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
					Background(t.card).Border(hairline, t.border).Justify(ui.SpaceBetween).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
				if btn.Hovered() {
					btn.Background(t.tint(1.5))
				}
				btn.Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						a.drawGameIcon(c, rew.TurnInItemIconID, false, 26, t.radius.md)
						ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
							ui.Text(c, rew.TurnInItemName).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
							sub := rew.ShopName
							if rew.MinCollectability > 0 {
								sub += fmt.Sprintf(" · 价值 %d+", rew.MinCollectability)
							}
							numText(c, sub).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
						})
					})
					amtStr := fmt.Sprintf("%d / %d / %d", rew.RewardLow, rew.RewardMid, rew.RewardHigh)
					if rew.RewardLow == rew.RewardHigh {
						amtStr = fmt.Sprintf("×%d", rew.RewardHigh)
					}
					numText(c, amtStr).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine().Shrink(0)
				})
				if btn.Clicked() {
					a.selectItem(rew.TurnInItemID)
				}
			}
		})
	})
}

func (a *App) renderQuestSourcesSection(c *ui.Context, quests []gamedata.ItemQuestSource) {
	t := themeOf(c)
	drawDetailSection(c, "任务奖励", fmt.Sprintf("%d 项", len(quests)), nil, func() {
		ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
			for idx, qs := range quests {
				btn := ui.ButtonBase(c.Key(fmt.Sprintf("item-qsrc-%d-%d", qs.QuestID, idx))).
					Label("查看任务 "+qs.NameCn).MinWidth(240).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
					Background(t.card).Border(hairline, t.border).Justify(ui.SpaceBetween).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
				if btn.Hovered() {
					btn.Background(t.tint(1.5))
				}
				btn.Children(func() {
					ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						ui.Text(c, gamedata.CleanQuestTitle(qs.NameCn)).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
						sub := qs.GroupLabel
						if qs.GenreName != "" {
							sub = qs.GenreName + " · " + sub
						}
						if qs.Level > 0 {
							sub = fmt.Sprintf("Lv.%d · %s", qs.Level, sub)
						}
						numText(c, sub).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
					})
					numTextf(c, "×%d", qs.Count).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine().Shrink(0)
				})
				if btn.Clicked() {
					a.inspectQuestFromOtherTab(qs.QuestID)
				}
			}
		})
	})
}

func (a *App) renderLeveSourcesSection(c *ui.Context, title string, leves []gamedata.ItemLeveSource, isTurnIn bool) {
	t := themeOf(c)
	drawDetailSection(c, title, fmt.Sprintf("%d 项", len(leves)), nil, func() {
		ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
			for idx, ls := range leves {
				btn := ui.ButtonBase(c.Key(fmt.Sprintf("item-lsrc-%t-%d-%d", isTurnIn, ls.LeveID, idx))).
					Label("查看理符 "+ls.NameCn).MinWidth(240).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
					Background(t.card).Border(hairline, t.border).Justify(ui.SpaceBetween).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
				if btn.Hovered() {
					btn.Background(t.tint(1.5))
				}
				btn.Children(func() {
					ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						ui.Text(c, ls.NameCn).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
						sub := fmt.Sprintf("Lv.%d · %s", ls.Level, ls.AssignmentTypeName)
						numText(c, sub).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
					})
					right := fmt.Sprintf("×%d", ls.Count)
					if !isTurnIn && ls.ProbabilityPercent > 0 {
						right = fmt.Sprintf("×%d (%d%%)", ls.Count, ls.ProbabilityPercent)
					}
					numText(c, right).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine().Shrink(0)
				})
				if btn.Clicked() {
					a.inspectLeveFromOtherTab(ls.LeveID)
				}
			}
		})
	})
}

func (a *App) renderRecipesSection(c *ui.Context, item *gamedata.ItemDetail) {
	t := themeOf(c)
	if a.activeRecipeIdx >= len(item.Recipes) {
		a.activeRecipeIdx = 0
	}
	cur := item.Recipes[a.activeRecipeIdx]
	var curTree *gamedata.RecipeTreeNode
	if a.activeRecipeIdx < len(item.RecipeTrees) {
		curTree = &item.RecipeTrees[a.activeRecipeIdx]
	} else if len(item.RecipeTrees) > 0 {
		curTree = &item.RecipeTrees[0]
	}

	var trailing func()
	if len(item.Recipes) > 1 {
		labels := make([]string, len(item.Recipes))
		for i, r := range item.Recipes {
			labels[i] = r.CraftJobName
		}
		trailing = func() {
			if nextIdx, chg := kitSegmentedTabs(c, fmt.Sprintf("rec-%d", item.ID), a.activeRecipeIdx, labels); chg {
				a.activeRecipeIdx = nextIdx
			}
		}
	}

	meta := fmt.Sprintf("%d 种职业", len(item.Recipes))
	if len(item.Recipes) == 1 {
		meta = ""
	}
	drawDetailSection(c, "制作配方", meta, trailing, func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			// 配方头部摘要
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				a.drawGameIcon(c, cur.CraftJobIconID, false, 32, t.radius.md)
				ui.Column(c).Gap(3).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Baseline).Wrap().Children(func() {
						ui.Textf(c, "%s · Lv.%d%s", cur.CraftJobName, cur.RecipeLevel, formatStars(cur.Stars)).
							FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
						if cur.ResultAmount > 1 {
							numTextf(c, "单次产出 ×%d", cur.ResultAmount).FontSize(t.text.xs).FontWeight(500).TextColor(t.successText)
						}
						numTextf(c, "Recipe #%d", cur.ID).FontSize(t.text.micro).TextColor(t.muted)
					})
					if cur.SecretRecipeBookName != "" || cur.IsSpecializationRequired || cur.IsExpert || cur.CanQuickSynth {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
							if cur.SecretRecipeBookName != "" {
								drawPill(c, "秘籍："+cur.SecretRecipeBookName, pillWarning)
							}
							if cur.IsSpecializationRequired {
								drawPill(c, "需专家之证", pillNegative)
							}
							if cur.IsExpert {
								drawPill(c, "高难配方", pillWarning)
							}
							if cur.CanQuickSynth {
								ui.Text(c, "支持简易制作").FontSize(t.text.micro).TextColor(t.muted)
							}
						})
					}
				})
			})

			// 6 项制作参数：单层轻底色自适应流式指标条（窄屏优雅折行，绝不挤爆）
			reqCraftLabel := "推荐作业"
			reqCraftVal := cur.SuggestedCraftsmanship
			if cur.RequiredCraftsmanship > 0 {
				reqCraftLabel = "要求作业"
				reqCraftVal = cur.RequiredCraftsmanship
			}
			reqCtrlStr := "–"
			if cur.RequiredControl > 0 {
				reqCtrlStr = fmt.Sprintf("%d", cur.RequiredControl)
			}
			metrics := []struct {
				label string
				val   string
				col   ui.Color
			}{
				{"作业难度", fmt.Sprintf("%d", cur.Difficulty), t.foreground},
				{"最高品质", fmt.Sprintf("%d", cur.Quality), t.infoText},
				{"耐久度", fmt.Sprintf("%d", cur.Durability), t.successText},
				{reqCraftLabel, fmt.Sprintf("%d", reqCraftVal), t.foreground},
				{"要求加工", reqCtrlStr, t.foreground},
				{"初期品质", fmt.Sprintf("%d%%", cur.MaterialQualityFactor), t.foreground},
			}
			kitBlock(c, ui.Row(c).FillWidth().Wrap(), toneCard).Children(func() {
				for i, m := range metrics {
					cell := ui.Column(c).MinWidth(94).Grow(1).Shrink(1).Padding(8, 12).Gap(2)
					if i > 0 {
						cell.BorderWidth(0, 0, 0, hairline).BorderColor(t.border)
					}
					cell.Children(func() {
						ui.Text(c, m.label).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
						numText(c, m.val).FontSize(t.text.sm).FontWeight(600).TextColor(m.col).SingleLine()
					})
				}
			})

			// 制作材料与子配方分解树
			if curTree != nil {
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					ui.Text(c, "制作材料").FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Padding(2, 0)
					for idx, ch := range curTree.Children {
						a.renderRecipeTreeNode(c, ch, 1, 0, fmt.Sprintf("rt-%d-%d", cur.ID, idx))
					}
					for idx, cry := range curTree.Crystals {
						a.renderCrystalRow(c, cry, 1, fmt.Sprintf("rc-%d-%d", cur.ID, idx))
					}
				})
			}
		})
	})
}

func (a *App) renderRecipeTreeNode(c *ui.Context, node gamedata.RecipeTreeNode, parentCrafts int, depth int, keyPath string) {
	t := themeOf(c)
	totalNeeded := node.AmountPerCraft * parentCrafts
	yield := max(1, node.YieldPerCraft)
	craftsNeeded := max(1, (totalNeeded+yield-1)/yield)

	ui.Column(c.Key(keyPath)).FillWidth().Gap(3).Children(func() {
		row := ui.ButtonBase(c.Key(keyPath+"-btn")).Label(node.NameCn).
			FillWidth().Padding(6, 10).Radius(t.radius.md).
			Background(t.card).Justify(ui.SpaceBetween).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
		if row.Hovered() {
			row.Background(t.tint(1.5))
		}
		row.Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				a.drawGameIcon(c, node.IconID, false, 22, t.radius.sm)
				ui.Text(c, node.NameCn).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
				if node.CraftJobName != "" {
					numTextf(c, "%s Lv.%d%s", node.CraftJobName, node.RecipeLevel, formatStars(node.Stars)).
						FontSize(t.text.micro).TextColor(t.muted).SingleLine()
				} else if node.IsGatherable {
					ui.Text(c, "可采集").FontSize(t.text.micro).TextColor(t.successText).SingleLine()
				}
			})
			numTextf(c, "×%d", totalNeeded).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine().Shrink(0)
		})
		if row.Clicked() {
			a.selectItem(node.ItemID)
		}

		if len(node.Children) > 0 || len(node.Crystals) > 0 {
			ui.Column(c).FillWidth().Margin(0, 0, 0, 14).Padding(0, 0, 0, 10).Gap(3).
				BorderWidth(0, 0, 0, hairline).BorderColor(t.border).Children(func() {
				for idx, ch := range node.Children {
					a.renderRecipeTreeNode(c, ch, craftsNeeded, depth+1, fmt.Sprintf("%s-c%d", keyPath, idx))
				}
				for idx, cry := range node.Crystals {
					a.renderCrystalRow(c, cry, craftsNeeded, fmt.Sprintf("%s-k%d", keyPath, idx))
				}
			})
		}
	})
}

func (a *App) renderCrystalRow(c *ui.Context, cry gamedata.RecipeCrystalNode, crafts int, key string) {
	t := themeOf(c)
	row := ui.ButtonBase(c.Key(key)).Label(cry.NameCn).FillWidth().Padding(5, 10).Radius(t.radius.md).
		Justify(ui.SpaceBetween).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
	if row.Hovered() {
		row.Background(t.tint(1.25))
	}
	row.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			a.drawGameIcon(c, cry.IconID, false, 18, 3)
			ui.Text(c, cry.NameCn).FontSize(t.text.xs).TextColor(t.subdued).SingleLine()
		})
		numTextf(c, "×%d", cry.Amount*crafts).FontSize(t.text.xs).FontWeight(600).TextColor(t.muted).SingleLine()
	})
	if row.Clicked() {
		a.selectItem(cry.ItemID)
	}
}

func (a *App) renderGatheringSections(c *ui.Context, itemName string, g gamedata.GatheringMethodDetail) {
	t := themeOf(c)
	now := time.Now()

	// 1. 采矿 / 园艺采集点
	if len(g.Nodes) > 0 {
		drawDetailSection(c, "采集点", fmt.Sprintf("%d 处", len(g.Nodes)), nil, func() {
			ui.Column(c).FillWidth().Children(func() {
				for idx, node := range g.Nodes {
					if idx > 0 {
						kitSeparator(c, false)
					}
					ui.Column(c.Key(fmt.Sprintf("gnode-%d-%d", node.PointID, idx))).FillWidth().Padding(9, 2).Gap(5).Children(func() {
						ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(10).Children(func() {
							ui.Row(c).Gap(9).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
								a.drawGameIcon(c, node.GatheringTypeIconID, false, 28, t.radius.md)
								ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
									ui.Row(c).Gap(8).AlignItems(ui.Baseline).Wrap().Children(func() {
										ui.Textf(c, "%s · %s", node.DisciplineName, node.GatheringTypeName).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
										numTextf(c, "Lv.%d%s", node.NodeLevel, formatStars(node.Stars)).FontSize(t.text.xs).FontWeight(500).TextColor(t.successText)
									})
									ui.Textf(c, "%s › %s › %s", node.RegionName, node.ZoneName, node.PlaceName).FontSize(t.text.xs).TextColor(t.muted)
								})
							})
							if node.MapPath != "" {
								if kitButton(c, fmt.Sprintf("gnode-map-%d-%d", node.PointID, idx), fmt.Sprintf("%.1f, %.1f", node.CoordX, node.CoordY), btnOpts{variant: btnOutline, size: btnSizeXs, icon: icoMapPin}) {
									a.openMap(MapMarkerData{
										Title:      fmt.Sprintf("%s (%s Lv.%d)", itemName, node.DisciplineName, node.NodeLevel),
										Subtitle:   node.GatheringTypeName,
										RegionName: node.RegionName,
										ZoneName:   node.ZoneName,
										PlaceName:  node.PlaceName,
										MapPath:    node.MapPath,
										CoordX:     node.CoordX,
										CoordY:     node.CoordY,
										NormX:      node.NormX,
										NormY:      node.NormY,
									})
								}
							}
						})

						if node.FolkloreBookName != "" || node.IsHidden || node.IsEphemeral || node.IsTimed {
							ui.Row(c).FillWidth().Padding(0, 0, 0, 37).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
								if node.IsTimed {
									ts := gamedata.EvaluateGatheringWindows(node.TimeWindows, now)
									var winStrs []string
									for _, w := range node.TimeWindows {
										winStrs = append(winStrs, fmt.Sprintf("%s–%s", w.StartET, w.EndET))
									}
									dotCol := t.attention
									statusCol := t.warningText
									if ts.IsActive {
										dotCol = t.success
										statusCol = t.successText
									}
									ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
										drawStatusDot(c, dotCol)
										numText(c, "ET "+strings.Join(winStrs, " / ")).FontSize(t.text.xs).TextColor(t.subdued)
										numText(c, ts.StatusText).FontSize(t.text.xs).FontWeight(500).TextColor(statusCol)
									})
								}
								if node.FolkloreBookName != "" {
									drawPill(c, "传承录："+node.FolkloreBookName, pillWarning)
								}
								if node.IsHidden {
									drawPill(c, "隐藏", pillNeutral)
								}
								if node.IsEphemeral {
									drawPill(c, "精选限时", pillAccent)
								}
							})
						}
					})
				}
			})
		})
	}

	// 2. 捕鱼人垂钓钓场
	if len(g.FishingSpots) > 0 {
		drawDetailSection(c, "钓场", fmt.Sprintf("%d 处", len(g.FishingSpots)), nil, func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				if g.FishingSpots[0].FishGuideText != "" {
					kitBlock(c, ui.Box(c).FillWidth().Padding(9, 12), toneCard).Children(func() {
						ui.Text(c, g.FishingSpots[0].FishGuideText).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.subdued).Selectable()
					})
				}
				ui.Column(c).FillWidth().Children(func() {
					for idx, sp := range g.FishingSpots {
						if idx > 0 {
							kitSeparator(c, false)
						}
						ui.Row(c.Key(fmt.Sprintf("fspot-%d", sp.SpotID))).FillWidth().Padding(8, 2).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(10).Children(func() {
							ui.Row(c).Gap(9).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
								a.drawGameIcon(c, 62018, false, 26, t.radius.md)
								ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
									ui.Row(c).Gap(8).AlignItems(ui.Baseline).Wrap().Children(func() {
										ui.Text(c, sp.PlaceName).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
										numTextf(c, "Lv.%d · %s", sp.Level, sp.CategoryName).FontSize(t.text.xs).TextColor(t.infoText)
									})
									sub := fmt.Sprintf("%s › %s", sp.RegionName, sp.ZoneName)
									if sp.FolkloreBookName != "" {
										sub += " · 需传承录：" + sp.FolkloreBookName
									}
									ui.Text(c, sub).FontSize(t.text.xs).TextColor(t.muted)
								})
							})
							if sp.MapPath != "" {
								if kitButton(c, fmt.Sprintf("fspot-map-%d", sp.SpotID), fmt.Sprintf("%.1f, %.1f", sp.CoordX, sp.CoordY), btnOpts{variant: btnOutline, size: btnSizeXs, icon: icoMapPin}) {
									a.openMap(MapMarkerData{
										Title:      fmt.Sprintf("%s（钓场：%s）", itemName, sp.PlaceName),
										Subtitle:   fmt.Sprintf("Lv.%d %s", sp.Level, sp.CategoryName),
										RegionName: sp.RegionName,
										ZoneName:   sp.ZoneName,
										PlaceName:  sp.PlaceName,
										MapPath:    sp.MapPath,
										CoordX:     sp.CoordX,
										CoordY:     sp.CoordY,
										NormX:      sp.NormX,
										NormY:      sp.NormY,
									})
								}
							}
						})
					}
				})
			})
		})
	}

	// 3. 刺鱼渔场
	if len(g.Spearfishing) > 0 {
		drawDetailSection(c, "刺鱼", fmt.Sprintf("%d 处", len(g.Spearfishing)), nil, func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				if g.Spearfishing[0].Description != "" {
					kitBlock(c, ui.Box(c).FillWidth().Padding(9, 12), toneCard).Children(func() {
						ui.Text(c, g.Spearfishing[0].Description).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.subdued).Selectable()
					})
				}
				ui.Column(c).FillWidth().Children(func() {
					for idx, sf := range g.Spearfishing {
						if idx > 0 {
							kitSeparator(c, false)
						}
						ui.Row(c.Key(fmt.Sprintf("sf-%d", sf.NotebookID))).FillWidth().Padding(8, 2).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(10).Children(func() {
							ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
								ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
									ui.Text(c, sf.PlaceName).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
									numTextf(c, "Lv.%d", sf.Level).FontSize(t.text.xs).TextColor(t.infoText)
								})
								ui.Textf(c, "%s › %s", sf.RegionName, sf.ZoneName).FontSize(t.text.xs).TextColor(t.muted)
							})
							if sf.MapPath != "" {
								if kitButton(c, fmt.Sprintf("sf-map-%d", sf.NotebookID), fmt.Sprintf("%.1f, %.1f", sf.CoordX, sf.CoordY), btnOpts{variant: btnOutline, size: btnSizeXs, icon: icoMapPin}) {
									a.openMap(MapMarkerData{
										Title:      fmt.Sprintf("%s（刺鱼：%s）", itemName, sf.PlaceName),
										Subtitle:   fmt.Sprintf("Lv.%d 刺鱼渔场", sf.Level),
										RegionName: sf.RegionName,
										ZoneName:   sf.ZoneName,
										PlaceName:  sf.PlaceName,
										MapPath:    sf.MapPath,
										CoordX:     sf.CoordX,
										CoordY:     sf.CoordY,
										NormX:      sf.NormX,
										NormY:      sf.NormY,
									})
								}
							}
						})
					}
				})
			})
		})
	}
}

func venturePillKind(t string) pillKind {
	switch t {
	case "hunt":
		return pillWarning
	case "botany":
		return pillPositive
	case "fishing":
		return pillAccent
	default:
		return pillNeutral
	}
}

func tierStatName(statType string) string {
	switch statType {
	case "ilvl":
		return "装等"
	case "gathering":
		return "获得力"
	default:
		return "鉴别力"
	}
}

func formatCompactTierReq(tier gamedata.RetainerVentureTier) string {
	if tier.RequiredStat <= 0 {
		return "基础"
	}
	return fmt.Sprintf("≥ %d", tier.RequiredStat)
}

func (a *App) renderRetainerSection(c *ui.Context, ventures []gamedata.RetainerVentureSource) {
	t := themeOf(c)
	drawDetailSection(c, "雇员探险", fmt.Sprintf("%d 项", len(ventures)), nil, func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			for i, v := range ventures {
				if i > 0 {
					kitSeparator(c, false)
				}
				ui.Column(c.Key(fmt.Sprintf("ret-%d", v.TaskID))).FillWidth().Gap(8).Children(func() {
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
							drawPill(c, v.VentureTypeLabel, venturePillKind(v.VentureType))
							ui.Text(c, v.TaskName).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
							ui.Text(c, v.ClassJobCategoryName).FontSize(t.text.xs).TextColor(t.muted)
							if v.RequiresLogUnlock {
								drawPill(c, "需解锁图鉴", pillNeutral)
							}
						})
						statLabel := ""
						if len(v.Tiers) > 0 {
							statLabel = tierStatName(v.Tiers[0].StatType) + "分档 · "
						}
						numTextf(c, "%sLv.%d · %d 分钟 · 探险币 ×%d · 经验 %s",
							statLabel, v.RetainerLevel, v.DurationMinutes, v.VentureCost, formatInt(v.Experience)).
							FontSize(t.text.xs).TextColor(t.muted)
					})

					// 单层 5 档紧凑分隔条（无套娃小框，窄屏不截断）
					if len(v.Tiers) > 0 {
						kitBlock(c, ui.Row(c).FillWidth().Clip(), toneCard).Children(func() {
							for ti, tier := range v.Tiers {
								cell := ui.Column(c).Grow(1).Shrink(1).Basis(0).MinWidth(0).Padding(7, 10).Gap(2)
								if ti > 0 {
									cell.BorderWidth(0, 0, 0, hairline).BorderColor(t.border)
								}
								cell.Children(func() {
									numText(c, formatCompactTierReq(tier)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
									numTextf(c, "×%d", tier.Quantity).FontSize(t.text.sm).FontWeight(600).TextColor(t.infoText).SingleLine()
								})
							}
						})
					}
				})
			}
		})
	})
}

func (a *App) renderUsedInRecipesSection(c *ui.Context, used []gamedata.UsedInRecipeItem) {
	t := themeOf(c)
	drawDetailSection(c, "用于制作", fmt.Sprintf("%d 项", len(used)), nil, func() {
		ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
			for _, u := range used {
				btn := ui.ButtonBase(c.Key(fmt.Sprintf("used-%d", u.RecipeID))).Label(u.ResultNameCn).
					MinWidth(210).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
					Background(t.card).Justify(ui.SpaceBetween).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
				if btn.Hovered() {
					btn.Background(t.tint(1.5))
				}
				btn.Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						a.drawGameIcon(c, u.ResultIconID, false, 26, t.radius.md)
						ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
							ui.Text(c, u.ResultNameCn).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
							numTextf(c, "%s Lv.%d%s", u.CraftJobName, u.RecipeLevel, formatStars(u.Stars)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
						})
					})
					numTextf(c, "×%d", u.AmountRequired).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine().Shrink(0)
				})
				if btn.Clicked() {
					a.selectItem(u.ResultItemID)
				}
			}
		})
	})
}

func boolText(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}
