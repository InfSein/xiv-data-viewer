package uiapp

import (
	"slices"
	"strings"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/egoist/mygo/ui"
)

func TestToolsMenuAndTextSearchUI(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	itemRow := make(sqpack.Row, 95)
	itemRow[9] = "光之战士之剑"
	itemRow[8] = "传说中的圣剑"
	itemRow[20] = 660

	store := &gamedata.Store{
		HasCN:         true,
		CNVersion:     "2026.11.01",
		AppLang:       i18n.LangZH,
		PrimaryLang:   sqpack.LangChineseSimplified,
		ItemsPrimary:  map[int]sqpack.Row{1001: itemRow},
		ItemSummaries: []gamedata.ItemSummary{{ID: 1001, NameCn: "光之战士之剑", CategoryName: "武器", ItemLevel: 660}},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			1001: {ID: 1001, NameCn: "光之战士之剑", CategoryName: "武器", ItemLevel: 660},
		},
		Quests: []gamedata.QuestEntry{{ID: 65536, InternalID: "Msq001_00001", NameCn: "光之启程"}},
		QuestByID: map[int]gamedata.QuestEntry{
			65536: {ID: 65536, InternalID: "Msq001_00001", NameCn: "光之启程"},
		},
	}
	svc := gamedata.NewService(store)

	longStory := strings.Repeat("这是一段非常长的剧情铺垫文本。", 20) + "关键线索ABCD藏在这里" + strings.Repeat("后续对话仍在继续推进。", 15)
	textIdx := gamedata.NewTextSearchIndexFromRecords("chs",
		[]gamedata.TextIndexSheetInput{
			{Name: "Item", IsLocalized: true, ColNames: []string{"Name", "Description"}},
			{Name: "quest/000/Msq001_00001", IsLocalized: true, ColNames: []string{"Key", "Text"}},
			{Name: "Addon", IsLocalized: true, ColNames: []string{"Text"}},
		},
		[]gamedata.TextIndexRowInput{
			{SheetIdx: 0, RowID: 1001, SubRowID: -1, ColIdx: 0, Text: "ABCD"},
			{SheetIdx: 0, RowID: 1001, SubRowID: -1, ColIdx: 1, Text: "AD"},
			{SheetIdx: 1, RowID: 1, SubRowID: -1, ColIdx: 1, Text: longStory},
			{SheetIdx: 2, RowID: 50, SubRowID: -1, ColIdx: 0, Text: "AxxxD 界面提示"},
			{SheetIdx: 2, RowID: 51, SubRowID: -1, ColIdx: 0, Text: "DCBA 无关顺序"},
		},
		nil, nil,
	)
	svc.SetTextSearchIndex("chs", textIdx)

	app.mode = modeReady
	app.tab = tabWorkbench
	app.activateLangState(&langSessionCache{
		service: svc,
		status:  svc.GetSystemStatus(),
	}, false)

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 验证侧栏「对比」已更名为「工具」，其下同时包含「版本对比」与「文本检索」
	toolsHdr, okTools := tt.Find("工具")
	diffNav, okDiff := tt.Find("版本对比")
	tsNav, okTS := tt.Find("文本检索")
	if !okTools || !okDiff || !okTS {
		t.Fatalf("侧栏「工具」菜单组或子项缺失: tools=%v diff=%v ts=%v texts=%v", okTools, okDiff, okTS, tt.Texts())
	}
	if slices.Contains(tt.Texts(), "对比") {
		t.Fatalf("旧菜单组名称「对比」不应继续存在")
	}
	if !(toolsHdr.Y < diffNav.Y && diffNav.Y < tsNav.Y) {
		t.Fatalf("侧栏「工具」下菜单顺序异常: tools=%.1f diff=%.1f ts=%.1f", toolsHdr.Y, diffNav.Y, tsNav.Y)
	}

	// 2. 点击「文本检索」进入页面并展示空输入引导与总文本统计
	if err := tt.Click("文本检索"); err != nil {
		t.Fatalf("点击「文本检索」导航失败: %v", err)
	}
	if app.tab != tabTextSearch {
		t.Fatalf("期望切换到 tabTextSearch，实际: %v", app.tab)
	}
	if !tt.HasText("检索游戏全部文本内容") || !tt.HasText("5 条文本 · 3 张表") {
		t.Fatalf("文本检索空引导或索引统计未渲染: %v", tt.Texts())
	}

	// 3. 模糊检索 AD：应处于模糊匹配模式，同时命中 AD、ABCD、AxxxD 与长剧情中的 ABCD，且 AD 排在 ABCD 前面
	app.textSearchQ = "AD"
	app.textSearchPageSize = 2
	app.textSearchPage = 1
	app.runTextSearch(false)
	tt.Frame()

	if app.textSearchRes.Mode != gamedata.TextMatchFuzzy {
		t.Fatalf("期望模糊模式，实际: %v", app.textSearchRes.Mode)
	}
	if app.textSearchRes.Total != 4 || app.textSearchRes.TotalPages != 2 || len(app.textSearchRes.Items) != 2 {
		t.Fatalf("模糊检索 AD 分页结果异常: total=%d pages=%d len=%d", app.textSearchRes.Total, app.textSearchRes.TotalPages, len(app.textSearchRes.Items))
	}
	if app.textSearchRes.Items[0].Text != "AD" || app.textSearchRes.Items[1].Text != "ABCD" {
		t.Fatalf("模糊检索 AD 匹配度排序异常: [0]=%q [1]=%q", app.textSearchRes.Items[0].Text, app.textSearchRes.Items[1].Text)
	}
	if !tt.HasText("模糊匹配") {
		t.Fatalf("界面应显示「模糊匹配」模式标签: %v", tt.Texts())
	}

	// 翻到第 2 页
	if err := tt.Click("下一页"); err != nil {
		t.Fatalf("点击下一页失败: %v", err)
	}
	if app.textSearchRes.Page != 2 || len(app.textSearchRes.Items) != 2 {
		t.Fatalf("翻页至第 2 页异常: page=%d len=%d", app.textSearchRes.Page, len(app.textSearchRes.Items))
	}
	// 长文本应展示「展开全文」按钮并可切换展开/收起
	if !tt.HasText("展开全文") {
		t.Fatalf("第 2 页超长剧情文本应显示「展开全文」按钮: %v", tt.Texts())
	}
	if err := tt.Click("展开全文"); err != nil {
		t.Fatalf("点击展开全文失败: %v", err)
	}
	if !tt.HasText("收起") {
		t.Fatalf("展开后应切换为「收起」按钮")
	}

	// 4. 英文双引号精确检索 "AD"：应自动切为精确匹配，且仅命中连贯的 AD，不命中 ABCD
	app.textSearchQ = `"AD"`
	app.textSearchPage = 1
	app.runTextSearch(false)
	tt.Frame()

	if app.textSearchRes.Mode != gamedata.TextMatchExact || app.textSearchRes.Total != 1 || app.textSearchRes.Items[0].Text != "AD" {
		t.Fatalf("精确检索 \"AD\" 结果异常: mode=%v total=%d items=%+v", app.textSearchRes.Mode, app.textSearchRes.Total, app.textSearchRes.Items)
	}
	if !tt.HasText("精确匹配") {
		t.Fatalf("英文双引号检索应显示「精确匹配」标签: %v", tt.Texts())
	}

	// 5. 英文双引号精确检索 "AB"：应命中 ABCD 与长剧情里的 ABCD，且支持跳转到物品与任务详情
	app.textSearchQ = `"AB"`
	app.textSearchPage = 1
	app.runTextSearch(false)
	tt.Frame()
	if app.textSearchRes.Total != 2 {
		t.Fatalf("精确检索 \"AB\" 期望命中 2 条，实际: %d", app.textSearchRes.Total)
	}
	if app.textSearchRes.Items[0].JumpKind != "item" || app.textSearchRes.Items[0].JumpID != 1001 {
		t.Fatalf("Item 表命中应支持跳转到物品 #1001，实际: %+v", app.textSearchRes.Items[0])
	}
	if app.textSearchRes.Items[1].JumpKind != "quest" || app.textSearchRes.Items[1].JumpID != 65536 {
		t.Fatalf("quest/xxx 任务剧情表命中应反查并支持跳转到任务 #65536，实际: %+v", app.textSearchRes.Items[1])
	}

	// 验证最小窗口 1024×640 下渲染稳定不崩溃
	ttMin := ui.NewTester(app.View, 1024, 640)
	if !ttMin.HasText("精确匹配") || !ttMin.HasText("Item") {
		t.Fatalf("最小视口 1024×640 下文本检索页面渲染异常: %v", ttMin.Texts())
	}

	// 点击直跳按钮前往物品详情
	if err := tt.Click("查看物品详情"); err != nil {
		t.Fatalf("点击查看物品详情失败: %v", err)
	}
	if app.tab != tabWorkbench || app.selectedItemID != 1001 {
		t.Fatalf("期望跳转至物品 #1001，实际 tab=%v itemID=%d", app.tab, app.selectedItemID)
	}

	// 6. 验证跨语言统一检索与多语言对照列，且操作列已移除复制按钮、页头不显示检索语言切换下拉
	svc.SetTextSearchIndex("chs", gamedata.NewTextSearchIndexFromRecords("chs",
		[]gamedata.TextIndexSheetInput{
			{Name: "Item", IsLocalized: true, ColNames: []string{"Name", "Description"}},
			{Name: "ClassJobCategory", IsLocalized: true, ColNames: []string{"Name"}},
		},
		[]gamedata.TextIndexRowInput{
			{SheetIdx: 0, RowID: 1001, SubRowID: -1, ColIdx: 0, Text: "ABCD"},
			{SheetIdx: 1, RowID: 33, SubRowID: -1, ColIdx: 0, Text: "能工巧匠"},
		},
		nil, nil,
	))
	svc.SetTextSearchIndex("ja", gamedata.NewTextSearchIndexFromRecords("ja",
		[]gamedata.TextIndexSheetInput{
			{Name: "Item", IsLocalized: true, ColNames: []string{"Name"}},
			{Name: "ClassJobCategory", IsLocalized: true, ColNames: []string{"Name"}},
		},
		[]gamedata.TextIndexRowInput{
			{SheetIdx: 0, RowID: 1001, SubRowID: -1, ColIdx: 0, Text: "ABCDの日本語名"},
			{SheetIdx: 1, RowID: 33, SubRowID: -1, ColIdx: 0, Text: "クラフター"},
		},
		nil, nil,
	))
	svc.SetTextSearchIndex("en", gamedata.NewTextSearchIndexFromRecords("en",
		[]gamedata.TextIndexSheetInput{
			{Name: "Item", IsLocalized: true, ColNames: []string{"Name"}},
			{Name: "ClassJobCategory", IsLocalized: true, ColNames: []string{"Name"}},
		},
		[]gamedata.TextIndexRowInput{
			{SheetIdx: 0, RowID: 1001, SubRowID: -1, ColIdx: 0, Text: "ABCD English Name"},
			{SheetIdx: 1, RowID: 33, SubRowID: -1, ColIdx: 0, Text: "Disciples of the Hand"},
		},
		nil, nil,
	))
	app.navigateTab(tabTextSearch)
	app.textSearchQ = `"AB"`
	app.textSearchPage = 1
	app.runTextSearch(false)
	tt.Frame()
	if !tt.HasText("多语言对照") || !tt.HasText("ABCDの日本語名") || !tt.HasText("ABCD English Name") {
		t.Fatalf("存在多语言数据时应在表格中展示多语言对照列及日英译文: %v", tt.Texts())
	}
	if _, hasCopy := tt.Find("复制文本"); hasCopy {
		t.Fatalf("文本检索表格操作列不应再显示复制按钮")
	}
	if tt.HasText("简体中文 (chs)") {
		t.Fatalf("文本检索页头不应再显示手动语言切换下拉")
	}

	// 不切换语言直接输入日文与英文，均应检索到「能工巧匠」条目
	for _, q := range []string{"クラフター", "Disciples of the Hand"} {
		app.textSearchQ = q
		app.textSearchPage = 1
		app.runTextSearch(false)
		tt.Frame()
		if app.textSearchRes.Total != 1 || len(app.textSearchRes.Items) != 1 || app.textSearchRes.Items[0].Text != "能工巧匠" {
			t.Fatalf("中文界面下直接搜索 %q 应命中「能工巧匠」，实际: %+v", q, app.textSearchRes.Items)
		}
		if !tt.HasText("能工巧匠") || !tt.HasText("クラフター") || !tt.HasText("Disciples of the Hand") {
			t.Fatalf("中文界面下直接搜索 %q 未正确渲染多语言对照内容: %v", q, tt.Texts())
		}
	}
}

func TestVersionDiffTextPartitionUI(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	itemRow := make(sqpack.Row, 95)
	itemRow[9] = "新圣剑"
	itemRow[20] = 660

	storeV1 := &gamedata.Store{
		HasCN:          true,
		PrimaryVersion: "2026.10.01.0000.0000",
		AppLang:        i18n.LangZH,
		PrimaryLang:    sqpack.LangChineseSimplified,
		ItemSummaries:  []gamedata.ItemSummary{{ID: 1001, NameCn: "旧圣剑", ItemLevel: 660, CategoryName: "武器"}},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			1001: {ID: 1001, NameCn: "旧圣剑", ItemLevel: 660, CategoryName: "武器"},
		},
	}
	bDir := t.TempDir()
	storeV2 := &gamedata.Store{
		HasCN:          true,
		PrimaryVersion: "2026.11.01.0000.0000",
		AppLang:        i18n.LangZH,
		PrimaryLang:    sqpack.LangChineseSimplified,
		ItemsPrimary:   map[int]sqpack.Row{1001: itemRow},
		ItemSummaries:  []gamedata.ItemSummary{{ID: 1001, NameCn: "新圣剑", ItemLevel: 660, CategoryName: "武器"}},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			1001: {ID: 1001, NameCn: "新圣剑", ItemLevel: 660, CategoryName: "武器"},
		},
	}
	bSvcV2 := backup.NewService(storeV2, bDir)
	bSvcV2.RegisterMemorySnapshot(backup.MemorySnapshot{
		Region: "cn", Patch: "7.55", Version: "2026.10.01.0000.0000",
		Store: storeV1,
		Texts: []backup.TextSnapshotRecord{
			{SheetName: "Item", RowID: 1001, SubRowID: -1, ColIdx: 0, Text: "旧圣剑威力200"},
			{SheetName: "Addon", RowID: 88, SubRowID: -1, ColIdx: 0, Text: "即将删除的旧系统文案"},
		},
	})
	bSvcV2.SetLiveMemoryTexts([]backup.TextSnapshotRecord{
		{SheetName: "Item", RowID: 1001, SubRowID: -1, ColIdx: 0, Text: "新圣剑威力240"},
		{SheetName: "Item", RowID: 9999, SubRowID: -1, ColIdx: 0, Text: "新增文本不可展示"},
	})
	vst := bSvcV2.GetVersionStatus()

	svc := gamedata.NewService(storeV2)
	app.mode = modeReady
	app.tab = tabDiff
	app.activateLangState(&langSessionCache{
		service:       svc,
		backupSvc:     bSvcV2,
		status:        svc.GetSystemStatus(),
		versionStatus: vst,
	}, false)

	tt := ui.NewTester(app.View, 1366, 768)

	// 点击「文本」独立分区按钮
	if err := tt.Click("文本"); err != nil {
		t.Fatalf("点击版本对比「文本」分区失败: %v", err)
	}
	if app.diffEntity != "text" || app.diffRes == nil || app.diffRes.TotalFiltered != 2 {
		t.Fatalf("文本对比分区结果异常: entity=%q total=%d", app.diffEntity, app.diffRes.TotalFiltered)
	}
	// 文本分区不显示「新增」筛选按钮，且不展示新增条目
	if slices.Contains(tt.Texts(), "新增") {
		t.Fatalf("文本对比分区不应提供「新增」筛选项: %v", tt.Texts())
	}
	if tt.HasText("新增文本不可展示") {
		t.Fatalf("文本对比分区不应展示新增文本")
	}
	if !tt.HasText("旧圣剑威力") || !tt.HasText("新圣剑威力") || !tt.HasText("即将删除的旧系统文案") {
		t.Fatalf("文本对比分区未正确渲染变更与移除文本: %v", tt.Texts())
	}

	// 切换「移除」筛选
	if err := tt.Click("移除"); err != nil {
		t.Fatalf("切换移除筛选失败: %v", err)
	}
	if app.diffRes.TotalFiltered != 1 || app.diffRes.Items[0].TextSheet != "Addon" {
		t.Fatalf("文本移除筛选异常: %+v", app.diffRes.Items)
	}

	// 最小视口 1024×640 渲染稳定
	ttMin := ui.NewTester(app.View, 1024, 640)
	if !ttMin.HasText("即将删除的旧系统文案") {
		t.Fatalf("最小视口下文本对比分区渲染异常: %v", ttMin.Texts())
	}
}
