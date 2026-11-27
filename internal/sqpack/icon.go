package sqpack

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const (
	// MaxIconID 表示 FF14 图标 6 位数字编号空间上限（000000 ~ 999999），与 SaintCoinach 一致。
	MaxIconID = 999999
	// IconGroupSize 表示每个图标目录分组包含的连续 ID 数量。
	IconGroupSize = 1000
	// IconGroupCount 表示 000000 ~ 999000 的全部分组总数。
	IconGroupCount = 1000
)

// KnownIconVariants 按 SaintCoinach 规范顺序定义图标子目录变体（"" 表示分组根目录）。
var KnownIconVariants = []string{
	"",
	"hq",
	"chs",
	"en",
	"ja",
	"de",
	"fr",
	"cht",
	"ko",
	"tc",
}

// IconFileEntry 描述单个在 SqPack 中实际存在的图标贴图资源条目。
type IconFileEntry struct {
	IconID  int
	GroupID int
	Variant string
	HR1     bool
	Path    string
}

// IconGroupID 返回图标编号所属的 1000 步长分组起始编号（例如 37618 -> 37000）。
func IconGroupID(iconID int) int {
	if iconID < 0 {
		return 0
	}
	return (iconID / IconGroupSize) * IconGroupSize
}

// IconGroupCode 返回 6 位补零的分组目录名（例如 37618 -> "037000"）。
func IconGroupCode(groupID int) string {
	return fmt.Sprintf("%06d", IconGroupID(groupID))
}

// IconResourcePath 生成对应图标在 SqPack 内的标准虚拟路径。
func IconResourcePath(iconID int, variant string, hr1 bool) string {
	return buildIconPath(iconID, variant, hr1, "tex")
}

// IconExportRelPath 生成遵循 SaintCoinach 规范的 PNG 导出相对路径（如 "ui/icon/037000/037618.png"）。
func IconExportRelPath(iconID int, variant string, hr1 bool) string {
	return buildIconPath(iconID, variant, hr1, "png")
}

func buildIconPath(iconID int, variant string, hr1 bool, ext string) string {
	group := IconGroupCode(iconID)
	v := strings.Trim(strings.ToLower(strings.TrimSpace(variant)), "/")
	suffix := ""
	if hr1 {
		suffix = "_hr1"
	}
	if v == "" {
		return fmt.Sprintf("ui/icon/%s/%06d%s.%s", group, iconID, suffix, ext)
	}
	return fmt.Sprintf("ui/icon/%s/%s/%06d%s.%s", group, v, iconID, suffix, ext)
}

// TexFormatName 返回 FF14 .tex 贴图格式编号的可读名称。
func TexFormatName(format uint32) string {
	switch format {
	case TexFormatL8:
		return "L8"
	case TexFormatA8:
		return "A8"
	case TexFormatB4G4R4A4:
		return "BGRA4444"
	case TexFormatB5G5R5A1:
		return "BGRA5551"
	case TexFormatB8G8R8A8:
		return "BGRA8"
	case TexFormatB8G8R8X8:
		return "BGRX8"
	case TexFormatDXT1:
		return "DXT1"
	case TexFormatDXT3:
		return "DXT3"
	case TexFormatDXT5:
		return "DXT5"
	case TexFormatBC4:
		return "BC4"
	case TexFormatBC5:
		return "BC5"
	case TexFormatBC7:
		return "BC7"
	default:
		return fmt.Sprintf("0x%X", format)
	}
}

func writeSixDigits(dst []byte, n int) {
	dst[5] = byte('0' + n%10)
	n /= 10
	dst[4] = byte('0' + n%10)
	n /= 10
	dst[3] = byte('0' + n%10)
	n /= 10
	dst[2] = byte('0' + n%10)
	n /= 10
	dst[1] = byte('0' + n%10)
	n /= 10
	dst[0] = byte('0' + n%10)
}

func buildIconTexPathFast(iconID int, variant string, hr1 bool) string {
	groupID := IconGroupID(iconID)
	var buf [48]byte
	copy(buf[0:8], "ui/icon/")
	writeSixDigits(buf[8:14], groupID)
	pos := 14
	buf[pos] = '/'
	pos++
	if variant != "" {
		pos += copy(buf[pos:], variant)
		buf[pos] = '/'
		pos++
	}
	writeSixDigits(buf[pos:pos+6], iconID)
	pos += 6
	if hr1 {
		pos += copy(buf[pos:], "_hr1.tex")
	} else {
		pos += copy(buf[pos:], ".tex")
	}
	return string(buf[:pos])
}

// ScanIconFiles 只读扫描指定客户端 SqPack 0600 归档中全部真实存在的有效图标资源，自动剔除空占位条目 (fileTypeEmpty)。
func ScanIconFiles(reader *GameReader) ([]IconFileEntry, error) {
	if reader == nil {
		return nil, nil
	}
	directory := filepath.Join(reader.gamePath, "sqpack", "ffxiv")
	cands, err := reader.getCandidates(directory, "0600")
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, nil
	}

	// 预计算每个分组编号的 1000 个标准与高清文件名哈希（栈缓冲零堆分配）
	var stdFileHashes [IconGroupCount][IconGroupSize]uint32
	var hr1FileHashes [IconGroupCount][IconGroupSize]uint32
	var groupHashed [IconGroupCount]bool

	ensureGroupHashes := func(g int) {
		if groupHashed[g] {
			return
		}
		base := g * IconGroupSize
		var stdBuf [10]byte
		copy(stdBuf[6:], ".tex")
		var hr1Buf [14]byte
		copy(hr1Buf[6:], "_hr1.tex")
		for sub := 0; sub < IconGroupSize; sub++ {
			id := base + sub
			writeSixDigits(stdBuf[:6], id)
			copy(hr1Buf[:6], stdBuf[:6])
			stdFileHashes[g][sub] = JamCRC32(stdBuf[:])
			hr1FileHashes[g][sub] = JamCRC32(hr1Buf[:])
		}
		groupHashed[g] = true
	}

	type rawCandidate struct {
		ar         *ArchiveReader
		arIdx      uint16
		dataFileID uint32
		offset     uint64
		iconID     uint32
		variantIdx uint8
		hr1        bool
	}

	matched := make([]rawCandidate, 0, 210000)
	// 用整数键 (iconID<<8 | variantIdx<<1 | hr1Bit) 去重，避免 20 万次字符串分配
	seenKey := make(map[uint64]struct{}, 210000)
	makeDedupKey := func(id int, vIdx int, hr1 bool) uint64 {
		k := (uint64(id) << 8) | (uint64(vIdx) << 1)
		if hr1 {
			k |= 1
		}
		return k
	}

	var dirBuf [32]byte
	copy(dirBuf[0:8], "ui/icon/")

	for arIdx, cand := range cands {
		ar, err := reader.getArchive(cand)
		if err != nil {
			return nil, err
		}

		if !ar.useIndex2 {
			validDirs := make(map[uint32]struct{}, 2048)
			for h := range ar.indexEntries {
				validDirs[uint32(h>>32)] = struct{}{}
			}
			for g := 0; g < IconGroupCount; g++ {
				groupID := g * IconGroupSize
				writeSixDigits(dirBuf[8:14], groupID)
				for vIdx, v := range KnownIconVariants {
					dirLen := 14
					if v != "" {
						dirBuf[14] = '/'
						dirLen = 15 + copy(dirBuf[15:], v)
					}
					dh := JamCRC32(dirBuf[:dirLen])
					if _, ok := validDirs[dh]; !ok {
						continue
					}
					ensureGroupHashes(g)
					dirPrefix := uint64(dh) << 32
					for sub := 0; sub < IconGroupSize; sub++ {
						id := groupID + sub
						if e, ok := ar.indexEntries[dirPrefix|uint64(stdFileHashes[g][sub])]; ok {
							dk := makeDedupKey(id, vIdx, false)
							if _, dup := seenKey[dk]; !dup {
								seenKey[dk] = struct{}{}
								matched = append(matched, rawCandidate{
									ar:         ar,
									arIdx:      uint16(arIdx),
									dataFileID: e.dataFileID,
									offset:     e.offset,
									iconID:     uint32(id),
									variantIdx: uint8(vIdx),
									hr1:        false,
								})
							}
						}
						if e, ok := ar.indexEntries[dirPrefix|uint64(hr1FileHashes[g][sub])]; ok {
							dk := makeDedupKey(id, vIdx, true)
							if _, dup := seenKey[dk]; !dup {
								seenKey[dk] = struct{}{}
								matched = append(matched, rawCandidate{
									ar:         ar,
									arIdx:      uint16(arIdx),
									dataFileID: e.dataFileID,
									offset:     e.offset,
									iconID:     uint32(id),
									variantIdx: uint8(vIdx),
									hr1:        true,
								})
							}
						}
					}
				}
			}
			continue
		}

		// .index2 回退路径：无独立目录哈希，直接按已知变体路径查表
		for g := 0; g < IconGroupCount; g++ {
			groupID := g * IconGroupSize
			for vIdx, v := range KnownIconVariants {
				for sub := 0; sub < IconGroupSize; sub++ {
					id := groupID + sub
					for _, hr1 := range []bool{false, true} {
						dk := makeDedupKey(id, vIdx, hr1)
						if _, dup := seenKey[dk]; dup {
							continue
						}
						p := buildIconTexPathFast(id, v, hr1)
						if e, ok := ar.lookup(p); ok {
							seenKey[dk] = struct{}{}
							matched = append(matched, rawCandidate{
								ar:         ar,
								arIdx:      uint16(arIdx),
								dataFileID: e.dataFileID,
								offset:     e.offset,
								iconID:     uint32(id),
								variantIdx: uint8(vIdx),
								hr1:        hr1,
							})
						}
					}
				}
			}
		}
	}

	if len(matched) == 0 {
		return nil, nil
	}

	// 按物理归档、数据文件号与偏移升序排列，便于合并窗口顺序读取
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].arIdx != matched[j].arIdx {
			return matched[i].arIdx < matched[j].arIdx
		}
		if matched[i].dataFileID != matched[j].dataFileID {
			return matched[i].dataFileID < matched[j].dataFileID
		}
		return matched[i].offset < matched[j].offset
	})

	// 按 (arIdx, dataFileID) 分组：不同 .dat 文件并发处理，同一 .dat 文件内单协程顺序读取，
	// 避免 Windows 下多协程共享同一文件句柄调用 ReadAt 触发锁竞争，同时使用 64KB 顺序窗口大幅减少系统调用次数。
	type verifyGroup struct {
		items []rawCandidate
	}
	var groups []verifyGroup
	for start := 0; start < len(matched); {
		end := start + 1
		aIdx := matched[start].arIdx
		dID := matched[start].dataFileID
		for end < len(matched) && matched[end].arIdx == aIdx && matched[end].dataFileID == dID {
			end++
		}
		groups = append(groups, verifyGroup{items: matched[start:end]})
		start = end
	}

	type validItem struct {
		iconID     uint32
		variantIdx uint8
		hr1        bool
	}
	groupResults := make([][]validItem, len(groups))

	verifyOneFile := func(items []rawCandidate) []validItem {
		if len(items) == 0 {
			return nil
		}
		f, err := items[0].ar.getDataFile(items[0].dataFileID)
		if err != nil {
			return nil
		}
		const winSize = 64 * 1024
		buf := make([]byte, winSize)
		var winStart int64 = -1
		winEnd := int64(-1)
		local := make([]validItem, 0, len(items))

		for _, m := range items {
			off := int64(m.offset)
			if off < winStart || off+12 > winEnd {
				n, rErr := f.ReadAt(buf, off)
				if n < 12 || (rErr != nil && rErr != io.EOF) {
					winStart, winEnd = -1, -1
					continue
				}
				winStart = off
				winEnd = off + int64(n)
			}
			rel := off - winStart
			fileType := binary.LittleEndian.Uint32(buf[rel+4 : rel+8])
			rawSize := binary.LittleEndian.Uint32(buf[rel+8 : rel+12])
			if fileType != fileTypeTexture || rawSize == 0 {
				continue
			}
			local = append(local, validItem{
				iconID:     m.iconID,
				variantIdx: m.variantIdx,
				hr1:        m.hr1,
			})
		}
		return local
	}

	if len(groups) == 1 {
		groupResults[0] = verifyOneFile(groups[0].items)
	} else {
		workers := min(len(groups), min(4, max(2, runtime.NumCPU())))
		jobCh := make(chan int, len(groups))
		for i := range groups {
			jobCh <- i
		}
		close(jobCh)
		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				for idx := range jobCh {
					groupResults[idx] = verifyOneFile(groups[idx].items)
				}
			}()
		}
		wg.Wait()
	}

	totalValid := 0
	for _, gr := range groupResults {
		totalValid += len(gr)
	}
	valid := make([]validItem, 0, totalValid)
	for _, gr := range groupResults {
		valid = append(valid, gr...)
	}

	sort.Slice(valid, func(i, j int) bool {
		if valid[i].iconID != valid[j].iconID {
			return valid[i].iconID < valid[j].iconID
		}
		if valid[i].variantIdx != valid[j].variantIdx {
			return valid[i].variantIdx < valid[j].variantIdx
		}
		return !valid[i].hr1 && valid[j].hr1
	})

	out := make([]IconFileEntry, len(valid))
	for i, v := range valid {
		id := int(v.iconID)
		variant := KnownIconVariants[v.variantIdx]
		out[i] = IconFileEntry{
			IconID:  id,
			GroupID: IconGroupID(id),
			Variant: variant,
			HR1:     v.hr1,
			Path:    buildIconTexPathFast(id, variant, v.hr1),
		}
	}
	return out, nil
}

const (
	iconScanCacheMagic      = "XIVICN01"
	iconScanCacheHeaderSize = 8 + 32 + 4
	iconScanCacheEntrySize  = 6
)

// computeIconScanFingerprint 基于客户端 0600 图标索引文件及版本文件的只读元数据计算指纹，
// 客户端更新后指纹自动变化以触发重扫，绝不修改游戏目录。
func computeIconScanFingerprint(reader *GameReader) ([32]byte, bool) {
	var zero [32]byte
	if reader == nil || strings.TrimSpace(reader.gamePath) == "" {
		return zero, false
	}
	directory := filepath.Join(reader.gamePath, "sqpack", "ffxiv")
	cands, err := reader.getCandidates(directory, "0600")
	if err != nil || len(cands) == 0 {
		return zero, false
	}

	h := sha256.New()
	_, _ = h.Write([]byte(filepath.Clean(reader.gamePath)))
	_, _ = h.Write([]byte{0})

	if verBytes, err := os.ReadFile(filepath.Join(reader.gamePath, "ffxivgame.ver")); err == nil {
		_, _ = h.Write(bytes.TrimSpace(verBytes))
	}
	_, _ = h.Write([]byte{0})

	for _, cand := range cands {
		ext := ".win32.index"
		if cand.useIndex2 {
			ext = ".win32.index2"
		}
		idxPath := cand.prefix + ext
		st, err := os.Stat(idxPath)
		if err != nil {
			return zero, false
		}
		var meta [16]byte
		binary.LittleEndian.PutUint64(meta[0:8], uint64(st.Size()))
		binary.LittleEndian.PutUint64(meta[8:16], uint64(st.ModTime().UnixNano()))
		_, _ = h.Write([]byte(filepath.Base(idxPath)))
		_, _ = h.Write(meta[:])
	}

	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, true
}

func loadIconScanCache(cachePath string, wantFP [32]byte) ([]IconFileEntry, bool) {
	raw, err := os.ReadFile(cachePath)
	if err != nil || len(raw) < iconScanCacheHeaderSize {
		return nil, false
	}
	if string(raw[0:8]) != iconScanCacheMagic {
		return nil, false
	}
	if !bytes.Equal(raw[8:40], wantFP[:]) {
		return nil, false
	}
	count := int(binary.LittleEndian.Uint32(raw[40:44]))
	if count < 0 || iconScanCacheHeaderSize+count*iconScanCacheEntrySize != len(raw) {
		return nil, false
	}

	out := make([]IconFileEntry, count)
	pos := iconScanCacheHeaderSize
	for i := 0; i < count; i++ {
		id := int(binary.LittleEndian.Uint32(raw[pos : pos+4]))
		vIdx := int(raw[pos+4])
		hr1 := raw[pos+5] != 0
		pos += iconScanCacheEntrySize
		if vIdx >= len(KnownIconVariants) || id < 0 || id > MaxIconID {
			return nil, false
		}
		variant := KnownIconVariants[vIdx]
		out[i] = IconFileEntry{
			IconID:  id,
			GroupID: IconGroupID(id),
			Variant: variant,
			HR1:     hr1,
			Path:    buildIconTexPathFast(id, variant, hr1),
		}
	}
	return out, true
}

func saveIconScanCache(cachePath string, fp [32]byte, files []IconFileEntry) {
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	variantToIdx := make(map[string]uint8, len(KnownIconVariants))
	for idx, v := range KnownIconVariants {
		variantToIdx[v] = uint8(idx)
	}

	buf := make([]byte, iconScanCacheHeaderSize+len(files)*iconScanCacheEntrySize)
	copy(buf[0:8], iconScanCacheMagic)
	copy(buf[8:40], fp[:])
	binary.LittleEndian.PutUint32(buf[40:44], uint32(len(files)))
	pos := iconScanCacheHeaderSize
	for _, f := range files {
		vIdx, ok := variantToIdx[f.Variant]
		if !ok {
			return
		}
		binary.LittleEndian.PutUint32(buf[pos:pos+4], uint32(f.IconID))
		buf[pos+4] = vIdx
		if f.HR1 {
			buf[pos+5] = 1
		}
		pos += iconScanCacheEntrySize
	}

	tmpPath := fmt.Sprintf("%s.%d.tmp", cachePath, os.Getpid())
	if err := os.WriteFile(tmpPath, buf, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		_ = os.Remove(tmpPath)
	}
}

// ScanIconFilesCached 带本地版本指纹缓存的只读图标扫描；cacheDir 为空时退化为直接扫描。
func ScanIconFilesCached(reader *GameReader, cacheDir string) ([]IconFileEntry, error) {
	if reader == nil {
		return nil, nil
	}
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return ScanIconFiles(reader)
	}

	fp, ok := computeIconScanFingerprint(reader)
	if !ok {
		return ScanIconFiles(reader)
	}
	cachePath := filepath.Join(cacheDir, "icon-scan-"+hex.EncodeToString(fp[:8])+".bin")
	if cached, hit := loadIconScanCache(cachePath, fp); hit {
		return cached, nil
	}

	files, err := ScanIconFiles(reader)
	if err != nil {
		return nil, err
	}
	saveIconScanCache(cachePath, fp, files)
	return files, nil
}

// ReadIconVariantRaw 读取指定图标变体的原始 .tex 字节与贴图文件头（支持 iconID == 0）。
func ReadIconVariantRaw(reader *GameReader, iconID int, variant string, hr1 bool) ([]byte, TextureHeader, error) {
	if reader == nil || iconID < 0 {
		return nil, TextureHeader{}, nil
	}
	p := IconResourcePath(iconID, variant, hr1)
	raw, err := reader.ReadFile(p)
	if err != nil || len(raw) == 0 {
		return nil, TextureHeader{}, err
	}
	hdr, err := ParseTextureHeader(raw)
	if err != nil {
		return nil, TextureHeader{}, err
	}
	return raw, hdr, nil
}

// ReadIconVariantImage 读取指定图标变体并解码为预乘 *image.RGBA；当 maxThumbSide > 0 且贴图超大时按比例缩小以节省 GPU 显存。
func ReadIconVariantImage(reader *GameReader, cache *TextureCache, iconID int, variant string, hr1 bool, maxThumbSide int) (*image.RGBA, TextureHeader, error) {
	if reader == nil || iconID < 0 {
		return nil, TextureHeader{}, nil
	}
	vNorm := strings.Trim(strings.ToLower(strings.TrimSpace(variant)), "/")
	cacheKey := fmt.Sprintf("iconvar:%d:%s:%v:%d", iconID, vNorm, hr1, maxThumbSide)
	if cache != nil {
		if img, ok := cache.get(cacheKey); ok {
			return img, TextureHeader{Width: uint16(img.Bounds().Dx()), Height: uint16(img.Bounds().Dy())}, nil
		}
	}

	raw, hdr, err := ReadIconVariantRaw(reader, iconID, vNorm, hr1)
	if err != nil || len(raw) == 0 {
		return nil, hdr, err
	}
	img, err := DecodeTexToRGBA(raw)
	if err != nil {
		return nil, hdr, err
	}
	if maxThumbSide > 0 {
		img = downscaleRGBAIfNeeded(img, maxThumbSide)
	}
	if cache != nil {
		cache.set(cacheKey, img)
	}
	return img, hdr, nil
}

// downscaleRGBAIfNeeded 使用双线性区域采样将超大图按比例缩放到 maxSide 以内（仅用于网格缩略图）。
func downscaleRGBAIfNeeded(src *image.RGBA, maxSide int) *image.RGBA {
	if src == nil || maxSide <= 0 {
		return src
	}
	sw := src.Bounds().Dx()
	sh := src.Bounds().Dy()
	if sw <= maxSide && sh <= maxSide {
		return src
	}
	var dw, dh int
	if sw >= sh {
		dw = maxSide
		dh = max(1, (sh*maxSide+sw/2)/sw)
	} else {
		dh = maxSide
		dw = max(1, (sw*maxSide+sh/2)/sh)
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy0 := (y * sh) / dh
		sy1 := max(sy0+1, ((y+1)*sh)/dh)
		if sy1 > sh {
			sy1 = sh
		}
		for x := 0; x < dw; x++ {
			sx0 := (x * sw) / dw
			sx1 := max(sx0+1, ((x+1)*sw)/dw)
			if sx1 > sw {
				sx1 = sw
			}
			var rSum, gSum, bSum, aSum, cnt int
			for sy := sy0; sy < sy1; sy++ {
				rowOff := sy * src.Stride
				for sx := sx0; sx < sx1; sx++ {
					off := rowOff + sx*4
					rSum += int(src.Pix[off])
					gSum += int(src.Pix[off+1])
					bSum += int(src.Pix[off+2])
					aSum += int(src.Pix[off+3])
					cnt++
				}
			}
			if cnt > 0 {
				dOff := y*dst.Stride + x*4
				dst.Pix[dOff] = byte(rSum / cnt)
				dst.Pix[dOff+1] = byte(gSum / cnt)
				dst.Pix[dOff+2] = byte(bSum / cnt)
				dst.Pix[dOff+3] = byte(aSum / cnt)
			}
		}
	}
	return dst
}

// DecodeTexToPNGWithCompression 将 FF14 .tex 原始字节解码为无损直通 Alpha PNG 字节，并支持指定压缩级别。
func DecodeTexToPNGWithCompression(raw []byte, level png.CompressionLevel) ([]byte, error) {
	img, err := DecodeTexToNRGBA(raw)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: level}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
