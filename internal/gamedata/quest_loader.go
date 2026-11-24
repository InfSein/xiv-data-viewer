package gamedata

import (
	"fmt"
	"sort"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

// FF14 任务类型标准图标 ID（144×144，对应 ui/icon/061000/06143x.tex）。
const (
	QuestIconSide       = 61431
	QuestIconMSQ        = 61432
	QuestIconRepeatable = 61433
	QuestIconFeature    = 61439
)

var questNeededFields = func() []string {
	fields := []string{
		"Name", "Id",
		"GilReward", "CurrencyReward", "CurrencyRewardCount",
		"Reward[0]", "Reward[1]", "Reward[2]", "Reward[3]", "Reward[4]", "Reward[5]", "Reward[6]",
		"OptionalItemReward[0]", "OptionalItemReward[1]", "OptionalItemReward[2]", "OptionalItemReward[3]", "OptionalItemReward[4]",
		"InstanceContentUnlock", "ExpFactor", "EmoteReward", "ActionReward",
		"ItemCatalyst[0]", "ItemCatalyst[1]", "ItemCatalyst[2]",
		"ItemCountCatalyst[0]", "ItemCountCatalyst[1]", "ItemCountCatalyst[2]",
		"ItemRewardType",
		"ItemCountReward[0]", "ItemCountReward[1]", "ItemCountReward[2]", "ItemCountReward[3]", "ItemCountReward[4]", "ItemCountReward[5]", "ItemCountReward[6]",
		"OptionalItemCountReward[0]", "OptionalItemCountReward[1]", "OptionalItemCountReward[2]", "OptionalItemCountReward[3]", "OptionalItemCountReward[4]",
		"GeneralActionReward[0]", "GeneralActionReward[1]", "OtherReward",
		"Tomestone", "TomestoneReward", "TomestoneCountReward", "ReputationReward",
		"OptionalItemIsHQReward[0]", "OptionalItemIsHQReward[1]", "OptionalItemIsHQReward[2]", "OptionalItemIsHQReward[3]", "OptionalItemIsHQReward[4]",
		"PreviousQuest[0]", "PreviousQuest[1]", "PreviousQuest[2]", "PreviousQuestJoin",
		"QuestLock[0]", "QuestLock[1]",
		"InstanceContent[0]", "InstanceContent[1]", "InstanceContent[2]",
		"IssuerStart", "IssuerLocation", "TargetEnd", "JournalGenre", "Icon", "IconSpecial",
		"MountRequired", "ClassJobLevel[0]", "ClassJobLevel[1]",
		"BellStart", "BellEnd", "BeastReputationValue", "PlaceName", "SortKey", "Expansion",
		"ClassJobCategory0", "ClassJobCategory1", "QuestLevelOffset",
		"ClassJobUnlock", "GrandCompany", "BeastTribe", "BeastReputationRank",
		"RepeatIntervalType", "LevelMax", "ClassJobRequired", "EventIconType",
		"IsHouseRequired", "IsRepeatable", "CanCancel",
	}
	for i := 0; i < 50; i++ {
		fields = append(fields, fmt.Sprintf("QuestParams[%d].ScriptArg", i))
	}
	for i := 0; i < 64; i++ {
		fields = append(fields, fmt.Sprintf("QuestListenerParams[%d].Listener", i))
	}
	return fields
}()

type questLoaderInput struct {
	quests                  *sqpack.SchemaSheet
	questsEn                *sqpack.SchemaSheet
	questsJa                *sqpack.SchemaSheet
	journalGenres           *sqpack.SchemaSheet
	journalCategories       *sqpack.SchemaSheet
	journalSections         *sqpack.SchemaSheet
	questClassJobRewards    *sqpack.SchemaSubrowSheet
	questRewardOthers       *sqpack.SchemaSheet
	eventIconTypes          *sqpack.SchemaSheet
	paramGrows              *sqpack.SchemaSheet
	beastTribes             *sqpack.SchemaSheet
	beastRankBonuses        *sqpack.SchemaSheet
	beastReputationRanks    *sqpack.SchemaSheet
	grandCompanies          *sqpack.SchemaSheet
	exVersions              *sqpack.SchemaSheet
	mounts                  *sqpack.SchemaSheet
	actions                 *sqpack.SchemaSheet
	emotes                  *sqpack.SchemaSheet
	generalActions          *sqpack.SchemaSheet
	instanceContents        *sqpack.SchemaSheet
	contentFinderConditions *sqpack.SchemaSheet
	eobjNames               *sqpack.SchemaSheet
	npcNames                map[int]RawNpcName
	npcLocations            map[int]RawNpcLocation
	levelLocations          map[int]RawNpcLocation
	tomestoneByIndex        map[int]int
	classJobs               map[int]classJobDict
	classJobCategories      map[int]string
	placeNames              map[int]string
	territoryTypes          map[int]sqpack.Row
	maps                    map[int]sqpack.Row
	itemMeta                func(int) (string, int)
}

type questIndices struct {
	quests             []QuestEntry
	questByID          map[int]QuestEntry
	questsByRewardItem map[int][]ItemQuestSource
	sectionList        []QuestSectionInfo
	questActorIDs      map[int][]int
}

// CleanQuestTitle 去除任务标题前缀中的 FF14 私有字体符号（如 \ue0be 等级调整、\ue0bf 固定职业）。
func CleanQuestTitle(name string) string {
	return sqpack.CleanPUARunes(name)
}

func resolveQuestTypeIcon(isMSQ, isFeature, isRepeatable bool) int {
	switch {
	case isMSQ:
		return QuestIconMSQ
	case isFeature:
		return QuestIconFeature
	case isRepeatable:
		return QuestIconRepeatable
	default:
		return QuestIconSide
	}
}

func repeatIntervalText(repeatType int, isRepeatable bool) string {
	switch repeatType {
	case 1:
		return "每日可重复"
	case 2:
		return "每周可重复"
	default:
		if isRepeatable {
			return "可重复"
		}
		return ""
	}
}

func buildQuestData(in questLoaderInput) questIndices {
	out := questIndices{
		questByID:          make(map[int]QuestEntry),
		questsByRewardItem: make(map[int][]ItemQuestSource),
		questActorIDs:      make(map[int][]int),
	}
	if in.quests == nil || len(in.quests.Rows) == 0 {
		return out
	}

	readQuestName := func(qID int) string {
		if qID <= 0 {
			return ""
		}
		if r, ok := in.quests.Rows[qID]; ok {
			return strings.TrimSpace(in.quests.Str(r, "Name"))
		}
		return ""
	}

	readActorName := func(actorID int) string {
		if actorID <= 0 {
			return ""
		}
		if n, ok := in.npcNames[actorID]; ok && n.Name != "" {
			return n.Name
		}
		if in.eobjNames != nil {
			if row, ok := in.eobjNames.Rows[actorID]; ok {
				return strings.TrimSpace(in.eobjNames.Str(row, "Singular"))
			}
		}
		return ""
	}

	resolveInstanceName := func(icID int) string {
		if icID <= 0 || in.instanceContents == nil {
			return ""
		}
		icRow, ok := in.instanceContents.Rows[icID]
		if !ok {
			return ""
		}
		cfcID := in.instanceContents.Int(icRow, "ContentFinderCondition")
		if cfcID > 0 && in.contentFinderConditions != nil {
			if cfcRow, ok := in.contentFinderConditions.Rows[cfcID]; ok {
				return strings.TrimSpace(in.contentFinderConditions.Str(cfcRow, "Name"))
			}
		}
		return ""
	}

	resolveItem := func(itemID int) (string, int) {
		if in.itemMeta != nil {
			return in.itemMeta(itemID)
		}
		return "", 0
	}

	sectionCount := make(map[int]int)
	sectionNames := make(map[int]string)

	out.quests = make([]QuestEntry, 0, len(in.quests.Rows))
	for questID, row := range in.quests.Rows {
		if questID <= 0 {
			continue
		}
		nameCn := strings.TrimSpace(in.quests.Str(row, "Name"))
		if nameCn == "" {
			continue
		}

		nameEn := ""
		if in.questsEn != nil {
			if r, ok := in.questsEn.Rows[questID]; ok {
				nameEn = strings.TrimSpace(in.questsEn.Str(r, "Name"))
			}
		}
		nameJa := ""
		if in.questsJa != nil {
			if r, ok := in.questsJa.Rows[questID]; ok {
				nameJa = strings.TrimSpace(in.questsJa.Str(r, "Name"))
			}
		}

		genreID := in.quests.Int(row, "JournalGenre")
		genreName := ""
		catName := ""
		sectionID := -1
		sectionName := ""
		if genreID > 0 && in.journalGenres != nil {
			if gRow, ok := in.journalGenres.Rows[genreID]; ok {
				genreName = strings.TrimSpace(in.journalGenres.Str(gRow, "Name"))
				catID := in.journalGenres.Int(gRow, "JournalCategory")
				if catID > 0 && in.journalCategories != nil {
					if cRow, ok := in.journalCategories.Rows[catID]; ok {
						catName = strings.TrimSpace(in.journalCategories.Str(cRow, "Name"))
						secID := in.journalCategories.Int(cRow, "JournalSection")
						if secID >= 0 && secID < 255 && in.journalSections != nil {
							if sRow, ok := in.journalSections.Rows[secID]; ok {
								sectionID = secID
								sectionName = strings.TrimSpace(in.journalSections.Str(sRow, "Name"))
							}
						}
					}
				}
			}
		}
		if sectionID >= 0 && sectionName != "" {
			sectionCount[sectionID]++
			sectionNames[sectionID] = sectionName
		}

		eventIconType := in.quests.Int(row, "EventIconType")
		eventIconID := 0
		if eventIconType > 0 && in.eventIconTypes != nil {
			if eRow, ok := in.eventIconTypes.Rows[eventIconType]; ok {
				eventIconID = in.eventIconTypes.Int(eRow, "NpcIconAvailable")
			}
		}
		bannerIconID := in.quests.Int(row, "Icon")
		specialIconID := in.quests.Int(row, "IconSpecial")

		isMSQ := eventIconType == 3 || eventIconID == 71200 || sectionID == 0 || sectionID == 1
		isFeature := eventIconType == 8 || eventIconType == 10 || eventIconID == 71340
		hasLevelSync := strings.ContainsRune(nameCn, '\ue0be')
		isJobFixed := strings.ContainsRune(nameCn, '\ue0bf')

		expID := in.quests.Int(row, "Expansion")
		expName := ""
		if in.exVersions != nil {
			if exRow, ok := in.exVersions.Rows[expID]; ok {
				expName = strings.TrimSpace(in.exVersions.Str(exRow, "Name"))
			}
		}

		lvl := in.quests.Int(row, "ClassJobLevel[0]")
		expFactor := in.quests.Int(row, "ExpFactor")
		expReward := 0
		if lvl > 0 && expFactor > 0 && in.paramGrows != nil {
			if pgRow, ok := in.paramGrows.Rows[lvl]; ok {
				scaledXP := in.paramGrows.Int(pgRow, "ScaledQuestXP")
				expReward = scaledXP * expFactor / 100
			}
		}

		issuerID := in.quests.Int(row, "IssuerStart")
		issuerLocID := in.quests.Int(row, "IssuerLocation")
		var issuerLoc *MapLocationPoint
		if rawLoc, ok := in.levelLocations[issuerLocID]; ok {
			issuerLoc = resolveMapLocation(rawLoc, in.territoryTypes, in.maps, in.placeNames)
		} else if rawLoc, ok := in.npcLocations[issuerID]; ok {
			issuerLoc = resolveMapLocation(rawLoc, in.territoryTypes, in.maps, in.placeNames)
		}

		targetID := in.quests.Int(row, "TargetEnd")
		var targetLoc *MapLocationPoint
		if rawLoc, ok := in.npcLocations[targetID]; ok {
			targetLoc = resolveMapLocation(rawLoc, in.territoryTypes, in.maps, in.placeNames)
		}

		var prevIDs []int
		var prevNames []string
		for i := 0; i < 3; i++ {
			pqID := in.quests.Int(row, fmt.Sprintf("PreviousQuest[%d]", i))
			if pqID > 0 {
				prevIDs = append(prevIDs, pqID)
				pName := readQuestName(pqID)
				if pName == "" {
					pName = fmt.Sprintf("任务 #%d", pqID)
				}
				prevNames = append(prevNames, pName)
			}
		}

		var lockIDs []int
		var lockNames []string
		for i := 0; i < 2; i++ {
			lID := in.quests.Int(row, fmt.Sprintf("QuestLock[%d]", i))
			if lID > 0 {
				lockIDs = append(lockIDs, lID)
				lName := readQuestName(lID)
				if lName == "" {
					lName = fmt.Sprintf("任务 #%d", lID)
				}
				lockNames = append(lockNames, lName)
			}
		}

		var instNames []string
		for i := 0; i < 3; i++ {
			if n := resolveInstanceName(in.quests.Int(row, fmt.Sprintf("InstanceContent[%d]", i))); n != "" {
				instNames = append(instNames, n)
			}
		}

		gcName := ""
		if gcID := in.quests.Int(row, "GrandCompany"); gcID > 0 && in.grandCompanies != nil {
			if gcRow, ok := in.grandCompanies.Rows[gcID]; ok {
				gcName = strings.TrimSpace(in.grandCompanies.Str(gcRow, "Singular"))
			}
		}

		btName := ""
		if btID := in.quests.Int(row, "BeastTribe"); btID > 0 && in.beastTribes != nil {
			if btRow, ok := in.beastTribes.Rows[btID]; ok {
				btName = strings.TrimSpace(in.beastTribes.Str(btRow, "Name"))
			}
		}
		btRankName := ""
		if rankID := in.quests.Int(row, "BeastReputationRank"); rankID > 0 && in.beastReputationRanks != nil {
			if rRow, ok := in.beastReputationRanks.Rows[rankID]; ok {
				btRankName = strings.TrimSpace(in.beastReputationRanks.Str(rRow, "Name"))
			}
		}

		mountName := ""
		if mountID := in.quests.Int(row, "MountRequired"); mountID > 0 && in.mounts != nil {
			if mRow, ok := in.mounts.Rows[mountID]; ok {
				mountName = strings.TrimSpace(in.mounts.Str(mRow, "Singular"))
			}
		}

		currItemID := in.quests.Int(row, "CurrencyReward")
		currCount := in.quests.Int(row, "CurrencyRewardCount")
		currName, currIcon := resolveItem(currItemID)

		tomeRaw := in.quests.Int(row, "TomestoneReward")
		tomeIdx := in.quests.Int(row, "Tomestone")
		tomeCount := in.quests.Int(row, "TomestoneCountReward")
		tomeItemID := 0
		if tomeRaw >= 10 {
			tomeItemID = tomeRaw
		} else if tomeRaw > 0 {
			tomeItemID = in.tomestoneByIndex[tomeRaw]
		} else if tomeIdx > 0 {
			tomeItemID = in.tomestoneByIndex[tomeIdx]
		}
		tomeName, tomeIcon := resolveItem(tomeItemID)

		isRepeatable := in.quests.Bool(row, "IsRepeatable")
		repeatLabel := repeatIntervalText(in.quests.Int(row, "RepeatIntervalType"), isRepeatable)
		if repeatLabel != "" {
			isRepeatable = true
		}
		iconID := resolveQuestTypeIcon(isMSQ, isFeature, isRepeatable)

		// 收集该任务脚本引用的 NPC/EObj ID，供对话按需解析说话人中文名使用
		var actorIDs []int
		actorSeen := make(map[int]bool)
		addActor := func(aid int) {
			if aid <= 0 || actorSeen[aid] {
				return
			}
			if _, ok := in.npcNames[aid]; ok {
				actorSeen[aid] = true
				actorIDs = append(actorIDs, aid)
				return
			}
			if in.eobjNames != nil {
				if _, ok := in.eobjNames.Rows[aid]; ok {
					actorSeen[aid] = true
					actorIDs = append(actorIDs, aid)
				}
			}
		}
		addActor(issuerID)
		addActor(targetID)
		for i := 0; i < 50; i++ {
			addActor(in.quests.Int(row, fmt.Sprintf("QuestParams[%d].ScriptArg", i)))
		}
		for i := 0; i < 64; i++ {
			addActor(in.quests.Int(row, fmt.Sprintf("QuestListenerParams[%d].Listener", i)))
		}
		if len(actorIDs) > 0 {
			out.questActorIDs[questID] = actorIDs
		}

		entry := QuestEntry{
			ID:                    questID,
			InternalID:            strings.TrimSpace(in.quests.Str(row, "Id")),
			NameCn:                nameCn,
			NameEn:                nameEn,
			NameJa:                nameJa,
			IconID:                iconID,
			BannerIconID:          bannerIconID,
			SpecialIconID:         specialIconID,
			EventIconID:           eventIconID,
			HasLevelSync:          hasLevelSync,
			IsJobFixed:            isJobFixed,
			SectionID:             sectionID,
			SectionName:           sectionName,
			CategoryName:          catName,
			GenreID:               genreID,
			GenreName:             genreName,
			ExpansionID:           expID,
			ExpansionName:         expName,
			PlaceName:             in.placeNames[in.quests.Int(row, "PlaceName")],
			SortKey:               in.quests.Int(row, "SortKey"),
			Level:                 lvl,
			LevelOffset:           in.quests.Int(row, "QuestLevelOffset"),
			LevelMax:              in.quests.Int(row, "LevelMax"),
			ClassJobCategoryName:  in.classJobCategories[in.quests.Int(row, "ClassJobCategory0")],
			ClassJobCategory2Name: in.classJobCategories[in.quests.Int(row, "ClassJobCategory1")],
			Level2:                in.quests.Int(row, "ClassJobLevel[1]"),
			ClassJobRequiredName:  in.classJobs[in.quests.Int(row, "ClassJobRequired")].name,
			IssuerNpcID:           issuerID,
			IssuerName:            readActorName(issuerID),
			IssuerLocation:        issuerLoc,
			TargetNpcID:           targetID,
			TargetName:            readActorName(targetID),
			TargetLocation:        targetLoc,
			PreviousQuestIDs:      prevIDs,
			PreviousQuestNames:    prevNames,
			PreviousQuestJoin:     in.quests.Int(row, "PreviousQuestJoin"),
			QuestLockIDs:          lockIDs,
			QuestLockNames:        lockNames,
			InstanceContentNames:  instNames,
			GrandCompanyName:      gcName,
			BeastTribeName:        btName,
			BeastReputationRank:   btRankName,
			BeastReputationValue:  in.quests.Int(row, "BeastReputationValue"),
			MountRequiredName:     mountName,
			IsHouseRequired:       in.quests.Bool(row, "IsHouseRequired"),
			IsRepeatable:          isRepeatable,
			RepeatIntervalLabel:   repeatLabel,
			CanCancel:             in.quests.Bool(row, "CanCancel"),
			BellStart:             in.quests.Int(row, "BellStart"),
			BellEnd:               in.quests.Int(row, "BellEnd"),
			IsMSQ:                 isMSQ,
			IsFeature:             isFeature,
			GilReward:             in.quests.Int(row, "GilReward"),
			ExpFactor:             expFactor,
			ExpReward:             expReward,
			CurrencyRewardItemID:  currItemID,
			CurrencyRewardName:    currName,
			CurrencyRewardIconID:  currIcon,
			CurrencyRewardCount:   currCount,
			TomestoneItemID:       tomeItemID,
			TomestoneName:         tomeName,
			TomestoneIconID:       tomeIcon,
			TomestoneCount:        tomeCount,
			ReputationReward:      in.quests.Int(row, "ReputationReward"),
		}

		// 解析奖励道具（固定 / 分性别 / 按职业分支 / 友好部族 / 可选 / 魔晶石催化剂）
		rewardSeen := make(map[string]bool)
		addRewardItem := func(itemID, count int, isHQ bool, groupLabel, cjCatName string) {
			if itemID <= 0 {
				return
			}
			if count <= 0 {
				count = 1
			}
			iName, iIcon := resolveItem(itemID)
			if iName == "" {
				iName = fmt.Sprintf("物品 #%d", itemID)
			}
			dedupKey := fmt.Sprintf("%d#%s#%s#%t", itemID, groupLabel, cjCatName, isHQ)
			if rewardSeen[dedupKey] {
				return
			}
			rewardSeen[dedupKey] = true
			entry.RewardItems = append(entry.RewardItems, QuestRewardItem{
				ItemID:           itemID,
				ItemName:         iName,
				IconID:           iIcon,
				Count:            count,
				IsHQ:             isHQ,
				GroupLabel:       groupLabel,
				ClassJobCategory: cjCatName,
			})
			out.questsByRewardItem[itemID] = append(out.questsByRewardItem[itemID], ItemQuestSource{
				QuestID:    questID,
				NameCn:     nameCn,
				GenreName:  genreName,
				Level:      lvl,
				Count:      count,
				GroupLabel: groupLabel,
			})
		}

		rewardType := in.quests.Int(row, "ItemRewardType")
		for idx := 0; idx < 7; idx++ {
			rewVal := in.quests.Int(row, fmt.Sprintf("Reward[%d]", idx))
			if rewVal <= 0 {
				continue
			}
			rewCnt := in.quests.Int(row, fmt.Sprintf("ItemCountReward[%d]", idx))
			switch rewardType {
			case 6:
				if in.questClassJobRewards != nil {
					for _, sub := range in.questClassJobRewards.Rows[rewVal] {
						subCJ := in.classJobCategories[in.questClassJobRewards.Int(sub.Data, "ClassJobCategory")]
						for k := 0; k < 4; k++ {
							itID := in.questClassJobRewards.Int(sub.Data, fmt.Sprintf("RewardItem[%d]", k))
							itCnt := in.questClassJobRewards.Int(sub.Data, fmt.Sprintf("RewardAmount[%d]", k))
							if itID > 0 {
								addRewardItem(itID, itCnt, false, "职业奖励", subCJ)
							}
						}
					}
				}
			case 7:
				if in.beastRankBonuses != nil {
					if brRow, ok := in.beastRankBonuses.Rows[rewVal]; ok {
						itID := in.beastRankBonuses.Int(brRow, "Item")
						itCnt := 1
						for k := 0; k < 8; k++ {
							if q := in.beastRankBonuses.Int(brRow, fmt.Sprintf("ItemQuantity[%d]", k)); q > 0 {
								itCnt = q
								break
							}
						}
						addRewardItem(itID, itCnt, false, "部族奖励", "")
					}
				}
			case 3:
				grp := "固定奖励"
				if idx == 0 {
					grp = "固定奖励（男）"
				} else if idx == 1 {
					grp = "固定奖励（女）"
				}
				addRewardItem(rewVal, rewCnt, false, grp, "")
			default:
				addRewardItem(rewVal, rewCnt, false, "固定奖励", "")
			}
		}

		for idx := 0; idx < 5; idx++ {
			optID := in.quests.Int(row, fmt.Sprintf("OptionalItemReward[%d]", idx))
			if optID <= 0 {
				continue
			}
			optCnt := in.quests.Int(row, fmt.Sprintf("OptionalItemCountReward[%d]", idx))
			optHQ := in.quests.Bool(row, fmt.Sprintf("OptionalItemIsHQReward[%d]", idx))
			addRewardItem(optID, optCnt, optHQ, "自选奖励", "")
		}

		for idx := 0; idx < 3; idx++ {
			catID := in.quests.Int(row, fmt.Sprintf("ItemCatalyst[%d]", idx))
			if catID <= 0 {
				continue
			}
			catCnt := in.quests.Int(row, fmt.Sprintf("ItemCountCatalyst[%d]", idx))
			addRewardItem(catID, catCnt, false, "额外道具", "")
		}

		if currItemID > 0 && currCount > 0 {
			out.questsByRewardItem[currItemID] = append(out.questsByRewardItem[currItemID], ItemQuestSource{
				QuestID:    questID,
				NameCn:     nameCn,
				GenreName:  genreName,
				Level:      lvl,
				Count:      currCount,
				GroupLabel: "货币奖励",
			})
		}
		if tomeItemID > 0 && tomeCount > 0 {
			out.questsByRewardItem[tomeItemID] = append(out.questsByRewardItem[tomeItemID], ItemQuestSource{
				QuestID:    questID,
				NameCn:     nameCn,
				GenreName:  genreName,
				Level:      lvl,
				Count:      tomeCount,
				GroupLabel: "神典石奖励",
			})
		}

		// 解锁内容（职业 / 副本 / 情感动作 / 技能 / 通用技能 / 其他）
		if cjUnlock := in.quests.Int(row, "ClassJobUnlock"); cjUnlock > 0 {
			if cjName := in.classJobs[cjUnlock].name; cjName != "" {
				entry.Unlocks = append(entry.Unlocks, QuestUnlockReward{
					Kind:  "job",
					Label: "解锁职业：" + cjName,
				})
			}
		}
		if icUnlockName := resolveInstanceName(in.quests.Int(row, "InstanceContentUnlock")); icUnlockName != "" {
			entry.Unlocks = append(entry.Unlocks, QuestUnlockReward{
				Kind:  "instance",
				Label: "解锁副本：" + icUnlockName,
			})
		}
		if emoteID := in.quests.Int(row, "EmoteReward"); emoteID > 0 && in.emotes != nil {
			if eRow, ok := in.emotes.Rows[emoteID]; ok {
				if eName := strings.TrimSpace(in.emotes.Str(eRow, "Name")); eName != "" {
					entry.Unlocks = append(entry.Unlocks, QuestUnlockReward{
						Kind:   "emote",
						Label:  "情感动作：" + eName,
						IconID: in.emotes.Int(eRow, "Icon"),
					})
				}
			}
		}
		if actID := in.quests.Int(row, "ActionReward"); actID > 0 && in.actions != nil {
			if aRow, ok := in.actions.Rows[actID]; ok {
				if aName := strings.TrimSpace(in.actions.Str(aRow, "Name")); aName != "" {
					entry.Unlocks = append(entry.Unlocks, QuestUnlockReward{
						Kind:   "action",
						Label:  "技能：" + aName,
						IconID: in.actions.Int(aRow, "Icon"),
					})
				}
			}
		}
		for idx := 0; idx < 2; idx++ {
			gaID := in.quests.Int(row, fmt.Sprintf("GeneralActionReward[%d]", idx))
			if gaID > 0 && in.generalActions != nil {
				if gRow, ok := in.generalActions.Rows[gaID]; ok {
					if gName := strings.TrimSpace(in.generalActions.Str(gRow, "Name")); gName != "" {
						entry.Unlocks = append(entry.Unlocks, QuestUnlockReward{
							Kind:   "general",
							Label:  "共通技能：" + gName,
							IconID: in.generalActions.Int(gRow, "Icon"),
						})
					}
				}
			}
		}
		if otherID := in.quests.Int(row, "OtherReward"); otherID > 0 && in.questRewardOthers != nil {
			if oRow, ok := in.questRewardOthers.Rows[otherID]; ok {
				if oName := strings.TrimSpace(in.questRewardOthers.Str(oRow, "Name")); oName != "" {
					entry.Unlocks = append(entry.Unlocks, QuestUnlockReward{
						Kind:   "other",
						Label:  oName,
						IconID: in.questRewardOthers.Int(oRow, "Icon"),
					})
				}
			}
		}

		out.quests = append(out.quests, entry)
		out.questByID[questID] = entry
	}

	sort.Slice(out.quests, func(i, j int) bool {
		if out.quests[i].Level != out.quests[j].Level {
			return out.quests[i].Level < out.quests[j].Level
		}
		if out.quests[i].SortKey != out.quests[j].SortKey {
			return out.quests[i].SortKey < out.quests[j].SortKey
		}
		return out.quests[i].ID < out.quests[j].ID
	})

	for secID, name := range sectionNames {
		out.sectionList = append(out.sectionList, QuestSectionInfo{
			ID:    secID,
			Name:  name,
			Count: sectionCount[secID],
		})
	}
	sort.Slice(out.sectionList, func(i, j int) bool {
		return out.sectionList[i].ID < out.sectionList[j].ID
	})

	for itemID, list := range out.questsByRewardItem {
		seen := make(map[int]bool, len(list))
		deduped := make([]ItemQuestSource, 0, len(list))
		for _, src := range list {
			if !seen[src.QuestID] {
				seen[src.QuestID] = true
				deduped = append(deduped, src)
			}
		}
		sort.Slice(deduped, func(i, j int) bool {
			if deduped[i].Level != deduped[j].Level {
				return deduped[i].Level < deduped[j].Level
			}
			return deduped[i].QuestID < deduped[j].QuestID
		})
		out.questsByRewardItem[itemID] = deduped
	}

	return out
}
