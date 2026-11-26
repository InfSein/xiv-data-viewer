package gamedata

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

var musicExpansionRe = regexp.MustCompile(`^music/ex(\d+)/`)

type musicLoaderInput struct {
	appLang                 i18n.Lang
	primaryReader           sqpack.FileReader
	hasFileReader           *sqpack.GameReader
	presetFileExists        map[string]bool
	schemasFS               fs.FS
	bgmSheet                *sqpack.SchemaSheet
	bgmSituations           *sqpack.SchemaSheet
	bgmSwitches             *sqpack.SchemaSheet
	bgmSystemDefines        *sqpack.SchemaSheet
	weddingBGMs             *sqpack.SchemaSheet
	weddingBGMsZh           *sqpack.SchemaSheet
	weddingBGMsEn           *sqpack.SchemaSheet
	weddingBGMsJa           *sqpack.SchemaSheet
	orchPrimary             *sqpack.SchemaSheet
	orchCn                  *sqpack.SchemaSheet
	orchEn                  *sqpack.SchemaSheet
	orchJa                  *sqpack.SchemaSheet
	orchPaths               *sqpack.SchemaSheet
	orchUiparams            *sqpack.SchemaSheet
	orchCategories          *sqpack.SchemaSheet
	exVersions              *sqpack.SchemaSheet
	territoryTypes          map[int]sqpack.Row
	placeNames              map[int]string
	instanceContents        *sqpack.SchemaSheet
	contentFinderConditions *sqpack.SchemaSheet
	mounts                  *sqpack.SchemaSheet
	fates                   *sqpack.SchemaSheet
	gfates                  *sqpack.SchemaSheet
	gfateTypes              *sqpack.SchemaSheet
	goldSaucerTexts         *sqpack.SchemaSheet
	leves                   *sqpack.SchemaSheet
	itemsPrimary            map[int]sqpack.Row
	itemSummaryByID         map[int]ItemSummary
}

type musicIndices struct {
	music          []MusicEntry
	bgms           []MusicEntry
	orchestrions   []MusicEntry
	musicByKey     map[string]MusicEntry
	bgmByID        map[int]MusicEntry
	orchByID       map[int]MusicEntry
	categories     []MusicCategoryInfo
	expansions     []MusicExpansionInfo
	totalBGM       int
	totalOrch      int
	totalMusic     int
	bgmItemOrchMap map[int]int
}

func resolveSchemaColumnIndexes(reader sqpack.FileReader, schemasFS fs.FS, sheetName string, fields ...string) map[string]int {
	if reader == nil || schemasFS == nil || len(fields) == 0 {
		return nil
	}
	lower := strings.ToLower(sheetName)
	exhBuf, err := reader.ReadFile("exd/" + lower + ".exh")
	if err != nil || len(exhBuf) == 0 {
		return nil
	}
	exh, err := sqpack.ReadEXHHeader(exhBuf)
	if err != nil {
		return nil
	}
	flatNames, err := sqpack.LoadSchemaFieldNames(schemasFS, sheetName)
	if err != nil {
		return nil
	}
	_, colIndex := sqpack.AlignEXHColumns(exh, flatNames)
	out := make(map[string]int, len(fields))
	for _, f := range fields {
		if idx, ok := colIndex[f]; ok {
			out[f] = idx
		} else {
			out[f] = -1
		}
	}
	return out
}

func musicKey(kind string, id int) string {
	return fmt.Sprintf("%s:%d", kind, id)
}

func extractFileStem(resourcePath string) string {
	base := path.Base(strings.ReplaceAll(strings.TrimSpace(resourcePath), "\\", "/"))
	if base == "" || base == "." || base == "/" {
		return ""
	}
	if dot := strings.LastIndexByte(base, '.'); dot > 0 {
		return base[:dot]
	}
	return base
}

func inferMusicExpansion(filePath string, exVersions *sqpack.SchemaSheet, lang i18n.Lang) (int, string) {
	lower := strings.ToLower(strings.TrimSpace(filePath))
	expID := 0
	if m := musicExpansionRe.FindStringSubmatch(lower); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil && v >= 0 {
			expID = v
		}
	}
	if exVersions != nil {
		if row, ok := exVersions.Rows[expID]; ok {
			if name := strings.TrimSpace(exVersions.Str(row, "Name")); name != "" {
				return expID, name
			}
		}
	}
	if expID == 0 {
		return 0, i18n.T(lang, "gamedata.music.exp_base")
	}
	return expID, fmt.Sprintf("EX%d", expID)
}

func isPlaceholderBGMPath(filePath string) bool {
	stem := strings.ToLower(extractFileStem(filePath))
	return stem == "" || strings.HasSuffix(stem, "_null") || stem == "bgm_null"
}

func buildMusicData(in musicLoaderInput) musicIndices {
	in.appLang = resolveAppLang(in.appLang)
	out := musicIndices{
		musicByKey:     make(map[string]MusicEntry),
		bgmByID:        make(map[int]MusicEntry),
		orchByID:       make(map[int]MusicEntry),
		bgmItemOrchMap: make(map[int]int),
	}

	// 1. 整理所有游戏引用到 BGM 的反向场景映射
	usagesByBGM := make(map[int][]MusicUsage)
	seenUsage := make(map[int]map[string]bool)
	addUsage := func(bgmID int, u MusicUsage) {
		if bgmID <= 0 || u.Label == "" {
			return
		}
		if seenUsage[bgmID] == nil {
			seenUsage[bgmID] = make(map[string]bool)
		}
		sig := fmt.Sprintf("%s|%s|%s|%d", u.Kind, u.Role, u.Label, u.RefID)
		if seenUsage[bgmID][sig] {
			return
		}
		seenUsage[bgmID][sig] = true
		usagesByBGM[bgmID] = append(usagesByBGM[bgmID], u)
	}

	resolveSituationOrBGM := func(bgmOrSitID int, kind, baseRole, label, subLabel string, iconID, refID int) {
		if bgmOrSitID <= 0 {
			return
		}
		if in.bgmSituations != nil {
			if sitRow, ok := in.bgmSituations.Rows[bgmOrSitID]; ok {
				roles := []struct {
					col string
					key string
				}{
					{"DaytimeID", "gamedata.music.role_day"},
					{"NightID", "gamedata.music.role_night"},
					{"BattleID", "gamedata.music.role_battle"},
					{"DaybreakID", "gamedata.music.role_daybreak"},
					{"TwilightID", "gamedata.music.role_twilight"},
				}
				for _, r := range roles {
					if bid := in.bgmSituations.Int(sitRow, r.col); bid > 0 {
						addUsage(bid, MusicUsage{
							Kind:     kind,
							Role:     i18n.T(in.appLang, r.key),
							Label:    label,
							SubLabel: subLabel,
							IconID:   iconID,
							RefID:    refID,
						})
					}
				}
				return
			}
		}
		addUsage(bgmOrSitID, MusicUsage{
			Kind:     kind,
			Role:     baseRole,
			Label:    label,
			SubLabel: subLabel,
			IconID:   iconID,
			RefID:    refID,
		})
	}

	// 1.1 TerritoryType -> PlaceName / BGM / BGMSituation
	if len(in.territoryTypes) > 0 {
		tCols := resolveSchemaColumnIndexes(in.primaryReader, in.schemasFS, "TerritoryType", "Name", "PlaceName", "PlaceNameZone", "BGM")
		colName := tCols["Name"]
		colPlace := tCols["PlaceName"]
		colZone := tCols["PlaceNameZone"]
		colBGM := tCols["BGM"]
		if colBGM >= 0 {
			for tid, row := range in.territoryTypes {
				bgmVal := row.IntAt(colBGM)
				if bgmVal <= 0 {
					continue
				}
				placeName := ""
				if colPlace >= 0 {
					placeName = in.placeNames[row.IntAt(colPlace)]
				}
				zoneName := ""
				if colZone >= 0 {
					zoneName = in.placeNames[row.IntAt(colZone)]
				}
				label := placeName
				if label == "" {
					label = zoneName
				}
				if label == "" && colName >= 0 {
					label = strings.TrimSpace(row.StringAt(colName))
				}
				if label == "" {
					continue
				}
				sub := ""
				if zoneName != "" && zoneName != label {
					sub = zoneName
				}
				resolveSituationOrBGM(bgmVal, "territory", i18n.T(in.appLang, "gamedata.music.role_zone"), label, sub, 0, tid)
			}
		}
	}

	// 1.2 InstanceContent -> ContentFinderCondition (BGM & WinBGM)
	if in.instanceContents != nil {
		for iid, row := range in.instanceContents.Rows {
			cfcID := in.instanceContents.Int(row, "ContentFinderCondition")
			name := ""
			iconID := 0
			if cfcID > 0 && in.contentFinderConditions != nil {
				if cr, ok := in.contentFinderConditions.Rows[cfcID]; ok {
					name = strings.TrimSpace(in.contentFinderConditions.Str(cr, "Name"))
					iconID = in.contentFinderConditions.Int(cr, "Icon")
				}
			}
			if name == "" {
				continue
			}
			if bid := in.instanceContents.Int(row, "BGM"); bid > 0 {
				addUsage(bid, MusicUsage{
					Kind:   "duty",
					Role:   i18n.T(in.appLang, "gamedata.music.role_duty"),
					Label:  name,
					IconID: iconID,
					RefID:  iid,
				})
			}
			if wbid := in.instanceContents.Int(row, "WinBGM"); wbid > 0 {
				addUsage(wbid, MusicUsage{
					Kind:   "duty",
					Role:   i18n.T(in.appLang, "gamedata.music.role_duty_win"),
					Label:  name,
					IconID: iconID,
					RefID:  iid,
				})
			}
		}
	}

	// 1.3 Mount -> RideBGM
	if in.mounts != nil {
		for mid, row := range in.mounts.Rows {
			bid := in.mounts.Int(row, "RideBGM")
			if bid <= 0 {
				continue
			}
			name := strings.TrimSpace(in.mounts.Str(row, "Singular"))
			if name == "" {
				continue
			}
			addUsage(bid, MusicUsage{
				Kind:   "mount",
				Role:   i18n.T(in.appLang, "gamedata.music.role_mount"),
				Label:  name,
				IconID: in.mounts.Int(row, "Icon"),
				RefID:  mid,
			})
		}
	}

	// 1.4 Fate -> Music
	if in.fates != nil {
		for fid, row := range in.fates.Rows {
			bid := in.fates.Int(row, "Music")
			if bid <= 0 {
				continue
			}
			name := strings.TrimSpace(in.fates.Str(row, "Name"))
			if name == "" {
				continue
			}
			addUsage(bid, MusicUsage{
				Kind:   "fate",
				Role:   i18n.T(in.appLang, "gamedata.music.role_fate"),
				Label:  name,
				IconID: in.fates.Int(row, "Icon"),
				RefID:  fid,
			})
		}
	}

	// 1.5 GFATE -> BGM
	if in.gfates != nil {
		for gid, row := range in.gfates.Rows {
			bid := in.gfates.Int(row, "BGM")
			if bid <= 0 {
				continue
			}
			typeID := in.gfates.Int(row, "Type")
			label := ""
			if in.gfateTypes != nil && typeID > 0 {
				if tr, ok := in.gfateTypes.Rows[typeID]; ok {
					textID := in.gfateTypes.Int(tr, "Name")
					if textID > 0 && in.goldSaucerTexts != nil {
						if gsr, ok2 := in.goldSaucerTexts.Rows[textID]; ok2 {
							label = strings.TrimSpace(in.goldSaucerTexts.Str(gsr, "Text"))
						}
					}
					if label == "" {
						label = strings.TrimSpace(in.gfateTypes.Str(tr, "Name"))
					}
				}
			}
			if label == "" {
				label = fmt.Sprintf("GATE #%d", gid)
			}
			addUsage(bid, MusicUsage{
				Kind:  "gfate",
				Role:  i18n.T(in.appLang, "gamedata.music.role_gfate"),
				Label: label,
				RefID: gid,
			})
		}
	}

	// 1.6 WeddingBGM -> Song & SongName
	weddingTitleByBGM := make(map[int][3]string) // primary, en, ja
	if in.weddingBGMs != nil {
		for wid, row := range in.weddingBGMs.Rows {
			bid := in.weddingBGMs.Int(row, "Song")
			name := strings.TrimSpace(in.weddingBGMs.Str(row, "SongName"))
			if bid <= 0 || name == "" {
				continue
			}
			var titles [3]string
			titles[0] = name
			if in.weddingBGMsEn != nil {
				if er, ok := in.weddingBGMsEn.Rows[wid]; ok {
					titles[1] = strings.TrimSpace(in.weddingBGMsEn.Str(er, "SongName"))
				}
			}
			if in.weddingBGMsJa != nil {
				if jr, ok := in.weddingBGMsJa.Rows[wid]; ok {
					titles[2] = strings.TrimSpace(in.weddingBGMsJa.Str(jr, "SongName"))
				}
			}
			weddingTitleByBGM[bid] = titles
			addUsage(bid, MusicUsage{
				Kind:  "wedding",
				Role:  i18n.T(in.appLang, "gamedata.music.role_wedding"),
				Label: name,
				RefID: wid,
			})
		}
	}

	// 1.7 Leve -> BGM
	if in.leves != nil {
		for lid, row := range in.leves.Rows {
			bid := in.leves.Int(row, "BGM")
			if bid <= 0 {
				continue
			}
			name := strings.TrimSpace(in.leves.Str(row, "Name"))
			if name == "" {
				continue
			}
			addUsage(bid, MusicUsage{
				Kind:  "leve",
				Role:  i18n.T(in.appLang, "gamedata.music.role_leve"),
				Label: name,
				RefID: lid,
			})
		}
	}

	// 1.8 BGMSwitch -> BGMSystemDefine
	if in.bgmSwitches != nil && in.bgmSystemDefines != nil {
		for sid, row := range in.bgmSwitches.Rows {
			bgmVal := in.bgmSwitches.Int(row, "BGM")
			sysID := in.bgmSwitches.Int(row, "BGMSystemDefine")
			if bgmVal <= 0 || sysID <= 0 {
				continue
			}
			if sr, ok := in.bgmSystemDefines.Rows[sysID]; ok {
				if def := strings.TrimSpace(in.bgmSystemDefines.Str(sr, "Define")); def != "" {
					resolveSituationOrBGM(bgmVal, "switch", i18n.T(in.appLang, "gamedata.music.role_system"), def, "", 0, sid)
				}
			}
		}
	}

	// 2. 构建 BGM 表条目
	expCount := make(map[int]int)
	expNames := make(map[int]string)

	// 收集需要检查存在性的文件路径并并发 HasFile 检查（实测 <20ms）
	uniquePaths := make(map[string]bool)
	if in.bgmSheet != nil {
		for _, row := range in.bgmSheet.Rows {
			if f := strings.TrimSpace(in.bgmSheet.Str(row, "File")); f != "" {
				uniquePaths[f] = true
			}
		}
	}
	if in.orchPaths != nil {
		for _, row := range in.orchPaths.Rows {
			if f := strings.TrimSpace(in.orchPaths.Str(row, "File")); f != "" {
				uniquePaths[f] = true
			}
		}
	}

	existsMap := make(map[string]bool, len(uniquePaths))
	hasExistsSource := false
	if in.presetFileExists != nil {
		hasExistsSource = true
		for p, ok := range in.presetFileExists {
			if ok {
				existsMap[p] = true
			}
		}
	} else if in.hasFileReader != nil && len(uniquePaths) > 0 {
		hasExistsSource = true
		paths := make([]string, 0, len(uniquePaths))
		for p := range uniquePaths {
			paths = append(paths, p)
		}
		var mu sync.Mutex
		ch := make(chan string, len(paths))
		for _, p := range paths {
			ch <- p
		}
		close(ch)
		workers := min(len(paths), max(4, runtime.NumCPU()))
		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				for p := range ch {
					ok, _ := in.hasFileReader.HasFile(p)
					if ok {
						mu.Lock()
						existsMap[p] = true
						mu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
	}

	usagePriority := func(kind string) int {
		switch kind {
		case "wedding":
			return 0
		case "duty":
			return 1
		case "territory":
			return 2
		case "mount":
			return 3
		case "fate":
			return 4
		case "gfate":
			return 5
		case "leve":
			return 6
		default:
			return 7
		}
	}

	if in.bgmSheet != nil {
		bgmIDs := make([]int, 0, len(in.bgmSheet.Rows))
		for id, row := range in.bgmSheet.Rows {
			if id <= 0 {
				continue
			}
			if f := strings.TrimSpace(in.bgmSheet.Str(row, "File")); f != "" {
				bgmIDs = append(bgmIDs, id)
			}
		}
		sort.Ints(bgmIDs)

		for _, id := range bgmIDs {
			row := in.bgmSheet.Rows[id]
			filePath := strings.TrimSpace(in.bgmSheet.Str(row, "File"))
			stem := extractFileStem(filePath)
			expID, expName := inferMusicExpansion(filePath, in.exVersions, in.appLang)
			expCount[expID]++
			expNames[expID] = expName

			usages := usagesByBGM[id]
			sort.SliceStable(usages, func(i, j int) bool {
				pi, pj := usagePriority(usages[i].Kind), usagePriority(usages[j].Kind)
				if pi != pj {
					return pi < pj
				}
				return usages[i].RefID < usages[j].RefID
			})

			title := stem
			nameZh, nameEn, nameJa := "", "", ""
			if wt, ok := weddingTitleByBGM[id]; ok && wt[0] != "" {
				title = wt[0]
				nameEn = wt[1]
				nameJa = wt[2]
				if in.appLang == i18n.LangZH {
					nameZh = wt[0]
				}
			}

			usageSummary := summarizeMusicUsages(in.appLang, usages)
			fileExists := true
			if hasExistsSource {
				fileExists = existsMap[filePath]
			}
			entry := MusicEntry{
				Key:                     musicKey(MusicKindBGM, id),
				Kind:                    MusicKindBGM,
				ID:                      id,
				Title:                   title,
				NameCn:                  title,
				NameZh:                  nameZh,
				NameEn:                  nameEn,
				NameJa:                  nameJa,
				File:                    filePath,
				FileStem:                stem,
				ExpansionID:             expID,
				ExpansionName:           expName,
				CategoryName:            i18n.T(in.appLang, "gamedata.music.kind_bgm"),
				Priority:                in.bgmSheet.Int(row, "Priority"),
				SpecialMode:             in.bgmSheet.Int(row, "SpecialMode"),
				DisableRestartResetTime: in.bgmSheet.Float(row, "DisableRestartResetTime"),
				DisableRestartTimeOut:   in.bgmSheet.Bool(row, "DisableRestartTimeOut"),
				DisableRestart:          in.bgmSheet.Bool(row, "DisableRestart"),
				PassEnd:                 in.bgmSheet.Bool(row, "PassEnd"),
				FileExists:              fileExists,
				IsPlaceholder:           isPlaceholderBGMPath(filePath),
				UsageSummary:            usageSummary,
				Usages:                  usages,
			}
			out.bgms = append(out.bgms, entry)
			out.bgmByID[id] = entry
			out.musicByKey[entry.Key] = entry
		}
	}

	// 3. 构建管弦乐琴乐谱表与物品关联
	if len(in.itemsPrimary) > 0 && in.schemasFS != nil {
		itemCols := resolveSchemaColumnIndexes(in.primaryReader, in.schemasFS, "Item", "FilterGroup", "AdditionalData")
		fgCol := itemCols["FilterGroup"]
		adCol := itemCols["AdditionalData"]
		if fgCol >= 0 && adCol >= 0 {
			for itemID, row := range in.itemsPrimary {
				if row.IntAt(fgCol) == 32 {
					if orchID := row.IntAt(adCol); orchID > 0 {
						if _, dup := out.bgmItemOrchMap[orchID]; !dup || itemID < out.bgmItemOrchMap[orchID] {
							out.bgmItemOrchMap[orchID] = itemID
						}
					}
				}
			}
		}
	}

	catDict := make(map[int]MusicCategoryInfo)
	if in.orchCategories != nil {
		for cid, crow := range in.orchCategories.Rows {
			if cid <= 1 {
				continue
			}
			cname := strings.TrimSpace(in.orchCategories.Str(crow, "Name"))
			if cname == "" {
				continue
			}
			catDict[cid] = MusicCategoryInfo{
				ID:     cid,
				Name:   cname,
				IconID: in.orchCategories.Int(crow, "Icon"),
				Order:  in.orchCategories.Int(crow, "Order"),
			}
		}
	}

	catCounts := make(map[int]int)
	if in.orchPrimary != nil {
		orchIDs := make([]int, 0, len(in.orchPrimary.Rows))
		for id, row := range in.orchPrimary.Rows {
			if id <= 0 {
				continue
			}
			if name := strings.TrimSpace(in.orchPrimary.Str(row, "Name")); name != "" {
				orchIDs = append(orchIDs, id)
			}
		}
		sort.Ints(orchIDs)

		for _, id := range orchIDs {
			row := in.orchPrimary.Rows[id]
			namePrimary := strings.TrimSpace(in.orchPrimary.Str(row, "Name"))
			descPrimary := strings.TrimSpace(in.orchPrimary.Str(row, "Description"))
			nameZh, nameEn, nameJa := "", "", ""
			if in.orchCn != nil {
				if r, ok := in.orchCn.Rows[id]; ok {
					nameZh = strings.TrimSpace(in.orchCn.Str(r, "Name"))
				}
			}
			var descEn, descJa string
			if in.orchEn != nil {
				if r, ok := in.orchEn.Rows[id]; ok {
					nameEn = strings.TrimSpace(in.orchEn.Str(r, "Name"))
					descEn = strings.TrimSpace(in.orchEn.Str(r, "Description"))
				}
			}
			if in.orchJa != nil {
				if r, ok := in.orchJa.Rows[id]; ok {
					nameJa = strings.TrimSpace(in.orchJa.Str(r, "Name"))
					descJa = strings.TrimSpace(in.orchJa.Str(r, "Description"))
				}
			}
			switch in.appLang {
			case i18n.LangZH:
				if nameZh == "" {
					nameZh = namePrimary
				}
			case i18n.LangEN:
				if nameEn == "" {
					nameEn = namePrimary
					descEn = descPrimary
				}
			case i18n.LangJA:
				if nameJa == "" {
					nameJa = namePrimary
					descJa = descPrimary
				}
			}

			filePath := ""
			if in.orchPaths != nil {
				if pr, ok := in.orchPaths.Rows[id]; ok {
					filePath = strings.TrimSpace(in.orchPaths.Str(pr, "File"))
				}
			}
			stem := extractFileStem(filePath)
			catID := 0
			order := 0
			if in.orchUiparams != nil {
				if ur, ok := in.orchUiparams.Rows[id]; ok {
					catID = in.orchUiparams.Int(ur, "OrchestrionCategory")
					order = in.orchUiparams.Int(ur, "Order")
				}
			}
			catInfo := catDict[catID]
			catName := catInfo.Name
			if catName == "" {
				catName = i18n.T(in.appLang, "gamedata.music.kind_orchestrion")
			} else {
				catCounts[catID]++
			}

			itemID := out.bgmItemOrchMap[id]
			var itemName string
			var itemIconID int
			if itemID > 0 {
				if sum, ok := in.itemSummaryByID[itemID]; ok {
					itemName = sum.NameCn
					itemIconID = sum.IconID
				}
			}

			fileExists := filePath != ""
			if filePath != "" && hasExistsSource {
				fileExists = existsMap[filePath]
			}
			expID, expName := inferMusicExpansion(filePath, in.exVersions, in.appLang)

			entry := MusicEntry{
				Key:            musicKey(MusicKindOrchestrion, id),
				Kind:           MusicKindOrchestrion,
				ID:             id,
				Title:          namePrimary,
				NameCn:         namePrimary,
				NameZh:         nameZh,
				NameEn:         nameEn,
				NameJa:         nameJa,
				Description:    descPrimary,
				DescriptionEn:  descEn,
				DescriptionJa:  descJa,
				File:           filePath,
				FileStem:       stem,
				ExpansionID:    expID,
				ExpansionName:  expName,
				CategoryID:     catID,
				CategoryName:   catName,
				CategoryIconID: catInfo.IconID,
				Order:          order,
				ItemID:         itemID,
				ItemName:       itemName,
				ItemIconID:     itemIconID,
				FileExists:     fileExists,
				UsageSummary:   catName,
			}
			out.orchestrions = append(out.orchestrions, entry)
			out.orchByID[id] = entry
			out.musicByKey[entry.Key] = entry
		}
	}

	out.music = make([]MusicEntry, 0, len(out.bgms)+len(out.orchestrions))
	out.music = append(out.music, out.bgms...)
	out.music = append(out.music, out.orchestrions...)
	out.totalBGM = len(out.bgms)
	out.totalOrch = len(out.orchestrions)
	out.totalMusic = len(out.music)

	for cid, ci := range catDict {
		if cnt := catCounts[cid]; cnt > 0 {
			ci.Count = cnt
			out.categories = append(out.categories, ci)
		}
	}
	sort.Slice(out.categories, func(i, j int) bool {
		if out.categories[i].Order != out.categories[j].Order {
			return out.categories[i].Order < out.categories[j].Order
		}
		return out.categories[i].ID < out.categories[j].ID
	})

	for eid, cnt := range expCount {
		if cnt > 0 {
			out.expansions = append(out.expansions, MusicExpansionInfo{
				ID:    eid,
				Name:  expNames[eid],
				Count: cnt,
			})
		}
	}
	sort.Slice(out.expansions, func(i, j int) bool {
		return out.expansions[i].ID < out.expansions[j].ID
	})

	return out
}

func summarizeMusicUsages(lang i18n.Lang, usages []MusicUsage) string {
	if len(usages) == 0 {
		return ""
	}
	var parts []string
	seen := make(map[string]bool)
	limit := 2
	for _, u := range usages {
		text := u.Label
		if u.Role != "" && u.Kind != "territory" {
			text = u.Role + " · " + u.Label
		} else if u.Role != "" && u.Kind == "territory" && u.Role != i18n.T(lang, "gamedata.music.role_zone") {
			text = fmt.Sprintf("%s (%s)", u.Label, u.Role)
		}
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		parts = append(parts, text)
		if len(parts) >= limit {
			break
		}
	}
	summary := strings.Join(parts, " · ")
	if len(usages) > len(parts) && summary != "" {
		summary += " " + i18n.Tf(lang, "gamedata.music.more_usages", "count", strconv.Itoa(len(usages)))
	}
	return summary
}

// ComputeBGMDigests 并发计算全量游戏 BGM 文件的内容 MD5 摘要与字节体积（支持内存缓存以消除重复读取）。
func (s *Store) ComputeBGMDigests(force bool) map[int]BGMDigest {
	if s == nil {
		return nil
	}
	s.bgmDigestMu.Lock()
	if !force && len(s.bgmDigests) > 0 {
		cp := make(map[int]BGMDigest, len(s.bgmDigests))
		for k, v := range s.bgmDigests {
			cp[k] = v
		}
		s.bgmDigestMu.Unlock()
		return cp
	}
	s.bgmDigestMu.Unlock()

	result := make(map[int]BGMDigest, len(s.BGMs))
	if s.PrimaryReader == nil {
		for _, b := range s.BGMs {
			result[b.ID] = BGMDigest{
				ID:         b.ID,
				File:       b.File,
				FileExists: b.FileExists,
				FileSize:   b.FileSize,
				ContentMD5: b.ContentMD5,
			}
		}
	} else if len(s.BGMs) > 0 {
		// 对同一虚拟路径去重计算摘要，避免多个 BGM 行引用同一文件时重复 I/O
		uniqueFiles := make(map[string]bool, len(s.BGMs))
		for _, b := range s.BGMs {
			if b.File != "" {
				uniqueFiles[b.File] = true
			}
		}
		type fileDigest struct {
			exists bool
			size   uint32
			md5    string
		}
		digestsByPath := make(map[string]fileDigest, len(uniqueFiles))
		var pathMu sync.Mutex

		ch := make(chan string, len(uniqueFiles))
		for f := range uniqueFiles {
			ch <- f
		}
		close(ch)

		workers := min(len(uniqueFiles), max(4, runtime.NumCPU()))
		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				for f := range ch {
					sz, md5Hex, ok, err := s.PrimaryReader.ReadFileDigest(f)
					if err == nil && ok {
						pathMu.Lock()
						digestsByPath[f] = fileDigest{exists: true, size: sz, md5: md5Hex}
						pathMu.Unlock()
					}
				}
			}()
		}
		wg.Wait()

		for _, b := range s.BGMs {
			fd := digestsByPath[b.File]
			result[b.ID] = BGMDigest{
				ID:         b.ID,
				File:       b.File,
				FileExists: fd.exists,
				FileSize:   fd.size,
				ContentMD5: fd.md5,
			}
		}
	}

	s.bgmDigestMu.Lock()
	s.bgmDigests = make(map[int]BGMDigest, len(result))
	for k, v := range result {
		s.bgmDigests[k] = v
	}
	s.bgmDigestMu.Unlock()
	return result
}
