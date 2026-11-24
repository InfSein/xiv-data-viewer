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
	fontsOnce sync.Once
	fontsErr  error
)

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
