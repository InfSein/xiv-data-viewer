package uiapp

import (
	"fmt"
	"math"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// oklchColor 对应 OKLCH 感知均匀色彩空间，用于构建微冷石板中性色与语义色阶。
type oklchColor struct{ l, c, h float32 }

func (o oklchColor) color() ui.Color { return ui.Oklch(o.l, o.c, o.h) }

func midOKLCH(a, b oklchColor) oklchColor {
	h := (a.h + b.h) / 2
	switch {
	case a.c == 0:
		h = b.h
	case b.c == 0:
		h = a.h
	case math.Abs(float64(a.h-b.h)) > 180:
		h = float32(math.Mod(float64(h)+180, 360))
	}
	return oklchColor{(a.l + b.l) / 2, (a.c + b.c) / 2, h}
}

// 沉静冷石板中性色阶（微带 250° 以太石板色韵，避免零饱和死灰）与克制语义色板。
var (
	pWhite = oklchColor{1, 0, 0}
	pBlack = oklchColor{0, 0, 0}

	pSlate50  = oklchColor{0.984, 0.004, 252}
	pSlate100 = oklchColor{0.964, 0.006, 252}
	pSlate200 = oklchColor{0.916, 0.008, 252}
	pSlate300 = oklchColor{0.858, 0.010, 252}
	pSlate400 = oklchColor{0.680, 0.012, 252}
	pSlate500 = oklchColor{0.555, 0.012, 252}
	pSlate600 = oklchColor{0.465, 0.012, 252}
	pSlate700 = oklchColor{0.355, 0.011, 252}
	pSlate800 = oklchColor{0.258, 0.010, 252}
	pSlate900 = oklchColor{0.202, 0.009, 252}
	pSlate950 = oklchColor{0.152, 0.008, 252}

	// 品牌主强调色：克制沉稳的以太靛蓝（仅用于主行动按钮、激活筛选、可点击数据项与焦点环）
	pAether50  = oklchColor{0.962, 0.018, 254}
	pAether100 = oklchColor{0.928, 0.034, 254}
	pAether200 = oklchColor{0.865, 0.060, 254}
	pAether300 = oklchColor{0.775, 0.098, 252}
	pAether400 = oklchColor{0.680, 0.132, 252}
	pAether600 = oklchColor{0.485, 0.145, 256}
	pAether700 = oklchColor{0.440, 0.150, 256}
	pAether800 = oklchColor{0.365, 0.115, 256}
	pAether950 = oklchColor{0.238, 0.045, 256}

	// 语义色：绿（采集/新增/就绪）、琥珀（HQ/限时/变更）、红（移除/限制/错误）
	pGreen50  = oklchColor{0.976, 0.014, 155}
	pGreen100 = oklchColor{0.948, 0.034, 155}
	pGreen200 = oklchColor{0.895, 0.062, 155}
	pGreen300 = oklchColor{0.790, 0.115, 153}
	pGreen500 = oklchColor{0.665, 0.155, 151}
	pGreen600 = oklchColor{0.560, 0.145, 151}
	pGreen700 = oklchColor{0.455, 0.125, 151}
	pGreen800 = oklchColor{0.390, 0.098, 152}
	pGreen950 = oklchColor{0.245, 0.048, 153}

	pAmber50  = oklchColor{0.978, 0.018, 88}
	pAmber200 = oklchColor{0.900, 0.085, 84}
	pAmber300 = oklchColor{0.815, 0.125, 78}
	pAmber500 = oklchColor{0.725, 0.155, 68}
	pAmber800 = oklchColor{0.455, 0.118, 52}
	pAmber950 = oklchColor{0.258, 0.052, 52}

	pRed50  = oklchColor{0.974, 0.012, 20}
	pRed200 = oklchColor{0.890, 0.052, 20}
	pRed300 = oklchColor{0.785, 0.118, 22}
	pRed500 = oklchColor{0.650, 0.185, 25}
	pRed600 = oklchColor{0.555, 0.195, 26}
	pRed700 = oklchColor{0.475, 0.175, 26}
	pRed800 = oklchColor{0.405, 0.135, 26}
	pRed950 = oklchColor{0.248, 0.062, 25}
)

// 排版、尺寸与动效常量。
const (
	fontStack   = defaultEnglishFont + ", Microsoft YaHei UI, Microsoft YaHei, PingFang SC, Segoe UI, system-ui"
	fontStackJA = defaultEnglishFont + ", Yu Gothic UI, Meiryo, Hiragino Sans, Noto Sans JP, Segoe UI, system-ui"

	lineUI    float32 = 1.45
	lineProse float32 = 1.62
	lineTight float32 = 1.0

	strokeWidth  float32 = 1
	hairline     float32 = 0.01 // MyGo 向上取整为 1 物理像素
	materialTint float32 = 0.68 // 浅色 Mica 窗口材质微透明叠层

	titleBarHeight        float32 = 40
	sidebarDefWidth       float32 = 220
	sidebarMinWidth       float32 = 176
	sidebarMaxWidth       float32 = 276
	itemListDefWidth      float32 = 380
	itemListMinWidth      float32 = 316
	itemListMaxWidth      float32 = 500
	iconGroupListDefWidth float32 = 216
	iconGroupListMinWidth float32 = 184
	iconGroupListMaxWidth float32 = 300

	ringWidth  float32 = 2.5
	gripWidth  float32 = 3
	gripHeight float32 = 48

	iconXs float32 = 12
	iconSm float32 = 14
	iconMd float32 = 16
	iconLg float32 = 18

	motionFast = 120 * time.Millisecond
	motionBase = 190 * time.Millisecond
)

type radiusScale struct {
	xs, sm, md, lg, xl, x2l, full float32
}

// textScale 严格收敛的 6 级语义字号阶梯（全部 ≥ 11px）。
type textScale struct {
	micro float32 // 11px：ID 编号、键帽、极次级辅助标注
	xs    float32 // 12px：表头、元数据、徽章、紧凑控件
	sm    float32 // 13px：标准界面正文、列表项、按钮、输入框
	base  float32 // 15px：区块标题、弹窗主标题
	xl    float32 // 20px：详情物品主名、设置分段标题
	x3l   float32 // 24px：页面顶层主标题
}

// uiTokens 保存浅色与深色模式下的全部语义色彩和尺寸令牌。
type uiTokens struct {
	dark bool
	lang i18n.Lang
	font string

	window       ui.Color // 窗口底层/外框色
	sidebar      ui.Color // 侧栏与弹窗底部条底色
	background   ui.Color // 主内容面板画布底色
	card         ui.Color // 面板内柔和次级区块底色
	raised       ui.Color // 弹窗与下拉浮层底色
	foreground   ui.Color // 主文字色
	subdued      ui.Color // 次级正文色
	muted        ui.Color // 辅助信息色（确保 WCAG AA ≥ 4.5:1）
	idle         ui.Color // 禁用文字与弱占位符色
	border       ui.Color // 1px 细分割线与面板边框
	input        ui.Color // 控件边界色
	ring         ui.Color // 焦点环颜色
	primary      ui.Color // 主操作色（以太靛蓝）
	primaryText  ui.Color // 主操作前景色
	accentSoft   ui.Color // 主色极浅底色（用于轻量激活态）
	accentBorder ui.Color // 主色柔和描边
	hover        ui.Color // 实底悬停色
	hoverTint    ui.Color // 半透明叠加悬停基色
	scrim        ui.Color // 模态遮罩底色

	success       ui.Color
	successBg     ui.Color
	successBorder ui.Color
	successText   ui.Color

	infoBg     ui.Color
	infoBorder ui.Color
	infoText   ui.Color

	warningBg     ui.Color
	warningBorder ui.Color
	warningText   ui.Color
	attention     ui.Color

	danger       ui.Color
	dangerBg     ui.Color
	dangerBorder ui.Color
	dangerText   ui.Color

	insBg   ui.Color
	insText ui.Color
	delBg   ui.Color
	delText ui.Color

	radius radiusScale
	text   textScale
}

func (t uiTokens) shadowInk() ui.Color {
	if t.dark {
		return pBlack.color()
	}
	return pSlate950.color()
}

// tint 返回与表面融合的 n 级微冷灰叠加色（1=悬停，1.75=选中，2.25=选中悬停）。
func (t uiTokens) tint(n float32) ui.Color {
	return t.hoverTint.Alpha(n)
}

func defaultRadii() radiusScale {
	return radiusScale{xs: 2, sm: 4, md: 6, lg: 8, xl: 12, x2l: 14, full: 9999}
}

func defaultTextScale() textScale {
	return textScale{micro: 11, xs: 12, sm: 13, base: 15, xl: 20, x3l: 24}
}

func lightTokens() uiTokens {
	return uiTokens{
		dark:         false,
		window:       pSlate100.color(),
		sidebar:      pSlate50.color(),
		background:   pWhite.color(),
		card:         pSlate50.color(),
		raised:       pWhite.color(),
		foreground:   pSlate950.color(),
		subdued:      pSlate700.color(),
		muted:        pSlate600.color(),
		idle:         pSlate500.color(),
		border:       pSlate200.color(),
		input:        pSlate300.color(),
		ring:         pAether600.color(),
		primary:      pAether600.color(),
		primaryText:  pWhite.color(),
		accentSoft:   pAether50.color(),
		accentBorder: pAether200.color(),
		hover:        pSlate100.color(),
		hoverTint:    pSlate950.color().Alpha(0.042),
		scrim:        pSlate950.color().Alpha(0.24),

		success:       pGreen600.color(),
		successBg:     pGreen50.color(),
		successBorder: pGreen200.color(),
		successText:   pGreen700.color(),

		infoBg:     pAether50.color(),
		infoBorder: pAether200.color(),
		infoText:   pAether700.color(),

		warningBg:     pAmber50.color(),
		warningBorder: pAmber200.color(),
		warningText:   pAmber800.color(),
		attention:     pAmber500.color(),

		danger:       pRed600.color(),
		dangerBg:     pRed50.color(),
		dangerBorder: pRed200.color(),
		dangerText:   pRed700.color(),

		insBg:   pGreen100.color(),
		insText: pGreen800.color(),
		delBg:   pRed50.color(),
		delText: pRed700.color(),

		radius: defaultRadii(),
		text:   defaultTextScale(),
	}
}

func darkTokens() uiTokens {
	return uiTokens{
		dark:         true,
		window:       midOKLCH(pSlate900, pSlate800).color(),
		sidebar:      pSlate900.color(),
		background:   pSlate950.color(),
		card:         pSlate900.color(),
		raised:       pSlate900.color(),
		foreground:   pSlate50.color(),
		subdued:      pSlate300.color(),
		muted:        pSlate400.color(),
		idle:         pSlate500.color(),
		border:       pWhite.color().Alpha(0.10),
		input:        pWhite.color().Alpha(0.16),
		ring:         pAether400.color(),
		primary:      pAether400.color(),
		primaryText:  pSlate950.color(),
		accentSoft:   pAether950.color(),
		accentBorder: pAether800.color(),
		hover:        pSlate800.color(),
		hoverTint:    pWhite.color().Alpha(0.075),
		scrim:        pBlack.color().Alpha(0.48),

		success:       pGreen500.color(),
		successBg:     pGreen950.color(),
		successBorder: pGreen800.color(),
		successText:   pGreen300.color(),

		infoBg:     pAether950.color(),
		infoBorder: pAether800.color(),
		infoText:   pAether300.color(),

		warningBg:     pAmber950.color(),
		warningBorder: pAmber800.color(),
		warningText:   pAmber300.color(),
		attention:     pAmber500.color(),

		danger:       pRed500.color(),
		dangerBg:     pRed950.color(),
		dangerBorder: pRed800.color(),
		dangerText:   pRed300.color(),

		insBg:   pGreen950.color(),
		insText: pGreen300.color(),
		delBg:   pRed950.color(),
		delText: pRed300.color(),

		radius: defaultRadii(),
		text:   defaultTextScale(),
	}
}

type themeCacheKey struct {
	dark bool
	font string
}

var (
	themeMu    sync.Mutex
	mygoThemes = map[themeCacheKey]*ui.Theme{}
	tokByTheme = map[*ui.Theme]uiTokens{}
)

func fontStackForLang(lang i18n.Lang) string {
	if i18n.NormalizeLang(string(lang)) == i18n.LangJA {
		return fontStackJA
	}
	return fontStack
}

// applyAppTheme 根据用户配置与系统深浅色状态，为当前帧注入默认中文界面主题令牌。
func applyAppTheme(c *ui.Context, mode string) uiTokens {
	return applyAppThemeWithLang(c, mode, i18n.LangZH)
}

// applyAppThemeWithLang 根据深浅色模式与当前界面语言为当前帧注入主题令牌与字体栈。
func applyAppThemeWithLang(c *ui.Context, mode string, lang i18n.Lang) uiTokens {
	ensureDefaultFonts()
	lang = i18n.NormalizeLang(string(lang))
	dark := c.Theme().Dark
	switch mode {
	case "light":
		dark = false
	case "dark":
		dark = true
	}

	t := lightTokens()
	if dark {
		t = darkTokens()
	}
	t.lang = lang
	t.font = fontStackForLang(lang)

	key := themeCacheKey{dark: dark, font: t.font}
	themeMu.Lock()
	th, ok := mygoThemes[key]
	if !ok {
		base := *ui.LightTheme()
		if dark {
			base = *ui.DarkTheme()
		}
		base.Background = t.background
		base.Surface = t.background
		base.SurfaceHover = t.hover
		base.SurfacePressed = t.border
		base.Border = t.border
		base.Text = t.foreground
		base.TextMuted = t.muted
		base.Accent = t.primary
		base.AccentText = t.primaryText
		base.AccentHover = t.primary.Mix(t.background, 0.12)
		base.AccentPressed = t.primary.Mix(t.background, 0.22)
		base.InactiveAccent = t.tint(1.25)
		base.Danger = t.danger
		base.Warning = t.attention
		base.Success = t.success
		base.Selection = t.primary.Alpha(0.18)
		base.Focus = t.ring.Alpha(0.32)
		base.Scrollbar = t.muted.Alpha(0.38)
		base.Inverse = t.foreground
		base.InverseText = t.background
		base.Radius = t.radius.lg
		base.Spacing = 4
		base.Font = t.font
		base.FontSize = t.text.sm
		th = &base
		mygoThemes[key] = th
		tokByTheme[th] = t
	}
	themeMu.Unlock()

	c.SetTheme(th)
	c.Root().LineHeight(lineUI)
	return t
}

// themeOf 获取当前帧生效的语义色彩令牌。
func themeOf(c *ui.Context) uiTokens {
	th := c.Theme()
	themeMu.Lock()
	t, ok := tokByTheme[th]
	themeMu.Unlock()
	if ok {
		return t
	}
	if th.Dark {
		return darkTokens()
	}
	return lightTokens()
}

// ---------------------------------------------------------------------------
// 浮层海拔（遵循单一海拔声明：仅浮离画布的弹窗/浮层使用阴影，浅色用阴影，深色用 1px 描边）
// ---------------------------------------------------------------------------

type shadowLayer struct{ y, blur, spread, alpha float32 }
type elevation []shadowLayer

var (
	shadowOverlay = elevation{{6, 16, -2, 0.10}, {2, 6, -2, 0.06}}
	shadowDialog  = elevation{{18, 40, -10, 0.20}, {4, 12, -4, 0.08}}
)

func applyOverlaySurface(c *ui.Context, e ui.Element, s elevation) ui.Element {
	t := themeOf(c)
	e.Background(t.raised)
	if t.dark {
		e.Border(hairline, t.border)
		return e
	}
	ink := t.shadowInk()
	for _, l := range s {
		e.Shadow(0, l.y, l.blur, l.spread, ink.Alpha(l.alpha))
	}
	return e
}

func colorTransition(d time.Duration) ui.ElementTransition {
	return ui.ElementTransition{Colors: true, Duration: d}
}

// shortcutLabel 返回匹配当前操作系统的快捷键文案（macOS 显示 ⌘，其他平台显示 Ctrl+）。
func shortcutLabel(key string) string {
	if runtime.GOOS == "darwin" {
		return "⌘" + key
	}
	return "Ctrl+" + key
}

// ---------------------------------------------------------------------------
// 通用文本与数字格式化
// ---------------------------------------------------------------------------

// numText 绘制强制启用等宽数字（tnum）的文本节点。
func numText(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontFeatures("tnum")
}

// numTextf 绘制带格式化的等宽数字文本节点。
func numTextf(c *ui.Context, format string, args ...any) ui.Element {
	return ui.Textf(c, format, args...).FontFeatures("tnum")
}

// formatInt 将整数格式化为带千位分隔符的字符串（如 51,225）。
func formatInt(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var out strings.Builder
	if neg {
		out.WriteByte('-')
	}
	rem := len(s) % 3
	if rem > 0 {
		out.WriteString(s[:rem])
		if len(s) > rem {
			out.WriteByte(',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		out.WriteString(s[i : i+3])
		if i+3 < len(s) {
			out.WriteByte(',')
		}
	}
	return out.String()
}

// formatStars 将配方或采集星级格式化为纯文本。
func formatStars(stars int) string {
	return formatStarsForLang(i18n.LangZH, stars)
}

func formatStarsForLang(lang i18n.Lang, stars int) string {
	if stars <= 0 {
		return ""
	}
	return i18n.Tf(lang, "common.star_suffix", "stars", strconv.Itoa(stars))
}

// ---------------------------------------------------------------------------
// 精炼基础组件库（清晰边界、无幽灵卡、无嵌套套娃）
// ---------------------------------------------------------------------------

// kitPanel 绘制主工作台画布面板（仅以清晰 1px hairline 界定边界，无多余卡片阴影）。
func kitPanel(c *ui.Context, radius float32) ui.Element {
	t := themeOf(c)
	return ui.Column(c).Radius(radius).Clip().Background(t.background).Border(hairline, t.border)
}

type surfaceTone uint8

const (
	toneCard surfaceTone = iota
	toneTint
	toneOutline
	toneRaised
	toneSuccess
	toneInfo
	toneWarning
	toneDanger
)

func applyTone(e ui.Element, t uiTokens, tone surfaceTone, edged bool) ui.Element {
	switch tone {
	case toneCard:
		e.Background(t.card)
		if edged {
			e.Border(hairline, t.border)
		}
	case toneTint:
		e.Background(t.tint(1))
	case toneOutline:
		e.Background(t.background).Border(hairline, t.border)
	case toneRaised:
		e.Background(t.background)
	case toneSuccess:
		e.Background(t.successBg).Border(hairline, t.successBorder).TextColor(t.successText)
	case toneInfo:
		e.Background(t.infoBg).Border(hairline, t.infoBorder).TextColor(t.infoText)
	case toneWarning:
		e.Background(t.warningBg).Border(hairline, t.warningBorder).TextColor(t.warningText)
	case toneDanger:
		e.Background(t.dangerBg).Border(hairline, t.dangerBorder).TextColor(t.dangerText)
	}
	return e
}

func kitCard(c *ui.Context, e ui.Element, tone surfaceTone) ui.Element {
	t := themeOf(c)
	return applyTone(e.Radius(t.radius.xl), t, tone, true)
}

func kitBlock(c *ui.Context, e ui.Element, tone surfaceTone) ui.Element {
	t := themeOf(c)
	return applyTone(e.Radius(t.radius.lg), t, tone, false)
}

// kitSeparator 绘制 1 物理像素水平或垂直细分割线。
func kitSeparator(c *ui.Context, vertical bool) ui.Element {
	t := themeOf(c)
	b := ui.Box(c).BorderColor(t.border)
	if vertical {
		return b.Size(strokeWidth, 16).Shrink(0).BorderWidth(0, 0, 0, hairline)
	}
	return b.Height(strokeWidth).Shrink(0).AlignSelf(ui.Stretch).BorderWidth(hairline, 0, 0, 0)
}

type pillKind uint8

const (
	pillNeutral pillKind = iota
	pillOutline
	pillPositive
	pillAccent
	pillWarning
	pillNegative
)

// drawPill 绘制克制紧凑的语义状态徽章（仅用于需要视觉突出的状态或稀有限制标记）。
func drawPill(c *ui.Context, text string, kind pillKind) {
	t := themeOf(c)
	row := ui.Row(c).Height(19).Padding(0, 6).Radius(t.radius.sm).AlignItems(ui.Center)
	fg := t.subdued
	switch kind {
	case pillOutline:
		row.Border(hairline, t.border)
		fg = t.muted
	case pillPositive:
		row.Background(t.successBg)
		fg = t.successText
	case pillAccent:
		row.Background(t.infoBg)
		fg = t.infoText
	case pillWarning:
		row.Background(t.warningBg)
		fg = t.warningText
	case pillNegative:
		row.Background(t.dangerBg)
		fg = t.dangerText
	default:
		row.Background(t.tint(1))
		fg = t.subdued
	}
	row.Children(func() {
		ui.Text(c, text).FontSize(t.text.micro).FontWeight(500).LineHeight(lineTight).TextColor(fg).FontFeatures("tnum").SingleLine()
	})
}

// drawStatusDot 绘制 6×6 状态圆点。
func drawStatusDot(c *ui.Context, col ui.Color) {
	t := themeOf(c)
	ui.Box(c).Size(6, 6).Shrink(0).Radius(t.radius.full).Background(col)
}

// drawDetailSection 绘制详情页无套娃扁平分节（顶部分割线 + 清晰节标题 + 直接铺展的内容区）。
func drawDetailSection(c *ui.Context, title string, meta string, trailing func(), body func()) {
	t := themeOf(c)
	ui.Column(c).FillWidth().Padding(18, 0, 0, 0).Gap(10).
		BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Baseline).Gap(10).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Baseline).Grow(1).Shrink(1).Children(func() {
				ui.Text(c, title).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				if meta != "" {
					numText(c, meta).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				}
			})
			if trailing != nil {
				trailing()
			}
		})
		body()
	})
}

// drawCollapsibleDetailSection 绘制带点击展开/收起标题栏的详情页分节。
func drawCollapsibleDetailSection(
	c *ui.Context,
	key any,
	title string,
	meta string,
	open *bool,
	collapsedHint string,
	expandedHint string,
	body func(),
) {
	t := themeOf(c)
	ui.Column(c).FillWidth().Padding(14, 0, 0, 0).Gap(10).
		BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
		trig := ui.ButtonBase(c.Key(key)).Label(title).
			FillWidth().Padding(6, 10).Radius(t.radius.lg).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
		if *open {
			trig.Background(t.card)
		} else if trig.Hovered() {
			trig.Background(t.tint(1.25))
		} else {
			trig.Background(t.tint(0.6))
		}
		trig.Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).Children(func() {
				ico := icoChevronRight
				if *open {
					ico = icoChevronDown
				}
				ui.Icon(c, ico).Size(iconSm, iconSm).Shrink(0).TextColor(t.muted)
				ui.Text(c, title).FontSize(t.text.base).FontWeight(600).TextColor(t.foreground).SingleLine()
				if meta != "" {
					numText(c, meta).FontSize(t.text.xs).TextColor(t.muted).SingleLine()
				}
			})
			hint := collapsedHint
			if *open {
				hint = expandedHint
			}
			if hint != "" {
				ui.Text(c, hint).FontSize(t.text.micro).TextColor(t.muted).SingleLine().Shrink(0)
			}
		})
		if trig.Clicked() {
			*open = !*open
		}

		if *open && body != nil {
			body()
		}
	})
}

type btnVariant uint8

const (
	btnDefault btnVariant = iota
	btnSecondary
	btnOutline
	btnGhost
)

type btnSize uint8

const (
	btnSizeMd btnSize = iota // 32px
	btnSizeSm                // 28px
	btnSizeXs                // 24px
)

type btnOpts struct {
	variant  btnVariant
	size     btnSize
	disabled bool
	flat     bool
	icon     *ui.SVG
}

func kitButton(c *ui.Context, key any, label string, o btnOpts) bool {
	t := themeOf(c)
	var h, padX, fs, icoSize float32
	switch o.size {
	case btnSizeXs:
		h, padX, fs, icoSize = 24, 8, t.text.xs, iconXs
	case btnSizeSm:
		h, padX, fs, icoSize = 28, 10, t.text.xs, iconSm
	default:
		h, padX, fs, icoSize = 32, 12, t.text.sm, iconMd
	}
	rad := t.radius.lg
	if h < 32 {
		rad = t.radius.md
	}

	b := ui.ButtonBase(c.Key(key)).Label(label).Height(h).PaddingX(padX).Radius(rad).
		Gap(6).Justify(ui.Center).AlignItems(ui.Center).
		FontSize(fs).FontWeight(500).
		Disabled(o.disabled).DimDisabled(false).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))

	hover := b.Hovered() && !o.disabled
	pressed := hover && b.Pressed()

	fg := t.foreground
	switch o.variant {
	case btnDefault:
		bg := t.primary
		fg = t.primaryText
		if pressed {
			bg = t.primary.Mix(t.foreground, 0.16)
		} else if hover {
			bg = t.primary.Mix(t.foreground, 0.08)
		}
		if o.disabled {
			bg = t.primary.Alpha(0.40)
			fg = t.primaryText.Alpha(0.75)
		}
		b.Background(bg).TextColor(fg)
	case btnSecondary:
		bg := t.accentSoft
		fg = t.infoText
		if pressed {
			bg = t.accentSoft.Mix(t.primary, 0.14)
		} else if hover {
			bg = t.accentSoft.Mix(t.primary, 0.08)
		}
		if o.disabled {
			bg = t.tint(1)
			fg = t.idle
		}
		b.Background(bg).Border(hairline, t.accentBorder).TextColor(fg)
	case btnOutline:
		bg := t.background
		if pressed {
			bg = t.hover.Mix(t.foreground, 0.05)
		} else if hover {
			bg = t.hover
		}
		if o.disabled {
			fg = t.idle
		}
		b.Background(bg).Border(strokeWidth, t.input).TextColor(fg)
	case btnGhost:
		fg = t.subdued
		if pressed {
			b.Background(t.tint(2))
			fg = t.foreground
		} else if hover {
			b.Background(t.tint(1))
			fg = t.foreground
		}
		if o.disabled {
			fg = t.idle
		}
		b.TextColor(fg)
	}

	b.Children(func() {
		if o.icon != nil {
			ui.Icon(c, o.icon).Size(icoSize, icoSize).TextColor(fg)
		}
		ui.Text(c, label).FontSize(fs).FontWeight(500).TextColor(fg).SingleLine()
	})
	return b.Clicked()
}

// kitIconButton 绘制带 Tooltip 的正方形图标按钮。
func kitIconButton(c *ui.Context, key any, ico *ui.SVG, tip string, o btnOpts) bool {
	t := themeOf(c)
	var h, icoSize float32
	switch o.size {
	case btnSizeXs:
		h, icoSize = 24, iconXs
	case btnSizeSm:
		h, icoSize = 28, iconMd
	default:
		h, icoSize = 32, iconMd
	}
	rad := t.radius.lg
	if h < 32 {
		rad = t.radius.md
	}

	b := ui.ButtonBase(c.Key(key)).Label(tip).Size(h, h).Radius(rad).
		Center().Disabled(o.disabled).DimDisabled(false).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))

	hover := b.Hovered() && !o.disabled
	pressed := hover && b.Pressed()
	fg := t.muted

	switch o.variant {
	case btnOutline:
		fg = t.foreground
		b.Background(t.background).Border(strokeWidth, t.input)
		if hover {
			b.Background(t.hover)
		}
	default:
		if pressed {
			b.Background(t.tint(2))
			fg = t.foreground
		} else if hover {
			b.Background(t.tint(1))
			fg = t.foreground
		}
	}
	if o.disabled {
		fg = t.idle
	}

	if tip != "" {
		kitTooltip(c, b, tip)
	}
	b.Children(func() {
		ui.Icon(c, ico).Size(icoSize, icoSize).TextColor(fg)
	})
	return b.Clicked()
}

// kitFavoriteToggleButton 绘制详情顶栏的「加入收藏 / 已收藏」切换按钮。
func kitFavoriteToggleButton(c *ui.Context, key any, addLabel, addedLabel string, favorited bool) bool {
	t := themeOf(c)
	lbl := addLabel
	ico := icoStar
	if favorited {
		lbl = addedLabel
		ico = icoStarFill
	}

	b := ui.ButtonBase(c.Key(key)).Label(lbl).Height(28).PaddingX(10).Radius(t.radius.md).
		Gap(5).Justify(ui.Center).AlignItems(ui.Center).
		FontSize(t.text.xs).FontWeight(500).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))

	hover := b.Hovered()
	pressed := hover && b.Pressed()
	fg := t.foreground
	icoCol := t.muted

	if favorited {
		bg := t.warningBg
		fg = t.warningText
		icoCol = t.attention
		if pressed {
			bg = bg.Mix(t.attention, 0.18)
		} else if hover {
			bg = bg.Mix(t.attention, 0.10)
		}
		b.Background(bg).Border(hairline, t.warningBorder).TextColor(fg)
	} else {
		bg := t.background
		if pressed {
			bg = t.hover.Mix(t.foreground, 0.05)
			icoCol = t.attention
		} else if hover {
			bg = t.hover
			icoCol = t.attention
		}
		b.Background(bg).Border(strokeWidth, t.input).TextColor(fg)
	}

	b.Children(func() {
		ui.Icon(c, ico).Size(iconSm, iconSm).Shrink(0).TextColor(icoCol)
		ui.Text(c, lbl).FontSize(t.text.xs).FontWeight(500).TextColor(fg).SingleLine()
	})
	return b.Clicked()
}

// kitFavoriteStarIconButton 绘制表格或列表行内的紧凑星标收藏切换按钮。
func kitFavoriteStarIconButton(c *ui.Context, key any, tip string, favorited bool) bool {
	t := themeOf(c)
	b := ui.ButtonBase(c.Key(key)).Label(tip).Size(28, 28).Radius(t.radius.md).
		Center().Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))

	hover := b.Hovered()
	pressed := hover && b.Pressed()
	ico := icoStar
	fg := t.muted
	if favorited {
		ico = icoStarFill
		fg = t.attention
		if pressed {
			b.Background(t.warningBg.Mix(t.attention, 0.18))
		} else if hover {
			b.Background(t.warningBg)
		}
	} else {
		if pressed {
			b.Background(t.tint(2))
			fg = t.attention
		} else if hover {
			b.Background(t.tint(1))
			fg = t.foreground
		}
	}

	if tip != "" {
		kitTooltip(c, b, tip)
	}
	b.Children(func() {
		ui.Icon(c, ico).Size(iconMd, iconMd).TextColor(fg)
	})
	return b.Clicked()
}

func kitTooltip(c *ui.Context, anchor ui.Element, text string) {
	t := themeOf(c)
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		tip.Padding(5, 9).Radius(t.radius.md).Background(t.foreground).TextColor(t.background).FontSize(t.text.xs)
		tip.Children(func() {
			ui.Text(c, text).SingleLine()
		})
	})
}

// kitSidebarItem 绘制侧边栏导航行（32px 高、圆角 8px、半透明叠加选中态、线面图标切换）。
func kitSidebarItem(c *ui.Context, key any, icoLine, icoFill *ui.SVG, label, rightMeta string, selected bool, accentDot bool) bool {
	t := themeOf(c)
	b := ui.ButtonBase(c.Key(key)).Label(label).FillWidth().Height(32).Padding(0, 10).Radius(t.radius.lg).
		Justify(ui.SpaceBetween).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))

	switch {
	case selected && b.Hovered():
		b.Background(t.tint(2.25))
	case selected:
		b.Background(t.tint(1.75))
	case b.Hovered():
		b.Background(t.tint(1))
	}

	b.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Shrink(1).Children(func() {
			ico := icoLine
			icoCol := t.muted
			if selected {
				if icoFill != nil {
					ico = icoFill
				}
				icoCol = t.primary
			} else if b.Hovered() {
				icoCol = t.foreground
			}
			if ico != nil {
				ui.Icon(c, ico).Size(iconMd, iconMd).Shrink(0).TextColor(icoCol)
			}
			fg := t.subdued
			fw := 400
			if selected {
				fg = t.foreground
				fw = 500
			} else if b.Hovered() {
				fg = t.foreground
			}
			ui.Text(c, label).FontSize(t.text.sm).FontWeight(fw).TextColor(fg).SingleLine().Grow(1).Shrink(1)
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
			if accentDot {
				drawStatusDot(c, t.success)
			}
			if rightMeta != "" {
				numText(c, rightMeta).FontSize(t.text.micro).TextColor(t.muted).SingleLine()
			}
		})
	})
	return b.Clicked()
}

// kitSegmentedTabs 绘制分段切换器（柔和轨道 + 干净实底激活滑块）。
func kitSegmentedTabs(c *ui.Context, scope string, selected int, labels []string) (int, bool) {
	t := themeOf(c)
	next := selected
	changed := false

	ui.Row(c.Key("seg-" + scope)).Padding(2).Gap(2).Radius(t.radius.lg).Background(t.tint(1.1)).AlignItems(ui.Center).Children(func() {
		for i, lbl := range labels {
			active := i == selected
			tab := ui.ButtonBase(c.Key(fmt.Sprintf("seg-%s-%d", scope, i))).Label(lbl).
				Height(24).PaddingX(10).Radius(t.radius.md).
				Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			if active {
				tab.Background(t.background).Border(hairline, t.border)
			} else if tab.Hovered() {
				tab.Background(t.tint(0.75))
			}
			tab.Transition(colorTransition(motionFast))
			tab.Children(func() {
				fg := t.muted
				if active {
					fg = t.foreground
				} else if tab.Hovered() {
					fg = t.subdued
				}
				ui.Text(c, lbl).FontSize(t.text.xs).FontWeight(500).TextColor(fg).SingleLine()
			})
			if tab.Clicked() && !active {
				next = i
				changed = true
			}
		}
	})
	return next, changed
}

// kitToggleChip 绘制紧凑筛选切换标签（激活时使用克制以太靛蓝，非激活时极轻底色）。
func kitToggleChip(c *ui.Context, key any, label, count string, active bool, iconBmp *ui.Bitmap) bool {
	t := themeOf(c)
	b := ui.ButtonBase(c.Key(key)).Label(label).Height(25).Padding(0, 8).Gap(5).Radius(t.radius.md).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))

	if active {
		b.Background(t.accentSoft).Border(hairline, t.accentBorder)
	} else if b.Hovered() {
		b.Background(t.tint(1.5))
	} else {
		b.Background(t.tint(0.75))
	}

	b.Children(func() {
		if iconBmp != nil {
			ui.Image(c, iconBmp).Size(14, 14).Fit(ui.Contain)
		}
		fg := t.subdued
		fw := 400
		if active {
			fg = t.infoText
			fw = 500
		} else if b.Hovered() {
			fg = t.foreground
		}
		ui.Text(c, label).FontSize(t.text.xs).FontWeight(fw).TextColor(fg).SingleLine()
		if count != "" {
			cntCol := t.muted
			if active {
				cntCol = t.infoText.Alpha(0.80)
			}
			numText(c, count).FontSize(t.text.micro).TextColor(cntCol).SingleLine()
		}
	})
	return b.Clicked()
}

// kitSwitch 绘制 32×18 开关滑块。
func kitSwitch(c *ui.Context, key any, on *bool, label string) bool {
	t := themeOf(c)
	el := ui.SwitchBase(c.Key(key), on).Label(label).Radius(t.radius.full).Cursor(ui.CursorPointer)
	el.Children(func() {
		tr := ui.Row(c).Size(32, 18).Shrink(0).Padding(2).Radius(t.radius.full).
			Background(t.input).Justify(ui.Start).Transition(colorTransition(motionFast))
		thumb := t.background
		switch {
		case *on:
			tr.Background(t.primary).Justify(ui.End)
			thumb = t.primaryText
		case t.dark:
			thumb = t.foreground
		}
		tr.Children(func() {
			ui.Box(c).Size(14, 14).Radius(t.radius.full).Background(thumb).
				Transition(ui.ElementTransition{Position: true, Duration: motionFast})
		})
	})
	return el.Changed()
}

// kitInputOpts 控制单行输入框样式。
type kitInputOpts struct {
	placeholder string
	label       string
	size        btnSize
	tintBg      bool
	flat        bool
	disabled    bool
	leading     *ui.SVG
	shortcut    string
	clearable   bool
}

// kitInput 绘制带平滑焦点环、前导图标与清空按钮的单行输入框。
func kitInput(c *ui.Context, key any, val *string, o kitInputOpts) ui.Element {
	field, _ := kitInputWithContainer(c, key, val, o)
	return field
}

// kitInputWithContainer 绘制单行输入框并返回输入元素与外层容器（供弹出层锚点绑定）。
func kitInputWithContainer(c *ui.Context, key any, val *string, o kitInputOpts) (ui.Element, ui.Element) {
	t := themeOf(c)
	var h, padX, fs float32 = 32, 10, t.text.sm
	switch o.size {
	case btnSizeXs:
		h, padX, fs = 24, 7, t.text.xs
	case btnSizeSm:
		h, padX, fs = 28, 9, t.text.sm
	}
	rad := t.radius.lg
	if h < 32 {
		rad = t.radius.md
	}

	box := ui.Row(c.Key(fmt.Sprintf("inp-box-%v", key))).Height(h).Radius(rad).
		AlignItems(ui.Center).Gap(7).Padding(0, padX).FocusRing(false)
	if o.tintBg {
		box.Background(t.hoverTint)
	} else {
		box.Background(t.background)
	}

	borderCol := t.input
	if o.tintBg {
		borderCol = t.border
	}
	ringCol := c.Theme().Focus.Alpha(0)
	if box.FocusWithin() && !o.disabled {
		borderCol = t.ring
		ringCol = c.Theme().Focus
	}
	anim := ui.Animation{Duration: motionFast, Ease: ui.EaseOut}
	box.Border(strokeWidth, box.AnimateColor("b", borderCol, anim)).
		Shadow(0, 0, 0, ringWidth, box.AnimateColor("r", ringCol, anim))

	var field ui.Element
	box.Children(func() {
		if o.leading != nil {
			ui.Icon(c, o.leading).Size(iconSm, iconSm).Shrink(0).TextColor(t.muted)
		}
		field = ui.TextInputBase(c.Key(key), val).Placeholder(o.placeholder).Label(o.label).
			Disabled(o.disabled).FocusRing(false).FontSize(fs).TextColor(t.foreground).
			Grow(1).Shrink(1).MinWidth(0).AlignSelf(ui.Stretch).Padding(0)
		if o.clearable && *val != "" && !o.disabled {
			clr := ui.ButtonBase(c.Key(fmt.Sprintf("inp-clr-%v", key))).Label(i18n.T(t.lang, "common.clear")).
				Size(18, 18).Radius(t.radius.full).Center().Cursor(ui.CursorPointer)
			if clr.Hovered() {
				clr.Background(t.tint(1.5))
			}
			clr.Children(func() {
				ui.Icon(c, icoClose).Size(11, 11).TextColor(t.muted)
			})
			if clr.Clicked() {
				*val = ""
				field.SetChanged()
			}
		} else if o.shortcut != "" && *val == "" {
			kitKbd(c, o.shortcut)
		}
	})
	return field, box
}

// kitKbd 绘制紧凑快捷键键帽（字号严格不低于 11px）。
func kitKbd(c *ui.Context, text string) {
	t := themeOf(c)
	ui.Box(c).Height(19).PaddingX(5).Radius(t.radius.sm).Border(hairline, t.border).Background(t.background).Center().Shrink(0).Children(func() {
		ui.Text(c, text).FontSize(t.text.micro).LineHeight(lineTight).TextColor(t.muted).FontFeatures("tnum").SingleLine()
	})
}

type selectOption[T comparable] struct {
	value T
	label string
}

// kitSelect 绘制下拉选择框（触发器使用清晰 1px 描边，弹出面板使用浮层海拔）。
func kitSelect[T comparable](c *ui.Context, key any, val *T, opts []selectOption[T], width, popupWidth float32, flat bool) bool {
	t := themeOf(c)
	sel := ui.SelectBase(c.Key(key), val)
	curLabel := ""
	for _, opt := range opts {
		if opt.value == *val {
			curLabel = opt.label
			break
		}
	}
	if curLabel == "" && len(opts) > 0 {
		curLabel = opts[0].label
	}

	trig := sel.Trigger.Height(28).Width(width).Padding(0, 8, 0, 10).Radius(t.radius.md).
		Background(t.background).Border(strokeWidth, t.input).
		Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6).
		Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
	if trig.Hovered() {
		trig.Background(t.hover)
	}
	trig.Children(func() {
		ui.Text(c, curLabel).FontSize(t.text.xs).TextColor(t.foreground).FontFeatures("tnum").SingleLine().Grow(1).Shrink(1)
		ui.Icon(c, icoChevronDown).Size(iconSm, iconSm).Shrink(0).TextColor(t.muted)
	})

	sel.Popup(func(panel ui.Element) {
		pw := popupWidth
		if pw <= 0 {
			pw = max(width, 160)
		}
		applyOverlaySurface(c, panel.Margin(4, 0, 0, 0).Padding(4).Radius(t.radius.lg).MaxHeight(300).Width(pw), shadowOverlay)
		ui.Scroll(c.Key(fmt.Sprintf("sel-scr-%v", key))).Shrink(1).Children(func() {
			for _, opt := range opts {
				isCur := opt.value == *val
				row := sel.Item(opt.value).Height(28).Padding(0, 8).Radius(t.radius.md).
					Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(6)
				if row.Highlighted() {
					row.Background(t.tint(1.25))
				}
				row.Children(func() {
					fw := 400
					if isCur {
						fw = 500
					}
					ui.Text(c, opt.label).FontSize(t.text.xs).FontWeight(fw).TextColor(t.foreground).FontFeatures("tnum").SingleLine().Grow(1).Shrink(1)
					if isCur {
						ui.Icon(c, icoCheck).Size(iconXs, iconXs).Shrink(0).TextColor(t.primary)
					}
				})
			}
		})
	})

	return sel.Trigger.Changed()
}

// renderSplitterGrip 绘制可拖拽竖向分隔握柄（供侧栏和列表双栏复用）。
func renderSplitterGrip(c *ui.Context, key any, size *float32, minW, maxW, width float32, label string) {
	t := themeOf(c)
	grip := ui.Box(c.Key(key)).Width(width).Shrink(0).Center().
		Role(ui.RoleSplitter).Label(label).Cursor(ui.CursorResizeEW)
	dx, _, held := grip.Dragged()
	if dx != 0 {
		*size = min(max(*size+dx, minW), maxW)
	}
	if held || grip.Hovered() {
		grip.Children(func() {
			ui.Box(c).Width(gripWidth).Height(gripHeight).Radius(t.radius.full).Background(t.foreground.Alpha(0.24))
		})
	}
}

// kitResizer 实现双栏拖拽分隔容器。
func kitResizer(c *ui.Context, key any, size *float32, minW, maxW, gap float32, label string, first, second func()) {
	ui.Row(c.Key(key)).Grow(1).Shrink(1).FillHeight().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(*size).Shrink(0).FillHeight().ClipX().Children(first)
		renderSplitterGrip(c, fmt.Sprintf("%v-grip", key), size, minW, maxW, gap, label)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).FillHeight().ClipX().Children(second)
	})
}

// kitAlert 绘制语义提示条（错误/成功/警告/提示）。
func kitAlert(c *ui.Context, tone surfaceTone, text string) {
	t := themeOf(c)
	ico := icoInfo
	if tone == toneDanger || tone == toneWarning {
		ico = icoAlertCircle
	} else if tone == toneSuccess {
		ico = icoCheck
	}
	kitBlock(c, ui.Row(c).FillWidth().Padding(10, 12).Gap(8).AlignItems(ui.Center), tone).Children(func() {
		ui.Icon(c, ico).Size(iconSm, iconSm).Shrink(0)
		ui.Text(c, text).FontSize(t.text.xs).Grow(1).Shrink(1).Selectable()
	})
}

// kitEmpty 绘制居中空状态占位块。
func kitEmpty(c *ui.Context, ico *ui.SVG, title, desc string, action func()) {
	t := themeOf(c)
	ui.Column(c).Grow(1).FillWidth().Center().Padding(32, 24).Gap(10).Children(func() {
		if ico != nil {
			ui.Icon(c, ico).Size(24, 24).TextColor(t.idle)
		}
		ui.Column(c).AlignItems(ui.Center).Gap(4).MaxWidth(340).Children(func() {
			ui.Text(c, title).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground).TextAlign(ui.Center)
			if desc != "" {
				ui.Text(c, desc).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.muted).TextAlign(ui.Center)
			}
		})
		if action != nil {
			ui.Box(c).Padding(4, 0, 0, 0).Children(action)
		}
	})
}

// drawSettingsSection 绘制设置页标准分组区块（单一层次卡片 + 内缩 1px 行间分隔）。
func drawSettingsSection(c *ui.Context, title, footnote string, rows ...func()) {
	t := themeOf(c)
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground).PaddingX(2)
		}
		kitCard(c, ui.Column(c).FillWidth().Clip(), toneCard).Children(func() {
			for i, row := range rows {
				if i > 0 {
					kitSeparator(c, false).Margin(0, 16)
				}
				row()
			}
		})
		if footnote != "" {
			ui.Text(c, footnote).FontSize(t.text.xs).TextColor(t.muted).PaddingX(2)
		}
	})
}

// ---------------------------------------------------------------------------
// 统一风格矢量图标库（24×24 网格，1.75 描边，涵盖线框态与选中填充态）
// ---------------------------------------------------------------------------

var (
	icoAppLogo = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2.5 20.5 7.2v9.6L12 21.5 3.5 16.8V7.2L12 2.5Z"/><path d="M12 21.5v-9.5"/><path d="M20.5 7.2 12 12 3.5 7.2"/></svg>`))

	icoPanelLeft = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2.5"/><line x1="9" y1="4" x2="9" y2="20"/></svg>`))

	icoSearch = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><line x1="20" y1="20" x2="16.2" y2="16.2"/></svg>`))

	icoClose = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="6" y1="6" x2="18" y2="18"/><line x1="18" y1="6" x2="6" y2="18"/></svg>`))

	icoCheck = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="5 12.5 9.8 17.3 19 8"/></svg>`))

	icoChevronDown = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>`))

	icoChevronRight = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 6 15 12 9 18"/></svg>`))

	icoArrowLeft = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><line x1="19" y1="12" x2="5" y2="12"/><polyline points="11 6 5 12 11 18"/></svg>`))

	icoArrowRight = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><line x1="5" y1="12" x2="19" y2="12"/><polyline points="13 6 19 12 13 18"/></svg>`))

	icoTrophy = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M7 4h10v6a5 5 0 0 1-10 0V4Z"/><path d="M7 6H4v2a3 3 0 0 0 3 3"/><path d="M17 6h3v2a3 3 0 0 1-3 3"/><line x1="12" y1="15" x2="12" y2="19"/><line x1="8" y1="20" x2="16" y2="20"/></svg>`))

	icoTrophyFill = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M6.2 3.2A1 1 0 0 1 7 2.8h10a1 1 0 0 1 .8.4c.13.17.2.38.2.6v1.4h2a1 1 0 0 1 1 1v2a4 4 0 0 1-3.32 3.94A6 6 0 0 1 13 15.92V19h3a1 1 0 1 1 0 2H8a1 1 0 1 1 0-2h3v-3.08a6 6 0 0 1-4.68-3.78A4 4 0 0 1 3 8.2v-2a1 1 0 0 1 1-1h2V3.8c0-.22.07-.43.2-.6ZM5 7.2v1a2 2 0 0 0 1 1.73V7.2H5Zm14 0h-1v2.73a2 2 0 0 0 1-1.73v-1Z"/></svg>`))

	icoPackage = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 20 7.5v9L12 21l-8-4.5v-9L12 3Z"/><path d="M12 12 20 7.5"/><path d="M12 12v9"/><path d="M12 12 4 7.5"/></svg>`))

	icoPackageFill = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M11.5 2.28a1 1 0 0 1 1 0l7.5 4.22a1 1 0 0 1 .5.87v9.26a1 1 0 0 1-.5.87l-7.5 4.22a1 1 0 0 1-1 0L4 17.5a1 1 0 0 1-.5-.87V7.37a1 1 0 0 1 .5-.87l7.5-4.22ZM12 10.85l5.95-3.35L12 4.15 6.05 7.5 12 10.85Z"/></svg>`))

	icoCompare = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="6" r="2.5"/><circle cx="18" cy="18" r="2.5"/><path d="M13 6h3a2 2 0 0 1 2 2v7.5"/><polyline points="15 4 13 6 15 8"/><path d="M11 18H8a2 2 0 0 1-2-2V8.5"/><polyline points="9 20 11 18 9 16"/></svg>`))

	icoSettings = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><line x1="4" y1="7" x2="20" y2="7"/><line x1="4" y1="17" x2="20" y2="17"/><circle cx="9" cy="7" r="2.2" fill="currentColor"/><circle cx="15" cy="17" r="2.2" fill="currentColor"/></svg>`))

	icoFilter = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M4 6h16l-6 7.2V19l-4-1.8v-4L4 6Z"/></svg>`))

	icoFolder = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6.5A2 2 0 0 1 5 4.5h4.2l2 2H19a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-11Z"/></svg>`))

	icoDownload = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4v11"/><polyline points="7.5 10.5 12 15 16.5 10.5"/><path d="M4 19h16"/></svg>`))

	icoCopy = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/></svg>`))

	icoRefresh = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M20 11a8 8 0 1 0 2 5.3"/><polyline points="20 5 20 11 14 11"/></svg>`))

	icoMapPin = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 21s-6.5-5.2-6.5-10.5a6.5 6.5 0 1 1 13 0C18.5 15.8 12 21 12 21Z"/><circle cx="12" cy="10.5" r="2.2"/></svg>`))

	icoShield = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 5 6v6c0 4.5 3 7.8 7 9 4-1.2 7-4.5 7-9V6l-7-3Z"/><polyline points="9 12 11.2 14.2 15.5 9.8"/></svg>`))

	icoLayers = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 3 3 8 12 13 21 8 12 3"/><polyline points="3 13 12 18 21 13"/><polyline points="3 17.5 12 22.5 21 17.5"/></svg>`))

	icoLock = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 1 1 8 0v3"/></svg>`))

	icoAlertCircle = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><line x1="12" y1="8" x2="12" y2="12.5"/><circle cx="12" cy="16" r="0.8" fill="currentColor"/></svg>`))

	icoInfo = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><line x1="12" y1="11" x2="12" y2="16"/><circle cx="12" cy="8" r="0.8" fill="currentColor"/></svg>`))

	icoScroll = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M19 4H8a2 2 0 0 0-2 2v12a2 2 0 0 1-2 2h12a2 2 0 0 0 2-2V6a2 2 0 0 1 2-2Z"/><line x1="10" y1="9" x2="15" y2="9"/><line x1="10" y1="13" x2="15" y2="13"/></svg>`))

	icoScrollFill = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M8 3a3 3 0 0 0-3 3v11.17A3 3 0 0 0 3 20a1 1 0 0 0 1 1h12a3 3 0 0 0 3-3V6a3 3 0 0 0 3-3 1 1 0 0 0-1-1H8Zm2 5.25a.75.75 0 0 1 .75-.75h4.5a.75.75 0 0 1 0 1.5h-4.5A.75.75 0 0 1 10 8.25Zm0 4a.75.75 0 0 1 .75-.75h4.5a.75.75 0 0 1 0 1.5h-4.5a.75.75 0 0 1-.75-.75Z"/></svg>`))

	icoClipboard = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="8" y="2.5" width="8" height="3.5" rx="1"/><path d="M16 4h2a2 2 0 0 1 2 2v13a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><line x1="9" y1="11" x2="15" y2="11"/><line x1="9" y1="15" x2="13" y2="15"/></svg>`))

	icoClipboardFill = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M9.5 2A1.5 1.5 0 0 0 8 3.5v.5H6a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2h-2v-.5A1.5 1.5 0 0 0 14.5 2h-5ZM9 10.25a.75.75 0 0 1 .75-.75h4.5a.75.75 0 0 1 0 1.5h-4.5a.75.75 0 0 1-.75-.75Zm0 4a.75.75 0 0 1 .75-.75h2.5a.75.75 0 0 1 0 1.5h-2.5a.75.75 0 0 1-.75-.75Z"/></svg>`))

	icoChangelog = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8l-5-5Z"/><polyline points="14 3 14 8 19 8"/><circle cx="9.2" cy="12.5" r="0.8" fill="currentColor"/><line x1="12" y1="12.5" x2="15.5" y2="12.5"/><circle cx="9.2" cy="16.5" r="0.8" fill="currentColor"/><line x1="12" y1="16.5" x2="15.5" y2="16.5"/></svg>`))

	icoChangelogFill = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" fill-rule="evenodd"><path d="M14.2 2.5H7A2.5 2.5 0 0 0 4.5 5v14A2.5 2.5 0 0 0 7 21.5h10a2.5 2.5 0 0 0 2.5-2.5V7.8a1 1 0 0 0-.29-.7l-4.3-4.3a1 1 0 0 0-.71-.3Zm-.45 2.06L17.44 8.25H14.5a.75.75 0 0 1-.75-.75V4.56ZM9.2 11.65a.85.85 0 1 0 0 1.7.85.85 0 0 0 0-1.7Zm2.8.1a.75.75 0 0 0 0 1.5h3.5a.75.75 0 0 0 0-1.5H12Zm-2.8 3.9a.85.85 0 1 0 0 1.7.85.85 0 0 0 0-1.7Zm2.8.1a.75.75 0 0 0 0 1.5h3.5a.75.75 0 0 0 0-1.5H12Z"/></svg>`))

	icoClock = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><polyline points="12 7.5 12 12 15.2 14"/></svg>`))

	icoClockFill = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" fill-rule="evenodd"><path d="M12 2.25a9.75 9.75 0 1 0 0 19.5 9.75 9.75 0 0 0 0-19.5ZM12.75 7.5a.75.75 0 0 0-1.5 0V12c0 .25.13.48.34.62l3.2 2a.75.75 0 1 0 .8-1.27L12.75 11.58V7.5Z"/></svg>`))

	icoStar = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 3.2 14.68 8.63 20.68 9.5 16.34 13.73 17.36 19.71 12 16.9 6.64 19.71 7.66 13.73 3.32 9.5 9.32 8.63 12 3.2"/></svg>`))

	icoStarFill = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M11.1 2.74a1 1 0 0 1 1.8 0l2.38 4.82a1 1 0 0 0 .75.55l5.32.77a1 1 0 0 1 .55 1.7l-3.85 3.76a1 1 0 0 0-.29.88l.91 5.3a1 1 0 0 1-1.45 1.05l-4.76-2.5a1 1 0 0 0-.92 0l-4.76 2.5a1 1 0 0 1-1.45-1.05l.91-5.3a1 1 0 0 0-.29-.88L2.1 10.58a1 1 0 0 1 .55-1.7l5.32-.77a1 1 0 0 0 .75-.55l2.38-4.82Z"/></svg>`))

	icoExternalLink = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M14 4h6v6"/><path d="M10 14 20 4"/><path d="M19 13v5a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2h5"/></svg>`))

	icoGlobe = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 3c2.5 2.6 4 5.7 4 9s-1.5 6.4-4 9c-2.5-2.6-4-5.7-4-9s1.5-6.4 4-9Z"/><line x1="3.6" y1="9" x2="20.4" y2="9"/><line x1="3.6" y1="15" x2="20.4" y2="15"/></svg>`))

	icoMusic = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18V5l11-2v13"/><circle cx="6.5" cy="18" r="2.7"/><circle cx="17.5" cy="16" r="2.7"/></svg>`))

	icoMusicFill = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M20.2 2.26a1 1 0 0 0-.87-.24l-11 2A1 1 0 0 0 7.5 5v9.8a3.7 3.7 0 1 0 2 3.2V9.83l9.5-1.73v4.7a3.7 3.7 0 1 0 2 3.2V3a1 1 0 0 0-.8-.74Z"/></svg>`))

	icoPlay = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M7.2 4.6a1 1 0 0 1 1.5-.86l10.6 6.4a1 1 0 0 1 0 1.72l-10.6 6.4A1 1 0 0 1 7.2 17.4V4.6Z"/></svg>`))

	icoPause = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><rect x="6.5" y="4.5" width="4" height="15" rx="1.2"/><rect x="13.5" y="4.5" width="4" height="15" rx="1.2"/></svg>`))

	icoStop = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><rect x="5.5" y="5.5" width="13" height="13" rx="2"/></svg>`))

	icoRepeat = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><polyline points="17 2 21 6 17 10"/><path d="M3 11V9a3 3 0 0 1 3-3h15"/><polyline points="7 22 3 18 7 14"/><path d="M21 13v2a3 3 0 0 1-3 3H3"/></svg>`))

	icoVolume = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2.5 9 2.5 15 6 15 11 19 11 5"/><path d="M15.5 8.5a5 5 0 0 1 0 7"/><path d="M18.6 6a8.5 8.5 0 0 1 0 12"/></svg>`))

	icoVolumeMute = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2.5 9 2.5 15 6 15 11 19 11 5"/><line x1="21.5" y1="9" x2="15.5" y2="15"/><line x1="15.5" y1="9" x2="21.5" y2="15"/></svg>`))

	icoImage = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="m21 15-4.5-4.5a2 2 0 0 0-2.8 0L6 18"/></svg>`))

	icoImageFill = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" stroke="none"><path fill-rule="evenodd" d="M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Zm3.5 5a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3Zm10.5 10V13.8l-3.1-3.1a2 2 0 0 0-2.8 0L5.8 18H19Z"/></svg>`))

	icoTextSearch = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h4"/><path d="M14 3v5h5"/><path d="M19 8v2.5"/><line x1="8.5" y1="11.5" x2="12.5" y2="11.5"/><line x1="8.5" y1="15.5" x2="11" y2="15.5"/><circle cx="16.5" cy="16.5" r="3.2"/><line x1="18.9" y1="18.9" x2="21.2" y2="21.2"/></svg>`))

	icoTextSearchFill = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M14.2 2.5H7A2.5 2.5 0 0 0 4.5 5v14A2.5 2.5 0 0 0 7 21.5h4.3a5.2 5.2 0 0 1-.8-2.8 5.2 5.2 0 0 1 5.2-5.2c1.45 0 2.76.6 3.7 1.56V7.8a1 1 0 0 0-.29-.7l-4.3-4.3a1 1 0 0 0-.61-.3Zm-.45 2.06L17.44 8.25H14.5a.75.75 0 0 1-.75-.75V4.56ZM8.5 10.75a.75.75 0 0 0 0 1.5h4.5a.75.75 0 0 0 0-1.5H8.5Zm0 3.75a.75.75 0 0 0 0 1.5h2a.75.75 0 0 0 0-1.5h-2Z"/><path d="M15.7 14.8a3.9 3.9 0 1 0 2.15 7.15l1.95 1.95a.9.9 0 1 0 1.27-1.27l-1.95-1.95A3.9 3.9 0 0 0 15.7 14.8Zm0 1.8a2.1 2.1 0 1 1 0 4.2 2.1 2.1 0 0 1 0-4.2Z"/></svg>`))
)
