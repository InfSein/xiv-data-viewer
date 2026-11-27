package uiapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/egoist/mygo/ui"
	"github.com/klauspost/compress/zstd"
)

func buildUITestXivSnap(t *testing.T, region, patch, gameVer string, langs []string) ([]byte, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), snapshot.AssetFileName(patch, region))
	w, err := snapshot.NewWriter(p, zstd.SpeedDefault)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.AddFile("exd/root.exl", []byte("EXLT,2\n"))
	if err := w.Close(snapshot.SnapshotMeta{
		ID:          snapshot.MakeSnapshotID(region, patch),
		Region:      region,
		Patch:       patch,
		GameVersion: gameVer,
		Languages:   langs,
		CreatedAt:   "2026-09-10T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:])
}

func TestDiffOnlineSnapshotManagerFlow(t *testing.T) {
	dataDir := t.TempDir()
	cnBytes, cnSHA := buildUITestXivSnap(t, snapshot.RegionCN, "7.55", "2026.09.10.0000.0000", []string{"chs"})
	glBytes, glSHA := buildUITestXivSnap(t, snapshot.RegionGlobal, "7.55", "2026.09.10.0000.0000", []string{"ja", "en"})

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog.json":
			cat := snapshot.Catalog{
				SchemaVersion: snapshot.CatalogSchemaVersion,
				UpdatedAt:     "2026-11-26T00:00:00Z",
				Versions: []snapshot.VersionCatalogEntry{
					{
						Patch:      "7.58",
						CreatedAt:  "2026-11-03T00:00:00Z",
						ReleaseTag: "v7.58",
						CN: &snapshot.RegionAsset{
							GameVersion: "2026.11.03.0000.0000",
							Languages:   []string{"chs"},
							AssetName:   snapshot.AssetFileName("7.58", snapshot.RegionCN),
							DownloadURL: srv.URL + "/assets/cn-7.58",
							Size:        1024,
						},
						Global: &snapshot.RegionAsset{
							GameVersion: "2026.11.03.0000.0000",
							Languages:   []string{"ja", "en"},
							AssetName:   snapshot.AssetFileName("7.58", snapshot.RegionGlobal),
							DownloadURL: srv.URL + "/assets/gl-7.58",
							Size:        2048,
						},
					},
					{
						Patch:      "7.55",
						CreatedAt:  "2026-09-10T00:00:00Z",
						ReleaseTag: "v7.55",
						CN: &snapshot.RegionAsset{
							GameVersion: "2026.09.10.0000.0000",
							Languages:   []string{"chs"},
							AssetName:   snapshot.AssetFileName("7.55", snapshot.RegionCN),
							DownloadURL: srv.URL + "/assets/cn-7.55",
							Size:        int64(len(cnBytes)),
							SHA256:      cnSHA,
						},
						Global: &snapshot.RegionAsset{
							GameVersion: "2026.09.10.0000.0000",
							Languages:   []string{"ja", "en"},
							AssetName:   snapshot.AssetFileName("7.55", snapshot.RegionGlobal),
							DownloadURL: srv.URL + "/assets/gl-7.55",
							Size:        int64(len(glBytes)),
							SHA256:      glSHA,
						},
					},
				},
			}
			_ = json.NewEncoder(rw).Encode(cat)
		case "/assets/cn-7.55":
			_, _ = rw.Write(cnBytes)
		case "/assets/gl-7.55":
			_, _ = rw.Write(glBytes)
		default:
			http.NotFound(rw, r)
		}
	}))
	defer srv.Close()

	// 同时配置国服 + 国际服客户端
	store := &gamedata.Store{
		HasCN:          true,
		HasGlobal:      true,
		CNVersion:      "2026.11.03.0000.0000",
		GlobalVersion:  "2026.11.03.0000.0000",
		PrimaryVersion: "2026.11.03.0000.0000",
		PrimaryLang:    sqpack.LangChineseSimplified,
		AppLang:        i18n.LangZH,
	}
	svc := gamedata.NewService(store)
	bSvc := backup.NewService(store, filepath.Join(dataDir, "snapshots"))
	bSvc.SetSnapshotClient(snapshot.NewClient(snapshot.ClientOptions{
		CatalogURL: srv.URL + "/catalog.json",
		HTTPClient: srv.Client(),
	}))
	vst := bSvc.GetVersionStatus()

	app := newZHApp(t, dataDir)
	defer app.Close()
	app.activateLangState(&langSessionCache{
		service:       svc,
		backupSvc:     bSvc,
		status:        svc.GetSystemStatus(),
		versionStatus: vst,
	}, false)
	app.mode = modeReady
	app.tab = tabDiff

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 双服模式下顶部应提供「国服 / 国际服」同服对比切换，且无快照时引导从 GitHub 下载
	if !tt.HasText("国服") || !tt.HasText("国际服") {
		t.Fatalf("双服模式下版本对比顶部应提供国服/国际服切换器: %v", tt.Texts())
	}
	if !tt.HasText("从 GitHub 下载版本快照开始对比") || tt.HasText("刷新快照") {
		t.Fatalf("无快照引导界面不符合预期: %v", tt.Texts())
	}

	// 2. 点击「打开快照下载中心」应自动拉取目录：同时识别国服 7.58 与国际服 7.58，但列表仅按统一版本号展示（不拆分国服/国际服）
	if err := tt.Click("打开快照下载中心"); err != nil {
		t.Fatalf("点击打开快照下载中心失败: %v", err)
	}
	if !app.snapMgrOpen || !tt.HasText("版本快照管理（GitHub）") || !tt.HasText("7.55") || !tt.HasText("当前客户端同版") {
		t.Fatalf("快照管理弹窗状态异常: open=%v texts=%v", app.snapMgrOpen, tt.Texts())
	}
	// 右上角按钮应为「刷新」而非「刷新目录」，且提供「全部 / 已下载 / 未下载」三个切换栏目
	if !tt.HasText("刷新") || tt.HasText("刷新目录") {
		t.Fatalf("刷新按钮文案应为「刷新」: %v", tt.Texts())
	}
	if !tt.HasText("全部") || !tt.HasText("已下载") || !tt.HasText("未下载") {
		t.Fatalf("快照管理弹窗应提供全部/已下载/未下载三个栏目: %v", tt.Texts())
	}
	// 下载与删除按钮仅保留图标（无可见文本节点，仅保留无障碍 Label），且移除「设为旧版对比」按钮
	countText := func(texts []string, target string) int {
		n := 0
		for _, s := range texts {
			if s == target {
				n++
			}
		}
		return n
	}
	if tt.HasText("设为旧版对比") {
		t.Fatalf("快照管理行不应包含设为旧版对比按钮: %v", tt.Texts())
	}
	// 2 条未下载版本各 1 个图标下载按钮（每按钮仅 1 个无障碍 Label，若带可见文本会出现 2 次/行）
	if got := countText(tt.Texts(), "下载"); got != 2 {
		t.Fatalf("期望 2 个纯图标下载按钮（仅无障碍 Label），实际计数=%d texts=%v", got, tt.Texts())
	}
	if got := countText(tt.Texts(), "删除"); got != 0 {
		t.Fatalf("未下载任何快照时不应出现删除按钮，实际计数=%d texts=%v", got, tt.Texts())
	}
	if tt.HasText("全部区服") || tt.HasText("国服 7.55") || tt.HasText("国际服 7.55") {
		t.Fatalf("快照下载列表不应拆分国服/国际服或展示区服筛选: %v", tt.Texts())
	}
	if app.versionStatus == nil || app.versionStatus.CNPatch != "7.58" || app.versionStatus.GlobalPatch != "7.58" {
		t.Fatalf("双服客户端内部版本 2026.11.03.0000.0000 应同时映射为国服 7.58 与国际服 7.58: %+v", app.versionStatus)
	}

	// 记录「全部」栏目下弹窗标题与底栏高度，验证切换栏目时弹窗高度与垂直位置绝对稳定
	measureModalSpan := func(label, footerText string) (float32, float32) {
		titleR, okT := tt.Find("版本快照管理（GitHub）")
		footR, okF := tt.Find(footerText)
		if !okT || !okF {
			t.Fatalf("(%s) 未定位到快照弹窗标题或底栏节点: title=%v foot=%v (%q)", label, okT, okF, footerText)
		}
		return titleR.Y, footR.Y - titleR.Y
	}
	baseTitleY, baseModalSpan := measureModalSpan("全部-未下载前", app.t("diff.snap_footer_hint"))

	// 切换到「已下载」栏目：尚未下载时应显示空状态，且弹窗高度与位置保持不变
	if err := tt.Click("已下载"); err != nil {
		t.Fatalf("切换已下载栏目失败: %v", err)
	}
	if !tt.HasText("暂无已下载的版本快照") {
		t.Fatalf("未下载快照时「已下载」栏目应提示为空: %v", tt.Texts())
	}
	dlEmptyTitleY, dlEmptySpan := measureModalSpan("已下载-空态", app.t("diff.snap_footer_hint"))
	if math.Abs(float64(dlEmptyTitleY-baseTitleY)) > 0.5 || math.Abs(float64(dlEmptySpan-baseModalSpan)) > 0.5 {
		t.Fatalf("切至空态「已下载」栏目后弹窗高度发生变化: base=(Y=%.1f span=%.1f) got=(Y=%.1f span=%.1f)",
			baseTitleY, baseModalSpan, dlEmptyTitleY, dlEmptySpan)
	}
	if err := tt.Click("全部"); err != nil {
		t.Fatalf("切回全部栏目失败: %v", err)
	}

	// 3. 按统一版本号下载 7.55（自动补齐国服与国际服）并验证栏目筛选、同服切换与删除
	app.startSnapshotDownload("7.55", "7.55")
	tt.Frame()
	if tt.HasText("设为旧版对比") {
		t.Fatalf("下载完成后不应展示设为旧版对比按钮: %v", tt.Texts())
	}
	if got := countText(tt.Texts(), "删除"); got != 1 {
		t.Fatalf("已下载 1 个版本时应仅展示 1 个纯图标删除按钮，实际计数=%d texts=%v", got, tt.Texts())
	}
	if app.versionStatus.DefaultCNFrom != "7.55" || app.versionStatus.DefaultGlobalFrom != "7.55" {
		t.Fatalf("双服统一下载 7.55 后两区服均应就绪: %+v", app.versionStatus)
	}

	// 切到「已下载」仅显示 7.55（2026.09.10.0000.0000）；切到「未下载」仅显示 7.58（2026.11.03.0000.0000），且弹窗高度恒定
	app.snapMgrFilter = "downloaded"
	tt.Frame()
	if !tt.HasText("2026.09.10.0000.0000") || tt.HasText("2026.11.03.0000.0000") {
		t.Fatalf("「已下载」栏目筛选异常: %v", tt.Texts())
	}
	dlTitleY, dlSpan := measureModalSpan("已下载-1条", app.snapMgrNotice)
	if math.Abs(float64(dlTitleY-baseTitleY)) > 0.5 || math.Abs(float64(dlSpan-baseModalSpan)) > 2.0 {
		t.Fatalf("切至「已下载」栏目后弹窗位置或高度变化: base=(Y=%.1f span=%.1f) got=(Y=%.1f span=%.1f)",
			baseTitleY, baseModalSpan, dlTitleY, dlSpan)
	}
	app.snapMgrFilter = "not_downloaded"
	tt.Frame()
	if tt.HasText("2026.09.10.0000.0000") || !tt.HasText("2026.11.03.0000.0000") {
		t.Fatalf("「未下载」栏目筛选异常: %v", tt.Texts())
	}
	ndTitleY, ndSpan := measureModalSpan("未下载-1条", app.snapMgrNotice)
	if math.Abs(float64(ndTitleY-baseTitleY)) > 0.5 || math.Abs(float64(ndSpan-dlSpan)) > 0.5 {
		t.Fatalf("切至「未下载」栏目后弹窗位置或高度变化: baseY=%.1f dlSpan=%.1f got=(Y=%.1f span=%.1f)",
			baseTitleY, dlSpan, ndTitleY, ndSpan)
	}
	app.snapMgrFilter = "all"

	app.snapMgrOpen = false
	app.switchDiffRegion(snapshot.RegionGlobal)
	tt.Frame()
	if app.diffRegion != snapshot.RegionGlobal || app.diffFromVer != "7.55" || app.diffToVer != backup.LiveVersionID {
		t.Fatalf("切换国际服对比后状态异常: region=%q from=%q to=%q", app.diffRegion, app.diffFromVer, app.diffToVer)
	}

	app.snapMgrOpen = true
	app.deleteLocalSnapshot("7.55", "7.55")
	tt.Frame()
	app.snapMgrFilter = "downloaded"
	tt.Frame()
	if !tt.HasText("暂无已下载的版本快照") {
		t.Fatalf("删除本地快照后「已下载」栏目应恢复为空: %v", tt.Texts())
	}
	app.snapMgrFilter = "all"

	// 4. 最小视口 1024×640 下弹窗渲染稳定
	ttMin := ui.NewTester(app.View, 1024, 640)
	if !ttMin.HasText("版本快照管理（GitHub）") {
		t.Fatalf("1024x640 下快照管理弹窗渲染异常: %v", ttMin.Texts())
	}
}

func TestDiffOptimizationsAndLatestVersionBadgesUI(t *testing.T) {
	dataDir := t.TempDir()
	storeOld := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		ItemSummaries:  []gamedata.ItemSummary{{ID: 1001, NameCn: "旧武器", ItemLevel: 700, CategoryName: "武器"}},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			1001: {ID: 1001, NameCn: "旧武器", ItemLevel: 700, CategoryName: "武器"},
		},
		Quests: []gamedata.QuestEntry{{ID: 2001, NameCn: "旧主线任务", Level: 100}},
		Leves:  []gamedata.LeveEntry{{ID: 3001, NameCn: "旧制作理符", LeveKind: "craft", ClassJobLevel: 100}},
		Achievements: []gamedata.AchievementEntry{
			{ID: 4001, NameCn: "旧成就", Points: 10, KindName: "战斗", CategoryName: "综合"},
		},
	}
	storeLive := &gamedata.Store{
		HasCN:          true,
		CNVersion:      "2026.11.03.0000.0000",
		PrimaryVersion: "2026.11.03.0000.0000",
		PrimaryLang:    sqpack.LangChineseSimplified,
		AppLang:        i18n.LangZH,
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 1001, NameCn: "旧武器", ItemLevel: 700, CategoryName: "武器"},
			{ID: 1002, NameCn: "新版本圣剑", ItemLevel: 730, CategoryName: "武器"},
		},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			1001: {ID: 1001, NameCn: "旧武器", ItemLevel: 700, CategoryName: "武器"},
			1002: {ID: 1002, NameCn: "新版本圣剑", ItemLevel: 730, CategoryName: "武器"},
		},
		Quests: []gamedata.QuestEntry{
			{ID: 2001, NameCn: "旧主线任务", Level: 100},
			{ID: 2002, NameCn: "新版本主线", Level: 100},
		},
		QuestByID: map[int]gamedata.QuestEntry{
			2001: {ID: 2001, NameCn: "旧主线任务", Level: 100},
			2002: {ID: 2002, NameCn: "新版本主线", Level: 100},
		},
		Leves: []gamedata.LeveEntry{
			{ID: 3001, NameCn: "旧制作理符", LeveKind: "craft", ClassJobLevel: 100},
			{ID: 3002, NameCn: "新版本理符", LeveKind: "craft", ClassJobLevel: 100},
		},
		LeveByID: map[int]gamedata.LeveEntry{
			3001: {ID: 3001, NameCn: "旧制作理符", LeveKind: "craft", ClassJobLevel: 100},
			3002: {ID: 3002, NameCn: "新版本理符", LeveKind: "craft", ClassJobLevel: 100},
		},
		Achievements: []gamedata.AchievementEntry{
			{ID: 4001, NameCn: "旧成就", Points: 10, KindName: "战斗", CategoryName: "综合"},
			{
				ID:               4002,
				NameCn:           "绝境新成就",
				DescriptionCn:    "通关最新绝境战",
				Points:           20,
				KindName:         "战斗",
				CategoryName:     "高难度任务",
				TitleID:          88,
				TitleMasculineCn: "绝境征服者",
				RewardItemID:     1002,
				RewardItemNameCn: "新版本圣剑",
			},
		},
		AchievementByID: map[int]gamedata.AchievementEntry{
			4001: {ID: 4001, NameCn: "旧成就", Points: 10, KindName: "战斗", CategoryName: "综合"},
			4002: {ID: 4002, NameCn: "绝境新成就", DescriptionCn: "通关最新绝境战", Points: 20, KindName: "战斗", CategoryName: "高难度任务", TitleID: 88, TitleMasculineCn: "绝境征服者", RewardItemID: 1002, RewardItemNameCn: "新版本圣剑"},
		},
		Icons: []gamedata.IconEntry{{ID: 100, Code: "000100"}},
	}

	svc := gamedata.NewService(storeLive)
	bSvc := backup.NewService(storeLive, filepath.Join(dataDir, "snapshots"))
	// 注册 7.57（仅 0a，无图标数据）与 7.58（用于映射当前客户端版本名）
	bSvc.RegisterMemorySnapshot(backup.MemorySnapshot{
		Region:  snapshot.RegionCN,
		Patch:   "7.58",
		Version: "2026.11.03.0000.0000",
		Store:   storeLive,
	})
	bSvc.RegisterMemorySnapshot(backup.MemorySnapshot{
		Region:  snapshot.RegionCN,
		Patch:   "7.57",
		Version: "2026.10.01.0000.0000",
		Store:   storeOld,
		Texts: []backup.TextSnapshotRecord{
			{SheetName: "Action", RowID: 10, SubRowID: -1, ColIdx: 0, Text: "威力：7500\n持续6秒\n恢复力7500"},
		},
	})
	bSvc.SetLiveMemoryTexts([]backup.TextSnapshotRecord{
		{SheetName: "Action", RowID: 10, SubRowID: -1, ColIdx: 0, Text: "威力：8000\n持续6秒\n恢复力8000"},
	})

	app := newZHApp(t, dataDir)
	defer app.Close()
	app.activateLangState(&langSessionCache{
		service:       svc,
		backupSvc:     bSvc,
		status:        svc.GetSystemStatus(),
		versionStatus: bSvc.GetVersionStatus(),
	}, false)
	app.mode = modeReady

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 物品页应展示「7.58 新增」徽章与筛选按钮，点击筛选后仅保留新版本物品
	if !tt.HasText("7.58 新增") {
		t.Fatalf("物品页应展示 7.58 新增标记与筛选入口: %v", tt.Texts())
	}
	app.itemQuery.NewOnly = true
	app.refreshItemList()
	tt.Frame()
	if app.itemRes.Total != 1 || !tt.HasText("新版本圣剑") || tt.HasText("旧武器") {
		t.Fatalf("物品页按最新版本新增筛选异常: total=%d texts=%v", app.itemRes.Total, tt.Texts())
	}

	// 2. 版本对比 -> 成就：直接按成就表格展示称号、达成条件与奖励道具
	app.tab = tabDiff
	app.diffEntity = "achievement"
	app.refreshDiff()
	tt.Frame()
	if !tt.HasText("《绝境征服者》") || !tt.HasText("通关最新绝境战") || !tt.HasText("奖励称号 / 道具") {
		t.Fatalf("版本对比成就页未直接展示称号与达成条件表格: %v", tt.Texts())
	}

	// 3. 版本对比 -> 图标：旧版本 7.57 无图标数据（仅 0a）时应给出明确提示
	app.diffEntity = "icon"
	app.refreshDiff()
	tt.Frame()
	if !tt.HasText("无法进行图标对比") || !tt.HasText("不包含图标数据") {
		t.Fatalf("仅 0a 版本进行图标对比时应提示无图标数据: %v", tt.Texts())
	}

	// 4. 版本对比 -> 文本：应展示双列表头（旧文本 / 新文本）并支持精确对比渲染
	app.diffEntity = "text"
	app.refreshDiff()
	tt.Frame()
	if !tt.HasText("旧文本") || !tt.HasText("新文本") {
		t.Fatalf("文本对比应采用旧文本/新文本双列表头: %v", tt.Texts())
	}
}
