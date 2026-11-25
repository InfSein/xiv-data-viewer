package changelog

import (
	"testing"
	"testing/fstest"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
)

func TestParseStructuredChangelogWithSubItemsAndInlineSpans(t *testing.T) {
	src := "\ufeff# 更新日志 (CHANGELOG)\r\n\r\n" +
		"本文件记录 **XIV Data Viewer** 的版本变更历史，遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 规范。\r\n\r\n" +
		"## [Unreleased]\r\n\r\n" +
		"### 新增\r\n" +
		"- **GitHub Release 应用内置自动更新**：新增 `internal/updater` 模块：\r\n" +
		"  - 菜单新增「检查更新...」入口；\r\n" +
		"  - 支持自动校验 `SHA256SUMS.txt` 并调用 `mygo.App.Relaunch` 重启。\r\n\r\n" +
		"### 修复\r\n" +
		"- **贴图白晕修复**：分离 `DecodeTexToNRGBA` 路径。\r\n\r\n" +
		"## [2.0.0] - 2026-11-24\r\n\r\n" +
		"### 新增\r\n" +
		"- 基于 `egoist/mygo` 的首个原生桌面版本。\r\n"

	doc := Parse(src)
	if doc.Title != "更新日志 (CHANGELOG)" {
		t.Fatalf("标题解析异常: %q", doc.Title)
	}
	if len(doc.IntroSpans) < 3 {
		t.Fatalf("引导段落内联片段解析异常: %+v", doc.IntroSpans)
	}
	hasLink := false
	for _, sp := range doc.IntroSpans {
		if sp.Kind == SpanLink && sp.Text == "Keep a Changelog" && sp.URL == "https://keepachangelog.com/zh-CN/1.1.0/" {
			hasLink = true
		}
	}
	if !hasLink {
		t.Fatalf("未解析出引导链接: %+v", doc.IntroSpans)
	}

	if len(doc.Releases) != 2 {
		t.Fatalf("期望 2 个版本节点，实际 %d", len(doc.Releases))
	}

	unrel := doc.Releases[0]
	if !unrel.IsUnreleased || unrel.Version != "Unreleased" || unrel.TotalItems != 2 {
		t.Fatalf("Unreleased 解析异常: %+v", unrel)
	}
	if len(unrel.Sections) != 2 || unrel.Sections[0].Kind != CategoryAdded || unrel.Sections[1].Kind != CategoryFixed {
		t.Fatalf("分类解析异常: %+v", unrel.Sections)
	}

	updaterItem := unrel.Sections[0].Items[0]
	if updaterItem.Title != "GitHub Release 应用内置自动更新" {
		t.Fatalf("标题提取异常: %q", updaterItem.Title)
	}
	if len(updaterItem.SubItems) != 2 {
		t.Fatalf("子条目解析异常: %+v", updaterItem.SubItems)
	}
	if updaterItem.SubItems[1].PlainText != "支持自动校验 SHA256SUMS.txt 并调用 mygo.App.Relaunch 重启。" {
		t.Fatalf("子条目纯文本拼接异常: %q", updaterItem.SubItems[1].PlainText)
	}

	v200 := doc.Releases[1]
	if v200.IsUnreleased || v200.Version != "2.0.0" || v200.Date != "2026-11-24" {
		t.Fatalf("2.0.0 版本元数据异常: %+v", v200)
	}
	if v200.Sections[0].Items[0].Title != "" || v200.Sections[0].Items[0].Body != "基于 egoist/mygo 的首个原生桌面版本。" {
		t.Fatalf("无加粗前缀条目解析异常: %+v", v200.Sections[0].Items[0])
	}
}

func TestClassifyCategoryAcrossLanguages(t *testing.T) {
	cases := []struct {
		heading string
		want    CategoryKind
	}{
		{"新增", CategoryAdded},
		{"Added", CategoryAdded},
		{"新規追加", CategoryAdded},
		{"优化与调整", CategoryChanged},
		{"Changed", CategoryChanged},
		{"改善・調整", CategoryChanged},
		{"修复", CategoryFixed},
		{"Fixed", CategoryFixed},
		{"修正", CategoryFixed},
		{"工程与测试", CategoryEngineering},
		{"Engineering & Tests", CategoryEngineering},
		{"開発・テスト", CategoryEngineering},
	}
	for _, tc := range cases {
		if got := ClassifyCategory(tc.heading); got != tc.want {
			t.Errorf("ClassifyCategory(%q) = %v, want %v", tc.heading, got, tc.want)
		}
	}
}

func TestLoadForLangAndEmptyUnreleasedFiltering(t *testing.T) {
	fsys := fstest.MapFS{
		"CHANGELOG.md": &fstest.MapFile{
			Data: []byte("# 更新日志\n\n## [Unreleased]\n\n## [2.1.0] - 2026-11-24\n\n### 新增\n- **新功能**：中文内容\n"),
		},
		"docs/CHANGELOG.en.md": &fstest.MapFile{
			Data: []byte("# Changelog\n\n## [Unreleased]\n\n## [2.1.0] - 2026-11-24\n\n### Added\n- **Feature**: English content\n"),
		},
		"docs/CHANGELOG.ja.md": &fstest.MapFile{
			Data: []byte("# 更新履歴\n\n## [Unreleased]\n\n## [2.1.0] - 2026-11-24\n\n### 新規追加\n- **機能**：日本語の内容\n"),
		},
	}

	for _, tc := range []struct {
		lang i18n.Lang
		want string
	}{
		{i18n.LangZH, "新功能"},
		{i18n.LangEN, "Feature"},
		{i18n.LangJA, "機能"},
	} {
		doc, err := LoadForLang(fsys, tc.lang)
		if err != nil {
			t.Fatalf("LoadForLang(%s) failed: %v", tc.lang, err)
		}
		nonEmpty := doc.NonEmptyReleases()
		if len(nonEmpty) != 1 || nonEmpty[0].Version != "2.1.0" {
			t.Fatalf("NonEmptyReleases(%s) 应过滤空的 Unreleased: %+v", tc.lang, nonEmpty)
		}
		if nonEmpty[0].Sections[0].Items[0].Title != tc.want {
			t.Fatalf("LoadForLang(%s) 内容不匹配: got=%q want=%q", tc.lang, nonEmpty[0].Sections[0].Items[0].Title, tc.want)
		}
	}
}
