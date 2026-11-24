package gamedata

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

var equipSlotNames = []string{
	"主手", "副手", "头部", "身体", "手部", "腰部", "腿部", "脚部",
	"耳饰", "颈部", "腕部", "戒指", "戒指", "灵魂水晶",
}

func formatEquipSlotCategory(row sqpack.Row) string {
	var slots []string
	limit := min(len(row), len(equipSlotNames))
	for i := 0; i < limit; i++ {
		if row.IntAt(i) > 0 {
			name := equipSlotNames[i]
			if !slices.Contains(slots, name) {
				slots = append(slots, name)
			}
		}
	}
	return strings.Join(slots, " / ")
}

// HasItemEquipmentStats 判断 Item 行是否具有装备性能与副属性词条。
func HasItemEquipmentStats(row sqpack.Row) bool {
	if row.IntAt(51) > 0 || row.IntAt(52) > 0 || row.IntAt(55) > 0 ||
		row.IntAt(56) > 0 || row.IntAt(57) > 0 || row.IntAt(58) > 0 ||
		row.IntAt(86) > 0 {
		return true
	}
	for i := 0; i < 6; i++ {
		if row.IntAt(59+i*2) > 0 && row.IntAt(60+i*2) != 0 {
			return true
		}
		if row.IntAt(73+i*2) > 0 && row.IntAt(74+i*2) != 0 {
			return true
		}
	}
	return false
}

type categoryDict struct {
	name   string
	iconID int
}

type classJobDict struct {
	name string
	abbr string
}

type baseParamDict struct {
	name        string
	description string
}

type bookDict struct {
	itemID int
	name   string
}

type gatheringItemLevelDict struct {
	level int
	stars int
}

type recipeUsage struct {
	recipeID int
	amount   int
}

type spearfishingLink struct {
	spearfishingItemRow sqpack.Row
	notebookID          int
	notebookRow         sqpack.Row
}

// Store 保存已加载到内存中的全量游戏数据表与正反向索引（支持单国服、单国际服及双服模式）。
type Store struct {
	CNReader      *sqpack.GameReader
	GlobalReader  *sqpack.GameReader
	PrimaryReader *sqpack.GameReader
	TextureCache  *sqpack.TextureCache

	HasCN          bool
	HasGlobal      bool
	CNVersion      string
	GlobalVersion  string
	PrimaryVersion string
	CNPath         string
	GlobalPath     string
	LoadTimeMs     int64

	// 核心原始表与字典映射
	ItemsPrimary        map[int]sqpack.Row
	ItemsEn             map[int]sqpack.Row
	ItemsJa             map[int]sqpack.Row
	Categories          map[int]categoryDict
	ClassJobs           map[int]classJobDict
	ClassJobCategories  map[int]string
	BaseParams          map[int]baseParamDict
	EquipSlots          map[int]string
	ItemRepairResources map[int]int
	ItemActions         map[int]sqpack.Row
	ItemFoods           map[int]sqpack.Row
	CabinetItemIDs      map[int]struct{}

	// 配方表与反向索引
	Recipes                 map[int]sqpack.Row
	RecipesByResultItem     map[int][]int
	RecipesByIngredientItem map[int][]recipeUsage
	RecipeLevelTables       map[int]sqpack.Row
	CraftTypes              map[int]CraftTypeInfo
	SecretRecipeBooks       map[int]bookDict

	// 采集与捕鱼表及反向索引
	GatheringItems                 map[int]sqpack.Row
	GatheringItemsByItem           map[int][]int
	GatheringPointsByGatheringItem map[int][]int
	GatheringPoints                map[int]sqpack.Row
	GatheringPointBases            map[int]sqpack.Row
	ExportedGatheringPoints        map[int]sqpack.Row
	GatheringPointTransients       map[int]sqpack.Row
	GatheringRarePopTimeTables     map[int]sqpack.Row
	GatheringItemLevels            map[int]gatheringItemLevelDict
	GatheringTypes                 map[int]categoryDict
	GatheringSubCategories         map[int]bookDict
	TerritoryTypes                 map[int]sqpack.Row
	PlaceNames                     map[int]string
	Maps                           map[int]sqpack.Row
	FishingSpots                   map[int]sqpack.Row
	FishingSpotsByItem             map[int][]int
	FishParametersByItem           map[int]sqpack.Row
	SpearfishingByItem             map[int][]spearfishingLink

	// NPC 商店与坐标反向索引
	ShopsByItem      map[int][]RawShopEntry
	NpcNames         map[int]RawNpcName
	NpcLocations     map[int]RawNpcLocation
	TotalActiveShops int

	// 雇员探险反向索引
	RetainerVenturesByItem map[int][]RawRetainerVenture
	TotalRetainerTasks     int

	// 预构建道具摘要索引与统计分类
	ItemSummaries   []ItemSummary
	ItemSummaryByID map[int]ItemSummary
	CategoryList    []ItemCategoryInfo
	CraftTypeList   []CraftTypeInfo

	// 成就与称号索引
	Achievements             []AchievementEntry
	AchievementByID          map[int]AchievementEntry
	AchievementsByRewardItem map[int][]AchievementEntry
	AchievementKindList      []AchievementKindInfo
	TotalTitles              int
}

// Close 释放所有底层的 SqPack 只读句柄。
func (s *Store) Close() error {
	var firstErr error
	if s.CNReader != nil {
		if err := s.CNReader.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.GlobalReader != nil {
		if err := s.GlobalReader.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// LoadGameDataStore 并发读取已配置的本地 FF14 客户端数据并构建完整内存正反向索引。
func LoadGameDataStore(resolved *config.ResolvedPaths) (*Store, error) {
	if resolved == nil || (!resolved.HasCN() && !resolved.HasGlobal()) {
		return nil, config.ErrNoClientConfigured
	}

	startTime := time.Now()

	var cnReader, globalReader *sqpack.GameReader
	var cnVer, globalVer, cnPath, globalPath string
	if resolved.HasCN() {
		cnReader = sqpack.NewGameReader(resolved.CN.GamePath)
		cnVer = resolved.CN.Version
		cnPath = resolved.CN.RootPath
	}
	if resolved.HasGlobal() {
		globalReader = sqpack.NewGameReader(resolved.Global.GamePath)
		globalVer = resolved.Global.Version
		globalPath = resolved.Global.RootPath
	}

	// 确定主数据读取器与主语言（优先国服中文，仅国际服时使用国际服英文为主语言）
	primaryReader := cnReader
	primaryLang := sqpack.LangChineseSimplified
	if primaryReader == nil {
		primaryReader = globalReader
		primaryLang = sqpack.LangEnglish
	}

	var (
		itemsPrimary, itemsEn, itemsJa                                                map[int]sqpack.Row
		rawCategories, rawClassJobs, rawClassJobCategories, rawBaseParams             map[int]sqpack.Row
		rawEquipSlots, rawRepairResources, itemActions, itemFoods                     map[int]sqpack.Row
		recipes, recipeLevelTables, rawCraftTypes, rawSecretBooks                     map[int]sqpack.Row
		gatheringItems, gatheringPoints, gatheringPointBases, exportedGatheringPoints map[int]sqpack.Row
		gatheringPointTransients, gatheringRarePopTimeTables, rawGatheringItemLevels  map[int]sqpack.Row
		rawGatheringTypes, rawGatheringSubCategories, territoryTypes, rawPlaceNames   map[int]sqpack.Row
		maps, fishingSpots, fishParameters, spearfishingItems, spearfishingNotebooks  map[int]sqpack.Row
		gilShops, specialShops, fccShops, tomestonesItems                             map[int]sqpack.Row
		enpcResidents, enpcBases, topicSelects, preHandlers                           map[int]sqpack.Row
		inclusionShops, inclusionCategories, customTalks, levels, rawCabinets         map[int]sqpack.Row
		achievementsPrimary, achievementsEn, achievementsJa                           map[int]sqpack.Row
		rawAchievementCategories, rawAchievementKinds                                 map[int]sqpack.Row
		titlesPrimary, titlesEn, titlesJa                                             map[int]sqpack.Row
		retainerTasks, retainerTaskNormals, retainerTaskParameters                    map[int]sqpack.Row

		gatheringItemPointSubrows map[int][]sqpack.SubRow
		gilShopItems              map[int][]sqpack.SubRow
		gcScripShopItems          map[int][]sqpack.SubRow
		inclusionSeries           map[int][]sqpack.SubRow
	)

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)

	setErr := func(name string, err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = fmt.Errorf("读取数据表 %s 失败: %w", name, err)
		}
		errMu.Unlock()
	}

	loadSheet := func(reader *sqpack.GameReader, sheet string, lang sqpack.Language, cols []int, dst *map[int]sqpack.Row) {
		if reader == nil {
			*dst = make(map[int]sqpack.Row)
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := sqpack.LoadSheetRows(reader, sheet, lang, cols)
			if err != nil {
				setErr(sheet, err)
				return
			}
			*dst = rows
		}()
	}

	loadSub := func(reader *sqpack.GameReader, sheet string, lang sqpack.Language, dst *map[int][]sqpack.SubRow) {
		if reader == nil {
			*dst = make(map[int][]sqpack.SubRow)
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := sqpack.LoadSubrowSheet(reader, sheet, lang)
			if err != nil {
				setErr(sheet, err)
				return
			}
			*dst = rows
		}()
	}

	// 并发加载全量核心数据表
	loadSheet(primaryReader, "Item", primaryLang, nil, &itemsPrimary)
	if globalReader != nil {
		if cnReader != nil {
			loadSheet(globalReader, "Item", sqpack.LangEnglish, []int{8, 9}, &itemsEn)
		}
		loadSheet(globalReader, "Item", sqpack.LangJapanese, []int{8, 9}, &itemsJa)
	} else {
		itemsEn = make(map[int]sqpack.Row)
		itemsJa = make(map[int]sqpack.Row)
	}

	loadSheet(primaryReader, "ItemUICategory", primaryLang, nil, &rawCategories)
	loadSheet(primaryReader, "ClassJob", primaryLang, nil, &rawClassJobs)
	loadSheet(primaryReader, "ClassJobCategory", primaryLang, nil, &rawClassJobCategories)
	loadSheet(primaryReader, "BaseParam", primaryLang, nil, &rawBaseParams)
	loadSheet(primaryReader, "EquipSlotCategory", sqpack.LangNone, nil, &rawEquipSlots)
	loadSheet(primaryReader, "ItemRepairResource", sqpack.LangNone, nil, &rawRepairResources)
	loadSheet(primaryReader, "ItemAction", sqpack.LangNone, nil, &itemActions)
	loadSheet(primaryReader, "ItemFood", sqpack.LangNone, nil, &itemFoods)
	loadSheet(primaryReader, "Recipe", sqpack.LangNone, nil, &recipes)
	loadSheet(primaryReader, "RecipeLevelTable", sqpack.LangNone, nil, &recipeLevelTables)
	loadSheet(primaryReader, "CraftType", primaryLang, nil, &rawCraftTypes)
	loadSheet(primaryReader, "SecretRecipeBook", primaryLang, nil, &rawSecretBooks)
	loadSheet(primaryReader, "GatheringItem", sqpack.LangNone, nil, &gatheringItems)
	loadSub(primaryReader, "GatheringItemPoint", sqpack.LangNone, &gatheringItemPointSubrows)
	loadSheet(primaryReader, "GatheringPoint", sqpack.LangNone, nil, &gatheringPoints)
	loadSheet(primaryReader, "GatheringPointBase", sqpack.LangNone, nil, &gatheringPointBases)
	loadSheet(primaryReader, "ExportedGatheringPoint", sqpack.LangNone, nil, &exportedGatheringPoints)
	loadSheet(primaryReader, "GatheringPointTransient", sqpack.LangNone, nil, &gatheringPointTransients)
	loadSheet(primaryReader, "GatheringRarePopTimeTable", sqpack.LangNone, nil, &gatheringRarePopTimeTables)
	loadSheet(primaryReader, "GatheringItemLevelConvertTable", sqpack.LangNone, nil, &rawGatheringItemLevels)
	loadSheet(primaryReader, "GatheringType", primaryLang, nil, &rawGatheringTypes)
	loadSheet(primaryReader, "GatheringSubCategory", primaryLang, nil, &rawGatheringSubCategories)
	loadSheet(primaryReader, "TerritoryType", sqpack.LangNone, nil, &territoryTypes)
	loadSheet(primaryReader, "PlaceName", primaryLang, nil, &rawPlaceNames)
	loadSheet(primaryReader, "Map", sqpack.LangNone, nil, &maps)
	loadSheet(primaryReader, "FishingSpot", primaryLang, nil, &fishingSpots)
	loadSheet(primaryReader, "FishParameter", primaryLang, nil, &fishParameters)
	loadSheet(primaryReader, "SpearfishingItem", primaryLang, nil, &spearfishingItems)
	loadSheet(primaryReader, "SpearfishingNotebook", sqpack.LangNone, nil, &spearfishingNotebooks)
	loadSheet(primaryReader, "GilShop", primaryLang, nil, &gilShops)
	loadSub(primaryReader, "GilShopItem", sqpack.LangNone, &gilShopItems)
	loadSheet(primaryReader, "SpecialShop", primaryLang, nil, &specialShops)
	loadSub(primaryReader, "GCScripShopItem", sqpack.LangNone, &gcScripShopItems)
	loadSheet(primaryReader, "FccShop", primaryLang, nil, &fccShops)
	loadSheet(primaryReader, "TomestonesItem", sqpack.LangNone, nil, &tomestonesItems)
	loadSheet(primaryReader, "ENpcResident", primaryLang, nil, &enpcResidents)
	loadSheet(primaryReader, "ENpcBase", sqpack.LangNone, nil, &enpcBases)
	loadSheet(primaryReader, "TopicSelect", primaryLang, nil, &topicSelects)
	loadSheet(primaryReader, "PreHandler", sqpack.LangNone, nil, &preHandlers)
	loadSheet(primaryReader, "InclusionShop", sqpack.LangNone, nil, &inclusionShops)
	loadSheet(primaryReader, "InclusionShopCategory", primaryLang, nil, &inclusionCategories)
	loadSub(primaryReader, "InclusionShopSeries", sqpack.LangNone, &inclusionSeries)
	loadSheet(primaryReader, "CustomTalk", sqpack.LangNone, nil, &customTalks)
	loadSheet(primaryReader, "Level", sqpack.LangNone, nil, &levels)
	loadSheet(primaryReader, "Cabinet", sqpack.LangNone, nil, &rawCabinets)

	loadSheet(primaryReader, "Achievement", primaryLang, nil, &achievementsPrimary)
	if globalReader != nil {
		if cnReader != nil {
			loadSheet(globalReader, "Achievement", sqpack.LangEnglish, []int{1, 2}, &achievementsEn)
		}
		loadSheet(globalReader, "Achievement", sqpack.LangJapanese, []int{1, 2}, &achievementsJa)
	} else {
		achievementsEn = make(map[int]sqpack.Row)
		achievementsJa = make(map[int]sqpack.Row)
	}
	loadSheet(primaryReader, "AchievementCategory", primaryLang, nil, &rawAchievementCategories)
	loadSheet(primaryReader, "AchievementKind", primaryLang, nil, &rawAchievementKinds)
	loadSheet(primaryReader, "Title", primaryLang, nil, &titlesPrimary)
	if globalReader != nil {
		if cnReader != nil {
			loadSheet(globalReader, "Title", sqpack.LangEnglish, []int{0, 1}, &titlesEn)
		}
		loadSheet(globalReader, "Title", sqpack.LangJapanese, []int{0, 1}, &titlesJa)
	} else {
		titlesEn = make(map[int]sqpack.Row)
		titlesJa = make(map[int]sqpack.Row)
	}
	loadSheet(primaryReader, "RetainerTask", sqpack.LangNone, nil, &retainerTasks)
	loadSheet(primaryReader, "RetainerTaskNormal", sqpack.LangNone, nil, &retainerTaskNormals)
	loadSheet(primaryReader, "RetainerTaskParameter", sqpack.LangNone, nil, &retainerTaskParameters)

	wg.Wait()
	if firstErr != nil {
		if cnReader != nil {
			_ = cnReader.Close()
		}
		if globalReader != nil {
			_ = globalReader.Close()
		}
		return nil, firstErr
	}

	// 仅国际服模式时，itemsPrimary 本身即为英文表，复用避免重复读取
	if cnReader == nil && globalReader != nil {
		itemsEn = itemsPrimary
		achievementsEn = achievementsPrimary
		titlesEn = titlesPrimary
	}

	// 构建基础字典映射
	categories := make(map[int]categoryDict, len(rawCategories))
	for id, row := range rawCategories {
		if name := row.StringAt(0); name != "" {
			categories[id] = categoryDict{name: name, iconID: row.IntAt(1)}
		}
	}

	classJobs := make(map[int]classJobDict, len(rawClassJobs))
	for id, row := range rawClassJobs {
		if name := row.StringAt(0); name != "" {
			classJobs[id] = classJobDict{name: name, abbr: row.StringAt(1)}
		}
	}

	classJobCategories := make(map[int]string, len(rawClassJobCategories))
	for id, row := range rawClassJobCategories {
		if name := row.StringAt(0); name != "" {
			classJobCategories[id] = name
		}
	}

	baseParams := make(map[int]baseParamDict, len(rawBaseParams))
	for id, row := range rawBaseParams {
		if name := row.StringAt(1); name != "" {
			baseParams[id] = baseParamDict{name: name, description: row.StringAt(2)}
		}
	}

	equipSlots := make(map[int]string, len(rawEquipSlots))
	for id, row := range rawEquipSlots {
		equipSlots[id] = formatEquipSlotCategory(row)
	}

	itemRepairResources := make(map[int]int, len(rawRepairResources))
	for id, row := range rawRepairResources {
		itemRepairResources[id] = row.IntAt(0)
	}

	secretRecipeBooks := make(map[int]bookDict, len(rawSecretBooks))
	for id, row := range rawSecretBooks {
		if name := row.StringAt(1); name != "" {
			secretRecipeBooks[id] = bookDict{itemID: row.IntAt(0), name: name}
		}
	}

	gatheringItemLevels := make(map[int]gatheringItemLevelDict, len(rawGatheringItemLevels))
	for id, row := range rawGatheringItemLevels {
		gatheringItemLevels[id] = gatheringItemLevelDict{level: row.IntAt(0), stars: row.IntAt(1)}
	}

	gatheringTypes := make(map[int]categoryDict, len(rawGatheringTypes))
	for id, row := range rawGatheringTypes {
		gatheringTypes[id] = categoryDict{name: row.StringAt(0), iconID: row.IntAt(1)}
	}

	gatheringSubCategories := make(map[int]bookDict, len(rawGatheringSubCategories))
	for id, row := range rawGatheringSubCategories {
		if name := row.StringAt(5); name != "" {
			gatheringSubCategories[id] = bookDict{itemID: row.IntAt(4), name: name}
		}
	}

	placeNames := make(map[int]string, len(rawPlaceNames))
	for id, row := range rawPlaceNames {
		if name := row.StringAt(0); name != "" {
			placeNames[id] = name
		}
	}

	// 构建配方索引
	recipesByResult := make(map[int][]int)
	recipesByIngredient := make(map[int][]recipeUsage)
	recipeCountByCraft := make(map[int]int)
	for recipeID, row := range recipes {
		resultItemID := row.IntAt(4)
		if resultItemID <= 0 {
			continue
		}
		craftTypeID := row.IntAt(1)
		recipeCountByCraft[craftTypeID]++
		recipesByResult[resultItemID] = append(recipesByResult[resultItemID], recipeID)
		for i := 0; i < 8; i++ {
			ingID := row.IntAt(6 + i*2)
			ingAmt := row.IntAt(7 + i*2)
			if ingID > 0 && ingAmt > 0 {
				recipesByIngredient[ingID] = append(recipesByIngredient[ingID], recipeUsage{
					recipeID: recipeID,
					amount:   ingAmt,
				})
			}
		}
	}
	for _, list := range recipesByResult {
		sort.Ints(list)
	}

	craftTypes := make(map[int]CraftTypeInfo, len(rawCraftTypes))
	var craftTypeList []CraftTypeInfo
	for id, row := range rawCraftTypes {
		name := row.StringAt(2)
		if name == "" {
			continue
		}
		jobName := name
		if cj, ok := classJobs[id+8]; ok && cj.name != "" {
			jobName = cj.name
		}
		info := CraftTypeInfo{
			ID:          id,
			Name:        name,
			JobName:     jobName,
			IconID:      62008 + id,
			RecipeCount: recipeCountByCraft[id],
		}
		craftTypes[id] = info
		craftTypeList = append(craftTypeList, info)
	}
	sort.Slice(craftTypeList, func(i, j int) bool { return craftTypeList[i].ID < craftTypeList[j].ID })

	// 构建采集与捕鱼反向索引
	gatheringItemsByItem := make(map[int][]int)
	for gItemID, row := range gatheringItems {
		itemID := row.IntAt(0)
		if itemID > 0 {
			gatheringItemsByItem[itemID] = append(gatheringItemsByItem[itemID], gItemID)
		}
	}

	gatheringPointsByGItem := make(map[int][]int)
	addPointLink := func(gItemID, pointID int) {
		if gItemID <= 0 || pointID <= 0 {
			return
		}
		if !slices.Contains(gatheringPointsByGItem[gItemID], pointID) {
			gatheringPointsByGItem[gItemID] = append(gatheringPointsByGItem[gItemID], pointID)
		}
	}
	for gItemID, subs := range gatheringItemPointSubrows {
		for _, sub := range subs {
			addPointLink(gItemID, sub.Data.IntAt(0))
		}
	}
	for pointID, ptRow := range gatheringPoints {
		baseID := ptRow.IntAt(3)
		territoryID := ptRow.IntAt(7)
		placeNameID := ptRow.IntAt(8)
		if baseID <= 0 || territoryID <= 1 || placeNameID <= 0 {
			continue
		}
		baseRow, ok := gatheringPointBases[baseID]
		if !ok {
			continue
		}
		for i := 2; i <= 9; i++ {
			gItemID := baseRow.IntAt(i)
			if gItemID > 0 && gItemID < 20000 {
				addPointLink(gItemID, pointID)
			}
		}
	}

	fishingSpotsByItem := make(map[int][]int)
	for spotID, row := range fishingSpots {
		if row.IntAt(6) <= 0 {
			continue
		}
		for i := 13; i <= 22; i++ {
			fishID := row.IntAt(i)
			if fishID > 0 && !slices.Contains(fishingSpotsByItem[fishID], spotID) {
				fishingSpotsByItem[fishID] = append(fishingSpotsByItem[fishID], spotID)
			}
		}
	}

	fishParamsByItem := make(map[int]sqpack.Row)
	for _, row := range fishParameters {
		itemID := row.IntAt(4)
		if itemID > 0 {
			if _, exists := fishParamsByItem[itemID]; !exists {
				fishParamsByItem[itemID] = row
			}
		}
	}

	type nbEntry struct {
		id  int
		row sqpack.Row
	}
	notebooksByBaseID := make(map[int][]nbEntry)
	for nbID, nbRow := range spearfishingNotebooks {
		if baseID := nbRow.IntAt(9); baseID > 0 {
			notebooksByBaseID[baseID] = append(notebooksByBaseID[baseID], nbEntry{id: nbID, row: nbRow})
		}
	}
	spearfishingByItem := make(map[int][]spearfishingLink)
	for baseID, nbs := range notebooksByBaseID {
		baseRow, ok := gatheringPointBases[baseID]
		if !ok {
			continue
		}
		for i := 2; i <= 9; i++ {
			sfID := baseRow.IntAt(i)
			if sfID <= 0 {
				continue
			}
			sfRow, ok := spearfishingItems[sfID]
			if !ok {
				continue
			}
			itemID := sfRow.IntAt(1)
			if itemID <= 0 {
				continue
			}
			for _, nb := range nbs {
				spearfishingByItem[itemID] = append(spearfishingByItem[itemID], spearfishingLink{
					spearfishingItemRow: sfRow,
					notebookID:          nb.id,
					notebookRow:         nb.row,
				})
			}
		}
	}

	// 构建商店与雇员探险索引
	shopIdx := buildShopIndices(shopLoaderInput{
		itemsPrimary:        itemsPrimary,
		gilShops:            gilShops,
		gilShopItems:        gilShopItems,
		specialShops:        specialShops,
		gcScripShopItems:    gcScripShopItems,
		fccShops:            fccShops,
		tomestonesItems:     tomestonesItems,
		enpcResidents:       enpcResidents,
		enpcBases:           enpcBases,
		topicSelects:        topicSelects,
		preHandlers:         preHandlers,
		inclusionShops:      inclusionShops,
		inclusionCategories: inclusionCategories,
		inclusionSeries:     inclusionSeries,
		customTalks:         customTalks,
		levels:              levels,
	})

	retainerIdx := buildRetainerIndices(retainerTasks, retainerTaskNormals, retainerTaskParameters, classJobCategories)

	cabinetIDs := make(map[int]struct{}, len(rawCabinets))
	for _, row := range rawCabinets {
		if id := row.IntAt(0); id > 0 {
			cabinetIDs[id] = struct{}{}
		}
	}

	// 构建全量有效道具摘要列表
	itemSummaries := make([]ItemSummary, 0, len(itemsPrimary))
	itemSummaryByID := make(map[int]ItemSummary, len(itemsPrimary))
	categoryCount := make(map[int]int)

	for itemID, row := range itemsPrimary {
		if itemID <= 0 {
			continue
		}
		namePrimary := row.StringAt(9)
		if namePrimary == "" {
			continue
		}
		enName := ""
		if enRow, ok := itemsEn[itemID]; ok {
			enName = enRow.StringAt(9)
		}
		jaName := ""
		if jaRow, ok := itemsJa[itemID]; ok {
			jaName = jaRow.StringAt(9)
		}

		catID := row.IntAt(15)
		catInfo, hasCat := categories[catID]
		catName := "其他"
		catIconID := 0
		if hasCat {
			catName = catInfo.name
			catIconID = catInfo.iconID
		}

		hasGatherNode := false
		for _, gid := range gatheringItemsByItem[itemID] {
			if len(gatheringPointsByGItem[gid]) > 0 {
				hasGatherNode = true
				break
			}
		}
		isGatherable := hasGatherNode || len(fishingSpotsByItem[itemID]) > 0 || len(spearfishingByItem[itemID]) > 0

		itemShops := shopIdx.ShopsByItem[itemID]
		isPurchasable := len(itemShops) > 0
		isGilShop := false
		isSpecialShop := false
		for _, sh := range itemShops {
			if sh.ShopType == "gil" {
				isGilShop = true
			} else {
				isSpecialShop = true
			}
		}

		isTradable := !row.BoolAt(22)
		isMarketable := isTradable && row.IntAt(16) > 0

		summary := ItemSummary{
			ID:                 itemID,
			NameCn:             namePrimary,
			NameEn:             enName,
			NameJa:             jaName,
			IconID:             row.IntAt(10),
			ItemLevel:          row.IntAt(11),
			EquipLevel:         row.IntAt(40),
			Rarity:             row.IntAt(12),
			CategoryID:         catID,
			CategoryName:       catName,
			CategoryIconID:     catIconID,
			PriceMid:           row.IntAt(25),
			CanBeHQ:            row.BoolAt(27),
			IsTradable:         isTradable,
			IsMarketable:       isMarketable,
			IsCraftable:        len(recipesByResult[itemID]) > 0,
			IsGatherable:       isGatherable,
			IsPurchasable:      isPurchasable,
			IsGilShop:          isGilShop,
			IsSpecialShop:      isSpecialShop,
			IsRetainerVenture:  len(retainerIdx.VenturesByItem[itemID]) > 0,
			HasEquipmentStats:  HasItemEquipmentStats(row),
			UsedInRecipesCount: len(recipesByIngredient[itemID]),
		}
		itemSummaries = append(itemSummaries, summary)
		itemSummaryByID[itemID] = summary
		if catID > 0 {
			categoryCount[catID]++
		}
	}
	sort.Slice(itemSummaries, func(i, j int) bool { return itemSummaries[i].ID < itemSummaries[j].ID })

	var categoryList []ItemCategoryInfo
	for id, info := range categories {
		if cnt := categoryCount[id]; cnt > 0 {
			categoryList = append(categoryList, ItemCategoryInfo{
				ID:     id,
				Name:   info.name,
				IconID: info.iconID,
				Count:  cnt,
			})
		}
	}
	sort.Slice(categoryList, func(i, j int) bool { return categoryList[i].ID < categoryList[j].ID })

	// 解析成就分类、大类与称号表
	achKinds := make(map[int]string)
	for id, row := range rawAchievementKinds {
		if name := row.StringAt(0); name != "" {
			achKinds[id] = name
		}
	}
	type achCatDict struct {
		name   string
		kindID int
	}
	achCats := make(map[int]achCatDict)
	for id, row := range rawAchievementCategories {
		if name := row.StringAt(0); name != "" {
			achCats[id] = achCatDict{name: name, kindID: row.IntAt(1)}
		}
	}

	totalTitles := 0
	for _, row := range titlesPrimary {
		if row.StringAt(0) != "" {
			totalTitles++
		}
	}

	achievements := make([]AchievementEntry, 0, len(achievementsPrimary))
	achievementByID := make(map[int]AchievementEntry, len(achievementsPrimary))
	achievementsByRewardItem := make(map[int][]AchievementEntry)
	kindCount := make(map[int]int)

	for achID, row := range achievementsPrimary {
		if achID <= 0 {
			continue
		}
		namePrimary := row.StringAt(1)
		if namePrimary == "" {
			continue
		}
		enRow := achievementsEn[achID]
		jaRow := achievementsJa[achID]
		catID := row.IntAt(0)
		catInfo := achCats[catID]
		kindID := catInfo.kindID
		kindName := achKinds[kindID]
		if kindName == "" {
			kindName = "其他"
		}
		catName := catInfo.name
		if catName == "" {
			catName = "常规"
		}

		titleID := row.IntAt(6)
		var titleMasc, titleFem, titleEn, titleJa string
		var titleIsPrefix bool
		if titleID > 0 {
			if tr, ok := titlesPrimary[titleID]; ok {
				titleMasc = tr.StringAt(0)
				titleFem = tr.StringAt(1)
				titleIsPrefix = tr.BoolAt(2)
			}
			if tr, ok := titlesEn[titleID]; ok {
				titleEn = tr.StringAt(0)
			}
			if tr, ok := titlesJa[titleID]; ok {
				titleJa = tr.StringAt(0)
			}
		}

		rewardItemID := row.IntAt(7)
		var rewardItemName string
		var rewardItemIcon int
		if rewardItemID > 0 {
			if s, ok := itemSummaryByID[rewardItemID]; ok {
				rewardItemName = s.NameCn
				rewardItemIcon = s.IconID
			}
		}

		entry := AchievementEntry{
			ID:               achID,
			NameCn:           namePrimary,
			NameEn:           enRow.StringAt(1),
			NameJa:           jaRow.StringAt(1),
			DescriptionCn:    row.StringAt(2),
			DescriptionEn:    enRow.StringAt(2),
			DescriptionJa:    jaRow.StringAt(2),
			Points:           row.IntAt(5),
			IconID:           row.IntAt(11),
			KindID:           kindID,
			KindName:         kindName,
			CategoryID:       catID,
			CategoryName:     catName,
			TitleID:          titleID,
			TitleMasculineCn: titleMasc,
			TitleFeminineCn:  titleFem,
			TitleNameEn:      titleEn,
			TitleNameJa:      titleJa,
			TitleIsPrefix:    titleIsPrefix,
			RewardItemID:     rewardItemID,
			RewardItemNameCn: rewardItemName,
			RewardItemIconID: rewardItemIcon,
		}
		achievements = append(achievements, entry)
		achievementByID[achID] = entry
		if kindID > 0 {
			kindCount[kindID]++
		}
		if rewardItemID > 0 {
			achievementsByRewardItem[rewardItemID] = append(achievementsByRewardItem[rewardItemID], entry)
		}
	}
	sort.Slice(achievements, func(i, j int) bool { return achievements[i].ID < achievements[j].ID })

	var achievementKindList []AchievementKindInfo
	for id, name := range achKinds {
		if cnt := kindCount[id]; cnt > 0 {
			achievementKindList = append(achievementKindList, AchievementKindInfo{
				ID:    id,
				Name:  name,
				Count: cnt,
			})
		}
	}
	sort.Slice(achievementKindList, func(i, j int) bool { return achievementKindList[i].ID < achievementKindList[j].ID })

	return &Store{
		CNReader:                       cnReader,
		GlobalReader:                   globalReader,
		PrimaryReader:                  primaryReader,
		TextureCache:                   sqpack.NewTextureCache(1000),
		HasCN:                          resolved.HasCN(),
		HasGlobal:                      resolved.HasGlobal(),
		CNVersion:                      cnVer,
		GlobalVersion:                  globalVer,
		PrimaryVersion:                 resolved.PrimaryVersion(),
		CNPath:                         cnPath,
		GlobalPath:                     globalPath,
		LoadTimeMs:                     time.Since(startTime).Milliseconds(),
		ItemsPrimary:                   itemsPrimary,
		ItemsEn:                        itemsEn,
		ItemsJa:                        itemsJa,
		Categories:                     categories,
		ClassJobs:                      classJobs,
		ClassJobCategories:             classJobCategories,
		BaseParams:                     baseParams,
		EquipSlots:                     equipSlots,
		ItemRepairResources:            itemRepairResources,
		ItemActions:                    itemActions,
		ItemFoods:                      itemFoods,
		CabinetItemIDs:                 cabinetIDs,
		Recipes:                        recipes,
		RecipesByResultItem:            recipesByResult,
		RecipesByIngredientItem:        recipesByIngredient,
		RecipeLevelTables:              recipeLevelTables,
		CraftTypes:                     craftTypes,
		SecretRecipeBooks:              secretRecipeBooks,
		GatheringItems:                 gatheringItems,
		GatheringItemsByItem:           gatheringItemsByItem,
		GatheringPointsByGatheringItem: gatheringPointsByGItem,
		GatheringPoints:                gatheringPoints,
		GatheringPointBases:            gatheringPointBases,
		ExportedGatheringPoints:        exportedGatheringPoints,
		GatheringPointTransients:       gatheringPointTransients,
		GatheringRarePopTimeTables:     gatheringRarePopTimeTables,
		GatheringItemLevels:            gatheringItemLevels,
		GatheringTypes:                 gatheringTypes,
		GatheringSubCategories:         gatheringSubCategories,
		TerritoryTypes:                 territoryTypes,
		PlaceNames:                     placeNames,
		Maps:                           maps,
		FishingSpots:                   fishingSpots,
		FishingSpotsByItem:             fishingSpotsByItem,
		FishParametersByItem:           fishParamsByItem,
		SpearfishingByItem:             spearfishingByItem,
		ShopsByItem:                    shopIdx.ShopsByItem,
		NpcNames:                       shopIdx.NpcNames,
		NpcLocations:                   shopIdx.NpcLocations,
		TotalActiveShops:               shopIdx.TotalActiveShops,
		RetainerVenturesByItem:         retainerIdx.VenturesByItem,
		TotalRetainerTasks:             retainerIdx.TotalRetainerTasks,
		ItemSummaries:                  itemSummaries,
		ItemSummaryByID:                itemSummaryByID,
		CategoryList:                   categoryList,
		CraftTypeList:                  craftTypeList,
		Achievements:                   achievements,
		AchievementByID:                achievementByID,
		AchievementsByRewardItem:       achievementsByRewardItem,
		AchievementKindList:            achievementKindList,
		TotalTitles:                    totalTitles,
	}, nil
}
