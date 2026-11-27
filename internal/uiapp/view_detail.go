package uiapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type itemDetailPaneOpts struct {
	keyPrefix       string
	showOpenInMain  bool
	detail          *gamedata.ItemDetail
	activeRecipeIdx *int
	scroll          *ui.ScrollState
	onJumpItem      func(int)
	onJumpQuest     func(int)
	onJumpLeve      func(int)
}

// renderItemDetailPane 渲染右侧详情阅读面板（扁平百科式排版：顶部分隔节标题 + 零嵌套卡片 + 克制语义标注）。
func (a *App) renderItemDetailPane(c *ui.Context) {
	a.renderItemDetailWithOpts(c, itemDetailPaneOpts{
		keyPrefix:       "wb-item",
		detail:          a.selectedDetail,
		activeRecipeIdx: &a.activeRecipeIdx,
		scroll:          &a.detailScroll,
		onJumpItem:      a.jumpToItem,
		onJumpQuest:     a.inspectQuestFromOtherTab,
		onJumpLeve:      a.inspectLeveFromOtherTab,
	})
}

func (a *App) renderItemDetailWithOpts(c *ui.Context, opts itemDetailPaneOpts) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Grow(1).Shrink(1).FillHeight().Children(func() {
		item := opts.detail
		if item == nil {
			kitEmpty(c, icoPackage, a.t("detail.empty_title"), a.t("detail.empty_desc"), nil)
			return
		}

		onJumpItem := opts.onJumpItem
		if onJumpItem == nil {
			onJumpItem = a.jumpToItem
		}
		onJumpQuest := opts.onJumpQuest
		if onJumpQuest == nil {
			onJumpQuest = a.inspectQuestFromOtherTab
		}
		onJumpLeve := opts.onJumpLeve
		if onJumpLeve == nil {
			onJumpLeve = a.inspectLeveFromOtherTab
		}
		activeRecipeIdx := opts.activeRecipeIdx
		if activeRecipeIdx == nil {
			activeRecipeIdx = &a.activeRecipeIdx
		}
		scroll := opts.scroll
		if scroll == nil {
			scroll = &a.detailScroll
		}

		isHQ := item.CanBeHQ && a.showHQ

		// 顶部工具条：常驻物品 ID 与基本信息 / 跳转主页面 + 收藏按钮 + 复制名称 + NQ/HQ 切换
		ui.Row(c).FillWidth().Height(42).Padding(0, 16).Gap(10).
			BorderWidth(0, 0, hairline, 0).BorderColor(t.border).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).ClipX().Children(func() {
				numText(c, a.tf("detail.item_id", "id", fmt.Sprintf("%d", item.ID))).FontSize(t.text.xs).TextColor(t.muted).SingleLine().Shrink(0)
				if opts.showOpenInMain {
					if kitIconButton(c, opts.keyPrefix+"-open-main", icoExternalLink, a.t("recent.open_in_items"), btnOpts{variant: btnGhost, size: btnSizeXs}) {
						a.inspectItemFromOtherTab(item.ID)
					}
				}
				var barMeta []string
				if item.CategoryName != "" {
					barMeta = append(barMeta, item.CategoryName)
				}
				if item.ItemLevel > 0 {
					barMeta = append(barMeta, a.tf("detail.ilvl", "ilvl", fmt.Sprintf("%d", item.ItemLevel)))
				}
				if len(barMeta) > 0 {
					numText(c, "· "+strings.Join(barMeta, " · ")).FontSize(t.text.xs).TextColor(t.muted).SingleLine().Shrink(1).MinWidth(0)
				}
			})

			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
				fav := a.isFavorite(config.RecordKindItem, item.ID)
				if kitFavoriteToggleButton(c, opts.keyPrefix+"-fav-btn", a.t("favorites.add_btn"), a.t("favorites.added_btn"), fav) {
					a.toggleFavorite(config.RecordKindItem, item.ID)
				}

				copied := a.copiedItemID == item.ID && time.Now().Before(a.copiedUntil)
				copyLabel := a.t("detail.copy_name")
				copyIco := icoCopy
				if copied {
					copyLabel = a.t("detail.copied")
					copyIco = icoCheck
				}
				if kitButton(c, opts.keyPrefix+"-copy-name", copyLabel, btnOpts{variant: btnOutline, size: btnSizeSm, icon: copyIco}) {
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
					if nextIdx, chg := kitSegmentedTabs(c, opts.keyPrefix+"-hq-toggle", selIdx, []string{a.t("common.nq"), a.t("common.hq")}); chg {
						a.showHQ = nextIdx == 1
					}
				}
			})
		})

		// 主详情滚动阅读区
		ui.Scroll(c.Key(opts.keyPrefix + "-scroll")).TrackScroll(scroll).Grow(1).Shrink(1).FillWidth().Children(func() {
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
						a.renderCollectableTurnInsSection(c, item.NameCn, item.CollectableTurnIns, onJumpItem)
					}

					if hasColReward {
						a.renderCollectableRewardsSection(c, item.NameCn, item.CollectableRewards, onJumpItem)
					}

					if hasAch {
						a.renderRewardAchievementsSection(c, item.RewardedFromAchievements)
					}

					if hasQuestSrc {
						a.renderQuestSourcesSection(c, item.RewardedFromQuests, onJumpQuest)
					}

					if hasLeveTurnIn {
						a.renderLeveSourcesSection(c, a.t("detail.leve_turnins"), item.TurnInForLeves, true, onJumpLeve)
					}

					if hasLeveReward {
						a.renderLeveSourcesSection(c, a.t("detail.leve_rewards"), item.RewardedFromLeves, false, onJumpLeve)
					}

					if hasShops {
						a.renderShopsSection(c, item.NameCn, item.Shops, onJumpItem)
					}

					if hasRecipes {
						a.renderRecipesSection(c, opts.keyPrefix, item, activeRecipeIdx, onJumpItem)
					}

					if hasGather {
						a.renderGatheringSections(c, item.NameCn, item.Gathering)
					}

					if hasRetainer {
						a.renderRetainerSection(c, item.RetainerVentures)
					}

					if hasUsedIn {
						a.renderUsedInRecipesSection(c, item.UsedInRecipes, onJumpItem)
					}

					if !hasShops && !hasRecipes && !hasUsedIn && !hasGather && !hasRetainer && !hasAch &&
						!hasColTurnIn && !hasColReward && !hasQuestSrc && !hasLeveTurnIn && !hasLeveReward {
						ui.Row(c).FillWidth().Padding(16, 0, 0, 0).Gap(8).AlignItems(ui.Center).
							BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
							ui.Icon(c, icoInfo).Size(iconSm, iconSm).Shrink(0).TextColor(t.muted)
							ui.Text(c, a.t("detail.no_source")).
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
					if a.isNewItem(item.ID) {
						a.drawLatestBadge(c)
					}
					if isHQ {
						drawPill(c, "HQ", pillWarning)
					}
					numTextf(c, "#%d", item.ID).FontSize(t.text.xs).TextColor(t.muted).Selectable()
				})

				if secLines := a.secondaryDataLines(item.NameCn, item.NameJa, item.NameEn); len(secLines) > 0 {
					ui.Row(c).Gap(12).Wrap().Children(func() {
						for _, sec := range secLines {
							ui.Textf(c, "%s  %s", sec.badge, sec.text).FontSize(t.text.xs).TextColor(t.muted).Selectable()
						}
					})
				}

				// 结构化内联元数据行：用纯排版替代原本 6+ 个无意义彩色胶囊
				metaParts := []string{
					item.CategoryName,
					a.tf("detail.ilvl", "ilvl", fmt.Sprintf("%d", item.ItemLevel)),
				}
				if item.EquipLevel > 0 {
					metaParts = append(metaParts, a.tf("detail.equip_lv", "lv", fmt.Sprintf("%d", item.EquipLevel)))
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
					specialFlags = append(specialFlags, flagTag{a.t("detail.untradable"), pillNegative})
				}
				if !item.IsMarketable {
					specialFlags = append(specialFlags, flagTag{a.t("detail.unmarketable"), pillWarning})
				}
				if !item.IsSellable {
					specialFlags = append(specialFlags, flagTag{a.t("detail.unsellable"), pillNegative})
				}
				if item.IsUnique {
					specialFlags = append(specialFlags, flagTag{a.t("detail.unique"), pillWarning})
				}
				if item.IsCollectable {
					specialFlags = append(specialFlags, flagTag{a.t("detail.collectable"), pillAccent})
				}
				if item.CanAetherialReduce {
					specialFlags = append(specialFlags, flagTag{a.t("detail.aetherial_reduce"), pillNeutral})
				}
				if item.IsTradable && item.IsMarketable {
					metaParts = append(metaParts, a.t("detail.tradable_marketable"))
				} else if item.IsTradable {
					metaParts = append(metaParts, a.t("detail.tradable"))
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
		thirdLabel := a.t("detail.obtain_methods")
		thirdValue := ""
		if showRepair {
			thirdLabel = a.t("detail.repair_job")
			thirdValue = item.RepairClassJobName
			if item.RepairItemName != "" {
				thirdValue += formatParenMeta(a.Language(), item.RepairItemName)
			}
		} else {
			var srcParts []string
			if item.IsGilShop {
				srcParts = append(srcParts, a.t("detail.src_gil"))
			}
			if item.IsSpecialShop {
				srcParts = append(srcParts, a.t("detail.src_special"))
			}
			if item.IsCraftable {
				srcParts = append(srcParts, a.t("detail.src_craft"))
			}
			if item.IsGatherable {
				srcParts = append(srcParts, a.t("detail.src_gather"))
			}
			if item.IsRetainerVenture {
				srcParts = append(srcParts, a.t("detail.src_retainer"))
			}
			if len(item.CollectableTurnIns) > 0 {
				srcParts = append(srcParts, a.t("detail.src_collectable_turnin"))
			}
			if len(item.CollectableRewards) > 0 {
				srcParts = append(srcParts, a.t("detail.src_collectable_reward"))
			}
			if len(item.RewardedFromQuests) > 0 {
				srcParts = append(srcParts, a.t("detail.src_quest"))
			}
			if len(item.TurnInForLeves) > 0 || len(item.RewardedFromLeves) > 0 {
				srcParts = append(srcParts, a.t("detail.src_leve"))
			}
			if hasAch {
				srcParts = append(srcParts, a.t("detail.src_achievement"))
			}
			if len(srcParts) == 0 {
				thirdValue = a.t("detail.src_dungeon_event")
			} else {
				thirdValue = strings.Join(srcParts, " · ")
			}
		}

		sellStr := a.t("detail.unsellable")
		if item.IsSellable {
			sellStr = a.tf("common.gil_amount", "amount", formatInt(item.PriceLow))
		}

		kitBlock(c, ui.Row(c).FillWidth().Clip(), toneCard).Children(func() {
			drawMetricSplitCell(c, a.t("detail.stack_size"), a.tf("common.count_pieces", "count", formatInt(item.StackSize)), false)
			drawMetricSplitCell(c, a.t("detail.sell_price"), sellStr, true)
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
		meld := a.t("detail.meld_no")
		if st.IsAdvancedMeldingPermitted {
			meld = a.t("detail.meld_yes")
		}
		meta = a.tf("detail.materia_meta", "count", fmt.Sprintf("%d", st.MateriaSlotCount), "meld", meld)
	}
	drawDetailSection(c, a.t("detail.equip_stats"), meta, nil, func() {
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
				core = append(core, coreStat{a.t("detail.phys_dmg"), fmt.Sprintf("%d", v), sub, t.foreground})
			}
			if st.DamageMag > 0 {
				v, sub := st.DamageMag, ""
				if isHQ && st.DamageMagHQ > st.DamageMag {
					v, sub = st.DamageMagHQ, fmt.Sprintf("NQ %d", st.DamageMag)
				}
				core = append(core, coreStat{a.t("detail.mag_dmg"), fmt.Sprintf("%d", v), sub, t.infoText})
			}
			if st.AutoAttack > 0 {
				v, sub := st.AutoAttack, ""
				if isHQ && st.AutoAttackHQ > st.AutoAttack {
					v, sub = st.AutoAttackHQ, fmt.Sprintf("NQ %.2f", st.AutoAttack)
				}
				core = append(core, coreStat{a.t("detail.auto_attack"), fmt.Sprintf("%.2f", v), sub, t.foreground})
			}
			if st.DelaySec > 0 {
				core = append(core, coreStat{a.t("detail.delay"), fmt.Sprintf("%.2fs", st.DelaySec), "", t.foreground})
			}
			if st.BlockRate > 0 {
				v, sub := st.BlockRate, ""
				if isHQ && st.BlockRateHQ > st.BlockRate {
					v, sub = st.BlockRateHQ, fmt.Sprintf("NQ %d", st.BlockRate)
				}
				core = append(core, coreStat{a.t("detail.block_rate"), fmt.Sprintf("%d", v), sub, t.foreground})
			}
			if st.Block > 0 {
				v, sub := st.Block, ""
				if isHQ && st.BlockHQ > st.Block {
					v, sub = st.BlockHQ, fmt.Sprintf("NQ %d", st.Block)
				}
				core = append(core, coreStat{a.t("detail.block_str"), fmt.Sprintf("%d", v), sub, t.foreground})
			}
			if st.DefensePhys > 0 {
				v, sub := st.DefensePhys, ""
				if isHQ && st.DefensePhysHQ > st.DefensePhys {
					v, sub = st.DefensePhysHQ, fmt.Sprintf("NQ %d", st.DefensePhys)
				}
				core = append(core, coreStat{a.t("detail.phys_def"), fmt.Sprintf("%d", v), sub, t.foreground})
			}
			if st.DefenseMag > 0 {
				v, sub := st.DefenseMag, ""
				if isHQ && st.DefenseMagHQ > st.DefenseMag {
					v, sub = st.DefenseMagHQ, fmt.Sprintf("NQ %d", st.DefenseMag)
				}
				core = append(core, coreStat{a.t("detail.mag_def"), fmt.Sprintf("%d", v), sub, t.foreground})
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
	drawDetailSection(c, a.t("detail.equip_attrs"), "", nil, func() {
		desynthTxt := a.t("detail.desynth_no")
		if attr.CanDesynth {
			desynthTxt = a.t("detail.desynth_yes")
			if attr.DesynthClassJobName != "" {
				desynthTxt = a.tf("detail.desynth_job", "job", attr.DesynthClassJobName)
			}
		}
		dyeTxt := a.t("detail.dye_no")
		if attr.DyeCount > 0 {
			dyeTxt = a.tf("detail.dye_yes", "count", fmt.Sprintf("%d", attr.DyeCount))
		}
		entries := []struct {
			text string
			pos  bool
		}{
			{boolText(attr.CanExtractMateria, a.t("detail.extract_yes"), a.t("detail.extract_no")), attr.CanExtractMateria},
			{boolText(attr.IsProjectable, a.t("detail.glamour_yes"), a.t("detail.glamour_no")), attr.IsProjectable},
			{desynthTxt, attr.CanDesynth},
			{dyeTxt, attr.DyeCount > 0},
			{boolText(attr.IsCrestWorthy, a.t("detail.crest_yes"), a.t("detail.crest_no")), attr.IsCrestWorthy},
			{boolText(attr.CanStoreInGlamourDresser, a.t("detail.dresser_yes"), a.t("detail.dresser_no")), attr.CanStoreInGlamourDresser},
			{boolText(attr.CanStoreInArmoire, a.t("detail.armoire_yes"), a.t("detail.armoire_no")), attr.CanStoreInArmoire},
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
	durTxt := a.tf("detail.dur_sec", "sec", ce.DurationSec)
	if ce.DurationSec/60 > 0 {
		durTxt = a.tf("detail.dur_min", "min", ce.DurationSec/60)
	}
	meta := a.tf("detail.consumable_meta", "dur", durTxt)
	if ce.CooldownSec > 0 {
		cd := ce.CooldownSec
		if isHQ {
			cd = ce.CooldownHQSec
		}
		meta += a.tf("detail.consumable_recast", "sec", cd)
	}
	var trailing func()
	if ce.ExpBonusPercent > 0 {
		expTxt := a.tf("detail.exp_bonus", "pct", ce.ExpBonusPercent)
		trailing = func() {
			drawPill(c, expTxt, pillPositive)
		}
	}

	sectionTitle := a.t("detail.consumable_title_food")
	if ce.TypeName == a.t("gamedata.consume_med") || ce.TypeName == "药品" || ce.TypeName == "Medicine" || ce.TypeName == "薬品" {
		sectionTitle = a.t("detail.consumable_title_med")
	}

	drawDetailSection(c, sectionTitle, meta, trailing, func() {
		ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
			for _, p := range ce.Params {
				val, mx := p.ValueNQ, p.MaxNQ
				if isHQ {
					val, mx = p.ValueHQ, p.MaxHQ
				}
				mainStr := fmt.Sprintf("+%d", val)
				if p.IsRelative {
					mainStr = a.tf("detail.consumable_cap", "val", val, "max", mx)
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
								alt = a.tf("detail.consumable_nq_cap", "val", p.ValueNQ, "max", p.MaxNQ)
							}
						} else if p.IsRelative {
							alt = a.tf("detail.consumable_hq_cap", "val", p.ValueHQ, "max", p.MaxHQ)
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
	drawDetailSection(c, a.t("detail.ach_rewards"), a.tf("common.items_count", "count", fmt.Sprintf("%d", len(achs))), nil, func() {
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
								numText(c, a.tf("detail.ach_points_meta", "id", fmt.Sprintf("%d", ach.ID), "pts", fmt.Sprintf("%d", ach.Points))).FontSize(t.text.micro).TextColor(t.muted)
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

func (a *App) renderShopsSection(c *ui.Context, itemName string, shops []gamedata.ShopPurchaseSource, onJumpItem func(int)) {
	t := themeOf(c)
	drawDetailSection(c, a.t("detail.shops"), a.tf("common.items_count", "count", fmt.Sprintf("%d", len(shops))), nil, func() {
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
							numText(c, a.tf("detail.shop_receive", "count", fmt.Sprintf("%d", shop.ReceiveAmount))).FontSize(t.text.xs).TextColor(t.muted)
							if shop.ReceiveHQ {
								drawPill(c, a.t("common.hq"), pillWarning)
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
								btn := kitLinkSurface(c, ui.ButtonBase(c.Key(fmt.Sprintf("cost-%d-%d-%d", shop.ShopID, sIdx, cIdx))).
									Label(a.tf("common.view_item", "name", cost.NameCn)).
									Padding(3, 8).Radius(t.radius.md))
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
									onJumpItem(cost.ItemID)
								}
							}
						})
					})

					// 第二行：售卖 NPC 与坐标按钮
					a.renderVendorNpcChips(c, fmt.Sprintf("npcmap-%d-%d", shop.ShopID, sIdx), fmt.Sprintf("%s · %s", shop.ShopName, itemName), a.t("detail.shop_generic_npc"), shop.NPCs)
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
		btn := kitLinkSurface(c, ui.ButtonBase(c.Key(key)).Label(a.tf("common.view_map", "coord", coordLbl)).
			Height(24).Padding(0, 8).Radius(t.radius.md).Gap(5).
			AlignItems(ui.Center))
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
				btn := kitLinkSurface(c, ui.ButtonBase(c.Key(fmt.Sprintf("%s-%d", keyPrefix, npc.NpcID))).
					Label(a.tf("common.view_map", "coord", coordLbl)).
					Height(24).Padding(0, 8).Radius(t.radius.md).Gap(5).
					AlignItems(ui.Center))
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
			numText(c, a.tf("detail.more_npcs", "count", fmt.Sprintf("%d", len(npcs)))).FontSize(t.text.micro).TextColor(t.muted)
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

func (a *App) renderCollectableTurnInsSection(c *ui.Context, itemName string, list []gamedata.CollectableTurnInSource, onJumpItem func(int)) {
	t := themeOf(c)
	drawDetailSection(c, a.t("detail.col_turnin_title"), a.tf("common.items_count", "count", fmt.Sprintf("%d", len(list))), nil, func() {
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
								drawPill(c, a.t("detail.col_bonus"), pillWarning)
							}
						})
						var metas []string
						if src.LevelMin > 0 && src.LevelMax > src.LevelMin {
							metas = append(metas, fmt.Sprintf("Lv.%d~%d", src.LevelMin, src.LevelMax))
						} else if src.LevelMin > 0 {
							metas = append(metas, fmt.Sprintf("Lv.%d", src.LevelMin))
						}
						if src.Stars > 0 {
							metas = append(metas, a.tf("common.stars", "stars", fmt.Sprintf("%d", src.Stars)))
						}
						if src.RequiredQuestName != "" {
							metas = append(metas, a.tf("detail.col_req_quest", "name", src.RequiredQuestName))
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
									numText(c, a.tf("detail.col_tier_val", "tier", tier.Label, "val", fmt.Sprintf("%d", tier.MinCollectability))).
										FontSize(t.text.micro).TextColor(t.muted).SingleLine()

									if tier.RewardItemID > 0 && tier.RewardAmount > 0 {
										btn := ui.ButtonBase(c.Key(fmt.Sprintf("coltier-%d-%d-%d", src.ShopID, idx, ti))).
											Label(a.tf("common.view_item", "name", tier.RewardItemName)).Padding(2, 4).Radius(t.radius.sm).
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
											onJumpItem(tier.RewardItemID)
										}
									}

									var extras []string
									if tier.SecondaryItemID > 0 && tier.SecondaryAmount > 0 {
										extras = append(extras, fmt.Sprintf("%s ×%d", tier.SecondaryItemName, tier.SecondaryAmount))
									}
									if tier.GilReward > 0 {
										extras = append(extras, a.tf("common.gil_amount", "amount", formatInt(tier.GilReward)))
									}
									if tier.Satisfaction > 0 {
										extras = append(extras, a.tf("detail.col_satisfaction_up", "val", fmt.Sprintf("%d", tier.Satisfaction)))
									}
									if tier.ExpReward > 0 {
										extras = append(extras, a.tf("common.exp_amount", "amount", formatInt(tier.ExpReward)))
									} else if tier.ExpRatio > 0 {
										extras = append(extras, a.tf("detail.col_exp_ratio", "ratio", fmt.Sprintf("%d", tier.ExpRatio)))
									}
									if tier.Points > 0 {
										extras = append(extras, a.tf("detail.col_points", "pts", fmt.Sprintf("%d", tier.Points)))
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

func (a *App) renderCollectableRewardsSection(c *ui.Context, itemName string, list []gamedata.CollectableRewardSource, onJumpItem func(int)) {
	t := themeOf(c)
	drawDetailSection(c, a.t("detail.col_reward_title"), a.tf("common.items_count", "count", fmt.Sprintf("%d", len(list))), nil, func() {
		ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
			for idx, rew := range list {
				btn := kitLinkSurface(c, ui.ButtonBase(c.Key(fmt.Sprintf("colrew-%d-%d", rew.TurnInItemID, idx))).
					Label(a.tf("common.view_item", "name", rew.TurnInItemName)).MinWidth(250).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
					Justify(ui.SpaceBetween).AlignItems(ui.Center))
				btn.Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						a.drawGameIcon(c, rew.TurnInItemIconID, false, 26, t.radius.md)
						ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
							ui.Text(c, rew.TurnInItemName).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
							sub := rew.ShopName
							if rew.MinCollectability > 0 {
								sub += a.tf("detail.col_min_val", "val", fmt.Sprintf("%d", rew.MinCollectability))
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
					onJumpItem(rew.TurnInItemID)
				}
			}
		})
	})
}

func (a *App) renderQuestSourcesSection(c *ui.Context, quests []gamedata.ItemQuestSource, onJumpQuest func(int)) {
	t := themeOf(c)
	drawDetailSection(c, a.t("detail.quest_rewards"), a.tf("common.items_count", "count", fmt.Sprintf("%d", len(quests))), nil, func() {
		ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
			for idx, qs := range quests {
				btn := kitLinkSurface(c, ui.ButtonBase(c.Key(fmt.Sprintf("item-qsrc-%d-%d", qs.QuestID, idx))).
					Label(a.tf("common.view_quest", "name", qs.NameCn)).MinWidth(240).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
					Justify(ui.SpaceBetween).AlignItems(ui.Center))
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
					onJumpQuest(qs.QuestID)
				}
			}
		})
	})
}

func (a *App) renderLeveSourcesSection(c *ui.Context, title string, leves []gamedata.ItemLeveSource, isTurnIn bool, onJumpLeve func(int)) {
	t := themeOf(c)
	drawDetailSection(c, title, a.tf("common.items_count", "count", fmt.Sprintf("%d", len(leves))), nil, func() {
		ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
			for idx, ls := range leves {
				btn := kitLinkSurface(c, ui.ButtonBase(c.Key(fmt.Sprintf("item-lsrc-%t-%d-%d", isTurnIn, ls.LeveID, idx))).
					Label(a.tf("common.view_leve", "name", ls.NameCn)).MinWidth(240).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
					Justify(ui.SpaceBetween).AlignItems(ui.Center))
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
					onJumpLeve(ls.LeveID)
				}
			}
		})
	})
}

func (a *App) renderRecipesSection(c *ui.Context, keyPrefix string, item *gamedata.ItemDetail, activeRecipeIdx *int, onJumpItem func(int)) {
	t := themeOf(c)
	if *activeRecipeIdx >= len(item.Recipes) {
		*activeRecipeIdx = 0
	}
	cur := item.Recipes[*activeRecipeIdx]
	var curTree *gamedata.RecipeTreeNode
	if *activeRecipeIdx < len(item.RecipeTrees) {
		curTree = &item.RecipeTrees[*activeRecipeIdx]
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
			if nextIdx, chg := kitSegmentedTabs(c, fmt.Sprintf("%s-rec-%d", keyPrefix, item.ID), *activeRecipeIdx, labels); chg {
				*activeRecipeIdx = nextIdx
			}
		}
	}

	meta := a.tf("detail.recipes_jobs_meta", "count", len(item.Recipes))
	if len(item.Recipes) == 1 {
		meta = ""
	}
	drawDetailSection(c, a.t("detail.recipes_title"), meta, trailing, func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			// 配方头部摘要
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				a.drawGameIcon(c, cur.CraftJobIconID, false, 32, t.radius.md)
				ui.Column(c).Gap(3).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Baseline).Wrap().Children(func() {
						ui.Textf(c, "%s · Lv.%d%s", cur.CraftJobName, cur.RecipeLevel, formatStarsForLang(a.Language(), cur.Stars)).
							FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
						if cur.ResultAmount > 1 {
							numText(c, a.tf("detail.recipe_yield", "count", cur.ResultAmount)).FontSize(t.text.xs).FontWeight(500).TextColor(t.successText)
						}
						numTextf(c, "Recipe #%d", cur.ID).FontSize(t.text.micro).TextColor(t.muted)
					})
					if cur.SecretRecipeBookName != "" || cur.IsSpecializationRequired || cur.IsExpert || cur.CanQuickSynth {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
							if cur.SecretRecipeBookName != "" {
								drawPill(c, a.tf("detail.recipe_book", "name", cur.SecretRecipeBookName), pillWarning)
							}
							if cur.IsSpecializationRequired {
								drawPill(c, a.t("detail.recipe_specialist"), pillNegative)
							}
							if cur.IsExpert {
								drawPill(c, a.t("detail.recipe_expert"), pillWarning)
							}
							if cur.CanQuickSynth {
								ui.Text(c, a.t("detail.recipe_quick_synth")).FontSize(t.text.micro).TextColor(t.muted)
							}
						})
					}
				})
			})

			// 6 项制作参数：单层轻底色自适应流式指标条（窄屏优雅折行，绝不挤爆）
			reqCraftLabel := a.t("detail.recipe_sug_craft")
			reqCraftVal := cur.SuggestedCraftsmanship
			if cur.RequiredCraftsmanship > 0 {
				reqCraftLabel = a.t("detail.recipe_req_craft")
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
				{a.t("detail.recipe_difficulty"), fmt.Sprintf("%d", cur.Difficulty), t.foreground},
				{a.t("detail.recipe_quality"), fmt.Sprintf("%d", cur.Quality), t.infoText},
				{a.t("detail.recipe_durability"), fmt.Sprintf("%d", cur.Durability), t.successText},
				{reqCraftLabel, fmt.Sprintf("%d", reqCraftVal), t.foreground},
				{a.t("detail.recipe_req_ctrl"), reqCtrlStr, t.foreground},
				{a.t("detail.recipe_init_qual"), fmt.Sprintf("%d%%", cur.MaterialQualityFactor), t.foreground},
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
					ui.Text(c, a.t("detail.recipe_materials")).FontSize(t.text.xs).FontWeight(500).TextColor(t.muted).Padding(2, 0)
					for idx, ch := range curTree.Children {
						a.renderRecipeTreeNode(c, ch, 1, 0, fmt.Sprintf("rt-%d-%d", cur.ID, idx), onJumpItem)
					}
					for idx, cry := range curTree.Crystals {
						a.renderCrystalRow(c, cry, 1, fmt.Sprintf("rc-%d-%d", cur.ID, idx), onJumpItem)
					}
				})
			}
		})
	})
}

func (a *App) renderRecipeTreeNode(c *ui.Context, node gamedata.RecipeTreeNode, parentCrafts int, depth int, keyPath string, onJumpItem func(int)) {
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
					numTextf(c, "%s Lv.%d%s", node.CraftJobName, node.RecipeLevel, formatStarsForLang(a.Language(), node.Stars)).
						FontSize(t.text.micro).TextColor(t.muted).SingleLine()
				} else if node.IsGatherable {
					ui.Text(c, a.t("detail.gatherable")).FontSize(t.text.micro).TextColor(t.successText).SingleLine()
				}
			})
			numTextf(c, "×%d", totalNeeded).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine().Shrink(0)
		})
		if row.Clicked() {
			onJumpItem(node.ItemID)
		}

		if len(node.Children) > 0 || len(node.Crystals) > 0 {
			ui.Column(c).FillWidth().Margin(0, 0, 0, 14).Padding(0, 0, 0, 10).Gap(3).
				BorderWidth(0, 0, 0, hairline).BorderColor(t.border).Children(func() {
				for idx, ch := range node.Children {
					a.renderRecipeTreeNode(c, ch, craftsNeeded, depth+1, fmt.Sprintf("%s-c%d", keyPath, idx), onJumpItem)
				}
				for idx, cry := range node.Crystals {
					a.renderCrystalRow(c, cry, craftsNeeded, fmt.Sprintf("%s-k%d", keyPath, idx), onJumpItem)
				}
			})
		}
	})
}

func (a *App) renderCrystalRow(c *ui.Context, cry gamedata.RecipeCrystalNode, crafts int, key string, onJumpItem func(int)) {
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
		onJumpItem(cry.ItemID)
	}
}

func (a *App) renderGatheringSections(c *ui.Context, itemName string, g gamedata.GatheringMethodDetail) {
	t := themeOf(c)
	now := time.Now()

	// 1. 采矿 / 园艺采集点
	if len(g.Nodes) > 0 {
		drawDetailSection(c, a.t("detail.gather_nodes"), a.tf("detail.gather_places_count", "count", len(g.Nodes)), nil, func() {
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
										numTextf(c, "Lv.%d%s", node.NodeLevel, formatStarsForLang(a.Language(), node.Stars)).FontSize(t.text.xs).FontWeight(500).TextColor(t.successText)
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
									ts := gamedata.EvaluateGatheringWindowsForLang(node.TimeWindows, now, a.Language())
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
									drawPill(c, a.tf("detail.folklore", "name", node.FolkloreBookName), pillWarning)
								}
								if node.IsHidden {
									drawPill(c, a.t("detail.hidden"), pillNeutral)
								}
								if node.IsEphemeral {
									drawPill(c, a.t("detail.ephemeral"), pillAccent)
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
		drawDetailSection(c, a.t("detail.fishing_spots"), a.tf("detail.gather_places_count", "count", len(g.FishingSpots)), nil, func() {
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
										sub += a.tf("detail.need_folklore", "name", sp.FolkloreBookName)
									}
									ui.Text(c, sub).FontSize(t.text.xs).TextColor(t.muted)
								})
							})
							if sp.MapPath != "" {
								if kitButton(c, fmt.Sprintf("fspot-map-%d", sp.SpotID), fmt.Sprintf("%.1f, %.1f", sp.CoordX, sp.CoordY), btnOpts{variant: btnOutline, size: btnSizeXs, icon: icoMapPin}) {
									a.openMap(MapMarkerData{
										Title:      a.tf("detail.map_fishing_title", "item", itemName, "place", sp.PlaceName),
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
		drawDetailSection(c, a.t("detail.spearfishing"), a.tf("detail.gather_places_count", "count", len(g.Spearfishing)), nil, func() {
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
										Title:      a.tf("detail.map_spearfishing_title", "item", itemName, "place", sf.PlaceName),
										Subtitle:   a.tf("detail.map_spearfishing_sub", "lv", sf.Level),
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

func (a *App) tierStatName(statType string) string {
	switch statType {
	case "ilvl":
		return a.t("detail.stat_ilvl")
	case "gathering":
		return a.t("detail.stat_gathering")
	default:
		return a.t("detail.stat_perception")
	}
}

func (a *App) formatCompactTierReq(tier gamedata.RetainerVentureTier) string {
	if tier.RequiredStat <= 0 {
		return a.t("detail.tier_base")
	}
	return fmt.Sprintf("≥ %d", tier.RequiredStat)
}

func (a *App) renderRetainerSection(c *ui.Context, ventures []gamedata.RetainerVentureSource) {
	t := themeOf(c)
	drawDetailSection(c, a.t("detail.retainer_title"), a.tf("common.items_count", "count", fmt.Sprintf("%d", len(ventures))), nil, func() {
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
								drawPill(c, a.t("detail.retainer_unlock"), pillNeutral)
							}
						})
						statLabel := ""
						if len(v.Tiers) > 0 {
							statLabel = a.tf("detail.retainer_tier_prefix", "stat", a.tierStatName(v.Tiers[0].StatType))
						}
						numText(c, a.tf("detail.retainer_meta",
							"stat", statLabel,
							"lv", v.RetainerLevel,
							"min", v.DurationMinutes,
							"cost", v.VentureCost,
							"exp", formatInt(v.Experience))).
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
									numText(c, a.formatCompactTierReq(tier)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
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

func (a *App) renderUsedInRecipesSection(c *ui.Context, used []gamedata.UsedInRecipeItem, onJumpItem func(int)) {
	t := themeOf(c)
	drawDetailSection(c, a.t("detail.used_in"), a.tf("common.items_count", "count", fmt.Sprintf("%d", len(used))), nil, func() {
		ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
			for _, u := range used {
				btn := kitLinkSurface(c, ui.ButtonBase(c.Key(fmt.Sprintf("used-%d", u.RecipeID))).Label(u.ResultNameCn).
					MinWidth(210).Grow(1).Shrink(1).Padding(7, 10).Radius(t.radius.lg).
					Justify(ui.SpaceBetween).AlignItems(ui.Center))
				btn.Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						a.drawGameIcon(c, u.ResultIconID, false, 26, t.radius.md)
						ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
							ui.Text(c, u.ResultNameCn).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine()
							numTextf(c, "%s Lv.%d%s", u.CraftJobName, u.RecipeLevel, formatStarsForLang(a.Language(), u.Stars)).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
						})
					})
					numTextf(c, "×%d", u.AmountRequired).FontSize(t.text.xs).FontWeight(600).TextColor(t.infoText).SingleLine().Shrink(0)
				})
				if btn.Clicked() {
					onJumpItem(u.ResultItemID)
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
