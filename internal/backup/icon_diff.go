package backup

import (
	"fmt"
	"image"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

type compactIconRecord struct {
	ID        int
	Digest    string
	Variants  string
	BlobKey   string
	ThumbPNG  []byte
	HasHR1    bool
	HasHQ     bool
	HasLocale bool
}

func convertIconDigests(in []snapshot.IconDigestRecord) []compactIconRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]compactIconRecord, len(in))
	for i, r := range in {
		out[i] = compactIconRecord{
			ID:        r.ID,
			Digest:    r.Digest,
			Variants:  r.Variants,
			BlobKey:   r.BlobKey,
			HasHR1:    r.HasHR1,
			HasHQ:     r.HasHQ,
			HasLocale: r.HasLocale,
		}
	}
	return out
}

func (s *Service) loadLiveIconsForRegionLocked(region string, liveStore *gamedata.Store) []compactIconRecord {
	reg := snapshot.NormalizeRegion(region)
	if icons, ok := s.memLiveIcons[reg]; ok && len(icons) > 0 {
		return icons
	}
	if s.liveIconComputedAt[reg] {
		return s.liveIconCache[reg]
	}
	s.liveIconComputedAt[reg] = true
	if liveStore == nil {
		return nil
	}

	// 若启动阶段采用后台延迟加载图标索引，此处先等待完成并直接复用扫描结果，避免重复扫盘
	if liveStore.CNReader != nil || liveStore.GlobalReader != nil || liveStore.PrimaryReader != nil {
		_ = liveStore.EnsureIconsLoaded()
	}

	iconsSnap := liveStore.SnapshotIconsForExport(-1)
	if len(iconsSnap) == 0 {
		return nil
	}

	// 无底层客户端归档时（纯内存测试 Store）根据内存图标元数据生成轻量指纹
	if liveStore.CNReader == nil && liveStore.GlobalReader == nil && liveStore.PrimaryReader == nil {
		out := make([]compactIconRecord, 0, len(iconsSnap))
		for _, ic := range iconsSnap {
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
		s.liveIconCache[reg] = out
		return out
	}

	// 严格仅扫描当前对比区服对应客户端的 060000 图标（国服对国服、国际服对国际服）
	var targetReader *sqpack.GameReader
	if reg == snapshot.RegionGlobal {
		targetReader = liveStore.GlobalReader
		if targetReader == nil {
			targetReader = liveStore.PrimaryReader
		}
	} else {
		targetReader = liveStore.CNReader
		if targetReader == nil {
			targetReader = liveStore.PrimaryReader
		}
	}
	if targetReader == nil {
		return nil
	}
	files := liveStore.IconFilesForRegion(reg)
	if len(files) == 0 {
		var err error
		files, err = sqpack.ScanIconFiles(targetReader)
		if err != nil || len(files) == 0 {
			return nil
		}
	}
	snaps, _, err := sqpack.ComputeIconDigests([]sqpack.IconDigestSource{{
		Reader: targetReader,
		Files:  files,
	}})
	if err != nil || len(snaps) == 0 {
		return nil
	}
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
	s.liveIconCache[reg] = out
	return out
}

// ReadIconBlobRaw 从内存测试数据或已下载的 .xivsnap 快照中读取指定键的原始图标载荷字节。
func (s *Service) ReadIconBlobRaw(blobKey string) ([]byte, bool) {
	if s == nil || blobKey == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if raw, ok := s.memBlobs[blobKey]; ok && len(raw) > 0 {
		return append([]byte(nil), raw...), true
	}
	for _, arch := range s.archives {
		if raw, ok := arch.ReadIconThumbBlob(blobKey); ok && len(raw) > 0 {
			return append([]byte(nil), raw...), true
		}
	}
	for id, info := range s.localSnaps {
		if _, opened := s.archives[id]; opened {
			continue
		}
		arch, err := snapshot.OpenArchive(info.FilePath)
		if err != nil {
			continue
		}
		s.archives[id] = arch
		if raw, ok := arch.ReadIconThumbBlob(blobKey); ok && len(raw) > 0 {
			return append([]byte(nil), raw...), true
		}
	}
	return nil, false
}

// ReadIconBlobImage 从已下载的 .xivsnap 快照附带的变动/移除旧图标缩略图载荷中解码历史图标图像。
func (s *Service) ReadIconBlobImage(blobKey string) (*image.RGBA, error) {
	raw, ok := s.ReadIconBlobRaw(blobKey)
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	return sqpack.DecodeIconBlobToRGBA(raw)
}

// ReadIconBlobPNG 从内存载荷或快照归档中读取历史图标并还原为 PNG 字节（供版本对比批量导出使用）。
func (s *Service) ReadIconBlobPNG(blobKey string, thumbRaw []byte) ([]byte, error) {
	if len(thumbRaw) > 0 {
		return sqpack.DecodeIconBlobToPNG(thumbRaw)
	}
	if blobKey == "" || s == nil {
		return nil, nil
	}
	raw, ok := s.ReadIconBlobRaw(blobKey)
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	return sqpack.DecodeIconBlobToPNG(raw)
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
