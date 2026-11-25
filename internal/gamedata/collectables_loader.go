package gamedata

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

type collectablesLoaderInput struct {
	appLang                i18n.Lang
	shops                  *sqpack.SchemaSheet
	shopItems              *sqpack.SchemaSubrowSheet
	shopItemGroups         *sqpack.SchemaSheet
	shopRefines            *sqpack.SchemaSheet
	shopRewardItems        *sqpack.SchemaSheet
	shopRewardScrips       *sqpack.SchemaSheet
	satisfactionNpcs       *sqpack.SchemaSheet
	satisfactionSupplies   *sqpack.SchemaSubrowSheet
	satisfactionRewards    *sqpack.SchemaSheet
	hwdSupplies            *sqpack.SchemaSheet
	hwdRewards             *sqpack.SchemaSheet
	hwdTerms               *sqpack.SchemaSheet
	quests                 *sqpack.SchemaSheet
	collectablesShopToNPCs map[int][]int
	npcNames               map[int]RawNpcName
	npcLocations           map[int]RawNpcLocation
	levelLocations         map[int]RawNpcLocation
	tomestoneByIndex       map[int]int
	placeNames             map[int]string
	territoryTypes         map[int]sqpack.Row
	maps                   map[int]sqpack.Row
	itemMeta               func(int) (string, int)
}

type collectablesIndices struct {
	turnInsByItem map[int][]CollectableTurnInSource
	rewardsByItem map[int][]CollectableRewardSource
	totalEntries  int
}

func resolveCollectableCurrency(appLang i18n.Lang, rawID int, tomestoneByIndex map[int]int, itemMeta func(int) (string, int)) (int, string, int) {
	if rawID <= 0 {
		return 0, "", 0
	}
	itemID := resolveSpecialCurrencyID(rawID, 16, tomestoneByIndex)
	name, iconID := itemMeta(itemID)
	if name == "" {
		switch itemID {
		case 33913:
			name = i18n.T(appLang, "gamedata.col.scrip_purple_crafter")
		case 33914:
			name = i18n.T(appLang, "gamedata.col.scrip_purple_gatherer")
		case 41784:
			name = i18n.T(appLang, "gamedata.col.scrip_orange_crafter")
		case 41785:
			name = i18n.T(appLang, "gamedata.col.scrip_orange_gatherer")
		case 28063:
			name = i18n.T(appLang, "gamedata.col.scrip_skybuilders")
		default:
			name = i18n.Tf(appLang, "gamedata.scrip_fallback", "id", fmt.Sprintf("%d", itemID))
		}
	}
	return itemID, name, iconID
}

func buildCollectablesData(in collectablesLoaderInput) collectablesIndices {
	in.appLang = resolveAppLang(in.appLang)
	out := collectablesIndices{
		turnInsByItem: make(map[int][]CollectableTurnInSource),
		rewardsByItem: make(map[int][]CollectableRewardSource),
	}

	readQuestName := func(qID int) string {
		if qID <= 0 || in.quests == nil {
			return ""
		}
		if row, ok := in.quests.Rows[qID]; ok {
			return strings.TrimSpace(in.quests.Str(row, "Name"))
		}
		return ""
	}

	resolveItem := func(itemID int) (string, int) {
		if in.itemMeta != nil {
			return in.itemMeta(itemID)
		}
		return "", 0
	}

	resolveNPCs := func(npcIDs []int) []ShopVendorNpc {
		var vendors []ShopVendorNpc
		seen := make(map[int]bool)
		for _, npcID := range npcIDs {
			if seen[npcID] || len(vendors) >= 12 {
				continue
			}
			v, ok := resolveVendorNpc(npcID, in.npcNames, in.npcLocations, in.territoryTypes, in.maps, in.placeNames)
			if ok {
				seen[npcID] = true
				vendors = append(vendors, v)
			}
		}
		return vendors
	}

	recordRewardReverse := func(turnInItemID int, src CollectableTurnInSource) {
		if len(src.Tiers) == 0 {
			return
		}
		tName, tIcon := resolveItem(turnInItemID)
		if tName == "" {
			tName = i18n.Tf(in.appLang, "gamedata.item_fallback", "id", fmt.Sprintf("%d", turnInItemID))
		}

		// 每个目标奖励道具收集高/中/低三档回报量
		type rewardAgg struct {
			minCol int
			low    int
			mid    int
			high   int
		}
		byReward := make(map[int]*rewardAgg)
		var rewOrder []int
		addTierAmt := func(itemID, minCol, tierIdx, amt int) {
			if itemID <= 0 || amt <= 0 {
				return
			}
			agg, ok := byReward[itemID]
			if !ok {
				agg = &rewardAgg{minCol: minCol}
				byReward[itemID] = agg
				rewOrder = append(rewOrder, itemID)
			} else if minCol > 0 && (agg.minCol == 0 || minCol < agg.minCol) {
				agg.minCol = minCol
			}
			switch tierIdx {
			case 0:
				if amt > agg.low {
					agg.low = amt
				}
			case 1:
				if amt > agg.mid {
					agg.mid = amt
				}
			default:
				if amt > agg.high {
					agg.high = amt
				}
			}
		}

		for ti, tier := range src.Tiers {
			addTierAmt(tier.RewardItemID, tier.MinCollectability, ti, tier.RewardAmount)
			addTierAmt(tier.SecondaryItemID, tier.MinCollectability, ti, tier.SecondaryAmount)
		}

		for _, rewID := range rewOrder {
			agg := byReward[rewID]
			out.rewardsByItem[rewID] = append(out.rewardsByItem[rewID], CollectableRewardSource{
				ShopName:          src.ShopName,
				TurnInItemID:      turnInItemID,
				TurnInItemName:    tName,
				TurnInItemIconID:  tIcon,
				GroupName:         src.GroupName,
				LevelMin:          src.LevelMin,
				LevelMax:          src.LevelMax,
				MinCollectability: agg.minCol,
				RewardLow:         agg.low,
				RewardMid:         agg.mid,
				RewardHigh:        agg.high,
				NPCs:              src.NPCs,
			})
		}
	}

	// 1. 解析 CollectablesShop / CollectablesShopItem（常规收藏品交易）
	if in.shops != nil && in.shopItems != nil {
		type mergedShopItem struct {
			itemID int
			src    CollectableTurnInSource
			npcIDs []int
		}
		merged := make(map[string]*mergedShopItem)
		var mergedOrder []string

		for shopID, sRow := range in.shops.Rows {
			if shopID <= 0 {
				continue
			}
			shopName := strings.TrimSpace(in.shops.Str(sRow, "Name"))
			if shopName == "" {
				shopName = i18n.T(in.appLang, "gamedata.col.shop_default")
			}
			shopQuestID := in.shops.Int(sRow, "Quest")
			rewardType := in.shops.Int(sRow, "RewardType")
			shopNPCs := in.collectablesShopToNPCs[shopID]

			for slot := 0; slot < 11; slot++ {
				groupSubID := in.shops.Int(sRow, fmt.Sprintf("ShopItems[%d]", slot))
				if groupSubID <= 0 {
					continue
				}
				subrows := in.shopItems.Rows[groupSubID]
				for _, sub := range subrows {
					itemID := in.shopItems.Int(sub.Data, "Item")
					if itemID <= 0 {
						continue
					}
					lvlMin := in.shopItems.Int(sub.Data, "LevelMin")
					lvlMax := in.shopItems.Int(sub.Data, "LevelMax")
					stars := in.shopItems.Int(sub.Data, "Stars")
					reqQuest := in.shopItems.Int(sub.Data, "RequiredQuest")
					if reqQuest <= 0 {
						reqQuest = shopQuestID
					}

					groupName := ""
					if grpID := in.shopItems.Int(sub.Data, "CollectablesShopItemGroup"); grpID > 0 && in.shopItemGroups != nil {
						if gRow, ok := in.shopItemGroups.Rows[grpID]; ok {
							groupName = strings.TrimSpace(in.shopItemGroups.Str(gRow, "Name"))
						}
					}

					var lowCol, midCol, highCol int
					if refID := in.shopItems.Int(sub.Data, "CollectablesShopRefine"); refID > 0 && in.shopRefines != nil {
						if rRow, ok := in.shopRefines.Rows[refID]; ok {
							lowCol = in.shopRefines.Int(rRow, "LowCollectability")
							midCol = in.shopRefines.Int(rRow, "MidCollectability")
							highCol = in.shopRefines.Int(rRow, "HighCollectability")
						}
					}

					rewID := in.shopItems.Int(sub.Data, "CollectablesShopRewardScrip")
					if rewID <= 0 {
						continue
					}

					tierLow := i18n.T(in.appLang, "gamedata.col.tier_low")
					tierMid := i18n.T(in.appLang, "gamedata.col.tier_mid")
					tierHigh := i18n.T(in.appLang, "gamedata.col.tier_high")
					var tiers []CollectableTier
					rewardMode := "scrip"
					if rewardType == 2 && in.shopRewardItems != nil {
						rewardMode = "item"
						if riRow, ok := in.shopRewardItems.Rows[rewID]; ok {
							rItemID := in.shopRewardItems.Int(riRow, "Item")
							rName, rIcon := resolveItem(rItemID)
							if rItemID > 0 {
								tiers = []CollectableTier{
									{Label: tierLow, MinCollectability: lowCol, RewardItemID: rItemID, RewardItemName: rName, RewardItemIconID: rIcon, RewardAmount: in.shopRewardItems.Int(riRow, "RewardLow")},
									{Label: tierMid, MinCollectability: midCol, RewardItemID: rItemID, RewardItemName: rName, RewardItemIconID: rIcon, RewardAmount: in.shopRewardItems.Int(riRow, "RewardMid")},
									{Label: tierHigh, MinCollectability: highCol, RewardItemID: rItemID, RewardItemName: rName, RewardItemIconID: rIcon, RewardAmount: in.shopRewardItems.Int(riRow, "RewardHigh")},
								}
							}
						}
					} else if in.shopRewardScrips != nil {
						if rsRow, ok := in.shopRewardScrips.Rows[rewID]; ok {
							cItemID, cName, cIcon := resolveCollectableCurrency(in.appLang, in.shopRewardScrips.Int(rsRow, "Currency"), in.tomestoneByIndex, resolveItem)
							tiers = []CollectableTier{
								{Label: tierLow, MinCollectability: lowCol, RewardItemID: cItemID, RewardItemName: cName, RewardItemIconID: cIcon, RewardAmount: in.shopRewardScrips.Int(rsRow, "LowReward"), ExpRatio: in.shopRewardScrips.Int(rsRow, "ExpRatioLow")},
								{Label: tierMid, MinCollectability: midCol, RewardItemID: cItemID, RewardItemName: cName, RewardItemIconID: cIcon, RewardAmount: in.shopRewardScrips.Int(rsRow, "MidReward"), ExpRatio: in.shopRewardScrips.Int(rsRow, "ExpRatioMid")},
								{Label: tierHigh, MinCollectability: highCol, RewardItemID: cItemID, RewardItemName: cName, RewardItemIconID: cIcon, RewardAmount: in.shopRewardScrips.Int(rsRow, "HighReward"), ExpRatio: in.shopRewardScrips.Int(rsRow, "ExpRatioHigh")},
							}
						}
					}
					if len(tiers) == 0 {
						continue
					}

					mKey := fmt.Sprintf("%d#%s#%s#%d#%d#%d#%d", itemID, shopName, groupName, rewardType, rewID, lowCol, highCol)
					mEntry, exists := merged[mKey]
					if !exists {
						mEntry = &mergedShopItem{
							itemID: itemID,
							src: CollectableTurnInSource{
								SourceKind:        "shop",
								SourceLabel:       i18n.T(in.appLang, "gamedata.col.shop_default"),
								ShopID:            shopID,
								ShopName:          shopName,
								GroupName:         groupName,
								LevelMin:          lvlMin,
								LevelMax:          lvlMax,
								Stars:             stars,
								RequiredQuestID:   reqQuest,
								RequiredQuestName: readQuestName(reqQuest),
								RewardMode:        rewardMode,
								Tiers:             tiers,
							},
						}
						merged[mKey] = mEntry
						mergedOrder = append(mergedOrder, mKey)
					}
					for _, npcID := range shopNPCs {
						if !slices.Contains(mEntry.npcIDs, npcID) {
							mEntry.npcIDs = append(mEntry.npcIDs, npcID)
						}
					}
				}
			}
		}

		for _, key := range mergedOrder {
			mEntry := merged[key]
			mEntry.src.NPCs = resolveNPCs(mEntry.npcIDs)
			out.turnInsByItem[mEntry.itemID] = append(out.turnInsByItem[mEntry.itemID], mEntry.src)
			recordRewardReverse(mEntry.itemID, mEntry.src)
			out.totalEntries++
		}
	}

	// 2. 解析 SatisfactionNpc / SatisfactionSupply / SatisfactionSupplyReward（老主顾交易）
	if in.satisfactionNpcs != nil && in.satisfactionSupplies != nil && in.satisfactionRewards != nil {
		for npcRowID, nRow := range in.satisfactionNpcs.Rows {
			if npcRowID <= 0 {
				continue
			}
			enpcID := in.satisfactionNpcs.Int(nRow, "Npc")
			if enpcID <= 0 {
				continue
			}
			npcName := in.npcNames[enpcID].Name
			if npcName == "" {
				npcName = i18n.Tf(in.appLang, "gamedata.satisfaction_npc_fallback", "id", fmt.Sprintf("%d", enpcID))
			}
			reqQuest := in.satisfactionNpcs.Int(nRow, "QuestRequired")
			lvlUnlock := in.satisfactionNpcs.Int(nRow, "LevelUnlock")
			levelID := in.satisfactionNpcs.Int(nRow, "Level")

			var npcs []ShopVendorNpc
			if v, ok := resolveVendorNpc(enpcID, in.npcNames, in.npcLocations, in.territoryTypes, in.maps, in.placeNames); ok {
				if v.MapPath == "" && levelID > 0 {
					if rawLoc, lok := in.levelLocations[levelID]; lok {
						if loc := resolveMapLocation(rawLoc, in.territoryTypes, in.maps, in.placeNames); loc != nil {
							v.RegionName = loc.RegionName
							v.ZoneName = loc.ZoneName
							v.PlaceName = loc.PlaceName
							v.TerritoryID = loc.TerritoryID
							v.MapID = loc.MapID
							v.MapPath = loc.MapPath
							v.SizeFactor = loc.SizeFactor
							v.CoordX = loc.CoordX
							v.CoordY = loc.CoordY
							v.NormX = loc.NormX
							v.NormY = loc.NormY
						}
					}
				}
				npcs = []ShopVendorNpc{v}
			}

			for rankIdx := 0; rankIdx < 6; rankIdx++ {
				supplyIdx := in.satisfactionNpcs.Int(nRow, fmt.Sprintf("SatisfactionNpcParams[%d].SupplyIndex", rankIdx))
				if supplyIdx <= 0 {
					continue
				}
				rankGroupLabel := i18n.Tf(in.appLang, "gamedata.col.satisfaction_rank", "rank", fmt.Sprintf("%d", rankIdx+1))
				for _, sub := range in.satisfactionSupplies.Rows[supplyIdx] {
					itemID := in.satisfactionSupplies.Int(sub.Data, "Item")
					if itemID <= 0 {
						continue
					}
					rewID := in.satisfactionSupplies.Int(sub.Data, "Reward")
					rRow, ok := in.satisfactionRewards.Rows[rewID]
					if !ok {
						continue
					}
					c1ID, c1Name, c1Icon := resolveCollectableCurrency(in.appLang, in.satisfactionRewards.Int(rRow, "SatisfactionSupplyRewardData[0].RewardCurrency"), in.tomestoneByIndex, resolveItem)
					c2ID, c2Name, c2Icon := resolveCollectableCurrency(in.appLang, in.satisfactionRewards.Int(rRow, "SatisfactionSupplyRewardData[1].RewardCurrency"), in.tomestoneByIndex, resolveItem)

					tiers := []CollectableTier{
						{
							Label:             i18n.T(in.appLang, "gamedata.col.tier_low"),
							MinCollectability: in.satisfactionSupplies.Int(sub.Data, "CollectabilityLow"),
							RewardItemID:      c1ID,
							RewardItemName:    c1Name,
							RewardItemIconID:  c1Icon,
							RewardAmount:      in.satisfactionRewards.Int(rRow, "SatisfactionSupplyRewardData[0].QuantityLow"),
							SecondaryItemID:   c2ID,
							SecondaryItemName: c2Name,
							SecondaryIconID:   c2Icon,
							SecondaryAmount:   in.satisfactionRewards.Int(rRow, "SatisfactionSupplyRewardData[1].QuantityLow"),
							GilReward:         in.satisfactionRewards.Int(rRow, "GilLow"),
							Satisfaction:      in.satisfactionRewards.Int(rRow, "SatisfactionLow"),
						},
						{
							Label:             i18n.T(in.appLang, "gamedata.col.tier_mid"),
							MinCollectability: in.satisfactionSupplies.Int(sub.Data, "CollectabilityMid"),
							RewardItemID:      c1ID,
							RewardItemName:    c1Name,
							RewardItemIconID:  c1Icon,
							RewardAmount:      in.satisfactionRewards.Int(rRow, "SatisfactionSupplyRewardData[0].QuantityMid"),
							SecondaryItemID:   c2ID,
							SecondaryItemName: c2Name,
							SecondaryIconID:   c2Icon,
							SecondaryAmount:   in.satisfactionRewards.Int(rRow, "SatisfactionSupplyRewardData[1].QuantityMid"),
							GilReward:         in.satisfactionRewards.Int(rRow, "GilMid"),
							Satisfaction:      in.satisfactionRewards.Int(rRow, "SatisfactionMid"),
						},
						{
							Label:             i18n.T(in.appLang, "gamedata.col.tier_high"),
							MinCollectability: in.satisfactionSupplies.Int(sub.Data, "CollectabilityHigh"),
							RewardItemID:      c1ID,
							RewardItemName:    c1Name,
							RewardItemIconID:  c1Icon,
							RewardAmount:      in.satisfactionRewards.Int(rRow, "SatisfactionSupplyRewardData[0].QuantityHigh"),
							SecondaryItemID:   c2ID,
							SecondaryItemName: c2Name,
							SecondaryIconID:   c2Icon,
							SecondaryAmount:   in.satisfactionRewards.Int(rRow, "SatisfactionSupplyRewardData[1].QuantityHigh"),
							GilReward:         in.satisfactionRewards.Int(rRow, "GilHigh"),
							Satisfaction:      in.satisfactionRewards.Int(rRow, "SatisfactionHigh"),
						},
					}

					src := CollectableTurnInSource{
						SourceKind:        "satisfaction",
						SourceLabel:       i18n.T(in.appLang, "gamedata.col.satisfaction_label"),
						ShopID:            npcRowID,
						ShopName:          i18n.Tf(in.appLang, "gamedata.col.satisfaction_prefix", "name", npcName),
						GroupName:         rankGroupLabel,
						LevelMin:          lvlUnlock,
						LevelMax:          lvlUnlock,
						RequiredQuestID:   reqQuest,
						RequiredQuestName: readQuestName(reqQuest),
						RewardMode:        "scrip",
						IsBonus:           in.satisfactionSupplies.Bool(sub.Data, "IsBonus"),
						Tiers:             tiers,
						NPCs:              npcs,
					}
					out.turnInsByItem[itemID] = append(out.turnInsByItem[itemID], src)
					recordRewardReverse(itemID, src)
					out.totalEntries++
				}
			}
		}
	}

	// 3. 解析 HWDCrafterSupply / HWDCrafterSupplyReward（伊修加德复兴/苍天街工票供货）
	if in.hwdSupplies != nil && in.hwdRewards != nil {
		scripID := 28063
		scripName, scripIcon := resolveItem(scripID)
		if scripName == "" {
			scripName = i18n.T(in.appLang, "gamedata.col.scrip_skybuilders")
		}

		for supplyID, row := range in.hwdSupplies.Rows {
			if supplyID <= 0 {
				continue
			}
			for slot := 0; slot < 23; slot++ {
				itemID := in.hwdSupplies.Int(row, fmt.Sprintf("HWDCrafterSupplyParams[%d].ItemTradeIn", slot))
				if itemID <= 0 {
					continue
				}
				termName := i18n.T(in.appLang, "gamedata.col.hwd_term_default")
				if termID := in.hwdSupplies.Int(row, fmt.Sprintf("HWDCrafterSupplyParams[%d].TermName", slot)); termID > 0 && in.hwdTerms != nil {
					if tRow, ok := in.hwdTerms.Rows[termID]; ok {
						if n := strings.TrimSpace(in.hwdTerms.Str(tRow, "Name")); n != "" {
							termName = n
						}
					}
				}

				buildHWDTier := func(label, ratingField, rewardField string) *CollectableTier {
					minCol := in.hwdSupplies.Int(row, fmt.Sprintf("HWDCrafterSupplyParams[%d].%s", slot, ratingField))
					rewID := in.hwdSupplies.Int(row, fmt.Sprintf("HWDCrafterSupplyParams[%d].%s", slot, rewardField))
					if rewID <= 0 {
						return nil
					}
					rRow, ok := in.hwdRewards.Rows[rewID]
					if !ok {
						return nil
					}
					return &CollectableTier{
						Label:             label,
						MinCollectability: minCol,
						RewardItemID:      scripID,
						RewardItemName:    scripName,
						RewardItemIconID:  scripIcon,
						RewardAmount:      in.hwdRewards.Int(rRow, "ScriptRewardAmount"),
						ExpReward:         in.hwdRewards.Int(rRow, "ExpReward"),
						Points:            in.hwdRewards.Int(rRow, "Points"),
					}
				}

				var tiers []CollectableTier
				for _, spec := range [3]struct{ lbl, colF, rewF string }{
					{i18n.T(in.appLang, "gamedata.col.tier_low"), "BaseCollectableRating", "BaseCollectableReward"},
					{i18n.T(in.appLang, "gamedata.col.tier_mid"), "MidCollectableRating", "MidCollectableReward"},
					{i18n.T(in.appLang, "gamedata.col.tier_high"), "HighCollectableRating", "HighCollectableReward"},
				} {
					if t := buildHWDTier(spec.lbl, spec.colF, spec.rewF); t != nil {
						tiers = append(tiers, *t)
					}
				}
				if len(tiers) == 0 {
					continue
				}

				lvlMin := in.hwdSupplies.Int(row, fmt.Sprintf("HWDCrafterSupplyParams[%d].Level", slot))
				lvlMax := in.hwdSupplies.Int(row, fmt.Sprintf("HWDCrafterSupplyParams[%d].LevelMax", slot))
				src := CollectableTurnInSource{
					SourceKind:  "hwd",
					SourceLabel: i18n.T(in.appLang, "gamedata.col.hwd_label"),
					ShopID:      supplyID,
					ShopName:    i18n.Tf(in.appLang, "gamedata.col.hwd_prefix", "term", termName),
					GroupName:   termName,
					LevelMin:    lvlMin,
					LevelMax:    lvlMax,
					RewardMode:  "supply",
					Tiers:       tiers,
				}
				out.turnInsByItem[itemID] = append(out.turnInsByItem[itemID], src)
				recordRewardReverse(itemID, src)
				out.totalEntries++
			}
		}
	}

	// 稳定排序所有正向/反向收藏品索引
	kindRank := map[string]int{"shop": 0, "satisfaction": 1, "hwd": 2}
	for _, list := range out.turnInsByItem {
		sort.Slice(list, func(i, j int) bool {
			if kindRank[list[i].SourceKind] != kindRank[list[j].SourceKind] {
				return kindRank[list[i].SourceKind] < kindRank[list[j].SourceKind]
			}
			if list[i].LevelMax != list[j].LevelMax {
				return list[i].LevelMax > list[j].LevelMax
			}
			return list[i].ShopID < list[j].ShopID
		})
	}
	for itemID, list := range out.rewardsByItem {
		sort.Slice(list, func(i, j int) bool {
			if list[i].RewardHigh != list[j].RewardHigh {
				return list[i].RewardHigh > list[j].RewardHigh
			}
			return list[i].TurnInItemID < list[j].TurnInItemID
		})
		if len(list) > 80 {
			out.rewardsByItem[itemID] = list[:80]
		}
	}

	return out
}
