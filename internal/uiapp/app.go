package uiapp

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"runtime"
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
	loadStageSnapshot
)

type activeTab uint8

const (
	tabWorkbench activeTab = iota
	tabQuests
	tabLeves
	tabAchievements
	tabDiff
	tabChangelog
	tabSettings
)

type historyEntry struct {
	id     int
	nameCn string
	iconID int
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
	service     *gamedata.Service
	backupSvc   *backup.Service
	exporterSvc *exporter.Service
	bitmaps     *BitmapCache

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
	history            []historyEntry
	detailScroll       ui.ScrollState
	itemListScroll     ui.ScrollState
	scrollItemListToID int

	// 任务视图状态
	questQuery          gamedata.QuestSearchQuery
	questPageStr        string
	questRes            gamedata.QuestSearchResponse
	questListScroll     ui.ScrollState
	questDetailScroll   ui.ScrollState
	scrollQuestListToID int
	selectedQuestID     int
	selectedQuest       *gamedata.QuestEntry
	selectedQuestText   *gamedata.QuestTextData
	questTextLoading    bool
	questTextSeq        uint64
	questSummaryOpen    bool
	questDialogueOpen   bool

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

	// 版本 Diff 视图状态
	diffFromVer string
	diffToVer   string
	diffFilter  string
	diffEntity  string
	diffSearch  string
	diffPage    int
	diffPageStr string
	diffRes     *backup.VersionDiffResponse
	diffBusy    bool
	diffNotice  string
	diffError   string
	diffScroll  ui.ScrollState

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
		achPageStr:         "1",
		diffFilter:         "all",
		diffEntity:         "all",
		diffPage:           1,
		diffPageStr:        "1",
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
		exportDirDraft: cfg.ExportOutputDir,
		langCache:      make(map[i18n.Lang]*langSessionCache),
		updaterClient: updater.NewClient(updater.Options{
			CurrentVersion: appVersion,
		}),
	}

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
	if next == tabSettings {
		a.draftCNPath = a.cfg.CNPath
		a.draftGlobalPath = a.cfg.GlobalPath
		a.draftError = ""
		a.draftNotice = ""
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
	if a.vibrancy && !a.mapOpen && !a.exportOpen && !a.updateModalOpen {
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
		// 每秒驱动一次重绘以刷新实时艾欧泽亚时钟及限时采集倒计时
		c.After(time.Second)

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

	// 全局模态弹窗层（地图预览、CSV 导出、应用检查更新）
	a.renderMapModal(c)
	a.renderExportModal(c)
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

func (a *App) backupDirForStore(store *gamedata.Store) string {
	return a.backupDirForStoreAndLang(store, a.Language())
}

func (a *App) backupDirForStoreAndLang(store *gamedata.Store, lang i18n.Lang) string {
	if store != nil {
		if store.PrimaryLang == sqpack.LangJapanese {
			return filepath.Join(a.dataDir, "backups", "ja")
		}
		if store.PrimaryLang == sqpack.LangEnglish && i18n.NormalizeLang(string(lang)) == i18n.LangEN {
			return filepath.Join(a.dataDir, "backups", "en")
		}
	}
	return filepath.Join(a.dataDir, "backups")
}

// resetLangCacheAndReaders 释放已缓存的多语言数据仓库与共享只读 SqPack 句柄，并清空贴图缓存。
// 仅在客户端路径变更或应用退出时调用，常规切换语言不调用以保留图标与会话缓存。
func (a *App) resetLangCacheAndReaders() {
	if a.exporterSvc != nil {
		a.exporterSvc.Cancel()
	}
	closedSvc := make(map[*gamedata.Service]bool, len(a.langCache)+1)
	for _, cached := range a.langCache {
		if cached == nil {
			continue
		}
		if cached.exporterSvc != nil {
			cached.exporterSvc.Cancel()
		}
		if cached.service != nil && !closedSvc[cached.service] {
			closedSvc[cached.service] = true
			_ = cached.service.Close()
		}
	}
	if a.service != nil && !closedSvc[a.service] {
		_ = a.service.Close()
	}
	a.langCache = make(map[i18n.Lang]*langSessionCache)
	a.langCachePathSig = ""
	a.service = nil
	a.backupSvc = nil
	a.exporterSvc = nil
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

	backupDir := a.backupDirForStoreAndLang(store, loadLang)
	bSvc := backup.NewService(store, backupDir)
	bSvc.SetLang(loadLang)
	vStatus, bErr := bSvc.InitializeOnStartup()
	if bErr != nil {
		vStatus = bSvc.GetVersionStatus()
	}

	sm := exporter.NewSchemaManager(a.schemasFS)
	expSvc := exporter.NewService(store.CNReader, store.GlobalReader, sm)

	state := &langSessionCache{
		service:         svc,
		backupSvc:       bSvc,
		exporterSvc:     expSvc,
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

	st := state.status
	vst := state.versionStatus
	a.service = state.service
	a.backupSvc = state.backupSvc
	a.exporterSvc = state.exporterSvc
	a.status = &st
	a.versionStatus = &vst
	a.dismissUpdateBanner = false
	a.exportTotalShts = state.exportTotalShts
	if a.backupSvc != nil {
		a.backupSvc.SetLang(a.Language())
	}
	a.syncAvailableExportLangs()

	if !preserveSelection {
		a.itemQuery.Page = 1
		a.itemPageStr = "1"
	}
	a.refreshItemList()
	a.refreshQuests()
	if preserveSelection && prevQuestID > 0 && a.service.GetQuestDetail(prevQuestID) != nil {
		a.jumpToQuest(prevQuestID)
	} else if len(a.questRes.Items) > 0 {
		a.selectQuest(a.questRes.Items[0].ID)
	} else {
		a.selectedQuestID = 0
		a.selectedQuest = nil
	}

	a.refreshLeves()
	if preserveSelection && prevLeveID > 0 && a.service.GetLeveDetail(prevLeveID) != nil {
		a.jumpToLeve(prevLeveID)
	} else if len(a.leveRes.Items) > 0 {
		a.selectLeve(a.leveRes.Items[0].ID)
	} else {
		a.selectedLeveID = 0
		a.selectedLeve = nil
	}
	a.refreshAchievements()

	if preserveSelection && prevItemID > 0 && a.service.GetItemDetail(prevItemID) != nil {
		a.jumpToItem(prevItemID)
		a.detailScroll.Y = prevDetailScroll
	} else if a.service.GetItemDetail(44099) != nil {
		a.selectItem(44099)
	} else if len(a.itemRes.Items) > 0 {
		a.selectItem(a.itemRes.Items[0].ID)
	} else {
		a.selectedItemID = 0
		a.selectedDetail = nil
	}

	a.diffToVer = vst.CurrentVersion
	a.diffFromVer = vst.PreviousVersion
	a.diffPage = 1
	a.diffPageStr = "1"
	a.refreshDiff()
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
		dispatchProgress(loadStageSnapshot, 0, 0, i18n.T(loadLang, "loading.checking_snapshot"))
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
		} else if state.versionStatus.HasVersionUpdate {
			a.tab = tabDiff
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

	// 更新最近浏览足迹（最多保留 10 条）
	next := make([]historyEntry, 0, 10)
	next = append(next, historyEntry{id: detail.ID, nameCn: detail.NameCn, iconID: detail.IconID})
	for _, h := range a.history {
		if h.id != detail.ID && len(next) < 10 {
			next = append(next, h)
		}
	}
	a.history = next
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
	if a.service == nil || itemID <= 0 {
		return
	}
	a.selectItem(itemID)
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

func (a *App) selectQuest(questID int) {
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

	a.questTextSeq++
	seq := a.questTextSeq
	svc := a.service

	if a.win == nil {
		a.questTextLoading = false
		a.selectedQuestText = svc.GetQuestText(questID)
		return
	}

	a.questTextLoading = true
	go func() {
		txt := svc.GetQuestText(questID)
		a.win.Update(func() {
			if a.questTextSeq != seq || a.selectedQuestID != questID {
				return
			}
			a.questTextLoading = false
			a.selectedQuestText = txt
		})
	}()
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
	if a.service == nil || questID <= 0 {
		return
	}
	a.selectQuest(questID)
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
	if a.service == nil || leveID <= 0 {
		return
	}
	a.selectLeve(leveID)
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
	case tabDiff:
		a.diffSearch = q
		a.diffPage = 1
		a.refreshDiff()
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
	}
}

func (a *App) refreshDiff() {
	if a.backupSvc == nil {
		return
	}
	res, err := a.backupSvc.CompareVersions(backup.VersionDiffQuery{
		From:       a.diffFromVer,
		To:         a.diffToVer,
		Filter:     a.diffFilter,
		EntityType: a.diffEntity,
		Q:          a.diffSearch,
		Page:       a.diffPage,
		PageSize:   25,
	})
	if err != nil {
		a.diffError = a.tf("diff.err_compare", "err", err.Error())
		return
	}
	a.diffError = ""
	a.diffRes = &res
	a.diffPage = res.Page
	a.diffPageStr = strconv.Itoa(res.Page)
	a.diffScroll.Y = 0
}

// refreshVersionBackup 手动刷新当前客户端版本快照；在有窗口时异步执行，避免阻塞界面并明确提示成功或失败。
func (a *App) refreshVersionBackup() {
	if a.backupSvc == nil || a.diffBusy {
		return
	}
	a.diffBusy = true
	a.diffError = ""
	a.diffNotice = ""
	svc := a.backupSvc

	apply := func(st backup.VersionStatusResponse, err error) {
		a.diffBusy = false
		if err != nil {
			a.diffError = a.tf("diff.err_refresh", "err", err.Error())
			return
		}
		a.versionStatus = &st
		a.diffNotice = a.tf("diff.notice_refreshed", "version", st.CurrentVersion)
		a.refreshDiff()
	}

	if a.win == nil {
		st, err := svc.CreateOrRefreshBackup()
		apply(st, err)
		return
	}
	go func() {
		st, err := svc.CreateOrRefreshBackup()
		a.win.Update(func() {
			apply(st, err)
		})
	}()
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
