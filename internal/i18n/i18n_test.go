package i18n

import (
	"regexp"
	"slices"
	"sort"
	"testing"
)

var placeholderRe = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

func extractPlaceholders(s string) []string {
	matches := placeholderRe.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	var out []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

func TestCatalogCompletenessAndPlaceholders(t *testing.T) {
	loadCatalogs()
	if catalogErr != nil {
		t.Fatalf("加载语言包失败: %v", catalogErr)
	}
	zh := catalogs[LangZH]
	if len(zh) == 0 {
		t.Fatalf("中文语言包不能为空")
	}

	for _, lang := range []Lang{LangJA, LangEN} {
		other := catalogs[lang]
		if len(other) == 0 {
			t.Fatalf("语言包 %s 为空", lang)
		}
		for k, zhVal := range zh {
			val, ok := other[k]
			if !ok {
				t.Errorf("语言包 %s 缺少键 %q", lang, k)
				continue
			}
			if val == "" {
				t.Errorf("语言包 %s 键 %q 的值为空", lang, k)
			}
			zhPH := extractPlaceholders(zhVal)
			otherPH := extractPlaceholders(val)
			if !slices.Equal(zhPH, otherPH) {
				t.Errorf("语言包 %s 键 %q 占位符不一致: zh=%v %s=%v", lang, k, zhPH, lang, otherPH)
			}
		}
		for k := range other {
			if _, ok := zh[k]; !ok {
				t.Errorf("语言包 %s 包含多余键 %q", lang, k)
			}
		}
	}
}

func TestParseAndMatchPreferredLanguage(t *testing.T) {
	cases := []struct {
		in   string
		want Lang
		ok   bool
	}{
		{"zh", LangZH, true},
		{"zh-CN", LangZH, true},
		{"zh_CN.UTF-8", LangZH, true},
		{"zh-Hans-CN", LangZH, true},
		{"zh-Hant-TW", LangZH, true},
		{"ja", LangJA, true},
		{"ja-JP", LangJA, true},
		{"ja_JP.UTF-8", LangJA, true},
		{"en", LangEN, true},
		{"en-US", LangEN, true},
		{"en_GB.UTF-8", LangEN, true},
		{"C.UTF-8", "", false},
		{"POSIX", "", false},
		{"fr-FR", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseLang(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseLang(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}

	if got := MatchPreferredLanguage([]string{"fr-FR", "ja-JP", "en-US"}); got != LangJA {
		t.Errorf("MatchPreferredLanguage 应优先命中首个受支持语言 ja，实际: %q", got)
	}
	if got := MatchPreferredLanguage([]string{"zh_CN.UTF-8", "en-US"}); got != LangZH {
		t.Errorf("MatchPreferredLanguage 应命中 zh，实际: %q", got)
	}
	if got := MatchPreferredLanguage([]string{"de-DE", "C"}); got != LangEN {
		t.Errorf("未命中受支持语言时应回退至 en，实际: %q", got)
	}
}

func TestSecondaryDataLangsRule(t *testing.T) {
	if got := SecondaryDataLangs(LangZH); !slices.Equal(got, []Lang{LangJA, LangEN}) {
		t.Errorf("中文副语言应为日+英，实际: %v", got)
	}
	if got := SecondaryDataLangs(LangJA); !slices.Equal(got, []Lang{LangEN}) {
		t.Errorf("日语副语言应仅为英语，实际: %v", got)
	}
	if got := SecondaryDataLangs(LangEN); !slices.Equal(got, []Lang{LangJA}) {
		t.Errorf("英语副语言应仅为日语，实际: %v", got)
	}
}

func TestTfPlaceholderReplacement(t *testing.T) {
	got := Tf(LangEN, "export.subtitle", "sheets", "100", "tasks", "300")
	want := "100 sheets · 300 CSV files to export"
	if got != want {
		t.Fatalf("Tf 替换异常: got=%q want=%q", got, want)
	}
}

func TestDetectSystemLanguageDoesNotPanic(t *testing.T) {
	got := DetectSystemLanguage()
	if got != LangZH && got != LangJA && got != LangEN {
		t.Fatalf("DetectSystemLanguage 返回了未知语言: %q", got)
	}
}
