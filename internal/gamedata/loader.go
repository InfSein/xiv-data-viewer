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
	EXDReader     sqpack.FileReader
	TextureCache  *sqpack.TextureCache
	SchemasFS     fs.FS

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
	QuestCutVars             map[int]map[string]int
	QuestBGMVars             map[int]map[string]int
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

	// 音乐（游戏 BGM 与管弦乐琴）索引
	Music              []MusicEntry
	BGMs               []MusicEntry
	Orchestrions       []MusicEntry
	MusicByKey         map[string]MusicEntry
	BGMByID            map[int]MusicEntry
	OrchByID           map[int]MusicEntry
	MusicCategoryList  []MusicCategoryInfo
	MusicExpansionList []MusicExpansionInfo
	EventSystemBGMs    map[string]int
	BGMByFile          map[string]MusicEntry
	OrchByFile         map[string]MusicEntry

	bgmDigestMu sync.Mutex
	bgmDigests  map[int]BGMDigest

	// 图标资源索引（ui/icon）与按客户端缓存的原始扫描结果（支持启动后后台延迟构建，并发安全读写）
	iconMu                sync.RWMutex
	iconLoadMu            sync.Mutex
	iconCacheDir          string
	iconsReady            bool
	iconsLoading          bool
	iconsErr              error
	Icons                 []IconEntry
	IconByID              map[int]IconEntry
	IconGroups            []IconGroupInfo
	IconsByGroup          map[int][]IconEntry
	AvailableIconVariants []string
	IconUsagesByID        map[int][]IconUsageRef
	cnIconFiles           []sqpack.IconFileEntry
	globalIconFiles       []sqpack.IconFileEntry

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
	bgms                         *sqpack.SchemaSheet
	bgmSituations                *sqpack.SchemaSheet
	bgmSwitches                  *sqpack.SchemaSheet
	bgmSystemDefines             *sqpack.SchemaSheet
	orchPaths                    *sqpack.SchemaSheet
	orchUiparams                 *sqpack.SchemaSheet
	gfates                       *sqpack.SchemaSheet
	gfateTypes                   *sqpack.SchemaSheet
	cutscenes                    *sqpack.SchemaSheet
	eventSystemDefines           *sqpack.SchemaSheet
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
	completeJournals           *sqpack.SchemaSheet
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
	weddingBGMs                *sqpack.SchemaSheet
	weddingBGMsEn              *sqpack.SchemaSheet
	weddingBGMsJa              *sqpack.SchemaSheet
	orchPrimary                *sqpack.SchemaSheet
	orchCn                     *sqpack.SchemaSheet
	orchEn                     *sqpack.SchemaSheet
	orchJa                     *sqpack.SchemaSheet
	orchCategories             *sqpack.SchemaSheet
	fates                      *sqpack.SchemaSheet
	goldSaucerTexts            *sqpack.SchemaSheet
}

// StoreLoadOptions 控制 GameDataStore 的加载选项，支持多语言切换时复用只读档案句柄与语言无关表。
type StoreLoadOptions struct {
	AppLang       i18n.Lang
	BaseStore     *Store
	SharedReaders bool
	DeferIcons    bool   // 为 true 时跳过启动阶段全量图标扫描，留待进入主界面后后台静默或按需加载
	IconCacheDir  string // 可选本地图标扫描缓存目录（仅写入应用数据目录，按客户端版本指纹自动失效）
	OnProgress    func(LoadProgress)
}

// DataRegion 返回当前主数据源所属区服（"cn" 或 "global"），用于匹配 GitHub 版本快照。
func (s *Store) DataRegion() string {
	if s == nil {
		return "cn"
	}
	if s.PrimaryLang == sqpack.LangJapanese || s.PrimaryLang == sqpack.LangEnglish {
		return "global"
	}
	if !s.HasCN && s.HasGlobal {
		return "global"
	}
	return "cn"
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

// SnapshotStoreOptions 控制从历史 .xivsnap 快照构建只读 Store 的选项。
type SnapshotStoreOptions struct {
	AppLang          i18n.Lang
	Region           string // "cn" | "global"
	Version          string
	BGMDigests       map[int]BGMDigest
	PresetFileExists map[string]bool
	OnProgress       func(LoadProgress)
}

// LoadGameDataStoreFromSnapshot 直接使用与本地客户端相同的解析引擎，从 .xivsnap 快照构建只读内存 Store。
func LoadGameDataStoreFromSnapshot(snapReader sqpack.FileReader, schemasFS fs.FS, opts SnapshotStoreOptions) (*Store, error) {
	if snapReader == nil {
		return nil, fmt.Errorf("未提供有效的快照读取器")
	}
	appLang := i18n.NormalizeLang(string(opts.AppLang))
	region := strings.ToLower(strings.TrimSpace(opts.Region))
	var cnEXD, globalEXD sqpack.FileReader
	var primaryLang sqpack.Language
	var hasCN, hasGlobal bool
	var cnVer, globalVer string

	if region == "global" {
		hasGlobal = true
		globalVer = opts.Version
		globalEXD = snapReader
		if appLang == i18n.LangJA {
			primaryLang = sqpack.LangJapanese
		} else {
			primaryLang = sqpack.LangEnglish
		}
	} else {
		hasCN = true
		cnVer = opts.Version
		cnEXD = snapReader
		primaryLang = sqpack.LangChineseSimplified
	}

	return loadGameDataStoreCore(coreStoreLoadInput{
		appLang:          appLang,
		schemasFS:        schemasFS,
		primaryEXD:       snapReader,
		cnEXD:            cnEXD,
		globalEXD:        globalEXD,
		primaryLang:      primaryLang,
		hasCN:            hasCN,
		hasGlobal:        hasGlobal,
		cnVer:            cnVer,
		globalVer:        globalVer,
		primaryVer:       opts.Version,
		presetBGMDigests: opts.BGMDigests,
		presetFileExists: opts.PresetFileExists,
		skipIcons:        true,
		onProgress:       opts.OnProgress,
	})
}

type coreStoreLoadInput struct {
	appLang          i18n.Lang
	schemasFS        fs.FS
	cnReader         *sqpack.GameReader
	globalReader     *sqpack.GameReader
	primaryReader    *sqpack.GameReader
	openedCN         bool
	openedGlobal     bool
	sharedReaders    bool
	primaryEXD       sqpack.FileReader
	cnEXD            sqpack.FileReader
	globalEXD        sqpack.FileReader
	primaryLang      sqpack.Language
	hasCN            bool
	hasGlobal        bool
	cnVer            string
	globalVer        string
	primaryVer       string
	cnPath           string
	globalPath       string
	base             *Store
	texCache         *sqpack.TextureCache
	presetBGMDigests map[int]BGMDigest
	presetFileExists map[string]bool
	skipIcons        bool
	deferIcons       bool
	iconCacheDir     string
	onProgress       func(LoadProgress)
}

// LoadGameDataStoreWithOptions 按给定选项加载客户端数据，支持在切换语言时复用只读档案句柄与语言无关表。
func LoadGameDataStoreWithOptions(resolved *config.ResolvedPaths, schemasFS fs.FS, opts StoreLoadOptions) (*Store, error) {
	if resolved == nil || (!resolved.HasCN() && !resolved.HasGlobal()) {
		return nil, config.ErrNoClientConfigured
	}

	appLang := i18n.NormalizeLang(string(opts.AppLang))
	base := opts.BaseStore

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

	var cnEXD, globalEXD sqpack.FileReader
	if cnReader != nil {
		cnEXD = cnReader
	}
	if globalReader != nil {
		globalEXD = globalReader
	}
	return loadGameDataStoreCore(coreStoreLoadInput{
		appLang:       appLang,
		schemasFS:     schemasFS,
		cnReader:      cnReader,
		globalReader:  globalReader,
		primaryReader: primaryReader,
		openedCN:      openedCN,
		openedGlobal:  openedGlobal,
		sharedReaders: opts.SharedReaders,
		primaryEXD:    primaryReader,
		cnEXD:         cnEXD,
		globalEXD:     globalEXD,
		primaryLang:   primaryLang,
		hasCN:         resolved.HasCN(),
		hasGlobal:     resolved.HasGlobal(),
		cnVer:         cnVer,
		globalVer:     globalVer,
		primaryVer:    resolved.VersionForClient(primaryReader == globalReader),
		cnPath:        cnPath,
		globalPath:    globalPath,
		base:          base,
		texCache:      texCache,
		deferIcons:    opts.DeferIcons,
		iconCacheDir:  opts.IconCacheDir,
		onProgress:    opts.OnProgress,
	})
}

// 宽表与字典表按需列裁剪列表：跳过大量未使用的单元格装箱，大幅降低加载期内存分配与 GC 压力。
var (
	colsENpcBase = func() []int {
		cols := make([]int, 0, 32)
		for i := 2; i <= 33; i++ {
			cols = append(cols, i)
		}
		return cols
	}()
	colsSpecialShop = func() []int {
		cols := make([]int, 0, 1+60*15+1)
		cols = append(cols, 0)
		blocks := []int{1, 61, 181, 241, 301, 421, 481, 541, 661, 721, 781, 901, 961, 1021, 1141}
		for _, start := range blocks {
			for slot := 0; slot < 60; slot++ {
				cols = append(cols, start+slot)
			}
		}
		cols = append(cols, 2041)
		return cols
	}()
	colsLevel              = []int{0, 2, 5, 6, 7, 9}
	colsENpcResident       = []int{0, 7, 8}
	colsTopicSelect        = []int{0, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	colsFccShop            = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	colsAchievementPrimary = []int{0, 1, 2, 5, 6, 7, 11}
	colsTitlePrimary       = []int{0, 1, 2}
	colsPlaceName          = []int{0}
	colsCol0               = []int{0}
	colsCol01              = []int{0, 1}
	colsCol02              = []int{0, 2}
	colsCol12              = []int{1, 2}
	colsCol2               = []int{2}
	colsCol45              = []int{4, 5}
	colsAchCategory        = []int{0, 1, 4}
)

func loadGameDataStoreCore(in coreStoreLoadInput) (*Store, error) {
	appLang := in.appLang
	schemasFS := in.schemasFS
	cnReader := in.cnReader
	globalReader := in.globalReader
	primaryReader := in.primaryReader
	primaryEXD := in.primaryEXD
	cnEXD := in.cnEXD
	globalEXD := in.globalEXD
	primaryLang := in.primaryLang
	base := in.base
	onProgress := in.onProgress
	startTime := time.Now()

	reuseNone := base != nil && primaryReader != nil && base.PrimaryReader == primaryReader && base.langNone != nil
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
		completeJournals                                           *sqpack.SchemaSheet
		cutscenes, eventSystemDefines                              *sqpack.SchemaSheet
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
		bgmSheet, bgmSituations, bgmSwitches, bgmSystemDefines     *sqpack.SchemaSheet
		orchPaths, orchUiparams, gfates, gfateTypes                *sqpack.SchemaSheet
		weddingBGMs, weddingBGMsEn, weddingBGMsJa                  *sqpack.SchemaSheet
		orchPrimary, orchCn, orchEn, orchJa, orchCategories        *sqpack.SchemaSheet
		fates, goldSaucerTexts                                     *sqpack.SchemaSheet
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

	loadSheet := func(reader sqpack.FileReader, sheet string, lang sqpack.Language, cols []int, dst *map[int]sqpack.Row) {
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

	loadSub := func(reader sqpack.FileReader, sheet string, lang sqpack.Language, dst *map[int][]sqpack.SubRow) {
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

	loadSchema := func(reader sqpack.FileReader, sheet string, lang sqpack.Language, fields []string, dst **sqpack.SchemaSheet) {
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

	loadSchemaSub := func(reader sqpack.FileReader, sheet string, lang sqpack.Language, dst **sqpack.SchemaSubrowSheet) {
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
		bgmSheet = bn.bgms
		bgmSituations = bn.bgmSituations
		bgmSwitches = bn.bgmSwitches
		bgmSystemDefines = bn.bgmSystemDefines
		orchPaths = bn.orchPaths
		orchUiparams = bn.orchUiparams
		gfates = bn.gfates
		gfateTypes = bn.gfateTypes
		cutscenes = bn.cutscenes
		eventSystemDefines = bn.eventSystemDefines
	} else {
		loadSheet(primaryEXD, "EquipSlotCategory", sqpack.LangNone, nil, &rawEquipSlots)
		loadSheet(primaryEXD, "ItemRepairResource", sqpack.LangNone, colsCol0, &rawRepairResources)
		loadSheet(primaryEXD, "ItemAction", sqpack.LangNone, nil, &itemActions)
		loadSheet(primaryEXD, "ItemFood", sqpack.LangNone, nil, &itemFoods)
		loadSheet(primaryEXD, "Recipe", sqpack.LangNone, nil, &recipes)
		loadSheet(primaryEXD, "RecipeLevelTable", sqpack.LangNone, nil, &recipeLevelTables)
		loadSheet(primaryEXD, "GatheringItem", sqpack.LangNone, nil, &gatheringItems)
		loadSub(primaryEXD, "GatheringItemPoint", sqpack.LangNone, &gatheringItemPointSubrows)
		loadSheet(primaryEXD, "GatheringPoint", sqpack.LangNone, nil, &gatheringPoints)
		loadSheet(primaryEXD, "GatheringPointBase", sqpack.LangNone, nil, &gatheringPointBases)
		loadSheet(primaryEXD, "ExportedGatheringPoint", sqpack.LangNone, nil, &exportedGatheringPoints)
		loadSheet(primaryEXD, "GatheringPointTransient", sqpack.LangNone, nil, &gatheringPointTransients)
		loadSheet(primaryEXD, "GatheringRarePopTimeTable", sqpack.LangNone, nil, &gatheringRarePopTimeTables)
		loadSheet(primaryEXD, "GatheringItemLevelConvertTable", sqpack.LangNone, colsCol01, &rawGatheringItemLevels)
		loadSheet(primaryEXD, "TerritoryType", sqpack.LangNone, nil, &territoryTypes)
		loadSheet(primaryEXD, "Map", sqpack.LangNone, nil, &maps)
		loadSheet(primaryEXD, "SpearfishingNotebook", sqpack.LangNone, nil, &spearfishingNotebooks)
		loadSub(primaryEXD, "GilShopItem", sqpack.LangNone, &gilShopItems)
		loadSub(primaryEXD, "GCScripShopItem", sqpack.LangNone, &gcScripShopItems)
		loadSheet(primaryEXD, "TomestonesItem", sqpack.LangNone, colsCol02, &tomestonesItems)
		loadSheet(primaryEXD, "ENpcBase", sqpack.LangNone, colsENpcBase, &enpcBases)
		loadSheet(primaryEXD, "PreHandler", sqpack.LangNone, colsCol2, &preHandlers)
		loadSheet(primaryEXD, "InclusionShop", sqpack.LangNone, nil, &inclusionShops)
		loadSub(primaryEXD, "InclusionShopSeries", sqpack.LangNone, &inclusionSeries)
		loadSheet(primaryEXD, "CustomTalk", sqpack.LangNone, nil, &customTalks)
		loadSheet(primaryEXD, "Level", sqpack.LangNone, colsLevel, &levels)
		loadSheet(primaryEXD, "Cabinet", sqpack.LangNone, colsCol0, &rawCabinets)
		loadSheet(primaryEXD, "RetainerTask", sqpack.LangNone, nil, &retainerTasks)
		loadSheet(primaryEXD, "RetainerTaskNormal", sqpack.LangNone, nil, &retainerTaskNormals)
		loadSheet(primaryEXD, "RetainerTaskParameter", sqpack.LangNone, nil, &retainerTaskParameters)
		if schemasFS != nil {
			loadSchemaSub(primaryEXD, "QuestClassJobReward", sqpack.LangNone, &questClassJobRewards)
			loadSchema(primaryEXD, "EventIconType", sqpack.LangNone, []string{"NpcIconAvailable"}, &eventIconTypes)
			loadSchema(primaryEXD, "ParamGrow", sqpack.LangNone, []string{"ExpToNext", "ScaledQuestXP"}, &paramGrows)
			loadSchema(primaryEXD, "BeastRankBonus", sqpack.LangNone, nil, &beastRankBonuses)
			loadSchema(primaryEXD, "InstanceContent", sqpack.LangNone, []string{"ContentFinderCondition", "BGM", "WinBGM"}, &instanceContents)
			loadSchema(primaryEXD, "CraftLeve", sqpack.LangNone, nil, &craftLeves)
			loadSchema(primaryEXD, "GatheringLeve", sqpack.LangNone, nil, &gatheringLeves)
			loadSchema(primaryEXD, "GatheringLeveRoute", sqpack.LangNone, nil, &gatheringLeveRoutes)
			loadSchema(primaryEXD, "BattleLeve", sqpack.LangNone, nil, &battleLeves)
			loadSchema(primaryEXD, "CompanyLeve", sqpack.LangNone, nil, &companyLeves)
			loadSchema(primaryEXD, "LeveRewardItem", sqpack.LangNone, nil, &leveRewardItems)
			loadSchema(primaryEXD, "LeveRewardItemGroup", sqpack.LangNone, nil, &leveRewardItemGroups)
			loadSchemaSub(primaryEXD, "CollectablesShopItem", sqpack.LangNone, &collectablesShopItems)
			loadSchema(primaryEXD, "CollectablesShopRefine", sqpack.LangNone, nil, &collectablesShopRefines)
			loadSchema(primaryEXD, "CollectablesShopRewardItem", sqpack.LangNone, nil, &collectablesShopRewardItems)
			loadSchema(primaryEXD, "CollectablesShopRewardScrip", sqpack.LangNone, nil, &collectablesShopRewardScrips)
			loadSchema(primaryEXD, "SatisfactionNpc", sqpack.LangNone, nil, &satisfactionNpcs)
			loadSchemaSub(primaryEXD, "SatisfactionSupply", sqpack.LangNone, &satisfactionSupplies)
			loadSchema(primaryEXD, "SatisfactionSupplyReward", sqpack.LangNone, nil, &satisfactionRewards)
			loadSchema(primaryEXD, "HWDCrafterSupply", sqpack.LangNone, nil, &hwdSupplies)
			loadSchema(primaryEXD, "HWDCrafterSupplyReward", sqpack.LangNone, nil, &hwdRewards)
			loadSchema(primaryEXD, "BGM", sqpack.LangNone, nil, &bgmSheet)
			loadSchema(primaryEXD, "BGMSituation", sqpack.LangNone, nil, &bgmSituations)
			loadSchema(primaryEXD, "BGMSwitch", sqpack.LangNone, nil, &bgmSwitches)
			loadSchema(primaryEXD, "BGMSystemDefine", sqpack.LangNone, nil, &bgmSystemDefines)
			loadSchema(primaryEXD, "OrchestrionPath", sqpack.LangNone, nil, &orchPaths)
			loadSchema(primaryEXD, "OrchestrionUiparam", sqpack.LangNone, nil, &orchUiparams)
			loadSchema(primaryEXD, "GFATE", sqpack.LangNone, []string{"BGM", "Type"}, &gfates)
			loadSchema(primaryEXD, "GFateType", sqpack.LangNone, []string{"Name"}, &gfateTypes)
			loadSchema(primaryEXD, "Cutscene", sqpack.LangNone, []string{"Path"}, &cutscenes)
			loadSchema(primaryEXD, "EventSystemDefine", sqpack.LangNone, []string{"Text", "DefineValue"}, &eventSystemDefines)
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
		completeJournals = bp.completeJournals
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
		weddingBGMs = bp.weddingBGMs
		weddingBGMsEn = bp.weddingBGMsEn
		weddingBGMsJa = bp.weddingBGMsJa
		orchPrimary = bp.orchPrimary
		orchCn = bp.orchCn
		orchEn = bp.orchEn
		orchJa = bp.orchJa
		orchCategories = bp.orchCategories
		fates = bp.fates
		goldSaucerTexts = bp.goldSaucerTexts
	} else {
		loadSheet(primaryEXD, "Item", primaryLang, nil, &itemsPrimary)
		if cnEXD != nil && primaryLang != sqpack.LangChineseSimplified {
			loadSheet(cnEXD, "Item", sqpack.LangChineseSimplified, []int{8, 9}, &itemsCn)
		}
		if globalEXD != nil {
			if primaryLang != sqpack.LangEnglish {
				loadSheet(globalEXD, "Item", sqpack.LangEnglish, []int{8, 9}, &itemsEn)
			}
			if primaryLang != sqpack.LangJapanese {
				loadSheet(globalEXD, "Item", sqpack.LangJapanese, []int{8, 9}, &itemsJa)
			}
		} else {
			itemsEn = make(map[int]sqpack.Row)
			itemsJa = make(map[int]sqpack.Row)
		}

		loadSheet(primaryEXD, "ItemUICategory", primaryLang, colsCol01, &rawCategories)
		loadSheet(primaryEXD, "ClassJob", primaryLang, colsCol01, &rawClassJobs)
		loadSheet(primaryEXD, "ClassJobCategory", primaryLang, colsCol0, &rawClassJobCategories)
		loadSheet(primaryEXD, "BaseParam", primaryLang, colsCol12, &rawBaseParams)
		loadSheet(primaryEXD, "CraftType", primaryLang, colsCol2, &rawCraftTypes)
		loadSheet(primaryEXD, "SecretRecipeBook", primaryLang, colsCol01, &rawSecretBooks)
		loadSheet(primaryEXD, "GatheringType", primaryLang, colsCol01, &rawGatheringTypes)
		loadSheet(primaryEXD, "GatheringSubCategory", primaryLang, colsCol45, &rawGatheringSubCategories)
		loadSheet(primaryEXD, "PlaceName", primaryLang, colsPlaceName, &rawPlaceNames)
		loadSheet(primaryEXD, "FishingSpot", primaryLang, nil, &fishingSpots)
		loadSheet(primaryEXD, "FishParameter", primaryLang, nil, &fishParameters)
		loadSheet(primaryEXD, "SpearfishingItem", primaryLang, nil, &spearfishingItems)
		loadSheet(primaryEXD, "GilShop", primaryLang, colsCol0, &gilShops)
		loadSheet(primaryEXD, "SpecialShop", primaryLang, colsSpecialShop, &specialShops)
		loadSheet(primaryEXD, "FccShop", primaryLang, colsFccShop, &fccShops)
		loadSheet(primaryEXD, "ENpcResident", primaryLang, colsENpcResident, &enpcResidents)
		loadSheet(primaryEXD, "TopicSelect", primaryLang, colsTopicSelect, &topicSelects)
		loadSheet(primaryEXD, "InclusionShopCategory", primaryLang, colsCol02, &inclusionCategories)

		loadSheet(primaryEXD, "Achievement", primaryLang, colsAchievementPrimary, &achievementsPrimary)
		if cnEXD != nil && primaryLang != sqpack.LangChineseSimplified {
			loadSheet(cnEXD, "Achievement", sqpack.LangChineseSimplified, []int{1, 2}, &achievementsCn)
		}
		if globalEXD != nil {
			if primaryLang != sqpack.LangEnglish {
				loadSheet(globalEXD, "Achievement", sqpack.LangEnglish, []int{1, 2}, &achievementsEn)
			}
			if primaryLang != sqpack.LangJapanese {
				loadSheet(globalEXD, "Achievement", sqpack.LangJapanese, []int{1, 2}, &achievementsJa)
			}
		} else {
			achievementsEn = make(map[int]sqpack.Row)
			achievementsJa = make(map[int]sqpack.Row)
		}
		loadSheet(primaryEXD, "AchievementCategory", primaryLang, colsAchCategory, &rawAchievementCategories)
		loadSheet(primaryEXD, "AchievementKind", primaryLang, colsCol01, &rawAchievementKinds)
		loadSheet(primaryEXD, "Title", primaryLang, colsTitlePrimary, &titlesPrimary)
		if globalEXD != nil {
			if primaryLang != sqpack.LangEnglish {
				loadSheet(globalEXD, "Title", sqpack.LangEnglish, []int{0, 1}, &titlesEn)
			}
			if primaryLang != sqpack.LangJapanese {
				loadSheet(globalEXD, "Title", sqpack.LangJapanese, []int{0, 1}, &titlesJa)
			}
		} else {
			titlesEn = make(map[int]sqpack.Row)
			titlesJa = make(map[int]sqpack.Row)
		}

		if schemasFS != nil {
			loadSchema(primaryEXD, "Quest", primaryLang, questNeededFields, &questsPrimary)
			if cnEXD != nil && primaryLang != sqpack.LangChineseSimplified {
				loadSchema(cnEXD, "Quest", sqpack.LangChineseSimplified, []string{"Name"}, &questsCn)
				loadSchema(cnEXD, "Leve", sqpack.LangChineseSimplified, []string{"Name"}, &levesCn)
				loadSchema(cnEXD, "Orchestrion", sqpack.LangChineseSimplified, []string{"Name"}, &orchCn)
			}
			if globalEXD != nil {
				if primaryLang != sqpack.LangEnglish {
					loadSchema(globalEXD, "Quest", sqpack.LangEnglish, []string{"Name"}, &questsEn)
					loadSchema(globalEXD, "Leve", sqpack.LangEnglish, []string{"Name"}, &levesEn)
					loadSchema(globalEXD, "Orchestrion", sqpack.LangEnglish, []string{"Name", "Description"}, &orchEn)
					loadSchema(globalEXD, "WeddingBGM", sqpack.LangEnglish, []string{"SongName"}, &weddingBGMsEn)
				}
				if primaryLang != sqpack.LangJapanese {
					loadSchema(globalEXD, "Quest", sqpack.LangJapanese, []string{"Name"}, &questsJa)
					loadSchema(globalEXD, "Leve", sqpack.LangJapanese, []string{"Name"}, &levesJa)
					loadSchema(globalEXD, "Orchestrion", sqpack.LangJapanese, []string{"Name", "Description"}, &orchJa)
					loadSchema(globalEXD, "WeddingBGM", sqpack.LangJapanese, []string{"SongName"}, &weddingBGMsJa)
				}
			}
			loadSchema(primaryEXD, "JournalGenre", primaryLang, []string{"Name", "Icon", "JournalCategory"}, &journalGenres)
			loadSchema(primaryEXD, "JournalCategory", primaryLang, []string{"Name", "JournalSection"}, &journalCategories)
			loadSchema(primaryEXD, "JournalSection", primaryLang, []string{"Name"}, &journalSections)
			loadSchema(primaryEXD, "CompleteJournal", primaryLang, completeJournalNeededFields, &completeJournals)
			loadSchema(primaryEXD, "QuestRewardOther", primaryLang, []string{"Name", "Icon"}, &questRewardOthers)
			loadSchema(primaryEXD, "BeastTribe", primaryLang, []string{"Name", "CurrencyItem", "BeastRankBonus"}, &beastTribes)
			loadSchema(primaryEXD, "BeastReputationRank", primaryLang, []string{"Name"}, &beastReputationRanks)
			loadSchema(primaryEXD, "GrandCompany", primaryLang, []string{"Singular"}, &grandCompanies)
			loadSchema(primaryEXD, "ExVersion", primaryLang, []string{"Name"}, &exVersions)
			loadSchema(primaryEXD, "Mount", primaryLang, []string{"Singular", "Icon", "RideBGM"}, &mounts)
			loadSchema(primaryEXD, "Action", primaryLang, []string{"Name", "Icon"}, &actions)
			loadSchema(primaryEXD, "Emote", primaryLang, []string{"Name", "Icon"}, &emotes)
			loadSchema(primaryEXD, "GeneralAction", primaryLang, []string{"Name", "Icon"}, &generalActions)
			loadSchema(primaryEXD, "ContentFinderCondition", primaryLang, []string{"Name", "Icon"}, &contentFinderConditions)
			loadSchema(primaryEXD, "EObjName", primaryLang, []string{"Singular"}, &eobjNames)

			loadSchema(primaryEXD, "Leve", primaryLang, nil, &levesPrimary)
			loadSchema(primaryEXD, "LeveAssignmentType", primaryLang, []string{"Name", "Icon"}, &leveAssignmentTypes)
			loadSchema(primaryEXD, "LeveClient", primaryLang, []string{"Name"}, &leveClients)
			loadSchema(primaryEXD, "Town", primaryLang, []string{"Name", "Icon"}, &towns)
			loadSchema(primaryEXD, "LeveString", primaryLang, []string{"Objective"}, &leveStrings)
			loadSchema(primaryEXD, "GatheringLeveRule", primaryLang, []string{"Rule"}, &gatheringLeveRules)
			loadSchema(primaryEXD, "BattleLeveRule", primaryLang, []string{"Rule"}, &battleLeveRules)
			loadSchema(primaryEXD, "CompanyLeveRule", primaryLang, nil, &companyLeveRules)
			loadSchema(primaryEXD, "EventItem", primaryLang, []string{"Singular", "Name", "Icon"}, &eventItems)
			loadSchema(primaryEXD, "BNpcName", primaryLang, []string{"Singular"}, &bnpcNames)

			loadSchema(primaryEXD, "CollectablesShop", primaryLang, nil, &collectablesShops)
			loadSchema(primaryEXD, "CollectablesShopItemGroup", primaryLang, []string{"Name"}, &collectablesShopItemGroups)
			loadSchema(primaryEXD, "HWDCrafterSupplyTerm", primaryLang, []string{"Name"}, &hwdTerms)
			loadSchema(primaryEXD, "WeddingBGM", primaryLang, nil, &weddingBGMs)
			loadSchema(primaryEXD, "Orchestrion", primaryLang, nil, &orchPrimary)
			loadSchema(primaryEXD, "OrchestrionCategory", primaryLang, nil, &orchCategories)
			loadSchema(primaryEXD, "Fate", primaryLang, []string{"Name", "Music", "Icon"}, &fates)
			loadSchema(primaryEXD, "GoldSaucerTextData", primaryLang, []string{"Text"}, &goldSaucerTexts)
		}
	}

	// 3. 图标资源扫描（语言无关，切换界面语言时直接复用；支持 DeferIcons 延迟到进入主界面后后台静默扫描）
	var cnIconFiles, globalIconFiles []sqpack.IconFileEntry
	var hasBaseCNIcons, hasBaseGlobalIcons bool
	if !in.skipIcons && base != nil {
		base.iconMu.RLock()
		if cnReader != nil && base.CNReader == cnReader && base.cnIconFiles != nil {
			cnIconFiles = base.cnIconFiles
			hasBaseCNIcons = true
		}
		if globalReader != nil && base.GlobalReader == globalReader && base.globalIconFiles != nil {
			globalIconFiles = base.globalIconFiles
			hasBaseGlobalIcons = true
		}
		base.iconMu.RUnlock()
	}

	needCNIcons := !in.skipIcons && cnReader != nil && !hasBaseCNIcons
	needGlobalIcons := !in.skipIcons && globalReader != nil && !hasBaseGlobalIcons
	deferIconScan := in.deferIcons && (needCNIcons || needGlobalIcons)

	var iconScanTasks []func()
	if !deferIconScan {
		if needCNIcons {
			iconScanTasks = append(iconScanTasks, func() {
				files, err := sqpack.ScanIconFilesCached(cnReader, in.iconCacheDir)
				if err != nil {
					setErr("ui/icon(CN)", err)
					return
				}
				cnIconFiles = files
			})
		}
		if needGlobalIcons {
			iconScanTasks = append(iconScanTasks, func() {
				files, err := sqpack.ScanIconFilesCached(globalReader, in.iconCacheDir)
				if err != nil {
					setErr("ui/icon(Global)", err)
					return
				}
				globalIconFiles = files
			})
		}
	}
	// 同步扫描模式下将耗时最长的图标扫描前置入队，避免在任务队列末尾形成长尾阻塞
	if len(iconScanTasks) > 0 {
		tasks = append(iconScanTasks, tasks...)
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
		if !in.sharedReaders {
			if in.openedCN && cnReader != nil {
				_ = cnReader.Close()
			}
			if in.openedGlobal && globalReader != nil {
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
		orchEn = orchPrimary
		weddingBGMsEn = weddingBGMs
	case sqpack.LangJapanese:
		itemsJa = itemsPrimary
		achievementsJa = achievementsPrimary
		titlesJa = titlesPrimary
		questsJa = questsPrimary
		levesJa = levesPrimary
		orchJa = orchPrimary
		weddingBGMsJa = weddingBGMs
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

	tomestoneByIndex := make(map[int]int)
	for _, row := range tomestonesItems {
		itemID := row.IntAt(0)
		idx := row.IntAt(2)
		if itemID > 0 && idx > 0 {
			tomestoneByIndex[idx] = itemID
		}
	}

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

	// 并行阶段 1：同时构建配方、采集/捕鱼、商店/NPC 与雇员探险索引
	var (
		recipesByResult        map[int][]int
		recipesByIngredient    map[int][]recipeUsage
		craftTypes             map[int]CraftTypeInfo
		craftTypeList          []CraftTypeInfo
		gatheringItemsByItem   map[int][]int
		gatheringPointsByGItem map[int][]int
		fishingSpotsByItem     map[int][]int
		fishParamsByItem       map[int]sqpack.Row
		spearfishingByItem     map[int][]spearfishingLink
		shopIdx                ShopIndices
		retainerIdx            RetainerIndices
	)

	var phase1WG sync.WaitGroup
	phase1WG.Add(4)
	go func() {
		defer phase1WG.Done()
		recipesByResult = make(map[int][]int)
		recipesByIngredient = make(map[int][]recipeUsage)
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

		craftTypes = make(map[int]CraftTypeInfo, len(rawCraftTypes))
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
	}()

	go func() {
		defer phase1WG.Done()
		gatheringItemsByItem = make(map[int][]int)
		for gItemID, row := range gatheringItems {
			itemID := row.IntAt(0)
			if itemID > 0 {
				gatheringItemsByItem[itemID] = append(gatheringItemsByItem[itemID], gItemID)
			}
		}

		gatheringPointsByGItem = make(map[int][]int)
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

		fishingSpotsByItem = make(map[int][]int)
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

		fishParamsByItem = make(map[int]sqpack.Row)
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
		spearfishingByItem = make(map[int][]spearfishingLink)
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
	}()

	go func() {
		defer phase1WG.Done()
		shopIdx = buildShopIndices(shopLoaderInput{
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
	}()

	go func() {
		defer phase1WG.Done()
		retainerIdx = buildRetainerIndices(appLang, retainerTasks, retainerTaskNormals, retainerTaskParameters, classJobCategories)
	}()
	phase1WG.Wait()

	emit(LoadProgress{
		Stage:   LoadStageIndices,
		Done:    1,
		Total:   4,
		Message: i18n.T(appLang, "loading.msg_shops"),
	}, true)

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

	// 并行阶段 2：同时构建收藏品交易与任务全量索引
	var (
		collectablesIdx collectablesIndices
		questIdx        questIndices
	)
	var phase2WG sync.WaitGroup
	phase2WG.Add(2)
	go func() {
		defer phase2WG.Done()
		collectablesIdx = buildCollectablesData(collectablesLoaderInput{
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
	}()
	go func() {
		defer phase2WG.Done()
		questIdx = buildQuestData(questLoaderInput{
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
			cutscenes:               cutscenes,
			completeJournals:        completeJournals,
			bgms:                    bgmSheet,
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
	}()
	phase2WG.Wait()

	// 构建全量有效道具摘要列表
	itemSummaries := make([]ItemSummary, 0, len(itemsPrimary))
	itemSummaryByID := make(map[int]ItemSummary, len(itemsPrimary))
	categoryCount := make(map[int]int)
	otherCatLabel := i18n.T(appLang, "common.other")

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
		catName := otherCatLabel
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

	// 并行阶段 3：同时构建理符、成就称号与音乐索引
	var (
		leveIdx                  leveIndices
		totalTitles              int
		achievements             []AchievementEntry
		achievementByID          map[int]AchievementEntry
		achievementsByRewardItem map[int][]AchievementEntry
		achievementKindList      []AchievementKindInfo
		achievementCategoryList  []AchievementCategoryInfo
		musicIdx                 musicIndices
		eventSystemBGMs          map[string]int
		bgmByFile, orchByFile    map[string]MusicEntry
	)

	var phase3WG sync.WaitGroup
	phase3WG.Add(3)
	go func() {
		defer phase3WG.Done()
		leveIdx = buildLeveData(leveLoaderInput{
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
	}()

	go func() {
		defer phase3WG.Done()
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

		for _, row := range titlesPrimary {
			if row.StringAt(0) != "" {
				totalTitles++
			}
		}

		achievements = make([]AchievementEntry, 0, len(achievementsPrimary))
		achievementByID = make(map[int]AchievementEntry, len(achievementsPrimary))
		achievementsByRewardItem = make(map[int][]AchievementEntry)
		kindCount := make(map[int]int)
		catCount := make(map[int]int)
		generalLabel := i18n.T(appLang, "common.general")

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
				kindName = otherCatLabel
			}
			catName := catInfo.name
			if catName == "" {
				catName = generalLabel
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

		catsByKind := make(map[int][]AchievementCategoryInfo)
		for id, cat := range achCats {
			cnt := catCount[id]
			if cnt <= 0 || id <= 0 {
				continue
			}
			kName := achKinds[cat.kindID].name
			if kName == "" {
				kName = otherCatLabel
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

		for _, k := range achievementKindList {
			achievementCategoryList = append(achievementCategoryList, k.Categories...)
		}
		if orphanCats := catsByKind[0]; len(orphanCats) > 0 {
			achievementCategoryList = append(achievementCategoryList, orphanCats...)
		}
	}()

	go func() {
		defer phase3WG.Done()
		musicIdx = buildMusicData(musicLoaderInput{
			appLang:                 appLang,
			primaryReader:           primaryEXD,
			hasFileReader:           primaryReader,
			presetFileExists:        in.presetFileExists,
			schemasFS:               schemasFS,
			bgmSheet:                bgmSheet,
			bgmSituations:           bgmSituations,
			bgmSwitches:             bgmSwitches,
			bgmSystemDefines:        bgmSystemDefines,
			weddingBGMs:             weddingBGMs,
			weddingBGMsEn:           weddingBGMsEn,
			weddingBGMsJa:           weddingBGMsJa,
			orchPrimary:             orchPrimary,
			orchCn:                  orchCn,
			orchEn:                  orchEn,
			orchJa:                  orchJa,
			orchPaths:               orchPaths,
			orchUiparams:            orchUiparams,
			orchCategories:          orchCategories,
			exVersions:              exVersions,
			territoryTypes:          territoryTypes,
			placeNames:              placeNames,
			instanceContents:        instanceContents,
			contentFinderConditions: contentFinderConditions,
			mounts:                  mounts,
			fates:                   fates,
			gfates:                  gfates,
			gfateTypes:              gfateTypes,
			goldSaucerTexts:         goldSaucerTexts,
			leves:                   levesPrimary,
			itemsPrimary:            itemsPrimary,
			itemSummaryByID:         itemSummaryByID,
		})
		eventSystemBGMs = buildEventSystemBGMMap(eventSystemDefines, bgmSheet)
		bgmByFile, orchByFile = buildMusicFileLookup(musicIdx)
	}()
	phase3WG.Wait()

	var iconIdx iconIndices
	iconsReady := true
	if !in.skipIcons {
		if deferIconScan {
			iconsReady = false
		} else {
			iconIdx = buildIconIndex(iconBuildInput{
				primaryReader:   primaryReader,
				cnReader:        cnReader,
				globalReader:    globalReader,
				cnIconFiles:     cnIconFiles,
				globalIconFiles: globalIconFiles,
				itemSummaries:   itemSummaries,
				achievements:    achievements,
				quests:          questIdx.quests,
				leves:           leveIdx.leves,
			})
		}
	}

	var reusedDigests map[int]BGMDigest
	if len(in.presetBGMDigests) > 0 {
		reusedDigests = make(map[int]BGMDigest, len(in.presetBGMDigests))
		for k, v := range in.presetBGMDigests {
			reusedDigests[k] = v
		}
	} else if base != nil && primaryReader != nil && base.PrimaryReader == primaryReader {
		base.bgmDigestMu.Lock()
		if len(base.bgmDigests) > 0 {
			reusedDigests = make(map[int]BGMDigest, len(base.bgmDigests))
			for k, v := range base.bgmDigests {
				reusedDigests[k] = v
			}
		}
		base.bgmDigestMu.Unlock()
	}
	if len(reusedDigests) > 0 {
		for i := range musicIdx.bgms {
			b := &musicIdx.bgms[i]
			if d, ok := reusedDigests[b.ID]; ok && d.File == b.File {
				b.FileExists = d.FileExists
				b.FileSize = d.FileSize
				b.ContentMD5 = d.ContentMD5
				musicIdx.bgmByID[b.ID] = *b
				musicIdx.musicByKey[b.Key] = *b
			}
		}
		for i := range musicIdx.music {
			if musicIdx.music[i].Kind == MusicKindBGM {
				if d, ok := reusedDigests[musicIdx.music[i].ID]; ok && d.File == musicIdx.music[i].File {
					musicIdx.music[i].FileExists = d.FileExists
					musicIdx.music[i].FileSize = d.FileSize
					musicIdx.music[i].ContentMD5 = d.ContentMD5
				}
			}
		}
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
			bgms:                         bgmSheet,
			bgmSituations:                bgmSituations,
			bgmSwitches:                  bgmSwitches,
			bgmSystemDefines:             bgmSystemDefines,
			orchPaths:                    orchPaths,
			orchUiparams:                 orchUiparams,
			gfates:                       gfates,
			gfateTypes:                   gfateTypes,
			cutscenes:                    cutscenes,
			eventSystemDefines:           eventSystemDefines,
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
			completeJournals:           completeJournals,
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
			weddingBGMs:                weddingBGMs,
			weddingBGMsEn:              weddingBGMsEn,
			weddingBGMsJa:              weddingBGMsJa,
			orchPrimary:                orchPrimary,
			orchCn:                     orchCn,
			orchEn:                     orchEn,
			orchJa:                     orchJa,
			orchCategories:             orchCategories,
			fates:                      fates,
			goldSaucerTexts:            goldSaucerTexts,
		}
	}

	return &Store{
		CNReader:                       cnReader,
		GlobalReader:                   globalReader,
		PrimaryReader:                  primaryReader,
		EXDReader:                      primaryEXD,
		TextureCache:                   in.texCache,
		SchemasFS:                      schemasFS,
		HasCN:                          in.hasCN,
		HasGlobal:                      in.hasGlobal,
		CNVersion:                      in.cnVer,
		GlobalVersion:                  in.globalVer,
		PrimaryVersion:                 in.primaryVer,
		CNPath:                         in.cnPath,
		GlobalPath:                     in.globalPath,
		LoadTimeMs:                     time.Since(startTime).Milliseconds(),
		AppLang:                        appLang,
		PrimaryLang:                    primaryLang,
		sharedReaders:                  in.sharedReaders,
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
		QuestCutVars:                   questIdx.questCutVars,
		QuestBGMVars:                   questIdx.questBGMVars,
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
		Music:                          musicIdx.music,
		BGMs:                           musicIdx.bgms,
		Orchestrions:                   musicIdx.orchestrions,
		MusicByKey:                     musicIdx.musicByKey,
		BGMByID:                        musicIdx.bgmByID,
		OrchByID:                       musicIdx.orchByID,
		MusicCategoryList:              musicIdx.categories,
		MusicExpansionList:             musicIdx.expansions,
		EventSystemBGMs:                eventSystemBGMs,
		BGMByFile:                      bgmByFile,
		OrchByFile:                     orchByFile,
		bgmDigests:                     reusedDigests,
		iconCacheDir:                   in.iconCacheDir,
		iconsReady:                     iconsReady,
		Icons:                          iconIdx.icons,
		IconByID:                       iconIdx.iconByID,
		IconGroups:                     iconIdx.groups,
		IconsByGroup:                   iconIdx.iconsByGroup,
		AvailableIconVariants:          iconIdx.availableVariants,
		IconUsagesByID:                 iconIdx.usagesByID,
		cnIconFiles:                    cnIconFiles,
		globalIconFiles:                globalIconFiles,
	}, nil
}
