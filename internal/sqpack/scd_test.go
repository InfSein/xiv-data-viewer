package sqpack

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildSyntheticOggSCD(oggPayload []byte, oggVersion, xorByte uint8, oggHeaderSize uint32) []byte {
	const (
		tablesOff    = 0x30
		audioTblOff  = 0x60
		entryOff     = 0x80
		subInfoSize  = 0x20 + 0x20 // SeekTableHeader(0x20) + OggHeader
		markAndExtra = 0x20
	)
	_ = markAndExtra
	if int(oggHeaderSize) > len(oggPayload) {
		oggHeaderSize = uint32(len(oggPayload))
	}
	streamData := oggPayload[oggHeaderSize:]
	streamSize := uint32(len(streamData))
	actualSubInfoSize := uint32(0x20) + oggHeaderSize
	totalSize := entryOff + 0x20 + int(actualSubInfoSize) + len(streamData)

	buf := make([]byte, totalSize)
	copy(buf[0:8], "SEDBSSCF")
	binary.LittleEndian.PutUint32(buf[0x08:0x0c], 3)
	buf[0x0c] = 0x00
	buf[0x0d] = 0x04
	binary.LittleEndian.PutUint16(buf[0x0e:0x10], tablesOff)
	binary.LittleEndian.PutUint32(buf[0x10:0x14], uint32(totalSize))

	// ScdHeader
	binary.LittleEndian.PutUint16(buf[tablesOff+0x04:tablesOff+0x06], 1)
	binary.LittleEndian.PutUint32(buf[tablesOff+0x0c:tablesOff+0x10], audioTblOff)
	binary.LittleEndian.PutUint32(buf[audioTblOff:audioTblOff+4], entryOff)

	// AudioBasicDesc
	binary.LittleEndian.PutUint32(buf[entryOff+0x00:entryOff+0x04], streamSize)
	binary.LittleEndian.PutUint32(buf[entryOff+0x04:entryOff+0x08], 2)
	binary.LittleEndian.PutUint32(buf[entryOff+0x08:entryOff+0x0c], 44100)
	binary.LittleEndian.PutUint32(buf[entryOff+0x0c:entryOff+0x10], uint32(ScdFormatOggVorbis))
	binary.LittleEndian.PutUint32(buf[entryOff+0x18:entryOff+0x1c], actualSubInfoSize)

	subInfoPos := entryOff + 0x20
	buf[subInfoPos+0x00] = oggVersion
	buf[subInfoPos+0x01] = 0x20
	buf[subInfoPos+0x02] = xorByte
	binary.LittleEndian.PutUint32(buf[subInfoPos+0x14:subInfoPos+0x18], oggHeaderSize)

	hdrCopy := make([]byte, oggHeaderSize)
	copy(hdrCopy, oggPayload[:oggHeaderSize])
	streamCopy := make([]byte, len(streamData))
	copy(streamCopy, streamData)

	switch oggVersion {
	case 2:
		for i := range hdrCopy {
			hdrCopy[i] ^= xorByte
		}
	case 3:
		combined := make([]byte, len(oggPayload))
		copy(combined, oggPayload)
		b1 := byte(streamSize & 0x7F)
		b2 := b1 & 0x3F
		for i := range combined {
			combined[i] = oggXorTable[(int(b2)+i)&0xFF] ^ combined[i] ^ b1
		}
		copy(hdrCopy, combined[:oggHeaderSize])
		copy(streamCopy, combined[oggHeaderSize:])
	}

	copy(buf[subInfoPos+0x20:], hdrCopy)
	copy(buf[subInfoPos+int(actualSubInfoSize):], streamCopy)
	return buf
}

func buildFakeOggWithComment(loopStart, loopEnd, title string) []byte {
	var pkt bytes.Buffer
	pkt.WriteByte(0x03)
	pkt.WriteString("vorbis")
	_ = binary.Write(&pkt, binary.LittleEndian, uint32(4))
	pkt.WriteString("test")
	comments := []string{}
	if title != "" {
		comments = append(comments, "TITLE="+title)
	}
	if loopStart != "" {
		comments = append(comments, "LOOPSTART="+loopStart)
	}
	if loopEnd != "" {
		comments = append(comments, "LOOPEND="+loopEnd)
	}
	_ = binary.Write(&pkt, binary.LittleEndian, uint32(len(comments)))
	for _, c := range comments {
		_ = binary.Write(&pkt, binary.LittleEndian, uint32(len(c)))
		pkt.WriteString(c)
	}
	pktBytes := pkt.Bytes()

	// 构造含 OggS 页头 + Vorbis Comment 包的测试流（PageSegments >= 1 避免触发空页测试）
	var ogg bytes.Buffer
	ogg.WriteString("OggS")
	ogg.Write(make([]byte, 22))
	ogg.WriteByte(1)
	ogg.WriteByte(byte(len(pktBytes)))
	ogg.Write(pktBytes)
	ogg.WriteString("OggS")
	ogg.Write(make([]byte, 22))
	ogg.WriteByte(1)
	ogg.WriteByte(4)
	ogg.Write(make([]byte, 4))
	return ogg.Bytes()
}

func TestParseSCDSyntheticV2V3AndEmpty(t *testing.T) {
	ogg := buildFakeOggWithComment("44100", "882000", "Prelude")
	hdrSz := uint32(40)

	scdV2 := buildSyntheticOggSCD(ogg, 2, 0x5A, hdrSz)
	infoV2, err := ParseSCD(scdV2)
	if err != nil {
		t.Fatalf("ParseSCD v2 失败: %v", err)
	}
	if infoV2.Format != ScdFormatOggVorbis || infoV2.OggVersion != 2 || !bytes.Equal(infoV2.OggData, ogg) {
		t.Fatalf("v2 解密结果不匹配: format=%v ver=%d oggMatch=%v", infoV2.Format, infoV2.OggVersion, bytes.Equal(infoV2.OggData, ogg))
	}
	if !infoV2.HasLoop || infoV2.LoopStartSample != 44100 || infoV2.LoopEndSample != 882000 || infoV2.VorbisTitle != "Prelude" {
		t.Fatalf("v2 Vorbis 标签解析不符: %+v", infoV2)
	}

	scdV3 := buildSyntheticOggSCD(ogg, 3, 0x20, hdrSz)
	infoV3, err := ParseSCD(scdV3)
	if err != nil {
		t.Fatalf("ParseSCD v3 失败: %v", err)
	}
	if infoV3.Format != ScdFormatOggVorbis || infoV3.OggVersion != 3 || !bytes.Equal(infoV3.OggData, ogg) {
		t.Fatalf("v3 解密结果不匹配: format=%v ver=%d oggMatch=%v", infoV3.Format, infoV3.OggVersion, bytes.Equal(infoV3.OggData, ogg))
	}

	// 空音轨 SCD（audioCount=0）
	empty := make([]byte, 0x60)
	copy(empty[0:8], "SEDBSSCF")
	binary.LittleEndian.PutUint32(empty[0x08:0x0c], 3)
	binary.LittleEndian.PutUint16(empty[0x0e:0x10], 0x30)
	infoEmpty, err := ParseSCD(empty)
	if err != nil || infoEmpty.Format != ScdFormatEmpty {
		t.Fatalf("空音轨 SCD 解析异常: info=%+v err=%v", infoEmpty, err)
	}
}

func TestParseRealClientSCDAndDigest(t *testing.T) {
	envRaw, err := os.ReadFile(filepath.Join("..", "..", ".env"))
	if err != nil {
		t.Skip("本地未配置 .env，跳过真实客户端 SCD 测试")
	}
	var cnRoot string
	for _, line := range strings.Split(string(envRaw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FFXIV_CN_PATH=") {
			cnRoot = strings.Trim(strings.TrimPrefix(line, "FFXIV_CN_PATH="), `"`)
		}
	}
	if cnRoot == "" {
		t.Skip("本地未配置 FFXIV_CN_PATH，跳过真实客户端 SCD 测试")
	}
	gamePath := filepath.Join(cnRoot, "game")
	if _, err := os.Stat(gamePath); err != nil {
		t.Skipf("国服客户端目录不可用: %v", err)
	}

	r := NewGameReader(gamePath)
	defer r.Close()

	for _, p := range []string{"music/ffxiv/BGM_System_Title.scd", "music/ffxiv/Orchestrion/BGM_ORCH_318.scd"} {
		raw, err := r.ReadFile(p)
		if err != nil || len(raw) == 0 {
			t.Fatalf("读取真实音频 %s 失败: len=%d err=%v", p, len(raw), err)
		}
		info, err := ParseSCD(raw)
		if err != nil {
			t.Fatalf("解析真实 SCD %s 失败: %v", p, err)
		}
		if info.Format != ScdFormatOggVorbis || len(info.OggData) == 0 || info.Duration <= 0 || info.SampleRate <= 0 {
			t.Fatalf("真实 SCD %s 解码元信息异常: %+v", p, info)
		}

		sz, md5A, ok, err := r.ReadFileDigest(p)
		if err != nil || !ok || sz != uint32(len(raw)) || len(md5A) != 32 {
			t.Fatalf("ReadFileDigest %s 异常: sz=%d raw=%d md5=%q ok=%v err=%v", p, sz, len(raw), md5A, ok, err)
		}
		_, md5B, _, _ := r.ReadFileDigest(p)
		if md5A != md5B {
			t.Fatalf("ReadFileDigest 结果不稳定: %q != %q", md5A, md5B)
		}
	}
}
