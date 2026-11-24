package uiapp

import (
	"encoding/binary"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestEmbeddedGoogleSansAssets(t *testing.T) {
	if !strings.HasPrefix(fontStack, defaultEnglishFont+",") {
		t.Fatalf("期望 fontStack 以 %q 开头，实际: %q", defaultEnglishFont, fontStack)
	}
	for _, path := range embeddedFontFiles {
		data, err := embeddedFontsFS.ReadFile(path)
		if err != nil {
			t.Fatalf("读取内嵌字体 %s 失败: %v", path, err)
		}
		if len(data) < 12 {
			t.Fatalf("内嵌字体 %s 文件长度异常: %d", path, len(data))
		}
		// TrueType sfnt 版本头应为 0x00010000
		if ver := binary.BigEndian.Uint32(data[:4]); ver != 0x00010000 {
			t.Fatalf("内嵌字体 %s 不是有效的 TTF 头: %#08x", path, ver)
		}
	}
}

func TestGoogleSansRegisteredWeightsAndTabularNums(t *testing.T) {
	ensureDefaultFonts()
	if fontsErr != nil {
		msg := fontsErr.Error()
		if strings.Contains(msg, "no text engine") || strings.Contains(msg, "fontconfig") {
			t.Skipf("当前无头环境缺少字体渲染引擎，跳过渲染校验: %v", fontsErr)
		}
		t.Fatalf("注册默认英文字体失败: %v", fontsErr)
	}

	families := ui.FontFamilies()
	if !slices.ContainsFunc(families, func(f ui.FontFamily) bool {
		return f.Registered && f.Name == defaultEnglishFont && !f.Monospaced
	}) {
		t.Fatalf("未在已注册字体列表中找到非等宽字体 %q", defaultEnglishFont)
	}

	// 验证等宽数字 (tnum) 与默认比例数字区分
	measureWidth := func(family, text, feat string) float32 {
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Background(ui.RGB(255, 255, 255)).AlignItems(ui.Start).Children(func() {
				el := ui.Text(c, text).Font(family).FontSize(24).TextColor(ui.RGB(0, 0, 0))
				if feat != "" {
					el.FontFeatures(feat)
				}
			})
		}, 600, 80)
		rect, ok := tt.Find(text)
		if !ok {
			t.Fatalf("未能定位渲染文本 %q", text)
		}
		return rect.W
	}

	wProp1 := measureWidth(fontStack, "1111111111", "")
	wTab1 := measureWidth(fontStack, "1111111111", "tnum")
	wTab8 := measureWidth(fontStack, "8888888888", "tnum")
	if wTab1 != wTab8 {
		t.Fatalf("启用 tnum 后 1 与 8 宽度应一致: 1s=%.2f 8s=%.2f", wTab1, wTab8)
	}
	if wProp1 >= wTab1 {
		t.Fatalf("期望 Google Sans 比例数字 1 窄于等宽数字 1: prop=%.2f tnum=%.2f", wProp1, wTab1)
	}

	// 验证英文优先匹配 Google Sans，而非跳过至系统字体
	wStackEN := measureWidth(fontStack, "XIV Data Viewer", "")
	wDirectEN := measureWidth(defaultEnglishFont, "XIV Data Viewer", "")
	if wStackEN != wDirectEN {
		t.Fatalf("fontStack 英文渲染宽度应与直接使用 %q 一致: stack=%.2f direct=%.2f", defaultEnglishFont, wStackEN, wDirectEN)
	}

	// 验证不同字重在 fontStack 下可正常区分（Regular < Medium < Bold）
	inkByWeight := func(weight int) int64 {
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Background(ui.RGB(255, 255, 255)).AlignItems(ui.Start).Children(func() {
				ui.Text(c, "HHWW Lv.100 收藏品").Font(fontStack).FontSize(28).FontWeight(weight).TextColor(ui.RGB(0, 0, 0))
			})
		}, 600, 80)
		img := tt.Image()
		b := img.Bounds()
		var sum int64
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				sum += int64(255 - img.RGBAAt(x, y).R)
			}
		}
		return sum
	}
	ink400 := inkByWeight(400)
	ink500 := inkByWeight(500)
	ink700 := inkByWeight(700)
	if !(ink400 > 0 && ink400 < ink500 && ink500 < ink700) {
		t.Fatalf("期望字重墨量递增 (400 < 500 < 700): 400=%d 500=%d 700=%d", ink400, ink500, ink700)
	}
}
