package uiapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
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
	cacheKey := string(a.Language()) + "\x00" + norm
	now := time.Now()
	if cached, ok := a.pathDiagCache[cacheKey]; ok && now.Sub(cached.checkedAt) < pathDiagTTL {
		return cached.diag
	}
	diag := a.diagnoseClientPath(norm)
	a.pathDiagCache[cacheKey] = cachedPathDiag{diag: diag, checkedAt: now}
	return diag
}

// diagnoseClientPath 以严格只读方式校验目录，并在路径有误时生成玩家可理解的恢复建议。
func (a *App) diagnoseClientPath(rawPath string) pathDiagnosis {
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
				detail:    a.t("setup.diag_not_found"),
			}
		}
		if errors.Is(statErr, os.ErrPermission) {
			return pathDiagnosis{
				status:    pathInvalid,
				cleanPath: cleaned,
				detail:    a.t("setup.diag_permission"),
			}
		}
		return pathDiagnosis{
			status:    pathInvalid,
			cleanPath: cleaned,
			detail:    a.t("setup.diag_unreadable"),
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
					detail:         a.t("setup.diag_file_found"),
					suggestedPath:  info.RootPath,
					suggestedLabel: filepath.Base(info.RootPath),
				}
			}
		}
		return pathDiagnosis{
			status:    pathInvalid,
			cleanPath: cleaned,
			detail:    a.t("setup.diag_file_invalid"),
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
			msg := a.t("setup.diag_sub_folder")
			switch {
			case base == "boot" || base == "sdo" || base == "rail_files":
				msg = a.t("setup.diag_boot_folder")
			case base == "sqpack" || parentBase == "sqpack":
				msg = a.t("setup.diag_deep_folder")
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
				detail:    a.t("setup.diag_dir_perm"),
			}
		}
		return pathDiagnosis{
			status:    pathInvalid,
			cleanPath: cleaned,
			detail:    a.t("setup.diag_dir_unreadable"),
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
				detail:         a.tf("setup.diag_parent_found", "name", entry.Name()),
				suggestedPath:  info.RootPath,
				suggestedLabel: entry.Name(),
			}
		}
	}

	return pathDiagnosis{
		status:    pathInvalid,
		cleanPath: cleaned,
		detail:    a.t("setup.diag_missing_game"),
	}
}

func (a *App) friendlyConfigError(err error, cfg config.AppConfig) string {
	if errors.Is(err, config.ErrNoClientConfigured) {
		return a.t("setup.err_none")
	}
	if cn := normalizeUserPathInput(cfg.CNPath); cn != "" {
		if d := a.diagnoseClientPath(cn); d.status == pathInvalid {
			return a.tf("setup.err_cn", "detail", d.detail)
		}
	}
	if gl := normalizeUserPathInput(cfg.GlobalPath); gl != "" {
		if d := a.diagnoseClientPath(gl); d.status == pathInvalid {
			return a.tf("setup.err_global", "detail", d.detail)
		}
	}
	return a.t("setup.err_generic")
}

// renderLanguageSegmentedBar 绘制 简体中文 / 日本語 / English 三语言切换分段控件。
func (a *App) renderLanguageSegmentedBar(c *ui.Context, keyPrefix string, compact bool) {
	t := themeOf(c)
	curLang := a.Language()
	h := float32(28)
	padX := float32(10)
	if compact {
		h = 26
		padX = 8
	}
	ui.Row(c.Key(keyPrefix + "-track")).Padding(3).Gap(3).Radius(t.radius.md).Background(t.tint(1.1)).AlignItems(ui.Center).Shrink(0).Children(func() {
		for _, opt := range i18n.SupportedLangs {
			active := curLang == opt.Code
			btn := ui.ButtonBase(c.Key(keyPrefix + "-" + string(opt.Code))).Label(opt.Autonym).
				Height(h).PaddingX(padX).Radius(t.radius.sm).
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
				ui.Text(c, opt.Autonym).FontSize(t.text.xs).FontWeight(fw).TextColor(fg).SingleLine()
			})
			if btn.Clicked() && !active {
				a.SetLanguage(opt.Code)
			}
		}
	})
}

func (a *App) renderSetupLanguageBar(c *ui.Context) {
	t := themeOf(c)
	hintKey := "lang.secondary_hint_" + string(a.Language())
	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, a.t(hintKey)).FontSize(t.text.xs).TextColor(t.muted).Shrink(1).SingleLine()
		a.renderLanguageSegmentedBar(c, "setup-lang", true)
	})
}

// renderSetupView 渲染初次启动选目录页：单画布聚焦布局、语言设置、区服模式引导、拖拽填入与非术语化纠错。
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
						// 0. 欢迎界面语言设置栏（首次打开自动匹配系统语言，也支持直接手动切换）
						a.renderSetupLanguageBar(c)

						// 1. 核心任务标题与一句话说明
						ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Center).Children(func() {
							if bmp := appLogoBitmap(); bmp != nil {
								ui.Image(c, bmp).Size(48, 48).Fit(ui.Contain).Shrink(0)
							}
							ui.Column(c).Grow(1).Shrink(1).Gap(6).Children(func() {
								ui.Text(c, a.t("setup.title")).FontSize(t.text.x3l).FontWeight(600).LetterSpacing(-0.3).TextColor(t.foreground)
								ui.Text(c, a.t("setup.subtitle")).
									FontSize(t.text.sm).LineHeight(lineProse).TextColor(t.muted)
							})
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
									a.t("setup.cn_dir_title"),
									a.t("setup.cn_dir_placeholder"),
									a.t("setup.cn_dir_guide"),
									&a.draftCNPath,
									a.t("setup.cn_dialog_title"),
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
									a.t("setup.global_dir_title"),
									a.t("setup.global_dir_placeholder"),
									a.t("setup.global_dir_guide"),
									&a.draftGlobalPath,
									a.t("setup.global_dialog_title"),
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

						summaryTxt, summaryCol := a.setupSubmitSummary(t, showCN, showGlobal, cnDiag, glDiag)
						ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
							numText(c, summaryTxt).FontSize(t.text.xs).TextColor(summaryCol).Grow(1).Shrink(1)

							if kitButton(c, "setup-start-btn", a.t("setup.start_btn"), btnOpts{
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
							ui.Text(c, a.t("setup.config_path_prefix")+config.ConfigFilePath(a.dataDir)).
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
			label: a.t("setup.mode_cn"),
			desc:  a.t("setup.mode_cn_desc"),
		},
		{
			mode:  setupModeGlobal,
			label: a.t("setup.mode_global"),
			desc:  a.t("setup.mode_global_desc"),
		},
		{
			mode:  setupModeBoth,
			label: a.t("setup.mode_both"),
			desc:  a.t("setup.mode_both_desc"),
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
		ui.Text(c, a.t("setup.mode_heading")).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)

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
					a.setupModeTouched = true
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
				drawPill(c, a.tf("setup.pill_valid", "version", diag.version), pillPositive)
			case pathInvalid:
				drawPill(c, a.t("setup.pill_invalid"), pillNegative)
			default:
				if dropWrap.FileDragOver() {
					drawPill(c, a.t("setup.pill_drop"), pillAccent)
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
			if kitButton(c, keyPrefix+"-browse", a.t("common.browse"), btnOpts{
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
			msg := a.tf("setup.valid_msg", "version", diag.version)
			if diag.pickedGameDir {
				msg = a.tf("setup.valid_auto_root", "root", diag.rootPath, "version", diag.version)
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
					btnText := a.tf("setup.fix_btn", "name", diag.suggestedLabel)
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

func (a *App) setupSubmitSummary(t uiTokens, showCN, showGlobal bool, cnDiag, glDiag pathDiagnosis) (string, ui.Color) {
	switch {
	case showCN && showGlobal:
		if cnDiag.status == pathValid && glDiag.status == pathValid {
			return a.tf("setup.summary_both_ready", "cn", cnDiag.version, "global", glDiag.version), t.subdued
		}
		return a.t("setup.summary_both_wait"), t.muted
	case showCN:
		if cnDiag.status == pathValid {
			return a.tf("setup.summary_cn_ready", "cn", cnDiag.version), t.subdued
		}
		return a.t("setup.summary_cn_wait"), t.muted
	default:
		if glDiag.status == pathValid {
			langLabel := i18n.OptionOf(a.Language()).Autonym
			if a.Language() == i18n.LangZH {
				langLabel = i18n.OptionOf(i18n.LangEN).Autonym
			}
			return a.tf("setup.summary_global_ready", "global", glDiag.version, "lang", langLabel), t.subdued
		}
		return a.t("setup.summary_global_wait"), t.muted
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
			pct = 75 * float32(a.loadSheetsDone) / float32(a.loadSheetsTotal)
		} else {
			pct = 8
		}
	case loadStageIndices:
		totalIdx := max(1, a.loadIndexTotal)
		pct = 75 + 23*float32(a.loadIndexStep)/float32(totalIdx)
	}
	pct = min(max(pct, 4), 98)

	elapsedSec := 0.0
	if !a.loadStartedAt.IsZero() {
		elapsedSec = time.Since(a.loadStartedAt).Seconds()
	}

	var clientParts []string
	if a.loadingCNVer != "" {
		clientParts = append(clientParts, a.t("shell.server_cn")+" v"+a.loadingCNVer)
	}
	if a.loadingGlobalVer != "" {
		clientParts = append(clientParts, a.t("shell.server_global")+" v"+a.loadingGlobalVer)
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
									ui.Spinner(c).FontSize(18).TextColor(t.primary).Label(a.t("loading.spinner"))
									ui.Text(c, a.t("loading.title")).FontSize(t.text.xl).FontWeight(600).TextColor(t.foreground)
								})
								drawPill(c, a.t("common.readonly_mode"), pillPositive)
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
									statusTxt = a.t("loading.preparing")
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
						sheetsMeta := a.t("loading.waiting")
						switch {
						case a.loadStage > loadStageSheets && a.loadSheetsTotal > 0:
							sheetsMeta = a.tf("loading.done_count", "total", fmt.Sprintf("%d", a.loadSheetsTotal))
						case a.loadStage > loadStageSheets:
							sheetsMeta = a.t("loading.done")
						case a.loadSheetsTotal > 0:
							sheetsMeta = a.tf("loading.sheets_progress", "done", fmt.Sprintf("%d", a.loadSheetsDone), "total", fmt.Sprintf("%d", a.loadSheetsTotal))
						default:
							sheetsMeta = a.t("loading.reading")
						}

						indicesMeta := a.t("loading.waiting")
						switch {
						case a.loadStage > loadStageIndices:
							indicesMeta = a.t("loading.done")
						case a.loadStage == loadStageIndices:
							indicesMeta = a.tf("loading.step_progress", "step", fmt.Sprintf("%d", min(a.loadIndexStep+1, max(1, a.loadIndexTotal))), "total", fmt.Sprintf("%d", max(1, a.loadIndexTotal)))
						}

						kitCard(c, ui.Column(c).FillWidth().Clip(), toneCard).Children(func() {
							drawLoadingStageRow(c, a.t("loading.stage_sheets"), sheetsMeta, a.loadStage == loadStageSheets, a.loadStage > loadStageSheets)
							kitSeparator(c, false).Margin(0, 16)
							drawLoadingStageRow(c, a.t("loading.stage_indices"), indicesMeta, a.loadStage == loadStageIndices, a.loadStage > loadStageIndices)
						})

						ui.Text(c, a.t("loading.footnote")).
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

// renderSettingsView 渲染居中设置页：语言与外观、客户端路径、CSV 导出与运行状态。
func (a *App) renderSettingsView(c *ui.Context) {
	t := themeOf(c)
	kitPanel(c, a.panelRadius(c)).Fill().Children(func() {
		ui.Scroll(c.Key("settings-scroll")).Grow(1).Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(28, 32, 36, 32).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(720).Gap(22).Children(func() {
					// 页面主标题
					ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Baseline).Gap(12).Children(func() {
						ui.Text(c, a.t("settings.title")).FontSize(t.text.x3l).FontWeight(600).LetterSpacing(-0.3).TextColor(t.foreground)
						numText(c, "v"+a.appVersion).FontSize(t.text.xs).TextColor(t.muted)
					})

					// 1. 语言与外观设置
					langDesc := a.t("lang.desc") + " · " + a.t("lang.secondary_hint_"+string(a.Language()))
					drawSettingsSection(c, a.t("settings.section_appearance"), "",
						func() {
							ui.Row(c).FillWidth().Padding(14, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
								ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
									ui.Text(c, a.t("lang.label")).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
									ui.Text(c, langDesc).FontSize(t.text.xs).TextColor(t.muted)
								})
								a.renderLanguageSegmentedBar(c, "settings-lang", false)
							})
						},
						func() {
							ui.Row(c).FillWidth().Padding(14, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
								ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
									ui.Text(c, a.t("settings.theme_title")).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
									ui.Text(c, a.t("settings.theme_desc")).FontSize(t.text.xs).TextColor(t.muted)
								})
								ui.Row(c).Gap(10).Shrink(0).Children(func() {
									a.renderThemeTile(c, "system", a.t("settings.theme_system"))
									a.renderThemeTile(c, "light", a.t("settings.theme_light"))
									a.renderThemeTile(c, "dark", a.t("settings.theme_dark"))
								})
							})
						},
					)

					// 2. FF14 游戏客户端路径配置
					pathDirty := normalizeUserPathInput(a.draftCNPath) != strings.TrimSpace(a.cfg.CNPath) ||
						normalizeUserPathInput(a.draftGlobalPath) != strings.TrimSpace(a.cfg.GlobalPath)
					drawSettingsSection(c, a.t("settings.section_clients"), a.t("settings.clients_footnote"),
						func() {
							ui.Column(c).FillWidth().Padding(14, 16).Children(func() {
								a.renderSinglePathInput(c, "settings-cn", a.t("settings.cn_label"), a.t("settings.cn_hint"), a.t("settings.cn_placeholder"), &a.draftCNPath, a.t("settings.cn_dialog"))
							})
						},
						func() {
							ui.Column(c).FillWidth().Padding(14, 16).Children(func() {
								a.renderSinglePathInput(c, "settings-global", a.t("settings.global_label"), a.t("settings.global_hint"), a.t("settings.global_placeholder"), &a.draftGlobalPath, a.t("settings.global_dialog"))
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
									if kitButton(c, "settings-revert-btn", a.t("settings.revert"), btnOpts{variant: btnGhost, size: btnSizeSm, disabled: !pathDirty}) {
										a.draftCNPath = a.cfg.CNPath
										a.draftGlobalPath = a.cfg.GlobalPath
										a.draftError = ""
										a.draftNotice = a.t("settings.reverted")
										a.invalidatePathDiag()
									}
									canSave := pathDirty && (normalizeUserPathInput(a.draftCNPath) != "" || normalizeUserPathInput(a.draftGlobalPath) != "")
									if kitButton(c, "settings-save-btn", a.t("settings.save_reload"), btnOpts{variant: btnDefault, size: btnSizeSm, disabled: !canSave}) {
										a.SaveAndReloadPaths(a.draftCNPath, a.draftGlobalPath)
									}
								})
							})
						},
					)

					// 3. 数据工具
					drawSettingsSection(c, a.t("settings.section_tools"), "",
						func() {
							ui.Row(c).FillWidth().Padding(14, 16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(16).Children(func() {
								ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
									ui.Text(c, a.t("settings.export_title")).FontSize(t.text.sm).FontWeight(500).TextColor(t.foreground)
									ui.Text(c, a.t("settings.export_desc")).FontSize(t.text.xs).TextColor(t.muted)
								})
								if kitButton(c, "settings-open-export", a.t("settings.export_btn"), btnOpts{variant: btnOutline, size: btnSizeSm, icon: icoDownload}) {
									a.openExportModal()
								}
							})
						},
					)

					// 4. 运行状态与本地目录
					cnState := a.t("settings.state_unconfigured")
					if a.status != nil && a.status.HasCN {
						cnState = a.tf("settings.state_loaded", "version", a.status.CNVersion)
					}
					glState := a.t("settings.state_unconfigured")
					if a.status != nil && a.status.HasGlobal {
						glState = a.tf("settings.state_loaded", "version", a.status.GlobalVersion)
					}
					statsText := "–"
					if a.status != nil {
						statsText = a.tf("settings.stats_summary",
							"items", formatInt(a.status.Stats.TotalItems),
							"recipes", formatInt(a.status.Stats.TotalRecipes),
							"gather", formatInt(a.status.Stats.GatherableItems),
							"ach", formatInt(a.status.Stats.TotalAchievements),
							"ms", fmt.Sprintf("%d", a.status.Stats.LoadTimeMs),
						)
					}
					backupDirDisp := a.snapshotDir()
					if a.versionStatus != nil && a.versionStatus.SnapshotDir != "" {
						backupDirDisp = a.versionStatus.SnapshotDir
					}
					drawSettingsSection(c, a.t("settings.section_status"), "",
						func() { drawSettingKVRow(c, a.t("settings.kv_cn"), cnState) },
						func() { drawSettingKVRow(c, a.t("settings.kv_global"), glState) },
						func() { drawSettingKVRow(c, a.t("settings.kv_overview"), statsText) },
						func() { drawSettingKVRow(c, a.t("settings.kv_config_path"), config.ConfigFilePath(a.dataDir)) },
						func() { drawSettingKVRow(c, a.t("settings.kv_backup_dir"), backupDirDisp) },
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
				drawPill(c, a.tf("setup.pill_valid", "version", diag.version), pillPositive)
			case pathInvalid:
				drawPill(c, a.t("setup.pill_invalid"), pillNegative)
			default:
				drawPill(c, a.t("setup.pill_empty"), pillOutline)
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
			if kitButton(c, keyPrefix+"-browse", a.t("common.browse"), btnOpts{variant: btnOutline, size: btnSizeMd, icon: icoFolder}) {
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
					if kitButton(c, keyPrefix+"-fix", a.tf("setup.fix_btn", "name", diag.suggestedLabel), btnOpts{variant: btnSecondary, size: btnSizeXs}) {
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
