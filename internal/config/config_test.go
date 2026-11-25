package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndValidateConfig(t *testing.T) {
	tmp := t.TempDir()

	// 1. 首次启动无配置文件时返回空配置且校验返回 ErrNoClientConfigured
	cfg, err := LoadAppConfig(tmp)
	if err != nil {
		t.Fatalf("加载空配置不应报错: %v", err)
	}
	if _, err := ValidateAndResolve(cfg); !errors.Is(err, ErrNoClientConfigured) {
		t.Fatalf("期望返回 ErrNoClientConfigured，实际: %v", err)
	}

	// 2. 构建模拟只读目录（仅配置单国服）并验证自动识别根目录与 game 子目录
	cnRoot := filepath.Join(tmp, "ffxiv-cn")
	sqpackDir := filepath.Join(cnRoot, "game", "sqpack", "ffxiv")
	if err := os.MkdirAll(sqpackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cnRoot, "game", "ffxivgame.ver"), []byte("2026.11.01.0000.0000\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg.CNPath = cnRoot
	cfg.ExportOutputDir = filepath.Join(tmp, "out")
	if err := SaveAppConfig(tmp, cfg); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	reloaded, err := LoadAppConfig(tmp)
	if err != nil {
		t.Fatalf("重新加载配置失败: %v", err)
	}
	if reloaded.CNPath != cnRoot || reloaded.ExportOutputDir != cfg.ExportOutputDir {
		t.Fatalf("配置持久化不一致: %+v", reloaded)
	}

	resolved, err := ValidateAndResolve(reloaded)
	if err != nil {
		t.Fatalf("单国服校验应成功: %v", err)
	}
	if !resolved.HasCN() || resolved.HasGlobal() {
		t.Fatalf("期望仅国服有效: HasCN=%v HasGlobal=%v", resolved.HasCN(), resolved.HasGlobal())
	}
	if resolved.CN.Version != "2026.11.01.0000.0000" {
		t.Fatalf("版本号读取错误: %q", resolved.CN.Version)
	}

	// 3. 验证直接填写 game 子目录也能自动归一化
	resolvedFromGame, err := ValidateAndResolve(AppConfig{GlobalPath: filepath.Join(cnRoot, "game")})
	if err != nil {
		t.Fatalf("直接选择 game 目录也应校验通过: %v", err)
	}
	if !resolvedFromGame.HasGlobal() || resolvedFromGame.Global.RootPath != cnRoot {
		t.Fatalf("game 目录归一化错误: %+v", resolvedFromGame.Global)
	}
}

func TestConfigLanguagePersistenceAndNormalization(t *testing.T) {
	tmp := t.TempDir()

	// 1. 保存并加载各类语言标记，验证持久化与规范化一致
	cases := []struct {
		in   string
		want string
	}{
		{in: "zh-CN", want: "zh"},
		{in: "ja-JP", want: "ja"},
		{in: "EN", want: "en"},
		{in: "fr-FR", want: ""},
	}
	for _, tc := range cases {
		if err := SaveAppConfig(tmp, AppConfig{Language: tc.in}); err != nil {
			t.Fatalf("SaveAppConfig(%q) 失败: %v", tc.in, err)
		}
		got, err := LoadAppConfig(tmp)
		if err != nil {
			t.Fatalf("LoadAppConfig 失败: %v", err)
		}
		if got.Language != tc.want {
			t.Fatalf("Language(%q) 归一化结果 = %q, want %q", tc.in, got.Language, tc.want)
		}
	}
}
