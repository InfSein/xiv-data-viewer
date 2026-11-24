package backup

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
)

// VersionBackupMeta 保存单个本地版本快照的元信息。
type VersionBackupMeta struct {
	Version              string `json:"version"`
	GlobalVersion        string `json:"globalVersion"`
	CreatedAt            string `json:"createdAt"`
	ItemCount            int    `json:"itemCount"`
	RecipeCount          int    `json:"recipeCount"`
	GatherableCount      int    `json:"gatherableCount"`
	PurchasableCount     int    `json:"purchasableCount"`
	AchievementCount     int    `json:"achievementCount"`
	TitleCount           int    `json:"titleCount"`
	QuestCount           int    `json:"questCount,omitempty"`
	LeveCount            int    `json:"leveCount,omitempty"`
	CollectableCount     int    `json:"collectableCount,omitempty"`
	CollectableItemCount int    `json:"collectableItemCount,omitempty"`
	IsBaseline           bool   `json:"isBaseline"`
}

// VersionStatusResponse 汇总当前客户端版本与已备份快照清单。
type VersionStatusResponse struct {
	CurrentVersion   string              `json:"currentVersion"`
	GlobalVersion    string              `json:"globalVersion"`
	PreviousVersion  string              `json:"previousVersion"`
	HasVersionUpdate bool                `json:"hasVersionUpdate"`
	LastBackupAt     string              `json:"lastBackupAt"`
	BackupDir        string              `json:"backupDir"`
	Backups          []VersionBackupMeta `json:"backups"`
}

// ItemFieldChange 描述版本对比中单个字段的变更前后对比。
type ItemFieldChange struct {
	Field  string `json:"field"`
	Label  string `json:"label"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// DiffItemEntry 描述版本对比结果中的单条道具、任务、理符或成就差异记录。
type DiffItemEntry struct {
	ID                   int               `json:"id"`
	EntityType           string            `json:"entityType"` // item|quest|leve|achievement
	ChangeType           string            `json:"changeType"` // added|modified|removed
	NameCn               string            `json:"nameCn"`
	NameEn               string            `json:"nameEn"`
	IconID               int               `json:"iconId"`
	ItemLevel            int               `json:"itemLevel"`
	EquipLevel           int               `json:"equipLevel"`
	CategoryName         string            `json:"categoryName"`
	PriceMid             int               `json:"priceMid"`
	IsCraftable          bool              `json:"isCraftable"`
	IsGatherable         bool              `json:"isGatherable"`
	IsPurchasable        bool              `json:"isPurchasable"`
	HasCollectableChange bool              `json:"hasCollectableChange,omitempty"`
	Points               int               `json:"points"`
	TitleNameCn          string            `json:"titleNameCn"`
	RewardItemID         int               `json:"rewardItemId"`
	RewardItemNameCn     string            `json:"rewardItemNameCn"`
	Changes              []ItemFieldChange `json:"changes"`
}

// DiffSummary 汇总两个版本之间的变更统计数字。
type DiffSummary struct {
	AddedItems                int `json:"addedItems"`
	ModifiedItems             int `json:"modifiedItems"`
	RemovedItems              int `json:"removedItems"`
	AddedRecipes              int `json:"addedRecipes"`
	NewCraftableItems         int `json:"newCraftableItems"`
	NewGatherableItems        int `json:"newGatherableItems"`
	NewPurchasableItems       int `json:"newPurchasableItems"`
	NewCollectableTurnInItems int `json:"newCollectableTurnInItems"`
	ModifiedCollectableItems  int `json:"modifiedCollectableItems"`
	AddedQuests               int `json:"addedQuests"`
	ModifiedQuests            int `json:"modifiedQuests"`
	RemovedQuests             int `json:"removedQuests"`
	AddedLeves                int `json:"addedLeves"`
	ModifiedLeves             int `json:"modifiedLeves"`
	RemovedLeves              int `json:"removedLeves"`
	AddedAchievements         int `json:"addedAchievements"`
	ModifiedAchievements      int `json:"modifiedAchievements"`
	RemovedAchievements       int `json:"removedAchievements"`
	AddedTitles               int `json:"addedTitles"`
}

// VersionDiffQuery 版本差异检索条件。
type VersionDiffQuery struct {
	From       string
	To         string
	Filter     string // all|added|modified|removed
	EntityType string // all|item|quest|leve|achievement
	Q          string
	Page       int
	PageSize   int
}

// VersionDiffResponse 版本对比分页结果。
type VersionDiffResponse struct {
	FromVersion   string          `json:"fromVersion"`
	ToVersion     string          `json:"toVersion"`
	FromCreatedAt string          `json:"fromCreatedAt"`
	ToCreatedAt   string          `json:"toCreatedAt"`
	Summary       DiffSummary     `json:"summary"`
	Items         []DiffItemEntry `json:"items"`
	TotalFiltered int             `json:"totalFiltered"`
	Page          int             `json:"page"`
	PageSize      int             `json:"pageSize"`
	TotalPages    int             `json:"totalPages"`
}

type compactItemRecord struct {
	ID                  int    `json:"id"`
	NameCn              string `json:"nameCn"`
	NameEn              string `json:"nameEn"`
	IconID              int    `json:"iconId"`
	ItemLevel           int    `json:"itemLevel"`
	EquipLevel          int    `json:"equipLevel"`
	CategoryID          int    `json:"categoryId"`
	CategoryName        string `json:"categoryName"`
	PriceMid            int    `json:"priceMid"`
	IsCraftable         bool   `json:"isCraftable"`
	IsGatherable        bool   `json:"isGatherable"`
	IsPurchasable       bool   `json:"isPurchasable"`
	IsRetainerVenture   *bool  `json:"isRetainerVenture,omitempty"`
	IsCollectableTurnIn *bool  `json:"isCollectableTurnIn,omitempty"`
	IsCollectableReward *bool  `json:"isCollectableReward,omitempty"`
	CollectableSummary  string `json:"collectableSummary,omitempty"`
}

type compactQuestRecord struct {
	ID            int    `json:"id"`
	NameCn        string `json:"nameCn"`
	NameEn        string `json:"nameEn"`
	IconID        int    `json:"iconId"`
	Level         int    `json:"level"`
	GenreName     string `json:"genreName"`
	SectionName   string `json:"sectionName"`
	ExpansionName string `json:"expansionName"`
	IssuerName    string `json:"issuerName"`
	GilReward     int    `json:"gilReward"`
	ExpReward     int    `json:"expReward"`
	RewardSummary string `json:"rewardSummary,omitempty"`
	PrimaryItemID int    `json:"primaryItemId,omitempty"`
	IsMSQ         bool   `json:"isMsq"`
	IsFeature     bool   `json:"isFeature"`
	IsRepeatable  bool   `json:"isRepeatable"`
}

type compactLeveRecord struct {
	ID                 int    `json:"id"`
	NameCn             string `json:"nameCn"`
	NameEn             string `json:"nameEn"`
	IconID             int    `json:"iconId"`
	LeveKind           string `json:"leveKind"`
	LeveKindLabel      string `json:"leveKindLabel"`
	AssignmentTypeName string `json:"assignmentTypeName"`
	ClassJobLevel      int    `json:"classJobLevel"`
	ExpReward          int    `json:"expReward"`
	GilReward          int    `json:"gilReward"`
	CraftRepeats       int    `json:"craftRepeats"`
	TurnInSummary      string `json:"turnInSummary,omitempty"`
	RewardSummary      string `json:"rewardSummary,omitempty"`
}

type compactAchievementRecord struct {
	ID               int    `json:"id"`
	NameCn           string `json:"nameCn"`
	NameEn           string `json:"nameEn"`
	DescriptionCn    string `json:"descriptionCn"`
	Points           int    `json:"points"`
	IconID           int    `json:"iconId"`
	KindName         string `json:"kindName"`
	CategoryName     string `json:"categoryName"`
	TitleID          int    `json:"titleId"`
	TitleMasculineCn string `json:"titleMasculineCn"`
	TitleFeminineCn  string `json:"titleFeminineCn"`
	RewardItemID     int    `json:"rewardItemId"`
	RewardItemNameCn string `json:"rewardItemNameCn"`
}

type snapshotPayload struct {
	Meta         VersionBackupMeta          `json:"meta"`
	Items        []compactItemRecord        `json:"items"`
	Quests       []compactQuestRecord       `json:"quests,omitempty"`
	Leves        []compactLeveRecord        `json:"leves,omitempty"`
	Achievements []compactAchievementRecord `json:"achievements,omitempty"`
}

type backupManifest struct {
	LatestVersion    string              `json:"latestVersion"`
	PreviousVersion  string              `json:"previousVersion"`
	HasVersionUpdate bool                `json:"hasVersionUpdate"`
	LastBackupAt     string              `json:"lastBackupAt"`
	Backups          []VersionBackupMeta `json:"backups"`
}

var unsafeFileCharsRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

func sanitizeVersionFileName(version string) string {
	return unsafeFileCharsRe.ReplaceAllString(version, "_")
}

// Service 管理本地客户端版本快照备份与跨版本字段级 Diff 计算。
type Service struct {
	store        *gamedata.Store
	backupDir    string
	manifestPath string

	mu            sync.Mutex
	manifest      *backupManifest
	snapshotCache map[string]*snapshotPayload
}

// NewService 创建版本快照备份服务，快照保存在 backupDir 下。
func NewService(store *gamedata.Store, backupDir string) *Service {
	return &Service{
		store:         store,
		backupDir:     backupDir,
		manifestPath:  filepath.Join(backupDir, "manifest.json"),
		snapshotCache: make(map[string]*snapshotPayload),
	}
}

func (s *Service) snapshotFilePath(version string) string {
	return filepath.Join(s.backupDir, fmt.Sprintf("snapshot-%s.json", sanitizeVersionFileName(version)))
}

func (s *Service) snapshotExists(version string) bool {
	_, err := os.Stat(s.snapshotFilePath(version))
	return err == nil
}

func (s *Service) readManifest() *backupManifest {
	raw, err := os.ReadFile(s.manifestPath)
	if err != nil {
		return nil
	}
	var m backupManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return &m
}

func (s *Service) writeManifest(m *backupManifest) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.manifestPath, raw, 0o644)
}

func (s *Service) writeSnapshot(version string, payload *snapshotPayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.snapshotFilePath(version), raw, 0o644); err != nil {
		return err
	}
	s.snapshotCache[version] = payload
	return nil
}

func (s *Service) loadSnapshot(version string) *snapshotPayload {
	if cached, ok := s.snapshotCache[version]; ok {
		return cached
	}
	raw, err := os.ReadFile(s.snapshotFilePath(version))
	if err != nil {
		return nil
	}
	var payload snapshotPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	s.snapshotCache[version] = &payload
	return &payload
}

func deriveBaselineVersionName(currentVersion string) string {
	if currentVersion == "2026.09.01.0000.0000" {
		return "2026.08.01.0000.0000"
	}
	return "2026.09.01.0000.0000"
}

// InitializeOnStartup 启动时校验客户端版本号、自动维护本地快照并检测是否发生版本更新。
func (s *Service) InitializeOnStartup() (VersionStatusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.backupDir, 0o755); err != nil {
		return VersionStatusResponse{}, fmt.Errorf("创建版本备份目录失败: %w", err)
	}

	currentVersion := s.store.PrimaryVersion
	existing := s.readManifest()
	currentSnap := s.buildLiveSnapshot(currentVersion)

	baselineVer := deriveBaselineVersionName(currentVersion)
	var baselineMeta *VersionBackupMeta
	if !s.snapshotExists(baselineVer) {
		baseSnap := s.buildBaselineSnapshot(baselineVer, currentSnap)
		if err := s.writeSnapshot(baselineVer, baseSnap); err != nil {
			return VersionStatusResponse{}, err
		}
		metaCopy := baseSnap.Meta
		baselineMeta = &metaCopy
	} else {
		loaded := s.loadSnapshot(baselineVer)
		if !snapshotNeedsSchemaUpgrade(loaded, currentSnap) {
			metaCopy := loaded.Meta
			baselineMeta = &metaCopy
		} else {
			baseSnap := s.buildBaselineSnapshot(baselineVer, currentSnap)
			if err := s.writeSnapshot(baselineVer, baseSnap); err != nil {
				return VersionStatusResponse{}, err
			}
			metaCopy := baseSnap.Meta
			baselineMeta = &metaCopy
		}
	}

	currentExists := s.snapshotExists(currentVersion)
	if err := s.writeSnapshot(currentVersion, currentSnap); err != nil {
		return VersionStatusResponse{}, err
	}

	var prevVersion string
	var hasUpdate bool
	backupsMap := make(map[string]VersionBackupMeta)
	if baselineMeta != nil {
		backupsMap[baselineMeta.Version] = *baselineMeta
	}

	if existing != nil {
		for _, b := range existing.Backups {
			if s.snapshotExists(b.Version) {
				backupsMap[b.Version] = b
			}
		}
		if existing.LatestVersion != currentVersion {
			prevVersion = existing.LatestVersion
			hasUpdate = true
		} else {
			prevVersion = existing.PreviousVersion
			if prevVersion == "" && baselineMeta != nil {
				prevVersion = baselineMeta.Version
			}
			hasUpdate = existing.HasVersionUpdate && !currentExists
		}
	} else if baselineMeta != nil {
		prevVersion = baselineMeta.Version
	}

	backupsMap[currentVersion] = currentSnap.Meta

	backups := make([]VersionBackupMeta, 0, len(backupsMap))
	for _, m := range backupsMap {
		backups = append(backups, m)
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].IsBaseline != backups[j].IsBaseline {
			return !backups[i].IsBaseline
		}
		return backups[i].Version > backups[j].Version
	})

	s.manifest = &backupManifest{
		LatestVersion:    currentVersion,
		PreviousVersion:  prevVersion,
		HasVersionUpdate: hasUpdate,
		LastBackupAt:     currentSnap.Meta.CreatedAt,
		Backups:          backups,
	}
	if err := s.writeManifest(s.manifest); err != nil {
		return VersionStatusResponse{}, err
	}
	return s.statusLocked(), nil
}

func (s *Service) statusLocked() VersionStatusResponse {
	var prev, lastAt string
	var hasUpd bool
	var backups []VersionBackupMeta
	if s.manifest != nil {
		prev = s.manifest.PreviousVersion
		hasUpd = s.manifest.HasVersionUpdate
		lastAt = s.manifest.LastBackupAt
		backups = append([]VersionBackupMeta(nil), s.manifest.Backups...)
	} else {
		lastAt = time.Now().UTC().Format(time.RFC3339)
	}
	return VersionStatusResponse{
		CurrentVersion:   s.store.PrimaryVersion,
		GlobalVersion:    s.store.GlobalVersion,
		PreviousVersion:  prev,
		HasVersionUpdate: hasUpd,
		LastBackupAt:     lastAt,
		BackupDir:        s.backupDir,
		Backups:          backups,
	}
}

// GetVersionStatus 返回当前版本与快照备份状态。
func (s *Service) GetVersionStatus() VersionStatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

// CreateOrRefreshBackup 手动刷新当前版本的本地快照备份。
func (s *Service) CreateOrRefreshBackup() (VersionStatusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentVersion := s.store.PrimaryVersion
	payload := s.buildLiveSnapshot(currentVersion)
	if err := s.writeSnapshot(currentVersion, payload); err != nil {
		return VersionStatusResponse{}, err
	}
	if s.manifest != nil {
		s.manifest.LastBackupAt = payload.Meta.CreatedAt
		updated := false
		for i, b := range s.manifest.Backups {
			if b.Version == currentVersion {
				s.manifest.Backups[i] = payload.Meta
				updated = true
				break
			}
		}
		if !updated {
			s.manifest.Backups = append([]VersionBackupMeta{payload.Meta}, s.manifest.Backups...)
		}
		if err := s.writeManifest(s.manifest); err != nil {
			return VersionStatusResponse{}, err
		}
	}
	return s.statusLocked(), nil
}

func snapshotNeedsSchemaUpgrade(loaded, current *snapshotPayload) bool {
	if loaded == nil {
		return true
	}
	if len(current.Achievements) > 0 && len(loaded.Achievements) == 0 {
		return true
	}
	if len(current.Quests) > 0 && len(loaded.Quests) == 0 {
		return true
	}
	if len(current.Leves) > 0 && len(loaded.Leves) == 0 {
		return true
	}
	if current.Meta.CollectableItemCount > 0 && loaded.Meta.CollectableItemCount == 0 {
		return true
	}
	return false
}

// CompareVersions 计算两个本地快照之间的道具（含收藏品）、任务、理符与成就字段级差异。
func (s *Service) CompareVersions(query VersionDiffQuery) (VersionDiffResponse, error) {
	s.mu.Lock()
	if s.manifest == nil {
		s.mu.Unlock()
		if _, err := s.InitializeOnStartup(); err != nil {
			return VersionDiffResponse{}, err
		}
		s.mu.Lock()
	}
	defer s.mu.Unlock()

	toVersion := query.To
	if toVersion == "" {
		toVersion = s.store.PrimaryVersion
	}
	fromVersion := query.From
	if fromVersion == "" && s.manifest != nil {
		fromVersion = s.manifest.PreviousVersion
	}
	if fromVersion == "" {
		fromVersion = deriveBaselineVersionName(s.store.PrimaryVersion)
	}

	toSnap := s.loadSnapshot(toVersion)
	if toSnap == nil {
		toSnap = s.buildLiveSnapshot(s.store.PrimaryVersion)
	}
	fromSnap := s.loadSnapshot(fromVersion)
	if fromSnap == nil {
		fromSnap = s.buildBaselineSnapshot(fromVersion, toSnap)
	}

	var allEntries []DiffItemEntry
	var summary DiffSummary

	allEntries = append(allEntries, diffItems(fromSnap, toSnap, &summary)...)
	allEntries = append(allEntries, diffQuests(fromSnap, toSnap, &summary)...)
	allEntries = append(allEntries, diffLeves(fromSnap, toSnap, &summary)...)
	allEntries = append(allEntries, diffAchievements(fromSnap, toSnap, &summary)...)
	summary.AddedRecipes = max(0, toSnap.Meta.RecipeCount-fromSnap.Meta.RecipeCount)

	filter := query.Filter
	if filter == "" {
		filter = "all"
	}
	entityType := query.EntityType
	if entityType == "" {
		entityType = "all"
	}
	q := strings.ToLower(strings.TrimSpace(query.Q))

	filtered := make([]DiffItemEntry, 0, len(allEntries))
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

	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	totalFiltered := len(filtered)
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
		items = filtered[start:end]
	}

	return VersionDiffResponse{
		FromVersion:   fromSnap.Meta.Version,
		ToVersion:     toSnap.Meta.Version,
		FromCreatedAt: fromSnap.Meta.CreatedAt,
		ToCreatedAt:   toSnap.Meta.CreatedAt,
		Summary:       summary,
		Items:         items,
		TotalFiltered: totalFiltered,
		Page:          page,
		PageSize:      pageSize,
		TotalPages:    totalPages,
	}, nil
}

func diffEntryMatchesQuery(e DiffItemEntry, q string) bool {
	if strings.Contains(strings.ToLower(e.NameCn), q) ||
		strings.Contains(strings.ToLower(e.NameEn), q) ||
		strings.Contains(strings.ToLower(e.CategoryName), q) ||
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

func diffItems(fromSnap, toSnap *snapshotPayload, summary *DiffSummary) []DiffItemEntry {
	fromItems := make(map[int]compactItemRecord, len(fromSnap.Items))
	for _, it := range fromSnap.Items {
		fromItems[it.ID] = it
	}
	toItems := make(map[int]compactItemRecord, len(toSnap.Items))
	for _, it := range toSnap.Items {
		toItems[it.ID] = it
	}

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
			afterDesc := fmt.Sprintf("品级 %d · %s", toItem.ItemLevel, toItem.CategoryName)
			changes := []ItemFieldChange{{
				Field:  "status",
				Label:  "版本新增",
				Before: "无",
				After:  afterDesc,
			}}
			if toCol && toItem.CollectableSummary != "" {
				changes = append(changes, ItemFieldChange{
					Field:  "collectableSummary",
					Label:  "收藏品交纳",
					Before: "无",
					After:  toItem.CollectableSummary,
				})
			}
			entries = append(entries, DiffItemEntry{
				ID:                   toItem.ID,
				EntityType:           "item",
				ChangeType:           "added",
				NameCn:               toItem.NameCn,
				NameEn:               toItem.NameEn,
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

		changes, colChanged := diffSingleItem(fromItem, toItem)
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
					Label:  "版本移除",
					Before: fmt.Sprintf("品级 %d", fromItem.ItemLevel),
					After:  "已移除",
				}},
			})
		}
	}
	return entries
}

func questCategoryLabel(q compactQuestRecord) string {
	genre := nonEmptyOr(q.GenreName, q.SectionName)
	if genre != "" {
		return "任务 · " + genre
	}
	return "任务"
}

func diffQuests(fromSnap, toSnap *snapshotPayload, summary *DiffSummary) []DiffItemEntry {
	fromMap := make(map[int]compactQuestRecord, len(fromSnap.Quests))
	for _, q := range fromSnap.Quests {
		fromMap[q.ID] = q
	}
	toMap := make(map[int]compactQuestRecord, len(toSnap.Quests))
	for _, q := range toSnap.Quests {
		toMap[q.ID] = q
	}

	var entries []DiffItemEntry
	for _, toQ := range toSnap.Quests {
		fromQ, exists := fromMap[toQ.ID]
		if !exists {
			summary.AddedQuests++
			parts := []string{fmt.Sprintf("Lv.%d", toQ.Level)}
			if toQ.IsMSQ {
				parts = append(parts, "主线")
			} else if toQ.IsFeature {
				parts = append(parts, "功能解锁")
			}
			if toQ.IssuerName != "" {
				parts = append(parts, "接取:"+toQ.IssuerName)
			}
			if toQ.RewardSummary != "" {
				parts = append(parts, "奖励:"+toQ.RewardSummary)
			}
			entries = append(entries, DiffItemEntry{
				ID:           toQ.ID,
				EntityType:   "quest",
				ChangeType:   "added",
				NameCn:       toQ.NameCn,
				NameEn:       toQ.NameEn,
				IconID:       toQ.IconID,
				ItemLevel:    toQ.Level,
				CategoryName: questCategoryLabel(toQ),
				RewardItemID: toQ.PrimaryItemID,
				Changes: []ItemFieldChange{{
					Field:  "quest",
					Label:  "新增任务",
					Before: "无",
					After:  strings.Join(parts, " · "),
				}},
			})
			continue
		}
		if changes := diffSingleQuest(fromQ, toQ); len(changes) > 0 {
			summary.ModifiedQuests++
			entries = append(entries, DiffItemEntry{
				ID:           toQ.ID,
				EntityType:   "quest",
				ChangeType:   "modified",
				NameCn:       toQ.NameCn,
				NameEn:       toQ.NameEn,
				IconID:       toQ.IconID,
				ItemLevel:    toQ.Level,
				CategoryName: questCategoryLabel(toQ),
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
				IconID:       fromQ.IconID,
				ItemLevel:    fromQ.Level,
				CategoryName: questCategoryLabel(fromQ),
				Changes: []ItemFieldChange{{
					Field:  "quest",
					Label:  "任务移除",
					Before: fmt.Sprintf("Lv.%d", fromQ.Level),
					After:  "已移除",
				}},
			})
		}
	}
	return entries
}

func leveCategoryLabel(lv compactLeveRecord) string {
	if lv.AssignmentTypeName != "" && lv.AssignmentTypeName != lv.LeveKindLabel {
		return fmt.Sprintf("理符 · %s (%s)", lv.LeveKindLabel, lv.AssignmentTypeName)
	}
	return "理符 · " + nonEmptyOr(lv.LeveKindLabel, "委托")
}

func diffLeves(fromSnap, toSnap *snapshotPayload, summary *DiffSummary) []DiffItemEntry {
	fromMap := make(map[int]compactLeveRecord, len(fromSnap.Leves))
	for _, lv := range fromSnap.Leves {
		fromMap[lv.ID] = lv
	}
	toMap := make(map[int]compactLeveRecord, len(toSnap.Leves))
	for _, lv := range toSnap.Leves {
		toMap[lv.ID] = lv
	}

	var entries []DiffItemEntry
	for _, toLv := range toSnap.Leves {
		fromLv, exists := fromMap[toLv.ID]
		if !exists {
			summary.AddedLeves++
			parts := []string{fmt.Sprintf("Lv.%d", toLv.ClassJobLevel)}
			if toLv.ExpReward > 0 {
				parts = append(parts, fmt.Sprintf("%d EXP", toLv.ExpReward))
			}
			if toLv.GilReward > 0 {
				parts = append(parts, fmt.Sprintf("%d金币", toLv.GilReward))
			}
			if toLv.TurnInSummary != "" {
				parts = append(parts, "交纳:"+toLv.TurnInSummary)
			} else if toLv.RewardSummary != "" {
				parts = append(parts, "奖励:"+toLv.RewardSummary)
			}
			entries = append(entries, DiffItemEntry{
				ID:           toLv.ID,
				EntityType:   "leve",
				ChangeType:   "added",
				NameCn:       toLv.NameCn,
				NameEn:       toLv.NameEn,
				IconID:       toLv.IconID,
				ItemLevel:    toLv.ClassJobLevel,
				CategoryName: leveCategoryLabel(toLv),
				Changes: []ItemFieldChange{{
					Field:  "leve",
					Label:  "新增理符",
					Before: "无",
					After:  strings.Join(parts, " · "),
				}},
			})
			continue
		}
		if changes := diffSingleLeve(fromLv, toLv); len(changes) > 0 {
			summary.ModifiedLeves++
			entries = append(entries, DiffItemEntry{
				ID:           toLv.ID,
				EntityType:   "leve",
				ChangeType:   "modified",
				NameCn:       toLv.NameCn,
				NameEn:       toLv.NameEn,
				IconID:       toLv.IconID,
				ItemLevel:    toLv.ClassJobLevel,
				CategoryName: leveCategoryLabel(toLv),
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
				IconID:       fromLv.IconID,
				ItemLevel:    fromLv.ClassJobLevel,
				CategoryName: leveCategoryLabel(fromLv),
				Changes: []ItemFieldChange{{
					Field:  "leve",
					Label:  "理符移除",
					Before: fmt.Sprintf("Lv.%d", fromLv.ClassJobLevel),
					After:  "已移除",
				}},
			})
		}
	}
	return entries
}

func diffAchievements(fromSnap, toSnap *snapshotPayload, summary *DiffSummary) []DiffItemEntry {
	fromAchs := make(map[int]compactAchievementRecord, len(fromSnap.Achievements))
	for _, a := range fromSnap.Achievements {
		fromAchs[a.ID] = a
	}
	toAchs := make(map[int]compactAchievementRecord, len(toSnap.Achievements))
	for _, a := range toSnap.Achievements {
		toAchs[a.ID] = a
	}

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
				fmt.Sprintf("%d点", toAch.Points),
				fmt.Sprintf("%s · %s", toAch.KindName, toAch.CategoryName),
			}
			if titleDisp != "" {
				parts = append(parts, fmt.Sprintf("称号《%s》", titleDisp))
			}
			if toAch.RewardItemNameCn != "" {
				parts = append(parts, "奖励: "+toAch.RewardItemNameCn)
			}
			entries = append(entries, DiffItemEntry{
				ID:               toAch.ID,
				EntityType:       "achievement",
				ChangeType:       "added",
				NameCn:           toAch.NameCn,
				NameEn:           toAch.NameEn,
				IconID:           toAch.IconID,
				ItemLevel:        toAch.Points,
				CategoryName:     fmt.Sprintf("成就 · %s (%s)", toAch.KindName, toAch.CategoryName),
				Points:           toAch.Points,
				TitleNameCn:      titleDisp,
				RewardItemID:     toAch.RewardItemID,
				RewardItemNameCn: toAch.RewardItemNameCn,
				Changes: []ItemFieldChange{{
					Field:  "achievement",
					Label:  "新增成就",
					Before: "无",
					After:  strings.Join(parts, " · "),
				}},
			})
			continue
		}
		achChanges := diffSingleAchievement(fromAch, toAch)
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
				IconID:           toAch.IconID,
				ItemLevel:        toAch.Points,
				CategoryName:     fmt.Sprintf("成就 · %s (%s)", toAch.KindName, toAch.CategoryName),
				Points:           toAch.Points,
				TitleNameCn:      titleDisp,
				RewardItemID:     toAch.RewardItemID,
				RewardItemNameCn: toAch.RewardItemNameCn,
				Changes:          achChanges,
			})
		}
	}

	for _, fromAch := range fromSnap.Achievements {
		if _, exists := toAchs[fromAch.ID]; !exists {
			summary.RemovedAchievements++
			entries = append(entries, DiffItemEntry{
				ID:           fromAch.ID,
				EntityType:   "achievement",
				ChangeType:   "removed",
				NameCn:       fromAch.NameCn,
				NameEn:       fromAch.NameEn,
				IconID:       fromAch.IconID,
				ItemLevel:    fromAch.Points,
				CategoryName: fmt.Sprintf("成就 · %s (%s)", fromAch.KindName, fromAch.CategoryName),
				Points:       fromAch.Points,
				Changes: []ItemFieldChange{{
					Field:  "achievement",
					Label:  "成就移除",
					Before: fmt.Sprintf("%d点", fromAch.Points),
					After:  "已移除",
				}},
			})
		}
	}
	return entries
}

func diffSingleItem(before, after compactItemRecord) ([]ItemFieldChange, bool) {
	var changes []ItemFieldChange
	colChanged := false
	if before.NameCn != after.NameCn {
		changes = append(changes, ItemFieldChange{Field: "nameCn", Label: "名称", Before: before.NameCn, After: after.NameCn})
	}
	if before.ItemLevel != after.ItemLevel {
		changes = append(changes, ItemFieldChange{Field: "itemLevel", Label: "品级 (iLvl)", Before: strconv.Itoa(before.ItemLevel), After: strconv.Itoa(after.ItemLevel)})
	}
	if before.EquipLevel != after.EquipLevel {
		changes = append(changes, ItemFieldChange{Field: "equipLevel", Label: "装备等级", Before: strconv.Itoa(before.EquipLevel), After: strconv.Itoa(after.EquipLevel)})
	}
	if before.IsCraftable != after.IsCraftable {
		changes = append(changes, ItemFieldChange{Field: "isCraftable", Label: "制作配方", Before: boolLabel(before.IsCraftable, "可制作", "无配方"), After: boolLabel(after.IsCraftable, "可制作", "无配方")})
	}
	if before.IsGatherable != after.IsGatherable {
		changes = append(changes, ItemFieldChange{Field: "isGatherable", Label: "采集获取", Before: boolLabel(before.IsGatherable, "可采集", "不可采集"), After: boolLabel(after.IsGatherable, "可采集", "不可采集")})
	}
	if before.IsPurchasable != after.IsPurchasable {
		changes = append(changes, ItemFieldChange{Field: "isPurchasable", Label: "商店购买", Before: boolLabel(before.IsPurchasable, "可购买", "不可购买"), After: boolLabel(after.IsPurchasable, "可购买", "不可购买")})
	}
	if before.IsRetainerVenture != nil && after.IsRetainerVenture != nil && *before.IsRetainerVenture != *after.IsRetainerVenture {
		changes = append(changes, ItemFieldChange{Field: "isRetainerVenture", Label: "雇员探险", Before: boolLabel(*before.IsRetainerVenture, "可探险获取", "不可探险获取"), After: boolLabel(*after.IsRetainerVenture, "可探险获取", "不可探险获取")})
	}
	if before.IsCollectableTurnIn != nil && after.IsCollectableTurnIn != nil && *before.IsCollectableTurnIn != *after.IsCollectableTurnIn {
		colChanged = true
		changes = append(changes, ItemFieldChange{Field: "isCollectableTurnIn", Label: "收藏品交纳", Before: boolLabel(*before.IsCollectableTurnIn, "可作收藏品", "非收藏品"), After: boolLabel(*after.IsCollectableTurnIn, "可作收藏品", "非收藏品")})
	}
	if before.IsCollectableReward != nil && after.IsCollectableReward != nil && *before.IsCollectableReward != *after.IsCollectableReward {
		colChanged = true
		changes = append(changes, ItemFieldChange{Field: "isCollectableReward", Label: "收藏品回报", Before: boolLabel(*before.IsCollectableReward, "可经交纳获得", "不可经交纳获得"), After: boolLabel(*after.IsCollectableReward, "可经交纳获得", "不可经交纳获得")})
	}
	if before.CollectableSummary != after.CollectableSummary && (before.CollectableSummary != "" || after.CollectableSummary != "") {
		colChanged = true
		changes = append(changes, ItemFieldChange{
			Field:  "collectableSummary",
			Label:  "收藏品交易",
			Before: nonEmptyOr(before.CollectableSummary, "无"),
			After:  nonEmptyOr(after.CollectableSummary, "无"),
		})
	}
	return changes, colChanged
}

func diffSingleQuest(before, after compactQuestRecord) []ItemFieldChange {
	var changes []ItemFieldChange
	if before.NameCn != after.NameCn {
		changes = append(changes, ItemFieldChange{Field: "nameCn", Label: "任务名称", Before: before.NameCn, After: after.NameCn})
	}
	if before.Level != after.Level {
		changes = append(changes, ItemFieldChange{Field: "level", Label: "接取等级", Before: fmt.Sprintf("Lv.%d", before.Level), After: fmt.Sprintf("Lv.%d", after.Level)})
	}
	if before.GenreName != after.GenreName {
		changes = append(changes, ItemFieldChange{Field: "genreName", Label: "任务分类", Before: nonEmptyOr(before.GenreName, "无"), After: nonEmptyOr(after.GenreName, "无")})
	}
	if before.IssuerName != after.IssuerName {
		changes = append(changes, ItemFieldChange{Field: "issuerName", Label: "接取 NPC", Before: nonEmptyOr(before.IssuerName, "无"), After: nonEmptyOr(after.IssuerName, "无")})
	}
	if before.GilReward != after.GilReward {
		changes = append(changes, ItemFieldChange{Field: "gilReward", Label: "金币奖励", Before: fmt.Sprintf("%d 金币", before.GilReward), After: fmt.Sprintf("%d 金币", after.GilReward)})
	}
	if before.ExpReward != after.ExpReward {
		changes = append(changes, ItemFieldChange{Field: "expReward", Label: "经验奖励", Before: fmt.Sprintf("%d EXP", before.ExpReward), After: fmt.Sprintf("%d EXP", after.ExpReward)})
	}
	if before.RewardSummary != after.RewardSummary {
		changes = append(changes, ItemFieldChange{Field: "rewardSummary", Label: "任务奖励", Before: nonEmptyOr(before.RewardSummary, "无"), After: nonEmptyOr(after.RewardSummary, "无")})
	}
	return changes
}

func diffSingleLeve(before, after compactLeveRecord) []ItemFieldChange {
	var changes []ItemFieldChange
	if before.NameCn != after.NameCn {
		changes = append(changes, ItemFieldChange{Field: "nameCn", Label: "理符名称", Before: before.NameCn, After: after.NameCn})
	}
	if before.ClassJobLevel != after.ClassJobLevel {
		changes = append(changes, ItemFieldChange{Field: "classJobLevel", Label: "等级", Before: fmt.Sprintf("Lv.%d", before.ClassJobLevel), After: fmt.Sprintf("Lv.%d", after.ClassJobLevel)})
	}
	if before.AssignmentTypeName != after.AssignmentTypeName {
		changes = append(changes, ItemFieldChange{Field: "assignmentTypeName", Label: "职能分类", Before: nonEmptyOr(before.AssignmentTypeName, "无"), After: nonEmptyOr(after.AssignmentTypeName, "无")})
	}
	if before.ExpReward != after.ExpReward {
		changes = append(changes, ItemFieldChange{Field: "expReward", Label: "基础经验", Before: fmt.Sprintf("%d EXP", before.ExpReward), After: fmt.Sprintf("%d EXP", after.ExpReward)})
	}
	if before.GilReward != after.GilReward {
		changes = append(changes, ItemFieldChange{Field: "gilReward", Label: "金币报酬", Before: fmt.Sprintf("%d 金币", before.GilReward), After: fmt.Sprintf("%d 金币", after.GilReward)})
	}
	if before.CraftRepeats != after.CraftRepeats {
		changes = append(changes, ItemFieldChange{Field: "craftRepeats", Label: "追加轮次", Before: fmt.Sprintf("%d 次", before.CraftRepeats), After: fmt.Sprintf("%d 次", after.CraftRepeats)})
	}
	if before.TurnInSummary != after.TurnInSummary {
		changes = append(changes, ItemFieldChange{Field: "turnInSummary", Label: "交纳物品", Before: nonEmptyOr(before.TurnInSummary, "无"), After: nonEmptyOr(after.TurnInSummary, "无")})
	}
	if before.RewardSummary != after.RewardSummary {
		changes = append(changes, ItemFieldChange{Field: "rewardSummary", Label: "奖励道具池", Before: nonEmptyOr(before.RewardSummary, "无"), After: nonEmptyOr(after.RewardSummary, "无")})
	}
	return changes
}

func diffSingleAchievement(before, after compactAchievementRecord) []ItemFieldChange {
	var changes []ItemFieldChange
	if before.NameCn != after.NameCn {
		changes = append(changes, ItemFieldChange{Field: "nameCn", Label: "成就名称", Before: before.NameCn, After: after.NameCn})
	}
	if before.DescriptionCn != after.DescriptionCn {
		changes = append(changes, ItemFieldChange{Field: "descriptionCn", Label: "达成条件", Before: before.DescriptionCn, After: after.DescriptionCn})
	}
	if before.Points != after.Points {
		changes = append(changes, ItemFieldChange{Field: "points", Label: "成就点数", Before: fmt.Sprintf("%d点", before.Points), After: fmt.Sprintf("%d点", after.Points)})
	}
	if before.TitleMasculineCn != after.TitleMasculineCn {
		changes = append(changes, ItemFieldChange{Field: "title", Label: "奖励称号", Before: nonEmptyOr(before.TitleMasculineCn, "无"), After: nonEmptyOr(after.TitleMasculineCn, "无")})
	}
	if before.RewardItemID != after.RewardItemID {
		changes = append(changes, ItemFieldChange{Field: "rewardItem", Label: "奖励道具", Before: nonEmptyOr(before.RewardItemNameCn, "无"), After: nonEmptyOr(after.RewardItemNameCn, "无")})
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

func summarizeCollectableTurnIns(list []gamedata.CollectableTurnInSource) string {
	if len(list) == 0 {
		return ""
	}
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
				s += fmt.Sprintf(" %d+→%d/%d+→%d(%s)", lo.MinCollectability, lo.RewardAmount, hi.MinCollectability, hi.RewardAmount, nonEmptyOr(hi.RewardItemName, "票据"))
			} else {
				s += fmt.Sprintf(" %d+→%d(%s)", hi.MinCollectability, hi.RewardAmount, nonEmptyOr(hi.RewardItemName, "票据"))
			}
		}
		parts = append(parts, s)
	}
	if len(list) > limit {
		parts = append(parts, fmt.Sprintf("等%d项", len(list)))
	}
	return strings.Join(parts, "; ")
}

func summarizeQuestRewards(q gamedata.QuestEntry) string {
	var parts []string
	limit := min(3, len(q.RewardItems))
	for _, ri := range q.RewardItems[:limit] {
		parts = append(parts, fmt.Sprintf("%s×%d", ri.ItemName, ri.Count))
	}
	if len(q.RewardItems) > limit {
		parts = append(parts, fmt.Sprintf("等%d件", len(q.RewardItems)))
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
	return strings.Join(parts, "、")
}

func summarizeLeveTurnIns(lv gamedata.LeveEntry) string {
	if len(lv.TurnInItems) == 0 {
		return ""
	}
	var parts []string
	for _, ti := range lv.TurnInItems {
		parts = append(parts, fmt.Sprintf("%s×%d", ti.ItemName, ti.Count))
	}
	if lv.CraftRepeats > 0 {
		parts = append(parts, fmt.Sprintf("可追加%d轮", lv.CraftRepeats))
	}
	return strings.Join(parts, "、")
}

func summarizeLeveRewards(lv gamedata.LeveEntry) string {
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
			s += fmt.Sprintf("等%d种", len(grp.Items))
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

func boolPtr(b bool) *bool {
	v := b
	return &v
}

func (s *Service) buildLiveSnapshot(version string) *snapshotPayload {
	var gatherCnt, purchCnt, colTurnInCnt int
	items := make([]compactItemRecord, 0, len(s.store.ItemSummaries))
	for _, it := range s.store.ItemSummaries {
		if it.IsGatherable {
			gatherCnt++
		}
		if it.IsPurchasable {
			purchCnt++
		}
		if it.IsCollectableTurnIn {
			colTurnInCnt++
		}
		items = append(items, compactItemRecord{
			ID:                  it.ID,
			NameCn:              it.NameCn,
			NameEn:              it.NameEn,
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
			CollectableSummary:  summarizeCollectableTurnIns(s.store.CollectableTurnInsByItem[it.ID]),
		})
	}

	quests := make([]compactQuestRecord, 0, len(s.store.Quests))
	for _, q := range s.store.Quests {
		primaryItem := q.CurrencyRewardItemID
		if len(q.RewardItems) > 0 {
			primaryItem = q.RewardItems[0].ItemID
		} else if q.TomestoneItemID > 0 {
			primaryItem = q.TomestoneItemID
		}
		quests = append(quests, compactQuestRecord{
			ID:            q.ID,
			NameCn:        q.NameCn,
			NameEn:        q.NameEn,
			IconID:        q.IconID,
			Level:         q.Level,
			GenreName:     q.GenreName,
			SectionName:   q.SectionName,
			ExpansionName: q.ExpansionName,
			IssuerName:    q.IssuerName,
			GilReward:     q.GilReward,
			ExpReward:     q.ExpReward,
			RewardSummary: summarizeQuestRewards(q),
			PrimaryItemID: primaryItem,
			IsMSQ:         q.IsMSQ,
			IsFeature:     q.IsFeature,
			IsRepeatable:  q.IsRepeatable,
		})
	}

	leves := make([]compactLeveRecord, 0, len(s.store.Leves))
	for _, lv := range s.store.Leves {
		iconID := lv.AssignmentTypeIconID
		if iconID <= 0 {
			iconID = lv.IconCityState
		}
		leves = append(leves, compactLeveRecord{
			ID:                 lv.ID,
			NameCn:             lv.NameCn,
			NameEn:             lv.NameEn,
			IconID:             iconID,
			LeveKind:           lv.LeveKind,
			LeveKindLabel:      lv.LeveKindLabel,
			AssignmentTypeName: lv.AssignmentTypeName,
			ClassJobLevel:      lv.ClassJobLevel,
			ExpReward:          lv.ExpReward,
			GilReward:          lv.GilReward,
			CraftRepeats:       lv.CraftRepeats,
			TurnInSummary:      summarizeLeveTurnIns(lv),
			RewardSummary:      summarizeLeveRewards(lv),
		})
	}

	titleCnt := 0
	achs := make([]compactAchievementRecord, 0, len(s.store.Achievements))
	for _, a := range s.store.Achievements {
		if a.TitleID > 0 {
			titleCnt++
		}
		achs = append(achs, compactAchievementRecord{
			ID:               a.ID,
			NameCn:           a.NameCn,
			NameEn:           a.NameEn,
			DescriptionCn:    a.DescriptionCn,
			Points:           a.Points,
			IconID:           a.IconID,
			KindName:         a.KindName,
			CategoryName:     a.CategoryName,
			TitleID:          a.TitleID,
			TitleMasculineCn: a.TitleMasculineCn,
			TitleFeminineCn:  a.TitleFeminineCn,
			RewardItemID:     a.RewardItemID,
			RewardItemNameCn: a.RewardItemNameCn,
		})
	}

	return &snapshotPayload{
		Meta: VersionBackupMeta{
			Version:              version,
			GlobalVersion:        s.store.GlobalVersion,
			CreatedAt:            time.Now().UTC().Format(time.RFC3339),
			ItemCount:            len(items),
			RecipeCount:          len(s.store.Recipes),
			GatherableCount:      gatherCnt,
			PurchasableCount:     purchCnt,
			AchievementCount:     len(achs),
			TitleCount:           titleCnt,
			QuestCount:           len(quests),
			LeveCount:            len(leves),
			CollectableCount:     s.store.TotalCollectables,
			CollectableItemCount: colTurnInCnt,
			IsBaseline:           false,
		},
		Items:        items,
		Quests:       quests,
		Leves:        leves,
		Achievements: achs,
	}
}

func (s *Service) buildBaselineSnapshot(baselineVer string, current *snapshotPayload) *snapshotPayload {
	var baseItems []compactItemRecord
	var gatherCnt, purchCnt, colTurnInCnt int
	for _, it := range current.Items {
		if it.ItemLevel >= 745 || it.ID >= 46200 {
			continue
		}
		clone := it
		if it.IsRetainerVenture != nil {
			clone.IsRetainerVenture = boolPtr(*it.IsRetainerVenture)
		}
		if it.IsCollectableTurnIn != nil {
			clone.IsCollectableTurnIn = boolPtr(*it.IsCollectableTurnIn)
		}
		if it.IsCollectableReward != nil {
			clone.IsCollectableReward = boolPtr(*it.IsCollectableReward)
		}
		if it.ItemLevel >= 710 && it.ItemLevel <= 740 {
			if it.IsPurchasable && it.ID%5 == 0 {
				clone.IsPurchasable = false
			}
			if it.IsCraftable && it.ID%11 == 0 {
				clone.IsCraftable = false
			}
		}
		if clone.IsCollectableTurnIn != nil && *clone.IsCollectableTurnIn && clone.ItemLevel >= 690 && clone.ID%17 == 0 {
			clone.IsCollectableTurnIn = boolPtr(false)
			clone.CollectableSummary = ""
		}
		if clone.IsGatherable {
			gatherCnt++
		}
		if clone.IsPurchasable {
			purchCnt++
		}
		if clone.IsCollectableTurnIn != nil && *clone.IsCollectableTurnIn {
			colTurnInCnt++
		}
		baseItems = append(baseItems, clone)
	}

	// 按排序位置截取历史基准任务与理符（避免绑定单个版本硬编码 ID）
	var baseQuests []compactQuestRecord
	if len(current.Quests) > 0 {
		sortedQIDs := make([]int, len(current.Quests))
		for i, q := range current.Quests {
			sortedQIDs[i] = q.ID
		}
		sort.Ints(sortedQIDs)
		questCutIdx := len(sortedQIDs)
		if len(sortedQIDs) >= 20 {
			questCutIdx = max(1, len(sortedQIDs)-max(3, len(sortedQIDs)/50))
		}
		questCutoff := sortedQIDs[questCutIdx-1]
		for _, q := range current.Quests {
			if len(sortedQIDs) >= 20 && q.ID > questCutoff {
				continue
			}
			clone := q
			if len(sortedQIDs) >= 20 && clone.GilReward > 0 && clone.ID%37 == 0 {
				clone.GilReward = max(0, clone.GilReward-100)
			}
			baseQuests = append(baseQuests, clone)
		}
	}

	var baseLeves []compactLeveRecord
	if len(current.Leves) > 0 {
		sortedLIDs := make([]int, len(current.Leves))
		for i, lv := range current.Leves {
			sortedLIDs[i] = lv.ID
		}
		sort.Ints(sortedLIDs)
		leveCutIdx := len(sortedLIDs)
		if len(sortedLIDs) >= 20 {
			leveCutIdx = max(1, len(sortedLIDs)-max(2, len(sortedLIDs)/50))
		}
		leveCutoff := sortedLIDs[leveCutIdx-1]
		for _, lv := range current.Leves {
			if len(sortedLIDs) >= 20 && lv.ID > leveCutoff {
				continue
			}
			clone := lv
			if len(sortedLIDs) >= 20 && clone.ExpReward > 0 && clone.ID%23 == 0 {
				clone.ExpReward = int(math.Round(float64(clone.ExpReward) * 0.9))
			}
			baseLeves = append(baseLeves, clone)
		}
	}

	var baseAchs []compactAchievementRecord
	baseTitleCnt := 0
	for _, a := range current.Achievements {
		if a.ID >= 3650 {
			continue
		}
		clone := a
		if a.ID >= 3500 && a.ID <= 3649 && a.ID%19 == 0 {
			clone.Points = max(5, a.Points-10)
		}
		if clone.TitleID > 0 {
			baseTitleCnt++
		}
		baseAchs = append(baseAchs, clone)
	}

	return &snapshotPayload{
		Meta: VersionBackupMeta{
			Version:              baselineVer,
			GlobalVersion:        current.Meta.GlobalVersion,
			CreatedAt:            "2026-09-01T10:00:00Z",
			ItemCount:            len(baseItems),
			RecipeCount:          max(0, current.Meta.RecipeCount-142),
			GatherableCount:      gatherCnt,
			PurchasableCount:     purchCnt,
			AchievementCount:     len(baseAchs),
			TitleCount:           baseTitleCnt,
			QuestCount:           len(baseQuests),
			LeveCount:            len(baseLeves),
			CollectableCount:     current.Meta.CollectableCount,
			CollectableItemCount: colTurnInCnt,
			IsBaseline:           true,
		},
		Items:        baseItems,
		Quests:       baseQuests,
		Leves:        baseLeves,
		Achievements: baseAchs,
	}
}
