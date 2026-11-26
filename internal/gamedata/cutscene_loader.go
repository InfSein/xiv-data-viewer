package gamedata

import (
	"fmt"
	"path"
	"runtime"
	"sort"
	"strings"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

const questCutsceneCacheCapacity = 128

const (
	completeJournalDutyIcon = 61802
	completeJournalLeveIcon = 62501
)

var completeJournalNeededFields = func() []string {
	fields := []string{"Name", "Unknown0", "Icon"}
	for i := 0; i < 24; i++ {
		fields = append(fields, fmt.Sprintf("Cutscene[%d]", i))
	}
	return fields
}()

// BuildQuestScriptPath 根据任务内部 ID 生成 SqPack 任务 Lua 字节码虚拟路径（如 game_script/quest/033/LucKma401_03300.luab）。
func BuildQuestScriptPath(internalID string) string {
	id := strings.TrimSpace(internalID)
	idx := strings.IndexByte(id, '_')
	if idx <= 0 || idx+4 > len(id) {
		return ""
	}
	numPart := id[idx+1:]
	if len(numPart) < 3 {
		return ""
	}
	return fmt.Sprintf("game_script/quest/%s/%s.luab", numPart[:3], id)
}

func isStrictCutsceneInstruction(ins string) bool {
	u := strings.ToUpper(strings.TrimSpace(ins))
	if u == "" || strings.Contains(u, "BGM") {
		return false
	}
	for _, bad := range []string{
		"ACTOR", "POS", "_SE", "VFX", "CAMERA", "SOUND", "ANIME", "MOTION",
		"ITEM", "EOBJ", "POP", "ENEMY", "BNPC", "MINION", "MOUNT", "WEATHER",
		"TIME", "ACTION", "EMOTE", "GIMMICK", "LAYOUT", "MAP", "TERRITORY",
		"EFFECT", "LIGHT", "SCREEN", "FADE", "VOICE", "TEXT", "MESSAGE",
		"QUEST", "SYSTEM", "FLAG", "VAR", "WORK", "VAL", "NUM", "ID", "IDX", "TYPE",
	} {
		if strings.Contains(u, bad) {
			return false
		}
	}
	return strings.Contains(u, "CUTSCENE") ||
		strings.Contains(u, "CUT_SCENE") ||
		strings.Contains(u, "NCUT") ||
		strings.HasPrefix(u, "CUT") ||
		strings.Contains(u, "_CUT")
}

// ExtractCutsceneShortName 从完整过场动画路径中提取便于展示的简短片段名。
func ExtractCutsceneShortName(csPath string) string {
	p := strings.TrimSpace(strings.ReplaceAll(csPath, "\\", "/"))
	if p == "" {
		return ""
	}
	base := path.Base(p)
	if base == "" || base == "." || base == "/" {
		return p
	}
	return base
}

// SummarizeQuestCutscenes 生成用于版本对比的紧凑过场动画摘要。
func SummarizeQuestCutscenes(cuts []QuestCutsceneRef) string {
	if len(cuts) == 0 {
		return ""
	}
	limit := min(2, len(cuts))
	parts := make([]string, 0, limit+1)
	for _, c := range cuts[:limit] {
		name := ExtractCutsceneShortName(c.Path)
		if name == "" {
			name = fmt.Sprintf("#%d", c.CutsceneID)
		}
		parts = append(parts, name)
	}
	summary := strings.Join(parts, ", ")
	if len(cuts) > limit {
		summary += fmt.Sprintf(" (+%d)", len(cuts)-limit)
	}
	return summary
}

type questCutsceneLinkData struct {
	questCutscenes map[int][]QuestCutsceneRef
	questCutVars   map[int]map[string]int
	questBGMVars   map[int]map[string]int
}

func buildQuestCutsceneLinks(
	quests *sqpack.SchemaSheet,
	cutscenes *sqpack.SchemaSheet,
	completeJournals *sqpack.SchemaSheet,
	bgmSheet *sqpack.SchemaSheet,
) questCutsceneLinkData {
	out := questCutsceneLinkData{
		questCutscenes: make(map[int][]QuestCutsceneRef),
		questCutVars:   make(map[int]map[string]int),
		questBGMVars:   make(map[int]map[string]int),
	}
	if quests == nil || cutscenes == nil || len(quests.Rows) == 0 || len(cutscenes.Rows) == 0 {
		return out
	}

	cutPathByID := make(map[int]string, len(cutscenes.Rows))
	for cid, row := range cutscenes.Rows {
		if cid <= 0 {
			continue
		}
		if p := strings.TrimSpace(cutscenes.Str(row, "Path")); p != "" {
			cutPathByID[cid] = p
		}
	}

	// 1. 提取每个任务 QuestParams 中的 Cutscene 与 BGM 脚本指令映射
	scriptCutByQuest := make(map[int][]int)
	for qid, row := range quests.Rows {
		if qid <= 0 {
			continue
		}
		var cutIDs []int
		seenCut := make(map[int]bool)
		var cutVars map[string]int
		var bgmVars map[string]int

		for i := 0; i < 50; i++ {
			ins := strings.TrimSpace(quests.Str(row, fmt.Sprintf("QuestParams[%d].ScriptInstruction", i)))
			arg := quests.Int(row, fmt.Sprintf("QuestParams[%d].ScriptArg", i))
			if ins == "" || arg <= 0 {
				continue
			}
			upper := strings.ToUpper(ins)
			if strings.Contains(upper, "BGM") && bgmSheet != nil {
				if _, ok := bgmSheet.Rows[arg]; ok {
					if bgmVars == nil {
						bgmVars = make(map[string]int)
					}
					bgmVars[ins] = arg
				}
				continue
			}
			if isStrictCutsceneInstruction(ins) {
				if _, ok := cutPathByID[arg]; ok {
					if cutVars == nil {
						cutVars = make(map[string]int)
					}
					cutVars[ins] = arg
					if !seenCut[arg] {
						seenCut[arg] = true
						cutIDs = append(cutIDs, arg)
					}
				}
			}
		}
		if len(cutIDs) > 0 {
			scriptCutByQuest[qid] = cutIDs
		}
		if len(cutVars) > 0 {
			out.questCutVars[qid] = cutVars
		}
		if len(bgmVars) > 0 {
			out.questBGMVars[qid] = bgmVars
		}
	}

	// 2. 通过 CompleteJournal 的 Quest 主键偏移 (Unknown0 + 65536 == QuestID) 补充青光纪念录剧情顺序与补充过场动画
	// 预先剔除理符（62501）与副本（61802）分类条目，并二次校验任务标题一致，杜绝同名副本或废弃任务串台
	journalCutByQuest := make(map[int][]int)
	if completeJournals != nil {
		for _, row := range completeJournals.Rows {
			icon := completeJournals.Int(row, "Icon")
			if icon == completeJournalDutyIcon || icon == completeJournalLeveIcon {
				continue
			}
			u0 := completeJournals.Int(row, "Unknown0")
			if u0 <= 0 {
				continue
			}
			candQID := 65536 + u0
			qRow, ok := quests.Rows[candQID]
			if !ok {
				continue
			}
			cjName := CleanQuestTitle(strings.TrimSpace(completeJournals.Str(row, "Name")))
			qName := CleanQuestTitle(strings.TrimSpace(quests.Str(qRow, "Name")))
			if cjName == "" || qName == "" || cjName != qName {
				continue
			}
			var cuts []int
			seen := make(map[int]bool)
			for i := 0; i < 24; i++ {
				cid := completeJournals.Int(row, fmt.Sprintf("Cutscene[%d]", i))
				if cid > 0 && !seen[cid] {
					if _, valid := cutPathByID[cid]; valid {
						seen[cid] = true
						cuts = append(cuts, cid)
					}
				}
			}
			if len(cuts) > 0 {
				journalCutByQuest[candQID] = cuts
			}
		}
	}

	// 3. 合并去重：以剧情日志顺序为优先，补入脚本独有过场动画
	for qid := range quests.Rows {
		jCuts := journalCutByQuest[qid]
		sCuts := scriptCutByQuest[qid]
		if len(jCuts) == 0 && len(sCuts) == 0 {
			continue
		}
		jSet := make(map[int]bool, len(jCuts))
		for _, cid := range jCuts {
			jSet[cid] = true
		}
		sSet := make(map[int]bool, len(sCuts))
		for _, cid := range sCuts {
			sSet[cid] = true
		}

		merged := make([]QuestCutsceneRef, 0, len(jCuts)+len(sCuts))
		seen := make(map[int]bool)
		addRef := func(cid int) {
			if cid <= 0 || seen[cid] {
				return
			}
			p := cutPathByID[cid]
			if p == "" {
				return
			}
			seen[cid] = true
			var src string
			switch {
			case sSet[cid] && jSet[cid]:
				src = "script+journal"
			case sSet[cid]:
				src = "script"
			default:
				src = "journal"
			}
			merged = append(merged, QuestCutsceneRef{
				CutsceneID: cid,
				Path:       p,
				Source:     src,
			})
		}
		for _, cid := range jCuts {
			addRef(cid)
		}
		for _, cid := range sCuts {
			addRef(cid)
		}
		if len(merged) > 0 {
			out.questCutscenes[qid] = merged
		}
	}

	return out
}

func buildEventSystemBGMMap(esd *sqpack.SchemaSheet, bgmSheet *sqpack.SchemaSheet) map[string]int {
	out := make(map[string]int)
	if esd == nil {
		return out
	}
	for _, row := range esd.Rows {
		key := strings.TrimSpace(esd.Str(row, "Text"))
		if !strings.HasPrefix(key, "BGM_") {
			continue
		}
		val := esd.Int(row, "DefineValue")
		if val <= 0 {
			continue
		}
		if bgmSheet != nil {
			if _, ok := bgmSheet.Rows[val]; !ok {
				continue
			}
		}
		out[key] = val
	}
	return out
}

func buildMusicFileLookup(music musicIndices) (bgmByFile map[string]MusicEntry, orchByFile map[string]MusicEntry) {
	bgmByFile = make(map[string]MusicEntry, len(music.bgms))
	for _, m := range music.bgms {
		k := strings.ToLower(strings.TrimSpace(m.File))
		if k == "" || m.IsPlaceholder {
			continue
		}
		if prev, exists := bgmByFile[k]; !exists || m.ID < prev.ID {
			bgmByFile[k] = m
		}
	}
	orchByFile = make(map[string]MusicEntry, len(music.orchestrions))
	for _, m := range music.orchestrions {
		k := strings.ToLower(strings.TrimSpace(m.File))
		if k == "" {
			continue
		}
		if prev, exists := orchByFile[k]; !exists || m.ID < prev.ID {
			orchByFile[k] = m
		}
	}
	return bgmByFile, orchByFile
}

type cutbExtractedBGM struct {
	bgmID  int
	fadeMs int
}

func (s *Service) musicBGMLookupMaps() (map[int]MusicEntry, map[string]MusicEntry) {
	bgmByID := s.store.BGMByID
	if len(bgmByID) == 0 && len(s.store.BGMs) > 0 {
		bgmByID = make(map[int]MusicEntry, len(s.store.BGMs))
		for _, b := range s.store.BGMs {
			if b.ID > 0 {
				bgmByID[b.ID] = b
			}
		}
	}
	bgmByFile := s.store.BGMByFile
	if len(bgmByFile) == 0 && len(bgmByID) > 0 {
		bgmByFile = make(map[string]MusicEntry, len(bgmByID))
		for _, b := range bgmByID {
			k := strings.ToLower(strings.TrimSpace(b.File))
			if k == "" || b.IsPlaceholder || isPlaceholderBGMPath(b.File) {
				continue
			}
			if prev, ok := bgmByFile[k]; !ok || b.ID < prev.ID {
				bgmByFile[k] = b
			}
		}
	}
	return bgmByID, bgmByFile
}

func extractCutbBGMTracks(rawCutb []byte, bgmByID map[int]MusicEntry, bgmByFile map[string]MusicEntry) ([]cutbExtractedBGM, bool) {
	if len(rawCutb) == 0 {
		return nil, false
	}
	cutbInfo, err := sqpack.ExtractCutbAudio(rawCutb)
	if err != nil {
		return nil, false
	}
	var out []cutbExtractedBGM
	seen := make(map[int]bool)
	for _, tr := range cutbInfo.Tracks {
		if tr.BGMID <= 0 || seen[tr.BGMID] {
			continue
		}
		b, ok := bgmByID[tr.BGMID]
		if !ok || b.IsPlaceholder || isPlaceholderBGMPath(b.File) {
			continue
		}
		seen[tr.BGMID] = true
		out = append(out, cutbExtractedBGM{bgmID: tr.BGMID, fadeMs: tr.FadeMs})
	}
	if len(out) == 0 {
		for _, mp := range cutbInfo.MovieAudioPaths {
			b, ok := bgmByFile[strings.ToLower(strings.TrimSpace(mp))]
			if !ok || seen[b.ID] || b.IsPlaceholder || isPlaceholderBGMPath(b.File) {
				continue
			}
			seen[b.ID] = true
			out = append(out, cutbExtractedBGM{bgmID: b.ID})
		}
	}
	return out, true
}

func resolveLuabBGMCalls(
	luaRaw []byte,
	cutVars map[string]int,
	bgmVars map[string]int,
	esBGMs map[string]int,
) (byCutscene map[int][]int, allBGMIDs []int) {
	if len(luaRaw) == 0 {
		return nil, nil
	}
	luaInfo, err := sqpack.ParseLuabCutsceneAndBGM(luaRaw)
	if err != nil {
		return nil, nil
	}
	resolveArg := func(arg string) int {
		if bid := bgmVars[arg]; bid > 0 {
			return bid
		}
		return esBGMs[arg]
	}

	if len(luaInfo.FuncLinks) > 0 && len(cutVars) > 0 {
		byCutscene = make(map[int][]int)
		seenPerCS := make(map[int]map[int]bool)
		for _, fn := range luaInfo.FuncLinks {
			var fnBGMs []int
			for _, bArg := range fn.BGMArgs {
				if bid := resolveArg(bArg); bid > 0 {
					fnBGMs = append(fnBGMs, bid)
				}
			}
			if len(fnBGMs) == 0 {
				continue
			}
			for _, cArg := range fn.CutsceneArgs {
				cid := cutVars[cArg]
				if cid <= 0 {
					continue
				}
				if seenPerCS[cid] == nil {
					seenPerCS[cid] = make(map[int]bool)
				}
				for _, bid := range fnBGMs {
					if !seenPerCS[cid][bid] {
						seenPerCS[cid][bid] = true
						byCutscene[cid] = append(byCutscene[cid], bid)
					}
				}
			}
		}
	}

	if len(luaInfo.BGMArgs) > 0 {
		seenAll := make(map[int]bool, len(luaInfo.BGMArgs))
		for _, bArg := range luaInfo.BGMArgs {
			if bid := resolveArg(bArg); bid > 0 && !seenAll[bid] {
				seenAll[bid] = true
				allBGMIDs = append(allBGMIDs, bid)
			}
		}
	}
	return byCutscene, allBGMIDs
}

// GetQuestCutscenes 按需从 SqPack 只读解析指定任务关联的所有过场动画 `.cutb` 及所用 BGM 列表（带内存缓存）。
func (s *Service) GetQuestCutscenes(questID int) []QuestCutsceneEntry {
	if s == nil || s.store == nil || questID <= 0 {
		return nil
	}

	s.questCutMu.Lock()
	if cached, ok := s.questCutCache[questID]; ok {
		out := cloneQuestCutsceneEntries(cached)
		s.questCutMu.Unlock()
		return out
	}
	s.questCutMu.Unlock()

	q, ok := s.store.QuestByID[questID]
	if !ok || len(q.Cutscenes) == 0 {
		return nil
	}

	bgmByID, bgmByFile := s.musicBGMLookupMaps()
	resolveBGMItem := func(bgmID, fadeMs int, source string) (QuestCutsceneBGM, bool) {
		if bgmID <= 0 {
			return QuestCutsceneBGM{}, false
		}
		b, ok := bgmByID[bgmID]
		if !ok || b.IsPlaceholder || isPlaceholderBGMPath(b.File) {
			return QuestCutsceneBGM{}, false
		}
		title := strings.TrimSpace(b.Title)
		if title == "" {
			title = b.FileStem
		}
		subTitle := ""
		if b.File != "" {
			if orch, hasOrch := s.store.OrchByFile[strings.ToLower(strings.TrimSpace(b.File))]; hasOrch && orch.Title != "" && orch.Title != title {
				subTitle = title
				title = orch.Title
			}
		}
		return QuestCutsceneBGM{
			BGMID:      b.ID,
			Title:      title,
			SubTitle:   subTitle,
			File:       b.File,
			FileExists: b.FileExists,
			FadeMs:     fadeMs,
			Source:     source,
		}, true
	}

	entries := make([]QuestCutsceneEntry, len(q.Cutscenes))
	anyMissingBGM := false
	for i, ref := range q.Cutscenes {
		entry := QuestCutsceneEntry{
			CutsceneID: ref.CutsceneID,
			Path:       ref.Path,
			Source:     ref.Source,
		}
		if s.store.PrimaryReader != nil && ref.Path != "" {
			rawCutb, err := s.store.PrimaryReader.ReadFile("cut/" + ref.Path + ".cutb")
			if err != nil || len(rawCutb) == 0 {
				entry.MissingCutb = true
			} else if tracks, ok := extractCutbBGMTracks(rawCutb, bgmByID, bgmByFile); ok {
				for _, tr := range tracks {
					if item, valid := resolveBGMItem(tr.bgmID, tr.fadeMs, "cutb"); valid {
						entry.BGMs = append(entry.BGMs, item)
					}
				}
			}
		}
		if len(entry.BGMs) == 0 {
			anyMissingBGM = true
		}
		entries[i] = entry
	}

	// 若存在未能从 `.cutb` 直接解出配乐的过场动画，则补充分析任务 `.luab` 脚本中的 PlayCutScene / PlayBGM 调用配对
	if anyMissingBGM && s.store.PrimaryReader != nil && q.InternalID != "" {
		if luaPath := BuildQuestScriptPath(q.InternalID); luaPath != "" {
			if luaRaw, err := s.store.PrimaryReader.ReadFile(luaPath); err == nil && len(luaRaw) > 0 {
				luaBGMsByCutscene, _ := resolveLuabBGMCalls(
					luaRaw,
					s.store.QuestCutVars[questID],
					s.store.QuestBGMVars[questID],
					s.store.EventSystemBGMs,
				)
				for i := range entries {
					if len(entries[i].BGMs) > 0 {
						continue
					}
					for _, bid := range luaBGMsByCutscene[entries[i].CutsceneID] {
						if item, valid := resolveBGMItem(bid, 0, "lua"); valid {
							entries[i].BGMs = append(entries[i].BGMs, item)
						}
					}
				}
			}
		}
	}

	s.questCutMu.Lock()
	if s.questCutCache == nil {
		s.questCutCache = make(map[int][]QuestCutsceneEntry, questCutsceneCacheCapacity)
	}
	if _, exists := s.questCutCache[questID]; !exists {
		if len(s.questCutOrder) >= questCutsceneCacheCapacity {
			oldest := s.questCutOrder[0]
			s.questCutOrder = s.questCutOrder[1:]
			delete(s.questCutCache, oldest)
		}
		s.questCutOrder = append(s.questCutOrder, questID)
	}
	s.questCutCache[questID] = cloneQuestCutsceneEntries(entries)
	s.questCutMu.Unlock()

	return entries
}

func runParallelIndexTasks(count int, fn func(idx int)) {
	if count <= 0 {
		return
	}
	ch := make(chan int, count)
	for i := range count {
		ch <- i
	}
	close(ch)
	workers := min(count, max(4, runtime.NumCPU()))
	done := make(chan struct{}, workers)
	for range workers {
		go func() {
			defer func() { done <- struct{}{} }()
			for idx := range ch {
				fn(idx)
			}
		}()
	}
	for range workers {
		<-done
	}
}

func (s *Service) buildMusicQuestIndex() {
	if s == nil || s.store == nil {
		return
	}
	bgmByID, bgmByFile := s.musicBGMLookupMaps()
	if len(bgmByID) == 0 {
		return
	}

	// 按游戏剧情/任务日志既定顺序收集全部任务
	questList := make([]QuestEntry, 0, len(s.store.Quests)+len(s.store.QuestByID))
	seenQuest := make(map[int]bool, len(s.store.Quests))
	for _, q := range s.store.Quests {
		if q.ID > 0 && !seenQuest[q.ID] {
			seenQuest[q.ID] = true
			questList = append(questList, q)
		}
	}
	if len(s.store.QuestByID) > len(questList) {
		extraIDs := make([]int, 0)
		for qid := range s.store.QuestByID {
			if qid > 0 && !seenQuest[qid] {
				extraIDs = append(extraIDs, qid)
			}
		}
		sort.Ints(extraIDs)
		for _, qid := range extraIDs {
			seenQuest[qid] = true
			questList = append(questList, s.store.QuestByID[qid])
		}
	}
	if len(questList) == 0 {
		return
	}

	questByID := make(map[int]QuestEntry, len(questList))
	questOrder := make(map[int]int, len(questList))
	for i, q := range questList {
		questByID[q.ID] = q
		questOrder[q.ID] = i
	}

	type questBGMRefAcc struct {
		cutscenes []string
		seenCS    map[string]bool
		viaScript bool
	}
	accByBGM := make(map[int]map[int]*questBGMRefAcc)
	addRef := func(bgmID, questID int, cutName string, viaScript bool) {
		if bgmID <= 0 || questID <= 0 {
			return
		}
		b, ok := bgmByID[bgmID]
		if !ok || b.IsPlaceholder || isPlaceholderBGMPath(b.File) {
			return
		}
		byQuest := accByBGM[bgmID]
		if byQuest == nil {
			byQuest = make(map[int]*questBGMRefAcc)
			accByBGM[bgmID] = byQuest
		}
		acc := byQuest[questID]
		if acc == nil {
			acc = &questBGMRefAcc{}
			byQuest[questID] = acc
		}
		if viaScript {
			acc.viaScript = true
		}
		if cutName != "" {
			if acc.seenCS == nil {
				acc.seenCS = make(map[string]bool)
			}
			if !acc.seenCS[cutName] {
				acc.seenCS[cutName] = true
				acc.cutscenes = append(acc.cutscenes, cutName)
			}
		}
	}

	// 1. 内存已有 QuestParams BGM 参数直接建索引
	for qid, vars := range s.store.QuestBGMVars {
		if _, ok := questByID[qid]; !ok {
			continue
		}
		for _, bgmID := range vars {
			addRef(bgmID, qid, "", true)
		}
	}

	// 2 & 3. 并发扫描去重 `.cutb` 过场文件与任务 `.luab` 脚本调用
	if s.store.PrimaryReader != nil {
		// 2. `.cutb` 过场时间线与影片音轨
		uniqueCutPaths := make(map[string]string) // lowerPath -> rawPath
		for _, q := range questList {
			for _, ref := range q.Cutscenes {
				p := strings.TrimSpace(ref.Path)
				if p != "" {
					k := strings.ToLower(p)
					if _, ok := uniqueCutPaths[k]; !ok {
						uniqueCutPaths[k] = p
					}
				}
			}
		}
		cutbBGMsByPath := make(map[string][]int, len(uniqueCutPaths))
		if len(uniqueCutPaths) > 0 {
			type cutbTask struct {
				key string
				raw string
			}
			tasks := make([]cutbTask, 0, len(uniqueCutPaths))
			for k, raw := range uniqueCutPaths {
				tasks = append(tasks, cutbTask{key: k, raw: raw})
			}
			results := make([][]int, len(tasks))
			runParallelIndexTasks(len(tasks), func(idx int) {
				rawCutb, err := s.store.PrimaryReader.ReadFile("cut/" + tasks[idx].raw + ".cutb")
				if err != nil || len(rawCutb) == 0 {
					return
				}
				if tracks, ok := extractCutbBGMTracks(rawCutb, bgmByID, bgmByFile); ok && len(tracks) > 0 {
					ids := make([]int, 0, len(tracks))
					for _, tr := range tracks {
						ids = append(ids, tr.bgmID)
					}
					results[idx] = ids
				}
			})
			for idx, ids := range results {
				if len(ids) > 0 {
					cutbBGMsByPath[tasks[idx].key] = ids
				}
			}
		}

		for _, q := range questList {
			for _, ref := range q.Cutscenes {
				p := strings.TrimSpace(ref.Path)
				if p == "" {
					continue
				}
				csName := ExtractCutsceneShortName(p)
				if csName == "" {
					csName = fmt.Sprintf("#%d", ref.CutsceneID)
				}
				for _, bid := range cutbBGMsByPath[strings.ToLower(p)] {
					addRef(bid, q.ID, csName, false)
				}
			}
		}

		// 3. 任务 `.luab` 脚本 BGM 调用（覆盖过场切歌配对及对话/事件内直接 PlayBGM）
		type luaTask struct {
			questIdx int
			path     string
		}
		var luaTasks []luaTask
		for i, q := range questList {
			if p := BuildQuestScriptPath(q.InternalID); p != "" {
				luaTasks = append(luaTasks, luaTask{questIdx: i, path: p})
			}
		}
		if len(luaTasks) > 0 {
			type luaResult struct {
				byCutscene map[int][]int
				allBGMIDs  []int
			}
			luaResults := make([]luaResult, len(luaTasks))
			runParallelIndexTasks(len(luaTasks), func(idx int) {
				t := luaTasks[idx]
				q := questList[t.questIdx]
				luaRaw, err := s.store.PrimaryReader.ReadFile(t.path)
				if err != nil || len(luaRaw) == 0 {
					return
				}
				byCS, allBGMs := resolveLuabBGMCalls(
					luaRaw,
					s.store.QuestCutVars[q.ID],
					s.store.QuestBGMVars[q.ID],
					s.store.EventSystemBGMs,
				)
				if len(byCS) > 0 || len(allBGMs) > 0 {
					luaResults[idx] = luaResult{
						byCutscene: byCS,
						allBGMIDs:  allBGMs,
					}
				}
			})

			for idx, res := range luaResults {
				if len(res.byCutscene) == 0 && len(res.allBGMIDs) == 0 {
					continue
				}
				q := questList[luaTasks[idx].questIdx]
				csNameByID := make(map[int]string, len(q.Cutscenes))
				for _, ref := range q.Cutscenes {
					n := ExtractCutsceneShortName(ref.Path)
					if n == "" {
						n = fmt.Sprintf("#%d", ref.CutsceneID)
					}
					csNameByID[ref.CutsceneID] = n
				}
				for cid, bids := range res.byCutscene {
					csName := csNameByID[cid]
					if csName == "" {
						csName = fmt.Sprintf("#%d", cid)
					}
					for _, bid := range bids {
						addRef(bid, q.ID, csName, true)
					}
				}
				for _, bid := range res.allBGMIDs {
					addRef(bid, q.ID, "", true)
				}
			}
		}
	} else {
		// 无客户端读取器（单测环境）时合并已有过场动画缓存
		s.questCutMu.Lock()
		for qid, cuts := range s.questCutCache {
			if _, ok := questByID[qid]; !ok {
				continue
			}
			for _, cs := range cuts {
				csName := ExtractCutsceneShortName(cs.Path)
				if csName == "" && cs.CutsceneID > 0 {
					csName = fmt.Sprintf("#%d", cs.CutsceneID)
				}
				for _, bgm := range cs.BGMs {
					addRef(bgm.BGMID, qid, csName, bgm.Source == "lua")
				}
			}
		}
		s.questCutMu.Unlock()
	}

	built := make(map[int][]MusicQuestRef, len(accByBGM))
	for bgmID, byQuest := range accByBGM {
		refs := make([]MusicQuestRef, 0, len(byQuest))
		for qid, acc := range byQuest {
			q := questByID[qid]
			refs = append(refs, MusicQuestRef{
				QuestID:       q.ID,
				NameCn:        q.NameCn,
				NameZh:        q.NameZh,
				NameEn:        q.NameEn,
				NameJa:        q.NameJa,
				Level:         q.Level,
				TypeIconID:    q.IconID,
				SectionName:   q.SectionName,
				GenreName:     q.GenreName,
				ExpansionName: q.ExpansionName,
				IsMSQ:         q.IsMSQ,
				IsFeature:     q.IsFeature,
				IsRepeatable:  q.IsRepeatable,
				CutsceneNames: acc.cutscenes,
				ViaScript:     acc.viaScript,
				orderIndex:    questOrder[qid],
			})
		}
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].orderIndex != refs[j].orderIndex {
				return refs[i].orderIndex < refs[j].orderIndex
			}
			return refs[i].QuestID < refs[j].QuestID
		})
		built[bgmID] = refs
	}
	s.questsByBGMID = built
}

// GetMusicQuests 返回引用指定音乐（BGM 或对应同一音源文件的管弦乐琴曲目）的任务列表。
func (s *Service) GetMusicQuests(kind string, id int) []MusicQuestRef {
	if s == nil || s.store == nil || id <= 0 {
		return nil
	}
	s.musicQuestOnce.Do(s.buildMusicQuestIndex)

	if kind == "" {
		kind = MusicKindBGM
	}
	var targetBGMIDs []int
	switch kind {
	case MusicKindBGM:
		targetBGMIDs = []int{id}
	case MusicKindOrchestrion:
		orch := s.GetMusicDetail(MusicKindOrchestrion, id)
		if orch == nil {
			return nil
		}
		targetFile := strings.TrimSpace(orch.File)
		if targetFile == "" || isPlaceholderBGMPath(targetFile) {
			return nil
		}
		bgmByID, _ := s.musicBGMLookupMaps()
		for bid, b := range bgmByID {
			if !b.IsPlaceholder && !isPlaceholderBGMPath(b.File) && strings.EqualFold(strings.TrimSpace(b.File), targetFile) {
				targetBGMIDs = append(targetBGMIDs, bid)
			}
		}
		sort.Ints(targetBGMIDs)
	default:
		return nil
	}
	if len(targetBGMIDs) == 0 {
		return nil
	}
	if len(targetBGMIDs) == 1 {
		return cloneMusicQuestRefs(s.questsByBGMID[targetBGMIDs[0]])
	}

	merged := make(map[int]MusicQuestRef)
	seenCS := make(map[int]map[string]bool)
	for _, bid := range targetBGMIDs {
		for _, ref := range s.questsByBGMID[bid] {
			cur, exists := merged[ref.QuestID]
			if !exists {
				cp := ref
				cp.CutsceneNames = append([]string(nil), ref.CutsceneNames...)
				merged[ref.QuestID] = cp
				if len(cp.CutsceneNames) > 0 {
					sMap := make(map[string]bool, len(cp.CutsceneNames))
					for _, cn := range cp.CutsceneNames {
						sMap[cn] = true
					}
					seenCS[ref.QuestID] = sMap
				}
				continue
			}
			cur.ViaScript = cur.ViaScript || ref.ViaScript
			if seenCS[ref.QuestID] == nil && len(ref.CutsceneNames) > 0 {
				seenCS[ref.QuestID] = make(map[string]bool, len(ref.CutsceneNames))
			}
			for _, cn := range ref.CutsceneNames {
				if !seenCS[ref.QuestID][cn] {
					seenCS[ref.QuestID][cn] = true
					cur.CutsceneNames = append(cur.CutsceneNames, cn)
				}
			}
			merged[ref.QuestID] = cur
		}
	}
	out := make([]MusicQuestRef, 0, len(merged))
	for _, ref := range merged {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].orderIndex != out[j].orderIndex {
			return out[i].orderIndex < out[j].orderIndex
		}
		return out[i].QuestID < out[j].QuestID
	})
	return out
}

func cloneMusicQuestRefs(src []MusicQuestRef) []MusicQuestRef {
	if len(src) == 0 {
		return nil
	}
	out := make([]MusicQuestRef, len(src))
	for i, r := range src {
		out[i] = r
		if len(r.CutsceneNames) > 0 {
			out[i].CutsceneNames = append([]string(nil), r.CutsceneNames...)
		}
	}
	return out
}

func cloneQuestCutsceneEntries(src []QuestCutsceneEntry) []QuestCutsceneEntry {
	if len(src) == 0 {
		return nil
	}
	out := make([]QuestCutsceneEntry, len(src))
	for i, cs := range src {
		out[i] = cs
		if len(cs.BGMs) > 0 {
			out[i].BGMs = append([]QuestCutsceneBGM(nil), cs.BGMs...)
		}
	}
	return out
}
