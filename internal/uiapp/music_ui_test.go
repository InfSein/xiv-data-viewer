package uiapp

import (
	"fmt"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/egoist/mygo/ui"
)

func TestMusicSidebarNavigationGlobalSearchAndDiff(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	oldMD5 := "aaaa1111bbbb2222cccc3333dddd4444"
	newMD5 := "eeee5555ffff66667777888899990000"

	rowRoll := make(sqpack.Row, 95)
	rowRoll[9] = "管弦乐琴乐谱：序曲"
	rowRoll[20] = 1

	// 先构建 v1 快照以触发真实跨版本 BGM 新增与 MD5 内容变化检测
	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01",
		BGMs: []gamedata.MusicEntry{
			{
				Key:           "bgm:10",
				Kind:          gamedata.MusicKindBGM,
				ID:            10,
				Title:         "BGM_System_Title",
				File:          "music/ffxiv/BGM_System_Title.scd",
				FileStem:      "BGM_System_Title",
				ExpansionID:   0,
				ExpansionName: "重生之境",
				FileExists:    true,
				FileSize:      2000000,
				ContentMD5:    oldMD5,
			},
		},
	}
	bSvcV1 := backup.NewService(storeV1, dataDir+"/backups")
	if _, err := bSvcV1.InitializeOnStartup(); err != nil {
		t.Fatalf("v1 备份初始化失败: %v", err)
	}

	bgm10 := gamedata.MusicEntry{
		Key:           "bgm:10",
		Kind:          gamedata.MusicKindBGM,
		ID:            10,
		Title:         "BGM_System_Title",
		File:          "music/ffxiv/BGM_System_Title.scd",
		FileStem:      "BGM_System_Title",
		ExpansionID:   0,
		ExpansionName: "重生之境",
		FileExists:    true,
		FileSize:      2100000,
		ContentMD5:    newMD5,
		Usages: []gamedata.MusicUsage{
			{Kind: "duty", Role: "战斗", Label: "萨斯塔夏溶洞", RefID: 1},
		},
		UsageSummary: "萨斯塔夏溶洞",
	}
	bgm20 := gamedata.MusicEntry{
		Key:           "bgm:20",
		Kind:          gamedata.MusicKindBGM,
		ID:            20,
		Title:         "BGM_EX5_Title",
		File:          "music/ex5/BGM_EX5_Title.scd",
		FileStem:      "BGM_EX5_Title",
		ExpansionID:   5,
		ExpansionName: "金曦之遗辉",
		FileExists:    true,
		FileSize:      3145728,
		ContentMD5:    newMD5,
	}
	orch100 := gamedata.MusicEntry{
		Key:           "orchestrion:100",
		Kind:          gamedata.MusicKindOrchestrion,
		ID:            100,
		Title:         "序曲 - 发现",
		NameCn:        "序曲 - 发现",
		NameEn:        "Prelude - Discoveries",
		NameJa:        "プレリュード ～発見～",
		Description:   "响彻幻想世界大厅的古典旋律。",
		File:          "music/ffxiv/Orchestrion/BGM_ORCH_001.scd",
		FileStem:      "BGM_ORCH_001",
		ExpansionID:   0,
		ExpansionName: "重生之境",
		CategoryID:    1,
		CategoryName:  "野外场景",
		ItemID:        14284,
		ItemName:      "管弦乐琴乐谱：序曲",
		FileExists:    true,
	}
	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.01",
		ItemsPrimary:   map[int]sqpack.Row{14284: rowRoll},
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 14284, NameCn: "管弦乐琴乐谱：序曲", CategoryName: "杂货", ItemLevel: 1},
		},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			14284: {ID: 14284, NameCn: "管弦乐琴乐谱：序曲", CategoryName: "杂货", ItemLevel: 1},
		},
		Music:        []gamedata.MusicEntry{bgm10, bgm20, orch100},
		BGMs:         []gamedata.MusicEntry{bgm10, bgm20},
		Orchestrions: []gamedata.MusicEntry{orch100},
		MusicByKey: map[string]gamedata.MusicEntry{
			bgm10.Key:   bgm10,
			bgm20.Key:   bgm20,
			orch100.Key: orch100,
		},
		BGMByID: map[int]gamedata.MusicEntry{
			10: bgm10,
			20: bgm20,
		},
		OrchByID: map[int]gamedata.MusicEntry{
			100: orch100,
		},
		MusicCategoryList: []gamedata.MusicCategoryInfo{
			{ID: 1, Name: "野外场景", Count: 1},
		},
		MusicExpansionList: []gamedata.MusicExpansionInfo{
			{ID: 0, Name: "重生之境", Count: 2},
			{ID: 5, Name: "金曦之遗辉", Count: 1},
		},
	}

	svc := gamedata.NewService(storeV2)
	bSvcV2 := backup.NewService(storeV2, dataDir+"/backups")
	vst, err := bSvcV2.InitializeOnStartup()
	if err != nil {
		t.Fatalf("v2 备份初始化失败: %v", err)
	}
	app.activateLangState(&langSessionCache{
		service:       svc,
		backupSvc:     bSvcV2,
		status:        svc.GetSystemStatus(),
		versionStatus: vst,
	}, false)
	app.mode = modeReady
	if len(app.records.Recent) != 0 {
		t.Fatalf("启动初始化选中音乐不应写入最近浏览记录，实际: %+v", app.records.Recent)
	}

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 验证侧边栏菜单顺序：【数据】 -> 成就 -> 【资源】 -> 音乐 -> 【历史】 -> 【对比】
	dataHdr, okD := tt.Find(app.t("nav.section.data"))
	achNav, okA := tt.Find(app.t("nav.achievements"))
	resHdr, okR := tt.Find(app.t("nav.section.resources"))
	musicNav, okM := tt.Find(app.t("nav.music"))
	histHdr, okH := tt.Find(app.t("nav.section.history"))
	cmpHdr, okC := tt.Find(app.t("nav.section.compare"))
	if !okD || !okA || !okR || !okM || !okH || !okC {
		t.Fatalf("侧边栏菜单项缺失: %v", tt.Texts())
	}
	if !(dataHdr.Y < achNav.Y && achNav.Y < resHdr.Y && resHdr.Y < musicNav.Y && musicNav.Y < histHdr.Y && histHdr.Y < cmpHdr.Y) {
		t.Fatalf("侧边栏「资源/音乐」应位于「数据」与「对比」之间: data=%.1f ach=%.1f res=%.1f music=%.1f hist=%.1f cmp=%.1f",
			dataHdr.Y, achNav.Y, resHdr.Y, musicNav.Y, histHdr.Y, cmpHdr.Y)
	}

	// 2. 点击打开「音乐」视图，切换管弦乐琴并跳转乐谱道具详情
	if err := tt.Click(app.t("nav.music")); err != nil {
		t.Fatalf("点击音乐导航失败: %v", err)
	}
	if app.tab != tabMusic || app.musicRes.Total != 3 {
		t.Fatalf("打开音乐页失败: tab=%v total=%d", app.tab, app.musicRes.Total)
	}
	if err := tt.Click(app.t("music.filter_orch")); err != nil {
		t.Fatalf("切换管弦乐琴筛选项失败: %v", err)
	}
	if app.musicRes.Total != 1 {
		t.Fatalf("筛选管弦乐琴后数量应为 1，实际 %d", app.musicRes.Total)
	}
	if err := tt.Click("序曲 - 发现"); err != nil {
		t.Fatalf("选择管弦乐琴曲目失败: %v", err)
	}
	if app.selectedMusicKey != orch100.Key {
		t.Fatalf("选中曲目 key 异常: %q", app.selectedMusicKey)
	}
	if err := tt.Click(app.t("favorites.add_btn")); err != nil {
		t.Fatalf("在音乐页点击加入收藏失败: %v", err)
	}
	if !app.isFavorite(config.RecordKindOrchestrion, 100) || !tt.HasText(app.t("favorites.added_btn")) {
		t.Fatalf("管弦乐琴 #100 收藏状态未更新")
	}
	if err := tt.Click(app.t("music.orch_open_item")); err != nil {
		t.Fatalf("跳转乐谱物品详情失败: %v", err)
	}
	if app.tab != tabWorkbench || app.selectedItemID != 14284 {
		t.Fatalf("应跳转至乐谱道具 #14284 详情，实际 tab=%v itemID=%d", app.tab, app.selectedItemID)
	}

	// 3. 验证全局搜索底部含「音乐」跳转入口，点击后直接进入音乐页搜索结果
	if err := tt.Click(i18n.T(i18n.LangZH, "search.global_label")); err != nil {
		t.Fatalf("点击全局搜索失败: %v", err)
	}
	tt.Type("萨斯塔夏")
	if !tt.HasText("在「音乐」中搜索") || !tt.HasText("BGM_System_Title") {
		t.Fatalf("全局搜索未展示音乐命中或「在「音乐」中搜索」跳转项: %v", tt.Texts())
	}
	if err := tt.Click("在「音乐」中搜索"); err != nil {
		t.Fatalf("点击跳转到音乐页搜索失败: %v", err)
	}
	if app.tab != tabMusic || app.musicQuery.Q != "萨斯塔夏" || app.musicRes.Total != 1 {
		t.Fatalf("跳转至音乐页检索结果异常: tab=%v q=%q total=%d", app.tab, app.musicQuery.Q, app.musicRes.Total)
	}

	// 4. 验证版本对比中展示新增 BGM 与既有 BGM 内容变化提示，但不展示新旧 MD5 明文
	if err := tt.Click(app.t("nav.diff")); err != nil {
		t.Fatalf("点击版本对比导航失败: %v", err)
	}
	var diffBGMChip ui.Rect
	var foundDiffBGMChip bool
	for _, info := range tt.AccessAll() {
		if info.Name == app.t("diff.entity_bgm") && info.Bounds.X > 250 {
			diffBGMChip = info.Bounds
			foundDiffBGMChip = true
			break
		}
	}
	if !foundDiffBGMChip {
		t.Fatalf("未在版本对比筛选栏找到「音乐」筛选按钮")
	}
	tt.ClickAt(diffBGMChip.X+diffBGMChip.W/2, diffBGMChip.Y+diffBGMChip.H/2)
	if app.diffEntity != "bgm" || app.diffRes == nil || app.diffRes.TotalFiltered != 2 {
		t.Fatalf("版本对比按音乐筛选应有 2 条变更，实际 entity=%q res=%+v", app.diffEntity, app.diffRes)
	}
	if !tt.HasText("BGM_EX5_Title") || !tt.HasText("BGM_System_Title") || !tt.HasText(app.t("backup.bgm_content_changed")) {
		t.Fatalf("版本对比未展示新增与内容变化 BGM 文案: %v", tt.Texts())
	}
	for _, txt := range tt.Texts() {
		if txt == oldMD5 || txt == newMD5 {
			t.Fatalf("版本对比界面文本不应包含任何 MD5 哈希: %q", txt)
		}
	}

	// 点击版本对比中的 BGM 条目可跳转至「音乐」页面定位播放详情，并测试收藏 BGM
	if err := tt.Click("BGM_System_Title"); err != nil {
		t.Fatalf("点击版本对比中的 BGM 条目失败: %v", err)
	}
	if app.tab != tabMusic || app.selectedMusicKind != gamedata.MusicKindBGM || app.selectedMusicID != 10 {
		t.Fatalf("点击 Diff BGM 后未跳转到音乐详情 #10: tab=%v kind=%q id=%d", app.tab, app.selectedMusicKind, app.selectedMusicID)
	}
	if err := tt.Click(app.t("favorites.add_btn")); err != nil {
		t.Fatalf("在音乐页收藏 BGM #10 失败: %v", err)
	}
	if !app.isFavorite(config.RecordKindBGM, 10) {
		t.Fatalf("BGM #10 收藏状态未更新")
	}

	// 5. 验证「最近」页面记录了打开过的音乐，支持「音乐」筛选、切换查看详情及「在音乐页打开」
	if err := tt.Click(app.t("nav.recent")); err != nil {
		t.Fatalf("点击最近导航失败: %v", err)
	}
	var recMusicFilter ui.Rect
	var foundRecMusicFilter bool
	for _, info := range tt.AccessAll() {
		if info.Name == app.t("recent.filter_music") && info.Bounds.X > 220 {
			recMusicFilter = info.Bounds
			foundRecMusicFilter = true
			break
		}
	}
	if !foundRecMusicFilter {
		t.Fatalf("最近页面缺少「音乐」筛选按钮")
	}
	tt.ClickAt(recMusicFilter.X+recMusicFilter.W/2, recMusicFilter.Y+recMusicFilter.H/2)
	if app.recentFilter != "music" || !tt.HasText("序曲 - 发现") || !tt.HasText("BGM_System_Title") {
		t.Fatalf("最近页面音乐筛选异常: filter=%q texts=%v", app.recentFilter, tt.Texts())
	}
	if err := tt.Click("序曲 - 发现"); err != nil {
		t.Fatalf("在最近页面点选音乐条目失败: %v", err)
	}
	if app.recentSelectedKind != config.RecordKindOrchestrion || app.recentSelectedID != 100 || !tt.HasText("响彻幻想世界大厅的古典旋律") {
		t.Fatalf("最近页面未展示选中音乐详情: kind=%s id=%d", app.recentSelectedKind, app.recentSelectedID)
	}
	if err := tt.Click(app.t("recent.open_in_music")); err != nil {
		t.Fatalf("在最近页面点击「在音乐页打开」失败: %v", err)
	}
	if app.tab != tabMusic || app.selectedMusicKey != orch100.Key {
		t.Fatalf("从最近页跳转回音乐页失败: tab=%v key=%q", app.tab, app.selectedMusicKey)
	}

	// 6. 验证「收藏」页面「收藏音乐」标签页展示收藏的 BGM 与管弦乐琴、支持类型筛选、取消收藏与重启恢复
	if err := tt.Click(app.t("nav.favorites")); err != nil {
		t.Fatalf("点击收藏导航失败: %v", err)
	}
	if err := tt.Click(app.t("favorites.tab_music_a11y")); err != nil {
		t.Fatalf("点击收藏音乐标签页失败: %v", err)
	}
	if app.favTab != "music" || !tt.HasText("序曲 - 发现") || !tt.HasText("BGM_System_Title") {
		t.Fatalf("收藏音乐页签未展示两首收藏曲目: favTab=%q texts=%v", app.favTab, tt.Texts())
	}
	if err := tt.Click(app.t("music.filter_bgm")); err != nil {
		t.Fatalf("收藏音乐页签切换游戏 BGM 筛选失败: %v", err)
	}
	if app.favMusicKind != gamedata.MusicKindBGM || !tt.HasText("BGM_System_Title") {
		t.Fatalf("收藏音乐按 BGM 筛选异常: kind=%q", app.favMusicKind)
	}
	if err := tt.Click(app.t("favorites.added_btn")); err != nil {
		t.Fatalf("在收藏音乐页签取消收藏 BGM #10 失败: %v", err)
	}
	if app.isFavorite(config.RecordKindBGM, 10) || !app.isFavorite(config.RecordKindOrchestrion, 100) {
		t.Fatalf("取消 BGM #10 收藏后状态异常")
	}

	// 7. 验证持久化重启后音乐最近记录与收藏名单完整恢复
	app.flushRecordsSave()
	app2 := newZHApp(t, dataDir)
	defer app2.Close()
	if !app2.isFavorite(config.RecordKindOrchestrion, 100) || app2.isFavorite(config.RecordKindBGM, 10) {
		t.Fatalf("重启后音乐收藏状态恢复异常: %+v", app2.records.Favorites)
	}
	if len(app2.records.Recent) < 2 {
		t.Fatalf("重启后音乐最近浏览记录应已持久化: %+v", app2.records.Recent)
	}
}

func TestQuestCutscenesDisplayAndBGMJump(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	bgm707 := gamedata.MusicEntry{
		Key:           "bgm:707",
		Kind:          gamedata.MusicKindBGM,
		ID:            707,
		Title:         "BGM_EX3_Event_15",
		File:          "music/ex3/BGM_EX3_Event_15.scd",
		FileStem:      "BGM_EX3_Event_15",
		ExpansionID:   3,
		ExpansionName: "暗影之逆焰",
		FileExists:    true,
	}
	q68836 := gamedata.QuestEntry{
		ID:         68836,
		InternalID: "LucKma401_03300",
		NameCn:     "灵光卫",
		Level:      71,
		IsMSQ:      true,
		Cutscenes: []gamedata.QuestCutsceneRef{
			{CutsceneID: 1975, Path: "ex3/lucact/lucact01230/lucact01230", Source: "script+journal"},
		},
	}
	qOther := gamedata.QuestEntry{
		ID:     68837,
		NameCn: "普通支线",
		Level:  71,
	}

	store := &gamedata.Store{
		PrimaryVersion: "2026.11.01",
		Quests:         []gamedata.QuestEntry{q68836, qOther},
		QuestByID:      map[int]gamedata.QuestEntry{68836: q68836, 68837: qOther},
		Music:          []gamedata.MusicEntry{bgm707},
		BGMs:           []gamedata.MusicEntry{bgm707},
		MusicByKey:     map[string]gamedata.MusicEntry{bgm707.Key: bgm707},
		BGMByID:        map[int]gamedata.MusicEntry{707: bgm707},
	}
	svc := gamedata.NewService(store)
	app.activateLangState(&langSessionCache{
		service: svc,
		status:  svc.GetSystemStatus(),
	}, false)
	app.mode = modeReady

	tt := ui.NewTester(app.View, 1366, 768)

	app.selectQuest(68836)
	app.selectedQuestCutscenes = []gamedata.QuestCutsceneEntry{
		{
			CutsceneID: 1975,
			Path:       "ex3/lucact/lucact01230/lucact01230",
			Source:     "script+journal",
			BGMs: []gamedata.QuestCutsceneBGM{
				{
					BGMID:      707,
					Title:      "明日与明日",
					SubTitle:   "BGM_EX3_Event_15",
					File:       "music/ex3/BGM_EX3_Event_15.scd",
					FileExists: true,
					Source:     "cutb",
				},
			},
		},
	}

	if err := tt.Click(app.t("nav.quests")); err != nil {
		t.Fatalf("点击任务导航失败: %v", err)
	}
	if err := tt.Click(app.t("quests.scope_with_cutscene")); err != nil {
		t.Fatalf("点击「含过场动画」筛选失败: %v", err)
	}
	if app.questQuery.Filter != "with_cutscene" || app.questRes.Total != 1 {
		t.Fatalf("含过场动画筛选结果异常: filter=%q total=%d", app.questQuery.Filter, app.questRes.Total)
	}

	if !tt.HasText("lucact01230") || !tt.HasText("明日与明日") || !tt.HasText("BGM_EX3_Event_15") {
		t.Fatalf("任务详情未展示过场动画与对应 BGM 信息: texts=%v", tt.Texts())
	}

	if err := tt.Click("明日与明日 #707"); err != nil {
		t.Fatalf("点击任务过场动画 BGM 跳转按钮失败: %v", err)
	}
	if app.tab != tabMusic || app.selectedMusicKind != gamedata.MusicKindBGM || app.selectedMusicID != 707 {
		t.Fatalf("点击过场动画 BGM 后未跳转到对应音乐详情 #707: tab=%v kind=%q id=%d", app.tab, app.selectedMusicKind, app.selectedMusicID)
	}
}

func TestMusicDetailQuestsAndUsageCollapse(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	defer app.Close()

	// 构造含 51 条使用场景的 BGM（触发超过 50 条自动折叠）与含任务脚本引用的 BGM/乐谱
	usages51 := make([]gamedata.MusicUsage, 51)
	for i := range usages51 {
		usages51[i] = gamedata.MusicUsage{
			Kind:  "territory",
			Role:  "区域",
			Label: "测试区域场景_" + string(rune('A'+i%26)) + "_" + fmt.Sprintf("%02d", i+1),
			RefID: 100 + i,
		}
	}
	bgmDense := gamedata.MusicEntry{
		Key:           "bgm:101",
		Kind:          gamedata.MusicKindBGM,
		ID:            101,
		Title:         "BGM_Dense_Zones",
		File:          "music/ffxiv/BGM_Dense_Zones.scd",
		FileStem:      "BGM_Dense_Zones",
		ExpansionID:   0,
		ExpansionName: "重生之境",
		FileExists:    true,
		Usages:        usages51,
	}
	bgmStory := gamedata.MusicEntry{
		Key:           "bgm:707",
		Kind:          gamedata.MusicKindBGM,
		ID:            707,
		Title:         "BGM_EX3_Event_15",
		File:          "music/ex3/BGM_EX3_Event_15.scd",
		FileStem:      "BGM_EX3_Event_15",
		ExpansionID:   3,
		ExpansionName: "暗影之逆焰",
		FileExists:    true,
		Usages: []gamedata.MusicUsage{
			{Kind: "duty", Role: "战斗", Label: "哈迪斯孤念歼灭战", RefID: 20},
		},
	}
	orch555 := gamedata.MusicEntry{
		Key:           "orchestrion:555",
		Kind:          gamedata.MusicKindOrchestrion,
		ID:            555,
		Title:         "明日与明日",
		NameCn:        "明日与明日",
		File:          "music/ex3/BGM_EX3_Event_15.scd",
		FileStem:      "BGM_EX3_Event_15",
		ExpansionID:   3,
		ExpansionName: "暗影之逆焰",
		CategoryID:    1,
		CategoryName:  "剧情",
		FileExists:    true,
	}
	q68836 := gamedata.QuestEntry{
		ID:          68836,
		NameCn:      "灵光卫",
		Level:       71,
		IconID:      gamedata.QuestIconMSQ,
		IsMSQ:       true,
		SectionName: "主线任务",
	}

	store := &gamedata.Store{
		PrimaryVersion: "2026.11.01",
		Quests:         []gamedata.QuestEntry{q68836},
		QuestByID:      map[int]gamedata.QuestEntry{68836: q68836},
		QuestBGMVars:   map[int]map[string]int{68836: {"LCUT_BGM1": 707}},
		Music:          []gamedata.MusicEntry{bgmDense, bgmStory, orch555},
		BGMs:           []gamedata.MusicEntry{bgmDense, bgmStory},
		Orchestrions:   []gamedata.MusicEntry{orch555},
		MusicByKey: map[string]gamedata.MusicEntry{
			bgmDense.Key: bgmDense,
			bgmStory.Key: bgmStory,
			orch555.Key:  orch555,
		},
		BGMByID:  map[int]gamedata.MusicEntry{101: bgmDense, 707: bgmStory},
		OrchByID: map[int]gamedata.MusicEntry{555: orch555},
	}
	svc := gamedata.NewService(store)
	app.activateLangState(&langSessionCache{
		service: svc,
		status:  svc.GetSystemStatus(),
	}, false)
	app.mode = modeReady

	tt := ui.NewTester(app.View, 1366, 768)

	if err := tt.Click(app.t("nav.music")); err != nil {
		t.Fatalf("点击音乐导航失败: %v", err)
	}

	// 1. BGM #101 超过 50 条使用场景应默认折叠，点击后展开并可再次收起
	app.selectMusic(gamedata.MusicKindBGM, 101)
	if err := tt.Click("BGM_Dense_Zones"); err != nil {
		t.Fatalf("选中 BGM_Dense_Zones 失败: %v", err)
	}
	usageTitle51 := app.tf("music.usages_section", "count", "51")
	if !tt.HasText(usageTitle51) || !tt.HasText(app.tf("music.collapse_hint_collapsed", "limit", "50")) {
		t.Fatalf("超过 50 条使用场景未显示默认折叠栏: %v", tt.Texts())
	}
	if tt.HasText(usages51[0].Label) {
		t.Fatalf("超过 50 条使用场景默认状态下不应直接展开明文内容")
	}

	if err := tt.Click(usageTitle51); err != nil {
		t.Fatalf("点击展开使用场景失败: %v", err)
	}
	if !tt.HasText(usages51[0].Label) || !tt.HasText(usages51[50].Label) || !tt.HasText(app.t("music.collapse_hint_expanded")) {
		t.Fatalf("点击后未完整展开 51 条使用场景: %v", tt.Texts())
	}

	if err := tt.Click(usageTitle51); err != nil {
		t.Fatalf("再次点击收起使用场景失败: %v", err)
	}
	if tt.HasText(usages51[0].Label) {
		t.Fatalf("再次点击后应恢复折叠状态")
	}

	// 2. 小于等于 50 条场景直接展开，并展示引用任务且可直跳任务详情
	if err := tt.Click("BGM_EX3_Event_15"); err != nil {
		t.Fatalf("选中 BGM #707 失败: %v", err)
	}
	if !tt.HasText("哈迪斯孤念歼灭战") || !tt.HasText("灵光卫") || !tt.HasText(app.t("music.quest_src_script")) {
		t.Fatalf("BGM #707 详情应直接展开使用场景并展示关联任务: %v", tt.Texts())
	}

	// 3. 管弦乐琴同音源曲目同样展示关联任务，点击任务直接跳转任务详情页
	if err := tt.Click("明日与明日"); err != nil {
		t.Fatalf("选中管弦乐琴 #555 失败: %v", err)
	}
	if !tt.HasText("灵光卫") {
		t.Fatalf("同音源管弦乐琴 #555 应展示关联任务灵光卫: %v", tt.Texts())
	}
	if err := tt.Click("灵光卫 #68836"); err != nil {
		t.Fatalf("点击关联任务跳转失败: %v", err)
	}
	if app.tab != tabQuests || app.selectedQuestID != 68836 {
		t.Fatalf("点击音乐关联任务后应跳转至任务 #68836: tab=%v questID=%d", app.tab, app.selectedQuestID)
	}
}
