package gamedata

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io/fs"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

// TextMatchMode 表示文本检索的匹配模式。
type TextMatchMode string

const (
	// TextMatchFuzzy 按字符顺序进行子序列模糊匹配（如输入 AD 可匹配 ABCD）。
	TextMatchFuzzy TextMatchMode = "fuzzy"
	// TextMatchExact 以英文双引号包裹时进行连贯子串精确匹配（如输入 "AB" 必须连续出现 AB）。
	TextMatchExact TextMatchMode = "exact"
)

// TextSearchScope 表示文本检索的数据表分类筛选范围。
type TextSearchScope string

const (
	TextScopeAll   TextSearchScope = "all"
	TextScopeCore  TextSearchScope = "core"
	TextScopeStory TextSearchScope = "story"
	TextScopeUI    TextSearchScope = "ui"
	TextScopeOther TextSearchScope = "other"
)

// TextMatchSpan 描述命中字符在原始展示文本中的 UTF-8 字节区间 [Start, End)。
type TextMatchSpan struct {
	Start int
	End   int
}

// TextSearchLangOption 描述可在文本检索页选择的客户端数据语言。
type TextSearchLangOption struct {
	Code  string
	Label string
	Lang  sqpack.Language
}

// TextIndexProgress 上报全量文本索引的异步构建进度。
type TextIndexProgress struct {
	LangCode     string
	Phase        string // "scanning" | "indexing" | "ready"
	DoneSheets   int
	TotalSheets  int
	IndexedTexts int
}

// TextIndexStats 描述已就绪全量文本索引的统计概览。
type TextIndexStats struct {
	LangCode    string
	TotalSheets int
	TotalTexts  int
	BuildTimeMs int64
}

// TextSearchQuery 定义文本检索页面的查询条件与分页参数。
type TextSearchQuery struct {
	LangCode    string
	Q           string
	Scope       TextSearchScope
	SheetFilter string // 可选：仅限定指定数据表名（精确匹配，忽略大小写）
	Page        int
	PageSize    int
}

// TextSheetFacet 汇总当前检索结果中各数据表的命中数量（供用户一键按表缩小范围）。
type TextSheetFacet struct {
	SheetName string
	Scope     TextSearchScope
	Count     int
}

// TextTranslation 表示单条文本在另一种客户端语言下的对应译文。
type TextTranslation struct {
	LangCode string // "chs" | "ja" | "en" | "de" | "fr"
	Badge    string // "ZH" | "JA" | "EN" | "DE" | "FR"
	Text     string
}

// TextSearchHit 表示文本检索的单条命中记录。
type TextSearchHit struct {
	DocID        uint32
	SheetName    string
	Scope        TextSearchScope
	RowID        int
	SubRowID     int // 普通表为 -1，子行表 >= 0
	ColIndex     int
	ColName      string
	Text         string
	Score        int
	MatchSpans   []TextMatchSpan
	Translations []TextTranslation
	JumpKind     string // "item"|"quest"|"leve"|"achievement"|"bgm"|"orchestrion"|""
	JumpID       int
}

// TextSearchResponse 保存分页后的文本检索响应。
type TextSearchResponse struct {
	LangCode     string
	CompareLangs []string
	Query        string
	Mode         TextMatchMode
	Items        []TextSearchHit
	TopSheets    []TextSheetFacet
	Total        int
	Page         int
	PageSize     int
	TotalPages   int
	ElapsedMs    float64
	IndexStats   TextIndexStats
}

// ParsedTextQuery 保存解析后的查询语法结构。
type ParsedTextQuery struct {
	Raw          string
	Mode         TextMatchMode
	Empty        bool
	ExactTerms   []string // 折叠后的精确连贯子串列表
	ExactRaw     []string // 原始精确词（用于完全一致大小写微加分）
	FuzzyFull    string   // 折叠后的完整模糊串（用于检测整串连续命中）
	FuzzyCompact string   // 折叠并去除空白后的连续串
	FuzzyRunes   []rune   // 模糊子序列匹配目标字符序列
	QueryMask    uint64   // 64-bit 字符存在位图（O(1) 剪枝）
	UniqueRunes  []rune   // 去重后的所有必填折叠字符（用于倒排表查候选）
}

// TextIndexSheetInput 供单元测试构造内存文本索引的表定义输入。
type TextIndexSheetInput struct {
	Name        string
	IsLocalized bool
	ColNames    []string
}

// TextIndexRowInput 供单元测试构造内存文本索引的单条文本输入。
type TextIndexRowInput struct {
	SheetIdx int
	RowID    int
	SubRowID int // 普通表填 -1
	ColIdx   int
	Text     string
}

const (
	textFlagFoldSameAsText uint16 = 1 << 0
	textFlagInternalKey    uint16 = 1 << 1
	textFlagLocalized      uint16 = 1 << 2
)

// textRecord 为零指针紧凑结构体（32 字节），避免数百万条记录给 Go GC 带来扫描压力。
type textRecord struct {
	sheetIdx  uint16
	colIdx    uint16
	rowID     uint32
	subRowID  int16
	flags     uint16
	textOff   uint32
	textLen   uint32
	foldOff   uint32
	foldLen   uint32
	runeCount uint32
}

type textSheetMeta struct {
	name        string
	lowerName   string
	scope       TextSearchScope
	isLocalized bool
	count       int

	colMu      sync.RWMutex
	colLoaded  bool
	colNameMap map[uint16]string
}

type postingSpan struct {
	off   uint32
	count uint32
}

type textSearchCacheEntry struct {
	key       string
	mode      TextMatchMode
	packed    []uint64
	topSheets []TextSheetFacet
}

// TextSearchIndex 是面向全量 EXD 文本的纯内存高性能检索引擎：
// 采用无指针字节 Arena、独立 SoA 64 位字符位图、高频裁剪倒排索引、多核并行匹配与翻页结果缓存。
type TextSearchIndex struct {
	langCode    string
	buildTimeMs int64
	schemasFS   fs.FS
	reader      *sqpack.GameReader

	sheets       []textSheetMeta
	sheetByLower map[string]int
	records      []textRecord
	charMasks    []uint64 // 独立 SoA 切片，单个 64B Cache Line 容纳 8 个位图
	textBlob     []byte   // 原始展示文本连续字节池（无指针）
	foldBlob     []byte   // 折叠后文本连续字节池（无指针，与原文相同时直接复用 textBlob）

	// 高频裁剪字符倒排索引：BMP 直寻址查找表（O(1) 零哈希）+ 非 BMP 扩展表 + 连续无指针 []uint32 数组
	invBMP      []postingSpan // 长度 65536：count>0 表示有效倒排，off==invHighFreqSentinel 表示高频裁剪字符
	invExtSpans map[rune]postingSpan
	invExtHigh  map[rune]struct{}
	invPostings []uint32

	cacheMu sync.Mutex
	cache   *textSearchCacheEntry
}

const invHighFreqSentinel uint32 = 0xFFFFFFFF

// Stats 返回当前文本索引的统计概览。
func (idx *TextSearchIndex) Stats() TextIndexStats {
	if idx == nil {
		return TextIndexStats{}
	}
	return TextIndexStats{
		LangCode:    idx.langCode,
		TotalSheets: len(idx.sheets),
		TotalTexts:  len(idx.records),
		BuildTimeMs: idx.buildTimeMs,
	}
}

// ParseTextSearchQuery 解析用户输入：
// 若整段输入被英文双引号 "..." 包裹（支持单组或多组由空白分隔的双引号词组），进入精确连贯子串检索；
// 否则进入按字符顺序匹配的模糊检索模式。
func ParseTextSearchQuery(raw string) ParsedTextQuery {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ParsedTextQuery{Raw: raw, Mode: TextMatchFuzzy, Empty: true}
	}

	if len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		rawTerms := parseQuotedTerms(trimmed)
		var exactTerms []string
		var exactRaw []string
		var mask uint64
		var unique []rune
		seen := make(map[rune]struct{}, 32)

		for _, rt := range rawTerms {
			if rt == "" {
				continue
			}
			folded := foldString(rt)
			if folded == "" {
				continue
			}
			exactRaw = append(exactRaw, rt)
			exactTerms = append(exactTerms, folded)
			for _, r := range folded {
				mask |= charBit(r)
				if _, ok := seen[r]; !ok {
					seen[r] = struct{}{}
					unique = append(unique, r)
				}
			}
		}
		if len(exactTerms) == 0 {
			return ParsedTextQuery{Raw: raw, Mode: TextMatchExact, Empty: true}
		}
		return ParsedTextQuery{
			Raw:         raw,
			Mode:        TextMatchExact,
			ExactTerms:  exactTerms,
			ExactRaw:    exactRaw,
			QueryMask:   mask,
			UniqueRunes: unique,
		}
	}

	foldedFull := foldString(trimmed)
	var compactBuf strings.Builder
	compactBuf.Grow(len(foldedFull))
	runes := make([]rune, 0, utf8.RuneCountInString(foldedFull))
	var mask uint64
	unique := make([]rune, 0, 16)
	seen := make(map[rune]struct{}, 16)

	for _, r := range foldedFull {
		if unicode.IsSpace(r) {
			continue
		}
		compactBuf.WriteRune(r)
		runes = append(runes, r)
		mask |= charBit(r)
		if _, ok := seen[r]; !ok {
			seen[r] = struct{}{}
			unique = append(unique, r)
		}
	}
	if len(runes) == 0 {
		return ParsedTextQuery{Raw: raw, Mode: TextMatchFuzzy, Empty: true}
	}

	return ParsedTextQuery{
		Raw:          raw,
		Mode:         TextMatchFuzzy,
		FuzzyFull:    foldedFull,
		FuzzyCompact: compactBuf.String(),
		FuzzyRunes:   runes,
		QueryMask:    mask,
		UniqueRunes:  unique,
	}
}

// parseQuotedTerms 解析以英文双引号开头并结尾的查询串：
// 常规单组 `"AB"` 直接返回 `["AB"]`；多组由空白分隔的 `"foo" "bar"` 返回 `["foo", "bar"]`；
// 若引号不配对则回退为去除首尾双引号的整体精确词。
func parseQuotedTerms(s string) []string {
	inner := s[1 : len(s)-1]
	if !strings.ContainsRune(inner, '"') {
		return []string{inner}
	}
	var terms []string
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] != '"' {
			// 非纯多组引号语法，整体作为首尾去引号后的单个精确词
			return []string{inner}
		}
		end := strings.IndexByte(s[i+1:], '"')
		if end < 0 {
			return []string{inner}
		}
		terms = append(terms, s[i+1:i+1+end])
		i = i + 1 + end + 1
	}
	if len(terms) == 0 {
		return []string{inner}
	}
	return terms
}

// charBit 将单个折叠字符稳定映射到 64 位位图中的 1 位，用于 O(1) 快速排除不可能命中的记录。
func charBit(r rune) uint64 {
	h := uint32(r) * 2654435761
	return uint64(1) << (h >> 26)
}

// foldRune 将单个字符归一化（ASCII/Unicode 小写 + 全角拉丁字母数字转半角 + 全角空格转半角）。
func foldRune(r rune) rune {
	if r < 0x80 {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}
	if r == 0x3000 {
		return ' '
	}
	if r >= 0xFF01 && r <= 0xFF5E {
		r -= 0xFEE0
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}
	return unicode.ToLower(r)
}

// foldString 返回字符串归一化后的版本，若文本已完全归一化则零拷贝直接返回原串。
func foldString(s string) string {
	changed := false
	for i := 0; i < len(s); {
		b := s[i]
		if b < 0x80 {
			if b >= 'A' && b <= 'Z' {
				changed = true
				break
			}
			i++
			continue
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		if foldRune(r) != r {
			changed = true
			break
		}
		i += sz
	}
	if !changed {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		sb.WriteRune(foldRune(r))
	}
	return sb.String()
}

// appendFoldedBytesAndStats 将 b 的归一化形式追加到 dst，同时返回字符数、64 位位图及是否与原文完全一致（零堆分配）。
func appendFoldedBytesAndStats(dst []byte, b []byte) (next []byte, foldOff, foldLen, runeCount uint32, mask uint64, sameAsOrig bool) {
	needFold := false
	var cnt uint32
	for i := 0; i < len(b); {
		ch := b[i]
		if ch < 0x80 {
			cnt++
			if ch >= 'A' && ch <= 'Z' {
				needFold = true
				mask |= charBit(rune(ch + ('a' - 'A')))
			} else {
				mask |= charBit(rune(ch))
			}
			i++
			continue
		}
		r, sz := utf8.DecodeRune(b[i:])
		cnt++
		fr := foldRune(r)
		if fr != r {
			needFold = true
		}
		mask |= charBit(fr)
		i += sz
	}
	if !needFold {
		return dst, 0, uint32(len(b)), cnt, mask, true
	}

	off := uint32(len(dst))
	var utfBuf [utf8.UTFMax]byte
	for i := 0; i < len(b); {
		ch := b[i]
		if ch < 0x80 {
			if ch >= 'A' && ch <= 'Z' {
				dst = append(dst, ch+('a'-'A'))
			} else {
				dst = append(dst, ch)
			}
			i++
			continue
		}
		r, sz := utf8.DecodeRune(b[i:])
		fr := foldRune(r)
		if fr < 0x80 {
			dst = append(dst, byte(fr))
		} else {
			n := utf8.EncodeRune(utfBuf[:], fr)
			dst = append(dst, utfBuf[:n]...)
		}
		i += sz
	}
	return dst, off, uint32(len(dst)) - off, cnt, mask, false
}

// isInternalKeyBytes 判断字节切片是否为内部标识键（如 TEXT_CLSARC100_00001_SEQ_00）或纯技术资源路径。
func isInternalKeyBytes(isLocalized bool, colIdx uint16, text []byte) bool {
	if len(text) >= 5 && text[0] == 'T' && text[1] == 'E' && text[2] == 'X' && text[3] == 'T' && text[4] == '_' {
		return true
	}
	if !isLocalized {
		return true
	}
	if colIdx == 0 && len(text) >= 8 && bytes.IndexByte(text, '_') >= 0 && isUpperIdentASCIIBytes(text) {
		return true
	}
	return false
}

func isUpperIdentASCIIBytes(b []byte) bool {
	hasUpper := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c >= 'A' && c <= 'Z' {
			hasUpper = true
			continue
		}
		if (c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return hasUpper
}

var coreTextSheets = map[string]struct{}{
	"item": {}, "quest": {}, "leve": {}, "achievement": {}, "title": {},
	"action": {}, "actiontransient": {}, "craftaction": {}, "generalaction": {},
	"status": {}, "classjob": {}, "classjobcategory": {}, "placename": {},
	"enpcresident": {}, "bnpcname": {}, "eobjname": {}, "eventitem": {},
	"contentfindercondition": {}, "mount": {}, "mounttransient": {},
	"companion": {}, "companiontransient": {}, "orchestrion": {}, "bgm": {},
	"emote": {}, "fate": {}, "tripletiadcard": {}, "tripletriadcard": {},
	"tripletriadcardresident": {}, "weather": {}, "fishparameter": {},
	"gatheringitem": {}, "gatheringpointname": {}, "gilshop": {},
	"specialshop": {}, "collectablesshop": {}, "minionrace": {},
	"minionrules": {}, "race": {}, "tribe": {}, "world": {},
	"grandcompany": {}, "beasttribe": {}, "beastreputationrank": {},
	"journalgenre": {}, "journalcategory": {}, "journalsection": {},
	"itemuicategory": {}, "baseparam": {}, "recipe": {}, "secretrecipebook": {},
	"town": {}, "leveclient": {}, "leveassignmenttype": {}, "levestring": {},
	"hwdcraftersupplyterm": {}, "satisfactionnpc": {}, "exversion": {},
}

// classifyTextSheet 按数据表名划分用户友好的检索范围分类。
func classifyTextSheet(lowerName string) TextSearchScope {
	if strings.HasPrefix(lowerName, "quest/") ||
		strings.HasPrefix(lowerName, "cut_scene/") ||
		strings.HasPrefix(lowerName, "opening/") ||
		strings.HasPrefix(lowerName, "custom/") ||
		strings.HasPrefix(lowerName, "dungeon/") ||
		strings.HasPrefix(lowerName, "raid/") ||
		strings.HasPrefix(lowerName, "story/") ||
		strings.Contains(lowerName, "talk") ||
		strings.Contains(lowerName, "balloon") ||
		strings.Contains(lowerName, "speechbubble") ||
		strings.Contains(lowerName, "completejournal") {
		return TextScopeStory
	}
	if _, ok := coreTextSheets[lowerName]; ok {
		return TextScopeCore
	}
	if lowerName == "addon" ||
		lowerName == "logmessage" ||
		lowerName == "error" ||
		lowerName == "lobby" ||
		lowerName == "textcommand" ||
		strings.HasPrefix(lowerName, "tutorial") ||
		strings.HasPrefix(lowerName, "howto") ||
		strings.HasPrefix(lowerName, "maincommand") ||
		strings.HasPrefix(lowerName, "config") ||
		strings.HasPrefix(lowerName, "charamake") ||
		strings.HasPrefix(lowerName, "credit") ||
		strings.HasPrefix(lowerName, "help") ||
		strings.HasPrefix(lowerName, "notice") {
		return TextScopeUI
	}
	return TextScopeOther
}

// BuildTextSearchIndexFromScan 从底层 EXD 全量文本扫描结果构建无指针 Arena 与倒排检索引擎。
func BuildTextSearchIndexFromScan(
	ctx context.Context,
	langCode string,
	scan *sqpack.EXDTextScanResult,
	schemasFS fs.FS,
	reader *sqpack.GameReader,
	startedAt time.Time,
) (*TextSearchIndex, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if scan == nil {
		return &TextSearchIndex{
			langCode:     langCode,
			schemasFS:    schemasFS,
			reader:       reader,
			sheetByLower: make(map[string]int),
		}, nil
	}

	sheets := make([]textSheetMeta, len(scan.Sheets))
	sheetByLower := make(map[string]int, len(scan.Sheets))
	for i, s := range scan.Sheets {
		sheets[i] = textSheetMeta{
			name:        s.Name,
			lowerName:   s.LowerName,
			scope:       classifyTextSheet(s.LowerName),
			isLocalized: s.IsLocalized,
		}
		sheetByLower[s.LowerName] = i
	}

	// 预估总文本字节体积并多核并行预折算，最后一次性合并进连续无指针 Arena
	totalCells := 0
	totalBytes := 0
	for i := range scan.Shards {
		totalCells += len(scan.Shards[i].Cells)
		totalBytes += len(scan.Shards[i].Blob)
	}

	type shardPrebuilt struct {
		records   []textRecord
		charMasks []uint64
		foldBlob  []byte
		textBase  uint32
		foldBase  uint32
	}
	prebuilt := make([]shardPrebuilt, len(scan.Shards))
	var pwg sync.WaitGroup
	for si := range scan.Shards {
		shData := &scan.Shards[si]
		if len(shData.Cells) == 0 {
			continue
		}
		pwg.Add(1)
		go func(idx int, shard *sqpack.EXDStringShard) {
			defer pwg.Done()
			recs := make([]textRecord, 0, len(shard.Cells))
			masks := make([]uint64, 0, len(shard.Cells))
			fBlob := make([]byte, 0, len(shard.Blob)/2+256)
			for ci := range shard.Cells {
				if (ci&4095) == 0 && ctx.Err() != nil {
					return
				}
				c := shard.Cells[ci]
				if int(c.SheetIdx) >= len(sheets) || c.TextLen == 0 {
					continue
				}
				rawBytes := shard.Blob[c.TextOff : c.TextOff+c.TextLen]
				var foldOff, foldLen, rCnt uint32
				var mask uint64
				var same bool
				fBlob, foldOff, foldLen, rCnt, mask, same = appendFoldedBytesAndStats(fBlob, rawBytes)

				var flags uint16
				if same {
					flags |= textFlagFoldSameAsText
					foldOff = c.TextOff
				}
				isLoc := sheets[c.SheetIdx].isLocalized
				if isLoc {
					flags |= textFlagLocalized
				}
				if isInternalKeyBytes(isLoc, c.ColIdx, rawBytes) {
					flags |= textFlagInternalKey
				}

				recs = append(recs, textRecord{
					sheetIdx:  c.SheetIdx,
					colIdx:    c.ColIdx,
					rowID:     c.RowID,
					subRowID:  c.SubRowID,
					flags:     flags,
					textOff:   c.TextOff,
					textLen:   c.TextLen,
					foldOff:   foldOff,
					foldLen:   foldLen,
					runeCount: rCnt,
				})
				masks = append(masks, mask)
			}
			prebuilt[idx] = shardPrebuilt{
				records:   recs,
				charMasks: masks,
				foldBlob:  fBlob,
			}
		}(si, shData)
	}
	pwg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	totalRecs := 0
	totalFoldBytes := 0
	for i := range prebuilt {
		prebuilt[i].textBase = uint32(0)
		totalRecs += len(prebuilt[i].records)
		totalFoldBytes += len(prebuilt[i].foldBlob)
	}

	records := make([]textRecord, 0, totalRecs)
	charMasks := make([]uint64, 0, totalRecs)
	textBlob := make([]byte, 0, totalBytes)
	foldBlob := make([]byte, 0, totalFoldBytes)

	for i := range scan.Shards {
		pb := &prebuilt[i]
		if len(pb.records) == 0 {
			continue
		}
		tBase := uint32(len(textBlob))
		fBase := uint32(len(foldBlob))
		textBlob = append(textBlob, scan.Shards[i].Blob...)
		foldBlob = append(foldBlob, pb.foldBlob...)
		// 及时释放分片缓冲，降低构建峰值内存
		scan.Shards[i].Blob = nil
		scan.Shards[i].Cells = nil
		pb.foldBlob = nil

		for j := range pb.records {
			r := &pb.records[j]
			r.textOff += tBase
			if (r.flags & textFlagFoldSameAsText) != 0 {
				r.foldOff = r.textOff
			} else {
				r.foldOff += fBase
			}
			sheets[r.sheetIdx].count++
		}
		records = append(records, pb.records...)
		charMasks = append(charMasks, pb.charMasks...)
		pb.records = nil
		pb.charMasks = nil
	}

	idx := &TextSearchIndex{
		langCode:     langCode,
		schemasFS:    schemasFS,
		reader:       reader,
		sheets:       sheets,
		sheetByLower: sheetByLower,
		records:      records,
		charMasks:    charMasks,
		textBlob:     textBlob,
		foldBlob:     foldBlob,
	}

	if err := idx.buildInvertedRuneIndex(ctx); err != nil {
		return nil, err
	}
	if !startedAt.IsZero() {
		idx.buildTimeMs = time.Since(startedAt).Milliseconds()
	}
	return idx, nil
}

// NewTextSearchIndexFromRecords 基于纯内存记录直接构造文本索引（供单元测试与基准测试使用）。
func NewTextSearchIndexFromRecords(
	langCode string,
	sheetInputs []TextIndexSheetInput,
	rowInputs []TextIndexRowInput,
	schemasFS fs.FS,
	reader *sqpack.GameReader,
) *TextSearchIndex {
	started := time.Now()
	sheets := make([]textSheetMeta, len(sheetInputs))
	sheetByLower := make(map[string]int, len(sheetInputs))
	for i, s := range sheetInputs {
		lower := strings.ToLower(s.Name)
		colMap := make(map[uint16]string, len(s.ColNames))
		for ci, cn := range s.ColNames {
			if cn != "" {
				colMap[uint16(ci)] = cn
			}
		}
		sheets[i] = textSheetMeta{
			name:        s.Name,
			lowerName:   lower,
			scope:       classifyTextSheet(lower),
			isLocalized: s.IsLocalized,
			colLoaded:   len(colMap) > 0,
			colNameMap:  colMap,
		}
		sheetByLower[lower] = i
	}

	records := make([]textRecord, 0, len(rowInputs))
	charMasks := make([]uint64, 0, len(rowInputs))
	textBlob := make([]byte, 0, len(rowInputs)*24)
	foldBlob := make([]byte, 0, len(rowInputs)*12)

	for _, r := range rowInputs {
		if r.SheetIdx < 0 || r.SheetIdx >= len(sheets) {
			continue
		}
		txt := strings.TrimSpace(r.Text)
		if txt == "" {
			continue
		}
		sh := &sheets[r.SheetIdx]
		sh.count++

		textOff := uint32(len(textBlob))
		textBlob = append(textBlob, txt...)
		textLen := uint32(len(txt))

		var foldOff, foldLen, rCnt uint32
		var mask uint64
		var same bool
		rawBytes := textBlob[textOff : textOff+textLen]
		foldBlob, foldOff, foldLen, rCnt, mask, same = appendFoldedBytesAndStats(foldBlob, rawBytes)

		var flags uint16
		if same {
			flags |= textFlagFoldSameAsText
			foldOff = textOff
		}
		if sh.isLocalized {
			flags |= textFlagLocalized
		}
		if isInternalKeyBytes(sh.isLocalized, uint16(r.ColIdx), rawBytes) {
			flags |= textFlagInternalKey
		}

		subID := int16(-1)
		if r.SubRowID >= 0 {
			subID = int16(r.SubRowID)
		}

		records = append(records, textRecord{
			sheetIdx:  uint16(r.SheetIdx),
			colIdx:    uint16(r.ColIdx),
			rowID:     uint32(r.RowID),
			subRowID:  subID,
			flags:     flags,
			textOff:   textOff,
			textLen:   textLen,
			foldOff:   foldOff,
			foldLen:   foldLen,
			runeCount: rCnt,
		})
		charMasks = append(charMasks, mask)
	}

	idx := &TextSearchIndex{
		langCode:     langCode,
		schemasFS:    schemasFS,
		reader:       reader,
		sheets:       sheets,
		sheetByLower: sheetByLower,
		records:      records,
		charMasks:    charMasks,
		textBlob:     textBlob,
		foldBlob:     foldBlob,
	}
	_ = idx.buildInvertedRuneIndex(context.Background())
	idx.buildTimeMs = time.Since(started).Milliseconds()
	return idx
}

// buildInvertedRuneIndex 使用多核分片 + BMP [65536] 直寻址数组（零 Go map 哈希查找）构建单块无指针字符倒排索引。
func (idx *TextSearchIndex) buildInvertedRuneIndex(ctx context.Context) error {
	n := len(idx.records)
	invBMP := make([]postingSpan, 65536)
	invExtSpans := make(map[rune]postingSpan)
	invExtHigh := make(map[rune]struct{})
	if n == 0 {
		idx.invBMP = invBMP
		idx.invExtSpans = invExtSpans
		idx.invExtHigh = invExtHigh
		return nil
	}

	highFreqCutoff := uint32(max(n/8, 65536))

	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	if workers > 12 {
		workers = 12
	}
	if n < 16384 {
		workers = 1
	}

	// Pass 1：多核并行统计每个分片内字符文档频次（使用 epoch 标记数组 O(1) 去重，完全消除 map 查找）
	chunk := (n + workers - 1) / workers
	shardBMPFreq := make([][]uint32, workers)
	shardExtFreq := make([]map[rune]uint32, workers)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		lo := w * chunk
		if lo >= n {
			break
		}
		hi := min(lo+chunk, n)
		wg.Add(1)
		go func(wid, start, end int) {
			defer wg.Done()
			freqBMP := make([]uint32, 65536)
			seenEpoch := make([]uint32, 65536)
			var extFreq map[rune]uint32
			var extSeen map[rune]uint32
			var epoch uint32

			for docID := start; docID < end; docID++ {
				if ((docID-start)&8191) == 0 && ctx.Err() != nil {
					return
				}
				epoch++
				fb := idx.foldBytesOf(docID)
				for i := 0; i < len(fb); {
					r, sz := decodeByteRune(fb, i)
					i += sz
					if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
						continue
					}
					if uint32(r) < 65536 {
						if seenEpoch[r] != epoch {
							seenEpoch[r] = epoch
							freqBMP[r]++
						}
						continue
					}
					if unicode.IsSpace(r) {
						continue
					}
					if extFreq == nil {
						extFreq = make(map[rune]uint32, 32)
						extSeen = make(map[rune]uint32, 32)
					}
					if extSeen[r] != epoch {
						extSeen[r] = epoch
						extFreq[r]++
					}
				}
			}
			shardBMPFreq[wid] = freqBMP
			shardExtFreq[wid] = extFreq
		}(w, lo, hi)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 汇总全局频次并计算每个分片在 postings 中的精确独占写入区间（从而 Pass 2 也能完全无锁多核并行写入！）
	totalBMPFreq := make([]uint32, 65536)
	totalExtFreq := make(map[rune]uint32)
	for w := 0; w < workers; w++ {
		if sf := shardBMPFreq[w]; sf != nil {
			for r := 0; r < 65536; r++ {
				if sf[r] > 0 {
					totalBMPFreq[r] += sf[r]
				}
			}
		}
		for r, c := range shardExtFreq[w] {
			totalExtFreq[r] += c
		}
	}

	var totalPostings uint64
	shardBMPOff := make([][]uint32, workers)
	for w := 0; w < workers; w++ {
		if shardBMPFreq[w] != nil {
			shardBMPOff[w] = make([]uint32, 65536)
		}
	}

	for r := 0; r < 65536; r++ {
		cnt := totalBMPFreq[r]
		if cnt == 0 {
			continue
		}
		if cnt > highFreqCutoff {
			invBMP[r] = postingSpan{off: invHighFreqSentinel, count: 0}
			continue
		}
		base := uint32(totalPostings)
		invBMP[r] = postingSpan{off: base, count: cnt}
		totalPostings += uint64(cnt)

		cursor := base
		for w := 0; w < workers; w++ {
			if shardBMPFreq[w] != nil {
				shardBMPOff[w][r] = cursor
				cursor += shardBMPFreq[w][r]
			}
		}
	}

	shardExtOff := make([]map[rune]uint32, workers)
	for r, cnt := range totalExtFreq {
		if cnt > highFreqCutoff {
			invExtHigh[r] = struct{}{}
			continue
		}
		base := uint32(totalPostings)
		invExtSpans[r] = postingSpan{off: base, count: cnt}
		totalPostings += uint64(cnt)
		cursor := base
		for w := 0; w < workers; w++ {
			if c := shardExtFreq[w][r]; c > 0 {
				if shardExtOff[w] == nil {
					shardExtOff[w] = make(map[rune]uint32, 16)
				}
				shardExtOff[w][r] = cursor
				cursor += c
			}
		}
	}

	postings := make([]uint32, totalPostings)

	// Pass 2：各协程无锁并发填充各自独占 postings 切片区间（天然保持全局 docID 严格升序！）
	for w := 0; w < workers; w++ {
		lo := w * chunk
		if lo >= n {
			break
		}
		hi := min(lo+chunk, n)
		wg.Add(1)
		go func(wid, start, end int) {
			defer wg.Done()
			writeBMP := shardBMPOff[wid]
			writeExt := shardExtOff[wid]
			seenEpoch := make([]uint32, 65536)
			var extSeen map[rune]uint32
			var epoch uint32

			for docID := start; docID < end; docID++ {
				if ((docID-start)&8191) == 0 && ctx.Err() != nil {
					return
				}
				epoch++
				fb := idx.foldBytesOf(docID)
				for i := 0; i < len(fb); {
					r, sz := decodeByteRune(fb, i)
					i += sz
					if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
						continue
					}
					if uint32(r) < 65536 {
						if invBMP[r].count == 0 {
							continue
						}
						if seenEpoch[r] != epoch {
							seenEpoch[r] = epoch
							p := writeBMP[r]
							postings[p] = uint32(docID)
							writeBMP[r] = p + 1
						}
						continue
					}
					if writeExt == nil {
						continue
					}
					if _, high := invExtHigh[r]; high {
						continue
					}
					if extSeen == nil {
						extSeen = make(map[rune]uint32, 16)
					}
					if extSeen[r] != epoch {
						extSeen[r] = epoch
						p := writeExt[r]
						postings[p] = uint32(docID)
						writeExt[r] = p + 1
					}
				}
			}
		}(w, lo, hi)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	idx.invBMP = invBMP
	idx.invExtSpans = invExtSpans
	idx.invExtHigh = invExtHigh
	idx.invPostings = postings
	return nil
}

// parallelSortUint64 将大 uint64 切片划分为多段并行排序后两两归并，大幅加速十万级结果排序。
func parallelSortUint64(data []uint64) {
	n := len(data)
	workers := min(runtime.NumCPU(), 8)
	if workers < 2 || n < 32768 {
		slices.Sort(data)
		return
	}
	// 向上凑成 2 的幂次分块（2/4/8），方便两两并行归并
	parts := 2
	for parts*2 <= workers {
		parts *= 2
	}

	bounds := make([]int, parts+1)
	for i := 0; i <= parts; i++ {
		bounds[i] = (n * i) / parts
	}

	var wg sync.WaitGroup
	for i := 0; i < parts; i++ {
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			slices.Sort(data[lo:hi])
		}(bounds[i], bounds[i+1])
	}
	wg.Wait()

	tmp := make([]uint64, n)
	src, dst := data, tmp
	for width := 1; width < parts; width *= 2 {
		for i := 0; i < parts; i += width * 2 {
			lo := bounds[i]
			mid := bounds[i+width]
			hi := bounds[i+width*2]
			wg.Add(1)
			go func(l, m, r int) {
				defer wg.Done()
				mergeSortedUint64(src[l:m], src[m:r], dst[l:r])
			}(lo, mid, hi)
		}
		wg.Wait()
		src, dst = dst, src
	}
	if &src[0] != &data[0] {
		copy(data, src)
	}
}

func mergeSortedUint64(a, b, dst []uint64) {
	i, j, k := 0, 0, 0
	for i < len(a) && j < len(b) {
		if a[i] <= b[j] {
			dst[k] = a[i]
			i++
		} else {
			dst[k] = b[j]
			j++
		}
		k++
	}
	if i < len(a) {
		copy(dst[k:], a[i:])
	} else if j < len(b) {
		copy(dst[k:], b[j:])
	}
}

func decodeByteRune(b []byte, i int) (rune, int) {
	if b[i] < utf8.RuneSelf {
		return rune(b[i]), 1
	}
	return utf8.DecodeRune(b[i:])
}

func (idx *TextSearchIndex) textBytesOf(docID int) []byte {
	rec := idx.records[docID]
	return idx.textBlob[rec.textOff : rec.textOff+rec.textLen]
}

func (idx *TextSearchIndex) foldBytesOf(docID int) []byte {
	rec := idx.records[docID]
	if (rec.flags & textFlagFoldSameAsText) != 0 {
		return idx.textBlob[rec.foldOff : rec.foldOff+rec.foldLen]
	}
	return idx.foldBlob[rec.foldOff : rec.foldOff+rec.foldLen]
}

func (idx *TextSearchIndex) resolveColumnName(sheetIdx uint16, colIdx uint16) string {
	if int(sheetIdx) >= len(idx.sheets) {
		return fmt.Sprintf("Col %d", colIdx)
	}
	sh := &idx.sheets[sheetIdx]
	sh.colMu.RLock()
	if sh.colLoaded {
		name := sh.colNameMap[colIdx]
		sh.colMu.RUnlock()
		if name != "" {
			return name
		}
		return fmt.Sprintf("Col %d", colIdx)
	}
	sh.colMu.RUnlock()

	// 按需懒加载该表的 EXDSchema 字段名对齐映射（仅对当前页命中的少量表触发一次）
	var loadedMap map[uint16]string
	if idx.schemasFS != nil && idx.reader != nil {
		if flatNames, err := sqpack.LoadSchemaFieldNames(idx.schemasFS, sh.name); err == nil && len(flatNames) > 0 {
			if exhBuf, err := idx.reader.ReadFile("exd/" + sh.lowerName + ".exh"); err == nil && len(exhBuf) >= 32 {
				if exh, err := sqpack.ReadEXHHeader(exhBuf); err == nil {
					aligned, _ := sqpack.AlignEXHColumns(exh, flatNames)
					loadedMap = make(map[uint16]string, len(aligned))
					for _, c := range aligned {
						if c.Name != "" {
							loadedMap[uint16(c.Index)] = c.Name
						}
					}
				}
			}
		}
	}

	sh.colMu.Lock()
	if !sh.colLoaded {
		sh.colNameMap = loadedMap
		sh.colLoaded = true
	}
	name := sh.colNameMap[colIdx]
	sh.colMu.Unlock()
	if name != "" {
		return name
	}
	return fmt.Sprintf("Col %d", colIdx)
}

// Search 执行模糊或精确全量文本检索，返回按匹配程度排序的分页结果与表分布统计。
func (idx *TextSearchIndex) Search(ctx context.Context, q TextSearchQuery, resolveJump func(sheetLower string, rowID int) (string, int)) (TextSearchResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	started := time.Now()
	stats := idx.Stats()

	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	scope := q.Scope
	if scope == "" {
		scope = TextScopeAll
	}
	sheetFilterLower := strings.ToLower(strings.TrimSpace(q.SheetFilter))

	parsed := ParseTextSearchQuery(q.Q)
	if idx == nil || parsed.Empty {
		return TextSearchResponse{
			LangCode:   stats.LangCode,
			Query:      q.Q,
			Mode:       parsed.Mode,
			Page:       1,
			PageSize:   pageSize,
			TotalPages: 1,
			IndexStats: stats,
		}, nil
	}

	cacheKey := fmt.Sprintf("%s\x00%s\x00%s", q.Q, scope, sheetFilterLower)
	idx.cacheMu.Lock()
	var cached *textSearchCacheEntry
	if idx.cache != nil && idx.cache.key == cacheKey {
		cached = idx.cache
	}
	idx.cacheMu.Unlock()

	var packed []uint64
	var topSheets []TextSheetFacet

	if cached != nil {
		packed = cached.packed
		topSheets = cached.topSheets
	} else {
		var err error
		packed, topSheets, err = idx.executeSearch(ctx, parsed, scope, sheetFilterLower)
		if err != nil {
			return TextSearchResponse{}, err
		}
		idx.cacheMu.Lock()
		idx.cache = &textSearchCacheEntry{
			key:       cacheKey,
			mode:      parsed.Mode,
			packed:    packed,
			topSheets: topSheets,
		}
		idx.cacheMu.Unlock()
	}

	total := len(packed)
	page, totalPages, start, end := paginateRange(total, q.Page, pageSize)

	items := make([]TextSearchHit, 0, max(0, end-start))
	for i := start; i < end; i++ {
		key := packed[i]
		docID := uint32(key & 0xFFFFFFFF)
		score := 65535 - int(key>>48)
		rec := idx.records[docID]
		sh := &idx.sheets[rec.sheetIdx]
		origText := string(idx.textBytesOf(int(docID)))
		spans := computeHighlightSpans(origText, parsed)

		jumpKind := ""
		jumpID := 0
		if resolveJump != nil {
			jumpKind, jumpID = resolveJump(sh.lowerName, int(rec.rowID))
		}

		items = append(items, TextSearchHit{
			DocID:      docID,
			SheetName:  sh.name,
			Scope:      sh.scope,
			RowID:      int(rec.rowID),
			SubRowID:   int(rec.subRowID),
			ColIndex:   int(rec.colIdx),
			ColName:    idx.resolveColumnName(rec.sheetIdx, rec.colIdx),
			Text:       origText,
			Score:      score,
			MatchSpans: spans,
			JumpKind:   jumpKind,
			JumpID:     jumpID,
		})
	}

	return TextSearchResponse{
		LangCode:   stats.LangCode,
		Query:      q.Q,
		Mode:       parsed.Mode,
		Items:      items,
		TopSheets:  topSheets,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		ElapsedMs:  float64(time.Since(started).Microseconds()) / 1000.0,
		IndexStats: stats,
	}, nil
}

// executeSearch 结合倒排候选集、SoA 64 位位图剪枝、多核并行匹配和 64 位整数打包排序执行检索。
func (idx *TextSearchIndex) executeSearch(
	ctx context.Context,
	parsed ParsedTextQuery,
	scope TextSearchScope,
	sheetFilterLower string,
) ([]uint64, []TextSheetFacet, error) {
	n := len(idx.records)
	if n == 0 {
		return nil, nil, nil
	}

	// 检查指定表筛选是否存在
	targetSheetIdx := -1
	if sheetFilterLower != "" {
		sIdx, ok := idx.sheetByLower[sheetFilterLower]
		if !ok {
			return nil, nil, nil
		}
		targetSheetIdx = sIdx
	}

	// 1. 通过 BMP 直寻址倒排索引寻找最稀有字符对应的候选文档列表
	var candidates []uint32
	hasCandidates := false
	bestCount := uint32(0)

	for _, r := range parsed.UniqueRunes {
		if unicode.IsSpace(r) {
			continue
		}
		var sp postingSpan
		if uint32(r) < 65536 && len(idx.invBMP) == 65536 {
			sp = idx.invBMP[r]
			if sp.off == invHighFreqSentinel {
				continue
			}
			if sp.count == 0 {
				return nil, nil, nil
			}
		} else {
			if _, isHigh := idx.invExtHigh[r]; isHigh {
				continue
			}
			var ok bool
			sp, ok = idx.invExtSpans[r]
			if !ok || sp.count == 0 {
				return nil, nil, nil
			}
		}
		if !hasCandidates || sp.count < bestCount {
			bestCount = sp.count
			candidates = idx.invPostings[sp.off : sp.off+sp.count]
			hasCandidates = true
		}
	}

	// 预准备查询字节切片，避免热循环中反复 string->[]byte 拷贝
	var exactTermBytes [][]byte
	var exactRawBytes [][]byte
	var fuzzyFullBytes []byte
	var fuzzyCompactBytes []byte
	if parsed.Mode == TextMatchExact {
		exactTermBytes = make([][]byte, len(parsed.ExactTerms))
		exactRawBytes = make([][]byte, len(parsed.ExactRaw))
		for i, t := range parsed.ExactTerms {
			exactTermBytes[i] = []byte(t)
			exactRawBytes[i] = []byte(parsed.ExactRaw[i])
		}
	} else {
		fuzzyFullBytes = []byte(parsed.FuzzyFull)
		fuzzyCompactBytes = []byte(parsed.FuzzyCompact)
	}

	queryMask := parsed.QueryMask
	checkScope := scope != TextScopeAll
	checkSheet := targetSheetIdx >= 0

	evalDoc := func(docID int) (uint64, uint16, bool) {
		if (idx.charMasks[docID] & queryMask) != queryMask {
			return 0, 0, false
		}
		rec := idx.records[docID]
		if checkSheet && int(rec.sheetIdx) != targetSheetIdx {
			return 0, 0, false
		}
		if checkScope && idx.sheets[rec.sheetIdx].scope != scope {
			return 0, 0, false
		}

		foldBytes := idx.foldBytesOf(docID)
		var score int
		var ok bool
		if parsed.Mode == TextMatchExact {
			origBytes := idx.textBytesOf(docID)
			score, ok = scoreExactMatch(foldBytes, origBytes, exactTermBytes, exactRawBytes, int(rec.runeCount))
		} else {
			score, ok = scoreFuzzyMatch(foldBytes, parsed.FuzzyRunes, fuzzyFullBytes, fuzzyCompactBytes, int(rec.runeCount))
		}
		if !ok {
			return 0, 0, false
		}

		if (rec.flags & textFlagInternalKey) != 0 {
			score -= 1200
		}
		if score < 1 {
			score = 1
		}
		if score > 65535 {
			score = 65535
		}

		lenTie := rec.runeCount
		if lenTie > 65535 {
			lenTie = 65535
		}
		// 打包为单 uint64 排序键：分数降序 -> 字符数升序 -> 文档顺序升序
		packedKey := (uint64(65535-score) << 48) | (uint64(lenTie) << 32) | uint64(docID)
		return packedKey, rec.sheetIdx, true
	}

	totalWork := n
	if hasCandidates {
		totalWork = len(candidates)
	}

	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	if workers > 12 {
		workers = 12
	}
	// 小规模候选集直接单核无同步跑完，避免协程调度开销
	if totalWork < 8192 {
		workers = 1
	}

	var allPacked []uint64
	sheetCounts := make(map[uint16]int, 64)

	if workers == 1 {
		if hasCandidates {
			for i, docID := range candidates {
				if (i&4095) == 0 && ctx.Err() != nil {
					return nil, nil, ctx.Err()
				}
				if key, sIdx, ok := evalDoc(int(docID)); ok {
					allPacked = append(allPacked, key)
					sheetCounts[sIdx]++
				}
			}
		} else {
			for docID := 0; docID < n; docID++ {
				if (docID&4095) == 0 && ctx.Err() != nil {
					return nil, nil, ctx.Err()
				}
				if key, sIdx, ok := evalDoc(docID); ok {
					allPacked = append(allPacked, key)
					sheetCounts[sIdx]++
				}
			}
		}
	} else {
		shardKeys := make([][]uint64, workers)
		shardSheets := make([]map[uint16]int, workers)
		chunk := (totalWork + workers - 1) / workers
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			lo := w * chunk
			if lo >= totalWork {
				break
			}
			hi := min(lo+chunk, totalWork)
			wg.Add(1)
			go func(wid, start, end int) {
				defer wg.Done()
				localKeys := make([]uint64, 0, 128)
				localSheets := make(map[uint16]int, 32)
				if hasCandidates {
					for i := start; i < end; i++ {
						if ((i-start)&4095) == 0 && ctx.Err() != nil {
							return
						}
						if key, sIdx, ok := evalDoc(int(candidates[i])); ok {
							localKeys = append(localKeys, key)
							localSheets[sIdx]++
						}
					}
				} else {
					for docID := start; docID < end; docID++ {
						if ((docID-start)&4095) == 0 && ctx.Err() != nil {
							return
						}
						if key, sIdx, ok := evalDoc(docID); ok {
							localKeys = append(localKeys, key)
							localSheets[sIdx]++
						}
					}
				}
				shardKeys[wid] = localKeys
				shardSheets[wid] = localSheets
			}(w, lo, hi)
		}
		wg.Wait()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		totalHits := 0
		for _, sk := range shardKeys {
			totalHits += len(sk)
		}
		allPacked = make([]uint64, 0, totalHits)
		for w := 0; w < workers; w++ {
			allPacked = append(allPacked, shardKeys[w]...)
			for sIdx, cnt := range shardSheets[w] {
				sheetCounts[sIdx] += cnt
			}
		}
	}

	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	// 大规模命中（如 10 万+ 条）使用并行归并排序，中小结果集直接单线程 uint64 原位快排
	if len(allPacked) >= 32768 {
		parallelSortUint64(allPacked)
	} else {
		slices.Sort(allPacked)
	}

	// 统计当前检索命中的 Top 数据表（按命中数降序，供筛选栏快速下钻）
	facets := make([]TextSheetFacet, 0, len(sheetCounts))
	for sIdx, cnt := range sheetCounts {
		sh := &idx.sheets[sIdx]
		facets = append(facets, TextSheetFacet{
			SheetName: sh.name,
			Scope:     sh.scope,
			Count:     cnt,
		})
	}
	sort.Slice(facets, func(i, j int) bool {
		if facets[i].Count != facets[j].Count {
			return facets[i].Count > facets[j].Count
		}
		return facets[i].SheetName < facets[j].SheetName
	})
	if len(facets) > 30 {
		facets = facets[:30]
	}

	return allPacked, facets, nil
}

// scoreExactMatch 计算精确连贯子串匹配得分（全部精确词必须连续出现）。
func scoreExactMatch(foldBytes, origBytes []byte, terms, rawTerms [][]byte, textRunes int) (int, bool) {
	if len(terms) == 0 {
		return 0, false
	}
	firstPos := -1
	totalMatchedRunes := 0
	exactCaseAll := true

	for i, term := range terms {
		pos := bytes.Index(foldBytes, term)
		if pos < 0 {
			return 0, false
		}
		if i == 0 {
			firstPos = pos
		}
		totalMatchedRunes += utf8.RuneCount(term)
		if exactCaseAll && len(rawTerms) > i && bytes.Index(origBytes, rawTerms[i]) < 0 {
			exactCaseAll = false
		}
	}

	score := 10500
	if len(terms) == 1 && len(foldBytes) == len(terms[0]) {
		score = 22000
	} else if firstPos == 0 {
		score = 16000
	} else if isWordBoundaryAt(foldBytes, firstPos) {
		score = 13500
	}

	if exactCaseAll {
		score += 200
	}
	if textRunes < 1 {
		textRunes = 1
	}
	cov := (totalMatchedRunes * 3200) / textRunes
	if cov > 3200 {
		cov = 3200
	}
	score += cov

	// 位置越靠前得分越高
	posPenalty := firstPos * 6
	if posPenalty > 900 {
		posPenalty = 900
	}
	score += 900 - posPenalty
	return score, true
}

// scoreFuzzyMatch 执行高速顺序子序列模糊匹配并按匹配紧密度、连击度、覆盖率与位置打分（零堆分配）。
func scoreFuzzyMatch(foldBytes []byte, qRunes []rune, qFullBytes, qCompactBytes []byte, textRunes int) (int, bool) {
	qLen := len(qRunes)
	if qLen == 0 || len(foldBytes) == 0 {
		return 0, false
	}
	if textRunes < 1 {
		textRunes = 1
	}

	// 快速通道 1：整串直接连续命中（SIMD bytes.Index，天然最优窗口）
	if len(qFullBytes) > 0 {
		if pos := bytes.Index(foldBytes, qFullBytes); pos >= 0 {
			return scoreContiguousFuzzy(foldBytes, pos, len(qFullBytes), qLen, textRunes, true), true
		}
	}
	// 快速通道 2：去空白后的紧凑串直接连续命中（如输入 "光 战" 命中 "光战"）
	if len(qCompactBytes) > 0 && len(qCompactBytes) != len(qFullBytes) {
		if pos := bytes.Index(foldBytes, qCompactBytes); pos >= 0 {
			return scoreContiguousFuzzy(foldBytes, pos, len(qCompactBytes), qLen, textRunes, false), true
		}
	}
	if qLen == 1 {
		return 0, false
	}

	// 通用前向-后向紧缩最优子序列匹配（最多滑移尝试 3 个窗口以避开前部孤立首字）
	bestScore := -1
	searchFrom := 0
	for attempt := 0; attempt < 3 && searchFrom < len(foldBytes); attempt++ {
		endByte, found := forwardSubseqEnd(foldBytes, searchFrom, qRunes)
		if !found {
			break
		}
		startByte, spanRunes, consecPairs, firstRuneSize := backwardSubseqWindow(foldBytes, searchFrom, endByte, qRunes)
		sc := calcSubseqScore(foldBytes, startByte, spanRunes, consecPairs, qLen, textRunes)
		if sc > bestScore {
			bestScore = sc
		}
		// 如果窗口已经达到近乎完全紧缩（跨度 <= qLen+1），无需继续滑动
		if spanRunes <= qLen+1 {
			break
		}
		searchFrom = startByte + firstRuneSize
	}
	if bestScore < 0 {
		return 0, false
	}
	return bestScore, true
}

func scoreContiguousFuzzy(foldBytes []byte, pos, matchByteLen, qRunes, textRunes int, fullMatch bool) int {
	score := 10500
	if len(foldBytes) == matchByteLen {
		score = 22000
	} else if pos == 0 {
		score = 16000
	} else if isWordBoundaryAt(foldBytes, pos) {
		score = 13500
	}
	if !fullMatch {
		score -= 400
	}
	cov := (qRunes * 3200) / textRunes
	if cov > 3200 {
		cov = 3200
	}
	score += cov
	posPenalty := pos * 6
	if posPenalty > 900 {
		posPenalty = 900
	}
	score += 900 - posPenalty
	return score
}

// forwardSubseqEnd 从 searchFrom 向前扫描确认 qRunes 按顺序全部存在，返回最后一个匹配字符的结束字节偏移。
func forwardSubseqEnd(b []byte, searchFrom int, qRunes []rune) (int, bool) {
	qi := 0
	target := qRunes[0]
	for i := searchFrom; i < len(b); {
		r, sz := decodeByteRune(b, i)
		i += sz
		if r == target {
			qi++
			if qi == len(qRunes) {
				return i, true
			}
			target = qRunes[qi]
		}
	}
	return 0, false
}

// backwardSubseqWindow 从 endByte 反向收缩至最紧凑起点，同时统计窗口字符跨度与相邻连击对数。
func backwardSubseqWindow(b []byte, minStart, endByte int, qRunes []rune) (startByte, spanRunes, consecPairs, firstRuneSize int) {
	qi := len(qRunes) - 1
	target := qRunes[qi]
	pos := endByte
	lastMatchRuneDist := -1
	runesSeen := 0
	firstRuneSize = 1

	for pos > minStart {
		r, sz := utf8.DecodeLastRune(b[:pos])
		pos -= sz
		runesSeen++
		if r == target {
			if lastMatchRuneDist == 1 {
				consecPairs++
			}
			lastMatchRuneDist = 0
			qi--
			if qi < 0 {
				startByte = pos
				spanRunes = runesSeen
				firstRuneSize = sz
				return
			}
			target = qRunes[qi]
		} else if lastMatchRuneDist >= 0 {
			lastMatchRuneDist++
		}
	}
	return minStart, runesSeen, consecPairs, firstRuneSize
}

func calcSubseqScore(foldBytes []byte, startByte, spanRunes, consecPairs, qLen, textRunes int) int {
	if spanRunes < qLen {
		spanRunes = qLen
	}
	score := 1500
	if qLen > 1 {
		score += (consecPairs * 2800) / (qLen - 1)
	}
	score += (qLen * 2600) / spanRunes
	cov := (qLen * 1300) / max(textRunes, qLen)
	if cov > 1300 {
		cov = 1300
	}
	score += cov

	if startByte == 0 {
		score += 700
	} else if isWordBoundaryAt(foldBytes, startByte) {
		score += 350
	}
	posPenalty := startByte * 4
	if posPenalty > 300 {
		posPenalty = 300
	}
	score += 300 - posPenalty
	return score
}

func isWordBoundaryAt(b []byte, bytePos int) bool {
	if bytePos <= 0 || bytePos > len(b) {
		return true
	}
	prev, _ := utf8.DecodeLastRune(b[:bytePos])
	if unicode.IsSpace(prev) {
		return true
	}
	return unicode.IsPunct(prev) || unicode.IsSymbol(prev)
}

// computeHighlightSpans 仅对当前展示页的结果计算精确的原文字节高亮区间，自动合并相邻片段。
func computeHighlightSpans(origText string, parsed ParsedTextQuery) []TextMatchSpan {
	if origText == "" || parsed.Empty {
		return nil
	}
	type origRunePos struct {
		start  int
		end    int
		folded rune
	}
	rList := make([]origRunePos, 0, utf8.RuneCountInString(origText))
	for i := 0; i < len(origText); {
		r, sz := utf8.DecodeRuneInString(origText[i:])
		rList = append(rList, origRunePos{
			start:  i,
			end:    i + sz,
			folded: foldRune(r),
		})
		i += sz
	}
	if len(rList) == 0 {
		return nil
	}

	var spans []TextMatchSpan
	if parsed.Mode == TextMatchExact {
		for _, term := range parsed.ExactTerms {
			tRunes := []rune(term)
			if len(tRunes) == 0 || len(tRunes) > len(rList) {
				continue
			}
			for i := 0; i <= len(rList)-len(tRunes); i++ {
				matched := true
				for j := 0; j < len(tRunes); j++ {
					if rList[i+j].folded != tRunes[j] {
						matched = false
						break
					}
				}
				if matched {
					spans = append(spans, TextMatchSpan{
						Start: rList[i].start,
						End:   rList[i+len(tRunes)-1].end,
					})
					break
				}
			}
		}
		return mergeMatchSpans(spans)
	}

	// 模糊模式：优先标出连续命中，否则使用前向-后向紧缩窗口定位精确高亮字符
	qRunes := parsed.FuzzyRunes
	if len(qRunes) == 0 {
		return nil
	}
	for _, candStr := range []string{parsed.FuzzyFull, parsed.FuzzyCompact} {
		cRunes := []rune(candStr)
		if len(cRunes) == 0 || len(cRunes) > len(rList) {
			continue
		}
		for i := 0; i <= len(rList)-len(cRunes); i++ {
			matched := true
			for j := 0; j < len(cRunes); j++ {
				if rList[i+j].folded != cRunes[j] {
					matched = false
					break
				}
			}
			if matched {
				return []TextMatchSpan{{
					Start: rList[i].start,
					End:   rList[i+len(cRunes)-1].end,
				}}
			}
		}
	}

	// 找最紧凑子序列索引序列
	bestSpanLen := len(rList) + 1
	var bestIdxs []int
	searchFrom := 0
	for attempt := 0; attempt < 3 && searchFrom < len(rList); attempt++ {
		qi := 0
		endIdx := -1
		for i := searchFrom; i < len(rList); i++ {
			if rList[i].folded == qRunes[qi] {
				qi++
				if qi == len(qRunes) {
					endIdx = i
					break
				}
			}
		}
		if endIdx < 0 {
			break
		}
		idxs := make([]int, len(qRunes))
		qi = len(qRunes) - 1
		for i := endIdx; i >= searchFrom && qi >= 0; i-- {
			if rList[i].folded == qRunes[qi] {
				idxs[qi] = i
				qi--
			}
		}
		spanLen := idxs[len(idxs)-1] - idxs[0] + 1
		if spanLen < bestSpanLen {
			bestSpanLen = spanLen
			bestIdxs = idxs
		}
		if spanLen <= len(qRunes)+1 {
			break
		}
		searchFrom = idxs[0] + 1
	}

	for _, idx := range bestIdxs {
		spans = append(spans, TextMatchSpan{
			Start: rList[idx].start,
			End:   rList[idx].end,
		})
	}
	return mergeMatchSpans(spans)
}

func mergeMatchSpans(spans []TextMatchSpan) []TextMatchSpan {
	if len(spans) <= 1 {
		return spans
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start != spans[j].Start {
			return spans[i].Start < spans[j].Start
		}
		return spans[i].End < spans[j].End
	})
	out := make([]TextMatchSpan, 0, len(spans))
	cur := spans[0]
	for i := 1; i < len(spans); i++ {
		if spans[i].Start <= cur.End {
			if spans[i].End > cur.End {
				cur.End = spans[i].End
			}
			continue
		}
		out = append(out, cur)
		cur = spans[i]
	}
	out = append(out, cur)
	return out
}

// ---------------------------------------------------------------------------
// Service 层的全量文本检索接入与按语言缓存管理
// ---------------------------------------------------------------------------

func textSearchLangEnum(code string) (sqpack.Language, bool) {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "chs":
		return sqpack.LangChineseSimplified, true
	case "ja":
		return sqpack.LangJapanese, true
	case "en":
		return sqpack.LangEnglish, true
	case "de":
		return sqpack.LangGerman, true
	case "fr":
		return sqpack.LangFrench, true
	default:
		return sqpack.LangNone, false
	}
}

// DefaultTextSearchLang 返回当前主数据对应的默认文本检索语言代码（chs / ja / en）。
func (s *Service) DefaultTextSearchLang() string {
	if s == nil || s.store == nil {
		return "chs"
	}
	switch s.store.PrimaryLang {
	case sqpack.LangJapanese:
		return "ja"
	case sqpack.LangEnglish:
		return "en"
	case sqpack.LangGerman:
		return "de"
	case sqpack.LangFrench:
		return "fr"
	case sqpack.LangChineseSimplified:
		return "chs"
	}
	if s.store.HasCN {
		return "chs"
	}
	if s.store.HasGlobal {
		return "en"
	}
	return "chs"
}

// AvailableTextSearchLangs 返回当前已挂载客户端支持的所有文本检索语言选项（主数据语言排在首位）。
func (s *Service) AvailableTextSearchLangs() []TextSearchLangOption {
	if s == nil || s.store == nil {
		return nil
	}
	var opts []TextSearchLangOption
	if s.store.HasCN && s.store.CNReader != nil {
		opts = append(opts, TextSearchLangOption{Code: "chs", Label: "简体中文 (chs)", Lang: sqpack.LangChineseSimplified})
	}
	if s.store.HasGlobal && s.store.GlobalReader != nil {
		opts = append(opts,
			TextSearchLangOption{Code: "ja", Label: "日本語 (ja)", Lang: sqpack.LangJapanese},
			TextSearchLangOption{Code: "en", Label: "English (en)", Lang: sqpack.LangEnglish},
			TextSearchLangOption{Code: "de", Label: "Deutsch (de)", Lang: sqpack.LangGerman},
			TextSearchLangOption{Code: "fr", Label: "Français (fr)", Lang: sqpack.LangFrench},
		)
	}
	def := s.DefaultTextSearchLang()
	for i := range opts {
		if opts[i].Code == def && i > 0 {
			found := opts[i]
			copy(opts[1:i+1], opts[0:i])
			opts[0] = found
			break
		}
	}
	return opts
}

func (s *Service) textSearchReaderAndLang(langCode string) (*sqpack.GameReader, sqpack.Language, string) {
	if s == nil || s.store == nil {
		return nil, sqpack.LangNone, ""
	}
	code := strings.ToLower(strings.TrimSpace(langCode))
	if code == "" {
		code = s.DefaultTextSearchLang()
	}
	if lang, ok := textSearchLangEnum(code); ok {
		if lang == sqpack.LangChineseSimplified && s.store.CNReader != nil {
			return s.store.CNReader, lang, "chs"
		}
		if lang != sqpack.LangChineseSimplified && s.store.GlobalReader != nil {
			return s.store.GlobalReader, lang, code
		}
	}
	if s.store.PrimaryReader != nil {
		pLang := s.store.PrimaryLang
		pCode := sqpack.FormatLanguage(pLang)
		if pCode == "" {
			pCode = s.DefaultTextSearchLang()
		}
		return s.store.PrimaryReader, pLang, pCode
	}
	return nil, sqpack.LangNone, code
}

// GetTextSearchIndex 返回指定语言已缓存的文本检索索引（未构建时返回 nil）。
func (s *Service) GetTextSearchIndex(langCode string) *TextSearchIndex {
	if s == nil {
		return nil
	}
	_, _, normCode := s.textSearchReaderAndLang(langCode)
	s.textIndexMu.RLock()
	defer s.textIndexMu.RUnlock()
	if s.textIndices == nil {
		return nil
	}
	return s.textIndices[normCode]
}

// SetTextSearchIndex 注入预构建的文本检索索引（供单元测试与无客户端测试使用）。
func (s *Service) SetTextSearchIndex(langCode string, index *TextSearchIndex) {
	if s == nil || index == nil {
		return
	}
	code := strings.ToLower(strings.TrimSpace(langCode))
	if code == "" {
		code = s.DefaultTextSearchLang()
	}
	s.textIndexMu.Lock()
	if s.textIndices == nil {
		s.textIndices = make(map[string]*TextSearchIndex, 4)
	}
	s.textIndices[code] = index
	s.textIndexMu.Unlock()
}

// BuildTextSearchIndex 按需异步/同步构建并缓存指定客户端语言的全部 EXD 文本索引。
func (s *Service) BuildTextSearchIndex(ctx context.Context, langCode string, onProgress func(TextIndexProgress)) (*TextSearchIndex, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("游戏数据服务未初始化")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reader, lang, normCode := s.textSearchReaderAndLang(langCode)

	s.textIndexMu.RLock()
	if cached := s.textIndices[normCode]; cached != nil {
		s.textIndexMu.RUnlock()
		if onProgress != nil {
			st := cached.Stats()
			onProgress(TextIndexProgress{
				LangCode:     normCode,
				Phase:        "ready",
				DoneSheets:   st.TotalSheets,
				TotalSheets:  st.TotalSheets,
				IndexedTexts: st.TotalTexts,
			})
		}
		return cached, nil
	}
	s.textIndexMu.RUnlock()

	if reader == nil {
		return nil, fmt.Errorf("未配置对应客户端，无法构建 %s 文本索引", normCode)
	}

	started := time.Now()
	var resolveFn sqpack.SeSheetResolver
	if lang == s.store.PrimaryLang {
		resolveFn = s.resolveSeSheetText
	}

	scanRes, err := sqpack.ScanAllEXDStrings(ctx, reader, sqpack.EXDTextScanOptions{
		PreferredLang: lang,
		ResolveSheet:  resolveFn,
		OnProgress: func(done, total, texts int) {
			if onProgress != nil {
				onProgress(TextIndexProgress{
					LangCode:     normCode,
					Phase:        "scanning",
					DoneSheets:   done,
					TotalSheets:  total,
					IndexedTexts: texts,
				})
			}
		},
	})
	if err != nil {
		return nil, err
	}

	if onProgress != nil {
		onProgress(TextIndexProgress{
			LangCode:     normCode,
			Phase:        "indexing",
			DoneSheets:   len(scanRes.Sheets),
			TotalSheets:  len(scanRes.Sheets),
			IndexedTexts: scanRes.TotalRows,
		})
	}

	built, err := BuildTextSearchIndexFromScan(ctx, normCode, scanRes, s.store.SchemasFS, reader, started)
	if err != nil {
		return nil, err
	}

	s.textIndexMu.Lock()
	if s.textIndices == nil {
		s.textIndices = make(map[string]*TextSearchIndex, 4)
	}
	s.textIndices[normCode] = built
	s.textIndexMu.Unlock()

	if onProgress != nil {
		st := built.Stats()
		onProgress(TextIndexProgress{
			LangCode:     normCode,
			Phase:        "ready",
			DoneSheets:   st.TotalSheets,
			TotalSheets:  st.TotalSheets,
			IndexedTexts: st.TotalTexts,
		})
	}
	return built, nil
}

// SearchTexts 在指定语言的已构建全文索引中执行查询并返回分页结果、多语言对照与实体跳转信息。
func (s *Service) SearchTexts(ctx context.Context, query TextSearchQuery) (TextSearchResponse, error) {
	if s == nil {
		return TextSearchResponse{Page: 1, PageSize: 50, TotalPages: 1}, nil
	}
	idx := s.GetTextSearchIndex(query.LangCode)
	if idx == nil {
		return TextSearchResponse{
			LangCode:   query.LangCode,
			Query:      query.Q,
			Mode:       ParseTextSearchQuery(query.Q).Mode,
			Page:       1,
			PageSize:   max(1, query.PageSize),
			TotalPages: 1,
		}, nil
	}
	res, err := idx.Search(ctx, query, s.resolveTextJumpTarget)
	if err != nil {
		return res, err
	}
	compareLangs := s.CompareTextSearchLangs(res.LangCode)
	res.CompareLangs = compareLangs
	if len(compareLangs) > 0 && len(res.Items) > 0 {
		s.populateHitTranslations(idx, res.Items, compareLangs)
	}
	return res, nil
}

// CompareTextSearchLangs 返回针对当前检索语言可供表格多语言对照的其他语言代码列表（核心中/日/英优先）。
func (s *Service) CompareTextSearchLangs(searchLangCode string) []string {
	if s == nil {
		return nil
	}
	cur := strings.ToLower(strings.TrimSpace(searchLangCode))
	if cur == "" {
		cur = s.DefaultTextSearchLang()
	}

	avail := make(map[string]bool, 5)
	if s.store != nil {
		if s.store.HasCN && s.store.CNReader != nil {
			avail["chs"] = true
		}
		if s.store.HasGlobal && s.store.GlobalReader != nil {
			avail["ja"] = true
			avail["en"] = true
			avail["de"] = true
			avail["fr"] = true
		}
	}
	// 同时兼容测试中直接注入的内存语言索引
	s.textIndexMu.RLock()
	for code, idx := range s.textIndices {
		if idx != nil {
			avail[strings.ToLower(code)] = true
		}
	}
	s.textIndexMu.RUnlock()

	var order []string
	switch cur {
	case "chs":
		order = []string{"ja", "en"}
	case "ja":
		order = []string{"chs", "en"}
	case "en":
		order = []string{"chs", "ja"}
	default:
		order = []string{"chs", "ja", "en"}
	}

	out := make([]string, 0, len(order))
	for _, code := range order {
		if code != cur && avail[code] {
			out = append(out, code)
		}
	}
	return out
}

func textLangBadge(code string) string {
	switch strings.ToLower(code) {
	case "chs":
		return "ZH"
	case "ja":
		return "JA"
	case "en":
		return "EN"
	case "de":
		return "DE"
	case "fr":
		return "FR"
	default:
		return strings.ToUpper(code)
	}
}

type parallelEXHMeta struct {
	exh         *sqpack.EXHHeader
	langSuffix  string
	isLocalized bool
	strCols     []int
	colByName   map[string]int
}

type parallelEXDPage struct {
	data       []byte
	rowOffsets map[uint32]uint32
	dataOffset int
	variant    uint8
	columns    []sqpack.EXHColumn
}

type textParallelCache struct {
	mu       sync.Mutex
	exhMap   map[string]*parallelEXHMeta
	exhOrder []string
	exdMap   map[string]*parallelEXDPage
	exdOrder []string
}

const (
	maxParallelEXHCache = 512
	maxParallelEXDCache = 512
)

func (s *Service) ensureParallelCache() *textParallelCache {
	s.textIndexMu.Lock()
	defer s.textIndexMu.Unlock()
	if s.textParallel == nil {
		s.textParallel = &textParallelCache{
			exhMap: make(map[string]*parallelEXHMeta, 128),
			exdMap: make(map[string]*parallelEXDPage, 128),
		}
	}
	return s.textParallel
}

// lookupInLoadedIndex 在已构建的内存全文索引中按表、行与列精准查找单条文本（仅用于跨语言索引已就绪或测试场景）。
func (idx *TextSearchIndex) lookupExactCell(sheetLower string, rowID, subRowID, colIdx int) (string, bool) {
	if idx == nil {
		return "", false
	}
	sIdx, ok := idx.sheetByLower[sheetLower]
	if !ok || !idx.sheets[sIdx].isLocalized {
		return "", false
	}
	uSheet := uint16(sIdx)
	uRow := uint32(rowID)
	iSub := int16(subRowID)
	uCol := uint16(colIdx)
	for docID := range idx.records {
		r := idx.records[docID]
		if r.sheetIdx == uSheet && r.rowID == uRow && r.subRowID == iSub && r.colIdx == uCol {
			return string(idx.textBytesOf(docID)), true
		}
	}
	return "", false
}

// populateHitTranslations 为当前分页命中的少量条目（最多每页 30~100 条）并发按需读取其他语言的对照译文，
// 配合有界 EXH/EXD 分片缓存，冷查与翻页均保持毫秒级响应且无需为对照语言构建全量内存索引。
func (s *Service) populateHitTranslations(srcIdx *TextSearchIndex, items []TextSearchHit, compareLangs []string) {
	if len(items) == 0 || len(compareLangs) == 0 {
		return
	}
	fillOne := func(i int) {
		hit := &items[i]
		sheetLower := strings.ToLower(hit.SheetName)
		// 非本地化通用表（各客户端一致的内部路径/标识）不重复显示对照语言
		if srcIdx != nil {
			if sIdx, ok := srcIdx.sheetByLower[sheetLower]; ok && !srcIdx.sheets[sIdx].isLocalized {
				return
			}
		}
		var trans []TextTranslation
		for _, langCode := range compareLangs {
			txt := s.lookupParallelCellText(langCode, hit.SheetName, sheetLower, hit.RowID, hit.SubRowID, hit.ColIndex, hit.ColName)
			if txt == "" {
				continue
			}
			trans = append(trans, TextTranslation{
				LangCode: langCode,
				Badge:    textLangBadge(langCode),
				Text:     txt,
			})
		}
		if len(trans) > 0 {
			hit.Translations = trans
		}
	}

	if len(items) <= 4 {
		for i := range items {
			fillOne(i)
		}
		return
	}

	workers := min(len(items), min(runtime.NumCPU(), 8))
	if workers < 2 {
		for i := range items {
			fillOne(i)
		}
		return
	}
	jobs := make(chan int, len(items))
	for i := range items {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for idx := range jobs {
				fillOne(idx)
			}
		}()
	}
	wg.Wait()
}

func (s *Service) lookupParallelCellText(langCode, sheetName, sheetLower string, rowID, subRowID, colIdx int, colName string) string {
	// 1. 若目标语言纯内存索引已存在于测试或会话缓存，且无真实客户端句柄，直接查询内存索引
	reader, langEnum, normCode := s.textSearchReaderAndLang(langCode)
	if reader == nil {
		s.textIndexMu.RLock()
		otherIdx := s.textIndices[strings.ToLower(langCode)]
		s.textIndexMu.RUnlock()
		if otherIdx != nil {
			if txt, ok := otherIdx.lookupExactCell(sheetLower, rowID, subRowID, colIdx); ok {
				return txt
			}
		}
		return ""
	}
	if langEnum == sqpack.LangNone {
		return ""
	}

	cache := s.ensureParallelCache()
	exhKey := normCode + "\x00" + sheetLower

	cache.mu.Lock()
	meta, ok := cache.exhMap[exhKey]
	cache.mu.Unlock()

	if !ok {
		meta = s.loadParallelEXHMeta(reader, sheetName, sheetLower, langEnum)
		cache.mu.Lock()
		if _, dup := cache.exhMap[exhKey]; !dup {
			if len(cache.exhOrder) >= maxParallelEXHCache {
				oldest := cache.exhOrder[0]
				cache.exhOrder = cache.exhOrder[1:]
				delete(cache.exhMap, oldest)
			}
			cache.exhOrder = append(cache.exhOrder, exhKey)
			cache.exhMap[exhKey] = meta
		}
		cache.mu.Unlock()
	}
	if meta == nil || meta.exh == nil || !meta.isLocalized {
		return ""
	}

	// 优先按 EXDSchema 字段名对齐目标列（防国服/国际服跨版本列偏移），无 Schema 时回退原始列序号
	targetColIdx := -1
	if colName != "" && !strings.HasPrefix(colName, "Col ") && meta.colByName != nil {
		if idxByName, found := meta.colByName[colName]; found && idxByName >= 0 && idxByName < len(meta.exh.Columns) && meta.exh.Columns[idxByName].Type == 0 {
			targetColIdx = idxByName
		}
	}
	if targetColIdx < 0 {
		if colIdx >= 0 && colIdx < len(meta.exh.Columns) && meta.exh.Columns[colIdx].Type == 0 {
			targetColIdx = colIdx
		} else {
			return ""
		}
	}

	// 根据 EXH 分片定义定位 rowID 所在分片
	var pageStartID uint32
	foundPage := false
	uRow := uint32(rowID)
	for _, pg := range meta.exh.Paginations {
		if uRow >= pg.StartID && uRow < pg.StartID+pg.RowCount {
			pageStartID = pg.StartID
			foundPage = true
			break
		}
	}
	if !foundPage {
		return ""
	}

	exdKey := fmt.Sprintf("%s\x00%s\x00%d", normCode, sheetLower, pageStartID)
	cache.mu.Lock()
	page, hasPage := cache.exdMap[exdKey]
	cache.mu.Unlock()

	if !hasPage {
		page = loadParallelEXDPage(reader, sheetLower, pageStartID, meta.langSuffix, meta.exh)
		cache.mu.Lock()
		if _, dup := cache.exdMap[exdKey]; !dup {
			if len(cache.exdOrder) >= maxParallelEXDCache {
				oldest := cache.exdOrder[0]
				cache.exdOrder = cache.exdOrder[1:]
				delete(cache.exdMap, oldest)
			}
			cache.exdOrder = append(cache.exdOrder, exdKey)
			cache.exdMap[exdKey] = page
		}
		cache.mu.Unlock()
	}
	if page == nil {
		return ""
	}

	rawBytes := page.readStringBytes(uRow, subRowID, targetColIdx)
	if len(rawBytes) == 0 {
		return ""
	}
	var resolver sqpack.SeSheetResolver
	if langEnum == s.store.PrimaryLang {
		resolver = s.resolveSeSheetText
	}
	return sqpack.DecodeSeStringDisplayForLang(rawBytes, langEnum, resolver)
}

func (s *Service) loadParallelEXHMeta(reader *sqpack.GameReader, sheetName, sheetLower string, preferredLang sqpack.Language) *parallelEXHMeta {
	exhBuf, err := reader.ReadFile("exd/" + sheetLower + ".exh")
	if err != nil || len(exhBuf) < 32 {
		return nil
	}
	exh, err := sqpack.ReadEXHHeader(exhBuf)
	if err != nil || len(exh.Paginations) == 0 {
		return nil
	}
	hasLang := false
	for _, l := range exh.Languages {
		if l == preferredLang {
			hasLang = true
			break
		}
	}
	if !hasLang {
		return &parallelEXHMeta{exh: exh, isLocalized: false}
	}
	code := sqpack.FormatLanguage(preferredLang)
	langSuffix := ""
	if code != "" && !strings.HasPrefix(code, "unk(") {
		langSuffix = "_" + code
	}

	var strCols []int
	for ci, c := range exh.Columns {
		if c.Type == 0 {
			strCols = append(strCols, ci)
		}
	}
	var colByName map[string]int
	if s.store != nil && s.store.SchemasFS != nil {
		if flatNames, err := sqpack.LoadSchemaFieldNames(s.store.SchemasFS, sheetName); err == nil && len(flatNames) > 0 {
			_, colByName = sqpack.AlignEXHColumns(exh, flatNames)
		}
	}
	return &parallelEXHMeta{
		exh:         exh,
		langSuffix:  langSuffix,
		isLocalized: true,
		strCols:     strCols,
		colByName:   colByName,
	}
}

func loadParallelEXDPage(reader *sqpack.GameReader, sheetLower string, startID uint32, langSuffix string, exh *sqpack.EXHHeader) *parallelEXDPage {
	exdPath := fmt.Sprintf("exd/%s_%d%s.exd", sheetLower, startID, langSuffix)
	data, err := reader.ReadFile(exdPath)
	if err != nil || len(data) < 32 {
		return nil
	}
	if data[0] != 'E' || data[1] != 'X' || data[2] != 'D' || data[3] != 'F' {
		return nil
	}
	indexSize := int(binary.BigEndian.Uint32(data[8:12]))
	if 32+indexSize > len(data) {
		return nil
	}
	count := indexSize / 8
	offs := make(map[uint32]uint32, count)
	pos := 32
	for i := 0; i < count; i++ {
		rid := binary.BigEndian.Uint32(data[pos : pos+4])
		off := binary.BigEndian.Uint32(data[pos+4 : pos+8])
		offs[rid] = off
		pos += 8
	}
	return &parallelEXDPage{
		data:       data,
		rowOffsets: offs,
		dataOffset: int(exh.DataOffset),
		variant:    exh.Variant,
		columns:    exh.Columns,
	}
}

func (p *parallelEXDPage) readStringBytes(rowID uint32, subRowID int, colIdx int) []byte {
	hdrOff, ok := p.rowOffsets[rowID]
	if !ok || int(hdrOff)+6 > len(p.data) || colIdx < 0 || colIdx >= len(p.columns) {
		return nil
	}
	col := p.columns[colIdx]
	if col.Type != 0 {
		return nil
	}
	rowCnt := int(binary.BigEndian.Uint16(p.data[hdrOff+4 : hdrOff+6]))
	baseOff := int(hdrOff) + 6

	rowOff := baseOff
	if p.variant == sqpack.ExcelVariantSubrows {
		found := false
		for s := 0; s < rowCnt; s++ {
			subOff := baseOff + s*(p.dataOffset+2)
			if subOff+2+p.dataOffset > len(p.data) {
				break
			}
			sid := int(binary.BigEndian.Uint16(p.data[subOff : subOff+2]))
			if sid == subRowID {
				rowOff = subOff + 2
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	} else if rowCnt != 1 {
		return nil
	}

	off := rowOff + int(col.Offset)
	if off+4 > len(p.data) {
		return nil
	}
	strOff := int(binary.BigEndian.Uint32(p.data[off : off+4]))
	start := rowOff + p.dataOffset + strOff
	if start < 0 || start >= len(p.data) || p.data[start] == 0 {
		return nil
	}
	relEnd := bytes.IndexByte(p.data[start:], 0)
	if relEnd < 0 {
		return p.data[start:]
	}
	if relEnd == 0 {
		return nil
	}
	return p.data[start : start+relEnd]
}

func (s *Service) ensureQuestByInternalID() map[string]int {
	s.questInternalOnce.Do(func() {
		if s.store == nil {
			return
		}
		m := make(map[string]int, len(s.store.Quests))
		for _, q := range s.store.Quests {
			if q.InternalID != "" {
				m[strings.ToLower(q.InternalID)] = q.ID
			}
		}
		s.questByInternalID = m
	})
	return s.questByInternalID
}

// resolveTextJumpTarget 判断命中的数据表与行号是否可直接跳转至内置详情页（物品/任务/理符/成就/音乐）。
func (s *Service) resolveTextJumpTarget(sheetLower string, rowID int) (string, int) {
	if s == nil || s.store == nil || rowID < 0 {
		return "", 0
	}
	switch sheetLower {
	case "item":
		if _, ok := s.store.ItemSummaryByID[rowID]; ok {
			return "item", rowID
		}
	case "quest":
		if _, ok := s.store.QuestByID[rowID]; ok {
			return "quest", rowID
		}
	case "leve":
		if _, ok := s.store.LeveByID[rowID]; ok {
			return "leve", rowID
		}
	case "achievement":
		if _, ok := s.store.AchievementByID[rowID]; ok {
			return "achievement", rowID
		}
	case "bgm":
		if _, ok := s.store.BGMByID[rowID]; ok {
			return "bgm", rowID
		}
	case "orchestrion":
		if _, ok := s.store.OrchByID[rowID]; ok {
			return "orchestrion", rowID
		}
	default:
		// 任务剧情表 quest/053/FesNyx301_05377 自动反查对应任务详情
		if strings.HasPrefix(sheetLower, "quest/") {
			if slash := strings.LastIndexByte(sheetLower, '/'); slash >= 0 && slash+1 < len(sheetLower) {
				internalLower := sheetLower[slash+1:]
				if qid, ok := s.ensureQuestByInternalID()[internalLower]; ok && qid > 0 {
					return "quest", qid
				}
			}
		}
	}
	return "", 0
}

// FormatRowKey 格式化普通行号或子行号（例如 #44099 或 #100.2）。
func FormatTextRowKey(rowID, subRowID int) string {
	if subRowID >= 0 {
		return "#" + strconv.Itoa(rowID) + "." + strconv.Itoa(subRowID)
	}
	return "#" + strconv.Itoa(rowID)
}
