package gamedata

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

const questTextCacheCapacity = 128

// BuildQuestTextSheetPath 根据任务内部 ID 生成 SqPack 任务文本表虚拟路径（如 quest/053/FesNyx301_05377）。
func BuildQuestTextSheetPath(internalID string) string {
	id := strings.TrimSpace(internalID)
	if len(id) < 5 {
		return ""
	}
	tail := id[len(id)-5:]
	return fmt.Sprintf("quest/%s/%s", tail[:3], id)
}

// GetQuestText 按需从客户端 SqPack 解包指定任务的概要、目标步骤与对话文本（带只读缓存）。
func (s *Service) GetQuestText(questID int) *QuestTextData {
	if s == nil || s.store == nil || questID <= 0 {
		return nil
	}

	s.questTextMu.Lock()
	if cached, ok := s.questTextCache[questID]; ok {
		s.questTextMu.Unlock()
		return cached
	}
	s.questTextMu.Unlock()

	q, ok := s.store.QuestByID[questID]
	if !ok || s.store.PrimaryReader == nil {
		return nil
	}
	sheetPath := BuildQuestTextSheetPath(q.InternalID)
	if sheetPath == "" {
		return nil
	}

	lang := s.store.PrimaryLang
	if lang == sqpack.LangNone {
		lang = sqpack.LangChineseSimplified
		if !s.store.HasCN && s.store.HasGlobal {
			lang = sqpack.LangEnglish
		}
	}
	rows, err := sqpack.LoadSheetRows(s.store.PrimaryReader, sheetPath, lang, nil)
	if err != nil || len(rows) == 0 {
		return nil
	}

	actorMap := s.buildQuestSpeakerMap(q)
	parsed := ParseQuestTextRowsForLang(q.ID, q.InternalID, rows, actorMap, s.appLang(), lang, s.resolveSeSheetText)

	s.questTextMu.Lock()
	if s.questTextCache == nil {
		s.questTextCache = make(map[int]*QuestTextData, questTextCacheCapacity)
	}
	if _, exists := s.questTextCache[questID]; !exists {
		if len(s.questTextOrder) >= questTextCacheCapacity {
			oldest := s.questTextOrder[0]
			s.questTextOrder = s.questTextOrder[1:]
			delete(s.questTextCache, oldest)
		}
		s.questTextOrder = append(s.questTextOrder, questID)
	}
	s.questTextCache[questID] = parsed
	s.questTextMu.Unlock()

	return parsed
}

func (s *Service) resolveSeSheetText(sheetName string, rowID int, colIdx int) string {
	if s == nil || s.store == nil || rowID <= 0 {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(sheetName)) {
	case "item", "itemhq", "itemmp":
		if it, ok := s.store.ItemSummaryByID[rowID]; ok && it.NameCn != "" {
			return it.NameCn
		}
		if r, ok := s.store.ItemsPrimary[rowID]; ok {
			if name := r.StringAt(9); name != "" {
				return name
			}
		}
	case "eventitem":
		return s.store.EventItemNames[rowID]
	case "bnpcname":
		return s.store.BNpcNames[rowID]
	case "eobj", "eobjname":
		return s.store.EObjNames[rowID]
	case "classjob":
		return s.store.ClassJobs[rowID].name
	case "placename":
		return s.store.PlaceNames[rowID]
	}
	return ""
}

func (s *Service) ensureActorEnNames() map[int]string {
	s.actorEnOnce.Do(func() {
		if s.store == nil || s.store.GlobalReader == nil {
			return
		}
		names := make(map[int]string)
		if enpcs, err := sqpack.LoadSheetRows(s.store.GlobalReader, "ENpcResident", sqpack.LangEnglish, []int{0}); err == nil {
			for id, r := range enpcs {
				if n := normalizeSpeakerToken(r.StringAt(0)); n != "" {
					names[id] = n
				}
			}
		}
		if eobjs, err := sqpack.LoadSheetRows(s.store.GlobalReader, "EObjName", sqpack.LangEnglish, []int{0}); err == nil {
			for id, r := range eobjs {
				if _, exists := names[id]; !exists {
					if n := normalizeSpeakerToken(r.StringAt(0)); n != "" {
						names[id] = n
					}
				}
			}
		}
		s.actorEnNames = names
	})
	return s.actorEnNames
}

func (s *Service) buildQuestSpeakerMap(q QuestEntry) map[string]string {
	out := make(map[string]string)
	enNames := s.ensureActorEnNames()
	if len(enNames) == 0 {
		return out
	}

	resolveCn := func(aid int) string {
		if n, ok := s.store.NpcNames[aid]; ok && n.Name != "" {
			return n.Name
		}
		if n := s.store.EObjNames[aid]; n != "" {
			return n
		}
		return ""
	}

	var candidates []int
	if q.IssuerNpcID > 0 {
		candidates = append(candidates, q.IssuerNpcID)
	}
	if q.TargetNpcID > 0 {
		candidates = append(candidates, q.TargetNpcID)
	}
	candidates = append(candidates, s.store.QuestActorIDs[q.ID]...)

	for _, aid := range candidates {
		enNorm := enNames[aid]
		cnName := resolveCn(aid)
		if enNorm != "" && cnName != "" {
			if _, exists := out[enNorm]; !exists {
				out[enNorm] = cnName
			}
		}
	}
	return out
}

func normalizeSpeakerToken(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}

func extractInlineSpeaker(text string) (speaker, body string) {
	if !strings.HasPrefix(text, "(-") {
		return "", text
	}
	end := strings.Index(text, "-)")
	if end <= 2 || end > 60 {
		return "", text
	}
	sp := strings.TrimSpace(text[2:end])
	rest := strings.TrimSpace(text[end+2:])
	if sp == "" || rest == "" {
		return "", text
	}
	return sp, rest
}

func isDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func formatFallbackSpeaker(token, internalID string, appLang i18n.Lang) string {
	// 去除末尾与任务序号相同的 5 位数字后缀（如 CITIZENA05377 -> CITIZENA）
	if idx := strings.LastIndexByte(internalID, '_'); idx >= 0 && idx+1 < len(internalID) {
		questNum := internalID[idx+1:]
		if len(questNum) >= 4 && strings.HasSuffix(token, questNum) && len(token) > len(questNum) {
			token = token[:len(token)-len(questNum)]
		}
	}
	if len(token) > 5 && isDigitsOnly(token[len(token)-5:]) {
		token = token[:len(token)-5]
	}
	if token == "" {
		return "NPC"
	}
	// 将单个字母尾缀拆分出来（如 CITIZENA -> Citizen A）
	if len(token) >= 7 && strings.HasPrefix(token, "CITIZEN") {
		suffix := token[len("CITIZEN"):]
		if len(suffix) <= 2 {
			return i18n.Tf(appLang, "gamedata.quest_text.citizen", "suffix", suffix)
		}
	}
	if len(token) >= 10 && strings.HasPrefix(token, "ADVENTURER") {
		suffix := token[len("ADVENTURER"):]
		if len(suffix) <= 2 {
			return i18n.Tf(appLang, "gamedata.quest_text.adventurer", "suffix", suffix)
		}
	}
	lower := strings.ToLower(token)
	return strings.ToUpper(lower[:1]) + lower[1:]
}

func classifyQuestTextKey(rawKey, internalID string, appLang i18n.Lang) (cat, token string) {
	upperKey := strings.ToUpper(strings.TrimSpace(rawKey))
	upperID := strings.ToUpper(strings.TrimSpace(internalID))
	rest := upperKey
	if strings.HasPrefix(upperKey, "TEXT_"+upperID+"_") {
		rest = upperKey[len("TEXT_"+upperID+"_"):]
	} else if strings.HasPrefix(upperKey, "TEXT_"+upperID) {
		rest = strings.TrimPrefix(upperKey[len("TEXT_"+upperID):], "_")
	}
	seg := strings.SplitN(rest, "_", 2)[0]
	switch seg {
	case "SEQ":
		return "seq", seg
	case "TODO":
		return "todo", seg
	case "SYSTEM":
		return "system", i18n.T(appLang, "gamedata.quest_text.system")
	case "ACCESS":
		return "system", i18n.T(appLang, "gamedata.quest_text.access")
	case "ANNOUNCE":
		return "system", i18n.T(appLang, "gamedata.quest_text.announce")
	case "QIB":
		return "battle", i18n.T(appLang, "gamedata.quest_text.battle")
	}
	if len(seg) >= 1 && seg[0] == 'Q' && (len(seg) == 1 || isDigitsOnly(seg[1:])) {
		return "question", i18n.T(appLang, "gamedata.quest_text.question")
	}
	if len(seg) >= 1 && seg[0] == 'A' && (len(seg) == 1 || isDigitsOnly(seg[1:])) {
		return "answer", i18n.T(appLang, "gamedata.quest_text.answer")
	}
	return "dialogue", seg
}

// ParseQuestTextRows 将任务文本 EXD 行数据解析为结构化的概要、目的与对话列表。
func ParseQuestTextRows(questID int, internalID string, rows map[int]sqpack.Row, actorMap map[string]string, resolve sqpack.SeSheetResolver) *QuestTextData {
	return ParseQuestTextRowsForLang(questID, internalID, rows, actorMap, i18n.LangZH, sqpack.LangChineseSimplified, resolve)
}

// ParseQuestTextRowsForLang 按指定界面语言与数据语言解析任务文本 EXD 行数据。
func ParseQuestTextRowsForLang(questID int, internalID string, rows map[int]sqpack.Row, actorMap map[string]string, appLang i18n.Lang, dataLang sqpack.Language, resolve sqpack.SeSheetResolver) *QuestTextData {
	if len(rows) == 0 {
		return nil
	}
	if dataLang == sqpack.LangNone {
		switch i18n.NormalizeLang(string(appLang)) {
		case i18n.LangJA:
			dataLang = sqpack.LangJapanese
		case i18n.LangEN:
			dataLang = sqpack.LangEnglish
		default:
			dataLang = sqpack.LangChineseSimplified
		}
	}
	ids := make([]int, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	out := &QuestTextData{QuestID: questID}
	for _, rid := range ids {
		row := rows[rid]
		rawKey := row.StringAt(0)
		text := row.StringDisplayAtLang(1, dataLang, resolve)
		if text == "" {
			continue
		}

		cat, token := classifyQuestTextKey(rawKey, internalID, appLang)
		switch cat {
		case "seq":
			out.Journals = append(out.Journals, text)
		case "todo":
			out.Objectives = append(out.Objectives, text)
		default:
			kind := cat
			speaker := token
			if kind == "dialogue" {
				if inlineSp, cleaned := extractInlineSpeaker(text); inlineSp != "" {
					speaker = inlineSp
					text = cleaned
				} else if cn, ok := actorMap[token]; ok && cn != "" {
					speaker = cn
				} else {
					speaker = formatFallbackSpeaker(token, internalID, appLang)
				}
			}

			// 将同一角色连续的多句普通台词合并，提升长剧情的阅读连贯性
			canMerge := kind == "dialogue" || kind == "system" || kind == "battle"
			if canMerge && len(out.Dialogues) > 0 {
				last := &out.Dialogues[len(out.Dialogues)-1]
				if last.Kind == kind && last.Speaker == speaker {
					last.Text += "\n" + text
					continue
				}
			}
			out.Dialogues = append(out.Dialogues, QuestDialogueLine{
				Key:     rawKey,
				Kind:    kind,
				Speaker: speaker,
				Text:    text,
			})
		}
	}

	if len(out.Journals) == 0 && len(out.Objectives) == 0 && len(out.Dialogues) == 0 {
		return nil
	}
	return out
}
