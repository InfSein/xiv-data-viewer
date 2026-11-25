package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func boolPtr(v bool) *bool { return &v }

func buildTestPortableZip(t *testing.T, files map[string][]byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func makeFakePE(payload string) []byte {
	b := make([]byte, 128)
	b[0] = 'M'
	b[1] = 'Z'
	copy(b[2:], payload)
	return b
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.2.0", "2.1.0", 1},
		{"v2.10.0", "2.2.0", 1},
		{"2.1.0", "v2.1.0", 0},
		{"2.1.0", "2.2.0", -1},
		{"2.2.0", "2.2.0-rc.1", 1},
		{"2.2.0-beta.2", "2.2.0-beta.10", -1},
		{"2.2.0+build1", "2.2.0", 0},
		{"", "2.1.0", -1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckLatestFromAPI(t *testing.T) {
	zipBytes, zipSHA := buildTestPortableZip(t, map[string][]byte{
		defaultExeName: makeFakePE("new-v2.2.0"),
		"README.md":    []byte("v2.2.0"),
	})
	assetName := "xiv-data-viewer-portable-2.2.0-windows-amd64.zip"

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/InfSein/xiv-data-viewer/releases/latest":
			_ = json.NewEncoder(w).Encode(githubReleaseDTO{
				TagName:     "v2.2.0",
				Name:        "XIV Data Viewer v2.2.0",
				Body:        "## 更新内容\n- 新增内置更新功能",
				HTMLURL:     srv.URL + "/InfSein/xiv-data-viewer/releases/tag/v2.2.0",
				PublishedAt: "2026-11-25T10:00:00Z",
				Assets: []githubAssetDTO{
					{
						Name:               assetName,
						Size:               int64(len(zipBytes)),
						BrowserDownloadURL: srv.URL + "/download/" + assetName,
						Digest:             "sha256:" + zipSHA,
					},
					{
						Name:               checksumsAsset,
						Size:               128,
						BrowserDownloadURL: srv.URL + "/download/" + checksumsAsset,
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(Options{
		CurrentVersion: "2.1.0",
		OS:             "windows",
		Arch:           "amd64",
		IsDev:          boolPtr(false),
		APIBaseURL:     srv.URL,
		WebBaseURL:     srv.URL,
		HTTPClient:     srv.Client(),
	})

	res, err := client.Check(context.Background())
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !res.HasUpdate || res.LatestVersion != "2.2.0" || res.Release == nil {
		t.Fatalf("unexpected CheckResult: %+v", res)
	}
	if res.Release.AssetSHA256 != zipSHA {
		t.Fatalf("expected sha256 %s, got %s", zipSHA, res.Release.AssetSHA256)
	}

	// 同版本应返回 HasUpdate=false
	clientSame := NewClient(Options{
		CurrentVersion: "2.2.0",
		OS:             "windows",
		Arch:           "amd64",
		IsDev:          boolPtr(false),
		APIBaseURL:     srv.URL,
		WebBaseURL:     srv.URL,
		HTTPClient:     srv.Client(),
	})
	sameRes, err := clientSame.Check(context.Background())
	if err != nil {
		t.Fatalf("Check same version failed: %v", err)
	}
	if sameRes.HasUpdate {
		t.Fatalf("expected HasUpdate=false for 2.2.0, got %+v", sameRes)
	}
}

func TestCheckLatestRateLimitFallback(t *testing.T) {
	zipBytes, zipSHA := buildTestPortableZip(t, map[string][]byte{
		defaultExeName: makeFakePE("new-v2.3.0"),
	})
	assetName := "xiv-data-viewer-portable-2.3.0-windows-amd64.zip"
	sumsBody := fmt.Sprintf("%s  %s\n", zipSHA, assetName)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/InfSein/xiv-data-viewer/releases/latest":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
		case "/InfSein/xiv-data-viewer/releases/latest":
			http.Redirect(w, r, srv.URL+"/InfSein/xiv-data-viewer/releases/tag/v2.3.0", http.StatusFound)
		case "/InfSein/xiv-data-viewer/releases/tag/v2.3.0":
			w.WriteHeader(http.StatusOK)
		case "/InfSein/xiv-data-viewer/releases/download/v2.3.0/" + checksumsAsset:
			_, _ = w.Write([]byte(sumsBody))
		case "/InfSein/xiv-data-viewer/releases/download/v2.3.0/" + assetName:
			_, _ = w.Write(zipBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(Options{
		CurrentVersion: "2.1.0",
		OS:             "windows",
		Arch:           "amd64",
		IsDev:          boolPtr(false),
		APIBaseURL:     srv.URL,
		WebBaseURL:     srv.URL,
		HTTPClient:     srv.Client(),
	})

	res, err := client.Check(context.Background())
	if err != nil {
		t.Fatalf("Check fallback failed: %v", err)
	}
	if !res.HasUpdate || res.LatestVersion != "2.3.0" || res.Release == nil {
		t.Fatalf("unexpected fallback result: %+v", res)
	}
	if res.Release.AssetSHA256 != zipSHA || res.Release.AssetName != assetName {
		t.Fatalf("unexpected release info: %+v", res.Release)
	}
}

func TestDownloadAndInstallSuccessAndPreservesUninstaller(t *testing.T) {
	installDir := t.TempDir()
	exePath := filepath.Join(installDir, defaultExeName)
	oldPE := makeFakePE("old-v2.1.0")
	if err := os.WriteFile(exePath, oldPE, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "README.md"), []byte("old-readme"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "Uninstall.exe"), []byte("nsis-uninstaller"), 0o755); err != nil {
		t.Fatal(err)
	}

	newPE := makeFakePE("new-v2.2.0")
	zipBytes, zipSHA := buildTestPortableZip(t, map[string][]byte{
		defaultExeName: newPE,
		"README.md":    []byte("new-readme"),
		"icon.png":     []byte("png-data"),
	})
	assetName := "xiv-data-viewer-portable-2.2.0-windows-amd64.zip"
	sumsText := fmt.Sprintf("%s  %s\n", zipSHA, assetName)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/" + checksumsAsset:
			_, _ = w.Write([]byte(sumsText))
		case "/download/" + assetName:
			_, _ = w.Write(zipBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(Options{
		CurrentVersion: "2.1.0",
		OS:             "windows",
		Arch:           "amd64",
		ExePath:        exePath,
		IsDev:          boolPtr(false),
		HTTPClient:     srv.Client(),
	})

	rel := ReleaseInfo{
		Version:      "2.2.0",
		TagName:      "v2.2.0",
		AssetName:    assetName,
		AssetURL:     srv.URL + "/download/" + assetName,
		AssetSize:    int64(len(zipBytes)),
		ChecksumsURL: srv.URL + "/download/" + checksumsAsset,
	}

	var phases []InstallPhase
	err := client.DownloadAndInstall(context.Background(), rel, func(p Progress) {
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
	})
	if err != nil {
		t.Fatalf("DownloadAndInstall failed: %v", err)
	}

	gotExe, err := os.ReadFile(exePath)
	if err != nil || !bytes.Equal(gotExe, newPE) {
		t.Fatalf("exe was not replaced cleanly: err=%v len=%d", err, len(gotExe))
	}
	gotReadme, _ := os.ReadFile(filepath.Join(installDir, "README.md"))
	if string(gotReadme) != "new-readme" {
		t.Fatalf("README.md = %q, want new-readme", gotReadme)
	}
	gotIcon, _ := os.ReadFile(filepath.Join(installDir, "icon.png"))
	if string(gotIcon) != "png-data" {
		t.Fatalf("icon.png = %q, want png-data", gotIcon)
	}
	gotUninstall, _ := os.ReadFile(filepath.Join(installDir, "Uninstall.exe"))
	if string(gotUninstall) != "nsis-uninstaller" {
		t.Fatalf("Uninstall.exe should be preserved, got %q", gotUninstall)
	}
	if len(phases) == 0 || phases[len(phases)-1] != PhaseReplacing {
		t.Fatalf("expected phases ending with replacing, got %v", phases)
	}
}

func TestDownloadAndInstallChecksumMismatchDoesNotTouchApp(t *testing.T) {
	installDir := t.TempDir()
	exePath := filepath.Join(installDir, defaultExeName)
	oldPE := makeFakePE("old-v2.1.0")
	if err := os.WriteFile(exePath, oldPE, 0o755); err != nil {
		t.Fatal(err)
	}

	zipBytes, _ := buildTestPortableZip(t, map[string][]byte{
		defaultExeName: makeFakePE("tampered-v2.2.0"),
	})
	assetName := "xiv-data-viewer-portable-2.2.0-windows-amd64.zip"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	client := NewClient(Options{
		CurrentVersion: "2.1.0",
		OS:             "windows",
		Arch:           "amd64",
		ExePath:        exePath,
		IsDev:          boolPtr(false),
		HTTPClient:     srv.Client(),
	})

	err := client.DownloadAndInstall(context.Background(), ReleaseInfo{
		Version:     "2.2.0",
		AssetName:   assetName,
		AssetURL:    srv.URL + "/" + assetName,
		AssetSize:   int64(len(zipBytes)),
		AssetSHA256: strings.Repeat("0", 64),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected sha256 mismatch error, got %v", err)
	}

	gotExe, _ := os.ReadFile(exePath)
	if !bytes.Equal(gotExe, oldPE) {
		t.Fatalf("original exe was modified after failed verification")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(installDir, ".xiv-update-*")); len(leftovers) > 0 {
		t.Fatalf("staging directory was not cleaned up: %v", leftovers)
	}
}

func TestSwapDirectoryEntriesRollbackOnFailure(t *testing.T) {
	targetDir := t.TempDir()
	unpackedDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(targetDir, "a.txt"), []byte("old-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "z_locked.txt"), []byte("old-z"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 在目标目录放置阻塞对象使 aside 路径变成非空目录，强制触发 os.Rename 失败以验证完整回滚。
	blocker := filepath.Join(targetDir, fmt.Sprintf(".z_locked.txt.old-%d", os.Getpid()))
	if err := os.MkdirAll(filepath.Join(blocker, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(unpackedDir, "a.txt"), []byte("new-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unpackedDir, "new_only.txt"), []byte("new-only"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unpackedDir, "z_locked.txt"), []byte("new-z"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := swapDirectoryEntries(context.Background(), unpackedDir, targetDir); err == nil {
		t.Fatal("expected swapDirectoryEntries to fail on blocked rename")
	}
	_ = os.RemoveAll(blocker)

	gotA, _ := os.ReadFile(filepath.Join(targetDir, "a.txt"))
	if string(gotA) != "old-a" {
		t.Fatalf("a.txt not rolled back: got %q, want old-a", gotA)
	}
	gotZ, _ := os.ReadFile(filepath.Join(targetDir, "z_locked.txt"))
	if string(gotZ) != "old-z" {
		t.Fatalf("z_locked.txt = %q, want old-z", gotZ)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "new_only.txt")); !os.IsNotExist(err) {
		t.Fatalf("new_only.txt should have been rolled back, stat err=%v", err)
	}
}

func TestCanSelfUpdateAndCleanupLeftovers(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, defaultExeName)
	_ = os.WriteFile(exePath, makeFakePE("v1"), 0o755)

	devClient := NewClient(Options{
		CurrentVersion: "2.1.0",
		OS:             "windows",
		Arch:           "amd64",
		ExePath:        exePath,
		IsDev:          boolPtr(true),
	})
	if ok, reason := devClient.CanSelfUpdate(); ok || reason != DisableReasonDev {
		t.Fatalf("expected dev disabled, got ok=%v reason=%s", ok, reason)
	}

	prodLinux := NewClient(Options{
		CurrentVersion: "2.1.0",
		OS:             "linux",
		Arch:           "amd64",
		ExePath:        exePath,
		IsDev:          boolPtr(false),
	})
	if ok, reason := prodLinux.CanSelfUpdate(); ok || reason != DisableReasonPlatform {
		t.Fatalf("expected platform disabled, got ok=%v reason=%s", ok, reason)
	}

	if runtime.GOOS == "windows" {
		prodWin := NewClient(Options{
			CurrentVersion: "2.1.0",
			OS:             "windows",
			Arch:           "amd64",
			ExePath:        exePath,
			IsDev:          boolPtr(false),
		})
		if ok, reason := prodWin.CanSelfUpdate(); !ok || reason != DisableReasonNone {
			t.Fatalf("expected windows prod self-update enabled, got ok=%v reason=%s", ok, reason)
		}
	}

	staleOld := filepath.Join(dir, ".XIV Data Viewer.exe.old-999999")
	curOld := filepath.Join(dir, fmt.Sprintf(".XIV Data Viewer.exe.old-%d", os.Getpid()))
	staleStage := filepath.Join(dir, ".xiv-update-stale")
	_ = os.WriteFile(staleOld, []byte("old"), 0o644)
	_ = os.WriteFile(curOld, []byte("cur"), 0o644)
	_ = os.MkdirAll(staleStage, 0o755)
	oldTime := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(staleStage, oldTime, oldTime)

	CleanupLeftovers(exePath)

	if _, err := os.Stat(staleOld); !os.IsNotExist(err) {
		t.Fatalf("stale .old file should be removed, err=%v", err)
	}
	if _, err := os.Stat(curOld); err != nil {
		t.Fatalf("current pid .old file should not be removed: %v", err)
	}
	if _, err := os.Stat(staleStage); !os.IsNotExist(err) {
		t.Fatalf("stale staging dir should be removed, err=%v", err)
	}
}

func TestFormatReleaseNotes(t *testing.T) {
	raw := "## 更新说明\r\n- **内置更新**：支持 `v2.2.0` 自动检查\r\n---\r\n\r\n* 更安全的回滚机制"
	got := FormatReleaseNotes(raw)
	want := "更新说明\n• 内置更新：支持 v2.2.0 自动检查\n\n• 更安全的回滚机制"
	if got != want {
		t.Fatalf("FormatReleaseNotes:\ngot:  %q\nwant: %q", got, want)
	}
}
