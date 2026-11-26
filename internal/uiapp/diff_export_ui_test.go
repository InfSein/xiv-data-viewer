package uiapp

import (
	"path/filepath"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/exporter"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/egoist/mygo/ui"
)

func TestDiffExportModalContextDefaultsAndViewport(t *testing.T) {
	dataDir := t.TempDir()
	oldStore := &gamedata.Store{
		HasCN:          true,
		CNVersion:      "2026.09.10.0000.0000",
		PrimaryVersion: "2026.09.10.0000.0000",
		PrimaryLang:    sqpack.LangChineseSimplified,
		AppLang:        i18n.LangZH,
		BGMs: []gamedata.MusicEntry{
			{ID: 10, File: "music/ffxiv/bgm_old.scd", Title: "旧曲目", NameCn: "旧曲目", FileExists: true, FileSize: 100, ContentMD5: "old"},
		},
	}
	liveStore := &gamedata.Store{
		HasCN:          true,
		CNVersion:      "2026.11.03.0000.0000",
		PrimaryVersion: "2026.11.03.0000.0000",
		PrimaryLang:    sqpack.LangChineseSimplified,
		AppLang:        i18n.LangZH,
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 44099, NameCn: "辉光魔晶石", IconID: 26290, ItemLevel: 710, CategoryName: "魔晶石"},
		},
		BGMs: []gamedata.MusicEntry{
			{ID: 10, File: "music/ffxiv/bgm_old.scd", Title: "旧曲目", NameCn: "旧曲目", FileExists: true, FileSize: 100, ContentMD5: "old"},
			{ID: 205, File: "music/ex5/bgm_new.scd", Title: "星海回响", NameCn: "星海回响", FileExists: true, FileSize: 1024, ContentMD5: "abc"},
		},
	}

	svc := gamedata.NewService(liveStore)
	bSvc := backup.NewService(liveStore, filepath.Join(dataDir, "snapshots"))
	bSvc.RegisterMemorySnapshot(backup.MemorySnapshot{
		Region:  snapshot.RegionCN,
		Patch:   "7.55",
		Version: "2026.09.10.0000.0000",
		Store:   oldStore,
		Icons: []snapshot.IconDigestRecord{
			{ID: 26291, Digest: "old", Variants: "default:std"},
		},
		Texts: []backup.TextSnapshotRecord{
			{SheetName: "Addon", RowID: 1, SubRowID: -1, ColIdx: 0, Text: "旧文本"},
		},
	})
	bSvc.SetLiveMemoryIcons([]snapshot.IconDigestRecord{
		{ID: 26290, Digest: "new", Variants: "default:std|default:hr1", HasHR1: true},
		{ID: 26291, Digest: "mod", Variants: "default:std|default:hr1", HasHR1: true},
	}, nil)
	bSvc.SetLiveMemoryTexts([]backup.TextSnapshotRecord{
		{SheetName: "Addon", RowID: 1, SubRowID: -1, ColIdx: 0, Text: "新文本"},
	})

	app := newZHApp(t, dataDir)
	defer app.Close()
	app.activateLangState(&langSessionCache{
		service:         svc,
		backupSvc:       bSvc,
		diffExporterSvc: exporter.NewDiffService(svc, bSvc),
		status:          svc.GetSystemStatus(),
		versionStatus:   bSvc.GetVersionStatus(),
	}, false)
	app.mode = modeReady
	app.tab = tabDiff

	tt := ui.NewTester(app.View, 1366, 768)

	// 1. 切换至「图标」分区后点击导出：应默认选中图标批量导出并展示 026290.png 示例与高清优先
	app.diffEntity = "icon"
	app.refreshDiff()
	tt.Frame()
	if err := tt.Click("导出"); err != nil {
		t.Fatalf("点击版本对比导出按钮失败: %v", err)
	}
	if !app.diffExportOpen || app.diffExportKind != exporter.DiffExportIcons || !app.diffExportPreferHR1 {
		t.Fatalf("图标分区默认导出状态异常: open=%v kind=%q hd=%v", app.diffExportOpen, app.diffExportKind, app.diffExportPreferHR1)
	}
	if !tt.HasText("导出版本对比结果") || !tt.HasText("优先高清 (HD，无高清回退标准)") || !tt.HasText("命名示例：026291.png") {
		t.Fatalf("图标导出弹窗文案异常: %v", tt.Texts())
	}

	// 2. 切换至「物品」分类后打开导出：应默认选中「导出物品图标 (PNG)」及「物品编号_物品名.png」
	app.diffExportOpen = false
	app.diffEntity = "item"
	app.refreshDiff()
	tt.Frame()
	if err := tt.Click("导出"); err != nil {
		t.Fatalf("物品分类点击导出失败: %v", err)
	}
	if app.diffExportKind != exporter.DiffExportIcons || app.diffExportIconNaming != exporter.DiffIconNamingEntityIDName {
		t.Fatalf("物品分类默认导出状态异常: kind=%q naming=%q", app.diffExportKind, app.diffExportIconNaming)
	}
	if !tt.HasText("导出物品图标 (PNG)") || !tt.HasText("命名示例：44099_辉光魔晶石.png") {
		t.Fatalf("物品导出弹窗应展示物品编号_物品名示例: %v", tt.Texts())
	}

	// 3. 切换至「音乐」分类：默认选中批量导出音乐
	app.diffExportOpen = false
	app.diffEntity = "bgm"
	app.refreshDiff()
	tt.Frame()
	if err := tt.Click("导出"); err != nil {
		t.Fatalf("音乐分类点击导出失败: %v", err)
	}
	if app.diffExportKind != exporter.DiffExportAudio || !tt.HasText("批量导出音乐 (OGG/SCD)") {
		t.Fatalf("音乐分类默认导出状态异常: kind=%q texts=%v", app.diffExportKind, tt.Texts())
	}

	// 4. 切换至「文本」分类：默认选中 CSV 表格且不显示图标导出
	app.diffExportOpen = false
	app.diffEntity = "text"
	app.refreshDiff()
	tt.Frame()
	if err := tt.Click("导出"); err != nil {
		t.Fatalf("文本分类点击导出失败: %v", err)
	}
	if app.diffExportKind != exporter.DiffExportCSV || !tt.HasText("保存文件...") {
		t.Fatalf("文本分类默认导出状态异常: kind=%q texts=%v", app.diffExportKind, tt.Texts())
	}

	// 5. 最小窗口 1024×640 下渲染稳定
	ttMin := ui.NewTester(app.View, 1024, 640)
	if !ttMin.HasText("导出版本对比结果") {
		t.Fatalf("1024x640 下导出弹窗渲染异常: %v", ttMin.Texts())
	}
}
