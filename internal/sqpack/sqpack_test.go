package sqpack

import (
	"path/filepath"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/config"
)

func TestHashAndSeString(t *testing.T) {
	// 验证基础纯文本与含 0x02 换行控制符的 SeString 解析
	plain := DecodeSeString([]byte("  图拉尔菠萝蛋糕  "))
	if plain != "图拉尔菠萝蛋糕" {
		t.Fatalf("期望纯文本 '图拉尔菠萝蛋糕'，实际 %q", plain)
	}

	// 含 LineBreak 标签: 0x02, 0x10 (16=LineBreak), 0x01 (len=0), 0x03
	withBreak := []byte{'A', 0x02, 0x10, 0x01, 0x03, 'B'}
	if got := DecodeSeString(withBreak); got != "A\nB" {
		t.Fatalf("期望换行解析 'A\\nB'，实际 %q", got)
	}
}

func TestRealLocalSqPackIfPresent(t *testing.T) {
	envPath := filepath.Join("..", "..", ".env")
	cfg := config.LoadAppConfigFromEnv(envPath)
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过真实数据验证: %v", err)
	}

	if resolved.HasCN() {
		reader := NewGameReader(resolved.CN.GamePath)
		defer reader.Close()

		// 1. 测试 root.exl 读取
		exlRaw, err := reader.ReadFile("exd/root.exl")
		if err != nil || exlRaw == nil {
			t.Fatalf("读取 exd/root.exl 失败: %v", err)
		}
		exl := ReadEXLFile(exlRaw)
		if len(exl.Entries) < 7000 {
			t.Fatalf("期望 root.exl 表数量 > 7000，实际 %d", len(exl.Entries))
		}

		// 2. 测试 Item 表读取（道具 #44099 图拉尔菠萝蛋糕）
		items, err := LoadSheetRows(reader, "Item", LangChineseSimplified, []int{9, 10, 11})
		if err != nil {
			t.Fatalf("读取 Item 表失败: %v", err)
		}
		row, ok := items[44099]
		if !ok {
			t.Fatalf("Item 表未找到 #44099")
		}
		if name := row.StringAt(9); name != "图拉尔菠萝蛋糕" {
			t.Fatalf("Item #44099 名称不匹配: got %q", name)
		}

		// 3. 测试图标 .tex 解码
		iconID := row.IntAt(10)
		img, err := ReadIconImage(reader, NewTextureCache(16), iconID, false)
		if err != nil || img == nil || img.Bounds().Dx() <= 0 {
			t.Fatalf("解码图标 %d 失败: img=%v err=%v", iconID, img, err)
		}

		// 4. 测试地图 .tex 解码 (s1f1/00)
		mapImg, err := ReadMapImage(reader, NewTextureCache(16), "s1f1/00")
		if err != nil || mapImg == nil || mapImg.Bounds().Dx() <= 0 {
			t.Fatalf("解码地图贴图 s1f1/00 失败: img=%v err=%v", mapImg, err)
		}
	}

	if resolved.HasGlobal() {
		reader := NewGameReader(resolved.Global.GamePath)
		defer reader.Close()

		itemsEn, err := LoadSheetRows(reader, "Item", LangEnglish, []int{9})
		if err != nil {
			t.Fatalf("读取国际服英文 Item 表失败: %v", err)
		}
		if name := itemsEn[44099].StringAt(9); name != "Turali Pineapple Ponzecake" {
			t.Fatalf("国际服英文名不匹配: got %q", name)
		}
	}
}
