package gamedata

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

func TestParseTextSearchQueryModes(t *testing.T) {
	pFuzzy := ParseTextSearchQuery("AD")
	if pFuzzy.Empty || pFuzzy.Mode != TextMatchFuzzy {
		t.Fatalf("普通输入应解析为模糊模式: %+v", pFuzzy)
	}
	if string(pFuzzy.FuzzyRunes) != "ad" {
		t.Fatalf("模糊字符序列应折叠小写为 ad，实际: %q", string(pFuzzy.FuzzyRunes))
	}

	pExact := ParseTextSearchQuery(`"AB"`)
	if pExact.Empty || pExact.Mode != TextMatchExact {
		t.Fatalf("英文双引号包裹应解析为精确模式: %+v", pExact)
	}
	if len(pExact.ExactTerms) != 1 || pExact.ExactTerms[0] != "ab" {
		t.Fatalf("精确词解析错误: %+v", pExact.ExactTerms)
	}

	pExactSpace := ParseTextSearchQuery(`"A B"`)
	if pExactSpace.Empty || pExactSpace.Mode != TextMatchExact || pExactSpace.ExactTerms[0] != "a b" {
		t.Fatalf("精确模式应保留引号内部空格: %+v", pExactSpace)
	}

	pMultiExact := ParseTextSearchQuery(`"光之" "战士"`)
	if pMultiExact.Empty || pMultiExact.Mode != TextMatchExact || len(pMultiExact.ExactTerms) != 2 ||
		pMultiExact.ExactTerms[0] != "光之" || pMultiExact.ExactTerms[1] != "战士" {
		t.Fatalf("多组双引号精确词解析异常: %+v", pMultiExact)
	}

	if !ParseTextSearchQuery(`""`).Empty {
		t.Fatalf("空双引号应视为空查询")
	}
	if !ParseTextSearchQuery("   ").Empty {
		t.Fatalf("纯空白应视为空查询")
	}
}

func TestTextSearchFuzzyVsExactAndRanking(t *testing.T) {
	sheets := []TextIndexSheetInput{
		{Name: "Item", IsLocalized: true, ColNames: []string{"Name", "Description"}},
		{Name: "quest/000/ClsArc100_00001", IsLocalized: true, ColNames: []string{"Key", "Text"}},
		{Name: "Addon", IsLocalized: true, ColNames: []string{"Text"}},
	}
	rows := []TextIndexRowInput{
		{SheetIdx: 0, RowID: 1, SubRowID: -1, ColIdx: 0, Text: "ABCD"},
		{SheetIdx: 0, RowID: 2, SubRowID: -1, ColIdx: 0, Text: "AD"},
		{SheetIdx: 0, RowID: 3, SubRowID: -1, ColIdx: 0, Text: "AD护手"},
		{SheetIdx: 0, RowID: 4, SubRowID: -1, ColIdx: 0, Text: "极品AD药水"},
		{SheetIdx: 0, RowID: 5, SubRowID: -1, ColIdx: 0, Text: "AxxxxxxD"},
		{SheetIdx: 0, RowID: 6, SubRowID: -1, ColIdx: 0, Text: "AxxxxxxxxxxABCD"},
		{SheetIdx: 0, RowID: 7, SubRowID: -1, ColIdx: 0, Text: "DCBA"},
		{SheetIdx: 1, RowID: 10, SubRowID: -1, ColIdx: 0, Text: "TEXT_AD_INTERNAL_KEY"},
		{SheetIdx: 1, RowID: 10, SubRowID: -1, ColIdx: 1, Text: "光之战士踏上冒险旅程"},
		{SheetIdx: 2, RowID: 100, SubRowID: -1, ColIdx: 0, Text: "光之战士"},
		{SheetIdx: 2, RowID: 101, SubRowID: -1, ColIdx: 0, Text: "欢迎来到光之与暗之战士殿堂"},
		{SheetIdx: 0, RowID: 8, SubRowID: 2, ColIdx: 1, Text: "ＡＢＣＤ全角字符测试"},
	}

	idx := NewTextSearchIndexFromRecords("chs", sheets, rows, nil, nil)

	// 1. 模糊检索 AD：应命中 AD、AD护手、极品AD药水、ABCD、AxxxxxxD、AxxxxxxxxxxABCD、TEXT_AD...、全角ＡＢＣＤ，不命中 DCBA
	resFuzzy, err := idx.Search(context.Background(), TextSearchQuery{
		Q:        "AD",
		Page:     1,
		PageSize: 20,
	}, nil)
	if err != nil {
		t.Fatalf("模糊检索失败: %v", err)
	}
	if resFuzzy.Mode != TextMatchFuzzy {
		t.Fatalf("期望模糊模式，实际: %v", resFuzzy.Mode)
	}

	posByText := make(map[string]int, len(resFuzzy.Items))
	for i, it := range resFuzzy.Items {
		posByText[it.Text] = i
	}
	for _, mustHit := range []string{"AD", "AD护手", "极品AD药水", "ABCD", "AxxxxxxD", "AxxxxxxxxxxABCD", "ＡＢＣＤ全角字符测试"} {
		if _, ok := posByText[mustHit]; !ok {
			t.Fatalf("模糊检索 AD 应命中 %q，实际命中: %v", mustHit, posByText)
		}
	}
	if _, ok := posByText["DCBA"]; ok {
		t.Fatalf("模糊检索 AD 不应命中逆序 DCBA")
	}

	// 验证匹配程度排序：完全匹配 > 连贯前缀 > 连贯包含 > 紧凑子序列(ABCD) > 松散子序列(AxxxxxxD)
	if !(posByText["AD"] < posByText["AD护手"] &&
		posByText["AD护手"] < posByText["极品AD药水"] &&
		posByText["极品AD药水"] < posByText["ABCD"] &&
		posByText["ABCD"] < posByText["AxxxxxxD"]) {
		t.Fatalf("模糊检索排序不符合匹配程度预期: %v", posByText)
	}

	// 验证 "ABCD" 搜 AD 的高亮区间准确指向 A [0,1) 与 D [3,4)
	for _, it := range resFuzzy.Items {
		if it.Text == "ABCD" {
			if len(it.MatchSpans) != 2 || it.MatchSpans[0] != (TextMatchSpan{0, 1}) || it.MatchSpans[1] != (TextMatchSpan{3, 4}) {
				t.Fatalf("ABCD 高亮区间异常: %+v", it.MatchSpans)
			}
		}
		if it.Text == "AxxxxxxxxxxABCD" {
			// 应锁定末尾紧凑的 ABCD 中的 A 和 D，而非开头孤立的 A
			if len(it.MatchSpans) != 2 || it.MatchSpans[0].Start != 11 || it.MatchSpans[1].Start != 14 {
				t.Fatalf("AxxxxxxxxxxABCD 应锁定末尾更紧凑窗口，实际高亮: %+v", it.MatchSpans)
			}
		}
	}

	// 2. 精确检索 "AD"：必须连贯出现 AD，不能命中 ABCD / AxxxxxxD
	resExactAD, err := idx.Search(context.Background(), TextSearchQuery{
		Q:        `"AD"`,
		Page:     1,
		PageSize: 20,
	}, nil)
	if err != nil {
		t.Fatalf("精确检索 \"AD\" 失败: %v", err)
	}
	if resExactAD.Mode != TextMatchExact {
		t.Fatalf("期望精确模式，实际: %v", resExactAD.Mode)
	}
	exactTexts := make(map[string]bool, len(resExactAD.Items))
	for _, it := range resExactAD.Items {
		exactTexts[it.Text] = true
	}
	if !exactTexts["AD"] || !exactTexts["AD护手"] || !exactTexts["极品AD药水"] {
		t.Fatalf("精确检索 \"AD\" 应命中连贯包含 AD 的文本: %v", exactTexts)
	}
	if exactTexts["ABCD"] || exactTexts["AxxxxxxD"] || exactTexts["AxxxxxxxxxxABCD"] {
		t.Fatalf("精确检索 \"AD\" 不应命中非连贯的 ABCD / AxxxxxxD: %v", exactTexts)
	}

	// 3. 精确检索 "AB"：应命中 ABCD、AxxxxxxxxxxABCD、全角ＡＢＣＤ，不命中 AD
	resExactAB, err := idx.Search(context.Background(), TextSearchQuery{
		Q:        `"AB"`,
		Page:     1,
		PageSize: 20,
	}, nil)
	if err != nil {
		t.Fatalf("精确检索 \"AB\" 失败: %v", err)
	}
	if resExactAB.Total != 3 {
		t.Fatalf("精确检索 \"AB\" 期望命中 3 条，实际 %d: %+v", resExactAB.Total, resExactAB.Items)
	}
	if resExactAB.Items[0].Text != "ABCD" {
		t.Fatalf("精确检索 \"AB\" 首条应为前缀命中的 ABCD，实际: %q", resExactAB.Items[0].Text)
	}

	// 4. 分页与范围/表筛选测试
	p1, err := idx.Search(context.Background(), TextSearchQuery{Q: "光战", Page: 1, PageSize: 2}, nil)
	if err != nil {
		t.Fatalf("中文模糊检索失败: %v", err)
	}
	if p1.Total != 3 || p1.TotalPages != 2 || len(p1.Items) != 2 {
		t.Fatalf("分页结果异常: total=%d pages=%d len=%d", p1.Total, p1.TotalPages, len(p1.Items))
	}
	if p1.Items[0].Text != "光之战士" {
		t.Fatalf("搜「光战」最紧凑的「光之战士」应排第一，实际: %q", p1.Items[0].Text)
	}
	p2, err := idx.Search(context.Background(), TextSearchQuery{Q: "光战", Page: 2, PageSize: 2}, nil)
	if err != nil || p2.Page != 2 || len(p2.Items) != 1 {
		t.Fatalf("第 2 页结果异常: %+v err=%v", p2, err)
	}

	// 仅筛选剧情对话范围
	storyOnly, err := idx.Search(context.Background(), TextSearchQuery{
		Q:        "光战",
		Scope:    TextScopeStory,
		Page:     1,
		PageSize: 20,
	}, nil)
	if err != nil || storyOnly.Total != 1 || storyOnly.Items[0].SheetName != "quest/000/ClsArc100_00001" {
		t.Fatalf("范围筛选 Story 异常: %+v err=%v", storyOnly, err)
	}
}

func TestTextSearchRealClientPerformance(t *testing.T) {
	envCfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(envCfg)
	if err != nil || !resolved.HasCN() {
		t.Skip("本机未配置国服客户端，跳过真实全量文本检索性能测试")
	}

	reader := sqpack.NewGameReader(resolved.CN.GamePath)
	defer reader.Close()

	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	t0 := time.Now()
	scanRes, err := sqpack.ScanAllEXDStrings(context.Background(), reader, sqpack.EXDTextScanOptions{
		PreferredLang: sqpack.LangChineseSimplified,
	})
	if err != nil {
		t.Fatalf("扫描全量 EXD 文本失败: %v", err)
	}
	scanDur := time.Since(t0)

	t1 := time.Now()
	schemasFS := os.DirFS(filepath.Join("..", "..", "schemas"))
	idx, err := BuildTextSearchIndexFromScan(context.Background(), "chs", scanRes, schemasFS, reader, t0)
	if err != nil {
		t.Fatalf("构建全文索引失败: %v", err)
	}
	indexDur := time.Since(t1)
	scanRes = nil
	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	st := idx.Stats()
	heapMB := float64(int64(memAfter.HeapAlloc)-int64(memBefore.HeapAlloc)) / (1024 * 1024)
	invCount := len(idx.invExtSpans)
	highCount := len(idx.invExtHigh)
	for _, sp := range idx.invBMP {
		if sp.off == invHighFreqSentinel {
			highCount++
		} else if sp.count > 0 {
			invCount++
		}
	}
	t.Logf("全量 EXD 索引统计: sheets=%d texts=%d scan=%v index=%v total=%v heapDelta=%.1fMB invRunes=%d highFreq=%d postings=%d",
		st.TotalSheets, st.TotalTexts, scanDur, indexDur, scanDur+indexDur, heapMB,
		invCount, highCount, len(idx.invPostings))

	if st.TotalSheets < 1000 || st.TotalTexts < 100000 {
		t.Fatalf("全量文本数量过少，疑似扫描不完整: sheets=%d texts=%d", st.TotalSheets, st.TotalTexts)
	}

	queries := []string{
		"光之战士",
		`"光之战士"`,
		"光战",
		"AD",
		`"AD"`,
		"的",
		"拂晓血盟",
		"艾欧泽亚",
		"绝对不存在的生僻词XYZ999",
	}
	for _, q := range queries {
		res, err := idx.Search(context.Background(), TextSearchQuery{
			Q:        q,
			Page:     1,
			PageSize: 50,
		}, nil)
		if err != nil {
			t.Fatalf("检索 %q 失败: %v", q, err)
		}
		topSample := ""
		if len(res.Items) > 0 {
			topSample = res.Items[0].SheetName + " " + FormatTextRowKey(res.Items[0].RowID, res.Items[0].SubRowID) + " [" + res.Items[0].ColName + "] " + truncateForLog(res.Items[0].Text, 24)
		}
		// 再测一次翻页缓存命中速度
		resP2, _ := idx.Search(context.Background(), TextSearchQuery{
			Q:        q,
			Page:     2,
			PageSize: 50,
		}, nil)
		t.Logf("query=%-22s mode=%-5s total=%-7d p1=%.2fms p2=%.2fms top=%s",
			q, res.Mode, res.Total, res.ElapsedMs, resP2.ElapsedMs, topSample)
	}
}

func truncateForLog(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "..."
}

func TestTextSearchParallelTranslations(t *testing.T) {
	store := &Store{
		HasCN:       true,
		HasGlobal:   true,
		PrimaryLang: sqpack.LangChineseSimplified,
	}
	svc := NewService(store)

	idxCN := NewTextSearchIndexFromRecords("chs",
		[]TextIndexSheetInput{
			{Name: "Item", IsLocalized: true, ColNames: []string{"Name"}},
			{Name: "BGM", IsLocalized: false, ColNames: []string{"File"}},
		},
		[]TextIndexRowInput{
			{SheetIdx: 0, RowID: 100, SubRowID: -1, ColIdx: 0, Text: "光之战士之剑"},
			{SheetIdx: 0, RowID: 101, SubRowID: -1, ColIdx: 0, Text: "国服独占光之饰品"},
			{SheetIdx: 1, RowID: 1, SubRowID: -1, ColIdx: 0, Text: "music/ffxiv/light_theme.scd"},
		},
		nil, nil,
	)
	idxJA := NewTextSearchIndexFromRecords("ja",
		[]TextIndexSheetInput{{Name: "Item", IsLocalized: true, ColNames: []string{"Name"}}},
		[]TextIndexRowInput{{SheetIdx: 0, RowID: 100, SubRowID: -1, ColIdx: 0, Text: "光の戦士の剣"}},
		nil, nil,
	)
	idxEN := NewTextSearchIndexFromRecords("en",
		[]TextIndexSheetInput{{Name: "Item", IsLocalized: true, ColNames: []string{"Name"}}},
		[]TextIndexRowInput{{SheetIdx: 0, RowID: 100, SubRowID: -1, ColIdx: 0, Text: "Sword of the Warrior of Light"}},
		nil, nil,
	)
	svc.SetTextSearchIndex("chs", idxCN)
	svc.SetTextSearchIndex("ja", idxJA)
	svc.SetTextSearchIndex("en", idxEN)

	res, err := svc.SearchTexts(context.Background(), TextSearchQuery{
		LangCode: "chs",
		Q:        "光之",
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("多语言对照检索失败: %v", err)
	}
	if len(res.CompareLangs) != 2 || res.CompareLangs[0] != "ja" || res.CompareLangs[1] != "en" {
		t.Fatalf("期望中文检索对照语言为 [ja en]，实际: %v", res.CompareLangs)
	}
	if len(res.Items) != 2 {
		t.Fatalf("期望命中 2 条，实际: %d", len(res.Items))
	}

	var hit100, hit101 *TextSearchHit
	for i := range res.Items {
		if res.Items[i].RowID == 100 {
			hit100 = &res.Items[i]
		} else if res.Items[i].RowID == 101 {
			hit101 = &res.Items[i]
		}
	}
	if hit100 == nil || len(hit100.Translations) != 2 ||
		hit100.Translations[0].Badge != "JA" || hit100.Translations[0].Text != "光の戦士の剣" ||
		hit100.Translations[1].Badge != "EN" || hit100.Translations[1].Text != "Sword of the Warrior of Light" {
		t.Fatalf("物品 #100 多语言对照缺失或错误: %+v", hit100)
	}
	if hit101 == nil || len(hit101.Translations) != 0 {
		t.Fatalf("无国际服数据的条目不应伪造对照译文: %+v", hit101)
	}

	// 若本机双服客户端齐全，实测真实国服/国际服跨服多语言对照点查
	envCfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(envCfg)
	if err != nil || !resolved.HasCN() || !resolved.HasGlobal() {
		return
	}
	cnR := sqpack.NewGameReader(resolved.CN.GamePath)
	defer cnR.Close()
	glR := sqpack.NewGameReader(resolved.Global.GamePath)
	defer glR.Close()
	schemasFS := os.DirFS(filepath.Join("..", "..", "schemas"))

	realStore := &Store{
		CNReader:      cnR,
		GlobalReader:  glR,
		PrimaryReader: cnR,
		SchemasFS:     schemasFS,
		HasCN:         true,
		HasGlobal:     true,
		PrimaryLang:   sqpack.LangChineseSimplified,
	}
	realSvc := NewService(realStore)
	if _, err := realSvc.BuildTextSearchIndex(context.Background(), "chs", nil); err != nil {
		t.Fatalf("构建国服全文索引失败: %v", err)
	}
	tStart := time.Now()
	realRes, err := realSvc.SearchTexts(context.Background(), TextSearchQuery{
		LangCode: "chs",
		Q:        `"光之战士"`,
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("双服对照检索失败: %v", err)
	}
	withTrans := 0
	for _, it := range realRes.Items {
		if len(it.Translations) > 0 {
			withTrans++
		}
	}
	t.Logf("双服真实对照检索 \"光之战士\": hits=%d withTranslations=%d/%d cost=%v",
		realRes.Total, withTrans, len(realRes.Items), time.Since(tStart))
	if withTrans == 0 {
		t.Fatalf("双服模式下应检索到真实日/英多语言对照译文")
	}
}
