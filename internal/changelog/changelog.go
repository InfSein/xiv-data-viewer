package changelog

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
)

// CategoryKind 表示 Keep a Changelog 条目的标准语义分类。
type CategoryKind uint8

const (
	CategoryOther CategoryKind = iota
	CategoryAdded
	CategoryChanged
	CategoryFixed
	CategoryEngineering
	CategoryRemoved
	CategoryDeprecated
	CategorySecurity
)

// InlineSpanKind 表示行内富文本片段的类型。
type InlineSpanKind uint8

const (
	SpanText InlineSpanKind = iota
	SpanBold
	SpanCode
	SpanLink
)

// InlineSpan 表示一段解析后的行内样式文本。
type InlineSpan struct {
	Kind InlineSpanKind
	Text string
	URL  string
}

// SubItem 表示变更条目下的二级子条目。
type SubItem struct {
	Spans     []InlineSpan
	PlainText string
}

// ChangeItem 表示单条结构化变更说明（含可选加粗标题、正文与子条目）。
type ChangeItem struct {
	Title      string
	TitleSpans []InlineSpan
	Body       string
	BodySpans  []InlineSpan
	SubItems   []SubItem
	PlainText  string
}

// Section 表示同一版本下的一个分类区块（如 新增 / 优化与调整 / 修复 / 工程与测试）。
type Section struct {
	Kind    CategoryKind
	Heading string
	Items   []ChangeItem
}

// Release 表示一个版本（或 Unreleased 开发中版本）的完整变更记录。
type Release struct {
	RawHeading   string
	Version      string
	Date         string
	IsUnreleased bool
	Sections     []Section
	TotalItems   int
}

// Document 表示整份解析后的更新日志文档。
type Document struct {
	Title      string
	Intro      string
	IntroSpans []InlineSpan
	Releases   []Release
}

// NonEmptyReleases 返回至少包含一条有效记录的版本列表（过滤空 Unreleased 占位区块）。
func (d Document) NonEmptyReleases() []Release {
	out := make([]Release, 0, len(d.Releases))
	for _, r := range d.Releases {
		if r.TotalItems > 0 {
			out = append(out, r)
		}
	}
	return out
}

// CandidatePaths 返回指定界面语言对应的更新日志文件路径候选顺序（缺失时回退中文源文件）。
func CandidatePaths(lang i18n.Lang) []string {
	switch i18n.NormalizeLang(string(lang)) {
	case i18n.LangEN:
		return []string{"docs/CHANGELOG.en.md", "CHANGELOG.md"}
	case i18n.LangJA:
		return []string{"docs/CHANGELOG.ja.md", "CHANGELOG.md"}
	default:
		return []string{"CHANGELOG.md"}
	}
}

// LoadForLang 从指定 fs.FS 中按当前界面语言读取并解析更新日志。
func LoadForLang(fsys fs.FS, lang i18n.Lang) (Document, error) {
	if fsys == nil {
		return Document{}, fmt.Errorf("changelog filesystem is nil")
	}
	var lastErr error
	for _, p := range CandidatePaths(lang) {
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			lastErr = err
			continue
		}
		return Parse(string(raw)), nil
	}
	if lastErr != nil {
		return Document{}, lastErr
	}
	return Document{}, fmt.Errorf("changelog file not found")
}

// Parse 将 Keep a Changelog 格式的 Markdown 文本解析为结构化 Document。
func Parse(markdown string) Document {
	s := strings.TrimPrefix(markdown, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")

	var doc Document
	var introLines []string

	var curRel *Release
	var curSec *Section
	var curItemLines []string
	var curSubLines [][]string

	flushSub := func() {
		// 子条目会在 flushItem 时统一汇总
	}
	_ = flushSub

	flushItem := func() {
		if curSec == nil {
			curItemLines = nil
			curSubLines = nil
			return
		}
		rawMain := strings.TrimSpace(strings.Join(curItemLines, " "))
		if rawMain != "" || len(curSubLines) > 0 {
			item := buildChangeItem(rawMain, curSubLines)
			curSec.Items = append(curSec.Items, item)
			if curRel != nil {
				curRel.TotalItems++
			}
		}
		curItemLines = nil
		curSubLines = nil
	}

	flushSection := func() {
		flushItem()
		if curRel != nil && curSec != nil && len(curSec.Items) > 0 {
			curRel.Sections = append(curRel.Sections, *curSec)
		}
		curSec = nil
	}

	flushRelease := func() {
		flushSection()
		if curRel != nil {
			doc.Releases = append(doc.Releases, *curRel)
		}
		curRel = nil
	}

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)

		// 一级标题：文档标题
		if curRel == nil && strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			if doc.Title == "" {
				doc.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			}
			continue
		}

		// 二级标题：版本节点
		if strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ") {
			flushRelease()
			heading := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			rel := parseReleaseHeading(heading)
			curRel = &rel
			continue
		}

		// 版本节点之前的引导段落
		if curRel == nil {
			if trimmed == "" || trimmed == "---" {
				continue
			}
			introLines = append(introLines, trimmed)
			continue
		}

		// 三级标题：变更分类
		if strings.HasPrefix(trimmed, "### ") && !strings.HasPrefix(trimmed, "#### ") {
			flushSection()
			heading := strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			curSec = &Section{
				Kind:    ClassifyCategory(heading),
				Heading: heading,
			}
			continue
		}

		if trimmed == "" || trimmed == "---" {
			continue
		}

		// 缩进二级子条目（如 "  - ..." 或 "\t- ..."）
		indent := countLeadingIndent(rawLine)
		if indent >= 2 && (strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ")) {
			if len(curItemLines) > 0 {
				subBody := strings.TrimSpace(trimmed[2:])
				if subBody != "" {
					curSubLines = append(curSubLines, []string{subBody})
				}
			}
			continue
		}

		// 一级列表条目（允许直接位于 ## 下，无 ### 时自动归入 CategoryOther）
		if indent < 2 && (strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ")) {
			if curSec == nil {
				curSec = &Section{Kind: CategoryOther}
			}
			flushItem()
			itemBody := strings.TrimSpace(trimmed[2:])
			if itemBody != "" {
				curItemLines = []string{itemBody}
			}
			continue
		}

		// 多行列表续行
		if len(curSubLines) > 0 && indent >= 4 {
			last := len(curSubLines) - 1
			curSubLines[last] = append(curSubLines[last], trimmed)
		} else if len(curItemLines) > 0 {
			curItemLines = append(curItemLines, trimmed)
		}
	}

	flushRelease()

	if len(introLines) > 0 {
		doc.Intro = strings.Join(introLines, " ")
		doc.IntroSpans = ParseInlineSpans(doc.Intro)
	}
	return doc
}

func countLeadingIndent(s string) int {
	n := 0
	for _, r := range s {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

func parseReleaseHeading(raw string) Release {
	rel := Release{RawHeading: raw}
	clean := strings.TrimSpace(raw)

	// 拆解如 "[2.1.0] - 2026-11-24" 或 "v2.1.0 (2026-11-24)"
	verPart := clean
	datePart := ""
	for _, sep := range []string{" - ", " — ", " – "} {
		if left, right, ok := strings.Cut(clean, sep); ok {
			verPart = strings.TrimSpace(left)
			datePart = strings.TrimSpace(right)
			break
		}
	}
	if datePart == "" {
		if idx := strings.LastIndex(verPart, "("); idx > 0 && strings.HasSuffix(verPart, ")") {
			datePart = strings.TrimSpace(verPart[idx+1 : len(verPart)-1])
			verPart = strings.TrimSpace(verPart[:idx])
		}
	}

	verPart = strings.TrimPrefix(verPart, "[")
	verPart = strings.TrimSuffix(verPart, "]")
	verPart = strings.TrimSpace(verPart)

	if strings.EqualFold(verPart, "unreleased") {
		rel.IsUnreleased = true
		rel.Version = "Unreleased"
		return rel
	}

	verPart = strings.TrimPrefix(verPart, "v")
	verPart = strings.TrimPrefix(verPart, "V")
	rel.Version = strings.TrimSpace(verPart)
	rel.Date = strings.TrimSpace(datePart)
	return rel
}

// ClassifyCategory 将中/英/日分类标题映射为统一的语义 CategoryKind。
func ClassifyCategory(heading string) CategoryKind {
	h := strings.ToLower(strings.TrimSpace(heading))
	switch {
	case h == "新增" || h == "新功能" || h == "added" || h == "new" || h == "features" || h == "新規追加" || h == "追加" || h == "新機能":
		return CategoryAdded
	case h == "优化与调整" || h == "优化" || h == "调整" || h == "变更" || h == "改进" ||
		h == "changed" || h == "improved" || h == "improvements" || h == "updated" ||
		h == "改善・調整" || h == "改善" || h == "変更" || h == "調整":
		return CategoryChanged
	case h == "修复" || h == "问题修复" || h == "fixed" || h == "fixes" || h == "bug fixes" ||
		h == "修正" || h == "不具合修正" || h == "バグ修正":
		return CategoryFixed
	case h == "工程与测试" || h == "工程" || h == "测试" || h == "构建与测试" ||
		h == "engineering" || h == "engineering & tests" || h == "tests" || h == "chore" || h == "build" || h == "ci" ||
		h == "開発・テスト" || h == "開発とテスト" || h == "エンジニアリング・テスト" || h == "テスト":
		return CategoryEngineering
	case h == "移除" || h == "删除" || h == "removed" || h == "削除":
		return CategoryRemoved
	case h == "弃用" || h == "废弃" || h == "deprecated" || h == "非推奨":
		return CategoryDeprecated
	case h == "安全" || h == "security" || h == "セキュリティ":
		return CategorySecurity
	default:
		return CategoryOther
	}
}

func buildChangeItem(rawMain string, rawSubs [][]string) ChangeItem {
	var item ChangeItem
	var plainParts []string

	titleRaw, bodyRaw := splitLeadTitle(rawMain)
	if titleRaw != "" {
		item.TitleSpans = ParseInlineSpans(titleRaw)
		item.Title = spansPlainText(item.TitleSpans)
		plainParts = append(plainParts, item.Title)
	}
	if bodyRaw != "" {
		item.BodySpans = ParseInlineSpans(bodyRaw)
		item.Body = spansPlainText(item.BodySpans)
		plainParts = append(plainParts, item.Body)
	}

	for _, sub := range rawSubs {
		subText := strings.TrimSpace(strings.Join(sub, " "))
		if subText == "" {
			continue
		}
		spans := ParseInlineSpans(subText)
		plain := spansPlainText(spans)
		item.SubItems = append(item.SubItems, SubItem{
			Spans:     spans,
			PlainText: plain,
		})
		plainParts = append(plainParts, plain)
	}

	item.PlainText = strings.Join(plainParts, " ")
	return item
}

// splitLeadTitle 识别 "**标题**：正文" 或 "**Title**: Body" 结构并返回拆分结果。
func splitLeadTitle(raw string) (string, string) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "**") {
		return "", s
	}
	closeIdx := strings.Index(s[2:], "**")
	if closeIdx <= 0 {
		return "", s
	}
	end := 2 + closeIdx
	inner := strings.TrimSpace(s[2:end])
	after := strings.TrimSpace(s[end+2:])

	// 兼容 "**标题：**正文" 或 "**标题**：正文"
	hadInnerColon := strings.HasSuffix(inner, "：") || strings.HasSuffix(inner, ":")
	inner = strings.TrimSpace(strings.TrimRight(inner, ":："))

	if after == "" {
		return "", s
	}

	hasAfterColon := strings.HasPrefix(after, "：") || strings.HasPrefix(after, ":")
	if !hadInnerColon && !hasAfterColon {
		return "", s
	}

	if hasAfterColon {
		after = strings.TrimLeft(after, ":：")
		after = strings.TrimSpace(after)
	}
	if inner == "" || after == "" {
		return "", s
	}
	return inner, after
}

// ParseInlineSpans 将包含行内代码 (`code`)、加粗 (**bold**) 与链接 ([text](url)) 的文本解析为片段列表。
func ParseInlineSpans(s string) []InlineSpan {
	if s == "" {
		return nil
	}
	var out []InlineSpan
	pushText := func(txt string) {
		if txt == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Kind == SpanText {
			out[n-1].Text += txt
			return
		}
		out = append(out, InlineSpan{Kind: SpanText, Text: txt})
	}

	i := 0
	for i < len(s) {
		switch {
		case s[i] == '`':
			end := strings.IndexByte(s[i+1:], '`')
			if end > 0 {
				code := s[i+1 : i+1+end]
				out = append(out, InlineSpan{Kind: SpanCode, Text: code})
				i += 1 + end + 1
				continue
			}
		case strings.HasPrefix(s[i:], "**"):
			end := strings.Index(s[i+2:], "**")
			if end > 0 {
				bold := s[i+2 : i+2+end]
				out = append(out, InlineSpan{Kind: SpanBold, Text: bold})
				i += 2 + end + 2
				continue
			}
		case s[i] == '[':
			if label, url, nextIdx, ok := parseMarkdownLink(s, i); ok {
				out = append(out, InlineSpan{Kind: SpanLink, Text: label, URL: url})
				i = nextIdx
				continue
			}
		}

		// 消费直到下一个潜在内联标记起始位置，支持多字节 UTF-8 字符
		next := nextMarkupIndex(s, i+1)
		pushText(s[i:next])
		i = next
	}
	return out
}

func nextMarkupIndex(s string, start int) int {
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '`', '[':
			return i
		case '*':
			if strings.HasPrefix(s[i:], "**") {
				return i
			}
		}
	}
	return len(s)
}

func parseMarkdownLink(s string, start int) (label, url string, nextIdx int, ok bool) {
	closeBracket := strings.IndexByte(s[start+1:], ']')
	if closeBracket <= 0 {
		return "", "", 0, false
	}
	mid := start + 1 + closeBracket
	if mid+1 >= len(s) || s[mid+1] != '(' {
		return "", "", 0, false
	}
	closeParen := strings.IndexByte(s[mid+2:], ')')
	if closeParen <= 0 {
		return "", "", 0, false
	}
	label = s[start+1 : mid]
	url = strings.TrimSpace(s[mid+2 : mid+2+closeParen])
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return "", "", 0, false
	}
	return label, url, mid + 2 + closeParen + 1, true
}

func spansPlainText(spans []InlineSpan) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	return b.String()
}
