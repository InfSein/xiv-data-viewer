package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/changelog"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/resources"
	"github.com/egoist/mygo"
)

func TestResolveUserDataDirUsesSlugDirectory(t *testing.T) {
	appDataRoot := t.TempDir()
	mygo.App.SetName("艾欧泽亚数据工作台")
	mygo.App.SetPath(mygo.PathAppData, appDataRoot)
	mygo.App.SetPath(mygo.PathUserData, "")
	t.Cleanup(func() {
		mygo.App.SetName("")
		mygo.App.SetPath(mygo.PathAppData, "")
		mygo.App.SetPath(mygo.PathUserData, "")
	})

	wantDir := filepath.Join(appDataRoot, "xiv-data-viewer")
	gotDir := resolveUserDataDir()
	if gotDir != wantDir {
		t.Fatalf("resolveUserDataDir() = %q, want %q", gotDir, wantDir)
	}

	info, err := os.Stat(gotDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("期望自动创建用户数据目录 %q: err=%v", gotDir, err)
	}

	gotMyGoDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		t.Fatalf("mygo.App.Path(PathUserData) 返回错误: %v", err)
	}
	if gotMyGoDir != wantDir {
		t.Fatalf("mygo.PathUserData = %q, want %q", gotMyGoDir, wantDir)
	}
}

func TestResolveUserDataDirFallbackWhenAppDataUnwritable(t *testing.T) {
	tmpRoot := t.TempDir()
	// 用普通文件充当 PathAppData，确保拼接子目录并 MkdirAll 时必然失败以验证回退路径。
	notADir := filepath.Join(tmpRoot, "file-as-appdata")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatalf("创建占位文件失败: %v", err)
	}

	workDir := filepath.Join(tmpRoot, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("创建工作目录失败: %v", err)
	}
	t.Chdir(workDir)

	mygo.App.SetPath(mygo.PathAppData, notADir)
	mygo.App.SetPath(mygo.PathUserData, "")
	t.Cleanup(func() {
		mygo.App.SetPath(mygo.PathAppData, "")
		mygo.App.SetPath(mygo.PathUserData, "")
	})

	wantDir := filepath.Join(workDir, "data")
	gotDir := resolveUserDataDir()
	if gotDir != wantDir {
		t.Fatalf("resolveUserDataDir() 回退路径 = %q, want %q", gotDir, wantDir)
	}

	gotMyGoDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || gotMyGoDir != wantDir {
		t.Fatalf("回退时 mygo.PathUserData = %q (err=%v), want %q", gotMyGoDir, err, wantDir)
	}
}

func TestEmbeddedChangelogAllLanguagesSynced(t *testing.T) {
	fsys := getChangelogFS()
	zhDoc, err := changelog.LoadForLang(fsys, i18n.LangZH)
	if err != nil {
		t.Fatalf("加载内嵌中文更新日志失败: %v", err)
	}
	enDoc, err := changelog.LoadForLang(fsys, i18n.LangEN)
	if err != nil {
		t.Fatalf("加载内嵌英文更新日志失败: %v", err)
	}
	jaDoc, err := changelog.LoadForLang(fsys, i18n.LangJA)
	if err != nil {
		t.Fatalf("加载内嵌日文更新日志失败: %v", err)
	}

	if len(zhDoc.Releases) == 0 {
		t.Fatalf("中文更新日志不应为空")
	}
	if zhDoc.Intro != "" || enDoc.Intro != "" || jaDoc.Intro != "" {
		t.Fatalf("三语更新日志不应再包含规范导语段落: zh=%q en=%q ja=%q", zhDoc.Intro, enDoc.Intro, jaDoc.Intro)
	}
	if len(enDoc.Releases) != len(zhDoc.Releases) || len(jaDoc.Releases) != len(zhDoc.Releases) {
		t.Fatalf("三语更新日志版本数量不一致: zh=%d en=%d ja=%d", len(zhDoc.Releases), len(enDoc.Releases), len(jaDoc.Releases))
	}
	for i := range zhDoc.Releases {
		if zhDoc.Releases[i].Version != enDoc.Releases[i].Version || zhDoc.Releases[i].Version != jaDoc.Releases[i].Version {
			t.Fatalf("版本 #%d 标识不一致: zh=%q en=%q ja=%q", i, zhDoc.Releases[i].Version, enDoc.Releases[i].Version, jaDoc.Releases[i].Version)
		}
		if zhDoc.Releases[i].TotalItems != enDoc.Releases[i].TotalItems || zhDoc.Releases[i].TotalItems != jaDoc.Releases[i].TotalItems {
			t.Fatalf("版本 %s 条目数不一致: zh=%d en=%d ja=%d", zhDoc.Releases[i].Version, zhDoc.Releases[i].TotalItems, enDoc.Releases[i].TotalItems, jaDoc.Releases[i].TotalItems)
		}
	}
}

func TestEmbeddedAppIconPNGValidAndTransparentCorners(t *testing.T) {
	if len(resources.IconPNG) == 0 {
		t.Fatalf("内嵌应用图标 IconPNG 不应为空")
	}
	img, err := png.Decode(bytes.NewReader(resources.IconPNG))
	if err != nil {
		t.Fatalf("解码内嵌应用图标 PNG 失败: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 1024 || b.Dy() != 1024 {
		t.Fatalf("应用图标尺寸 = %dx%d, 期望 1024x1024", b.Dx(), b.Dy())
	}
	for _, pt := range [][2]int{{0, 0}, {1023, 0}, {0, 1023}, {1023, 1023}} {
		_, _, _, a := img.At(pt[0], pt[1]).RGBA()
		if a != 0 {
			t.Fatalf("应用图标四角 (%d,%d) 期望完全透明，实际 alpha=%d", pt[0], pt[1], a)
		}
	}
	_, _, _, centerA := img.At(512, 512).RGBA()
	if centerA == 0 {
		t.Fatalf("应用图标中心区域不应透明")
	}
}
