package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/klauspost/compress/zstd"
)

// PackOptions 控制从本地只读 FF14 客户端打包生成 .xivsnap 快照文件的参数。
type PackOptions struct {
	ClientGamePath   string
	Region           string // "cn" | "global"
	Patch            string // 玩家版本号，如 "7.58"
	GameVersion      string // 可选；留空时自动读取 ffxivgame.ver
	SchemasFS        fs.FS
	OutputPath       string
	PrevSnapshotPath string // 可选：上一版本 .xivsnap 路径，用于提取变更/移除的旧图标缩略图
	IconCacheDir     string // 可选：维护者本机去重图标缩略图缓存目录
	CompressionLevel zstd.EncoderLevel
	Workers          int
	OnProgress       func(stage string, done, total int)
}

// PackResult 汇总成功生成并校验后的 .xivsnap 归档产物信息。
type PackResult struct {
	Meta       SnapshotMeta
	OutputPath string
	Size       int64
	SHA256     string
}

func allowedLanguagesForRegion(region string) ([]sqpack.Language, []string) {
	if NormalizeRegion(region) == RegionGlobal {
		// 决策 2-A：国际服单包内合入日/英双语与语言无关表，剔除德/法语
		return []sqpack.Language{sqpack.LangNone, sqpack.LangJapanese, sqpack.LangEnglish}, []string{"ja", "en"}
	}
	return []sqpack.Language{sqpack.LangNone, sqpack.LangChineseSimplified}, []string{"chs"}
}

// PackSnapshot 从本地 FF14 客户端严格只读提取全量 EXD 数据表、配套 EXDSchema、BGM/图标指纹与变动旧图，
// 生成独立完整的 .xivsnap 归档文件。
func PackSnapshot(ctx context.Context, opts PackOptions) (PackResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.ClientGamePath) == "" {
		return PackResult{}, errors.New("未提供有效的客户端 game 目录路径")
	}
	patch := strings.TrimSpace(opts.Patch)
	if patch == "" {
		return PackResult{}, errors.New("未提供目标玩家版本号（如 7.58）")
	}
	if strings.TrimSpace(opts.OutputPath) == "" {
		return PackResult{}, errors.New("未提供快照输出文件路径")
	}

	region := NormalizeRegion(opts.Region)
	snapID := MakeSnapshotID(region, patch)
	allowedLangs, langCodes := allowedLanguagesForRegion(region)
	allowedSet := make(map[sqpack.Language]bool, len(allowedLangs))
	for _, l := range allowedLangs {
		allowedSet[l] = true
	}

	gameVer := strings.TrimSpace(opts.GameVersion)
	if gameVer == "" {
		if raw, err := os.ReadFile(filepath.Join(opts.ClientGamePath, "ffxivgame.ver")); err == nil {
			gameVer = strings.TrimSpace(string(raw))
		}
	}
	if gameVer == "" {
		return PackResult{}, errors.New("无法从 ffxivgame.ver 读取客户端内部版本号")
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers < 2 {
			workers = 2
		}
		if workers > 12 {
			workers = 12
		}
	}

	reader := sqpack.NewGameReader(opts.ClientGamePath)
	defer func() { _ = reader.Close() }()

	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		return PackResult{}, fmt.Errorf("创建输出目录失败: %w", err)
	}
	tmpPath := opts.OutputPath + ".tmp"
	_ = os.Remove(tmpPath)

	level := opts.CompressionLevel
	if level == 0 {
		level = zstd.SpeedBestCompression
	}
	w, err := NewWriter(tmpPath, level)
	if err != nil {
		return PackResult{}, err
	}
	success := false
	defer func() {
		if !success {
			w.Abort()
			_ = os.Remove(tmpPath)
		}
	}()

	emit := func(stage string, done, total int) {
		if opts.OnProgress != nil {
			opts.OnProgress(stage, done, total)
		}
	}

	// 1. 提取 exd/root.exl 并按表名升序分批流式写入全部目标语言 EXH/EXD 原始文件
	exlRaw, err := reader.ReadFile("exd/root.exl")
	if err != nil || len(exlRaw) == 0 {
		return PackResult{}, fmt.Errorf("读取 exd/root.exl 失败: %w", err)
	}
	if err := w.AddFile("exd/root.exl", exlRaw); err != nil {
		return PackResult{}, err
	}
	exl := sqpack.ReadEXLFile(exlRaw)
	sheetNames := make([]string, 0, len(exl.Entries))
	seenSheet := make(map[string]struct{}, len(exl.Entries))
	for _, e := range exl.Entries {
		lower := strings.ToLower(strings.TrimSpace(e.Name))
		if lower == "" {
			continue
		}
		if _, dup := seenSheet[lower]; dup {
			continue
		}
		seenSheet[lower] = struct{}{}
		sheetNames = append(sheetNames, lower)
	}
	sort.Strings(sheetNames)

	type extractedFile struct {
		path string
		data []byte
	}
	type sheetBatchOut struct {
		files []extractedFile
		ok    bool
	}

	sheetCount := 0
	exdFileCount := 0
	const batchSize = 128
	emit("exd", 0, len(sheetNames))

	for start := 0; start < len(sheetNames); start += batchSize {
		if ctx.Err() != nil {
			return PackResult{}, ctx.Err()
		}
		end := min(len(sheetNames), start+batchSize)
		batch := sheetNames[start:end]
		results := make([]sheetBatchOut, len(batch))

		var wg sync.WaitGroup
		sem := make(chan struct{}, workers)
		for i, lower := range batch {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, sLower string) {
				defer wg.Done()
				defer func() { <-sem }()
				exhPath := "exd/" + sLower + ".exh"
				exhRaw, rErr := reader.ReadFile(exhPath)
				if rErr != nil || len(exhRaw) < 32 {
					return
				}
				exh, pErr := sqpack.ReadEXHHeader(exhRaw)
				if pErr != nil {
					return
				}
				files := make([]extractedFile, 0, 1+len(exh.Paginations)*2)
				files = append(files, extractedFile{path: exhPath, data: exhRaw})

				// 选择需要保留的语言后缀（自动去重）
				seenSuffix := make(map[string]struct{}, 4)
				for _, l := range exh.Languages {
					if !allowedSet[l] {
						continue
					}
					suffix := ""
					if l != sqpack.LangNone {
						code := sqpack.FormatLanguage(l)
						if code != "" && !strings.HasPrefix(code, "unk(") {
							suffix = "_" + code
						}
					}
					if _, dup := seenSuffix[suffix]; dup {
						continue
					}
					seenSuffix[suffix] = struct{}{}
					for _, pg := range exh.Paginations {
						exdPath := fmt.Sprintf("exd/%s_%d%s.exd", sLower, pg.StartID, suffix)
						exdRaw, eErr := reader.ReadFile(exdPath)
						if eErr != nil || len(exdRaw) == 0 {
							continue
						}
						files = append(files, extractedFile{path: exdPath, data: exdRaw})
					}
				}
				results[idx] = sheetBatchOut{files: files, ok: true}
			}(i, lower)
		}
		wg.Wait()

		for _, res := range results {
			if !res.ok {
				continue
			}
			sheetCount++
			for _, f := range res.files {
				if err := w.AddFile(f.path, f.data); err != nil {
					return PackResult{}, err
				}
				if strings.HasSuffix(f.path, ".exd") {
					exdFileCount++
				}
			}
		}
		emit("exd", end, len(sheetNames))
	}

	// 2. 封存配套 EXDSchema YAML 文件，确保跨大版本时可按原表结构准确解析
	schemaCount := 0
	if opts.SchemasFS != nil {
		entries, err := fs.ReadDir(opts.SchemasFS, ".")
		if err != nil {
			return PackResult{}, fmt.Errorf("读取 EXDSchema 目录失败: %w", err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".yml") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		emit("schemas", 0, len(names))
		for i, name := range names {
			raw, rErr := fs.ReadFile(opts.SchemasFS, name)
			if rErr != nil {
				return PackResult{}, fmt.Errorf("读取 schema %s 失败: %w", name, rErr)
			}
			if err := w.AddFile("schemas/"+name, raw); err != nil {
				return PackResult{}, err
			}
			schemaCount++
			if (i+1)%200 == 0 || i+1 == len(names) {
				emit("schemas", i+1, len(names))
			}
		}
	}

	// 3. 计算 BGM 与管弦乐琴音频存在性及内容指纹（不打包数 GB 的 .scd 原文件）
	emit("bgm", 0, 1)
	bgmDigests, err := collectClientBGMDigests(ctx, reader, opts.SchemasFS, workers)
	if err != nil {
		return PackResult{}, err
	}
	if bgmRaw, err := EncodeBGMDigests(bgmDigests); err != nil {
		return PackResult{}, err
	} else if err := w.AddFile(metaBGMPath, bgmRaw); err != nil {
		return PackResult{}, err
	}
	emit("bgm", 1, 1)

	// 4. 扫描全量图标指纹，并按决策 1-A 提取相比上一版本变更/移除的旧图标缩略图
	emit("icons", 0, 1)
	iconFiles, err := sqpack.ScanIconFiles(reader)
	if err != nil {
		return PackResult{}, fmt.Errorf("扫描客户端图标目录失败: %w", err)
	}
	var iconDigests []IconDigestRecord
	var blobTasks []sqpack.IconBlobTask
	if len(iconFiles) > 0 {
		snaps, tasks, dErr := sqpack.ComputeIconDigests([]sqpack.IconDigestSource{{
			Reader: reader,
			Files:  iconFiles,
		}})
		if dErr != nil {
			return PackResult{}, fmt.Errorf("计算客户端图标指纹失败: %w", dErr)
		}
		blobTasks = tasks
		iconDigests = make([]IconDigestRecord, len(snaps))
		for i, r := range snaps {
			iconDigests[i] = IconDigestRecord{
				ID:        r.IconID,
				Digest:    r.Digest,
				Variants:  r.VariantSummary,
				BlobKey:   r.BlobKey,
				HasHR1:    r.HasHR1,
				HasHQ:     r.HasHQ,
				HasLocale: r.HasLocalized,
			}
		}
	}
	if iconRaw, err := EncodeIconDigests(iconDigests); err != nil {
		return PackResult{}, err
	} else if err := w.AddFile(metaIconDigestPath, iconRaw); err != nil {
		return PackResult{}, err
	}

	var iconCache *IconThumbCache
	if strings.TrimSpace(opts.IconCacheDir) != "" {
		iconCache = OpenIconThumbCache(opts.IconCacheDir)
	}

	prevSnapshotID := ""
	prevIconThumbs := make(map[string][]byte)
	if strings.TrimSpace(opts.PrevSnapshotPath) != "" {
		prevArch, oErr := OpenArchive(opts.PrevSnapshotPath)
		if oErr != nil {
			return PackResult{}, fmt.Errorf("打开上一版本快照以提取变动旧图标失败: %w", oErr)
		}
		prevSnapshotID = prevArch.Meta().ID
		prevIcons, pErr := prevArch.ReadIconDigests()
		if pErr == nil && len(prevIcons) > 0 {
			currByID := make(map[int]IconDigestRecord, len(iconDigests))
			for _, ic := range iconDigests {
				currByID[ic.ID] = ic
			}
			for _, prevIc := range prevIcons {
				if prevIc.BlobKey == "" {
					continue
				}
				currIc, exists := currByID[prevIc.ID]
				if !exists || currIc.Digest != prevIc.Digest || currIc.Variants != prevIc.Variants {
					if data, ok := prevArch.ReadIconThumbBlob(prevIc.BlobKey); ok {
						prevIconThumbs[prevIc.BlobKey] = data
					} else if iconCache != nil {
						if data, ok := iconCache.Read(prevIc.BlobKey); ok {
							prevIconThumbs[prevIc.BlobKey] = data
						}
					}
				}
			}
		}
		_ = prevArch.Close()
	}

	if len(prevIconThumbs) > 0 {
		if thumbRaw, err := EncodeIconThumbs(prevIconThumbs); err != nil {
			return PackResult{}, err
		} else if err := w.AddFile(metaIconThumbsPath, thumbRaw); err != nil {
			return PackResult{}, err
		}
	}

	// 将当前客户端的新图标同步到本地维护者缓存，供下一版本打包时抽取旧图
	if iconCache != nil && len(blobTasks) > 0 {
		emit("icon-cache", 0, len(blobTasks))
		if err := iconCache.SyncFromTasks(ctx, blobTasks, workers); err != nil {
			return PackResult{}, fmt.Errorf("更新维护者本地图标缓存失败: %w", err)
		}
		emit("icon-cache", len(blobTasks), len(blobTasks))
	}
	emit("icons", 1, 1)

	meta := SnapshotMeta{
		FormatVersion:  FormatVersion,
		ID:             snapID,
		Region:         region,
		Patch:          patch,
		GameVersion:    gameVer,
		Languages:      langCodes,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		PrevSnapshotID: prevSnapshotID,
		Stats: SnapshotStats{
			SheetCount:         sheetCount,
			EXDFileCount:       exdFileCount,
			SchemaCount:        schemaCount,
			BGMCount:           len(bgmDigests),
			IconCount:          len(iconDigests),
			PrevIconThumbCount: len(prevIconThumbs),
		},
	}
	if err := w.Close(meta); err != nil {
		return PackResult{}, err
	}

	// 计算 SHA256 并校验生成的归档可正常打开读取
	sum, size, err := fileSHA256AndSize(tmpPath)
	if err != nil {
		return PackResult{}, err
	}
	if _, _, err := ReadArchiveMeta(tmpPath); err != nil {
		return PackResult{}, fmt.Errorf("生成的快照自检失败: %w", err)
	}
	if err := os.Rename(tmpPath, opts.OutputPath); err != nil {
		return PackResult{}, fmt.Errorf("落盘快照文件失败: %w", err)
	}
	success = true

	return PackResult{
		Meta:       meta,
		OutputPath: opts.OutputPath,
		Size:       size,
		SHA256:     sum,
	}, nil
}

func collectClientBGMDigests(ctx context.Context, reader *sqpack.GameReader, schemasFS fs.FS, workers int) ([]BGMDigestRecord, error) {
	if reader == nil || schemasFS == nil {
		return nil, nil
	}
	bgmSheet, err := sqpack.LoadSheetWithSchema(reader, schemasFS, "BGM", sqpack.LangNone, []string{"File"})
	if err != nil || bgmSheet == nil {
		return nil, nil
	}
	orchPaths, _ := sqpack.LoadSheetWithSchema(reader, schemasFS, "OrchestrionPath", sqpack.LangNone, []string{"File"})

	type bgmItem struct {
		id   int
		file string
	}
	items := make([]bgmItem, 0, len(bgmSheet.Rows))
	uniquePaths := make(map[string]struct{}, len(bgmSheet.Rows))
	for id, row := range bgmSheet.Rows {
		if id <= 0 {
			continue
		}
		f := strings.TrimSpace(bgmSheet.Str(row, "File"))
		items = append(items, bgmItem{id: id, file: f})
		if f != "" {
			uniquePaths[f] = struct{}{}
		}
	}
	if orchPaths != nil {
		for _, row := range orchPaths.Rows {
			if f := strings.TrimSpace(orchPaths.Str(row, "File")); f != "" {
				uniquePaths[f] = struct{}{}
			}
		}
	}

	type digestVal struct {
		exists bool
		size   uint32
		md5    string
	}
	pathList := make([]string, 0, len(uniquePaths))
	for p := range uniquePaths {
		pathList = append(pathList, p)
	}
	sort.Strings(pathList)

	byPath := make(map[string]digestVal, len(pathList))
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobCh := make(chan string, len(pathList))
	for _, p := range pathList {
		jobCh <- p
	}
	close(jobCh)

	wCount := min(max(1, workers), max(1, len(pathList)))
	for i := 0; i < wCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobCh {
				if ctx.Err() != nil {
					return
				}
				sz, md5Hex, ok, rErr := reader.ReadFileDigest(p)
				if rErr != nil {
					ok = false
				}
				mu.Lock()
				byPath[p] = digestVal{exists: ok, size: sz, md5: md5Hex}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	records := make([]BGMDigestRecord, 0, len(items))
	covered := make(map[string]bool, len(items))
	for _, it := range items {
		rec := BGMDigestRecord{ID: it.id, File: it.file}
		if it.file != "" {
			covered[it.file] = true
			if d, ok := byPath[it.file]; ok {
				rec.FileExists = d.exists
				rec.FileSize = d.size
				rec.ContentMD5 = d.md5
			}
		}
		records = append(records, rec)
	}
	// 追加仅在 OrchestrionPath 中出现、未出现在 BGM 表中的路径存在性（ID=0）
	for _, p := range pathList {
		if covered[p] {
			continue
		}
		d := byPath[p]
		records = append(records, BGMDigestRecord{
			ID:         0,
			File:       p,
			FileExists: d.exists,
			FileSize:   d.size,
			ContentMD5: d.md5,
		})
	}
	return records, nil
}

func fileSHA256AndSize(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
