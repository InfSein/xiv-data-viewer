package snapshot

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	archiveMagic        = "XIVSNAP1"
	archiveTrailerSize  = 8 + 8 + len(archiveMagic)
	defaultBlockMaxRaw  = 4 << 20 // 每个固实压缩块目标 4 MiB，兼顾高压缩率与随机寻址速度
	defaultBlockCacheSz = 16      // 最多常驻 16 个解压块（约 64 MiB）
)

type blockDesc struct {
	offset   int64
	compSize uint32
	rawSize  uint32
}

type fileLoc struct {
	blockIdx uint32
	offset   uint32
	size     uint32
}

type dirEntry struct {
	path string
	loc  fileLoc
}

// Writer 将多个虚拟路径文件按分块固实 zstd 压缩流式写入 .xivsnap 归档文件。
type Writer struct {
	f          *os.File
	bw         *bufio.Writer
	enc        *zstd.Encoder
	blockLimit int
	written    int64

	curPrefix string
	curRaw    bytes.Buffer
	blocks    []blockDesc
	entries   []dirEntry
	seen      map[string]struct{}
	closed    bool
}

// NewWriter 创建 .xivsnap 归档写入器。
func NewWriter(outPath string, level zstd.EncoderLevel) (*Writer, error) {
	if level == 0 {
		level = zstd.SpeedBestCompression
	}
	f, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("创建快照文件失败: %w", err)
	}
	bw := bufio.NewWriterSize(f, 256*1024)
	if _, err := bw.WriteString(archiveMagic); err != nil {
		_ = f.Close()
		return nil, err
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(level))
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("初始化 zstd 编码器失败: %w", err)
	}
	return &Writer{
		f:          f,
		bw:         bw,
		enc:        enc,
		blockLimit: defaultBlockMaxRaw,
		written:    int64(len(archiveMagic)),
		seen:       make(map[string]struct{}, 4096),
	}, nil
}

func topPathPrefix(vpath string) string {
	idx := strings.IndexByte(vpath, '/')
	if idx < 0 {
		return ""
	}
	return vpath[:idx]
}

// AddFile 向归档追加一个虚拟路径文件；不同顶级目录前缀自动切块以优化随机访问。
func (w *Writer) AddFile(vpath string, data []byte) error {
	if w.closed {
		return errors.New("快照写入器已关闭")
	}
	cleanPath := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(vpath), "\\", "/"), "/")
	if cleanPath == "" {
		return errors.New("虚拟路径不能为空")
	}
	if _, dup := w.seen[cleanPath]; dup {
		return nil
	}
	w.seen[cleanPath] = struct{}{}

	prefix := topPathPrefix(cleanPath)
	if w.curRaw.Len() > 0 && (prefix != w.curPrefix || w.curRaw.Len()+len(data) > w.blockLimit) {
		if err := w.flushBlock(); err != nil {
			return err
		}
	}
	w.curPrefix = prefix

	blockIdx := uint32(len(w.blocks))
	off := uint32(w.curRaw.Len())
	if len(data) > 0 {
		w.curRaw.Write(data)
	}
	w.entries = append(w.entries, dirEntry{
		path: cleanPath,
		loc: fileLoc{
			blockIdx: blockIdx,
			offset:   off,
			size:     uint32(len(data)),
		},
	})
	// 单文件本身超过阈值时立即落盘，避免缓冲无限增长
	if w.curRaw.Len() >= w.blockLimit {
		if err := w.flushBlock(); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) flushBlock() error {
	if w.curRaw.Len() == 0 {
		return nil
	}
	raw := w.curRaw.Bytes()
	comp := w.enc.EncodeAll(raw, make([]byte, 0, len(raw)/2))
	off := w.written
	if _, err := w.bw.Write(comp); err != nil {
		return err
	}
	w.written += int64(len(comp))
	w.blocks = append(w.blocks, blockDesc{
		offset:   off,
		compSize: uint32(len(comp)),
		rawSize:  uint32(len(raw)),
	})
	w.curRaw.Reset()
	return nil
}

// Close 写入目录尾部并安全关闭归档文件。
func (w *Writer) Close(meta SnapshotMeta) error {
	if w.closed {
		return nil
	}
	w.closed = true
	defer func() {
		w.enc.Close()
		_ = w.f.Close()
	}()

	if err := w.flushBlock(); err != nil {
		return err
	}

	if meta.FormatVersion == 0 {
		meta.FormatVersion = FormatVersion
	}
	footerRaw, err := encodeFooter(meta, w.blocks, w.entries)
	if err != nil {
		return err
	}
	footerComp := w.enc.EncodeAll(footerRaw, make([]byte, 0, len(footerRaw)/2))
	footerOffset := w.written
	if _, err := w.bw.Write(footerComp); err != nil {
		return err
	}
	w.written += int64(len(footerComp))

	var trailer [archiveTrailerSize]byte
	binary.LittleEndian.PutUint64(trailer[0:8], uint64(footerOffset))
	binary.LittleEndian.PutUint64(trailer[8:16], uint64(len(footerComp)))
	copy(trailer[16:], archiveMagic)
	if _, err := w.bw.Write(trailer[:]); err != nil {
		return err
	}
	if err := w.bw.Flush(); err != nil {
		return err
	}
	return w.f.Close()
}

// Abort 异常中止写入并关闭底层句柄。
func (w *Writer) Abort() {
	if w.closed {
		return
	}
	w.closed = true
	w.enc.Close()
	_ = w.f.Close()
}

func encodeFooter(meta SnapshotMeta, blocks []blockDesc, entries []dirEntry) ([]byte, error) {
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("序列化快照元信息失败: %w", err)
	}
	var buf bytes.Buffer
	var u32 [4]byte
	var u64 [8]byte
	var u16 [2]byte

	binary.LittleEndian.PutUint32(u32[:], uint32(len(metaBytes)))
	buf.Write(u32[:])
	buf.Write(metaBytes)

	binary.LittleEndian.PutUint32(u32[:], uint32(len(blocks)))
	buf.Write(u32[:])
	for _, b := range blocks {
		binary.LittleEndian.PutUint64(u64[:], uint64(b.offset))
		buf.Write(u64[:])
		binary.LittleEndian.PutUint32(u32[:], b.compSize)
		buf.Write(u32[:])
		binary.LittleEndian.PutUint32(u32[:], b.rawSize)
		buf.Write(u32[:])
	}

	binary.LittleEndian.PutUint32(u32[:], uint32(len(entries)))
	buf.Write(u32[:])
	for _, e := range entries {
		if len(e.path) > 0xFFFF {
			return nil, fmt.Errorf("快照路径过长: %s", e.path)
		}
		binary.LittleEndian.PutUint16(u16[:], uint16(len(e.path)))
		buf.Write(u16[:])
		buf.WriteString(e.path)
		binary.LittleEndian.PutUint32(u32[:], e.loc.blockIdx)
		buf.Write(u32[:])
		binary.LittleEndian.PutUint32(u32[:], e.loc.offset)
		buf.Write(u32[:])
		binary.LittleEndian.PutUint32(u32[:], e.loc.size)
		buf.Write(u32[:])
	}
	return buf.Bytes(), nil
}

func decodeFooter(raw []byte) (SnapshotMeta, []blockDesc, []dirEntry, error) {
	pos := 0
	if len(raw) < 4 {
		return SnapshotMeta{}, nil, nil, io.ErrUnexpectedEOF
	}
	metaLen := int(binary.LittleEndian.Uint32(raw[pos : pos+4]))
	pos += 4
	if pos+metaLen+4 > len(raw) {
		return SnapshotMeta{}, nil, nil, io.ErrUnexpectedEOF
	}
	var meta SnapshotMeta
	if err := json.Unmarshal(raw[pos:pos+metaLen], &meta); err != nil {
		return SnapshotMeta{}, nil, nil, fmt.Errorf("解析快照元信息失败: %w", err)
	}
	pos += metaLen

	blockCnt := int(binary.LittleEndian.Uint32(raw[pos : pos+4]))
	pos += 4
	if pos+blockCnt*16+4 > len(raw) {
		return SnapshotMeta{}, nil, nil, io.ErrUnexpectedEOF
	}
	blocks := make([]blockDesc, blockCnt)
	for i := 0; i < blockCnt; i++ {
		blocks[i] = blockDesc{
			offset:   int64(binary.LittleEndian.Uint64(raw[pos : pos+8])),
			compSize: binary.LittleEndian.Uint32(raw[pos+8 : pos+12]),
			rawSize:  binary.LittleEndian.Uint32(raw[pos+12 : pos+16]),
		}
		pos += 16
	}

	entryCnt := int(binary.LittleEndian.Uint32(raw[pos : pos+4]))
	pos += 4
	entries := make([]dirEntry, entryCnt)
	for i := 0; i < entryCnt; i++ {
		if pos+2 > len(raw) {
			return SnapshotMeta{}, nil, nil, io.ErrUnexpectedEOF
		}
		pLen := int(binary.LittleEndian.Uint16(raw[pos : pos+2]))
		pos += 2
		if pos+pLen+12 > len(raw) {
			return SnapshotMeta{}, nil, nil, io.ErrUnexpectedEOF
		}
		p := string(raw[pos : pos+pLen])
		pos += pLen
		entries[i] = dirEntry{
			path: p,
			loc: fileLoc{
				blockIdx: binary.LittleEndian.Uint32(raw[pos : pos+4]),
				offset:   binary.LittleEndian.Uint32(raw[pos+4 : pos+8]),
				size:     binary.LittleEndian.Uint32(raw[pos+8 : pos+12]),
			},
		}
		pos += 12
	}
	return meta, blocks, entries, nil
}

type cachedBlock struct {
	data []byte
	seq  uint64
}

// Archive 为已下载的 .xivsnap 快照提供并发安全的随机读取与按块缓存能力，实现 sqpack.FileReader。
type Archive struct {
	path       string
	f          *os.File
	dec        *zstd.Decoder
	meta       SnapshotMeta
	blocks     []blockDesc
	exactFiles map[string]fileLoc
	lowerFiles map[string]fileLoc

	cacheMu    sync.Mutex
	blockCache map[uint32]*cachedBlock
	cacheSeq   uint64
	blockLocks map[uint32]*sync.Mutex

	thumbMu     sync.Mutex
	thumbsReady bool
	thumbs      map[string][]byte
}

// OpenArchive 以严格只读方式打开 .xivsnap 快照文件并加载目录索引。
func OpenArchive(archivePath string) (*Archive, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("打开快照文件失败: %w", err)
	}
	meta, blocks, entries, dec, err := openAndDecodeFooter(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	exact := make(map[string]fileLoc, len(entries))
	lower := make(map[string]fileLoc, len(entries))
	for _, e := range entries {
		exact[e.path] = e.loc
		lower[strings.ToLower(e.path)] = e.loc
	}
	return &Archive{
		path:       archivePath,
		f:          f,
		dec:        dec,
		meta:       meta,
		blocks:     blocks,
		exactFiles: exact,
		lowerFiles: lower,
		blockCache: make(map[uint32]*cachedBlock, defaultBlockCacheSz),
		blockLocks: make(map[uint32]*sync.Mutex),
	}, nil
}

// ReadArchiveMeta 快速读取单个 .xivsnap 文件的元信息后立即关闭句柄（不常驻内存）。
func ReadArchiveMeta(archivePath string) (SnapshotMeta, int64, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return SnapshotMeta{}, 0, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return SnapshotMeta{}, 0, err
	}
	meta, _, _, dec, err := openAndDecodeFooter(f)
	if dec != nil {
		dec.Close()
	}
	if err != nil {
		return SnapshotMeta{}, 0, err
	}
	return meta, st.Size(), nil
}

func openAndDecodeFooter(f *os.File) (SnapshotMeta, []blockDesc, []dirEntry, *zstd.Decoder, error) {
	st, err := f.Stat()
	if err != nil {
		return SnapshotMeta{}, nil, nil, nil, err
	}
	minSize := int64(len(archiveMagic) + archiveTrailerSize)
	if st.Size() < minSize {
		return SnapshotMeta{}, nil, nil, nil, errors.New("快照文件体积过小或已损坏")
	}

	var head [len(archiveMagic)]byte
	if _, err := f.ReadAt(head[:], 0); err != nil {
		return SnapshotMeta{}, nil, nil, nil, err
	}
	if string(head[:]) != archiveMagic {
		return SnapshotMeta{}, nil, nil, nil, fmt.Errorf("无效的快照文件魔数: %q", string(head[:]))
	}

	var trailer [archiveTrailerSize]byte
	if _, err := f.ReadAt(trailer[:], st.Size()-int64(archiveTrailerSize)); err != nil {
		return SnapshotMeta{}, nil, nil, nil, err
	}
	if string(trailer[16:]) != archiveMagic {
		return SnapshotMeta{}, nil, nil, nil, errors.New("快照文件尾部校验失败（文件可能未完整下载）")
	}
	footerOff := int64(binary.LittleEndian.Uint64(trailer[0:8]))
	footerCompSize := int64(binary.LittleEndian.Uint64(trailer[8:16]))
	if footerOff < int64(len(archiveMagic)) || footerCompSize <= 0 || footerOff+footerCompSize > st.Size()-int64(archiveTrailerSize) {
		return SnapshotMeta{}, nil, nil, nil, errors.New("快照目录偏移越界")
	}

	footerComp := make([]byte, footerCompSize)
	if _, err := io.ReadFull(io.NewSectionReader(f, footerOff, footerCompSize), footerComp); err != nil {
		return SnapshotMeta{}, nil, nil, nil, err
	}

	dec, err := zstd.NewReader(nil)
	if err != nil {
		return SnapshotMeta{}, nil, nil, nil, err
	}
	footerRaw, err := dec.DecodeAll(footerComp, nil)
	if err != nil {
		dec.Close()
		return SnapshotMeta{}, nil, nil, nil, fmt.Errorf("解压快照目录失败: %w", err)
	}
	meta, blocks, entries, err := decodeFooter(footerRaw)
	if err != nil {
		dec.Close()
		return SnapshotMeta{}, nil, nil, nil, err
	}
	return meta, blocks, entries, dec, nil
}

// Meta 返回快照元信息。
func (a *Archive) Meta() SnapshotMeta {
	if a == nil {
		return SnapshotMeta{}
	}
	return a.meta
}

// Path 返回快照本地文件路径。
func (a *Archive) Path() string {
	if a == nil {
		return ""
	}
	return a.path
}

// Close 释放快照打开的文件句柄、解码器与块缓存。
func (a *Archive) Close() error {
	if a == nil {
		return nil
	}
	a.cacheMu.Lock()
	a.blockCache = nil
	a.blockLocks = nil
	a.cacheMu.Unlock()
	if a.dec != nil {
		a.dec.Close()
	}
	if a.f != nil {
		return a.f.Close()
	}
	return nil
}

// ReadFile 读取快照中指定虚拟路径的文件内容（不区分大小写，满足 sqpack.FileReader）。找不到返回 nil, nil。
func (a *Archive) ReadFile(resourcePath string) ([]byte, error) {
	if a == nil {
		return nil, nil
	}
	norm := strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(resourcePath), "\\", "/"), "/"))
	loc, ok := a.lowerFiles[norm]
	if !ok {
		return nil, nil
	}
	return a.readLoc(loc)
}

func (a *Archive) readExactFile(exactPath string) ([]byte, bool, error) {
	if a == nil {
		return nil, false, nil
	}
	loc, ok := a.exactFiles[exactPath]
	if !ok {
		return nil, false, nil
	}
	data, err := a.readLoc(loc)
	return data, true, err
}

func (a *Archive) readLoc(loc fileLoc) ([]byte, error) {
	if loc.size == 0 {
		return []byte{}, nil
	}
	blockData, err := a.getBlock(loc.blockIdx)
	if err != nil {
		return nil, err
	}
	end := uint64(loc.offset) + uint64(loc.size)
	if end > uint64(len(blockData)) {
		return nil, fmt.Errorf("快照块内偏移越界: block=%d off=%d size=%d", loc.blockIdx, loc.offset, loc.size)
	}
	out := make([]byte, loc.size)
	copy(out, blockData[loc.offset:end])
	return out, nil
}

func (a *Archive) getBlock(idx uint32) ([]byte, error) {
	if int(idx) >= len(a.blocks) {
		return nil, fmt.Errorf("快照块索引越界: %d", idx)
	}

	a.cacheMu.Lock()
	if cb, ok := a.blockCache[idx]; ok {
		a.cacheSeq++
		cb.seq = a.cacheSeq
		data := cb.data
		a.cacheMu.Unlock()
		return data, nil
	}
	bl, ok := a.blockLocks[idx]
	if !ok {
		bl = &sync.Mutex{}
		a.blockLocks[idx] = bl
	}
	a.cacheMu.Unlock()

	bl.Lock()
	defer bl.Unlock()

	a.cacheMu.Lock()
	if cb, ok := a.blockCache[idx]; ok {
		a.cacheSeq++
		cb.seq = a.cacheSeq
		data := cb.data
		a.cacheMu.Unlock()
		return data, nil
	}
	a.cacheMu.Unlock()

	bd := a.blocks[idx]
	comp := make([]byte, bd.compSize)
	if _, err := io.ReadFull(io.NewSectionReader(a.f, bd.offset, int64(bd.compSize)), comp); err != nil {
		return nil, fmt.Errorf("读取快照压缩块 %d 失败: %w", idx, err)
	}
	raw, err := a.dec.DecodeAll(comp, make([]byte, 0, bd.rawSize))
	if err != nil {
		return nil, fmt.Errorf("解压快照块 %d 失败: %w", idx, err)
	}

	a.cacheMu.Lock()
	if a.blockCache != nil {
		if len(a.blockCache) >= defaultBlockCacheSz {
			var oldestKey uint32
			var oldestSeq uint64 = ^uint64(0)
			for k, v := range a.blockCache {
				if v.seq < oldestSeq {
					oldestSeq = v.seq
					oldestKey = k
				}
			}
			delete(a.blockCache, oldestKey)
		}
		a.cacheSeq++
		a.blockCache[idx] = &cachedBlock{data: raw, seq: a.cacheSeq}
	}
	a.cacheMu.Unlock()
	return raw, nil
}

// ReadBGMDigests 读取快照内嵌的 BGM 音频存在性与内容指纹清单。
func (a *Archive) ReadBGMDigests() ([]BGMDigestRecord, error) {
	raw, err := a.ReadFile(metaBGMPath)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	return DecodeBGMDigests(raw)
}

// ReadIconDigests 读取快照内嵌的全量图标跨版本指纹清单。
func (a *Archive) ReadIconDigests() ([]IconDigestRecord, error) {
	raw, err := a.ReadFile(metaIconDigestPath)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	return DecodeIconDigests(raw)
}

func (a *Archive) ensureThumbsLoaded() {
	a.thumbMu.Lock()
	defer a.thumbMu.Unlock()
	if a.thumbsReady {
		return
	}
	a.thumbsReady = true
	raw, err := a.ReadFile(metaIconThumbsPath)
	if err != nil || len(raw) == 0 {
		return
	}
	m, err := DecodeIconThumbs(raw)
	if err == nil {
		a.thumbs = m
	}
}

// ReadIconThumbBlob 按内容键读取快照中附带的变动/移除旧图标缩略图载荷。
func (a *Archive) ReadIconThumbBlob(blobKey string) ([]byte, bool) {
	if a == nil || blobKey == "" {
		return nil, false
	}
	a.ensureThumbsLoaded()
	a.thumbMu.Lock()
	defer a.thumbMu.Unlock()
	v, ok := a.thumbs[blobKey]
	return v, ok
}

// SchemasFS 返回只读 fs.FS，直接访问快照内封存的配套 EXDSchema YAML 定义（供跨大版本表结构兼容使用）。
func (a *Archive) SchemasFS() fs.FS {
	if a == nil {
		return nil
	}
	hasAny := false
	for p := range a.exactFiles {
		if strings.HasPrefix(p, "schemas/") {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return nil
	}
	return &archiveSchemasFS{a: a}
}

type archiveSchemasFS struct {
	a *Archive
}

func (s *archiveSchemasFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		entries := s.listEntries()
		return &archiveDirFile{name: ".", entries: entries}, nil
	}
	target := "schemas/" + path.Clean(name)
	data, ok, err := s.a.readExactFile(target)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &archiveMemFile{
		name: path.Base(name),
		r:    bytes.NewReader(data),
		size: int64(len(data)),
	}, nil
}

func (s *archiveSchemasFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	data, ok, err := s.a.readExactFile("schemas/" + path.Clean(name))
	if err != nil {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: err}
	}
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return data, nil
}

func (s *archiveSchemasFS) listEntries() []fs.DirEntry {
	var names []string
	for p := range s.a.exactFiles {
		if strings.HasPrefix(p, "schemas/") {
			rel := strings.TrimPrefix(p, "schemas/")
			if rel != "" && !strings.Contains(rel, "/") {
				names = append(names, rel)
			}
		}
	}
	sort.Strings(names)
	out := make([]fs.DirEntry, 0, len(names))
	for _, n := range names {
		loc := s.a.exactFiles["schemas/"+n]
		out = append(out, &archiveDirEntry{name: n, size: int64(loc.size)})
	}
	return out
}

type archiveMemFile struct {
	name string
	r    *bytes.Reader
	size int64
}

func (f *archiveMemFile) Stat() (fs.FileInfo, error) {
	return &archiveFileInfo{name: f.name, size: f.size}, nil
}
func (f *archiveMemFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *archiveMemFile) Close() error               { return nil }

type archiveDirFile struct {
	name    string
	entries []fs.DirEntry
	pos     int
}

func (d *archiveDirFile) Stat() (fs.FileInfo, error) {
	return &archiveFileInfo{name: d.name, isDir: true}, nil
}
func (d *archiveDirFile) Read([]byte) (int, error) { return 0, io.EOF }
func (d *archiveDirFile) Close() error             { return nil }
func (d *archiveDirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 {
		rest := d.entries[d.pos:]
		d.pos = len(d.entries)
		return rest, nil
	}
	if d.pos >= len(d.entries) {
		return nil, io.EOF
	}
	end := min(len(d.entries), d.pos+n)
	out := d.entries[d.pos:end]
	d.pos = end
	return out, nil
}

type archiveDirEntry struct {
	name string
	size int64
}

func (e *archiveDirEntry) Name() string      { return e.name }
func (e *archiveDirEntry) IsDir() bool       { return false }
func (e *archiveDirEntry) Type() fs.FileMode { return 0 }
func (e *archiveDirEntry) Info() (fs.FileInfo, error) {
	return &archiveFileInfo{name: e.name, size: e.size}, nil
}

type archiveFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (i *archiveFileInfo) Name() string { return i.name }
func (i *archiveFileInfo) Size() int64  { return i.size }
func (i *archiveFileInfo) Mode() fs.FileMode {
	if i.isDir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i *archiveFileInfo) ModTime() time.Time { return time.Time{} }
func (i *archiveFileInfo) IsDir() bool        { return i.isDir }
func (i *archiveFileInfo) Sys() any           { return nil }
