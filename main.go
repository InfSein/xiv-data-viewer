package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/InfSein/xiv-data-viewer/internal/uiapp"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// appVersion 默认版本号，正式构建时也可通过 -ldflags "-X main.appVersion=x.y.z" 覆盖。
var appVersion = "2.1.0"

// userDataDirName 是应用持久化用户数据（配置、版本快照、窗口状态等）的目录名。
const userDataDirName = "xiv-data-viewer"

// resolveUserDataDir 解析并确保用户数据目录存在。
// 固定使用系统应用数据目录下的 xiv-data-viewer，并同步覆盖 mygo.PathUserData，
// 避免 Windows 打包后因 displayName 为中文而写入中文目录。
func resolveUserDataDir() string {
	if appDataDir, err := mygo.App.Path(mygo.PathAppData); err == nil && appDataDir != "" {
		dir := filepath.Join(appDataDir, userDataDirName)
		if err := os.MkdirAll(dir, 0o755); err == nil {
			mygo.App.SetPath(mygo.PathUserData, dir)
			return dir
		}
	}

	// 极端环境回退到当前工作目录下的 data 目录
	cwd, _ := os.Getwd()
	dir := filepath.Join(cwd, "data")
	_ = os.MkdirAll(dir, 0o755)
	mygo.App.SetPath(mygo.PathUserData, dir)
	return dir
}

func windowOptions(goos string) mygo.WindowOptions {
	opts := mygo.WindowOptions{
		Title:         "XIV Data Viewer",
		Width:         1502,
		Height:        845,
		MinWidth:      1024,
		MinHeight:     640,
		StateKey:      "xiv-data-viewer-main",
		TitleBarStyle: mygo.TitleBarHiddenInset,
	}
	switch goos {
	case "darwin":
		// macOS 使用侧边栏材质与隐藏原生标题栏，将交通灯垂直居中于自定义顶栏内
		opts.Vibrancy = mygo.VibrancySidebar
		opts.TitleBarStyle = mygo.TitleBarHidden
		opts.Transparent = true
		opts.TrafficLightPosition = &mygo.Point{X: 16, Y: 15}
	case "windows":
		// Windows 11 启用 Mica 材质与 40px 融合标题栏（参考 Godwit 客户端窗口规格）
		opts.Vibrancy = mygo.VibrancyMica
		opts.TitleBarHeight = 40
	}
	return opts
}

func main() {
	dataDir := resolveUserDataDir()

	ver := mygo.App.Version()
	if ver == "" {
		ver = appVersion
	}

	app := uiapp.NewApp(ver, dataDir, getSchemasFS())

	mygo.App.WhenReady(func() {
		// 在窗口创建前应用已持久化的外观主题，使窗口材质与原生控件同步跟随
		mygo.Theme.SetSource(app.ThemeSource())
		opts := windowOptions(runtime.GOOS)
		opts.Content = ui.View(app.View)
		win := mygo.NewWindow(opts)
		app.AttachWindow(win, opts.Vibrancy != mygo.VibrancyNone)
	})

	mygo.App.OnQuit(func() {
		app.Close()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
