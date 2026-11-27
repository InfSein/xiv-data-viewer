package sqpack

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// EXDStringSheetMeta 描述包含文本列的 EXD 数据表元信息。
type EXDStringSheetMeta struct {
	SheetIdx    int
	Name        string
	LowerName   string
	Variant     uint8
	IsLocalized bool
	LangSuffix  string
	ColumnCount int
	StringCols  []EXHColumn
	StringIdx   []int // 每个 StringCols 元素在 EXH.Columns 中的原始列序号
}

// EXDStringCellRef 表示单条解包后的非空文本单元格在分片字节池中的紧凑定位（零指针，避免百万级堆 string 分配）。
type EXDStringCellRef struct {
	SheetIdx uint16
	ColIdx   uint16
	RowID    uint32
	SubRowID int16 // 普通表固定为 -1，子行表为子行序号
	TextOff  uint32
	TextLen  uint32
}

// EXDStringShard 保存单个并发工作协程提取出的连续文本字节池与单元格定位切片。
type EXDStringShard struct {
	Blob  []byte
	Cells []EXDStringCellRef
}

// EXDTextScanOptions 控制全量 EXD 文本并发扫描选项。
type EXDTextScanOptions struct {
	PreferredLang Language
	ResolveSheet  SeSheetResolver
	// LocalizedOnly 为 true 时仅扫描具备目标语言本地化分片的数据表，跳过无语言通用表（用于同客户端多语言扫描去重）。
	LocalizedOnly bool
	// SheetNames 为空时自动读取 exd/root.exl 获取全量数据表清单。
	SheetNames []string
	// Workers 为并发解压协程数，<=0 时自动使用 CPU 核心数。
	Workers int
	// OnProgress 定期上报扫描进度（已处理分片数、总分片数、累计提取文本条数）。
	OnProgress func(done, total, texts int)
}

// EXDTextScanResult 保存全量 EXD 文本扫描得到的表目录与各协程分片结果。
type EXDTextScanResult struct {
	Sheets    []EXDStringSheetMeta
	Shards    []EXDStringShard
	TotalRows int
}

// readStandardFileFast 使用复用的解压器与单次连续区间读取加速标准文件解压，
// 显著降低扫描数万个 EXH/EXD 小文件时的系统调用与堆分配开销。
func (a *ArchiveReader) readStandardFileFast(entry indexEntry, flatePool *sync.Pool, spanBuf, outBuf *[]byte) ([]byte, error) {
	f, err := a.getDataFile(entry.dataFileID)
	if err != nil {
		return nil, err
	}
	return readStandardFileAtOffset(f, int64(entry.offset), flatePool, spanBuf, outBuf)
}

func readStandardFileAtOffset(f *os.File, fileOffset int64, flatePool *sync.Pool, spanBuf, outBuf *[]byte) ([]byte, error) {
	var hdrStack [256]byte
	n, rErr := f.ReadAt(hdrStack[:], fileOffset)
	if n < 24 || (rErr != nil && !errors.Is(rErr, io.EOF)) {
		return nil, fmt.Errorf("读取标准文件头失败: %w", rErr)
	}
	fiSize := binary.LittleEndian.Uint32(hdrStack[0:4])
	fileType := binary.LittleEndian.Uint32(hdrStack[4:8])
	rawFileSize := binary.LittleEndian.Uint32(hdrStack[8:12])
	if fileType == fileTypeEmpty || rawFileSize == 0 {
		return nil, nil
	}
	if fileType != fileTypeStandard || fiSize < 24 {
		return nil, nil
	}

	var fiBuf []byte
	if int(fiSize) <= n {
		fiBuf = hdrStack[:fiSize]
	} else {
		fiBuf = make([]byte, fiSize)
		copy(fiBuf, hdrStack[:n])
		if err := readAtFull(f, fiBuf[n:], fileOffset+int64(n)); err != nil {
			return nil, err
		}
	}

	chunkCount := int(binary.LittleEndian.Uint32(fiBuf[20:24]))
	pos := 24
	if chunkCount == 0 || pos+chunkCount*8 > len(fiBuf) {
		return nil, nil
	}

	// 计算所有分块覆盖的最大连续偏移，支持一次 ReadAt 整块读入内存
	var maxEnd uint64
	for i := 0; i < chunkCount; i++ {
		off := binary.LittleEndian.Uint32(fiBuf[pos : pos+4])
		comp := binary.LittleEndian.Uint16(fiBuf[pos+4 : pos+6])
		if end := uint64(off) + uint64(comp); end > maxEnd {
			maxEnd = end
		}
		pos += 8
	}

	baseOffset := fileOffset + int64(fiSize)
	const maxSingleSpan = 32 << 20 // 单个 EXD 分片上限 32MB
	if maxEnd > 0 && maxEnd <= maxSingleSpan {
		var span []byte
		if spanBuf != nil {
			if cap(*spanBuf) < int(maxEnd) {
				*spanBuf = make([]byte, maxEnd)
			}
			span = (*spanBuf)[:maxEnd]
		} else {
			span = make([]byte, maxEnd)
		}
		if err := readAtFull(f, span, baseOffset); err == nil {
			var out []byte
			if outBuf != nil {
				if cap(*outBuf) < int(rawFileSize) {
					*outBuf = make([]byte, 0, rawFileSize)
				}
				out = (*outBuf)[:0]
			} else {
				out = make([]byte, 0, rawFileSize)
			}
			pos = 24
			allOK := true
			for i := 0; i < chunkCount; i++ {
				ciOff := int(binary.LittleEndian.Uint32(fiBuf[pos : pos+4]))
				pos += 8
				if ciOff+16 > len(span) {
					allOK = false
					break
				}
				hdrSize := int(binary.LittleEndian.Uint32(span[ciOff : ciOff+4]))
				compSize := int(binary.LittleEndian.Uint32(span[ciOff+8 : ciOff+12]))
				uncompSize := int(binary.LittleEndian.Uint32(span[ciOff+12 : ciOff+16]))
				dataStart := ciOff + hdrSize
				if compSize == uncompressedChunkSz {
					if dataStart+uncompSize > len(span) {
						allOK = false
						break
					}
					out = append(out, span[dataStart:dataStart+uncompSize]...)
					continue
				}
				if dataStart+compSize > len(span) {
					allOK = false
					break
				}
				curLen := len(out)
				if cap(out)-curLen < uncompSize {
					grown := make([]byte, curLen, curLen+uncompSize)
					copy(grown, out)
					out = grown
				}
				dst := out[curLen : curLen+uncompSize]
				if err := inflatePooled(flatePool, span[dataStart:dataStart+compSize], dst); err != nil {
					allOK = false
					break
				}
				out = out[:curLen+uncompSize]
			}
			if allOK && uint32(len(out)) == rawFileSize {
				if outBuf != nil {
					*outBuf = out
				}
				return out, nil
			}
		}
	}

	// 回退到常规逐块解压路径
	fi := fileInfo{
		size:        fiSize,
		fileType:    fileType,
		rawFileSize: rawFileSize,
	}
	return readStandardFile(f, fileOffset, fi, fiBuf)
}

func inflatePooled(pool *sync.Pool, compressed, dst []byte) error {
	br := bytes.NewReader(compressed)
	var fr io.ReadCloser
	if pool != nil {
		if v := pool.Get(); v != nil {
			fr = v.(io.ReadCloser)
			if r, ok := fr.(flate.Resetter); ok {
				if err := r.Reset(br, nil); err != nil {
					fr = flate.NewReader(br)
				}
			} else {
				fr = flate.NewReader(br)
			}
		}
	}
	if fr == nil {
		fr = flate.NewReader(br)
	}
	_, err := io.ReadFull(fr, dst)
	if pool != nil {
		pool.Put(fr)
	} else {
		_ = fr.Close()
	}
	return err
}

// resolvePreferredSheetLang 为文本检索严格挑选目标语言后缀：
// 若表支持 preferredLang 则返回本地化后缀；若表无多语言且支持 LangNone 则返回空后缀；否则标记为不匹配并跳过。
func resolvePreferredSheetLang(exh *EXHHeader, preferred Language) (suffix string, isLocalized bool, ok bool) {
	if exh == nil || len(exh.Languages) == 0 {
		return "", false, false
	}
	if preferred != LangNone {
		for _, l := range exh.Languages {
			if l == preferred {
				code := FormatLanguage(l)
				if code != "" && !strings.HasPrefix(code, "unk(") {
					return "_" + code, true, true
				}
				return "", true, true
			}
		}
	}
	for _, l := range exh.Languages {
		if l == LangNone {
			return "", false, true
		}
	}
	if preferred == LangNone {
		first := exh.Languages[0]
		code := FormatLanguage(first)
		if code != "" && !strings.HasPrefix(code, "unk(") {
			return "_" + code, true, true
		}
		return "", false, true
	}
	return "", false, false
}

// appendCleanedScannedText 将原始 EXD 字符串清洗后直接追加到连续字节缓冲区，
// 常规 UTF-8/ASCII 文本走零堆分配快速路径，返回追加后的起始偏移与长度。
func appendCleanedScannedText(blob *[]byte, raw []byte, lang Language, resolve SeSheetResolver) (uint32, uint32) {
	if len(raw) == 0 {
		return 0, 0
	}
	if bytes.IndexByte(raw, sePayloadStart) >= 0 {
		txt := DecodeSeStringDisplayForLang(raw, lang, resolve)
		if txt == "" {
			return 0, 0
		}
		off := uint32(len(*blob))
		*blob = append(*blob, txt...)
		return off, uint32(len(txt))
	}

	// 快速修剪首尾 ASCII 空白
	start := 0
	end := len(raw)
	for start < end && isASCIIWhitespace(raw[start]) {
		start++
	}
	for end > start && isASCIIWhitespace(raw[end-1]) {
		end--
	}
	if start >= end {
		return 0, 0
	}
	trimmed := raw[start:end]

	hasNonASCII := false
	for _, b := range trimmed {
		if b >= 0x80 {
			hasNonASCII = true
			break
		}
	}
	if !hasNonASCII {
		off := uint32(len(*blob))
		*blob = append(*blob, trimmed...)
		return off, uint32(len(trimmed))
	}

	// 扫描 UTF-8 文本，仅在含私用区字形或首尾全角空白时才走清洗路径
	hasPUA := false
	for i := 0; i < len(trimmed); {
		b := trimmed[i]
		if b < 0x80 {
			i++
			continue
		}
		r, sz := utf8.DecodeRune(trimmed[i:])
		if r >= 0xE000 && r <= 0xF8FF {
			hasPUA = true
			break
		}
		i += sz
	}
	if !hasPUA {
		// 检查首尾是否为非 ASCII 空白（如 U+3000 全角空格），常见情形直接零分配追加
		firstR, _ := utf8.DecodeRune(trimmed)
		lastR, _ := utf8.DecodeLastRune(trimmed)
		if firstR != 0x3000 && lastR != 0x3000 && firstR != 0x00A0 && lastR != 0x00A0 {
			off := uint32(len(*blob))
			*blob = append(*blob, trimmed...)
			return off, uint32(len(trimmed))
		}
		txt := strings.TrimSpace(string(trimmed))
		if txt == "" {
			return 0, 0
		}
		off := uint32(len(*blob))
		*blob = append(*blob, txt...)
		return off, uint32(len(txt))
	}

	txt := CleanPUARunes(string(trimmed))
	if txt == "" {
		return 0, 0
	}
	off := uint32(len(*blob))
	*blob = append(*blob, txt...)
	return off, uint32(len(txt))
}

func isASCIIWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

// extractEXDStrings 直接从二进制 EXD 分片按列偏移提取非空 String 单元格并写入连续字节池，跳过非文本列与堆 string 装箱。
func extractEXDStrings(
	exdData []byte,
	sheet EXDStringSheetMeta,
	dataOffset int,
	lang Language,
	resolve SeSheetResolver,
	shard *EXDStringShard,
) int {
	if len(exdData) < 32 || len(sheet.StringCols) == 0 {
		return 0
	}
	if exdData[0] != 'E' || exdData[1] != 'X' || exdData[2] != 'D' || exdData[3] != 'F' {
		return 0
	}
	indexSize := int(binary.BigEndian.Uint32(exdData[8:12]))
	if 32+indexSize > len(exdData) {
		return 0
	}
	count := indexSize / 8
	sheetIdx := uint16(sheet.SheetIdx)
	added := 0

	pos := 32
	if sheet.Variant == ExcelVariantSubrows {
		for i := 0; i < count; i++ {
			rowID := binary.BigEndian.Uint32(exdData[pos : pos+4])
			hdrOff := int(binary.BigEndian.Uint32(exdData[pos+4 : pos+8]))
			pos += 8
			if hdrOff+6 > len(exdData) {
				continue
			}
			subCnt := int(binary.BigEndian.Uint16(exdData[hdrOff+4 : hdrOff+6]))
			baseOff := hdrOff + 6
			for s := 0; s < subCnt; s++ {
				subOff := baseOff + s*(dataOffset+2)
				if subOff+2+dataOffset > len(exdData) {
					break
				}
				subID := binary.BigEndian.Uint16(exdData[subOff : subOff+2])
				rowOff := subOff + 2
				strBase := rowOff + dataOffset
				for ci, col := range sheet.StringCols {
					off := rowOff + int(col.Offset)
					if off+4 > len(exdData) {
						continue
					}
					strOff := int(binary.BigEndian.Uint32(exdData[off : off+4]))
					start := strBase + strOff
					if start < 0 || start >= len(exdData) || exdData[start] == 0 {
						continue
					}
					relEnd := bytes.IndexByte(exdData[start:], 0)
					var raw []byte
					if relEnd < 0 {
						raw = exdData[start:]
					} else if relEnd == 0 {
						continue
					} else {
						raw = exdData[start : start+relEnd]
					}
					tOff, tLen := appendCleanedScannedText(&shard.Blob, raw, lang, resolve)
					if tLen == 0 {
						continue
					}
					shard.Cells = append(shard.Cells, EXDStringCellRef{
						SheetIdx: sheetIdx,
						ColIdx:   uint16(sheet.StringIdx[ci]),
						RowID:    rowID,
						SubRowID: int16(subID),
						TextOff:  tOff,
						TextLen:  tLen,
					})
					added++
				}
			}
		}
		return added
	}

	// 普通表 (Variant == 1)
	for i := 0; i < count; i++ {
		rowID := binary.BigEndian.Uint32(exdData[pos : pos+4])
		hdrOff := int(binary.BigEndian.Uint32(exdData[pos+4 : pos+8]))
		pos += 8
		if hdrOff+6 > len(exdData) {
			continue
		}
		rc := binary.BigEndian.Uint16(exdData[hdrOff+4 : hdrOff+6])
		if rc != 1 {
			continue
		}
		rowOff := hdrOff + 6
		strBase := rowOff + dataOffset
		if strBase > len(exdData) {
			continue
		}
		for ci, col := range sheet.StringCols {
			off := rowOff + int(col.Offset)
			if off+4 > len(exdData) {
				continue
			}
			strOff := int(binary.BigEndian.Uint32(exdData[off : off+4]))
			start := strBase + strOff
			if start < 0 || start >= len(exdData) || exdData[start] == 0 {
				continue
			}
			relEnd := bytes.IndexByte(exdData[start:], 0)
			var raw []byte
			if relEnd < 0 {
				raw = exdData[start:]
			} else if relEnd == 0 {
				continue
			} else {
				raw = exdData[start : start+relEnd]
			}
			tOff, tLen := appendCleanedScannedText(&shard.Blob, raw, lang, resolve)
			if tLen == 0 {
				continue
			}
			shard.Cells = append(shard.Cells, EXDStringCellRef{
				SheetIdx: sheetIdx,
				ColIdx:   uint16(sheet.StringIdx[ci]),
				RowID:    rowID,
				SubRowID: -1,
				TextOff:  tOff,
				TextLen:  tLen,
			})
			added++
		}
	}
	return added
}

// ScanAllEXDStrings 只读扫描客户端或历史快照中全部包含文本列的 EXD 数据表与分页，
// 多核并发提取全部非空字符串单元格至无指针分片字节池。
func ScanAllEXDStrings(ctx context.Context, reader FileReader, opts EXDTextScanOptions) (*EXDTextScanResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if isNilFileReader(reader) {
		return &EXDTextScanResult{}, nil
	}
	if gr, ok := reader.(*GameReader); ok {
		return scanAllEXDStringsFromGameReader(ctx, gr, opts)
	}
	return scanAllEXDStringsFromFileReader(ctx, reader, opts)
}

func scanAllEXDStringsFromFileReader(ctx context.Context, reader FileReader, opts EXDTextScanOptions) (*EXDTextScanResult, error) {
	sheetNames := opts.SheetNames
	if len(sheetNames) == 0 {
		exlRaw, err := reader.ReadFile("exd/root.exl")
		if err != nil {
			return nil, fmt.Errorf("读取 exd/root.exl 失败: %w", err)
		}
		if len(exlRaw) == 0 {
			return &EXDTextScanResult{}, nil
		}
		exl := ReadEXLFile(exlRaw)
		sheetNames = make([]string, 0, len(exl.Entries))
		for _, e := range exl.Entries {
			if e.Name != "" {
				sheetNames = append(sheetNames, e.Name)
			}
		}
	}
	if len(sheetNames) == 0 {
		return &EXDTextScanResult{}, nil
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers < 2 {
			workers = 2
		}
		if workers > 16 {
			workers = 16
		}
	}

	type genericEXHTask struct {
		origIdx int
		name    string
		lower   string
		path    string
	}
	exhTasks := make([]genericEXHTask, 0, len(sheetNames))
	for i, name := range sheetNames {
		lower := strings.ToLower(name)
		exhTasks = append(exhTasks, genericEXHTask{
			origIdx: i,
			name:    name,
			lower:   lower,
			path:    "exd/" + lower + ".exh",
		})
	}
	// 按路径升序排列，最大化历史快照分块压缩缓存命中率
	sort.Slice(exhTasks, func(i, j int) bool {
		return exhTasks[i].path < exhTasks[j].path
	})

	type parsedSheetPlan struct {
		origIdx     int
		meta        EXDStringSheetMeta
		dataOffset  int
		paginations []EXHPagination
	}
	plansByIdx := make([]*parsedSheetPlan, len(sheetNames))

	var exhCursor atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				idx := int(exhCursor.Add(1) - 1)
				if idx >= len(exhTasks) {
					return
				}
				t := exhTasks[idx]
				raw, err := reader.ReadFile(t.path)
				if err != nil || len(raw) < 32 {
					continue
				}
				exh, err := ReadEXHHeader(raw)
				if err != nil || len(exh.Paginations) == 0 {
					continue
				}
				var strCols []EXHColumn
				var strIdx []int
				for ci, col := range exh.Columns {
					if col.Type == 0 {
						strCols = append(strCols, col)
						strIdx = append(strIdx, ci)
					}
				}
				if len(strCols) == 0 {
					continue
				}
				langSuffix, isLoc, ok := resolvePreferredSheetLang(exh, opts.PreferredLang)
				if !ok || (opts.LocalizedOnly && !isLoc) {
					continue
				}
				plansByIdx[t.origIdx] = &parsedSheetPlan{
					origIdx: t.origIdx,
					meta: EXDStringSheetMeta{
						Name:        t.name,
						LowerName:   t.lower,
						Variant:     exh.Variant,
						IsLocalized: isLoc,
						LangSuffix:  langSuffix,
						ColumnCount: int(exh.ColumnCount),
						StringCols:  strCols,
						StringIdx:   strIdx,
					},
					dataOffset:  int(exh.DataOffset),
					paginations: exh.Paginations,
				}
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	sheets := make([]EXDStringSheetMeta, 0, len(exhTasks)/2)
	sheetDataOffsets := make([]int, 0, len(exhTasks)/2)

	type genericEXDTask struct {
		sheetIdx int
		path     string
	}
	exdTasks := make([]genericEXDTask, 0, len(exhTasks))
	for _, p := range plansByIdx {
		if p == nil {
			continue
		}
		sIdx := len(sheets)
		p.meta.SheetIdx = sIdx
		sheets = append(sheets, p.meta)
		sheetDataOffsets = append(sheetDataOffsets, p.dataOffset)
		for _, pg := range p.paginations {
			exdTasks = append(exdTasks, genericEXDTask{
				sheetIdx: sIdx,
				path:     fmt.Sprintf("exd/%s_%d%s.exd", p.meta.LowerName, pg.StartID, p.meta.LangSuffix),
			})
		}
	}
	if len(exdTasks) == 0 {
		return &EXDTextScanResult{Sheets: sheets}, nil
	}

	sort.Slice(exdTasks, func(i, j int) bool {
		return exdTasks[i].path < exdTasks[j].path
	})

	totalTasks := len(exdTasks)
	if opts.OnProgress != nil {
		opts.OnProgress(0, totalTasks, 0)
	}

	shards := make([]EXDStringShard, workers)
	var doneTasks atomic.Int64
	var totalTexts atomic.Int64
	var taskCursor atomic.Int64
	var progMu sync.Mutex
	lastReport := time.Now()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			local := EXDStringShard{
				Blob:  make([]byte, 0, 512*1024),
				Cells: make([]EXDStringCellRef, 0, 16384),
			}
			const batchSize = 16
			for {
				if ctx.Err() != nil {
					return
				}
				start := int(taskCursor.Add(batchSize) - batchSize)
				if start >= totalTasks {
					break
				}
				end := start + batchSize
				if end > totalTasks {
					end = totalTasks
				}
				batchTexts := 0
				for i := start; i < end; i++ {
					if ctx.Err() != nil {
						return
					}
					t := exdTasks[i]
					raw, err := reader.ReadFile(t.path)
					if err != nil || len(raw) < 32 {
						continue
					}
					batchTexts += extractEXDStrings(
						raw,
						sheets[t.sheetIdx],
						sheetDataOffsets[t.sheetIdx],
						opts.PreferredLang,
						opts.ResolveSheet,
						&local,
					)
				}
				done := int(doneTasks.Add(int64(end - start)))
				texts := int(totalTexts.Add(int64(batchTexts)))
				if opts.OnProgress != nil {
					progMu.Lock()
					if done >= totalTasks || time.Since(lastReport) >= 80*time.Millisecond {
						lastReport = time.Now()
						opts.OnProgress(done, totalTasks, texts)
					}
					progMu.Unlock()
				}
			}
			shards[workerID] = local
		}(w)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	totalRows := int(totalTexts.Load())
	if opts.OnProgress != nil {
		opts.OnProgress(totalTasks, totalTasks, totalRows)
	}
	return &EXDTextScanResult{
		Sheets:    sheets,
		Shards:    shards,
		TotalRows: totalRows,
	}, nil
}

func scanAllEXDStringsFromGameReader(ctx context.Context, reader *GameReader, opts EXDTextScanOptions) (*EXDTextScanResult, error) {
	if reader == nil {
		return &EXDTextScanResult{}, nil
	}

	sheetNames := opts.SheetNames
	if len(sheetNames) == 0 {
		exlRaw, err := reader.ReadFile("exd/root.exl")
		if err != nil {
			return nil, fmt.Errorf("读取 exd/root.exl 失败: %w", err)
		}
		if len(exlRaw) == 0 {
			return &EXDTextScanResult{}, nil
		}
		exl := ReadEXLFile(exlRaw)
		sheetNames = make([]string, 0, len(exl.Entries))
		for _, e := range exl.Entries {
			if e.Name != "" {
				sheetNames = append(sheetNames, e.Name)
			}
		}
	}
	if len(sheetNames) == 0 {
		return &EXDTextScanResult{}, nil
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers < 2 {
			workers = 2
		}
		if workers > 16 {
			workers = 16
		}
	}

	// 阶段 1：解析 exd/*.exh 物理位置并按磁盘偏移顺序并发扫描表头，筛除纯数值表
	type exhTask struct {
		origIdx    int
		name       string
		lower      string
		ar         *ArchiveReader
		entry      indexEntry
		archiveOrd int
	}

	arOrder := make(map[*ArchiveReader]int, 8)
	exhTasks := make([]exhTask, 0, len(sheetNames))
	for i, name := range sheetNames {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lower := strings.ToLower(name)
		ar, normPath, err := reader.locate("exd/" + lower + ".exh")
		if err != nil || ar == nil {
			continue
		}
		entry, ok := ar.lookup(normPath)
		if !ok {
			continue
		}
		ord, exists := arOrder[ar]
		if !exists {
			ord = len(arOrder)
			arOrder[ar] = ord
		}
		exhTasks = append(exhTasks, exhTask{
			origIdx:    i,
			name:       name,
			lower:      lower,
			ar:         ar,
			entry:      entry,
			archiveOrd: ord,
		})
	}
	if len(exhTasks) == 0 {
		return &EXDTextScanResult{}, nil
	}

	sort.Slice(exhTasks, func(i, j int) bool {
		if exhTasks[i].archiveOrd != exhTasks[j].archiveOrd {
			return exhTasks[i].archiveOrd < exhTasks[j].archiveOrd
		}
		if exhTasks[i].entry.dataFileID != exhTasks[j].entry.dataFileID {
			return exhTasks[i].entry.dataFileID < exhTasks[j].entry.dataFileID
		}
		return exhTasks[i].entry.offset < exhTasks[j].entry.offset
	})

	type parsedSheetPlan struct {
		origIdx     int
		meta        EXDStringSheetMeta
		dataOffset  int
		paginations []EXHPagination
	}

	plansByIdx := make([]*parsedSheetPlan, len(sheetNames))
	flatePool := &sync.Pool{}

	var exhCursor atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var spanBuf, outBuf []byte
			for {
				if ctx.Err() != nil {
					return
				}
				idx := int(exhCursor.Add(1) - 1)
				if idx >= len(exhTasks) {
					return
				}
				t := exhTasks[idx]
				raw, err := t.ar.readStandardFileFast(t.entry, flatePool, &spanBuf, &outBuf)
				if err != nil || len(raw) < 32 {
					continue
				}
				exh, err := ReadEXHHeader(raw)
				if err != nil || len(exh.Paginations) == 0 {
					continue
				}
				var strCols []EXHColumn
				var strIdx []int
				for ci, col := range exh.Columns {
					if col.Type == 0 {
						strCols = append(strCols, col)
						strIdx = append(strIdx, ci)
					}
				}
				if len(strCols) == 0 {
					continue
				}
				langSuffix, isLoc, ok := resolvePreferredSheetLang(exh, opts.PreferredLang)
				if !ok || (opts.LocalizedOnly && !isLoc) {
					continue
				}
				plansByIdx[t.origIdx] = &parsedSheetPlan{
					origIdx: t.origIdx,
					meta: EXDStringSheetMeta{
						Name:        t.name,
						LowerName:   t.lower,
						Variant:     exh.Variant,
						IsLocalized: isLoc,
						LangSuffix:  langSuffix,
						ColumnCount: int(exh.ColumnCount),
						StringCols:  strCols,
						StringIdx:   strIdx,
					},
					dataOffset:  int(exh.DataOffset),
					paginations: exh.Paginations,
				}
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	sheets := make([]EXDStringSheetMeta, 0, len(exhTasks)/2)
	sheetDataOffsets := make([]int, 0, len(exhTasks)/2)

	type exdTask struct {
		sheetIdx   int
		ar         *ArchiveReader
		entry      indexEntry
		archiveOrd int
	}
	exdTasks := make([]exdTask, 0, len(exhTasks))

	for _, p := range plansByIdx {
		if p == nil {
			continue
		}
		sIdx := len(sheets)
		p.meta.SheetIdx = sIdx
		sheets = append(sheets, p.meta)
		sheetDataOffsets = append(sheetDataOffsets, p.dataOffset)

		for _, pg := range p.paginations {
			exdPath := fmt.Sprintf("exd/%s_%d%s.exd", p.meta.LowerName, pg.StartID, p.meta.LangSuffix)
			ar, normPath, err := reader.locate(exdPath)
			if err != nil || ar == nil {
				continue
			}
			entry, ok := ar.lookup(normPath)
			if !ok {
				continue
			}
			ord, exists := arOrder[ar]
			if !exists {
				ord = len(arOrder)
				arOrder[ar] = ord
			}
			exdTasks = append(exdTasks, exdTask{
				sheetIdx:   sIdx,
				ar:         ar,
				entry:      entry,
				archiveOrd: ord,
			})
		}
	}

	if len(exdTasks) == 0 {
		return &EXDTextScanResult{Sheets: sheets}, nil
	}

	// 阶段 2：按物理偏移排序后并发解压 EXD 分片并直接提取全部文本至无指针字节池
	sort.Slice(exdTasks, func(i, j int) bool {
		if exdTasks[i].archiveOrd != exdTasks[j].archiveOrd {
			return exdTasks[i].archiveOrd < exdTasks[j].archiveOrd
		}
		if exdTasks[i].entry.dataFileID != exdTasks[j].entry.dataFileID {
			return exdTasks[i].entry.dataFileID < exdTasks[j].entry.dataFileID
		}
		return exdTasks[i].entry.offset < exdTasks[j].entry.offset
	})

	totalTasks := len(exdTasks)
	if opts.OnProgress != nil {
		opts.OnProgress(0, totalTasks, 0)
	}

	shards := make([]EXDStringShard, workers)
	var doneTasks atomic.Int64
	var totalTexts atomic.Int64
	var taskCursor atomic.Int64
	var progMu sync.Mutex
	lastReport := time.Now()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			local := EXDStringShard{
				Blob:  make([]byte, 0, 512*1024),
				Cells: make([]EXDStringCellRef, 0, 16384),
			}
			var spanBuf, outBuf []byte
			const batchSize = 16
			for {
				if ctx.Err() != nil {
					return
				}
				start := int(taskCursor.Add(batchSize) - batchSize)
				if start >= totalTasks {
					break
				}
				end := start + batchSize
				if end > totalTasks {
					end = totalTasks
				}
				batchTexts := 0
				for i := start; i < end; i++ {
					if ctx.Err() != nil {
						return
					}
					t := exdTasks[i]
					raw, err := t.ar.readStandardFileFast(t.entry, flatePool, &spanBuf, &outBuf)
					if err != nil || len(raw) < 32 {
						continue
					}
					batchTexts += extractEXDStrings(
						raw,
						sheets[t.sheetIdx],
						sheetDataOffsets[t.sheetIdx],
						opts.PreferredLang,
						opts.ResolveSheet,
						&local,
					)
				}
				done := int(doneTasks.Add(int64(end - start)))
				texts := int(totalTexts.Add(int64(batchTexts)))
				if opts.OnProgress != nil {
					progMu.Lock()
					if done >= totalTasks || time.Since(lastReport) >= 80*time.Millisecond {
						lastReport = time.Now()
						opts.OnProgress(done, totalTasks, texts)
					}
					progMu.Unlock()
				}
			}
			shards[workerID] = local
		}(w)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	totalRows := int(totalTexts.Load())
	if opts.OnProgress != nil {
		opts.OnProgress(totalTasks, totalTasks, totalRows)
	}
	return &EXDTextScanResult{
		Sheets:    sheets,
		Shards:    shards,
		TotalRows: totalRows,
	}, nil
}
