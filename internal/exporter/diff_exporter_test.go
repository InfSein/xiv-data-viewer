package exporter

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

func makeTestPNGBytes(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码测试 PNG 失败: %v", err)
	}
	return buf.Bytes()
}

func waitForDiffExportDone(t *testing.T, svc *DiffService) DiffExportProgress {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p := svc.GetProgress()
		if p != nil && !p.IsRunning {
			return *p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待版本对比导出完成超时")
	return DiffExportProgress{}
}

func buildTestDiffFixture(t *testing.T) (*gamedata.Service, *backup.Service, []byte, []byte) {
	t.Helper()
	oldPNG := makeTestPNGBytes(t, 32, 32, color.RGBA{R: 200, G: 60, B: 60, A: 255})
	newPNG := makeTestPNGBytes(t, 64, 64, color.RGBA{R: 60, G: 180, B: 90, A: 255})

	oldStore := &gamedata.Store{
		HasCN:          true,
		CNVersion:      "2026.09.10.0000.0000",
		PrimaryVersion: "2026.09.10.0000.0000",
		PrimaryLang:    sqpack.LangChineseSimplified,
		AppLang:        i18n.LangZH,
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 1001, NameCn: "旧矿石", NameJa: "旧鉱石", NameEn: "Old Ore", IconID: 20001, ItemLevel: 10, CategoryName: "矿石"},
		},
		BGMs: []gamedata.MusicEntry{
			{ID: 10, File: "music/ffxiv/bgm_old.scd", Title: "旧曲目", NameCn: "旧曲目", FileExists: true, FileSize: 100, ContentMD5: "aaa"},
		},
	}

	liveStore := &gamedata.Store{
		HasCN:          true,
		CNVersion:      "2026.11.03.0000.0000",
		PrimaryVersion: "2026.11.03.0000.0000",
		PrimaryLang:    sqpack.LangChineseSimplified,
		AppLang:        i18n.LangZH,
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 1001, NameCn: "旧矿石", NameJa: "旧鉱石", NameEn: "Old Ore", IconID: 20001, ItemLevel: 20, CategoryName: "矿石"},
			{ID: 44099, NameCn: "辉光魔晶石", NameJa: "輝光マテリア", NameEn: "Radiant Materia", IconID: 26290, ItemLevel: 710, CategoryName: "魔晶石"},
			{ID: 44100, NameCn: "试作/魔晶石:I型*", NameJa: "試作マテリア", NameEn: "Proto Materia", IconID: 26290, ItemLevel: 710, CategoryName: "魔晶石"},
			{ID: 44101, NameCn: "无图标占位品", IconID: 0, ItemLevel: 1, CategoryName: "其他"},
		},
		BGMs: []gamedata.MusicEntry{
			{ID: 10, File: "music/ffxiv/bgm_old.scd", Title: "旧曲目", NameCn: "旧曲目", FileExists: true, FileSize: 100, ContentMD5: "aaa"},
			{ID: 205, File: "music/ex5/bgm_new.scd", Title: "星海回响/序章", NameCn: "星海回响/序章", FileExists: true, FileSize: 2048, ContentMD5: "bbb"},
		},
	}

	gameSvc := gamedata.NewService(liveStore)
	backupSvc := backup.NewService(liveStore, filepath.Join(t.TempDir(), "snapshots"))
	backupSvc.RegisterMemorySnapshot(backup.MemorySnapshot{
		Region:  snapshot.RegionCN,
		Patch:   "7.55",
		Version: "2026.09.10.0000.0000",
		Store:   oldStore,
		Icons: []snapshot.IconDigestRecord{
			{ID: 26291, Digest: "old_digest", Variants: "default:std|default:hr1", BlobKey: "blob_26291_old", HasHR1: true},
			{ID: 26292, Digest: "rem_digest", Variants: "default:std", BlobKey: "blob_26292_old"},
		},
		IconBlobs: map[string][]byte{
			"blob_26291_old": append([]byte{'P'}, oldPNG...),
			"blob_26292_old": append([]byte{'P'}, oldPNG...),
		},
		Texts: []backup.TextSnapshotRecord{
			{SheetName: "Addon", RowID: 10, SubRowID: -1, ColIdx: 0, Text: "旧提示文案"},
			{SheetName: "Addon", RowID: 11, SubRowID: -1, ColIdx: 0, Text: "将被移除的文案"},
		},
	})
	backupSvc.SetLiveMemoryIcons([]snapshot.IconDigestRecord{
		{ID: 26290, Digest: "add_digest", Variants: "default:std|default:hr1", HasHR1: true},
		{ID: 26291, Digest: "new_digest", Variants: "default:std|default:hr1", HasHR1: true},
	}, nil)
	backupSvc.SetLiveMemoryTexts([]backup.TextSnapshotRecord{
		{SheetName: "Addon", RowID: 10, SubRowID: -1, ColIdx: 0, Text: "新提示文案"},
	})
	return gameSvc, backupSvc, oldPNG, newPNG
}

func TestDiffExportIconsPartitionHDAndNaming(t *testing.T) {
	gameSvc, backupSvc, oldPNG, newPNG := buildTestDiffFixture(t)
	sdPNG := makeTestPNGBytes(t, 40, 40, color.RGBA{R: 50, G: 90, B: 220, A: 255})

	diffExp := NewDiffService(gameSvc, backupSvc)
	// 模拟 026290 有高清图、026291 仅标准图、026292 已移除走历史旧图
	var reqMu sync.Mutex
	var requestedHR1 []string
	diffExp.iconResolver = func(entry backup.DiffItemEntry, region string, preferHR1 bool, wantOld bool) ([]byte, error) {
		if wantOld || entry.ChangeType == "removed" {
			return backupSvc.ReadIconBlobPNG(entry.OldBlobKey, entry.OldThumbPNG)
		}
		reqMu.Lock()
		requestedHR1 = append(requestedHR1, fmt.Sprintf("%06d:%v", entry.IconID, preferHR1))
		reqMu.Unlock()
		if entry.IconID == 26290 && preferHR1 {
			return newPNG, nil
		}
		return sdPNG, nil
	}

	outDir := filepath.Join(t.TempDir(), "icons-out")
	err := diffExp.StartExport(DiffExportOptions{
		Query: backup.VersionDiffQuery{
			Region:     snapshot.RegionCN,
			From:       "7.55",
			To:         backup.LiveVersionID,
			Filter:     "all",
			EntityType: "icon",
		},
		Kind:       DiffExportIcons,
		Lang:       i18n.LangZH,
		PreferHR1:  true,
		IconNaming: DiffIconNamingIconID,
		OutputPath: outDir,
	}, nil)
	if err != nil {
		t.Fatalf("启动图标导出失败: %v", err)
	}
	prog := waitForDiffExportDone(t, diffExp)
	if !prog.IsCompleted || prog.ErrorCount != 0 || prog.SuccessCount != 3 {
		t.Fatalf("图标导出结果异常: %+v", prog)
	}

	// 校验命名格式为 026290.png、026291.png、026292.png 且高清优先、移除图标还原旧图
	got26290, err := os.ReadFile(filepath.Join(outDir, "026290.png"))
	if err != nil || !bytes.Equal(got26290, newPNG) {
		t.Fatalf("026290.png 应导出高清 PNG: err=%v len=%d", err, len(got26290))
	}
	got26291, err := os.ReadFile(filepath.Join(outDir, "026291.png"))
	if err != nil || !bytes.Equal(got26291, sdPNG) {
		t.Fatalf("026291.png 应导出标准回退 PNG: err=%v len=%d", err, len(got26291))
	}
	got26292, err := os.ReadFile(filepath.Join(outDir, "026292.png"))
	if err != nil || !bytes.Equal(got26292, oldPNG) {
		t.Fatalf("026292.png 应从快照历史缩略图还原: err=%v len=%d", err, len(got26292))
	}

	// 测试启用「同时导出变更前旧图」并输出为 Zip 压缩包
	zipPath := filepath.Join(t.TempDir(), "diff-icons.zip")
	err = diffExp.StartExport(DiffExportOptions{
		Query: backup.VersionDiffQuery{
			Region:     snapshot.RegionCN,
			From:       "7.55",
			To:         backup.LiveVersionID,
			Filter:     "modified",
			EntityType: "icon",
		},
		Kind:            DiffExportIcons,
		Lang:            i18n.LangZH,
		PreferHR1:       true,
		IconNaming:      DiffIconNamingIconID,
		IncludeOldIcons: true,
		ToZip:           true,
		OutputPath:      zipPath,
	}, nil)
	if err != nil {
		t.Fatalf("启动 Zip 导出失败: %v", err)
	}
	prog = waitForDiffExportDone(t, diffExp)
	if !prog.IsCompleted || prog.SuccessCount != 2 {
		t.Fatalf("变更图标含旧图 Zip 导出异常: %+v", prog)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("打开 Zip 失败: %v", err)
	}
	defer func() { _ = zr.Close() }()
	names := make(map[string]bool)
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["026291.png"] || !names["026291_old.png"] {
		t.Fatalf("Zip 内应同时包含 026291.png 与 026291_old.png, got=%v", names)
	}
}

func TestDiffExportItemIconsNamingAndSanitization(t *testing.T) {
	gameSvc, backupSvc, _, newPNG := buildTestDiffFixture(t)
	diffExp := NewDiffService(gameSvc, backupSvc)
	var decodeCalls atomic.Int64
	diffExp.iconResolver = func(entry backup.DiffItemEntry, region string, preferHR1 bool, wantOld bool) ([]byte, error) {
		decodeCalls.Add(1)
		return newPNG, nil
	}

	// 1. 物品筛选「新增」，按「物品编号_物品名.png」导出到本地目录
	outDir := filepath.Join(t.TempDir(), "item-icons")
	err := diffExp.StartExport(DiffExportOptions{
		Query: backup.VersionDiffQuery{
			Region:     snapshot.RegionCN,
			From:       "7.55",
			To:         backup.LiveVersionID,
			Filter:     "added",
			EntityType: "item",
		},
		Kind:       DiffExportIcons,
		Lang:       i18n.LangZH,
		PreferHR1:  true,
		IconNaming: DiffIconNamingEntityIDName,
		OutputPath: outDir,
	}, nil)
	if err != nil {
		t.Fatalf("启动物品图标导出失败: %v", err)
	}
	prog := waitForDiffExportDone(t, diffExp)
	// 3 条新增物品中，44101 无图标应跳过，44099 与 44100 均应导出
	if !prog.IsCompleted || prog.SuccessCount != 2 || prog.SkippedCount != 1 || prog.ErrorCount != 0 {
		t.Fatalf("物品图标导出统计异常: %+v", prog)
	}
	if _, err := os.Stat(filepath.Join(outDir, "44099_辉光魔晶石.png")); err != nil {
		t.Fatalf("缺少预期的物品图标文件 44099_辉光魔晶石.png: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "44100_试作_魔晶石_I型_.png")); err != nil {
		t.Fatalf("非法路径字符未按预期清洗为 44100_试作_魔晶石_I型_.png: %v", err)
	}

	// 2. 物品图标切换为按「图标ID.png」导出到 Zip 时应自动按图标编号去重
	zipPath := filepath.Join(t.TempDir(), "item-icons-dedup.zip")
	err = diffExp.StartExport(DiffExportOptions{
		Query: backup.VersionDiffQuery{
			Region:     snapshot.RegionCN,
			From:       "7.55",
			To:         backup.LiveVersionID,
			Filter:     "added",
			EntityType: "item",
		},
		Kind:       DiffExportIcons,
		Lang:       i18n.LangZH,
		PreferHR1:  true,
		IconNaming: DiffIconNamingIconID,
		ToZip:      true,
		OutputPath: zipPath,
	}, nil)
	if err != nil {
		t.Fatalf("启动物品图标按ID去重 Zip 导出失败: %v", err)
	}
	prog = waitForDiffExportDone(t, diffExp)
	if !prog.IsCompleted || prog.SuccessCount != 1 {
		t.Fatalf("按图标ID去重导出应仅产出 1 个文件: %+v", prog)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	if len(zr.File) != 1 || zr.File[0].Name != "026290.png" {
		t.Fatalf("Zip 内应仅包含去重后的 026290.png: %+v", zr.File)
	}
}

func TestDiffExportAudioAndReports(t *testing.T) {
	gameSvc, backupSvc, _, _ := buildTestDiffFixture(t)
	diffExp := NewDiffService(gameSvc, backupSvc)
	fakeSCD := []byte("SEDBSSCF_FAKE_RAW_SCD_BYTES")
	diffExp.audioResolver = func(filePath, region string) ([]byte, error) {
		return fakeSCD, nil
	}

	// 1. 音乐批量导出（非 OGG 自动回退 .scd）
	audDir := filepath.Join(t.TempDir(), "bgm-out")
	if err := diffExp.StartExport(DiffExportOptions{
		Query: backup.VersionDiffQuery{
			Region:     snapshot.RegionCN,
			From:       "7.55",
			To:         backup.LiveVersionID,
			Filter:     "added",
			EntityType: "bgm",
		},
		Kind:        DiffExportAudio,
		Lang:        i18n.LangZH,
		AudioFormat: DiffAudioOGG,
		OutputPath:  audDir,
	}, nil); err != nil {
		t.Fatalf("启动音乐导出失败: %v", err)
	}
	prog := waitForDiffExportDone(t, diffExp)
	if !prog.IsCompleted || prog.SuccessCount != 1 {
		t.Fatalf("音乐导出异常: %+v", prog)
	}
	if got, err := os.ReadFile(filepath.Join(audDir, "205_星海回响_序章.scd")); err != nil || !bytes.Equal(got, fakeSCD) {
		t.Fatalf("非 OGG 音频应回退导出为 205_星海回响_序章.scd: err=%v", err)
	}

	// 2. CSV / JSON / Markdown 报告导出（覆盖物品与文本分区）
	csvPath := filepath.Join(t.TempDir(), "items.csv")
	if err := diffExp.StartExport(DiffExportOptions{
		Query: backup.VersionDiffQuery{
			Region:     snapshot.RegionCN,
			From:       "7.55",
			To:         backup.LiveVersionID,
			Filter:     "all",
			EntityType: "item",
		},
		Kind:       DiffExportCSV,
		Lang:       i18n.LangZH,
		OutputPath: csvPath,
	}, nil); err != nil {
		t.Fatal(err)
	}
	prog = waitForDiffExportDone(t, diffExp)
	if !prog.IsCompleted || prog.SuccessCount != 4 {
		t.Fatalf("CSV 导出结果异常: %+v", prog)
	}
	csvRaw, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(csvRaw, []byte("\uFEFF")) {
		t.Fatalf("CSV 应包含 UTF-8 BOM")
	}
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(csvRaw, []byte("\uFEFF")))).ReadAll()
	if err != nil || len(rows) != 5 {
		t.Fatalf("CSV 行数不符（1表头+4数据）: err=%v rows=%d", err, len(rows))
	}

	jsonPath := filepath.Join(t.TempDir(), "texts.json")
	if err := diffExp.StartExport(DiffExportOptions{
		Query: backup.VersionDiffQuery{
			Region:     snapshot.RegionCN,
			From:       "7.55",
			To:         backup.LiveVersionID,
			Filter:     "all",
			EntityType: "text",
		},
		Kind:       DiffExportJSON,
		Lang:       i18n.LangZH,
		OutputPath: jsonPath,
	}, nil); err != nil {
		t.Fatal(err)
	}
	prog = waitForDiffExportDone(t, diffExp)
	if !prog.IsCompleted || prog.SuccessCount != 2 {
		t.Fatalf("文本 JSON 导出结果异常: %+v", prog)
	}
	jsonRaw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed diffJSONExportDoc
	if err := json.Unmarshal(jsonRaw, &parsed); err != nil || parsed.TotalFiltered != 2 || len(parsed.Items) != 2 {
		t.Fatalf("JSON 解析结果异常: err=%v doc=%+v", err, parsed)
	}

	mdPath := filepath.Join(t.TempDir(), "report.md")
	if err := diffExp.StartExport(DiffExportOptions{
		Query: backup.VersionDiffQuery{
			Region:     snapshot.RegionCN,
			From:       "7.55",
			To:         backup.LiveVersionID,
			Filter:     "all",
			EntityType: "text",
		},
		Kind:       DiffExportMarkdown,
		Lang:       i18n.LangZH,
		OutputPath: mdPath,
	}, nil); err != nil {
		t.Fatal(err)
	}
	prog = waitForDiffExportDone(t, diffExp)
	if !prog.IsCompleted {
		t.Fatalf("Markdown 导出失败: %+v", prog)
	}
	mdRaw, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mdRaw), "旧提示文案") || !strings.Contains(string(mdRaw), "新提示文案") {
		t.Fatalf("Markdown 报告缺少预期的文本变更内容: %s", string(mdRaw))
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "辉光魔晶石", want: "辉光魔晶石"},
		{in: `A/B\C:D*E?F"G<H>I|J`, want: "A_B_C_D_E_F_G_H_I_J"},
		{in: "  trailing...  ", want: "trailing"},
		{in: "CON", want: "CON_"},
		{in: "line1\nline2", want: "line1 line2"},
	}
	for _, tc := range cases {
		if got := SanitizeFileName(tc.in); got != tc.want {
			t.Errorf("SanitizeFileName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
