package backup

import (
	"bufio"
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

type iconBlobLoc struct {
	offset int64
	length uint32
}

type compactIconRecord struct {
	ID        int    `json:"id"`
	Digest    string `json:"d"`
	Variants  string `json:"v,omitempty"`
	BlobKey   string `json:"b,omitempty"`
	ThumbPNG  []byte `json:"t,omitempty"` // 仅用于单元测试或合成内联缩略图回退
	HasHR1    bool   `json:"h,omitempty"`
	HasHQ     bool   `json:"q,omitempty"`
	HasLocale bool   `json:"l,omitempty"`
}

func (s *Service) iconBlobIndexPath() string {
	return filepath.Join(s.backupDir, "icon_blobs.idx")
}

func (s *Service) iconBlobDataPath() string {
	return filepath.Join(s.backupDir, "icon_blobs.dat")
}

func (s *Service) ensureBlobIndexLocked() {
	if s.blobLoaded {
		return
	}
	s.blobLoaded = true
	if s.blobIndex == nil {
		s.blobIndex = make(map[string]iconBlobLoc, 4096)
	}
	f, err := os.Open(s.iconBlobIndexPath())
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}
		off, err1 := strconv.ParseInt(parts[1], 10, 64)
		sz, err2 := strconv.ParseUint(parts[2], 10, 32)
		if err1 != nil || err2 != nil || off < 0 || sz == 0 {
			continue
		}
		s.blobIndex[parts[0]] = iconBlobLoc{offset: off, length: uint32(sz)}
	}
}

func (s *Service) hasIconBlob(key string) bool {
	if key == "" {
		return false
	}
	s.blobMu.Lock()
	defer s.blobMu.Unlock()
	s.ensureBlobIndexLocked()
	_, ok := s.blobIndex[key]
	return ok
}

// ReadIconBlobImage 从去重备份包 icon_blobs.dat 中只读解码指定键对应的历史/快照图标图像。
func (s *Service) ReadIconBlobImage(blobKey string) (*image.RGBA, error) {
	if s == nil || blobKey == "" {
		return nil, nil
	}
	s.blobMu.Lock()
	s.ensureBlobIndexLocked()
	loc, ok := s.blobIndex[blobKey]
	s.blobMu.Unlock()
	if !ok || loc.length == 0 {
		return nil, nil
	}

	f, err := os.Open(s.iconBlobDataPath())
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, loc.length)
	if _, err := io.ReadFull(io.NewSectionReader(f, loc.offset, int64(loc.length)), buf); err != nil {
		return nil, err
	}
	return sqpack.DecodeIconBlobToRGBA(buf)
}

func (s *Service) buildLiveIcons(reuse *snapshotPayload, forceDigest bool) []compactIconRecord {
	if s.store == nil || len(s.store.Icons) == 0 {
		return nil
	}
	if !forceDigest && reuse != nil && len(reuse.Icons) == len(s.store.Icons) && len(reuse.Icons) > 0 {
		return reuse.Icons
	}

	// 若未挂载客户端归档（如单元测试纯内存 Store），生成轻量元数据记录以便无客户端场景测试
	if s.store.CNReader == nil && s.store.GlobalReader == nil && s.store.PrimaryReader == nil {
		out := make([]compactIconRecord, 0, len(s.store.Icons))
		for _, ic := range s.store.Icons {
			var parts []string
			for _, v := range ic.Variants {
				vn := v.Variant
				if vn == "" {
					vn = "default"
				}
				if v.HasStd {
					parts = append(parts, vn+":std")
				}
				if v.HasHR1 {
					parts = append(parts, vn+":hr1")
				}
			}
			varSum := strings.Join(parts, "|")
			out = append(out, compactIconRecord{
				ID:        ic.ID,
				Digest:    fmt.Sprintf("mem_%06d_%s", ic.ID, varSum),
				Variants:  varSum,
				HasHR1:    ic.HasHR1,
				HasHQ:     ic.HasHQ,
				HasLocale: ic.HasLocalized,
			})
		}
		return out
	}

	snaps, blobTasks, err := s.store.ComputeIconDigests()
	if err != nil || len(snaps) == 0 {
		return nil
	}

	// 暂存待归档任务；避免阻塞启动加载页，稍后由 StartBackgroundBlobSync 在后台静默落盘
	s.blobMu.Lock()
	s.pendingBlobs = blobTasks
	s.blobMu.Unlock()

	out := make([]compactIconRecord, len(snaps))
	for i, r := range snaps {
		out[i] = compactIconRecord{
			ID:        r.IconID,
			Digest:    r.Digest,
			Variants:  r.VariantSummary,
			BlobKey:   r.BlobKey,
			HasHR1:    r.HasHR1,
			HasHQ:     r.HasHQ,
			HasLocale: r.HasLocalized,
		}
	}
	return out
}

// StartBackgroundBlobSync 在后台低优先级队列中去重保存当前版本的图标代表缩略图（不阻塞主界面打开）。
func (s *Service) StartBackgroundBlobSync() {
	if s == nil {
		return
	}
	s.blobMu.Lock()
	if len(s.pendingBlobs) == 0 || s.blobSyncCancel != nil {
		s.blobMu.Unlock()
		return
	}
	tasks := s.pendingBlobs
	s.pendingBlobs = nil
	ctx, cancel := context.WithCancel(context.Background())
	s.blobSyncCancel = cancel
	s.blobSyncWg.Add(1)
	s.blobMu.Unlock()

	go func() {
		defer s.blobSyncWg.Done()
		_ = s.archiveTasks(ctx, tasks, 2)
	}()
}

// Close 停止并等待后台缩略图归档写入任务安全退出并关闭文件句柄。
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.blobMu.Lock()
	cancel := s.blobSyncCancel
	s.blobSyncCancel = nil
	s.pendingBlobs = nil
	s.blobMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.blobSyncWg.Wait()
}

func (s *Service) archiveTasks(ctx context.Context, tasks []sqpack.IconBlobTask, workers int) error {
	if len(tasks) == 0 {
		return nil
	}
	s.blobMu.Lock()
	s.ensureBlobIndexLocked()
	s.blobMu.Unlock()

	var datFile, idxFile *os.File
	var datWriter, idxWriter *bufio.Writer
	var datOffset int64

	ensureOpenWriters := func() error {
		if datFile != nil {
			return nil
		}
		if err := os.MkdirAll(s.backupDir, 0o755); err != nil {
			return err
		}
		df, err := os.OpenFile(s.iconBlobDataPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		st, err := df.Stat()
		if err != nil {
			_ = df.Close()
			return err
		}
		inf, err := os.OpenFile(s.iconBlobIndexPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			_ = df.Close()
			return err
		}
		datFile = df
		idxFile = inf
		datOffset = st.Size()
		datWriter = bufio.NewWriterSize(df, 256*1024)
		idxWriter = bufio.NewWriterSize(inf, 64*1024)
		return nil
	}

	defer func() {
		if datWriter != nil {
			_ = datWriter.Flush()
		}
		if idxWriter != nil {
			_ = idxWriter.Flush()
		}
		if datFile != nil {
			_ = datFile.Close()
		}
		if idxFile != nil {
			_ = idxFile.Close()
		}
	}()

	return sqpack.ArchiveIconBlobs(
		ctx,
		tasks,
		s.hasIconBlob,
		func(blobKey string, payload []byte) error {
			if len(payload) == 0 || blobKey == "" {
				return nil
			}
			s.blobMu.Lock()
			defer s.blobMu.Unlock()
			s.ensureBlobIndexLocked()
			if _, ok := s.blobIndex[blobKey]; ok {
				return nil
			}
			if err := ensureOpenWriters(); err != nil {
				return err
			}
			off := datOffset
			n, err := datWriter.Write(payload)
			if err != nil {
				return err
			}
			datOffset += int64(n)
			if _, err := fmt.Fprintf(idxWriter, "%s\t%d\t%d\n", blobKey, off, n); err != nil {
				return err
			}
			s.blobIndex[blobKey] = iconBlobLoc{offset: off, length: uint32(n)}
			return nil
		},
		workers,
	)
}

func buildBaselineIcons(current []compactIconRecord) []compactIconRecord {
	if len(current) == 0 {
		return nil
	}
	sorted := make([]compactIconRecord, len(current))
	copy(sorted, current)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})

	cutTail := 1
	if len(sorted) >= 30 {
		cutTail = min(16, max(4, len(sorted)/200))
	}
	keepN := max(1, len(sorted)-cutTail)

	existingIDs := make(map[int]bool, len(sorted))
	for _, r := range sorted {
		existingIDs[r.ID] = true
	}

	out := make([]compactIconRecord, 0, keepN+2)
	modTarget := 3
	if keepN < 10 {
		modTarget = 1
	}
	modCount := 0

	for i := 0; i < keepN; i++ {
		clone := sorted[i]
		if len(clone.ThumbPNG) > 0 {
			clone.ThumbPNG = append([]byte(nil), clone.ThumbPNG...)
		}
		if modCount < modTarget && keepN >= 4 && i > 0 && (i == keepN-2 || (keepN >= 30 && i%max(1, keepN/4) == 0)) {
			neighbor := sorted[(i+1)%keepN]
			clone.Digest = "baseline-prev-" + clone.Digest
			if neighbor.BlobKey != "" && neighbor.BlobKey != clone.BlobKey {
				clone.BlobKey = neighbor.BlobKey
			}
			if len(neighbor.ThumbPNG) > 0 {
				clone.ThumbPNG = append([]byte(nil), neighbor.ThumbPNG...)
			}
			modCount++
		}
		out = append(out, clone)
	}

	if keepN >= 12 {
		addedRemoved := 0
		for _, candID := range []int{999998, 999999} {
			if existingIDs[candID] || addedRemoved >= len(out) {
				continue
			}
			src := out[addedRemoved]
			out = append(out, compactIconRecord{
				ID:        candID,
				Digest:    fmt.Sprintf("baseline-rem-%d", candID),
				Variants:  src.Variants,
				BlobKey:   src.BlobKey,
				ThumbPNG:  src.ThumbPNG,
				HasHR1:    src.HasHR1,
				HasHQ:     src.HasHQ,
				HasLocale: src.HasLocale,
			})
			addedRemoved++
		}
	}

	return out
}

func summarizeIconRecordVariants(lang i18n.Lang, ic compactIconRecord) string {
	var tags []string
	if ic.HasHR1 {
		tags = append(tags, "HD")
	}
	if ic.HasHQ {
		tags = append(tags, "HQ")
	}
	if ic.HasLocale {
		tags = append(tags, i18n.T(lang, "icons.badge_loc"))
	}
	if len(tags) == 0 {
		return i18n.T(lang, "icons.badge_std")
	}
	return strings.Join(tags, " · ")
}

func diffIcons(lang i18n.Lang, fromSnap, toSnap *snapshotPayload, summary *DiffSummary) []DiffItemEntry {
	// 兼容未记录图标指纹的历史旧版快照：缺失字段时平滑跳过，不误报全量新增
	if len(fromSnap.Icons) == 0 || len(toSnap.Icons) == 0 {
		return nil
	}

	fromMap := make(map[int]compactIconRecord, len(fromSnap.Icons))
	for _, ic := range fromSnap.Icons {
		fromMap[ic.ID] = ic
	}
	toMap := make(map[int]compactIconRecord, len(toSnap.Icons))
	for _, ic := range toSnap.Icons {
		toMap[ic.ID] = ic
	}

	var entries []DiffItemEntry
	noneStr := i18n.T(lang, "common.none")

	for _, toIc := range toSnap.Icons {
		code := fmt.Sprintf("%06d", toIc.ID)
		groupCode := sqpack.IconGroupCode(toIc.ID)
		fromIc, exists := fromMap[toIc.ID]
		if !exists {
			summary.AddedIcons++
			entries = append(entries, DiffItemEntry{
				ID:                 toIc.ID,
				EntityType:         "icon",
				ChangeType:         "added",
				NameCn:             "#" + code,
				NameEn:             "#" + code,
				NameJa:             "#" + code,
				IconID:             toIc.ID,
				CategoryName:       groupCode,
				IconVariantSummary: summarizeIconRecordVariants(lang, toIc),
				NewBlobKey:         toIc.BlobKey,
				NewThumbPNG:        toIc.ThumbPNG,
				Changes: []ItemFieldChange{{
					Field:  "icon",
					Label:  i18n.T(lang, "diff.change_added"),
					Before: noneStr,
					After:  summarizeIconRecordVariants(lang, toIc),
				}},
			})
			continue
		}

		if fromIc.Digest != toIc.Digest || fromIc.Variants != toIc.Variants {
			summary.ModifiedIcons++
			var changes []ItemFieldChange
			if fromIc.Variants != toIc.Variants {
				changes = append(changes, ItemFieldChange{
					Field:  "variants",
					Label:  i18n.T(lang, "icons.insp_variants_label"),
					Before: summarizeIconRecordVariants(lang, fromIc),
					After:  summarizeIconRecordVariants(lang, toIc),
				})
			} else {
				changes = append(changes, ItemFieldChange{
					Field:  "image",
					Label:  i18n.T(lang, "diff.icon_image_changed"),
					Before: i18n.T(lang, "diff.icon_prev_label"),
					After:  i18n.T(lang, "diff.icon_new_label"),
				})
			}
			entries = append(entries, DiffItemEntry{
				ID:                 toIc.ID,
				EntityType:         "icon",
				ChangeType:         "modified",
				NameCn:             "#" + code,
				NameEn:             "#" + code,
				NameJa:             "#" + code,
				IconID:             toIc.ID,
				CategoryName:       groupCode,
				IconVariantSummary: summarizeIconRecordVariants(lang, toIc),
				OldBlobKey:         fromIc.BlobKey,
				NewBlobKey:         toIc.BlobKey,
				OldThumbPNG:        fromIc.ThumbPNG,
				NewThumbPNG:        toIc.ThumbPNG,
				Changes:            changes,
			})
		}
	}

	for _, fromIc := range fromSnap.Icons {
		if _, exists := toMap[fromIc.ID]; !exists {
			summary.RemovedIcons++
			code := fmt.Sprintf("%06d", fromIc.ID)
			entries = append(entries, DiffItemEntry{
				ID:                 fromIc.ID,
				EntityType:         "icon",
				ChangeType:         "removed",
				NameCn:             "#" + code,
				NameEn:             "#" + code,
				NameJa:             "#" + code,
				IconID:             fromIc.ID,
				CategoryName:       sqpack.IconGroupCode(fromIc.ID),
				IconVariantSummary: summarizeIconRecordVariants(lang, fromIc),
				OldBlobKey:         fromIc.BlobKey,
				OldThumbPNG:        fromIc.ThumbPNG,
				Changes: []ItemFieldChange{{
					Field:  "icon",
					Label:  i18n.T(lang, "diff.change_removed"),
					Before: summarizeIconRecordVariants(lang, fromIc),
					After:  noneStr,
				}},
			})
		}
	}

	return entries
}
