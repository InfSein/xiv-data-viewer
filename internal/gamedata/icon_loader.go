package gamedata

import (
	"fmt"
	"image"
	"sort"
	"strconv"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

type iconBuildInput struct {
	primaryReader   *sqpack.GameReader
	cnReader        *sqpack.GameReader
	globalReader    *sqpack.GameReader
	cnIconFiles     []sqpack.IconFileEntry
	globalIconFiles []sqpack.IconFileEntry
	itemSummaries   []ItemSummary
	achievements    []AchievementEntry
	quests          []QuestEntry
	leves           []LeveEntry
}

type iconIndices struct {
	icons             []IconEntry
	iconByID          map[int]IconEntry
	groups            []IconGroupInfo
	iconsByGroup      map[int][]IconEntry
	availableVariants []string
	usagesByID        map[int][]IconUsageRef
}

func isLocalizedIconVariant(v string) bool {
	return v != "" && v != "hq"
}

func buildIconIndex(in iconBuildInput) iconIndices {
	var primaryFiles []sqpack.IconFileEntry
	var secondaryFiles []sqpack.IconFileEntry
	var secondarySource IconReaderSource

	switch {
	case in.primaryReader != nil && in.primaryReader == in.cnReader:
		primaryFiles = in.cnIconFiles
		if in.globalReader != nil && in.globalReader != in.primaryReader {
			secondaryFiles = in.globalIconFiles
			secondarySource = IconSourceGlobal
		}
	case in.primaryReader != nil && in.primaryReader == in.globalReader:
		primaryFiles = in.globalIconFiles
		if in.cnReader != nil && in.cnReader != in.primaryReader {
			secondaryFiles = in.cnIconFiles
			secondarySource = IconSourceCN
		}
	default:
		if len(in.cnIconFiles) > 0 {
			primaryFiles = in.cnIconFiles
		} else {
			primaryFiles = in.globalIconFiles
		}
	}

	type accIcon struct {
		id       int
		groupID  int
		variants map[string]*IconVariantInfo
	}
	byID := make(map[int]*accIcon, 75000)
	variantUsed := make(map[string]bool, len(sqpack.KnownIconVariants))

	addFile := func(f sqpack.IconFileEntry, src IconReaderSource) {
		acc := byID[f.IconID]
		if acc == nil {
			acc = &accIcon{
				id:       f.IconID,
				groupID:  f.GroupID,
				variants: make(map[string]*IconVariantInfo, 2),
			}
			byID[f.IconID] = acc
		}
		vi := acc.variants[f.Variant]
		if vi == nil {
			vi = &IconVariantInfo{Variant: f.Variant}
			acc.variants[f.Variant] = vi
		}
		variantUsed[f.Variant] = true
		if f.HR1 {
			if !vi.HasHR1 {
				vi.HasHR1 = true
				vi.HR1Source = src
			}
		} else {
			if !vi.HasStd {
				vi.HasStd = true
				vi.StdSource = src
			}
		}
	}

	for _, f := range primaryFiles {
		addFile(f, IconSourcePrimary)
	}
	if secondarySource != IconSourceNone {
		for _, f := range secondaryFiles {
			addFile(f, secondarySource)
		}
	}

	variantOrder := make(map[string]int, len(sqpack.KnownIconVariants))
	for i, v := range sqpack.KnownIconVariants {
		variantOrder[v] = i
	}

	var availableVariants []string
	for _, v := range sqpack.KnownIconVariants {
		if variantUsed[v] {
			availableVariants = append(availableVariants, v)
		}
	}

	ids := make([]int, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	icons := make([]IconEntry, 0, len(ids))
	iconByID := make(map[int]IconEntry, len(ids))
	iconsByGroup := make(map[int][]IconEntry, 200)
	groupStats := make(map[int]*IconGroupInfo, 200)

	for _, id := range ids {
		acc := byID[id]
		vars := make([]IconVariantInfo, 0, len(acc.variants))
		hasHQ := false
		hasHR1 := false
		hasLoc := false
		for _, v := range acc.variants {
			vars = append(vars, *v)
			if v.Variant == "hq" {
				hasHQ = true
			}
			if v.HasHR1 {
				hasHR1 = true
			}
			if isLocalizedIconVariant(v.Variant) {
				hasLoc = true
			}
		}
		sort.Slice(vars, func(i, j int) bool {
			return variantOrder[vars[i].Variant] < variantOrder[vars[j].Variant]
		})

		defVar := ""
		defHR1 := false
		if len(vars) > 0 {
			chosen := vars[0]
			for _, v := range vars {
				if v.Variant == "" {
					chosen = v
					break
				}
			}
			defVar = chosen.Variant
			defHR1 = !chosen.HasStd && chosen.HasHR1
		}

		entry := IconEntry{
			ID:             id,
			GroupID:        acc.groupID,
			Code:           fmt.Sprintf("%06d", id),
			DefaultVariant: defVar,
			DefaultHR1:     defHR1,
			HasHQ:          hasHQ,
			HasHR1:         hasHR1,
			HasLocalized:   hasLoc,
			Variants:       vars,
		}
		icons = append(icons, entry)
		iconByID[id] = entry
		iconsByGroup[acc.groupID] = append(iconsByGroup[acc.groupID], entry)

		gs := groupStats[acc.groupID]
		if gs == nil {
			code := sqpack.IconGroupCode(acc.groupID)
			gs = &IconGroupInfo{
				GroupID:    acc.groupID,
				Code:       code,
				RangeLabel: fmt.Sprintf("%s – %06d", code, acc.groupID+sqpack.IconGroupSize-1),
			}
			groupStats[acc.groupID] = gs
		}
		gs.Count++
		if hasHQ {
			gs.HQCount++
		}
		if hasHR1 {
			gs.HR1Count++
		}
		if hasLoc {
			gs.LocalizedCount++
		}
	}

	groupIDs := make([]int, 0, len(groupStats))
	for gid := range groupStats {
		groupIDs = append(groupIDs, gid)
	}
	sort.Ints(groupIDs)

	groups := make([]IconGroupInfo, 0, len(groupIDs))
	for _, gid := range groupIDs {
		groups = append(groups, *groupStats[gid])
	}

	// 构建图标编号到关联游戏条目的反向索引（去重并限定合理上限，避免极高频通用图标占用过多内存）
	usagesByID := make(map[int][]IconUsageRef, len(in.itemSummaries)/2)
	seenUsage := make(map[int]map[string]bool)
	addUsage := func(iconID int, kind string, refID int, name string) {
		if iconID < 0 || refID <= 0 || strings.TrimSpace(name) == "" {
			return
		}
		if _, exists := iconByID[iconID]; !exists {
			return
		}
		if len(usagesByID[iconID]) >= 24 {
			return
		}
		if seenUsage[iconID] == nil {
			seenUsage[iconID] = make(map[string]bool, 4)
		}
		sig := fmt.Sprintf("%s:%d", kind, refID)
		if seenUsage[iconID][sig] {
			return
		}
		seenUsage[iconID][sig] = true
		usagesByID[iconID] = append(usagesByID[iconID], IconUsageRef{
			Kind: kind,
			ID:   refID,
			Name: name,
		})
	}

	for _, it := range in.itemSummaries {
		addUsage(it.IconID, "item", it.ID, it.NameCn)
	}
	for _, ach := range in.achievements {
		addUsage(ach.IconID, "achievement", ach.ID, ach.NameCn)
	}
	for _, q := range in.quests {
		title := CleanQuestTitle(q.NameCn)
		if title == "" {
			title = q.NameCn
		}
		addUsage(q.IconID, "quest", q.ID, title)
		addUsage(q.BannerIconID, "quest", q.ID, title)
		addUsage(q.SpecialIconID, "quest", q.ID, title)
	}
	for _, lv := range in.leves {
		addUsage(lv.IconCityState, "leve", lv.ID, lv.NameCn)
		addUsage(lv.IconIssuer, "leve", lv.ID, lv.NameCn)
		addUsage(lv.AssignmentTypeIconID, "leve", lv.ID, lv.NameCn)
	}

	return iconIndices{
		icons:             icons,
		iconByID:          iconByID,
		groups:            groups,
		iconsByGroup:      iconsByGroup,
		availableVariants: availableVariants,
		usagesByID:        usagesByID,
	}
}

func (s *Service) readerForIconSource(src IconReaderSource) *sqpack.GameReader {
	if s == nil || s.store == nil {
		return nil
	}
	switch src {
	case IconSourceCN:
		if s.store.CNReader != nil {
			return s.store.CNReader
		}
	case IconSourceGlobal:
		if s.store.GlobalReader != nil {
			return s.store.GlobalReader
		}
	}
	return s.store.PrimaryReader
}

// ResolveIconVariantSource 查找指定图标、变体和分辨率对应的只读 SqPack 读取器。
func (s *Service) ResolveIconVariantSource(iconID int, variant string, hr1 bool) *sqpack.GameReader {
	if s == nil || s.store == nil || iconID < 0 {
		return nil
	}
	vNorm := strings.Trim(strings.ToLower(strings.TrimSpace(variant)), "/")
	if entry, ok := s.store.IconByID[iconID]; ok {
		for _, v := range entry.Variants {
			if v.Variant != vNorm {
				continue
			}
			if hr1 {
				if v.HasHR1 {
					return s.readerForIconSource(v.HR1Source)
				}
			} else {
				if v.HasStd {
					return s.readerForIconSource(v.StdSource)
				}
			}
		}
	}
	return s.store.PrimaryReader
}

// GetIconGroups 返回所有包含有效图标资源的分组（每 1000 个 ID 一组，隐藏无资源空分组）。
func (s *Service) GetIconGroups() []IconGroupInfo {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.IconGroups
}

// GetIconDetail 返回指定编号图标的变体元数据；不存在有效资源时返回 nil。
func (s *Service) GetIconDetail(iconID int) *IconEntry {
	if s == nil || s.store == nil || iconID < 0 {
		return nil
	}
	if entry, ok := s.store.IconByID[iconID]; ok {
		cp := entry
		return &cp
	}
	return nil
}

// GetIconUsages 返回引用指定图标编号的游戏物品、任务、成就或理符列表。
func (s *Service) GetIconUsages(iconID int) []IconUsageRef {
	if s == nil || s.store == nil || iconID < 0 {
		return nil
	}
	return s.store.IconUsagesByID[iconID]
}

// GetIconVariantImage 读取并解码指定图标变体与分辨率；maxThumbSide > 0 时按比例缩放超大贴图以节省显存。
func (s *Service) GetIconVariantImage(iconID int, variant string, hr1 bool, maxThumbSide int) (*image.RGBA, sqpack.TextureHeader, error) {
	reader := s.ResolveIconVariantSource(iconID, variant, hr1)
	if reader == nil {
		return nil, sqpack.TextureHeader{}, nil
	}
	img, hdr, err := sqpack.ReadIconVariantImage(reader, s.store.TextureCache, iconID, variant, hr1, maxThumbSide)
	if (img != nil || err != nil) || s.store.GlobalReader == nil || s.store.GlobalReader == reader {
		return img, hdr, err
	}
	return sqpack.ReadIconVariantImage(s.store.GlobalReader, s.store.TextureCache, iconID, variant, hr1, maxThumbSide)
}

// ReadIconVariantRaw 读取指定图标变体的原始 .tex 字节与头信息。
func (s *Service) ReadIconVariantRaw(iconID int, variant string, hr1 bool) ([]byte, sqpack.TextureHeader, error) {
	reader := s.ResolveIconVariantSource(iconID, variant, hr1)
	if reader == nil {
		return nil, sqpack.TextureHeader{}, nil
	}
	raw, hdr, err := sqpack.ReadIconVariantRaw(reader, iconID, variant, hr1)
	if (len(raw) > 0 || err != nil) || s.store.GlobalReader == nil || s.store.GlobalReader == reader {
		return raw, hdr, err
	}
	return sqpack.ReadIconVariantRaw(s.store.GlobalReader, iconID, variant, hr1)
}

// SearchIcons 按分组、变体条件或编号/关联名称过滤有效图标；无资源图标与空分组均跳过。
func (s *Service) SearchIcons(query IconSearchQuery) IconSearchResponse {
	if s == nil || s.store == nil {
		return IconSearchResponse{}
	}
	qRaw := strings.TrimSpace(query.Q)
	qTrim := strings.TrimPrefix(qRaw, "#")
	qLower := toLowerFast(qTrim)
	flt := query.VariantFilter
	if flt == "" {
		flt = "all"
	}

	var src []IconEntry
	if query.GroupID >= 0 {
		src = s.store.IconsByGroup[sqpack.IconGroupID(query.GroupID)]
	} else {
		src = s.store.Icons
	}
	if len(src) == 0 {
		return IconSearchResponse{}
	}

	if qLower == "" && flt == "all" {
		return IconSearchResponse{
			Items: src,
			Total: len(src),
		}
	}

	exactID := -1
	if qTrim != "" {
		if parsed, err := strconv.Atoi(qTrim); err == nil && parsed >= 0 {
			exactID = parsed
		}
	}

	out := make([]IconEntry, 0, min(len(src), 256))
	for _, ic := range src {
		switch flt {
		case "hr1":
			if !ic.HasHR1 {
				continue
			}
		case "hq":
			if !ic.HasHQ {
				continue
			}
		case "localized":
			if !ic.HasLocalized {
				continue
			}
		}

		if qLower != "" {
			matched := (exactID >= 0 && ic.ID == exactID) ||
				strings.Contains(ic.Code, qLower) ||
				strings.Contains(strconv.Itoa(ic.ID), qLower) ||
				strings.Contains(sqpack.IconGroupCode(ic.GroupID), qLower)
			if !matched {
				for _, u := range s.store.IconUsagesByID[ic.ID] {
					if strings.Contains(toLowerFast(u.Name), qLower) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, ic)
	}

	return IconSearchResponse{
		Items: out,
		Total: len(out),
	}
}

// ComputeIconDigests 快速扫描已挂载客户端图标指纹与代表性缩略图任务清单，不在启动阶段耗时解码全量像素。
func (s *Store) ComputeIconDigests() ([]sqpack.IconSnapshotRecord, []sqpack.IconBlobTask, error) {
	if s == nil {
		return nil, nil, nil
	}
	var sources []sqpack.IconDigestSource
	switch {
	case s.PrimaryReader != nil && s.PrimaryReader == s.CNReader:
		sources = append(sources, sqpack.IconDigestSource{Reader: s.CNReader, Files: s.cnIconFiles})
		if s.GlobalReader != nil && s.GlobalReader != s.PrimaryReader {
			sources = append(sources, sqpack.IconDigestSource{Reader: s.GlobalReader, Files: s.globalIconFiles})
		}
	case s.PrimaryReader != nil && s.PrimaryReader == s.GlobalReader:
		sources = append(sources, sqpack.IconDigestSource{Reader: s.GlobalReader, Files: s.globalIconFiles})
		if s.CNReader != nil && s.CNReader != s.PrimaryReader {
			sources = append(sources, sqpack.IconDigestSource{Reader: s.CNReader, Files: s.cnIconFiles})
		}
	default:
		if s.CNReader != nil {
			sources = append(sources, sqpack.IconDigestSource{Reader: s.CNReader, Files: s.cnIconFiles})
		}
		if s.GlobalReader != nil {
			sources = append(sources, sqpack.IconDigestSource{Reader: s.GlobalReader, Files: s.globalIconFiles})
		}
	}
	return sqpack.ComputeIconDigests(sources)
}

// ComputeIconSnapshots 同步扫描并完成代表性缩略图载荷构建。
func (s *Store) ComputeIconSnapshots(
	hasBlob func(blobKey string) bool,
	onBlob func(blobKey string, payload []byte) error,
) ([]sqpack.IconSnapshotRecord, error) {
	records, tasks, err := s.ComputeIconDigests()
	if err != nil || len(records) == 0 {
		return records, err
	}
	if err := sqpack.ArchiveIconBlobs(nil, tasks, hasBlob, onBlob, 0); err != nil {
		return nil, err
	}
	return records, nil
}
