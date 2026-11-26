package uiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/exporter"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
)

func buildSyntheticIconTestStore() *gamedata.Store {
	icon0 := gamedata.IconEntry{
		ID:             0,
		GroupID:        0,
		Code:           "000000",
		DefaultVariant: "",
		HasHQ:          false,
		HasHR1:         false,
		HasLocalized:   false,
		Variants: []gamedata.IconVariantInfo{
			{Variant: "", HasStd: true, StdSource: gamedata.IconSourceCN},
		},
	}
	icon37618 := gamedata.IconEntry{
		ID:             37618,
		GroupID:        37000,
		Code:           "037618",
		DefaultVariant: "",
		HasHQ:          true,
		HasHR1:         true,
		HasLocalized:   true,
		Variants: []gamedata.IconVariantInfo{
			{Variant: "", HasStd: true, HasHR1: true, StdSource: gamedata.IconSourceCN, HR1Source: gamedata.IconSourceCN},
			{Variant: "hq", HasStd: true, HasHR1: true, StdSource: gamedata.IconSourceCN, HR1Source: gamedata.IconSourceCN},
			{Variant: "chs", HasStd: true, HasHR1: true, StdSource: gamedata.IconSourceCN, HR1Source: gamedata.IconSourceCN},
		},
	}
	icon37619 := gamedata.IconEntry{
		ID:             37619,
		GroupID:        37000,
		Code:           "037619",
		DefaultVariant: "hq",
		HasHQ:          true,
		HasHR1:         false,
		HasLocalized:   false,
		Variants: []gamedata.IconVariantInfo{
			{Variant: "hq", HasStd: true, StdSource: gamedata.IconSourceCN},
		},
	}
	icon65001 := gamedata.IconEntry{
		ID:             65001,
		GroupID:        65000,
		Code:           "065001",
		DefaultVariant: "en",
		DefaultHR1:     true,
		HasHQ:          false,
		HasHR1:         true,
		HasLocalized:   true,
		Variants: []gamedata.IconVariantInfo{
			{Variant: "en", HasHR1: true, HR1Source: gamedata.IconSourceGlobal},
			{Variant: "ja", HasStd: true, StdSource: gamedata.IconSourceGlobal},
		},
	}

	allIcons := []gamedata.IconEntry{icon0, icon37618, icon37619, icon65001}
	byID := make(map[int]gamedata.IconEntry, len(allIcons))
	for _, ic := range allIcons {
		byID[ic.ID] = ic
	}

	testItemSum := gamedata.ItemSummary{
		ID:           2,
		NameCn:       "幻象测试药",
		NameEn:       "Test Fantasia",
		NameJa:       "テスト幻想薬",
		IconID:       37618,
		ItemLevel:    1,
		Rarity:       1,
		CategoryName: "药品",
	}
	testQuest := gamedata.QuestEntry{
		ID:     68836,
		NameCn: "幻象测试任务",
		NameEn: "Test Icon Quest",
		NameJa: "テストクエスト",
		IconID: 37618,
	}

	return &gamedata.Store{
		PrimaryVersion:  "2026.11.20.0000.0000",
		AppLang:         i18n.LangZH,
		ItemSummaries:   []gamedata.ItemSummary{testItemSum},
		ItemSummaryByID: map[int]gamedata.ItemSummary{2: testItemSum},
		Quests:          []gamedata.QuestEntry{testQuest},
		QuestByID:       map[int]gamedata.QuestEntry{68836: testQuest},
		Icons:           allIcons,
		IconByID:        byID,
		IconGroups: []gamedata.IconGroupInfo{
			{GroupID: 0, Code: "000000", RangeLabel: "000000 – 000999", Count: 1},
			{GroupID: 37000, Code: "037000", RangeLabel: "037000 – 037999", Count: 2, HQCount: 2, HR1Count: 1, LocalizedCount: 1},
			{GroupID: 65000, Code: "065000", RangeLabel: "065000 – 065999", Count: 1, HR1Count: 1, LocalizedCount: 1},
		},
		IconsByGroup: map[int][]gamedata.IconEntry{
			0:     {icon0},
			37000: {icon37618, icon37619},
			65000: {icon65001},
		},
		AvailableIconVariants: []string{"", "hq", "chs", "en", "ja"},
		IconUsagesByID: map[int][]gamedata.IconUsageRef{
			37618: {
				{Kind: "quest", ID: 68836, Name: "幻象测试任务"},
			},
		},
	}
}

func TestIconsSidebarNavigationGroupingSearchInspectorAndExportModal(t *testing.T) {
	dataDir := t.TempDir()
	app := newZHApp(t, dataDir)
	store := buildSyntheticIconTestStore()
	svc := gamedata.NewService(store)
	iconExpSvc := exporter.NewIconService(svc)

	app.activateLangState(&langSessionCache{
		service:         svc,
		iconExporterSvc: iconExpSvc,
		status:          svc.GetSystemStatus(),
	}, false)
	app.mode = modeReady

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 验证侧边栏菜单顺序：【资源】 -> 音乐 -> 图标 -> 【历史】
	resHdr, okR := tt.Find(app.t("nav.section.resources"))
	musicNav, okM := tt.Find(app.t("nav.music"))
	iconsNav, okI := tt.Find(app.t("nav.icons"))
	histHdr, okH := tt.Find(app.t("nav.section.history"))
	if !okR || !okM || !okI || !okH {
		t.Fatalf("侧边栏菜单项缺失: res=%v music=%v icons=%v hist=%v", okR, okM, okI, okH)
	}
	if !(resHdr.Y < musicNav.Y && musicNav.Y < iconsNav.Y && iconsNav.Y < histHdr.Y) {
		t.Fatalf("侧边栏菜单排序不符合规范: resY=%.1f musicY=%.1f iconsY=%.1f histY=%.1f",
			resHdr.Y, musicNav.Y, iconsNav.Y, histHdr.Y)
	}

	// 2. 点击进入「图标」页面，验证分组列表隐藏空组，点击 037000 仅展示有效资源编号
	if err := tt.Click(app.t("nav.icons")); err != nil {
		t.Fatalf("点击图标导航失败: %v", err)
	}
	if app.tab != tabIcons {
		t.Fatalf("期望当前标签页为 tabIcons，实际: %v", app.tab)
	}
	if !tt.HasText("000000") || !tt.HasText("037000") || !tt.HasText("065000") {
		t.Fatalf("左侧分组列表缺少预期非空分组: %v", tt.Texts())
	}
	if tt.HasText("001000") || tt.HasText("036000") {
		t.Fatalf("空分组不应出现在图标分组列表中: %v", tt.Texts())
	}

	// 验证 #0 图标有效且可正常选中展示
	if err := tt.Click("000000"); err != nil {
		t.Fatalf("点击分组 000000 失败: %v", err)
	}
	if app.selectedIconID != 0 || !tt.HasText("ui/icon/000000/000000.png") {
		t.Fatalf("选中 #0 图标失败: selectedIconID=%d texts=%v", app.selectedIconID, tt.Texts())
	}

	// 验证分组 037000 仅展示 037618 与 037619，跳过无资源编号
	if err := tt.Click("037000"); err != nil {
		t.Fatalf("点击分组 037000 失败: %v", err)
	}
	if app.iconQuery.GroupID != 37000 || len(app.iconRes.Items) != 2 {
		t.Fatalf("分组 037000 筛选数量异常: group=%d len=%d", app.iconQuery.GroupID, len(app.iconRes.Items))
	}
	if tt.HasText("037617") || tt.HasText("037001") {
		t.Fatalf("组内无资源图标编号未跳过显示: %v", tt.Texts())
	}
	if app.selectedIconID != 37618 || !tt.HasText("#037618") {
		t.Fatalf("切换到 037000 组后应默认选中 037618，实际 id=%d", app.selectedIconID)
	}
	// 详情页应默认优先显示高清 (_hr1) 贴图
	if !app.iconInspectorHR1 || !tt.HasText("ui/icon/037000/037618_hr1.png") {
		t.Fatalf("详情页应默认展示高清 _hr1 图片: hr1=%v texts=%v", app.iconInspectorHR1, tt.Texts())
	}

	// 3. 测试详情大图点击弹出放大预览弹窗、在弹窗内快捷切换目录变体/清晰度，以及网格双击弹出大图预览
	if err := tt.Click(app.t("icons.preview_zoom_tip")); err != nil {
		t.Fatalf("点击详情页预览大图失败: %v", err)
	}
	if !app.iconLightboxOpen || !tt.HasText("ui/icon/037000/037618_hr1.png") {
		t.Fatalf("点击详情图后未展示大图弹窗")
	}
	// 在大图弹窗内部直接切换 chs/ 与 hq/ 目录变体与标准分辨率
	var chsBtnRect ui.Rect
	var foundChsInModal bool
	for _, info := range tt.AccessAll() {
		if info.Name == app.t("icons.variant_chs") && info.Bounds.X < 1000 {
			chsBtnRect = info.Bounds
			foundChsInModal = true
			break
		}
	}
	if !foundChsInModal {
		t.Fatalf("大图弹窗内未找到目录变体切换按钮 %q: %v", app.t("icons.variant_chs"), tt.AccessAll())
	}
	tt.ClickAt(chsBtnRect.X+chsBtnRect.W/2, chsBtnRect.Y+chsBtnRect.H/2)
	if app.iconLightboxVariant != "chs" || !tt.HasText("ui/icon/037000/chs/037618_hr1.png") {
		t.Fatalf("大图弹窗切换 chs/ 变体未生效: var=%q texts=%v", app.iconLightboxVariant, tt.Texts())
	}
	// 恢复默认根目录变体后关闭大图弹窗
	var defVarRect ui.Rect
	for _, info := range tt.AccessAll() {
		if info.Name == app.t("icons.variant_default") && info.Bounds.X < 1000 {
			defVarRect = info.Bounds
			break
		}
	}
	tt.ClickAt(defVarRect.X+defVarRect.W/2, defVarRect.Y+defVarRect.H/2)
	if err := tt.Click(app.t("common.close")); err != nil {
		t.Fatalf("关闭大图弹窗失败: %v", err)
	}
	if app.iconLightboxOpen {
		t.Fatalf("大图弹窗未正常关闭")
	}

	// 双击图标列表中的 037618 单元格触发大图展示
	cellRect, okCell := tt.Find("037618")
	if !okCell {
		t.Fatalf("未找到网格中的 037618 图标项")
	}
	tt.ClickAt(cellRect.X+cellRect.W/2, cellRect.Y+cellRect.H/2)
	tt.Advance(40 * time.Millisecond)
	tt.ClickAt(cellRect.X+cellRect.W/2, cellRect.Y+cellRect.H/2)
	if !app.iconLightboxOpen || app.iconLightboxID != 37618 {
		t.Fatalf("双击网格图标未能弹出大图弹窗: open=%v id=%d", app.iconLightboxOpen, app.iconLightboxID)
	}
	if err := tt.Click(app.t("common.close")); err != nil {
		t.Fatalf("关闭双击大图弹窗失败: %v", err)
	}

	// 在检查器中切换标准与 chs/ 变体，验证导出相对路径更新
	if err := tt.Click(app.t("icons.insp_res_std")); err != nil {
		t.Fatalf("切换标准分辨率失败: %v", err)
	}
	if !tt.HasText("ui/icon/037000/037618.png") {
		t.Fatalf("切回标准分辨率路径未更新: %v", tt.Texts())
	}
	if err := tt.Click(app.t("icons.variant_chs")); err != nil {
		t.Fatalf("切换 chs/ 变体失败: %v", err)
	}
	if err := tt.Click(app.t("icons.insp_res_hr1")); err != nil {
		t.Fatalf("切换 _hr1 分辨率失败: %v", err)
	}
	if !tt.HasText("ui/icon/037000/chs/037618_hr1.png") {
		t.Fatalf("SaintCoinach 变体导出路径未同步更新: %v", tt.Texts())
	}

	// 4. 点击检查器内关联任务并验证跳转任务工作台
	if err := tt.Click("幻象测试任务"); err != nil {
		t.Fatalf("点击关联任务失败: %v", err)
	}
	if app.tab != tabQuests || app.selectedQuestID != 68836 {
		t.Fatalf("点击图标关联任务后应跳转到任务详情 #68836，实际 tab=%v quest=%d", app.tab, app.selectedQuestID)
	}

	// 5. 测试顶栏全局搜索「在「图标」中搜索」
	if err := tt.Click(i18n.T(i18n.LangZH, "search.global_label")); err != nil {
		t.Fatalf("打开顶栏全局搜索失败: %v", err)
	}
	tt.Type("065001")
	jumpTxt := "在「" + app.t("search.global_module_icon") + "」中搜索"
	if !tt.HasText(jumpTxt) {
		t.Fatalf("全局搜索缺少「%s」跳转项: %v", jumpTxt, tt.Texts())
	}
	if err := tt.Click(jumpTxt); err != nil {
		t.Fatalf("点击「%s」失败: %v", jumpTxt, err)
	}
	if app.tab != tabIcons || app.selectedIconID != 65001 {
		t.Fatalf("全局搜索跳转图标页面异常: tab=%v selectedIconID=%d", app.tab, app.selectedIconID)
	}

	// 6. 打开批量导出弹窗，验证默认全选可用变体、切换勾选与关闭
	if err := tt.Click(app.t("icons.export_btn")); err != nil {
		t.Fatalf("点击导出弹窗按钮失败: %v", err)
	}
	if !app.iconExportOpen || !tt.HasText(app.t("icon_export.title")) {
		t.Fatalf("图标批量导出弹窗未打开")
	}
	// 默认应全选全部可用变体（""、"hq"、"chs"、"en"、"ja"）
	for _, v := range store.AvailableIconVariants {
		if !app.iconExportVariants[v] {
			t.Fatalf("变体 %q 默认未勾选", v)
		}
	}

	// 7. 取消勾选高清分辨率并确认预览文件数与状态刷新后验证进度面板显示与关闭弹窗
	if err := tt.Click(app.t("icon_export.res_hr1")); err != nil {
		t.Fatalf("切换高清复选框失败: %v", err)
	}
	if app.iconExportIncludeHR1 {
		t.Fatalf("点击后应取消勾选 _hr1")
	}
	app.iconExportProgress = &exporter.IconExportProgress{
		IsRunning:      true,
		CurrentFile:    "ui/icon/037000/037618.png",
		ProcessedTasks: 45,
		TotalTasks:     100,
		Percent:        45,
	}
	tt.Frame()
	if !tt.HasText("45%") || !tt.HasText("ui/icon/037000/037618.png") {
		t.Fatalf("导出进度面板未正确显示实时百分比与当前文件: %v", tt.Texts())
	}
	app.iconExportProgress = nil
	tt.Frame()

	if err := tt.Click(app.t("common.close")); err != nil {
		t.Fatalf("关闭导出弹窗失败: %v", err)
	}
	if app.iconExportOpen {
		t.Fatalf("关闭按钮未能关闭导出弹窗")
	}

	// 8. 验证最小窗口尺寸 1024×640 下页面关键区域均正常显示
	tt.SetSize(1024, 640)
	tt.Frame()
	if !tt.HasText(app.t("icons.title")) || !tt.HasText(app.t("icons.groups_title")) || !tt.HasText(app.t("icons.meta_title")) {
		t.Fatalf("1024x640 最小视口下图标页面关键区域显示不全: %v", tt.Texts())
	}
}

func TestIconsRealClientIntegrationIfConfigured(t *testing.T) {
	cnPath := readEnvVarForUITest("FFXIV_CN_PATH")
	globalPath := readEnvVarForUITest("FFXIV_GLOBAL_PATH")
	if cnPath == "" && globalPath == "" {
		t.Skip("未配置 FFXIV_CN_PATH / FFXIV_GLOBAL_PATH，跳过真实客户端 UI 集成验证")
	}

	dataDir := t.TempDir()
	cfg := config.AppConfig{
		CNPath:     cnPath,
		GlobalPath: globalPath,
		Language:   string(i18n.LangZH),
	}
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("客户端路径不可用: %v", err)
	}

	store, err := gamedata.LoadGameDataStoreWithOptions(resolved, nil, gamedata.StoreLoadOptions{
		AppLang: i18n.LangZH,
	})
	if err != nil {
		t.Fatalf("加载游戏数据失败: %v", err)
	}
	defer func() { _ = store.Close() }()

	app := newZHApp(t, dataDir)
	svc := gamedata.NewService(store)
	app.activateLangState(&langSessionCache{
		service:         svc,
		iconExporterSvc: exporter.NewIconService(svc),
		status:          svc.GetSystemStatus(),
	}, false)
	app.mode = modeReady

	tt := ui.NewTester(app.View, 1366, 768)
	if err := tt.Click(app.t("nav.icons")); err != nil {
		t.Fatalf("打开图标页失败: %v", err)
	}
	if err := tt.Click(app.t("icons.groups_filter_placeholder")); err != nil {
		t.Fatalf("聚焦组号筛选框失败: %v", err)
	}
	tt.Type("037")
	if err := tt.Click("037000"); err != nil {
		t.Fatalf("切换到分组 037000 失败: %v", err)
	}
	if app.iconQuery.GroupID != 37000 || len(app.iconRes.Items) == 0 {
		t.Fatalf("切换到 037000 分组后状态异常: group=%d items=%d", app.iconQuery.GroupID, len(app.iconRes.Items))
	}

	// 通过页面搜索框检索 037618 并验证真实无窗口环境下的 .tex 解码属性
	if err := tt.Click(app.t("icons.search_label")); err != nil {
		t.Fatalf("聚焦图标搜索框失败: %v", err)
	}
	tt.Type("037618")
	if !tt.HasText("037618") || app.selectedIconID != 37618 {
		t.Fatalf("搜索 037618 后未选中对应图标，实际 %d", app.selectedIconID)
	}
	prev := app.iconPreviewCache[app.selectedIconPreviewKey()]
	if prev == nil || prev.err != "" || prev.hdr.Width == 0 || prev.rawSize == 0 {
		t.Fatalf("真实 #037618 图标详情解码异常: %+v", prev)
	}
}

func readEnvVarForUITest(key string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", ".env"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, val, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(val), `"'`)
		}
	}
	return ""
}

func TestIconsVersionDiffSeparatePartitionAndVisualCards(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := filepath.Join(dataDir, "backups")

	// V1 客户端：含 37618（无高清）、37619、99999（V2 移除）及未变更道具
	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		AppLang:        i18n.LangZH,
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 100, NameCn: "老道具", IconID: 37619},
		},
		Icons: []gamedata.IconEntry{
			{
				ID: 37618, GroupID: 37000, Code: "037618",
				Variants: []gamedata.IconVariantInfo{{Variant: "", HasStd: true}},
			},
			{
				ID: 37619, GroupID: 37000, Code: "037619",
				Variants: []gamedata.IconVariantInfo{{Variant: "", HasStd: true}},
			},
			{
				ID: 99999, GroupID: 99000, Code: "099999",
				Variants: []gamedata.IconVariantInfo{{Variant: "", HasStd: true}},
			},
		},
	}
	// V2 客户端：37618 新增高清变体（变更）、新增 65001、移除 99999，新增对比道具 #200
	storeV2 := buildSyntheticIconTestStore()
	storeV2.PrimaryVersion = "2026.11.01.0000.0000"
	storeV2.ItemSummaries = append(storeV2.ItemSummaries, gamedata.ItemSummary{
		ID: 200, NameCn: "新版本测试药", IconID: 37618, ItemLevel: 740,
	})
	svcV2 := gamedata.NewService(storeV2)
	bSvcV2 := backup.NewService(storeV2, backupDir)
	bSvcV2.RegisterMemorySnapshot(backup.MemorySnapshot{
		Region: "cn", Patch: "7.55", Version: "2026.10.01.0000.0000",
		Store: storeV1,
		Icons: []snapshot.IconDigestRecord{
			{ID: 37618, Digest: "v1_37618", Variants: "default:std"},
			{ID: 37619, Digest: "mem_037619_default:std", Variants: "default:std"},
			{ID: 99999, Digest: "v1_99999", Variants: "default:std"},
		},
	})
	vst := bSvcV2.GetVersionStatus()

	app := newZHApp(t, dataDir)
	app.activateLangState(&langSessionCache{
		service:         svcV2,
		backupSvc:       bSvcV2,
		iconExporterSvc: exporter.NewIconService(svcV2),
		status:          svcV2.GetSystemStatus(),
		versionStatus:   vst,
	}, false)
	app.mode = modeReady

	tt := ui.NewTester(app.View, 1366, 768)
	if err := tt.Click(app.t("nav.diff")); err != nil {
		t.Fatalf("进入版本对比失败: %v", err)
	}

	// 1. 「全部」视图不混入图标条目，仅展示道具变动
	if app.diffEntity != "all" {
		t.Fatalf("期望默认版本对比标签为 all，实际: %q", app.diffEntity)
	}
	if tt.HasText("#065001") || tt.HasText("#099999") {
		t.Fatalf("版本对比「全部」不应混入图标差异: %v", tt.Texts())
	}
	if !tt.HasText("新版本测试药") {
		t.Fatalf("版本对比「全部」应展示常规道具变更")
	}

	// 2. 「图标」分组按钮前应有竖向分割线，提示前面的「全部」等统计不含图标
	var diffIconChip, diffBGMChip, diffAchChip ui.Rect
	var foundDiffIconChip, foundDiffBGMChip, foundDiffAchChip bool
	for _, info := range tt.AccessAll() {
		if info.Bounds.X > 250 && strings.HasPrefix(info.Name, app.t("diff.entity_icon")) {
			diffIconChip = info.Bounds
			foundDiffIconChip = true
			break
		}
	}
	if !foundDiffIconChip {
		t.Fatalf("未在版本对比筛选栏找到「图标」筛选按钮")
	}
	for _, info := range tt.AccessAll() {
		if info.Bounds.X <= 250 || info.Bounds.X >= diffIconChip.X {
			continue
		}
		dy := info.Bounds.Y - diffIconChip.Y
		if dy < -4 || dy > 4 {
			continue
		}
		switch info.Name {
		case app.t("diff.entity_bgm"):
			diffBGMChip = info.Bounds
			foundDiffBGMChip = true
		case app.t("diff.entity_achievement"):
			diffAchChip = info.Bounds
			foundDiffAchChip = true
		}
	}
	if !foundDiffBGMChip || !foundDiffAchChip {
		t.Fatalf("未在同一行筛选栏找到「成就」或「音乐」分组按钮: ach=%v bgm=%v", foundDiffAchChip, foundDiffBGMChip)
	}
	normalGap := diffBGMChip.X - (diffAchChip.X + diffAchChip.W)
	iconSepGap := diffIconChip.X - (diffBGMChip.X + diffBGMChip.W)
	if iconSepGap <= normalGap+4 {
		t.Fatalf("「图标」分组按钮前缺少分割线间距: normalGap=%.1f iconSepGap=%.1f", normalGap, iconSepGap)
	}
	img := tt.Image()
	sepMidY := int(diffIconChip.Y + diffIconChip.H/2)
	hasSepLine := false
	for x := int(diffBGMChip.X+diffBGMChip.W) + 1; x < int(diffIconChip.X); x++ {
		pxMid := img.RGBAAt(x, sepMidY)
		pxTop := img.RGBAAt(x, sepMidY-5)
		pxBot := img.RGBAAt(x, sepMidY+5)
		if (pxMid.R < 245 || pxMid.G < 245 || pxMid.B < 245) &&
			(pxTop.R < 245 || pxTop.G < 245 || pxTop.B < 245) &&
			(pxBot.R < 245 || pxBot.G < 245 || pxBot.B < 245) {
			hasSepLine = true
			break
		}
	}
	if !hasSepLine {
		t.Fatalf("未在「音乐」与「图标」分组按钮之间检测到竖向分割线像素")
	}

	// 3. 切换到独立「图标」分区：无文字表头，分别渲染变更（新旧并排）、新增与移除平铺网格
	tt.ClickAt(diffIconChip.X+diffIconChip.W/2, diffIconChip.Y+diffIconChip.H/2)
	if app.diffEntity != "icon" {
		t.Fatalf("期望 diffEntity 切换为 icon，实际: %q", app.diffEntity)
	}
	if tt.HasText(app.t("diff.col_changes")) {
		t.Fatalf("图标独立分区不应显示常规表格列头「%s」", app.t("diff.col_changes"))
	}
	if !tt.HasText(app.t("diff.icon_section_modified")) ||
		!tt.HasText(app.t("diff.icon_section_added")) ||
		!tt.HasText(app.t("diff.icon_section_removed")) {
		t.Fatalf("图标对比分区缺少变更/新增/移除平铺区块: %v", tt.Texts())
	}
	if !tt.HasText(app.t("diff.icon_prev_label")) || !tt.HasText(app.t("diff.icon_new_label")) {
		t.Fatalf("变更图标卡片应同时标注旧图标与新图标: %v", tt.Texts())
	}
	if !tt.HasText("#037618") || !tt.HasText("065001") || !tt.HasText("099999") {
		t.Fatalf("图标对比分区缺少预期的变更/新增/移除图标编号: %v", tt.Texts())
	}

	// 4. 点击移除图标瓷砖弹出历史图像大图预览
	if err := tt.Click("#099999"); err != nil {
		t.Fatalf("点击移除图标 099999 失败: %v", err)
	}
	if !app.iconLightboxOpen {
		t.Fatalf("点击已移除图标应打开大图预览弹窗")
	}
	if err := tt.Click(app.t("common.close")); err != nil {
		t.Fatalf("关闭大图弹窗失败: %v", err)
	}

	// 5. 点击变更图标卡片跳转回「图标」工作台并定位选中 037618
	if err := tt.Click("#037618"); err != nil {
		t.Fatalf("点击变更图标卡片跳转失败: %v", err)
	}
	if app.tab != tabIcons || app.selectedIconID != 37618 {
		t.Fatalf("点击变更图标卡片应跳转到图标页 #037618，实际 tab=%v icon=%d", app.tab, app.selectedIconID)
	}
}
