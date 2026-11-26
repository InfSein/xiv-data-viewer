package sqpack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"sort"
	"strings"
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

	// 预计算每个分组编号的 1000 个标准与高清文件名哈希，避免跨变体重复计算 CRC32
	var stdFileHashes [IconGroupCount][IconGroupSize]uint32
	var hr1FileHashes [IconGroupCount][IconGroupSize]uint32
	// 延迟初始化：仅当某分组存在至少一个变体目录时填充该分组的文件名哈希
	var groupHashed [IconGroupCount]bool

	ensureGroupHashes := func(g int) {
		if groupHashed[g] {
			return
		}
		base := g * IconGroupSize
		for sub := 0; sub < IconGroupSize; sub++ {
			id := base + sub
			stdName := fmt.Sprintf("%06d.tex", id)
			hr1Name := fmt.Sprintf("%06d_hr1.tex", id)
			stdFileHashes[g][sub] = JamCRC32([]byte(stdName))
			hr1FileHashes[g][sub] = JamCRC32([]byte(hr1Name))
		}
		groupHashed[g] = true
	}

	type rawCandidate struct {
		ar         *ArchiveReader
		dataFileID uint32
		offset     uint64
		entry      IconFileEntry
	}

	var matched []rawCandidate
	seenPath := make(map[string]bool)

	for _, cand := range cands {
		ar, err := reader.getArchive(cand)
		if err != nil {
			return nil, err
		}

		if !ar.useIndex2 {
			validDirs := make(map[uint32]bool, 2048)
			for h := range ar.indexEntries {
				validDirs[uint32(h>>32)] = true
			}
			for g := 0; g < IconGroupCount; g++ {
				groupID := g * IconGroupSize
				groupCode := fmt.Sprintf("%06d", groupID)
				for _, v := range KnownIconVariants {
					dirPath := "ui/icon/" + groupCode
					if v != "" {
						dirPath += "/" + v
					}
					dh := JamCRC32([]byte(dirPath))
					if !validDirs[dh] {
						continue
					}
					ensureGroupHashes(g)
					dirPrefix := uint64(dh) << 32
					for sub := 0; sub < IconGroupSize; sub++ {
						id := groupID + sub
						if e, ok := ar.indexEntries[dirPrefix|uint64(stdFileHashes[g][sub])]; ok {
							p := buildIconPath(id, v, false, "tex")
							if !seenPath[p] {
								seenPath[p] = true
								matched = append(matched, rawCandidate{
									ar:         ar,
									dataFileID: e.dataFileID,
									offset:     e.offset,
									entry: IconFileEntry{
										IconID:  id,
										GroupID: groupID,
										Variant: v,
										HR1:     false,
										Path:    p,
									},
								})
							}
						}
						if e, ok := ar.indexEntries[dirPrefix|uint64(hr1FileHashes[g][sub])]; ok {
							p := buildIconPath(id, v, true, "tex")
							if !seenPath[p] {
								seenPath[p] = true
								matched = append(matched, rawCandidate{
									ar:         ar,
									dataFileID: e.dataFileID,
									offset:     e.offset,
									entry: IconFileEntry{
										IconID:  id,
										GroupID: groupID,
										Variant: v,
										HR1:     true,
										Path:    p,
									},
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
			for _, v := range KnownIconVariants {
				for sub := 0; sub < IconGroupSize; sub++ {
					id := groupID + sub
					for _, hr1 := range []bool{false, true} {
						p := buildIconPath(id, v, hr1, "tex")
						if seenPath[p] {
							continue
						}
						if e, ok := ar.lookup(p); ok {
							seenPath[p] = true
							matched = append(matched, rawCandidate{
								ar:         ar,
								dataFileID: e.dataFileID,
								offset:     e.offset,
								entry: IconFileEntry{
									IconID:  id,
									GroupID: groupID,
									Variant: v,
									HR1:     hr1,
									Path:    p,
								},
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

	// 按物理归档与偏移升序排列，使 .dat 文件头校验保持顺序 I/O
	arOrder := make(map[*ArchiveReader]int, len(cands))
	for i := range matched {
		if _, ok := arOrder[matched[i].ar]; !ok {
			arOrder[matched[i].ar] = len(arOrder)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		ai, aj := arOrder[matched[i].ar], arOrder[matched[j].ar]
		if ai != aj {
			return ai < aj
		}
		if matched[i].dataFileID != matched[j].dataFileID {
			return matched[i].dataFileID < matched[j].dataFileID
		}
		return matched[i].offset < matched[j].offset
	})

	out := make([]IconFileEntry, 0, len(matched))
	var hdr [12]byte
	for _, m := range matched {
		f, err := m.ar.getDataFile(m.dataFileID)
		if err != nil {
			continue
		}
		if err := readAtFull(f, hdr[:], int64(m.offset)); err != nil {
			continue
		}
		fileType := binary.LittleEndian.Uint32(hdr[4:8])
		rawSize := binary.LittleEndian.Uint32(hdr[8:12])
		if fileType != fileTypeTexture || rawSize == 0 {
			continue
		}
		out = append(out, m.entry)
	}

	variantOrder := make(map[string]int, len(KnownIconVariants))
	for idx, v := range KnownIconVariants {
		variantOrder[v] = idx
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IconID != out[j].IconID {
			return out[i].IconID < out[j].IconID
		}
		vi, vj := variantOrder[out[i].Variant], variantOrder[out[j].Variant]
		if vi != vj {
			return vi < vj
		}
		return !out[i].HR1 && out[j].HR1
	})

	return out, nil
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
