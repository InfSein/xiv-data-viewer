package sqpack

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	sqPackMagic         = "SqPack\x00\x00"
	fileTypeEmpty       = 1
	fileTypeStandard    = 2
	fileTypeModel       = 3
	fileTypeTexture     = 4
	uncompressedChunkSz = 32000
	textureHeaderSize   = 80
)

// sharedFlatePool 复用 flate 解压器实例，避免每次解压分块都分配 64KB+ 字典内存。
var sharedFlatePool sync.Pool

type indexEntry struct {
	dataFileID uint32
	offset     uint64
	isSynonym  bool
}

type fileInfo struct {
	size               uint32
	fileType           uint32
	rawFileSize        uint32
	numberOfBlocks     uint32
	usedNumberOfBlocks uint32
}

type standardChunkInfo struct {
	offset           uint32
	compressedSize   uint16
	uncompressedSize uint16
}

type textureChunkInfo struct {
	offset           uint32
	compressedSize   uint32
	uncompressedSize uint32
	blockOffset      uint32
	blockCount       uint32
}

// ArchiveReader 封装单个 SqPack 索引与对应 .dat 数据文件的只读访问器。
type ArchiveReader struct {
	prefix       string
	useIndex2    bool
	indexEntries map[uint64]indexEntry

	mu          sync.RWMutex
	dataHandles map[uint32]*os.File
}

// OpenArchive 以严格只读方式打开指定前缀的 .index 或 .index2 文件并加载哈希表。
func OpenArchive(prefix string, useIndex2 bool) (*ArchiveReader, error) {
	ext := "index"
	if useIndex2 {
		ext = "index2"
	}
	indexPath := prefix + "." + ext
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("读取 SqPack 索引文件失败 (%s): %w", indexPath, err)
	}
	entries, err := parseIndexEntries(raw, useIndex2)
	if err != nil {
		return nil, fmt.Errorf("解析 SqPack 索引失败 (%s): %w", indexPath, err)
	}
	return &ArchiveReader{
		prefix:       prefix,
		useIndex2:    useIndex2,
		indexEntries: entries,
		dataHandles:  make(map[uint32]*os.File),
	}, nil
}

func parseIndexEntries(data []byte, useIndex2 bool) (map[uint64]indexEntry, error) {
	if len(data) < 2048 {
		return nil, errors.New("索引文件体积过小")
	}
	if string(data[0:8]) != sqPackMagic {
		return nil, errors.New("无效的 SqPack 文件魔数")
	}
	headerSize := binary.LittleEndian.Uint32(data[12:16])
	if int(headerSize)+16 > len(data) {
		return nil, errors.New("SqPack 头部偏移越界")
	}
	ih := data[headerSize:]
	indexDataOffset := binary.LittleEndian.Uint32(ih[8:12])
	indexDataSize := binary.LittleEndian.Uint32(ih[12:16])
	end := uint64(indexDataOffset) + uint64(indexDataSize)
	if end > uint64(len(data)) {
		return nil, errors.New("SqPack 索引数据区越界")
	}

	section := data[indexDataOffset:end]
	entrySize := 16
	if useIndex2 {
		entrySize = 8
	}
	entries := make(map[uint64]indexEntry, len(section)/entrySize)
	for pos := 0; pos+entrySize <= len(section); pos += entrySize {
		var hash uint64
		var packed uint32
		if useIndex2 {
			hash = uint64(binary.LittleEndian.Uint32(section[pos : pos+4]))
			packed = binary.LittleEndian.Uint32(section[pos+4 : pos+8])
		} else {
			fileHash := binary.LittleEndian.Uint32(section[pos : pos+4])
			dirHash := binary.LittleEndian.Uint32(section[pos+4 : pos+8])
			packed = binary.LittleEndian.Uint32(section[pos+8 : pos+12])
			hash = (uint64(dirHash) << 32) | uint64(fileHash)
		}
		entries[hash] = indexEntry{
			isSynonym:  (packed & 1) == 1,
			dataFileID: (packed & 7) >> 1,
			offset:     uint64(packed&^0xF) * 8,
		}
	}
	return entries, nil
}

func (a *ArchiveReader) lookup(filePath string) (indexEntry, bool) {
	if a.useIndex2 {
		h := uint64(CalculateIndex2Hash(filePath))
		e, ok := a.indexEntries[h]
		return e, ok
	}
	h, err := CalculateIndexHash(filePath)
	if err != nil {
		return indexEntry{}, false
	}
	e, ok := a.indexEntries[h]
	return e, ok
}

// HasFile 检查指定资源路径是否存在于当前索引中。
func (a *ArchiveReader) HasFile(filePath string) bool {
	_, ok := a.lookup(filePath)
	return ok
}

func (a *ArchiveReader) getDataFile(id uint32) (*os.File, error) {
	a.mu.RLock()
	f, ok := a.dataHandles[id]
	a.mu.RUnlock()
	if ok {
		return f, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if f, ok := a.dataHandles[id]; ok {
		return f, nil
	}
	dataPath := fmt.Sprintf("%s.dat%d", a.prefix, id)
	f, err := os.Open(dataPath) // 严格只读打开
	if err != nil {
		return nil, fmt.Errorf("打开 SqPack 数据文件失败 (%s): %w", dataPath, err)
	}
	a.dataHandles[id] = f
	return f, nil
}

// ReadFile 从对应 .dat 文件读取并解压指定资源，找不到时返回 nil, nil。
func (a *ArchiveReader) ReadFile(filePath string) ([]byte, error) {
	entry, ok := a.lookup(filePath)
	if !ok {
		return nil, nil
	}
	f, err := a.getDataFile(entry.dataFileID)
	if err != nil {
		return nil, err
	}

	fi, fiBuf, err := readFileInfo(f, int64(entry.offset))
	if err != nil {
		return nil, fmt.Errorf("读取文件头失败 (%s): %w", filePath, err)
	}
	if fi.fileType == fileTypeEmpty {
		return nil, nil
	}
	if fi.rawFileSize == 0 {
		return nil, fmt.Errorf("文件 %s 长度为 0", filePath)
	}

	switch fi.fileType {
	case fileTypeStandard:
		return readStandardFile(f, int64(entry.offset), fi, fiBuf)
	case fileTypeTexture:
		return readTextureFile(f, int64(entry.offset), fi, fiBuf)
	default:
		return nil, fmt.Errorf("不支持的 SqPack 文件类型: %d (%s)", fi.fileType, filePath)
	}
}

// ReadFileDigest 计算指定归档内资源的未解压大小与内容 MD5 摘要（标准分块文件跳过 zlib 解压以大幅提升批量校验速度）。
func (a *ArchiveReader) ReadFileDigest(filePath string) (rawSize uint32, md5Hex string, ok bool, err error) {
	entry, found := a.lookup(filePath)
	if !found {
		return 0, "", false, nil
	}
	f, err := a.getDataFile(entry.dataFileID)
	if err != nil {
		return 0, "", false, err
	}

	fi, fiBuf, err := readFileInfo(f, int64(entry.offset))
	if err != nil {
		return 0, "", false, fmt.Errorf("读取文件头失败 (%s): %w", filePath, err)
	}
	if fi.fileType == fileTypeEmpty || fi.rawFileSize == 0 {
		return 0, "", false, nil
	}
	if fi.fileType == fileTypeStandard {
		digest, err := hashStandardFileChunks(f, int64(entry.offset), fi, fiBuf)
		if err != nil {
			return 0, "", false, err
		}
		return fi.rawFileSize, digest, true, nil
	}
	raw, err := a.ReadFile(filePath)
	if err != nil || len(raw) == 0 {
		return 0, "", false, err
	}
	sum := md5.Sum(raw)
	return fi.rawFileSize, hex.EncodeToString(sum[:]), true, nil
}

func hashStandardFileChunks(f *os.File, fileOffset int64, fi fileInfo, fiBuf []byte) (string, error) {
	if len(fiBuf) < 24 {
		return "", errors.New("标准文件头长度不足")
	}
	chunkCount := int(binary.LittleEndian.Uint32(fiBuf[20:24]))
	pos := 24
	if pos+chunkCount*8 > len(fiBuf) {
		return "", errors.New("标准文件分块表越界")
	}

	h := md5.New()
	var meta [12]byte
	binary.LittleEndian.PutUint32(meta[0:4], fi.fileType)
	binary.LittleEndian.PutUint32(meta[4:8], fi.rawFileSize)
	binary.LittleEndian.PutUint32(meta[8:12], uint32(chunkCount))
	h.Write(meta[:])

	if chunkCount == 0 {
		return hex.EncodeToString(h.Sum(nil)), nil
	}

	chunks := make([]standardChunkInfo, chunkCount)
	var maxEnd uint64
	for i := 0; i < chunkCount; i++ {
		off := binary.LittleEndian.Uint32(fiBuf[pos : pos+4])
		comp := binary.LittleEndian.Uint16(fiBuf[pos+4 : pos+6])
		uncomp := binary.LittleEndian.Uint16(fiBuf[pos+6 : pos+8])
		chunks[i] = standardChunkInfo{
			offset:           off,
			compressedSize:   comp,
			uncompressedSize: uncomp,
		}
		if end := uint64(off) + uint64(comp); end > maxEnd {
			maxEnd = end
		}
		pos += 8
	}

	baseOffset := fileOffset + int64(fi.size)
	// 若分块在单个连续区间内（<= 64MB），一次性顺序读取后在内存内摘要，避免海量小 ReadAt 系统调用开销
	if maxEnd > 0 && maxEnd <= 64<<20 {
		span := make([]byte, maxEnd)
		if err := readAtFull(f, span, baseOffset); err == nil {
			allValid := true
			for _, ci := range chunks {
				off := int(ci.offset)
				if off+16 > len(span) {
					allValid = false
					break
				}
				hdrSize := int(binary.LittleEndian.Uint32(span[off : off+4]))
				compSize := int(binary.LittleEndian.Uint32(span[off+8 : off+12]))
				uncompSize := int(binary.LittleEndian.Uint32(span[off+12 : off+16]))
				payloadSize := compSize
				if compSize == uncompressedChunkSz {
					payloadSize = uncompSize
				}
				if hdrSize < 16 || off+hdrSize+payloadSize > len(span) {
					allValid = false
					break
				}
				h.Write(span[off+8 : off+16])
				h.Write(span[off+hdrSize : off+hdrSize+payloadSize])
			}
			if allValid {
				return hex.EncodeToString(h.Sum(nil)), nil
			}
			// 回退重算摘要
			h.Reset()
			h.Write(meta[:])
		}
	}

	var buf []byte
	for _, ci := range chunks {
		var hdr [16]byte
		chunkOff := baseOffset + int64(ci.offset)
		if err := readAtFull(f, hdr[:], chunkOff); err != nil {
			return "", err
		}
		hdrSize := binary.LittleEndian.Uint32(hdr[0:4])
		compSize := binary.LittleEndian.Uint32(hdr[8:12])
		uncompSize := binary.LittleEndian.Uint32(hdr[12:16])
		payloadSize := int(compSize)
		if compSize == uncompressedChunkSz {
			payloadSize = int(uncompSize)
		}
		if cap(buf) < payloadSize {
			buf = make([]byte, payloadSize)
		}
		if err := readAtFull(f, buf[:payloadSize], chunkOff+int64(hdrSize)); err != nil {
			return "", err
		}
		h.Write(hdr[8:16])
		h.Write(buf[:payloadSize])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Close 关闭当前归档打开的所有只读文件句柄。
func (a *ArchiveReader) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var firstErr error
	for id, f := range a.dataHandles {
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(a.dataHandles, id)
	}
	return firstErr
}

func readAtFull(f *os.File, buf []byte, off int64) error {
	n, err := f.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("读取字节数不足: 期望 %d, 实际 %d", len(buf), n)
	}
	return nil
}

func readFileInfo(f *os.File, offset int64) (fileInfo, []byte, error) {
	var stack [256]byte
	n, err := f.ReadAt(stack[:], offset)
	if n < 24 || (err != nil && !errors.Is(err, io.EOF)) {
		if err != nil {
			return fileInfo{}, nil, err
		}
		return fileInfo{}, nil, fmt.Errorf("读取文件头字节数不足: 实际 %d", n)
	}
	fi := fileInfo{
		size:               binary.LittleEndian.Uint32(stack[0:4]),
		fileType:           binary.LittleEndian.Uint32(stack[4:8]),
		rawFileSize:        binary.LittleEndian.Uint32(stack[8:12]),
		numberOfBlocks:     binary.LittleEndian.Uint32(stack[12:16]),
		usedNumberOfBlocks: binary.LittleEndian.Uint32(stack[16:20]),
	}
	if fi.size == 0 {
		return fi, nil, nil
	}
	if int(fi.size) <= n {
		out := make([]byte, fi.size)
		copy(out, stack[:fi.size])
		return fi, out, nil
	}
	full := make([]byte, fi.size)
	copy(full, stack[:n])
	if err := readAtFull(f, full[n:], offset+int64(n)); err != nil {
		return fileInfo{}, nil, err
	}
	return fi, full, nil
}

func readDataChunk(f *os.File, offset int64) ([]byte, error) {
	var hdr [16]byte
	if err := readAtFull(f, hdr[:], offset); err != nil {
		return nil, err
	}
	hdrSize := binary.LittleEndian.Uint32(hdr[0:4])
	compSize := binary.LittleEndian.Uint32(hdr[8:12])
	uncompSize := binary.LittleEndian.Uint32(hdr[12:16])

	dataOffset := offset + int64(hdrSize)
	if compSize == uncompressedChunkSz {
		out := make([]byte, uncompSize)
		if err := readAtFull(f, out, dataOffset); err != nil {
			return nil, err
		}
		return out, nil
	}

	compressed := make([]byte, compSize)
	if err := readAtFull(f, compressed, dataOffset); err != nil {
		return nil, err
	}
	out := make([]byte, uncompSize)
	if err := inflatePooled(&sharedFlatePool, compressed, out); err != nil {
		return nil, fmt.Errorf("解压数据块失败: %w", err)
	}
	return out, nil
}

func readStandardFile(f *os.File, fileOffset int64, fi fileInfo, fiBuf []byte) ([]byte, error) {
	if len(fiBuf) < 24 {
		return nil, errors.New("标准文件头长度不足")
	}
	chunkCount := int(binary.LittleEndian.Uint32(fiBuf[20:24]))
	pos := 24
	if pos+chunkCount*8 > len(fiBuf) {
		return nil, errors.New("标准文件分块表越界")
	}

	// 计算所有分块覆盖的连续区间，优先一次 ReadAt 读入内存并使用池化解压器直接写入输出缓冲区
	var maxEnd uint64
	scanPos := pos
	for i := 0; i < chunkCount; i++ {
		off := binary.LittleEndian.Uint32(fiBuf[scanPos : scanPos+4])
		comp := binary.LittleEndian.Uint16(fiBuf[scanPos+4 : scanPos+6])
		if end := uint64(off) + uint64(comp); end > maxEnd {
			maxEnd = end
		}
		scanPos += 8
	}

	baseOffset := fileOffset + int64(fi.size)
	const maxSingleSpan = 32 << 20
	if chunkCount > 0 && maxEnd > 0 && maxEnd <= maxSingleSpan {
		span := make([]byte, maxEnd)
		if err := readAtFull(f, span, baseOffset); err == nil {
			out := make([]byte, 0, fi.rawFileSize)
			curPos := pos
			allOK := true
			for i := 0; i < chunkCount; i++ {
				ciOff := int(binary.LittleEndian.Uint32(fiBuf[curPos : curPos+4]))
				curPos += 8
				if ciOff+16 > len(span) {
					allOK = false
					break
				}
				hdrSize := int(binary.LittleEndian.Uint32(span[ciOff : ciOff+4]))
				compSize := int(binary.LittleEndian.Uint32(span[ciOff+8 : ciOff+12]))
				uncompSize := int(binary.LittleEndian.Uint32(span[ciOff+12 : ciOff+16]))
				dataStart := ciOff + hdrSize
				if hdrSize < 16 {
					allOK = false
					break
				}
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
				if err := inflatePooled(&sharedFlatePool, span[dataStart:dataStart+compSize], dst); err != nil {
					allOK = false
					break
				}
				out = out[:curLen+uncompSize]
			}
			if allOK && uint32(len(out)) == fi.rawFileSize {
				return out, nil
			}
		}
	}

	// 回退到逐块读取路径（超大文件或非常规块布局）
	out := make([]byte, 0, fi.rawFileSize)
	for i := 0; i < chunkCount; i++ {
		off := binary.LittleEndian.Uint32(fiBuf[pos : pos+4])
		pos += 8
		chunk, err := readDataChunk(f, baseOffset+int64(off))
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}
	if uint32(len(out)) != fi.rawFileSize {
		return nil, fmt.Errorf("文件解压大小不匹配: 期望 %d, 实际 %d", fi.rawFileSize, len(out))
	}
	return out, nil
}

func readTextureFile(f *os.File, fileOffset int64, fi fileInfo, fiBuf []byte) ([]byte, error) {
	if len(fiBuf) < 24 {
		return nil, errors.New("贴图文件头长度不足")
	}
	chunkCount := int(binary.LittleEndian.Uint32(fiBuf[20:24]))
	pos := 24
	if pos+chunkCount*20 > len(fiBuf) {
		return nil, errors.New("贴图文件分块表越界")
	}

	chunks := make([]textureChunkInfo, chunkCount)
	subBlockTotal := 0
	for i := 0; i < chunkCount; i++ {
		chunks[i] = textureChunkInfo{
			offset:           binary.LittleEndian.Uint32(fiBuf[pos : pos+4]),
			compressedSize:   binary.LittleEndian.Uint32(fiBuf[pos+4 : pos+8]),
			uncompressedSize: binary.LittleEndian.Uint32(fiBuf[pos+8 : pos+12]),
			blockOffset:      binary.LittleEndian.Uint32(fiBuf[pos+12 : pos+16]),
			blockCount:       binary.LittleEndian.Uint32(fiBuf[pos+16 : pos+20]),
		}
		subBlockTotal += int(chunks[i].blockCount)
		pos += 20
	}

	if pos+subBlockTotal*2 > len(fiBuf) {
		return nil, errors.New("贴图子块大小表越界")
	}
	blockSizes := make([]uint16, subBlockTotal)
	for i := 0; i < subBlockTotal; i++ {
		blockSizes[i] = binary.LittleEndian.Uint16(fiBuf[pos : pos+2])
		pos += 2
	}

	baseOffset := fileOffset + int64(fi.size)
	texHdr := make([]byte, textureHeaderSize)
	if err := readAtFull(f, texHdr, baseOffset); err != nil {
		return nil, err
	}

	out := make([]byte, 0, fi.rawFileSize)
	out = append(out, texHdr...)

	for _, ci := range chunks {
		start := int(ci.blockOffset)
		end := start + int(ci.blockCount)
		if end > len(blockSizes) {
			return nil, errors.New("贴图块索引越界")
		}
		var subOff int64
		for _, bsz := range blockSizes[start:end] {
			chunk, err := readDataChunk(f, baseOffset+int64(ci.offset)+subOff)
			if err != nil {
				return nil, err
			}
			out = append(out, chunk...)
			subOff += int64(bsz)
		}
	}

	if uint32(len(out)) != fi.rawFileSize {
		return nil, fmt.Errorf("贴图解压大小不匹配: 期望 %d, 实际 %d", fi.rawFileSize, len(out))
	}
	return out, nil
}

// sqPackCategoryIDs 映射游戏资源顶级目录名到 SqPack 分类编号。
var sqPackCategoryIDs = map[string]int{
	"common":      0,
	"bgcommon":    1,
	"bg":          2,
	"cut":         3,
	"chara":       4,
	"shader":      5,
	"ui":          6,
	"sound":       7,
	"vfx":         8,
	"ui_script":   9,
	"exd":         10,
	"game_script": 11,
	"music":       12,
	"sqpack_test": 18,
	"debug":       19,
}

var expansionRe = regexp.MustCompile(`^ex(\d+)$`)

type parsedResourcePath struct {
	path        string
	repository  string
	categoryID  int
	expansionID int
}

func parseResourcePath(resourcePath string) (parsedResourcePath, error) {
	p := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(resourcePath, "\\", "/")))
	parts := strings.Split(p, "/")
	if len(parts) < 2 || strings.ContainsRune(p, 0) {
		return parsedResourcePath{}, fmt.Errorf("无效的资源路径: %s", resourcePath)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return parsedResourcePath{}, fmt.Errorf("无效的资源路径: %s", resourcePath)
		}
	}
	catID, ok := sqPackCategoryIDs[parts[0]]
	if !ok {
		return parsedResourcePath{}, fmt.Errorf("未知的 SqPack 资源分类: %s", parts[0])
	}
	repo := "ffxiv"
	expID := 0
	if m := expansionRe.FindStringSubmatch(parts[1]); m != nil {
		repo = parts[1]
		n, err := strconv.Atoi(m[1])
		if err != nil || n > 255 {
			return parsedResourcePath{}, fmt.Errorf("无效的资料片仓库: %s", repo)
		}
		expID = n
	}
	return parsedResourcePath{
		path:        p,
		repository:  repo,
		categoryID:  catID,
		expansionID: expID,
	}, nil
}

type archiveCandidate struct {
	prefix    string
	useIndex2 bool
}

// FileReader 抽象只读虚拟路径文件访问能力（可由本地客户端 GameReader 或历史快照 .xivsnap 实现）。
type FileReader interface {
	ReadFile(resourcePath string) ([]byte, error)
}

func isNilFileReader(r FileReader) bool {
	if r == nil {
		return true
	}
	if gr, ok := r.(*GameReader); ok && gr == nil {
		return true
	}
	return false
}

// GameReader 为本地 FF14 game 目录提供全仓库并发安全的只读 SqPack 文件访问能力。
type GameReader struct {
	gamePath string

	mu           sync.Mutex
	repositories map[string][]string
	candidates   map[string][]archiveCandidate
	archives     map[string]*ArchiveReader
}

// NewGameReader 创建指定 FF14 game 目录的只读 SqPack 读取器。
func NewGameReader(gamePath string) *GameReader {
	return &GameReader{
		gamePath:     gamePath,
		repositories: make(map[string][]string),
		candidates:   make(map[string][]archiveCandidate),
		archives:     make(map[string]*ArchiveReader),
	}
}

// GamePath 返回当前读取器挂载的 game 目录路径。
func (g *GameReader) GamePath() string {
	return g.gamePath
}

func (g *GameReader) getCandidates(directory, stem string) ([]archiveCandidate, error) {
	key := filepath.Join(directory, stem)
	g.mu.Lock()
	if cands, ok := g.candidates[key]; ok {
		g.mu.Unlock()
		return cands, nil
	}
	files, listed := g.repositories[directory]
	g.mu.Unlock()

	if !listed {
		entries, err := os.ReadDir(directory)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				files = nil
			} else {
				return nil, err
			}
		} else {
			files = make([]string, 0, len(entries))
			for _, e := range entries {
				files = append(files, e.Name())
			}
		}
		g.mu.Lock()
		g.repositories[directory] = files
		g.mu.Unlock()
	}

	re := regexp.MustCompile(`^` + regexp.QuoteMeta(stem) + `[0-9a-f]{2}\.win32\.index2?$`)
	matched := make([]string, 0)
	for _, f := range files {
		if re.MatchString(f) {
			matched = append(matched, f)
		}
	}
	sort.Strings(matched)

	seen := make(map[string]struct{}, len(matched))
	cands := make([]archiveCandidate, 0, len(matched))
	for _, f := range matched {
		useIndex2 := strings.HasSuffix(f, ".index2")
		base := strings.TrimSuffix(strings.TrimSuffix(f, ".win32.index2"), ".win32.index")
		prefix := filepath.Join(directory, base)
		if _, dup := seen[prefix]; dup {
			continue
		}
		seen[prefix] = struct{}{}
		cands = append(cands, archiveCandidate{
			prefix:    prefix,
			useIndex2: useIndex2,
		})
	}

	g.mu.Lock()
	g.candidates[key] = cands
	g.mu.Unlock()
	return cands, nil
}

func (g *GameReader) getArchive(cand archiveCandidate) (*ArchiveReader, error) {
	g.mu.Lock()
	if ar, ok := g.archives[cand.prefix]; ok {
		g.mu.Unlock()
		return ar, nil
	}
	g.mu.Unlock()

	opened, err := OpenArchive(cand.prefix+".win32", cand.useIndex2)
	if err != nil {
		return nil, err
	}

	g.mu.Lock()
	if existing, ok := g.archives[cand.prefix]; ok {
		g.mu.Unlock()
		_ = opened.Close()
		return existing, nil
	}
	g.archives[cand.prefix] = opened
	g.mu.Unlock()
	return opened, nil
}

func (g *GameReader) locate(resourcePath string) (*ArchiveReader, string, error) {
	parsed, err := parseResourcePath(resourcePath)
	if err != nil {
		return nil, "", err
	}
	directory := filepath.Join(g.gamePath, "sqpack", parsed.repository)
	stem := fmt.Sprintf("%02x%02x", parsed.categoryID, parsed.expansionID)
	cands, err := g.getCandidates(directory, stem)
	if err != nil {
		return nil, "", err
	}
	for _, cand := range cands {
		ar, err := g.getArchive(cand)
		if err != nil {
			return nil, "", err
		}
		if ar.HasFile(parsed.path) {
			return ar, parsed.path, nil
		}
	}
	return nil, "", nil
}

// ReadFile 读取并解压游戏内指定虚拟路径资源；文件不存在时返回 nil, nil。
func (g *GameReader) ReadFile(resourcePath string) ([]byte, error) {
	ar, normPath, err := g.locate(resourcePath)
	if err != nil || ar == nil {
		return nil, err
	}
	return ar.ReadFile(normPath)
}

// HasFile 检查游戏内指定虚拟路径资源是否存在。
func (g *GameReader) HasFile(resourcePath string) (bool, error) {
	ar, _, err := g.locate(resourcePath)
	return ar != nil, err
}

// ReadFileDigest 计算游戏内指定虚拟路径资源的大小与内容 MD5 摘要；文件不存在或为空时返回 ok=false。
func (g *GameReader) ReadFileDigest(resourcePath string) (rawSize uint32, md5Hex string, ok bool, err error) {
	ar, normPath, err := g.locate(resourcePath)
	if err != nil || ar == nil {
		return 0, "", false, err
	}
	return ar.ReadFileDigest(normPath)
}

// Close 释放所有已打开的只读归档句柄与索引缓存。
func (g *GameReader) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	var firstErr error
	for k, ar := range g.archives {
		if err := ar.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(g.archives, k)
	}
	g.repositories = make(map[string][]string)
	g.candidates = make(map[string][]archiveCandidate)
	return firstErr
}
