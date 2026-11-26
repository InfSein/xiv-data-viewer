package sqpack

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const (
	iconBlobKindTexZlib byte = 'T'
	iconBlobKindPNG     byte = 'P'
)

// IconDigestSource 描述用于提取图标版本快照的客户端读取源与有效图标文件清单。
type IconDigestSource struct {
	Reader *GameReader
	Files  []IconFileEntry
}

// IconSnapshotRecord 描述单个图标编号的跨版本指纹、变体概览及代表性缩略图内容键。
type IconSnapshotRecord struct {
	IconID         int
	Digest         string
	VariantSummary string
	BlobKey        string
	HasHR1         bool
	HasHQ          bool
	HasLocalized   bool
}

// IconBlobTask 描述一个待归档代表性缩略图的数据源文件定位信息。
type IconBlobTask struct {
	BlobKey    string
	ar         *ArchiveReader
	dataFileID uint32
	offset     uint64
}

// ComputeIconDigests 按物理归档偏移顺序仅扫描每个图标文件的 4KB 头与首块像素数据，
// 在不触发完整纹理解压的情况下生成全量跨版本指纹及待归档代表缩略图任务清单。
func ComputeIconDigests(sources []IconDigestSource) ([]IconSnapshotRecord, []IconBlobTask, error) {
	variantOrder := make(map[string]int, len(KnownIconVariants))
	for i, v := range KnownIconVariants {
		variantOrder[v] = i
	}

	type locatedTask struct {
		ar         *ArchiveReader
		dataFileID uint32
		offset     uint64
		file       IconFileEntry
	}

	seenKey := make(map[string]bool, 200000)
	var allTasks []locatedTask
	for _, src := range sources {
		if src.Reader == nil || len(src.Files) == 0 {
			continue
		}
		for _, f := range src.Files {
			k := fmt.Sprintf("%d|%s|%v", f.IconID, f.Variant, f.HR1)
			if seenKey[k] {
				continue
			}
			ar, normPath, err := src.Reader.locate(f.Path)
			if err != nil || ar == nil {
				continue
			}
			entry, ok := ar.lookup(normPath)
			if !ok {
				continue
			}
			seenKey[k] = true
			allTasks = append(allTasks, locatedTask{
				ar:         ar,
				dataFileID: entry.dataFileID,
				offset:     entry.offset,
				file:       f,
			})
		}
	}

	if len(allTasks) == 0 {
		return nil, nil, nil
	}

	arOrder := make(map[*ArchiveReader]int, 8)
	for i := range allTasks {
		if _, ok := arOrder[allTasks[i].ar]; !ok {
			arOrder[allTasks[i].ar] = len(arOrder)
		}
	}
	sort.Slice(allTasks, func(i, j int) bool {
		ai, aj := arOrder[allTasks[i].ar], arOrder[allTasks[j].ar]
		if ai != aj {
			return ai < aj
		}
		if allTasks[i].dataFileID != allTasks[j].dataFileID {
			return allTasks[i].dataFileID < allTasks[j].dataFileID
		}
		return allTasks[i].offset < allTasks[j].offset
	})

	type fileDigestInfo struct {
		file     IconFileEntry
		fileHash string
		taskIdx  int
	}
	byIconID := make(map[int][]fileDigestInfo, 75000)

	const sampleCap = 4096
	scratch := make([]byte, sampleCap)

	for idx, t := range allTasks {
		f, err := t.ar.getDataFile(t.dataFileID)
		if err != nil {
			continue
		}
		n, rErr := f.ReadAt(scratch, int64(t.offset))
		if n < 24 || (rErr != nil && !errors.Is(rErr, io.EOF)) {
			continue
		}
		fiSize := int(binary.LittleEndian.Uint32(scratch[0:4]))
		fileType := binary.LittleEndian.Uint32(scratch[4:8])
		rawSize := binary.LittleEndian.Uint32(scratch[8:12])
		if fileType != fileTypeTexture || rawSize == 0 || fiSize < 24 {
			continue
		}

		sample := scratch[:n]
		if fiSize+80 > len(sample) && fiSize+80 <= 256*1024 {
			bigger := make([]byte, fiSize+2048)
			bn, brErr := f.ReadAt(bigger, int64(t.offset))
			if bn >= 24 && (brErr == nil || errors.Is(brErr, io.EOF)) {
				sample = bigger[:bn]
			}
		}

		h := md5.New()
		h.Write([]byte(t.file.Variant))
		if t.file.HR1 {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		var meta [12]byte
		binary.LittleEndian.PutUint32(meta[0:4], fileType)
		binary.LittleEndian.PutUint32(meta[4:8], rawSize)
		binary.LittleEndian.PutUint32(meta[8:12], uint32(len(sample)))
		h.Write(meta[:])
		h.Write(sample)
		fHash := hex.EncodeToString(h.Sum(nil)[:8])

		byIconID[t.file.IconID] = append(byIconID[t.file.IconID], fileDigestInfo{
			file:     t.file,
			fileHash: fHash,
			taskIdx:  idx,
		})
	}

	if len(byIconID) == 0 {
		return nil, nil, nil
	}

	iconIDs := make([]int, 0, len(byIconID))
	for id := range byIconID {
		iconIDs = append(iconIDs, id)
	}
	sort.Ints(iconIDs)

	var blobTasks []IconBlobTask
	seenBlobTask := make(map[string]bool, len(iconIDs))

	records := make([]IconSnapshotRecord, 0, len(iconIDs))
	for _, id := range iconIDs {
		files := byIconID[id]
		sort.Slice(files, func(i, j int) bool {
			vi, vj := variantOrder[files[i].file.Variant], variantOrder[files[j].file.Variant]
			if vi != vj {
				return vi < vj
			}
			return !files[i].file.HR1 && files[j].file.HR1
		})

		overall := md5.New()
		var varParts []string
		hasHR1 := false
		hasHQ := false
		hasLoc := false
		repIdx := 0
		repFoundDefault := false

		for idx, fd := range files {
			if fd.file.HR1 {
				hasHR1 = true
			}
			if fd.file.Variant == "hq" {
				hasHQ = true
			}
			if fd.file.Variant != "" && fd.file.Variant != "hq" {
				hasLoc = true
			}

			resTag := "std"
			if fd.file.HR1 {
				resTag = "hr1"
			}
			vName := fd.file.Variant
			if vName == "" {
				vName = "default"
			}
			varParts = append(varParts, vName+":"+resTag)
			overall.Write([]byte(fd.fileHash))

			if !repFoundDefault && fd.file.Variant == "" && !fd.file.HR1 {
				repIdx = idx
				repFoundDefault = true
			}
		}
		if !repFoundDefault {
			for idx, fd := range files {
				if !fd.file.HR1 {
					repIdx = idx
					break
				}
			}
		}

		rep := files[repIdx]
		blobKey := "ib1_" + rep.fileHash
		if !seenBlobTask[blobKey] {
			seenBlobTask[blobKey] = true
			lt := allTasks[rep.taskIdx]
			blobTasks = append(blobTasks, IconBlobTask{
				BlobKey:    blobKey,
				ar:         lt.ar,
				dataFileID: lt.dataFileID,
				offset:     lt.offset,
			})
		}

		records = append(records, IconSnapshotRecord{
			IconID:         id,
			Digest:         hex.EncodeToString(overall.Sum(nil)[:8]),
			VariantSummary: strings.Join(varParts, "|"),
			BlobKey:        blobKey,
			HasHR1:         hasHR1,
			HasHQ:          hasHQ,
			HasLocalized:   hasLoc,
		})
	}

	return records, blobTasks, nil
}

// ArchiveIconBlobs 按需将缺失的图标缩略图载荷读取压缩并通过回调落盘，支持上下文取消与有限后台并发。
func ArchiveIconBlobs(
	ctx context.Context,
	tasks []IconBlobTask,
	hasBlob func(blobKey string) bool,
	onBlob func(blobKey string, payload []byte) error,
	maxWorkers int,
) error {
	if onBlob == nil || len(tasks) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var todo []IconBlobTask
	for _, t := range tasks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if hasBlob != nil && hasBlob(t.BlobKey) {
			continue
		}
		todo = append(todo, t)
	}
	if len(todo) == 0 {
		return nil
	}

	arOrder := make(map[*ArchiveReader]int, 8)
	for i := range todo {
		if _, ok := arOrder[todo[i].ar]; !ok {
			arOrder[todo[i].ar] = len(arOrder)
		}
	}
	sort.Slice(todo, func(i, j int) bool {
		ai, aj := arOrder[todo[i].ar], arOrder[todo[j].ar]
		if ai != aj {
			return ai < aj
		}
		if todo[i].dataFileID != todo[j].dataFileID {
			return todo[i].dataFileID < todo[j].dataFileID
		}
		return todo[i].offset < todo[j].offset
	})

	type blobResult struct {
		key     string
		payload []byte
	}

	if maxWorkers <= 0 {
		maxWorkers = max(4, runtime.NumCPU())
	}
	workers := min(len(todo), maxWorkers)
	jobCh := make(chan IconBlobTask, min(len(todo), workers*4))
	resCh := make(chan blobResult, min(len(todo), workers*4))

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for job := range jobCh {
				if ctx.Err() != nil {
					return
				}
				f, err := job.ar.getDataFile(job.dataFileID)
				if err != nil {
					continue
				}
				fi, fiBuf, err := readFileInfo(f, int64(job.offset))
				if err != nil || fi.fileType != fileTypeTexture || fi.rawFileSize == 0 {
					continue
				}
				raw, err := readTextureFile(f, int64(job.offset), fi, fiBuf)
				if err != nil || len(raw) == 0 {
					continue
				}
				payload, err := BuildIconBlobPayload(raw)
				if err != nil || len(payload) == 0 {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case resCh <- blobResult{key: job.BlobKey, payload: payload}:
				}
			}
		}()
	}

	go func() {
	feedLoop:
		for _, j := range todo {
			select {
			case <-ctx.Done():
				break feedLoop
			case jobCh <- j:
			}
		}
		close(jobCh)
		wg.Wait()
		close(resCh)
	}()

	var writeErr error
	for r := range resCh {
		if writeErr != nil {
			continue
		}
		if err := onBlob(r.key, r.payload); err != nil {
			writeErr = err
		}
	}
	if writeErr != nil {
		return writeErr
	}
	return ctx.Err()
}

// ComputeIconSnapshots 同步执行图标指纹扫描并完成代表性缩略图归档（主要用于单步批量验证场景）。
func ComputeIconSnapshots(
	sources []IconDigestSource,
	hasBlob func(blobKey string) bool,
	onBlob func(blobKey string, payload []byte) error,
) ([]IconSnapshotRecord, error) {
	records, tasks, err := ComputeIconDigests(sources)
	if err != nil || len(records) == 0 {
		return records, err
	}
	if err := ArchiveIconBlobs(context.Background(), tasks, hasBlob, onBlob, 0); err != nil {
		return nil, err
	}
	return records, nil
}

// BuildIconBlobPayload 将原始 .tex 贴图编码为适合本地版本快照去重存储的紧凑载荷。
func BuildIconBlobPayload(rawTex []byte) ([]byte, error) {
	if len(rawTex) == 0 {
		return nil, errors.New("空的贴图数据")
	}
	hdr, err := ParseTextureHeader(rawTex)
	if err != nil {
		return nil, err
	}
	// 标准尺寸小图标（<= 48×48 且原始 <= 10KB）直接保留 zlib 压缩 .tex，兼顾体积与解码速度
	if hdr.Width <= 48 && hdr.Height <= 48 && len(rawTex) <= 10*1024 {
		var buf bytes.Buffer
		buf.WriteByte(iconBlobKindTexZlib)
		zw, zErr := zlib.NewWriterLevel(&buf, zlib.BestSpeed)
		if zErr != nil {
			return nil, zErr
		}
		if _, wErr := zw.Write(rawTex); wErr != nil {
			_ = zw.Close()
			return nil, wErr
		}
		if cErr := zw.Close(); cErr != nil {
			return nil, cErr
		}
		return buf.Bytes(), nil
	}

	// 更大贴图统一缩放到 64px 以内 PNG，大幅降低全量历史包磁盘占用
	img, err := DecodeTexToRGBA(rawTex)
	if err != nil {
		return nil, err
	}
	scaled := downscaleRGBAIfNeeded(img, 64)
	var buf bytes.Buffer
	buf.WriteByte(iconBlobKindPNG)
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, scaled); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeIconBlobToRGBA 将本地备份包中的图标缩略图载荷还原为预乘 *image.RGBA。
func DecodeIconBlobToRGBA(payload []byte) (*image.RGBA, error) {
	if len(payload) == 0 {
		return nil, errors.New("空图标缓存载荷")
	}
	switch payload[0] {
	case iconBlobKindTexZlib:
		zr, err := zlib.NewReader(bytes.NewReader(payload[1:]))
		if err != nil {
			return nil, err
		}
		defer func() { _ = zr.Close() }()
		raw, err := io.ReadAll(zr)
		if err != nil {
			return nil, err
		}
		return DecodeTexToRGBA(raw)
	case iconBlobKindPNG:
		return decodePNGToPremulRGBA(payload[1:])
	default:
		return decodePNGToPremulRGBA(payload)
	}
}

func decodePNGToPremulRGBA(pngBytes []byte) (*image.RGBA, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba, nil
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst, nil
}
