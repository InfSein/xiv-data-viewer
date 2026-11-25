package uiapp

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"image"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/InfSein/xiv-data-viewer/internal/updater"
	"github.com/egoist/mygo/ui"
)

// newZHApp 创建一个预置中文界面语言的测试应用实例，确保现有中文断言测试在任意系统语言环境下稳定运行。
func newZHApp(t *testing.T, dataDir string) *App {
	t.Helper()
	if err := config.SaveAppConfig(dataDir, config.AppConfig{Language: string(i18n.LangZH)}); err != nil {
		t.Fatalf("写入测试中文配置失败: %v", err)
	}
	return NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
}

func TestFirstRunSetupAndSettingsFlow(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	// 1. 首次启动未配置路径时应停留在引导设置页面
	if app.mode != modeSetup {
		t.Fatalf("期望首次启动进入 modeSetup，实际: %v", app.mode)
	}

	tt := ui.NewTester(app.View, 1366, 768)
	if !tt.HasText("选择 FF14 游戏目录") || !tt.HasText("仅国服") {
		t.Fatalf("引导页标题或区服引导未渲染: %q", tt.Texts())
	}

	// 2. 填写无效路径应报错并留在引导页
	if app.SaveAndReloadPaths(filepath.Join(dataDir, "not-exists"), "") {
		t.Fatalf("无效路径不应保存成功")
	}
	tt.Frame()
	if app.mode != modeSetup || app.draftError == "" {
		t.Fatalf("期望停留在引导页并显示错误信息，mode=%v err=%q", app.mode, app.draftError)
	}

	// 3. 若本机 .env 存在真实客户端，进一步端到端验证精简后的主工作台及各视图切换
	envCfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	if _, err := config.ValidateAndResolve(envCfg); err != nil {
		t.Logf("本机未配置真实客户端，跳过主工作台全流程测试: %v", err)
		return
	}

	firstPath := envCfg.CNPath
	secondPath := ""
	if firstPath == "" {
		secondPath = envCfg.GlobalPath
	}
	if !app.SaveAndReloadPaths(firstPath, secondPath) {
		t.Fatalf("保存有效单服路径失败: %s", app.draftError)
	}
	tt.Frame()

	if app.mode != modeReady {
		t.Fatalf("期望加载完成后进入 modeReady，实际: %v (err=%q)", app.mode, app.draftError)
	}

	// 验证左侧菜单精简结构：【数据】成就、物品；【对比】版本对比；【设置】
	for _, want := range []string{"数据", "成就", "物品", "对比", "版本对比", "设置"} {
		if !tt.HasText(want) {
			t.Fatalf("左侧精简导航缺少预期文案 %q", want)
		}
	}

	// 点击「物品」进入物品工作台，验证「制作物品」等筛选位于物品界面内
	if err := tt.Click("物品"); err != nil {
		t.Fatalf("点击物品导航失败: %v", err)
	}
	if app.tab != tabWorkbench || !tt.HasText("制作物品") {
		t.Fatalf("期望位于物品视图并包含内置「制作物品」筛选，tab=%v", app.tab)
	}

	// 点击内置「制作物品」后，自动展示能工巧匠职业筛选条
	if err := tt.Click("制作物品"); err != nil {
		t.Fatalf("点击物品页内「制作物品」失败: %v", err)
	}
	if app.itemQuery.Filter != "craftable" || !tt.HasText("能工巧匠职业") {
		t.Fatalf("期望进入制作物品过滤并显示职业栏，filter=%q", app.itemQuery.Filter)
	}

	// 切换至「成就」
	if err := tt.Click("成就"); err != nil {
		t.Fatalf("点击成就导航失败: %v", err)
	}
	if app.tab != tabAchievements || !tt.HasText("成就与称号") {
		t.Fatalf("期望切换到成就页面，tab=%v", app.tab)
	}

	// 切换至「版本对比」
	if err := tt.Click("版本对比"); err != nil {
		t.Fatalf("点击版本对比导航失败: %v", err)
	}
	if app.tab != tabDiff || !tt.HasText("版本变更对比") {
		t.Fatalf("期望切换到版本对比页面，tab=%v", app.tab)
	}

	// 切换至「设置」，测试主题切换与内置 CSV 导出入口
	if err := tt.Click("设置"); err != nil {
		t.Fatalf("点击设置导航失败: %v", err)
	}
	if app.tab != tabSettings || !tt.HasText("界面主题") || !tt.HasText("FF14 游戏客户端") {
		t.Fatalf("期望切换到设置页面，tab=%v", app.tab)
	}

	if err := tt.Click("深色"); err != nil {
		t.Fatalf("切换深色外观失败: %v", err)
	}
	if app.cfg.Theme != "dark" {
		t.Fatalf("期望主题持久化为 dark，实际: %q", app.cfg.Theme)
	}
	if err := tt.Click("浅色"); err != nil {
		t.Fatalf("切换浅色外观失败: %v", err)
	}

	// 从设置页打开 CSV 导出弹窗并关闭
	if err := tt.Click("导出游戏 CSV"); err != nil {
		t.Fatalf("点击导出游戏 CSV 失败: %v", err)
	}
	if !app.exportOpen || !tt.HasText("游戏全量 CSV 解包与本地导出") {
		t.Fatalf("期望打开 CSV 导出弹窗，exportOpen=%v", app.exportOpen)
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatalf("关闭导出弹窗失败: %v", err)
	}
	if app.exportOpen {
		t.Fatalf("期望导出弹窗已关闭")
	}
}

func TestMinimumViewportAndFilterReset(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	// 验证最小窗口尺寸 1024×640 渲染稳定（含引导页与地图/导出弹窗边界）
	tt := ui.NewTester(app.View, 1024, 640)
	if !tt.HasText("选择 FF14 游戏目录") || !tt.HasText("读取并进入工作台") {
		t.Fatalf("最小视口下引导页主配置区未渲染: %q", tt.Texts())
	}

	// 验证地图弹窗边缘坐标钳制在 1024×640 下正常渲染
	app.openMap(MapMarkerData{
		Title:      "边缘测试标记",
		Subtitle:   "测试区域",
		RegionName: "黑衣森林",
		ZoneName:   "黑衣森林南部林区",
		PlaceName:  "恬静路营地",
		MapPath:    "",
		CoordX:     41.2,
		CoordY:     40.8,
		NormX:      0.98,
		NormY:      0.97,
	})
	tt.Frame()
	if !tt.HasText("41.2, 40.8") {
		t.Fatalf("地图边缘标点坐标未正常渲染")
	}
	app.mapOpen = false
	tt.Frame()

	// 验证筛选状态检测与一键重置逻辑
	if app.hasAnyItemFilter() {
		t.Fatalf("初始状态下不应检测到激活筛选")
	}
	app.itemQuery.Filter = "craftable"
	app.itemQuery.TradableOnly = true
	if !app.hasAnyItemFilter() || app.countActiveAdvFilters() != 1 {
		t.Fatalf("期望检测到筛选已激活")
	}
	app.resetItemFilters()
	if app.hasAnyItemFilter() || app.itemQuery.Filter != "all" {
		t.Fatalf("期望重置后清空全部筛选状态")
	}
}

func TestQuestLeveAndCollectablesViews(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	rowCake := make(sqpack.Row, 95)
	rowCake[9] = "收藏用图拉尔菠萝蛋糕"
	rowCake[8] = "用于工票收藏品交纳的点心。"
	rowCake[20] = 99
	rowCake[26] = 10

	rowScrip := make(sqpack.Row, 95)
	rowScrip[9] = "巧手紫票"
	rowScrip[20] = 4000

	questEntry := gamedata.QuestEntry{
		ID:            65536,
		InternalID:    "Msq001",
		NameCn:        "\ue0be 启程之日",
		NameEn:        "Coming to Gridania",
		IconID:        gamedata.QuestIconMSQ,
		HasLevelSync:  true,
		SectionID:     0,
		SectionName:   "主线任务",
		GenreName:     "第七星历主线",
		ExpansionID:   0,
		ExpansionName: "重生之境",
		Level:         15,
		IssuerName:    "缪恩",
		IsMSQ:         true,
		GilReward:     600,
		ExpReward:     2400,
		RewardItems: []gamedata.QuestRewardItem{{
			ItemID:     44099,
			ItemName:   "收藏用图拉尔菠萝蛋糕",
			Count:      2,
			GroupLabel: "固定奖励",
		}},
	}

	leveEntry := gamedata.LeveEntry{
		ID:                 201,
		NameCn:             "大量制作：甜点委托",
		LeveKind:           "craft",
		LeveKindLabel:      "制作理符",
		AssignmentTypeID:   5,
		AssignmentTypeName: "大量筹备",
		ClassJobLevel:      98,
		TownName:           "图莱尤拉",
		ExpReward:          850000,
		GilReward:          2400,
		CraftRepeats:       2,
		TurnInItems: []gamedata.LeveTurnInItem{{
			ItemID:   44099,
			ItemName: "收藏用图拉尔菠萝蛋糕",
			Count:    3,
		}},
		RewardGroups: []gamedata.LeveRewardGroup{{
			ProbabilityPercent: 50,
			Items: []gamedata.LeveRewardEntry{{
				ItemID:   33913,
				ItemName: "巧手紫票",
				Count:    10,
			}},
		}},
	}

	store := &gamedata.Store{
		PrimaryVersion: "2026.11.01",
		ItemsPrimary:   map[int]sqpack.Row{44099: rowCake, 33913: rowScrip},
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 33913, NameCn: "巧手紫票", CategoryName: "货币", ItemLevel: 1, IsCollectableReward: true},
			{ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", CategoryName: "食品", ItemLevel: 690, IsCollectableTurnIn: true},
		},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			33913: {ID: 33913, NameCn: "巧手紫票", CategoryName: "货币", ItemLevel: 1, IsCollectableReward: true},
			44099: {ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", CategoryName: "食品", ItemLevel: 690, IsCollectableTurnIn: true},
		},
		CollectableTurnInsByItem: map[int][]gamedata.CollectableTurnInSource{
			44099: {{
				SourceKind:  "shop",
				SourceLabel: "收藏品交易",
				ShopID:      1,
				ShopName:    "图莱尤拉收藏品交易",
				GroupName:   "Lv.100",
				LevelMin:    100,
				LevelMax:    100,
				Stars:       2,
				Tiers: []gamedata.CollectableTier{
					{Label: "低档", MinCollectability: 650, RewardItemID: 33913, RewardItemName: "巧手紫票", RewardAmount: 60},
					{Label: "中档", MinCollectability: 1000, RewardItemID: 33913, RewardItemName: "巧手紫票", RewardAmount: 100},
					{Label: "高档", MinCollectability: 1350, RewardItemID: 33913, RewardItemName: "巧手紫票", RewardAmount: 144},
				},
			}},
		},
		CollectableRewardsByItem: map[int][]gamedata.CollectableRewardSource{
			33913: {{
				ShopName:          "图莱尤拉收藏品交易",
				TurnInItemID:      44099,
				TurnInItemName:    "收藏用图拉尔菠萝蛋糕",
				MinCollectability: 650,
				RewardLow:         60,
				RewardMid:         100,
				RewardHigh:        144,
			}},
		},
		Quests:    []gamedata.QuestEntry{questEntry},
		QuestByID: map[int]gamedata.QuestEntry{65536: questEntry},
		QuestsByRewardItem: map[int][]gamedata.ItemQuestSource{
			44099: {{QuestID: 65536, NameCn: "启程之日", GenreName: "第七星历主线", Level: 15, Count: 2, GroupLabel: "固定奖励"}},
		},
		QuestSectionList: []gamedata.QuestSectionInfo{{ID: 0, Name: "主线任务", Count: 1}},
		Leves:            []gamedata.LeveEntry{leveEntry},
		LeveByID:         map[int]gamedata.LeveEntry{201: leveEntry},
		LevesByItem: map[int][]gamedata.ItemLeveSource{
			44099: {{LeveID: 201, NameCn: "大量制作：甜点委托", AssignmentTypeName: "大量筹备", Level: 98, Count: 9, IsTurnIn: true}},
			33913: {{LeveID: 201, NameCn: "大量制作：甜点委托", AssignmentTypeName: "大量筹备", Level: 98, Count: 10, ProbabilityPercent: 50, IsTurnIn: false}},
		},
		LeveAssignmentTypes: []gamedata.LeveAssignmentTypeInfo{{ID: 5, Name: "大量筹备", Kind: "craft", Count: 1}},
	}

	svc := gamedata.NewService(store)
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.mode = modeReady
	app.tab = tabWorkbench
	app.refreshItemList()
	app.selectItem(44099)
	app.refreshQuests()
	app.selectQuest(65536)
	app.refreshLeves()
	app.selectLeve(201)

	tt := ui.NewTester(app.View, 1366, 768)
	// 验证物品页内含收藏品过滤与详情联动，且左侧菜单仅含 物品/任务/理符/成就（不单独设立收藏品菜单）
	for _, want := range []string{"物品", "任务", "理符", "成就", "收藏品交纳", "收藏品交易交纳", "任务奖励", "理符交纳委托"} {
		if !tt.HasText(want) {
			t.Fatalf("工作台页面缺少预期文案 %q: %v", want, tt.Texts())
		}
	}

	// 验证切换到任务视图，以及任务概要和对话默认折叠（防剧透）与点击展开
	app.selectedQuestText = &gamedata.QuestTextData{
		QuestID:    65536,
		Journals:   []string{"缪恩正在等候新冒险者前来报到。"},
		Objectives: []string{"和魔女咖啡馆的缪恩对话"},
		Dialogues: []gamedata.QuestDialogueLine{{
			Kind:    "dialogue",
			Speaker: "缪恩",
			Text:    "欢迎来到格里达尼亚！",
		}},
	}
	if err := tt.Click("任务"); err != nil {
		t.Fatalf("点击任务导航失败: %v", err)
	}
	if app.tab != tabQuests || !tt.HasText("启程之日") || !tt.HasText("等级调整") {
		t.Fatalf("任务视图渲染异常: tab=%v", app.tab)
	}
	for _, txt := range tt.Texts() {
		if strings.ContainsRune(txt, '\ue0be') {
			t.Fatalf("任务标题不应暴露私有字符 \\ue0be: %q", txt)
		}
	}
	if app.questSummaryOpen || app.questDialogueOpen {
		t.Fatalf("任务概要与对话应默认折叠以防剧透")
	}
	if !tt.HasText("任务概要与目的") || !tt.HasText("任务对话") || tt.HasText("缪恩正在等候新冒险者前来报到。") || tt.HasText("欢迎来到格里达尼亚！") {
		t.Fatalf("默认折叠状态未正确隐藏剧透文本: %v", tt.Texts())
	}
	if err := tt.Click("任务概要与目的"); err != nil {
		t.Fatalf("展开任务概要失败: %v", err)
	}
	if !app.questSummaryOpen || !tt.HasText("缪恩正在等候新冒险者前来报到。") || !tt.HasText("和魔女咖啡馆的缪恩对话") {
		t.Fatalf("展开任务概要后未展示正文与目的: open=%v texts=%v", app.questSummaryOpen, tt.Texts())
	}
	if err := tt.Click("任务对话"); err != nil {
		t.Fatalf("展开任务对话失败: %v", err)
	}
	if !app.questDialogueOpen || !tt.HasText("欢迎来到格里达尼亚！") {
		t.Fatalf("展开任务对话后未展示台词: open=%v texts=%v", app.questDialogueOpen, tt.Texts())
	}
	// 切换任务后应自动复位为折叠状态
	app.selectQuest(65536)
	if app.questSummaryOpen || app.questDialogueOpen {
		t.Fatalf("切换任务后未重置折叠状态")
	}

	// 验证切换到理符视图
	if err := tt.Click("理符"); err != nil {
		t.Fatalf("点击理符导航失败: %v", err)
	}
	if app.tab != tabLeves || !tt.HasText("大量制作：甜点委托") || !tt.HasText("随机奖励道具池") {
		t.Fatalf("理符视图渲染异常: tab=%v", app.tab)
	}

	// 验证版本 Diff 体现任务、理符与收藏品变更及详情跳转
	backupDir := filepath.Join(dataDir, "backups")
	oldStore := &gamedata.Store{
		PrimaryVersion: "2026.10.01",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", CategoryName: "食品", ItemLevel: 690},
		},
	}
	if _, err := backup.NewService(oldStore, backupDir).InitializeOnStartup(); err != nil {
		t.Fatalf("构建旧版本快照失败: %v", err)
	}
	bSvc := backup.NewService(store, backupDir)
	vst, err := bSvc.InitializeOnStartup()
	if err != nil {
		t.Fatalf("构建当前版本快照失败: %v", err)
	}
	app.backupSvc = bSvc
	app.versionStatus = &vst
	app.diffFromVer = vst.PreviousVersion
	app.diffToVer = vst.CurrentVersion
	app.refreshDiff()

	if err := tt.Click("版本对比"); err != nil {
		t.Fatalf("点击版本对比失败: %v", err)
	}
	for _, want := range []string{"版本变更对比", "收藏品变动", "新增任务", "新增理符", "收藏品", "启程之日", "大量制作：甜点委托"} {
		if !tt.HasText(want) {
			t.Fatalf("版本对比缺少预期文案 %q: %v", want, tt.Texts())
		}
	}

	// 验证按任务筛选并直接点击任务名称跳转至任务页
	app.diffEntity = "quest"
	app.refreshDiff()
	tt.Frame()
	if err := tt.Click("启程之日"); err != nil {
		t.Fatalf("从版本对比点击任务名称跳转失败: %v", err)
	}
	if app.tab != tabQuests || app.selectedQuestID != 65536 {
		t.Fatalf("期望跳转至任务详情页 #65536，实际 tab=%v questID=%d", app.tab, app.selectedQuestID)
	}
}

func TestDiffViewLayoutAndInteractions(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	rowCake := make(sqpack.Row, 95)
	rowCake[9] = "收藏用图拉尔菠萝蛋糕"
	rowCake[20] = 690

	questEntry := gamedata.QuestEntry{
		ID:          65536,
		NameCn:      "启程之日",
		IconID:      gamedata.QuestIconMSQ,
		SectionName: "主线任务",
		GenreName:   "第七星历主线",
		Level:       15,
	}
	leveEntry := gamedata.LeveEntry{
		ID:                 201,
		NameCn:             "大量制作：甜点委托",
		LeveKind:           "craft",
		LeveKindLabel:      "制作理符",
		AssignmentTypeName: "大量筹备",
		ClassJobLevel:      98,
	}
	achEntry := gamedata.AchievementEntry{
		ID:            1001,
		NameCn:        "新版本成就",
		DescriptionCn: "完成版本新挑战",
		Points:        10,
		KindID:        1,
		KindName:      "战斗",
		CategoryID:    1,
		CategoryName:  "整体",
	}

	store := &gamedata.Store{
		PrimaryVersion: "2026.11.01",
		ItemsPrimary:   map[int]sqpack.Row{44099: rowCake},
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", CategoryName: "食品", ItemLevel: 690, IsCollectableTurnIn: true},
		},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			44099: {ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", CategoryName: "食品", ItemLevel: 690, IsCollectableTurnIn: true},
		},
		Quests:       []gamedata.QuestEntry{questEntry},
		QuestByID:    map[int]gamedata.QuestEntry{65536: questEntry},
		Leves:        []gamedata.LeveEntry{leveEntry},
		LeveByID:     map[int]gamedata.LeveEntry{201: leveEntry},
		Achievements: []gamedata.AchievementEntry{achEntry},
	}

	svc := gamedata.NewService(store)
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.mode = modeReady
	app.tab = tabDiff
	app.diffRes = &backup.VersionDiffResponse{
		FromVersion:   "2026.10.01",
		ToVersion:     "2026.11.01",
		TotalFiltered: 5,
		Page:          1,
		PageSize:      25,
		TotalPages:    1,
		Items: []backup.DiffItemEntry{
			{
				ID:           65536,
				EntityType:   "quest",
				ChangeType:   "added",
				NameCn:       "启程之日",
				CategoryName: "任务 · 第七星历主线",
				ItemLevel:    15,
				Changes: []backup.ItemFieldChange{
					{Field: "quest", Label: "新增详情标签", Before: "无", After: "Lv.15 · 主线"},
				},
			},
			{
				ID:                   44099,
				EntityType:           "item",
				ChangeType:           "modified",
				NameCn:               "收藏用图拉尔菠萝蛋糕",
				CategoryName:         "食品",
				ItemLevel:            690,
				HasCollectableChange: true,
				Changes: []backup.ItemFieldChange{
					{Field: "collectableSummary", Label: "收藏品交易", Before: "无", After: "收藏品交易 Lv.100"},
				},
			},
			{
				ID:           99999,
				EntityType:   "item",
				ChangeType:   "removed",
				NameCn:       "已淘汰测试道具",
				CategoryName: "食品",
				ItemLevel:    500,
				Changes: []backup.ItemFieldChange{
					{Field: "status", Label: "版本移除", Before: "品级 500", After: "已移除"},
				},
			},
			{
				ID:           201,
				EntityType:   "leve",
				ChangeType:   "added",
				NameCn:       "大量制作：甜点委托",
				CategoryName: "理符 · 制作理符",
				ItemLevel:    98,
			},
			{
				ID:           1001,
				EntityType:   "achievement",
				ChangeType:   "added",
				NameCn:       "新版本成就",
				CategoryName: "成就 · 战斗",
				Points:       10,
			},
		},
	}

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 菜单与表头：改为「版本对比」，不再包含「等级/品级/点数」与「操作」列
	if !tt.HasText("版本对比") || tt.HasText("版本 Diff") {
		t.Fatalf("侧边栏菜单名称应为「版本对比」: %v", tt.Texts())
	}
	for _, absent := range []string{"等级/品级/点数", "操作", "详情", "奖励"} {
		if tt.HasText(absent) {
			t.Fatalf("版本对比页面不应再显示 %q: %v", absent, tt.Texts())
		}
	}
	for _, hdr := range []string{"ID", "名称", "类型", "变更内容"} {
		if !tt.HasText(hdr) {
			t.Fatalf("版本对比表头缺少 %q: %v", hdr, tt.Texts())
		}
	}

	// 2. 新增与移除条目的变更内容不显示；变更条目仍正常显示变更前后明细
	for _, hidden := range []string{"新增详情标签", "Lv.15 · 主线", "版本移除", "品级 500"} {
		if tt.HasText(hidden) {
			t.Fatalf("新增或已移除条目不应显示变更内容 %q: %v", hidden, tt.Texts())
		}
	}
	for _, shown := range []string{"收藏品交易", "收藏品交易 Lv.100"} {
		if !tt.HasText(shown) {
			t.Fatalf("变更条目缺少变更内容 %q: %v", shown, tt.Texts())
		}
	}

	// 3. ID 列左对齐，且「收藏品」tag 移入 ID 列（位于 #44099 正下方、名称左侧）
	hdrID, okHdrID := tt.Find("ID")
	valID, okValID := tt.Find("#44099")
	tagCol, okTag := tt.Find("收藏品")
	valName, okName := tt.Find("收藏用图拉尔菠萝蛋糕")
	if !okHdrID || !okValID || !okTag || !okName {
		t.Fatalf("未找到 ID/标签/名称布局节点: hdr=%v id=%v tag=%v name=%v", okHdrID, okValID, okTag, okName)
	}
	if math.Abs(float64(valID.X-hdrID.X)) > 1.0 {
		t.Fatalf("ID 列未与表头左对齐: hdr=%+v val=%+v", hdrID, valID)
	}
	if tagCol.Y <= valID.Y || math.Abs(float64(tagCol.X-valID.X)) > 10.0 || tagCol.X+tagCol.W >= valName.X {
		t.Fatalf("实体标签应位于 ID 列下方且在名称列左侧: id=%+v tag=%+v name=%+v", valID, tagCol, valName)
	}

	// 4. 类型列居中对齐，且下方胶囊无多余宽度空白
	hdrType, okHdrType := tt.Find("类型")
	removedPill, okRemoved := tt.Find("已移除")
	if !okHdrType || !okRemoved {
		t.Fatalf("未找到类型列头或已移除标签: hdr=%v pill=%v", okHdrType, okRemoved)
	}
	if math.Abs(float64((hdrType.X+hdrType.W/2)-(removedPill.X+removedPill.W/2))) > 1.5 {
		t.Fatalf("类型列未与表头水平居中对齐: hdr=%+v pill=%+v", hdrType, removedPill)
	}
	if removedPill.W >= diffColTypeW-6 {
		t.Fatalf("类型标签宽度异常，疑似被拉伸产生多余空白: pill=%+v colW=%v", removedPill, diffColTypeW)
	}
	// 检查类型胶囊右侧边界之外不应继续绘制危险底色（应为白底画布，即胶囊背景未拉满列宽）
	img := tt.Image()
	bgRight := img.RGBAAt(int(removedPill.X+removedPill.W+8), int(removedPill.Y+removedPill.H/2))
	if bgRight.R < 245 || bgRight.G < 245 || bgRight.B < 245 {
		t.Fatalf("类型胶囊右侧疑似存在多余背景色空白: px=%+v", bgRight)
	}

	// 5. 点击 ID 或名称列均可跳转；已移除条目点击不跳转
	if err := tt.Click("#65536"); err != nil {
		t.Fatalf("点击任务 ID 跳转失败: %v", err)
	}
	if app.tab != tabQuests || app.selectedQuestID != 65536 {
		t.Fatalf("点击任务 ID 后未跳转至任务 #65536: tab=%v questID=%d", app.tab, app.selectedQuestID)
	}

	app.navigateTab(tabDiff)
	tt.Frame()
	if err := tt.Click("收藏用图拉尔菠萝蛋糕"); err != nil {
		t.Fatalf("点击物品名称跳转失败: %v", err)
	}
	if app.tab != tabWorkbench || app.selectedItemID != 44099 {
		t.Fatalf("点击物品名称后未跳转至物品 #44099: tab=%v itemID=%d", app.tab, app.selectedItemID)
	}

	app.navigateTab(tabDiff)
	tt.Frame()
	if err := tt.Click("#201"); err != nil {
		t.Fatalf("点击理符 ID 跳转失败: %v", err)
	}
	if app.tab != tabLeves || app.selectedLeveID != 201 {
		t.Fatalf("点击理符 ID 后未跳转至理符 #201: tab=%v leveID=%d", app.tab, app.selectedLeveID)
	}

	app.navigateTab(tabDiff)
	tt.Frame()
	if err := tt.Click("新版本成就"); err != nil {
		t.Fatalf("点击成就名称跳转失败: %v", err)
	}
	if app.tab != tabAchievements || app.achQuery.Q != "1001" || len(app.achRes.Items) != 1 {
		t.Fatalf("点击成就后未跳转并定位至成就 #1001: tab=%v q=%q len=%d", app.tab, app.achQuery.Q, len(app.achRes.Items))
	}

	app.navigateTab(tabDiff)
	tt.Frame()
	if removedRect, ok := tt.Find("已淘汰测试道具"); !ok {
		t.Fatalf("未找到已移除测试条目")
	} else {
		tt.ClickAt(removedRect.X+removedRect.W/2, removedRect.Y+removedRect.H/2)
		if app.tab != tabDiff {
			t.Fatalf("已移除条目点击不应跳转页面，实际 tab=%v", app.tab)
		}
	}
}

func makeFakeClientDir(t *testing.T, root, version string) string {
	t.Helper()
	sqpackDir := filepath.Join(root, "game", "sqpack", "ffxiv")
	if err := os.MkdirAll(sqpackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "boot"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sqpackDir, "000000.win32.index"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "game", "ffxivgame.ver"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSetupModesDiagnosisAndLoadingView(t *testing.T) {
	dataDir := t.TempDir()
	gamesRoot := t.TempDir()
	cnDir := makeFakeClientDir(t, filepath.Join(gamesRoot, "最终幻想XIV"), "2026.11.01.0000.0000")
	glDir := makeFakeClientDir(t, filepath.Join(gamesRoot, "FINAL FANTASY XIV Online"), "2026.11.02.0000.0000")

	app := newZHApp(t, dataDir)
	defer app.Close()

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 默认进入「仅国服」，不应出现「国际服游戏目录」干扰
	if !tt.HasText("国服游戏目录") || tt.HasText("国际服游戏目录") {
		t.Fatalf("仅国服模式下目录块展示不符合预期: %v", tt.Texts())
	}

	// 2. 切换为「仅国际服」后仅显示国际服目录
	if err := tt.Click("仅国际服"); err != nil {
		t.Fatalf("切换仅国际服失败: %v", err)
	}
	if app.setupMode != setupModeGlobal || !tt.HasText("国际服游戏目录") || tt.HasText("国服游戏目录") {
		t.Fatalf("仅国际服模式切换异常: mode=%v texts=%v", app.setupMode, tt.Texts())
	}

	// 3. 切换为「国服 + 国际服」后同时展示两者
	if err := tt.Click("国服 + 国际服"); err != nil {
		t.Fatalf("切换双服模式失败: %v", err)
	}
	if app.setupMode != setupModeBoth || !tt.HasText("国服游戏目录") || !tt.HasText("国际服游戏目录") {
		t.Fatalf("双服模式切换异常: mode=%v", app.setupMode)
	}

	// 4. 选错目录（boot 子目录）应给出非术语提示并可一键改选到根目录
	if err := tt.Click("仅国服"); err != nil {
		t.Fatalf("切回仅国服失败: %v", err)
	}
	app.draftCNPath = filepath.Join(cnDir, "boot")
	app.invalidatePathDiag()
	tt.Frame()
	if !tt.HasText("启动器文件夹") || !tt.HasText("改选「最终幻想XIV」") {
		t.Fatalf("未生成预期的启动器误选纠错提示: %v", tt.Texts())
	}
	for _, txt := range tt.Texts() {
		if strings.Contains(txt, "sqpack") {
			t.Fatalf("界面文案不应暴露内部术语 sqpack: %q", txt)
		}
	}
	if err := tt.Click("改选「最终幻想XIV」"); err != nil {
		t.Fatalf("点击一键改选失败: %v", err)
	}
	if app.draftCNPath != cnDir {
		t.Fatalf("期望一键改选回根目录 %q，实际: %q", cnDir, app.draftCNPath)
	}
	if !tt.HasText("已识别 · v2026.11.01.0000.0000") {
		t.Fatalf("有效目录识别徽章未显示: %v", tt.Texts())
	}

	// 5. 选到上级目录应能探测出游戏子文件夹
	app.draftCNPath = gamesRoot
	app.invalidatePathDiag()
	tt.Frame()
	if !tt.HasText("外层目录") {
		t.Fatalf("选到上级目录时缺少提示: %v", tt.Texts())
	}

	// 6. 验证带双引号的路径（资源管理器“复制文件地址”格式）可直接规范化保存
	app.draftCNPath = `"` + cnDir + `"`
	app.draftGlobalPath = `"` + glDir + `"`
	if normalizeUserPathInput(app.draftCNPath) != cnDir {
		t.Fatalf("双引号路径规范化失败")
	}

	// 7. 验证加载页三阶段展示
	app.mode = modeLoading
	app.loadingCNVer = "2026.11.01.0000.0000"
	app.loadingGlobalVer = "2026.11.02.0000.0000"
	app.loadStartedAt = time.Now().Add(-1500 * time.Millisecond)
	app.loadStage = loadStageSheets
	app.loadSheetsDone = 46
	app.loadSheetsTotal = 92
	app.loadingStatusTxt = "正在读取游戏数据表..."
	tt.Frame()
	for _, want := range []string{"正在读取本地游戏数据", "只读模式", "读取游戏原始数据表", "46 / 92 张表", "构建物品、配方与任务索引", "校验本地版本快照"} {
		if !tt.HasText(want) {
			t.Fatalf("加载页缺少预期文案 %q: %v", want, tt.Texts())
		}
	}
}

func TestAchievementsViewFiltersAndColumns(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	rowItem := make(sqpack.Row, 95)
	rowItem[9] = "光之战士玩偶"
	rowItem[20] = 1

	cats := []gamedata.AchievementCategoryInfo{
		{ID: 1, KindID: 1, KindName: "战斗", Name: "整体", Count: 1},
		{ID: 2, KindID: 1, KindName: "战斗", Name: "迷宫探险", Count: 1},
		{ID: 35, KindID: 8, KindName: "任务", Name: "整体", Count: 1},
	}

	store := &gamedata.Store{
		PrimaryVersion: "2026.11.01",
		ItemsPrimary:   map[int]sqpack.Row{50001: rowItem},
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 50001, NameCn: "光之战士玩偶", CategoryName: "宠物", ItemLevel: 1},
		},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			50001: {ID: 50001, NameCn: "光之战士玩偶", CategoryName: "宠物", ItemLevel: 1},
		},
		Achievements: []gamedata.AchievementEntry{
			{
				ID:               1,
				NameCn:           "胜利的荣光1",
				NameEn:           "To Crush Your Enemies I",
				NameJa:           "勝利の栄光：ランク1",
				DescriptionCn:    "在副本中取得胜利",
				Points:           5,
				KindID:           1,
				KindName:         "战斗",
				CategoryID:       1,
				CategoryName:     "整体",
				TitleID:          10,
				TitleMasculineCn: "沙场勇将",
				TitleFeminineCn:  "沙场女杰",
				TitleNameEn:      "Battleborn",
				TitleIsPrefix:    false,
			},
			{
				ID:               2,
				NameCn:           "天然要害探险者",
				NameEn:           "Mapping the Realm: Sastasha",
				NameJa:           "サスタシャの踏破者",
				DescriptionCn:    "绘制天然要害沙斯塔夏溶洞全图",
				Points:           30,
				KindID:           1,
				KindName:         "战斗",
				CategoryID:       2,
				CategoryName:     "迷宫探险",
				RewardItemID:     50001,
				RewardItemNameCn: "光之战士玩偶",
			},
			{
				ID:            3,
				NameCn:        "艾欧泽亚旅人",
				NameEn:        "A Realm Wanderer",
				NameJa:        "エオルゼアの旅人",
				DescriptionCn: "完成任意任务",
				Points:        20,
				KindID:        8,
				KindName:      "任务",
				CategoryID:    35,
				CategoryName:  "整体",
			},
		},
		AchievementKindList: []gamedata.AchievementKindInfo{
			{ID: 1, Name: "战斗", Count: 2, Categories: cats[:2]},
			{ID: 8, Name: "任务", Count: 1, Categories: cats[2:]},
		},
		AchievementCategoryList: cats,
		TotalTitles:             1,
	}

	svc := gamedata.NewService(store)
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.mode = modeReady
	app.tab = tabAchievements
	app.refreshAchievements()

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 验证合并后的表头且不再存在拆分的独立列头
	texts := tt.Texts()
	if !slices.Contains(texts, "奖励称号 / 道具") || slices.Contains(texts, "奖励称号") || slices.Contains(texts, "奖励道具") {
		t.Fatalf("奖励列合并未生效: %v", texts)
	}

	// 验证点数列宽度已扩大且表头与数值水平居中对齐
	hdrPts, okHdr := tt.Find("点数")
	valPts, okVal := tt.Find("30")
	if !okHdr || !okVal {
		t.Fatalf("未定位到点数表头或数值节点: hdr=%v val=%v", okHdr, okVal)
	}
	if hdrPts.W < 60 || math.Abs(float64((hdrPts.X+hdrPts.W/2)-(valPts.X+valPts.W/2))) > 1.0 {
		t.Fatalf("点数列宽度或水平居中未对齐: hdr=%+v val=%+v", hdrPts, valPts)
	}

	// 2. 验证即使英文成就名非空，日文成就名也同步展示，且日文在前英文在后
	for _, want := range []string{
		"胜利的荣光1", "To Crush Your Enemies I", "勝利の栄光：ランク1",
		"天然要害探险者", "Mapping the Realm: Sastasha", "サスタシャの踏破者",
		"《沙场勇将 / 沙场女杰》", "Battleborn", "光之战士玩偶",
	} {
		if !tt.HasText(want) {
			t.Fatalf("成就页面缺少预期多语言或奖励文案 %q: %v", want, tt.Texts())
		}
	}
	if tt.HasText("前缀") || tt.HasText("后缀") {
		t.Fatalf("奖励称号不应再显示前缀或后缀标记: %v", tt.Texts())
	}
	cnRect, okCn := tt.Find("胜利的荣光1")
	jaRect, okJa := tt.Find("勝利の栄光：ランク1")
	enRect, okEn := tt.Find("To Crush Your Enemies I")
	if !okCn || !okJa || !okEn || !(cnRect.Y < jaRect.Y && jaRect.Y < enRect.Y) {
		t.Fatalf("成就名称显示顺序应为 中文->日文->英文: cn=%+v ja=%+v en=%+v", cnRect, jaRect, enRect)
	}

	// 验证表格内数据允许用户拖拽框选并复制（含跨多列/跨元素框选）
	startRect, okStart := tt.Find("胜利的荣光1")
	endRect, okEnd := tt.Find("在副本中取得胜利")
	if !okStart || !okEnd {
		t.Fatalf("未定位到用于框选测试的表格文本")
	}
	tt.Press(startRect.X+2, startRect.Y+startRect.H/2)
	tt.Move(endRect.X+endRect.W-2, endRect.Y+endRect.H/2)
	tt.Release(endRect.X+endRect.W-2, endRect.Y+endRect.H/2)
	tt.Key(ui.Cmd, ui.KeyC)
	copied := tt.Clipboard()
	if !strings.Contains(copied, "勝利の栄光：ランク1") || !strings.Contains(copied, "To Crush Your Enemies I") || !strings.Contains(copied, "在副本中取得胜利") {
		t.Fatalf("表格内框选复制内容不符合预期: %q", copied)
	}

	// 3. 验证二级分类下拉选项展示及筛选联动（战斗 · 迷宫探险）
	catOpts := app.buildAchievementCategoryOptions()
	foundPrefixOpt := false
	for _, opt := range catOpts {
		if opt.value == 2 && strings.Contains(opt.label, "战斗 · 迷宫探险") {
			foundPrefixOpt = true
			break
		}
	}
	if !foundPrefixOpt {
		t.Fatalf("未在全部大类下为二级分类生成大类前缀选项: %+v", catOpts)
	}

	app.achQuery.CategoryID = 2
	app.onAchievementCategoryChanged()
	tt.Frame()
	if app.achQuery.KindID != 1 || app.achRes.Total != 1 || app.achRes.Items[0].ID != 2 {
		t.Fatalf("选定二级分类后未联动大类或筛选结果异常: query=%+v res=%+v", app.achQuery, app.achRes)
	}
	if !tt.HasText("天然要害探险者") || tt.HasText("胜利的荣光1") || tt.HasText("艾欧泽亚旅人") {
		t.Fatalf("二级分类筛选渲染结果不符合预期: %v", tt.Texts())
	}

	// 切换回大类为“任务”时，原有战斗下的二级分类应自动重置
	app.achQuery.KindID = 8
	app.onAchievementKindChanged()
	tt.Frame()
	if app.achQuery.CategoryID != 0 || app.achRes.Total != 1 || app.achRes.Items[0].ID != 3 {
		t.Fatalf("切换大类时未正确重置跨类二级分类: query=%+v res=%+v", app.achQuery, app.achRes)
	}

	// 重置按钮应清空全部筛选条件
	if err := tt.Click("重置"); err != nil {
		t.Fatalf("点击重置筛选按钮失败: %v", err)
	}
	if app.hasAchievementFilter() || app.achRes.Total != 3 {
		t.Fatalf("重置后应恢复全部成就: query=%+v total=%d", app.achQuery, app.achRes.Total)
	}

	// 验证最小窗口尺寸 1024×640 下成就页稳定渲染
	ttMin := ui.NewTester(app.View, 1024, 640)
	if !ttMin.HasText("成就与称号") || !ttMin.HasText("奖励称号 / 道具") || !ttMin.HasText("サスタシャの踏破者") {
		t.Fatalf("最小视口下成就页面渲染异常: %v", ttMin.Texts())
	}

	// 验证点击合并列内的奖励道具按钮仍可跳转物品详情
	if err := tt.Click("查看 光之战士玩偶"); err != nil {
		t.Fatalf("点击成就奖励道具失败: %v", err)
	}
	if app.tab != tabWorkbench || app.selectedItemID != 50001 {
		t.Fatalf("期望跳转至奖励道具详情 #50001，实际 tab=%v item=%d", app.tab, app.selectedItemID)
	}
}

func TestDarkModeTransparentIconRendering(t *testing.T) {
	// 构造 32×32 BGRA8 贴图：外圈为透明纯白 (255,255,255,0)，中心 16×16 为不透明金色 (240,180,60,255)
	const w, h = 32, 32
	raw := make([]byte, 80+w*h*4)
	binary.LittleEndian.PutUint32(raw[0:4], 0x800000)
	binary.LittleEndian.PutUint32(raw[4:8], sqpack.TexFormatB8G8R8A8)
	binary.LittleEndian.PutUint16(raw[8:10], w)
	binary.LittleEndian.PutUint16(raw[10:12], h)
	binary.LittleEndian.PutUint16(raw[12:14], 1)
	raw[14] = 1
	raw[15] = 1
	binary.LittleEndian.PutUint32(raw[28:32], 80)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := 80 + (y*w+x)*4
			if x >= 8 && x < 24 && y >= 8 && y < 24 {
				// BGRA: B=60, G=180, R=240, A=255
				raw[off], raw[off+1], raw[off+2], raw[off+3] = 60, 180, 240, 255
			} else {
				// 模拟 FF14 货币/职业图标透明区域中常见的纯白 RGB + Alpha=0
				raw[off], raw[off+1], raw[off+2], raw[off+3] = 255, 255, 255, 0
			}
		}
	}

	img, err := sqpack.DecodeTexToRGBA(raw)
	if err != nil {
		t.Fatalf("DecodeTexToRGBA 失败: %v", err)
	}
	bmp := ui.NewBitmap(img)

	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()
	app.cfg.Theme = "dark"
	app.service = gamedata.NewService(&gamedata.Store{})
	app.bitmaps.put("icon:65001:false", bmp)

	// 在深色模式下渲染 drawGameIcon（34×34 图标框位于 (8,8)..(42,42)）及筛选芯片图标
	rendered := ui.Render(func(c *ui.Context) {
		t := applyAppTheme(c, "dark")
		ui.Row(c).Fill().Background(t.background).Padding(8).Gap(8).AlignItems(ui.Center).Children(func() {
			app.drawGameIcon(c, 65001, false, 34, t.radius.md)
			kitToggleChip(c, "dark-chip", "刻木", "", false, bmp)
		})
	}, 140, 50, 1)

	// (12,12) 落在图标透明外圈内：深色模式下必须透出深色背景，不得出现白色替换
	cornerPx := rendered.RGBAAt(12, 12)
	if cornerPx.R > 60 || cornerPx.G > 60 || cornerPx.B > 60 {
		t.Fatalf("深色模式下图标透明底被替换为亮色/白色: got=%+v", cornerPx)
	}

	// (25,25) 落在图标不透明中心内：应保持金色主体颜色
	centerPx := rendered.RGBAAt(25, 25)
	if centerPx.R < 200 || centerPx.G < 140 || centerPx.B > 100 {
		t.Fatalf("图标主体颜色异常: got=%+v", centerPx)
	}

	// 检查芯片图标区域 (50..64, 18..32) 不得存在纯白像素
	for y := 18; y < 32; y++ {
		for x := 50; x < 64; x++ {
			px := rendered.RGBAAt(x, y)
			if px.R > 245 && px.G > 245 && px.B > 245 {
				t.Fatalf("深色模式下筛选标签图标透明区域出现纯白像素 (%d,%d): %+v", x, y, px)
			}
		}
	}
}

func TestListRowDisplayFormat(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	store := &gamedata.Store{
		PrimaryVersion: "2026.11.01",
		ItemSummaries: []gamedata.ItemSummary{
			{
				ID:           1,
				NameCn:       "金币",
				NameEn:       "Gil",
				CategoryName: "其他",
				ItemLevel:    1,
				IsGatherable: true,
			},
		},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			1: {ID: 1, NameCn: "金币", NameEn: "Gil", CategoryName: "其他", ItemLevel: 1, IsGatherable: true},
		},
		Quests: []gamedata.QuestEntry{
			{
				ID:          65536,
				NameCn:      "冒险者入门",
				GenreName:   "重生之境",
				SectionName: "主线任务1",
				PlaceName:   "格里达尼亚新街",
				Level:       1,
				IsMSQ:       true,
				RewardItems: []gamedata.QuestRewardItem{{ItemID: 1, ItemName: "金币", Count: 1}},
			},
			{
				ID:           66001,
				NameCn:       "日常委托示例",
				GenreName:    "日常任务",
				PlaceName:    "摩杜纳",
				Level:        50,
				IsRepeatable: true,
			},
			{
				ID:        66002,
				NameCn:    "如何加入行会",
				PlaceName: "利姆萨·罗敏萨",
				Level:     10,
				IsFeature: true,
			},
		},
		Leves: []gamedata.LeveEntry{
			{
				ID:                 21,
				NameCn:             "制作委托：长枪",
				LeveKind:           "craft",
				LeveKindLabel:      "制作理符",
				AssignmentTypeName: "刻木匠",
				ClassJobLevel:      1,
				PlaceNameStart:     "黑衣森林中央林区",
				TurnInItems:        []gamedata.LeveTurnInItem{{ItemID: 1, ItemName: "长枪", Count: 1}},
			},
		},
	}

	svc := gamedata.NewService(store)
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.mode = modeReady

	// 1. 物品页面列表：左下角仅显示类型，右下角仅显示 iLv X
	app.tab = tabWorkbench
	app.refreshItemList()
	tt := ui.NewTester(app.View, 1366, 768)
	if !tt.HasText("其他") || !tt.HasText("iLv 1") {
		t.Fatalf("物品列表行缺少预期类型或品级文案: %v", tt.Texts())
	}
	if tt.HasText("其他 · Gil") || tt.HasText("i1 · 采集") || tt.HasText("i1") {
		t.Fatalf("物品列表行不应包含英文名或来源标签: %v", tt.Texts())
	}

	// 2. 任务页面列表：左下角仅显示类型（主线任务、日常任务等），右下角仅显示等级
	if got := app.formatQuestRowType(store.Quests[0]); got != "主线任务" {
		t.Fatalf("主线任务类型文本异常: got=%q", got)
	}
	if got := app.formatQuestRowType(store.Quests[1]); got != "日常任务" {
		t.Fatalf("日常任务类型文本异常: got=%q", got)
	}
	if got := app.formatQuestRowType(store.Quests[2]); got != "功能解锁" {
		t.Fatalf("功能解锁任务类型文本异常: got=%q", got)
	}
	if got := formatQuestRowMeta(store.Quests[0]); got != "Lv.1" {
		t.Fatalf("任务等级文本异常: got=%q", got)
	}
	app.tab = tabQuests
	app.refreshQuests()
	tt.Frame()
	for _, want := range []string{"主线任务", "日常任务", "功能解锁", "Lv.1", "Lv.50", "Lv.10"} {
		if !tt.HasText(want) {
			t.Fatalf("任务列表行缺少预期文案 %q: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"格里达尼亚新街", "摩杜纳", "利姆萨·罗敏萨", "Lv.1 · 主线 · 道具", "重生之境"} {
		if tt.HasText(unwanted) {
			t.Fatalf("任务列表行不应显示 %q: %v", unwanted, tt.Texts())
		}
	}

	// 3. 理符页面列表：左下角仅显示职业，右下角仅显示等级
	app.tab = tabLeves
	app.refreshLeves()
	tt.Frame()
	if !tt.HasText("刻木匠") || !tt.HasText("Lv.1") {
		t.Fatalf("理符列表行缺少预期职业或等级文案: %v", tt.Texts())
	}
	for _, unwanted := range []string{"黑衣森林中央林区", "刻木匠 · 黑衣森林中央林区", "Lv.1 · 交纳 1 种"} {
		if tt.HasText(unwanted) {
			t.Fatalf("理符列表行不应显示 %q: %v", unwanted, tt.Texts())
		}
	}
}

func TestTopLeftBrandTitleAndVersion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{in: "2.0.0", want: "v2.0.0"},
		{in: "v2.0.0", want: "v2.0.0"},
		{in: "V2.1.0-rc.1", want: "v2.1.0-rc.1"},
		{in: "  ", want: ""},
	} {
		if got := formatAppVersionTag(tc.in); got != tc.want {
			t.Fatalf("formatAppVersionTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	// 1. 验证品牌组件文本顺序与小字号高度
	ttCap := ui.NewTester(func(c *ui.Context) {
		applyAppTheme(c, "light")
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			app.renderBrandCaption(c)
		})
	}, 400, 60)
	capTitleRect, okCapTitle := ttCap.Find(appBrandTitle)
	capVerRect, okCapVer := ttCap.Find("v2.0.0")
	if !okCapTitle || !okCapVer {
		t.Fatalf("品牌标题组件缺少标题或版本号: title=%v ver=%v texts=%v", okCapTitle, okCapVer, ttCap.Texts())
	}
	if capVerRect.X <= capTitleRect.X+capTitleRect.W {
		t.Fatalf("版本号应位于标题右侧: title=%+v ver=%+v", capTitleRect, capVerRect)
	}
	if capVerRect.H >= capTitleRect.H {
		t.Fatalf("版本号字号高度应小于主标题: title=%+v ver=%+v", capTitleRect, capVerRect)
	}

	// 2. 引导页左上角标题与版本验证
	ttSetup := ui.NewTester(app.View, 1366, 768)
	if ttSetup.HasText("艾欧泽亚数据工作台") {
		t.Fatalf("引导页仍包含旧标题文案: %v", ttSetup.Texts())
	}
	setupTitleRect, okSetupTitle := ttSetup.Find(appBrandTitle)
	setupVerRect, okSetupVer := ttSetup.Find("v2.0.0")
	if !okSetupTitle || !okSetupVer || setupVerRect.X <= setupTitleRect.X+setupTitleRect.W {
		t.Fatalf("引导页左上角标题或版本位置异常: title=%+v (%v) ver=%+v (%v)", setupTitleRect, okSetupTitle, setupVerRect, okSetupVer)
	}

	// 3. 主工作台左上角品牌菜单标题、版本与点击弹出菜单验证
	svc := gamedata.NewService(&gamedata.Store{PrimaryVersion: "2026.11.01"})
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.mode = modeReady
	app.tab = tabWorkbench

	ttReady := ui.NewTester(app.View, 1366, 768)
	if ttReady.HasText("艾欧泽亚数据工作台") {
		t.Fatalf("主工作台仍包含旧标题文案: %v", ttReady.Texts())
	}
	btnRect, okBtn := ttReady.Find(appBrandTitle)
	readyVerRect, okReadyVer := ttReady.Find("v2.0.0")
	if !okBtn || !okReadyVer {
		t.Fatalf("主工作台缺少左上角品牌菜单或版本号: btn=%v ver=%v texts=%v", okBtn, okReadyVer, ttReady.Texts())
	}
	if readyVerRect.X <= btnRect.X+btnRect.W/2 || readyVerRect.X+readyVerRect.W > btnRect.X+btnRect.W {
		t.Fatalf("主工作台版本号应位于品牌菜单右侧内: btn=%+v ver=%+v", btnRect, readyVerRect)
	}
	if readyVerRect.H >= capTitleRect.H {
		t.Fatalf("主工作台版本号字号高度应小于主标题: title=%+v ver=%+v", capTitleRect, readyVerRect)
	}

	if err := ttReady.Click(appBrandTitle); err != nil {
		t.Fatalf("点击左上角品牌菜单失败: %v", err)
	}
	if !slices.Contains(ttReady.Menu(), "导出游戏 CSV...") {
		t.Fatalf("点击左上角品牌菜单后未展示菜单项: %v", ttReady.Menu())
	}
}

func TestFirstLaunchAutoDetectsOSLanguage(t *testing.T) {
	dataDir := t.TempDir()
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
	defer app.Close()

	wantLang := i18n.DetectSystemLanguage()
	if app.Language() != wantLang {
		t.Fatalf("首次启动未读取系统语言: got=%q want=%q", app.Language(), wantLang)
	}
	if app.mode != modeSetup {
		t.Fatalf("首次无配置应停留在引导页: got=%v", app.mode)
	}
	wantSetupMode := setupModeCN
	if wantLang != i18n.LangZH {
		wantSetupMode = setupModeGlobal
	}
	if app.setupMode != wantSetupMode {
		t.Fatalf("首次启动默认区服与语言不匹配: lang=%q setupMode=%v want=%v", wantLang, app.setupMode, wantSetupMode)
	}
	if inferSetupMode("", "", i18n.LangZH) != setupModeCN ||
		inferSetupMode("", "", i18n.LangJA) != setupModeGlobal ||
		inferSetupMode("", "", i18n.LangEN) != setupModeGlobal {
		t.Fatalf("不同语言下首次默认区服推断结果异常")
	}

	tt := ui.NewTester(app.View, 1366, 768)
	if !tt.HasText(i18n.T(wantLang, "setup.title")) {
		t.Fatalf("引导页未按系统语言渲染标题 %q: %v", i18n.T(wantLang, "setup.title"), tt.Texts())
	}
}

func TestWelcomeAndSettingsLanguageSwitchAndPersistence(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 欢迎页应直接展示三种语言选项并支持即时切换与持久化
	for _, wantBtn := range []string{"简体中文", "日本語", "English"} {
		if !tt.HasText(wantBtn) {
			t.Fatalf("欢迎页缺少语言切换按钮 %q: %v", wantBtn, tt.Texts())
		}
	}
	if err := tt.Click("日本語"); err != nil {
		t.Fatalf("欢迎页切换日语失败: %v", err)
	}
	if app.Language() != i18n.LangJA || !tt.HasText(i18n.T(i18n.LangJA, "setup.title")) {
		t.Fatalf("欢迎页切换日语后界面未刷新: lang=%q texts=%v", app.Language(), tt.Texts())
	}
	if cfg, err := config.LoadAppConfig(dataDir); err != nil || cfg.Language != "ja" {
		t.Fatalf("欢迎页切换日语后未持久化: cfg=%+v err=%v", cfg, err)
	}

	if err := tt.Click("English"); err != nil {
		t.Fatalf("欢迎页切换英语失败: %v", err)
	}
	if app.Language() != i18n.LangEN || !tt.HasText(i18n.T(i18n.LangEN, "setup.title")) {
		t.Fatalf("欢迎页切换英语后界面未刷新: lang=%q texts=%v", app.Language(), tt.Texts())
	}

	if err := tt.Click("简体中文"); err != nil {
		t.Fatalf("欢迎页切回中文失败: %v", err)
	}
	if app.Language() != i18n.LangZH || !tt.HasText(i18n.T(i18n.LangZH, "setup.title")) {
		t.Fatalf("欢迎页切回中文后界面未刷新: lang=%q", app.Language())
	}

	// 2. 进入工作台「设置」页后仍可自由切换语言且保留当前页签状态
	svc := gamedata.NewService(&gamedata.Store{PrimaryVersion: "2026.11.01"})
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.mode = modeReady
	app.tab = tabWorkbench
	tt.Frame()

	if err := tt.Click(i18n.T(i18n.LangZH, "nav.settings")); err != nil {
		t.Fatalf("点击设置导航失败: %v", err)
	}
	if app.tab != tabSettings || !tt.HasText(i18n.T(i18n.LangZH, "settings.title")) {
		t.Fatalf("未进入设置页面: tab=%v", app.tab)
	}

	if err := tt.Click("日本語"); err != nil {
		t.Fatalf("设置页切换日语失败: %v", err)
	}
	if app.Language() != i18n.LangJA || app.mode != modeReady || app.tab != tabSettings || !tt.HasText(i18n.T(i18n.LangJA, "settings.title")) {
		t.Fatalf("设置页切换日语状态异常: lang=%q mode=%v tab=%v texts=%v", app.Language(), app.mode, app.tab, tt.Texts())
	}
	if cfg, err := config.LoadAppConfig(dataDir); err != nil || cfg.Language != "ja" {
		t.Fatalf("设置页切换日语后未持久化: cfg=%+v err=%v", cfg, err)
	}

	if err := tt.Click("English"); err != nil {
		t.Fatalf("设置页切换英语失败: %v", err)
	}
	if app.Language() != i18n.LangEN || app.mode != modeReady || app.tab != tabSettings || !tt.HasText(i18n.T(i18n.LangEN, "settings.title")) {
		t.Fatalf("设置页切换英语状态异常: lang=%q mode=%v tab=%v texts=%v", app.Language(), app.mode, app.tab, tt.Texts())
	}

	// 3. 验证国际服日语 / 英语与中文版本快照目录隔离
	if got := app.backupDirForStore(&gamedata.Store{PrimaryLang: sqpack.LangChineseSimplified}); got != filepath.Join(dataDir, "backups") {
		t.Fatalf("中文快照目录异常: %q", got)
	}
	if got := app.backupDirForStore(&gamedata.Store{PrimaryLang: sqpack.LangJapanese}); got != filepath.Join(dataDir, "backups", "ja") {
		t.Fatalf("日语快照目录应独立隔离: %q", got)
	}
	if got := app.backupDirForStore(&gamedata.Store{PrimaryLang: sqpack.LangEnglish}); got != filepath.Join(dataDir, "backups", "en") {
		t.Fatalf("英语模式下国际服快照目录应独立隔离: %q", got)
	}
}

func TestSecondaryDataLanguageDisplayRules(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	rowItem := make(sqpack.Row, 95)
	rowItem[9] = "主名称物品"
	rowItem[20] = 690
	rowItemJA := make(sqpack.Row, 95)
	rowItemJA[9] = "日本語アイテム"
	rowItemEN := make(sqpack.Row, 95)
	rowItemEN[9] = "English Item"

	cats := []gamedata.AchievementCategoryInfo{
		{ID: 1, KindID: 1, KindName: "Battle", Name: "General", Count: 1},
	}
	store := &gamedata.Store{
		PrimaryVersion: "2026.11.01",
		ItemsPrimary:   map[int]sqpack.Row{44099: rowItem},
		ItemsJa:        map[int]sqpack.Row{44099: rowItemJA},
		ItemsEn:        map[int]sqpack.Row{44099: rowItemEN},
		ItemSummaries: []gamedata.ItemSummary{{
			ID: 44099, NameCn: "主名称物品", NameZh: "主名称物品", NameJa: "日本語アイテム", NameEn: "English Item",
			CategoryName: "Food", ItemLevel: 690,
		}},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			44099: {
				ID: 44099, NameCn: "主名称物品", NameZh: "主名称物品", NameJa: "日本語アイテム", NameEn: "English Item",
				CategoryName: "Food", ItemLevel: 690,
			},
		},
		Quests: []gamedata.QuestEntry{{
			ID: 65536, NameCn: "主名称任务", NameZh: "主名称任务", NameJa: "日本語クエスト", NameEn: "English Quest",
			Level: 15, SectionName: "MSQ", GenreName: "MSQ",
		}},
		QuestByID: map[int]gamedata.QuestEntry{
			65536: {
				ID: 65536, NameCn: "主名称任务", NameZh: "主名称任务", NameJa: "日本語クエスト", NameEn: "English Quest",
				Level: 15, SectionName: "MSQ", GenreName: "MSQ",
			},
		},
		Leves: []gamedata.LeveEntry{{
			ID: 201, NameCn: "主名称理符", NameZh: "主名称理符", NameJa: "日本語リーヴ", NameEn: "English Leve",
			ClassJobLevel: 10, LeveKind: "craft", LeveKindLabel: "Craft", AssignmentTypeName: "CRP",
		}},
		LeveByID: map[int]gamedata.LeveEntry{
			201: {
				ID: 201, NameCn: "主名称理符", NameZh: "主名称理符", NameJa: "日本語リーヴ", NameEn: "English Leve",
				ClassJobLevel: 10, LeveKind: "craft", LeveKindLabel: "Craft", AssignmentTypeName: "CRP",
			},
		},
		Achievements: []gamedata.AchievementEntry{{
			ID: 1, NameCn: "主名称成就", NameJa: "日本語アチーブ", NameEn: "English Achievement",
			DescriptionCn: "Win battle", Points: 10, KindID: 1, KindName: "Battle", CategoryID: 1, CategoryName: "General",
			TitleID: 10, TitleMasculineCn: "主名称称号男", TitleFeminineCn: "主名称称号女",
			TitleNameJa: "日本語称号", TitleNameEn: "English Title",
		}},
		AchievementKindList:     []gamedata.AchievementKindInfo{{ID: 1, Name: "Battle", Count: 1, Categories: cats}},
		AchievementCategoryList: cats,
	}

	svc := gamedata.NewService(store)
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.mode = modeReady
	app.refreshItemList()
	app.selectItem(44099)
	app.refreshQuests()
	app.selectQuest(65536)
	app.refreshLeves()
	app.selectLeve(201)
	app.refreshAchievements()

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 中文模式下：四个实体均同时显示日语+英语（日语在先，英语在后）
	app.SetLanguage(i18n.LangZH)
	app.tab = tabWorkbench
	tt.Frame()
	itemJaRect, okItemJA := tt.Find("JA  日本語アイテム")
	itemEnRect, okItemEN := tt.Find("EN  English Item")
	if !okItemJA || !okItemEN || itemJaRect.X >= itemEnRect.X {
		t.Fatalf("中文下物品详情副语言应为 JA + EN: ja=%v en=%v texts=%v", okItemJA, okItemEN, tt.Texts())
	}

	app.tab = tabQuests
	tt.Frame()
	qJaRect, okQJA := tt.Find("JA  日本語クエスト")
	qEnRect, okQEN := tt.Find("EN  English Quest")
	if !okQJA || !okQEN || qJaRect.X >= qEnRect.X {
		t.Fatalf("中文下任务详情副语言应为 JA + EN: ja=%v en=%v texts=%v", okQJA, okQEN, tt.Texts())
	}

	app.tab = tabLeves
	tt.Frame()
	lJaRect, okLJA := tt.Find("JA  日本語リーヴ")
	lEnRect, okLEN := tt.Find("EN  English Leve")
	if !okLJA || !okLEN || lJaRect.X >= lEnRect.X {
		t.Fatalf("中文下理符详情副语言应为 JA + EN: ja=%v en=%v texts=%v", okLJA, okLEN, tt.Texts())
	}

	app.tab = tabAchievements
	tt.Frame()
	achJaRect, okAchJA := tt.Find("日本語アチーブ")
	achEnRect, okAchEN := tt.Find("English Achievement")
	tJaRect, okTitleJA := tt.Find("日本語称号")
	tEnRect, okTitleEN := tt.Find("English Title")
	if !okAchJA || !okAchEN || achJaRect.Y >= achEnRect.Y || !okTitleJA || !okTitleEN || tJaRect.Y >= tEnRect.Y {
		t.Fatalf("中文下成就与称号副语言应为 日语在前、英语在后: texts=%v", tt.Texts())
	}

	// 2. 日语模式下：仅显示英语，不显示日语副语言
	app.SetLanguage(i18n.LangJA)
	app.tab = tabWorkbench
	tt.Frame()
	if !tt.HasText("EN  English Item") || tt.HasText("JA  日本語アイテム") {
		t.Fatalf("日语下物品详情应仅显示 EN 副语言: %v", tt.Texts())
	}
	app.tab = tabQuests
	tt.Frame()
	if !tt.HasText("EN  English Quest") || tt.HasText("JA  日本語クエスト") {
		t.Fatalf("日语下任务详情应仅显示 EN 副语言: %v", tt.Texts())
	}
	app.tab = tabLeves
	tt.Frame()
	if !tt.HasText("EN  English Leve") || tt.HasText("JA  日本語リーヴ") {
		t.Fatalf("日语下理符详情应仅显示 EN 副语言: %v", tt.Texts())
	}
	app.tab = tabAchievements
	tt.Frame()
	if !tt.HasText("English Achievement") || tt.HasText("日本語アチーブ") || !tt.HasText("English Title") || tt.HasText("日本語称号") {
		t.Fatalf("日语下成就与称号应仅显示英语副语言: %v", tt.Texts())
	}

	// 3. 英语模式下：仅显示日语，不显示英语副语言
	app.SetLanguage(i18n.LangEN)
	app.tab = tabWorkbench
	tt.Frame()
	if !tt.HasText("JA  日本語アイテム") || tt.HasText("EN  English Item") {
		t.Fatalf("英语下物品详情应仅显示 JA 副语言: %v", tt.Texts())
	}
	app.tab = tabQuests
	tt.Frame()
	if !tt.HasText("JA  日本語クエスト") || tt.HasText("EN  English Quest") {
		t.Fatalf("英语下任务详情应仅显示 JA 副语言: %v", tt.Texts())
	}
	app.tab = tabLeves
	tt.Frame()
	if !tt.HasText("JA  日本語リーヴ") || tt.HasText("EN  English Leve") {
		t.Fatalf("英语下理符详情应仅显示 JA 副语言: %v", tt.Texts())
	}
	app.tab = tabAchievements
	tt.Frame()
	if !tt.HasText("日本語アチーブ") || tt.HasText("English Achievement") || !tt.HasText("日本語称号") || tt.HasText("English Title") {
		t.Fatalf("英语下成就与称号应仅显示日语副语言: %v", tt.Texts())
	}
}

func TestMinimumViewportInJapaneseAndEnglish(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.LangJA, i18n.LangEN} {
		dataDir := t.TempDir()
		if err := config.SaveAppConfig(dataDir, config.AppConfig{Language: string(lang)}); err != nil {
			t.Fatal(err)
		}
		app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))

		tt := ui.NewTester(app.View, 1024, 640)
		if !tt.HasText(i18n.T(lang, "setup.title")) || !tt.HasText(i18n.T(lang, "setup.start_btn")) {
			app.Close()
			t.Fatalf("最小视口 (%s) 引导页未正常渲染: %v", lang, tt.Texts())
		}

		svc := gamedata.NewService(&gamedata.Store{PrimaryVersion: "2026.11.01"})
		st := svc.GetSystemStatus()
		app.service = svc
		app.status = &st
		app.mode = modeReady

		for _, tab := range []activeTab{tabWorkbench, tabQuests, tabLeves, tabAchievements, tabDiff, tabChangelog, tabSettings} {
			app.tab = tab
			tt.Frame()
			if len(tt.Texts()) == 0 {
				app.Close()
				t.Fatalf("最小视口 (%s) 页签 %v 渲染为空", lang, tab)
			}
		}
		if !tt.HasText(i18n.T(lang, "settings.title")) {
			app.Close()
			t.Fatalf("最小视口 (%s) 设置页缺少标题: %v", lang, tt.Texts())
		}
		app.Close()
	}
}

func TestTopRightLanguageSwitcherEverywhere(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 欢迎引导页 (modeSetup)：右上角按钮位于右侧且点击后展开菜单并切换语言
	setupBtnRect, okSetupBtn := tt.Find(i18n.T(i18n.LangZH, "lang.switch_tip"))
	if !okSetupBtn || setupBtnRect.X <= 1366/2 || setupBtnRect.Y >= 48 {
		t.Fatalf("引导页右上角语言切换按钮位置异常: ok=%v rect=%+v", okSetupBtn, setupBtnRect)
	}
	if err := tt.Click(i18n.T(i18n.LangZH, "lang.switch_tip")); err != nil {
		t.Fatalf("引导页点击右上角语言按钮失败: %v", err)
	}
	if !app.langPopoverOpen {
		t.Fatalf("点击右上角语言按钮后未打开切换浮层")
	}
	// 点击浮层中的第二项（「日本語」：MarginTop 4 + PaddingTop 4 + Row1 28 + Gap 2 + Row2 半高 14）
	tt.ClickAt(setupBtnRect.X+setupBtnRect.W/2, setupBtnRect.Y+setupBtnRect.H+52)
	if app.Language() != i18n.LangJA || app.langPopoverOpen || !tt.HasText(i18n.T(i18n.LangJA, "setup.title")) {
		t.Fatalf("引导页右上角浮层切换日语失败: lang=%q open=%v texts=%v", app.Language(), app.langPopoverOpen, tt.Texts())
	}

	// 2. 加载等待页 (modeLoading)：右上角按钮可打开并通过浮层即时切换语言
	app.mode = modeLoading
	app.loadingStatusTxt = i18n.T(i18n.LangJA, "loading.preparing")
	tt.Frame()
	loadBtnRect, okLoadBtn := tt.Find(i18n.T(i18n.LangJA, "lang.switch_tip"))
	if !okLoadBtn || loadBtnRect.X <= 1366/2 || loadBtnRect.Y >= 48 {
		t.Fatalf("加载页右上角语言切换按钮位置异常: ok=%v rect=%+v", okLoadBtn, loadBtnRect)
	}
	if err := tt.Click(i18n.T(i18n.LangJA, "lang.switch_tip")); err != nil {
		t.Fatalf("加载页点击右上角语言按钮失败: %v", err)
	}
	if err := tt.Click("English"); err != nil {
		t.Fatalf("加载页浮层切换英语失败: %v", err)
	}
	if app.Language() != i18n.LangEN || !tt.HasText(i18n.T(i18n.LangEN, "loading.title")) {
		t.Fatalf("加载页通过右上角切换英语失败: lang=%q texts=%v", app.Language(), tt.Texts())
	}

	// 3. 主工作台各页面 (modeReady)：物品、任务、理符、成就、版本对比、设置全量覆盖
	svc := gamedata.NewService(&gamedata.Store{PrimaryVersion: "2026.11.01"})
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.mode = modeReady

	for _, tab := range []activeTab{tabWorkbench, tabQuests, tabLeves, tabAchievements, tabDiff, tabChangelog, tabSettings} {
		app.tab = tab
		tt.Frame()
		rect, ok := tt.Find(i18n.T(app.Language(), "lang.switch_tip"))
		if !ok || rect.X <= 1366/2 || rect.Y >= 48 {
			t.Fatalf("页签 %v 右上角语言切换按钮缺失或位置异常: ok=%v rect=%+v", tab, ok, rect)
		}
	}

	// 从版本对比页通过右上角按钮切换到日语，并保持在当前页签
	app.tab = tabDiff
	tt.Frame()
	if err := tt.Click(i18n.T(i18n.LangEN, "lang.switch_tip")); err != nil {
		t.Fatalf("版本对比页点击右上角语言按钮失败: %v", err)
	}
	if err := tt.Click("日本語"); err != nil {
		t.Fatalf("版本对比页浮层切换日语失败: %v", err)
	}
	if app.Language() != i18n.LangJA || app.tab != tabDiff || !tt.HasText(i18n.T(i18n.LangJA, "diff.title")) {
		t.Fatalf("版本对比页右上角切换日语异常: lang=%q tab=%v", app.Language(), app.tab)
	}

	// 从物品页通过右上角按钮切回简体中文，并验证配置持久化
	app.tab = tabWorkbench
	tt.Frame()
	if err := tt.Click(i18n.T(i18n.LangJA, "lang.switch_tip")); err != nil {
		t.Fatalf("物品页点击右上角语言按钮失败: %v", err)
	}
	if err := tt.Click("简体中文"); err != nil {
		t.Fatalf("物品页浮层切换简体中文失败: %v", err)
	}
	if app.Language() != i18n.LangZH || app.tab != tabWorkbench || !tt.HasText(i18n.T(i18n.LangZH, "nav.items")) {
		t.Fatalf("物品页右上角切换中文异常: lang=%q tab=%v", app.Language(), app.tab)
	}
	if cfg, err := config.LoadAppConfig(dataDir); err != nil || cfg.Language != "zh" {
		t.Fatalf("通过右上角切换语言后未持久化: cfg=%+v err=%v", cfg, err)
	}

	// 4. 最小窗口尺寸 1024×640 下顶栏全局检索框、内置搜索框与右上角语言按钮不重叠、不溢出
	ttMin := ui.NewTester(app.View, 1024, 640)
	globalRect, okGlobal := ttMin.Find(i18n.T(i18n.LangZH, "search.global_label"))
	searchRect, okSearch := ttMin.Find(i18n.T(i18n.LangZH, "search.item_label"))
	minLangRect, okMinLang := ttMin.Find(i18n.T(i18n.LangZH, "lang.switch_tip"))
	if !okGlobal || !okSearch || !okMinLang || globalRect.Y >= 48 || globalRect.X+globalRect.W >= minLangRect.X || searchRect.Y <= 48 || minLangRect.X+minLangRect.W > 1024 {
		t.Fatalf("1024x640 下顶栏全局搜索框、页内搜索框与右上角语言按钮布局异常: global=%+v (%v) page=%+v (%v) lang=%+v (%v)",
			globalRect, okGlobal, searchRect, okSearch, minLangRect, okMinLang)
	}
}

func TestLanguageSwitchPreservesSelectionBitmapsAndCache(t *testing.T) {
	// 1. 无客户端模式下：切换语言不清空图标位图缓存，且同字体栈语言 (zh/en) 复用同一主题对象
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	app.bitmaps.put("icon:99999:false", ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 2, 2))))
	app.mode = modeReady
	app.tab = tabWorkbench
	app.SetLanguage(i18n.LangJA)
	app.SetLanguage(i18n.LangEN)
	app.bitmaps.mu.Lock()
	_, keptMockSentinel := app.bitmaps.bitmaps["icon:99999:false"]
	app.bitmaps.mu.Unlock()
	if !keptMockSentinel {
		t.Fatalf("无客户端模式下切语言也不应清空图标位图缓存")
	}

	tt := ui.NewTester(func(c *ui.Context) {
		_ = applyAppThemeWithLang(c, "light", i18n.LangZH)
	}, 100, 100)
	_ = tt
	var thZH, thEN, thJA *ui.Theme
	ui.NewTester(func(c *ui.Context) {
		_ = applyAppThemeWithLang(c, "light", i18n.LangZH)
		thZH = c.Theme()
		_ = applyAppThemeWithLang(c, "light", i18n.LangEN)
		thEN = c.Theme()
		_ = applyAppThemeWithLang(c, "light", i18n.LangJA)
		thJA = c.Theme()
	}, 100, 100)
	if thZH != thEN || thZH == thJA {
		t.Fatalf("同字体栈语言应复用同一 Theme 指针且日语独立: zh=%p en=%p ja=%p", thZH, thEN, thJA)
	}

	// 2. 若本机配置真实客户端：验证切语言不退回 modeLoading、保留图标与当前选中条目，并命中多语言会话缓存
	envCfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	if _, err := config.ValidateAndResolve(envCfg); err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过真实客户端语言切换缓存测试: %v", err)
	}

	realDir := t.TempDir()
	realApp := newZHApp(t, realDir)
	defer realApp.Close()

	if !realApp.SaveAndReloadPaths(envCfg.CNPath, envCfg.GlobalPath) {
		t.Fatalf("加载真实客户端失败: %s", realApp.draftError)
	}
	if realApp.mode != modeReady {
		t.Fatalf("期望进入 modeReady，实际 %v", realApp.mode)
	}
	realApp.bitmaps.put("icon:99999:false", ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 2, 2))))
	realApp.tab = tabWorkbench
	realApp.selectItem(34577)
	origZHService := realApp.service

	// 首次切换到日语：停留在当前页面与 tabWorkbench，不清空位图，尽量保留选中物品 #34577
	realApp.SetLanguage(i18n.LangJA)
	if realApp.mode != modeReady || realApp.tab != tabWorkbench || realApp.selectedItemID != 34577 {
		t.Fatalf("切到日语不应跳转加载页或丢失选中物品: mode=%v tab=%v item=%d", realApp.mode, realApp.tab, realApp.selectedItemID)
	}
	realApp.bitmaps.mu.Lock()
	_, keptSentinel := realApp.bitmaps.bitmaps["icon:99999:false"]
	realApp.bitmaps.mu.Unlock()
	if !keptSentinel {
		t.Fatalf("切语言不应清空已缓存的图标位图")
	}

	// 切换到英语：命中缓存或共享句柄，继续保留选中条目与当前页面
	realApp.SetLanguage(i18n.LangEN)
	if realApp.mode != modeReady || realApp.selectedItemID != 34577 {
		t.Fatalf("切到英语状态异常: mode=%v item=%d", realApp.mode, realApp.selectedItemID)
	}

	// 切回已缓存的简体中文：应直接命中会话缓存（同一 Service 实例，零重载）
	realApp.SetLanguage(i18n.LangZH)
	if realApp.service != origZHService || realApp.mode != modeReady || realApp.selectedItemID != 34577 {
		t.Fatalf("切回已缓存中文应直接命中会话缓存并保持选中条目: sameSvc=%v mode=%v item=%d",
			realApp.service == origZHService, realApp.mode, realApp.selectedItemID)
	}
}

func TestGlobalSearchAndBuiltInPageSearchBars(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	rowCake := make(sqpack.Row, 95)
	rowCake[9] = "收藏用图拉尔菠萝蛋糕"
	rowCake[20] = 99
	rowTea := make(sqpack.Row, 95)
	rowTea[9] = "高山红茶"
	rowTea[20] = 99

	quest1 := gamedata.QuestEntry{
		ID: 65536, NameCn: "\ue0be 启程之日", NameEn: "Coming to Gridania",
		IconID: gamedata.QuestIconMSQ, SectionName: "主线任务", GenreName: "第七星历主线", Level: 15, IsMSQ: true,
	}
	quest2 := gamedata.QuestEntry{
		ID: 65537, NameCn: "蛋糕试吃大会", NameEn: "Cake Tasting",
		IconID: gamedata.QuestIconSide, SectionName: "支线任务", GenreName: "太阳海岸支线", Level: 50,
	}
	leve1 := gamedata.LeveEntry{
		ID: 201, NameCn: "大量制作：甜点委托", NameEn: "Sweet Treat",
		LeveKind: "craft", LeveKindLabel: "制作理符", AssignmentTypeName: "大量筹备", ClassJobLevel: 98,
	}
	leve2 := gamedata.LeveEntry{
		ID: 202, NameCn: "制作委托：蛋糕拼盘", NameEn: "Cake Platter",
		LeveKind: "craft", LeveKindLabel: "制作理符", AssignmentTypeName: "工匠委托", ClassJobLevel: 90,
	}
	cats := []gamedata.AchievementCategoryInfo{
		{ID: 1, KindID: 1, KindName: "战斗", Name: "整体", Count: 2},
	}
	ach1 := gamedata.AchievementEntry{
		ID: 1001, NameCn: "胜利的荣光1", DescriptionCn: "战斗获胜", Points: 10,
		KindID: 1, KindName: "战斗", CategoryID: 1, CategoryName: "整体",
		TitleID: 1, TitleMasculineCn: "沙场勇将",
	}
	ach2 := gamedata.AchievementEntry{
		ID: 1002, NameCn: "蛋糕大师", DescriptionCn: "制作大量甜点", Points: 20,
		KindID: 1, KindName: "战斗", CategoryID: 1, CategoryName: "整体",
	}

	store := &gamedata.Store{
		PrimaryVersion: "2026.11.01",
		ItemsPrimary:   map[int]sqpack.Row{44099: rowCake, 44100: rowTea},
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", CategoryName: "食品", ItemLevel: 690},
			{ID: 44100, NameCn: "高山红茶", CategoryName: "食品", ItemLevel: 680},
		},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			44099: {ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", CategoryName: "食品", ItemLevel: 690},
			44100: {ID: 44100, NameCn: "高山红茶", CategoryName: "食品", ItemLevel: 680},
		},
		Quests:                  []gamedata.QuestEntry{quest1, quest2},
		QuestByID:               map[int]gamedata.QuestEntry{65536: quest1, 65537: quest2},
		Leves:                   []gamedata.LeveEntry{leve1, leve2},
		LeveByID:                map[int]gamedata.LeveEntry{201: leve1, 202: leve2},
		Achievements:            []gamedata.AchievementEntry{ach1, ach2},
		AchievementKindList:     []gamedata.AchievementKindInfo{{ID: 1, Name: "战斗", Count: 2, Categories: cats}},
		AchievementCategoryList: cats,
	}

	backupDir := filepath.Join(dataDir, "backups")
	oldStore := &gamedata.Store{PrimaryVersion: "2026.10.01"}
	if _, err := backup.NewService(oldStore, backupDir).InitializeOnStartup(); err != nil {
		t.Fatal(err)
	}
	bSvc := backup.NewService(store, backupDir)
	vst, err := bSvc.InitializeOnStartup()
	if err != nil {
		t.Fatal(err)
	}

	svc := gamedata.NewService(store)
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.backupSvc = bSvc
	app.versionStatus = &vst
	app.diffFromVer = vst.PreviousVersion
	app.diffToVer = vst.CurrentVersion
	app.mode = modeReady
	app.tab = tabWorkbench
	app.refreshItemList()
	app.selectItem(44100)
	app.refreshQuests()
	app.selectQuest(65536)
	app.refreshLeves()
	app.selectLeve(201)
	app.refreshAchievements()
	app.refreshDiff()

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 验证全局搜索支持名称包含检索（输入部分词“启程”命中“启程之日”）并支持按回车直接跳转详情
	if err := tt.Click(i18n.T(i18n.LangZH, "search.global_label")); err != nil {
		t.Fatalf("点击顶栏全局搜索框失败: %v", err)
	}
	tt.Type("启程")
	if !app.globalSearchOpen || !tt.HasText("匹配结果") || !tt.HasText("启程之日") || !tt.HasText("跳转到对应页面搜索") {
		t.Fatalf("输入后未弹出全局搜索浮层或未包含匹配结果: open=%v texts=%v", app.globalSearchOpen, tt.Texts())
	}
	if !tt.HasText("在「物品」中搜索") || !tt.HasText("在「任务」中搜索") || !tt.HasText("在「理符」中搜索") || !tt.HasText("在「成就」中搜索") || !tt.HasText("在「版本对比」中搜索") {
		t.Fatalf("全局搜索浮层末尾缺少各模块跳转搜索入口: %v", tt.Texts())
	}

	// 按 Enter 应直接打开命中的任务 #65536
	tt.Key(0, ui.KeyEnter)
	if app.tab != tabQuests || app.selectedQuestID != 65536 || app.globalSearchOpen {
		t.Fatalf("全局搜索回车未跳转到任务详情 #65536: tab=%v questID=%d open=%v", app.tab, app.selectedQuestID, app.globalSearchOpen)
	}

	// 2. 验证包含匹配成就称号（“沙场”命中“沙场勇将”）并点击命中条目跳转至成就页
	if err := tt.Click("清空"); err != nil {
		t.Fatalf("清空全局搜索框失败: %v", err)
	}
	if err := tt.Click(i18n.T(i18n.LangZH, "search.global_label")); err != nil {
		t.Fatalf("重新聚焦全局搜索框失败: %v", err)
	}
	tt.Type("沙场")
	if !app.globalSearchOpen || !tt.HasText("胜利的荣光1") {
		t.Fatalf("输入成就称号关键词未命中对应成就: open=%v texts=%v", app.globalSearchOpen, tt.Texts())
	}
	if err := tt.Click("胜利的荣光1"); err != nil {
		t.Fatalf("点击全局搜索命中成就失败: %v", err)
	}
	if app.tab != tabAchievements || app.achQuery.Q != "1001" || app.globalSearchOpen {
		t.Fatalf("点击全局成就命中后未跳转定位: tab=%v q=%q open=%v", app.tab, app.achQuery.Q, app.globalSearchOpen)
	}

	// 3. 验证无结果词提示，以及输入“蛋糕”可跨模块命中并从末尾跳转到指定页面搜索
	if err := tt.Click("清空"); err != nil {
		t.Fatalf("清空全局搜索框失败: %v", err)
	}
	if err := tt.Click(i18n.T(i18n.LangZH, "search.global_label")); err != nil {
		t.Fatalf("重新聚焦全局搜索框失败: %v", err)
	}
	tt.Type("不存在的词")
	if !app.globalSearchOpen || !tt.HasText(i18n.T(i18n.LangZH, "search.global_no_matches")) {
		t.Fatalf("无命中词时应显示空提示: open=%v hits=%+v", app.globalSearchOpen, app.globalSearchRes.Hits)
	}
	if err := tt.Click("清空"); err != nil {
		t.Fatalf("清空全局搜索框失败: %v", err)
	}
	if err := tt.Click(i18n.T(i18n.LangZH, "search.global_label")); err != nil {
		t.Fatalf("重新聚焦全局搜索框失败: %v", err)
	}
	tt.Type("蛋糕")
	if !app.globalSearchOpen || app.globalSearchRes.TotalMatched != 4 {
		t.Fatalf("跨模块包含搜索“蛋糕”应同时命中 4 个模块共 4 项: open=%v res=%+v", app.globalSearchOpen, app.globalSearchRes)
	}
	if err := tt.Click("在「理符」中搜索"); err != nil {
		t.Fatalf("点击跳转到理符页面搜索失败: %v", err)
	}
	if app.tab != tabLeves || app.leveQuery.Q != "蛋糕" || app.leveRes.Total != 1 || app.leveRes.Items[0].ID != 202 {
		t.Fatalf("跳转到理符页面搜索状态异常: tab=%v q=%q res=%+v", app.tab, app.leveQuery.Q, app.leveRes)
	}

	// 4. 验证全部 5 个模块页面均具备内置搜索栏、可独立过滤且不干扰全局搜索框
	pageChecks := []struct {
		tab      activeTab
		label    string
		query    string
		want     string
		absent   string
		checkRes func() bool
	}{
		{
			tab: tabWorkbench, label: i18n.T(i18n.LangZH, "search.item_label"),
			query: "红茶", want: "高山红茶", absent: "收藏用图拉尔菠萝蛋糕",
			checkRes: func() bool { return app.itemRes.Total == 1 && app.itemRes.Items[0].ID == 44100 },
		},
		{
			tab: tabQuests, label: i18n.T(i18n.LangZH, "search.quest_label"),
			query: "试吃", want: "蛋糕试吃大会", absent: "启程之日",
			checkRes: func() bool { return app.questRes.Total == 1 && app.questRes.Items[0].ID == 65537 },
		},
		{
			tab: tabLeves, label: i18n.T(i18n.LangZH, "search.leve_label"),
			query: "甜点", want: "大量制作：甜点委托", absent: "制作委托：蛋糕拼盘",
			checkRes: func() bool { return app.leveRes.Total == 1 && app.leveRes.Items[0].ID == 201 },
		},
		{
			tab: tabAchievements, label: i18n.T(i18n.LangZH, "search.ach_label"),
			query: "蛋糕大师", want: "蛋糕大师", absent: "胜利的荣光1",
			checkRes: func() bool { return app.achRes.Total == 1 && app.achRes.Items[0].ID == 1002 },
		},
		{
			tab: tabDiff, label: i18n.T(i18n.LangZH, "search.diff_label"),
			query: "高山红茶", want: "高山红茶", absent: "蛋糕试吃大会",
			checkRes: func() bool {
				return app.diffRes != nil && app.diffRes.TotalFiltered == 1 && app.diffRes.Items[0].ID == 44100
			},
		},
	}
	for _, pc := range pageChecks {
		app.navigateTab(pc.tab)
		tt.Frame()
		// 切换页签后先重置可能遗留的筛选再测试页内搜索框
		switch pc.tab {
		case tabWorkbench:
			app.resetItemFilters()
		case tabQuests:
			app.resetQuestFilters()
		case tabLeves:
			app.resetLeveFilters()
		case tabAchievements:
			app.resetAchievementFilters()
		case tabDiff:
			app.resetDiffFilters()
		}
		tt.Frame()
		// 先验证 Ctrl/Cmd+F 可聚焦当前页内内置搜索栏
		tt.Key(ui.Cmd, ui.KeyF)
		if !tt.Focused(pc.label) {
			t.Fatalf("页签 %v 按快捷键未聚焦页内搜索框 %q", pc.tab, pc.label)
		}
		tt.Type(pc.query)
		if !pc.checkRes() || !tt.HasText(pc.want) {
			t.Fatalf("页签 %v 内置搜索过滤结果异常: want=%q texts=%v", pc.tab, pc.want, tt.Texts())
		}
		if r, ok := tt.Find(pc.absent); ok {
			// 分栏页面右侧详情面板仍可能展示此前选中的条目，左侧列表/整页表格内不应再出现被过滤条目
			if pc.tab == tabAchievements || pc.tab == tabDiff || r.X < app.sidebarW+app.listW {
				t.Fatalf("页签 %v 列表区仍包含被过滤条目 %q: rect=%+v", pc.tab, pc.absent, r)
			}
		}
		if app.globalSearchQ != "蛋糕" {
			t.Fatalf("页内搜索不应覆盖全局搜索输入内容: got=%q", app.globalSearchQ)
		}
	}

	// 验证 Ctrl/Cmd+K 可聚焦顶栏全局搜索框并重新打开浮层，Escape 可关闭浮层
	tt.Key(ui.Cmd, ui.KeyK)
	if !tt.Focused(i18n.T(i18n.LangZH, "search.global_label")) || !app.globalSearchOpen {
		t.Fatalf("全局搜索快捷键未聚焦顶栏搜索框或未展开浮层: focused=%v open=%v",
			tt.Focused(i18n.T(i18n.LangZH, "search.global_label")), app.globalSearchOpen)
	}
	tt.Key(0, ui.KeyEscape)
	if app.globalSearchOpen {
		t.Fatalf("按 Escape 应关闭全局搜索浮层")
	}

	// 5. 验证 1024×640 窄屏下全部 5 个页面内置搜索框与全局搜索框均正常显示且不越界
	ttMin := ui.NewTester(app.View, 1024, 640)
	for _, pc := range pageChecks {
		app.tab = pc.tab
		ttMin.Frame()
		r, ok := ttMin.Find(pc.label)
		if !ok || r.X < 0 || r.X+r.W > 1024 || r.Y < 48 {
			t.Fatalf("1024x640 下页签 %v 内置搜索框位置异常: ok=%v rect=%+v", pc.tab, ok, r)
		}
	}
}

func TestGlobalSearchFixedHeightAndJumpLocatesLeftList(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	itemsMap := make(map[int]sqpack.Row, 70)
	itemSummaries := make([]gamedata.ItemSummary, 0, 70)
	itemSummaryByID := make(map[int]gamedata.ItemSummary, 70)
	for i := 1; i <= 70; i++ {
		name := "以太测试物品 " + string(rune('A'+(i-1)%26))
		if i == 55 {
			name = "目标跃迁水晶"
		}
		if i == 60 {
			name = "目标关联材料"
		}
		row := make(sqpack.Row, 95)
		row[9] = name
		row[20] = 99
		itemsMap[i] = row
		sum := gamedata.ItemSummary{ID: i, NameCn: name, CategoryName: "药水", ItemLevel: 100 + i}
		itemSummaries = append(itemSummaries, sum)
		itemSummaryByID[i] = sum
	}

	quests := make([]gamedata.QuestEntry, 0, 40)
	questByID := make(map[int]gamedata.QuestEntry, 40)
	for i := 1; i <= 40; i++ {
		qid := 65500 + i
		qe := gamedata.QuestEntry{
			ID:          qid,
			NameCn:      "以太测试任务",
			IconID:      gamedata.QuestIconSide,
			SectionID:   0,
			SectionName: "支线任务",
			GenreName:   "支线",
			Level:       i,
		}
		if i == 35 {
			qe.NameCn = "目标第2页任务"
			qe.RewardItems = []gamedata.QuestRewardItem{{ItemID: 55, ItemName: "目标跃迁水晶", Count: 1}}
		}
		if i == 38 {
			qe.NameCn = "目标后续任务"
			qe.PreviousQuestIDs = []int{65535}
			qe.PreviousQuestNames = []string{"目标第2页任务"}
		}
		quests = append(quests, qe)
		questByID[qid] = qe
	}

	leves := make([]gamedata.LeveEntry, 0, 40)
	leveByID := make(map[int]gamedata.LeveEntry, 40)
	for i := 1; i <= 40; i++ {
		lid := 200 + i
		lv := gamedata.LeveEntry{
			ID:                 lid,
			NameCn:             "以太测试理符",
			LeveKind:           "craft",
			LeveKindLabel:      "制作理符",
			AssignmentTypeName: "工匠委托",
			ClassJobLevel:      i,
		}
		if i == 36 {
			lv.NameCn = "目标第2页理符"
		}
		leves = append(leves, lv)
		leveByID[lid] = lv
	}

	store := &gamedata.Store{
		PrimaryVersion:  "2026.11.01",
		ItemsPrimary:    itemsMap,
		ItemSummaries:   itemSummaries,
		ItemSummaryByID: itemSummaryByID,
		Recipes: map[int]sqpack.Row{
			9001: {0, 0, 1, 55, 1, 0, 60, 2},
		},
		RecipesByResultItem: map[int][]int{55: {9001}},
		Quests:              quests,
		QuestByID:           questByID,
		Leves:               leves,
		LeveByID:            leveByID,
	}

	svc := gamedata.NewService(store)
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.mode = modeReady
	app.tab = tabWorkbench
	app.refreshItemList()
	app.selectItem(1)
	app.refreshQuests()
	app.selectQuest(65501)
	app.refreshLeves()
	app.selectLeve(201)

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 验证 0 命中、1 命中、50 命中时全局搜索弹出面板高度与底部跳转入口位置恒定不变（不抖动）
	if err := tt.Click(i18n.T(i18n.LangZH, "search.global_label")); err != nil {
		t.Fatalf("点击顶栏全局搜索框失败: %v", err)
	}
	tt.Type("绝对不存在词")
	jumpTop0, ok0 := tt.Find("在「物品」中搜索")
	jumpEnd0, okEnd0 := tt.Find("在「版本对比」中搜索")
	if !ok0 || !okEnd0 {
		t.Fatalf("0 命中时底部跳转入口未显示: %v", tt.Texts())
	}

	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(i18n.T(i18n.LangZH, "search.global_label")); err != nil {
		t.Fatal(err)
	}
	tt.Type("目标跃迁水晶")
	jumpTop1, ok1 := tt.Find("在「物品」中搜索")
	jumpEnd1, okEnd1 := tt.Find("在「版本对比」中搜索")
	if !ok1 || !okEnd1 {
		t.Fatalf("1 命中时底部跳转入口未显示: %v", tt.Texts())
	}

	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(i18n.T(i18n.LangZH, "search.global_label")); err != nil {
		t.Fatal(err)
	}
	tt.Type("以太测试")
	if len(app.globalSearchRes.Hits) != 50 || app.globalSearchRes.TotalMatched != 145 || !tt.HasText("显示前 50 / 共 145 项") {
		t.Fatalf("全局搜索应展示前 50 项并提示总数: len=%d total=%d texts=%v", len(app.globalSearchRes.Hits), app.globalSearchRes.TotalMatched, tt.Texts())
	}
	jumpTop50, ok50 := tt.Find("在「物品」中搜索")
	jumpEnd50, okEnd50 := tt.Find("在「版本对比」中搜索")
	if !ok50 || !okEnd50 {
		t.Fatalf("50 命中时底部跳转入口被挤出或不可见: %v", tt.Texts())
	}
	if math.Abs(float64(jumpTop0.Y-jumpTop1.Y)) > 1 || math.Abs(float64(jumpTop0.Y-jumpTop50.Y)) > 1 ||
		math.Abs(float64(jumpEnd0.Y-jumpEnd1.Y)) > 1 || math.Abs(float64(jumpEnd0.Y-jumpEnd50.Y)) > 1 {
		t.Fatalf("不同结果数量下全局搜索面板底部位置发生抖动: 0=%+v/%+v 1=%+v/%+v 50=%+v/%+v",
			jumpTop0, jumpEnd0, jumpTop1, jumpEnd1, jumpTop50, jumpEnd50)
	}

	// 验证中部 50 条结果区域可独立滚动，同时底部跳转按钮位置不受滚动影响
	tt.Scroll(jumpTop50.X+40, jumpTop50.Y-80, 0, 240)
	if app.globalSearchScroll.Y <= 0 {
		t.Fatalf("期望全局搜索中间结果区可内部滚动，实际 Scroll.Y=%v", app.globalSearchScroll.Y)
	}
	jumpEndAfterScroll, _ := tt.Find("在「版本对比」中搜索")
	if math.Abs(float64(jumpEndAfterScroll.Y-jumpEnd50.Y)) > 1 {
		t.Fatalf("滚动中部结果后底部跳转入口位置不应改变: before=%+v after=%+v", jumpEnd50, jumpEndAfterScroll)
	}

	// 2. 通过全局搜索跳转到第 2 页任务 #65535，验证左侧任务列表切换至第 2 页并滚动定位到该任务
	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(i18n.T(i18n.LangZH, "search.global_label")); err != nil {
		t.Fatal(err)
	}
	tt.Type("目标第2页任务")
	tt.Key(0, ui.KeyEnter)
	if app.tab != tabQuests || app.selectedQuestID != 65535 || app.questQuery.Page != 2 {
		t.Fatalf("全局搜索跳转后任务左侧分页未同步定位: tab=%v qid=%d page=%d", app.tab, app.selectedQuestID, app.questQuery.Page)
	}
	qRowRect, okQRow := tt.Find("#65535")
	if !okQRow || qRowRect.X >= app.sidebarW+app.listW || qRowRect.Y < 48 || qRowRect.Y+qRowRect.H > 768 {
		t.Fatalf("跳转后左侧任务列表未定位显示 #65535: ok=%v rect=%+v", okQRow, qRowRect)
	}

	// 3. 在物品页故意保留冲突搜索词后，从任务奖励点击跳转第 2 页物品 #55，验证清空冲突筛选、翻页并滚动定位到 #55
	app.itemQuery.Q = "仅匹配不存在的词"
	app.refreshItemList()
	if err := tt.Click("目标跃迁水晶"); err != nil {
		t.Fatalf("点击任务奖励物品跳转失败: %v", err)
	}
	if app.tab != tabWorkbench || app.selectedItemID != 55 || app.itemQuery.Page != 2 || app.itemQuery.Q != "" {
		t.Fatalf("从任务跳转物品后左侧列表未正确翻页定位: tab=%v item=%d page=%d q=%q", app.tab, app.selectedItemID, app.itemQuery.Page, app.itemQuery.Q)
	}
	if app.itemListScroll.Y <= 0 {
		t.Fatalf("跳转到第 2 页末尾附近物品 #55 时左侧列表应自动滚动将其显示在视野内: scrollY=%v", app.itemListScroll.Y)
	}
	itemRowRect, okItemRow := tt.Find("#55")
	if !okItemRow || itemRowRect.X >= app.sidebarW+app.listW || itemRowRect.Y < 160 || itemRowRect.Y+itemRowRect.H > 728 {
		t.Fatalf("跳转后左侧物品列表行 #55 未滚动至可见区域: ok=%v rect=%+v", okItemRow, itemRowRect)
	}

	// 4. 在物品详情页内点击制作材料 #60（同样位于第 2 页末尾），验证页内联动跳转同样自动定位左侧列表
	if err := tt.Click("目标关联材料"); err != nil {
		t.Fatalf("点击配方材料联动跳转失败: %v", err)
	}
	if app.selectedItemID != 60 || app.itemQuery.Page != 2 || app.itemListScroll.Y <= 0 {
		t.Fatalf("物品详情内联动跳转未定位左侧列表 #60: item=%d page=%d scrollY=%v", app.selectedItemID, app.itemQuery.Page, app.itemListScroll.Y)
	}
}

func makeTestPortableZip(t *testing.T, exePayload []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("XIV Data Viewer.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(exePayload); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func makeTestPEBytes(tag string) []byte {
	b := make([]byte, 128)
	b[0] = 'M'
	b[1] = 'Z'
	copy(b[2:], tag)
	return b
}

func newReadyUpdateTestApp(t *testing.T, dataDir, appVer string) *App {
	t.Helper()
	app := newZHApp(t, dataDir)
	app.appVersion = appVer
	svc := gamedata.NewService(&gamedata.Store{PrimaryVersion: "2026.11.01"})
	st := svc.GetSystemStatus()
	app.service = svc
	app.status = &st
	app.mode = modeReady
	app.tab = tabWorkbench
	return app
}

func TestManualCheckUpdateMenuAndModalFlows(t *testing.T) {
	installDir := t.TempDir()
	exePath := filepath.Join(installDir, "XIV Data Viewer.exe")
	oldPE := makeTestPEBytes("v2.1.0")
	if err := os.WriteFile(exePath, oldPE, 0o755); err != nil {
		t.Fatal(err)
	}

	newPE := makeTestPEBytes("v2.2.0")
	zipBytes, zipSHA := makeTestPortableZip(t, newPE)
	assetName := "xiv-data-viewer-portable-2.2.0-windows-amd64.zip"

	var mode string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/InfSein/xiv-data-viewer/releases/latest":
			switch mode {
			case "error":
				w.WriteHeader(http.StatusInternalServerError)
			case "latest":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"tag_name":     "v2.1.0",
					"name":         "XIV Data Viewer v2.1.0",
					"html_url":     srv.URL + "/releases/tag/v2.1.0",
					"published_at": "2026-11-24T10:00:00Z",
					"assets": []map[string]any{{
						"name":                 "xiv-data-viewer-portable-2.1.0-windows-amd64.zip",
						"size":                 len(zipBytes),
						"browser_download_url": srv.URL + "/dl/" + assetName,
						"digest":               "sha256:" + zipSHA,
					}},
				})
			case "available":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"tag_name":     "v2.2.0",
					"name":         "XIV Data Viewer v2.2.0",
					"body":         "## 更新内容\n- 支持应用内置自动更新",
					"html_url":     srv.URL + "/releases/tag/v2.2.0",
					"published_at": "2026-11-25T12:00:00Z",
					"assets": []map[string]any{{
						"name":                 assetName,
						"size":                 len(zipBytes),
						"browser_download_url": srv.URL + "/dl/" + assetName,
						"digest":               "sha256:" + zipSHA,
					}},
				})
			}
		case "/dl/" + assetName:
			_, _ = w.Write(zipBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	isDev := false
	client := updater.NewClient(updater.Options{
		CurrentVersion: "2.1.0",
		OS:             "windows",
		Arch:           "amd64",
		ExePath:        exePath,
		IsDev:          &isDev,
		APIBaseURL:     srv.URL,
		WebBaseURL:     srv.URL,
		HTTPClient:     srv.Client(),
	})

	dataDir := t.TempDir()
	app := newReadyUpdateTestApp(t, dataDir, "2.1.0")
	defer app.Close()
	app.SetUpdaterClient(client)

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 验证左上角品牌菜单包含「检查更新...」
	if err := tt.Click(appBrandTitle); err != nil {
		t.Fatalf("点击左上角标题打开菜单失败: %v", err)
	}
	if !slices.Contains(tt.Menu(), "检查更新...") {
		t.Fatalf("左上角应用菜单缺少「检查更新...」: %v", tt.Menu())
	}

	// 2. 已是最新版本场景：主动检查应明确告知用户
	mode = "latest"
	if err := tt.ChooseMenuItem("检查更新..."); err != nil {
		t.Fatalf("点击菜单项「检查更新...」失败: %v", err)
	}
	if !app.updateModalOpen || app.updateState != updateUpToDate || !tt.HasText("已是最新版本") {
		t.Fatalf("期望弹窗提示已是最新版本，open=%v state=%v texts=%v", app.updateModalOpen, app.updateState, tt.Texts())
	}

	// 3. 主动检查出错场景：点击「重新检查」并模拟服务端 500，应明确向用户提示错误
	mode = "error"
	if err := tt.Click("重新检查"); err != nil {
		t.Fatalf("点击「重新检查」失败: %v", err)
	}
	if app.updateState != updateError || !tt.HasText("检查或更新失败") {
		t.Fatalf("主动检查出错应告知用户，state=%v texts=%v", app.updateState, tt.Texts())
	}

	// 4. 发现新版本场景：展示版本与更新说明 -> 点击「下载并更新」 -> 替换并提示重启，同时左下角展示重启按钮
	mode = "available"
	if err := tt.Click("重试"); err != nil {
		t.Fatalf("点击「重试」失败: %v", err)
	}
	if app.updateState != updateAvailable || !tt.HasText("发现新版本 v2.2.0") || !tt.HasText("支持应用内置自动更新") {
		t.Fatalf("期望展示新版本详情与说明，state=%v texts=%v", app.updateState, tt.Texts())
	}

	if err := tt.Click("下载并更新"); err != nil {
		t.Fatalf("点击「下载并更新」失败: %v", err)
	}
	if app.updateState != updateReady || !tt.HasText("新版本 v2.2.0 已准备就绪") || !tt.HasText("立即重启") {
		t.Fatalf("下载替换后期望处于就绪态并提示立即重启，state=%v texts=%v", app.updateState, tt.Texts())
	}
	gotExe, _ := os.ReadFile(exePath)
	if !bytes.Equal(gotExe, newPE) {
		t.Fatalf("磁盘上的可执行文件未替换为新版本")
	}

	// 关闭弹窗后，左下角应存在重启提醒按钮
	if err := tt.Click("稍后重启"); err != nil {
		t.Fatalf("点击「稍后重启」失败: %v", err)
	}
	if app.updateModalOpen {
		t.Fatalf("关闭更新弹窗后 updateModalOpen 应为 false")
	}
	restartRect, okRestart := tt.Find("重新启动以更新")
	if !okRestart || restartRect.X >= app.sidebarW || restartRect.Y < 450 {
		t.Fatalf("左下角应展示「重新启动以更新」按钮，ok=%v rect=%+v", okRestart, restartRect)
	}
	if err := tt.Click("重新启动以更新"); err != nil {
		t.Fatalf("点击左下角重启按钮失败: %v", err)
	}
	if !app.relaunchRequested {
		t.Fatalf("点击左下角重启按钮后应触发 Relaunch")
	}
}

func TestSilentStartupUpdateSuccessAndSilentRollback(t *testing.T) {
	// 子用例 1：静默更新过程中出错（哈希不匹配/网络异常）时静默中断回滚，不打扰用户
	t.Run("silent error rolls back without disturbing user", func(t *testing.T) {
		installDir := t.TempDir()
		exePath := filepath.Join(installDir, "XIV Data Viewer.exe")
		oldPE := makeTestPEBytes("v2.1.0")
		if err := os.WriteFile(exePath, oldPE, 0o755); err != nil {
			t.Fatal(err)
		}

		zipBytes, _ := makeTestPortableZip(t, makeTestPEBytes("corrupt-v2.2.0"))
		assetName := "xiv-data-viewer-portable-2.2.0-windows-amd64.zip"

		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/InfSein/xiv-data-viewer/releases/latest":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"tag_name": "v2.2.0",
					"assets": []map[string]any{{
						"name":                 assetName,
						"size":                 len(zipBytes),
						"browser_download_url": srv.URL + "/dl/" + assetName,
						"digest":               "sha256:" + strings.Repeat("f", 64),
					}},
				})
			case "/dl/" + assetName:
				_, _ = w.Write(zipBytes)
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		isDev := false
		client := updater.NewClient(updater.Options{
			CurrentVersion: "2.1.0",
			OS:             "windows",
			Arch:           "amd64",
			ExePath:        exePath,
			IsDev:          &isDev,
			APIBaseURL:     srv.URL,
			WebBaseURL:     srv.URL,
			HTTPClient:     srv.Client(),
		})

		app := newReadyUpdateTestApp(t, t.TempDir(), "2.1.0")
		defer app.Close()
		app.SetUpdaterClient(client)
		app.StartStartupUpdateCheck()

		tt := ui.NewTester(app.View, 1366, 768)
		if app.updateModalOpen || app.updateState != updateIdle || app.updateErrorMsg != "" {
			t.Fatalf("静默更新失败应静默中断且不弹窗: modal=%v state=%v err=%q", app.updateModalOpen, app.updateState, app.updateErrorMsg)
		}
		if tt.HasText("检查或更新失败") || tt.HasText("重新启动以更新") {
			t.Fatalf("静默失败不应向界面展示任何错误或重启按钮: %v", tt.Texts())
		}
		gotExe, _ := os.ReadFile(exePath)
		if !bytes.Equal(gotExe, oldPE) {
			t.Fatalf("静默失败回滚后应保留原可执行文件")
		}
	})

	// 子用例 2：静默更新成功下载并替换后，在左下角显示重启按钮
	t.Run("silent success shows bottom-left restart button", func(t *testing.T) {
		installDir := t.TempDir()
		exePath := filepath.Join(installDir, "XIV Data Viewer.exe")
		oldPE := makeTestPEBytes("v2.1.0")
		if err := os.WriteFile(exePath, oldPE, 0o755); err != nil {
			t.Fatal(err)
		}

		newPE := makeTestPEBytes("v2.2.0")
		zipBytes, zipSHA := makeTestPortableZip(t, newPE)
		assetName := "xiv-data-viewer-portable-2.2.0-windows-amd64.zip"

		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/InfSein/xiv-data-viewer/releases/latest":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"tag_name": "v2.2.0",
					"assets": []map[string]any{{
						"name":                 assetName,
						"size":                 len(zipBytes),
						"browser_download_url": srv.URL + "/dl/" + assetName,
						"digest":               "sha256:" + zipSHA,
					}},
				})
			case "/dl/" + assetName:
				_, _ = w.Write(zipBytes)
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		isDev := false
		client := updater.NewClient(updater.Options{
			CurrentVersion: "2.1.0",
			OS:             "windows",
			Arch:           "amd64",
			ExePath:        exePath,
			IsDev:          &isDev,
			APIBaseURL:     srv.URL,
			WebBaseURL:     srv.URL,
			HTTPClient:     srv.Client(),
		})

		app := newReadyUpdateTestApp(t, t.TempDir(), "2.1.0")
		defer app.Close()
		app.SetUpdaterClient(client)
		app.StartStartupUpdateCheck()

		tt := ui.NewTester(app.View, 1366, 768)
		if app.updateModalOpen || app.updateState != updateReady || app.updateReadyVer != "2.2.0" {
			t.Fatalf("静默更新完成后应静默处于就绪态: modal=%v state=%v ver=%q", app.updateModalOpen, app.updateState, app.updateReadyVer)
		}
		gotExe, _ := os.ReadFile(exePath)
		if !bytes.Equal(gotExe, newPE) {
			t.Fatalf("静默更新完成后磁盘文件应已替换")
		}
		if !tt.HasText("应用更新已就绪") || !tt.HasText("v2.2.0") || !tt.HasText("重新启动以更新") {
			t.Fatalf("左下角应显示重启提醒按钮: %v", tt.Texts())
		}
		rect, ok := tt.Find("重新启动以更新")
		if !ok || rect.X >= app.sidebarW || rect.Y < 450 {
			t.Fatalf("重启提醒按钮应位于左下角侧边栏区域: ok=%v rect=%+v", ok, rect)
		}
	})
}

func TestChangelogSidebarButtonAndStructuredView(t *testing.T) {
	dataDir := t.TempDir()
	app := newReadyUpdateTestApp(t, dataDir, "2.1.0")
	defer app.Close()

	// 先校验默认磁盘回退路径可正常加载仓库内的真实三语更新日志
	if doc, ok := app.loadCurrentChangelog(); !ok || len(doc.NonEmptyReleases()) == 0 {
		t.Fatalf("默认仓库更新日志回退加载失败: ok=%v err=%q", ok, app.changelogErr)
	}

	// 使用固定测试夹具隔离当前 Unreleased/已发布状态，避免随后续发版归档而失效
	app.SetChangelogFS(fstest.MapFS{
		"CHANGELOG.md": &fstest.MapFile{
			Data: []byte("# 更新日志\n\n## [Unreleased]\n\n### 新增\n- **更新日志页面**：在侧边栏「设置」上方新增「更新日志」页面，按版本与分类清晰展示各版本更新内容，支持搜索与筛选。\n\n## [2.1.0] - 2026-11-24\n\n### 新增\n- **任务与理符查询**：新增「任务」与「理符」专属页面。\n\n### 修复\n- **图标边缘显示修复**：修复部分游戏图标边缘白边问题。\n\n## [2.0.0] - 2026-11-24\n\n### 新增\n- 首个正式桌面版本发布。\n"),
		},
		"docs/CHANGELOG.en.md": &fstest.MapFile{
			Data: []byte("# Changelog\n\n## [Unreleased]\n\n### Added\n- **Changelog Page**: Added a Changelog page above Settings.\n\n## [2.1.0] - 2026-11-24\n\n### Added\n- **Quests & Leves Views**: Added dedicated Quests and Leves pages.\n\n### Fixed\n- **Icon Edge Halos**: Fixed bright white halos.\n\n## [2.0.0] - 2026-11-24\n\n### Added\n- First desktop release.\n"),
		},
		"docs/CHANGELOG.ja.md": &fstest.MapFile{
			Data: []byte("# 更新履歴\n\n## [Unreleased]\n\n### 新規追加\n- **更新履歴ページ**：設定の上に更新履歴ページを追加しました。\n\n## [2.1.0] - 2026-11-24\n\n### 新規追加\n- **クエスト・リーヴ閲覧機能**：専用ページを追加しました。\n\n### 修正\n- **アイコン縁の白浮き修正**：縁の白浮きを修正しました。\n\n## [2.0.0] - 2026-11-24\n\n### 新規追加\n- 最初のデスクトップ版をリリース。\n"),
		},
	})

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 验证侧栏「更新日志」按钮存在且严格位于「设置」按钮正上方
	clBtnRect, okCL := tt.Find("更新日志")
	setBtnRect, okSet := tt.Find("设置")
	diffBtnRect, okDiff := tt.Find("版本对比")
	if !okCL || !okSet || !okDiff {
		t.Fatalf("侧栏缺少预期导航项: changelog=%v settings=%v diff=%v texts=%v", okCL, okSet, okDiff, tt.Texts())
	}
	if clBtnRect.Y >= setBtnRect.Y || clBtnRect.Y <= diffBtnRect.Y {
		t.Fatalf("「更新日志」应位于「版本对比」下方、「设置」上方: diff=%+v changelog=%+v settings=%+v", diffBtnRect, clBtnRect, setBtnRect)
	}

	// 2. 点击「更新日志」打开页面并验证结构化内容展示
	if err := tt.Click("更新日志"); err != nil {
		t.Fatalf("点击侧栏「更新日志」失败: %v", err)
	}
	if app.tab != tabChangelog {
		t.Fatalf("期望跳转至 tabChangelog，实际: %v", app.tab)
	}
	for _, want := range []string{"更新日志", "当前 v2.1.0", "开发中版本", "v2.1.0", "当前版本", "v2.0.0", "2026-11-24", "任务与理符查询"} {
		if !tt.HasText(want) {
			t.Fatalf("更新日志页面缺少预期结构化内容 %q: %v", want, tt.Texts())
		}
	}

	// 验证标题在正文上方且均实际绘制在可视区域内（分层排版而非纯文本拼接）
	titleRect, okTitle := tt.Find("更新日志页面")
	wantBody := "在侧边栏「设置」上方新增「更新日志」页面，按版本与分类清晰展示各版本更新内容，支持搜索与筛选。"
	bodyRect, okBody := tt.Find(wantBody)
	if !okTitle || !okBody || titleRect.W <= 0 || bodyRect.W <= 0 || titleRect.Y >= bodyRect.Y {
		t.Fatalf("条目加粗标题与正文应分上下行结构化排版: title=%+v (%v) body=%+v (%v)", titleRect, okTitle, bodyRect, okBody)
	}

	// 3. 验证分类筛选与页内搜索（Ctrl+F）及重置
	if err := tt.Click("修复"); err != nil {
		t.Fatalf("点击「修复」分类筛选失败: %v", err)
	}
	if app.changelogCatFilter != "fixed" || !tt.HasText("图标边缘显示修复") || tt.HasText("任务与理符查询") {
		t.Fatalf("「修复」分类筛选未生效: filter=%q texts=%v", app.changelogCatFilter, tt.Texts())
	}

	tt.Key(ui.Cmd, ui.KeyF)
	if !tt.Focused(i18n.T(i18n.LangZH, "changelog.search_label")) {
		t.Fatalf("Ctrl+F 应聚焦更新日志页内搜索框")
	}
	tt.Type("不存在的版本特性")
	if !tt.HasText(i18n.T(i18n.LangZH, "changelog.empty_title")) {
		t.Fatalf("搜索无结果时应展示空状态提示: %v", tt.Texts())
	}
	if err := tt.Click("重置筛选"); err != nil {
		t.Fatalf("点击空状态「重置筛选」失败: %v", err)
	}
	if app.hasChangelogFilter() || !tt.HasText("任务与理符查询") {
		t.Fatalf("重置筛选后应恢复完整更新日志展示")
	}

	// 4. 验证版本折叠按钮交互
	if err := tt.Click("收起此版本"); err != nil {
		t.Fatalf("收起版本卡片失败: %v", err)
	}
	if tt.HasText("更新日志页面") || tt.HasText(wantBody) {
		t.Fatalf("收起 Unreleased 卡片后应隐藏其内容")
	}
	if err := tt.Click("展开此版本"); err != nil {
		t.Fatalf("展开版本卡片失败: %v", err)
	}
	if !tt.HasText(wantBody) {
		t.Fatalf("展开 Unreleased 卡片后应恢复显示内容")
	}

	// 5. 切换日语 / 英语，验证更新日志内容自动切换为 docs/CHANGELOG.ja.md 与 docs/CHANGELOG.en.md
	app.SetLanguage(i18n.LangJA)
	tt.Frame()
	if !tt.HasText("更新履歴") || !tt.HasText("クエスト・リーヴ閲覧機能") {
		t.Fatalf("日语模式下未展示日文更新日志内容: %v", tt.Texts())
	}

	app.SetLanguage(i18n.LangEN)
	tt.Frame()
	if !tt.HasText("Changelog") || !tt.HasText("Quests & Leves Views") {
		t.Fatalf("英语模式下未展示英文更新日志内容: %v", tt.Texts())
	}

	// 6. 验证最小视口 1024×640 及深色主题下渲染正常
	app.SetThemeMode("dark")
	ttMin := ui.NewTester(app.View, 1024, 640)
	if !ttMin.HasText("Changelog") || !ttMin.HasText("v2.1.0") {
		t.Fatalf("最小视口深色模式下更新日志渲染异常: %v", ttMin.Texts())
	}
}

func TestHistoryRecentAndFavoritesWorkflow(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	// 准备包含 520 个物品、2 个任务、2 个理符、2 个成就的测试仓库，覆盖 500 条上限与分页
	itemsPrimary := make(map[int]sqpack.Row, 520)
	itemSummaries := make([]gamedata.ItemSummary, 0, 520)
	itemSummaryByID := make(map[int]gamedata.ItemSummary, 520)
	for i := 1; i <= 520; i++ {
		var name string
		switch i {
		case 2:
			name = "精金矿"
		case 3:
			name = "星光苹果馅饼"
		case 20:
			name = "秘银矿"
		default:
			name = "测试素材-" + strconv.Itoa(i)
		}
		row := make(sqpack.Row, 95)
		row[9] = name
		row[8] = "测试描述：" + name
		row[20] = 99
		row[26] = 10
		itemsPrimary[i] = row
		sum := gamedata.ItemSummary{
			ID:           i,
			NameCn:       name,
			NameJa:       "アイテム-" + strconv.Itoa(i),
			NameEn:       "Item-" + strconv.Itoa(i),
			CategoryName: "食材",
			ItemLevel:    100 + i,
		}
		if i == 3 {
			sum.NameJa = "アップルパイ"
			sum.NameEn = "Apple Tart"
			sum.CategoryName = "食品"
		}
		itemSummaries = append(itemSummaries, sum)
		itemSummaryByID[i] = sum
	}

	quests := []gamedata.QuestEntry{
		{
			ID:            65536,
			InternalID:    "Msq001",
			NameCn:        "启程之日",
			NameJa:        "旅立ちの日",
			NameEn:        "Coming to Gridania",
			IconID:        gamedata.QuestIconMSQ,
			SectionID:     0,
			SectionName:   "主线任务",
			GenreName:     "第七星历主线",
			ExpansionName: "重生之境",
			Level:         15,
			IsMSQ:         true,
			IssuerName:    "缪恩",
			GilReward:     600,
			ExpReward:     2400,
		},
		{
			ID:          65537,
			InternalID:  "Sub002",
			NameCn:      "甜品试吃会",
			NameJa:      "試食会",
			NameEn:      "Sweet Tasting",
			IconID:      gamedata.QuestIconSide,
			SectionID:   1,
			SectionName: "支线任务",
			GenreName:   "乌尔达哈支线",
			Level:       20,
			GilReward:   300,
		},
	}
	questByID := map[int]gamedata.QuestEntry{
		65536: quests[0],
		65537: quests[1],
	}

	leves := []gamedata.LeveEntry{
		{
			ID:                 201,
			NameCn:             "大量制作：甜点委托",
			NameJa:             "大口依頼：菓子",
			NameEn:             "Bulk Dessert",
			LeveKind:           "craft",
			LeveKindLabel:      "制作理符",
			AssignmentTypeName: "大量筹备",
			ClassJobLevel:      98,
			TownName:           "图莱尤拉",
			ExpReward:          850000,
			GilReward:          2400,
			TurnInItems: []gamedata.LeveTurnInItem{{
				ItemID:   3,
				ItemName: "星光苹果馅饼",
				Count:    3,
			}},
		},
		{
			ID:                 202,
			NameCn:             "采集委托：秘银矿",
			NameJa:             "採集依頼：ミスリル",
			NameEn:             "Fieldwork Mythril",
			LeveKind:           "gathering",
			LeveKindLabel:      "采集理符",
			AssignmentTypeName: "筹备",
			ClassJobLevel:      45,
			TownName:           "沙之家",
			ExpReward:          12000,
		},
	}
	leveByID := map[int]gamedata.LeveEntry{
		201: leves[0],
		202: leves[1],
	}

	achs := []gamedata.AchievementEntry{
		{
			ID:               1001,
			NameCn:           "胜利的荣光1",
			NameJa:           "勝利の栄光1",
			NameEn:           "To Crush Your Enemies I",
			DescriptionCn:    "在副本中取得胜利",
			Points:           10,
			KindID:           1,
			KindName:         "战斗",
			CategoryID:       1,
			CategoryName:     "整体",
			TitleID:          10,
			TitleMasculineCn: "沙场勇将",
		},
		{
			ID:               1002,
			NameCn:           "美食鉴赏家",
			NameJa:           "美食家",
			NameEn:           "Gourmet",
			DescriptionCn:    "品尝大量料理",
			Points:           20,
			KindID:           2,
			KindName:         "道具",
			CategoryID:       2,
			CategoryName:     "制作",
			RewardItemID:     3,
			RewardItemNameCn: "星光苹果馅饼",
		},
	}

	store := &gamedata.Store{
		PrimaryVersion:  "2026.11.01",
		ItemsPrimary:    itemsPrimary,
		ItemSummaries:   itemSummaries,
		ItemSummaryByID: itemSummaryByID,
		Quests:          quests,
		QuestByID:       questByID,
		Leves:           leves,
		LeveByID:        leveByID,
		Achievements:    achs,
		AchievementByID: map[int]gamedata.AchievementEntry{1001: achs[0], 1002: achs[1]},
		TotalTitles:     1,
	}

	svc := gamedata.NewService(store)
	st := svc.GetSystemStatus()
	langCache := &langSessionCache{
		service: svc,
		status:  st,
	}
	// 通过 activateLangState 模拟初始载入默认选中，不应污染「最近」记录
	app.mode = modeReady
	app.tab = tabWorkbench
	app.activateLangState(langCache, false)
	if len(app.records.Recent) != 0 {
		t.Fatalf("启动默认初始化选中不应写入最近记录，实际 len=%d", len(app.records.Recent))
	}

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 验证侧栏存在「历史」分组及下属「最近」「收藏」两个菜单，且位于「成就」之下、「对比」之上
	histHdr, okHist := tt.Find("历史")
	recentNav, okRec := tt.Find("最近")
	favNav, okFav := tt.Find("收藏")
	achNav, okAch := tt.Find("成就")
	cmpHdr, okCmp := tt.Find("对比")
	if !okHist || !okRec || !okFav || !okAch || !okCmp {
		t.Fatalf("侧栏缺少历史菜单组或最近/收藏菜单: hist=%v rec=%v fav=%v ach=%v cmp=%v", okHist, okRec, okFav, okAch, okCmp)
	}
	if !(achNav.Y < histHdr.Y && histHdr.Y < recentNav.Y && recentNav.Y < favNav.Y && favNav.Y < cmpHdr.Y) {
		t.Fatalf("侧栏「历史」菜单位置异常: ach=%+v hist=%+v rec=%+v fav=%+v cmp=%+v", achNav, histHdr, recentNav, favNav, cmpHdr)
	}

	// 2. 在物品/任务/理符/成就界面实际点击浏览与收藏按钮
	if err := tt.Click("星光苹果馅饼"); err != nil {
		t.Fatalf("点击物品行失败: %v", err)
	}
	if err := tt.Click("精金矿"); err != nil {
		t.Fatalf("点击物品行失败: %v", err)
	}
	// 打开多个物品后，物品详情顶部栏不应再显示旧版「最近」快捷标签，而是常驻显示物品 ID 与基本信息
	if !tt.HasText("物品 #2") || !tt.HasText("· 食材 · 品级 102") {
		t.Fatalf("物品详情顶部栏应常驻显示物品 ID 与基本信息: %v", tt.Texts())
	}
	if r, ok := tt.Find("星光苹果馅饼"); ok && r.X >= app.sidebarW+app.listW {
		t.Fatalf("物品详情顶部栏不应再显示旧版最近浏览按钮: rect=%+v", r)
	}
	if err := tt.Click("星光苹果馅饼"); err != nil {
		t.Fatalf("点击物品行失败: %v", err)
	}
	if !tt.HasText("物品 #3") || !tt.HasText("· 食品 · 品级 103") {
		t.Fatalf("物品详情顶部栏应常驻显示当前物品 #3 ID 与基本信息: %v", tt.Texts())
	}
	if err := tt.Click("加入收藏"); err != nil {
		t.Fatalf("点击物品加入收藏失败: %v", err)
	}
	if !app.isFavorite(config.RecordKindItem, 3) || !tt.HasText("已收藏") {
		t.Fatalf("物品 #3 收藏状态未更新")
	}

	// 切换至任务页，点击任务并收藏
	if err := tt.Click("任务"); err != nil {
		t.Fatalf("切换至任务页失败: %v", err)
	}
	if err := tt.Click("启程之日"); err != nil {
		t.Fatalf("点击任务行失败: %v", err)
	}
	if err := tt.Click("加入收藏"); err != nil {
		t.Fatalf("点击任务加入收藏失败: %v", err)
	}
	if !app.isFavorite(config.RecordKindQuest, 65536) {
		t.Fatalf("任务 #65536 收藏状态未更新")
	}

	// 切换至理符页，点击理符并收藏
	if err := tt.Click("理符"); err != nil {
		t.Fatalf("切换至理符页失败: %v", err)
	}
	if err := tt.Click("大量制作：甜点委托"); err != nil {
		t.Fatalf("点击理符行失败: %v", err)
	}
	if err := tt.Click("加入收藏"); err != nil {
		t.Fatalf("点击理符加入收藏失败: %v", err)
	}
	if !app.isFavorite(config.RecordKindLeve, 201) {
		t.Fatalf("理符 #201 收藏状态未更新")
	}

	// 切换至成就页，点击成就星标按钮收藏「美食鉴赏家」
	if err := tt.Click("成就"); err != nil {
		t.Fatalf("切换至成就页失败: %v", err)
	}
	if err := tt.Click("收藏 美食鉴赏家"); err != nil {
		t.Fatalf("点击成就收藏按钮失败: %v", err)
	}
	if !app.isFavorite(config.RecordKindAchievement, 1002) {
		t.Fatalf("成就 #1002 收藏状态未更新")
	}

	// 3. 验证「最近」界面：记录了刚刚打开过的物品/任务/理符，点击切换详情，且支持类型筛选与 Ctrl+F 搜索
	if err := tt.Click("最近"); err != nil {
		t.Fatalf("切换至最近页面失败: %v", err)
	}
	if app.tab != tabRecent {
		t.Fatalf("期望位于 tabRecent，实际: %v", app.tab)
	}
	if len(app.records.Recent) != 4 {
		t.Fatalf("期望最近记录 4 条（去重后的物品/任务/理符），实际: %d (%+v)", len(app.records.Recent), app.records.Recent)
	}
	// 默认选中最新打开的理符 #201
	if app.recentSelectedKind != config.RecordKindLeve || app.recentSelectedID != 201 || !tt.HasText("随机奖励道具池") && !tt.HasText("需要交纳道具") {
		t.Fatalf("最近页面应默认展示最新打开的理符详情: kind=%s id=%d", app.recentSelectedKind, app.recentSelectedID)
	}
	// 点击列表中的「启程之日」，右侧应展示任务详情且不打乱最近列表顺序
	if err := tt.Click("启程之日"); err != nil {
		t.Fatalf("在最近页面点击任务失败: %v", err)
	}
	if app.recentSelectedKind != config.RecordKindQuest || app.recentSelectedID != 65536 || !tt.HasText("缪恩") {
		t.Fatalf("最近页面切换查看任务详情失败: kind=%s id=%d", app.recentSelectedKind, app.recentSelectedID)
	}
	if app.records.Recent[0].Kind != config.RecordKindLeve || app.records.Recent[0].ID != 201 {
		t.Fatalf("在最近页面内点选条目不应重排最近列表顺序: %+v", app.records.Recent)
	}
	// 点击在任务页打开按钮
	if err := tt.Click("在任务页打开"); err != nil {
		t.Fatalf("点击在任务页打开按钮失败: %v", err)
	}
	if app.tab != tabQuests || app.selectedQuestID != 65536 {
		t.Fatalf("点击在任务页打开未跳转到任务页: tab=%v qid=%d", app.tab, app.selectedQuestID)
	}

	// 4. 验证「最近」500 条上限与页内筛选/搜索/清空
	for i := 1; i <= 515; i++ {
		app.selectItem(i)
	}
	if len(app.records.Recent) != config.MaxRecentRecords {
		t.Fatalf("最近记录上限应严格为 %d，实际: %d", config.MaxRecentRecords, len(app.records.Recent))
	}
	if err := tt.Click("最近"); err != nil {
		t.Fatalf("回到最近页面失败: %v", err)
	}
	if !tt.HasText("500 / 500") {
		t.Fatalf("最近页面应显示 500 / 500 数量上限状态: %v", tt.Texts())
	}
	// 验证超出 500 条上限时最早的记录（如 #3）已被淘汰，而 #20（秘银矿）仍在列表中
	if slices.ContainsFunc(app.records.Recent, func(r config.HistoryRecord) bool {
		return r.Kind == config.RecordKindItem && r.ID == 3
	}) {
		t.Fatalf("超出 500 条上限后最早的物品 #3 应被淘汰")
	}
	tt.Key(ui.Cmd, ui.KeyF)
	if !tt.Focused("搜索最近记录") {
		t.Fatalf("Ctrl+F 未聚焦最近页内搜索框")
	}
	tt.Type("秘银矿")
	if !tt.HasText("秘银矿") {
		t.Fatalf("最近页面搜索过滤异常，未找到保留的 #20 秘银矿: %v", tt.Texts())
	}
	if r, ok := tt.Find("测试素材-515"); ok && r.X < app.sidebarW+app.listW {
		t.Fatalf("最近页面左侧列表过滤后不应仍显示被过滤条目测试素材-515: rect=%+v", r)
	}
	if err := tt.Click("重置"); err != nil {
		t.Fatalf("重置最近页面搜索失败: %v", err)
	}
	if err := tt.Click("清空"); err != nil {
		t.Fatalf("点击清空最近记录失败: %v", err)
	}
	if len(app.records.Recent) != 0 || !tt.HasText("暂无最近浏览记录") {
		t.Fatalf("清空最近记录后应展示空状态: len=%d texts=%v", len(app.records.Recent), tt.Texts())
	}

	// 5. 验证「收藏」界面内的 4 个 tab（物品/任务/理符/成就）与对应详情/表格视图
	if err := tt.Click("收藏"); err != nil {
		t.Fatalf("切换至收藏页面失败: %v", err)
	}
	if app.tab != tabFavorites {
		t.Fatalf("期望位于 tabFavorites，实际: %v", app.tab)
	}
	for _, wantTab := range []string{"收藏物品", "收藏任务", "收藏理符", "收藏成就"} {
		if _, ok := tt.Find(wantTab); !ok {
			t.Fatalf("收藏页面缺少 tab %q: %v", wantTab, tt.Texts())
		}
	}
	if app.favTab != "item" || !tt.HasText("星光苹果馅饼") || !tt.HasText("测试描述：星光苹果馅饼") {
		t.Fatalf("收藏物品 tab 未展示与物品页一致的列表与详情: favTab=%q texts=%v", app.favTab, tt.Texts())
	}

	if err := tt.Click("收藏任务"); err != nil {
		t.Fatalf("点击收藏任务 tab 失败: %v", err)
	}
	if app.favTab != "quest" || !tt.HasText("启程之日") || !tt.HasText("第七星历主线") {
		t.Fatalf("收藏任务 tab 未展示任务详情: favTab=%q texts=%v", app.favTab, tt.Texts())
	}

	if err := tt.Click("收藏理符"); err != nil {
		t.Fatalf("点击收藏理符 tab 失败: %v", err)
	}
	if app.favTab != "leve" || !tt.HasText("大量制作：甜点委托") || !tt.HasText("需要交纳道具") {
		t.Fatalf("收藏理符 tab 未展示理符详情: favTab=%q texts=%v", app.favTab, tt.Texts())
	}

	if err := tt.Click("收藏成就"); err != nil {
		t.Fatalf("点击收藏成就 tab 失败: %v", err)
	}
	if app.favTab != "achievement" || !tt.HasText("奖励称号 / 道具") || !tt.HasText("美食鉴赏家") || tt.HasText("胜利的荣光1") {
		t.Fatalf("收藏成就 tab 未展示与成就页一致的表格视图: favTab=%q texts=%v", app.favTab, tt.Texts())
	}

	// 验证取消收藏后立即更新收藏列表
	if err := tt.Click("取消收藏 美食鉴赏家"); err != nil {
		t.Fatalf("在收藏成就页面取消收藏失败: %v", err)
	}
	if app.isFavorite(config.RecordKindAchievement, 1002) || !tt.HasText("还没有收藏的成就") {
		t.Fatalf("取消唯一收藏成就后应显示空状态: fav=%v texts=%v", app.isFavorite(config.RecordKindAchievement, 1002), tt.Texts())
	}

	// 6. 验证多语言与最小视口 1024×640 深色模式渲染正常
	app.SetLanguage(i18n.LangEN)
	tt.Frame()
	if !tt.HasText("Favorites") || !tt.HasText("History") {
		t.Fatalf("英语模式下历史/收藏菜单文案未切换: %v", tt.Texts())
	}
	app.SetLanguage(i18n.LangJA)
	tt.Frame()
	if !tt.HasText("お気に入り") || !tt.HasText("履歴") {
		t.Fatalf("日语模式下历史/收藏菜单文案未切换: %v", tt.Texts())
	}

	app.SetLanguage(i18n.LangZH)
	app.favTab = "item"
	app.SetThemeMode("dark")
	ttMin := ui.NewTester(app.View, 1024, 640)
	if !ttMin.HasText("收藏") || !ttMin.HasText("星光苹果馅饼") {
		t.Fatalf("1024x640 深色模式下收藏页面渲染异常: %v", ttMin.Texts())
	}
	app.tab = tabRecent
	ttMin.Frame()
	if !ttMin.HasText("最近") {
		t.Fatalf("1024x640 深色模式下最近页面渲染异常: %v", ttMin.Texts())
	}

	// 7. 验证重启应用后从本地 history.json 恢复收藏名单
	app2 := newZHApp(t, dataDir)
	defer app2.Close()
	if !app2.isFavorite(config.RecordKindItem, 3) ||
		!app2.isFavorite(config.RecordKindQuest, 65536) ||
		!app2.isFavorite(config.RecordKindLeve, 201) ||
		app2.isFavorite(config.RecordKindAchievement, 1002) {
		t.Fatalf("重启后持久化收藏状态恢复不一致: %+v", app2.records.Favorites)
	}
	if len(app2.records.Recent) != 0 {
		t.Fatalf("重启后已清空的最近记录仍应为空: %+v", app2.records.Recent)
	}
}
