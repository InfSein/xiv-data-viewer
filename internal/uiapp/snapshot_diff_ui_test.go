package uiapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	if tt.HasText("全部区服") || tt.HasText("国服 7.55") || tt.HasText("国际服 7.55") {
		t.Fatalf("快照下载列表不应拆分国服/国际服或展示区服筛选: %v", tt.Texts())
	}
	if app.versionStatus == nil || app.versionStatus.CNPatch != "7.58" || app.versionStatus.GlobalPatch != "7.58" {
		t.Fatalf("双服客户端内部版本 2026.11.03.0000.0000 应同时映射为国服 7.58 与国际服 7.58: %+v", app.versionStatus)
	}

	// 3. 按统一版本号下载 7.55（自动补齐国服与国际服）并验证同服切换与删除
	app.startSnapshotDownload("7.55", "7.55")
	tt.Frame()
	if !tt.HasText("已下载") || !tt.HasText("设为旧版对比") {
		t.Fatalf("下载完成后应展示已下载状态与对比按钮: %v", tt.Texts())
	}
	if app.versionStatus.DefaultCNFrom != "7.55" || app.versionStatus.DefaultGlobalFrom != "7.55" {
		t.Fatalf("双服统一下载 7.55 后两区服均应就绪: %+v", app.versionStatus)
	}

	app.snapMgrOpen = false
	app.switchDiffRegion(snapshot.RegionGlobal)
	tt.Frame()
	if app.diffRegion != snapshot.RegionGlobal || app.diffFromVer != "7.55" || app.diffToVer != backup.LiveVersionID {
		t.Fatalf("切换国际服对比后状态异常: region=%q from=%q to=%q", app.diffRegion, app.diffFromVer, app.diffToVer)
	}

	app.snapMgrOpen = true
	app.deleteLocalSnapshot("7.55", "7.55")
	tt.Frame()
	if tt.HasText("设为旧版对比") {
		t.Fatalf("删除本地快照后不应继续展示设为旧版对比按钮: %v", tt.Texts())
	}

	// 4. 最小视口 1024×640 下弹窗渲染稳定
	ttMin := ui.NewTester(app.View, 1024, 640)
	if !ttMin.HasText("版本快照管理（GitHub）") {
		t.Fatalf("1024x640 下快照管理弹窗渲染异常: %v", ttMin.Texts())
	}
}
