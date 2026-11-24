package uiapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/egoist/mygo/ui"
)

type pathDiagnosisStatus uint8

const (
	pathEmpty pathDiagnosisStatus = iota
	pathValid
	pathInvalid
)

type pathDiagnosis struct {
	status         pathDiagnosisStatus
	cleanPath      string
	version        string
	rootPath       string
	pickedGameDir  bool
	detail         string
	suggestedPath  string
	suggestedLabel string
}

type cachedPathDiag struct {
	diag      pathDiagnosis
	checkedAt time.Time
}

const pathDiagTTL = 800 * time.Millisecond

func normalizeUserPathInput(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}

func normalizeDroppedPath(raw string) string {
	s := normalizeUserPathInput(raw)
	if s == "" {
		return ""
	}
	if st, err := os.Stat(s); err == nil && !st.IsDir() {
		return filepath.Dir(s)
	}
	return s
}

func (a *App) invalidatePathDiag() {
	if a.pathDiagCache != nil {
		clear(a.pathDiagCache)
	}
}

func (a *App) diagnosePathCached(raw string) pathDiagnosis {
	norm := normalizeUserPathInput(raw)
	if norm == "" {
		return pathDiagnosis{status: pathEmpty}
	}
	if a.pathDiagCache == nil {
		a.pathDiagCache = make(map[string]cachedPathDiag)
	}
	now := time.Now()
	if cached, ok := a.pathDiagCache[norm]; ok && now.Sub(cached.checkedAt) < pathDiagTTL {
		return cached.diag
	}
	diag := diagnoseClientPath(norm)
	a.pathDiagCache[norm] = cachedPathDiag{diag: diag, checkedAt: now}
	return diag
}

// diagnoseClientPath 以严格只读方式校验目录，并在路径有误时生成玩家可理解的恢复建议。
func diagnoseClientPath(rawPath string) pathDiagnosis {
	cleaned := normalizeUserPathInput(rawPath)
	if cleaned == "" {
		return pathDiagnosis{status: pathEmpty}
	}
	cleaned = filepath.Clean(cleaned)

	if info, err := config.ValidateClientPath(cleaned, ""); err == nil && info != nil {
		pickedGame := !strings.EqualFold(filepath.Clean(info.RootPath), cleaned)
		return pathDiagnosis{
			status:        pathValid,
			cleanPath:     cleaned,
			version:       info.Version,
			rootPath:      info.RootPath,
			pickedGameDir: pickedGame,
		}
	}

	st, statErr := os.Stat(cleaned)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return pathDiagnosis{
				status:    pathInvalid,
				cleanPath: cleaned,
				detail:    "找不到这个路径，请检查是否拼写有误，或点击「选择文件夹」重新定位。",
			}
		}
		if errors.Is(statErr, os.ErrPermission) {
			return pathDiagnosis{
				status:    pathInvalid,
				cleanPath: cleaned,
				detail:    "没有权限访问这个文件夹，请检查文件夹读取权限。",
			}
		}
		return pathDiagnosis{
			status:    pathInvalid,
			cleanPath: cleaned,
			detail:    "无法读取该路径，请确认这是一个本地磁盘上的游戏文件夹。",
		}
	}

	// 用户选择了可执行文件等单个文件时，尝试向上定位游戏根目录
	if !st.IsDir() {
		parent := filepath.Dir(cleaned)
		for _, cand := range []string{parent, filepath.Dir(parent)} {
			if info, err := config.ValidateClientPath(cand, ""); err == nil && info != nil {
				return pathDiagnosis{
					status:         pathInvalid,
					cleanPath:      cleaned,
					detail:         "这里填入的是单个文件，游戏目录应选择它所在的游戏安装文件夹。",
					suggestedPath:  info.RootPath,
					suggestedLabel: filepath.Base(info.RootPath),
				}
			}
		}
		return pathDiagnosis{
			status:    pathInvalid,
			cleanPath: cleaned,
			detail:    "这里填入的是单个文件，请选择《最终幻想14》的安装文件夹。",
		}
	}

	// 情况 1：用户点进了过深的子目录（如 boot、sdo、sqpack 等）
	parent := filepath.Dir(cleaned)
	grand := filepath.Dir(parent)
	for _, cand := range []string{parent, grand} {
		if cand == "" || cand == cleaned {
			continue
		}
		if info, err := config.ValidateClientPath(cand, ""); err == nil && info != nil {
			base := strings.ToLower(filepath.Base(cleaned))
			parentBase := strings.ToLower(filepath.Base(parent))
			msg := "当前选到了游戏内部子文件夹，请改为外层的游戏安装根目录。"
			switch {
			case base == "boot" || base == "sdo" || base == "rail_files":
				msg = "当前选中的是启动器文件夹，请返回上一级选择整个游戏安装根目录。"
			case base == "sqpack" || parentBase == "sqpack":
				msg = "当前选到了较深层的资源子目录，请返回外层的游戏安装根目录。"
			}
			return pathDiagnosis{
				status:         pathInvalid,
				cleanPath:      cleaned,
				detail:         msg,
				suggestedPath:  info.RootPath,
				suggestedLabel: filepath.Base(info.RootPath),
			}
		}
	}

	// 情况 2：用户只选到了外层父目录（如 D:\Games 或 Steam\common），只读探测直接子目录
	entries, readErr := os.ReadDir(cleaned)
	if readErr != nil {
		if errors.Is(readErr, os.ErrPermission) {
			return pathDiagnosis{
				status:    pathInvalid,
				cleanPath: cleaned,
				detail:    "没有读取该文件夹的权限，请检查系统访问权限。",
			}
		}
		return pathDiagnosis{
			status:    pathInvalid,
			cleanPath: cleaned,
			detail:    "无法读取该文件夹内容，请重新选择游戏安装目录。",
		}
	}

	checked := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		checked++
		if checked > 50 {
			break
		}
		subPath := filepath.Join(cleaned, entry.Name())
		if info, err := config.ValidateClientPath(subPath, ""); err == nil && info != nil {
			return pathDiagnosis{
				status:         pathInvalid,
				cleanPath:      cleaned,
				detail:         fmt.Sprintf("当前是外层目录，里面包含游戏文件夹「%s」，点击右侧按钮可直接切换。", entry.Name()),
				suggestedPath:  info.RootPath,
				suggestedLabel: entry.Name(),
			}
		}
	}

	return pathDiagnosis{
		status:    pathInvalid,
		cleanPath: cleaned,
		detail:    "这个文件夹里没有检测到《最终幻想14》完整游戏数据，请确认其中包含「game」文件夹。",
	}
}

func friendlyConfigError(err error, cfg config.AppConfig) string {
	if errors.Is(err, config.ErrNoClientConfigured) {
		return "请先选择一个有效的游戏安装目录。"
	}
	if cn := normalizeUserPathInput(cfg.CNPath); cn != "" {
		if d := diagnoseClientPath(cn); d.status == pathInvalid {
			return "国服游戏目录无效：" + d.detail
		}
	}
	if gl := normalizeUserPathInput(cfg.GlobalPath); gl != "" {
		if d := diagnoseClientPath(gl); d.status == pathInvalid {
			return "国际服游戏目录无效：" + d.detail
		}
	}
	return "无法读取指定的游戏目录，请检查文件夹是否完整且可读取。"
}

// renderSetupView 渲染初次启动选目录页：单画布聚焦布局、区服模式引导、拖拽填入与非术语化纠错。
func (a *App) renderSetupView(c *ui.Context) {
	t := themeOf(c)
	a.renderBareTitleBar(c, true)

	_, vh := c.Size()
	tbH := max(titleBarHeight, c.TitleBar().Height)
	panelMinH := max(float32(0), vh-tbH-10)

	ui.Column(c).Grow(1).FillWidth().Padding(0, 8, 8, 8).Children(func() {
		kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
			ui.Scroll(c.Key("setup-scroll")).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().MinHeight(panelMinH).
					Justify(ui.Center).AlignItems(ui.Center).
					Padding(28, 32).Children(func() {
					ui.Column(c).FillWidth().MaxWidth(600).Gap(22).Children(func() {
						// 1. 核心任务标题与一句话说明
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							ui.Text(c, "选择 FF14 游戏目录").FontSize(t.text.x3l).FontWeight(600).LetterSpacing(-0.3).TextColor(t.foreground)
							ui.Text(c, "支持仅读取国服、仅读取国际服或双服对照查询；所有游戏文件仅以只读方式打开，不会改动任何内容。").
								FontSize(t.text.sm).LineHeight(lineProse).TextColor(t.muted)
						})

						// 2. 区服使用方式选择（明确单服与双服差异，消除“是否都要填”的困惑）
						a.renderSetupModeSelector(c)

						// 3. 按当前选择的区服模式展示对应目录输入区
						showCN := a.setupMode != setupModeGlobal
						showGlobal := a.setupMode != setupModeCN

						var cnDiag, glDiag pathDiagnosis
						if showCN {
							cnDiag = a.diagnosePathCached(a.draftCNPath)
						}
						if showGlobal {
							glDiag = a.diagnosePathCached(a.draftGlobalPath)
						}

						kitCard(c, ui.Column(c).FillWidth().Clip(), toneCard).Children(func() {
							if showCN {
								a.renderSetupPathBlock(
									c,
									"setup-cn",
									"国服游戏目录",
									"D:\\Games\\最终幻想XIV（粘贴路径，或把文件夹拖到这里）",
									"通常选择包含「game」和「boot」两个子文件夹的目录（如「最终幻想XIV」；直接选其中的「game」文件夹也可以）。",
									&a.draftCNPath,
									"选择 FF14 国服游戏目录",
									cnDiag,
								)
							}
							if showCN && showGlobal {
								kitSeparator(c, false).Margin(0, 16)
							}
							if showGlobal {
								a.renderSetupPathBlock(
									c,
									"setup-global",
									"国际服游戏目录",
									"D:\\Games\\FINAL FANTASY XIV Online（粘贴路径，或把文件夹拖到这里）",
									"通常选择「FINAL FANTASY XIV Online」或「FINAL FANTASY XIV - A Realm Reborn」安装文件夹（直接选「game」文件夹也可以）。",
									&a.draftGlobalPath,
									"选择 FF14 国际服游戏目录",
									glDiag,
								)
							}
						})

						if a.draftError != "" {
							kitAlert(c, toneDanger, a.draftError)
						}

						// 4. 底部确认状态与主行动按钮
						cnReady := !showCN || cnDiag.status == pathValid
						glReady := !showGlobal || glDiag.status == pathValid
						canSubmit := cnReady && glReady

						summaryTxt, summaryCol := setupSubmitSummary(t, showCN, showGlobal, cnDiag, glDiag)
						ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
							numText(c, summaryTxt).FontSize(t.text.xs).TextColor(summaryCol).Grow(1).Shrink(1)

							if kitButton(c, "setup-start-btn", "读取并进入工作台", btnOpts{
								variant:  btnDefault,
								size:     btnSizeMd,
								disabled: !canSubmit,
							}) {
								submitCN := ""
								submitGL := ""
								if showCN {
									submitCN = a.draftCNPath
								}
								if showGlobal {
									submitGL = a.draftGlobalPath
								}
								a.SaveAndReloadPaths(submitCN, submitGL)
							}
						})

						// 5. 静默底栏：配置保存位置
						ui.Row(c).FillWidth().Padding(14, 0, 0, 0).
							BorderWidth(hairline, 0, 0, 0).BorderColor(t.border).Children(func() {
							ui.Text(c, "配置文件："+config.ConfigFilePath(a.dataDir)).
								FontSize(t.text.micro).TextColor(t.muted).Selectable().Grow(1).Shrink(1)
						})
					})
				})
			})
		})
	})
}

func (a *App) renderSetupModeSelector(c *ui.Context) {
	t := themeOf(c)
	type modeOption struct {
		mode  setupServerMode
		label string
		desc  string
	}
	options := []modeOption{
		{
			mode:  setupModeCN,
			label: "仅国服",
			desc:  "只需选择一个国服游戏目录，即可使用全部中文道具、制作配方、采集坐标与版本对比功能。",
		},
		{
			mode:  setupModeGlobal,
			label: "仅国际服",
			desc:  "只需选择一个国际服游戏目录，条目名称将以英文显示，并支持导出英、日、德、法多语数据。",
		},
		{
			mode:  setupModeBoth,
			label: "国服 + 国际服",
			desc:  "以国服简体中文为主数据，同时关联国际服英文和日文名称，适合双语对照查名与导出。",
		},
	}

	activeDesc := options[0].desc
	for _, opt := range options {
		if opt.mode == a.setupMode {
			activeDesc = opt.desc
			break
		}
	}

	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, "选择你的游玩区服").FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)

		ui.Row(c.Key("setup-mode-track")).FillWidth().Padding(3).Gap(3).
			Radius(t.radius.lg).Background(t.tint(1.1)).AlignItems(ui.Center).Children(func() {
			for _, opt := range options {
				active := a.setupMode == opt.mode
				btn := ui.ButtonBase(c.Key(fmt.Sprintf("setup-mode-%d", opt.mode))).Label(opt.label).
					Grow(1).Height(30).PaddingX(12).Radius(t.radius.md).
					Justify(ui.Center).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).Transition(colorTransition(motionFast))
				if active {
					btn.Background(t.background).Border(hairline, t.border)
				} else if btn.Hovered() {
					btn.Background(t.tint(0.75))
				}
				btn.Children(func() {
					fg := t.muted
					fw := 500
					if active {
						fg = t.foreground
						fw = 600
					} else if btn.Hovered() {
						fg = t.subdued
					}
					ui.Text(c, opt.label).FontSize(t.text.xs).FontWeight(fw).TextColor(fg).SingleLine()
				})
				if btn.Clicked() && !active {
					a.setupMode = opt.mode
					a.draftError = ""
				}
			}
		})

		ui.Text(c, activeDesc).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.muted)
	})
}

func (a *App) renderSetupPathBlock(
	c *ui.Context,
	keyPrefix string,
	title string,
	placeholder string,
	emptyGuide string,
	val *string,
	dialogTitle string,
	diag pathDiagnosis,
) {
	t := themeOf(c)

	dropWrap := ui.Column(c.Key(keyPrefix+"-drop")).FillWidth().Padding(16, 18).Gap(10).
		Transition(colorTransition(motionFast))

	if dropped := dropWrap.DroppedFiles(); len(dropped) > 0 {
		if picked := normalizeDroppedPath(dropped[0]); picked != "" {
			*val = picked
			a.draftError = ""
			a.draftNotice = ""
			a.invalidatePathDiag()
			diag = a.diagnosePathCached(picked)
		}
	}
	if dropWrap.FileDragOver() {
		dropWrap.Background(t.accentSoft)
	}

	dropWrap.Children(func() {
		// 标题行：右侧仅在已填写内容后展示清晰状态标签，空态保持安静
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, title).FontSize(t.text.sm).FontWeight(600).TextColor(t.foreground)
			switch diag.status {
			case pathValid:
				drawPill(c, "已识别 · v"+diag.version, pillPositive)
			case pathInvalid:
				drawPill(c, "目录需调整", pillNegative)
			default:
				if dropWrap.FileDragOver() {
					drawPill(c, "松开填入此目录", pillAccent)
				}
			}
		})

		// 输入框 + 系统文件夹选择按钮
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Grow(1).Shrink(1).Children(func() {
				if kitInput(c, keyPrefix+"-input", val, kitInputOpts{
					placeholder: placeholder,
					label:       title,
					size:        btnSizeMd,
					clearable:   true,
				}).Changed() {
					a.draftError = ""
					a.draftNotice = ""
					a.invalidatePathDiag()
				}
			})
			if kitButton(c, keyPrefix+"-browse", "选择文件夹", btnOpts{
				variant: btnOutline,
				size:    btnSizeMd,
				icon:    icoFolder,
			}) {
				cur := normalizeUserPathInput(*val)
				a.pickDirectoryAsync(dialogTitle, cur, func(picked string) {
					*val = picked
					a.draftError = ""
					a.draftNotice = ""
					a.invalidatePathDiag()
				})
			}
		})

		// 上下文反馈行：未填时给玩家易懂引导；有效时确认版本；无效时明确告知原因与一键改选
		switch diag.status {
		case pathValid:
			msg := fmt.Sprintf("目录有效 · 客户端版本 v%s · 只读访问已就绪", diag.version)
			if diag.pickedGameDir {
				msg = fmt.Sprintf("已自动定位游戏根目录：%s（v%s）", diag.rootPath, diag.version)
			}
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icoCheck).Size(iconSm, iconSm).Shrink(0).TextColor(t.successText)
				numText(c, msg).FontSize(t.text.xs).TextColor(t.successText).Grow(1).Shrink(1)
			})
		case pathInvalid:
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Start).Grow(1).Shrink(1).Children(func() {
					ui.Icon(c, icoAlertCircle).Size(iconSm, iconSm).Margin(2, 0, 0, 0).Shrink(0).TextColor(t.dangerText)
					ui.Text(c, diag.detail).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.dangerText).Grow(1).Shrink(1)
				})
				if diag.suggestedPath != "" {
					btnText := "改选「" + diag.suggestedLabel + "」"
					if kitButton(c, keyPrefix+"-fix-btn", btnText, btnOpts{variant: btnSecondary, size: btnSizeXs}) {
						*val = diag.suggestedPath
						a.draftError = ""
						a.draftNotice = ""
						a.invalidatePathDiag()
					}
				}
			})
		default:
			ui.Text(c, emptyGuide).FontSize(t.text.xs).LineHeight(lineProse).TextColor(t.muted)
		}
	})
}

func setupSubmitSummary(t uiTokens, showCN, showGlobal bool, cnDiag, glDiag pathDiagnosis) (string, ui.Color) {
	switch {
	case showCN && showGlobal:
		if cnDiag.status == pathValid && glDiag.status == pathValid {
			return fmt.Sprintf("即将只读加载：国服 v%s · 国际服 v%s", cnDiag.version, glDiag.version), t.subdued
		}
		return "请分别选择有效的国服和国际服目录，或切换为单服模式", t.muted
	case showCN:
		if cnDiag.status == pathValid {
			return fmt.Sprintf("即将只读加载：国服 v%s（简体中文）", cnDiag.version), t.subdued
		}
		return "请选择国服游戏目录后继续", t.muted
	default:
		if glDiag.status == pathValid {
			return fmt.Sprintf("即将只读加载：国际服 v%s（英文主界面）", glDiag.version), t.subdued
		}
		return "请选择国际服游戏目录后继续", t.muted
	}
}

// renderLoadingView 渲染清晰、可预期的加载等待页：显示客户端版本、实时分表进度与真实三阶段状态。
func (a *App) renderLoadingView(c *ui.Context) {
	t := themeOf(c)
	a.renderBareTitleBar(c, true)

	_, vh := c.Size()
	tbH := max(titleBarHeight, c.TitleBar().Height)
	panelMinH := max(float32(0), vh-tbH-10)

	// 计算基于真实阶段的进度百分比（0~98%）
	var pct float32
	switch a.loadStage {
	case loadStageSheets:
		if a.loadSheetsTotal > 0 {
			pct = 72 * float32(a.loadSheetsDone) / float32(a.loadSheetsTotal)
		} else {
			pct = 8
		}
	case loadStageIndices:
		totalIdx := max(1, a.loadIndexTotal)
		pct = 72 + 20*float32(a.loadIndexStep)/float32(totalIdx)
	case loadStageSnapshot:
		pct = 94
	}
	pct = min(max(pct, 4), 98)

	elapsedSec := 0.0
	if !a.loadStartedAt.IsZero() {
		elapsedSec = time.Since(a.loadStartedAt).Seconds()
	}

	var clientParts []string
	if a.loadingCNVer != "" {
		clientParts = append(clientParts, "国服 v"+a.loadingCNVer)
	}
	if a.loadingGlobalVer != "" {
		clientParts = append(clientParts, "国际服 v"+a.loadingGlobalVer)
	}
	clientSummary := strings.Join(clientParts, " · ")

	ui.Column(c).Grow(1).FillWidth().Padding(0, 8, 8, 8).Children(func() {
		kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
			ui.Scroll(c.Key("loading-scroll")).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().MinHeight(panelMinH).
					Justify(ui.Center).AlignItems(ui.Center).
					Padding(28, 32).Children(func() {
					ui.Column(c).FillWidth().MaxWidth(480).Gap(20).Children(func() {
						// 1. 顶部标题、旋转指示与只读徽章
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(10).Children(func() {
								ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
									ui.Spinner(c).FontSize(18).TextColor(t.primary).Label("正在加载")
									ui.Text(c, "正在读取本地游戏数据").FontSize(t.text.xl).FontWeight(600).TextColor(t.foreground)
								})
								drawPill(c, "只读模式", pillPositive)
							})
							if clientSummary != "" {
								numText(c, clientSummary).FontSize(t.text.xs).TextColor(t.muted)
							}
						})

						// 2. 真实进度条与当前操作说明
						ui.Column(c).FillWidth().Gap(8).Children(func() {
							ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(12).Children(func() {
								statusTxt := a.loadingStatusTxt
								if statusTxt == "" {
									statusTxt = "正在准备数据读取..."
								}
								ui.Text(c, statusTxt).FontSize(t.text.xs).FontWeight(500).TextColor(t.foreground).SingleLine().Grow(1).Shrink(1)
								numTextf(c, "%.0f%% · %.1fs", pct, elapsedSec).FontSize(t.text.xs).TextColor(t.muted).Shrink(0)
							})

							ui.Row(c).FillWidth().Height(4).Radius(t.radius.full).Background(t.border).Clip().
								AlignItems(ui.Stretch).Children(func() {
								ui.Box(c).Grow(pct).FillHeight().Background(t.primary)
								ui.Box(c).Grow(max(0, 100-pct)).FillHeight()
							})
						})

						// 3. 三阶段真实进度清单
						sheetsMeta := "等待中"
						switch {
						case a.loadStage > loadStageSheets && a.loadSheetsTotal > 0:
							sheetsMeta = fmt.Sprintf("已完成 %d 张", a.loadSheetsTotal)
						case a.loadStage > loadStageSheets:
							sheetsMeta = "已完成"
						case a.loadSheetsTotal > 0:
							sheetsMeta = fmt.Sprintf("%d / %d 张表", a.loadSheetsDone, a.loadSheetsTotal)
						default:
							sheetsMeta = "读取中..."
						}

						indicesMeta := "等待中"
						switch {
						case a.loadStage > loadStageIndices:
							indicesMeta = "已完成"
						case a.loadStage == loadStageIndices:
							indicesMeta = fmt.Sprintf("第 %d / %d 步", min(a.loadIndexStep+1, max(1, a.loadIndexTotal)), max(1, a.loadIndexTotal))
						}

						snapshotMeta := "等待中"
						if a.loadStage == loadStageSnapshot {
							snapshotMeta = "校验中..."
						}

						kitCard(c, ui.Column(c).FillWidth().Clip(), toneCard).Children(func() {
							drawLoadingStageRow(c, "读取游戏原始数据表", sheetsMeta, a.loadStage == loadStageSheets, a.loadStage > loadStageSheets)
							kitSeparator(c, false).Margin(0, 16)
							drawLoadingStageRow(c, "构建物品、配方与任务索引", indicesMeta, a.loadStage == loadStageIndices, a.loadStage > loadStageIndices)
							kitSeparator(c, false).Margin(0, 16)
							drawLoadingStageRow(c, "校验本地版本快照", snapshotMeta, a.loadStage == loadStageSnapshot, false)
						})

						ui.Text(c, "所有文件均从本地硬盘只读解析，通常只需数秒钟即可完成。").
							FontSize(t.text.xs).TextColor(t.muted)
					})
				})
			})
		})
	})
}

func drawLoadingStageRow(c *ui.Context, title, status string, active, done bool) {
	t := themeOf(c)
	ui.Row(c).FillWidth().MinHeight(40).Padding(9, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Grow(1).Shrink(1).Children(func() {
			// 使用等宽 14×14 容器包裹状态标记，保证三行阶段文案左边缘像素对齐
			ui.Box(c).Size(iconSm, iconSm).Center().Shrink(0).Children(func() {
				switch {
				case done:
					ui.Icon(c, icoCheck).Size(iconSm, iconSm).TextColor(t.successText)
				case active:
					drawStatusDot(c, t.primary)
				default:
					drawStatusDot(c, t.border)
				}
			})

			fg := t.muted
			fw := 400
			if done {
				fg = t.subdued
			} else if active {
				fg = t.foreground
				fw = 500
			}
			ui.Text(c, title).FontSize(t.text.xs).FontWeight(fw).TextColor(fg).SingleLine().Grow(1).Shrink(1)
		})

		metaCol := t.muted
		if done {
			metaCol = t.successText
		} else if active {
			metaCol = t.infoText
		}
		numText(c, status).FontSize(t.text.xs).TextColor(metaCol).Shrink(0).SingleLine()
	})
}

// renderSettingsView 渲染居中设置页：外观主题、客户端路径、CSV 导出与运行状态。
func (a *App) renderSettingsView(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		ui.Scroll(c.Key("settings-scroll")).Grow(1).Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(28, 32, 36, 32).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(720).Gap(22).Children(func() {
					// 页面主标题
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Baseline).Gap(12).Children(func() {
						ui.Text(c, "设置").FontSize(t.text.x3l).FontWeight(600).LetterSpacing(-0.3).TextColor(t.foreground)
						numText(c, "v"+a.appVersion).FontSize(t.text.xs).TextColor(t.muted)
					})

					// 1. 外观主题设置
					drawSettingsSection(c, "外观", "",
						func() {
							ui.Row(c).FillWidth().Padding(14, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
								ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
									ui.Text(c, "界面主题").FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
									ui.Text(c, "选择浅色、深色或自动跟随系统").FontSize(t.text.xs).TextColor(t.muted)
								})
								ui.Row(c).Gap(10).Shrink(0).Children(func() {
									a.renderThemeTile(c, "system", "跟随系统")
									a.renderThemeTile(c, "light", "浅色")
									a.renderThemeTile(c, "dark", "深色")
								})
							})
						},
					)

					// 2. FF14 游戏客户端路径配置
					pathDirty := normalizeUserPathInput(a.draftCNPath) != strings.TrimSpace(a.cfg.CNPath) ||
						normalizeUserPathInput(a.draftGlobalPath) != strings.TrimSpace(a.cfg.GlobalPath)
					drawSettingsSection(c, "FF14 游戏客户端", "保存后将重新读取本地数据；所有游戏文件保持只读。",
						func() {
							ui.Column(c).FillWidth().Padding(14, 16).Children(func() {
								a.renderSinglePathInput(c, "settings-cn", "国服客户端路径 (CN)", "提供简体中文与国服数据", "例如：D:\\Games\\最终幻想XIV", &a.draftCNPath, "选择 FF14 国服客户端目录")
							})
						},
						func() {
							ui.Column(c).FillWidth().Padding(14, 16).Children(func() {
								a.renderSinglePathInput(c, "settings-global", "国际服客户端路径 (Global)", "提供英/日/德/法多语言数据", "例如：D:\\Games\\FINAL FANTASY XIV Online", &a.draftGlobalPath, "选择 FF14 国际服客户端目录")
							})
						},
						func() {
							ui.Column(c).FillWidth().Padding(12, 16).Gap(10).Children(func() {
								if a.draftError != "" {
									kitAlert(c, toneDanger, a.draftError)
								}
								if a.draftNotice != "" {
									kitAlert(c, toneSuccess, a.draftNotice)
								}
								ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
									if kitButton(c, "settings-revert-btn", "撤销修改", btnOpts{variant: btnGhost, size: btnSizeSm, disabled: !pathDirty}) {
										a.draftCNPath = a.cfg.CNPath
										a.draftGlobalPath = a.cfg.GlobalPath
										a.draftError = ""
										a.draftNotice = "已恢复当前生效的路径配置。"
										a.invalidatePathDiag()
									}
									canSave := pathDirty && (normalizeUserPathInput(a.draftCNPath) != "" || normalizeUserPathInput(a.draftGlobalPath) != "")
									if kitButton(c, "settings-save-btn", "保存并重新加载", btnOpts{variant: btnDefault, size: btnSizeSm, disabled: !canSave}) {
										a.SaveAndReloadPaths(a.draftCNPath, a.draftGlobalPath)
									}
								})
							})
						},
					)

					// 3. 数据工具
					drawSettingsSection(c, "数据工具", "",
						func() {
							ui.Row(c).FillWidth().Padding(14, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
								ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
									ui.Text(c, "导出全量游戏 CSV").FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
									ui.Text(c, "将全部游戏数据表导出为标准 CSV 文件或 Zip 压缩包").FontSize(t.text.xs).TextColor(t.muted)
								})
								if kitButton(c, "settings-open-export", "导出游戏 CSV", btnOpts{variant: btnOutline, size: btnSizeSm, icon: icoDownload}) {
									a.openExportModal()
								}
							})
						},
					)

					// 4. 运行状态与本地目录
					cnState := "未配置"
					if a.status != nil && a.status.HasCN {
						cnState = fmt.Sprintf("已加载 · v%s", a.status.CNVersion)
					}
					glState := "未配置"
					if a.status != nil && a.status.HasGlobal {
						glState = fmt.Sprintf("已加载 · v%s", a.status.GlobalVersion)
					}
					statsText := "–"
					if a.status != nil {
						statsText = fmt.Sprintf("%s 物品 · %s 配方 · %s 采集 · %s 成就（%d ms）",
							formatInt(a.status.Stats.TotalItems),
							formatInt(a.status.Stats.TotalRecipes),
							formatInt(a.status.Stats.GatherableItems),
							formatInt(a.status.Stats.TotalAchievements),
							a.status.Stats.LoadTimeMs,
						)
					}
					drawSettingsSection(c, "运行状态与存储", "",
						func() { drawSettingKVRow(c, "国服客户端", cnState) },
						func() { drawSettingKVRow(c, "国际服客户端", glState) },
						func() { drawSettingKVRow(c, "数据概览", statsText) },
						func() { drawSettingKVRow(c, "配置文件路径", config.ConfigFilePath(a.dataDir)) },
						func() { drawSettingKVRow(c, "版本快照目录", filepath.Join(a.dataDir, "backups")) },
					)
				})
			})
		})
	})
}

// renderThemeTile 绘制外观缩略图选择卡片（76×48 迷你桌面预览窗）。
func (a *App) renderThemeTile(c *ui.Context, mode, label string) {
	t := themeOf(c)
	cur := a.cfg.Theme
	if cur == "" {
		cur = "system"
	}
	selected := cur == mode

	btn := ui.ButtonBase(c.Key("theme-tile-" + mode)).Label(label).
		AlignItems(ui.Center).Gap(5).Cursor(ui.CursorPointer)
	btn.Children(func() {
		ui.Column(c).AlignItems(ui.Center).Gap(5).Children(func() {
			frame := ui.Row(c).Size(76, 48).Radius(t.radius.lg).Clip().Transition(colorTransition(motionFast))
			if selected {
				frame.Border(2, t.primary)
			} else if btn.Hovered() {
				frame.Border(strokeWidth, t.input)
			} else {
				frame.Border(strokeWidth, t.border)
			}

			drawHalf := func(dark bool) {
				var winBg, panelBg, lineCol ui.Color
				if dark {
					winBg = pSlate800.color()
					panelBg = pSlate950.color()
					lineCol = pSlate700.color()
				} else {
					winBg = pSlate100.color()
					panelBg = pWhite.color()
					lineCol = pSlate300.color()
				}
				ui.Row(c).Grow(1).FillHeight().Background(winBg).Padding(5).Gap(4).Children(func() {
					ui.Column(c).Width(14).FillHeight().Gap(3).Padding(2, 0).Children(func() {
						ui.Box(c).FillWidth().Height(3).Radius(2).Background(lineCol)
						ui.Box(c).FillWidth().Height(3).Radius(2).Background(lineCol.Alpha(0.6))
						ui.Box(c).FillWidth().Height(3).Radius(2).Background(lineCol.Alpha(0.6))
					})
					ui.Column(c).Grow(1).FillHeight().Radius(4).Background(panelBg).Padding(5).Gap(4).Children(func() {
						ui.Box(c).FillWidth().Height(4).Radius(2).Background(lineCol)
						ui.Box(c).Width(20).Height(3).Radius(2).Background(lineCol.Alpha(0.55))
					})
				})
			}

			frame.Children(func() {
				switch mode {
				case "light":
					drawHalf(false)
				case "dark":
					drawHalf(true)
				default:
					ui.Row(c).Fill().Children(func() {
						ui.Row(c).Grow(1).FillHeight().Background(pSlate100.color()).Padding(5, 0, 5, 5).Children(func() {
							ui.Column(c).Fill().Radius(4, 0, 0, 4).Background(pWhite.color()).Padding(5).Gap(4).Children(func() {
								ui.Box(c).FillWidth().Height(4).Radius(2).Background(pSlate300.color())
								ui.Box(c).Width(16).Height(3).Radius(2).Background(pSlate200.color())
							})
						})
						ui.Row(c).Grow(1).FillHeight().Background(pSlate800.color()).Padding(5, 5, 5, 0).Children(func() {
							ui.Column(c).Fill().Radius(0, 4, 4, 0).Background(pSlate950.color()).Padding(5).Gap(4).Children(func() {
								ui.Box(c).FillWidth().Height(4).Radius(2).Background(pSlate700.color())
								ui.Box(c).Width(16).Height(3).Radius(2).Background(pSlate800.color())
							})
						})
					})
				}
			})

			fg := t.muted
			fw := 400
			if selected {
				fg = t.foreground
				fw = 500
			}
			ui.Text(c, label).FontSize(t.text.xs).FontWeight(fw).TextColor(fg).SingleLine()
		})
	})
	if btn.Clicked() {
		a.SetThemeMode(mode)
	}
}

func drawSettingKVRow(c *ui.Context, label, value string) {
	t := themeOf(c)
	ui.Row(c).FillWidth().MinHeight(42).Padding(10, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
		ui.Text(c, label).FontSize(t.text.sm).TextColor(t.subdued).Shrink(0).SingleLine()
		ui.Text(c, value).FontSize(t.text.xs).TextColor(t.muted).FontFeatures("tnum").Selectable().TextAlign(ui.End).SingleLine().Grow(1).Shrink(1)
	})
}

func (a *App) renderSinglePathInput(
	c *ui.Context,
	keyPrefix string,
	title string,
	hint string,
	placeholder string,
	val *string,
	dialogTitle string,
) {
	t := themeOf(c)
	diag := a.diagnosePathCached(*val)
	wrap := ui.Column(c.Key(keyPrefix + "-wrap")).FillWidth().Gap(8)
	if dropped := wrap.DroppedFiles(); len(dropped) > 0 {
		if picked := normalizeDroppedPath(dropped[0]); picked != "" {
			*val = picked
			a.draftError = ""
			a.draftNotice = ""
			a.invalidatePathDiag()
			diag = a.diagnosePathCached(picked)
		}
	}
	if wrap.FileDragOver() {
		wrap.Background(t.accentSoft).Radius(t.radius.md)
	}

	wrap.Children(func() {
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Column(c).Gap(1).Grow(1).Shrink(1).Children(func() {
				ui.Text(c, title).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
				ui.Text(c, hint).FontSize(t.text.xs).TextColor(t.muted)
			})
			switch diag.status {
			case pathValid:
				drawPill(c, "已识别 · v"+diag.version, pillPositive)
			case pathInvalid:
				drawPill(c, "目录需调整", pillNegative)
			default:
				drawPill(c, "未填写", pillOutline)
			}
		})

		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Grow(1).Shrink(1).Children(func() {
				if kitInput(c, keyPrefix+"-input", val, kitInputOpts{
					placeholder: placeholder, label: title, size: btnSizeMd, clearable: true,
				}).Changed() {
					a.draftError = ""
					a.draftNotice = ""
					a.invalidatePathDiag()
				}
			})
			if kitButton(c, keyPrefix+"-browse", "选择文件夹", btnOpts{variant: btnOutline, size: btnSizeMd, icon: icoFolder}) {
				cur := normalizeUserPathInput(*val)
				a.pickDirectoryAsync(dialogTitle, cur, func(picked string) {
					*val = picked
					a.draftError = ""
					a.draftNotice = ""
					a.invalidatePathDiag()
				})
			}
		})

		if diag.status == pathInvalid {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, diag.detail).FontSize(t.text.xs).TextColor(t.dangerText).Grow(1).Shrink(1)
				if diag.suggestedPath != "" {
					if kitButton(c, keyPrefix+"-fix", "改选「"+diag.suggestedLabel+"」", btnOpts{variant: btnSecondary, size: btnSizeXs}) {
						*val = diag.suggestedPath
						a.draftError = ""
						a.draftNotice = ""
						a.invalidatePathDiag()
					}
				}
			})
		}
	})
}
