package backup

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

// TextSnapshotRecord 表示版本文本对比中的单条本地化文本记录（用于测试夹具与内部归并）。
type TextSnapshotRecord struct {
	SheetName string
	RowID     uint32
	SubRowID  int16 // 普通表为 -1，子行表 >= 0
	ColIdx    uint16
	Text      string
}

type textSheetRows struct {
	name      string
	lowerName string
	rows      []textSnapRow
}

type textSnapRow struct {
	rowID    uint32
	subRowID int16
	colIdx   uint16
	text     string
}

type cachedTextDiff struct {
	key         string
	entries     []DiffItemEntry
	modifiedCnt int
	removedCnt  int
}

func buildTextSheetRows(records []TextSnapshotRecord) []textSheetRows {
	bySheet := make(map[string]*textSheetRows, 16)
	for _, r := range records {
		if r.SheetName == "" || r.Text == "" {
			continue
		}
		lower := strings.ToLower(r.SheetName)
		sh, ok := bySheet[lower]
		if !ok {
			sh = &textSheetRows{name: r.SheetName, lowerName: lower}
			bySheet[lower] = sh
		}
		sh.rows = append(sh.rows, textSnapRow{
			rowID:    r.RowID,
			subRowID: r.SubRowID,
			colIdx:   r.ColIdx,
			text:     r.Text,
		})
	}
	sheets := make([]textSheetRows, 0, len(bySheet))
	for _, sh := range bySheet {
		sortTextSnapRows(sh.rows)
		sh.rows = dedupTextSnapRows(sh.rows)
		sheets = append(sheets, *sh)
	}
	sort.Slice(sheets, func(i, j int) bool {
		return sheets[i].lowerName < sheets[j].lowerName
	})
	return sheets
}

func scanLocalizedTextsFromReader(ctx context.Context, reader sqpack.FileReader, lang sqpack.Language) ([]textSheetRows, error) {
	scanRes, err := sqpack.ScanAllEXDStrings(ctx, reader, sqpack.EXDTextScanOptions{
		PreferredLang: lang,
	})
	if err != nil {
		return nil, err
	}

	buckets := make([][]textSnapRow, len(scanRes.Sheets))
	for si := range scanRes.Shards {
		shData := &scanRes.Shards[si]
		for ci := range shData.Cells {
			c := shData.Cells[ci]
			if int(c.SheetIdx) >= len(scanRes.Sheets) || c.TextLen == 0 {
				continue
			}
			meta := scanRes.Sheets[c.SheetIdx]
			if !meta.IsLocalized {
				continue
			}
			raw := shData.Blob[c.TextOff : c.TextOff+c.TextLen]
			if isSnapshotInternalKey(c.ColIdx, raw) {
				continue
			}
			buckets[c.SheetIdx] = append(buckets[c.SheetIdx], textSnapRow{
				rowID:    c.RowID,
				subRowID: c.SubRowID,
				colIdx:   c.ColIdx,
				text:     string(raw),
			})
		}
		shData.Blob = nil
		shData.Cells = nil
	}

	sheets := make([]textSheetRows, 0, len(scanRes.Sheets))
	for i, rows := range buckets {
		if len(rows) == 0 {
			continue
		}
		sortTextSnapRows(rows)
		rows = dedupTextSnapRows(rows)
		sheets = append(sheets, textSheetRows{
			name:      scanRes.Sheets[i].Name,
			lowerName: scanRes.Sheets[i].LowerName,
			rows:      rows,
		})
	}
	sort.Slice(sheets, func(i, j int) bool {
		return sheets[i].lowerName < sheets[j].lowerName
	})
	return sheets, nil
}

func isSnapshotInternalKey(colIdx uint16, b []byte) bool {
	if len(b) >= 5 && b[0] == 'T' && b[1] == 'E' && b[2] == 'X' && b[3] == 'T' && b[4] == '_' {
		return true
	}
	if colIdx == 0 && len(b) >= 8 {
		hasUnderscore := false
		hasUpper := false
		for _, ch := range b {
			if ch == '_' {
				hasUnderscore = true
				continue
			}
			if ch >= 'A' && ch <= 'Z' {
				hasUpper = true
				continue
			}
			if (ch >= '0' && ch <= '9') || ch == '-' {
				continue
			}
			return false
		}
		return hasUnderscore && hasUpper
	}
	return false
}

func compareTextSnapKey(a, b textSnapRow) int {
	if a.rowID != b.rowID {
		if a.rowID < b.rowID {
			return -1
		}
		return 1
	}
	if a.subRowID != b.subRowID {
		if a.subRowID < b.subRowID {
			return -1
		}
		return 1
	}
	if a.colIdx != b.colIdx {
		if a.colIdx < b.colIdx {
			return -1
		}
		return 1
	}
	return 0
}

func sortTextSnapRows(rows []textSnapRow) {
	sort.Slice(rows, func(i, j int) bool {
		return compareTextSnapKey(rows[i], rows[j]) < 0
	})
}

func dedupTextSnapRows(rows []textSnapRow) []textSnapRow {
	if len(rows) <= 1 {
		return rows
	}
	out := rows[:1]
	for i := 1; i < len(rows); i++ {
		if compareTextSnapKey(rows[i], out[len(out)-1]) == 0 {
			out[len(out)-1] = rows[i]
			continue
		}
		out = append(out, rows[i])
	}
	return out
}

func (s *Service) hasTextSourceLocked(region, target string) bool {
	reg := snapshot.NormalizeRegion(region)
	if target == LiveVersionID {
		if len(s.memLiveTexts[reg]) > 0 {
			return true
		}
		r, _, _ := s.resolveEXDSourceLocked(reg, LiveVersionID)
		return r != nil
	}
	snapID := snapshot.MakeSnapshotID(reg, target)
	if ms, ok := s.memSnaps[snapID]; ok {
		if len(ms.Texts) > 0 {
			return true
		}
	}
	r, _, _ := s.resolveEXDSourceLocked(reg, target)
	return r != nil
}

func (s *Service) loadTargetTextsLocked(region, target string, uiLang i18n.Lang) ([]textSheetRows, bool) {
	reg := snapshot.NormalizeRegion(region)
	if target == LiveVersionID && len(s.memLiveTexts[reg]) > 0 {
		return s.memLiveTexts[reg], true
	}
	snapID := snapshot.MakeSnapshotID(reg, target)
	if ms, ok := s.memSnaps[snapID]; ok && len(ms.Texts) > 0 {
		return buildTextSheetRows(ms.Texts), true
	}

	reader, _, exdLang := s.resolveEXDSourceLocked(reg, target)
	if reader == nil {
		return nil, false
	}

	cacheKey := fmt.Sprintf("%s\x00%s\x00%d", reg, target, exdLang)
	s.textMu.Lock()
	if cached, ok := s.textCache[cacheKey]; ok {
		s.textSeq++
		s.textCacheSeq[cacheKey] = s.textSeq
		s.textMu.Unlock()
		return cached, true
	}
	s.textMu.Unlock()

	sheets, err := scanLocalizedTextsFromReader(context.Background(), reader, exdLang)
	if err != nil {
		return nil, false
	}

	s.textMu.Lock()
	const maxCachedTextSets = 3
	if len(s.textCache) >= maxCachedTextSets {
		var oldestKey string
		var oldestSeq uint64 = ^uint64(0)
		for k, seq := range s.textCacheSeq {
			if seq < oldestSeq {
				oldestSeq = seq
				oldestKey = k
			}
		}
		if oldestKey != "" {
			delete(s.textCache, oldestKey)
			delete(s.textCacheSeq, oldestKey)
		}
	}
	s.textSeq++
	s.textCache[cacheKey] = sheets
	s.textCacheSeq[cacheKey] = s.textSeq
	s.textMu.Unlock()
	return sheets, true
}

// computeTextDiffLocked 按需直接从同区服历史 EXD 快照与客户端实时计算全量文本差异（仅变更与移除）。
func (s *Service) computeTextDiffLocked(region string, lang i18n.Lang, fromID, toID string, loadIfUncached bool) (entries []DiffItemEntry, modCnt, remCnt int, available bool) {
	reg := snapshot.NormalizeRegion(region)
	if !s.hasTextSourceLocked(reg, fromID) || !s.hasTextSourceLocked(reg, toID) {
		return nil, 0, 0, false
	}

	cacheKey := fmt.Sprintf("%s\x00%s\x00%s\x00%s", reg, fromID, toID, lang)
	s.textMu.Lock()
	if s.textDiffCache != nil && s.textDiffCache.key == cacheKey {
		c := s.textDiffCache
		s.textMu.Unlock()
		return c.entries, c.modifiedCnt, c.removedCnt, true
	}
	s.textMu.Unlock()

	if !loadIfUncached {
		return nil, 0, 0, true
	}

	fromSheets, ok1 := s.loadTargetTextsLocked(reg, fromID, lang)
	toSheets, ok2 := s.loadTargetTextsLocked(reg, toID, lang)
	if !ok1 || !ok2 {
		return nil, 0, 0, false
	}

	toByLower := make(map[string]*textSheetRows, len(toSheets))
	for i := range toSheets {
		toByLower[toSheets[i].lowerName] = &toSheets[i]
	}

	diffEntries := make([]DiffItemEntry, 0, 256)
	beforeLbl := i18n.T(lang, "diff.text_before_label")
	removedLbl := i18n.T(lang, "diff.change_removed")

	for i := range fromSheets {
		fromSh := &fromSheets[i]
		toSh, exists := toByLower[fromSh.lowerName]
		if !exists {
			for _, fr := range fromSh.rows {
				remCnt++
				diffEntries = append(diffEntries, makeTextDiffEntry(fromSh.name, fr.rowID, fr.subRowID, fr.colIdx, "removed", fr.text, "", beforeLbl, removedLbl))
			}
			continue
		}

		fi, ti := 0, 0
		fRows, tRows := fromSh.rows, toSh.rows
		for fi < len(fRows) && ti < len(tRows) {
			cmp := compareTextSnapKey(fRows[fi], tRows[ti])
			switch {
			case cmp == 0:
				if fRows[fi].text != tRows[ti].text {
					modCnt++
					diffEntries = append(diffEntries, makeTextDiffEntry(fromSh.name, fRows[fi].rowID, fRows[fi].subRowID, fRows[fi].colIdx, "modified", fRows[fi].text, tRows[ti].text, beforeLbl, removedLbl))
				}
				fi++
				ti++
			case cmp < 0:
				remCnt++
				diffEntries = append(diffEntries, makeTextDiffEntry(fromSh.name, fRows[fi].rowID, fRows[fi].subRowID, fRows[fi].colIdx, "removed", fRows[fi].text, "", beforeLbl, removedLbl))
				fi++
			default:
				ti++
			}
		}
		for ; fi < len(fRows); fi++ {
			remCnt++
			diffEntries = append(diffEntries, makeTextDiffEntry(fromSh.name, fRows[fi].rowID, fRows[fi].subRowID, fRows[fi].colIdx, "removed", fRows[fi].text, "", beforeLbl, removedLbl))
		}
	}

	sort.Slice(diffEntries, func(i, j int) bool {
		if diffEntries[i].ChangeType != diffEntries[j].ChangeType {
			return diffEntries[i].ChangeType == "modified"
		}
		if diffEntries[i].TextSheet != diffEntries[j].TextSheet {
			return diffEntries[i].TextSheet < diffEntries[j].TextSheet
		}
		if diffEntries[i].ID != diffEntries[j].ID {
			return diffEntries[i].ID < diffEntries[j].ID
		}
		if diffEntries[i].TextSubRow != diffEntries[j].TextSubRow {
			return diffEntries[i].TextSubRow < diffEntries[j].TextSubRow
		}
		return diffEntries[i].TextColIdx < diffEntries[j].TextColIdx
	})

	s.textMu.Lock()
	s.textDiffCache = &cachedTextDiff{
		key:         cacheKey,
		entries:     diffEntries,
		modifiedCnt: modCnt,
		removedCnt:  remCnt,
	}
	s.textMu.Unlock()
	return diffEntries, modCnt, remCnt, true
}

func makeTextDiffEntry(sheetName string, rowID uint32, subRowID int16, colIdx uint16, changeType, before, after, fieldLbl, removedVal string) DiffItemEntry {
	rowKey := gamedata.FormatTextRowKey(int(rowID), int(subRowID))
	dispName := sheetName + " " + rowKey
	chgAfter := after
	if changeType == "removed" {
		chgAfter = removedVal
	}
	return DiffItemEntry{
		ID:           int(rowID),
		EntityType:   "text",
		ChangeType:   changeType,
		NameCn:       dispName,
		NameEn:       dispName,
		CategoryName: sheetName,
		TextSheet:    sheetName,
		TextSubRow:   int(subRowID),
		TextColIdx:   int(colIdx),
		TextBefore:   before,
		TextAfter:    after,
		Changes: []ItemFieldChange{{
			Field:  "text",
			Label:  fieldLbl,
			Before: before,
			After:  chgAfter,
		}},
	}
}

func textDiffEntryMatchesQuery(e DiffItemEntry, q string) bool {
	if strings.Contains(strings.ToLower(e.TextSheet), q) ||
		strings.Contains(strings.ToLower(e.TextColName), q) ||
		strings.Contains(strings.ToLower(e.TextBefore), q) ||
		strings.Contains(strings.ToLower(e.TextAfter), q) ||
		strconv.Itoa(e.ID) == q {
		return true
	}
	return false
}

// enrichTextDiffPageLocked 仅为当前展示页的少量文本差异条目补充 EXDSchema 字段名与关联实体跳转信息。
func (s *Service) enrichTextDiffPageLocked(entries []DiffItemEntry, region, toID string) {
	if s == nil || len(entries) == 0 {
		return
	}
	reader, schemasFS, _ := s.resolveEXDSourceLocked(region, toID)
	colCache := make(map[string]map[int]string, 16)
	var questByInternal map[string]int

	resolveCol := func(sheetName string, colIdx int) string {
		if m, ok := colCache[sheetName]; ok {
			if n := m[colIdx]; n != "" {
				return n
			}
			return fmt.Sprintf("Col %d", colIdx)
		}
		var loaded map[int]string
		if schemasFS != nil && reader != nil {
			if flatNames, err := sqpack.LoadSchemaFieldNames(schemasFS, sheetName); err == nil && len(flatNames) > 0 {
				if exhBuf, err := reader.ReadFile("exd/" + strings.ToLower(sheetName) + ".exh"); err == nil && len(exhBuf) >= 32 {
					if exh, err := sqpack.ReadEXHHeader(exhBuf); err == nil {
						aligned, _ := sqpack.AlignEXHColumns(exh, flatNames)
						loaded = make(map[int]string, len(aligned))
						for _, c := range aligned {
							if c.Name != "" {
								loaded[c.Index] = c.Name
							}
						}
					}
				}
			}
		}
		colCache[sheetName] = loaded
		if n := loaded[colIdx]; n != "" {
			return n
		}
		return fmt.Sprintf("Col %d", colIdx)
	}

	for i := range entries {
		e := &entries[i]
		if e.TextColName == "" {
			e.TextColName = resolveCol(e.TextSheet, e.TextColIdx)
		}
		if e.ChangeType == "removed" || s.store == nil {
			continue
		}
		lower := strings.ToLower(e.TextSheet)
		switch lower {
		case "item":
			if _, ok := s.store.ItemSummaryByID[e.ID]; ok {
				e.TextJumpKind, e.TextJumpID = "item", e.ID
			}
		case "quest":
			if _, ok := s.store.QuestByID[e.ID]; ok {
				e.TextJumpKind, e.TextJumpID = "quest", e.ID
			}
		case "leve":
			if _, ok := s.store.LeveByID[e.ID]; ok {
				e.TextJumpKind, e.TextJumpID = "leve", e.ID
			}
		case "achievement":
			if _, ok := s.store.AchievementByID[e.ID]; ok {
				e.TextJumpKind, e.TextJumpID = "achievement", e.ID
			}
		case "bgm":
			if _, ok := s.store.BGMByID[e.ID]; ok {
				e.TextJumpKind, e.TextJumpID = "bgm", e.ID
			}
		case "orchestrion":
			if _, ok := s.store.OrchByID[e.ID]; ok {
				e.TextJumpKind, e.TextJumpID = "orchestrion", e.ID
			}
		default:
			if strings.HasPrefix(lower, "quest/") {
				if questByInternal == nil {
					questByInternal = make(map[string]int, len(s.store.Quests))
					for _, q := range s.store.Quests {
						if q.InternalID != "" {
							questByInternal[strings.ToLower(q.InternalID)] = q.ID
						}
					}
				}
				if slash := strings.LastIndexByte(lower, '/'); slash >= 0 && slash+1 < len(lower) {
					if qid, ok := questByInternal[lower[slash+1:]]; ok && qid > 0 {
						e.TextJumpKind, e.TextJumpID = "quest", qid
					}
				}
			}
		}
	}
}

// ComputeInlineTextDiffBounds 返回旧文本与新文本中存在差异的 UTF-8 字节区间 [bStart, bEnd) 与 [aStart, aEnd)，
// 供版本对比界面精确高亮变更片段。
func ComputeInlineTextDiffBounds(before, after string) (bStart, bEnd, aStart, aEnd int) {
	if before == after {
		return 0, 0, 0, 0
	}
	bRunes := []rune(before)
	aRunes := []rune(after)

	prefix := 0
	for prefix < len(bRunes) && prefix < len(aRunes) && bRunes[prefix] == aRunes[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(bRunes)-prefix && suffix < len(aRunes)-prefix &&
		bRunes[len(bRunes)-1-suffix] == aRunes[len(aRunes)-1-suffix] {
		suffix++
	}

	bStart = runeSliceByteOffset(before, prefix)
	bEnd = runeSliceByteOffset(before, len(bRunes)-suffix)
	aStart = runeSliceByteOffset(after, prefix)
	aEnd = runeSliceByteOffset(after, len(aRunes)-suffix)
	return bStart, bEnd, aStart, aEnd
}

func runeSliceByteOffset(s string, runeIdx int) int {
	if runeIdx <= 0 {
		return 0
	}
	off := 0
	cnt := 0
	for off < len(s) && cnt < runeIdx {
		_, sz := utf8.DecodeRuneInString(s[off:])
		off += sz
		cnt++
	}
	return off
}

type diffToken struct {
	text  string
	start int // UTF-8 字节起点
	end   int // UTF-8 字节终点
}

// tokenizeForTextDiff 将文本切分为适合人类阅读的差异单元：
// 连续数字（含小数点，如 7500 / 2.5）和连续英文字母分别作为整体词元，其余字符（汉字、标点、空白、换行）按单字切分。
func tokenizeForTextDiff(s string) []diffToken {
	if s == "" {
		return nil
	}
	tokens := make([]diffToken, 0, len(s)/2+4)
	i := 0
	n := len(s)
	for i < n {
		b := s[i]
		if b >= '0' && b <= '9' {
			start := i
			i++
			for i < n && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			// 合并小数部分（如 2.50），但不吞掉句末句号
			if i+1 < n && s[i] == '.' && s[i+1] >= '0' && s[i+1] <= '9' {
				i += 2
				for i < n && s[i] >= '0' && s[i] <= '9' {
					i++
				}
			}
			tokens = append(tokens, diffToken{text: s[start:i], start: start, end: i})
			continue
		}
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') {
			start := i
			i++
			for i < n && ((s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z')) {
				i++
			}
			tokens = append(tokens, diffToken{text: s[start:i], start: start, end: i})
			continue
		}
		_, sz := utf8.DecodeRuneInString(s[i:])
		if sz <= 0 {
			sz = 1
		}
		tokens = append(tokens, diffToken{text: s[i : i+sz], start: i, end: i + sz})
		i += sz
	}
	return tokens
}

// ComputeInlineTextDiffSpans 使用基于词元的 Myers 差异算法，返回旧文本与新文本中所有真实发生变更的 UTF-8 字节区间，
// 避免仅做前后缀裁剪时将两处改动之间的未变过渡文本一并高亮。
func ComputeInlineTextDiffSpans(before, after string) (beforeSpans, afterSpans []gamedata.TextMatchSpan) {
	if before == after {
		return nil, nil
	}
	if before == "" {
		return nil, []gamedata.TextMatchSpan{{Start: 0, End: len(after)}}
	}
	if after == "" {
		return []gamedata.TextMatchSpan{{Start: 0, End: len(before)}}, nil
	}

	bToks := tokenizeForTextDiff(before)
	aToks := tokenizeForTextDiff(after)
	return diffTokenSlices(bToks, aToks, 256)
}

func diffTokenSlices(bToks, aToks []diffToken, maxD int) (beforeSpans, afterSpans []gamedata.TextMatchSpan) {
	prefix := 0
	for prefix < len(bToks) && prefix < len(aToks) && bToks[prefix].text == aToks[prefix].text {
		prefix++
	}
	suffix := 0
	for suffix < len(bToks)-prefix && suffix < len(aToks)-prefix &&
		bToks[len(bToks)-1-suffix].text == aToks[len(aToks)-1-suffix].text {
		suffix++
	}

	bMid := bToks[prefix : len(bToks)-suffix]
	aMid := aToks[prefix : len(aToks)-suffix]
	if len(bMid) == 0 && len(aMid) == 0 {
		return nil, nil
	}
	if len(bMid) == 0 {
		return nil, []gamedata.TextMatchSpan{{Start: aMid[0].start, End: aMid[len(aMid)-1].end}}
	}
	if len(aMid) == 0 {
		return []gamedata.TextMatchSpan{{Start: bMid[0].start, End: bMid[len(bMid)-1].end}}, nil
	}

	bSpans, aSpans, ok := myersDiffTokens(bMid, aMid, maxD)
	if ok {
		return bSpans, aSpans
	}

	// 超大文本编辑距离超过上限时，先按行对齐再对变动行块做词元级细分，最后才退化为整体区间
	if bLines, aLines := splitTokensByNewline(bMid), splitTokensByNewline(aMid); len(bLines) > 1 || len(aLines) > 1 {
		if lb, la, lok := diffLineTokenGroups(bLines, aLines); lok {
			return lb, la
		}
	}
	return []gamedata.TextMatchSpan{{Start: bMid[0].start, End: bMid[len(bMid)-1].end}},
		[]gamedata.TextMatchSpan{{Start: aMid[0].start, End: aMid[len(aMid)-1].end}}
}

type tokenEditOp uint8

const (
	tokenOpEqual tokenEditOp = iota
	tokenOpDelete
	tokenOpInsert
)

type tokenEdit struct {
	op   tokenEditOp
	bIdx int
	aIdx int
}

// myersDiffTokens 实现 Myers O((N+M)D) 最小编辑脚本算法，在编辑距离 <= maxD 时返回精确合并后的删除/新增字节区间。
func myersDiffTokens(bToks, aToks []diffToken, maxD int) ([]gamedata.TextMatchSpan, []gamedata.TextMatchSpan, bool) {
	n, m := len(bToks), len(aToks)
	limit := min(n+m, maxD)
	offset := limit
	v := make([]int, 2*limit+1)
	trace := make([][]int, 0, min(limit+1, 64))

	foundD := -1
	for d := 0; d <= limit; d++ {
		snap := make([]int, 2*d+1)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && bToks[x].text == aToks[y].text {
				x++
				y++
			}
			v[offset+k] = x
			snap[k+d] = x
			if x >= n && y >= m {
				trace = append(trace, snap)
				foundD = d
				break
			}
		}
		if foundD >= 0 {
			break
		}
		trace = append(trace, snap)
	}
	if foundD < 0 {
		return nil, nil, false
	}

	// 反向回溯恢复编辑操作序列
	edits := make([]tokenEdit, 0, n+m)
	x, y := n, m
	for d := foundD; d > 0; d-- {
		prev := trace[d-1]
		prevOff := d - 1
		k := x - y
		var prevK int
		if k == -d || (k != d && prev[prevOff+k-1] < prev[prevOff+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := prev[prevOff+prevK]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			x--
			y--
			edits = append(edits, tokenEdit{op: tokenOpEqual, bIdx: x, aIdx: y})
		}
		if x == prevX {
			y--
			edits = append(edits, tokenEdit{op: tokenOpInsert, aIdx: y})
		} else {
			x--
			edits = append(edits, tokenEdit{op: tokenOpDelete, bIdx: x})
		}
	}
	for x > 0 && y > 0 {
		x--
		y--
		edits = append(edits, tokenEdit{op: tokenOpEqual, bIdx: x, aIdx: y})
	}

	var bSpans, aSpans []gamedata.TextMatchSpan
	for i := len(edits) - 1; i >= 0; i-- {
		ed := edits[i]
		switch ed.op {
		case tokenOpDelete:
			tok := bToks[ed.bIdx]
			if len(bSpans) > 0 && bSpans[len(bSpans)-1].End == tok.start {
				bSpans[len(bSpans)-1].End = tok.end
			} else {
				bSpans = append(bSpans, gamedata.TextMatchSpan{Start: tok.start, End: tok.end})
			}
		case tokenOpInsert:
			tok := aToks[ed.aIdx]
			if len(aSpans) > 0 && aSpans[len(aSpans)-1].End == tok.start {
				aSpans[len(aSpans)-1].End = tok.end
			} else {
				aSpans = append(aSpans, gamedata.TextMatchSpan{Start: tok.start, End: tok.end})
			}
		}
	}
	return bSpans, aSpans, true
}

type lineTokenGroup struct {
	key    string
	tokens []diffToken
}

func splitTokensByNewline(toks []diffToken) []lineTokenGroup {
	if len(toks) == 0 {
		return nil
	}
	var groups []lineTokenGroup
	start := 0
	var keyBuf strings.Builder
	for i, tok := range toks {
		keyBuf.WriteString(tok.text)
		if tok.text == "\n" || i == len(toks)-1 {
			groups = append(groups, lineTokenGroup{
				key:    keyBuf.String(),
				tokens: toks[start : i+1],
			})
			keyBuf.Reset()
			start = i + 1
		}
	}
	return groups
}

func diffLineTokenGroups(bLines, aLines []lineTokenGroup) ([]gamedata.TextMatchSpan, []gamedata.TextMatchSpan, bool) {
	bLineToks := make([]diffToken, len(bLines))
	for i, g := range bLines {
		bLineToks[i] = diffToken{text: g.key, start: i, end: i + 1}
	}
	aLineToks := make([]diffToken, len(aLines))
	for i, g := range aLines {
		aLineToks[i] = diffToken{text: g.key, start: i, end: i + 1}
	}
	bLineSpans, aLineSpans, ok := myersDiffTokens(bLineToks, aLineToks, 128)
	if !ok {
		return nil, nil, false
	}
	var bOut, aOut []gamedata.TextMatchSpan
	appendSpans := func(dst *[]gamedata.TextMatchSpan, add []gamedata.TextMatchSpan) {
		for _, sp := range add {
			if len(*dst) > 0 && (*dst)[len(*dst)-1].End == sp.Start {
				(*dst)[len(*dst)-1].End = sp.End
			} else {
				*dst = append(*dst, sp)
			}
		}
	}
	// 将行级改动块成对细分；多余块直接整块高亮
	bi, ai := 0, 0
	for bi < len(bLineSpans) || ai < len(aLineSpans) {
		switch {
		case bi < len(bLineSpans) && ai < len(aLineSpans):
			var bFlat, aFlat []diffToken
			for li := bLineSpans[bi].Start; li < bLineSpans[bi].End; li++ {
				bFlat = append(bFlat, bLines[li].tokens...)
			}
			for li := aLineSpans[ai].Start; li < aLineSpans[ai].End; li++ {
				aFlat = append(aFlat, aLines[li].tokens...)
			}
			sb, sa := diffTokenSlices(bFlat, aFlat, 128)
			appendSpans(&bOut, sb)
			appendSpans(&aOut, sa)
			bi++
			ai++
		case bi < len(bLineSpans):
			first := bLines[bLineSpans[bi].Start].tokens[0]
			lastGrp := bLines[bLineSpans[bi].End-1].tokens
			last := lastGrp[len(lastGrp)-1]
			appendSpans(&bOut, []gamedata.TextMatchSpan{{Start: first.start, End: last.end}})
			bi++
		default:
			first := aLines[aLineSpans[ai].Start].tokens[0]
			lastGrp := aLines[aLineSpans[ai].End-1].tokens
			last := lastGrp[len(lastGrp)-1]
			appendSpans(&aOut, []gamedata.TextMatchSpan{{Start: first.start, End: last.end}})
			ai++
		}
	}
	return bOut, aOut, true
}
