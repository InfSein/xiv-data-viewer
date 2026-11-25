package uiapp

import (
	"embed"
	"fmt"
	"log"
	"sync"

	"github.com/egoist/mygo/ui"
)

// defaultEnglishFont 是客户端默认使用的内嵌西文与数字字体家族名称。
const defaultEnglishFont = "Google Sans"

//go:embed fonts/GoogleSans-Regular.ttf fonts/GoogleSans-Medium.ttf fonts/GoogleSans-Bold.ttf
var embeddedFontsFS embed.FS

var embeddedFontFiles = []string{
	"fonts/GoogleSans-Regular.ttf",
	"fonts/GoogleSans-Medium.ttf",
	"fonts/GoogleSans-Bold.ttf",
}

var (
	fontsOnce   sync.Once
	fontsErr    error
	prewarmOnce sync.Once
)

// prewarmLanguageFonts 在真实窗口就绪后于后台预热中/日/英三语字体回退链与常用字形，
// 避免首次切换语言时在主 UI 线程现场加载日语/中文系统字体造成卡顿。
func prewarmLanguageFonts() {
	prewarmOnce.Do(func() {
		ensureDefaultFonts()
		go func() {
			const sample = "简体中文日本語English XIV 0123456789 物品任务理符成就设置アイテムクエストリーヴアチーブ設定"
			specs := []struct {
				family string
				lang   string
			}{
				{fontStack, "zh-Hans"},
				{fontStack, "en"},
				{fontStackJA, "ja"},
			}
			weights := []int{400, 500, 600}
			sizes := []float32{11, 12, 13, 15, 20, 24}
			for _, sp := range specs {
				for _, w := range weights {
					for _, sz := range sizes {
						_ = ui.ShapeText(sample, ui.Font{
							Family: sp.family,
							Size:   sz,
							Weight: w,
							Lang:   sp.lang,
						}, 0)
					}
				}
			}
		}()
	})
}

// ensureDefaultFonts 幂等地注册内嵌的 Google Sans 字体字重（Regular/Medium/Bold），
// 使英文、数字及西文符号在各平台均优先使用 Google Sans，中文回退至系统字体。
func ensureDefaultFonts() {
	fontsOnce.Do(func() {
		for _, path := range embeddedFontFiles {
			data, err := embeddedFontsFS.ReadFile(path)
			if err != nil {
				fontsErr = fmt.Errorf("读取内嵌字体 %s 失败: %w", path, err)
				log.Printf("%v", fontsErr)
				return
			}
			if err := ui.RegisterFont(data, defaultEnglishFont); err != nil {
				fontsErr = fmt.Errorf("注册内嵌字体 %s 失败: %w", path, err)
				log.Printf("%v", fontsErr)
				return
			}
		}
	})
}
