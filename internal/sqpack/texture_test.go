package sqpack

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/config"
)

func buildSyntheticTex(format uint32, w, h int, payload []byte) []byte {
	raw := make([]byte, textureHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(raw[0:4], 0x800000)
	binary.LittleEndian.PutUint32(raw[4:8], format)
	binary.LittleEndian.PutUint16(raw[8:10], uint16(w))
	binary.LittleEndian.PutUint16(raw[10:12], uint16(h))
	binary.LittleEndian.PutUint16(raw[12:14], 1)
	raw[14] = 1
	raw[15] = 1
	binary.LittleEndian.PutUint32(raw[28:32], textureHeaderSize)
	copy(raw[textureHeaderSize:], payload)
	return raw
}

func assertValidPremultipliedRGBA(t *testing.T, label string, img *image.RGBA) {
	t.Helper()
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.R > c.A || c.G > c.A || c.B > c.A {
				t.Fatalf("%s (%d,%d) 违反预乘 Alpha 约束: RGBA=%+v", label, x, y, c)
			}
		}
	}
}

func TestDecodeTexPremultipliedAlpha(t *testing.T) {
	t.Run("BGRA8 透明白底与半透明边缘", func(t *testing.T) {
		// 像素 0: 透明白 (B=255,G=255,R=255,A=0)
		// 像素 1: 半透明橙 (B=0,G=128,R=255,A=128)
		// 像素 2: 不透明蓝 (B=255,G=0,R=0,A=255)
		// 像素 3: 透明纯黑 (B=0,G=0,R=0,A=0)
		payload := []byte{
			255, 255, 255, 0,
			0, 128, 255, 128,
			255, 0, 0, 255,
			0, 0, 0, 0,
		}
		raw := buildSyntheticTex(TexFormatB8G8R8A8, 2, 2, payload)

		rgba, err := DecodeTexToRGBA(raw)
		if err != nil {
			t.Fatalf("DecodeTexToRGBA 失败: %v", err)
		}
		assertValidPremultipliedRGBA(t, "BGRA8", rgba)

		if got := rgba.RGBAAt(0, 0); got.R != 0 || got.G != 0 || got.B != 0 || got.A != 0 {
			t.Fatalf("透明白像素预乘后应清零 RGB，实际 %+v", got)
		}
		if got := rgba.RGBAAt(1, 0); got.R != 128 || got.G != 64 || got.B != 0 || got.A != 128 {
			t.Fatalf("半透明像素预乘结果异常: got=%+v", got)
		}
		if got := rgba.RGBAAt(0, 1); got.R != 0 || got.G != 0 || got.B != 255 || got.A != 255 {
			t.Fatalf("不透明像素不应改变色值: got=%+v", got)
		}

		// 验证 PNG 导出保留直通半透明色彩且不溢出
		pngBytes, err := DecodeTexToPNG(raw)
		if err != nil {
			t.Fatalf("DecodeTexToPNG 失败: %v", err)
		}
		decoded, err := png.Decode(bytes.NewReader(pngBytes))
		if err != nil {
			t.Fatalf("解码生成 PNG 失败: %v", err)
		}
		nrgba, ok := decoded.(*image.NRGBA)
		if !ok {
			t.Fatalf("期望 PNG 解码为 *image.NRGBA，实际 %T", decoded)
		}
		if got := nrgba.NRGBAAt(1, 0); got.R != 255 || got.G != 128 || got.B != 0 || got.A != 128 {
			t.Fatalf("PNG 导出半透明直通色彩失真: got=%+v", got)
		}
	})

	t.Run("B4G4R4A4 与 B5G5R5A1 与 A8", func(t *testing.T) {
		// B4G4R4A4: A=0, R=15, G=15, B=15 -> 0x0FFF
		p4444 := make([]byte, 2)
		binary.LittleEndian.PutUint16(p4444, 0x0FFF)
		img4444, err := DecodeTexToRGBA(buildSyntheticTex(TexFormatB4G4R4A4, 1, 1, p4444))
		if err != nil {
			t.Fatal(err)
		}
		assertValidPremultipliedRGBA(t, "B4G4R4A4", img4444)

		// B5G5R5A1: A=0, R=31, G=31, B=31 -> 0x7FFF
		p5551 := make([]byte, 2)
		binary.LittleEndian.PutUint16(p5551, 0x7FFF)
		img5551, err := DecodeTexToRGBA(buildSyntheticTex(TexFormatB5G5R5A1, 1, 1, p5551))
		if err != nil {
			t.Fatal(err)
		}
		assertValidPremultipliedRGBA(t, "B5G5R5A1", img5551)

		// A8: 透明 0 与半透明 128
		imgA8, err := DecodeTexToRGBA(buildSyntheticTex(TexFormatA8, 2, 1, []byte{0, 128}))
		if err != nil {
			t.Fatal(err)
		}
		assertValidPremultipliedRGBA(t, "A8", imgA8)
		if got := imgA8.RGBAAt(1, 0); got.R != 128 || got.G != 128 || got.B != 128 || got.A != 128 {
			t.Fatalf("A8 半透明像素预乘异常: got=%+v", got)
		}
	})

	t.Run("DXT3 与 DXT5 与 BC7 透明白底", func(t *testing.T) {
		// DXT3: 8B alpha=0 + 8B color=(0xFFFF, 0xFFFF, 0) -> 全白底透明
		dxt3 := make([]byte, 16)
		binary.LittleEndian.PutUint16(dxt3[8:10], 0xFFFF)
		binary.LittleEndian.PutUint16(dxt3[10:12], 0xFFFF)
		img3, err := DecodeTexToRGBA(buildSyntheticTex(TexFormatDXT3, 4, 4, dxt3))
		if err != nil {
			t.Fatal(err)
		}
		assertValidPremultipliedRGBA(t, "DXT3", img3)

		// DXT5: 8B alpha=(0,0,...) + 8B color=(0xFFFF, 0xFFFF, 0)
		dxt5 := make([]byte, 16)
		binary.LittleEndian.PutUint16(dxt5[8:10], 0xFFFF)
		binary.LittleEndian.PutUint16(dxt5[10:12], 0xFFFF)
		img5, err := DecodeTexToRGBA(buildSyntheticTex(TexFormatDXT5, 4, 4, dxt5))
		if err != nil {
			t.Fatal(err)
		}
		assertValidPremultipliedRGBA(t, "DXT5", img5)

		// BC7 Mode 6: bits 0..6 = 0x40, bits 7..48 = all 1s (R/G/B=254), alpha=0
		bc7 := make([]byte, 16)
		lo := uint64(0x40) | (((uint64(1) << 42) - 1) << 7)
		binary.LittleEndian.PutUint64(bc7[0:8], lo)
		img7, err := DecodeTexToRGBA(buildSyntheticTex(TexFormatBC7, 4, 4, bc7))
		if err != nil {
			t.Fatal(err)
		}
		assertValidPremultipliedRGBA(t, "BC7", img7)
	})
}

func TestRealClientIconsPremultiplied(t *testing.T) {
	envPath := filepath.Join("..", "..", ".env")
	cfg := config.LoadAppConfigFromEnv(envPath)
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil || !resolved.HasCN() {
		t.Skipf("本地未配置国服客户端，跳过真实透明图标校验")
	}

	reader := NewGameReader(resolved.CN.GamePath)
	defer reader.Close()
	cache := NewTextureCache(32)

	// 覆盖金币/神典石/工票货币(BGRA8)、能工巧匠职业(BGRA8)、采集(BGRA8)、透明物品(BGRA8)、成就(B5G5R5A1)与常见物品(DXT1)
	iconIDs := []int{1, 27900, 27905, 44099, 60437, 60438, 62008, 62015, 62018, 65001, 65002, 65004, 65042}
	for _, id := range iconIDs {
		img, err := ReadIconImage(reader, cache, id, false)
		if err != nil || img == nil {
			t.Fatalf("读取图标 %d 失败: img=%v err=%v", id, img, err)
		}
		assertValidPremultipliedRGBA(t, "Icon", img)
	}
}
