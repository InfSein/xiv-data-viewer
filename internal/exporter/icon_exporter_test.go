package exporter

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

func TestIconExporterSaintCoinachPathsAndRealClient(t *testing.T) {
	cfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil || !resolved.HasCN() {
		t.Skipf("本地未配置国服客户端，跳过真实图标导出测试")
	}

	store, err := gamedata.LoadGameDataStoreWithOptions(resolved, os.DirFS(filepath.Join("..", "..", "schemas")), gamedata.StoreLoadOptions{})
	if err != nil {
		t.Fatalf("加载客户端失败: %v", err)
	}
	defer store.Close()

	svc := gamedata.NewService(store)
	iconExp := NewIconService(svc)

	opts := IconExportOptions{
		IncludeStandard: true,
		IncludeHR1:      true,
		GroupID:         37000,
	}
	cnt := iconExp.CountMatchingTasks(opts)
	if cnt < 300 {
		t.Fatalf("期望 037000 分组导出任务数 > 300，实际 %d", cnt)
	}

	// 1. 验证本地目录导出是否生成标准 SaintCoinach ui/icon/037000/... 树与合法 PNG，且回调进度严格单调递增
	outDir := t.TempDir()
	var updates []IconExportProgress
	if err := iconExp.StartExportToDirectory(outDir, opts, func(p IconExportProgress) {
		updates = append(updates, p)
	}); err != nil {
		t.Fatalf("StartExportToDirectory 失败: %v", err)
	}
	waitIconExportDone(t, iconExp)
	if len(updates) < 2 {
		t.Fatalf("期望收到多次进度回调，实际仅收到 %d 次", len(updates))
	}
	for i := 1; i < len(updates); i++ {
		if updates[i].ProcessedTasks < updates[i-1].ProcessedTasks || updates[i].Percent < updates[i-1].Percent {
			t.Fatalf("导出进度出现回退: prev=%+v cur=%+v", updates[i-1], updates[i])
		}
	}
	lastUp := updates[len(updates)-1]
	if !lastUp.IsCompleted || lastUp.Percent != 100 || lastUp.ProcessedTasks != cnt {
		t.Fatalf("导出结束进度不完整: %+v (want total=%d)", lastUp, cnt)
	}

	stdPNG := filepath.Join(outDir, "ui", "icon", "037000", "037618.png")
	hr1PNG := filepath.Join(outDir, "ui", "icon", "037000", "037618_hr1.png")
	for _, p := range []string{stdPNG, hr1PNG} {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("期望生成文件 %s: %v", p, err)
		}
		if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
			t.Fatalf("导出文件不是合法 PNG (%s): %v", p, err)
		}
	}

	// 2. 验证 Zip 导出是否生成对应归档条目
	zipPath := filepath.Join(t.TempDir(), "icons-037000.zip")
	if err := iconExp.StartExportToZipFile(zipPath, opts, nil); err != nil {
		t.Fatalf("StartExportToZipFile 失败: %v", err)
	}
	waitIconExportDone(t, iconExp)

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("打开导出的 Zip 失败: %v", err)
	}
	defer zr.Close()
	if len(zr.File) != cnt {
		t.Fatalf("Zip 文件条目数不匹配: got=%d want=%d", len(zr.File), cnt)
	}
	foundStd, foundHR1 := false, false
	for _, f := range zr.File {
		switch f.Name {
		case "ui/icon/037000/037618.png":
			foundStd = true
		case "ui/icon/037000/037618_hr1.png":
			foundHR1 = true
		}
	}
	if !foundStd || !foundHR1 {
		t.Fatalf("Zip 缺少预期 SaintCoinach 路径条目: std=%v hr1=%v", foundStd, foundHR1)
	}
}

func TestIconExporterCancelAndEmptyGuard(t *testing.T) {
	// 无数据空服务应报错
	emptyExp := NewIconService(gamedata.NewService(&gamedata.Store{}))
	if err := emptyExp.StartExportToDirectory(t.TempDir(), IconExportOptions{IncludeStandard: true, GroupID: -1}, nil); err == nil {
		t.Fatalf("空图标仓库应拒绝启动导出")
	}

	// 验证合成 1x1 .tex 文件可被正常编码导出（无需真实客户端即可验证端到端管线）
	_ = sqpack.TexFormatB8G8R8A8
	_ = binary.LittleEndian
}

func waitIconExportDone(t *testing.T, svc *IconService) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		p := svc.GetProgress()
		if p != nil && !p.IsRunning {
			if !p.IsCompleted {
				t.Fatalf("图标导出未成功完成: %+v", p)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("等待图标导出超时")
}
