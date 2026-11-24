package uiapp

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/exporter"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
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
	sidebarHidden     bool
	sidebarW          float32
	listW             float32
	tabHistory        []activeTab
	tabAhead          []activeTab
	focusSearch       bool
	statusPopoverOpen bool

	// 配置与引导/设置表单草稿
	cfg             config.AppConfig
	setupMode       setupServerMode
	draftCNPath     string
	draftGlobalPath string
	draftError      string
	draftNotice     string
	loadError       string
	pathDiagCache   map[string]cachedPathDiag

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
	itemQuery       gamedata.ItemSearchQuery
	itemMinIlvlStr  string
	itemMaxIlvlStr  string
	itemPageStr     string
	itemRes         gamedata.ItemSearchResponse
	itemAdvFilter   bool // 是否展开高级筛选抽屉（分类 / 品级 / 仅可交易）
	selectedItemID  int
	selectedDetail  *gamedata.ItemDetail
	showHQ          bool
	activeRecipeIdx int
	copiedItemID    int
	copiedUntil     time.Time
	history         []historyEntry
	detailScroll    ui.ScrollState
	itemListScroll  ui.ScrollState

	// 任务视图状态
	questQuery        gamedata.QuestSearchQuery
	questPageStr      string
	questRes          gamedata.QuestSearchResponse
	questListScroll   ui.ScrollState
	questDetailScroll ui.ScrollState
	selectedQuestID   int
	selectedQuest     *gamedata.QuestEntry
	selectedQuestText *gamedata.QuestTextData
	questTextLoading  bool
	questTextSeq      uint64
	questSummaryOpen  bool
	questDialogueOpen bool

	// 理符视图状态
	leveQuery        gamedata.LeveSearchQuery
	levePageStr      string
	leveRes          gamedata.LeveSearchResponse
	leveListScroll   ui.ScrollState
	leveDetailScroll ui.ScrollState
	selectedLeveID   int
	selectedLeve     *gamedata.LeveEntry

	// 成就与称号视图状态
	achQuery   gamedata.AchievementSearchQuery
	achPageStr string
	achRes     gamedata.AchievementSearchResponse
	achScroll  ui.ScrollState

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
}

// NewApp 创建客户端应用实例并读取本地持久化配置。
func NewApp(appVersion, dataDir string, schemasFS fs.FS) *App {
	ensureDefaultFonts()
	cfg, _ := config.LoadAppConfig(dataDir)
	a := &App{
		appVersion:      appVersion,
		dataDir:         dataDir,
		schemasFS:       schemasFS,
		sidebarW:        sidebarDefWidth,
		listW:           itemListDefWidth,
		cfg:             cfg,
		setupMode:       inferSetupMode(cfg.CNPath, cfg.GlobalPath),
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
		achPageStr:  "1",
		diffFilter:  "all",
		diffEntity:  "all",
		diffPage:    1,
		diffPageStr: "1",
		exportLangs: map[string]bool{
			"chs": true,
			"ja":  true,
			"en":  true,
			"de":  true,
			"fr":  true,
		},
		exportDirDraft: cfg.ExportOutputDir,
	}

	// 判断是否已有有效配置：首次启动或路径无效时停留在路径引导页
	if resolved, err := config.ValidateAndResolve(cfg); err != nil {
		a.mode = modeSetup
		if !errors.Is(err, config.ErrNoClientConfigured) {
			a.draftError = friendlyConfigError(err, cfg)
		}
	} else {
		a.mode = modeLoading
		a.prepareLoadingState(resolved)
	}
	return a
}

func inferSetupMode(cnPath, globalPath string) setupServerMode {
	hasCN := strings.TrimSpace(cnPath) != ""
	hasGlobal := strings.TrimSpace(globalPath) != ""
	switch {
	case hasCN && hasGlobal:
		return setupModeBoth
	case hasGlobal:
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
	a.loadingStatusTxt = "正在以只读模式读取本地游戏数据表..."
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
	_ = config.SaveAppConfig(a.dataDir, a.cfg)
}

// AttachWindow 绑定主窗口实例与窗口材质状态，若已有有效路径配置则立即启动异步加载。
func (a *App) AttachWindow(win *mygo.Window, vibrancy bool) {
	a.win = win
	a.vibrancy = vibrancy
	if a.mode == modeLoading {
		a.startAsyncLoad(a.cfg)
	}
}

// Close 释放应用持有的全部本地文件句柄与后台任务。
func (a *App) Close() {
	if a.exporterSvc != nil {
		a.exporterSvc.Cancel()
	}
	if a.service != nil {
		_ = a.service.Close()
		a.service = nil
	}
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
	if a.vibrancy && !a.mapOpen && !a.exportOpen {
		if t.dark {
			return ui.Transparent
		}
		return t.window.Alpha(materialTint)
	}
	return t.window
}

// View 为 MyGo 原生窗口构建每帧用户界面。
func (a *App) View(c *ui.Context) {
	t := applyAppTheme(c, a.cfg.Theme)
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
		if c.Shortcut(ui.Cmd, ui.KeyBracketLeft) {
			a.goBack()
		}
		if c.Shortcut(ui.Cmd, ui.KeyBracketRight) {
			a.goForward()
		}
	}

	rootBg := a.windowBackground(t)
	root := ui.Column(c).Fill().Background(rootBg).Font(fontStack).FontSize(t.text.sm).TextColor(t.foreground)
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

	// 全局模态弹窗层（地图预览、CSV 导出）
	a.renderMapModal(c)
	a.renderExportModal(c)

	// 末尾同步 Root 背景，防止 SetTheme 重置透明材质底色
	c.Root().Background(rootBg)
}

// SaveAndReloadPaths 校验并持久化国服/国际服路径配置，然后异步重载游戏数据仓库。
func (a *App) SaveAndReloadPaths(cnPath, globalPath string) bool {
	nextCfg := a.cfg
	nextCfg.CNPath = normalizeUserPathInput(cnPath)
	nextCfg.GlobalPath = normalizeUserPathInput(globalPath)

	resolved, err := config.ValidateAndResolve(nextCfg)
	if err != nil {
		a.draftError = friendlyConfigError(err, nextCfg)
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

	a.cfg = nextCfg
	a.setupMode = inferSetupMode(nextCfg.CNPath, nextCfg.GlobalPath)
	a.draftCNPath = nextCfg.CNPath
	a.draftGlobalPath = nextCfg.GlobalPath
	a.draftError = ""
	a.draftNotice = ""
	a.loadError = ""
	a.invalidatePathDiag()
	a.mode = modeLoading
	a.prepareLoadingState(resolved)

	a.startAsyncLoad(nextCfg)
	return true
}

func (a *App) startAsyncLoad(cfg config.AppConfig) {
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		a.mode = modeSetup
		a.setupMode = inferSetupMode(cfg.CNPath, cfg.GlobalPath)
		a.draftError = friendlyConfigError(err, cfg)
		return
	}

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

	runLoad := func() (*gamedata.Service, *backup.Service, *exporter.Service, gamedata.SystemStatus, backup.VersionStatusResponse, error) {
		store, err := gamedata.LoadGameDataStoreWithProgress(resolved, a.schemasFS, func(p gamedata.LoadProgress) {
			st := loadStageSheets
			if p.Stage == gamedata.LoadStageIndices {
				st = loadStageIndices
			}
			dispatchProgress(st, p.Done, p.Total, p.Message)
		})
		if err != nil {
			return nil, nil, nil, gamedata.SystemStatus{}, backup.VersionStatusResponse{}, err
		}

		dispatchProgress(loadStageSnapshot, 0, 0, "正在校验本地版本快照与对比基线...")
		svc := gamedata.NewService(store)
		status := svc.GetSystemStatus()

		backupDir := filepath.Join(a.dataDir, "backups")
		bSvc := backup.NewService(store, backupDir)
		vStatus, bErr := bSvc.InitializeOnStartup()
		if bErr != nil {
			vStatus = bSvc.GetVersionStatus()
		}

		sm := exporter.NewSchemaManager(a.schemasFS)
		expSvc := exporter.NewService(store.CNReader, store.GlobalReader, sm)
		return svc, bSvc, expSvc, status, vStatus, nil
	}

	applyResult := func(svc *gamedata.Service, bSvc *backup.Service, expSvc *exporter.Service, status gamedata.SystemStatus, vStatus backup.VersionStatusResponse, err error) {
		if a.loadSeq != seq {
			if svc != nil {
				_ = svc.Close()
			}
			return
		}
		if err != nil {
			a.loadError = err.Error()
			a.draftError = fmt.Sprintf("读取游戏数据时出现问题：%v", err)
			a.mode = modeSetup
			return
		}
		if a.service != nil {
			_ = a.service.Close()
		}
		a.bitmaps.Clear()
		a.service = svc
		a.backupSvc = bSvc
		a.exporterSvc = expSvc
		a.status = &status
		a.versionStatus = &vStatus
		a.dismissUpdateBanner = false
		a.exportTotalShts = expSvc.TotalSheetCount()
		a.syncAvailableExportLangs()

		// 重置并执行初始检索
		a.itemQuery.Page = 1
		a.itemPageStr = "1"
		a.refreshItemList()
		a.refreshQuests()
		if len(a.questRes.Items) > 0 {
			a.selectQuest(a.questRes.Items[0].ID)
		} else {
			a.selectedQuestID = 0
			a.selectedQuest = nil
		}
		a.refreshLeves()
		if len(a.leveRes.Items) > 0 {
			a.selectLeve(a.leveRes.Items[0].ID)
		} else {
			a.selectedLeveID = 0
			a.selectedLeve = nil
		}
		a.refreshAchievements()

		// 默认选中典型样本（若存在 #44099 图拉尔菠萝蛋糕则选中它，否则选当前列表首项）
		if a.service.GetItemDetail(44099) != nil {
			a.selectItem(44099)
		} else if len(a.itemRes.Items) > 0 {
			a.selectItem(a.itemRes.Items[0].ID)
		} else {
			a.selectedItemID = 0
			a.selectedDetail = nil
		}

		// 初始化版本对比下拉默认值
		a.diffToVer = vStatus.CurrentVersion
		a.diffFromVer = vStatus.PreviousVersion
		a.diffPage = 1
		a.diffPageStr = "1"
		a.refreshDiff()

		a.mode = modeReady
		if vStatus.HasVersionUpdate {
			a.tab = tabDiff
		} else {
			a.tab = tabWorkbench
		}
	}

	if a.win == nil {
		svc, bSvc, expSvc, st, vst, err := runLoad()
		applyResult(svc, bSvc, expSvc, st, vst, err)
		return
	}

	go func() {
		svc, bSvc, expSvc, st, vst, err := runLoad()
		a.win.Update(func() {
			applyResult(svc, bSvc, expSvc, st, vst, err)
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

func (a *App) inspectItemFromOtherTab(itemID int) {
	a.selectItem(itemID)
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

func (a *App) inspectQuestFromOtherTab(questID int) {
	a.selectQuest(questID)
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

func (a *App) inspectLeveFromOtherTab(leveID int) {
	a.selectLeve(leveID)
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
		a.diffError = fmt.Sprintf("计算版本差异失败：%v", err)
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
			a.diffError = fmt.Sprintf("刷新版本快照失败：%v", err)
			return
		}
		a.versionStatus = &st
		a.diffNotice = fmt.Sprintf("已更新当前版本快照（v%s）", st.CurrentVersion)
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
	cp := marker
	a.mapMarker = &cp
	a.mapOpen = true
}

func (a *App) openExportModal() {
	a.exportError = ""
	a.exportOpen = true
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
