package main

import (
	"os"
	"path/filepath"
	"testing"

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
