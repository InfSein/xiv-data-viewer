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
