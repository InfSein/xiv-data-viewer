package snapshot

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

const (
	iconDigestMagic = "ICNDIG01"
	iconThumbsMagic = "ICNTHM01"
)

// EncodeBGMDigests 将 BGM 音频指纹清单编码为 JSON 字节流。
func EncodeBGMDigests(records []BGMDigestRecord) ([]byte, error) {
	if records == nil {
		records = []BGMDigestRecord{}
	}
	return json.Marshal(records)
}

// DecodeBGMDigests 解析 BGM 音频指纹清单 JSON。
func DecodeBGMDigests(raw []byte) ([]BGMDigestRecord, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []BGMDigestRecord
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析 BGM 指纹清单失败: %w", err)
	}
	return out, nil
}

// EncodeIconDigests 将全量图标指纹编码为紧凑二进制格式（按 ID 升序 + Delta Varint）。
func EncodeIconDigests(records []IconDigestRecord) ([]byte, error) {
	sorted := make([]IconDigestRecord, len(records))
	copy(sorted, records)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})

	var buf bytes.Buffer
	buf.Grow(len(sorted)*40 + 16)
	buf.WriteString(iconDigestMagic)

	var vbuf [binary.MaxVarintLen64]byte
	writeUvar := func(v uint64) {
		n := binary.PutUvarint(vbuf[:], v)
		buf.Write(vbuf[:n])
	}
	writeStr := func(s string) {
		writeUvar(uint64(len(s)))
		buf.WriteString(s)
	}

	writeUvar(uint64(len(sorted)))
	prevID := 0
	for _, r := range sorted {
		delta := r.ID - prevID
		if delta < 0 {
			delta = 0
		}
		prevID = r.ID
		writeUvar(uint64(delta))

		var flags byte
		if r.HasHR1 {
			flags |= 1 << 0
		}
		if r.HasHQ {
			flags |= 1 << 1
		}
		if r.HasLocale {
			flags |= 1 << 2
		}
		buf.WriteByte(flags)
		writeStr(r.Digest)
		writeStr(r.Variants)
		writeStr(r.BlobKey)
	}
	return buf.Bytes(), nil
}

// DecodeIconDigests 从紧凑二进制格式解码全量图标指纹清单。
func DecodeIconDigests(raw []byte) ([]IconDigestRecord, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) < len(iconDigestMagic) || string(raw[:len(iconDigestMagic)]) != iconDigestMagic {
		return nil, errors.New("无效的图标指纹清单魔数")
	}
	br := bytes.NewReader(raw[len(iconDigestMagic):])
	readStr := func() (string, error) {
		n, err := binary.ReadUvarint(br)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", nil
		}
		if n > uint64(br.Len()) {
			return "", io.ErrUnexpectedEOF
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", err
		}
		return string(b), nil
	}

	count, err := binary.ReadUvarint(br)
	if err != nil {
		return nil, fmt.Errorf("读取图标指纹总数失败: %w", err)
	}
	out := make([]IconDigestRecord, count)
	prevID := 0
	for i := uint64(0); i < count; i++ {
		delta, err := binary.ReadUvarint(br)
		if err != nil {
			return nil, fmt.Errorf("读取图标编号增量失败: %w", err)
		}
		prevID += int(delta)
		flags, err := br.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("读取图标标志失败: %w", err)
		}
		digest, err := readStr()
		if err != nil {
			return nil, fmt.Errorf("读取图标摘要失败: %w", err)
		}
		variants, err := readStr()
		if err != nil {
			return nil, fmt.Errorf("读取图标变体失败: %w", err)
		}
		blobKey, err := readStr()
		if err != nil {
			return nil, fmt.Errorf("读取图标键名失败: %w", err)
		}
		out[i] = IconDigestRecord{
			ID:        prevID,
			Digest:    digest,
			Variants:  variants,
			BlobKey:   blobKey,
			HasHR1:    (flags & (1 << 0)) != 0,
			HasHQ:     (flags & (1 << 1)) != 0,
			HasLocale: (flags & (1 << 2)) != 0,
		}
	}
	return out, nil
}

// EncodeIconThumbs 将变动/移除旧图标的缩略图载荷映射编码为紧凑二进制块。
func EncodeIconThumbs(thumbs map[string][]byte) ([]byte, error) {
	keys := make([]string, 0, len(thumbs))
	totalSize := len(iconThumbsMagic) + 4
	for k, v := range thumbs {
		if k == "" || len(v) == 0 {
			continue
		}
		keys = append(keys, k)
		totalSize += 2 + len(k) + 4 + len(v)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.Grow(totalSize)
	buf.WriteString(iconThumbsMagic)
	var u32 [4]byte
	var u16 [2]byte
	binary.LittleEndian.PutUint32(u32[:], uint32(len(keys)))
	buf.Write(u32[:])

	for _, k := range keys {
		v := thumbs[k]
		if len(k) > 0xFFFF {
			return nil, fmt.Errorf("图标缩略图键名过长: %d", len(k))
		}
		binary.LittleEndian.PutUint16(u16[:], uint16(len(k)))
		buf.Write(u16[:])
		buf.WriteString(k)
		binary.LittleEndian.PutUint32(u32[:], uint32(len(v)))
		buf.Write(u32[:])
		buf.Write(v)
	}
	return buf.Bytes(), nil
}

// DecodeIconThumbs 解码变动/移除旧图标缩略图载荷映射。
func DecodeIconThumbs(raw []byte) (map[string][]byte, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) < len(iconThumbsMagic)+4 || string(raw[:len(iconThumbsMagic)]) != iconThumbsMagic {
		return nil, errors.New("无效的图标缩略图包魔数")
	}
	pos := len(iconThumbsMagic)
	count := int(binary.LittleEndian.Uint32(raw[pos : pos+4]))
	pos += 4

	out := make(map[string][]byte, count)
	for i := 0; i < count; i++ {
		if pos+2 > len(raw) {
			return nil, io.ErrUnexpectedEOF
		}
		kLen := int(binary.LittleEndian.Uint16(raw[pos : pos+2]))
		pos += 2
		if pos+kLen+4 > len(raw) {
			return nil, io.ErrUnexpectedEOF
		}
		key := string(raw[pos : pos+kLen])
		pos += kLen
		vLen := int(binary.LittleEndian.Uint32(raw[pos : pos+4]))
		pos += 4
		if pos+vLen > len(raw) {
			return nil, io.ErrUnexpectedEOF
		}
		data := make([]byte, vLen)
		copy(data, raw[pos:pos+vLen])
		pos += vLen
		out[key] = data
	}
	return out, nil
}
