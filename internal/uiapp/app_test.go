package uiapp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/egoist/mygo/ui"
)

func TestFirstRunSetupAndSettingsFlow(t *testing.T) {
	dataDir := t.TempDir()
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
	defer app.Close()

	// 1. 首次启动未配置路径时应停留在引导设置页面
	if app.mode != modeSetup {
		t.Fatalf("期望首次启动进入 modeSetup，实际: %v", app.mode)
	}

	tt := ui.NewTester(app.View, 1366, 768)
	if !tt.HasText("欢迎使用艾欧泽亚数据工作台") {
		t.Fatalf("引导页标题未渲染: %q", tt.Texts())
	}

	// 2. 填写无效路径应报错并留在引导页
	if app.SaveAndReloadPaths(filepath.Join(dataDir, "not-exists"), "") {
		t.Fatalf("无效路径不应保存成功")
	}
	tt.Frame()
	if app.mode != modeSetup || app.draftError == "" {
		t.Fatalf("期望停留在引导页并显示错误信息，mode=%v err=%q", app.mode, app.draftError)
	}

	// 3. 若本机 .env 存在真实客户端，进一步端到端验证精简后的主工作台及各视图切换
	envCfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	if _, err := config.ValidateAndResolve(envCfg); err != nil {
		t.Logf("本机未配置真实客户端，跳过主工作台全流程测试: %v", err)
		return
	}

	firstPath := envCfg.CNPath
	secondPath := ""
	if firstPath == "" {
		secondPath = envCfg.GlobalPath
	}
	if !app.SaveAndReloadPaths(firstPath, secondPath) {
		t.Fatalf("保存有效单服路径失败: %s", app.draftError)
	}
	tt.Frame()

	if app.mode != modeReady {
		t.Fatalf("期望加载完成后进入 modeReady，实际: %v (err=%q)", app.mode, app.draftError)
	}

	// 验证左侧菜单精简结构：【数据】成就、物品；【对比】版本 Diff；【设置】
	for _, want := range []string{"数据", "成就", "物品", "对比", "版本 Diff", "设置"} {
		if !tt.HasText(want) {
			t.Fatalf("左侧精简导航缺少预期文案 %q", want)
		}
	}

	// 点击「物品」进入物品工作台，验证「制作物品」等筛选位于物品界面内
	if err := tt.Click("物品"); err != nil {
		t.Fatalf("点击物品导航失败: %v", err)
	}
	if app.tab != tabWorkbench || !tt.HasText("制作物品") {
		t.Fatalf("期望位于物品视图并包含内置「制作物品」筛选，tab=%v", app.tab)
	}

	// 点击内置「制作物品」后，自动展示能工巧匠职业筛选条
	if err := tt.Click("制作物品"); err != nil {
		t.Fatalf("点击物品页内「制作物品」失败: %v", err)
	}
	if app.itemQuery.Filter != "craftable" || !tt.HasText("能工巧匠职业") {
		t.Fatalf("期望进入制作物品过滤并显示职业栏，filter=%q", app.itemQuery.Filter)
	}

	// 切换至「成就」
	if err := tt.Click("成就"); err != nil {
		t.Fatalf("点击成就导航失败: %v", err)
	}
	if app.tab != tabAchievements || !tt.HasText("成就与称号") {
		t.Fatalf("期望切换到成就页面，tab=%v", app.tab)
	}

	// 切换至「版本 Diff」
	if err := tt.Click("版本 Diff"); err != nil {
		t.Fatalf("点击版本 Diff 导航失败: %v", err)
	}
	if app.tab != tabDiff || !tt.HasText("版本变更对比") {
		t.Fatalf("期望切换到版本对比页面，tab=%v", app.tab)
	}

	// 切换至「设置」，测试主题切换与内置 CSV 导出入口
	if err := tt.Click("设置"); err != nil {
		t.Fatalf("点击设置导航失败: %v", err)
	}
	if app.tab != tabSettings || !tt.HasText("界面主题") || !tt.HasText("FF14 游戏客户端") {
		t.Fatalf("期望切换到设置页面，tab=%v", app.tab)
	}

	if err := tt.Click("深色"); err != nil {
		t.Fatalf("切换深色外观失败: %v", err)
	}
	if app.cfg.Theme != "dark" {
		t.Fatalf("期望主题持久化为 dark，实际: %q", app.cfg.Theme)
	}
	if err := tt.Click("浅色"); err != nil {
		t.Fatalf("切换浅色外观失败: %v", err)
	}

	// 从设置页打开 CSV 导出弹窗并关闭
	if err := tt.Click("导出游戏 CSV"); err != nil {
		t.Fatalf("点击导出游戏 CSV 失败: %v", err)
	}
	if !app.exportOpen || !tt.HasText("游戏全量 CSV 解包与本地导出") {
		t.Fatalf("期望打开 CSV 导出弹窗，exportOpen=%v", app.exportOpen)
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatalf("关闭导出弹窗失败: %v", err)
	}
	if app.exportOpen {
		t.Fatalf("期望导出弹窗已关闭")
	}
}

func TestMinimumViewportAndFilterReset(t *testing.T) {
	dataDir := t.TempDir()
	app := NewApp("2.0.0", dataDir, os.DirFS(filepath.Join("..", "..", "schemas")))
	defer app.Close()

	// 验证最小窗口尺寸 1024×640 渲染稳定（含引导页与地图/导出弹窗边界）
	tt := ui.NewTester(app.View, 1024, 640)
	if !tt.HasText("选择本地客户端目录") {
		t.Fatalf("最小视口下引导页主配置区未渲染: %q", tt.Texts())
	}

	// 验证地图弹窗边缘坐标钳制在 1024×640 下正常渲染
	app.openMap(MapMarkerData{
		Title:      "边缘测试标记",
		Subtitle:   "测试区域",
		RegionName: "黑衣森林",
		ZoneName:   "黑衣森林南部林区",
		PlaceName:  "恬静路营地",
		MapPath:    "",
		CoordX:     41.2,
		CoordY:     40.8,
		NormX:      0.98,
		NormY:      0.97,
	})
	tt.Frame()
	if !tt.HasText("41.2, 40.8") {
		t.Fatalf("地图边缘标点坐标未正常渲染")
	}
	app.mapOpen = false
	tt.Frame()

	// 验证筛选状态检测与一键重置逻辑
	if app.hasAnyItemFilter() {
		t.Fatalf("初始状态下不应检测到激活筛选")
	}
	app.itemQuery.Filter = "craftable"
	app.itemQuery.TradableOnly = true
	if !app.hasAnyItemFilter() || app.countActiveAdvFilters() != 1 {
		t.Fatalf("期望检测到筛选已激活")
	}
	app.resetItemFilters()
	if app.hasAnyItemFilter() || app.itemQuery.Filter != "all" {
		t.Fatalf("期望重置后清空全部筛选状态")
	}
}
