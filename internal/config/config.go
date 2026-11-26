package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
)

// ErrNoClientConfigured 表示用户尚未配置国服或国际服客户端路径。
var ErrNoClientConfigured = errors.New("尚未配置国服或国际服 FF14 游戏客户端路径，请至少填写其中之一")

// AppConfig 存储客户端持久化用户配置（保存在应用数据目录的 config.json）。
type AppConfig struct {
	CNPath              string `json:"cnPath"`
	GlobalPath          string `json:"globalPath"`
	ExportOutputDir     string `json:"exportOutputDir"`
	IconExportOutputDir string `json:"iconExportOutputDir,omitempty"`
	DiffExportOutputDir string `json:"diffExportOutputDir,omitempty"`
	Theme               string `json:"theme,omitempty"`    // "system" | "light" | "dark"，空值默认跟随系统
	Language            string `json:"language,omitempty"` // "zh" | "ja" | "en"，空值表示自动跟随系统语言
}

// ClientInfo 表示经过只读校验后的单个服务器客户端目录信息。
type ClientInfo struct {
	RootPath string // 用户配置的根目录
	GamePath string // 实际包含 sqpack 与 ffxivgame.ver 的 game 目录
	Version  string // 读取自 ffxivgame.ver 的版本号
}

// ResolvedPaths 表示校验通过后的国服与国际服客户端路径集合（至少存在其一）。
type ResolvedPaths struct {
	CN     *ClientInfo
	Global *ClientInfo
}

// HasCN 返回是否已配置有效的国服客户端。
func (r *ResolvedPaths) HasCN() bool {
	return r != nil && r.CN != nil
}

// HasGlobal 返回是否已配置有效的国际服客户端。
func (r *ResolvedPaths) HasGlobal() bool {
	return r != nil && r.Global != nil
}

// PrimaryVersion 返回主客户端版本号（优先国服，其次国际服）。
func (r *ResolvedPaths) PrimaryVersion() string {
	return r.VersionForClient(false)
}

// VersionForClient 返回首选客户端的版本号；若指定客户端未配置则自动回退。
func (r *ResolvedPaths) VersionForClient(preferGlobal bool) string {
	if preferGlobal {
		if r.HasGlobal() {
			return r.Global.Version
		}
		if r.HasCN() {
			return r.CN.Version
		}
		return "未知版本"
	}
	if r.HasCN() {
		return r.CN.Version
	}
	if r.HasGlobal() {
		return r.Global.Version
	}
	return "未知版本"
}

func normalizeConfigFields(cfg *AppConfig) {
	cfg.CNPath = strings.TrimSpace(cfg.CNPath)
	cfg.GlobalPath = strings.TrimSpace(cfg.GlobalPath)
	cfg.ExportOutputDir = strings.TrimSpace(cfg.ExportOutputDir)
	cfg.IconExportOutputDir = strings.TrimSpace(cfg.IconExportOutputDir)
	cfg.DiffExportOutputDir = strings.TrimSpace(cfg.DiffExportOutputDir)
	if lang, ok := i18n.ParseLang(cfg.Language); ok {
		cfg.Language = string(lang)
	} else {
		cfg.Language = ""
	}
}

// ConfigFilePath 返回指定数据目录下的配置文件路径。
func ConfigFilePath(dataDir string) string {
	return filepath.Join(dataDir, "config.json")
}

// LoadAppConfig 从应用数据目录读取用户配置；文件不存在时返回空配置。
func LoadAppConfig(dataDir string) (AppConfig, error) {
	var cfg AppConfig
	p := ConfigFilePath(dataDir)
	raw, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("读取配置文件失败: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("解析配置文件 JSON 失败: %w", err)
	}
	normalizeConfigFields(&cfg)
	return cfg, nil
}

// SaveAppConfig 将用户配置以 UTF-8 JSON 格式持久化到应用数据目录。
func SaveAppConfig(dataDir string, cfg AppConfig) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("创建应用配置目录失败: %w", err)
	}
	normalizeConfigFields(&cfg)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置文件失败: %w", err)
	}
	p := ConfigFilePath(dataDir)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

// ValidateClientPath 对单个 FF14 客户端目录执行严格只读校验并读取版本号。
// 兼容用户选择客户端根目录（包含 game/sqpack）或直接选择 game 目录（包含 sqpack）。
func ValidateClientPath(rawPath string, label string) (*ClientInfo, error) {
	cleaned := strings.TrimSpace(rawPath)
	if cleaned == "" {
		return nil, nil
	}
	cleaned = filepath.Clean(cleaned)

	// 兼容用户选择根目录或直接选择 game 子目录
	candidates := []struct {
		root string
		game string
	}{
		{root: cleaned, game: filepath.Join(cleaned, "game")},
		{root: filepath.Dir(cleaned), game: cleaned},
	}

	var matchedRoot, matchedGame string
	for _, cand := range candidates {
		sqpackDir := filepath.Join(cand.game, "sqpack")
		info, err := os.Stat(sqpackDir)
		if err == nil && info.IsDir() {
			// 只读尝试列出 sqpack 目录，确保当前账号有读取权限
			entries, readErr := os.ReadDir(sqpackDir)
			if readErr == nil && len(entries) > 0 {
				matchedRoot = cand.root
				matchedGame = cand.game
				break
			}
		}
	}

	if matchedGame == "" {
		return nil, fmt.Errorf("%s路径无效或无法只读访问 game/sqpack 数据目录: %q", label, cleaned)
	}

	version := "未知版本"
	verBytes, err := os.ReadFile(filepath.Join(matchedGame, "ffxivgame.ver"))
	if err == nil {
		if v := strings.TrimSpace(string(verBytes)); v != "" {
			version = v
		}
	}

	return &ClientInfo{
		RootPath: matchedRoot,
		GamePath: matchedGame,
		Version:  version,
	}, nil
}

// ValidateAndResolve 校验用户配置，要求国服与国际服路径至少配置一项且已填写项均有效。
func ValidateAndResolve(cfg AppConfig) (*ResolvedPaths, error) {
	cnRaw := strings.TrimSpace(cfg.CNPath)
	globalRaw := strings.TrimSpace(cfg.GlobalPath)

	if cnRaw == "" && globalRaw == "" {
		return nil, ErrNoClientConfigured
	}

	res := &ResolvedPaths{}
	if cnRaw != "" {
		info, err := ValidateClientPath(cnRaw, "国服客户端")
		if err != nil {
			return nil, err
		}
		res.CN = info
	}
	if globalRaw != "" {
		info, err := ValidateClientPath(globalRaw, "国际服客户端")
		if err != nil {
			return nil, err
		}
		res.Global = info
	}

	if !res.HasCN() && !res.HasGlobal() {
		return nil, ErrNoClientConfigured
	}
	return res, nil
}

// LoadAppConfigFromEnv 从 .env 文件或当前进程环境变量加载路径（供自动化测试与开发辅助使用）。
func LoadAppConfigFromEnv(envFilePath string) AppConfig {
	fileVars := map[string]string{}
	if envFilePath != "" {
		if raw, err := os.ReadFile(envFilePath); err == nil {
			content := strings.TrimPrefix(string(raw), "\uFEFF")
			for _, rawLine := range strings.Split(content, "\n") {
				line := strings.TrimSpace(rawLine)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				k, v, ok := strings.Cut(line, "=")
				if !ok {
					continue
				}
				k = strings.TrimSpace(k)
				v = strings.TrimSpace(v)
				if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
					v = v[1 : len(v)-1]
				}
				if k != "" {
					fileVars[k] = v
				}
			}
		}
	}

	cnPath := strings.TrimSpace(os.Getenv("FFXIV_CN_PATH"))
	if cnPath == "" {
		cnPath = strings.TrimSpace(fileVars["FFXIV_CN_PATH"])
	}
	globalPath := strings.TrimSpace(os.Getenv("FFXIV_GLOBAL_PATH"))
	if globalPath == "" {
		globalPath = strings.TrimSpace(fileVars["FFXIV_GLOBAL_PATH"])
	}

	return AppConfig{
		CNPath:     cnPath,
		GlobalPath: globalPath,
	}
}
