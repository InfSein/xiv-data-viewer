package gamedata

import (
	"fmt"
	"sort"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

type leveLoaderInput struct {
	appLang               i18n.Lang
	leves                 *sqpack.SchemaSheet
	levesCn               *sqpack.SchemaSheet
	levesEn               *sqpack.SchemaSheet
	levesJa               *sqpack.SchemaSheet
	craftLeves            *sqpack.SchemaSheet
	gatheringLeves        *sqpack.SchemaSheet
	gatheringLeveRoutes   *sqpack.SchemaSheet
	battleLeves           *sqpack.SchemaSheet
	companyLeves          *sqpack.SchemaSheet
	leveRewardItems       *sqpack.SchemaSheet
	leveRewardItemGroups  *sqpack.SchemaSheet
	leveAssignmentTypes   *sqpack.SchemaSheet
	leveClients           *sqpack.SchemaSheet
	towns                 *sqpack.SchemaSheet
	leveStrings           *sqpack.SchemaSheet
	gatheringLeveRules    *sqpack.SchemaSheet
	battleLeveRules       *sqpack.SchemaSheet
	companyLeveRules      *sqpack.SchemaSheet
	eventItems            *sqpack.SchemaSheet
	bnpcNames             *sqpack.SchemaSheet
	journalGenres         *sqpack.SchemaSheet
	fishingSpots          map[int]sqpack.Row
	spearfishingNotebooks map[int]sqpack.Row
	levelLocations        map[int]RawNpcLocation
	classJobCategories    map[int]string
	placeNames            map[int]string
	territoryTypes        map[int]sqpack.Row
	maps                  map[int]sqpack.Row
	itemSummaryByID       map[int]ItemSummary
}

type leveIndices struct {
	leves              []LeveEntry
	leveByID           map[int]LeveEntry
	levesByItem        map[int][]ItemLeveSource
	assignmentTypeList []LeveAssignmentTypeInfo
}

func inferLeveKind(appLang i18n.Lang, assignID int, dataID int, in leveLoaderInput) (string, string) {
	if in.craftLeves != nil {
		if _, ok := in.craftLeves.Rows[dataID]; ok {
			return "craft", i18n.T(appLang, "gamedata.leve.craft")
		}
	}
	if in.gatheringLeves != nil {
		if _, ok := in.gatheringLeves.Rows[dataID]; ok {
			return "gathering", i18n.T(appLang, "gamedata.leve.gathering")
		}
	}
	if in.battleLeves != nil {
		if _, ok := in.battleLeves.Rows[dataID]; ok {
			return "battle", i18n.T(appLang, "gamedata.leve.battle")
		}
	}
	if in.companyLeves != nil {
		if _, ok := in.companyLeves.Rows[dataID]; ok {
			return "company", i18n.T(appLang, "gamedata.leve.company")
		}
	}

	switch assignID {
	case 2, 3, 4:
		return "gathering", i18n.T(appLang, "gamedata.leve.gathering")
	case 5, 6, 7, 8, 9, 10, 11, 12:
		return "craft", i18n.T(appLang, "gamedata.leve.craft")
	case 13, 14, 15, 16:
		return "company", i18n.T(appLang, "gamedata.leve.company")
	default:
		return "battle", i18n.T(appLang, "gamedata.leve.battle")
	}
}

func buildLeveData(in leveLoaderInput) leveIndices {
	in.appLang = resolveAppLang(in.appLang)
	out := leveIndices{
		leveByID:    make(map[int]LeveEntry),
		levesByItem: make(map[int][]ItemLeveSource),
	}
	if in.leves == nil || len(in.leves.Rows) == 0 {
		return out
	}

	typeCount := make(map[int]int)
	typeMeta := make(map[int]LeveAssignmentTypeInfo)

	resolveMap := func(levelID int) *MapLocationPoint {
		if levelID <= 0 {
			return nil
		}
		rawLoc, ok := in.levelLocations[levelID]
		if !ok {
			return nil
		}
		return resolveMapLocation(rawLoc, in.territoryTypes, in.maps, in.placeNames)
	}

	readEventItemName := func(eventItemID int) string {
		if eventItemID <= 0 || in.eventItems == nil {
			return ""
		}
		row, ok := in.eventItems.Rows[eventItemID]
		if !ok {
			return ""
		}
		if s := strings.TrimSpace(in.eventItems.Str(row, "Singular")); s != "" {
			return s
		}
		return strings.TrimSpace(in.eventItems.Str(row, "Name"))
	}

	readBNpcName := func(bnpcID int) string {
		if bnpcID <= 0 || in.bnpcNames == nil {
			return ""
		}
		row, ok := in.bnpcNames.Rows[bnpcID]
		if !ok {
			return ""
		}
		return strings.TrimSpace(in.bnpcNames.Str(row, "Singular"))
	}

	readLeveStr := func(strID int) string {
		if strID <= 0 || in.leveStrings == nil {
			return ""
		}
		row, ok := in.leveStrings.Rows[strID]
		if !ok {
			return ""
		}
		return strings.TrimSpace(in.leveStrings.Str(row, "Objective"))
	}

	out.leves = make([]LeveEntry, 0, len(in.leves.Rows))
	for leveID, row := range in.leves.Rows {
		if leveID <= 0 {
			continue
		}
		nameCn := strings.TrimSpace(in.leves.Str(row, "Name"))
		if nameCn == "" {
			continue
		}

		nameZh := ""
		if in.levesCn != nil {
			if cnRow, ok := in.levesCn.Rows[leveID]; ok {
				nameZh = strings.TrimSpace(in.levesCn.Str(cnRow, "Name"))
			}
		}
		nameEn := ""
		if in.levesEn != nil {
			if enRow, ok := in.levesEn.Rows[leveID]; ok {
				nameEn = strings.TrimSpace(in.levesEn.Str(enRow, "Name"))
			}
		}
		nameJa := ""
		if in.levesJa != nil {
			if jaRow, ok := in.levesJa.Rows[leveID]; ok {
				nameJa = strings.TrimSpace(in.levesJa.Str(jaRow, "Name"))
			}
		}

		assignTypeID := in.leves.Int(row, "LeveAssignmentType")
		dataID := in.leves.Int(row, "DataId")
		kind, kindLabel := inferLeveKind(in.appLang, assignTypeID, dataID, in)

		assignName := kindLabel
		assignIconID := 0
		if in.leveAssignmentTypes != nil {
			if aRow, ok := in.leveAssignmentTypes.Rows[assignTypeID]; ok {
				if n := strings.TrimSpace(in.leveAssignmentTypes.Str(aRow, "Name")); n != "" {
					assignName = n
				}
				assignIconID = in.leveAssignmentTypes.Int(aRow, "Icon")
			}
		}
		if assignTypeID > 0 {
			typeCount[assignTypeID]++
			if _, exists := typeMeta[assignTypeID]; !exists {
				typeMeta[assignTypeID] = LeveAssignmentTypeInfo{
					ID:     assignTypeID,
					Name:   assignName,
					IconID: assignIconID,
					Kind:   kind,
				}
			}
		}

		genreName := ""
		if genreID := in.leves.Int(row, "JournalGenre"); genreID > 0 && in.journalGenres != nil {
			if gRow, ok := in.journalGenres.Rows[genreID]; ok {
				genreName = strings.TrimSpace(in.journalGenres.Str(gRow, "Name"))
			}
		}

		townName := ""
		if townID := in.leves.Int(row, "Town"); townID > 0 && in.towns != nil {
			if tRow, ok := in.towns.Rows[townID]; ok {
				townName = strings.TrimSpace(in.towns.Str(tRow, "Name"))
			}
		}

		clientName := ""
		if clientID := in.leves.Int(row, "LeveClient"); clientID > 0 && in.leveClients != nil {
			if cRow, ok := in.leveClients.Rows[clientID]; ok {
				clientName = strings.TrimSpace(in.leveClients.Str(cRow, "Name"))
			}
		}

		cjCatName := in.classJobCategories[in.leves.Int(row, "ClassJobCategory")]
		levelNum := in.leves.Int(row, "ClassJobLevel")

		fishingSpotName := ""
		if spotID := in.leves.Int(row, "FishingSpot"); spotID > 0 {
			if fsRow, ok := in.fishingSpots[spotID]; ok {
				fishingSpotName = in.placeNames[fsRow.IntAt(23)]
			}
		}
		spearPlaceName := ""
		if nbID := in.leves.Int(row, "SpearfishingNotebook"); nbID > 0 {
			if nbRow, ok := in.spearfishingNotebooks[nbID]; ok {
				spearPlaceName = in.placeNames[nbRow.IntAt(7)]
			}
		}

		entry := LeveEntry{
			ID:                    leveID,
			NameCn:                nameCn,
			NameZh:                nameZh,
			NameEn:                nameEn,
			NameJa:                nameJa,
			DescriptionCn:         strings.TrimSpace(in.leves.Str(row, "Description")),
			IconCityState:         in.leves.Int(row, "IconCityState"),
			IconIssuer:            in.leves.Int(row, "IconIssuer"),
			LeveKind:              kind,
			LeveKindLabel:         kindLabel,
			AssignmentTypeID:      assignTypeID,
			AssignmentTypeName:    assignName,
			AssignmentTypeIconID:  assignIconID,
			GenreName:             genreName,
			TownName:              townName,
			ClientName:            clientName,
			ClassJobCategoryName:  cjCatName,
			ClassJobLevel:         levelNum,
			PlaceNameStart:        in.placeNames[in.leves.Int(row, "PlaceNameStart")],
			PlaceNameIssued:       in.placeNames[in.leves.Int(row, "PlaceNameIssued")],
			PlaceNameStartZone:    in.placeNames[in.leves.Int(row, "PlaceNameStartZone")],
			LevemeteLocation:      resolveMap(in.leves.Int(row, "LevelLevemete")),
			StartLocation:         resolveMap(in.leves.Int(row, "LevelStart")),
			TimeLimit:             in.leves.Int(row, "TimeLimit"),
			AllowanceCost:         in.leves.Int(row, "AllowanceCost"),
			CanCancel:             in.leves.Bool(row, "CanCancel"),
			LockedLeve:            in.leves.Bool(row, "LockedLeve"),
			ExpReward:             in.leves.Int(row, "ExpReward"),
			ExpFactor:             in.leves.Float(row, "ExpFactor"),
			GilReward:             in.leves.Int(row, "GilReward"),
			FishingSpotName:       fishingSpotName,
			SpearfishingPlaceName: spearPlaceName,
		}

		// 按理符类型读取子表（CraftLeve / GatheringLeve / BattleLeve / CompanyLeve）
		switch kind {
		case "craft":
			if in.craftLeves != nil {
				if cRow, ok := in.craftLeves.Rows[dataID]; ok {
					entry.CraftRepeats = max(0, in.craftLeves.Int(cRow, "Repeats"))
					turnInSeen := make(map[int]bool)
					for idx := 0; idx < 4; idx++ {
						itemID := in.craftLeves.Int(cRow, fmt.Sprintf("Item[%d]", idx))
						if itemID <= 0 {
							continue
						}
						cnt := max(1, in.craftLeves.Int(cRow, fmt.Sprintf("ItemCount[%d]", idx)))
						it := in.itemSummaryByID[itemID]
						itemName := it.NameCn
						if itemName == "" {
							itemName = i18n.Tf(in.appLang, "gamedata.item_fallback", "id", fmt.Sprintf("%d", itemID))
						}
						entry.TurnInItems = append(entry.TurnInItems, LeveTurnInItem{
							ItemID:   itemID,
							ItemName: itemName,
							IconID:   it.IconID,
							Count:    cnt,
						})
						if !turnInSeen[itemID] {
							turnInSeen[itemID] = true
							out.levesByItem[itemID] = append(out.levesByItem[itemID], ItemLeveSource{
								LeveID:             leveID,
								NameCn:             nameCn,
								AssignmentTypeName: assignName,
								Level:              levelNum,
								Count:              cnt * (1 + entry.CraftRepeats),
								IsTurnIn:           true,
							})
						}
					}
				}
			}

		case "gathering":
			if in.gatheringLeves != nil {
				if gRow, ok := in.gatheringLeves.Rows[dataID]; ok {
					if ruleID := in.gatheringLeves.Int(gRow, "Rule"); ruleID > 0 && in.gatheringLeveRules != nil {
						if rRow, ok := in.gatheringLeveRules.Rows[ruleID]; ok {
							entry.RuleName = strings.TrimSpace(in.gatheringLeveRules.Str(rRow, "Rule"))
						}
					}
					entry.GatheringItemNumber = in.gatheringLeves.Int(gRow, "ItemNumber")
					entry.GatheringUseSecondary = in.gatheringLeves.Bool(gRow, "UseSecondaryTool")
					for idx := 0; idx < 2; idx++ {
						if obj := readLeveStr(in.gatheringLeves.Int(gRow, fmt.Sprintf("Objective[%d]", idx))); obj != "" {
							entry.Objectives = append(entry.Objectives, obj)
						}
					}
					for idx := 0; idx < 4; idx++ {
						evName := readEventItemName(in.gatheringLeves.Int(gRow, fmt.Sprintf("RequiredItem[%d]", idx)))
						qty := in.gatheringLeves.Int(gRow, fmt.Sprintf("RequiredItemQuantity[%d]", idx))
						if evName != "" {
							entry.RequiredObjectiveItems = append(entry.RequiredObjectiveItems, LeveObjectiveItem{
								ItemName: evName,
								Count:    max(1, qty),
							})
						}
					}
					for idx := 0; idx < 4 && entry.GatheringTargetLocation == nil && in.gatheringLeveRoutes != nil; idx++ {
						routeID := in.gatheringLeves.Int(gRow, fmt.Sprintf("Route[%d]", idx))
						if routeID <= 0 {
							continue
						}
						rRow, ok := in.gatheringLeveRoutes.Rows[routeID]
						if !ok {
							continue
						}
						for pIdx := 0; pIdx < 12; pIdx++ {
							if loc := resolveMap(in.gatheringLeveRoutes.Int(rRow, fmt.Sprintf("PopRange[%d]", pIdx))); loc != nil {
								entry.GatheringTargetLocation = loc
								break
							}
						}
					}
				}
			}

		case "battle":
			if in.battleLeves != nil {
				if bRow, ok := in.battleLeves.Rows[dataID]; ok {
					if ruleID := in.battleLeves.Int(bRow, "Rule"); ruleID > 0 && in.battleLeveRules != nil {
						if rRow, ok := in.battleLeveRules.Rows[ruleID]; ok {
							entry.RuleName = strings.TrimSpace(in.battleLeveRules.Str(rRow, "Rule"))
						}
					}
					for idx := 0; idx < 3; idx++ {
						if obj := readLeveStr(in.battleLeves.Int(bRow, fmt.Sprintf("Objectives[%d]", idx))); obj != "" {
							entry.Objectives = append(entry.Objectives, obj)
						}
					}
					seenTargets := make(map[string]bool)
					seenObjItems := make(map[string]bool)
					for idx := 0; idx < 8; idx++ {
						bName := readBNpcName(in.battleLeves.Int(bRow, fmt.Sprintf("LeveData[%d].BNpcName", idx)))
						eLvl := in.battleLeves.Int(bRow, fmt.Sprintf("LeveData[%d].EnemyLevel", idx))
						if bName != "" {
							cnt := 0
							for a := 0; a < 8; a++ {
								cnt += in.battleLeves.Int(bRow, fmt.Sprintf("LeveData[%d].NumOfAppearance[%d]", idx, a))
							}
							if cnt <= 0 {
								cnt = in.battleLeves.Int(bRow, fmt.Sprintf("LeveData[%d].ToDoNumberInvolved", idx))
							}
							tKey := fmt.Sprintf("%s#%d#%d", bName, eLvl, cnt)
							if !seenTargets[tKey] {
								seenTargets[tKey] = true
								entry.CombatTargets = append(entry.CombatTargets, LeveCombatTarget{
									BNpcName: bName,
									Level:    eLvl,
									Count:    cnt,
								})
							}
						}
						evName := readEventItemName(in.battleLeves.Int(bRow, fmt.Sprintf("LeveData[%d].ItemsInvolved", idx)))
						qty := in.battleLeves.Int(bRow, fmt.Sprintf("LeveData[%d].ItemsInvolvedQty", idx))
						if evName != "" && !seenObjItems[evName] {
							seenObjItems[evName] = true
							entry.RequiredObjectiveItems = append(entry.RequiredObjectiveItems, LeveObjectiveItem{
								ItemName: evName,
								Count:    max(1, qty),
								DropRate: in.battleLeves.Int(bRow, fmt.Sprintf("LeveData[%d].ItemDropRate", idx)),
							})
						}
					}
				}
			}

		case "company":
			if in.companyLeves != nil {
				if cRow, ok := in.companyLeves.Rows[dataID]; ok {
					if ruleID := in.companyLeves.Int(cRow, "Rule"); ruleID > 0 && in.companyLeveRules != nil {
						if rRow, ok := in.companyLeveRules.Rows[ruleID]; ok {
							entry.RuleName = strings.TrimSpace(in.companyLeveRules.Str(rRow, "Type"))
							if obj := readLeveStr(in.companyLeveRules.Int(rRow, "Objective")); obj != "" {
								entry.Objectives = append(entry.Objectives, obj)
							}
						}
					}
					seenTargets := make(map[string]bool)
					seenObjItems := make(map[string]bool)
					for idx := 0; idx < 8; idx++ {
						bName := readBNpcName(in.companyLeves.Int(cRow, fmt.Sprintf("CompanyLeveStruct[%d].BNpcName", idx)))
						eLvl := in.companyLeves.Int(cRow, fmt.Sprintf("CompanyLeveStruct[%d].EnemyLevel", idx))
						if bName != "" {
							cnt := 0
							for a := 0; a < 8; a++ {
								cnt += in.companyLeves.Int(cRow, fmt.Sprintf("CompanyLeveStruct[%d].NumOfAppearance[%d]", idx, a))
							}
							tKey := fmt.Sprintf("%s#%d#%d", bName, eLvl, cnt)
							if !seenTargets[tKey] {
								seenTargets[tKey] = true
								entry.CombatTargets = append(entry.CombatTargets, LeveCombatTarget{
									BNpcName: bName,
									Level:    eLvl,
									Count:    cnt,
								})
							}
						}
						evName := readEventItemName(in.companyLeves.Int(cRow, fmt.Sprintf("CompanyLeveStruct[%d].ItemsInvolved", idx)))
						qty := in.companyLeves.Int(cRow, fmt.Sprintf("CompanyLeveStruct[%d].ItemsInvolvedQty", idx))
						if evName != "" && !seenObjItems[evName] {
							seenObjItems[evName] = true
							entry.RequiredObjectiveItems = append(entry.RequiredObjectiveItems, LeveObjectiveItem{
								ItemName: evName,
								Count:    max(1, qty),
								DropRate: in.companyLeves.Int(cRow, fmt.Sprintf("CompanyLeveStruct[%d].ItemDropRate", idx)),
							})
						}
					}
				}
			}
		}

		// 解析随机奖励道具池
		rewardProbByItem := make(map[int]int)
		rewardMaxCountByItem := make(map[int]int)
		if rewID := in.leves.Int(row, "LeveRewardItem"); rewID > 0 && in.leveRewardItems != nil && in.leveRewardItemGroups != nil {
			if lrRow, ok := in.leveRewardItems.Rows[rewID]; ok {
				for gIdx := 0; gIdx < 8; gIdx++ {
					grpID := in.leveRewardItems.Int(lrRow, fmt.Sprintf("LeveRewardItemGroup[%d]", gIdx))
					prob := in.leveRewardItems.Int(lrRow, fmt.Sprintf("ProbabilityPercent[%d]", gIdx))
					if grpID <= 0 || prob <= 0 {
						continue
					}
					grpRow, ok := in.leveRewardItemGroups.Rows[grpID]
					if !ok {
						continue
					}
					var grpEntries []LeveRewardEntry
					seenInGroup := make(map[int]bool)
					for iIdx := 0; iIdx < 9; iIdx++ {
						itemID := in.leveRewardItemGroups.Int(grpRow, fmt.Sprintf("Item[%d]", iIdx))
						if itemID <= 0 {
							continue
						}
						cnt := max(1, in.leveRewardItemGroups.Int(grpRow, fmt.Sprintf("Count[%d]", iIdx)))
						isHQ := in.leveRewardItemGroups.Bool(grpRow, fmt.Sprintf("IsHQ[%d]", iIdx))
						it := in.itemSummaryByID[itemID]
						itemName := it.NameCn
						if itemName == "" {
							itemName = i18n.Tf(in.appLang, "gamedata.item_fallback", "id", fmt.Sprintf("%d", itemID))
						}
						grpEntries = append(grpEntries, LeveRewardEntry{
							ItemID:   itemID,
							ItemName: itemName,
							IconID:   it.IconID,
							Count:    cnt,
							IsHQ:     isHQ,
						})
						if !seenInGroup[itemID] {
							seenInGroup[itemID] = true
							rewardProbByItem[itemID] += prob
						}
						if cnt > rewardMaxCountByItem[itemID] {
							rewardMaxCountByItem[itemID] = cnt
						}
					}
					if len(grpEntries) > 0 {
						entry.RewardGroups = append(entry.RewardGroups, LeveRewardGroup{
							ProbabilityPercent: prob,
							Items:              grpEntries,
						})
					}
				}
			}
		}

		for itemID, prob := range rewardProbByItem {
			out.levesByItem[itemID] = append(out.levesByItem[itemID], ItemLeveSource{
				LeveID:             leveID,
				NameCn:             nameCn,
				AssignmentTypeName: assignName,
				Level:              levelNum,
				Count:              rewardMaxCountByItem[itemID],
				ProbabilityPercent: min(100, prob),
				IsTurnIn:           false,
			})
		}

		out.leves = append(out.leves, entry)
		out.leveByID[leveID] = entry
	}

	sort.Slice(out.leves, func(i, j int) bool {
		if out.leves[i].ClassJobLevel != out.leves[j].ClassJobLevel {
			return out.leves[i].ClassJobLevel < out.leves[j].ClassJobLevel
		}
		return out.leves[i].ID < out.leves[j].ID
	})

	for id, meta := range typeMeta {
		if cnt := typeCount[id]; cnt > 0 {
			meta.Count = cnt
			out.assignmentTypeList = append(out.assignmentTypeList, meta)
		}
	}
	sort.Slice(out.assignmentTypeList, func(i, j int) bool {
		return out.assignmentTypeList[i].ID < out.assignmentTypeList[j].ID
	})

	for _, list := range out.levesByItem {
		sort.Slice(list, func(i, j int) bool {
			if list[i].Level != list[j].Level {
				return list[i].Level < list[j].Level
			}
			return list[i].LeveID < list[j].LeveID
		})
	}

	return out
}
