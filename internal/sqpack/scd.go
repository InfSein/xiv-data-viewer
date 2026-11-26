package sqpack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jfreymuth/oggvorbis"
)

// ScdAudioFormat 表示 Square Enix SCD 容器内音轨的编码格式编号。
type ScdAudioFormat int32

const (
	ScdFormatEmpty     ScdAudioFormat = -1
	ScdFormatPCMBE     ScdAudioFormat = 0
	ScdFormatPCMLE     ScdAudioFormat = 1
	ScdFormatPSADPCM   ScdAudioFormat = 3
	ScdFormatOggVorbis ScdAudioFormat = 6
	ScdFormatMP3       ScdAudioFormat = 7
	ScdFormatMSADPCM   ScdAudioFormat = 12
	ScdFormatATRAC9    ScdAudioFormat = 22
	ScdFormatHCA       ScdAudioFormat = 26
)

// FormatName 返回 SCD 音频格式的可读名称。
func (f ScdAudioFormat) FormatName() string {
	switch f {
	case ScdFormatEmpty:
		return "Empty"
	case ScdFormatPCMBE:
		return "PCM (BE)"
	case ScdFormatPCMLE:
		return "PCM (LE)"
	case ScdFormatPSADPCM:
		return "PS-ADPCM"
	case ScdFormatOggVorbis:
		return "Ogg Vorbis"
	case ScdFormatMP3:
		return "MP3"
	case ScdFormatMSADPCM:
		return "MS-ADPCM"
	case ScdFormatATRAC9:
		return "ATRAC9"
	case ScdFormatHCA:
		return "CRI HCA"
	default:
		return fmt.Sprintf("Codec 0x%X", int32(f))
	}
}

// ScdAudioInfo 描述从 .scd 容器解析出的主音轨元数据与可播放音频流。
type ScdAudioInfo struct {
	SCDVersion      uint32
	TrackCount      int
	Format          ScdAudioFormat
	FormatName      string
	Channels        int
	SampleRate      int
	StreamSize      uint32
	LoopStartBytes  uint32
	LoopEndBytes    uint32
	OggVersion      uint8
	TotalSamples    int64
	Duration        time.Duration
	HasLoop         bool
	LoopStartSample int64
	LoopEndSample   int64
	VorbisTitle     string
	VorbisComments  map[string]string
	OggData         []byte // 解密重组后的标准 Ogg Vorbis 字节流（仅 OggVorbis 时非空）
}

// oggXorTable 为 FF14 苍穹之禁城起引入的 Ogg Vorbis v3 字节流固定 256 字节异或表（同 Lumina / vgmstream）。
var oggXorTable = [256]byte{
	0x3A, 0x32, 0x32, 0x32, 0x03, 0x7E, 0x12, 0xF7, 0xB2, 0xE2, 0xA2, 0x67, 0x32, 0x32, 0x22, 0x32,
	0x32, 0x52, 0x16, 0x1B, 0x3C, 0xA1, 0x54, 0x7B, 0x1B, 0x97, 0xA6, 0x93, 0x1A, 0x4B, 0xAA, 0xA6,
	0x7A, 0x7B, 0x1B, 0x97, 0xA6, 0xF7, 0x02, 0xBB, 0xAA, 0xA6, 0xBB, 0xF7, 0x2A, 0x51, 0xBE, 0x03,
	0xF4, 0x2A, 0x51, 0xBE, 0x03, 0xF4, 0x2A, 0x51, 0xBE, 0x12, 0x06, 0x56, 0x27, 0x32, 0x32, 0x36,
	0x32, 0xB2, 0x1A, 0x3B, 0xBC, 0x91, 0xD4, 0x7B, 0x58, 0xFC, 0x0B, 0x55, 0x2A, 0x15, 0xBC, 0x40,
	0x92, 0x0B, 0x5B, 0x7C, 0x0A, 0x95, 0x12, 0x35, 0xB8, 0x63, 0xD2, 0x0B, 0x3B, 0xF0, 0xC7, 0x14,
	0x51, 0x5C, 0x94, 0x86, 0x94, 0x59, 0x5C, 0xFC, 0x1B, 0x17, 0x3A, 0x3F, 0x6B, 0x37, 0x32, 0x32,
	0x30, 0x32, 0x72, 0x7A, 0x13, 0xB7, 0x26, 0x60, 0x7A, 0x13, 0xB7, 0x26, 0x50, 0xBA, 0x13, 0xB4,
	0x2A, 0x50, 0xBA, 0x13, 0xB5, 0x2E, 0x40, 0xFA, 0x13, 0x95, 0xAE, 0x40, 0x38, 0x18, 0x9A, 0x92,
	0xB0, 0x38, 0x00, 0xFA, 0x12, 0xB1, 0x7E, 0x00, 0xDB, 0x96, 0xA1, 0x7C, 0x08, 0xDB, 0x9A, 0x91,
	0xBC, 0x08, 0xD8, 0x1A, 0x86, 0xE2, 0x70, 0x39, 0x1F, 0x86, 0xE0, 0x78, 0x7E, 0x03, 0xE7, 0x64,
	0x51, 0x9C, 0x8F, 0x34, 0x6F, 0x4E, 0x41, 0xFC, 0x0B, 0xD5, 0xAE, 0x41, 0xFC, 0x0B, 0xD5, 0xAE,
	0x41, 0xFC, 0x3B, 0x70, 0x71, 0x64, 0x33, 0x32, 0x12, 0x32, 0x32, 0x36, 0x70, 0x34, 0x2B, 0x56,
	0x22, 0x70, 0x3A, 0x13, 0xB7, 0x26, 0x60, 0xBA, 0x1B, 0x94, 0xAA, 0x40, 0x38, 0x00, 0xFA, 0xB2,
	0xE2, 0xA2, 0x67, 0x32, 0x32, 0x12, 0x32, 0xB2, 0x32, 0x32, 0x32, 0x32, 0x75, 0xA3, 0x26, 0x7B,
	0x83, 0x26, 0xF9, 0x83, 0x2E, 0xFF, 0xE3, 0x16, 0x7D, 0xC0, 0x1E, 0x63, 0x21, 0x07, 0xE3, 0x01,
}

// ParseSCD 解析 Square Enix .scd 音频容器并提取首个有效音轨的元信息与 Ogg Vorbis 音频流。
func ParseSCD(data []byte) (*ScdAudioInfo, error) {
	if len(data) < 0x30 {
		return nil, errors.New("SCD 文件长度不足")
	}
	if string(data[0:8]) != "SEDBSSCF" {
		return nil, fmt.Errorf("无效的 SCD 文件魔数: %q", string(data[0:8]))
	}

	var bo binary.ByteOrder = binary.LittleEndian
	if data[0x0c] == 0x01 {
		bo = binary.BigEndian
	}

	scdVer := bo.Uint32(data[0x08:0x0c])
	if scdVer < 2 || scdVer > 4 {
		return nil, fmt.Errorf("不支持的 SCD 版本: %d", scdVer)
	}
	tablesOff := int(bo.Uint16(data[0x0e:0x10]))
	if tablesOff < 0x20 || tablesOff+0x20 > len(data) {
		return nil, errors.New("SCD 偏移表头越界")
	}

	audioCount := int(bo.Uint16(data[tablesOff+0x04 : tablesOff+0x06]))
	audioTableOff := int(bo.Uint32(data[tablesOff+0x0c : tablesOff+0x10]))
	if audioCount == 0 {
		return &ScdAudioInfo{
			SCDVersion: scdVer,
			Format:     ScdFormatEmpty,
			FormatName: ScdFormatEmpty.FormatName(),
		}, nil
	}
	if audioTableOff < 0 || audioTableOff+audioCount*4 > len(data) {
		return nil, errors.New("SCD 音频索引表越界")
	}

	validCount := 0
	targetOff := -1
	for i := 0; i < audioCount; i++ {
		off := int(bo.Uint32(data[audioTableOff+i*4 : audioTableOff+i*4+4]))
		if off <= 0 || off+0x20 > len(data) {
			continue
		}
		codec := ScdAudioFormat(int32(bo.Uint32(data[off+0x0c : off+0x10])))
		if codec == ScdFormatEmpty {
			continue
		}
		validCount++
		if targetOff < 0 {
			targetOff = off
		}
	}

	if targetOff < 0 {
		return &ScdAudioInfo{
			SCDVersion: scdVer,
			TrackCount: 0,
			Format:     ScdFormatEmpty,
			FormatName: ScdFormatEmpty.FormatName(),
		}, nil
	}

	streamSize := bo.Uint32(data[targetOff+0x00 : targetOff+0x04])
	channels := int(bo.Uint32(data[targetOff+0x04 : targetOff+0x08]))
	sampleRate := int(bo.Uint32(data[targetOff+0x08 : targetOff+0x0c]))
	format := ScdAudioFormat(int32(bo.Uint32(data[targetOff+0x0c : targetOff+0x10])))
	loopStartBytes := bo.Uint32(data[targetOff+0x10 : targetOff+0x14])
	loopEndBytes := bo.Uint32(data[targetOff+0x14 : targetOff+0x18])
	subInfoSize := int(bo.Uint32(data[targetOff+0x18 : targetOff+0x1c]))
	flags := bo.Uint32(data[targetOff+0x1c : targetOff+0x20])

	info := &ScdAudioInfo{
		SCDVersion:     scdVer,
		TrackCount:     validCount,
		Format:         format,
		FormatName:     format.FormatName(),
		Channels:       channels,
		SampleRate:     sampleRate,
		StreamSize:     streamSize,
		LoopStartBytes: loopStartBytes,
		LoopEndBytes:   loopEndBytes,
		HasLoop:        loopEndBytes > 0,
	}

	subInfoStart := targetOff + 0x20
	if subInfoSize < 0 || subInfoStart+subInfoSize > len(data) {
		return nil, errors.New("SCD SubInfo 区块越界")
	}

	extraPos := subInfoStart
	var markLoopStart, markLoopEnd int64
	if flags&0x01 != 0 && extraPos+16 <= subInfoStart+subInfoSize && string(data[extraPos:extraPos+4]) == "MARK" {
		markSize := int(bo.Uint32(data[extraPos+4 : extraPos+8]))
		markLoopStart = int64(int32(bo.Uint32(data[extraPos+8 : extraPos+12])))
		markLoopEnd = int64(int32(bo.Uint32(data[extraPos+12 : extraPos+16])))
		if markLoopEnd > markLoopStart && markLoopEnd > 0 {
			info.HasLoop = true
			info.LoopStartSample = markLoopStart
			info.LoopEndSample = markLoopEnd
		}
		if markSize > 0 && extraPos+markSize <= subInfoStart+subInfoSize {
			extraPos += markSize
		}
	}

	streamStart := subInfoStart + subInfoSize
	if streamStart+int(streamSize) > len(data) {
		return nil, errors.New("SCD 音频数据区块越界")
	}

	if format != ScdFormatOggVorbis {
		return info, nil
	}

	if extraPos >= subInfoStart+subInfoSize {
		return nil, errors.New("SCD Ogg 扩展头缺失")
	}

	oggVer := data[extraPos]
	info.OggVersion = oggVer

	var oggData []byte
	if oggVer == 0 {
		oggData = make([]byte, int(streamSize))
		copy(oggData, data[streamStart:streamStart+int(streamSize)])
	} else {
		if extraPos+0x20 > subInfoStart+subInfoSize {
			return nil, errors.New("SCD Ogg SeekTable 头部截断")
		}
		xorByte := data[extraPos+0x02]
		oggHeaderSize := int(bo.Uint32(data[extraPos+0x14 : extraPos+0x18]))
		if oggHeaderSize < 0 || oggHeaderSize > subInfoSize {
			return nil, fmt.Errorf("无效的 OggHeaderSize: %d", oggHeaderSize)
		}

		hdrStart := subInfoStart + subInfoSize - oggHeaderSize
		oggData = make([]byte, oggHeaderSize+int(streamSize))
		copy(oggData[:oggHeaderSize], data[hdrStart:hdrStart+oggHeaderSize])
		copy(oggData[oggHeaderSize:], data[streamStart:streamStart+int(streamSize)])

		switch oggVer {
		case 2:
			if xorByte != 0 {
				for i := 0; i < oggHeaderSize; i++ {
					oggData[i] ^= xorByte
				}
			}
		case 3:
			b1 := byte(streamSize & 0x7F)
			b2 := b1 & 0x3F
			for i := range oggData {
				oggData[i] = oggXorTable[(int(b2)+i)&0xFF] ^ oggData[i] ^ b1
			}
		default:
			return nil, fmt.Errorf("不支持的 SCD Ogg 加密版本: %d", oggVer)
		}
	}

	if len(oggData) < 4 || string(oggData[:4]) != "OggS" {
		return nil, errors.New("解密后未检测到 OggS 流魔数")
	}
	info.OggData = oggData

	// 解析 Vorbis 评论标签（如 LOOPSTART / LOOPEND / TITLE）
	comments := parseOggVorbisComments(oggData)
	if len(comments) > 0 {
		info.VorbisComments = comments
		if t := comments["TITLE"]; t != "" {
			info.VorbisTitle = t
		}
		if ls, ok := parsePositiveInt64(comments["LOOPSTART"]); ok {
			if le, ok2 := parsePositiveInt64(comments["LOOPEND"]); ok2 && le > ls {
				info.HasLoop = true
				info.LoopStartSample = ls
				info.LoopEndSample = le
			}
		}
	}

	// 读取准确采样率、声道与总时长（防止第三方 oggvorbis 在异常页头时 panic）
	if sr, ch, length, ok := safeProbeOggVorbis(oggData); ok {
		if sr > 0 {
			info.SampleRate = sr
		}
		if ch > 0 {
			info.Channels = ch
		}
		if length > 0 && info.SampleRate > 0 {
			info.TotalSamples = length
			info.Duration = time.Duration(float64(length) / float64(info.SampleRate) * float64(time.Second))
		}
	}

	return info, nil
}

func safeProbeOggVorbis(oggData []byte) (sampleRate, channels int, length int64, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	vr, err := oggvorbis.NewReader(bytes.NewReader(oggData))
	if err != nil || vr == nil {
		return 0, 0, 0, false
	}
	return vr.SampleRate(), vr.Channels(), vr.Length(), true
}

func parsePositiveInt64(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// parseOggVorbisComments 从标准 Ogg Vorbis 头部前几个页中提取 Vorbis Comment 键值对（键统一转为大写）。
func parseOggVorbisComments(oggData []byte) map[string]string {
	var packetBuf []byte
	pos := 0
	pagesScanned := 0
	foundHeader := false

	for pos+27 <= len(oggData) && pagesScanned < 16 {
		if string(oggData[pos:pos+4]) != "OggS" {
			break
		}
		segCount := int(oggData[pos+26])
		hdrLen := 27 + segCount
		if pos+hdrLen > len(oggData) {
			break
		}
		segTable := oggData[pos+27 : pos+hdrLen]
		dataStart := pos + hdrLen
		dataEnd := dataStart
		for _, segLen := range segTable {
			dataEnd += int(segLen)
		}
		if dataEnd > len(oggData) {
			break
		}

		// 收集包片段，直到遇到 0x03 'vorbis' 注释包结束
		curOff := dataStart
		for _, segLen := range segTable {
			packetBuf = append(packetBuf, oggData[curOff:curOff+int(segLen)]...)
			curOff += int(segLen)
			if segLen < 255 {
				if len(packetBuf) > 7 && packetBuf[0] == 0x03 && string(packetBuf[1:7]) == "vorbis" {
					foundHeader = true
					break
				}
				packetBuf = packetBuf[:0]
			}
		}
		if foundHeader {
			break
		}
		pos = dataEnd
		pagesScanned++
	}

	if !foundHeader || len(packetBuf) < 11 {
		return nil
	}

	p := 7
	if p+4 > len(packetBuf) {
		return nil
	}
	vendorLen := int(binary.LittleEndian.Uint32(packetBuf[p : p+4]))
	p += 4
	if vendorLen < 0 || p+vendorLen+4 > len(packetBuf) {
		return nil
	}
	p += vendorLen
	commentCount := int(binary.LittleEndian.Uint32(packetBuf[p : p+4]))
	p += 4
	if commentCount <= 0 || commentCount > 256 {
		return nil
	}

	out := make(map[string]string, commentCount)
	for i := 0; i < commentCount; i++ {
		if p+4 > len(packetBuf) {
			break
		}
		cLen := int(binary.LittleEndian.Uint32(packetBuf[p : p+4]))
		p += 4
		if cLen < 0 || p+cLen > len(packetBuf) {
			break
		}
		entry := string(packetBuf[p : p+cLen])
		p += cLen
		if k, v, ok := strings.Cut(entry, "="); ok {
			key := strings.ToUpper(strings.TrimSpace(k))
			if key != "" {
				out[key] = strings.TrimSpace(v)
			}
		}
	}
	return out
}
