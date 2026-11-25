package gamedata

// ItemCategoryInfo 表示道具 UI 分类统计信息。
type ItemCategoryInfo struct {
	ID     int
	Name   string
	IconID int
	Count  int
}

// CraftTypeInfo 表示能工巧匠制作职业统计信息。
type CraftTypeInfo struct {
	ID          int
	Name        string
	JobName     string
	IconID      int
	RecipeCount int
}

// AchievementCategoryInfo 表示成就二级分类统计信息。
type AchievementCategoryInfo struct {
	ID       int
	KindID   int
	KindName string
	Name     string
	Count    int
}

// AchievementKindInfo 表示成就大类统计信息。
type AchievementKindInfo struct {
	ID         int
	Name       string
	Count      int
	Categories []AchievementCategoryInfo
}

// SystemStats 汇总当前加载的客户端数据规模指标。
type SystemStats struct {
	TotalItems             int
	CraftableItems         int
	GatherableItems        int
	PurchasableItems       int
	GilShopItems           int
	SpecialShopItems       int
	RetainerVentureItems   int
	CollectableTurnInItems int
	TotalRecipes           int
	TotalGatheringPoints   int
	TotalFishingSpots      int
	TotalShops             int
	TotalRetainerTasks     int
	TotalAchievements      int
	TotalTitles            int
	TotalQuests            int
	TotalLeves             int
	TotalCollectables      int
	LoadTimeMs             int64
}

// SystemStatus 描述当前已挂载客户端版本、路径与各类汇总统计。
type SystemStatus struct {
	HasCN                 bool
	HasGlobal             bool
	CNVersion             string
	GlobalVersion         string
	PrimaryVersion        string
	CNPath                string
	GlobalPath            string
	Stats                 SystemStats
	Categories            []ItemCategoryInfo
	CraftTypes            []CraftTypeInfo
	AchievementKinds      []AchievementKindInfo
	AchievementCategories []AchievementCategoryInfo
	QuestSections         []QuestSectionInfo
	LeveAssignmentTypes   []LeveAssignmentTypeInfo
}

// MapLocationPoint 描述一个可直接用于地图弹窗展示的地点与坐标信息。
type MapLocationPoint struct {
	RegionName  string
	ZoneName    string
	PlaceName   string
	TerritoryID int
	MapID       int
	MapPath     string
	SizeFactor  int
	CoordX      float64
	CoordY      float64
	NormX       float64
	NormY       float64
}

// QuestSectionInfo 表示任务日志大类统计信息。
type QuestSectionInfo struct {
	ID    int
	Name  string
	Count int
}

// QuestRewardItem 表示任务的道具/货币/神典石奖励项。
type QuestRewardItem struct {
	ItemID           int
	ItemName         string
	IconID           int
	Count            int
	IsHQ             bool
	GroupLabel       string
	ClassJobCategory string
}

// QuestUnlockReward 表示任务解锁的职业、情感动作、技能或副本等内容。
type QuestUnlockReward struct {
	Kind   string // job|emote|action|general|instance|other
	Label  string
	IconID int
}

// QuestEntry 表示单条任务的完整解析数据。
type QuestEntry struct {
	ID                    int
	InternalID            string
	NameCn                string
	NameZh                string
	NameEn                string
	NameJa                string
	IconID                int // 任务类型图标 ID（061431 普通 / 061432 主线 / 061433 可重复 / 061439 功能解锁）
	BannerIconID          int // 任务详情横幅插图 ID（Quest.Icon，0 表示无）
	SpecialIconID         int // 任务专属徽章贴图 ID（Quest.IconSpecial，0 表示无）
	EventIconID           int
	HasLevelSync          bool // 任务名含等级调整标识（\ue0be）
	IsJobFixed            bool // 任务名含固定职业标识（\ue0bf）
	SectionID             int
	SectionName           string
	CategoryID            int
	CategoryName          string
	GenreID               int
	GenreName             string
	ExpansionID           int
	ExpansionName         string
	PlaceName             string
	SortKey               int
	Level                 int
	LevelOffset           int
	LevelMax              int
	ClassJobCategoryName  string
	ClassJobCategory2Name string
	Level2                int
	ClassJobRequiredName  string
	IssuerNpcID           int
	IssuerName            string
	IssuerLocation        *MapLocationPoint
	TargetNpcID           int
	TargetName            string
	TargetLocation        *MapLocationPoint
	PreviousQuestIDs      []int
	PreviousQuestNames    []string
	PreviousQuestJoin     int
	QuestLockIDs          []int
	QuestLockNames        []string
	InstanceContentNames  []string
	GrandCompanyName      string
	BeastTribeName        string
	BeastReputationRank   string
	BeastReputationValue  int
	MountRequiredName     string
	IsHouseRequired       bool
	IsRepeatable          bool
	RepeatIntervalLabel   string
	CanCancel             bool
	BellStart             int
	BellEnd               int
	IsMSQ                 bool
	IsFeature             bool
	GilReward             int
	ExpFactor             int
	ExpReward             int
	CurrencyRewardItemID  int
	CurrencyRewardName    string
	CurrencyRewardIconID  int
	CurrencyRewardCount   int
	TomestoneItemID       int
	TomestoneName         string
	TomestoneIconID       int
	TomestoneCount        int
	ReputationReward      int
	RewardItems           []QuestRewardItem
	Unlocks               []QuestUnlockReward
}

// QuestDialogueLine 表示按剧情顺序整理的单段任务对话、选项或系统提示。
type QuestDialogueLine struct {
	Key     string
	Kind    string // "dialogue" | "question" | "answer" | "system" | "battle"
	Speaker string
	Text    string
}

// QuestTextData 表示按需从 SqPack 解析的任务概要、目的步骤与对话文本。
type QuestTextData struct {
	QuestID    int
	Journals   []string
	Objectives []string
	Dialogues  []QuestDialogueLine
}

// QuestSearchQuery 任务检索与筛选参数。
type QuestSearchQuery struct {
	Q           string
	SectionID   int
	ExpansionID int
	Filter      string // all|msq|feature|repeatable|with_item
	MinLevel    int
	MaxLevel    int
	Page        int
	PageSize    int
}

// QuestSearchResponse 任务分页检索结果。
type QuestSearchResponse struct {
	Items      []QuestEntry
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

// LeveAssignmentTypeInfo 表示理符分类（佣兵/采集/制作/军队）统计信息。
type LeveAssignmentTypeInfo struct {
	ID     int
	Name   string
	IconID int
	Kind   string // craft|gathering|battle|company
	Count  int
}

// LeveTurnInItem 表示制作/捕鱼理符需要交纳的道具项。
type LeveTurnInItem struct {
	ItemID   int
	ItemName string
	IconID   int
	Count    int
}

// LeveCombatTarget 表示战斗/军队理符中的目标怪物信息。
type LeveCombatTarget struct {
	BNpcName string
	Level    int
	Count    int
}

// LeveObjectiveItem 表示采集/战斗理符的任务专用道具目标。
type LeveObjectiveItem struct {
	ItemName string
	Count    int
	DropRate int
}

// LeveRewardEntry 表示理符随机奖励组中的单条奖励道具。
type LeveRewardEntry struct {
	ItemID   int
	ItemName string
	IconID   int
	Count    int
	IsHQ     bool
}

// LeveRewardGroup 表示理符的单个概率奖励池。
type LeveRewardGroup struct {
	ProbabilityPercent int
	Items              []LeveRewardEntry
}

// LeveEntry 表示单条理符任务的完整解析数据。
type LeveEntry struct {
	ID                      int
	NameCn                  string
	NameZh                  string
	NameEn                  string
	NameJa                  string
	DescriptionCn           string
	IconCityState           int
	IconIssuer              int
	LeveKind                string // craft|gathering|battle|company
	LeveKindLabel           string
	AssignmentTypeID        int
	AssignmentTypeName      string
	AssignmentTypeIconID    int
	GenreName               string
	TownName                string
	ClientName              string
	ClassJobCategoryName    string
	ClassJobLevel           int
	PlaceNameStart          string
	PlaceNameIssued         string
	PlaceNameStartZone      string
	LevemeteLocation        *MapLocationPoint
	StartLocation           *MapLocationPoint
	TimeLimit               int
	AllowanceCost           int
	CanCancel               bool
	LockedLeve              bool
	ExpReward               int
	ExpFactor               float64
	GilReward               int
	CraftRepeats            int
	TurnInItems             []LeveTurnInItem
	RuleName                string
	GatheringItemNumber     int
	GatheringUseSecondary   bool
	FishingSpotName         string
	SpearfishingPlaceName   string
	GatheringTargetLocation *MapLocationPoint
	Objectives              []string
	CombatTargets           []LeveCombatTarget
	RequiredObjectiveItems  []LeveObjectiveItem
	RewardGroups            []LeveRewardGroup
}

// LeveSearchQuery 理符检索与筛选参数。
type LeveSearchQuery struct {
	Q                string
	Kind             string // all|craft|gathering|battle|company
	AssignmentTypeID int
	MinLevel         int
	MaxLevel         int
	Page             int
	PageSize         int
}

// LeveSearchResponse 理符分页检索结果。
type LeveSearchResponse struct {
	Items      []LeveEntry
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

// CollectableTier 表示单个收藏价值档位及对应奖励。
type CollectableTier struct {
	Label             string // 低档|中档|高档
	MinCollectability int
	RewardItemID      int
	RewardItemName    string
	RewardItemIconID  int
	RewardAmount      int
	ExpRatio          int
	ExpReward         int
	SecondaryItemID   int
	SecondaryItemName string
	SecondaryIconID   int
	SecondaryAmount   int
	GilReward         int
	Satisfaction      int
	Points            int
}

// CollectableTurnInSource 表示物品作为收藏品上交时的交易条目（收藏品商店 / 老主顾 / 重建供货）。
type CollectableTurnInSource struct {
	SourceKind        string // shop|satisfaction|hwd
	SourceLabel       string
	ShopID            int
	ShopName          string
	GroupName         string
	LevelMin          int
	LevelMax          int
	Stars             int
	RequiredQuestID   int
	RequiredQuestName string
	RewardMode        string // scrip|item|supply
	IsBonus           bool
	Tiers             []CollectableTier
	NPCs              []ShopVendorNpc
}

// CollectableRewardSource 表示某道具通过收藏品交易获得的反向来源。
type CollectableRewardSource struct {
	ShopName          string
	TurnInItemID      int
	TurnInItemName    string
	TurnInItemIconID  int
	GroupName         string
	LevelMin          int
	LevelMax          int
	MinCollectability int
	RewardLow         int
	RewardMid         int
	RewardHigh        int
	NPCs              []ShopVendorNpc
}

// ItemQuestSource 表示道具与任务的奖励关联摘要。
type ItemQuestSource struct {
	QuestID    int
	NameCn     string
	GenreName  string
	Level      int
	Count      int
	GroupLabel string
}

// ItemLeveSource 表示道具与理符的关联摘要（交纳委托或奖励掉落）。
type ItemLeveSource struct {
	LeveID             int
	NameCn             string
	AssignmentTypeName string
	Level              int
	Count              int
	ProbabilityPercent int
	IsTurnIn           bool
}

// AchievementEntry 表示单条成就及关联称号/奖励道具解析结果。
type AchievementEntry struct {
	ID               int
	NameCn           string
	NameZh           string
	NameEn           string
	NameJa           string
	DescriptionCn    string
	DescriptionEn    string
	DescriptionJa    string
	Points           int
	IconID           int
	KindID           int
	KindName         string
	CategoryID       int
	CategoryName     string
	TitleID          int
	TitleMasculineCn string
	TitleFeminineCn  string
	TitleNameEn      string
	TitleNameJa      string
	TitleIsPrefix    bool
	RewardItemID     int
	RewardItemNameCn string
	RewardItemIconID int
}

// AchievementSearchQuery 成就与称号检索条件。
type AchievementSearchQuery struct {
	Q          string
	KindID     int
	CategoryID int
	Filter     string // "all" | "with_title" | "with_item"
	Page       int
	PageSize   int
}

// AchievementSearchResponse 成就检索分页结果。
type AchievementSearchResponse struct {
	Items      []AchievementEntry
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

// ItemSummary 道具列表摘要信息。
type ItemSummary struct {
	ID                  int
	NameCn              string
	NameZh              string
	NameEn              string
	NameJa              string
	IconID              int
	ItemLevel           int
	EquipLevel          int
	Rarity              int
	CategoryID          int
	CategoryName        string
	CategoryIconID      int
	PriceMid            int
	CanBeHQ             bool
	IsTradable          bool
	IsMarketable        bool
	IsCraftable         bool
	IsGatherable        bool
	IsPurchasable       bool
	IsGilShop           bool
	IsSpecialShop       bool
	IsRetainerVenture   bool
	IsCollectableTurnIn bool
	IsCollectableReward bool
	HasEquipmentStats   bool
	UsedInRecipesCount  int
}

// ItemSearchQuery 道具多条件筛选与分页参数。
type ItemSearchQuery struct {
	Q            string
	CategoryID   int
	Filter       string // all|craftable|gatherable|purchasable|gil_shop|special_shop|retainer_venture|collectable_turnin|equipment|consumable
	CraftTypeID  *int
	MinIlvl      int
	MaxIlvl      int
	TradableOnly bool
	Sort         string // id_asc|id_desc|ilvl_desc|ilvl_asc|level_desc
	Page         int
	PageSize     int
}

// ItemSearchResponse 道具检索分页结果。
type ItemSearchResponse struct {
	Items      []ItemSummary
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

// EquipmentStatBonus 单条装备副属性加成。
type EquipmentStatBonus struct {
	ParamID int
	Name    string
	ValueNQ int
	ValueHQ int
}

// EquipmentStats 武器/防具/饰品基础性能与副属性。
type EquipmentStats struct {
	WeaponKind                 string // physical|magical|shield|none
	DamagePhys                 int
	DamagePhysHQ               int
	DamageMag                  int
	DamageMagHQ                int
	AutoAttack                 float64
	AutoAttackHQ               float64
	DelaySec                   float64
	BlockRate                  int
	BlockRateHQ                int
	Block                      int
	BlockHQ                    int
	DefensePhys                int
	DefensePhysHQ              int
	DefenseMag                 int
	DefenseMagHQ               int
	MateriaSlotCount           int
	IsAdvancedMeldingPermitted bool
	Bonuses                    []EquipmentStatBonus
}

// EquipmentAttributes 装备附加特性（魔晶石精制、投影、分解、染色、部队徽记、投影台、收藏柜）。
type EquipmentAttributes struct {
	CanExtractMateria        bool
	IsProjectable            bool
	CanDesynth               bool
	DesynthClassJobName      string
	DyeCount                 int
	IsCrestWorthy            bool
	CanStoreInGlamourDresser bool
	CanStoreInArmoire        bool
}

// ConsumableParamBonus 食品或爆发药单条属性加成。
type ConsumableParamBonus struct {
	ParamID    int
	Name       string
	IsRelative bool
	ValueNQ    int
	MaxNQ      int
	ValueHQ    int
	MaxHQ      int
}

// ConsumableEffect 食品与药水使用效果明细。
type ConsumableEffect struct {
	TypeName        string
	DurationSec     int
	CooldownSec     int
	CooldownHQSec   int
	ExpBonusPercent int
	Params          []ConsumableParamBonus
}

// RecipeIngredient 配方原料或水晶项。
type RecipeIngredient struct {
	ItemID       int
	NameCn       string
	NameEn       string
	IconID       int
	Amount       int
	ItemLevel    int
	IsCraftable  bool
	IsGatherable bool
}

// RecipeDetail 单条能工巧匠配方完整参数。
type RecipeDetail struct {
	ID                       int
	CraftTypeID              int
	CraftTypeName            string
	CraftJobName             string
	CraftJobIconID           int
	ResultItemID             int
	ResultAmount             int
	RecipeLevel              int
	Stars                    int
	Difficulty               int
	Quality                  int
	Durability               int
	SuggestedCraftsmanship   int
	RequiredCraftsmanship    int
	RequiredControl          int
	MaterialQualityFactor    int
	SecretRecipeBookName     string
	IsSpecializationRequired bool
	IsExpert                 bool
	CanQuickSynth            bool
	CanHQ                    bool
	Ingredients              []RecipeIngredient
	Crystals                 []RecipeIngredient
}

// RecipeCrystalNode 配方树中的水晶媒介条目。
type RecipeCrystalNode struct {
	ItemID int
	NameCn string
	IconID int
	Amount int
}

// RecipeTreeNode 递归子配方工序树节点。
type RecipeTreeNode struct {
	ItemID         int
	NameCn         string
	IconID         int
	AmountPerCraft int
	YieldPerCraft  int
	CraftJobName   string
	CraftJobIconID int
	RecipeLevel    int
	Stars          int
	IsGatherable   bool
	Children       []RecipeTreeNode
	Crystals       []RecipeCrystalNode
}

// UsedInRecipeItem 作为原料参与制作的下游配方项。
type UsedInRecipeItem struct {
	RecipeID        int
	ResultItemID    int
	ResultNameCn    string
	ResultIconID    int
	ResultItemLevel int
	AmountRequired  int
	CraftJobName    string
	CraftJobIconID  int
	RecipeLevel     int
	Stars           int
}

// GatheringTimeWindow 限时采集点的艾欧泽亚出现窗口。
type GatheringTimeWindow struct {
	StartET          string
	EndET            string
	StartMinuteOfDay int
	DurationMinutes  int
}

// GatheringNodeInfo 采矿工与园艺工采集点明细。
type GatheringNodeInfo struct {
	PointID             int
	BaseID              int
	GatheringTypeID     int
	GatheringTypeName   string
	DisciplineName      string
	DisciplineIconID    int
	GatheringTypeIconID int
	NodeLevel           int
	ItemLevel           int
	Stars               int
	IsHidden            bool
	RegionName          string
	ZoneName            string
	PlaceName           string
	TerritoryID         int
	MapID               int
	MapPath             string
	SizeFactor          int
	CoordX              float64
	CoordY              float64
	NormX               float64
	NormY               float64
	Radius              int
	FolkloreBookName    string
	IsTimed             bool
	IsEphemeral         bool
	TimeWindows         []GatheringTimeWindow
}

// FishingSpotInfo 捕鱼人垂钓钓场明细。
type FishingSpotInfo struct {
	SpotID           int
	Level            int
	CategoryName     string
	RegionName       string
	ZoneName         string
	PlaceName        string
	TerritoryID      int
	MapID            int
	MapPath          string
	SizeFactor       int
	CoordX           float64
	CoordY           float64
	NormX            float64
	NormY            float64
	Radius           int
	FishGuideText    string
	FolkloreBookName string
}

// SpearfishingInfo 捕鱼人刺鱼渔场明细。
type SpearfishingInfo struct {
	NotebookID  int
	Level       int
	ItemLevel   int
	RegionName  string
	ZoneName    string
	PlaceName   string
	TerritoryID int
	MapID       int
	MapPath     string
	SizeFactor  int
	CoordX      float64
	CoordY      float64
	NormX       float64
	NormY       float64
	Radius      int
	Description string
}

// GatheringMethodDetail 采集与捕鱼途径汇总。
type GatheringMethodDetail struct {
	Nodes        []GatheringNodeInfo
	FishingSpots []FishingSpotInfo
	Spearfishing []SpearfishingInfo
}

// ShopCostItem 商店兑换消耗货币或道具。
type ShopCostItem struct {
	ItemID         int
	NameCn         string
	IconID         int
	Amount         int
	Collectability int
}

// ShopVendorNpc 售卖 NPC 及其地图坐标。
type ShopVendorNpc struct {
	NpcID       int
	NpcName     string
	NpcTitle    string
	RegionName  string
	ZoneName    string
	PlaceName   string
	TerritoryID int
	MapID       int
	MapPath     string
	SizeFactor  int
	CoordX      float64
	CoordY      float64
	NormX       float64
	NormY       float64
}

// ShopPurchaseSource NPC 商店购买或特殊兑换途径。
type ShopPurchaseSource struct {
	ShopID        int
	ShopType      string // gil|special|gc|fcc
	ShopTypeLabel string
	ShopName      string
	ReceiveAmount int
	ReceiveHQ     bool
	Costs         []ShopCostItem
	NPCs          []ShopVendorNpc
}

// RetainerVentureTier 雇员探险筹集委托单档位门槛与产出数量。
type RetainerVentureTier struct {
	StatType     string // ilvl|gathering|perception
	RequiredStat int
	Quantity     int
}

// RetainerVentureSource 雇员探险筹集委托详情。
type RetainerVentureSource struct {
	TaskID               int
	TaskName             string
	VentureType          string // hunt|mining|botany|fishing
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

// ItemDetail 单个道具的完整解析详情。
type ItemDetail struct {
	ItemSummary
	DescriptionCn            string
	DescriptionEn            string
	DescriptionJa            string
	StackSize                int
	IsUnique                 bool
	IsIndisposable           bool
	IsCollectable            bool
	DyeCount                 int
	CanDesynth               bool
	CanAetherialReduce       bool
	IsGlamourous             bool
	PriceLow                 int
	IsSellable               bool
	IsEquipment              bool
	ClassJobCategoryName     string
	EquipSlotName            string
	RepairClassJobName       string
	RepairItemName           string
	EquipmentStats           *EquipmentStats
	EquipmentAttributes      *EquipmentAttributes
	ConsumableEffect         *ConsumableEffect
	Recipes                  []RecipeDetail
	RecipeTrees              []RecipeTreeNode
	UsedInRecipes            []UsedInRecipeItem
	Gathering                GatheringMethodDetail
	Shops                    []ShopPurchaseSource
	RetainerVentures         []RetainerVentureSource
	CollectableTurnIns       []CollectableTurnInSource
	CollectableRewards       []CollectableRewardSource
	RewardedFromQuests       []ItemQuestSource
	TurnInForLeves           []ItemLeveSource
	RewardedFromLeves        []ItemLeveSource
	RewardedFromAchievements []AchievementEntry
}

// GlobalSearchHitKind 标识全局搜索精准命中的所属模块。
type GlobalSearchHitKind string

const (
	GlobalHitItem        GlobalSearchHitKind = "item"
	GlobalHitQuest       GlobalSearchHitKind = "quest"
	GlobalHitLeve        GlobalSearchHitKind = "leve"
	GlobalHitAchievement GlobalSearchHitKind = "achievement"
)

// GlobalSearchHit 表示一条全局名称精准匹配结果。
type GlobalSearchHit struct {
	Kind         GlobalSearchHitKind
	ID           int
	Title        string
	MatchedAlias string
	Category     string
	ItemLevel    int
	Level        int
	Points       int
	IconID       int
	RewardItemID int
	matchRank    int
	sortLevel    int
}

// GlobalSearchResult 汇总各模块的精准匹配命中结果。
type GlobalSearchResult struct {
	Query        string
	Hits         []GlobalSearchHit
	TotalMatched int
}
