package gamedata

import (
	"sort"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

// RawRetainerVenture 保存单个道具对应的雇员定向筹集委托索引记录。
type RawRetainerVenture struct {
	TaskID               int
	ItemID               int
	VentureType          string
	VentureTypeLabel     string
	ClassJobCategoryID   int
	ClassJobCategoryName string
	RetainerLevel        int
	VentureCost          int
	DurationMinutes      int
	Experience           int
	RequiresLogUnlock    bool
	Tiers                []RetainerVentureTier
}

// RetainerIndices 汇总所有雇员探险筹集委托的反向索引。
type RetainerIndices struct {
	VenturesByItem     map[int][]RawRetainerVenture
	TotalRetainerTasks int
}

type ventureCategoryMeta struct {
	ventureType      string
	ventureTypeLabel string
	baseStatType     string
	tierStatType     string
	baseColIndex     int
	paramStartOffset int
}

var ventureCategoryMetaMap = map[int]ventureCategoryMeta{
	// 战斗精英 魔法导师 -> 狩猎筹集委托（装等门槛）
	34: {
		ventureType:      "hunt",
		ventureTypeLabel: "狩猎筹集委托",
		baseStatType:     "ilvl",
		tierStatType:     "ilvl",
		baseColIndex:     8,
		paramStartOffset: 0,
	},
	// 采矿工 -> 采矿筹集委托（基础获得力 + 4档鉴别力）
	17: {
		ventureType:      "mining",
		ventureTypeLabel: "采矿筹集委托",
		baseStatType:     "gathering",
		tierStatType:     "perception",
		baseColIndex:     11,
		paramStartOffset: 4,
	},
	// 园艺工 -> 园艺筹集委托（基础获得力 + 4档鉴别力）
	18: {
		ventureType:      "botany",
		ventureTypeLabel: "园艺筹集委托",
		baseStatType:     "gathering",
		tierStatType:     "perception",
		baseColIndex:     11,
		paramStartOffset: 4,
	},
	// 捕鱼人 -> 捕鱼筹集委托（基础获得力 + 4档鉴别力）
	19: {
		ventureType:      "fishing",
		ventureTypeLabel: "捕鱼筹集委托",
		baseStatType:     "gathering",
		tierStatType:     "perception",
		baseColIndex:     11,
		paramStartOffset: 8,
	},
}

func buildVentureTiers(taskRow, normalRow, paramRow sqpack.Row, meta ventureCategoryMeta) []RetainerVentureTier {
	var tiers []RetainerVentureTier
	baseThreshold := max(0, taskRow.IntAt(meta.baseColIndex))

	for i := 0; i < 5; i++ {
		qty := normalRow.IntAt(1 + i)
		if qty <= 0 {
			continue
		}
		if i == 0 {
			tiers = append(tiers, RetainerVentureTier{
				StatType:     meta.baseStatType,
				RequiredStat: baseThreshold,
				Quantity:     qty,
			})
		} else {
			paramThreshold := 0
			if paramRow != nil {
				paramThreshold = max(0, paramRow.IntAt(meta.paramStartOffset+(i-1)))
			}
			tiers = append(tiers, RetainerVentureTier{
				StatType:     meta.tierStatType,
				RequiredStat: paramThreshold,
				Quantity:     qty,
			})
		}
	}
	return tiers
}

// buildRetainerIndices 构建道具 -> 雇员探险定向筹集委托反向索引。
func buildRetainerIndices(
	retainerTasks map[int]sqpack.Row,
	retainerTaskNormals map[int]sqpack.Row,
	retainerTaskParameters map[int]sqpack.Row,
	classJobCategories map[int]string,
) RetainerIndices {
	venturesByItem := make(map[int][]RawRetainerVenture)
	totalTasks := 0

	for taskID, taskRow := range retainerTasks {
		// 跳过随机探索委托与空占位行
		if taskRow.BoolAt(0) {
			continue
		}
		normalID := taskRow.IntAt(13)
		if normalID <= 0 {
			continue
		}
		normalRow, ok := retainerTaskNormals[normalID]
		if !ok {
			continue
		}
		itemID := normalRow.IntAt(0)
		if itemID <= 0 {
			continue
		}
		cjCatID := taskRow.IntAt(1)
		meta, ok := ventureCategoryMetaMap[cjCatID]
		if !ok {
			continue
		}

		paramID := taskRow.IntAt(4)
		var paramRow sqpack.Row
		if paramID > 0 {
			paramRow = retainerTaskParameters[paramID]
		}
		tiers := buildVentureTiers(taskRow, normalRow, paramRow, meta)
		if len(tiers) == 0 {
			continue
		}

		catName := classJobCategories[cjCatID]
		if catName == "" {
			catName = meta.ventureTypeLabel
		}
		level := taskRow.IntAt(2)
		if level <= 0 {
			level = 1
		}
		cost := taskRow.IntAt(5)
		if cost <= 0 {
			cost = 1
		}
		duration := taskRow.IntAt(6)
		if duration <= 0 {
			duration = 60
		}

		venturesByItem[itemID] = append(venturesByItem[itemID], RawRetainerVenture{
			TaskID:               taskID,
			ItemID:               itemID,
			VentureType:          meta.ventureType,
			VentureTypeLabel:     meta.ventureTypeLabel,
			ClassJobCategoryID:   cjCatID,
			ClassJobCategoryName: catName,
			RetainerLevel:        level,
			VentureCost:          cost,
			DurationMinutes:      duration,
			Experience:           taskRow.IntAt(7),
			RequiresLogUnlock:    taskRow.IntAt(9) == 72,
			Tiers:                tiers,
		})
		totalTasks++
	}

	for _, list := range venturesByItem {
		sort.Slice(list, func(i, j int) bool {
			if list[i].RetainerLevel != list[j].RetainerLevel {
				return list[i].RetainerLevel < list[j].RetainerLevel
			}
			return list[i].TaskID < list[j].TaskID
		})
	}

	return RetainerIndices{
		VenturesByItem:     venturesByItem,
		TotalRetainerTasks: totalTasks,
	}
}
