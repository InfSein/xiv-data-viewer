package exporter

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

func TestFormatCSVCell(t *testing.T) {
	cases := []struct {
		val   any
		isStr bool
		want  string
	}{
		{"金币", true, `"金币"`},
		{"", true, `""`},
		{nil, true, `""`},
		{`Say "Hello"`, true, `"Say ""Hello"""`},
		{"Item with, comma", true, `"Item with, comma"`},
		{true, false, "True"},
		{false, false, "False"},
		{12345, false, "12345"},
		{nil, false, ""},
	}
	for _, tc := range cases {
		if got := FormatCSVCell(tc.val, tc.isStr); got != tc.want {
			t.Fatalf("FormatCSVCell(%v, %v) = %q, want %q", tc.val, tc.isStr, got, tc.want)
		}
	}
}

func TestRealExporterAndSchemas(t *testing.T) {
	cfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过 CSV 导出测试: %v", err)
	}

	var cnReader, glReader *sqpack.GameReader
	if resolved.HasCN() {
		cnReader = sqpack.NewGameReader(resolved.CN.GamePath)
		defer cnReader.Close()
	}
	if resolved.HasGlobal() {
		glReader = sqpack.NewGameReader(resolved.Global.GamePath)
		defer glReader.Close()
	}

	sm := NewSchemaManager(os.DirFS(filepath.Join("..", "..", "schemas")))
	exp := NewService(cnReader, glReader, sm)

	if cnReader != nil {
		meta, err := sm.GetSheetSchema(cnReader, "Item")
		if err != nil {
			t.Fatalf("解析 Item schema 失败: %v", err)
		}
		if len(meta.Columns) <= 80 || meta.Columns[0].Name != "Singular" || meta.Columns[8].Name != "Description" || meta.Columns[9].Name != "Name" {
			t.Fatalf("Item schema 列对齐异常: col0=%+v col8=%+v col9=%+v", meta.Columns[0], meta.Columns[8], meta.Columns[9])
		}

		all := sm.GetAllSheetNames(cnReader)
		if len(all) <= 7000 {
			t.Fatalf("全量表数量异常: %d", len(all))
		}

		chsCSV, err := exp.ExportSheetToCSV("CraftType", "chs")
		if err != nil {
			t.Fatalf("导出 CraftType(chs) 失败: %v", err)
		}
		if !strings.HasPrefix(chsCSV, "\uFEFFkey,0,1") || !strings.Contains(chsCSV, `"木工"`) {
			t.Fatalf("CraftType(chs) CSV 格式不符: %q", chsCSV[:min(200, len(chsCSV))])
		}

		// 验证本地解包清理旧目录并写出新 CSV
		outDir := t.TempDir()
		stale := filepath.Join(outDir, "chs", "stale.txt")
		if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := exp.StartUnpackToLocalDirectory(outDir, []string{"chs"}, []string{"CraftType"}, nil); err != nil {
			t.Fatalf("启动本地解包失败: %v", err)
		}
		waitExportDone(t, exp)

		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatalf("旧文件应已被清理: %s", stale)
		}
		if _, err := os.Stat(filepath.Join(outDir, "chs", "CraftType.csv")); err != nil {
			t.Fatalf("未生成 CraftType.csv: %v", err)
		}

		// 验证 Zip 导出
		zipPath := filepath.Join(t.TempDir(), "out.zip")
		if err := exp.StartExportToZipFile(zipPath, []string{"chs"}, []string{"CraftType"}, nil); err != nil {
			t.Fatalf("启动 Zip 导出失败: %v", err)
		}
		waitExportDone(t, exp)
		zr, err := zip.OpenReader(zipPath)
		if err != nil {
			t.Fatalf("打开导出的 Zip 失败: %v", err)
		}
		defer zr.Close()
		if len(zr.File) != 1 || zr.File[0].Name != "chs/CraftType.csv" {
			t.Fatalf("Zip 条目不符合预期: %+v", zr.File)
		}
	}
}

func waitExportDone(t *testing.T, exp *Service) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		p := exp.GetProgress()
		if p != nil && !p.IsRunning {
			if !p.IsCompleted {
				t.Fatalf("导出未成功完成: %+v", p)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("等待导出超时")
}
