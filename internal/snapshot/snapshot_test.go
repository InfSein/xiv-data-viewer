package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestArchiveRoundTripAndRandomAccess(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "cn-7.58.xivsnap")

	w, err := NewWriter(outPath, zstd.SpeedDefault)
	if err != nil {
		t.Fatalf("NewWriter 失败: %v", err)
	}
	if err := w.AddFile("exd/root.exl", []byte("EXLT,2\nItem,0\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.AddFile("exd/item.exh", []byte("EXHF-DUMMY-CONTENT")); err != nil {
		t.Fatal(err)
	}
	if err := w.AddFile("schemas/Item.yml", []byte("name: Item\nfields:\n  - name: Name\n")); err != nil {
		t.Fatal(err)
	}

	bgmRaw, err := EncodeBGMDigests([]BGMDigestRecord{
		{ID: 1, File: "music/ffxiv/bgm_test.scd", FileExists: true, FileSize: 123456, ContentMD5: "abc123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.AddFile(metaBGMPath, bgmRaw); err != nil {
		t.Fatal(err)
	}

	iconRaw, err := EncodeIconDigests([]IconDigestRecord{
		{ID: 100, Digest: "d100", Variants: "default:std", BlobKey: "k100", HasHR1: true},
		{ID: 2500, Digest: "d2500", Variants: "default:std|hq:hr1", BlobKey: "k2500", HasHR1: true, HasHQ: true, HasLocale: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.AddFile(metaIconDigestPath, iconRaw); err != nil {
		t.Fatal(err)
	}

	thumbRaw, err := EncodeIconThumbs(map[string][]byte{
		"k100": {'P', 1, 2, 3, 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.AddFile(metaIconThumbsPath, thumbRaw); err != nil {
		t.Fatal(err)
	}

	wantMeta := SnapshotMeta{
		ID:          "cn-7.58",
		Region:      RegionCN,
		Patch:       "7.58",
		GameVersion: "2026.11.03.0000.0000",
		Languages:   []string{"chs"},
		CreatedAt:   "2026-11-26T00:00:00Z",
		Stats: SnapshotStats{
			SheetCount:         1,
			EXDFileCount:       1,
			SchemaCount:        1,
			BGMCount:           1,
			IconCount:          2,
			PrevIconThumbCount: 1,
		},
	}
	if err := w.Close(wantMeta); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	meta, sz, err := ReadArchiveMeta(outPath)
	if err != nil || sz <= 0 {
		t.Fatalf("ReadArchiveMeta 失败: err=%v size=%d", err, sz)
	}
	if meta.ID != "cn-7.58" || meta.Patch != "7.58" || meta.GameVersion != "2026.11.03.0000.0000" {
		t.Fatalf("元信息不匹配: %+v", meta)
	}

	arch, err := OpenArchive(outPath)
	if err != nil {
		t.Fatalf("OpenArchive 失败: %v", err)
	}
	defer func() { _ = arch.Close() }()

	gotEXH, err := arch.ReadFile("EXD/Item.exh")
	if err != nil || string(gotEXH) != "EXHF-DUMMY-CONTENT" {
		t.Fatalf("ReadFile(EXD/Item.exh) = %q, err=%v", string(gotEXH), err)
	}
	missing, err := arch.ReadFile("exd/not_exist.exh")
	if err != nil || missing != nil {
		t.Fatalf("缺失文件应返回 nil, nil，实际 %v, %v", missing, err)
	}

	sfs := arch.SchemasFS()
	if sfs == nil {
		t.Fatalf("SchemasFS 不应为 nil")
	}
	ymlRaw, err := fs.ReadFile(sfs, "Item.yml")
	if err != nil || !bytes.Contains(ymlRaw, []byte("name: Item")) {
		t.Fatalf("读取内嵌 schema 失败: %q err=%v", string(ymlRaw), err)
	}

	bgms, err := arch.ReadBGMDigests()
	if err != nil || len(bgms) != 1 || bgms[0].ContentMD5 != "abc123" {
		t.Fatalf("ReadBGMDigests 异常: %+v err=%v", bgms, err)
	}
	icons, err := arch.ReadIconDigests()
	if err != nil || len(icons) != 2 || icons[1].ID != 2500 || !icons[1].HasHQ || !icons[1].HasLocale {
		t.Fatalf("ReadIconDigests 异常: %+v err=%v", icons, err)
	}
	thumb, ok := arch.ReadIconThumbBlob("k100")
	if !ok || !bytes.Equal(thumb, []byte{'P', 1, 2, 3, 4}) {
		t.Fatalf("ReadIconThumbBlob 异常: ok=%v data=%v", ok, thumb)
	}
}

func makeDummySnapshotBytes(t *testing.T, region, patch, gameVer string) ([]byte, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), region+"-"+patch+".xivsnap")
	w, err := NewWriter(p, zstd.SpeedDefault)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.AddFile("exd/root.exl", []byte("EXLT,2\n"))
	langs := []string{"chs"}
	if region == RegionGlobal {
		langs = []string{"ja", "en"}
	}
	if err := w.Close(SnapshotMeta{
		ID:          MakeSnapshotID(region, patch),
		Region:      region,
		Patch:       patch,
		GameVersion: gameVer,
		Languages:   langs,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func TestClientFetchUnifiedCatalogAndDownloadVersion(t *testing.T) {
	cnBytes, cnSHA := makeDummySnapshotBytes(t, RegionCN, "7.55", "2026.09.10.0000.0000")
	glBytes, glSHA := makeDummySnapshotBytes(t, RegionGlobal, "7.55", "2026.09.10.0000.0000")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog.json":
			cat := Catalog{
				SchemaVersion: CatalogSchemaVersion,
				UpdatedAt:     "2026-11-26T00:00:00Z",
				Versions: []VersionCatalogEntry{{
					Patch:      "7.55",
					CreatedAt:  "2026-09-10T00:00:00Z",
					ReleaseTag: "v7.55",
					CN: &RegionAsset{
						GameVersion: "2026.09.10.0000.0000",
						Languages:   []string{"chs"},
						AssetName:   AssetFileName("7.55", RegionCN),
						DownloadURL: srv.URL + "/assets/7.55-cn",
						Size:        int64(len(cnBytes)),
						SHA256:      cnSHA,
					},
					Global: &RegionAsset{
						GameVersion: "2026.09.10.0000.0000",
						Languages:   []string{"ja", "en"},
						AssetName:   AssetFileName("7.55", RegionGlobal),
						DownloadURL: srv.URL + "/assets/7.55-global",
						Size:        int64(len(glBytes)),
						SHA256:      glSHA,
					},
				}},
			}
			_ = json.NewEncoder(w).Encode(cat)
		case "/assets/7.55-cn":
			_, _ = w.Write(cnBytes)
		case "/assets/7.55-global":
			_, _ = w.Write(glBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(ClientOptions{
		CatalogURL: srv.URL + "/catalog.json",
		HTTPClient: srv.Client(),
	})

	destDir := filepath.Join(t.TempDir(), "downloaded")
	cat, err := client.FetchCatalog(context.Background(), destDir)
	if err != nil || len(cat.Versions) != 1 || cat.Versions[0].Patch != "7.55" {
		t.Fatalf("FetchCatalog 异常: %+v err=%v", cat, err)
	}
	cached, ok := LoadCachedCatalog(destDir)
	if !ok || len(cached.Versions) != 1 {
		t.Fatalf("LoadCachedCatalog 失败: ok=%v cat=%+v", ok, cached)
	}

	// 双服模式一键下载统一版本 7.55，应自动拉取 cn 与 global 两个文件并汇总进度
	var lastDown, lastTotal int64
	paths, err := client.DownloadVersion(context.Background(), cat.Versions[0], []string{RegionCN, RegionGlobal}, destDir, func(d, tot int64) {
		lastDown = d
		lastTotal = tot
	})
	if err != nil || len(paths) != 2 {
		t.Fatalf("DownloadVersion(双服) 失败: paths=%v err=%v", paths, err)
	}
	wantSum := int64(len(cnBytes) + len(glBytes))
	if lastDown != wantSum || lastTotal != wantSum {
		t.Fatalf("合并下载进度异常: down=%d total=%d want=%d", lastDown, lastTotal, wantSum)
	}

	locals, err := ListLocalSnapshots(destDir)
	if err != nil || len(locals) != 2 {
		t.Fatalf("ListLocalSnapshots 异常: %+v err=%v", locals, err)
	}

	// 验证 SHA256 错误时拒绝落盘
	badVer := cat.Versions[0]
	badCN := *badVer.CN
	badCN.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	badVer.Patch = "7.99"
	badVer.CN = &badCN
	if _, err := client.DownloadVersion(context.Background(), badVer, []string{RegionCN}, destDir, nil); err == nil {
		t.Fatalf("SHA256 不匹配时必须报错")
	}

	// 按统一版本号一键删除应同时移除该版本的 cn 与 global 文件
	if err := DeleteLocalVersion(destDir, "7.55"); err != nil {
		t.Fatalf("DeleteLocalVersion 失败: %v", err)
	}
	localsAfter, _ := ListLocalSnapshots(destDir)
	if len(localsAfter) != 0 {
		t.Fatalf("删除后应无本地快照，实际: %d", len(localsAfter))
	}
}
