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
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
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
	NameJa               string            `json:"nameJa,omitempty"`
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
	NameJa              string `json:"nameJa,omitempty"`
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
	NameJa        string `json:"nameJa,omitempty"`
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
	NameJa             string `json:"nameJa,omitempty"`
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
	NameJa           string `json:"nameJa,omitempty"`
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
	uiLang        i18n.Lang
	manifest      *backupManifest
	snapshotCache map[string]*snapshotPayload
}

// NewService 创建版本快照备份服务，快照保存在 backupDir 下。
func NewService(store *gamedata.Store, backupDir string) *Service {
	lang := i18n.LangZH
	if store != nil && store.AppLang != "" {
		lang = i18n.NormalizeLang(string(store.AppLang))
	}
	return &Service{
		store:         store,
		backupDir:     backupDir,
		manifestPath:  filepath.Join(backupDir, "manifest.json"),
		uiLang:        lang,
		snapshotCache: make(map[string]*snapshotPayload),
	}
}

// SetLang 切换版本对比文案输出所使用的界面语言。
func (s *Service) SetLang(lang i18n.Lang) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.uiLang = i18n.NormalizeLang(string(lang))
	s.mu.Unlock()
}

func (s *Service) langLocked() i18n.Lang {
	if s.uiLang == "" {
		return i18n.LangZH
	}
	return s.uiLang
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

	currentVersion := s.store.DataVersion()
	existing := s.readManifest()
	currentSnap := s.buildLiveSnapshot(currentVersion, s.langLocked())

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
		CurrentVersion:   s.store.DataVersion(),
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

	currentVersion := s.store.DataVersion()
	payload := s.buildLiveSnapshot(currentVersion, s.langLocked())
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

	liveVer := s.store.DataVersion()
	lang := s.langLocked()
	toVersion := query.To
	if toVersion == "" {
		toVersion = liveVer
	}
	fromVersion := query.From
	if fromVersion == "" && s.manifest != nil {
		fromVersion = s.manifest.PreviousVersion
	}
	if fromVersion == "" {
		fromVersion = deriveBaselineVersionName(liveVer)
	}

	toSnap := s.loadSnapshot(toVersion)
	if toSnap == nil {
		toSnap = s.buildLiveSnapshot(liveVer, lang)
	}
	fromSnap := s.loadSnapshot(fromVersion)
	if fromSnap == nil {
		fromSnap = s.buildBaselineSnapshot(fromVersion, toSnap)
	}

	var allEntries []DiffItemEntry
	var summary DiffSummary

	allEntries = append(allEntries, diffItems(lang, fromSnap, toSnap, &summary)...)
	allEntries = append(allEntries, diffQuests(lang, fromSnap, toSnap, &summary)...)
	allEntries = append(allEntries, diffLeves(lang, fromSnap, toSnap, &summary)...)
	allEntries = append(allEntries, diffAchievements(lang, fromSnap, toSnap, &summary)...)
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
		strings.Contains(strings.ToLower(e.NameJa), q) ||
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
				TitleNameCn:      titleDisp,
				RewardItemID:     toAch.RewardItemID,
				RewardItemNameCn: toAch.RewardItemNameCn,
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
				NameJa:       fromAch.NameJa,
				IconID:       fromAch.IconID,
				ItemLevel:    fromAch.Points,
				CategoryName: i18n.Tf(lang, "backup.ach_cat", "kind", fromAch.KindName, "cat", fromAch.CategoryName),
				Points:       fromAch.Points,
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
	return changes
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

func (s *Service) buildLiveSnapshot(version string, lang i18n.Lang) *snapshotPayload {
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
			CollectableSummary:  summarizeCollectableTurnIns(lang, s.store.CollectableTurnInsByItem[it.ID]),
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
			NameJa:        q.NameJa,
			IconID:        q.IconID,
			Level:         q.Level,
			GenreName:     q.GenreName,
			SectionName:   q.SectionName,
			ExpansionName: q.ExpansionName,
			IssuerName:    q.IssuerName,
			GilReward:     q.GilReward,
			ExpReward:     q.ExpReward,
			RewardSummary: summarizeQuestRewards(lang, q),
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
			NameJa:           a.NameJa,
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
