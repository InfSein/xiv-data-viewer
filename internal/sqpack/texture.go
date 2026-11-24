package sqpack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"regexp"
	"strings"
	"sync"
)

// FF14 .tex 贴图格式枚举值（与 @ffcafe/elpis-sqpack 一致）。
const (
	TexFormatL8       uint32 = 4400
	TexFormatA8       uint32 = 4401
	TexFormatB4G4R4A4 uint32 = 5184
	TexFormatB5G5R5A1 uint32 = 5185
	TexFormatB8G8R8A8 uint32 = 5200
	TexFormatB8G8R8X8 uint32 = 5201
	TexFormatDXT1     uint32 = 13344
	TexFormatDXT3     uint32 = 13360
	TexFormatDXT5     uint32 = 13361
	TexFormatBC4      uint32 = 24864
	TexFormatBC5      uint32 = 25136
	TexFormatBC7      uint32 = 25650
)

// TextureHeader 描述 FF14 .tex 文件的 80 字节头部结构。
type TextureHeader struct {
	Attribute      uint32
	Format         uint32
	Width          uint16
	Height         uint16
	Depth          uint16
	MipLevels      uint8
	ArraySize      uint8
	LodOffsets     [3]uint32
	SurfaceOffsets [13]uint32
}

// ParseTextureHeader 解析 80 字节 .tex 贴图文件头。
func ParseTextureHeader(raw []byte) (TextureHeader, error) {
	if len(raw) < textureHeaderSize {
		return TextureHeader{}, errors.New("贴图文件长度不足 80 字节")
	}
	h := TextureHeader{
		Attribute: binary.LittleEndian.Uint32(raw[0:4]),
		Format:    binary.LittleEndian.Uint32(raw[4:8]),
		Width:     binary.LittleEndian.Uint16(raw[8:10]),
		Height:    binary.LittleEndian.Uint16(raw[10:12]),
		Depth:     binary.LittleEndian.Uint16(raw[12:14]),
		MipLevels: raw[14],
		ArraySize: raw[15],
	}
	for i := 0; i < 3; i++ {
		h.LodOffsets[i] = binary.LittleEndian.Uint32(raw[16+i*4 : 20+i*4])
	}
	for i := 0; i < 13; i++ {
		h.SurfaceOffsets[i] = binary.LittleEndian.Uint32(raw[28+i*4 : 32+i*4])
	}
	return h, nil
}

// decodeTexPixels 将 FF14 .tex 原始字节解码为非预乘的直通 RGBA 像素切片（取最高清晰度 Mip 0）。
func decodeTexPixels(raw []byte) (int, int, []byte, error) {
	hdr, err := ParseTextureHeader(raw)
	if err != nil {
		return 0, 0, nil, err
	}
	w := int(hdr.Width)
	h := int(hdr.Height)
	if w <= 0 || h <= 0 {
		return 0, 0, nil, fmt.Errorf("无效贴图尺寸: %dx%d", w, h)
	}
	dataOff := int(hdr.SurfaceOffsets[0])
	if dataOff < textureHeaderSize || dataOff >= len(raw) {
		dataOff = textureHeaderSize
	}
	src := raw[dataOff:]
	pix := make([]byte, 4*w*h)

	switch hdr.Format {
	case TexFormatB8G8R8A8:
		decodeBGRA8(src, pix, w*h, false)
	case TexFormatB8G8R8X8:
		decodeBGRA8(src, pix, w*h, true)
	case TexFormatB4G4R4A4:
		decodeBGRA4444(src, pix, w*h)
	case TexFormatB5G5R5A1:
		decodeBGRA5551(src, pix, w*h)
	case TexFormatL8:
		decodeL8(src, pix, w*h)
	case TexFormatA8:
		decodeA8(src, pix, w*h)
	case TexFormatDXT1:
		decodeDXT1(src, pix, w, h)
	case TexFormatDXT3:
		decodeDXT3(src, pix, w, h)
	case TexFormatDXT5:
		decodeDXT5(src, pix, w, h)
	case TexFormatBC4:
		decodeBC4(src, pix, w, h)
	case TexFormatBC5:
		decodeBC5(src, pix, w, h)
	case TexFormatBC7:
		decodeBC7(src, pix, w, h)
	default:
		return 0, 0, nil, fmt.Errorf("暂不支持的 .tex 贴图格式: %d (0x%x)", hdr.Format, hdr.Format)
	}

	return w, h, pix, nil
}

// premultiplyRGBA 原地将直通 RGBA 像素预乘为 Go image.RGBA 规范要求的预乘 Alpha 像素。
// .tex 原始数据存储直通色彩（透明区 RGB 可能为 255），若直接作为预乘 *image.RGBA 提交给 GPU 混合，
// 会导致透明像素在深色背景上叠加出纯白或白边光晕。
func premultiplyRGBA(pix []byte) {
	for i := 0; i+3 < len(pix); i += 4 {
		a := uint32(pix[i+3])
		switch a {
		case 255:
			continue
		case 0:
			pix[i] = 0
			pix[i+1] = 0
			pix[i+2] = 0
		default:
			pix[i] = byte((uint32(pix[i])*a + 127) / 255)
			pix[i+1] = byte((uint32(pix[i+1])*a + 127) / 255)
			pix[i+2] = byte((uint32(pix[i+2])*a + 127) / 255)
		}
	}
}

// DecodeTexToNRGBA 将 FF14 .tex 原始字节解码为非预乘的直通 *image.NRGBA 图像（取最高清晰度 Mip 0）。
func DecodeTexToNRGBA(raw []byte) (*image.NRGBA, error) {
	w, h, pix, err := decodeTexPixels(raw)
	if err != nil {
		return nil, err
	}
	return &image.NRGBA{
		Pix:    pix,
		Stride: 4 * w,
		Rect:   image.Rect(0, 0, w, h),
	}, nil
}

// DecodeTexToRGBA 将 FF14 .tex 原始字节解码并预乘 Alpha，返回标准 *image.RGBA 图像（取最高清晰度 Mip 0）。
func DecodeTexToRGBA(raw []byte) (*image.RGBA, error) {
	w, h, pix, err := decodeTexPixels(raw)
	if err != nil {
		return nil, err
	}
	premultiplyRGBA(pix)
	return &image.RGBA{
		Pix:    pix,
		Stride: 4 * w,
		Rect:   image.Rect(0, 0, w, h),
	}, nil
}

// DecodeTexToPNG 将 FF14 .tex 原始字节解码并编码为无损直通 Alpha PNG 字节数组。
func DecodeTexToPNG(raw []byte) ([]byte, error) {
	img, err := DecodeTexToNRGBA(raw)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeBGRA8(src, dst []byte, pixels int, opaque bool) {
	n := min(pixels, len(src)/4)
	for i := 0; i < n; i++ {
		si := i * 4
		dst[si] = src[si+2]   // R
		dst[si+1] = src[si+1] // G
		dst[si+2] = src[si]   // B
		if opaque {
			dst[si+3] = 255
		} else {
			dst[si+3] = src[si+3] // A
		}
	}
}

func decodeBGRA4444(src, dst []byte, pixels int) {
	n := min(pixels, len(src)/2)
	for i := 0; i < n; i++ {
		v := binary.LittleEndian.Uint16(src[i*2 : i*2+2])
		b := byte((v & 0x000F) * 17)
		g := byte(((v >> 4) & 0x000F) * 17)
		r := byte(((v >> 8) & 0x000F) * 17)
		a := byte(((v >> 12) & 0x000F) * 17)
		di := i * 4
		dst[di] = r
		dst[di+1] = g
		dst[di+2] = b
		dst[di+3] = a
	}
}

func decodeBGRA5551(src, dst []byte, pixels int) {
	n := min(pixels, len(src)/2)
	for i := 0; i < n; i++ {
		v := binary.LittleEndian.Uint16(src[i*2 : i*2+2])
		b5 := v & 0x1F
		g5 := (v >> 5) & 0x1F
		r5 := (v >> 10) & 0x1F
		a := byte(0)
		if (v & 0x8000) != 0 {
			a = 255
		}
		di := i * 4
		dst[di] = byte((r5 << 3) | (r5 >> 2))
		dst[di+1] = byte((g5 << 3) | (g5 >> 2))
		dst[di+2] = byte((b5 << 3) | (b5 >> 2))
		dst[di+3] = a
	}
}

func decodeL8(src, dst []byte, pixels int) {
	n := min(pixels, len(src))
	for i := 0; i < n; i++ {
		l := src[i]
		di := i * 4
		dst[di] = l
		dst[di+1] = l
		dst[di+2] = l
		dst[di+3] = 255
	}
}

func decodeA8(src, dst []byte, pixels int) {
	n := min(pixels, len(src))
	for i := 0; i < n; i++ {
		di := i * 4
		dst[di] = 255
		dst[di+1] = 255
		dst[di+2] = 255
		dst[di+3] = src[i]
	}
}

func unpackRGB565(c uint16) (r, g, b byte) {
	r5 := (c >> 11) & 0x1F
	g6 := (c >> 5) & 0x3F
	b5 := c & 0x1F
	r = byte((r5 << 3) | (r5 >> 2))
	g = byte((g6 << 2) | (g6 >> 4))
	b = byte((b5 << 3) | (b5 >> 2))
	return
}

func decodeDXT1ColorBlock(block []byte, allowAlpha bool) [4][4]byte {
	c0 := binary.LittleEndian.Uint16(block[0:2])
	c1 := binary.LittleEndian.Uint16(block[2:4])
	r0, g0, b0 := unpackRGB565(c0)
	r1, g1, b1 := unpackRGB565(c1)

	var palette [4][4]byte
	palette[0] = [4]byte{r0, g0, b0, 255}
	palette[1] = [4]byte{r1, g1, b1, 255}
	if c0 > c1 || !allowAlpha {
		palette[2] = [4]byte{
			byte((2*uint16(r0) + uint16(r1) + 1) / 3),
			byte((2*uint16(g0) + uint16(g1) + 1) / 3),
			byte((2*uint16(b0) + uint16(b1) + 1) / 3),
			255,
		}
		palette[3] = [4]byte{
			byte((uint16(r0) + 2*uint16(r1) + 1) / 3),
			byte((uint16(g0) + 2*uint16(g1) + 1) / 3),
			byte((uint16(b0) + 2*uint16(b1) + 1) / 3),
			255,
		}
	} else {
		palette[2] = [4]byte{
			byte((uint16(r0) + uint16(r1)) / 2),
			byte((uint16(g0) + uint16(g1)) / 2),
			byte((uint16(b0) + uint16(b1)) / 2),
			255,
		}
		palette[3] = [4]byte{0, 0, 0, 0}
	}
	return palette
}

func decodeDXT1(src, dst []byte, w, h int) {
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	pos := 0
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			if pos+8 > len(src) {
				return
			}
			block := src[pos : pos+8]
			pos += 8
			palette := decodeDXT1ColorBlock(block, true)
			bits := binary.LittleEndian.Uint32(block[4:8])
			for py := 0; py < 4; py++ {
				y := by*4 + py
				if y >= h {
					continue
				}
				for px := 0; px < 4; px++ {
					x := bx*4 + px
					if x >= w {
						continue
					}
					idx := (bits >> uint(2*(py*4+px))) & 3
					col := palette[idx]
					di := (y*w + x) * 4
					copy(dst[di:di+4], col[:])
				}
			}
		}
	}
}

func decodeDXT3(src, dst []byte, w, h int) {
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	pos := 0
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			if pos+16 > len(src) {
				return
			}
			alphaBlock := src[pos : pos+8]
			colorBlock := src[pos+8 : pos+16]
			pos += 16
			palette := decodeDXT1ColorBlock(colorBlock, false)
			bits := binary.LittleEndian.Uint32(colorBlock[4:8])
			alphaBits := binary.LittleEndian.Uint64(alphaBlock)
			for py := 0; py < 4; py++ {
				y := by*4 + py
				if y >= h {
					continue
				}
				for px := 0; px < 4; px++ {
					x := bx*4 + px
					if x >= w {
						continue
					}
					pIdx := py*4 + px
					cIdx := (bits >> uint(2*pIdx)) & 3
					a4 := byte((alphaBits >> uint(4*pIdx)) & 0xF)
					col := palette[cIdx]
					di := (y*w + x) * 4
					dst[di] = col[0]
					dst[di+1] = col[1]
					dst[di+2] = col[2]
					dst[di+3] = a4 * 17
				}
			}
		}
	}
}

func decodeBC4Block(block []byte) [16]byte {
	a0 := block[0]
	a1 := block[1]
	var lut [8]byte
	lut[0] = a0
	lut[1] = a1
	if a0 > a1 {
		for i := 1; i <= 6; i++ {
			lut[i+1] = byte((uint16(7-i)*uint16(a0) + uint16(i)*uint16(a1) + 3) / 7)
		}
	} else {
		for i := 1; i <= 4; i++ {
			lut[i+1] = byte((uint16(5-i)*uint16(a0) + uint16(i)*uint16(a1) + 2) / 5)
		}
		lut[6] = 0
		lut[7] = 255
	}
	var bits uint64
	for i := 0; i < 6; i++ {
		bits |= uint64(block[2+i]) << uint(8*i)
	}
	var out [16]byte
	for i := 0; i < 16; i++ {
		out[i] = lut[(bits>>uint(3*i))&7]
	}
	return out
}

func decodeDXT5(src, dst []byte, w, h int) {
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	pos := 0
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			if pos+16 > len(src) {
				return
			}
			alphas := decodeBC4Block(src[pos : pos+8])
			colorBlock := src[pos+8 : pos+16]
			pos += 16
			palette := decodeDXT1ColorBlock(colorBlock, false)
			bits := binary.LittleEndian.Uint32(colorBlock[4:8])
			for py := 0; py < 4; py++ {
				y := by*4 + py
				if y >= h {
					continue
				}
				for px := 0; px < 4; px++ {
					x := bx*4 + px
					if x >= w {
						continue
					}
					pIdx := py*4 + px
					cIdx := (bits >> uint(2*pIdx)) & 3
					col := palette[cIdx]
					di := (y*w + x) * 4
					dst[di] = col[0]
					dst[di+1] = col[1]
					dst[di+2] = col[2]
					dst[di+3] = alphas[pIdx]
				}
			}
		}
	}
}

func decodeBC4(src, dst []byte, w, h int) {
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	pos := 0
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			if pos+8 > len(src) {
				return
			}
			vals := decodeBC4Block(src[pos : pos+8])
			pos += 8
			for py := 0; py < 4; py++ {
				y := by*4 + py
				if y >= h {
					continue
				}
				for px := 0; px < 4; px++ {
					x := bx*4 + px
					if x >= w {
						continue
					}
					v := vals[py*4+px]
					di := (y*w + x) * 4
					dst[di] = v
					dst[di+1] = v
					dst[di+2] = v
					dst[di+3] = 255
				}
			}
		}
	}
}

func decodeBC5(src, dst []byte, w, h int) {
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	pos := 0
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			if pos+16 > len(src) {
				return
			}
			reds := decodeBC4Block(src[pos : pos+8])
			greens := decodeBC4Block(src[pos+8 : pos+16])
			pos += 16
			for py := 0; py < 4; py++ {
				y := by*4 + py
				if y >= h {
					continue
				}
				for px := 0; px < 4; px++ {
					x := bx*4 + px
					if x >= w {
						continue
					}
					pIdx := py*4 + px
					di := (y*w + x) * 4
					dst[di] = reds[pIdx]
					dst[di+1] = greens[pIdx]
					dst[di+2] = 0
					dst[di+3] = 255
				}
			}
		}
	}
}

var mapPathPattern = regexp.MustCompile(`^[a-z0-9_]+/\d{2}$`)

// TextureCache 为已解码的图标与地图提供并发安全的有界内存缓存。
type TextureCache struct {
	mu      sync.Mutex
	maxSize int
	order   []string
	items   map[string]*image.RGBA
}

// NewTextureCache 创建一个容量上限为 maxSize 的贴图缓存。
func NewTextureCache(maxSize int) *TextureCache {
	if maxSize <= 0 {
		maxSize = 800
	}
	return &TextureCache{
		maxSize: maxSize,
		order:   make([]string, 0, maxSize),
		items:   make(map[string]*image.RGBA, maxSize),
	}
}

func (c *TextureCache) get(key string) (*image.RGBA, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	img, ok := c.items[key]
	return img, ok
}

func (c *TextureCache) set(key string, img *image.RGBA) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[key]; exists {
		c.items[key] = img
		return
	}
	if len(c.order) >= c.maxSize {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.items, oldest)
	}
	c.order = append(c.order, key)
	c.items[key] = img
}

// ReadIconImage 从本地 SqPack 读取道具/职业图标并解码为 *image.RGBA（优先 _hr1 高清版与 hq 目录）。
func ReadIconImage(reader *GameReader, cache *TextureCache, iconID int, isHQ bool) (*image.RGBA, error) {
	if reader == nil || iconID <= 0 {
		return nil, nil
	}
	key := fmt.Sprintf("icon:%d:%v", iconID, isHQ)
	if cache != nil {
		if img, ok := cache.get(key); ok {
			return img, nil
		}
	}

	folder := fmt.Sprintf("%06d", (iconID/1000)*1000)
	file := fmt.Sprintf("%06d", iconID)
	var candidates []string
	if isHQ {
		candidates = []string{
			fmt.Sprintf("ui/icon/%s/hq/%s_hr1.tex", folder, file),
			fmt.Sprintf("ui/icon/%s/hq/%s.tex", folder, file),
			fmt.Sprintf("ui/icon/%s/%s_hr1.tex", folder, file),
			fmt.Sprintf("ui/icon/%s/%s.tex", folder, file),
		}
	} else {
		candidates = []string{
			fmt.Sprintf("ui/icon/%s/%s_hr1.tex", folder, file),
			fmt.Sprintf("ui/icon/%s/%s.tex", folder, file),
		}
	}

	for _, p := range candidates {
		raw, err := reader.ReadFile(p)
		if err != nil {
			continue
		}
		if raw == nil {
			continue
		}
		img, err := DecodeTexToRGBA(raw)
		if err != nil {
			continue
		}
		if cache != nil {
			cache.set(key, img)
		}
		return img, nil
	}
	return nil, nil
}

// ReadMapImage 从本地 SqPack 读取指定地图贴图（如 "s1f1/00"）并解码为 *image.RGBA。
func ReadMapImage(reader *GameReader, cache *TextureCache, mapPath string) (*image.RGBA, error) {
	if reader == nil {
		return nil, nil
	}
	clean := strings.ToLower(strings.TrimSpace(mapPath))
	if !mapPathPattern.MatchString(clean) {
		return nil, nil
	}
	key := "map:" + clean
	if cache != nil {
		if img, ok := cache.get(key); ok {
			return img, nil
		}
	}

	flat := strings.ReplaceAll(clean, "/", "")
	candidates := []string{
		fmt.Sprintf("ui/map/%s/%s_m.tex", clean, flat),
		fmt.Sprintf("ui/map/%s/%s_s.tex", clean, flat),
	}
	for _, p := range candidates {
		raw, err := reader.ReadFile(p)
		if err != nil || raw == nil {
			continue
		}
		img, err := DecodeTexToRGBA(raw)
		if err != nil {
			continue
		}
		if cache != nil {
			cache.set(key, img)
		}
		return img, nil
	}
	return nil, nil
}
