package gamedata

import (
	"slices"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

// RawShopCost 保存商店兑换消耗项的原始结构。
type RawShopCost struct {
	ItemID         int
	NameCn         string
	IconID         int
	Amount         int
	Collectability int
}

// RawShopEntry 保存单条商店售卖或兑换记录。
type RawShopEntry struct {
	ShopID        int
	ShopType      string // gil|special|gc|fcc
	ShopTypeLabel string
	ShopName      string
	ReceiveAmount int
	ReceiveHQ     bool
	Costs         []RawShopCost
	NpcIDs        []int
}

// RawNpcLocation 保存 NPC 在 Level 表中的世界坐标与所属地图。
type RawNpcLocation struct {
	TerritoryID int
	MapID       int
	WorldX      float64
	WorldZ      float64
}

// RawNpcName 保存 NPC 名称与头衔。
type RawNpcName struct {
	Name  string
	Title string
}

// ShopIndices 汇总全量商店、收藏品商店关联、NPC 名称与坐标反向索引。
type ShopIndices struct {
	ShopsByItem            map[int][]RawShopEntry
	CollectablesShopToNPCs map[int][]int
	NpcNames               map[int]RawNpcName
	NpcLocations           map[int]RawNpcLocation
	LevelLocations         map[int]RawNpcLocation
	TotalActiveShops       int
}

func resolveSpecialCurrencyID(rawCostID, useCurrencyType int, tomestoneByIndex map[int]int) int {
	if rawCostID <= 0 || rawCostID >= 100 {
		return rawCostID
	}
	if useCurrencyType == 2 {
		if id := tomestoneByIndex[rawCostID]; id > 0 {
			return id
		}
		return rawCostID
	}
	if useCurrencyType == 16 || useCurrencyType == 4 {
		switch rawCostID {
		case 1:
			return 28 // 亚拉戈诗学神典石
		case 2:
			return 33913 // 巧手紫票
		case 3:
			if id := tomestoneByIndex[3]; id > 0 {
				return id
			}
			return 49 // 亚拉戈记忆神典石
		case 4:
			return 33914 // 大地紫票
		case 6:
			return 41784 // 巧手橙票
		case 7:
			return 41785 // 大地橙票
		default:
			return rawCostID
		}
	}
	if useCurrencyType == 8 && rawCostID == 1 {
		return 1 // 金币
	}
	return rawCostID
}

type shopLoaderInput struct {
	itemsPrimary        map[int]sqpack.Row
	gilShops            map[int]sqpack.Row
	gilShopItems        map[int][]sqpack.SubRow
	specialShops        map[int]sqpack.Row
	gcScripShopItems    map[int][]sqpack.SubRow
	fccShops            map[int]sqpack.Row
	tomestonesItems     map[int]sqpack.Row
	enpcResidents       map[int]sqpack.Row
	enpcBases           map[int]sqpack.Row
	topicSelects        map[int]sqpack.Row
	preHandlers         map[int]sqpack.Row
	inclusionShops      map[int]sqpack.Row
	inclusionCategories map[int]sqpack.Row
	inclusionSeries     map[int][]sqpack.SubRow
	customTalks         map[int]sqpack.Row
	levels              map[int]sqpack.Row
	collectablesShops   map[int]sqpack.Row
}

// buildShopIndices 解析金币商店、特殊兑换、军票与部队战绩商店并建立 NPC 坐标反向索引。
func buildShopIndices(in shopLoaderInput) ShopIndices {
	// 1. 解析 NPC 名称与头衔
	npcNames := make(map[int]RawNpcName, len(in.enpcResidents))
	for npcID, row := range in.enpcResidents {
		name := row.StringAt(0)
		if name == "" {
			continue
		}
		title := row.StringAt(8)
		if title == "" {
			title = row.StringAt(7)
		}
		npcNames[npcID] = RawNpcName{Name: name, Title: title}
	}

	// 2. 从 Level 表提取按 Level ID 和 EventNpc (Type == 8) 索引的世界坐标与所属地图
	npcLocations := make(map[int]RawNpcLocation)
	levelLocations := make(map[int]RawNpcLocation, len(in.levels))
	for levelID, row := range in.levels {
		territoryID := row.IntAt(9)
		if territoryID <= 0 {
			continue
		}
		loc := RawNpcLocation{
			TerritoryID: territoryID,
			MapID:       row.IntAt(7),
			WorldX:      row.FloatAt(0),
			WorldZ:      row.FloatAt(2),
		}
		levelLocations[levelID] = loc
		if row.IntAt(5) != 8 {
			continue
		}
		npcID := row.IntAt(6)
		if npcID <= 0 {
			continue
		}
		if _, exists := npcLocations[npcID]; exists {
			continue
		}
		npcLocations[npcID] = loc
	}

	// 3. 递归追踪 ENpcData 引用链（PreHandler / TopicSelect / InclusionShop / CustomTalk）
	shopFallbackNames := make(map[int]string)
	memo := make(map[int][]int)
	var resolveRef func(refID int, ctxTitle string) []int
	resolveRef = func(refID int, ctxTitle string) []int {
		if refID <= 0 {
			return nil
		}
		_, isCollectables := in.collectablesShops[refID]
		isDirectShop := isCollectables ||
			(refID >= 262144 && refID < 270000) ||
			(refID >= 1769472 && refID < 1780000) ||
			(refID >= 3866624 && refID < 3880000)
		if isDirectShop {
			if ctxTitle != "" {
				if _, ok := shopFallbackNames[refID]; !ok {
					shopFallbackNames[refID] = ctxTitle
				}
			}
			return []int{refID}
		}

		if cached, ok := memo[refID]; ok {
			if ctxTitle != "" {
				for _, sid := range cached {
					if _, exists := shopFallbackNames[sid]; !exists {
						shopFallbackNames[sid] = ctxTitle
					}
				}
			}
			return cached
		}

		// 先占位避免环形引用死循环
		memo[refID] = []int{}
		var result []int

		if ph, ok := in.preHandlers[refID]; ok {
			result = append(result, resolveRef(ph.IntAt(2), ctxTitle)...)
			memo[refID] = result
			return result
		}
		if ts, ok := in.topicSelects[refID]; ok {
			topicName := ts.StringAt(0)
			if topicName == "" {
				topicName = ctxTitle
			}
			for i := 4; i <= 13; i++ {
				if sub := ts.IntAt(i); sub > 0 {
					result = append(result, resolveRef(sub, topicName)...)
				}
			}
			memo[refID] = result
			return result
		}
		if inc, ok := in.inclusionShops[refID]; ok {
			for i := 3; i < len(inc); i++ {
				catID := inc.IntAt(i)
				if catID <= 0 {
					continue
				}
				cat, ok := in.inclusionCategories[catID]
				if !ok {
					continue
				}
				catName := cat.StringAt(0)
				if catName == "" {
					catName = ctxTitle
				}
				seriesID := cat.IntAt(2)
				for _, sub := range in.inclusionSeries[seriesID] {
					if spID := sub.Data.IntAt(0); spID > 0 {
						result = append(result, resolveRef(spID, catName)...)
					}
				}
			}
			memo[refID] = result
			return result
		}
		if ct, ok := in.customTalks[refID]; ok {
			for i := range ct {
				num := ct.IntAt(i)
				if (num >= 262144 && num < 270000) ||
					(num >= 1769472 && num < 1780000) ||
					(num >= 3276800 && num < 3900000) {
					result = append(result, resolveRef(num, ctxTitle)...)
				}
			}
			memo[refID] = result
			return result
		}
		return result
	}

	shopToNPCs := make(map[int][]int)
	for npcID, row := range in.enpcBases {
		if _, ok := npcNames[npcID]; !ok {
			continue
		}
		for i := 2; i <= 33; i++ {
			refID := row.IntAt(i)
			if refID <= 0 {
				continue
			}
			for _, shopID := range resolveRef(refID, "") {
				if !slices.Contains(shopToNPCs[shopID], npcID) {
					shopToNPCs[shopID] = append(shopToNPCs[shopID], npcID)
				}
			}
		}
	}

	// 4. 神典石索引映射
	tomestoneByIndex := make(map[int]int)
	for _, row := range in.tomestonesItems {
		itemID := row.IntAt(0)
		idx := row.IntAt(2)
		if itemID > 0 && idx > 0 {
			tomestoneByIndex[idx] = itemID
		}
	}

	shopsByItem := make(map[int][]RawShopEntry)
	activeShopIDs := make(map[int]struct{})
	addEntry := func(itemID int, entry RawShopEntry) {
		shopsByItem[itemID] = append(shopsByItem[itemID], entry)
		activeShopIDs[entry.ShopID] = struct{}{}
	}

	// 5. 解析 GilShopItem（金币商店）
	for shopID, subrows := range in.gilShopItems {
		rawName := ""
		if sr, ok := in.gilShops[shopID]; ok {
			rawName = sr.StringAt(0)
		}
		if strings.HasPrefix(rawName, "测试") {
			continue
		}
		shopName := rawName
		if shopName == "" {
			shopName = shopFallbackNames[shopID]
		}
		if shopName == "" {
			shopName = "金币道具商店"
		}
		npcIDs := shopToNPCs[shopID]

		for _, sub := range subrows {
			itemID := sub.Data.IntAt(0)
			if itemID <= 0 {
				continue
			}
			itemRow, ok := in.itemsPrimary[itemID]
			if !ok {
				continue
			}
			priceMid := itemRow.IntAt(25)
			if priceMid <= 0 {
				continue
			}
			addEntry(itemID, RawShopEntry{
				ShopID:        shopID,
				ShopType:      "gil",
				ShopTypeLabel: "金币商店",
				ShopName:      shopName,
				ReceiveAmount: 1,
				ReceiveHQ:     false,
				Costs: []RawShopCost{{
					ItemID: 1,
					NameCn: "金币",
					IconID: 65002,
					Amount: priceMid,
				}},
				NpcIDs: npcIDs,
			})
		}
	}

	// 6. 解析 SpecialShop（特殊货币/兑换商店）
	costItemBlocks := [3]int{481, 721, 961}
	costCountBlocks := [3]int{541, 781, 1021}
	collectBlocks := [3]int{661, 901, 1141}
	recvItemBlocks := [2]int{1, 241}
	recvCountBlocks := [2]int{61, 301}
	recvHQBlocks := [2]int{181, 421}

	for shopID, row := range in.specialShops {
		rawName := row.StringAt(0)
		if strings.HasPrefix(rawName, "测试") {
			continue
		}
		shopName := rawName
		if shopName == "" {
			shopName = shopFallbackNames[shopID]
		}
		if shopName == "" {
			shopName = "特殊兑换商店"
		}
		useCurrencyType := row.IntAt(2041)
		npcIDs := shopToNPCs[shopID]

		for slot := 0; slot < 60; slot++ {
			var costs []RawShopCost
			for c := 0; c < 3; c++ {
				rawCostID := row.IntAt(costItemBlocks[c] + slot)
				costAmount := row.IntAt(costCountBlocks[c] + slot)
				if rawCostID <= 0 || costAmount <= 0 {
					continue
				}
				resolvedCostID := resolveSpecialCurrencyID(rawCostID, useCurrencyType, tomestoneByIndex)
				colVal := row.IntAt(collectBlocks[c] + slot)
				costs = append(costs, RawShopCost{
					ItemID:         resolvedCostID,
					Amount:         costAmount,
					Collectability: colVal,
				})
			}
			if len(costs) == 0 {
				continue
			}

			for r := 0; r < 2; r++ {
				recvItemID := row.IntAt(recvItemBlocks[r] + slot)
				if recvItemID <= 0 {
					continue
				}
				recvAmount := row.IntAt(recvCountBlocks[r] + slot)
				if recvAmount <= 0 {
					recvAmount = 1
				}
				recvHQ := row.BoolAt(recvHQBlocks[r] + slot)
				addEntry(recvItemID, RawShopEntry{
					ShopID:        shopID,
					ShopType:      "special",
					ShopTypeLabel: "特殊兑换",
					ShopName:      shopName,
					ReceiveAmount: recvAmount,
					ReceiveHQ:     recvHQ,
					Costs:         costs,
					NpcIDs:        npcIDs,
				})
			}
		}
	}

	// 7. 解析 GCScripShopItem（大国防联军军票商店）
	for parentID, subrows := range in.gcScripShopItems {
		if parentID <= 0 {
			continue
		}
		gcName := "大国防联军军票商店"
		sealItemID := 20
		switch {
		case parentID >= 1 && parentID <= 12:
			gcName = "黑涡团军票商店"
			sealItemID = 20
		case parentID >= 13 && parentID <= 24:
			gcName = "双蛇党军票商店"
			sealItemID = 21
		case parentID >= 25:
			gcName = "恒辉队军票商店"
			sealItemID = 22
		}
		for _, sub := range subrows {
			itemID := sub.Data.IntAt(0)
			costAmount := sub.Data.IntAt(2)
			if itemID <= 0 || costAmount <= 0 {
				continue
			}
			addEntry(itemID, RawShopEntry{
				ShopID:        1441792 + parentID,
				ShopType:      "gc",
				ShopTypeLabel: "军票商店",
				ShopName:      gcName,
				ReceiveAmount: 1,
				Costs:         []RawShopCost{{ItemID: sealItemID, Amount: costAmount}},
			})
		}
	}

	// 8. 解析 FccShop（部队战绩商店）
	for shopID, row := range in.fccShops {
		shopName := row.StringAt(0)
		if shopName == "" {
			shopName = "部队战绩交易"
		}
		for i := 0; i < 10; i++ {
			itemID := row.IntAt(1 + i)
			costAmount := row.IntAt(11 + i)
			if itemID <= 0 || costAmount <= 0 {
				continue
			}
			addEntry(itemID, RawShopEntry{
				ShopID:        shopID,
				ShopType:      "fcc",
				ShopTypeLabel: "部队战绩",
				ShopName:      shopName,
				ReceiveAmount: 1,
				Costs: []RawShopCost{{
					ItemID: 0,
					NameCn: "部队战绩",
					IconID: 65016,
					Amount: costAmount,
				}},
			})
		}
	}

	collectablesShopToNPCs := make(map[int][]int)
	for shopID, npcs := range shopToNPCs {
		if _, ok := in.collectablesShops[shopID]; ok || (shopID >= 3866624 && shopID < 3880000) {
			collectablesShopToNPCs[shopID] = npcs
		}
	}

	return ShopIndices{
		ShopsByItem:            shopsByItem,
		CollectablesShopToNPCs: collectablesShopToNPCs,
		NpcNames:               npcNames,
		NpcLocations:           npcLocations,
		LevelLocations:         levelLocations,
		TotalActiveShops:       len(activeShopIDs),
	}
}
