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

// AchievementKindInfo 表示成就大类统计信息。
type AchievementKindInfo struct {
	ID    int
	Name  string
	Count int
}

// SystemStats 汇总当前加载的客户端数据规模指标。
type SystemStats struct {
	TotalItems           int
	CraftableItems       int
	GatherableItems      int
	PurchasableItems     int
	GilShopItems         int
	SpecialShopItems     int
	RetainerVentureItems int
	TotalRecipes         int
	TotalGatheringPoints int
	TotalFishingSpots    int
	TotalShops           int
	TotalRetainerTasks   int
	TotalAchievements    int
	TotalTitles          int
	LoadTimeMs           int64
}

// SystemStatus 描述当前已挂载客户端版本、路径与各类汇总统计。
type SystemStatus struct {
	HasCN            bool
	HasGlobal        bool
	CNVersion        string
	GlobalVersion    string
	PrimaryVersion   string
	CNPath           string
	GlobalPath       string
	Stats            SystemStats
	Categories       []ItemCategoryInfo
	CraftTypes       []CraftTypeInfo
	AchievementKinds []AchievementKindInfo
}

// AchievementEntry 表示单条成就及关联称号/奖励道具解析结果。
type AchievementEntry struct {
	ID               int
	NameCn           string
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
	Q        string
	KindID   int
	Filter   string // "all" | "with_title" | "with_item"
	Page     int
	PageSize int
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
	ID                 int
	NameCn             string
	NameEn             string
	NameJa             string
	IconID             int
	ItemLevel          int
	EquipLevel         int
	Rarity             int
	CategoryID         int
	CategoryName       string
	CategoryIconID     int
	PriceMid           int
	CanBeHQ            bool
	IsTradable         bool
	IsMarketable       bool
	IsCraftable        bool
	IsGatherable       bool
	IsPurchasable      bool
	IsGilShop          bool
	IsSpecialShop      bool
	IsRetainerVenture  bool
	HasEquipmentStats  bool
	UsedInRecipesCount int
}

// ItemSearchQuery 道具多条件筛选与分页参数。
type ItemSearchQuery struct {
	Q            string
	CategoryID   int
	Filter       string // all|craftable|gatherable|purchasable|gil_shop|special_shop|retainer_venture|equipment|consumable
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
	RewardedFromAchievements []AchievementEntry
}
