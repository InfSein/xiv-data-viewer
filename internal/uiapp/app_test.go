package uiapp

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/egoist/mygo/ui"
)

func TestFirstRunSetupAndSettingsFlow(t *testing.T) {
	dataDir := t.TempDir()
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
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
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
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
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
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
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
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

	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
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
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
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
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
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
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
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
	if got := formatQuestRowType(store.Quests[0]); got != "主线任务" {
		t.Fatalf("主线任务类型文本异常: got=%q", got)
	}
	if got := formatQuestRowType(store.Quests[1]); got != "日常任务" {
		t.Fatalf("日常任务类型文本异常: got=%q", got)
	}
	if got := formatQuestRowType(store.Quests[2]); got != "功能解锁" {
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
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
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
