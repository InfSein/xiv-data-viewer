package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Lang 表示受支持的应用界面语言代码。
type Lang string

const (
	LangZH Lang = "zh"
	LangJA Lang = "ja"
	LangEN Lang = "en"
)

// LangOption 描述语言选择器的元数据。
type LangOption struct {
	Code       Lang
	Autonym    string // 该语言自身的原生名称，便于任何语言状态下快速识别切换
	BCP47Tag   string // 传给 MyGo 原生控件的 BCP 47 标签
	ExportHint string
}

// SupportedLangs 定义应用支持的三种语言（中文、日语、英语）。
var SupportedLangs = []LangOption{
	{Code: LangZH, Autonym: "简体中文", BCP47Tag: "zh-Hans"},
	{Code: LangJA, Autonym: "日本語", BCP47Tag: "ja"},
	{Code: LangEN, Autonym: "English", BCP47Tag: "en"},
}

//go:embed locales/*.json
var localeFiles embed.FS

var (
	catalogOnce sync.Once
	catalogs    map[Lang]map[string]string
	catalogErr  error
)

func loadCatalogs() {
	catalogOnce.Do(func() {
		loaded := make(map[Lang]map[string]string, len(SupportedLangs))
		for _, opt := range SupportedLangs {
			path := fmt.Sprintf("locales/%s.json", opt.Code)
			raw, err := localeFiles.ReadFile(path)
			if err != nil {
				catalogErr = fmt.Errorf("read locale file %s: %w", path, err)
				return
			}
			var m map[string]string
			if err := json.Unmarshal(raw, &m); err != nil {
				catalogErr = fmt.Errorf("parse locale file %s: %w", path, err)
				return
			}
			loaded[opt.Code] = m
		}
		catalogs = loaded
	})
}

// ParseLang 将语言标签或配置字符串规范化为受支持的 Lang。
func ParseLang(tag string) (Lang, bool) {
	switch tag {
	case string(LangZH):
		return LangZH, true
	case string(LangJA):
		return LangJA, true
	case string(LangEN):
		return LangEN, true
	case "":
		return "", false
	}
	s := strings.TrimSpace(tag)
	if s == "" {
		return "", false
	}
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	s = strings.ToLower(strings.ReplaceAll(s, "_", "-"))
	if s == "" || s == "c" || s == "posix" {
		return "", false
	}
	primary, _, _ := strings.Cut(s, "-")
	switch primary {
	case "zh":
		return LangZH, true
	case "ja", "jp":
		return LangJA, true
	case "en":
		return LangEN, true
	default:
		return "", false
	}
}

// NormalizeLang 将任意语言字符串规范化为受支持语言，未匹配时回退至默认语言。
func NormalizeLang(tag string) Lang {
	if l, ok := ParseLang(tag); ok {
		return l
	}
	return LangEN
}

// MatchPreferredLanguage 按系统偏好语言顺序匹配受支持的语言，未命中时回退至英语。
func MatchPreferredLanguage(tags []string) Lang {
	for _, tag := range tags {
		if l, ok := ParseLang(tag); ok {
			return l
		}
	}
	return LangEN
}

// DetectSystemLanguage 检测操作系统当前设置的首选语言并映射为受支持的语言。
func DetectSystemLanguage() Lang {
	return MatchPreferredLanguage(detectSystemLocaleTags())
}

// OptionOf 返回指定语言对应的元数据信息。
func OptionOf(lang Lang) LangOption {
	norm := NormalizeLang(string(lang))
	for _, opt := range SupportedLangs {
		if opt.Code == norm {
			return opt
		}
	}
	return SupportedLangs[len(SupportedLangs)-1]
}

// BCP47Tag 返回对应语言供 MyGo 原生框架使用的 BCP 47 标签。
func BCP47Tag(lang Lang) string {
	return OptionOf(lang).BCP47Tag
}

// SecondaryDataLangs 返回当前界面语言下数据详情应展示的副语言代码列表：
// 中文 -> 日语 + 英语；日语 -> 仅英语；英语 -> 仅日语。
func SecondaryDataLangs(lang Lang) []Lang {
	switch NormalizeLang(string(lang)) {
	case LangZH:
		return []Lang{LangJA, LangEN}
	case LangJA:
		return []Lang{LangEN}
	default:
		return []Lang{LangJA}
	}
}

// T 获取指定语言下的静态翻译文本；缺失键时依次回退至英语、中文或键名本身。
func T(lang Lang, key string) string {
	loadCatalogs()
	norm := NormalizeLang(string(lang))
	if m, ok := catalogs[norm]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	if norm != LangEN {
		if m, ok := catalogs[LangEN]; ok {
			if v, ok := m[key]; ok {
				return v
			}
		}
	}
	if norm != LangZH {
		if m, ok := catalogs[LangZH]; ok {
			if v, ok := m[key]; ok {
				return v
			}
		}
	}
	return key
}

// Tf 获取带命名占位符替换（如 {name}）的本地化文本。
// pairs 按 key, value 成对传入，例如 Tf(lang, "item.count", "count", "10")。
func Tf(lang Lang, key string, pairs ...any) string {
	tmpl := T(lang, key)
	if len(pairs) < 2 {
		return tmpl
	}
	args := make([]string, 0, len(pairs))
	for i := 0; i+1 < len(pairs); i += 2 {
		k := fmt.Sprint(pairs[i])
		v := fmt.Sprint(pairs[i+1])
		args = append(args, "{"+k+"}", v)
	}
	return strings.NewReplacer(args...).Replace(tmpl)
}

func appendUniqueEnvTags(dst []string, raw string, splitColon bool) []string {
	if raw == "" {
		return dst
	}
	parts := []string{raw}
	if splitColon {
		parts = strings.Split(raw, ":")
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			dst = append(dst, p)
		}
	}
	return dst
}

func envLocaleTags() []string {
	var tags []string
	// 先检查明确指定消息区域的环境变量，兼容类 Unix 与测试覆盖场景
	tags = appendUniqueEnvTags(tags, envVar("LANGUAGE"), true)
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		tags = appendUniqueEnvTags(tags, envVar(key), false)
	}
	return tags
}
