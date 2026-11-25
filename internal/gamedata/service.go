package gamedata

import (
	"fmt"
	"image"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

func resolveAppLang(lang i18n.Lang) i18n.Lang {
	if lang == "" {
		return i18n.LangZH
	}
	return i18n.NormalizeLang(string(lang))
}

func (s *Service) appLang() i18n.Lang {
	if s == nil || s.store == nil {
		return i18n.LangZH
	}
	return resolveAppLang(s.store.AppLang)
}

func fishingSpotCategoryName(lang i18n.Lang, cat int) string {
	lang = resolveAppLang(lang)
	switch cat {
	case 1, 2, 3, 4, 5, 6, 7:
		return i18n.T(lang, fmt.Sprintf("gamedata.fish_cat.%d", cat))
	default:
		return ""
	}
}

func hhmmToMinutes(hhmm int) int {
	return (hhmm/100)*60 + (hhmm % 100)
}

func minutesToTimeStr(totalMinutes int) string {
	norm := ((totalMinutes % 1440) + 1440) % 1440
	return fmt.Sprintf("%02d:%02d", norm/60, norm%60)
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func convertWorldCoordToMap(worldX, worldY float64, sizeFactor int, offsetX, offsetY float64) (coordX, coordY, normX, normY float64) {
	if sizeFactor <= 0 {
		sizeFactor = 100
	}
	scale := float64(sizeFactor) / 100.0
	normX = clamp01(((worldX+offsetX)*scale + 1024.0) / 2048.0)
	normY = clamp01(((worldY+offsetY)*scale + 1024.0) / 2048.0)
	coordX = round1((41.0/scale)*normX + 1.0)
	coordY = round1((41.0/scale)*normY + 1.0)
	return
}

func convertPixelCoordToMap(pixelX, pixelY float64, sizeFactor int) (coordX, coordY, normX, normY float64) {
	if sizeFactor <= 0 {
		sizeFactor = 100
	}
	scale := float64(sizeFactor) / 100.0
	normX = clamp01(pixelX / 2048.0)
	normY = clamp01(pixelY / 2048.0)
	coordX = round1((41.0/scale)*normX + 1.0)
	coordY = round1((41.0/scale)*normY + 1.0)
	return
}

// Service 提供面向界面的道具检索、详情解析、配方树递归、采集坐标、商店兑换与成就查询接口。
type Service struct {
	store *Store

	questTextMu    sync.Mutex
	questTextCache map[int]*QuestTextData
	questTextOrder []int

	actorEnOnce  sync.Once
	actorEnNames map[int]string
}

// NewService 基于已加载的 Store 创建核心游戏数据查询服务。
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Store 返回底层内存仓库实例。
func (s *Service) Store() *Store {
	return s.store
}

// Close 释放底层 SqPack 文件句柄。
func (s *Service) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

// GetSystemStatus 返回系统概览统计与字典元数据。
func (s *Service) GetSystemStatus() SystemStatus {
	var craftable, gatherable, purchasable, gilShop, specialShop, retainer, collectableTurnIn int
	for _, it := range s.store.ItemSummaries {
		if it.IsCraftable {
			craftable++
		}
		if it.IsGatherable {
			gatherable++
		}
		if it.IsPurchasable {
			purchasable++
		}
		if it.IsGilShop {
			gilShop++
		}
		if it.IsSpecialShop {
			specialShop++
		}
		if it.IsRetainerVenture {
			retainer++
		}
		if it.IsCollectableTurnIn {
			collectableTurnIn++
		}
	}

	return SystemStatus{
		HasCN:          s.store.HasCN,
		HasGlobal:      s.store.HasGlobal,
		CNVersion:      s.store.CNVersion,
		GlobalVersion:  s.store.GlobalVersion,
		PrimaryVersion: s.store.PrimaryVersion,
		CNPath:         s.store.CNPath,
		GlobalPath:     s.store.GlobalPath,
		Stats: SystemStats{
			TotalItems:             len(s.store.ItemSummaries),
			CraftableItems:         craftable,
			GatherableItems:        gatherable,
			PurchasableItems:       purchasable,
			GilShopItems:           gilShop,
			SpecialShopItems:       specialShop,
			RetainerVentureItems:   retainer,
			CollectableTurnInItems: collectableTurnIn,
			TotalRecipes:           len(s.store.Recipes),
			TotalGatheringPoints:   len(s.store.GatheringPoints),
			TotalFishingSpots:      len(s.store.FishingSpots),
			TotalShops:             s.store.TotalActiveShops,
			TotalRetainerTasks:     s.store.TotalRetainerTasks,
			TotalAchievements:      len(s.store.Achievements),
			TotalTitles:            s.store.TotalTitles,
			TotalQuests:            len(s.store.Quests),
			TotalLeves:             len(s.store.Leves),
			TotalCollectables:      s.store.TotalCollectables,
			LoadTimeMs:             s.store.LoadTimeMs,
		},
		Categories:            s.store.CategoryList,
		CraftTypes:            s.store.CraftTypeList,
		AchievementKinds:      s.store.AchievementKindList,
		AchievementCategories: s.store.AchievementCategoryList,
		QuestSections:         s.store.QuestSectionList,
		LeveAssignmentTypes:   s.store.LeveAssignmentTypes,
	}
}

func (s *Service) filterAndSortItems(query ItemSearchQuery) []ItemSummary {
	q := strings.ToLower(strings.TrimSpace(query.Q))
	numericQuery := -1
	if q != "" {
		if parsed, err := strconv.Atoi(q); err == nil && parsed > 0 {
			numericQuery = parsed
		}
	}
	filter := query.Filter
	if filter == "" {
		filter = "all"
	}

	filtered := make([]ItemSummary, 0, 256)
	for _, it := range s.store.ItemSummaries {
		if query.CategoryID > 0 && it.CategoryID != query.CategoryID {
			continue
		}
		if query.MinIlvl > 0 && it.ItemLevel < query.MinIlvl {
			continue
		}
		if query.MaxIlvl > 0 && it.ItemLevel > query.MaxIlvl {
			continue
		}
		if query.TradableOnly && !it.IsTradable {
			continue
		}

		switch filter {
		case "craftable":
			if !it.IsCraftable {
				continue
			}
		case "gatherable":
			if !it.IsGatherable {
				continue
			}
		case "purchasable":
			if !it.IsPurchasable {
				continue
			}
		case "gil_shop":
			if !it.IsGilShop {
				continue
			}
		case "special_shop":
			if !it.IsSpecialShop {
				continue
			}
		case "retainer_venture":
			if !it.IsRetainerVenture {
				continue
			}
		case "collectable_turnin":
			if !it.IsCollectableTurnIn {
				continue
			}
		case "equipment":
			if !it.HasEquipmentStats {
				continue
			}
		case "consumable":
			if it.CategoryID != 44 && it.CategoryID != 46 {
				continue
			}
		}

		if query.CraftTypeID != nil && *query.CraftTypeID >= 0 {
			rIDs := s.store.RecipesByResultItem[it.ID]
			if len(rIDs) == 0 {
				continue
			}
			matched := false
			for _, rid := range rIDs {
				if rRow, ok := s.store.Recipes[rid]; ok && rRow.IntAt(1) == *query.CraftTypeID {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		if q != "" {
			if numericQuery > 0 && it.ID == numericQuery {
				filtered = append(filtered, it)
				continue
			}
			if !strings.Contains(strings.ToLower(it.NameCn), q) &&
				!strings.Contains(strings.ToLower(it.NameZh), q) &&
				!strings.Contains(strings.ToLower(it.NameEn), q) &&
				!strings.Contains(strings.ToLower(it.NameJa), q) &&
				strconv.Itoa(it.ID) != q {
				continue
			}
		}
		filtered = append(filtered, it)
	}

	sortMode := query.Sort
	if sortMode == "" {
		if q != "" {
			sortMode = "ilvl_desc"
		} else {
			sortMode = "id_asc"
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if q != "" {
			aExact := a.ID == numericQuery ||
				strings.ToLower(a.NameCn) == q ||
				strings.ToLower(a.NameZh) == q ||
				strings.ToLower(a.NameEn) == q ||
				strings.ToLower(a.NameJa) == q
			bExact := b.ID == numericQuery ||
				strings.ToLower(b.NameCn) == q ||
				strings.ToLower(b.NameZh) == q ||
				strings.ToLower(b.NameEn) == q ||
				strings.ToLower(b.NameJa) == q
			if aExact != bExact {
				return aExact
			}
		}
		switch sortMode {
		case "id_desc":
			return a.ID > b.ID
		case "ilvl_desc":
			if a.ItemLevel != b.ItemLevel {
				return a.ItemLevel > b.ItemLevel
			}
			return a.ID < b.ID
		case "ilvl_asc":
			if a.ItemLevel != b.ItemLevel {
				return a.ItemLevel < b.ItemLevel
			}
			return a.ID < b.ID
		case "level_desc":
			if a.EquipLevel != b.EquipLevel {
				return a.EquipLevel > b.EquipLevel
			}
			if a.ItemLevel != b.ItemLevel {
				return a.ItemLevel > b.ItemLevel
			}
			return a.ID < b.ID
		default:
			return a.ID < b.ID
		}
	})
	return filtered
}

// SearchItems 按多语言关键词、分类、获取途径、职业、品级区间与排序条件分页检索道具。
func (s *Service) SearchItems(query ItemSearchQuery) ItemSearchResponse {
	filtered := s.filterAndSortItems(query)
	page, totalPages, start, end := paginateRange(len(filtered), query.Page, query.PageSize)
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var items []ItemSummary
	if start < end {
		items = filtered[start:end]
	}

	return ItemSearchResponse{
		Items:      items,
		Total:      len(filtered),
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}

// LocateItemPage 在当前筛选排序条件下定位指定道具所在分页并返回该页结果。
func (s *Service) LocateItemPage(query ItemSearchQuery, itemID int) (ItemSearchResponse, bool) {
	if s == nil || s.store == nil || itemID <= 0 {
		return ItemSearchResponse{}, false
	}
	filtered := s.filterAndSortItems(query)
	idx := -1
	for i, it := range filtered {
		if it.ID == itemID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ItemSearchResponse{}, false
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	page, totalPages, start, end := paginateRange(len(filtered), idx/pageSize+1, pageSize)
	var items []ItemSummary
	if start < end {
		items = filtered[start:end]
	}
	return ItemSearchResponse{
		Items:      items,
		Total:      len(filtered),
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, true
}

// GetItemDetail 获取单个道具的完整详情。
func (s *Service) GetItemDetail(itemID int) *ItemDetail {
	summary, ok := s.store.ItemSummaryByID[itemID]
	row, rowOk := s.store.ItemsPrimary[itemID]
	if !ok || !rowOk {
		return nil
	}

	enRow := s.store.ItemsEn[itemID]
	jaRow := s.store.ItemsJa[itemID]

	equipSlotID := row.IntAt(17)
	isEquipment := equipSlotID > 0
	cjCatID := row.IntAt(43)
	repairCJID := row.IntAt(33)
	itemRepairID := row.IntAt(34)
	repairResourceItemID := 0
	if itemRepairID > 0 {
		repairResourceItemID = s.store.ItemRepairResources[itemRepairID]
	}

	repairCJName := s.store.ClassJobs[repairCJID].name
	equipStats := s.extractEquipmentStats(row)
	var equipAttrs *EquipmentAttributes
	if isEquipment {
		a := s.extractEquipmentAttributes(itemID, row, repairCJName)
		equipAttrs = &a
	}
	consumable := s.extractConsumableEffect(row)
	recipes := s.buildItemRecipes(itemID)
	recipeTrees := make([]RecipeTreeNode, 0, len(recipes))
	for _, r := range recipes {
		if tree := s.buildRecipeTreeForRecipe(r, r.ResultAmount, 0, map[int]bool{}); tree != nil {
			recipeTrees = append(recipeTrees, *tree)
		}
	}
	usedIn := s.buildUsedInRecipes(itemID)
	gathering := GatheringMethodDetail{
		Nodes:        s.buildGatheringNodes(itemID),
		FishingSpots: s.buildFishingSpots(itemID),
		Spearfishing: s.buildSpearfishing(itemID),
	}
	shops := s.buildShopSources(itemID)
	retainers := s.buildRetainerVentures(itemID, summary.NameCn)
	rewardAchs := s.store.AchievementsByRewardItem[itemID]
	colTurnIns := s.store.CollectableTurnInsByItem[itemID]
	colRewards := s.store.CollectableRewardsByItem[itemID]
	rewardQuests := s.store.QuestsByRewardItem[itemID]
	var turnInLeves, rewardLeves []ItemLeveSource
	for _, ls := range s.store.LevesByItem[itemID] {
		if ls.IsTurnIn {
			turnInLeves = append(turnInLeves, ls)
		} else {
			rewardLeves = append(rewardLeves, ls)
		}
	}

	isIndisposable := row.BoolAt(23)
	priceLow := row.IntAt(26)
	isSellable := !isIndisposable && priceLow > 0

	repairItemName := ""
	if isEquipment && repairResourceItemID > 0 {
		repairItemName = s.store.ItemSummaryByID[repairResourceItemID].NameCn
	}
	repClass := ""
	if isEquipment {
		repClass = repairCJName
	}

	return &ItemDetail{
		ItemSummary:              summary,
		DescriptionCn:            row.StringAt(8),
		DescriptionEn:            enRow.StringAt(8),
		DescriptionJa:            jaRow.StringAt(8),
		StackSize:                row.IntAt(20),
		IsUnique:                 row.BoolAt(21),
		IsIndisposable:           isIndisposable,
		IsCollectable:            row.BoolAt(37) || row.BoolAt(38),
		DyeCount:                 row.IntAt(28),
		CanDesynth:               row.IntAt(36) > 0,
		CanAetherialReduce:       row.IntAt(39) > 0,
		IsGlamourous:             row.BoolAt(90),
		PriceLow:                 priceLow,
		IsSellable:               isSellable,
		IsEquipment:              isEquipment,
		ClassJobCategoryName:     s.store.ClassJobCategories[cjCatID],
		EquipSlotName:            s.store.EquipSlots[equipSlotID],
		RepairClassJobName:       repClass,
		RepairItemName:           repairItemName,
		EquipmentStats:           equipStats,
		EquipmentAttributes:      equipAttrs,
		ConsumableEffect:         consumable,
		Recipes:                  recipes,
		RecipeTrees:              recipeTrees,
		UsedInRecipes:            usedIn,
		Gathering:                gathering,
		Shops:                    shops,
		RetainerVentures:         retainers,
		CollectableTurnIns:       colTurnIns,
		CollectableRewards:       colRewards,
		RewardedFromQuests:       rewardQuests,
		TurnInForLeves:           turnInLeves,
		RewardedFromLeves:        rewardLeves,
		RewardedFromAchievements: rewardAchs,
	}
}

func (s *Service) extractEquipmentAttributes(itemID int, row sqpack.Row, repairCJName string) EquipmentAttributes {
	canDesynth := row.IntAt(36) > 0
	desynthJob := ""
	if canDesynth {
		desynthJob = repairCJName
	}
	_, inArmoire := s.store.CabinetItemIDs[itemID]
	return EquipmentAttributes{
		CanExtractMateria:        row.IntAt(85) > 0,
		IsProjectable:            row.BoolAt(90),
		CanDesynth:               canDesynth,
		DesynthClassJobName:      desynthJob,
		DyeCount:                 row.IntAt(28),
		IsCrestWorthy:            row.BoolAt(29),
		CanStoreInGlamourDresser: row.BoolAt(90),
		CanStoreInArmoire:        inArmoire,
	}
}

func (s *Service) extractEquipmentStats(row sqpack.Row) *EquipmentStats {
	filterGroup := row.IntAt(13)
	rawDamagePhys := row.IntAt(51)
	rawDamageMag := row.IntAt(52)
	delayMs := row.IntAt(53)
	blockRate := row.IntAt(55)
	block := row.IntAt(56)
	defensePhys := row.IntAt(57)
	defenseMag := row.IntAt(58)
	materiaSlots := row.IntAt(86)
	advancedMelding := row.BoolAt(87)

	weaponKind := "none"
	switch {
	case filterGroup == 1:
		weaponKind = "physical"
	case filterGroup == 2:
		weaponKind = "magical"
	case filterGroup == 3 || blockRate > 0 || block > 0:
		weaponKind = "shield"
	case rawDamagePhys > 0 && rawDamageMag == 0:
		weaponKind = "physical"
	case rawDamageMag > 0 && rawDamagePhys == 0:
		weaponKind = "magical"
	case rawDamagePhys > 0:
		weaponKind = "physical"
	}

	damagePhys := rawDamagePhys
	if weaponKind == "magical" {
		damagePhys = 0
	}
	damageMag := rawDamageMag
	if weaponKind == "physical" {
		damageMag = 0
	}

	itemSpecialBonus := row.IntAt(71)
	specialBonusByParam := make(map[int]int)
	for i := 0; i < 6; i++ {
		pid := row.IntAt(73 + i*2)
		val := row.IntAt(74 + i*2)
		if pid > 0 && val != 0 {
			specialBonusByParam[pid] += val
		}
	}
	hqExtra := func(paramID int) int {
		if itemSpecialBonus == 1 {
			return specialBonusByParam[paramID]
		}
		return 0
	}

	var bonuses []EquipmentStatBonus
	existing := make(map[int]bool)
	for i := 0; i < 6; i++ {
		pid := row.IntAt(59 + i*2)
		valNQ := row.IntAt(60 + i*2)
		if pid > 0 && valNQ != 0 {
			name := s.store.BaseParams[pid].name
			if name == "" {
				name = i18n.Tf(s.appLang(), "gamedata.param_fallback", "id", strconv.Itoa(pid))
			}
			bonuses = append(bonuses, EquipmentStatBonus{
				ParamID: pid,
				Name:    name,
				ValueNQ: valNQ,
				ValueHQ: valNQ + hqExtra(pid),
			})
			existing[pid] = true
		}
	}

	if itemSpecialBonus == 1 {
		primaryIDs := map[int]bool{12: true, 13: true, 17: true, 18: true, 21: true, 24: true}
		var extraPIDs []int
		for pid, val := range specialBonusByParam {
			if !primaryIDs[pid] && !existing[pid] && val > 0 {
				extraPIDs = append(extraPIDs, pid)
			}
		}
		sort.Ints(extraPIDs)
		for _, pid := range extraPIDs {
			name := s.store.BaseParams[pid].name
			if name == "" {
				name = i18n.Tf(s.appLang(), "gamedata.param_fallback", "id", strconv.Itoa(pid))
			}
			bonuses = append(bonuses, EquipmentStatBonus{
				ParamID: pid,
				Name:    name,
				ValueNQ: 0,
				ValueHQ: specialBonusByParam[pid],
			})
		}
	}

	damagePhysHQ := 0
	if damagePhys > 0 {
		damagePhysHQ = damagePhys + hqExtra(12)
	}
	damageMagHQ := 0
	if damageMag > 0 {
		damageMagHQ = damageMag + hqExtra(13)
	}
	blockRateHQ := 0
	if blockRate > 0 {
		blockRateHQ = blockRate + hqExtra(17)
	}
	blockHQ := 0
	if block > 0 {
		blockHQ = block + hqExtra(18)
	}
	defensePhysHQ := 0
	if defensePhys > 0 {
		defensePhysHQ = defensePhys + hqExtra(21)
	}
	defenseMagHQ := 0
	if defenseMag > 0 {
		defenseMagHQ = defenseMag + hqExtra(24)
	}

	calcAutoAttack := func(physDmg int) float64 {
		if physDmg <= 0 || delayMs <= 0 {
			return 0
		}
		return math.Floor((float64(physDmg*delayMs)/3000.0)*100.0) / 100.0
	}
	rawDamagePhysHQ := 0
	if rawDamagePhys > 0 {
		rawDamagePhysHQ = rawDamagePhys + hqExtra(12)
	}

	if damagePhys == 0 && damageMag == 0 && blockRate == 0 && block == 0 &&
		defensePhys == 0 && defenseMag == 0 && len(bonuses) == 0 && materiaSlots == 0 {
		return nil
	}

	delaySec := 0.0
	if delayMs > 0 {
		delaySec = round2(float64(delayMs) / 1000.0)
	}

	return &EquipmentStats{
		WeaponKind:                 weaponKind,
		DamagePhys:                 damagePhys,
		DamagePhysHQ:               damagePhysHQ,
		DamageMag:                  damageMag,
		DamageMagHQ:                damageMagHQ,
		AutoAttack:                 calcAutoAttack(rawDamagePhys),
		AutoAttackHQ:               calcAutoAttack(rawDamagePhysHQ),
		DelaySec:                   delaySec,
		BlockRate:                  blockRate,
		BlockRateHQ:                blockRateHQ,
		Block:                      block,
		BlockHQ:                    blockHQ,
		DefensePhys:                defensePhys,
		DefensePhysHQ:              defensePhysHQ,
		DefenseMag:                 defenseMag,
		DefenseMagHQ:               defenseMagHQ,
		MateriaSlotCount:           materiaSlots,
		IsAdvancedMeldingPermitted: advancedMelding,
		Bonuses:                    bonuses,
	}
}

func (s *Service) extractConsumableEffect(row sqpack.Row) *ConsumableEffect {
	actionID := row.IntAt(30)
	if actionID <= 0 {
		return nil
	}
	actionRow, ok := s.store.ItemActions[actionID]
	if !ok {
		return nil
	}
	actionType := actionRow.IntAt(4)
	statusID := actionRow.IntAt(5)
	if actionType != 844 && actionType != 845 && actionType != 846 && statusID != 48 && statusID != 49 {
		return nil
	}

	foodID := actionRow.IntAt(6)
	if foodID <= 0 {
		foodID = statusID
	}
	durationSec := actionRow.IntAt(7)
	if durationSec <= 0 {
		durationSec = 1800
	}
	foodRow, ok := s.store.ItemFoods[foodID]
	if !ok {
		return nil
	}

	expBonus := foodRow.IntAt(0)
	var params []ConsumableParamBonus
	for i := 0; i < 3; i++ {
		base := 1 + i*6
		pid := foodRow.IntAt(base)
		if pid <= 0 {
			continue
		}
		name := s.store.BaseParams[pid].name
		if name == "" {
			name = i18n.Tf(s.appLang(), "gamedata.param_fallback", "id", strconv.Itoa(pid))
		}
		params = append(params, ConsumableParamBonus{
			ParamID:    pid,
			Name:       name,
			IsRelative: foodRow.BoolAt(base + 1),
			ValueNQ:    foodRow.IntAt(base + 2),
			MaxNQ:      foodRow.IntAt(base + 3),
			ValueHQ:    foodRow.IntAt(base + 4),
			MaxHQ:      foodRow.IntAt(base + 5),
		})
	}
	if len(params) == 0 && expBonus == 0 {
		return nil
	}

	cooldown := row.IntAt(32)
	cooldownHQ := 0
	if cooldown > 0 {
		cooldownHQ = int(math.Floor(float64(cooldown) * 0.9))
	}
	typeName := i18n.T(s.appLang(), "gamedata.consume_med")
	if statusID == 48 {
		typeName = i18n.T(s.appLang(), "gamedata.consume_food")
	}
	return &ConsumableEffect{
		TypeName:        typeName,
		DurationSec:     durationSec,
		CooldownSec:     cooldown,
		CooldownHQSec:   cooldownHQ,
		ExpBonusPercent: expBonus,
		Params:          params,
	}
}

func (s *Service) buildItemRecipes(itemID int) []RecipeDetail {
	recipeIDs := s.store.RecipesByResultItem[itemID]
	if len(recipeIDs) == 0 {
		return nil
	}
	details := make([]RecipeDetail, 0, len(recipeIDs))
	for _, rid := range recipeIDs {
		rRow, ok := s.store.Recipes[rid]
		if !ok {
			continue
		}
		ctID := rRow.IntAt(1)
		ctInfo, hasCT := s.store.CraftTypes[ctID]
		rltRow := s.store.RecipeLevelTables[rRow.IntAt(2)]

		recipeLevel := 1
		stars, sugCraft, baseDiff, baseQual, baseDur := 0, 0, 0, 0, 0
		if rltRow != nil {
			recipeLevel = rltRow.IntAt(0)
			stars = rltRow.IntAt(1)
			sugCraft = rltRow.IntAt(2)
			baseDiff = rltRow.IntAt(3)
			baseQual = rltRow.IntAt(4)
			baseDur = rltRow.IntAt(9)
		}

		diffFactor := rRow.IntAt(26)
		if diffFactor == 0 {
			diffFactor = 100
		}
		qualFactor := rRow.IntAt(27)
		if qualFactor == 0 {
			qualFactor = 100
		}
		durFactor := rRow.IntAt(28)
		if durFactor == 0 {
			durFactor = 100
		}

		var ingredients, crystals []RecipeIngredient
		for i := 0; i < 8; i++ {
			ingID := rRow.IntAt(6 + i*2)
			ingAmt := rRow.IntAt(7 + i*2)
			if ingID <= 0 || ingAmt <= 0 {
				continue
			}
			ingSum, ok := s.store.ItemSummaryByID[ingID]
			if !ok {
				continue
			}
			entry := RecipeIngredient{
				ItemID:       ingID,
				NameCn:       ingSum.NameCn,
				NameEn:       ingSum.NameEn,
				IconID:       ingSum.IconID,
				Amount:       ingAmt,
				ItemLevel:    ingSum.ItemLevel,
				IsCraftable:  ingSum.IsCraftable,
				IsGatherable: ingSum.IsGatherable,
			}
			if i >= 6 || ingSum.CategoryID == 59 {
				crystals = append(crystals, entry)
			} else {
				ingredients = append(ingredients, entry)
			}
		}

		craftTypeName := i18n.T(s.appLang(), "gamedata.craft_type_default")
		craftJobName := i18n.T(s.appLang(), "gamedata.craft_job_default")
		craftJobIcon := 62008
		if hasCT {
			craftTypeName = ctInfo.Name
			craftJobName = ctInfo.JobName
			craftJobIcon = ctInfo.IconID
		}
		resAmt := rRow.IntAt(5)
		if resAmt <= 0 {
			resAmt = 1
		}

		details = append(details, RecipeDetail{
			ID:                       rid,
			CraftTypeID:              ctID,
			CraftTypeName:            craftTypeName,
			CraftJobName:             craftJobName,
			CraftJobIconID:           craftJobIcon,
			ResultItemID:             rRow.IntAt(4),
			ResultAmount:             resAmt,
			RecipeLevel:              recipeLevel,
			Stars:                    stars,
			Difficulty:               (baseDiff * diffFactor) / 100,
			Quality:                  (baseQual * qualFactor) / 100,
			Durability:               (baseDur * durFactor) / 100,
			SuggestedCraftsmanship:   sugCraft,
			RequiredCraftsmanship:    rRow.IntAt(31),
			RequiredControl:          rRow.IntAt(32),
			MaterialQualityFactor:    rRow.IntAt(25),
			SecretRecipeBookName:     s.store.SecretRecipeBooks[rRow.IntAt(35)].name,
			IsSpecializationRequired: rRow.BoolAt(42),
			IsExpert:                 rRow.BoolAt(43),
			CanQuickSynth:            rRow.BoolAt(36),
			CanHQ:                    rRow.BoolAt(37),
			Ingredients:              ingredients,
			Crystals:                 crystals,
		})
	}
	return details
}

func (s *Service) buildRecipeTreeForRecipe(recipe RecipeDetail, amountPerCraft, depth int, visited map[int]bool) *RecipeTreeNode {
	sum, ok := s.store.ItemSummaryByID[recipe.ResultItemID]
	if !ok {
		return nil
	}
	nextVisited := make(map[int]bool, len(visited)+1)
	for k, v := range visited {
		nextVisited[k] = v
	}
	nextVisited[recipe.ResultItemID] = true

	var children []RecipeTreeNode
	for _, ing := range recipe.Ingredients {
		if child := s.buildRecipeTree(ing.ItemID, ing.Amount, depth+1, nextVisited); child != nil {
			children = append(children, *child)
		}
	}
	crystals := make([]RecipeCrystalNode, 0, len(recipe.Crystals))
	for _, c := range recipe.Crystals {
		crystals = append(crystals, RecipeCrystalNode{
			ItemID: c.ItemID,
			NameCn: c.NameCn,
			IconID: c.IconID,
			Amount: c.Amount,
		})
	}
	return &RecipeTreeNode{
		ItemID:         recipe.ResultItemID,
		NameCn:         sum.NameCn,
		IconID:         sum.IconID,
		AmountPerCraft: amountPerCraft,
		YieldPerCraft:  recipe.ResultAmount,
		CraftJobName:   recipe.CraftJobName,
		CraftJobIconID: recipe.CraftJobIconID,
		RecipeLevel:    recipe.RecipeLevel,
		Stars:          recipe.Stars,
		IsGatherable:   sum.IsGatherable,
		Children:       children,
		Crystals:       crystals,
	}
}

func (s *Service) buildRecipeTree(itemID, amountPerCraft, depth int, visited map[int]bool) *RecipeTreeNode {
	sum, ok := s.store.ItemSummaryByID[itemID]
	if !ok {
		return nil
	}
	rIDs := s.store.RecipesByResultItem[itemID]
	if len(rIDs) == 0 || depth > 5 || visited[itemID] {
		return &RecipeTreeNode{
			ItemID:         itemID,
			NameCn:         sum.NameCn,
			IconID:         sum.IconID,
			AmountPerCraft: amountPerCraft,
			YieldPerCraft:  1,
			IsGatherable:   sum.IsGatherable,
		}
	}
	recipes := s.buildItemRecipes(itemID)
	if len(recipes) == 0 {
		return nil
	}
	return s.buildRecipeTreeForRecipe(recipes[0], amountPerCraft, depth, visited)
}

func (s *Service) buildUsedInRecipes(itemID int) []UsedInRecipeItem {
	usages := s.store.RecipesByIngredientItem[itemID]
	if len(usages) == 0 {
		return nil
	}
	seen := make(map[int]bool)
	results := make([]UsedInRecipeItem, 0, min(60, len(usages)))
	for _, u := range usages {
		if seen[u.recipeID] {
			continue
		}
		seen[u.recipeID] = true
		rRow, ok := s.store.Recipes[u.recipeID]
		if !ok {
			continue
		}
		resID := rRow.IntAt(4)
		resSum, ok := s.store.ItemSummaryByID[resID]
		if !ok {
			continue
		}
		ctInfo, hasCT := s.store.CraftTypes[rRow.IntAt(1)]
		jobName := i18n.T(s.appLang(), "gamedata.craft_job_default")
		jobIcon := 62008
		if hasCT {
			jobName = ctInfo.JobName
			jobIcon = ctInfo.IconID
		}
		rltRow := s.store.RecipeLevelTables[rRow.IntAt(2)]
		recLv, stars := 1, 0
		if rltRow != nil {
			recLv = rltRow.IntAt(0)
			stars = rltRow.IntAt(1)
		}
		results = append(results, UsedInRecipeItem{
			RecipeID:        u.recipeID,
			ResultItemID:    resID,
			ResultNameCn:    resSum.NameCn,
			ResultIconID:    resSum.IconID,
			ResultItemLevel: resSum.ItemLevel,
			AmountRequired:  u.amount,
			CraftJobName:    jobName,
			CraftJobIconID:  jobIcon,
			RecipeLevel:     recLv,
			Stars:           stars,
		})
		if len(results) >= 60 {
			break
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].RecipeLevel != results[j].RecipeLevel {
			return results[i].RecipeLevel > results[j].RecipeLevel
		}
		return results[i].ResultItemLevel > results[j].ResultItemLevel
	})
	return results
}

type territoryMapLoc struct {
	regionName string
	zoneName   string
	mapID      int
	mapPath    string
	sizeFactor int
	offsetX    float64
	offsetY    float64
}

func resolveTerritoryAndMapFromTables(territoryID int, territoryTypes, maps map[int]sqpack.Row, placeNames map[int]string) *territoryMapLoc {
	return resolveTerritoryAndMapFromTablesForLang(territoryID, territoryTypes, maps, placeNames, i18n.LangZH)
}

func resolveTerritoryAndMapFromTablesForLang(territoryID int, territoryTypes, maps map[int]sqpack.Row, placeNames map[int]string, lang i18n.Lang) *territoryMapLoc {
	lang = resolveAppLang(lang)
	tRow, ok := territoryTypes[territoryID]
	if !ok {
		return nil
	}
	regionID := tRow.IntAt(3)
	zoneID := tRow.IntAt(5)
	if zoneID == 0 {
		zoneID = tRow.IntAt(4)
	}
	mapID := tRow.IntAt(6)
	mapRow := maps[mapID]

	regionName := placeNames[regionID]
	if regionName == "" {
		regionName = i18n.T(lang, "gamedata.region_default")
	}
	zoneName := placeNames[zoneID]
	if zoneName == "" {
		zoneName = i18n.T(lang, "gamedata.zone_unknown")
	}

	mapPath := ""
	sizeFactor := 100
	var offsetX, offsetY float64
	if mapRow != nil {
		mapPath = mapRow.StringAt(6)
		if sf := mapRow.IntAt(7); sf > 0 {
			sizeFactor = sf
		}
		offsetX = mapRow.FloatAt(8)
		offsetY = mapRow.FloatAt(9)
	}
	return &territoryMapLoc{
		regionName: regionName,
		zoneName:   zoneName,
		mapID:      mapID,
		mapPath:    mapPath,
		sizeFactor: sizeFactor,
		offsetX:    offsetX,
		offsetY:    offsetY,
	}
}

func resolveMapLocation(rawLoc RawNpcLocation, territoryTypes, maps map[int]sqpack.Row, placeNames map[int]string) *MapLocationPoint {
	tLoc := resolveTerritoryAndMapFromTables(rawLoc.TerritoryID, territoryTypes, maps, placeNames)
	if tLoc == nil {
		return nil
	}
	pt := &MapLocationPoint{
		RegionName:  tLoc.regionName,
		ZoneName:    tLoc.zoneName,
		PlaceName:   tLoc.zoneName,
		TerritoryID: rawLoc.TerritoryID,
		MapID:       rawLoc.MapID,
		MapPath:     tLoc.mapPath,
		SizeFactor:  tLoc.sizeFactor,
	}
	if pt.MapID == 0 {
		pt.MapID = tLoc.mapID
	}
	offX, offY := tLoc.offsetX, tLoc.offsetY
	if spMap, ok := maps[pt.MapID]; ok {
		if p := spMap.StringAt(6); p != "" {
			pt.MapPath = p
		}
		if sf := spMap.IntAt(7); sf > 0 {
			pt.SizeFactor = sf
		}
		offX = spMap.FloatAt(8)
		offY = spMap.FloatAt(9)
		subID := spMap.IntAt(12)
		if subID == 0 {
			subID = spMap.IntAt(11)
		}
		if subName := placeNames[subID]; subName != "" {
			pt.PlaceName = subName
		}
	}
	pt.CoordX, pt.CoordY, pt.NormX, pt.NormY = convertWorldCoordToMap(
		rawLoc.WorldX, rawLoc.WorldZ, pt.SizeFactor, offX, offY,
	)
	return pt
}

func resolveVendorNpc(npcID int, npcNames map[int]RawNpcName, npcLocations map[int]RawNpcLocation, territoryTypes, maps map[int]sqpack.Row, placeNames map[int]string) (ShopVendorNpc, bool) {
	npcInfo, ok := npcNames[npcID]
	if !ok {
		return ShopVendorNpc{}, false
	}
	vendor := ShopVendorNpc{
		NpcID:      npcID,
		NpcName:    npcInfo.Name,
		NpcTitle:   npcInfo.Title,
		SizeFactor: 100,
		NormX:      0.5,
		NormY:      0.5,
	}
	if rawLoc, ok := npcLocations[npcID]; ok {
		if loc := resolveMapLocation(rawLoc, territoryTypes, maps, placeNames); loc != nil {
			vendor.RegionName = loc.RegionName
			vendor.ZoneName = loc.ZoneName
			vendor.PlaceName = loc.PlaceName
			vendor.TerritoryID = loc.TerritoryID
			vendor.MapID = loc.MapID
			vendor.MapPath = loc.MapPath
			vendor.SizeFactor = loc.SizeFactor
			vendor.CoordX = loc.CoordX
			vendor.CoordY = loc.CoordY
			vendor.NormX = loc.NormX
			vendor.NormY = loc.NormY
		}
	}
	return vendor, true
}

func (s *Service) resolveTerritoryAndMap(territoryID int) *territoryMapLoc {
	return resolveTerritoryAndMapFromTablesForLang(territoryID, s.store.TerritoryTypes, s.store.Maps, s.store.PlaceNames, s.appLang())
}

func (s *Service) extractGatheringTimeWindows(pointID int) (isTimed bool, isEphemeral bool, windows []GatheringTimeWindow) {
	trRow, ok := s.store.GatheringPointTransients[pointID]
	if !ok {
		return false, false, nil
	}
	ephStart := trRow.IntAt(0)
	ephEnd := trRow.IntAt(1)
	rarePopID := trRow.IntAt(2)

	if ephStart != 65535 && ephEnd != 65535 && ephStart != ephEnd {
		isEphemeral = true
		startMin := hhmmToMinutes(ephStart)
		endMin := hhmmToMinutes(ephEnd)
		dur := endMin - startMin
		if dur <= 0 {
			dur += 1440
		}
		windows = append(windows, GatheringTimeWindow{
			StartET:          minutesToTimeStr(startMin),
			EndET:            minutesToTimeStr(endMin),
			StartMinuteOfDay: startMin,
			DurationMinutes:  dur,
		})
	}
	if rarePopID > 0 {
		if popRow, ok := s.store.GatheringRarePopTimeTables[rarePopID]; ok {
			for i := 0; i < 3; i++ {
				startHHMM := popRow.IntAt(i * 2)
				durHHMM := popRow.IntAt(i*2 + 1)
				if startHHMM < 2400 && durHHMM > 0 {
					startMin := hhmmToMinutes(startHHMM)
					durMin := hhmmToMinutes(durHHMM)
					endMin := (startMin + durMin) % 1440
					windows = append(windows, GatheringTimeWindow{
						StartET:          minutesToTimeStr(startMin),
						EndET:            minutesToTimeStr(endMin),
						StartMinuteOfDay: startMin,
						DurationMinutes:  durMin,
					})
				}
			}
		}
	}
	return len(windows) > 0, isEphemeral, windows
}

func (s *Service) buildGatheringNodes(itemID int) []GatheringNodeInfo {
	gItemIDs := s.store.GatheringItemsByItem[itemID]
	if len(gItemIDs) == 0 {
		return nil
	}
	var nodes []GatheringNodeInfo
	dedup := make(map[string]bool)

	for _, gItemID := range gItemIDs {
		gRow, ok := s.store.GatheringItems[gItemID]
		if !ok {
			continue
		}
		lvInfo, ok := s.store.GatheringItemLevels[gRow.IntAt(1)]
		if !ok {
			lvInfo = gatheringItemLevelDict{level: 1, stars: 0}
		}
		isHidden := gRow.BoolAt(2)

		for _, pointID := range s.store.GatheringPointsByGatheringItem[gItemID] {
			ptRow, ok := s.store.GatheringPoints[pointID]
			if !ok {
				continue
			}
			baseID := ptRow.IntAt(3)
			territoryID := ptRow.IntAt(7)
			placeNameID := ptRow.IntAt(8)
			subCatID := ptRow.IntAt(9)
			if baseID <= 0 || territoryID <= 1 {
				continue
			}
			key := fmt.Sprintf("%d:%d:%d", baseID, territoryID, placeNameID)
			if dedup[key] {
				continue
			}
			dedup[key] = true

			baseRow, ok := s.store.GatheringPointBases[baseID]
			if !ok {
				continue
			}
			loc := s.resolveTerritoryAndMap(territoryID)
			if loc == nil {
				continue
			}

			gTypeID := baseRow.IntAt(0)
			nodeLevel := baseRow.IntAt(1)
			gTypeInfo := s.store.GatheringTypes[gTypeID]
			isMiner := gTypeID == 0 || gTypeID == 1
			discName := i18n.T(s.appLang(), "gamedata.disc_botanist")
			discIcon := 62017
			if isMiner {
				discName = i18n.T(s.appLang(), "gamedata.disc_miner")
				discIcon = 62016
			}
			gTypeName := gTypeInfo.name
			if gTypeName == "" {
				gTypeName = i18n.T(s.appLang(), "gamedata.gather_default")
			}
			gTypeIcon := gTypeInfo.iconID
			if gTypeIcon == 0 {
				gTypeIcon = 60438
			}

			placeName := s.store.PlaceNames[placeNameID]
			if placeName == "" {
				placeName = loc.zoneName
			}

			coordX, coordY, normX, normY := 21.5, 21.5, 0.5, 0.5
			radius := 50
			if expRow, ok := s.store.ExportedGatheringPoints[baseID]; ok {
				if r := expRow.IntAt(4); r > 0 {
					radius = r
				}
				coordX, coordY, normX, normY = convertWorldCoordToMap(
					expRow.FloatAt(0), expRow.FloatAt(1),
					loc.sizeFactor, loc.offsetX, loc.offsetY,
				)
			}

			isTimed, isEphemeral, windows := s.extractGatheringTimeWindows(pointID)

			nodes = append(nodes, GatheringNodeInfo{
				PointID:             pointID,
				BaseID:              baseID,
				GatheringTypeID:     gTypeID,
				GatheringTypeName:   gTypeName,
				DisciplineName:      discName,
				DisciplineIconID:    discIcon,
				GatheringTypeIconID: gTypeIcon,
				NodeLevel:           nodeLevel,
				ItemLevel:           lvInfo.level,
				Stars:               lvInfo.stars,
				IsHidden:            isHidden,
				RegionName:          loc.regionName,
				ZoneName:            loc.zoneName,
				PlaceName:           placeName,
				TerritoryID:         territoryID,
				MapID:               loc.mapID,
				MapPath:             loc.mapPath,
				SizeFactor:          loc.sizeFactor,
				CoordX:              coordX,
				CoordY:              coordY,
				NormX:               normX,
				NormY:               normY,
				Radius:              radius,
				FolkloreBookName:    s.store.GatheringSubCategories[subCatID].name,
				IsTimed:             isTimed,
				IsEphemeral:         isEphemeral,
				TimeWindows:         windows,
			})
		}
	}
	return nodes
}

func (s *Service) buildFishingSpots(itemID int) []FishingSpotInfo {
	spotIDs := s.store.FishingSpotsByItem[itemID]
	if len(spotIDs) == 0 {
		return nil
	}
	fpRow := s.store.FishParametersByItem[itemID]
	guideText := fpRow.StringAt(0)
	folklore := s.store.GatheringSubCategories[fpRow.IntAt(11)].name

	var spots []FishingSpotInfo
	for _, spotID := range spotIDs {
		row, ok := s.store.FishingSpots[spotID]
		if !ok {
			continue
		}
		territoryID := row.IntAt(6)
		loc := s.resolveTerritoryAndMap(territoryID)
		if loc == nil {
			continue
		}
		coordX, coordY, normX, normY := convertPixelCoordToMap(row.FloatAt(9), row.FloatAt(10), loc.sizeFactor)
		catName := fishingSpotCategoryName(s.appLang(), row.IntAt(4))
		if catName == "" {
			catName = i18n.T(s.appLang(), "gamedata.fish_cat.default")
		}
		placeName := s.store.PlaceNames[row.IntAt(23)]
		if placeName == "" {
			placeName = loc.zoneName
		}
		radius := row.IntAt(11)
		if radius <= 0 {
			radius = 400
		}
		spots = append(spots, FishingSpotInfo{
			SpotID:           spotID,
			Level:            row.IntAt(0),
			CategoryName:     catName,
			RegionName:       loc.regionName,
			ZoneName:         loc.zoneName,
			PlaceName:        placeName,
			TerritoryID:      territoryID,
			MapID:            loc.mapID,
			MapPath:          loc.mapPath,
			SizeFactor:       loc.sizeFactor,
			CoordX:           coordX,
			CoordY:           coordY,
			NormX:            normX,
			NormY:            normY,
			Radius:           radius,
			FishGuideText:    guideText,
			FolkloreBookName: folklore,
		})
	}
	return spots
}

func (s *Service) buildSpearfishing(itemID int) []SpearfishingInfo {
	links := s.store.SpearfishingByItem[itemID]
	if len(links) == 0 {
		return nil
	}
	var out []SpearfishingInfo
	for _, l := range links {
		territoryID := l.notebookRow.IntAt(2)
		loc := s.resolveTerritoryAndMap(territoryID)
		if loc == nil {
			continue
		}
		itemLv := s.store.GatheringItemLevels[l.spearfishingItemRow.IntAt(2)].level
		if itemLv <= 0 {
			itemLv = 1
		}
		coordX, coordY, normX, normY := convertPixelCoordToMap(l.notebookRow.FloatAt(3), l.notebookRow.FloatAt(4), loc.sizeFactor)
		placeName := s.store.PlaceNames[l.notebookRow.IntAt(7)]
		if placeName == "" {
			placeName = loc.zoneName
		}
		radius := l.notebookRow.IntAt(5)
		if radius <= 0 {
			radius = 400
		}
		out = append(out, SpearfishingInfo{
			NotebookID:  l.notebookID,
			Level:       l.notebookRow.IntAt(0),
			ItemLevel:   itemLv,
			RegionName:  loc.regionName,
			ZoneName:    loc.zoneName,
			PlaceName:   placeName,
			TerritoryID: territoryID,
			MapID:       loc.mapID,
			MapPath:     loc.mapPath,
			SizeFactor:  loc.sizeFactor,
			CoordX:      coordX,
			CoordY:      coordY,
			NormX:       normX,
			NormY:       normY,
			Radius:      radius,
			Description: l.spearfishingItemRow.StringAt(0),
		})
	}
	return out
}

func (s *Service) buildShopSources(itemID int) []ShopPurchaseSource {
	rawEntries := s.store.ShopsByItem[itemID]
	if len(rawEntries) == 0 {
		return nil
	}
	type mergedGroup struct {
		source   ShopPurchaseSource
		seenNPCs map[int]bool
	}
	groups := make(map[string]*mergedGroup)
	var keys []string

	for _, raw := range rawEntries {
		costs := make([]ShopCostItem, 0, len(raw.Costs))
		costKeyParts := make([]string, 0, len(raw.Costs))
		for _, c := range raw.Costs {
			var ci ShopCostItem
			if c.NameCn != "" && c.IconID > 0 {
				ci = ShopCostItem(c)
			} else if sum, ok := s.store.ItemSummaryByID[c.ItemID]; ok {
				ci = ShopCostItem{
					ItemID:         c.ItemID,
					NameCn:         sum.NameCn,
					IconID:         sum.IconID,
					Amount:         c.Amount,
					Collectability: c.Collectability,
				}
			} else {
				costRow := s.store.ItemsPrimary[c.ItemID]
				name := costRow.StringAt(9)
				if name == "" {
					name = i18n.Tf(s.appLang(), "gamedata.currency_fallback", "id", strconv.Itoa(c.ItemID))
				}
				icon := costRow.IntAt(10)
				if icon == 0 {
					icon = 65002
				}
				ci = ShopCostItem{
					ItemID:         c.ItemID,
					NameCn:         name,
					IconID:         icon,
					Amount:         c.Amount,
					Collectability: c.Collectability,
				}
			}
			costs = append(costs, ci)
			costKeyParts = append(costKeyParts, fmt.Sprintf("%d:%d", ci.ItemID, ci.Amount))
		}

		hqFlag := 0
		if raw.ReceiveHQ {
			hqFlag = 1
		}
		mKey := fmt.Sprintf("%s:%s:%d:%d:%s", raw.ShopType, raw.ShopName, raw.ReceiveAmount, hqFlag, strings.Join(costKeyParts, "|"))
		grp, ok := groups[mKey]
		if !ok {
			grp = &mergedGroup{
				source: ShopPurchaseSource{
					ShopID:        raw.ShopID,
					ShopType:      raw.ShopType,
					ShopTypeLabel: raw.ShopTypeLabel,
					ShopName:      raw.ShopName,
					ReceiveAmount: raw.ReceiveAmount,
					ReceiveHQ:     raw.ReceiveHQ,
					Costs:         costs,
				},
				seenNPCs: make(map[int]bool),
			}
			groups[mKey] = grp
			keys = append(keys, mKey)
		}

		for _, npcID := range raw.NpcIDs {
			if grp.seenNPCs[npcID] || len(grp.source.NPCs) >= 16 {
				continue
			}
			vendor, ok := resolveVendorNpc(npcID, s.store.NpcNames, s.store.NpcLocations, s.store.TerritoryTypes, s.store.Maps, s.store.PlaceNames)
			if !ok {
				continue
			}
			grp.seenNPCs[npcID] = true
			grp.source.NPCs = append(grp.source.NPCs, vendor)
		}
	}

	out := make([]ShopPurchaseSource, 0, len(keys))
	for _, k := range keys {
		src := groups[k].source
		sort.Slice(src.NPCs, func(i, j int) bool {
			iHas := src.NPCs[i].MapPath != ""
			jHas := src.NPCs[j].MapPath != ""
			if iHas != jHas {
				return iHas
			}
			return src.NPCs[i].NpcID < src.NPCs[j].NpcID
		})
		out = append(out, src)
	}
	sort.Slice(out, func(i, j int) bool {
		aHas := len(out[i].NPCs) > 0 && out[i].NPCs[0].MapPath != ""
		bHas := len(out[j].NPCs) > 0 && out[j].NPCs[0].MapPath != ""
		if aHas != bHas {
			return aHas
		}
		return len(out[i].NPCs) > len(out[j].NPCs)
	})
	return out
}

func (s *Service) buildRetainerVentures(itemID int, itemName string) []RetainerVentureSource {
	raws := s.store.RetainerVenturesByItem[itemID]
	if len(raws) == 0 {
		return nil
	}
	out := make([]RetainerVentureSource, 0, len(raws))
	for _, r := range raws {
		out = append(out, RetainerVentureSource{
			TaskID:               r.TaskID,
			TaskName:             fmt.Sprintf("%s：%s", r.VentureTypeLabel, itemName),
			VentureType:          r.VentureType,
			VentureTypeLabel:     r.VentureTypeLabel,
			ClassJobCategoryID:   r.ClassJobCategoryID,
			ClassJobCategoryName: r.ClassJobCategoryName,
			RetainerLevel:        r.RetainerLevel,
			VentureCost:          r.VentureCost,
			DurationMinutes:      r.DurationMinutes,
			Experience:           r.Experience,
			RequiresLogUnlock:    r.RequiresLogUnlock,
			Tiers:                r.Tiers,
		})
	}
	return out
}

// SearchAchievements 搜索与筛选成就与称号列表。
func (s *Service) SearchAchievements(query AchievementSearchQuery) AchievementSearchResponse {
	q := strings.ToLower(strings.TrimSpace(query.Q))
	numericQuery := -1
	if q != "" {
		if parsed, err := strconv.Atoi(q); err == nil && parsed > 0 {
			numericQuery = parsed
		}
	}
	filter := query.Filter
	if filter == "" {
		filter = "all"
	}

	filtered := make([]AchievementEntry, 0, 128)
	for _, ach := range s.store.Achievements {
		if query.KindID > 0 && ach.KindID != query.KindID {
			continue
		}
		if query.CategoryID > 0 && ach.CategoryID != query.CategoryID {
			continue
		}
		if filter == "with_title" && ach.TitleID <= 0 {
			continue
		}
		if filter == "with_item" && ach.RewardItemID <= 0 {
			continue
		}
		if q != "" {
			if numericQuery > 0 && ach.ID == numericQuery {
				filtered = append(filtered, ach)
				continue
			}
			if !strings.Contains(strings.ToLower(ach.NameCn), q) &&
				!strings.Contains(strings.ToLower(ach.NameZh), q) &&
				!strings.Contains(strings.ToLower(ach.NameEn), q) &&
				!strings.Contains(strings.ToLower(ach.NameJa), q) &&
				!strings.Contains(strings.ToLower(ach.DescriptionCn), q) &&
				!strings.Contains(strings.ToLower(ach.TitleMasculineCn), q) &&
				!strings.Contains(strings.ToLower(ach.TitleFeminineCn), q) &&
				!strings.Contains(strings.ToLower(ach.TitleNameEn), q) &&
				!strings.Contains(strings.ToLower(ach.TitleNameJa), q) &&
				!strings.Contains(strings.ToLower(ach.RewardItemNameCn), q) &&
				strconv.Itoa(ach.ID) != q {
				continue
			}
		}
		filtered = append(filtered, ach)
	}
	if numericQuery > 0 && len(filtered) > 1 {
		sort.SliceStable(filtered, func(i, j int) bool {
			return (filtered[i].ID == numericQuery) && (filtered[j].ID != numericQuery)
		})
	}

	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	total := len(filtered)
	totalPages := max(1, (total+pageSize-1)/pageSize)
	page := query.Page
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * pageSize
	end := min(total, start+pageSize)
	var items []AchievementEntry
	if start < end {
		items = filtered[start:end]
	}

	return AchievementSearchResponse{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}

// paginateRange 计算标准 1-based 分页范围与总页数。
func paginateRange(total, reqPage, pageSize int) (page, totalPages, start, end int) {
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	totalPages = max(1, (total+pageSize-1)/pageSize)
	page = reqPage
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start = (page - 1) * pageSize
	end = min(total, start+pageSize)
	return
}

func (s *Service) filterQuests(query QuestSearchQuery) []QuestEntry {
	q := strings.ToLower(strings.TrimSpace(query.Q))
	numericQuery := -1
	if q != "" {
		if parsed, err := strconv.Atoi(q); err == nil && parsed > 0 {
			numericQuery = parsed
		}
	}
	filter := query.Filter
	if filter == "" {
		filter = "all"
	}

	filtered := make([]QuestEntry, 0, 128)
	for _, qe := range s.store.Quests {
		if query.SectionID >= 0 && qe.SectionID != query.SectionID {
			continue
		}
		if query.ExpansionID >= 0 && qe.ExpansionID != query.ExpansionID {
			continue
		}
		if query.MinLevel > 0 && qe.Level < query.MinLevel {
			continue
		}
		if query.MaxLevel > 0 && qe.Level > query.MaxLevel {
			continue
		}
		switch filter {
		case "msq":
			if !qe.IsMSQ {
				continue
			}
		case "feature":
			if !qe.IsFeature {
				continue
			}
		case "repeatable":
			if !qe.IsRepeatable {
				continue
			}
		case "with_item":
			if len(qe.RewardItems) == 0 {
				continue
			}
		}
		if q != "" {
			if numericQuery > 0 && qe.ID == numericQuery {
				filtered = append(filtered, qe)
				continue
			}
			if !strings.Contains(strings.ToLower(qe.NameCn), q) &&
				!strings.Contains(strings.ToLower(qe.NameZh), q) &&
				!strings.Contains(strings.ToLower(qe.NameEn), q) &&
				!strings.Contains(strings.ToLower(qe.NameJa), q) &&
				!strings.Contains(strings.ToLower(qe.InternalID), q) &&
				!strings.Contains(strings.ToLower(qe.GenreName), q) &&
				!strings.Contains(strings.ToLower(qe.PlaceName), q) &&
				!strings.Contains(strings.ToLower(qe.IssuerName), q) {
				continue
			}
		}
		filtered = append(filtered, qe)
	}
	return filtered
}

// SearchQuests 搜索与筛选任务列表。
func (s *Service) SearchQuests(query QuestSearchQuery) QuestSearchResponse {
	filtered := s.filterQuests(query)
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	page, totalPages, start, end := paginateRange(len(filtered), query.Page, pageSize)
	var items []QuestEntry
	if start < end {
		items = filtered[start:end]
	}
	return QuestSearchResponse{
		Items:      items,
		Total:      len(filtered),
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}

// LocateQuestPage 在当前筛选条件下定位指定任务所在分页并返回该页结果。
func (s *Service) LocateQuestPage(query QuestSearchQuery, questID int) (QuestSearchResponse, bool) {
	if s == nil || s.store == nil || questID <= 0 {
		return QuestSearchResponse{}, false
	}
	filtered := s.filterQuests(query)
	idx := -1
	for i, qe := range filtered {
		if qe.ID == questID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return QuestSearchResponse{}, false
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	page, totalPages, start, end := paginateRange(len(filtered), idx/pageSize+1, pageSize)
	var items []QuestEntry
	if start < end {
		items = filtered[start:end]
	}
	return QuestSearchResponse{
		Items:      items,
		Total:      len(filtered),
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, true
}

// GetQuestDetail 根据任务 ID 返回完整任务详情。
func (s *Service) GetQuestDetail(questID int) *QuestEntry {
	q, ok := s.store.QuestByID[questID]
	if !ok {
		return nil
	}
	return &q
}

func (s *Service) filterLeves(query LeveSearchQuery) []LeveEntry {
	q := strings.ToLower(strings.TrimSpace(query.Q))
	numericQuery := -1
	if q != "" {
		if parsed, err := strconv.Atoi(q); err == nil && parsed > 0 {
			numericQuery = parsed
		}
	}
	kind := query.Kind
	if kind == "" {
		kind = "all"
	}

	filtered := make([]LeveEntry, 0, 128)
	for _, lv := range s.store.Leves {
		if kind != "all" && lv.LeveKind != kind {
			continue
		}
		if query.AssignmentTypeID > 0 && lv.AssignmentTypeID != query.AssignmentTypeID {
			continue
		}
		if query.MinLevel > 0 && lv.ClassJobLevel < query.MinLevel {
			continue
		}
		if query.MaxLevel > 0 && lv.ClassJobLevel > query.MaxLevel {
			continue
		}
		if q != "" {
			if numericQuery > 0 && lv.ID == numericQuery {
				filtered = append(filtered, lv)
				continue
			}
			matchedTurnIn := false
			for _, ti := range lv.TurnInItems {
				if strings.Contains(strings.ToLower(ti.ItemName), q) {
					matchedTurnIn = true
					break
				}
			}
			if !matchedTurnIn &&
				!strings.Contains(strings.ToLower(lv.NameCn), q) &&
				!strings.Contains(strings.ToLower(lv.NameZh), q) &&
				!strings.Contains(strings.ToLower(lv.NameEn), q) &&
				!strings.Contains(strings.ToLower(lv.NameJa), q) &&
				!strings.Contains(strings.ToLower(lv.AssignmentTypeName), q) &&
				!strings.Contains(strings.ToLower(lv.TownName), q) &&
				!strings.Contains(strings.ToLower(lv.PlaceNameStart), q) &&
				!strings.Contains(strings.ToLower(lv.ClientName), q) {
				continue
			}
		}
		filtered = append(filtered, lv)
	}
	return filtered
}

// SearchLeves 搜索与筛选理符列表。
func (s *Service) SearchLeves(query LeveSearchQuery) LeveSearchResponse {
	filtered := s.filterLeves(query)
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	page, totalPages, start, end := paginateRange(len(filtered), query.Page, pageSize)
	var items []LeveEntry
	if start < end {
		items = filtered[start:end]
	}
	return LeveSearchResponse{
		Items:      items,
		Total:      len(filtered),
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}

// LocateLevePage 在当前筛选条件下定位指定理符所在分页并返回该页结果。
func (s *Service) LocateLevePage(query LeveSearchQuery, leveID int) (LeveSearchResponse, bool) {
	if s == nil || s.store == nil || leveID <= 0 {
		return LeveSearchResponse{}, false
	}
	filtered := s.filterLeves(query)
	idx := -1
	for i, lv := range filtered {
		if lv.ID == leveID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return LeveSearchResponse{}, false
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	page, totalPages, start, end := paginateRange(len(filtered), idx/pageSize+1, pageSize)
	var items []LeveEntry
	if start < end {
		items = filtered[start:end]
	}
	return LeveSearchResponse{
		Items:      items,
		Total:      len(filtered),
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, true
}

// GetLeveDetail 根据理符 ID 返回完整理符详情。
func (s *Service) GetLeveDetail(leveID int) *LeveEntry {
	lv, ok := s.store.LeveByID[leveID]
	if !ok {
		return nil
	}
	return &lv
}

// toLowerFast 在无需转换（如纯小写 ASCII 或常见不含大写字母的中文/日文）时避免额外分配。
func toLowerFast(s string) string {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b >= 'A' && b <= 'Z' {
			return strings.ToLower(s)
		}
		if b >= 0x80 {
			for _, r := range s[i:] {
				if unicode.IsUpper(r) {
					return strings.ToLower(s)
				}
			}
			return s
		}
	}
	return s
}

// matchNameByContains 按“完全一致 > 前缀匹配 > 包含匹配”评估单组主/副名称与查询词的匹配度。
// 返回 rank（0/3/5 表示主名完全/前缀/包含，1/4/6 表示别名完全/前缀/包含）及命中的别名。
func matchNameByContains(q, qLower, primary string, aliases ...string) (rank int, matchedAlias string, ok bool) {
	bestRank := 99
	bestAlias := ""

	eval := func(candidate string, isAlias bool) {
		cand := strings.TrimSpace(candidate)
		if cand == "" {
			return
		}
		var r int
		switch {
		case strings.EqualFold(cand, q):
			r = 0
		default:
			candLower := toLowerFast(cand)
			switch {
			case strings.HasPrefix(candLower, qLower):
				r = 3
			case strings.Contains(candLower, qLower):
				r = 5
			default:
				return
			}
		}
		if isAlias {
			r++
		}
		if r < bestRank {
			bestRank = r
			if isAlias {
				bestAlias = cand
			} else {
				bestAlias = ""
			}
		}
	}

	eval(primary, false)
	if bestRank == 0 {
		return 0, "", true
	}
	for _, alias := range aliases {
		eval(alias, true)
	}
	if bestRank == 99 {
		return 0, "", false
	}
	return bestRank, bestAlias, true
}

// parseExactPositiveID 仅接受纯正整数或带 # 前缀的正整数（如 "44099" 或 "#44099"）。
func parseExactPositiveID(q string) int {
	raw := strings.TrimPrefix(q, "#")
	if raw == "" {
		return -1
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return -1
		}
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return -1
	}
	return id
}

func sortModuleHits(moduleHits []GlobalSearchHit) {
	if len(moduleHits) <= 1 {
		return
	}
	sort.Slice(moduleHits, func(i, j int) bool {
		if moduleHits[i].matchRank != moduleHits[j].matchRank {
			return moduleHits[i].matchRank < moduleHits[j].matchRank
		}
		if moduleHits[i].sortLevel != moduleHits[j].sortLevel {
			return moduleHits[i].sortLevel > moduleHits[j].sortLevel
		}
		return moduleHits[i].ID < moduleHits[j].ID
	})
}

// selectTopGlobalHits 按匹配度优先、各模块轮询保底的方式选出最多 maxTotal 条跨模块命中，并按匹配度与模块顺序返回。
func selectTopGlobalHits(groups [][]GlobalSearchHit, maxTotal int) ([]GlobalSearchHit, int) {
	total := 0
	for _, g := range groups {
		total += len(g)
	}
	if total == 0 || maxTotal <= 0 {
		return nil, total
	}
	for _, g := range groups {
		sortModuleHits(g)
	}

	offsets := make([]int, len(groups))
	selected := make([]GlobalSearchHit, 0, min(total, maxTotal))

	for len(selected) < maxTotal {
		minRank := 99
		for gi, g := range groups {
			if offsets[gi] < len(g) && g[offsets[gi]].matchRank < minRank {
				minRank = g[offsets[gi]].matchRank
			}
		}
		if minRank == 99 {
			break
		}
		// 统计本匹配等级下各模块的命中切片范围
		rankCount := 0
		rankEnds := make([]int, len(groups))
		for gi, g := range groups {
			end := offsets[gi]
			for end < len(g) && g[end].matchRank == minRank {
				end++
			}
			rankEnds[gi] = end
			rankCount += end - offsets[gi]
		}
		if len(selected)+rankCount <= maxTotal {
			for gi, g := range groups {
				selected = append(selected, g[offsets[gi]:rankEnds[gi]]...)
				offsets[gi] = rankEnds[gi]
			}
			continue
		}
		// 当前匹配等级条目超出剩余配额：轮询各模块依次取 1 条，避免单模块挤占其他模块
		for len(selected) < maxTotal {
			progressed := false
			for gi, g := range groups {
				if offsets[gi] < rankEnds[gi] {
					selected = append(selected, g[offsets[gi]])
					offsets[gi]++
					progressed = true
					if len(selected) >= maxTotal {
						break
					}
				}
			}
			if !progressed {
				break
			}
		}
	}

	// 跨模块按匹配度优先排序（主名精准 > 别名/称号精准 > ID 精准 > 主名前缀 > 别名前缀 > 主名包含 > 别名包含）
	kindOrder := func(k GlobalSearchHitKind) int {
		switch k {
		case GlobalHitItem:
			return 0
		case GlobalHitQuest:
			return 1
		case GlobalHitLeve:
			return 2
		case GlobalHitAchievement:
			return 3
		default:
			return 4
		}
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].matchRank != selected[j].matchRank {
			return selected[i].matchRank < selected[j].matchRank
		}
		return kindOrder(selected[i].Kind) < kindOrder(selected[j].Kind)
	})
	return selected, total
}

// SearchGlobal 在物品、任务、理符与成就（含称号）中对名称进行包含检索（优先排序完全一致、前缀与精确 ID），最多返回 maxTotal 条。
func (s *Service) SearchGlobal(rawQuery string, maxTotal int) GlobalSearchResult {
	q := strings.TrimSpace(rawQuery)
	if q == "" || s == nil || s.store == nil {
		return GlobalSearchResult{Query: q}
	}
	if maxTotal <= 0 {
		maxTotal = 50
	}
	qLower := toLowerFast(q)
	idTarget := parseExactPositiveID(q)

	var itemHits, questHits, leveHits, achHits []GlobalSearchHit

	for _, it := range s.store.ItemSummaries {
		rank, alias, ok := matchNameByContains(q, qLower, it.NameCn, it.NameZh, it.NameJa, it.NameEn)
		if idTarget > 0 && it.ID == idTarget && (!ok || rank > 2) {
			rank = 2
			alias = ""
			ok = true
		}
		if !ok {
			continue
		}
		itemHits = append(itemHits, GlobalSearchHit{
			Kind:         GlobalHitItem,
			ID:           it.ID,
			Title:        it.NameCn,
			MatchedAlias: alias,
			Category:     it.CategoryName,
			ItemLevel:    it.ItemLevel,
			IconID:       it.IconID,
			matchRank:    rank,
			sortLevel:    it.ItemLevel,
		})
	}

	for _, qe := range s.store.Quests {
		cleanTitle := CleanQuestTitle(qe.NameCn)
		if cleanTitle == "" {
			cleanTitle = qe.NameCn
		}
		cleanZh := CleanQuestTitle(qe.NameZh)
		rank, alias, ok := matchNameByContains(q, qLower, cleanTitle, qe.NameCn, cleanZh, qe.NameJa, qe.NameEn)
		if alias == qe.NameCn {
			// 仅当未清理前才与清理后标题相同，避免展示重复别名
			if strings.EqualFold(CleanQuestTitle(alias), cleanTitle) {
				alias = ""
			}
		}
		if idTarget > 0 && qe.ID == idTarget && (!ok || rank > 2) {
			rank = 2
			alias = ""
			ok = true
		}
		if !ok {
			continue
		}
		cat := qe.GenreName
		if cat == "" {
			cat = qe.SectionName
		}
		questHits = append(questHits, GlobalSearchHit{
			Kind:         GlobalHitQuest,
			ID:           qe.ID,
			Title:        cleanTitle,
			MatchedAlias: alias,
			Category:     cat,
			Level:        qe.Level,
			IconID:       qe.IconID,
			matchRank:    rank,
			sortLevel:    qe.Level,
		})
	}

	for _, lv := range s.store.Leves {
		rank, alias, ok := matchNameByContains(q, qLower, lv.NameCn, lv.NameZh, lv.NameJa, lv.NameEn)
		if idTarget > 0 && lv.ID == idTarget && (!ok || rank > 2) {
			rank = 2
			alias = ""
			ok = true
		}
		if !ok {
			continue
		}
		cat := lv.AssignmentTypeName
		if cat == "" {
			cat = lv.LeveKindLabel
		}
		iconID := lv.AssignmentTypeIconID
		if iconID <= 0 {
			iconID = lv.IconCityState
		}
		leveHits = append(leveHits, GlobalSearchHit{
			Kind:         GlobalHitLeve,
			ID:           lv.ID,
			Title:        lv.NameCn,
			MatchedAlias: alias,
			Category:     cat,
			Level:        lv.ClassJobLevel,
			IconID:       iconID,
			matchRank:    rank,
			sortLevel:    lv.ClassJobLevel,
		})
	}

	for _, ach := range s.store.Achievements {
		rank, alias, ok := matchNameByContains(q, qLower, ach.NameCn, ach.NameZh, ach.NameJa, ach.NameEn, ach.TitleMasculineCn, ach.TitleFeminineCn, ach.TitleNameJa, ach.TitleNameEn)
		if idTarget > 0 && ach.ID == idTarget && (!ok || rank > 2) {
			rank = 2
			alias = ""
			ok = true
		}
		if !ok {
			continue
		}
		cat := ach.KindName
		if ach.CategoryName != "" && ach.CategoryName != cat {
			if cat != "" {
				cat += " · " + ach.CategoryName
			} else {
				cat = ach.CategoryName
			}
		}
		achHits = append(achHits, GlobalSearchHit{
			Kind:         GlobalHitAchievement,
			ID:           ach.ID,
			Title:        ach.NameCn,
			MatchedAlias: alias,
			Category:     cat,
			Points:       ach.Points,
			IconID:       ach.IconID,
			RewardItemID: ach.RewardItemID,
			matchRank:    rank,
			sortLevel:    ach.Points,
		})
	}

	hits, totalMatched := selectTopGlobalHits([][]GlobalSearchHit{itemHits, questHits, leveHits, achHits}, maxTotal)
	return GlobalSearchResult{
		Query:        q,
		Hits:         hits,
		TotalMatched: totalMatched,
	}
}

// GetIconImage 从已挂载客户端解码道具或职业图标为 *image.RGBA。
func (s *Service) GetIconImage(iconID int, isHQ bool) (*image.RGBA, error) {
	img, err := sqpack.ReadIconImage(s.store.PrimaryReader, s.store.TextureCache, iconID, isHQ)
	if img != nil || err != nil {
		return img, err
	}
	if s.store.GlobalReader != nil && s.store.GlobalReader != s.store.PrimaryReader {
		return sqpack.ReadIconImage(s.store.GlobalReader, s.store.TextureCache, iconID, isHQ)
	}
	return nil, nil
}

// GetMapImage 从已挂载客户端解码指定地图贴图为 *image.RGBA。
func (s *Service) GetMapImage(mapPath string) (*image.RGBA, error) {
	img, err := sqpack.ReadMapImage(s.store.PrimaryReader, s.store.TextureCache, mapPath)
	if img != nil || err != nil {
		return img, err
	}
	if s.store.GlobalReader != nil && s.store.GlobalReader != s.store.PrimaryReader {
		return sqpack.ReadMapImage(s.store.GlobalReader, s.store.TextureCache, mapPath)
	}
	return nil, nil
}
