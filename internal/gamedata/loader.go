package gamedata

import (
	"fmt"
	"io/fs"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

var equipSlotKeys = []string{
	"gamedata.slot.main_hand", "gamedata.slot.off_hand", "gamedata.slot.head", "gamedata.slot.body",
	"gamedata.slot.hands", "gamedata.slot.waist", "gamedata.slot.legs", "gamedata.slot.feet",
	"gamedata.slot.earrings", "gamedata.slot.necklace", "gamedata.slot.bracelets", "gamedata.slot.ring",
	"gamedata.slot.ring", "gamedata.slot.soul_crystal",
}

func formatEquipSlotCategory(row sqpack.Row) string {
	return formatEquipSlotCategoryForLang(row, i18n.LangZH)
}

func formatEquipSlotCategoryForLang(row sqpack.Row, appLang i18n.Lang) string {
	var slots []string
	limit := min(len(row), len(equipSlotKeys))
	for i := 0; i < limit; i++ {
		if row.IntAt(i) > 0 {
			name := i18n.T(appLang, equipSlotKeys[i])
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
	AppLang        i18n.Lang
	PrimaryLang    sqpack.Language

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
	LevelLocations   map[int]RawNpcLocation
	TotalActiveShops int

	// 雇员探险反向索引
	RetainerVenturesByItem map[int][]RawRetainerVenture
	TotalRetainerTasks     int

	// 任务、理符与收藏品交易索引
	Quests                   []QuestEntry
	QuestByID                map[int]QuestEntry
	QuestsByRewardItem       map[int][]ItemQuestSource
	QuestSectionList         []QuestSectionInfo
	QuestActorIDs            map[int][]int
	EventItemNames           map[int]string
	BNpcNames                map[int]string
	EObjNames                map[int]string
	Leves                    []LeveEntry
	LeveByID                 map[int]LeveEntry
	LevesByItem              map[int][]ItemLeveSource
	LeveAssignmentTypes      []LeveAssignmentTypeInfo
	CollectableTurnInsByItem map[int][]CollectableTurnInSource
	CollectableRewardsByItem map[int][]CollectableRewardSource
	TotalCollectables        int

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
	AchievementCategoryList  []AchievementCategoryInfo
	TotalTitles              int

	sharedReaders bool
	langNone      *rawLangNoneSheets
	langPrimary   *rawPrimaryLangSheets
}

type rawLangNoneSheets struct {
	rawEquipSlots                map[int]sqpack.Row
	rawRepairResources           map[int]sqpack.Row
	itemActions                  map[int]sqpack.Row
	itemFoods                    map[int]sqpack.Row
	recipes                      map[int]sqpack.Row
	recipeLevelTables            map[int]sqpack.Row
	gatheringItems               map[int]sqpack.Row
	gatheringItemPointSubrows    map[int][]sqpack.SubRow
	gatheringPoints              map[int]sqpack.Row
	gatheringPointBases          map[int]sqpack.Row
	exportedGatheringPoints      map[int]sqpack.Row
	gatheringPointTransients     map[int]sqpack.Row
	gatheringRarePopTimeTables   map[int]sqpack.Row
	rawGatheringItemLevels       map[int]sqpack.Row
	territoryTypes               map[int]sqpack.Row
	maps                         map[int]sqpack.Row
	spearfishingNotebooks        map[int]sqpack.Row
	gilShopItems                 map[int][]sqpack.SubRow
	gcScripShopItems             map[int][]sqpack.SubRow
	tomestonesItems              map[int]sqpack.Row
	enpcBases                    map[int]sqpack.Row
	preHandlers                  map[int]sqpack.Row
	inclusionShops               map[int]sqpack.Row
	inclusionSeries              map[int][]sqpack.SubRow
	customTalks                  map[int]sqpack.Row
	levels                       map[int]sqpack.Row
	rawCabinets                  map[int]sqpack.Row
	retainerTasks                map[int]sqpack.Row
	retainerTaskNormals          map[int]sqpack.Row
	retainerTaskParameters       map[int]sqpack.Row
	questClassJobRewards         *sqpack.SchemaSubrowSheet
	eventIconTypes               *sqpack.SchemaSheet
	paramGrows                   *sqpack.SchemaSheet
	beastRankBonuses             *sqpack.SchemaSheet
	instanceContents             *sqpack.SchemaSheet
	craftLeves                   *sqpack.SchemaSheet
	gatheringLeves               *sqpack.SchemaSheet
	gatheringLeveRoutes          *sqpack.SchemaSheet
	battleLeves                  *sqpack.SchemaSheet
	companyLeves                 *sqpack.SchemaSheet
	leveRewardItems              *sqpack.SchemaSheet
	leveRewardItemGroups         *sqpack.SchemaSheet
	collectablesShopItems        *sqpack.SchemaSubrowSheet
	collectablesShopRefines      *sqpack.SchemaSheet
	collectablesShopRewardItems  *sqpack.SchemaSheet
	collectablesShopRewardScrips *sqpack.SchemaSheet
	satisfactionNpcs             *sqpack.SchemaSheet
	satisfactionSupplies         *sqpack.SchemaSubrowSheet
	satisfactionRewards          *sqpack.SchemaSheet
	hwdSupplies                  *sqpack.SchemaSheet
	hwdRewards                   *sqpack.SchemaSheet
}

type rawPrimaryLangSheets struct {
	itemsPrimary               map[int]sqpack.Row
	itemsCn                    map[int]sqpack.Row
	itemsEn                    map[int]sqpack.Row
	itemsJa                    map[int]sqpack.Row
	rawCategories              map[int]sqpack.Row
	rawClassJobs               map[int]sqpack.Row
	rawClassJobCategories      map[int]sqpack.Row
	rawBaseParams              map[int]sqpack.Row
	rawCraftTypes              map[int]sqpack.Row
	rawSecretBooks             map[int]sqpack.Row
	rawGatheringTypes          map[int]sqpack.Row
	rawGatheringSubCategories  map[int]sqpack.Row
	rawPlaceNames              map[int]sqpack.Row
	fishingSpots               map[int]sqpack.Row
	fishParameters             map[int]sqpack.Row
	spearfishingItems          map[int]sqpack.Row
	gilShops                   map[int]sqpack.Row
	specialShops               map[int]sqpack.Row
	fccShops                   map[int]sqpack.Row
	enpcResidents              map[int]sqpack.Row
	topicSelects               map[int]sqpack.Row
	inclusionCategories        map[int]sqpack.Row
	achievementsPrimary        map[int]sqpack.Row
	achievementsCn             map[int]sqpack.Row
	achievementsEn             map[int]sqpack.Row
	achievementsJa             map[int]sqpack.Row
	rawAchievementCategories   map[int]sqpack.Row
	rawAchievementKinds        map[int]sqpack.Row
	titlesPrimary              map[int]sqpack.Row
	titlesEn                   map[int]sqpack.Row
	titlesJa                   map[int]sqpack.Row
	questsPrimary              *sqpack.SchemaSheet
	questsCn                   *sqpack.SchemaSheet
	questsEn                   *sqpack.SchemaSheet
	questsJa                   *sqpack.SchemaSheet
	journalGenres              *sqpack.SchemaSheet
	journalCategories          *sqpack.SchemaSheet
	journalSections            *sqpack.SchemaSheet
	questRewardOthers          *sqpack.SchemaSheet
	beastTribes                *sqpack.SchemaSheet
	beastReputationRanks       *sqpack.SchemaSheet
	grandCompanies             *sqpack.SchemaSheet
	exVersions                 *sqpack.SchemaSheet
	mounts                     *sqpack.SchemaSheet
	actions                    *sqpack.SchemaSheet
	emotes                     *sqpack.SchemaSheet
	generalActions             *sqpack.SchemaSheet
	contentFinderConditions    *sqpack.SchemaSheet
	eobjNames                  *sqpack.SchemaSheet
	levesPrimary               *sqpack.SchemaSheet
	levesCn                    *sqpack.SchemaSheet
	levesEn                    *sqpack.SchemaSheet
	levesJa                    *sqpack.SchemaSheet
	leveAssignmentTypes        *sqpack.SchemaSheet
	leveClients                *sqpack.SchemaSheet
	towns                      *sqpack.SchemaSheet
	leveStrings                *sqpack.SchemaSheet
	gatheringLeveRules         *sqpack.SchemaSheet
	battleLeveRules            *sqpack.SchemaSheet
	companyLeveRules           *sqpack.SchemaSheet
	eventItems                 *sqpack.SchemaSheet
	bnpcNames                  *sqpack.SchemaSheet
	collectablesShops          *sqpack.SchemaSheet
	collectablesShopItemGroups *sqpack.SchemaSheet
	hwdTerms                   *sqpack.SchemaSheet
}

// StoreLoadOptions 控制 GameDataStore 的加载选项，支持多语言切换时复用只读档案句柄与语言无关表。
type StoreLoadOptions struct {
	AppLang       i18n.Lang
	BaseStore     *Store
	SharedReaders bool
	OnProgress    func(LoadProgress)
}

// DataVersion 返回当前主数据语言对应客户端的版本号，用于版本快照目录与对比隔离。
func (s *Store) DataVersion() string {
	if s == nil {
		return ""
	}
	if s.PrimaryLang == sqpack.LangJapanese || s.PrimaryLang == sqpack.LangEnglish {
		if s.GlobalVersion != "" {
			return s.GlobalVersion
		}
	}
	if s.PrimaryVersion != "" {
		return s.PrimaryVersion
	}
	if s.CNVersion != "" {
		return s.CNVersion
	}
	return s.GlobalVersion
}

// Close 释放所有由当前 Store 独占打开的底层 SqPack 只读句柄。
func (s *Store) Close() error {
	if s == nil || s.sharedReaders {
		return nil
	}
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

// LoadStage 表示游戏数据读取与索引构建的真实阶段。
type LoadStage string

const (
	LoadStageSheets  LoadStage = "sheets"
	LoadStageIndices LoadStage = "indices"
)

// LoadProgress 描述加载过程中的实时进度（由真实分表计数与索引阶段驱动）。
type LoadProgress struct {
	Stage   LoadStage
	Done    int
	Total   int
	Message string
}

// LoadGameDataStore 并发读取已配置的本地 FF14 客户端数据（不含 schema 扩展表）。
func LoadGameDataStore(resolved *config.ResolvedPaths) (*Store, error) {
	return LoadGameDataStoreWithProgress(resolved, nil, nil)
}

// LoadGameDataStoreWithSchemas 并发读取已配置的本地 FF14 客户端数据，并在提供 schemasFS 时解析任务、理符与收藏品交易。
func LoadGameDataStoreWithSchemas(resolved *config.ResolvedPaths, schemasFS fs.FS) (*Store, error) {
	return LoadGameDataStoreWithProgress(resolved, schemasFS, nil)
}

// LoadGameDataStoreWithProgress 并发读取本地 FF14 客户端数据，并在各真实阶段回调进度（默认简体中文界面语言）。
func LoadGameDataStoreWithProgress(resolved *config.ResolvedPaths, schemasFS fs.FS, onProgress func(LoadProgress)) (*Store, error) {
	return LoadGameDataStoreForLang(resolved, schemasFS, i18n.LangZH, onProgress)
}

// LoadGameDataStoreForLang 按指定界面语言并发读取本地 FF14 客户端数据，并在各真实阶段回调进度。
func LoadGameDataStoreForLang(resolved *config.ResolvedPaths, schemasFS fs.FS, appLang i18n.Lang, onProgress func(LoadProgress)) (*Store, error) {
	return LoadGameDataStoreWithOptions(resolved, schemasFS, StoreLoadOptions{
		AppLang:    appLang,
		OnProgress: onProgress,
	})
}

// LoadGameDataStoreWithOptions 按给定选项加载客户端数据，支持在切换语言时复用只读档案句柄与语言无关表。
func LoadGameDataStoreWithOptions(resolved *config.ResolvedPaths, schemasFS fs.FS, opts StoreLoadOptions) (*Store, error) {
	if resolved == nil || (!resolved.HasCN() && !resolved.HasGlobal()) {
		return nil, config.ErrNoClientConfigured
	}

	appLang := i18n.NormalizeLang(string(opts.AppLang))
	onProgress := opts.OnProgress
	base := opts.BaseStore
	startTime := time.Now()

	var cnReader, globalReader *sqpack.GameReader
	var openedCN, openedGlobal bool
	var cnVer, globalVer, cnPath, globalPath string
	if resolved.HasCN() {
		cnVer = resolved.CN.Version
		cnPath = resolved.CN.RootPath
		if base != nil && base.CNReader != nil && base.CNPath == cnPath {
			cnReader = base.CNReader
		} else {
			cnReader = sqpack.NewGameReader(resolved.CN.GamePath)
			openedCN = true
		}
	}
	if resolved.HasGlobal() {
		globalVer = resolved.Global.Version
		globalPath = resolved.Global.RootPath
		if base != nil && base.GlobalReader != nil && base.GlobalPath == globalPath {
			globalReader = base.GlobalReader
		} else {
			globalReader = sqpack.NewGameReader(resolved.Global.GamePath)
			openedGlobal = true
		}
	}

	texCache := (*sqpack.TextureCache)(nil)
	if base != nil && base.TextureCache != nil {
		texCache = base.TextureCache
	} else {
		texCache = sqpack.NewTextureCache(1000)
	}

	// 根据界面语言选择主数据读取器与主语言，缺失对应客户端时回退至可用客户端
	var primaryReader *sqpack.GameReader
	var primaryLang sqpack.Language
	switch appLang {
	case i18n.LangJA:
		if globalReader != nil {
			primaryReader = globalReader
			primaryLang = sqpack.LangJapanese
		} else {
			primaryReader = cnReader
			primaryLang = sqpack.LangChineseSimplified
		}
	case i18n.LangEN:
		if globalReader != nil {
			primaryReader = globalReader
			primaryLang = sqpack.LangEnglish
		} else {
			primaryReader = cnReader
			primaryLang = sqpack.LangChineseSimplified
		}
	default:
		if cnReader != nil {
			primaryReader = cnReader
			primaryLang = sqpack.LangChineseSimplified
		} else {
			primaryReader = globalReader
			primaryLang = sqpack.LangEnglish
		}
	}

	reuseNone := base != nil && base.PrimaryReader == primaryReader && base.langNone != nil
	reusePrim := reuseNone && base.PrimaryLang == primaryLang && base.langPrimary != nil &&
		base.CNReader == cnReader && base.GlobalReader == globalReader

	var (
		itemsPrimary, itemsCn, itemsEn, itemsJa                                       map[int]sqpack.Row
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
		achievementsPrimary, achievementsCn, achievementsEn, achievementsJa           map[int]sqpack.Row
		rawAchievementCategories, rawAchievementKinds                                 map[int]sqpack.Row
		titlesPrimary, titlesEn, titlesJa                                             map[int]sqpack.Row
		retainerTasks, retainerTaskNormals, retainerTaskParameters                    map[int]sqpack.Row

		gatheringItemPointSubrows map[int][]sqpack.SubRow
		gilShopItems              map[int][]sqpack.SubRow
		gcScripShopItems          map[int][]sqpack.SubRow
		inclusionSeries           map[int][]sqpack.SubRow

		// Schema 对齐表（任务 / 理符 / 收藏品）
		questsPrimary, questsCn, questsEn, questsJa                *sqpack.SchemaSheet
		journalGenres, journalCategories, journalSections          *sqpack.SchemaSheet
		questClassJobRewards                                       *sqpack.SchemaSubrowSheet
		questRewardOthers, eventIconTypes, paramGrows              *sqpack.SchemaSheet
		beastTribes, beastRankBonuses, beastReputationRanks        *sqpack.SchemaSheet
		grandCompanies, exVersions, mounts, actions, emotes        *sqpack.SchemaSheet
		generalActions, instanceContents, contentFinderConditions  *sqpack.SchemaSheet
		eobjNames                                                  *sqpack.SchemaSheet
		levesPrimary, levesCn, levesEn, levesJa                    *sqpack.SchemaSheet
		craftLeves, gatheringLeves, gatheringLeveRoutes            *sqpack.SchemaSheet
		battleLeves, companyLeves                                  *sqpack.SchemaSheet
		leveRewardItems, leveRewardItemGroups, leveAssignmentTypes *sqpack.SchemaSheet
		leveClients, towns, leveStrings                            *sqpack.SchemaSheet
		gatheringLeveRules, battleLeveRules, companyLeveRules      *sqpack.SchemaSheet
		eventItems, bnpcNames                                      *sqpack.SchemaSheet
		collectablesShops, collectablesShopItemGroups              *sqpack.SchemaSheet
		collectablesShopItems                                      *sqpack.SchemaSubrowSheet
		collectablesShopRefines                                    *sqpack.SchemaSheet
		collectablesShopRewardItems, collectablesShopRewardScrips  *sqpack.SchemaSheet
		satisfactionNpcs, satisfactionRewards                      *sqpack.SchemaSheet
		satisfactionSupplies                                       *sqpack.SchemaSubrowSheet
		hwdSupplies, hwdRewards, hwdTerms                          *sqpack.SchemaSheet
	)

	var (
		errMu    sync.Mutex
		firstErr error
		tasks    []func()
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
		tasks = append(tasks, func() {
			rows, err := sqpack.LoadSheetRows(reader, sheet, lang, cols)
			if err != nil {
				setErr(sheet, err)
				return
			}
			*dst = rows
		})
	}

	loadSub := func(reader *sqpack.GameReader, sheet string, lang sqpack.Language, dst *map[int][]sqpack.SubRow) {
		if reader == nil {
			*dst = make(map[int][]sqpack.SubRow)
			return
		}
		tasks = append(tasks, func() {
			rows, err := sqpack.LoadSubrowSheet(reader, sheet, lang)
			if err != nil {
				setErr(sheet, err)
				return
			}
			*dst = rows
		})
	}

	loadSchema := func(reader *sqpack.GameReader, sheet string, lang sqpack.Language, fields []string, dst **sqpack.SchemaSheet) {
		if reader == nil || schemasFS == nil {
			return
		}
		tasks = append(tasks, func() {
			s, err := sqpack.LoadSheetWithSchema(reader, schemasFS, sheet, lang, fields)
			if err != nil {
				setErr(sheet, err)
				return
			}
			*dst = s
		})
	}

	loadSchemaSub := func(reader *sqpack.GameReader, sheet string, lang sqpack.Language, dst **sqpack.SchemaSubrowSheet) {
		if reader == nil || schemasFS == nil {
			return
		}
		tasks = append(tasks, func() {
			s, err := sqpack.LoadSubrowSheetWithSchema(reader, schemasFS, sheet, lang)
			if err != nil {
				setErr(sheet, err)
				return
			}
			*dst = s
		})
	}

	// 1. 语言无关 (LangNone) 原始表：同客户端下直接复用已加载快照，避免重复解压
	if reuseNone {
		bn := base.langNone
		rawEquipSlots = bn.rawEquipSlots
		rawRepairResources = bn.rawRepairResources
		itemActions = bn.itemActions
		itemFoods = bn.itemFoods
		recipes = bn.recipes
		recipeLevelTables = bn.recipeLevelTables
		gatheringItems = bn.gatheringItems
		gatheringItemPointSubrows = bn.gatheringItemPointSubrows
		gatheringPoints = bn.gatheringPoints
		gatheringPointBases = bn.gatheringPointBases
		exportedGatheringPoints = bn.exportedGatheringPoints
		gatheringPointTransients = bn.gatheringPointTransients
		gatheringRarePopTimeTables = bn.gatheringRarePopTimeTables
		rawGatheringItemLevels = bn.rawGatheringItemLevels
		territoryTypes = bn.territoryTypes
		maps = bn.maps
		spearfishingNotebooks = bn.spearfishingNotebooks
		gilShopItems = bn.gilShopItems
		gcScripShopItems = bn.gcScripShopItems
		tomestonesItems = bn.tomestonesItems
		enpcBases = bn.enpcBases
		preHandlers = bn.preHandlers
		inclusionShops = bn.inclusionShops
		inclusionSeries = bn.inclusionSeries
		customTalks = bn.customTalks
		levels = bn.levels
		rawCabinets = bn.rawCabinets
		retainerTasks = bn.retainerTasks
		retainerTaskNormals = bn.retainerTaskNormals
		retainerTaskParameters = bn.retainerTaskParameters
		questClassJobRewards = bn.questClassJobRewards
		eventIconTypes = bn.eventIconTypes
		paramGrows = bn.paramGrows
		beastRankBonuses = bn.beastRankBonuses
		instanceContents = bn.instanceContents
		craftLeves = bn.craftLeves
		gatheringLeves = bn.gatheringLeves
		gatheringLeveRoutes = bn.gatheringLeveRoutes
		battleLeves = bn.battleLeves
		companyLeves = bn.companyLeves
		leveRewardItems = bn.leveRewardItems
		leveRewardItemGroups = bn.leveRewardItemGroups
		collectablesShopItems = bn.collectablesShopItems
		collectablesShopRefines = bn.collectablesShopRefines
		collectablesShopRewardItems = bn.collectablesShopRewardItems
		collectablesShopRewardScrips = bn.collectablesShopRewardScrips
		satisfactionNpcs = bn.satisfactionNpcs
		satisfactionSupplies = bn.satisfactionSupplies
		satisfactionRewards = bn.satisfactionRewards
		hwdSupplies = bn.hwdSupplies
		hwdRewards = bn.hwdRewards
	} else {
		loadSheet(primaryReader, "EquipSlotCategory", sqpack.LangNone, nil, &rawEquipSlots)
		loadSheet(primaryReader, "ItemRepairResource", sqpack.LangNone, nil, &rawRepairResources)
		loadSheet(primaryReader, "ItemAction", sqpack.LangNone, nil, &itemActions)
		loadSheet(primaryReader, "ItemFood", sqpack.LangNone, nil, &itemFoods)
		loadSheet(primaryReader, "Recipe", sqpack.LangNone, nil, &recipes)
		loadSheet(primaryReader, "RecipeLevelTable", sqpack.LangNone, nil, &recipeLevelTables)
		loadSheet(primaryReader, "GatheringItem", sqpack.LangNone, nil, &gatheringItems)
		loadSub(primaryReader, "GatheringItemPoint", sqpack.LangNone, &gatheringItemPointSubrows)
		loadSheet(primaryReader, "GatheringPoint", sqpack.LangNone, nil, &gatheringPoints)
		loadSheet(primaryReader, "GatheringPointBase", sqpack.LangNone, nil, &gatheringPointBases)
		loadSheet(primaryReader, "ExportedGatheringPoint", sqpack.LangNone, nil, &exportedGatheringPoints)
		loadSheet(primaryReader, "GatheringPointTransient", sqpack.LangNone, nil, &gatheringPointTransients)
		loadSheet(primaryReader, "GatheringRarePopTimeTable", sqpack.LangNone, nil, &gatheringRarePopTimeTables)
		loadSheet(primaryReader, "GatheringItemLevelConvertTable", sqpack.LangNone, nil, &rawGatheringItemLevels)
		loadSheet(primaryReader, "TerritoryType", sqpack.LangNone, nil, &territoryTypes)
		loadSheet(primaryReader, "Map", sqpack.LangNone, nil, &maps)
		loadSheet(primaryReader, "SpearfishingNotebook", sqpack.LangNone, nil, &spearfishingNotebooks)
		loadSub(primaryReader, "GilShopItem", sqpack.LangNone, &gilShopItems)
		loadSub(primaryReader, "GCScripShopItem", sqpack.LangNone, &gcScripShopItems)
		loadSheet(primaryReader, "TomestonesItem", sqpack.LangNone, nil, &tomestonesItems)
		loadSheet(primaryReader, "ENpcBase", sqpack.LangNone, nil, &enpcBases)
		loadSheet(primaryReader, "PreHandler", sqpack.LangNone, nil, &preHandlers)
		loadSheet(primaryReader, "InclusionShop", sqpack.LangNone, nil, &inclusionShops)
		loadSub(primaryReader, "InclusionShopSeries", sqpack.LangNone, &inclusionSeries)
		loadSheet(primaryReader, "CustomTalk", sqpack.LangNone, nil, &customTalks)
		loadSheet(primaryReader, "Level", sqpack.LangNone, nil, &levels)
		loadSheet(primaryReader, "Cabinet", sqpack.LangNone, nil, &rawCabinets)
		loadSheet(primaryReader, "RetainerTask", sqpack.LangNone, nil, &retainerTasks)
		loadSheet(primaryReader, "RetainerTaskNormal", sqpack.LangNone, nil, &retainerTaskNormals)
		loadSheet(primaryReader, "RetainerTaskParameter", sqpack.LangNone, nil, &retainerTaskParameters)
		if schemasFS != nil {
			loadSchemaSub(primaryReader, "QuestClassJobReward", sqpack.LangNone, &questClassJobRewards)
			loadSchema(primaryReader, "EventIconType", sqpack.LangNone, []string{"NpcIconAvailable"}, &eventIconTypes)
			loadSchema(primaryReader, "ParamGrow", sqpack.LangNone, []string{"ExpToNext", "ScaledQuestXP"}, &paramGrows)
			loadSchema(primaryReader, "BeastRankBonus", sqpack.LangNone, nil, &beastRankBonuses)
			loadSchema(primaryReader, "InstanceContent", sqpack.LangNone, []string{"ContentFinderCondition"}, &instanceContents)
			loadSchema(primaryReader, "CraftLeve", sqpack.LangNone, nil, &craftLeves)
			loadSchema(primaryReader, "GatheringLeve", sqpack.LangNone, nil, &gatheringLeves)
			loadSchema(primaryReader, "GatheringLeveRoute", sqpack.LangNone, nil, &gatheringLeveRoutes)
			loadSchema(primaryReader, "BattleLeve", sqpack.LangNone, nil, &battleLeves)
			loadSchema(primaryReader, "CompanyLeve", sqpack.LangNone, nil, &companyLeves)
			loadSchema(primaryReader, "LeveRewardItem", sqpack.LangNone, nil, &leveRewardItems)
			loadSchema(primaryReader, "LeveRewardItemGroup", sqpack.LangNone, nil, &leveRewardItemGroups)
			loadSchemaSub(primaryReader, "CollectablesShopItem", sqpack.LangNone, &collectablesShopItems)
			loadSchema(primaryReader, "CollectablesShopRefine", sqpack.LangNone, nil, &collectablesShopRefines)
			loadSchema(primaryReader, "CollectablesShopRewardItem", sqpack.LangNone, nil, &collectablesShopRewardItems)
			loadSchema(primaryReader, "CollectablesShopRewardScrip", sqpack.LangNone, nil, &collectablesShopRewardScrips)
			loadSchema(primaryReader, "SatisfactionNpc", sqpack.LangNone, nil, &satisfactionNpcs)
			loadSchemaSub(primaryReader, "SatisfactionSupply", sqpack.LangNone, &satisfactionSupplies)
			loadSchema(primaryReader, "SatisfactionSupplyReward", sqpack.LangNone, nil, &satisfactionRewards)
			loadSchema(primaryReader, "HWDCrafterSupply", sqpack.LangNone, nil, &hwdSupplies)
			loadSchema(primaryReader, "HWDCrafterSupplyReward", sqpack.LangNone, nil, &hwdRewards)
		}
	}

	// 2. 与主语言相关的本地化表：仅在主客户端语言变化时读取
	if reusePrim {
		bp := base.langPrimary
		itemsPrimary = bp.itemsPrimary
		itemsCn = bp.itemsCn
		itemsEn = bp.itemsEn
		itemsJa = bp.itemsJa
		rawCategories = bp.rawCategories
		rawClassJobs = bp.rawClassJobs
		rawClassJobCategories = bp.rawClassJobCategories
		rawBaseParams = bp.rawBaseParams
		rawCraftTypes = bp.rawCraftTypes
		rawSecretBooks = bp.rawSecretBooks
		rawGatheringTypes = bp.rawGatheringTypes
		rawGatheringSubCategories = bp.rawGatheringSubCategories
		rawPlaceNames = bp.rawPlaceNames
		fishingSpots = bp.fishingSpots
		fishParameters = bp.fishParameters
		spearfishingItems = bp.spearfishingItems
		gilShops = bp.gilShops
		specialShops = bp.specialShops
		fccShops = bp.fccShops
		enpcResidents = bp.enpcResidents
		topicSelects = bp.topicSelects
		inclusionCategories = bp.inclusionCategories
		achievementsPrimary = bp.achievementsPrimary
		achievementsCn = bp.achievementsCn
		achievementsEn = bp.achievementsEn
		achievementsJa = bp.achievementsJa
		rawAchievementCategories = bp.rawAchievementCategories
		rawAchievementKinds = bp.rawAchievementKinds
		titlesPrimary = bp.titlesPrimary
		titlesEn = bp.titlesEn
		titlesJa = bp.titlesJa
		questsPrimary = bp.questsPrimary
		questsCn = bp.questsCn
		questsEn = bp.questsEn
		questsJa = bp.questsJa
		journalGenres = bp.journalGenres
		journalCategories = bp.journalCategories
		journalSections = bp.journalSections
		questRewardOthers = bp.questRewardOthers
		beastTribes = bp.beastTribes
		beastReputationRanks = bp.beastReputationRanks
		grandCompanies = bp.grandCompanies
		exVersions = bp.exVersions
		mounts = bp.mounts
		actions = bp.actions
		emotes = bp.emotes
		generalActions = bp.generalActions
		contentFinderConditions = bp.contentFinderConditions
		eobjNames = bp.eobjNames
		levesPrimary = bp.levesPrimary
		levesCn = bp.levesCn
		levesEn = bp.levesEn
		levesJa = bp.levesJa
		leveAssignmentTypes = bp.leveAssignmentTypes
		leveClients = bp.leveClients
		towns = bp.towns
		leveStrings = bp.leveStrings
		gatheringLeveRules = bp.gatheringLeveRules
		battleLeveRules = bp.battleLeveRules
		companyLeveRules = bp.companyLeveRules
		eventItems = bp.eventItems
		bnpcNames = bp.bnpcNames
		collectablesShops = bp.collectablesShops
		collectablesShopItemGroups = bp.collectablesShopItemGroups
		hwdTerms = bp.hwdTerms
	} else {
		loadSheet(primaryReader, "Item", primaryLang, nil, &itemsPrimary)
		if cnReader != nil && primaryLang != sqpack.LangChineseSimplified {
			loadSheet(cnReader, "Item", sqpack.LangChineseSimplified, []int{8, 9}, &itemsCn)
		}
		if globalReader != nil {
			if primaryLang != sqpack.LangEnglish {
				loadSheet(globalReader, "Item", sqpack.LangEnglish, []int{8, 9}, &itemsEn)
			}
			if primaryLang != sqpack.LangJapanese {
				loadSheet(globalReader, "Item", sqpack.LangJapanese, []int{8, 9}, &itemsJa)
			}
		} else {
			itemsEn = make(map[int]sqpack.Row)
			itemsJa = make(map[int]sqpack.Row)
		}

		loadSheet(primaryReader, "ItemUICategory", primaryLang, nil, &rawCategories)
		loadSheet(primaryReader, "ClassJob", primaryLang, nil, &rawClassJobs)
		loadSheet(primaryReader, "ClassJobCategory", primaryLang, nil, &rawClassJobCategories)
		loadSheet(primaryReader, "BaseParam", primaryLang, nil, &rawBaseParams)
		loadSheet(primaryReader, "CraftType", primaryLang, nil, &rawCraftTypes)
		loadSheet(primaryReader, "SecretRecipeBook", primaryLang, nil, &rawSecretBooks)
		loadSheet(primaryReader, "GatheringType", primaryLang, nil, &rawGatheringTypes)
		loadSheet(primaryReader, "GatheringSubCategory", primaryLang, nil, &rawGatheringSubCategories)
		loadSheet(primaryReader, "PlaceName", primaryLang, nil, &rawPlaceNames)
		loadSheet(primaryReader, "FishingSpot", primaryLang, nil, &fishingSpots)
		loadSheet(primaryReader, "FishParameter", primaryLang, nil, &fishParameters)
		loadSheet(primaryReader, "SpearfishingItem", primaryLang, nil, &spearfishingItems)
		loadSheet(primaryReader, "GilShop", primaryLang, nil, &gilShops)
		loadSheet(primaryReader, "SpecialShop", primaryLang, nil, &specialShops)
		loadSheet(primaryReader, "FccShop", primaryLang, nil, &fccShops)
		loadSheet(primaryReader, "ENpcResident", primaryLang, nil, &enpcResidents)
		loadSheet(primaryReader, "TopicSelect", primaryLang, nil, &topicSelects)
		loadSheet(primaryReader, "InclusionShopCategory", primaryLang, nil, &inclusionCategories)

		loadSheet(primaryReader, "Achievement", primaryLang, nil, &achievementsPrimary)
		if cnReader != nil && primaryLang != sqpack.LangChineseSimplified {
			loadSheet(cnReader, "Achievement", sqpack.LangChineseSimplified, []int{1, 2}, &achievementsCn)
		}
		if globalReader != nil {
			if primaryLang != sqpack.LangEnglish {
				loadSheet(globalReader, "Achievement", sqpack.LangEnglish, []int{1, 2}, &achievementsEn)
			}
			if primaryLang != sqpack.LangJapanese {
				loadSheet(globalReader, "Achievement", sqpack.LangJapanese, []int{1, 2}, &achievementsJa)
			}
		} else {
			achievementsEn = make(map[int]sqpack.Row)
			achievementsJa = make(map[int]sqpack.Row)
		}
		loadSheet(primaryReader, "AchievementCategory", primaryLang, nil, &rawAchievementCategories)
		loadSheet(primaryReader, "AchievementKind", primaryLang, nil, &rawAchievementKinds)
		loadSheet(primaryReader, "Title", primaryLang, nil, &titlesPrimary)
		if globalReader != nil {
			if primaryLang != sqpack.LangEnglish {
				loadSheet(globalReader, "Title", sqpack.LangEnglish, []int{0, 1}, &titlesEn)
			}
			if primaryLang != sqpack.LangJapanese {
				loadSheet(globalReader, "Title", sqpack.LangJapanese, []int{0, 1}, &titlesJa)
			}
		} else {
			titlesEn = make(map[int]sqpack.Row)
			titlesJa = make(map[int]sqpack.Row)
		}

		if schemasFS != nil {
			loadSchema(primaryReader, "Quest", primaryLang, questNeededFields, &questsPrimary)
			if cnReader != nil && primaryLang != sqpack.LangChineseSimplified {
				loadSchema(cnReader, "Quest", sqpack.LangChineseSimplified, []string{"Name"}, &questsCn)
				loadSchema(cnReader, "Leve", sqpack.LangChineseSimplified, []string{"Name"}, &levesCn)
			}
			if globalReader != nil {
				if primaryLang != sqpack.LangEnglish {
					loadSchema(globalReader, "Quest", sqpack.LangEnglish, []string{"Name"}, &questsEn)
					loadSchema(globalReader, "Leve", sqpack.LangEnglish, []string{"Name"}, &levesEn)
				}
				if primaryLang != sqpack.LangJapanese {
					loadSchema(globalReader, "Quest", sqpack.LangJapanese, []string{"Name"}, &questsJa)
					loadSchema(globalReader, "Leve", sqpack.LangJapanese, []string{"Name"}, &levesJa)
				}
			}
			loadSchema(primaryReader, "JournalGenre", primaryLang, []string{"Name", "Icon", "JournalCategory"}, &journalGenres)
			loadSchema(primaryReader, "JournalCategory", primaryLang, []string{"Name", "JournalSection"}, &journalCategories)
			loadSchema(primaryReader, "JournalSection", primaryLang, []string{"Name"}, &journalSections)
			loadSchema(primaryReader, "QuestRewardOther", primaryLang, []string{"Name", "Icon"}, &questRewardOthers)
			loadSchema(primaryReader, "BeastTribe", primaryLang, []string{"Name", "CurrencyItem", "BeastRankBonus"}, &beastTribes)
			loadSchema(primaryReader, "BeastReputationRank", primaryLang, []string{"Name"}, &beastReputationRanks)
			loadSchema(primaryReader, "GrandCompany", primaryLang, []string{"Singular"}, &grandCompanies)
			loadSchema(primaryReader, "ExVersion", primaryLang, []string{"Name"}, &exVersions)
			loadSchema(primaryReader, "Mount", primaryLang, []string{"Singular", "Icon"}, &mounts)
			loadSchema(primaryReader, "Action", primaryLang, []string{"Name", "Icon"}, &actions)
			loadSchema(primaryReader, "Emote", primaryLang, []string{"Name", "Icon"}, &emotes)
			loadSchema(primaryReader, "GeneralAction", primaryLang, []string{"Name", "Icon"}, &generalActions)
			loadSchema(primaryReader, "ContentFinderCondition", primaryLang, []string{"Name"}, &contentFinderConditions)
			loadSchema(primaryReader, "EObjName", primaryLang, []string{"Singular"}, &eobjNames)

			loadSchema(primaryReader, "Leve", primaryLang, nil, &levesPrimary)
			loadSchema(primaryReader, "LeveAssignmentType", primaryLang, []string{"Name", "Icon"}, &leveAssignmentTypes)
			loadSchema(primaryReader, "LeveClient", primaryLang, []string{"Name"}, &leveClients)
			loadSchema(primaryReader, "Town", primaryLang, []string{"Name", "Icon"}, &towns)
			loadSchema(primaryReader, "LeveString", primaryLang, []string{"Objective"}, &leveStrings)
			loadSchema(primaryReader, "GatheringLeveRule", primaryLang, []string{"Rule"}, &gatheringLeveRules)
			loadSchema(primaryReader, "BattleLeveRule", primaryLang, []string{"Rule"}, &battleLeveRules)
			loadSchema(primaryReader, "CompanyLeveRule", primaryLang, nil, &companyLeveRules)
			loadSchema(primaryReader, "EventItem", primaryLang, []string{"Singular", "Name", "Icon"}, &eventItems)
			loadSchema(primaryReader, "BNpcName", primaryLang, []string{"Singular"}, &bnpcNames)

			loadSchema(primaryReader, "CollectablesShop", primaryLang, nil, &collectablesShops)
			loadSchema(primaryReader, "CollectablesShopItemGroup", primaryLang, []string{"Name"}, &collectablesShopItemGroups)
			loadSchema(primaryReader, "HWDCrafterSupplyTerm", primaryLang, []string{"Name"}, &hwdTerms)
		}
	}

	totalSheets := len(tasks)
	var completedSheets atomic.Int32
	var progressMu sync.Mutex
	var lastEmit time.Time
	emit := func(p LoadProgress, force bool) {
		if onProgress == nil {
			return
		}
		progressMu.Lock()
		now := time.Now()
		if !force && now.Sub(lastEmit) < 60*time.Millisecond {
			progressMu.Unlock()
			return
		}
		lastEmit = now
		progressMu.Unlock()
		onProgress(p)
	}

	sheetsMsg := i18n.T(appLang, "loading.msg_sheets")
	if totalSheets > 0 {
		emit(LoadProgress{
			Stage:   LoadStageSheets,
			Done:    0,
			Total:   totalSheets,
			Message: sheetsMsg,
		}, true)

		// 限制并发工作协程数量，避免切换语言后台读取时抢占主 UI 线程的 CPU 时间片
		workers := min(totalSheets, max(4, runtime.NumCPU()))
		jobCh := make(chan func(), totalSheets)
		for _, fn := range tasks {
			jobCh <- fn
		}
		close(jobCh)

		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				for run := range jobCh {
					run()
					done := int(completedSheets.Add(1))
					emit(LoadProgress{
						Stage:   LoadStageSheets,
						Done:    done,
						Total:   totalSheets,
						Message: sheetsMsg,
					}, done == totalSheets)
				}
			}()
		}
		wg.Wait()
	}
	if firstErr != nil {
		if !opts.SharedReaders {
			if openedCN && cnReader != nil {
				_ = cnReader.Close()
			}
			if openedGlobal && globalReader != nil {
				_ = globalReader.Close()
			}
		}
		return nil, firstErr
	}

	emit(LoadProgress{
		Stage:   LoadStageIndices,
		Done:    0,
		Total:   4,
		Message: i18n.T(appLang, "loading.msg_recipes"),
	}, true)

	// 若主语言本身为英语或日语，直接复用主表作为对应语言副表，避免重复读取
	switch primaryLang {
	case sqpack.LangEnglish:
		itemsEn = itemsPrimary
		achievementsEn = achievementsPrimary
		titlesEn = titlesPrimary
		questsEn = questsPrimary
		levesEn = levesPrimary
	case sqpack.LangJapanese:
		itemsJa = itemsPrimary
		achievementsJa = achievementsPrimary
		titlesJa = titlesPrimary
		questsJa = questsPrimary
		levesJa = levesPrimary
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
		equipSlots[id] = formatEquipSlotCategoryForLang(row, appLang)
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

	emit(LoadProgress{
		Stage:   LoadStageIndices,
		Done:    1,
		Total:   4,
		Message: i18n.T(appLang, "loading.msg_shops"),
	}, true)

	// 构建商店与雇员探险索引
	shopIdx := buildShopIndices(shopLoaderInput{
		appLang:             appLang,
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

	retainerIdx := buildRetainerIndices(appLang, retainerTasks, retainerTaskNormals, retainerTaskParameters, classJobCategories)

	tomestoneByIndex := make(map[int]int)
	for _, row := range tomestonesItems {
		itemID := row.IntAt(0)
		idx := row.IntAt(2)
		if itemID > 0 && idx > 0 {
			tomestoneByIndex[idx] = itemID
		}
	}

	itemMeta := func(itemID int) (string, int) {
		if row, ok := itemsPrimary[itemID]; ok {
			return row.StringAt(9), row.IntAt(10)
		}
		return "", 0
	}

	emit(LoadProgress{
		Stage:   LoadStageIndices,
		Done:    2,
		Total:   4,
		Message: i18n.T(appLang, "loading.msg_relations"),
	}, true)

	collectablesIdx := buildCollectablesData(collectablesLoaderInput{
		appLang:                appLang,
		shops:                  collectablesShops,
		shopItems:              collectablesShopItems,
		shopItemGroups:         collectablesShopItemGroups,
		shopRefines:            collectablesShopRefines,
		shopRewardItems:        collectablesShopRewardItems,
		shopRewardScrips:       collectablesShopRewardScrips,
		satisfactionNpcs:       satisfactionNpcs,
		satisfactionSupplies:   satisfactionSupplies,
		satisfactionRewards:    satisfactionRewards,
		hwdSupplies:            hwdSupplies,
		hwdRewards:             hwdRewards,
		hwdTerms:               hwdTerms,
		quests:                 questsPrimary,
		collectablesShopToNPCs: shopIdx.CollectablesShopToNPCs,
		npcNames:               shopIdx.NpcNames,
		npcLocations:           shopIdx.NpcLocations,
		levelLocations:         shopIdx.LevelLocations,
		tomestoneByIndex:       tomestoneByIndex,
		placeNames:             placeNames,
		territoryTypes:         territoryTypes,
		maps:                   maps,
		itemMeta:               itemMeta,
	})

	questIdx := buildQuestData(questLoaderInput{
		appLang:                 appLang,
		quests:                  questsPrimary,
		questsCn:                questsCn,
		questsEn:                questsEn,
		questsJa:                questsJa,
		journalGenres:           journalGenres,
		journalCategories:       journalCategories,
		journalSections:         journalSections,
		questClassJobRewards:    questClassJobRewards,
		questRewardOthers:       questRewardOthers,
		eventIconTypes:          eventIconTypes,
		paramGrows:              paramGrows,
		beastTribes:             beastTribes,
		beastRankBonuses:        beastRankBonuses,
		beastReputationRanks:    beastReputationRanks,
		grandCompanies:          grandCompanies,
		exVersions:              exVersions,
		mounts:                  mounts,
		actions:                 actions,
		emotes:                  emotes,
		generalActions:          generalActions,
		instanceContents:        instanceContents,
		contentFinderConditions: contentFinderConditions,
		eobjNames:               eobjNames,
		npcNames:                shopIdx.NpcNames,
		npcLocations:            shopIdx.NpcLocations,
		levelLocations:          shopIdx.LevelLocations,
		tomestoneByIndex:        tomestoneByIndex,
		classJobs:               classJobs,
		classJobCategories:      classJobCategories,
		placeNames:              placeNames,
		territoryTypes:          territoryTypes,
		maps:                    maps,
		itemMeta:                itemMeta,
	})

	cabinetIDs := make(map[int]struct{}, len(rawCabinets))
	for _, row := range rawCabinets {
		if id := row.IntAt(0); id > 0 {
			cabinetIDs[id] = struct{}{}
		}
	}

	eventItemNames := make(map[int]string)
	if eventItems != nil {
		for id, row := range eventItems.Rows {
			if id <= 0 {
				continue
			}
			name := strings.TrimSpace(eventItems.Str(row, "Singular"))
			if name == "" {
				name = strings.TrimSpace(eventItems.Str(row, "Name"))
			}
			if name != "" {
				eventItemNames[id] = name
			}
		}
	}

	bnpcNameMap := make(map[int]string)
	if bnpcNames != nil {
		for id, row := range bnpcNames.Rows {
			if id <= 0 {
				continue
			}
			if name := strings.TrimSpace(bnpcNames.Str(row, "Singular")); name != "" {
				bnpcNameMap[id] = name
			}
		}
	}

	eobjNameMap := make(map[int]string)
	if eobjNames != nil {
		for id, row := range eobjNames.Rows {
			if id <= 0 {
				continue
			}
			if name := strings.TrimSpace(eobjNames.Str(row, "Singular")); name != "" {
				eobjNameMap[id] = name
			}
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
		zhName := ""
		if cnRow, ok := itemsCn[itemID]; ok {
			zhName = cnRow.StringAt(9)
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
		catName := i18n.T(appLang, "common.other")
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
			ID:                  itemID,
			NameCn:              namePrimary,
			NameZh:              zhName,
			NameEn:              enName,
			NameJa:              jaName,
			IconID:              row.IntAt(10),
			ItemLevel:           row.IntAt(11),
			EquipLevel:          row.IntAt(40),
			Rarity:              row.IntAt(12),
			CategoryID:          catID,
			CategoryName:        catName,
			CategoryIconID:      catIconID,
			PriceMid:            row.IntAt(25),
			CanBeHQ:             row.BoolAt(27),
			IsTradable:          isTradable,
			IsMarketable:        isMarketable,
			IsCraftable:         len(recipesByResult[itemID]) > 0,
			IsGatherable:        isGatherable,
			IsPurchasable:       isPurchasable,
			IsGilShop:           isGilShop,
			IsSpecialShop:       isSpecialShop,
			IsRetainerVenture:   len(retainerIdx.VenturesByItem[itemID]) > 0,
			IsCollectableTurnIn: len(collectablesIdx.turnInsByItem[itemID]) > 0,
			IsCollectableReward: len(collectablesIdx.rewardsByItem[itemID]) > 0,
			HasEquipmentStats:   HasItemEquipmentStats(row),
			UsedInRecipesCount:  len(recipesByIngredient[itemID]),
		}
		itemSummaries = append(itemSummaries, summary)
		itemSummaryByID[itemID] = summary
		if catID > 0 {
			categoryCount[catID]++
		}
	}
	sort.Slice(itemSummaries, func(i, j int) bool { return itemSummaries[i].ID < itemSummaries[j].ID })

	leveIdx := buildLeveData(leveLoaderInput{
		appLang:               appLang,
		leves:                 levesPrimary,
		levesCn:               levesCn,
		levesEn:               levesEn,
		levesJa:               levesJa,
		craftLeves:            craftLeves,
		gatheringLeves:        gatheringLeves,
		gatheringLeveRoutes:   gatheringLeveRoutes,
		battleLeves:           battleLeves,
		companyLeves:          companyLeves,
		leveRewardItems:       leveRewardItems,
		leveRewardItemGroups:  leveRewardItemGroups,
		leveAssignmentTypes:   leveAssignmentTypes,
		leveClients:           leveClients,
		towns:                 towns,
		leveStrings:           leveStrings,
		gatheringLeveRules:    gatheringLeveRules,
		battleLeveRules:       battleLeveRules,
		companyLeveRules:      companyLeveRules,
		eventItems:            eventItems,
		bnpcNames:             bnpcNames,
		journalGenres:         journalGenres,
		fishingSpots:          fishingSpots,
		spearfishingNotebooks: spearfishingNotebooks,
		levelLocations:        shopIdx.LevelLocations,
		classJobCategories:    classJobCategories,
		placeNames:            placeNames,
		territoryTypes:        territoryTypes,
		maps:                  maps,
		itemSummaryByID:       itemSummaryByID,
	})

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

	emit(LoadProgress{
		Stage:   LoadStageIndices,
		Done:    3,
		Total:   4,
		Message: i18n.T(appLang, "loading.msg_achievements"),
	}, true)

	// 解析成就分类、大类与称号表
	type achKindDict struct {
		name  string
		order int
	}
	achKinds := make(map[int]achKindDict)
	for id, row := range rawAchievementKinds {
		if name := row.StringAt(0); name != "" {
			achKinds[id] = achKindDict{name: name, order: row.IntAt(1)}
		}
	}
	type achCatDict struct {
		name   string
		kindID int
		order  int
	}
	achCats := make(map[int]achCatDict)
	for id, row := range rawAchievementCategories {
		if name := row.StringAt(0); name != "" {
			// AchievementCategory 物理列序：0=Name, 1=AchievementKind, 4=Order
			achCats[id] = achCatDict{name: name, kindID: row.IntAt(1), order: row.IntAt(4)}
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
	catCount := make(map[int]int)

	for achID, row := range achievementsPrimary {
		if achID <= 0 {
			continue
		}
		namePrimary := row.StringAt(1)
		if namePrimary == "" {
			continue
		}
		cnRow := achievementsCn[achID]
		enRow := achievementsEn[achID]
		jaRow := achievementsJa[achID]
		catID := row.IntAt(0)
		catInfo := achCats[catID]
		kindID := catInfo.kindID
		kindName := achKinds[kindID].name
		if kindName == "" {
			kindName = i18n.T(appLang, "common.other")
		}
		catName := catInfo.name
		if catName == "" {
			catName = i18n.T(appLang, "common.general")
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
			NameZh:           cnRow.StringAt(1),
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
		if catID > 0 {
			catCount[catID]++
		}
		if rewardItemID > 0 {
			achievementsByRewardItem[rewardItemID] = append(achievementsByRewardItem[rewardItemID], entry)
		}
	}
	sort.Slice(achievements, func(i, j int) bool { return achievements[i].ID < achievements[j].ID })

	// 按大类归集并依游戏内 Order 排序二级分类
	catsByKind := make(map[int][]AchievementCategoryInfo)
	for id, cat := range achCats {
		cnt := catCount[id]
		if cnt <= 0 || id <= 0 {
			continue
		}
		kName := achKinds[cat.kindID].name
		if kName == "" {
			kName = i18n.T(appLang, "common.other")
		}
		catsByKind[cat.kindID] = append(catsByKind[cat.kindID], AchievementCategoryInfo{
			ID:       id,
			KindID:   cat.kindID,
			KindName: kName,
			Name:     cat.name,
			Count:    cnt,
		})
	}
	for kID, list := range catsByKind {
		sort.Slice(list, func(i, j int) bool {
			oi, oj := achCats[list[i].ID].order, achCats[list[j].ID].order
			if oi != oj {
				return oi < oj
			}
			return list[i].ID < list[j].ID
		})
		catsByKind[kID] = list
	}

	var achievementKindList []AchievementKindInfo
	for id, k := range achKinds {
		if cnt := kindCount[id]; cnt > 0 {
			achievementKindList = append(achievementKindList, AchievementKindInfo{
				ID:         id,
				Name:       k.name,
				Count:      cnt,
				Categories: catsByKind[id],
			})
		}
	}
	sort.Slice(achievementKindList, func(i, j int) bool {
		oi, oj := achKinds[achievementKindList[i].ID].order, achKinds[achievementKindList[j].ID].order
		if oi != oj {
			return oi < oj
		}
		return achievementKindList[i].ID < achievementKindList[j].ID
	})

	var achievementCategoryList []AchievementCategoryInfo
	for _, k := range achievementKindList {
		achievementCategoryList = append(achievementCategoryList, k.Categories...)
	}
	// 兜底纳入未归属有效大类的二级分类
	if orphanCats := catsByKind[0]; len(orphanCats) > 0 {
		achievementCategoryList = append(achievementCategoryList, orphanCats...)
	}

	var savedNone *rawLangNoneSheets
	if reuseNone {
		savedNone = base.langNone
	} else {
		savedNone = &rawLangNoneSheets{
			rawEquipSlots:                rawEquipSlots,
			rawRepairResources:           rawRepairResources,
			itemActions:                  itemActions,
			itemFoods:                    itemFoods,
			recipes:                      recipes,
			recipeLevelTables:            recipeLevelTables,
			gatheringItems:               gatheringItems,
			gatheringItemPointSubrows:    gatheringItemPointSubrows,
			gatheringPoints:              gatheringPoints,
			gatheringPointBases:          gatheringPointBases,
			exportedGatheringPoints:      exportedGatheringPoints,
			gatheringPointTransients:     gatheringPointTransients,
			gatheringRarePopTimeTables:   gatheringRarePopTimeTables,
			rawGatheringItemLevels:       rawGatheringItemLevels,
			territoryTypes:               territoryTypes,
			maps:                         maps,
			spearfishingNotebooks:        spearfishingNotebooks,
			gilShopItems:                 gilShopItems,
			gcScripShopItems:             gcScripShopItems,
			tomestonesItems:              tomestonesItems,
			enpcBases:                    enpcBases,
			preHandlers:                  preHandlers,
			inclusionShops:               inclusionShops,
			inclusionSeries:              inclusionSeries,
			customTalks:                  customTalks,
			levels:                       levels,
			rawCabinets:                  rawCabinets,
			retainerTasks:                retainerTasks,
			retainerTaskNormals:          retainerTaskNormals,
			retainerTaskParameters:       retainerTaskParameters,
			questClassJobRewards:         questClassJobRewards,
			eventIconTypes:               eventIconTypes,
			paramGrows:                   paramGrows,
			beastRankBonuses:             beastRankBonuses,
			instanceContents:             instanceContents,
			craftLeves:                   craftLeves,
			gatheringLeves:               gatheringLeves,
			gatheringLeveRoutes:          gatheringLeveRoutes,
			battleLeves:                  battleLeves,
			companyLeves:                 companyLeves,
			leveRewardItems:              leveRewardItems,
			leveRewardItemGroups:         leveRewardItemGroups,
			collectablesShopItems:        collectablesShopItems,
			collectablesShopRefines:      collectablesShopRefines,
			collectablesShopRewardItems:  collectablesShopRewardItems,
			collectablesShopRewardScrips: collectablesShopRewardScrips,
			satisfactionNpcs:             satisfactionNpcs,
			satisfactionSupplies:         satisfactionSupplies,
			satisfactionRewards:          satisfactionRewards,
			hwdSupplies:                  hwdSupplies,
			hwdRewards:                   hwdRewards,
		}
	}

	var savedPrim *rawPrimaryLangSheets
	if reusePrim {
		savedPrim = base.langPrimary
	} else {
		savedPrim = &rawPrimaryLangSheets{
			itemsPrimary:               itemsPrimary,
			itemsCn:                    itemsCn,
			itemsEn:                    itemsEn,
			itemsJa:                    itemsJa,
			rawCategories:              rawCategories,
			rawClassJobs:               rawClassJobs,
			rawClassJobCategories:      rawClassJobCategories,
			rawBaseParams:              rawBaseParams,
			rawCraftTypes:              rawCraftTypes,
			rawSecretBooks:             rawSecretBooks,
			rawGatheringTypes:          rawGatheringTypes,
			rawGatheringSubCategories:  rawGatheringSubCategories,
			rawPlaceNames:              rawPlaceNames,
			fishingSpots:               fishingSpots,
			fishParameters:             fishParameters,
			spearfishingItems:          spearfishingItems,
			gilShops:                   gilShops,
			specialShops:               specialShops,
			fccShops:                   fccShops,
			enpcResidents:              enpcResidents,
			topicSelects:               topicSelects,
			inclusionCategories:        inclusionCategories,
			achievementsPrimary:        achievementsPrimary,
			achievementsCn:             achievementsCn,
			achievementsEn:             achievementsEn,
			achievementsJa:             achievementsJa,
			rawAchievementCategories:   rawAchievementCategories,
			rawAchievementKinds:        rawAchievementKinds,
			titlesPrimary:              titlesPrimary,
			titlesEn:                   titlesEn,
			titlesJa:                   titlesJa,
			questsPrimary:              questsPrimary,
			questsCn:                   questsCn,
			questsEn:                   questsEn,
			questsJa:                   questsJa,
			journalGenres:              journalGenres,
			journalCategories:          journalCategories,
			journalSections:            journalSections,
			questRewardOthers:          questRewardOthers,
			beastTribes:                beastTribes,
			beastReputationRanks:       beastReputationRanks,
			grandCompanies:             grandCompanies,
			exVersions:                 exVersions,
			mounts:                     mounts,
			actions:                    actions,
			emotes:                     emotes,
			generalActions:             generalActions,
			contentFinderConditions:    contentFinderConditions,
			eobjNames:                  eobjNames,
			levesPrimary:               levesPrimary,
			levesCn:                    levesCn,
			levesEn:                    levesEn,
			levesJa:                    levesJa,
			leveAssignmentTypes:        leveAssignmentTypes,
			leveClients:                leveClients,
			towns:                      towns,
			leveStrings:                leveStrings,
			gatheringLeveRules:         gatheringLeveRules,
			battleLeveRules:            battleLeveRules,
			companyLeveRules:           companyLeveRules,
			eventItems:                 eventItems,
			bnpcNames:                  bnpcNames,
			collectablesShops:          collectablesShops,
			collectablesShopItemGroups: collectablesShopItemGroups,
			hwdTerms:                   hwdTerms,
		}
	}

	return &Store{
		CNReader:                       cnReader,
		GlobalReader:                   globalReader,
		PrimaryReader:                  primaryReader,
		TextureCache:                   texCache,
		HasCN:                          resolved.HasCN(),
		HasGlobal:                      resolved.HasGlobal(),
		CNVersion:                      cnVer,
		GlobalVersion:                  globalVer,
		PrimaryVersion:                 resolved.VersionForClient(primaryReader == globalReader),
		CNPath:                         cnPath,
		GlobalPath:                     globalPath,
		LoadTimeMs:                     time.Since(startTime).Milliseconds(),
		AppLang:                        appLang,
		PrimaryLang:                    primaryLang,
		sharedReaders:                  opts.SharedReaders,
		langNone:                       savedNone,
		langPrimary:                    savedPrim,
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
		LevelLocations:                 shopIdx.LevelLocations,
		TotalActiveShops:               shopIdx.TotalActiveShops,
		RetainerVenturesByItem:         retainerIdx.VenturesByItem,
		TotalRetainerTasks:             retainerIdx.TotalRetainerTasks,
		Quests:                         questIdx.quests,
		QuestByID:                      questIdx.questByID,
		QuestsByRewardItem:             questIdx.questsByRewardItem,
		QuestSectionList:               questIdx.sectionList,
		QuestActorIDs:                  questIdx.questActorIDs,
		EventItemNames:                 eventItemNames,
		BNpcNames:                      bnpcNameMap,
		EObjNames:                      eobjNameMap,
		Leves:                          leveIdx.leves,
		LeveByID:                       leveIdx.leveByID,
		LevesByItem:                    leveIdx.levesByItem,
		LeveAssignmentTypes:            leveIdx.assignmentTypeList,
		CollectableTurnInsByItem:       collectablesIdx.turnInsByItem,
		CollectableRewardsByItem:       collectablesIdx.rewardsByItem,
		TotalCollectables:              collectablesIdx.totalEntries,
		ItemSummaries:                  itemSummaries,
		ItemSummaryByID:                itemSummaryByID,
		CategoryList:                   categoryList,
		CraftTypeList:                  craftTypeList,
		Achievements:                   achievements,
		AchievementByID:                achievementByID,
		AchievementsByRewardItem:       achievementsByRewardItem,
		AchievementKindList:            achievementKindList,
		AchievementCategoryList:        achievementCategoryList,
		TotalTitles:                    totalTitles,
	}, nil
}
