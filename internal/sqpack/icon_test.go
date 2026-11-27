package sqpack

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
)

func TestIconPathsAndGroupHelpers(t *testing.T) {
	if got := IconGroupID(37618); got != 37000 {
		t.Fatalf("IconGroupID(37618) = %d, want 37000", got)
	}
	if got := IconGroupCode(37618); got != "037000" {
		t.Fatalf("IconGroupCode(37618) = %q, want 037000", got)
	}
	if got := IconGroupCode(0); got != "000000" {
		t.Fatalf("IconGroupCode(0) = %q, want 000000", got)
	}
	if got := IconResourcePath(37618, "", false); got != "ui/icon/037000/037618.tex" {
		t.Fatalf("IconResourcePath std = %q", got)
	}
	if got := IconResourcePath(37618, "hq", true); got != "ui/icon/037000/hq/037618_hr1.tex" {
		t.Fatalf("IconResourcePath hq hr1 = %q", got)
	}
	if got := IconExportRelPath(37618, "", false); got != "ui/icon/037000/037618.png" {
		t.Fatalf("IconExportRelPath std = %q", got)
	}
	if got := IconExportRelPath(37618, "chs", true); got != "ui/icon/037000/chs/037618_hr1.png" {
		t.Fatalf("IconExportRelPath chs hr1 = %q", got)
	}
}

func TestScanSyntheticIconArchiveSkipsEmpty(t *testing.T) {
	tmp := t.TempDir()
	sqDir := filepath.Join(tmp, "sqpack", "ffxiv")
	if err := os.MkdirAll(sqDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(sqDir, "060000.win32")

	// 构造两个文件块：offset 0 为 fileTypeTexture (#37618)，offset 256 为 fileTypeEmpty (#37619)，offset 512 为 fileTypeTexture (#0)
	datBuf := make([]byte, 1024)
	// #37618 texture header info
	binary.LittleEndian.PutUint32(datBuf[0:4], 128)
	binary.LittleEndian.PutUint32(datBuf[4:8], fileTypeTexture)
	binary.LittleEndian.PutUint32(datBuf[8:12], 96)
	// #37619 empty placeholder info
	binary.LittleEndian.PutUint32(datBuf[256:260], 128)
	binary.LittleEndian.PutUint32(datBuf[260:264], fileTypeEmpty)
	binary.LittleEndian.PutUint32(datBuf[264:268], 0)
	// #0 texture header info
	binary.LittleEndian.PutUint32(datBuf[512:516], 128)
	binary.LittleEndian.PutUint32(datBuf[516:520], fileTypeTexture)
	binary.LittleEndian.PutUint32(datBuf[520:524], 96)
	if err := os.WriteFile(prefix+".dat0", datBuf, 0o644); err != nil {
		t.Fatal(err)
	}

	// 构造最小合法 SqPack .index 文件
	type idxItem struct {
		path   string
		offset uint64
	}
	items := []idxItem{
		{path: "ui/icon/000000/000000.tex", offset: 512},
		{path: "ui/icon/037000/037618.tex", offset: 0},
		{path: "ui/icon/037000/037619.tex", offset: 256},
	}
	idxData := make([]byte, 2048+len(items)*16)
	copy(idxData[0:8], sqPackMagic)
	binary.LittleEndian.PutUint32(idxData[12:16], 1024) // headerSize
	binary.LittleEndian.PutUint32(idxData[1024+8:1024+12], 2048)
	binary.LittleEndian.PutUint32(idxData[1024+12:1024+16], uint32(len(items)*16))
	for i, it := range items {
		h, err := CalculateIndexHash(it.path)
		if err != nil {
			t.Fatal(err)
		}
		pos := 2048 + i*16
		binary.LittleEndian.PutUint32(idxData[pos:pos+4], uint32(h))
		binary.LittleEndian.PutUint32(idxData[pos+4:pos+8], uint32(h>>32))
		packed := uint32((it.offset / 8) & ^uint64(0xF))
		binary.LittleEndian.PutUint32(idxData[pos+8:pos+12], packed)
	}
	if err := os.WriteFile(prefix+".index", idxData, 0o644); err != nil {
		t.Fatal(err)
	}

	reader := NewGameReader(tmp)
	defer reader.Close()
	got, err := ScanIconFiles(reader)
	if err != nil {
		t.Fatalf("ScanIconFiles 失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("期望剔除空占位后剩余 2 个图标，实际 %d: %+v", len(got), got)
	}
	if got[0].IconID != 0 || got[1].IconID != 37618 {
		t.Fatalf("扫描图标顺序或 ID 不符: %+v", got)
	}

	// 验证版本指纹缓存写入、二次命中复用以及客户端更新后自动失效重扫
	cacheDir := filepath.Join(tmp, "icon-cache")
	cached1, err := ScanIconFilesCached(reader, cacheDir)
	if err != nil {
		t.Fatalf("首次 ScanIconFilesCached 失败: %v", err)
	}
	if len(cached1) != 2 || cached1[0].IconID != 0 || cached1[1].IconID != 37618 {
		t.Fatalf("首次缓存扫描结果异常: %+v", cached1)
	}
	cacheEntries, err := os.ReadDir(cacheDir)
	if err != nil || len(cacheEntries) != 1 {
		t.Fatalf("期望写入 1 个图标缓存文件，实际 %d (err=%v)", len(cacheEntries), err)
	}

	cached2, err := ScanIconFilesCached(reader, cacheDir)
	if err != nil || len(cached2) != 2 || cached2[1].Path != "ui/icon/037000/037618.tex" {
		t.Fatalf("命中缓存读取异常: len=%d err=%v", len(cached2), err)
	}

	// 模拟游戏版本更新：更新 ffxivgame.ver 后应生成新指纹缓存而非误用旧缓存
	if err := os.WriteFile(filepath.Join(tmp, "ffxivgame.ver"), []byte("2026.12.01.0000.0000"), 0o644); err != nil {
		t.Fatal(err)
	}
	cached3, err := ScanIconFilesCached(reader, cacheDir)
	if err != nil || len(cached3) != 2 {
		t.Fatalf("版本变更后重扫失败: len=%d err=%v", len(cached3), err)
	}
	cacheEntries2, err := os.ReadDir(cacheDir)
	if err != nil || len(cacheEntries2) < 2 {
		t.Fatalf("期望版本变更后生成新的指纹缓存文件，实际 %d", len(cacheEntries2))
	}
}

func TestDownscaleAndPNGBestSpeed(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 256, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 256; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 120, G: 80, B: 40, A: 255})
		}
	}
	scaled := downscaleRGBAIfNeeded(img, 64)
	if scaled.Bounds().Dx() != 64 || scaled.Bounds().Dy() != 32 {
		t.Fatalf("缩略图比例异常: %v", scaled.Bounds())
	}

	raw := buildSyntheticTex(TexFormatB8G8R8A8, 2, 2, []byte{
		10, 20, 30, 255, 10, 20, 30, 255,
		10, 20, 30, 255, 10, 20, 30, 255,
	})
	pngBytes, err := DecodeTexToPNGWithCompression(raw, png.BestSpeed)
	if err != nil {
		t.Fatalf("DecodeTexToPNGWithCompression 失败: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(pngBytes)); err != nil {
		t.Fatalf("PNG 解码失败: %v", err)
	}
}

func TestRealClientScanIconFiles(t *testing.T) {
	envPath := filepath.Join("..", "..", ".env")
	cfg := config.LoadAppConfigFromEnv(envPath)
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil || !resolved.HasCN() {
		t.Skipf("本地未配置国服客户端，跳过真实图标全量索引测试")
	}

	reader := NewGameReader(resolved.CN.GamePath)
	defer reader.Close()

	t0 := time.Now()
	files, err := ScanIconFiles(reader)
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("ScanIconFiles 失败: %v", err)
	}
	t.Logf("扫描国服真实图标资源完成: %d 个文件, 耗时 %s", len(files), elapsed)
	if len(files) < 100000 {
		t.Fatalf("期望国服有效图标文件数 > 100000，实际 %d", len(files))
	}

	// 验证 #37618 存在且属于 037000 组，且可解码变体
	found37618 := false
	for _, f := range files {
		if f.IconID == 37618 {
			if f.GroupID != 37000 {
				t.Fatalf("图标 #37618 分组应为 37000，实际 %d", f.GroupID)
			}
			found37618 = true
			break
		}
	}
	if !found37618 {
		t.Fatalf("未扫描到图标 #37618")
	}

	img, hdr, err := ReadIconVariantImage(reader, NewTextureCache(8), 37618, "", false, 0)
	if err != nil || img == nil || hdr.Width == 0 {
		t.Fatalf("读取 037618 变体失败: img=%v hdr=%+v err=%v", img, hdr, err)
	}
}
