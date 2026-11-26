package uiapp

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/changelog"
	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/exporter"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/InfSein/xiv-data-viewer/internal/updater"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type appMode uint8

const (
	modeSetup appMode = iota
	modeLoading
	modeReady
)

type setupServerMode uint8

const (
	setupModeCN setupServerMode = iota
	setupModeGlobal
	setupModeBoth
)

type loadStage uint8

const (
	loadStageSheets loadStage = iota
	loadStageIndices
)

type activeTab uint8

const (
	tabWorkbench activeTab = iota
	tabQuests
	tabLeves
	tabAchievements
	tabMusic
	tabIcons
	tabRecent
	tabFavorites
	tabDiff
	tabTextSearch
	tabChangelog
	tabSettings
)

type recordKey struct {
	kind config.RecordKind
	id   int
}

// MapMarkerData 描述地图预览弹窗所需的区域、坐标与贴图路径。
type MapMarkerData struct {
	Title      string
	Subtitle   string
	RegionName string
	ZoneName   string
	PlaceName  string
	MapPath    string
	CoordX     float64
	CoordY     float64
	NormX      float64
	NormY      float64
}

type langSessionCache struct {
	service         *gamedata.Service
	backupSvc       *backup.Service
	exporterSvc     *exporter.Service
	iconExporterSvc *exporter.IconService
	diffExporterSvc *exporter.DiffService
	status          gamedata.SystemStatus
	versionStatus   backup.VersionStatusResponse
	exportTotalShts int
}

// App 维护 XIV Data Viewer 纯 Go 原生客户端的全部运行态与界面状态。
type App struct {
	win        *mygo.Window
	vibrancy   bool
	appVersion string
	dataDir    string
	schemasFS  fs.FS

	mode appMode
	tab  activeTab

	// 窗口外壳与面板布局状态（Godwit 风格）
	sidebarHidden        bool
	sidebarW             float32
	listW                float32
	tabHistory           []activeTab
	tabAhead             []activeTab
	focusSearch          bool
	focusPageSearch      bool
	globalSearchQ        string
	globalSearchOpen     bool
	globalSearchIdx      int
	globalSearchScrollTo bool
	globalSearchScroll   ui.ScrollState
	globalSearchRes      gamedata.GlobalSearchResult
	statusPopoverOpen    bool
	langPopoverOpen      bool

	// 多语言会话缓存与共享只读客户端句柄（消除切语言重加载与图标闪烁）
	langSwitching      bool
	langCache          map[i18n.Lang]*langSessionCache
	langCachePathSig   string
	sharedCNReader     *sqpack.GameReader
	sharedGlobalReader *sqpack.GameReader

	// 串行异步配置落盘队列（避免点击切语言/主题时阻塞主 UI 帧）
	saveMu      sync.Mutex
	saveNextCfg config.AppConfig
	savePending bool
	saveRunning bool
	saveDone    chan struct{}

	// 最近浏览（上限 500 条）与收藏名单及异步落盘队列
	records        config.UserRecords
	favSet         map[recordKey]bool
	saveRecMu      sync.Mutex
	saveRecNext    config.UserRecords
	saveRecPending bool
	saveRecRunning bool
	saveRecDone    chan struct{}

	// 配置与引导/设置表单草稿
	cfg              config.AppConfig
	lang             i18n.Lang
	setupMode        setupServerMode
	setupModeTouched bool
	loadReturnTab    activeTab
	draftCNPath      string
	draftGlobalPath  string
	draftError       string
	draftNotice      string
	loadError        string
	pathDiagCache    map[string]cachedPathDiag

	// 异步加载阶段与真实进度
	loadSeq          uint64
	loadStartedAt    time.Time
	loadingCNVer     string
	loadingGlobalVer string
	loadStage        loadStage
	loadSheetsDone   int
	loadSheetsTotal  int
	loadIndexStep    int
	loadIndexTotal   int
	loadingStatusTxt string

	// 核心后端服务
	service         *gamedata.Service
	backupSvc       *backup.Service
	exporterSvc     *exporter.Service
	iconExporterSvc *exporter.IconService
	diffExporterSvc *exporter.DiffService
	bitmaps         *BitmapCache

	// 系统与版本状态
	status              *gamedata.SystemStatus
	versionStatus       *backup.VersionStatusResponse
	dismissUpdateBanner bool

	// 道具工作台状态
	itemQuery          gamedata.ItemSearchQuery
	itemMinIlvlStr     string
	itemMaxIlvlStr     string
	itemPageStr        string
	itemRes            gamedata.ItemSearchResponse
	itemAdvFilter      bool // 是否展开高级筛选抽屉（分类 / 品级 / 仅可交易）
	selectedItemID     int
	selectedDetail     *gamedata.ItemDetail
	showHQ             bool
	activeRecipeIdx    int
	copiedItemID       int
	copiedUntil        time.Time
	detailScroll       ui.ScrollState
	itemListScroll     ui.ScrollState
	scrollItemListToID int

	// 任务视图状态
	questQuery             gamedata.QuestSearchQuery
	questPageStr           string
	questRes               gamedata.QuestSearchResponse
	questListScroll        ui.ScrollState
	questDetailScroll      ui.ScrollState
	scrollQuestListToID    int
	selectedQuestID        int
	selectedQuest          *gamedata.QuestEntry
	selectedQuestText      *gamedata.QuestTextData
	questTextLoading       bool
	questTextSeq           uint64
	questSummaryOpen       bool
	questDialogueOpen      bool
	selectedQuestCutscenes []gamedata.QuestCutsceneEntry
	questCutscenesLoading  bool
	questCutscenesSeq      uint64

	// 理符视图状态
	leveQuery          gamedata.LeveSearchQuery
	levePageStr        string
	leveRes            gamedata.LeveSearchResponse
	leveListScroll     ui.ScrollState
	leveDetailScroll   ui.ScrollState
	scrollLeveListToID int
	selectedLeveID     int
	selectedLeve       *gamedata.LeveEntry

	// 成就与称号视图状态
	achQuery      gamedata.AchievementSearchQuery
	achPageStr    string
	achRes        gamedata.AchievementSearchResponse
	achScroll     ui.ScrollState
	scrollAchToID int

	// 音乐（游戏 BGM 与管弦乐琴）视图状态
	musicQuery         gamedata.MusicSearchQuery
	musicPageStr       string
	musicRes           gamedata.MusicSearchResponse
	musicListScroll    ui.ScrollState
	musicDetailScroll  ui.ScrollState
	scrollMusicListKey string
	selectedMusicKey   string
	selectedMusicKind  string
	selectedMusicID    int
	musicDetail        *gamedata.MusicEntry
	musicAudioSlot     musicAudioSlot
	musicVolume        float64
	musicPlayer        *musicPlayer

	// 「图标」视图与导出状态
	iconGroupListW         float32
	iconGroupFilterQ       string
	iconQuery              gamedata.IconSearchQuery
	iconRes                gamedata.IconSearchResponse
	iconGroupScroll        ui.ScrollState
	iconGridState          ui.GridState
	iconGridSelIdx         int
	iconInspectorScroll    ui.ScrollState
	selectedIconID         int // < 0 表示未选中（注意 #0 为有效图标编号）
	selectedIconDetail     *gamedata.IconEntry
	iconInspectorVariant   string
	iconInspectorHR1       bool
	iconPreviewCache       map[string]*cachedIconPreview
	iconTileCache          *iconTileBitmapCache
	iconSingleExportMsg    string
	iconSingleExportIsErr  bool
	iconExportOpen         bool
	iconExportDirDraft     string
	iconExportIncludeStd   bool
	iconExportIncludeHR1   bool
	iconExportVariants     map[string]bool
	iconExportScopeGroup   bool
	iconExportProgress     *exporter.IconExportProgress
	iconExportError        string
	iconExportTaskCount    int
	iconExportTaskCountKey string
	iconLightboxOpen       bool
	iconLightboxID         int
	iconLightboxVariant    string
	iconLightboxHR1        bool
	iconLightboxTitle      string
	iconLightboxSubtitle   string
	iconLightboxBlobKey    string
	iconLightboxThumbPNG   []byte

	// 「最近」视图状态
	recentFilter                string // all|item|quest|leve|music
	recentQueryQ                string
	recentPage                  int
	recentPageStr               string
	recentListScroll            ui.ScrollState
	recentSelectedKind          config.RecordKind
	recentSelectedID            int
	recentItemDetail            *gamedata.ItemDetail
	recentItemRecipeIdx         int
	recentItemScroll            ui.ScrollState
	recentQuest                 *gamedata.QuestEntry
	recentQuestText             *gamedata.QuestTextData
	recentQuestTextLoading      bool
	recentQuestTextSeq          uint64
	recentQuestSummaryOpen      bool
	recentQuestDialogueOpen     bool
	recentQuestCutscenes        []gamedata.QuestCutsceneEntry
	recentQuestCutscenesLoading bool
	recentQuestCutscenesSeq     uint64
	recentQuestScroll           ui.ScrollState
	recentLeve                  *gamedata.LeveEntry
	recentLeveScroll            ui.ScrollState
	recentMusicDetail           *gamedata.MusicEntry
	recentMusicScroll           ui.ScrollState
	recentMusicAudioSlot        musicAudioSlot

	// 「收藏」视图状态
	favTab                   string // item|quest|leve|achievement|music
	favItemQ                 string
	favItemSort              string // added|id_asc|id_desc|ilvl_desc
	favItemPage              int
	favItemPageStr           string
	favItemScroll            ui.ScrollState
	favSelectedItemID        int
	favItemDetail            *gamedata.ItemDetail
	favItemRecipeIdx         int
	favItemDetailScroll      ui.ScrollState
	favQuestQ                string
	favQuestFilter           string // all|msq|feature|repeatable|with_item
	favQuestPage             int
	favQuestPageStr          string
	favQuestScroll           ui.ScrollState
	favSelectedQuestID       int
	favQuest                 *gamedata.QuestEntry
	favQuestText             *gamedata.QuestTextData
	favQuestTextLoading      bool
	favQuestTextSeq          uint64
	favQuestSummaryOpen      bool
	favQuestDialogueOpen     bool
	favQuestCutscenes        []gamedata.QuestCutsceneEntry
	favQuestCutscenesLoading bool
	favQuestCutscenesSeq     uint64
	favQuestDetailScroll     ui.ScrollState
	favLeveQ                 string
	favLeveKind              string // all|craft|gathering|battle|company
	favLevePage              int
	favLevePageStr           string
	favLeveScroll            ui.ScrollState
	favSelectedLeveID        int
	favLeve                  *gamedata.LeveEntry
	favLeveDetailScroll      ui.ScrollState
	favAchQ                  string
	favAchFilter             string // all|with_title|with_item
	favAchPage               int
	favAchPageStr            string
	favAchScroll             ui.ScrollState
	favMusicQ                string
	favMusicKind             string // all|bgm|orchestrion
	favMusicPage             int
	favMusicPageStr          string
	favMusicScroll           ui.ScrollState
	favSelectedMusicKey      string
	favMusicDetail           *gamedata.MusicEntry
	favMusicDetailScroll     ui.ScrollState
	favMusicAudioSlot        musicAudioSlot

	// 版本 Diff 与 GitHub 快照管理状态（统一按版本号下载，严格同服对比）
	diffRegion               string // "cn"|"global"
	diffFromVer              string
	diffToVer                string
	diffFilter               string
	diffEntity               string
	diffSearch               string
	diffPage                 int
	diffPageStr              string
	diffRes                  *backup.VersionDiffResponse
	diffBusy                 bool
	diffSeq                  uint64
	diffNotice               string
	diffError                string
	diffScroll               ui.ScrollState
	diffIconCache            map[string]*ui.Bitmap
	diffTextExpanded         map[string]bool
	diffTextCopiedKey        string
	diffTextCopiedEnd        time.Time
	snapMgrOpen              bool
	snapMgrScroll            ui.ScrollState
	snapCatalogBusy          bool
	snapCatalogTried         bool
	snapDownID               string
	snapDownName             string
	snapDownBytes            int64
	snapDownTotal            int64
	snapDownCancel           context.CancelFunc
	snapMgrNotice            string
	snapMgrError             string
	diffExportOpen           bool
	diffExportKind           exporter.DiffExportKind
	diffExportPreferHR1      bool
	diffExportIconNaming     exporter.DiffIconNaming
	diffExportIncludeOldIcon bool
	diffExportAudioFormat    exporter.DiffAudioFormat
	diffExportDirDraft       string
	diffExportProgress       *exporter.DiffExportProgress
	diffExportError          string

	// 全量文本检索视图状态
	textSearchLang      string
	textSearchQ         string
	textSearchScope     gamedata.TextSearchScope
	textSearchSheet     string
	textSearchPage      int
	textSearchPageSize  int
	textSearchPageStr   string
	textSearchRes       gamedata.TextSearchResponse
	textSearchScroll    ui.ScrollState
	textSearchIdxBusy   bool
	textSearchIdxErr    string
	textSearchIdxProg   gamedata.TextIndexProgress
	textSearchIdxSeq    uint64
	textSearchIdxCancel context.CancelFunc
	textSearchRunSeq    uint64
	textSearchRunCancel context.CancelFunc
	textSearchBusy      bool
	textSearchExpanded  map[uint32]bool
	textSearchCopiedDoc uint32
	textSearchCopiedEnd time.Time
	textIndexCache      map[string]*gamedata.TextSearchIndex

	// 更新日志视图状态
	changelogFS        fs.FS
	changelogDocs      map[i18n.Lang]changelog.Document
	changelogErr       string
	changelogCatFilter string
	changelogVerFilter string
	changelogSearch    string
	changelogCollapsed map[string]bool
	changelogScroll    ui.ScrollState

	// 地图预览弹窗状态
	mapOpen   bool
	mapMarker *MapMarkerData

	// CSV 导出弹窗状态
	exportOpen      bool
	exportLangs     map[string]bool
	exportDirDraft  string
	exportProgress  *exporter.ExportProgress
	exportError     string
	exportTotalShts int

	// 应用内置更新状态（GitHub Release）
	updaterClient       *updater.Client
	startupUpdateOnce   sync.Once
	updateCancel        context.CancelFunc
	updateModalOpen     bool
	updateState         appUpdateState
	updateSilentRunning bool
	updateManualActive  bool
	updateSeq           uint64
	updateRelease       *updater.ReleaseInfo
	updateReadyVer      string
	updatePhase         updater.InstallPhase
	updateDownBytes     int64
	updateDownTotal     int64
	updateErrorMsg      string
	relaunchRequested   bool
}

type appUpdateState uint8

const (
	updateIdle appUpdateState = iota
	updateChecking
	updateAvailable
	updateDownloading
	updateReady
	updateUpToDate
	updateError
)

// NewApp 创建客户端应用实例并读取本地持久化配置。
func NewApp(appVersion, dataDir string, schemasFS fs.FS) *App {
	ensureDefaultFonts()
	cfg, _ := config.LoadAppConfig(dataDir)
	records, _ := config.LoadUserRecords(dataDir)
	initLang := i18n.LangZH
	if cfg.Language != "" {
		initLang = i18n.NormalizeLang(cfg.Language)
	} else {
		initLang = i18n.DetectSystemLanguage()
	}
	a := &App{
		appVersion:      appVersion,
		dataDir:         dataDir,
		schemasFS:       schemasFS,
		sidebarW:        sidebarDefWidth,
		listW:           itemListDefWidth,
		cfg:             cfg,
		records:         records,
		lang:            initLang,
		setupMode:       inferSetupMode(cfg.CNPath, cfg.GlobalPath, initLang),
		draftCNPath:     cfg.CNPath,
		draftGlobalPath: cfg.GlobalPath,
		pathDiagCache:   make(map[string]cachedPathDiag),
		bitmaps:         NewBitmapCache(600),
		showHQ:          true,
		itemQuery: gamedata.ItemSearchQuery{
			Filter:   "all",
			Sort:     "id_asc",
			Page:     1,
			PageSize: 30,
		},
		itemPageStr: "1",
		questQuery: gamedata.QuestSearchQuery{
			SectionID:   -1,
			ExpansionID: -1,
			Filter:      "all",
			Page:        1,
			PageSize:    30,
		},
		questPageStr: "1",
		leveQuery: gamedata.LeveSearchQuery{
			Kind:     "all",
			Page:     1,
			PageSize: 30,
		},
		levePageStr: "1",
		achQuery: gamedata.AchievementSearchQuery{
			Filter:   "all",
			Page:     1,
			PageSize: 30,
		},
		achPageStr: "1",
		musicQuery: gamedata.MusicSearchQuery{
			Kind:        "all",
			ExpansionID: -1,
			Page:        1,
			PageSize:    30,
		},
		musicPageStr:       "1",
		musicVolume:        75,
		musicPlayer:        newMusicPlayer(),
		recentFilter:       "all",
		recentPage:         1,
		recentPageStr:      "1",
		favTab:             "item",
		favItemSort:        "added",
		favItemPage:        1,
		favItemPageStr:     "1",
		favQuestFilter:     "all",
		favQuestPage:       1,
		favQuestPageStr:    "1",
		favLeveKind:        "all",
		favLevePage:        1,
		favLevePageStr:     "1",
		favAchFilter:       "all",
		favAchPage:         1,
		favAchPageStr:      "1",
		favMusicKind:       "all",
		favMusicPage:       1,
		favMusicPageStr:    "1",
		diffFilter:         "all",
		diffEntity:         "all",
		diffPage:           1,
		diffPageStr:        "1",
		diffIconCache:      make(map[string]*ui.Bitmap),
		diffTextExpanded:   make(map[string]bool),
		textSearchScope:    gamedata.TextScopeAll,
		textSearchPage:     1,
		textSearchPageSize: 50,
		textSearchPageStr:  "1",
		textSearchExpanded: make(map[uint32]bool),
		textIndexCache:     make(map[string]*gamedata.TextSearchIndex),
		changelogCatFilter: "all",
		changelogDocs:      make(map[i18n.Lang]changelog.Document),
		changelogCollapsed: make(map[string]bool),
		exportLangs: map[string]bool{
			"chs": true,
			"ja":  true,
			"en":  true,
			"de":  true,
			"fr":  true,
		},
		exportDirDraft:        cfg.ExportOutputDir,
		iconGroupListW:        iconGroupListDefWidth,
		iconQuery:             gamedata.IconSearchQuery{GroupID: -1, VariantFilter: "all"},
		iconGridSelIdx:        -1,
		selectedIconID:        -1,
		iconPreviewCache:      make(map[string]*cachedIconPreview),
		iconTileCache:         newIconTileBitmapCache(900),
		iconExportDirDraft:    cfg.IconExportOutputDir,
		iconExportIncludeStd:  true,
		iconExportIncludeHR1:  true,
		iconExportVariants:    make(map[string]bool),
		diffExportDirDraft:    cfg.DiffExportOutputDir,
		diffExportPreferHR1:   true,
		diffExportAudioFormat: exporter.DiffAudioOGG,
		langCache:             make(map[i18n.Lang]*langSessionCache),
		updaterClient: updater.NewClient(updater.Options{
			CurrentVersion: appVersion,
		}),
	}
	a.rebuildFavIndex()

	// 判断是否已有有效配置：首次启动或路径无效时停留在路径引导页
	if resolved, err := config.ValidateAndResolve(cfg); err != nil {
		a.mode = modeSetup
		if !errors.Is(err, config.ErrNoClientConfigured) {
			a.draftError = a.friendlyConfigError(err, cfg)
		}
	} else {
		a.mode = modeLoading
		a.prepareLoadingState(resolved)
	}
	return a
}

type secondaryLangLine struct {
	badge string
	text  string
}

// Language 返回当前生效的界面语言。
func (a *App) Language() i18n.Lang {
	return i18n.NormalizeLang(string(a.lang))
}

// BCP47Locale 返回供 MyGo 原生控件使用的 BCP 47 语言标识。
func (a *App) BCP47Locale() string {
	return i18n.BCP47Tag(a.Language())
}

func (a *App) t(key string) string {
	return i18n.T(a.Language(), key)
}

func (a *App) tf(key string, pairs ...any) string {
	return i18n.Tf(a.Language(), key, pairs...)
}

// secondaryDataLines 根据当前界面语言返回需要展示的副语言文本行：
// 中文 -> 日语 + 英语；日语 -> 仅英语；英语 -> 仅日语。
func (a *App) secondaryDataLines(primary, nameJa, nameEn string) []secondaryLangLine {
	var out []secondaryLangLine
	for _, sl := range i18n.SecondaryDataLangs(a.Language()) {
		switch sl {
		case i18n.LangJA:
			if nameJa != "" && nameJa != primary {
				out = append(out, secondaryLangLine{badge: "JA", text: nameJa})
			}
		case i18n.LangEN:
			if nameEn != "" && nameEn != primary {
				out = append(out, secondaryLangLine{badge: "EN", text: nameEn})
			}
		}
	}
	return out
}

func clientPathSig(cnPath, globalPath string) string {
	return strings.TrimSpace(cnPath) + "\x00" + strings.TrimSpace(globalPath)
}

func (a *App) saveConfigAsync() {
	cfgCopy := a.cfg
	if a.win == nil {
		_ = config.SaveAppConfig(a.dataDir, cfgCopy)
		return
	}
	a.saveMu.Lock()
	a.saveNextCfg = cfgCopy
	a.savePending = true
	if !a.saveRunning {
		a.saveRunning = true
		done := make(chan struct{})
		a.saveDone = done
		go func() {
			defer close(done)
			for {
				a.saveMu.Lock()
				if !a.savePending {
					a.saveRunning = false
					a.saveMu.Unlock()
					return
				}
				c := a.saveNextCfg
				a.savePending = false
				a.saveMu.Unlock()
				_ = config.SaveAppConfig(a.dataDir, c)
			}
		}()
	}
	a.saveMu.Unlock()
}

func (a *App) flushConfigSave() {
	a.saveMu.Lock()
	done := a.saveDone
	running := a.saveRunning
	a.saveMu.Unlock()
	if running && done != nil {
		<-done
	}
}

func (a *App) rebuildFavIndex() {
	m := make(map[recordKey]bool, len(a.records.Favorites))
	for _, r := range a.records.Favorites {
		m[recordKey{kind: r.Kind, id: r.ID}] = true
	}
	a.favSet = m
}

func (a *App) saveRecordsAsync() {
	recCopy := config.UserRecords{
		Recent:    slices.Clone(a.records.Recent),
		Favorites: slices.Clone(a.records.Favorites),
	}
	if a.win == nil {
		_ = config.SaveUserRecords(a.dataDir, recCopy)
		return
	}
	a.saveRecMu.Lock()
	a.saveRecNext = recCopy
	a.saveRecPending = true
	if !a.saveRecRunning {
		a.saveRecRunning = true
		done := make(chan struct{})
		a.saveRecDone = done
		go func() {
			defer close(done)
			for {
				a.saveRecMu.Lock()
				if !a.saveRecPending {
					a.saveRecRunning = false
					a.saveRecMu.Unlock()
					return
				}
				r := a.saveRecNext
				a.saveRecPending = false
				a.saveRecMu.Unlock()
				_ = config.SaveUserRecords(a.dataDir, r)
			}
		}()
	}
	a.saveRecMu.Unlock()
}

func (a *App) flushRecordsSave() {
	a.saveRecMu.Lock()
	done := a.saveRecDone
	running := a.saveRecRunning
	a.saveRecMu.Unlock()
	if running && done != nil {
		<-done
	}
}

func (a *App) isFavorite(kind config.RecordKind, id int) bool {
	if id <= 0 || a.favSet == nil {
		return false
	}
	return a.favSet[recordKey{kind: kind, id: id}]
}

func (a *App) toggleFavorite(kind config.RecordKind, id int) bool {
	fav, changed := a.records.ToggleFavorite(kind, id, time.Now())
	if !changed {
		return fav
	}
	a.rebuildFavIndex()
	a.saveRecordsAsync()
	if !fav {
		a.syncFavoritesSelectionAfterRemoval(kind, id)
	}
	return fav
}

func (a *App) recordRecent(kind config.RecordKind, id int) {
	if a.records.RecordRecent(kind, id, time.Now()) {
		if a.tab != tabRecent {
			a.recentSelectedKind = ""
			a.recentSelectedID = 0
		}
		a.saveRecordsAsync()
	}
}

func (a *App) clearRecent() {
	if a.records.ClearRecent() {
		a.recentSelectedKind = ""
		a.recentSelectedID = 0
		a.recentItemDetail = nil
		a.recentQuest = nil
		a.recentQuestText = nil
		a.recentQuestTextLoading = false
		a.recentQuestCutscenes = nil
		a.recentQuestCutscenesLoading = false
		a.recentLeve = nil
		a.recentMusicDetail = nil
		a.recentMusicAudioSlot = musicAudioSlot{}
		a.recentPage = 1
		a.recentPageStr = "1"
		a.saveRecordsAsync()
	}
}

// SetLanguage 切换应用界面与数据语言：立即切换界面文案并优先从会话缓存热切，仅在首次切换到未缓存语言时后台平滑加载，不中断当前浏览。
func (a *App) SetLanguage(next i18n.Lang) {
	a.langPopoverOpen = false
	a.globalSearchOpen = false
	norm := i18n.NormalizeLang(string(next))
	changed := a.lang != norm || a.cfg.Language != string(norm)
	a.lang = norm
	a.cfg.Language = string(norm)
	if a.win != nil {
		mygo.App.SetLocale(a.BCP47Locale())
	}
	if !changed {
		return
	}
	a.saveConfigAsync()

	if a.mode == modeSetup {
		if !a.setupModeTouched && strings.TrimSpace(a.draftCNPath) == "" && strings.TrimSpace(a.draftGlobalPath) == "" {
			a.setupMode = inferSetupMode("", "", norm)
		}
		if a.draftError != "" {
			if _, err := config.ValidateAndResolve(config.AppConfig{
				CNPath:     a.draftCNPath,
				GlobalPath: a.draftGlobalPath,
			}); err != nil && !errors.Is(err, config.ErrNoClientConfigured) {
				a.draftError = a.friendlyConfigError(err, config.AppConfig{
					CNPath:     a.draftCNPath,
					GlobalPath: a.draftGlobalPath,
				})
			}
		}
		return
	}

	if a.backupSvc != nil {
		a.backupSvc.SetLang(norm)
	}
	if a.mode == modeLoading {
		// 加载过程中切换语言：更新进度文案，若已配置有效客户端则按新语言重启加载
		a.loadingStatusTxt = a.t("loading.preparing")
		if _, err := config.ValidateAndResolve(a.cfg); err == nil {
			a.startAsyncLoadWithTarget(a.cfg, a.loadReturnTab)
		}
		return
	}
	if a.mode == modeReady {
		sig := clientPathSig(a.cfg.CNPath, a.cfg.GlobalPath)
		if a.langCachePathSig == sig {
			if cached := a.langCache[norm]; cached != nil {
				a.langSwitching = false
				a.activateLangState(cached, true)
				return
			}
		}
		if resolved, err := config.ValidateAndResolve(a.cfg); err == nil {
			a.startBackgroundLangSwitch(a.cfg, resolved, norm)
			return
		}
		if a.diffRes != nil {
			a.refreshDiff()
		}
	}
}

func inferSetupMode(cnPath, globalPath string, lang i18n.Lang) setupServerMode {
	hasCN := strings.TrimSpace(cnPath) != ""
	hasGlobal := strings.TrimSpace(globalPath) != ""
	switch {
	case hasCN && hasGlobal:
		return setupModeBoth
	case hasGlobal:
		return setupModeGlobal
	case hasCN:
		return setupModeCN
	case i18n.NormalizeLang(string(lang)) != i18n.LangZH:
		return setupModeGlobal
	default:
		return setupModeCN
	}
}

func (a *App) prepareLoadingState(resolved *config.ResolvedPaths) {
	a.loadStartedAt = time.Now()
	a.loadStage = loadStageSheets
	a.loadSheetsDone = 0
	a.loadSheetsTotal = 0
	a.loadIndexStep = 0
	a.loadIndexTotal = 4
	a.loadingCNVer = ""
	a.loadingGlobalVer = ""
	if resolved != nil {
		if resolved.HasCN() {
			a.loadingCNVer = resolved.CN.Version
		}
		if resolved.HasGlobal() {
			a.loadingGlobalVer = resolved.Global.Version
		}
	}
	a.loadingStatusTxt = a.t("loading.init_readonly")
}

// ThemeSource 返回当前配置对应的 MyGo 窗口主题源。
func (a *App) ThemeSource() mygo.ThemeSource {
	switch a.cfg.Theme {
	case "light":
		return mygo.ThemeLight
	case "dark":
		return mygo.ThemeDark
	default:
		return mygo.ThemeSystem
	}
}

// SetThemeMode 切换界面外观（system / light / dark），立即应用并持久化配置。
func (a *App) SetThemeMode(mode string) {
	if mode != "light" && mode != "dark" {
		mode = "system"
	}
	if a.cfg.Theme == mode {
		return
	}
	a.cfg.Theme = mode
	// 无窗口的单元测试模式下不调用 mygo.Theme.SetSource，避免无主循环时阻塞 onMain
	if a.win != nil {
		mygo.Theme.SetSource(a.ThemeSource())
	}
	a.saveConfigAsync()
}

// AttachWindow 绑定主窗口实例与窗口材质状态，预热三语字体、触发启动静默更新并在已有有效路径配置时启动异步加载。
func (a *App) AttachWindow(win *mygo.Window, vibrancy bool) {
	a.win = win
	a.vibrancy = vibrancy
	if win != nil {
		prewarmLanguageFonts()
		a.StartStartupUpdateCheck()
	}
	if a.mode == modeLoading {
		a.startAsyncLoad(a.cfg)
	}
}

// Close 释放应用持有的全部本地文件句柄与后台任务。
func (a *App) Close() {
	if a.updateCancel != nil {
		a.updateCancel()
		a.updateCancel = nil
	}
	a.flushConfigSave()
	a.flushRecordsSave()
	if a.musicPlayer != nil {
		a.musicPlayer.Close()
	}
	a.resetLangCacheAndReaders()
}

// navigateTab 切换左侧主菜单并维护前进/后退导航历史。
func (a *App) navigateTab(next activeTab) {
	if next == a.tab {
		return
	}
	a.tabHistory = append(a.tabHistory, a.tab)
	if len(a.tabHistory) > 32 {
		a.tabHistory = a.tabHistory[len(a.tabHistory)-32:]
	}
	a.tabAhead = nil
	a.switchTabSideEffects(next)
	a.tab = next
}

func (a *App) goBack() {
	if len(a.tabHistory) == 0 {
		return
	}
	prev := a.tabHistory[len(a.tabHistory)-1]
	a.tabHistory = a.tabHistory[:len(a.tabHistory)-1]
	a.tabAhead = append(a.tabAhead, a.tab)
	a.switchTabSideEffects(prev)
	a.tab = prev
}

func (a *App) goForward() {
	if len(a.tabAhead) == 0 {
		return
	}
	next := a.tabAhead[len(a.tabAhead)-1]
	a.tabAhead = a.tabAhead[:len(a.tabAhead)-1]
	a.tabHistory = append(a.tabHistory, a.tab)
	a.switchTabSideEffects(next)
	a.tab = next
}

func (a *App) switchTabSideEffects(next activeTab) {
	a.langPopoverOpen = false
	a.statusPopoverOpen = false
	a.globalSearchOpen = false
	a.focusPageSearch = false
	switch next {
	case tabSettings:
		a.draftCNPath = a.cfg.CNPath
		a.draftGlobalPath = a.cfg.GlobalPath
		a.draftError = ""
		a.draftNotice = ""
	case tabRecent:
		a.ensureRecentSelection()
	case tabFavorites:
		a.ensureFavoriteSelection()
	case tabTextSearch:
		a.ensureTextSearchReady()
	}
}

// panelRadius 根据当前平台返回悬浮面板圆角（Windows 8px、macOS/Linux 12px，对齐 Godwit chromeMetrics）。
func (a *App) panelRadius(c *ui.Context) float32 {
	t := themeOf(c)
	if runtime.GOOS == "windows" {
		return t.radius.lg
	}
	return t.radius.xl
}

// windowBackground 返回根窗口画布色（启用 Mica/Vibrancy 时保持通透，否则回退中性底色）。
func (a *App) windowBackground(t uiTokens) ui.Color {
	if a.vibrancy && !a.mapOpen && !a.exportOpen && !a.iconExportOpen && !a.iconLightboxOpen && !a.updateModalOpen && !a.snapMgrOpen && !a.diffExportOpen {
		if t.dark {
			return ui.Transparent
		}
		return t.window.Alpha(materialTint)
	}
	return t.window
}

// View 为 MyGo 原生窗口构建每帧用户界面。
func (a *App) View(c *ui.Context) {
	t := applyAppThemeWithLang(c, a.cfg.Theme, a.Language())
	switch a.mode {
	case modeLoading:
		// 驱动加载页已用时间秒数平稳刷新
		c.After(150 * time.Millisecond)
	case modeReady:
		// 播放音乐或执行后台批量导出/快照下载时提升刷新频率，确保进度条与时间计数平滑推进
		musicPlaying := a.musicPlayer != nil && a.musicPlayer.Snapshot().Playing
		exportRunning := (a.iconExportProgress != nil && a.iconExportProgress.IsRunning) ||
			(a.exportProgress != nil && a.exportProgress.IsRunning) ||
			(a.diffExportProgress != nil && a.diffExportProgress.IsRunning)
		snapActive := a.diffBusy || a.snapCatalogBusy || a.snapDownID != ""
		if musicPlaying || exportRunning || a.textSearchIdxBusy || a.textSearchBusy || snapActive {
			c.After(120 * time.Millisecond)
		} else {
			c.After(time.Second)
		}

		// 全局快捷键（参考 Godwit 桌面端交互）
		if c.Shortcut(ui.Cmd, ui.KeyB) {
			a.sidebarHidden = !a.sidebarHidden
		}
		if c.Shortcut(ui.Cmd, ui.KeyK) {
			a.focusSearch = true
		}
		if c.Shortcut(ui.Cmd, ui.KeyF) && a.tab != tabSettings {
			a.focusPageSearch = true
		}
		if c.Shortcut(ui.Cmd, ui.KeyBracketLeft) {
			a.goBack()
		}
		if c.Shortcut(ui.Cmd, ui.KeyBracketRight) {
			a.goForward()
		}
	}

	rootBg := a.windowBackground(t)
	root := ui.Column(c).Fill().Background(rootBg).Font(t.font).FontSize(t.text.sm).TextColor(t.foreground)
	root.Children(func() {
		switch a.mode {
		case modeSetup:
			a.renderSetupView(c)
		case modeLoading:
			a.renderLoadingView(c)
		default:
			a.renderMainShell(c)
		}
	})

	// 若静默更新已完成但当前页面无侧边栏或侧边栏已折叠，在左下角显示重启提醒卡片（位于模态弹窗之下）
	if a.updateState == updateReady && (a.mode != modeReady || a.sidebarHidden) {
		ui.Box(c).Absolute().Left(14).Bottom(14).Width(212).Children(func() {
			a.renderSidebarAppRestartCard(c)
		})
	}

	// 全局模态弹窗层（地图预览、CSV 导出、图标导出、图标大图预览、版本快照管理、版本对比导出、应用检查更新）
	a.renderMapModal(c)
	a.renderExportModal(c)
	a.renderIconExportModal(c)
	a.renderIconLightboxModal(c)
	a.renderSnapshotManagerModal(c)
	a.renderDiffExportModal(c)
	a.renderUpdateModal(c)

	// 末尾同步 Root 背景，防止 SetTheme 重置透明材质底色
	c.Root().Background(rootBg)
}

// SaveAndReloadPaths 校验并持久化国服/国际服路径配置，然后异步重载游戏数据仓库。
func (a *App) SaveAndReloadPaths(cnPath, globalPath string) bool {
	nextCfg := a.cfg
	nextCfg.CNPath = normalizeUserPathInput(cnPath)
	nextCfg.GlobalPath = normalizeUserPathInput(globalPath)
	nextCfg.Language = string(a.Language())

	resolved, err := config.ValidateAndResolve(nextCfg)
	if err != nil {
		a.draftError = a.friendlyConfigError(err, nextCfg)
		a.draftNotice = ""
		return false
	}

	// 将归一化后的根目录写回配置
	if resolved.HasCN() {
		nextCfg.CNPath = resolved.CN.RootPath
	} else {
		nextCfg.CNPath = ""
	}
	if resolved.HasGlobal() {
		nextCfg.GlobalPath = resolved.Global.RootPath
	} else {
		nextCfg.GlobalPath = ""
	}

	if err := config.SaveAppConfig(a.dataDir, nextCfg); err != nil {
		a.draftError = err.Error()
		a.draftNotice = ""
		return false
	}

	nextSig := clientPathSig(nextCfg.CNPath, nextCfg.GlobalPath)
	if nextSig != a.langCachePathSig {
		a.resetLangCacheAndReaders()
	}
	a.langSwitching = false
	a.cfg = nextCfg
	a.setupMode = inferSetupMode(nextCfg.CNPath, nextCfg.GlobalPath, a.Language())
	a.draftCNPath = nextCfg.CNPath
	a.draftGlobalPath = nextCfg.GlobalPath
	a.draftError = ""
	a.draftNotice = ""
	a.loadError = ""
	a.invalidatePathDiag()
	a.mode = modeLoading
	a.prepareLoadingState(resolved)

	a.startAsyncLoadWithTarget(nextCfg, tabWorkbench)
	return true
}

func (a *App) snapshotDir() string {
	return filepath.Join(a.dataDir, "snapshots")
}

// resetLangCacheAndReaders 释放已缓存的多语言数据仓库与共享只读 SqPack 句柄，并清空贴图缓存。
// 仅在客户端路径变更或应用退出时调用，常规切换语言不调用以保留图标与会话缓存。
func (a *App) resetLangCacheAndReaders() {
	a.cancelSnapshotDownload()
	a.cancelTextSearchTasks()
	clear(a.textIndexCache)
	a.textSearchRes = gamedata.TextSearchResponse{}
	a.textSearchIdxErr = ""
	a.textSearchLang = ""
	if a.exporterSvc != nil {
		a.exporterSvc.Cancel()
	}
	if a.iconExporterSvc != nil {
		a.iconExporterSvc.Cancel()
	}
	if a.diffExporterSvc != nil {
		a.diffExporterSvc.Cancel()
	}
	closedSvc := make(map[*gamedata.Service]bool, len(a.langCache)+1)
	for _, cached := range a.langCache {
		if cached == nil {
			continue
		}
		if cached.exporterSvc != nil {
			cached.exporterSvc.Cancel()
		}
		if cached.iconExporterSvc != nil {
			cached.iconExporterSvc.Cancel()
		}
		if cached.diffExporterSvc != nil {
			cached.diffExporterSvc.Cancel()
		}
		if cached.backupSvc != nil {
			cached.backupSvc.Close()
		}
		if cached.service != nil && !closedSvc[cached.service] {
			closedSvc[cached.service] = true
			_ = cached.service.Close()
		}
	}
	if a.backupSvc != nil {
		a.backupSvc.Close()
	}
	if a.service != nil && !closedSvc[a.service] {
		_ = a.service.Close()
	}
	a.langCache = make(map[i18n.Lang]*langSessionCache)
	a.langCachePathSig = ""
	a.service = nil
	a.backupSvc = nil
	a.exporterSvc = nil
	a.iconExporterSvc = nil
	a.diffExporterSvc = nil
	a.status = nil
	a.versionStatus = nil

	if a.sharedCNReader != nil {
		_ = a.sharedCNReader.Close()
		a.sharedCNReader = nil
	}
	if a.sharedGlobalReader != nil {
		_ = a.sharedGlobalReader.Close()
		a.sharedGlobalReader = nil
	}
	if a.bitmaps != nil {
		a.bitmaps.Clear()
	}
	if a.iconTileCache != nil {
		a.iconTileCache.Clear()
	}
	clear(a.iconPreviewCache)
	clear(a.diffIconCache)
	a.iconExportTaskCountKey = ""
	a.iconLightboxOpen = false
	a.diffExportOpen = false
	a.diffExportProgress = nil
	a.diffExportError = ""
}

func (a *App) buildLangSessionState(
	resolved *config.ResolvedPaths,
	loadLang i18n.Lang,
	baseStore *gamedata.Store,
	onProgress func(gamedata.LoadProgress),
) (*langSessionCache, *sqpack.GameReader, *sqpack.GameReader, error) {
	store, err := gamedata.LoadGameDataStoreWithOptions(resolved, a.schemasFS, gamedata.StoreLoadOptions{
		AppLang:       loadLang,
		BaseStore:     baseStore,
		SharedReaders: true,
		OnProgress:    onProgress,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	svc := gamedata.NewService(store)
	status := svc.GetSystemStatus()

	bSvc := backup.NewService(store, a.snapshotDir())
	bSvc.SetLang(loadLang)
	vStatus := bSvc.GetVersionStatus()

	sm := exporter.NewSchemaManager(a.schemasFS)
	expSvc := exporter.NewService(store.CNReader, store.GlobalReader, sm)
	iconExpSvc := exporter.NewIconService(svc)
	diffExpSvc := exporter.NewDiffService(svc, bSvc)

	state := &langSessionCache{
		service:         svc,
		backupSvc:       bSvc,
		exporterSvc:     expSvc,
		iconExporterSvc: iconExpSvc,
		diffExporterSvc: diffExpSvc,
		status:          status,
		versionStatus:   vStatus,
		exportTotalShts: expSvc.TotalSheetCount(),
	}
	return state, store.CNReader, store.GlobalReader, nil
}

func (a *App) recordSharedReaders(cn, global *sqpack.GameReader) {
	if a.sharedCNReader == nil && cn != nil {
		a.sharedCNReader = cn
	}
	if a.sharedGlobalReader == nil && global != nil {
		a.sharedGlobalReader = global
	}
}

func (a *App) activateLangState(state *langSessionCache, preserveSelection bool) {
	if state == nil {
		return
	}
	prevItemID := a.selectedItemID
	prevDetailScroll := a.detailScroll.Y
	prevQuestID := a.selectedQuestID
	prevLeveID := a.selectedLeveID
	prevMusicKind := a.selectedMusicKind
	prevMusicID := a.selectedMusicID
	prevIconID := a.selectedIconID

	st := state.status
	vst := state.versionStatus
	a.service = state.service
	a.backupSvc = state.backupSvc
	a.exporterSvc = state.exporterSvc
	a.iconExporterSvc = state.iconExporterSvc
	a.diffExporterSvc = state.diffExporterSvc
	if a.diffExporterSvc == nil && a.backupSvc != nil {
		a.diffExporterSvc = exporter.NewDiffService(a.service, a.backupSvc)
	}
	a.status = &st
	a.versionStatus = &vst
	a.dismissUpdateBanner = false
	a.exportTotalShts = state.exportTotalShts
	if a.backupSvc != nil {
		a.backupSvc.SetLang(a.Language())
		freshStatus := a.backupSvc.ReloadLocalSnapshots()
		a.versionStatus = &freshStatus
		vst = freshStatus
	}
	a.syncAvailableExportLangs()
	a.syncAvailableIconExportVariants()
	clear(a.iconPreviewCache)
	clear(a.diffIconCache)
	a.iconExportTaskCountKey = ""

	if !preserveSelection {
		a.itemQuery.Page = 1
		a.itemPageStr = "1"
	}
	a.refreshItemList()
	a.refreshQuests()
	if preserveSelection && prevQuestID > 0 && a.service.GetQuestDetail(prevQuestID) != nil {
		a.jumpToQuestInternal(prevQuestID, false)
	} else if len(a.questRes.Items) > 0 {
		a.selectQuestInternal(a.questRes.Items[0].ID, false)
	} else {
		a.selectedQuestID = 0
		a.selectedQuest = nil
	}

	a.refreshLeves()
	if preserveSelection && prevLeveID > 0 && a.service.GetLeveDetail(prevLeveID) != nil {
		a.jumpToLeveInternal(prevLeveID, false)
	} else if len(a.leveRes.Items) > 0 {
		a.selectLeveInternal(a.leveRes.Items[0].ID, false)
	} else {
		a.selectedLeveID = 0
		a.selectedLeve = nil
	}
	a.refreshAchievements()

	a.refreshMusicList()
	if preserveSelection && prevMusicID > 0 && a.service.GetMusicDetail(prevMusicKind, prevMusicID) != nil {
		a.jumpToMusicInternal(prevMusicKind, prevMusicID, false)
	} else if len(a.musicRes.Items) > 0 {
		a.selectMusicInternal(a.musicRes.Items[0].Kind, a.musicRes.Items[0].ID, false)
	} else {
		a.selectedMusicKey = ""
		a.selectedMusicKind = ""
		a.selectedMusicID = 0
		a.musicDetail = nil
	}

	if !preserveSelection {
		a.iconQuery.GroupID = -1
	} else if a.iconQuery.GroupID >= 0 && !a.hasIconGroup(a.iconQuery.GroupID) {
		a.iconQuery.GroupID = -1
	}
	if !preserveSelection {
		a.selectedIconID = -1
	} else {
		a.selectedIconID = prevIconID
	}
	a.refreshIcons(!preserveSelection)

	if preserveSelection && prevItemID > 0 && a.service.GetItemDetail(prevItemID) != nil {
		a.jumpToItemInternal(prevItemID, false)
		a.detailScroll.Y = prevDetailScroll
	} else if a.service.GetItemDetail(44099) != nil {
		a.selectItemInternal(44099, false)
	} else if len(a.itemRes.Items) > 0 {
		a.selectItemInternal(a.itemRes.Items[0].ID, false)
	} else {
		a.selectedItemID = 0
		a.selectedDetail = nil
	}

	a.refreshRecentAndFavoriteDetails()

	if !preserveSelection || a.diffRegion == "" ||
		(a.diffRegion == snapshot.RegionCN && !vst.HasCN) ||
		(a.diffRegion == snapshot.RegionGlobal && !vst.HasGlobal) {
		a.diffRegion = vst.DefaultRegion
	}
	a.diffFromVer, a.diffToVer = vst.DefaultPairForRegion(a.diffRegion)
	a.diffPage = 1
	a.diffPageStr = "1"
	if a.diffFromVer != "" && a.diffToVer != "" {
		a.refreshDiff()
	} else {
		a.diffError = ""
		a.diffRes = &backup.VersionDiffResponse{
			NeedsDownload: true,
			Region:        a.diffRegion,
			FromID:        a.diffFromVer,
			ToID:          a.diffToVer,
			Page:          1,
			TotalPages:    1,
		}
	}
	a.syncTextSearchOnLangChange()
	a.refreshGlobalSearch()
}

func (a *App) startBackgroundLangSwitch(cfg config.AppConfig, resolved *config.ResolvedPaths, targetLang i18n.Lang) {
	a.loadSeq++
	seq := a.loadSeq
	sig := clientPathSig(cfg.CNPath, cfg.GlobalPath)
	a.langSwitching = true

	var baseStore *gamedata.Store
	if a.langCachePathSig == sig && a.service != nil {
		baseStore = a.service.Store()
	}

	apply := func(state *langSessionCache, cnR, glR *sqpack.GameReader, err error) {
		if err != nil {
			if a.loadSeq == seq {
				a.langSwitching = false
			}
			return
		}
		if a.langCachePathSig != sig {
			a.resetLangCacheAndReaders()
			a.langCachePathSig = sig
		}
		a.recordSharedReaders(cnR, glR)
		if a.langCache == nil {
			a.langCache = make(map[i18n.Lang]*langSessionCache)
		}
		a.langCache[targetLang] = state
		if a.loadSeq == seq && a.mode == modeReady && a.Language() == targetLang {
			a.langSwitching = false
			a.activateLangState(state, true)
		}
	}

	if a.win == nil {
		state, cnR, glR, err := a.buildLangSessionState(resolved, targetLang, baseStore, nil)
		apply(state, cnR, glR, err)
		return
	}

	go func() {
		state, cnR, glR, err := a.buildLangSessionState(resolved, targetLang, baseStore, nil)
		a.win.Update(func() {
			apply(state, cnR, glR, err)
		})
	}()
}

func (a *App) startAsyncLoad(cfg config.AppConfig) {
	a.startAsyncLoadWithTarget(cfg, tabWorkbench)
}

func (a *App) startAsyncLoadWithTarget(cfg config.AppConfig, returnTab activeTab) {
	a.loadReturnTab = returnTab
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		a.mode = modeSetup
		a.setupMode = inferSetupMode(cfg.CNPath, cfg.GlobalPath, a.Language())
		a.draftError = a.friendlyConfigError(err, cfg)
		return
	}

	sig := clientPathSig(cfg.CNPath, cfg.GlobalPath)
	a.loadSeq++
	seq := a.loadSeq
	a.prepareLoadingState(resolved)

	applyProgress := func(stage loadStage, done, total int, msg string) {
		if a.loadSeq != seq || a.mode != modeLoading {
			return
		}
		a.loadStage = stage
		switch stage {
		case loadStageSheets:
			a.loadSheetsDone = done
			a.loadSheetsTotal = total
		case loadStageIndices:
			a.loadIndexStep = done
			if total > 0 {
				a.loadIndexTotal = total
			}
		}
		if msg != "" {
			a.loadingStatusTxt = msg
		}
	}

	dispatchProgress := func(stage loadStage, done, total int, msg string) {
		if a.win == nil {
			applyProgress(stage, done, total, msg)
			return
		}
		a.win.Update(func() {
			applyProgress(stage, done, total, msg)
		})
	}

	var baseStore *gamedata.Store
	if a.langCachePathSig == sig && a.service != nil {
		baseStore = a.service.Store()
	}

	loadLang := a.Language()
	runLoad := func() (*langSessionCache, *sqpack.GameReader, *sqpack.GameReader, error) {
		state, cnR, glR, err := a.buildLangSessionState(resolved, loadLang, baseStore, func(p gamedata.LoadProgress) {
			st := loadStageSheets
			if p.Stage == gamedata.LoadStageIndices {
				st = loadStageIndices
			}
			dispatchProgress(st, p.Done, p.Total, p.Message)
		})
		if err != nil {
			return nil, nil, nil, err
		}
		return state, cnR, glR, nil
	}

	applyResult := func(state *langSessionCache, cnR, glR *sqpack.GameReader, err error) {
		if a.loadSeq != seq {
			if state != nil && state.service != nil {
				_ = state.service.Close()
			}
			return
		}
		if err != nil {
			a.loadError = err.Error()
			a.draftError = a.tf("setup.err_load", "err", err.Error())
			a.mode = modeSetup
			return
		}
		if a.langCachePathSig != sig {
			a.resetLangCacheAndReaders()
			a.langCachePathSig = sig
		}
		a.recordSharedReaders(cnR, glR)
		if a.langCache == nil {
			a.langCache = make(map[i18n.Lang]*langSessionCache)
		}
		a.langCache[loadLang] = state
		a.activateLangState(state, false)

		a.mode = modeReady
		if a.loadReturnTab == tabSettings {
			a.tab = tabSettings
		} else {
			a.tab = tabWorkbench
		}
	}

	if a.win == nil {
		state, cnR, glR, err := runLoad()
		applyResult(state, cnR, glR, err)
		return
	}

	go func() {
		state, cnR, glR, err := runLoad()
		a.win.Update(func() {
			applyResult(state, cnR, glR, err)
		})
	}()
}

func (a *App) syncAvailableExportLangs() {
	if a.exporterSvc == nil {
		return
	}
	avail := a.exporterSvc.AvailableLangCodes()
	availSet := make(map[string]bool, len(avail))
	for _, code := range avail {
		availSet[code] = true
	}
	anySelected := false
	for code := range a.exportLangs {
		if !availSet[code] {
			a.exportLangs[code] = false
		} else if a.exportLangs[code] {
			anySelected = true
		}
	}
	if !anySelected {
		for _, code := range avail {
			a.exportLangs[code] = true
		}
	}
}

func (a *App) refreshItemList() {
	if a.service == nil {
		return
	}
	a.itemQuery.MinIlvl = parseOptionalPosInt(a.itemMinIlvlStr)
	a.itemQuery.MaxIlvl = parseOptionalPosInt(a.itemMaxIlvlStr)
	a.itemRes = a.service.SearchItems(a.itemQuery)
	a.itemQuery.Page = a.itemRes.Page
	a.itemPageStr = strconv.Itoa(a.itemRes.Page)
	a.itemListScroll.Y = 0
}

func (a *App) selectItem(itemID int) {
	a.selectItemInternal(itemID, true)
}

func (a *App) selectItemInternal(itemID int, record bool) {
	if a.service == nil || itemID <= 0 {
		return
	}
	detail := a.service.GetItemDetail(itemID)
	if detail == nil {
		return
	}
	a.selectedItemID = itemID
	a.selectedDetail = detail
	a.activeRecipeIdx = 0
	a.detailScroll.Y = 0

	if record {
		a.recordRecent(config.RecordKindItem, itemID)
	}
}

func (a *App) locateItemInList(itemID int) {
	if a.service == nil || itemID <= 0 {
		return
	}
	a.itemQuery.MinIlvl = parseOptionalPosInt(a.itemMinIlvlStr)
	a.itemQuery.MaxIlvl = parseOptionalPosInt(a.itemMaxIlvlStr)
	res, ok := a.service.LocateItemPage(a.itemQuery, itemID)
	if !ok {
		pageSize := a.itemQuery.PageSize
		if pageSize <= 0 {
			pageSize = 30
		}
		sortMode := a.itemQuery.Sort
		if sortMode == "" {
			sortMode = "id_asc"
		}
		a.itemQuery = gamedata.ItemSearchQuery{
			Filter:   "all",
			Sort:     sortMode,
			Page:     1,
			PageSize: pageSize,
		}
		a.itemMinIlvlStr = ""
		a.itemMaxIlvlStr = ""
		res, ok = a.service.LocateItemPage(a.itemQuery, itemID)
	}
	if ok {
		if a.itemQuery.Page != res.Page {
			a.itemListScroll.Y = 0
		}
		a.itemQuery.Page = res.Page
		a.itemPageStr = strconv.Itoa(res.Page)
		a.itemRes = res
		a.scrollItemListToID = itemID
	}
}

func (a *App) jumpToItem(itemID int) {
	a.jumpToItemInternal(itemID, true)
}

func (a *App) jumpToItemInternal(itemID int, record bool) {
	if a.service == nil || itemID <= 0 {
		return
	}
	a.selectItemInternal(itemID, record)
	a.locateItemInList(itemID)
}

func (a *App) inspectItemFromOtherTab(itemID int) {
	a.jumpToItem(itemID)
	a.navigateTab(tabWorkbench)
}

func (a *App) refreshQuests() {
	if a.service == nil {
		return
	}
	a.questRes = a.service.SearchQuests(a.questQuery)
	a.questQuery.Page = a.questRes.Page
	a.questPageStr = strconv.Itoa(a.questRes.Page)
	a.questListScroll.Y = 0
}

func loadQuestAsyncData[T any](
	a *App,
	questID int,
	seqPtr *uint64,
	loadingPtr *bool,
	isStillSelected func() bool,
	fetch func(*gamedata.Service, int) T,
	onLoaded func(T),
) {
	*seqPtr++
	seq := *seqPtr
	svc := a.service
	var zero T
	if svc == nil || questID <= 0 {
		*loadingPtr = false
		onLoaded(zero)
		return
	}
	if a.win == nil {
		*loadingPtr = false
		onLoaded(fetch(svc, questID))
		return
	}
	*loadingPtr = true
	go func() {
		val := fetch(svc, questID)
		a.win.Update(func() {
			if *seqPtr != seq || !isStillSelected() {
				return
			}
			*loadingPtr = false
			onLoaded(val)
		})
	}()
}

func (a *App) loadQuestTextAsync(
	questID int,
	seqPtr *uint64,
	loadingPtr *bool,
	isStillSelected func() bool,
	onLoaded func(*gamedata.QuestTextData),
) {
	loadQuestAsyncData(
		a, questID, seqPtr, loadingPtr, isStillSelected,
		func(svc *gamedata.Service, qid int) *gamedata.QuestTextData {
			return svc.GetQuestText(qid)
		},
		onLoaded,
	)
}

func (a *App) loadQuestCutscenesAsync(
	questID int,
	seqPtr *uint64,
	loadingPtr *bool,
	isStillSelected func() bool,
	onLoaded func([]gamedata.QuestCutsceneEntry),
) {
	loadQuestAsyncData(
		a, questID, seqPtr, loadingPtr, isStillSelected,
		func(svc *gamedata.Service, qid int) []gamedata.QuestCutsceneEntry {
			return svc.GetQuestCutscenes(qid)
		},
		onLoaded,
	)
}

func (a *App) selectQuest(questID int) {
	a.selectQuestInternal(questID, true)
}

func (a *App) selectQuestInternal(questID int, record bool) {
	if a.service == nil || questID <= 0 {
		return
	}
	detail := a.service.GetQuestDetail(questID)
	if detail == nil {
		return
	}
	a.selectedQuestID = questID
	a.selectedQuest = detail
	a.questDetailScroll.Y = 0
	// 切换任务时默认折叠任务概要与对话文本，避免剧透
	a.questSummaryOpen = false
	a.questDialogueOpen = false
	a.selectedQuestText = nil
	a.selectedQuestCutscenes = nil

	if record {
		a.recordRecent(config.RecordKindQuest, questID)
	}

	isCur := func() bool { return a.selectedQuestID == questID }
	a.loadQuestTextAsync(
		questID,
		&a.questTextSeq,
		&a.questTextLoading,
		isCur,
		func(txt *gamedata.QuestTextData) { a.selectedQuestText = txt },
	)
	if len(detail.Cutscenes) > 0 {
		a.loadQuestCutscenesAsync(
			questID,
			&a.questCutscenesSeq,
			&a.questCutscenesLoading,
			isCur,
			func(cuts []gamedata.QuestCutsceneEntry) { a.selectedQuestCutscenes = cuts },
		)
	} else {
		a.questCutscenesLoading = false
	}
}

func (a *App) locateQuestInList(questID int) {
	if a.service == nil || questID <= 0 {
		return
	}
	res, ok := a.service.LocateQuestPage(a.questQuery, questID)
	if !ok {
		pageSize := a.questQuery.PageSize
		if pageSize <= 0 {
			pageSize = 30
		}
		a.questQuery = gamedata.QuestSearchQuery{
			SectionID:   -1,
			ExpansionID: -1,
			Filter:      "all",
			Page:        1,
			PageSize:    pageSize,
		}
		res, ok = a.service.LocateQuestPage(a.questQuery, questID)
	}
	if ok {
		if a.questQuery.Page != res.Page {
			a.questListScroll.Y = 0
		}
		a.questQuery.Page = res.Page
		a.questPageStr = strconv.Itoa(res.Page)
		a.questRes = res
		a.scrollQuestListToID = questID
	}
}

func (a *App) jumpToQuest(questID int) {
	a.jumpToQuestInternal(questID, true)
}

func (a *App) jumpToQuestInternal(questID int, record bool) {
	if a.service == nil || questID <= 0 {
		return
	}
	a.selectQuestInternal(questID, record)
	a.locateQuestInList(questID)
}

func (a *App) inspectQuestFromOtherTab(questID int) {
	a.jumpToQuest(questID)
	a.navigateTab(tabQuests)
}

func (a *App) refreshLeves() {
	if a.service == nil {
		return
	}
	a.leveRes = a.service.SearchLeves(a.leveQuery)
	a.leveQuery.Page = a.leveRes.Page
	a.levePageStr = strconv.Itoa(a.leveRes.Page)
	a.leveListScroll.Y = 0
}

func (a *App) selectLeve(leveID int) {
	a.selectLeveInternal(leveID, true)
}

func (a *App) selectLeveInternal(leveID int, record bool) {
	if a.service == nil || leveID <= 0 {
		return
	}
	detail := a.service.GetLeveDetail(leveID)
	if detail == nil {
		return
	}
	a.selectedLeveID = leveID
	a.selectedLeve = detail
	a.leveDetailScroll.Y = 0
	if record {
		a.recordRecent(config.RecordKindLeve, leveID)
	}
}

func (a *App) locateLeveInList(leveID int) {
	if a.service == nil || leveID <= 0 {
		return
	}
	res, ok := a.service.LocateLevePage(a.leveQuery, leveID)
	if !ok {
		pageSize := a.leveQuery.PageSize
		if pageSize <= 0 {
			pageSize = 30
		}
		a.leveQuery = gamedata.LeveSearchQuery{
			Kind:     "all",
			Page:     1,
			PageSize: pageSize,
		}
		res, ok = a.service.LocateLevePage(a.leveQuery, leveID)
	}
	if ok {
		if a.leveQuery.Page != res.Page {
			a.leveListScroll.Y = 0
		}
		a.leveQuery.Page = res.Page
		a.levePageStr = strconv.Itoa(res.Page)
		a.leveRes = res
		a.scrollLeveListToID = leveID
	}
}

func (a *App) jumpToLeve(leveID int) {
	a.jumpToLeveInternal(leveID, true)
}

func (a *App) jumpToLeveInternal(leveID int, record bool) {
	if a.service == nil || leveID <= 0 {
		return
	}
	a.selectLeveInternal(leveID, record)
	a.locateLeveInList(leveID)
}

func (a *App) inspectLeveFromOtherTab(leveID int) {
	a.jumpToLeve(leveID)
	a.navigateTab(tabLeves)
}

// inspectAchievementFromOtherTab 跳转至成就页并按 ID 筛选定位目标成就；若无匹配且存在奖励道具则回退跳转道具详情。
func (a *App) inspectAchievementFromOtherTab(achID, fallbackRewardItemID int) {
	if a.service == nil || achID <= 0 {
		return
	}
	query := gamedata.AchievementSearchQuery{
		Q:        strconv.Itoa(achID),
		Filter:   "all",
		Page:     1,
		PageSize: 30,
	}
	res := a.service.SearchAchievements(query)
	if res.Total > 0 {
		a.achQuery = query
		a.achRes = res
		a.achPageStr = strconv.Itoa(res.Page)
		a.achScroll.Y = 0
		a.scrollAchToID = achID
		a.navigateTab(tabAchievements)
		return
	}
	if fallbackRewardItemID > 0 {
		a.inspectItemFromOtherTab(fallbackRewardItemID)
	}
}

func (a *App) refreshAchievements() {
	if a.service == nil {
		return
	}
	a.achRes = a.service.SearchAchievements(a.achQuery)
	a.achQuery.Page = a.achRes.Page
	a.achPageStr = strconv.Itoa(a.achRes.Page)
	a.achScroll.Y = 0
}

func (a *App) refreshGlobalSearch() {
	q := strings.TrimSpace(a.globalSearchQ)
	if a.service == nil || q == "" {
		a.globalSearchRes = gamedata.GlobalSearchResult{}
		return
	}
	a.globalSearchRes = a.service.SearchGlobal(q, 50)
}

func (a *App) jumpToModuleSearch(target activeTab) {
	q := strings.TrimSpace(a.globalSearchQ)
	a.globalSearchOpen = false
	switch target {
	case tabQuests:
		a.questQuery.Q = q
		a.questQuery.Page = 1
		a.refreshQuests()
	case tabLeves:
		a.leveQuery.Q = q
		a.leveQuery.Page = 1
		a.refreshLeves()
	case tabAchievements:
		a.achQuery.Q = q
		a.achQuery.Page = 1
		a.refreshAchievements()
	case tabMusic:
		a.musicQuery.Q = q
		a.musicQuery.Page = 1
		a.refreshMusicList()
		if a.musicRes.Total == 0 && q != "" && a.hasMusicFilter() {
			a.musicQuery.Kind = "all"
			a.musicQuery.ExpansionID = -1
			a.musicQuery.CategoryID = 0
			a.musicQuery.WithUsageOnly = false
			a.refreshMusicList()
		}
	case tabIcons:
		a.iconQuery.Q = q
		a.refreshIcons(true)
		if a.iconRes.Total == 0 && q != "" && (a.iconQuery.GroupID >= 0 || a.iconQuery.VariantFilter != "all") {
			a.iconQuery.GroupID = -1
			a.iconQuery.VariantFilter = "all"
			a.refreshIcons(true)
		}
	case tabDiff:
		a.diffSearch = q
		a.diffPage = 1
		a.refreshDiff()
	case tabTextSearch:
		a.textSearchQ = q
		a.textSearchSheet = ""
		a.textSearchPage = 1
		a.textSearchPageStr = "1"
		a.ensureTextSearchReady()
		a.runTextSearch(false)
	default:
		target = tabWorkbench
		a.itemQuery.Q = q
		a.itemQuery.Page = 1
		a.refreshItemList()
	}
	a.navigateTab(target)
}

func (a *App) openGlobalSearchHit(hit gamedata.GlobalSearchHit) {
	a.globalSearchOpen = false
	switch hit.Kind {
	case gamedata.GlobalHitItem:
		a.inspectItemFromOtherTab(hit.ID)
	case gamedata.GlobalHitQuest:
		a.inspectQuestFromOtherTab(hit.ID)
	case gamedata.GlobalHitLeve:
		a.inspectLeveFromOtherTab(hit.ID)
	case gamedata.GlobalHitAchievement:
		a.inspectAchievementFromOtherTab(hit.ID, hit.RewardItemID)
	case gamedata.GlobalHitMusic:
		a.inspectMusicFromOtherTab(hit.MusicKind, hit.ID)
	}
}

// inspectIconFromOtherTab 从其他视图（如版本对比）跳转至「图标」页并定位高亮对应编号图标。
func (a *App) inspectIconFromOtherTab(iconID int) {
	if a.service == nil || iconID < 0 {
		return
	}
	detail := a.service.GetIconDetail(iconID)
	if detail == nil {
		return
	}
	a.selectedIconID = iconID
	if a.iconQuery.GroupID >= 0 && a.iconQuery.GroupID != sqpack.IconGroupID(iconID) {
		a.iconQuery.GroupID = sqpack.IconGroupID(iconID)
	}
	a.refreshIcons(false)
	if a.iconGridSelIdx < 0 {
		a.iconQuery.Q = ""
		a.iconQuery.VariantFilter = "all"
		a.iconQuery.GroupID = -1
		a.refreshIcons(false)
	}
	if a.iconGridSelIdx >= 0 {
		a.iconGridState.ScrollIntoView(a.iconGridSelIdx)
	}
	a.navigateTab(tabIcons)
}

func (a *App) switchDiffRegion(region string) {
	norm := snapshot.NormalizeRegion(region)
	if a.versionStatus != nil {
		if norm == snapshot.RegionCN && !a.versionStatus.HasCN {
			return
		}
		if norm == snapshot.RegionGlobal && !a.versionStatus.HasGlobal {
			return
		}
	}
	if a.diffRegion == norm {
		return
	}
	prevFrom := a.diffFromVer
	prevTo := a.diffToVer
	a.diffRegion = norm
	if a.versionStatus != nil {
		backups := a.versionStatus.BackupsForRegion(norm)
		hasInRegion := func(id string) bool {
			for _, b := range backups {
				if b.ID == id {
					return true
				}
			}
			return false
		}
		defFrom, defTo := a.versionStatus.DefaultPairForRegion(norm)
		if hasInRegion(prevFrom) {
			a.diffFromVer = prevFrom
		} else {
			a.diffFromVer = defFrom
		}
		if hasInRegion(prevTo) {
			a.diffToVer = prevTo
		} else {
			a.diffToVer = defTo
		}
		if a.diffFromVer == a.diffToVer && defFrom != "" && defFrom != a.diffToVer {
			a.diffFromVer = defFrom
		}
	}
	a.diffPage = 1
	a.diffPageStr = "1"
	a.diffScroll.Y = 0
	a.refreshDiff()
}

func (a *App) refreshDiff() {
	if a.backupSvc == nil {
		return
	}
	if a.diffEntity == "text" && a.diffFilter == "added" {
		a.diffFilter = "all"
	}
	pageSize := 25
	if a.diffEntity == "icon" {
		pageSize = 120
	} else if a.diffEntity == "text" {
		pageSize = 30
	}
	query := backup.VersionDiffQuery{
		Region:     a.diffRegion,
		From:       a.diffFromVer,
		To:         a.diffToVer,
		Filter:     a.diffFilter,
		EntityType: a.diffEntity,
		Q:          a.diffSearch,
		Page:       a.diffPage,
		PageSize:   pageSize,
	}
	a.diffSeq++
	seq := a.diffSeq
	svc := a.backupSvc

	apply := func(res backup.VersionDiffResponse, err error) {
		if a.diffSeq != seq || a.backupSvc != svc {
			return
		}
		a.diffBusy = false
		if err != nil {
			a.diffError = a.tf("diff.err_compare", "err", err.Error())
			return
		}
		a.diffError = ""
		a.diffRes = &res
		a.diffPage = res.Page
		a.diffPageStr = strconv.Itoa(res.Page)
		a.diffScroll.Y = 0
		clear(a.diffTextExpanded)
	}

	if a.win == nil {
		res, err := svc.CompareVersions(query)
		apply(res, err)
		return
	}

	a.diffBusy = true
	a.diffError = ""
	go func() {
		res, err := svc.CompareVersions(query)
		a.dispatchUI(func() {
			apply(res, err)
		})
	}()
}

func (a *App) openSnapshotManager() {
	a.langPopoverOpen = false
	a.statusPopoverOpen = false
	a.globalSearchOpen = false
	a.snapMgrOpen = true
	a.snapMgrError = ""
	a.snapMgrNotice = ""
	if !a.snapCatalogTried && !a.snapCatalogBusy {
		a.refreshSnapshotCatalog(false)
	}
}

func (a *App) syncVersionStatus(st backup.VersionStatusResponse) {
	cp := st
	a.versionStatus = &cp
	if a.langCache != nil {
		if cached, ok := a.langCache[a.Language()]; ok && cached != nil {
			cached.versionStatus = st
		}
	}
	if a.diffRegion == "" ||
		(a.diffRegion == snapshot.RegionCN && !st.HasCN) ||
		(a.diffRegion == snapshot.RegionGlobal && !st.HasGlobal) {
		a.diffRegion = st.DefaultRegion
	}
	backups := st.BackupsForRegion(a.diffRegion)
	defFrom, defTo := st.DefaultPairForRegion(a.diffRegion)

	// 如果尚未选定对比版本且当前区服已有可用快照，自动填入推荐组合
	if a.diffToVer == "" && defTo != "" {
		a.diffToVer = defTo
	}
	if a.diffFromVer == "" && defFrom != "" {
		a.diffFromVer = defFrom
	}
	// 校验当前选中的快照是否仍存在于当前区服可用列表，若被删除则自动回退
	hasID := func(id string) bool {
		if id == "" {
			return false
		}
		for _, b := range backups {
			if b.ID == id {
				return true
			}
		}
		return false
	}
	if a.diffFromVer != "" && !hasID(a.diffFromVer) {
		a.diffFromVer = defFrom
	}
	if a.diffToVer != "" && !hasID(a.diffToVer) {
		a.diffToVer = defTo
	}
}

func (a *App) refreshSnapshotCatalog(manual bool) {
	if a.backupSvc == nil || a.snapCatalogBusy {
		return
	}
	a.snapCatalogTried = true
	a.snapCatalogBusy = true
	if manual {
		a.snapMgrError = ""
		a.snapMgrNotice = ""
	}
	svc := a.backupSvc

	apply := func(st backup.VersionStatusResponse, err error) {
		a.snapCatalogBusy = false
		if a.backupSvc != svc {
			return
		}
		a.syncVersionStatus(st)
		if err != nil {
			if manual || !st.CatalogLoaded {
				a.snapMgrError = a.tf("diff.snap_err_catalog", "err", err.Error())
			}
			return
		}
		a.snapMgrError = ""
	}

	if a.win == nil {
		st, err := svc.RefreshCatalog(context.Background())
		apply(st, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		st, err := svc.RefreshCatalog(ctx)
		a.dispatchUI(func() {
			apply(st, err)
		})
	}()
}

func (a *App) startSnapshotDownload(patch, displayName string) {
	if a.backupSvc == nil || a.snapDownID != "" || patch == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.snapDownID = patch
	a.snapDownName = displayName
	a.snapDownBytes = 0
	a.snapDownTotal = 0
	a.snapDownCancel = cancel
	a.snapMgrError = ""
	a.snapMgrNotice = ""
	svc := a.backupSvc

	apply := func(st backup.VersionStatusResponse, err error) {
		if a.snapDownID != patch {
			return
		}
		a.snapDownID = ""
		a.snapDownName = ""
		a.snapDownBytes = 0
		a.snapDownTotal = 0
		a.snapDownCancel = nil
		if a.backupSvc != svc {
			return
		}
		a.syncVersionStatus(st)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				a.snapMgrError = a.tf("diff.snap_err_download", "err", err.Error())
			}
			return
		}
		a.snapMgrNotice = a.tf("diff.snap_notice_downloaded", "name", displayName)
		a.diffNotice = a.snapMgrNotice
		if a.diffFromVer == "" || (a.diffRes != nil && a.diffRes.NeedsDownload) {
			backups := st.BackupsForRegion(a.diffRegion)
			hasPatch := false
			for _, b := range backups {
				if b.ID == patch {
					hasPatch = true
					break
				}
			}
			defFrom, defTo := st.DefaultPairForRegion(a.diffRegion)
			if hasPatch {
				a.diffFromVer = patch
			} else {
				a.diffFromVer = defFrom
			}
			if a.diffToVer == "" {
				a.diffToVer = defTo
			}
			a.diffPage = 1
			a.refreshDiff()
		}
	}

	onProgress := func(down, total int64) {
		a.dispatchUI(func() {
			if a.snapDownID == patch {
				a.snapDownBytes = down
				a.snapDownTotal = total
			}
		})
	}

	if a.win == nil {
		st, err := svc.DownloadVersion(ctx, patch, onProgress)
		apply(st, err)
		return
	}
	go func() {
		st, err := svc.DownloadVersion(ctx, patch, onProgress)
		a.dispatchUI(func() {
			apply(st, err)
		})
	}()
}

func (a *App) cancelSnapshotDownload() {
	if a.snapDownCancel != nil {
		a.snapDownCancel()
		a.snapDownCancel = nil
	}
	a.snapDownID = ""
	a.snapDownName = ""
	a.snapDownBytes = 0
	a.snapDownTotal = 0
}

func (a *App) deleteLocalSnapshot(patch, displayName string) {
	if a.backupSvc == nil || patch == "" || patch == backup.LiveVersionID {
		return
	}
	st, err := a.backupSvc.DeleteVersion(patch)
	if err != nil {
		a.snapMgrError = a.tf("diff.snap_err_delete", "err", err.Error())
		return
	}
	a.snapMgrError = ""
	a.snapMgrNotice = a.tf("diff.snap_notice_deleted", "name", displayName)
	a.syncVersionStatus(st)
	a.diffPage = 1
	a.refreshDiff()
}

func (a *App) selectSnapshotAsFrom(patch string) {
	if patch == "" {
		return
	}
	if a.versionStatus != nil {
		inRegion := func(reg string) bool {
			for _, b := range a.versionStatus.BackupsForRegion(reg) {
				if b.ID == patch {
					return true
				}
			}
			return false
		}
		if !inRegion(a.diffRegion) {
			other := snapshot.RegionGlobal
			if a.diffRegion == snapshot.RegionGlobal {
				other = snapshot.RegionCN
			}
			if inRegion(other) {
				a.diffRegion = other
			}
		}
		_, defTo := a.versionStatus.DefaultPairForRegion(a.diffRegion)
		if a.diffToVer == "" {
			a.diffToVer = defTo
		}
	}
	a.diffFromVer = patch
	a.diffPage = 1
	a.snapMgrOpen = false
	a.refreshDiff()
}

func (a *App) openMap(marker MapMarkerData) {
	a.langPopoverOpen = false
	a.statusPopoverOpen = false
	a.globalSearchOpen = false
	cp := marker
	a.mapMarker = &cp
	a.mapOpen = true
}

func (a *App) openExportModal() {
	a.langPopoverOpen = false
	a.statusPopoverOpen = false
	a.globalSearchOpen = false
	a.exportError = ""
	a.exportOpen = true
}

// SetUpdaterClient 允许注入自定义更新客户端（供测试替换 GitHub 端点）。
func (a *App) SetUpdaterClient(c *updater.Client) {
	a.updaterClient = c
}

// SetChangelogFS 注入包含三语更新日志的只读文件系统（通常为 main 包内嵌的 embed.FS）。
func (a *App) SetChangelogFS(fsys fs.FS) {
	a.changelogFS = fsys
	a.changelogErr = ""
	clear(a.changelogDocs)
}

func (a *App) dispatchUI(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	a.win.Update(fn)
}

// StartStartupUpdateCheck 在应用启动时于后台静默检查 GitHub Release 更新；
// 若存在新版则静默下载、校验并原子替换文件，完成后仅在左下角提示用户重启；任意失败均静默回滚，不打扰用户。
func (a *App) StartStartupUpdateCheck() {
	a.startupUpdateOnce.Do(func() {
		client := a.updaterClient
		if client == nil {
			return
		}
		run := func() {
			updater.CleanupLeftovers("")
			if ok, _ := client.CanSelfUpdate(); !ok {
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			res, err := client.Check(ctx)
			cancel()
			if err != nil || !res.HasUpdate || res.Release == nil {
				return
			}

			var seq uint64
			claimed := true
			if a.win != nil {
				ch := make(chan bool, 1)
				a.win.Update(func() {
					if a.updateState == updateReady || a.updateManualActive || a.updateState == updateChecking || a.updateState == updateDownloading {
						ch <- false
						return
					}
					a.updateSeq++
					seq = a.updateSeq
					a.updateSilentRunning = true
					a.updateRelease = res.Release
					a.updatePhase = updater.PhaseDownloading
					a.updateDownBytes = 0
					a.updateDownTotal = res.Release.AssetSize
					ch <- true
				})
				claimed = <-ch
			} else {
				if a.updateState == updateReady || a.updateManualActive || a.updateState == updateChecking || a.updateState == updateDownloading {
					claimed = false
				} else {
					a.updateSeq++
					seq = a.updateSeq
					a.updateSilentRunning = true
					a.updateRelease = res.Release
					a.updatePhase = updater.PhaseDownloading
					a.updateDownBytes = 0
					a.updateDownTotal = res.Release.AssetSize
				}
			}
			if !claimed {
				return
			}

			dlCtx, dlCancel := context.WithTimeout(context.Background(), 10*time.Minute)
			a.dispatchUI(func() {
				if a.updateSeq == seq {
					a.updateCancel = dlCancel
				}
			})
			defer dlCancel()

			installErr := client.DownloadAndInstall(dlCtx, *res.Release, func(p updater.Progress) {
				cp := p
				a.dispatchUI(func() {
					if a.updateSeq != seq {
						return
					}
					a.updatePhase = cp.Phase
					a.updateDownBytes = cp.Downloaded
					a.updateDownTotal = cp.Total
				})
			})

			a.dispatchUI(func() {
				if a.updateSeq != seq {
					return
				}
				a.updateSilentRunning = false
				a.updateCancel = nil
				if installErr != nil {
					// 若用户中途主动打开了检查更新窗口并正在查看进度，向用户报告失败；否则彻底静默回滚。
					if a.updateManualActive && a.updateModalOpen {
						a.updateState = updateError
						a.updateErrorMsg = installErr.Error()
					} else {
						a.updateState = updateIdle
						a.updateRelease = nil
						a.updateDownBytes = 0
						a.updateDownTotal = 0
					}
					return
				}
				a.updateState = updateReady
				a.updateReadyVer = res.Release.Version
				a.updateRelease = res.Release
				a.updateErrorMsg = ""
			})
		}

		if a.win == nil {
			run()
		} else {
			go run()
		}
	})
}

func (a *App) openManualUpdateCheck() {
	a.langPopoverOpen = false
	a.statusPopoverOpen = false
	a.globalSearchOpen = false
	a.updateModalOpen = true
	a.updateManualActive = true

	if a.updateState == updateReady {
		return
	}
	if a.updateSilentRunning {
		a.updateState = updateDownloading
		return
	}
	if a.updateState == updateChecking || a.updateState == updateDownloading {
		return
	}
	a.startManualUpdateCheck()
}

func (a *App) closeUpdateModal() {
	if a.updateState == updateDownloading {
		return
	}
	a.updateModalOpen = false
	a.updateManualActive = false
	if a.updateState == updateChecking {
		if a.updateCancel != nil {
			a.updateCancel()
			a.updateCancel = nil
		}
		a.updateSeq++
		a.updateState = updateIdle
	}
}

func (a *App) startManualUpdateCheck() {
	client := a.updaterClient
	if client == nil {
		return
	}
	if a.updateCancel != nil {
		a.updateCancel()
		a.updateCancel = nil
	}

	a.updateSeq++
	seq := a.updateSeq
	a.updateState = updateChecking
	a.updateErrorMsg = ""
	a.updateDownBytes = 0
	a.updateDownTotal = 0

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	a.updateCancel = cancel

	apply := func(res updater.CheckResult, err error) {
		cancel()
		if a.updateSeq != seq {
			return
		}
		a.updateCancel = nil
		if err != nil {
			a.updateState = updateError
			a.updateErrorMsg = err.Error()
			return
		}
		a.updateRelease = res.Release
		if res.HasUpdate && res.Release != nil {
			a.updateState = updateAvailable
		} else {
			a.updateState = updateUpToDate
		}
	}

	if a.win == nil {
		res, err := client.Check(ctx)
		apply(res, err)
		return
	}
	go func() {
		res, err := client.Check(ctx)
		a.win.Update(func() {
			apply(res, err)
		})
	}()
}

func (a *App) startManualUpdateInstall() {
	client := a.updaterClient
	rel := a.updateRelease
	if client == nil || rel == nil {
		return
	}
	if a.updateCancel != nil {
		a.updateCancel()
		a.updateCancel = nil
	}

	a.updateSeq++
	seq := a.updateSeq
	a.updateState = updateDownloading
	a.updatePhase = updater.PhaseDownloading
	a.updateDownBytes = 0
	a.updateDownTotal = rel.AssetSize
	a.updateErrorMsg = ""

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	a.updateCancel = cancel
	relCopy := *rel

	apply := func(err error) {
		cancel()
		if a.updateSeq != seq {
			return
		}
		a.updateCancel = nil
		if err != nil {
			a.updateState = updateError
			a.updateErrorMsg = err.Error()
			return
		}
		a.updateState = updateReady
		a.updateReadyVer = relCopy.Version
		a.updateErrorMsg = ""
	}

	if a.win == nil {
		err := client.DownloadAndInstall(ctx, relCopy, func(p updater.Progress) {
			if a.updateSeq != seq {
				return
			}
			a.updatePhase = p.Phase
			a.updateDownBytes = p.Downloaded
			a.updateDownTotal = p.Total
		})
		apply(err)
		return
	}
	go func() {
		err := client.DownloadAndInstall(ctx, relCopy, func(p updater.Progress) {
			cp := p
			a.win.Update(func() {
				if a.updateSeq != seq {
					return
				}
				a.updatePhase = cp.Phase
				a.updateDownBytes = cp.Downloaded
				a.updateDownTotal = cp.Total
			})
		})
		a.win.Update(func() {
			apply(err)
		})
	}()
}

func (a *App) releasePageURL() string {
	if a.updateRelease != nil && a.updateRelease.HTMLURL != "" {
		return a.updateRelease.HTMLURL
	}
	if a.updaterClient != nil {
		return a.updaterClient.ReleasesPageURL()
	}
	return "https://github.com/" + updater.DefaultRepo + "/releases"
}

func (a *App) restartForUpdate() {
	a.relaunchRequested = true
	a.Close()
	if a.win != nil {
		mygo.App.Relaunch()
	}
}

// pickDirectoryAsync 弹出原生系统文件夹选择框并将结果回填至主线程。
func (a *App) pickDirectoryAsync(title, defaultPath string, onPicked func(dir string)) {
	if a.win == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      a.win,
			Title:       title,
			DefaultPath: defaultPath,
			Directory:   true,
		})
		if err != nil || len(paths) == 0 || strings.TrimSpace(paths[0]) == "" {
			return
		}
		picked := paths[0]
		a.win.Update(func() {
			onPicked(picked)
		})
	}()
}

func parseOptionalPosInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}
