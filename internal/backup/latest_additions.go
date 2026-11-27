package backup

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

type cachedLatestAdditions struct {
	key string
	val *gamedata.LatestPatchAdditions
}

// isSameOrNewerThanLive 判断本地快照是否与当前本地客户端为同一版本（或更新于当前客户端）。
// 若快照仓库尚未更新导致当前客户端版本号无法识别（livePatch 为空），则仅当内部构建号完全相同时视为同版。
func isSameOrNewerThanLive(d VersionBackupMeta, liveVer, livePatch string) bool {
	if liveVer != "" && d.Version != "" && d.Version == liveVer {
		return true
	}
	if livePatch != "" && d.Patch != "" {
		if d.Patch == livePatch || comparePatchDesc(d.Patch, livePatch) {
			return true
		}
	}
	return false
}

// selectPreviousSnapshotLocked 为指定区服从本地已下载快照中挑选「版本最新、但不是当前版本」的快照作为上一版本基线。
// 若目录尚未更新导致当前客户端版本号无法识别，则自动以本地已下载快照中最高的版本作为上一版本。
func (s *Service) selectPreviousSnapshotLocked(region string) (prevPatch, livePatch, liveVer string, ok bool) {
	reg := snapshot.NormalizeRegion(region)
	if reg == snapshot.RegionGlobal {
		if !s.hasGlobalLocked() {
			return "", "", "", false
		}
		liveVer = s.globalVersionLocked()
	} else {
		if !s.hasCNLocked() {
			return "", "", "", false
		}
		liveVer = s.cnVersionLocked()
	}
	livePatch = s.matchLivePatchLocked(reg, liveVer)

	// 收集该区服本地已下载（含内存测试注册）的全部快照
	seen := make(map[string]VersionBackupMeta)
	for _, info := range s.localSnaps {
		if snapshot.NormalizeRegion(info.Meta.Region) != reg || strings.TrimSpace(info.Meta.Patch) == "" {
			continue
		}
		seen[info.Meta.Patch] = VersionBackupMeta{
			ID:      info.Meta.Patch,
			Patch:   info.Meta.Patch,
			Version: info.Meta.GameVersion,
		}
	}
	for _, ms := range s.memSnaps {
		if snapshot.NormalizeRegion(ms.Region) != reg || strings.TrimSpace(ms.Patch) == "" {
			continue
		}
		seen[ms.Patch] = VersionBackupMeta{
			ID:      ms.Patch,
			Patch:   ms.Patch,
			Version: ms.Version,
		}
	}
	if len(seen) == 0 {
		return "", livePatch, liveVer, false
	}

	downloaded := make([]VersionBackupMeta, 0, len(seen))
	for _, m := range seen {
		downloaded = append(downloaded, m)
	}
	sortUnifiedVersions(downloaded)

	for _, d := range downloaded {
		if isSameOrNewerThanLive(d, liveVer, livePatch) {
			continue
		}
		return d.Patch, livePatch, liveVer, true
	}
	return "", livePatch, liveVer, false
}

// ComputeLatestAdditions 根据本地已下载的历史快照自动确定「上一版本」，并快速计算当前客户端相较于上一版本新增的
// 物品、任务、理符、成就、音乐（BGM / 管弦乐琴）与图标 ID 集合。若无可用的上一版本快照则返回 nil, nil。
func (s *Service) ComputeLatestAdditions(region string) (*gamedata.LatestPatchAdditions, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	reg := strings.TrimSpace(region)
	if reg == "" {
		reg = s.defaultRegionLocked()
	} else {
		reg = snapshot.NormalizeRegion(reg)
	}

	prevPatch, livePatch, liveVer, ok := s.selectPreviousSnapshotLocked(reg)
	if !ok || prevPatch == "" {
		return nil, nil
	}

	lang := s.langLocked()
	liveSt, err := s.liveStoreForRegionLocked(reg, lang)
	if err != nil || liveSt == nil {
		return nil, err
	}
	iconsReady := liveSt.IconsReady()
	cacheKey := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%t", reg, prevPatch, livePatch, liveVer, lang, iconsReady)
	if s.latestAddCache != nil && s.latestAddCache.key == cacheKey {
		return s.latestAddCache.val, nil
	}

	prevSets, err := s.loadPrevEntityIDSetsLocked(reg, prevPatch)
	if err != nil {
		return nil, err
	}

	hasIconBaseline := prevSets.hasIcons && iconsReady
	add := &gamedata.LatestPatchAdditions{
		PrevPatch:       prevPatch,
		CurrentPatch:    livePatch,
		Items:           make(map[int]bool),
		Quests:          make(map[int]bool),
		Leves:           make(map[int]bool),
		Achievements:    make(map[int]bool),
		BGMs:            make(map[int]bool),
		Orchestrions:    make(map[int]bool),
		Icons:           make(map[int]bool),
		HasIconBaseline: hasIconBaseline,
	}

	if prevSets.hasItems {
		for _, it := range liveSt.ItemSummaries {
			if !prevSets.items[it.ID] {
				add.Items[it.ID] = true
			}
		}
	}
	if prevSets.hasQuests {
		for _, q := range liveSt.Quests {
			if !prevSets.quests[q.ID] {
				add.Quests[q.ID] = true
			}
		}
	}
	if prevSets.hasLeves {
		for _, lv := range liveSt.Leves {
			if !prevSets.leves[lv.ID] {
				add.Leves[lv.ID] = true
			}
		}
	}
	if prevSets.hasAchievements {
		for _, ach := range liveSt.Achievements {
			if !prevSets.achievements[ach.ID] {
				add.Achievements[ach.ID] = true
			}
		}
	}
	if prevSets.hasBGMs {
		for _, b := range liveSt.BGMs {
			if !prevSets.bgms[b.ID] {
				add.BGMs[b.ID] = true
			}
		}
	}
	if prevSets.hasOrchestrions {
		for _, o := range liveSt.Orchestrions {
			if !prevSets.orchestrions[o.ID] {
				add.Orchestrions[o.ID] = true
			}
		}
	}
	// 仅当上一版本快照包含图标指纹（非仅 0a 快照）且图标索引已就绪时才标记新增图标，避免误将全量图标标为新增
	if hasIconBaseline {
		for _, ic := range liveSt.SnapshotIconsForExport(-1) {
			if !prevSets.icons[ic.ID] {
				add.Icons[ic.ID] = true
			}
		}
	}

	s.latestAddCache = &cachedLatestAdditions{
		key: cacheKey,
		val: add,
	}
	return add, nil
}

type prevEntityIDSets struct {
	items           map[int]bool
	hasItems        bool
	quests          map[int]bool
	hasQuests       bool
	leves           map[int]bool
	hasLeves        bool
	achievements    map[int]bool
	hasAchievements bool
	bgms            map[int]bool
	hasBGMs         bool
	orchestrions    map[int]bool
	hasOrchestrions bool
	icons           map[int]bool
	hasIcons        bool
}

func (s *Service) loadPrevEntityIDSetsLocked(region, patch string) (prevEntityIDSets, error) {
	var sets prevEntityIDSets
	snapID := snapshot.MakeSnapshotID(region, patch)

	// 1. 内存测试快照路径
	if ms, ok := s.memSnaps[snapID]; ok {
		if ms.Store != nil {
			if len(ms.Store.ItemSummaries) > 0 {
				sets.hasItems = true
				sets.items = make(map[int]bool, len(ms.Store.ItemSummaries))
				for _, it := range ms.Store.ItemSummaries {
					sets.items[it.ID] = true
				}
			}
			if len(ms.Store.Quests) > 0 {
				sets.hasQuests = true
				sets.quests = make(map[int]bool, len(ms.Store.Quests))
				for _, q := range ms.Store.Quests {
					sets.quests[q.ID] = true
				}
			}
			if len(ms.Store.Leves) > 0 {
				sets.hasLeves = true
				sets.leves = make(map[int]bool, len(ms.Store.Leves))
				for _, lv := range ms.Store.Leves {
					sets.leves[lv.ID] = true
				}
			}
			if len(ms.Store.Achievements) > 0 {
				sets.hasAchievements = true
				sets.achievements = make(map[int]bool, len(ms.Store.Achievements))
				for _, ach := range ms.Store.Achievements {
					sets.achievements[ach.ID] = true
				}
			}
			if len(ms.Store.BGMs) > 0 {
				sets.hasBGMs = true
				sets.bgms = make(map[int]bool, len(ms.Store.BGMs))
				for _, b := range ms.Store.BGMs {
					sets.bgms[b.ID] = true
				}
			}
			if len(ms.Store.Orchestrions) > 0 {
				sets.hasOrchestrions = true
				sets.orchestrions = make(map[int]bool, len(ms.Store.Orchestrions))
				for _, o := range ms.Store.Orchestrions {
					sets.orchestrions[o.ID] = true
				}
			}
		}
		if len(ms.Icons) > 0 {
			sets.hasIcons = true
			sets.icons = make(map[int]bool, len(ms.Icons))
			for _, ic := range ms.Icons {
				sets.icons[ic.ID] = true
			}
		}
		return sets, nil
	}

	// 2. 本地 .xivsnap 归档轻量快速读取：仅解压 6 张核心表的名称/路径列 + 图标指纹，不构建完整配方/商店图谱
	arch, err := s.openArchiveLocked(region, patch)
	if err != nil {
		return sets, err
	}
	schemasFS := arch.SchemasFS()
	if schemasFS == nil && s.store != nil {
		schemasFS = s.store.SchemasFS
	}

	exdLang := sqpack.LangChineseSimplified
	if region == snapshot.RegionGlobal {
		exdLang = globalCompareEXDLang(s.langLocked())
	}

	var wg sync.WaitGroup
	wg.Add(7)

	go func() {
		defer wg.Done()
		if rows, rErr := sqpack.LoadSheetRows(arch, "Item", exdLang, []int{9}); rErr == nil && len(rows) > 0 {
			m := make(map[int]bool, len(rows))
			for id, row := range rows {
				if id > 0 && row.StringAt(9) != "" {
					m[id] = true
				}
			}
			sets.items = m
			sets.hasItems = len(m) > 0
		}
	}()

	go func() {
		defer wg.Done()
		if sh := loadSchemaNameSheet(arch, schemasFS, "Quest", exdLang, "Name"); sh != nil {
			m := make(map[int]bool, len(sh.Rows))
			for id, row := range sh.Rows {
				if id > 0 && strings.TrimSpace(sh.Str(row, "Name")) != "" {
					m[id] = true
				}
			}
			sets.quests = m
			sets.hasQuests = len(m) > 0
		}
	}()

	go func() {
		defer wg.Done()
		if sh := loadSchemaNameSheet(arch, schemasFS, "Leve", exdLang, "Name"); sh != nil {
			m := make(map[int]bool, len(sh.Rows))
			for id, row := range sh.Rows {
				if id > 0 && strings.TrimSpace(sh.Str(row, "Name")) != "" {
					m[id] = true
				}
			}
			sets.leves = m
			sets.hasLeves = len(m) > 0
		}
	}()

	go func() {
		defer wg.Done()
		if rows, rErr := sqpack.LoadSheetRows(arch, "Achievement", exdLang, []int{1}); rErr == nil && len(rows) > 0 {
			m := make(map[int]bool, len(rows))
			for id, row := range rows {
				if id > 0 && row.StringAt(1) != "" {
					m[id] = true
				}
			}
			sets.achievements = m
			sets.hasAchievements = len(m) > 0
		}
	}()

	go func() {
		defer wg.Done()
		if sh := loadSchemaNameSheet(arch, schemasFS, "BGM", sqpack.LangNone, "File"); sh != nil {
			m := make(map[int]bool, len(sh.Rows))
			for id, row := range sh.Rows {
				if id > 0 && strings.TrimSpace(sh.Str(row, "File")) != "" {
					m[id] = true
				}
			}
			sets.bgms = m
			sets.hasBGMs = len(m) > 0
		}
	}()

	go func() {
		defer wg.Done()
		if sh := loadSchemaNameSheet(arch, schemasFS, "Orchestrion", exdLang, "Name"); sh != nil {
			m := make(map[int]bool, len(sh.Rows))
			for id, row := range sh.Rows {
				if id > 0 && strings.TrimSpace(sh.Str(row, "Name")) != "" {
					m[id] = true
				}
			}
			sets.orchestrions = m
			sets.hasOrchestrions = len(m) > 0
		}
	}()

	go func() {
		defer wg.Done()
		if iconDigests, rErr := arch.ReadIconDigests(); rErr == nil && len(iconDigests) > 0 {
			m := make(map[int]bool, len(iconDigests))
			for _, ic := range iconDigests {
				m[ic.ID] = true
			}
			sets.icons = m
			sets.hasIcons = true
		}
	}()

	wg.Wait()
	return sets, nil
}

func loadSchemaNameSheet(reader sqpack.FileReader, schemasFS fs.FS, sheet string, lang sqpack.Language, field string) *sqpack.SchemaSheet {
	if reader == nil || schemasFS == nil {
		return nil
	}
	sh, err := sqpack.LoadSheetWithSchema(reader, schemasFS, sheet, lang, []string{field})
	if err != nil {
		return nil
	}
	return sh
}
