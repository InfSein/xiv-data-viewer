package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

// LiveVersionID 表示当前已加载的本地 FF14 客户端（不落盘生成本地快照，直接以内存实时数据参与对比）。
const LiveVersionID = "__live__"

// ErrSnapshotNotDownloaded 表示请求的对比目标快照尚未从 GitHub 下载到本地。
var ErrSnapshotNotDownloaded = errors.New("所选历史版本在该区服下的快照尚未下载到本地")

// VersionBackupMeta 描述面向用户的统一版本条目（如 "7.58" 或当前客户端），内部自动涵盖国服/国际服下载状态。
type VersionBackupMeta struct {
	ID               string `json:"id"` // "__live__" 或统一版本号（如 "7.58"）
	Patch            string `json:"patch,omitempty"`
	Version          string `json:"version,omitempty"` // 当前对比区服对应的内部构建号
	CNVersion        string `json:"cnVersion,omitempty"`
	GlobalVersion    string `json:"globalVersion,omitempty"`
	DisplayName      string `json:"displayName"`
	CreatedAt        string `json:"createdAt,omitempty"`
	IsLive           bool   `json:"isLive,omitempty"`
	HasRemoteCN      bool   `json:"hasRemoteCn,omitempty"`
	HasRemoteGlobal  bool   `json:"hasRemoteGlobal,omitempty"`
	DownloadedCN     bool   `json:"downloadedCn,omitempty"`
	DownloadedGlobal bool   `json:"downloadedGlobal,omitempty"`
	Downloaded       bool   `json:"downloaded,omitempty"`
	NeedsSupplement  bool   `json:"needsSupplement,omitempty"`
	Size             int64  `json:"size,omitempty"`
	DownloadSize     int64  `json:"downloadSize,omitempty"`
}

// VersionStatusResponse 汇总双服当前客户端识别状态、各区服可用对比目标与统一版本目录。
type VersionStatusResponse struct {
	HasCN              bool                `json:"hasCn"`
	HasGlobal          bool                `json:"hasGlobal"`
	ConfiguredRegions  []string            `json:"configuredRegions"`
	DefaultRegion      string              `json:"defaultRegion"`
	CNVersion          string              `json:"cnVersion,omitempty"`
	CNPatch            string              `json:"cnPatch,omitempty"`
	CNDisplayName      string              `json:"cnDisplayName,omitempty"`
	GlobalVersion      string              `json:"globalVersion,omitempty"`
	GlobalPatch        string              `json:"globalPatch,omitempty"`
	GlobalDisplayName  string              `json:"globalDisplayName,omitempty"`
	CurrentVersion     string              `json:"currentVersion,omitempty"`
	CurrentPatch       string              `json:"currentPatch,omitempty"`
	CurrentDisplayName string              `json:"currentDisplayName,omitempty"`
	CNBackups          []VersionBackupMeta `json:"cnBackups"`
	GlobalBackups      []VersionBackupMeta `json:"globalBackups"`
	DefaultCNFrom      string              `json:"defaultCnFrom,omitempty"`
	DefaultCNTo        string              `json:"defaultCnTo,omitempty"`
	DefaultGlobalFrom  string              `json:"defaultGlobalFrom,omitempty"`
	DefaultGlobalTo    string              `json:"defaultGlobalTo,omitempty"`
	RemoteVersions     []VersionBackupMeta `json:"remoteVersions"` // 快照管理弹窗展示的统一版本清单（不区分国服/国际服）
	SnapshotDir        string              `json:"snapshotDir"`
	CatalogLoaded      bool                `json:"catalogLoaded"`
	CatalogUpdatedAt   string              `json:"catalogUpdatedAt,omitempty"`
	CatalogError       string              `json:"catalogError,omitempty"`
}

// BackupsForRegion 返回指定区服（"cn" / "global"）可供版本对比下拉选择的版本列表。
func (r VersionStatusResponse) BackupsForRegion(region string) []VersionBackupMeta {
	if snapshot.NormalizeRegion(region) == snapshot.RegionGlobal {
		return r.GlobalBackups
	}
	return r.CNBackups
}

// DefaultPairForRegion 返回指定区服默认的（旧版本、新版本）对比组合。
func (r VersionStatusResponse) DefaultPairForRegion(region string) (from, to string) {
	if snapshot.NormalizeRegion(region) == snapshot.RegionGlobal {
		return r.DefaultGlobalFrom, r.DefaultGlobalTo
	}
	return r.DefaultCNFrom, r.DefaultCNTo
}

// CurrentClientLabelForRegion 返回指定区服当前本地客户端的人类可读名称。
func (r VersionStatusResponse) CurrentClientLabelForRegion(region string) string {
	if snapshot.NormalizeRegion(region) == snapshot.RegionGlobal {
		return r.GlobalDisplayName
	}
	return r.CNDisplayName
}

// ItemFieldChange 描述版本对比中单个字段的变更前后对比。
type ItemFieldChange struct {
	Field  string `json:"field"`
	Label  string `json:"label"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// DiffItemEntry 描述版本对比结果中的单条道具、任务、理符、成就、音乐、图标或文本差异记录。
type DiffItemEntry struct {
	ID                   int               `json:"id"`
	EntityType           string            `json:"entityType"` // item|quest|leve|achievement|bgm|icon|text
	ChangeType           string            `json:"changeType"` // added|modified|removed
	NameCn               string            `json:"nameCn"`
	NameEn               string            `json:"nameEn"`
	NameJa               string            `json:"nameJa,omitempty"`
	IconID               int               `json:"iconId"`
	ItemLevel            int               `json:"itemLevel"`
	EquipLevel           int               `json:"equipLevel"`
	CategoryName         string            `json:"categoryName"`
	File                 string            `json:"file,omitempty"`
	PriceMid             int               `json:"priceMid"`
	IsCraftable          bool              `json:"isCraftable"`
	IsGatherable         bool              `json:"isGatherable"`
	IsPurchasable        bool              `json:"isPurchasable"`
	HasCollectableChange bool              `json:"hasCollectableChange,omitempty"`
	Points               int               `json:"points"`
	DescriptionCn        string            `json:"descriptionCn,omitempty"`
	AchKindName          string            `json:"achKindName,omitempty"`
	AchCategoryName      string            `json:"achCategoryName,omitempty"`
	TitleID              int               `json:"titleId,omitempty"`
	TitleNameCn          string            `json:"titleNameCn"`
	TitleMasculineCn     string            `json:"titleMasculineCn,omitempty"`
	TitleFeminineCn      string            `json:"titleFeminineCn,omitempty"`
	TitleNameJa          string            `json:"titleNameJa,omitempty"`
	TitleNameEn          string            `json:"titleNameEn,omitempty"`
	RewardItemID         int               `json:"rewardItemId"`
	RewardItemNameCn     string            `json:"rewardItemNameCn"`
	RewardItemIconID     int               `json:"rewardItemIconId,omitempty"`
	IconVariantSummary   string            `json:"iconVariantSummary,omitempty"`
	OldBlobKey           string            `json:"oldBlobKey,omitempty"`
	NewBlobKey           string            `json:"newBlobKey,omitempty"`
	OldThumbPNG          []byte            `json:"oldThumbPng,omitempty"`
	NewThumbPNG          []byte            `json:"newThumbPng,omitempty"`
	TextSheet            string            `json:"textSheet,omitempty"`
	TextSubRow           int               `json:"textSubRow,omitempty"`
	TextColIdx           int               `json:"textColIdx,omitempty"`
	TextColName          string            `json:"textColName,omitempty"`
	TextBefore           string            `json:"textBefore,omitempty"`
	TextAfter            string            `json:"textAfter,omitempty"`
	TextJumpKind         string            `json:"textJumpKind,omitempty"`
	TextJumpID           int               `json:"textJumpId,omitempty"`
	Changes              []ItemFieldChange `json:"changes"`
}

// DiffSummary 汇总两个版本之间的变更统计数字。
type DiffSummary struct {
	AddedItems                int  `json:"addedItems"`
	ModifiedItems             int  `json:"modifiedItems"`
	RemovedItems              int  `json:"removedItems"`
	AddedRecipes              int  `json:"addedRecipes"`
	NewCraftableItems         int  `json:"newCraftableItems"`
	NewGatherableItems        int  `json:"newGatherableItems"`
	NewPurchasableItems       int  `json:"newPurchasableItems"`
	NewCollectableTurnInItems int  `json:"newCollectableTurnInItems"`
	ModifiedCollectableItems  int  `json:"modifiedCollectableItems"`
	AddedQuests               int  `json:"addedQuests"`
	ModifiedQuests            int  `json:"modifiedQuests"`
	RemovedQuests             int  `json:"removedQuests"`
	AddedLeves                int  `json:"addedLeves"`
	ModifiedLeves             int  `json:"modifiedLeves"`
	RemovedLeves              int  `json:"removedLeves"`
	AddedAchievements         int  `json:"addedAchievements"`
	ModifiedAchievements      int  `json:"modifiedAchievements"`
	RemovedAchievements       int  `json:"removedAchievements"`
	AddedTitles               int  `json:"addedTitles"`
	AddedBGMs                 int  `json:"addedBgms"`
	ModifiedBGMs              int  `json:"modifiedBgms"`
	RemovedBGMs               int  `json:"removedBgms"`
	AddedIcons                int  `json:"addedIcons"`
	ModifiedIcons             int  `json:"modifiedIcons"`
	RemovedIcons              int  `json:"removedIcons"`
	IconDiffAvailable         bool `json:"iconDiffAvailable"`
	IconMissingFrom           bool `json:"iconMissingFrom,omitempty"`
	IconMissingTo             bool `json:"iconMissingTo,omitempty"`
	ModifiedTexts             int  `json:"modifiedTexts"`
	RemovedTexts              int  `json:"removedTexts"`
	TextDiffAvailable         bool `json:"textDiffAvailable"`
}

// VersionDiffQuery 版本差异检索条件。
type VersionDiffQuery struct {
	Region     string // "cn" | "global"（留空默认取当前主区服，保证国服对比国服、国际服对比国际服）
	From       string // 统一版本号（如 "7.55"）或 LiveVersionID
	To         string // 统一版本号（如 "7.58"）或 LiveVersionID
	Filter     string // all|added|modified|removed
	EntityType string // all|item|quest|leve|achievement|bgm|icon|text
	Q          string
	Page       int
	PageSize   int
}

// VersionDiffResponse 版本对比分页结果。
type VersionDiffResponse struct {
	NeedsDownload   bool            `json:"needsDownload,omitempty"`
	Region          string          `json:"region"`
	FromID          string          `json:"fromId"`
	ToID            string          `json:"toId"`
	FromVersion     string          `json:"fromVersion"`
	ToVersion       string          `json:"toVersion"`
	FromDisplayName string          `json:"fromDisplayName"`
	ToDisplayName   string          `json:"toDisplayName"`
	FromCreatedAt   string          `json:"fromCreatedAt"`
	ToCreatedAt     string          `json:"toCreatedAt"`
	Summary         DiffSummary     `json:"summary"`
	Items           []DiffItemEntry `json:"items"`
	TotalFiltered   int             `json:"totalFiltered"`
	Page            int             `json:"page"`
	PageSize        int             `json:"pageSize"`
	TotalPages      int             `json:"totalPages"`
}

// MemorySnapshot 允许单元测试注册内存虚拟快照（按 Region + Patch 注册），在不依赖真实客户端文件时验证全链路对比逻辑。
type MemorySnapshot struct {
	Region    string // "cn" | "global"
	Patch     string // 如 "7.55"
	Version   string // 内部构建号，如 "2026.10.01.0000.0000"
	CreatedAt string
	Store     *gamedata.Store
	Icons     []snapshot.IconDigestRecord
	IconBlobs map[string][]byte
	Texts     []TextSnapshotRecord
}

type compactItemRecord struct {
	ID                  int
	NameCn              string
	NameEn              string
	NameJa              string
	IconID              int
	ItemLevel           int
	EquipLevel          int
	CategoryID          int
	CategoryName        string
	PriceMid            int
	IsCraftable         bool
	IsGatherable        bool
	IsPurchasable       bool
	IsRetainerVenture   *bool
	IsCollectableTurnIn *bool
	IsCollectableReward *bool
	CollectableSummary  string
}

type compactQuestRecord struct {
	ID              int
	NameCn          string
	NameEn          string
	NameJa          string
	IconID          int
	Level           int
	GenreName       string
	SectionName     string
	ExpansionName   string
	IssuerName      string
	GilReward       int
	ExpReward       int
	RewardSummary   string
	PrimaryItemID   int
	IsMSQ           bool
	IsFeature       bool
	IsRepeatable    bool
	CutsceneCount   *int
	CutsceneSummary string
}

type compactLeveRecord struct {
	ID                 int
	NameCn             string
	NameEn             string
	NameJa             string
	IconID             int
	LeveKind           string
	LeveKindLabel      string
	AssignmentTypeName string
	ClassJobLevel      int
	ExpReward          int
	GilReward          int
	CraftRepeats       int
	TurnInSummary      string
	RewardSummary      string
}

type compactAchievementRecord struct {
	ID               int
	NameCn           string
	NameEn           string
	NameJa           string
	DescriptionCn    string
	Points           int
	IconID           int
	KindName         string
	CategoryName     string
	TitleID          int
	TitleMasculineCn string
	TitleFeminineCn  string
	TitleNameJa      string
	TitleNameEn      string
	RewardItemID     int
	RewardItemNameCn string
	RewardItemIconID int
}

type compactBGMRecord struct {
	ID            int
	File          string
	Title         string
	NameCn        string
	NameEn        string
	NameJa        string
	ExpansionID   int
	ExpansionName string
	UsageSummary  string
	IsPlaceholder bool
	FileExists    bool
	FileSize      uint32
	ContentMD5    string
}

type snapshotPayload struct {
	Meta         VersionBackupMeta
	RecipeCount  int
	Items        []compactItemRecord
	Quests       []compactQuestRecord
	Leves        []compactLeveRecord
	Achievements []compactAchievementRecord
	BGMs         []compactBGMRecord
	Icons        []compactIconRecord
}

type parsedSnapCacheEntry struct {
	payload *snapshotPayload
	store   *gamedata.Store
	seq     uint64
}

// Service 管理 GitHub 统一版本快照目录、按区服自动下载缓存以及严格同服（国服对国服、国际服对国际服）的版本对比计算。
type Service struct {
	store          *gamedata.Store
	snapshotDir    string
	snapshotClient *snapshot.Client

	mu                 sync.Mutex
	uiLang             i18n.Lang
	catalog            snapshot.Catalog
	catalogLoaded      bool
	catalogErr         string
	localSnaps         map[string]snapshot.LocalSnapshotInfo // key: "cn-7.58" / "global-7.58"
	memSnaps           map[string]*MemorySnapshot            // key: "cn-7.58" / "global-7.58"
	memLiveStores      map[string]*gamedata.Store            // key: "cn" / "global"
	memLiveTexts       map[string][]textSheetRows            // key: "cn" / "global"
	memLiveIcons       map[string][]compactIconRecord        // key: "cn" / "global"
	memBlobs           map[string][]byte
	archives           map[string]*snapshot.Archive     // key: "cn-7.58" / "global-7.58"
	liveRegionStores   map[string]*gamedata.Store       // 双服模式下按需加载的副区服实时 Store 缓存
	parsedCache        map[string]*parsedSnapCacheEntry // key: region|target|uiLang
	parsedSeq          uint64
	liveIconCache      map[string][]compactIconRecord // key: region
	liveIconComputedAt map[string]bool                // key: region
	latestAddCache     *cachedLatestAdditions

	textMu        sync.Mutex
	textCache     map[string][]textSheetRows
	textCacheSeq  map[string]uint64
	textSeq       uint64
	textDiffCache *cachedTextDiff
}

// NewService 创建基于 GitHub 统一版本快照的版本对比服务（不在本地自建任何游戏数据快照）。
func NewService(store *gamedata.Store, snapshotDir string) *Service {
	lang := i18n.LangZH
	if store != nil && store.AppLang != "" {
		lang = i18n.NormalizeLang(string(store.AppLang))
	}
	s := &Service{
		store:              store,
		snapshotDir:        snapshotDir,
		snapshotClient:     snapshot.NewClient(snapshot.ClientOptions{}),
		uiLang:             lang,
		localSnaps:         make(map[string]snapshot.LocalSnapshotInfo),
		memSnaps:           make(map[string]*MemorySnapshot),
		memLiveStores:      make(map[string]*gamedata.Store),
		memLiveTexts:       make(map[string][]textSheetRows),
		memLiveIcons:       make(map[string][]compactIconRecord),
		memBlobs:           make(map[string][]byte),
		archives:           make(map[string]*snapshot.Archive),
		liveRegionStores:   make(map[string]*gamedata.Store),
		parsedCache:        make(map[string]*parsedSnapCacheEntry),
		liveIconCache:      make(map[string][]compactIconRecord),
		liveIconComputedAt: make(map[string]bool),
		textCache:          make(map[string][]textSheetRows),
		textCacheSeq:       make(map[string]uint64),
	}
	if cachedCat, ok := snapshot.LoadCachedCatalog(snapshotDir); ok {
		s.catalog = cachedCat
		s.catalogLoaded = true
	}
	s.reloadLocalLocked()
	return s
}

// SetSnapshotClient 替换快照目录与下载客户端（供单元测试注入本地模拟服务）。
func (s *Service) SetSnapshotClient(client *snapshot.Client) {
	if s == nil || client == nil {
		return
	}
	s.mu.Lock()
	s.snapshotClient = client
	s.mu.Unlock()
}

// SetLang 切换版本对比展示标签与字段变更文案使用的界面语言。
func (s *Service) SetLang(lang i18n.Lang) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.uiLang = i18n.NormalizeLang(string(lang))
	// 切换界面语言时，双服实时 Store 的合成文案语言（以及国际服主数据语言）可能变化，统一清空按需重建
	clear(s.liveRegionStores)
	s.mu.Unlock()
}

// Close 释放所有已打开的 .xivsnap 归档文件句柄与内存缓存。
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	for id, arch := range s.archives {
		_ = arch.Close()
		delete(s.archives, id)
	}
	clear(s.parsedCache)
	clear(s.liveRegionStores)
	s.latestAddCache = nil
	s.mu.Unlock()

	s.textMu.Lock()
	clear(s.textCache)
	clear(s.textCacheSeq)
	s.textDiffCache = nil
	s.textMu.Unlock()
}

// RegisterMemorySnapshot 注册一个按区服+统一版本号索引的内存虚拟快照（供单元测试验证 Diff，不落盘）。
func (s *Service) RegisterMemorySnapshot(ms MemorySnapshot) {
	if s == nil || strings.TrimSpace(ms.Patch) == "" {
		return
	}
	s.mu.Lock()
	reg := snapshot.NormalizeRegion(ms.Region)
	if strings.TrimSpace(ms.Region) == "" {
		reg = s.defaultRegionLocked()
	}
	cp := ms
	cp.Region = reg
	cp.Patch = strings.TrimSpace(ms.Patch)
	snapID := snapshot.MakeSnapshotID(reg, cp.Patch)
	s.memSnaps[snapID] = &cp
	for k, v := range cp.IconBlobs {
		if k != "" && len(v) > 0 {
			s.memBlobs[k] = append([]byte(nil), v...)
		}
	}
	s.invalidatePatchCachesLocked(cp.Patch)
	s.mu.Unlock()
}

// SetLiveMemoryStore 为双服单元测试注入指定区服的当前客户端内存 Store。
func (s *Service) SetLiveMemoryStore(region string, store *gamedata.Store) {
	if s == nil || store == nil {
		return
	}
	reg := snapshot.NormalizeRegion(region)
	s.mu.Lock()
	s.memLiveStores[reg] = store
	s.invalidateLiveRegionCacheLocked(reg)
	s.mu.Unlock()
}

// SetLiveMemoryTexts 为内存单元测试中的当前客户端默认区服注入文本记录。
func (s *Service) SetLiveMemoryTexts(records []TextSnapshotRecord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	reg := s.defaultRegionLocked()
	s.mu.Unlock()
	s.SetLiveMemoryTextsForRegion(reg, records)
}

// SetLiveMemoryTextsForRegion 为内存单元测试中的指定区服当前客户端注入文本记录。
func (s *Service) SetLiveMemoryTextsForRegion(region string, records []TextSnapshotRecord) {
	if s == nil {
		return
	}
	reg := snapshot.NormalizeRegion(region)
	sheets := buildTextSheetRows(records)
	s.mu.Lock()
	s.memLiveTexts[reg] = sheets
	s.mu.Unlock()
	s.textMu.Lock()
	s.textDiffCache = nil
	s.textMu.Unlock()
}

// SetLiveMemoryIcons 为内存单元测试中的当前客户端默认区服注入图标指纹与缩略图载荷。
func (s *Service) SetLiveMemoryIcons(records []snapshot.IconDigestRecord, blobs map[string][]byte) {
	if s == nil {
		return
	}
	s.mu.Lock()
	reg := s.defaultRegionLocked()
	s.mu.Unlock()
	s.SetLiveMemoryIconsForRegion(reg, records, blobs)
}

// SetLiveMemoryIconsForRegion 为内存单元测试中的指定区服当前客户端注入图标指纹与缩略图载荷。
func (s *Service) SetLiveMemoryIconsForRegion(region string, records []snapshot.IconDigestRecord, blobs map[string][]byte) {
	if s == nil {
		return
	}
	reg := snapshot.NormalizeRegion(region)
	s.mu.Lock()
	s.memLiveIcons[reg] = convertIconDigests(records)
	for k, v := range blobs {
		if k != "" && len(v) > 0 {
			s.memBlobs[k] = append([]byte(nil), v...)
		}
	}
	s.invalidateLiveRegionCacheLocked(reg)
	s.mu.Unlock()
}

func (s *Service) langLocked() i18n.Lang {
	if s.uiLang == "" {
		return i18n.LangZH
	}
	return s.uiLang
}

func (s *Service) hasCNLocked() bool {
	if s.store == nil {
		return false
	}
	if s.store.HasCN || s.store.CNReader != nil || s.store.CNVersion != "" {
		return true
	}
	if !s.store.HasGlobal && s.store.GlobalReader == nil && s.store.GlobalVersion == "" {
		return true
	}
	return false
}

func (s *Service) hasGlobalLocked() bool {
	if s.store == nil {
		return false
	}
	return s.store.HasGlobal || s.store.GlobalReader != nil || s.store.GlobalVersion != "" || s.memLiveStores[snapshot.RegionGlobal] != nil
}

func (s *Service) configuredRegionsLocked() []string {
	var regs []string
	if s.hasCNLocked() {
		regs = append(regs, snapshot.RegionCN)
	}
	if s.hasGlobalLocked() {
		regs = append(regs, snapshot.RegionGlobal)
	}
	if len(regs) == 0 {
		return []string{snapshot.RegionCN}
	}
	return regs
}

func (s *Service) defaultRegionLocked() string {
	hasCN := s.hasCNLocked()
	hasGL := s.hasGlobalLocked()
	if hasCN && !hasGL {
		return snapshot.RegionCN
	}
	if hasGL && !hasCN {
		return snapshot.RegionGlobal
	}
	if s.store != nil {
		return snapshot.NormalizeRegion(s.store.DataRegion())
	}
	return snapshot.RegionCN
}

func (s *Service) cnVersionLocked() string {
	if ms, ok := s.memLiveStores[snapshot.RegionCN]; ok && ms != nil {
		if ms.CNVersion != "" {
			return ms.CNVersion
		}
		return ms.PrimaryVersion
	}
	if s.store == nil {
		return ""
	}
	if s.store.CNVersion != "" {
		return s.store.CNVersion
	}
	if s.hasCNLocked() {
		return s.store.PrimaryVersion
	}
	return ""
}

func (s *Service) globalVersionLocked() string {
	if ms, ok := s.memLiveStores[snapshot.RegionGlobal]; ok && ms != nil {
		if ms.GlobalVersion != "" {
			return ms.GlobalVersion
		}
		return ms.PrimaryVersion
	}
	if s.store == nil {
		return ""
	}
	if s.store.GlobalVersion != "" {
		return s.store.GlobalVersion
	}
	if s.hasGlobalLocked() && !s.hasCNLocked() {
		return s.store.PrimaryVersion
	}
	return ""
}

func (s *Service) reloadLocalLocked() {
	locals, err := snapshot.ListLocalSnapshots(s.snapshotDir)
	if err != nil {
		return
	}
	next := make(map[string]snapshot.LocalSnapshotInfo, len(locals))
	for _, info := range locals {
		next[info.Meta.ID] = info
	}
	s.localSnaps = next
}

func (s *Service) invalidateLiveRegionCacheLocked(region string) {
	regPrefix := region + "\x00"
	for k := range s.liveRegionStores {
		if strings.HasPrefix(k, regPrefix) {
			delete(s.liveRegionStores, k)
		}
	}
	prefix := region + "\x00" + LiveVersionID + "\x00"
	for k := range s.parsedCache {
		if strings.HasPrefix(k, prefix) {
			delete(s.parsedCache, k)
		}
	}
	delete(s.liveIconCache, region)
	delete(s.liveIconComputedAt, region)
	s.latestAddCache = nil
	s.textMu.Lock()
	for k := range s.textCache {
		if strings.HasPrefix(k, prefix) {
			delete(s.textCache, k)
			delete(s.textCacheSeq, k)
		}
	}
	s.textDiffCache = nil
	s.textMu.Unlock()
}

func (s *Service) invalidatePatchCachesLocked(patch string) {
	s.latestAddCache = nil
	for _, reg := range []string{snapshot.RegionCN, snapshot.RegionGlobal} {
		snapID := snapshot.MakeSnapshotID(reg, patch)
		if arch, ok := s.archives[snapID]; ok {
			_ = arch.Close()
			delete(s.archives, snapID)
		}
		prefix := reg + "\x00" + patch + "\x00"
		for k := range s.parsedCache {
			if strings.HasPrefix(k, prefix) {
				delete(s.parsedCache, k)
			}
		}
		s.textMu.Lock()
		for k := range s.textCache {
			if strings.HasPrefix(k, prefix) {
				delete(s.textCache, k)
				delete(s.textCacheSeq, k)
			}
		}
		s.textDiffCache = nil
		s.textMu.Unlock()
	}
}

// ReloadLocalSnapshots 重新扫描本地 snapshots 目录并返回最新状态。
func (s *Service) ReloadLocalSnapshots() VersionStatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadLocalLocked()
	return s.statusLocked()
}

// GetVersionStatus 返回双服当前客户端映射、各区服可用对比目标与统一版本目录状态。
func (s *Service) GetVersionStatus() VersionStatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

// RefreshCatalog 从 GitHub 拉取最新的统一版本目录清单，自动更新国服与国际服当前客户端版本名映射。
func (s *Service) RefreshCatalog(ctx context.Context) (VersionStatusResponse, error) {
	s.mu.Lock()
	client := s.snapshotClient
	dir := s.snapshotDir
	s.mu.Unlock()

	cat, err := client.FetchCatalog(ctx, dir)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadLocalLocked()
	if err != nil {
		s.catalogErr = err.Error()
		return s.statusLocked(), err
	}
	s.catalog = cat
	s.catalogLoaded = true
	s.catalogErr = ""
	s.latestAddCache = nil
	return s.statusLocked(), nil
}

// DownloadVersion 按用户当前已配置的客户端区服（单国服 / 单国际服 / 双服），一键下载指定统一版本所需的全部快照文件。
func (s *Service) DownloadVersion(
	ctx context.Context,
	patch string,
	onProgress func(downloaded, total int64),
) (VersionStatusResponse, error) {
	cleanPatch := strings.TrimSpace(patch)
	s.mu.Lock()
	client := s.snapshotClient
	dir := s.snapshotDir
	regions := s.configuredRegionsLocked()
	var target *snapshot.VersionCatalogEntry
	for i := range s.catalog.Versions {
		if s.catalog.Versions[i].Patch == cleanPatch {
			cp := s.catalog.Versions[i]
			target = &cp
			break
		}
	}
	s.mu.Unlock()

	if target == nil {
		return s.GetVersionStatus(), fmt.Errorf("未在目录中找到版本 %q", cleanPatch)
	}
	if _, err := client.DownloadVersion(ctx, *target, regions, dir, onProgress); err != nil {
		return s.GetVersionStatus(), err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidatePatchCachesLocked(cleanPatch)
	s.reloadLocalLocked()
	return s.statusLocked(), nil
}

// DeleteVersion 删除本地已下载的指定统一版本（同步清理其国服与国际服子快照）以释放磁盘空间。
func (s *Service) DeleteVersion(patch string) (VersionStatusResponse, error) {
	cleanPatch := strings.TrimSpace(patch)
	if cleanPatch == "" || cleanPatch == LiveVersionID {
		return s.GetVersionStatus(), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidatePatchCachesLocked(cleanPatch)
	if err := snapshot.DeleteLocalVersion(s.snapshotDir, cleanPatch); err != nil {
		return s.statusLocked(), err
	}
	delete(s.memSnaps, snapshot.MakeSnapshotID(snapshot.RegionCN, cleanPatch))
	delete(s.memSnaps, snapshot.MakeSnapshotID(snapshot.RegionGlobal, cleanPatch))
	s.reloadLocalLocked()
	return s.statusLocked(), nil
}

func formatLiveClientDisplayName(lang i18n.Lang, patch, gameVer string) string {
	if patch != "" {
		return i18n.Tf(lang, "diff.live_patch_only", "patch", patch)
	}
	if gameVer != "" {
		return i18n.Tf(lang, "diff.live_with_ver", "version", gameVer)
	}
	return i18n.T(lang, "diff.live_label")
}

func (s *Service) matchLivePatchLocked(region, gameVer string) string {
	if gameVer == "" {
		return ""
	}
	normReg := snapshot.NormalizeRegion(region)
	for _, ver := range s.catalog.Versions {
		if asset := ver.AssetForRegion(normReg); asset != nil && asset.GameVersion == gameVer && ver.Patch != "" {
			return ver.Patch
		}
	}
	for _, info := range s.localSnaps {
		if snapshot.NormalizeRegion(info.Meta.Region) == normReg && info.Meta.GameVersion == gameVer && info.Meta.Patch != "" {
			return info.Meta.Patch
		}
	}
	for _, ms := range s.memSnaps {
		if snapshot.NormalizeRegion(ms.Region) == normReg && ms.Version == gameVer && ms.Patch != "" {
			return ms.Patch
		}
	}
	return ""
}

func (s *Service) hasLocalRegionPatchLocked(region, patch string) (bool, string, string, int64) {
	snapID := snapshot.MakeSnapshotID(region, patch)
	if info, ok := s.localSnaps[snapID]; ok {
		return true, info.Meta.GameVersion, info.Meta.CreatedAt, info.Size
	}
	if ms, ok := s.memSnaps[snapID]; ok {
		return true, ms.Version, ms.CreatedAt, 0
	}
	return false, "", "", 0
}

func (s *Service) statusLocked() VersionStatusResponse {
	lang := s.langLocked()
	hasCN := s.hasCNLocked()
	hasGL := s.hasGlobalLocked()
	cfgRegions := s.configuredRegionsLocked()
	defReg := s.defaultRegionLocked()

	cnVer := s.cnVersionLocked()
	glVer := s.globalVersionLocked()
	cnPatch := ""
	glPatch := ""
	if hasCN {
		cnPatch = s.matchLivePatchLocked(snapshot.RegionCN, cnVer)
	}
	if hasGL {
		glPatch = s.matchLivePatchLocked(snapshot.RegionGlobal, glVer)
	}
	cnDisp := formatLiveClientDisplayName(lang, cnPatch, cnVer)
	glDisp := formatLiveClientDisplayName(lang, glPatch, glVer)

	// 聚合所有统一版本（合并 GitHub 目录与本地离线/测试已下载版本，每版本号仅出现 1 条）
	byPatch := make(map[string]*VersionBackupMeta)
	ensurePatch := func(patch string) *VersionBackupMeta {
		m, ok := byPatch[patch]
		if !ok {
			m = &VersionBackupMeta{
				ID:          patch,
				Patch:       patch,
				DisplayName: patch,
			}
			byPatch[patch] = m
		}
		return m
	}

	for _, ver := range s.catalog.Versions {
		if strings.TrimSpace(ver.Patch) == "" {
			continue
		}
		m := ensurePatch(ver.Patch)
		if ver.CreatedAt > m.CreatedAt {
			m.CreatedAt = ver.CreatedAt
		}
		if ver.CN != nil {
			m.HasRemoteCN = true
			m.CNVersion = ver.CN.GameVersion
		}
		if ver.Global != nil {
			m.HasRemoteGlobal = true
			m.GlobalVersion = ver.Global.GameVersion
		}
	}
	for _, info := range s.localSnaps {
		if strings.TrimSpace(info.Meta.Patch) == "" {
			continue
		}
		m := ensurePatch(info.Meta.Patch)
		if info.Meta.CreatedAt > m.CreatedAt {
			m.CreatedAt = info.Meta.CreatedAt
		}
		if snapshot.NormalizeRegion(info.Meta.Region) == snapshot.RegionGlobal {
			m.DownloadedGlobal = true
			if m.GlobalVersion == "" {
				m.GlobalVersion = info.Meta.GameVersion
			}
		} else {
			m.DownloadedCN = true
			if m.CNVersion == "" {
				m.CNVersion = info.Meta.GameVersion
			}
		}
	}
	for _, ms := range s.memSnaps {
		if strings.TrimSpace(ms.Patch) == "" {
			continue
		}
		m := ensurePatch(ms.Patch)
		if ms.CreatedAt > m.CreatedAt {
			m.CreatedAt = ms.CreatedAt
		}
		if ms.Region == snapshot.RegionGlobal {
			m.DownloadedGlobal = true
			if m.GlobalVersion == "" {
				m.GlobalVersion = ms.Version
			}
		} else {
			m.DownloadedCN = true
			if m.CNVersion == "" {
				m.CNVersion = ms.Version
			}
		}
	}

	// 计算每个统一版本针对用户当前已配置区服的下载状态、总大小与待补全字节数
	catByPatch := make(map[string]snapshot.VersionCatalogEntry, len(s.catalog.Versions))
	for _, v := range s.catalog.Versions {
		catByPatch[v.Patch] = v
	}

	remoteList := make([]VersionBackupMeta, 0, len(byPatch))
	for patch, m := range byPatch {
		var totalSz, remainSz int64
		needCnt := 0
		haveCnt := 0
		catEntry, hasCat := catByPatch[patch]
		for _, reg := range cfgRegions {
			dlLocal, _, _, localSz := s.hasLocalRegionPatchLocked(reg, patch)
			var remAsset *snapshot.RegionAsset
			if hasCat {
				remAsset = catEntry.AssetForRegion(reg)
			}
			if dlLocal || remAsset != nil {
				needCnt++
			}
			if dlLocal {
				haveCnt++
				if localSz > 0 {
					totalSz += localSz
				} else if remAsset != nil {
					totalSz += remAsset.Size
				}
			} else if remAsset != nil {
				totalSz += remAsset.Size
				remainSz += remAsset.Size
			}
		}
		// 若目录中仅含用户未配置的区服，仍统计其远端总大小以展示元信息
		if totalSz == 0 && hasCat {
			totalSz = catEntry.TotalSizeForRegions([]string{snapshot.RegionCN, snapshot.RegionGlobal})
		}
		m.Size = totalSz
		m.DownloadSize = remainSz
		m.Downloaded = needCnt > 0 && haveCnt == needCnt
		m.NeedsSupplement = haveCnt > 0 && haveCnt < needCnt
		if defReg == snapshot.RegionGlobal {
			m.Version = nonEmptyOr(m.GlobalVersion, m.CNVersion)
		} else {
			m.Version = nonEmptyOr(m.CNVersion, m.GlobalVersion)
		}
		remoteList = append(remoteList, *m)
	}
	sortUnifiedVersions(remoteList)

	// 分别构建国服与国际服的可用版本下拉列表（只列出该区服已下载的快照 + 该区服当前客户端）
	buildRegionBackups := func(reg string, hasLive bool, liveVer, livePatch, liveDisp string) ([]VersionBackupMeta, string, string) {
		var downloaded []VersionBackupMeta
		for _, v := range remoteList {
			ok, gVer, createdAt, sz := s.hasLocalRegionPatchLocked(reg, v.Patch)
			if !ok {
				continue
			}
			item := v
			item.Version = gVer
			if createdAt != "" {
				item.CreatedAt = createdAt
			}
			if sz > 0 {
				item.Size = sz
			}
			item.Downloaded = true
			downloaded = append(downloaded, item)
		}
		sortUnifiedVersions(downloaded)

		var out []VersionBackupMeta
		if hasLive {
			out = append(out, VersionBackupMeta{
				ID:          LiveVersionID,
				Patch:       livePatch,
				Version:     liveVer,
				DisplayName: liveDisp,
				IsLive:      true,
				Downloaded:  true,
			})
		}
		out = append(out, downloaded...)

		defTo := ""
		if hasLive {
			defTo = LiveVersionID
		} else if len(downloaded) > 0 {
			defTo = downloaded[0].ID
		}
		defFrom := ""
		for _, d := range downloaded {
			if d.ID == defTo {
				continue
			}
			if isSameOrNewerThanLive(d, liveVer, livePatch) {
				continue
			}
			defFrom = d.ID
			break
		}
		if defFrom == "" {
			for _, d := range downloaded {
				if d.ID != defTo {
					defFrom = d.ID
					break
				}
			}
		}
		return out, defFrom, defTo
	}

	cnBackups, defCNFrom, defCNTo := buildRegionBackups(snapshot.RegionCN, hasCN, cnVer, cnPatch, cnDisp)
	glBackups, defGLFrom, defGLTo := buildRegionBackups(snapshot.RegionGlobal, hasGL, glVer, glPatch, glDisp)

	curVer, curPatch, curDisp := cnVer, cnPatch, cnDisp
	if defReg == snapshot.RegionGlobal {
		curVer, curPatch, curDisp = glVer, glPatch, glDisp
	}

	return VersionStatusResponse{
		HasCN:              hasCN,
		HasGlobal:          hasGL,
		ConfiguredRegions:  cfgRegions,
		DefaultRegion:      defReg,
		CNVersion:          cnVer,
		CNPatch:            cnPatch,
		CNDisplayName:      cnDisp,
		GlobalVersion:      glVer,
		GlobalPatch:        glPatch,
		GlobalDisplayName:  glDisp,
		CurrentVersion:     curVer,
		CurrentPatch:       curPatch,
		CurrentDisplayName: curDisp,
		CNBackups:          cnBackups,
		GlobalBackups:      glBackups,
		DefaultCNFrom:      defCNFrom,
		DefaultCNTo:        defCNTo,
		DefaultGlobalFrom:  defGLFrom,
		DefaultGlobalTo:    defGLTo,
		RemoteVersions:     remoteList,
		SnapshotDir:        s.snapshotDir,
		CatalogLoaded:      s.catalogLoaded,
		CatalogUpdatedAt:   s.catalog.UpdatedAt,
		CatalogError:       s.catalogErr,
	}
}

func sortUnifiedVersions(list []VersionBackupMeta) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Patch != list[j].Patch {
			return comparePatchDesc(list[i].Patch, list[j].Patch)
		}
		return list[i].Version > list[j].Version
	})
}

func comparePatchDesc(a, b string) bool {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] == pb[i] {
			continue
		}
		na, ea := parseLeadingInt(pa[i])
		nb, eb := parseLeadingInt(pb[i])
		if ea && eb && na != nb {
			return na > nb
		}
		return pa[i] > pb[i]
	}
	if len(pa) != len(pb) {
		return len(pa) > len(pb)
	}
	return a > b
}

func parseLeadingInt(s string) (int, bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:i])
	return n, err == nil
}

func globalCompareEXDLang(uiLang i18n.Lang) sqpack.Language {
	if i18n.NormalizeLang(string(uiLang)) == i18n.LangJA {
		return sqpack.LangJapanese
	}
	return sqpack.LangEnglish
}

func (s *Service) liveStoreForRegionLocked(region string, uiLang i18n.Lang) (*gamedata.Store, error) {
	reg := snapshot.NormalizeRegion(region)
	normUI := i18n.NormalizeLang(string(uiLang))
	if ms, ok := s.memLiveStores[reg]; ok && ms != nil {
		return ms, nil
	}
	if s.store == nil {
		return nil, errors.New("当前未加载本地客户端数据")
	}
	storeUI := i18n.NormalizeLang(string(s.store.AppLang))
	cacheKey := reg + "\x00" + string(normUI)

	if reg == snapshot.RegionCN {
		if !s.hasCNLocked() {
			return nil, errors.New("当前未配置国服客户端")
		}
		// 纯内存测试 Store（无底层 CNReader）直接复用；真实客户端须主数据语言与界面合成文案语言（AppLang）同时一致，防止与快照产生语言伪差异
		if s.store.CNReader == nil {
			return s.store, nil
		}
		if s.store.PrimaryLang == sqpack.LangChineseSimplified && storeUI == normUI {
			return s.store, nil
		}
		if cached, ok := s.liveRegionStores[cacheKey]; ok && cached != nil {
			return cached, nil
		}
		st, err := gamedata.LoadGameDataStoreWithOptions(&config.ResolvedPaths{
			CN: &config.ClientInfo{
				RootPath: s.store.CNPath,
				GamePath: s.store.CNReader.GamePath(),
				Version:  s.store.CNVersion,
			},
		}, s.store.SchemasFS, gamedata.StoreLoadOptions{
			AppLang:       normUI,
			BaseStore:     s.store,
			SharedReaders: true,
		})
		if err != nil {
			return nil, fmt.Errorf("读取当前国服客户端数据失败: %w", err)
		}
		s.liveRegionStores[cacheKey] = st
		return st, nil
	}

	// 国际服实时客户端
	if !s.hasGlobalLocked() {
		return nil, errors.New("当前未配置国际服客户端")
	}
	glEXDLang := globalCompareEXDLang(normUI)
	if s.store.GlobalReader == nil {
		return s.store, nil
	}
	if s.store.PrimaryLang == glEXDLang && storeUI == normUI {
		return s.store, nil
	}
	if cached, ok := s.liveRegionStores[cacheKey]; ok && cached != nil {
		return cached, nil
	}
	st, err := gamedata.LoadGameDataStoreWithOptions(&config.ResolvedPaths{
		Global: &config.ClientInfo{
			RootPath: s.store.GlobalPath,
			GamePath: s.store.GlobalReader.GamePath(),
			Version:  s.store.GlobalVersion,
		},
	}, s.store.SchemasFS, gamedata.StoreLoadOptions{
		AppLang:       normUI,
		BaseStore:     s.store,
		SharedReaders: true,
	})
	if err != nil {
		return nil, fmt.Errorf("读取当前国际服客户端数据失败: %w", err)
	}
	s.liveRegionStores[cacheKey] = st
	return st, nil
}

func (s *Service) openArchiveLocked(region, patch string) (*snapshot.Archive, error) {
	snapID := snapshot.MakeSnapshotID(region, patch)
	if arch, ok := s.archives[snapID]; ok {
		return arch, nil
	}
	info, ok := s.localSnaps[snapID]
	if !ok {
		return nil, fmt.Errorf("%w: %s (%s)", ErrSnapshotNotDownloaded, patch, region)
	}
	arch, err := snapshot.OpenArchive(info.FilePath)
	if err != nil {
		return nil, fmt.Errorf("打开历史快照 %s 失败: %w", snapID, err)
	}
	s.archives[snapID] = arch
	return arch, nil
}

func (s *Service) loadTargetPayloadLocked(region, target string, uiLang i18n.Lang, needIcons bool) (*snapshotPayload, error) {
	reg := snapshot.NormalizeRegion(region)
	cacheKey := reg + "\x00" + target + "\x00" + string(uiLang)
	if entry, ok := s.parsedCache[cacheKey]; ok && entry != nil && entry.payload != nil {
		s.parsedSeq++
		entry.seq = s.parsedSeq
		if needIcons && len(entry.payload.Icons) == 0 && target == LiveVersionID {
			if liveSt, err := s.liveStoreForRegionLocked(reg, uiLang); err == nil {
				entry.payload.Icons = s.loadLiveIconsForRegionLocked(reg, liveSt)
			}
		}
		return entry.payload, nil
	}

	// 1. 当前本地客户端（严格对应当前对比区服：国服取国服，国际服取国际服）
	if target == LiveVersionID {
		liveSt, err := s.liveStoreForRegionLocked(reg, uiLang)
		if err != nil {
			return nil, err
		}
		curVer := s.cnVersionLocked()
		if reg == snapshot.RegionGlobal {
			curVer = s.globalVersionLocked()
		}
		curPatch := s.matchLivePatchLocked(reg, curVer)
		meta := VersionBackupMeta{
			ID:          LiveVersionID,
			Patch:       curPatch,
			Version:     curVer,
			DisplayName: formatLiveClientDisplayName(uiLang, curPatch, curVer),
			IsLive:      true,
			Downloaded:  true,
		}
		var icons []compactIconRecord
		if needIcons {
			icons = s.loadLiveIconsForRegionLocked(reg, liveSt)
		}
		payload := buildPayloadFromStore(liveSt, meta, uiLang, icons, true)
		s.putParsedCacheLocked(cacheKey, payload, liveSt)
		return payload, nil
	}

	// 2. 内存测试快照（按 region + patch 精确匹配）
	snapID := snapshot.MakeSnapshotID(reg, target)
	if ms, ok := s.memSnaps[snapID]; ok {
		m := VersionBackupMeta{
			ID:          ms.Patch,
			Patch:       ms.Patch,
			Version:     ms.Version,
			DisplayName: ms.Patch,
			CreatedAt:   ms.CreatedAt,
			Downloaded:  true,
		}
		icons := convertIconDigests(ms.Icons)
		payload := buildPayloadFromStore(ms.Store, m, uiLang, icons, false)
		s.putParsedCacheLocked(cacheKey, payload, ms.Store)
		return payload, nil
	}

	// 3. 本地已下载的同区服 .xivsnap 原始数据快照
	arch, err := s.openArchiveLocked(reg, target)
	if err != nil {
		return nil, err
	}
	am := arch.Meta()
	bgmRecords, _ := arch.ReadBGMDigests()
	presetDigests := make(map[int]gamedata.BGMDigest, len(bgmRecords))
	presetExists := make(map[string]bool, len(bgmRecords))
	for _, r := range bgmRecords {
		if r.File != "" && r.FileExists {
			presetExists[r.File] = true
		}
		if r.ID > 0 {
			presetDigests[r.ID] = gamedata.BGMDigest{
				File:       r.File,
				FileExists: r.FileExists,
				FileSize:   r.FileSize,
				ContentMD5: r.ContentMD5,
			}
		}
	}

	schemasFS := arch.SchemasFS()
	if schemasFS == nil && s.store != nil {
		schemasFS = s.store.SchemasFS
	}
	// 快照与当前客户端统一使用相同的界面语言 AppLang 格式化合成标签（如收藏品/解锁奖励前缀），避免同版本跨语言产生伪差异
	snapStore, err := gamedata.LoadGameDataStoreFromSnapshot(arch, schemasFS, gamedata.SnapshotStoreOptions{
		AppLang:          i18n.NormalizeLang(string(uiLang)),
		Region:           reg,
		Version:          am.GameVersion,
		BGMDigests:       presetDigests,
		PresetFileExists: presetExists,
	})
	if err != nil {
		return nil, fmt.Errorf("解析历史快照 %s (%s) 失败: %w", target, reg, err)
	}

	iconRecords, _ := arch.ReadIconDigests()
	meta := VersionBackupMeta{
		ID:          am.Patch,
		Patch:       am.Patch,
		Version:     am.GameVersion,
		DisplayName: am.Patch,
		CreatedAt:   am.CreatedAt,
		Downloaded:  true,
	}
	payload := buildPayloadFromStore(snapStore, meta, uiLang, convertIconDigests(iconRecords), false)
	s.putParsedCacheLocked(cacheKey, payload, snapStore)
	return payload, nil
}

func (s *Service) putParsedCacheLocked(key string, payload *snapshotPayload, st *gamedata.Store) {
	const maxCachedStores = 6
	if len(s.parsedCache) >= maxCachedStores {
		var oldestKey string
		var oldestSeq uint64 = ^uint64(0)
		for k, v := range s.parsedCache {
			if strings.Contains(k, "\x00"+LiveVersionID+"\x00") {
				continue
			}
			if v.seq < oldestSeq {
				oldestSeq = v.seq
				oldestKey = k
			}
		}
		if oldestKey != "" {
			delete(s.parsedCache, oldestKey)
		}
	}
	s.parsedSeq++
	s.parsedCache[key] = &parsedSnapCacheEntry{
		payload: payload,
		store:   st,
		seq:     s.parsedSeq,
	}
}

// CompareVersions 在指定区服下严格执行同服对比（国服对国服、国际服对国际服），返回分页结果。
func (s *Service) CompareVersions(query VersionDiffQuery) (VersionDiffResponse, error) {
	return s.compareVersions(query, true)
}

// CollectDiffEntries 在指定区服下严格执行同服对比，返回符合筛选条件的全部差异记录（不分页，供导出使用）。
func (s *Service) CollectDiffEntries(query VersionDiffQuery) (VersionDiffResponse, error) {
	return s.compareVersions(query, false)
}

func (s *Service) compareVersions(query VersionDiffQuery, paginate bool) (VersionDiffResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	lang := s.langLocked()
	status := s.statusLocked()

	region := strings.TrimSpace(query.Region)
	if region == "" {
		region = status.DefaultRegion
	} else {
		region = snapshot.NormalizeRegion(region)
	}

	defFrom, defTo := status.DefaultPairForRegion(region)
	toID := strings.TrimSpace(query.To)
	if toID == "" {
		toID = defTo
	}
	fromID := strings.TrimSpace(query.From)
	if fromID == "" {
		fromID = defFrom
	}

	if fromID == "" || toID == "" {
		return VersionDiffResponse{
			NeedsDownload: true,
			Region:        region,
			FromID:        fromID,
			ToID:          toID,
			Page:          1,
			PageSize:      max(1, query.PageSize),
			TotalPages:    1,
		}, nil
	}

	filter := query.Filter
	if filter == "" {
		filter = "all"
	}
	entityType := query.EntityType
	if entityType == "" {
		entityType = "all"
	}
	q := strings.ToLower(strings.TrimSpace(query.Q))

	needIcons := entityType == "icon" ||
		len(s.memLiveIcons[region]) > 0 ||
		(s.store != nil && s.store.PrimaryReader == nil && len(s.store.Icons) > 0)

	fromSnap, err := s.loadTargetPayloadLocked(region, fromID, lang, needIcons)
	if err != nil {
		return VersionDiffResponse{}, err
	}
	toSnap, err := s.loadTargetPayloadLocked(region, toID, lang, needIcons)
	if err != nil {
		return VersionDiffResponse{}, err
	}

	var allEntries []DiffItemEntry
	var summary DiffSummary

	allEntries = append(allEntries, diffItems(lang, fromSnap, toSnap, &summary)...)
	allEntries = append(allEntries, diffQuests(lang, fromSnap, toSnap, &summary)...)
	allEntries = append(allEntries, diffLeves(lang, fromSnap, toSnap, &summary)...)
	allEntries = append(allEntries, diffAchievements(lang, fromSnap, toSnap, &summary)...)
	allEntries = append(allEntries, diffBGMs(lang, fromSnap, toSnap, &summary)...)
	iconEntries := diffIcons(lang, fromSnap, toSnap, &summary)
	if needIcons {
		summary.IconMissingFrom = len(fromSnap.Icons) == 0
		summary.IconMissingTo = len(toSnap.Icons) == 0
		summary.IconDiffAvailable = !summary.IconMissingFrom && !summary.IconMissingTo
	} else {
		summary.IconDiffAvailable = len(fromSnap.Icons) > 0 && len(toSnap.Icons) > 0
	}
	summary.AddedRecipes = max(0, toSnap.RecipeCount-fromSnap.RecipeCount)

	textEntries, textMod, textRem, textAvail := s.computeTextDiffLocked(region, lang, fromID, toID, entityType == "text")
	summary.ModifiedTexts = textMod
	summary.RemovedTexts = textRem
	summary.TextDiffAvailable = textAvail

	filtered := make([]DiffItemEntry, 0, 128)
	switch entityType {
	case "text":
		for _, e := range textEntries {
			if filter == "added" {
				continue
			}
			if filter != "all" && e.ChangeType != filter {
				continue
			}
			if q != "" && !textDiffEntryMatchesQuery(e, q) {
				continue
			}
			filtered = append(filtered, e)
		}
	case "icon":
		for _, e := range iconEntries {
			if filter != "all" && e.ChangeType != filter {
				continue
			}
			if q != "" && !diffEntryMatchesQuery(e, q) {
				continue
			}
			filtered = append(filtered, e)
		}
		sort.Slice(filtered, func(i, j int) bool {
			rank := func(ct string) int {
				switch ct {
				case "modified":
					return 0
				case "added":
					return 1
				default:
					return 2
				}
			}
			ri, rj := rank(filtered[i].ChangeType), rank(filtered[j].ChangeType)
			if ri != rj {
				return ri < rj
			}
			return filtered[i].ID < filtered[j].ID
		})
	default:
		for _, e := range allEntries {
			if filter != "all" && e.ChangeType != filter {
				continue
			}
			if entityType != "all" && e.EntityType != entityType {
				continue
			}
			if q != "" && !diffEntryMatchesQuery(e, q) {
				continue
			}
			filtered = append(filtered, e)
		}
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].ItemLevel != filtered[j].ItemLevel {
				return filtered[i].ItemLevel > filtered[j].ItemLevel
			}
			return filtered[i].ID > filtered[j].ID
		})
	}

	totalFiltered := len(filtered)
	if !paginate {
		items := make([]DiffItemEntry, totalFiltered)
		copy(items, filtered)
		return VersionDiffResponse{
			Region:          region,
			FromID:          fromID,
			ToID:            toID,
			FromVersion:     fromSnap.Meta.Version,
			ToVersion:       toSnap.Meta.Version,
			FromDisplayName: fromSnap.Meta.DisplayName,
			ToDisplayName:   toSnap.Meta.DisplayName,
			FromCreatedAt:   fromSnap.Meta.CreatedAt,
			ToCreatedAt:     toSnap.Meta.CreatedAt,
			Summary:         summary,
			Items:           items,
			TotalFiltered:   totalFiltered,
			Page:            1,
			PageSize:        totalFiltered,
			TotalPages:      1,
		}, nil
	}

	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	maxPageSize := 100
	if entityType == "icon" {
		maxPageSize = 240
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	totalPages := max(1, (totalFiltered+pageSize-1)/pageSize)
	page := query.Page
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * pageSize
	end := min(totalFiltered, start+pageSize)
	var items []DiffItemEntry
	if start < end {
		items = make([]DiffItemEntry, end-start)
		copy(items, filtered[start:end])
		if entityType == "text" {
			s.enrichTextDiffPageLocked(items, region, toID)
		}
	}

	return VersionDiffResponse{
		Region:          region,
		FromID:          fromID,
		ToID:            toID,
		FromVersion:     fromSnap.Meta.Version,
		ToVersion:       toSnap.Meta.Version,
		FromDisplayName: fromSnap.Meta.DisplayName,
		ToDisplayName:   toSnap.Meta.DisplayName,
		FromCreatedAt:   fromSnap.Meta.CreatedAt,
		ToCreatedAt:     toSnap.Meta.CreatedAt,
		Summary:         summary,
		Items:           items,
		TotalFiltered:   totalFiltered,
		Page:            page,
		PageSize:        pageSize,
		TotalPages:      totalPages,
	}, nil
}

func (s *Service) resolveEXDSourceLocked(region, target string) (sqpack.FileReader, fs.FS, sqpack.Language) {
	reg := snapshot.NormalizeRegion(region)
	if target == LiveVersionID {
		liveSt, err := s.liveStoreForRegionLocked(reg, s.langLocked())
		if err != nil || liveSt == nil {
			return nil, nil, sqpack.LangNone
		}
		var r sqpack.FileReader
		if reg == snapshot.RegionGlobal {
			if liveSt.GlobalReader != nil {
				r = liveSt.GlobalReader
			} else if liveSt.EXDReader != nil {
				r = liveSt.EXDReader
			} else if liveSt.PrimaryReader != nil {
				r = liveSt.PrimaryReader
			}
			return r, liveSt.SchemasFS, globalCompareEXDLang(s.langLocked())
		}
		if liveSt.CNReader != nil {
			r = liveSt.CNReader
		} else if liveSt.EXDReader != nil {
			r = liveSt.EXDReader
		} else if liveSt.PrimaryReader != nil {
			r = liveSt.PrimaryReader
		}
		return r, liveSt.SchemasFS, sqpack.LangChineseSimplified
	}

	snapID := snapshot.MakeSnapshotID(reg, target)
	if ms, ok := s.memSnaps[snapID]; ok && ms.Store != nil {
		var r sqpack.FileReader = ms.Store.EXDReader
		if r == nil && ms.Store.PrimaryReader != nil {
			r = ms.Store.PrimaryReader
		}
		return r, ms.Store.SchemasFS, ms.Store.PrimaryLang
	}
	arch, err := s.openArchiveLocked(reg, target)
	if err != nil {
		return nil, nil, sqpack.LangNone
	}
	lang := sqpack.LangChineseSimplified
	if reg == snapshot.RegionGlobal {
		lang = globalCompareEXDLang(s.langLocked())
	}
	sfs := arch.SchemasFS()
	if sfs == nil && s.store != nil {
		sfs = s.store.SchemasFS
	}
	return arch, sfs, lang
}

func diffEntryMatchesQuery(e DiffItemEntry, q string) bool {
	if strings.Contains(strings.ToLower(e.NameCn), q) ||
		strings.Contains(strings.ToLower(e.NameEn), q) ||
		strings.Contains(strings.ToLower(e.NameJa), q) ||
		strings.Contains(strings.ToLower(e.CategoryName), q) ||
		strings.Contains(strings.ToLower(e.File), q) ||
		strings.Contains(strings.ToLower(e.TitleNameCn), q) ||
		strings.Contains(strings.ToLower(e.RewardItemNameCn), q) ||
		strconv.Itoa(e.ID) == q {
		return true
	}
	for _, ch := range e.Changes {
		if strings.Contains(strings.ToLower(ch.Label), q) || strings.Contains(strings.ToLower(ch.After), q) {
			return true
		}
	}
	return false
}

func diffItems(lang i18n.Lang, fromSnap, toSnap *snapshotPayload, summary *DiffSummary) []DiffItemEntry {
	fromItems := make(map[int]compactItemRecord, len(fromSnap.Items))
	for _, it := range fromSnap.Items {
		fromItems[it.ID] = it
	}
	toItems := make(map[int]compactItemRecord, len(toSnap.Items))
	for _, it := range toSnap.Items {
		toItems[it.ID] = it
	}

	noneStr := i18n.T(lang, "common.none")
	var entries []DiffItemEntry
	for _, toItem := range toSnap.Items {
		fromItem, exists := fromItems[toItem.ID]
		toCol := toItem.IsCollectableTurnIn != nil && *toItem.IsCollectableTurnIn
		if !exists {
			summary.AddedItems++
			if toItem.IsCraftable {
				summary.NewCraftableItems++
			}
			if toItem.IsGatherable {
				summary.NewGatherableItems++
			}
			if toItem.IsPurchasable {
				summary.NewPurchasableItems++
			}
			if toCol {
				summary.NewCollectableTurnInItems++
			}
			afterDesc := i18n.Tf(lang, "backup.ilvl_value", "ilvl", strconv.Itoa(toItem.ItemLevel)) + " · " + toItem.CategoryName
			changes := []ItemFieldChange{{
				Field:  "status",
				Label:  i18n.T(lang, "backup.status_added"),
				Before: noneStr,
				After:  afterDesc,
			}}
			if toCol && toItem.CollectableSummary != "" {
				changes = append(changes, ItemFieldChange{
					Field:  "collectableSummary",
					Label:  i18n.T(lang, "backup.collectable_turnin"),
					Before: noneStr,
					After:  toItem.CollectableSummary,
				})
			}
			entries = append(entries, DiffItemEntry{
				ID:                   toItem.ID,
				EntityType:           "item",
				ChangeType:           "added",
				NameCn:               toItem.NameCn,
				NameEn:               toItem.NameEn,
				NameJa:               toItem.NameJa,
				IconID:               toItem.IconID,
				ItemLevel:            toItem.ItemLevel,
				EquipLevel:           toItem.EquipLevel,
				CategoryName:         toItem.CategoryName,
				PriceMid:             toItem.PriceMid,
				IsCraftable:          toItem.IsCraftable,
				IsGatherable:         toItem.IsGatherable,
				IsPurchasable:        toItem.IsPurchasable,
				HasCollectableChange: toCol,
				Changes:              changes,
			})
			continue
		}

		changes, colChanged := diffSingleItem(lang, fromItem, toItem)
		if len(changes) > 0 {
			summary.ModifiedItems++
			if !fromItem.IsCraftable && toItem.IsCraftable {
				summary.NewCraftableItems++
			}
			if !fromItem.IsGatherable && toItem.IsGatherable {
				summary.NewGatherableItems++
			}
			if !fromItem.IsPurchasable && toItem.IsPurchasable {
				summary.NewPurchasableItems++
			}
			fromCol := fromItem.IsCollectableTurnIn != nil && *fromItem.IsCollectableTurnIn
			if !fromCol && toCol {
				summary.NewCollectableTurnInItems++
			} else if colChanged {
				summary.ModifiedCollectableItems++
			}
			entries = append(entries, DiffItemEntry{
				ID:                   toItem.ID,
				EntityType:           "item",
				ChangeType:           "modified",
				NameCn:               toItem.NameCn,
				NameEn:               toItem.NameEn,
				NameJa:               toItem.NameJa,
				IconID:               toItem.IconID,
				ItemLevel:            toItem.ItemLevel,
				EquipLevel:           toItem.EquipLevel,
				CategoryName:         toItem.CategoryName,
				PriceMid:             toItem.PriceMid,
				IsCraftable:          toItem.IsCraftable,
				IsGatherable:         toItem.IsGatherable,
				IsPurchasable:        toItem.IsPurchasable,
				HasCollectableChange: colChanged,
				Changes:              changes,
			})
		}
	}

	for _, fromItem := range fromSnap.Items {
		if _, exists := toItems[fromItem.ID]; !exists {
			summary.RemovedItems++
			entries = append(entries, DiffItemEntry{
				ID:            fromItem.ID,
				EntityType:    "item",
				ChangeType:    "removed",
				NameCn:        fromItem.NameCn,
				NameEn:        fromItem.NameEn,
				NameJa:        fromItem.NameJa,
				IconID:        fromItem.IconID,
				ItemLevel:     fromItem.ItemLevel,
				EquipLevel:    fromItem.EquipLevel,
				CategoryName:  fromItem.CategoryName,
				PriceMid:      fromItem.PriceMid,
				IsCraftable:   fromItem.IsCraftable,
				IsGatherable:  fromItem.IsGatherable,
				IsPurchasable: fromItem.IsPurchasable,
				Changes: []ItemFieldChange{{
					Field:  "status",
					Label:  i18n.T(lang, "backup.status_removed"),
					Before: i18n.Tf(lang, "backup.ilvl_value", "ilvl", strconv.Itoa(fromItem.ItemLevel)),
					After:  i18n.T(lang, "diff.change_removed"),
				}},
			})
		}
	}
	return entries
}

func questCategoryLabel(lang i18n.Lang, q compactQuestRecord) string {
	genre := nonEmptyOr(q.GenreName, q.SectionName)
	if genre != "" {
		return i18n.Tf(lang, "backup.quest_cat_genre", "genre", genre)
	}
	return i18n.T(lang, "backup.quest_cat")
}

func diffQuests(lang i18n.Lang, fromSnap, toSnap *snapshotPayload, summary *DiffSummary) []DiffItemEntry {
	fromMap := make(map[int]compactQuestRecord, len(fromSnap.Quests))
	for _, q := range fromSnap.Quests {
		fromMap[q.ID] = q
	}
	toMap := make(map[int]compactQuestRecord, len(toSnap.Quests))
	for _, q := range toSnap.Quests {
		toMap[q.ID] = q
	}

	noneStr := i18n.T(lang, "common.none")
	var entries []DiffItemEntry
	for _, toQ := range toSnap.Quests {
		fromQ, exists := fromMap[toQ.ID]
		if !exists {
			summary.AddedQuests++
			parts := []string{fmt.Sprintf("Lv.%d", toQ.Level)}
			if toQ.IsMSQ {
				parts = append(parts, i18n.T(lang, "backup.quest_msq"))
			} else if toQ.IsFeature {
				parts = append(parts, i18n.T(lang, "backup.quest_feature"))
			}
			if toQ.IssuerName != "" {
				parts = append(parts, i18n.Tf(lang, "backup.quest_issuer", "name", toQ.IssuerName))
			}
			if toQ.RewardSummary != "" {
				parts = append(parts, i18n.Tf(lang, "backup.quest_reward", "summary", toQ.RewardSummary))
			}
			entries = append(entries, DiffItemEntry{
				ID:           toQ.ID,
				EntityType:   "quest",
				ChangeType:   "added",
				NameCn:       toQ.NameCn,
				NameEn:       toQ.NameEn,
				NameJa:       toQ.NameJa,
				IconID:       toQ.IconID,
				ItemLevel:    toQ.Level,
				CategoryName: questCategoryLabel(lang, toQ),
				RewardItemID: toQ.PrimaryItemID,
				Changes: []ItemFieldChange{{
					Field:  "quest",
					Label:  i18n.T(lang, "backup.quest_added"),
					Before: noneStr,
					After:  strings.Join(parts, " · "),
				}},
			})
			continue
		}
		if changes := diffSingleQuest(lang, fromQ, toQ); len(changes) > 0 {
			summary.ModifiedQuests++
			entries = append(entries, DiffItemEntry{
				ID:           toQ.ID,
				EntityType:   "quest",
				ChangeType:   "modified",
				NameCn:       toQ.NameCn,
				NameEn:       toQ.NameEn,
				NameJa:       toQ.NameJa,
				IconID:       toQ.IconID,
				ItemLevel:    toQ.Level,
				CategoryName: questCategoryLabel(lang, toQ),
				RewardItemID: toQ.PrimaryItemID,
				Changes:      changes,
			})
		}
	}

	for _, fromQ := range fromSnap.Quests {
		if _, exists := toMap[fromQ.ID]; !exists {
			summary.RemovedQuests++
			entries = append(entries, DiffItemEntry{
				ID:           fromQ.ID,
				EntityType:   "quest",
				ChangeType:   "removed",
				NameCn:       fromQ.NameCn,
				NameEn:       fromQ.NameEn,
				NameJa:       fromQ.NameJa,
				IconID:       fromQ.IconID,
				ItemLevel:    fromQ.Level,
				CategoryName: questCategoryLabel(lang, fromQ),
				Changes: []ItemFieldChange{{
					Field:  "quest",
					Label:  i18n.T(lang, "backup.quest_removed"),
					Before: fmt.Sprintf("Lv.%d", fromQ.Level),
					After:  i18n.T(lang, "diff.change_removed"),
				}},
			})
		}
	}
	return entries
}

func leveCategoryLabel(lang i18n.Lang, lv compactLeveRecord) string {
	if lv.AssignmentTypeName != "" && lv.AssignmentTypeName != lv.LeveKindLabel {
		return i18n.Tf(lang, "backup.leve_cat_type", "kind", lv.LeveKindLabel, "type", lv.AssignmentTypeName)
	}
	return i18n.Tf(lang, "backup.leve_cat", "kind", nonEmptyOr(lv.LeveKindLabel, i18n.T(lang, "backup.leve_default_kind")))
}

func diffLeves(lang i18n.Lang, fromSnap, toSnap *snapshotPayload, summary *DiffSummary) []DiffItemEntry {
	fromMap := make(map[int]compactLeveRecord, len(fromSnap.Leves))
	for _, lv := range fromSnap.Leves {
		fromMap[lv.ID] = lv
	}
	toMap := make(map[int]compactLeveRecord, len(toSnap.Leves))
	for _, lv := range toSnap.Leves {
		toMap[lv.ID] = lv
	}

	noneStr := i18n.T(lang, "common.none")
	var entries []DiffItemEntry
	for _, toLv := range toSnap.Leves {
		fromLv, exists := fromMap[toLv.ID]
		if !exists {
			summary.AddedLeves++
			parts := []string{fmt.Sprintf("Lv.%d", toLv.ClassJobLevel)}
			if toLv.ExpReward > 0 {
				parts = append(parts, i18n.Tf(lang, "common.exp_amount", "amount", strconv.Itoa(toLv.ExpReward)))
			}
			if toLv.GilReward > 0 {
				parts = append(parts, i18n.Tf(lang, "common.gil_amount", "amount", strconv.Itoa(toLv.GilReward)))
			}
			if toLv.TurnInSummary != "" {
				parts = append(parts, i18n.Tf(lang, "backup.leve_turnin", "summary", toLv.TurnInSummary))
			} else if toLv.RewardSummary != "" {
				parts = append(parts, i18n.Tf(lang, "backup.leve_reward", "summary", toLv.RewardSummary))
			}
			entries = append(entries, DiffItemEntry{
				ID:           toLv.ID,
				EntityType:   "leve",
				ChangeType:   "added",
				NameCn:       toLv.NameCn,
				NameEn:       toLv.NameEn,
				NameJa:       toLv.NameJa,
				IconID:       toLv.IconID,
				ItemLevel:    toLv.ClassJobLevel,
				CategoryName: leveCategoryLabel(lang, toLv),
				Changes: []ItemFieldChange{{
					Field:  "leve",
					Label:  i18n.T(lang, "backup.leve_added"),
					Before: noneStr,
					After:  strings.Join(parts, " · "),
				}},
			})
			continue
		}
		if changes := diffSingleLeve(lang, fromLv, toLv); len(changes) > 0 {
			summary.ModifiedLeves++
			entries = append(entries, DiffItemEntry{
				ID:           toLv.ID,
				EntityType:   "leve",
				ChangeType:   "modified",
				NameCn:       toLv.NameCn,
				NameEn:       toLv.NameEn,
				NameJa:       toLv.NameJa,
				IconID:       toLv.IconID,
				ItemLevel:    toLv.ClassJobLevel,
				CategoryName: leveCategoryLabel(lang, toLv),
				Changes:      changes,
			})
		}
	}

	for _, fromLv := range fromSnap.Leves {
		if _, exists := toMap[fromLv.ID]; !exists {
			summary.RemovedLeves++
			entries = append(entries, DiffItemEntry{
				ID:           fromLv.ID,
				EntityType:   "leve",
				ChangeType:   "removed",
				NameCn:       fromLv.NameCn,
				NameEn:       fromLv.NameEn,
				NameJa:       fromLv.NameJa,
				IconID:       fromLv.IconID,
				ItemLevel:    fromLv.ClassJobLevel,
				CategoryName: leveCategoryLabel(lang, fromLv),
				Changes: []ItemFieldChange{{
					Field:  "leve",
					Label:  i18n.T(lang, "backup.leve_removed"),
					Before: fmt.Sprintf("Lv.%d", fromLv.ClassJobLevel),
					After:  i18n.T(lang, "diff.change_removed"),
				}},
			})
		}
	}
	return entries
}

func diffAchievements(lang i18n.Lang, fromSnap, toSnap *snapshotPayload, summary *DiffSummary) []DiffItemEntry {
	fromAchs := make(map[int]compactAchievementRecord, len(fromSnap.Achievements))
	for _, a := range fromSnap.Achievements {
		fromAchs[a.ID] = a
	}
	toAchs := make(map[int]compactAchievementRecord, len(toSnap.Achievements))
	for _, a := range toSnap.Achievements {
		toAchs[a.ID] = a
	}

	noneStr := i18n.T(lang, "common.none")
	var entries []DiffItemEntry
	for _, toAch := range toSnap.Achievements {
		titleDisp := toAch.TitleMasculineCn
		if toAch.TitleMasculineCn != "" && toAch.TitleFeminineCn != "" && toAch.TitleMasculineCn != toAch.TitleFeminineCn {
			titleDisp = toAch.TitleMasculineCn + " / " + toAch.TitleFeminineCn
		}
		fromAch, exists := fromAchs[toAch.ID]
		if !exists {
			summary.AddedAchievements++
			if toAch.TitleID > 0 {
				summary.AddedTitles++
			}
			parts := []string{
				i18n.Tf(lang, "backup.ach_points", "pts", strconv.Itoa(toAch.Points)),
				fmt.Sprintf("%s · %s", toAch.KindName, toAch.CategoryName),
			}
			if titleDisp != "" {
				parts = append(parts, i18n.Tf(lang, "backup.ach_title_tag", "title", titleDisp))
			}
			if toAch.RewardItemNameCn != "" {
				parts = append(parts, i18n.Tf(lang, "backup.ach_reward_item", "name", toAch.RewardItemNameCn))
			}
			entries = append(entries, DiffItemEntry{
				ID:               toAch.ID,
				EntityType:       "achievement",
				ChangeType:       "added",
				NameCn:           toAch.NameCn,
				NameEn:           toAch.NameEn,
				NameJa:           toAch.NameJa,
				IconID:           toAch.IconID,
				ItemLevel:        toAch.Points,
				CategoryName:     i18n.Tf(lang, "backup.ach_cat", "kind", toAch.KindName, "cat", toAch.CategoryName),
				Points:           toAch.Points,
				DescriptionCn:    toAch.DescriptionCn,
				AchKindName:      toAch.KindName,
				AchCategoryName:  toAch.CategoryName,
				TitleID:          toAch.TitleID,
				TitleNameCn:      titleDisp,
				TitleMasculineCn: toAch.TitleMasculineCn,
				TitleFeminineCn:  toAch.TitleFeminineCn,
				TitleNameJa:      toAch.TitleNameJa,
				TitleNameEn:      toAch.TitleNameEn,
				RewardItemID:     toAch.RewardItemID,
				RewardItemNameCn: toAch.RewardItemNameCn,
				RewardItemIconID: toAch.RewardItemIconID,
				Changes: []ItemFieldChange{{
					Field:  "achievement",
					Label:  i18n.T(lang, "backup.ach_added"),
					Before: noneStr,
					After:  strings.Join(parts, " · "),
				}},
			})
			continue
		}
		achChanges := diffSingleAchievement(lang, fromAch, toAch)
		if len(achChanges) > 0 {
			summary.ModifiedAchievements++
			if fromAch.TitleID == 0 && toAch.TitleID > 0 {
				summary.AddedTitles++
			}
			entries = append(entries, DiffItemEntry{
				ID:               toAch.ID,
				EntityType:       "achievement",
				ChangeType:       "modified",
				NameCn:           toAch.NameCn,
				NameEn:           toAch.NameEn,
				NameJa:           toAch.NameJa,
				IconID:           toAch.IconID,
				ItemLevel:        toAch.Points,
				CategoryName:     i18n.Tf(lang, "backup.ach_cat", "kind", toAch.KindName, "cat", toAch.CategoryName),
				Points:           toAch.Points,
				DescriptionCn:    toAch.DescriptionCn,
				AchKindName:      toAch.KindName,
				AchCategoryName:  toAch.CategoryName,
				TitleID:          toAch.TitleID,
				TitleNameCn:      titleDisp,
				TitleMasculineCn: toAch.TitleMasculineCn,
				TitleFeminineCn:  toAch.TitleFeminineCn,
				TitleNameJa:      toAch.TitleNameJa,
				TitleNameEn:      toAch.TitleNameEn,
				RewardItemID:     toAch.RewardItemID,
				RewardItemNameCn: toAch.RewardItemNameCn,
				RewardItemIconID: toAch.RewardItemIconID,
				Changes:          achChanges,
			})
		}
	}

	for _, fromAch := range fromSnap.Achievements {
		if _, exists := toAchs[fromAch.ID]; !exists {
			summary.RemovedAchievements++
			fromTitle := fromAch.TitleMasculineCn
			if fromAch.TitleMasculineCn != "" && fromAch.TitleFeminineCn != "" && fromAch.TitleMasculineCn != fromAch.TitleFeminineCn {
				fromTitle = fromAch.TitleMasculineCn + " / " + fromAch.TitleFeminineCn
			}
			entries = append(entries, DiffItemEntry{
				ID:               fromAch.ID,
				EntityType:       "achievement",
				ChangeType:       "removed",
				NameCn:           fromAch.NameCn,
				NameEn:           fromAch.NameEn,
				NameJa:           fromAch.NameJa,
				IconID:           fromAch.IconID,
				ItemLevel:        fromAch.Points,
				CategoryName:     i18n.Tf(lang, "backup.ach_cat", "kind", fromAch.KindName, "cat", fromAch.CategoryName),
				Points:           fromAch.Points,
				DescriptionCn:    fromAch.DescriptionCn,
				AchKindName:      fromAch.KindName,
				AchCategoryName:  fromAch.CategoryName,
				TitleID:          fromAch.TitleID,
				TitleNameCn:      fromTitle,
				TitleMasculineCn: fromAch.TitleMasculineCn,
				TitleFeminineCn:  fromAch.TitleFeminineCn,
				TitleNameJa:      fromAch.TitleNameJa,
				TitleNameEn:      fromAch.TitleNameEn,
				RewardItemID:     fromAch.RewardItemID,
				RewardItemNameCn: fromAch.RewardItemNameCn,
				RewardItemIconID: fromAch.RewardItemIconID,
				Changes: []ItemFieldChange{{
					Field:  "achievement",
					Label:  i18n.T(lang, "backup.ach_removed"),
					Before: i18n.Tf(lang, "backup.ach_points", "pts", strconv.Itoa(fromAch.Points)),
					After:  i18n.T(lang, "diff.change_removed"),
				}},
			})
		}
	}
	return entries
}

func bgmCategoryLabel(lang i18n.Lang, b compactBGMRecord) string {
	if b.ExpansionName != "" {
		return i18n.Tf(lang, "backup.bgm_cat_exp", "exp", b.ExpansionName)
	}
	return i18n.T(lang, "backup.bgm_cat")
}

func bgmDiffNames(b compactBGMRecord) (string, string, string) {
	cn := nonEmptyOr(b.NameCn, b.Title)
	en := nonEmptyOr(b.NameEn, b.Title)
	ja := nonEmptyOr(b.NameJa, b.Title)
	if cn == "" && en == "" && ja == "" {
		cn = b.File
	}
	return cn, en, ja
}

func formatBGMByteSize(size uint32) string {
	switch {
	case size >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	case size >= 1024:
		return fmt.Sprintf("%.0f KB", float64(size)/1024)
	case size > 0:
		return fmt.Sprintf("%d B", size)
	default:
		return ""
	}
}

func diffBGMs(lang i18n.Lang, fromSnap, toSnap *snapshotPayload, summary *DiffSummary) []DiffItemEntry {
	if len(fromSnap.BGMs) == 0 || len(toSnap.BGMs) == 0 {
		return nil
	}
	fromMap := make(map[int]compactBGMRecord, len(fromSnap.BGMs))
	for _, b := range fromSnap.BGMs {
		fromMap[b.ID] = b
	}
	toMap := make(map[int]compactBGMRecord, len(toSnap.BGMs))
	for _, b := range toSnap.BGMs {
		toMap[b.ID] = b
	}

	noneStr := i18n.T(lang, "common.none")
	var entries []DiffItemEntry
	for _, toB := range toSnap.BGMs {
		fromB, exists := fromMap[toB.ID]
		cn, en, ja := bgmDiffNames(toB)
		if !exists {
			summary.AddedBGMs++
			var parts []string
			if toB.ExpansionName != "" {
				parts = append(parts, toB.ExpansionName)
			}
			if toB.UsageSummary != "" {
				parts = append(parts, toB.UsageSummary)
			} else if toB.File != "" {
				parts = append(parts, toB.File)
			}
			if sz := formatBGMByteSize(toB.FileSize); sz != "" {
				parts = append(parts, sz)
			}
			entries = append(entries, DiffItemEntry{
				ID:           toB.ID,
				EntityType:   "bgm",
				ChangeType:   "added",
				NameCn:       cn,
				NameEn:       en,
				NameJa:       ja,
				CategoryName: bgmCategoryLabel(lang, toB),
				File:         toB.File,
				Changes: []ItemFieldChange{{
					Field:  "bgm",
					Label:  i18n.T(lang, "backup.bgm_added"),
					Before: noneStr,
					After:  nonEmptyOr(strings.Join(parts, " · "), toB.File),
				}},
			})
			continue
		}
		if changes := diffSingleBGM(lang, fromB, toB); len(changes) > 0 {
			summary.ModifiedBGMs++
			entries = append(entries, DiffItemEntry{
				ID:           toB.ID,
				EntityType:   "bgm",
				ChangeType:   "modified",
				NameCn:       cn,
				NameEn:       en,
				NameJa:       ja,
				CategoryName: bgmCategoryLabel(lang, toB),
				File:         toB.File,
				Changes:      changes,
			})
		}
	}

	for _, fromB := range fromSnap.BGMs {
		if _, exists := toMap[fromB.ID]; !exists {
			summary.RemovedBGMs++
			cn, en, ja := bgmDiffNames(fromB)
			entries = append(entries, DiffItemEntry{
				ID:           fromB.ID,
				EntityType:   "bgm",
				ChangeType:   "removed",
				NameCn:       cn,
				NameEn:       en,
				NameJa:       ja,
				CategoryName: bgmCategoryLabel(lang, fromB),
				File:         fromB.File,
				Changes: []ItemFieldChange{{
					Field:  "bgm",
					Label:  i18n.T(lang, "backup.bgm_removed"),
					Before: nonEmptyOr(fromB.UsageSummary, fromB.File),
					After:  i18n.T(lang, "diff.change_removed"),
				}},
			})
		}
	}
	return entries
}

func diffSingleBGM(lang i18n.Lang, before, after compactBGMRecord) []ItemFieldChange {
	var changes []ItemFieldChange
	noneStr := i18n.T(lang, "common.none")
	if before.File != after.File {
		changes = append(changes, ItemFieldChange{
			Field:  "file",
			Label:  i18n.T(lang, "backup.bgm_field_file"),
			Before: nonEmptyOr(before.File, noneStr),
			After:  nonEmptyOr(after.File, noneStr),
		})
	}
	if !before.FileExists && after.FileExists {
		changes = append(changes, ItemFieldChange{
			Field:  "content",
			Label:  i18n.T(lang, "backup.bgm_field_content"),
			Before: i18n.T(lang, "backup.bgm_missing"),
			After:  i18n.T(lang, "backup.bgm_content_added"),
		})
		return changes
	}
	if before.FileExists && !after.FileExists {
		changes = append(changes, ItemFieldChange{
			Field:  "content",
			Label:  i18n.T(lang, "backup.bgm_field_content"),
			Before: i18n.T(lang, "backup.bgm_content_prev"),
			After:  i18n.T(lang, "backup.bgm_missing"),
		})
		return changes
	}
	md5Changed := before.ContentMD5 != "" && after.ContentMD5 != "" && before.ContentMD5 != after.ContentMD5
	sizeChanged := before.FileSize > 0 && after.FileSize > 0 && before.FileSize != after.FileSize
	if (before.FileExists || after.FileExists) && (md5Changed || sizeChanged) {
		changes = append(changes, ItemFieldChange{
			Field:  "content",
			Label:  i18n.T(lang, "backup.bgm_field_content"),
			Before: i18n.T(lang, "backup.bgm_content_prev"),
			After:  i18n.T(lang, "backup.bgm_content_changed"),
		})
	}
	return changes
}

func diffSingleItem(lang i18n.Lang, before, after compactItemRecord) ([]ItemFieldChange, bool) {
	var changes []ItemFieldChange
	colChanged := false
	noneStr := i18n.T(lang, "common.none")
	if before.NameCn != after.NameCn {
		changes = append(changes, ItemFieldChange{Field: "nameCn", Label: i18n.T(lang, "backup.field_name"), Before: before.NameCn, After: after.NameCn})
	}
	if before.ItemLevel != after.ItemLevel {
		changes = append(changes, ItemFieldChange{Field: "itemLevel", Label: i18n.T(lang, "backup.field_ilvl"), Before: strconv.Itoa(before.ItemLevel), After: strconv.Itoa(after.ItemLevel)})
	}
	if before.EquipLevel != after.EquipLevel {
		changes = append(changes, ItemFieldChange{Field: "equipLevel", Label: i18n.T(lang, "backup.field_equip_lv"), Before: strconv.Itoa(before.EquipLevel), After: strconv.Itoa(after.EquipLevel)})
	}
	if before.IsCraftable != after.IsCraftable {
		y, n := i18n.T(lang, "backup.craft_yes"), i18n.T(lang, "backup.craft_no")
		changes = append(changes, ItemFieldChange{Field: "isCraftable", Label: i18n.T(lang, "backup.field_craftable"), Before: boolLabel(before.IsCraftable, y, n), After: boolLabel(after.IsCraftable, y, n)})
	}
	if before.IsGatherable != after.IsGatherable {
		y, n := i18n.T(lang, "backup.gather_yes"), i18n.T(lang, "backup.gather_no")
		changes = append(changes, ItemFieldChange{Field: "isGatherable", Label: i18n.T(lang, "backup.field_gatherable"), Before: boolLabel(before.IsGatherable, y, n), After: boolLabel(after.IsGatherable, y, n)})
	}
	if before.IsPurchasable != after.IsPurchasable {
		y, n := i18n.T(lang, "backup.purch_yes"), i18n.T(lang, "backup.purch_no")
		changes = append(changes, ItemFieldChange{Field: "isPurchasable", Label: i18n.T(lang, "backup.field_purchasable"), Before: boolLabel(before.IsPurchasable, y, n), After: boolLabel(after.IsPurchasable, y, n)})
	}
	if before.IsRetainerVenture != nil && after.IsRetainerVenture != nil && *before.IsRetainerVenture != *after.IsRetainerVenture {
		y, n := i18n.T(lang, "backup.ret_yes"), i18n.T(lang, "backup.ret_no")
		changes = append(changes, ItemFieldChange{Field: "isRetainerVenture", Label: i18n.T(lang, "backup.field_retainer"), Before: boolLabel(*before.IsRetainerVenture, y, n), After: boolLabel(*after.IsRetainerVenture, y, n)})
	}
	if before.IsCollectableTurnIn != nil && after.IsCollectableTurnIn != nil && *before.IsCollectableTurnIn != *after.IsCollectableTurnIn {
		colChanged = true
		y, n := i18n.T(lang, "backup.col_turnin_yes"), i18n.T(lang, "backup.col_turnin_no")
		changes = append(changes, ItemFieldChange{Field: "isCollectableTurnIn", Label: i18n.T(lang, "backup.collectable_turnin"), Before: boolLabel(*before.IsCollectableTurnIn, y, n), After: boolLabel(*after.IsCollectableTurnIn, y, n)})
	}
	if before.IsCollectableReward != nil && after.IsCollectableReward != nil && *before.IsCollectableReward != *after.IsCollectableReward {
		colChanged = true
		y, n := i18n.T(lang, "backup.col_reward_yes"), i18n.T(lang, "backup.col_reward_no")
		changes = append(changes, ItemFieldChange{Field: "isCollectableReward", Label: i18n.T(lang, "backup.collectable_reward"), Before: boolLabel(*before.IsCollectableReward, y, n), After: boolLabel(*after.IsCollectableReward, y, n)})
	}
	if before.CollectableSummary != after.CollectableSummary && (before.CollectableSummary != "" || after.CollectableSummary != "") {
		colChanged = true
		changes = append(changes, ItemFieldChange{
			Field:  "collectableSummary",
			Label:  i18n.T(lang, "backup.collectable_trade"),
			Before: nonEmptyOr(before.CollectableSummary, noneStr),
			After:  nonEmptyOr(after.CollectableSummary, noneStr),
		})
	}
	return changes, colChanged
}

func diffSingleQuest(lang i18n.Lang, before, after compactQuestRecord) []ItemFieldChange {
	var changes []ItemFieldChange
	noneStr := i18n.T(lang, "common.none")
	if before.NameCn != after.NameCn {
		changes = append(changes, ItemFieldChange{Field: "nameCn", Label: i18n.T(lang, "backup.quest_field_name"), Before: before.NameCn, After: after.NameCn})
	}
	if before.Level != after.Level {
		changes = append(changes, ItemFieldChange{Field: "level", Label: i18n.T(lang, "backup.quest_field_level"), Before: fmt.Sprintf("Lv.%d", before.Level), After: fmt.Sprintf("Lv.%d", after.Level)})
	}
	if before.GenreName != after.GenreName {
		changes = append(changes, ItemFieldChange{Field: "genreName", Label: i18n.T(lang, "backup.quest_field_genre"), Before: nonEmptyOr(before.GenreName, noneStr), After: nonEmptyOr(after.GenreName, noneStr)})
	}
	if before.IssuerName != after.IssuerName {
		changes = append(changes, ItemFieldChange{Field: "issuerName", Label: i18n.T(lang, "backup.quest_field_issuer"), Before: nonEmptyOr(before.IssuerName, noneStr), After: nonEmptyOr(after.IssuerName, noneStr)})
	}
	if before.GilReward != after.GilReward {
		changes = append(changes, ItemFieldChange{
			Field:  "gilReward",
			Label:  i18n.T(lang, "backup.quest_field_gil"),
			Before: i18n.Tf(lang, "common.gil_amount", "amount", strconv.Itoa(before.GilReward)),
			After:  i18n.Tf(lang, "common.gil_amount", "amount", strconv.Itoa(after.GilReward)),
		})
	}
	if before.ExpReward != after.ExpReward {
		changes = append(changes, ItemFieldChange{
			Field:  "expReward",
			Label:  i18n.T(lang, "backup.quest_field_exp"),
			Before: i18n.Tf(lang, "common.exp_amount", "amount", strconv.Itoa(before.ExpReward)),
			After:  i18n.Tf(lang, "common.exp_amount", "amount", strconv.Itoa(after.ExpReward)),
		})
	}
	if before.RewardSummary != after.RewardSummary {
		changes = append(changes, ItemFieldChange{Field: "rewardSummary", Label: i18n.T(lang, "backup.quest_field_reward"), Before: nonEmptyOr(before.RewardSummary, noneStr), After: nonEmptyOr(after.RewardSummary, noneStr)})
	}
	if before.CutsceneCount != nil && after.CutsceneCount != nil && (*before.CutsceneCount != *after.CutsceneCount || before.CutsceneSummary != after.CutsceneSummary) {
		changes = append(changes, ItemFieldChange{
			Field:  "cutscenes",
			Label:  i18n.T(lang, "backup.quest_field_cutscene"),
			Before: formatQuestCutsceneDiffVal(lang, *before.CutsceneCount, before.CutsceneSummary, noneStr),
			After:  formatQuestCutsceneDiffVal(lang, *after.CutsceneCount, after.CutsceneSummary, noneStr),
		})
	}
	return changes
}

func formatQuestCutsceneDiffVal(lang i18n.Lang, cnt int, summary, noneStr string) string {
	if cnt <= 0 {
		return noneStr
	}
	if summary != "" {
		return i18n.Tf(lang, "backup.quest_cutscene_val", "count", strconv.Itoa(cnt), "summary", summary)
	}
	return strconv.Itoa(cnt)
}

func diffSingleLeve(lang i18n.Lang, before, after compactLeveRecord) []ItemFieldChange {
	var changes []ItemFieldChange
	noneStr := i18n.T(lang, "common.none")
	if before.NameCn != after.NameCn {
		changes = append(changes, ItemFieldChange{Field: "nameCn", Label: i18n.T(lang, "backup.leve_field_name"), Before: before.NameCn, After: after.NameCn})
	}
	if before.ClassJobLevel != after.ClassJobLevel {
		changes = append(changes, ItemFieldChange{Field: "classJobLevel", Label: i18n.T(lang, "backup.leve_field_level"), Before: fmt.Sprintf("Lv.%d", before.ClassJobLevel), After: fmt.Sprintf("Lv.%d", after.ClassJobLevel)})
	}
	if before.AssignmentTypeName != after.AssignmentTypeName {
		changes = append(changes, ItemFieldChange{Field: "assignmentTypeName", Label: i18n.T(lang, "backup.leve_field_type"), Before: nonEmptyOr(before.AssignmentTypeName, noneStr), After: nonEmptyOr(after.AssignmentTypeName, noneStr)})
	}
	if before.ExpReward != after.ExpReward {
		changes = append(changes, ItemFieldChange{
			Field:  "expReward",
			Label:  i18n.T(lang, "backup.leve_field_exp"),
			Before: i18n.Tf(lang, "common.exp_amount", "amount", strconv.Itoa(before.ExpReward)),
			After:  i18n.Tf(lang, "common.exp_amount", "amount", strconv.Itoa(after.ExpReward)),
		})
	}
	if before.GilReward != after.GilReward {
		changes = append(changes, ItemFieldChange{
			Field:  "gilReward",
			Label:  i18n.T(lang, "backup.leve_field_gil"),
			Before: i18n.Tf(lang, "common.gil_amount", "amount", strconv.Itoa(before.GilReward)),
			After:  i18n.Tf(lang, "common.gil_amount", "amount", strconv.Itoa(after.GilReward)),
		})
	}
	if before.CraftRepeats != after.CraftRepeats {
		changes = append(changes, ItemFieldChange{
			Field:  "craftRepeats",
			Label:  i18n.T(lang, "backup.leve_field_repeats"),
			Before: i18n.Tf(lang, "backup.leve_repeats_val", "count", strconv.Itoa(before.CraftRepeats)),
			After:  i18n.Tf(lang, "backup.leve_repeats_val", "count", strconv.Itoa(after.CraftRepeats)),
		})
	}
	if before.TurnInSummary != after.TurnInSummary {
		changes = append(changes, ItemFieldChange{Field: "turnInSummary", Label: i18n.T(lang, "backup.leve_field_turnin"), Before: nonEmptyOr(before.TurnInSummary, noneStr), After: nonEmptyOr(after.TurnInSummary, noneStr)})
	}
	if before.RewardSummary != after.RewardSummary {
		changes = append(changes, ItemFieldChange{Field: "rewardSummary", Label: i18n.T(lang, "backup.leve_field_pool"), Before: nonEmptyOr(before.RewardSummary, noneStr), After: nonEmptyOr(after.RewardSummary, noneStr)})
	}
	return changes
}

func diffSingleAchievement(lang i18n.Lang, before, after compactAchievementRecord) []ItemFieldChange {
	var changes []ItemFieldChange
	noneStr := i18n.T(lang, "common.none")
	if before.NameCn != after.NameCn {
		changes = append(changes, ItemFieldChange{Field: "nameCn", Label: i18n.T(lang, "backup.ach_field_name"), Before: before.NameCn, After: after.NameCn})
	}
	if before.DescriptionCn != after.DescriptionCn {
		changes = append(changes, ItemFieldChange{Field: "descriptionCn", Label: i18n.T(lang, "backup.ach_field_desc"), Before: before.DescriptionCn, After: after.DescriptionCn})
	}
	if before.Points != after.Points {
		changes = append(changes, ItemFieldChange{
			Field:  "points",
			Label:  i18n.T(lang, "backup.ach_field_points"),
			Before: i18n.Tf(lang, "backup.ach_points", "pts", strconv.Itoa(before.Points)),
			After:  i18n.Tf(lang, "backup.ach_points", "pts", strconv.Itoa(after.Points)),
		})
	}
	if before.TitleMasculineCn != after.TitleMasculineCn {
		changes = append(changes, ItemFieldChange{Field: "title", Label: i18n.T(lang, "backup.ach_field_title"), Before: nonEmptyOr(before.TitleMasculineCn, noneStr), After: nonEmptyOr(after.TitleMasculineCn, noneStr)})
	}
	if before.RewardItemID != after.RewardItemID {
		changes = append(changes, ItemFieldChange{Field: "rewardItem", Label: i18n.T(lang, "backup.ach_field_reward_item"), Before: nonEmptyOr(before.RewardItemNameCn, noneStr), After: nonEmptyOr(after.RewardItemNameCn, noneStr)})
	}
	return changes
}

func boolLabel(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

func nonEmptyOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func summarizeCollectableTurnIns(lang i18n.Lang, list []gamedata.CollectableTurnInSource) string {
	if len(list) == 0 {
		return ""
	}
	scripFallback := i18n.T(lang, "backup.scrip_fallback")
	limit := min(2, len(list))
	parts := make([]string, 0, limit+1)
	for _, src := range list[:limit] {
		s := src.SourceLabel
		if src.LevelMin > 0 {
			s += fmt.Sprintf(" Lv.%d", src.LevelMin)
		}
		if len(src.Tiers) > 0 {
			lo := src.Tiers[0]
			hi := src.Tiers[len(src.Tiers)-1]
			if len(src.Tiers) > 1 {
				s += fmt.Sprintf(" %d+→%d/%d+→%d(%s)", lo.MinCollectability, lo.RewardAmount, hi.MinCollectability, hi.RewardAmount, nonEmptyOr(hi.RewardItemName, scripFallback))
			} else {
				s += fmt.Sprintf(" %d+→%d(%s)", hi.MinCollectability, hi.RewardAmount, nonEmptyOr(hi.RewardItemName, scripFallback))
			}
		}
		parts = append(parts, s)
	}
	if len(list) > limit {
		parts = append(parts, i18n.Tf(lang, "backup.etc_items", "count", strconv.Itoa(len(list))))
	}
	return strings.Join(parts, "; ")
}

func summarizeQuestRewards(lang i18n.Lang, q gamedata.QuestEntry) string {
	var parts []string
	limit := min(3, len(q.RewardItems))
	for _, ri := range q.RewardItems[:limit] {
		parts = append(parts, fmt.Sprintf("%s×%d", ri.ItemName, ri.Count))
	}
	if len(q.RewardItems) > limit {
		parts = append(parts, i18n.Tf(lang, "backup.etc_pieces", "count", strconv.Itoa(len(q.RewardItems))))
	}
	if q.CurrencyRewardName != "" && q.CurrencyRewardCount > 0 {
		parts = append(parts, fmt.Sprintf("%s×%d", q.CurrencyRewardName, q.CurrencyRewardCount))
	}
	if q.TomestoneName != "" && q.TomestoneCount > 0 {
		parts = append(parts, fmt.Sprintf("%s×%d", q.TomestoneName, q.TomestoneCount))
	}
	if len(q.Unlocks) > 0 {
		parts = append(parts, q.Unlocks[0].Label)
	}
	return strings.Join(parts, ", ")
}

func summarizeLeveTurnIns(lang i18n.Lang, lv gamedata.LeveEntry) string {
	if len(lv.TurnInItems) == 0 {
		return ""
	}
	var parts []string
	for _, ti := range lv.TurnInItems {
		parts = append(parts, fmt.Sprintf("%s×%d", ti.ItemName, ti.Count))
	}
	if lv.CraftRepeats > 0 {
		parts = append(parts, i18n.Tf(lang, "backup.leve_extra_rounds", "count", strconv.Itoa(lv.CraftRepeats)))
	}
	return strings.Join(parts, ", ")
}

func summarizeLeveRewards(lang i18n.Lang, lv gamedata.LeveEntry) string {
	if len(lv.RewardGroups) == 0 {
		return ""
	}
	limit := min(2, len(lv.RewardGroups))
	var parts []string
	for _, grp := range lv.RewardGroups[:limit] {
		if len(grp.Items) == 0 {
			continue
		}
		first := grp.Items[0]
		s := fmt.Sprintf("%d%%:%s×%d", grp.ProbabilityPercent, first.ItemName, first.Count)
		if len(grp.Items) > 1 {
			s += i18n.Tf(lang, "backup.etc_kinds", "count", strconv.Itoa(len(grp.Items)))
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

func boolPtr(b bool) *bool {
	v := b
	return &v
}

func buildPayloadFromStore(
	store *gamedata.Store,
	meta VersionBackupMeta,
	lang i18n.Lang,
	icons []compactIconRecord,
	computeLiveBGM bool,
) *snapshotPayload {
	if store == nil {
		return &snapshotPayload{Meta: meta, Icons: icons}
	}

	items := make([]compactItemRecord, 0, len(store.ItemSummaries))
	for _, it := range store.ItemSummaries {
		items = append(items, compactItemRecord{
			ID:                  it.ID,
			NameCn:              it.NameCn,
			NameEn:              it.NameEn,
			NameJa:              it.NameJa,
			IconID:              it.IconID,
			ItemLevel:           it.ItemLevel,
			EquipLevel:          it.EquipLevel,
			CategoryID:          it.CategoryID,
			CategoryName:        it.CategoryName,
			PriceMid:            it.PriceMid,
			IsCraftable:         it.IsCraftable,
			IsGatherable:        it.IsGatherable,
			IsPurchasable:       it.IsPurchasable,
			IsRetainerVenture:   boolPtr(it.IsRetainerVenture),
			IsCollectableTurnIn: boolPtr(it.IsCollectableTurnIn),
			IsCollectableReward: boolPtr(it.IsCollectableReward),
			CollectableSummary:  summarizeCollectableTurnIns(lang, store.CollectableTurnInsByItem[it.ID]),
		})
	}

	quests := make([]compactQuestRecord, 0, len(store.Quests))
	for _, q := range store.Quests {
		primaryItem := q.CurrencyRewardItemID
		if len(q.RewardItems) > 0 {
			primaryItem = q.RewardItems[0].ItemID
		} else if q.TomestoneItemID > 0 {
			primaryItem = q.TomestoneItemID
		}
		cutCnt := len(q.Cutscenes)
		quests = append(quests, compactQuestRecord{
			ID:              q.ID,
			NameCn:          q.NameCn,
			NameEn:          q.NameEn,
			NameJa:          q.NameJa,
			IconID:          q.IconID,
			Level:           q.Level,
			GenreName:       q.GenreName,
			SectionName:     q.SectionName,
			ExpansionName:   q.ExpansionName,
			IssuerName:      q.IssuerName,
			GilReward:       q.GilReward,
			ExpReward:       q.ExpReward,
			RewardSummary:   summarizeQuestRewards(lang, q),
			PrimaryItemID:   primaryItem,
			IsMSQ:           q.IsMSQ,
			IsFeature:       q.IsFeature,
			IsRepeatable:    q.IsRepeatable,
			CutsceneCount:   &cutCnt,
			CutsceneSummary: gamedata.SummarizeQuestCutscenes(q.Cutscenes),
		})
	}

	leves := make([]compactLeveRecord, 0, len(store.Leves))
	for _, lv := range store.Leves {
		iconID := lv.AssignmentTypeIconID
		if iconID <= 0 {
			iconID = lv.IconCityState
		}
		leves = append(leves, compactLeveRecord{
			ID:                 lv.ID,
			NameCn:             lv.NameCn,
			NameEn:             lv.NameEn,
			NameJa:             lv.NameJa,
			IconID:             iconID,
			LeveKind:           lv.LeveKind,
			LeveKindLabel:      lv.LeveKindLabel,
			AssignmentTypeName: lv.AssignmentTypeName,
			ClassJobLevel:      lv.ClassJobLevel,
			ExpReward:          lv.ExpReward,
			GilReward:          lv.GilReward,
			CraftRepeats:       lv.CraftRepeats,
			TurnInSummary:      summarizeLeveTurnIns(lang, lv),
			RewardSummary:      summarizeLeveRewards(lang, lv),
		})
	}

	achs := make([]compactAchievementRecord, 0, len(store.Achievements))
	for _, a := range store.Achievements {
		achs = append(achs, compactAchievementRecord{
			ID:               a.ID,
			NameCn:           a.NameCn,
			NameEn:           a.NameEn,
			NameJa:           a.NameJa,
			DescriptionCn:    a.DescriptionCn,
			Points:           a.Points,
			IconID:           a.IconID,
			KindName:         a.KindName,
			CategoryName:     a.CategoryName,
			TitleID:          a.TitleID,
			TitleMasculineCn: a.TitleMasculineCn,
			TitleFeminineCn:  a.TitleFeminineCn,
			TitleNameJa:      a.TitleNameJa,
			TitleNameEn:      a.TitleNameEn,
			RewardItemID:     a.RewardItemID,
			RewardItemNameCn: a.RewardItemNameCn,
			RewardItemIconID: a.RewardItemIconID,
		})
	}

	bgms := make([]compactBGMRecord, 0, len(store.BGMs))
	if len(store.BGMs) > 0 {
		var digests map[int]gamedata.BGMDigest
		if computeLiveBGM && store.PrimaryReader != nil {
			digests = store.ComputeBGMDigests(false)
		}
		for _, b := range store.BGMs {
			fileExists := b.FileExists
			fileSize := b.FileSize
			contentMD5 := b.ContentMD5
			if d, ok := digests[b.ID]; ok {
				fileExists = d.FileExists
				fileSize = d.FileSize
				contentMD5 = d.ContentMD5
			}
			bgms = append(bgms, compactBGMRecord{
				ID:            b.ID,
				File:          b.File,
				Title:         b.Title,
				NameCn:        b.NameCn,
				NameEn:        b.NameEn,
				NameJa:        b.NameJa,
				ExpansionID:   b.ExpansionID,
				ExpansionName: b.ExpansionName,
				UsageSummary:  b.UsageSummary,
				IsPlaceholder: b.IsPlaceholder,
				FileExists:    fileExists,
				FileSize:      fileSize,
				ContentMD5:    contentMD5,
			})
		}
	}

	return &snapshotPayload{
		Meta:         meta,
		RecipeCount:  len(store.Recipes),
		Items:        items,
		Quests:       quests,
		Leves:        leves,
		Achievements: achs,
		BGMs:         bgms,
		Icons:        icons,
	}
}
