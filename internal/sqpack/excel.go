package sqpack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Language 表示 FF14 EXH/EXD 数据表语言枚举。
type Language uint16

const (
	LangNone                Language = 0
	LangJapanese            Language = 1
	LangEnglish             Language = 2
	LangGerman              Language = 3
	LangFrench              Language = 4
	LangChineseSimplified   Language = 5
	LangChineseTraditional  Language = 6
	LangKorean              Language = 7
	LangChineseTraditional2 Language = 8
)

var languageCodeMap = map[Language]string{
	LangNone:                "",
	LangJapanese:            "ja",
	LangEnglish:             "en",
	LangGerman:              "de",
	LangFrench:              "fr",
	LangChineseSimplified:   "chs",
	LangChineseTraditional:  "cht",
	LangKorean:              "ko",
	LangChineseTraditional2: "tc",
}

// FormatLanguage 返回语言对应的 EXD 文件名后缀代码。
func FormatLanguage(lang Language) string {
	if code, ok := languageCodeMap[lang]; ok {
		return code
	}
	return fmt.Sprintf("unk(%d)", lang)
}

// ExcelVariant 表示数据表类型（1=普通表，2=子行表）。
const (
	ExcelVariantDefault uint8 = 1
	ExcelVariantSubrows uint8 = 2
)

// EXHColumn 描述 EXH 头文件中的单个列定义。
type EXHColumn struct {
	Type   uint16
	Offset uint16
}

// EXHPagination 描述 EXH 头文件中的单个分片页定义。
type EXHPagination struct {
	StartID  uint32
	RowCount uint32
}

// EXHHeader 保存解析后的 .exh 文件头部结构。
type EXHHeader struct {
	Magic         string
	Version       uint16
	DataOffset    uint16
	ColumnCount   uint16
	PageCount     uint16
	LanguageCount uint16
	Variant       uint8
	RowCount      uint32
	Columns       []EXHColumn
	Paginations   []EXHPagination
	Languages     []Language
}

// ReadEXHHeader 解析二进制 .exh 表头文件。
func ReadEXHHeader(raw []byte) (*EXHHeader, error) {
	if len(raw) < 32 {
		return nil, errors.New("EXH 文件长度不足")
	}
	magic := string(raw[0:4])
	if magic != "EXHF" {
		return nil, fmt.Errorf("无效的 EXH 魔数: %q", magic)
	}
	h := &EXHHeader{
		Magic:         magic,
		Version:       binary.BigEndian.Uint16(raw[4:6]),
		DataOffset:    binary.BigEndian.Uint16(raw[6:8]),
		ColumnCount:   binary.BigEndian.Uint16(raw[8:10]),
		PageCount:     binary.BigEndian.Uint16(raw[10:12]),
		LanguageCount: binary.BigEndian.Uint16(raw[12:14]),
		Variant:       raw[17],
		RowCount:      binary.BigEndian.Uint32(raw[20:24]),
	}

	pos := 32
	need := pos + int(h.ColumnCount)*4 + int(h.PageCount)*8 + int(h.LanguageCount)*2
	if len(raw) < need {
		return nil, fmt.Errorf("EXH 文件内容截断: 需要 %d 字节，实际 %d 字节", need, len(raw))
	}

	h.Columns = make([]EXHColumn, h.ColumnCount)
	for i := 0; i < int(h.ColumnCount); i++ {
		h.Columns[i] = EXHColumn{
			Type:   binary.BigEndian.Uint16(raw[pos : pos+2]),
			Offset: binary.BigEndian.Uint16(raw[pos+2 : pos+4]),
		}
		pos += 4
	}

	h.Paginations = make([]EXHPagination, h.PageCount)
	for i := 0; i < int(h.PageCount); i++ {
		h.Paginations[i] = EXHPagination{
			StartID:  binary.BigEndian.Uint32(raw[pos : pos+4]),
			RowCount: binary.BigEndian.Uint32(raw[pos+4 : pos+8]),
		}
		pos += 8
	}

	// 注意：EXH 中的语言枚举按小端序 uint16 存储
	h.Languages = make([]Language, h.LanguageCount)
	for i := 0; i < int(h.LanguageCount); i++ {
		h.Languages[i] = Language(binary.LittleEndian.Uint16(raw[pos : pos+2]))
		pos += 2
	}

	return h, nil
}

// ResolveLanguageSuffix 根据表头支持的语言列表计算 EXD 文件名后缀（如 "_chs"）。
func ResolveLanguageSuffix(exh *EXHHeader, preferred Language) string {
	var chosen Language
	found := false
	for _, l := range exh.Languages {
		if l == preferred {
			chosen = l
			found = true
			break
		}
	}
	if !found && len(exh.Languages) > 0 {
		chosen = exh.Languages[0]
		found = true
	}
	if !found || chosen == LangNone {
		return ""
	}
	code := FormatLanguage(chosen)
	if code != "" && !strings.HasPrefix(code, "unk(") {
		return "_" + code
	}
	return ""
}

// Row 表示 EXD 数据表中的单行稀疏或完整列值数组。
type Row []any

// SubRow 表示 Variant=2 子行表中的单个子行记录。
type SubRow struct {
	SubRowID uint16
	Data     Row
}

// IntAt 安全读取行中指定列为 int。
func (r Row) IntAt(idx int) int {
	if idx < 0 || idx >= len(r) || r[idx] == nil {
		return 0
	}
	switch v := r[idx].(type) {
	case int:
		return v
	case int8:
		return int(v)
	case uint8:
		return int(v)
	case int16:
		return int(v)
	case uint16:
		return int(v)
	case int32:
		return int(v)
	case uint32:
		return int(v)
	case int64:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case bool:
		if v {
			return 1
		}
		return 0
	default:
		return 0
	}
}

// FloatAt 安全读取行中指定列为 float64。
func (r Row) FloatAt(idx int) float64 {
	if idx < 0 || idx >= len(r) || r[idx] == nil {
		return 0
	}
	switch v := r[idx].(type) {
	case float32:
		return float64(v)
	case float64:
		return v
	default:
		return float64(r.IntAt(idx))
	}
}

// BoolAt 安全读取行中指定列为 bool。
func (r Row) BoolAt(idx int) bool {
	if idx < 0 || idx >= len(r) || r[idx] == nil {
		return false
	}
	if b, ok := r[idx].(bool); ok {
		return b
	}
	return r.IntAt(idx) != 0
}

// StringAt 将行中指定列的原始字节解码为可读纯文本（支持 SeString 去除控制标签）。
func (r Row) StringAt(idx int) string {
	if idx < 0 || idx >= len(r) || r[idx] == nil {
		return ""
	}
	switch v := r[idx].(type) {
	case []byte:
		return DecodeSeString(v)
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

// StringDisplayAt 将行中指定列解码为面向界面展示的可读文本，并展开表引用与玩家占位符。
func (r Row) StringDisplayAt(idx int, resolve SeSheetResolver) string {
	return r.StringDisplayAtLang(idx, LangChineseSimplified, resolve)
}

// StringDisplayAtLang 按指定语言将行中指定列解码为面向界面展示的可读文本。
func (r Row) StringDisplayAtLang(idx int, lang Language, resolve SeSheetResolver) string {
	if idx < 0 || idx >= len(r) || r[idx] == nil {
		return ""
	}
	switch v := r[idx].(type) {
	case []byte:
		return DecodeSeStringDisplayForLang(v, lang, resolve)
	case string:
		return CleanPUARunes(v)
	default:
		return ""
	}
}

// EXDReader 解析单个 .exd 分片文件。
type EXDReader struct {
	data        []byte
	offsetMap   map[uint32]uint32
	rowIDs      []uint32
	rowDataSize int
	variant     uint8
	columns     []EXHColumn
}

// NewEXDReader 根据 .exd 二进制数据与对应 EXH 表头创建读取器。
func NewEXDReader(data []byte, exh *EXHHeader) (*EXDReader, error) {
	if len(data) < 32 {
		return nil, errors.New("EXD 文件长度不足")
	}
	if string(data[0:4]) != "EXDF" {
		return nil, fmt.Errorf("无效的 EXD 魔数: %q", string(data[0:4]))
	}
	indexSize := binary.BigEndian.Uint32(data[8:12])
	if 32+int(indexSize) > len(data) {
		return nil, errors.New("EXD 行索引表越界")
	}
	count := int(indexSize) / 8
	offsetMap := make(map[uint32]uint32, count)
	rowIDs := make([]uint32, 0, count)
	pos := 32
	for i := 0; i < count; i++ {
		rowID := binary.BigEndian.Uint32(data[pos : pos+4])
		off := binary.BigEndian.Uint32(data[pos+4 : pos+8])
		offsetMap[rowID] = off
		rowIDs = append(rowIDs, rowID)
		pos += 8
	}
	return &EXDReader{
		data:        data,
		offsetMap:   offsetMap,
		rowIDs:      rowIDs,
		rowDataSize: int(exh.DataOffset),
		variant:     exh.Variant,
		columns:     exh.Columns,
	}, nil
}

// ListRowIDs 返回当前分片内的全部行 ID 列表（升序）。
func (r *EXDReader) ListRowIDs() []uint32 {
	out := make([]uint32, len(r.rowIDs))
	copy(out, r.rowIDs)
	slices.Sort(out)
	return out
}

func (r *EXDReader) readRowHeader(rowID uint32) (dataOffset int, rowCount int, err error) {
	hdrOff, ok := r.offsetMap[rowID]
	if !ok {
		return 0, 0, fmt.Errorf("未找到行 ID %d", rowID)
	}
	if int(hdrOff)+6 > len(r.data) {
		return 0, 0, fmt.Errorf("行头偏移越界: rowID=%d", rowID)
	}
	rc := int(binary.BigEndian.Uint16(r.data[hdrOff+4 : hdrOff+6]))
	return int(hdrOff) + 6, rc, nil
}

// ReadRow 读取 Variant=1 普通表的单行数据；可传 columnIndexes 仅解析部分列。
func (r *EXDReader) ReadRow(rowID uint32, columnIndexes []int) (Row, error) {
	off, rowCount, err := r.readRowHeader(rowID)
	if err != nil {
		return nil, err
	}
	if rowCount != 1 {
		return nil, fmt.Errorf("行 %d 包含 %d 条记录，期望 1 条", rowID, rowCount)
	}
	return r.readColumns(off, columnIndexes)
}

// GetSubRowCount 返回 Variant=2 子行表指定父行的子行数量。
func (r *EXDReader) GetSubRowCount(rowID uint32) (int, error) {
	_, rowCount, err := r.readRowHeader(rowID)
	return rowCount, err
}

// ReadSubrow 读取 Variant=2 子行表的指定子行数据。
func (r *EXDReader) ReadSubrow(rowID uint32, subIdx int, columnIndexes []int) (SubRow, error) {
	off, rowCount, err := r.readRowHeader(rowID)
	if err != nil {
		return SubRow{}, err
	}
	if subIdx < 0 || subIdx >= rowCount {
		return SubRow{}, fmt.Errorf("子行索引越界: %d (总数 %d)", subIdx, rowCount)
	}
	subOff := off + subIdx*(r.rowDataSize+2)
	if subOff+2 > len(r.data) {
		return SubRow{}, fmt.Errorf("子行头越界: row=%d sub=%d", rowID, subIdx)
	}
	subID := binary.BigEndian.Uint16(r.data[subOff : subOff+2])
	cols, err := r.readColumns(subOff+2, columnIndexes)
	if err != nil {
		return SubRow{}, err
	}
	return SubRow{SubRowID: subID, Data: cols}, nil
}

func (r *EXDReader) readColumns(rowOffset int, columnIndexes []int) (Row, error) {
	if len(columnIndexes) > 0 {
		maxIdx := 0
		for _, idx := range columnIndexes {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		vals := make(Row, maxIdx+1)
		for _, idx := range columnIndexes {
			if idx < 0 || idx >= len(r.columns) {
				return nil, fmt.Errorf("列索引越界: %d", idx)
			}
			v, err := r.readColumn(rowOffset, r.columns[idx])
			if err != nil {
				return nil, err
			}
			vals[idx] = v
		}
		return vals, nil
	}

	vals := make(Row, len(r.columns))
	for i, col := range r.columns {
		v, err := r.readColumn(rowOffset, col)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}

func (r *EXDReader) readColumn(rowOffset int, col EXHColumn) (any, error) {
	off := rowOffset + int(col.Offset)
	switch col.Type {
	case 0: // String
		if off+4 > len(r.data) {
			return []byte(nil), nil
		}
		strOff := int(binary.BigEndian.Uint32(r.data[off : off+4]))
		start := rowOffset + r.rowDataSize + strOff
		if start < 0 || start >= len(r.data) {
			return []byte(nil), nil
		}
		relEnd := bytes.IndexByte(r.data[start:], 0)
		if relEnd < 0 {
			out := make([]byte, len(r.data)-start)
			copy(out, r.data[start:])
			return out, nil
		}
		out := make([]byte, relEnd)
		copy(out, r.data[start:start+relEnd])
		return out, nil
	case 1: // Bool
		if off+1 > len(r.data) {
			return false, nil
		}
		return r.data[off] == 1, nil
	case 2: // Int8
		if off+1 > len(r.data) {
			return int8(0), nil
		}
		return int8(r.data[off]), nil
	case 3: // UInt8
		if off+1 > len(r.data) {
			return uint8(0), nil
		}
		return r.data[off], nil
	case 4: // Int16
		if off+2 > len(r.data) {
			return int16(0), nil
		}
		return int16(binary.BigEndian.Uint16(r.data[off : off+2])), nil
	case 5: // UInt16
		if off+2 > len(r.data) {
			return uint16(0), nil
		}
		return binary.BigEndian.Uint16(r.data[off : off+2]), nil
	case 6: // Int32
		if off+4 > len(r.data) {
			return int32(0), nil
		}
		return int32(binary.BigEndian.Uint32(r.data[off : off+4])), nil
	case 7: // UInt32
		if off+4 > len(r.data) {
			return uint32(0), nil
		}
		return binary.BigEndian.Uint32(r.data[off : off+4]), nil
	case 9: // Float32
		if off+4 > len(r.data) {
			return float32(0), nil
		}
		return math.Float32frombits(binary.BigEndian.Uint32(r.data[off : off+4])), nil
	case 10: // Int64
		if off+8 > len(r.data) {
			return int64(0), nil
		}
		return int64(binary.BigEndian.Uint64(r.data[off : off+8])), nil
	case 11: // UInt64
		if off+8 > len(r.data) {
			return uint64(0), nil
		}
		return binary.BigEndian.Uint64(r.data[off : off+8]), nil
	case 25, 26, 27, 28, 29, 30, 31, 32: // PackedBool0..7
		if off+1 > len(r.data) {
			return false, nil
		}
		mask := byte(1 << (col.Type - 25))
		return (r.data[off] & mask) != 0, nil
	default:
		return nil, fmt.Errorf("不支持的 EXH 列类型: %d", col.Type)
	}
}

// EXLEntry 表示 root.exl 中的单张数据表目录项。
type EXLEntry struct {
	Name string
	ID   int
}

// EXLFile 表示解析后的 exd/root.exl 文件。
type EXLFile struct {
	Magic   string
	Version int
	Entries []EXLEntry
}

// ReadEXLFile 解析 exd/root.exl 文本清单文件。
func ReadEXLFile(raw []byte) *EXLFile {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	out := &EXLFile{}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ",", 2)
		if i == 0 {
			out.Magic = parts[0]
			if len(parts) > 1 {
				out.Version, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
			}
			continue
		}
		if len(parts) < 2 {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name != "" {
			out.Entries = append(out.Entries, EXLEntry{Name: name, ID: id})
		}
	}
	return out
}

// LoadSheetRows 读取普通表（Variant=1）的全部行数据，按分页并发解码。
func LoadSheetRows(reader FileReader, sheetName string, preferredLang Language, columnIndexes []int) (map[int]Row, error) {
	if isNilFileReader(reader) {
		return make(map[int]Row), nil
	}
	lower := strings.ToLower(sheetName)
	exhBuf, err := reader.ReadFile("exd/" + lower + ".exh")
	if err != nil {
		return nil, err
	}
	if exhBuf == nil {
		return nil, fmt.Errorf("未找到数据表头文件: exd/%s.exh", lower)
	}
	exh, err := ReadEXHHeader(exhBuf)
	if err != nil {
		return nil, err
	}
	langSuffix := ResolveLanguageSuffix(exh, preferredLang)

	type pageResult struct {
		rows map[int]Row
		err  error
	}

	results := make([]pageResult, len(exh.Paginations))
	var wg sync.WaitGroup
	for idx, page := range exh.Paginations {
		wg.Add(1)
		go func(i int, startID uint32) {
			defer wg.Done()
			exdPath := fmt.Sprintf("exd/%s_%d%s.exd", lower, startID, langSuffix)
			exdBuf, rerr := reader.ReadFile(exdPath)
			if rerr != nil {
				results[i].err = rerr
				return
			}
			if exdBuf == nil {
				return
			}
			exd, perr := NewEXDReader(exdBuf, exh)
			if perr != nil {
				results[i].err = perr
				return
			}
			local := make(map[int]Row, len(exd.rowIDs))
			for _, rowID := range exd.rowIDs {
				row, rrErr := exd.ReadRow(rowID, columnIndexes)
				if rrErr != nil {
					continue
				}
				local[int(rowID)] = row
			}
			results[i].rows = local
		}(idx, page.StartID)
	}
	wg.Wait()

	merged := make(map[int]Row, exh.RowCount)
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		for id, row := range r.rows {
			merged[id] = row
		}
	}
	return merged, nil
}

// LoadSubrowSheet 读取子行表（Variant=2）的全部父行及子行数据。
func LoadSubrowSheet(reader FileReader, sheetName string, preferredLang Language) (map[int][]SubRow, error) {
	if isNilFileReader(reader) {
		return make(map[int][]SubRow), nil
	}
	lower := strings.ToLower(sheetName)
	exhBuf, err := reader.ReadFile("exd/" + lower + ".exh")
	if err != nil {
		return nil, err
	}
	if exhBuf == nil {
		return nil, fmt.Errorf("未找到子行表头文件: exd/%s.exh", lower)
	}
	exh, err := ReadEXHHeader(exhBuf)
	if err != nil {
		return nil, err
	}
	langSuffix := ResolveLanguageSuffix(exh, preferredLang)

	type pageResult struct {
		rows map[int][]SubRow
		err  error
	}

	results := make([]pageResult, len(exh.Paginations))
	var wg sync.WaitGroup
	for idx, page := range exh.Paginations {
		wg.Add(1)
		go func(i int, startID uint32) {
			defer wg.Done()
			exdPath := fmt.Sprintf("exd/%s_%d%s.exd", lower, startID, langSuffix)
			exdBuf, rerr := reader.ReadFile(exdPath)
			if rerr != nil {
				results[i].err = rerr
				return
			}
			if exdBuf == nil {
				return
			}
			exd, perr := NewEXDReader(exdBuf, exh)
			if perr != nil {
				results[i].err = perr
				return
			}
			local := make(map[int][]SubRow, len(exd.rowIDs))
			for _, rowID := range exd.rowIDs {
				cnt, cerr := exd.GetSubRowCount(rowID)
				if cerr != nil {
					continue
				}
				subs := make([]SubRow, 0, cnt)
				for s := 0; s < cnt; s++ {
					sr, serr := exd.ReadSubrow(rowID, s, nil)
					if serr != nil {
						continue
					}
					subs = append(subs, sr)
				}
				local[int(rowID)] = subs
			}
			results[i].rows = local
		}(idx, page.StartID)
	}
	wg.Wait()

	merged := make(map[int][]SubRow)
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		for id, subs := range r.rows {
			merged[id] = subs
		}
	}
	return merged, nil
}
