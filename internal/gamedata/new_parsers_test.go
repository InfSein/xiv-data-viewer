package gamedata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

func makeSchemaSheet(rows map[int]map[string]any) *sqpack.SchemaSheet {
	cols := make(map[string]int)
	var fieldList []string
	for _, fields := range rows {
		for k := range fields {
			if _, ok := cols[k]; !ok {
				cols[k] = len(fieldList)
				fieldList = append(fieldList, k)
			}
		}
	}
	built := make(map[int]sqpack.Row, len(rows))
	for rowID, fields := range rows {
		r := make(sqpack.Row, len(fieldList))
		for k, v := range fields {
			r[cols[k]] = v
		}
		built[rowID] = r
	}
	return &sqpack.SchemaSheet{
		Cols: cols,
		Rows: built,
	}
}

func makeSchemaSubSheet(rows map[int][]map[string]any) *sqpack.SchemaSubrowSheet {
	cols := make(map[string]int)
	var fieldList []string
	for _, subs := range rows {
		for _, fields := range subs {
			for k := range fields {
				if _, ok := cols[k]; !ok {
					cols[k] = len(fieldList)
					fieldList = append(fieldList, k)
				}
			}
		}
	}
	built := make(map[int][]sqpack.SubRow, len(rows))
	for parentID, subs := range rows {
		subList := make([]sqpack.SubRow, 0, len(subs))
		for idx, fields := range subs {
			r := make(sqpack.Row, len(fieldList))
			for k, v := range fields {
				r[cols[k]] = v
			}
			subList = append(subList, sqpack.SubRow{SubRowID: uint16(idx), Data: r})
		}
		built[parentID] = subList
	}
	return &sqpack.SchemaSubrowSheet{
		Cols: cols,
		Rows: built,
	}
}

func TestQuestParserAndSearch(t *testing.T) {
	items := map[int]struct {
		name string
		icon int
	}{
		1001: {"冒险者长剑", 21001},
		1002: {"可选料理", 24001},
		1003: {"骑士专用胸甲", 22001},
		1004: {"部族代币", 65001},
		28:   {"亚拉戈诗学神典石", 65023},
	}

	idx := buildQuestData(questLoaderInput{
		quests: makeSchemaSheet(map[int]map[string]any{
			65536: {
				"Name":                       "初出茅庐的冒险者",
				"Id":                         "ClsPgl001_00001",
				"JournalGenre":               1,
				"EventIconType":              3,
				"Icon":                       100001,
				"IconSpecial":                80001,
				"Expansion":                  0,
				"PlaceName":                  100,
				"SortKey":                    10,
				"ClassJobLevel[0]":           10,
				"ExpFactor":                  150,
				"GilReward":                  500,
				"ItemRewardType":             1,
				"Reward[0]":                  1001,
				"ItemCountReward[0]":         1,
				"OptionalItemReward[0]":      1002,
				"OptionalItemCountReward[0]": 3,
				"OptionalItemIsHQReward[0]":  true,
				"IssuerStart":                1000001,
				"IssuerLocation":             9001,
				"TargetEnd":                  2000001,
				"TomestoneReward":            28,
				"TomestoneCountReward":       15,
				"InstanceContentUnlock":      501,
				"EmoteReward":                601,
			},
			65537: {
				"Name":               "职业转职试炼",
				"Id":                 "JobPld001_00002",
				"JournalGenre":       2,
				"EventIconType":      8,
				"Expansion":          1,
				"ClassJobLevel[0]":   30,
				"ItemRewardType":     6,
				"Reward[0]":          701,
				"PreviousQuest[0]":   65536,
				"PreviousQuestJoin":  0,
				"ClassJobUnlock":     19,
				"ActionReward":       801,
				"IsRepeatable":       true,
				"RepeatIntervalType": 1,
			},
			65538: {
				"Name":             "\ue0be 友好部族日常",
				"Id":               "Bea001_00003",
				"JournalGenre":     2,
				"EventIconType":    1,
				"ClassJobLevel[0]": 50,
				"ItemRewardType":   7,
				"Reward[0]":        901,
			},
			65539: {
				"Name":               "降神节可重复庆典",
				"Id":                 "Fes001_00004",
				"JournalGenre":       2,
				"EventIconType":      1,
				"ClassJobLevel[0]":   15,
				"IsRepeatable":       true,
				"RepeatIntervalType": 1,
			},
		}),
		questsEn: makeSchemaSheet(map[int]map[string]any{
			65536: {"Name": "A Fresh Beginning"},
		}),
		journalGenres: makeSchemaSheet(map[int]map[string]any{
			1: {"Name": "第七星历主线", "JournalCategory": 1},
			2: {"Name": "特职任务", "JournalCategory": 2},
		}),
		journalCategories: makeSchemaSheet(map[int]map[string]any{
			1: {"Name": "主线剧情", "JournalSection": 0},
			2: {"Name": "职业篇章", "JournalSection": 6},
		}),
		journalSections: makeSchemaSheet(map[int]map[string]any{
			0: {"Name": "主线任务"},
			6: {"Name": "职业任务"},
		}),
		questClassJobRewards: makeSchemaSubSheet(map[int][]map[string]any{
			701: {{
				"ClassJobCategory": 20,
				"RewardItem[0]":    1003,
				"RewardAmount[0]":  1,
			}},
		}),
		beastRankBonuses: makeSchemaSheet(map[int]map[string]any{
			901: {
				"Item":            1004,
				"ItemQuantity[0]": 2,
			},
		}),
		eventIconTypes: makeSchemaSheet(map[int]map[string]any{
			3: {"NpcIconAvailable": 71200},
			8: {"NpcIconAvailable": 71340},
		}),
		paramGrows: makeSchemaSheet(map[int]map[string]any{
			10: {"ScaledQuestXP": 1200},
		}),
		exVersions: makeSchemaSheet(map[int]map[string]any{
			0: {"Name": "重生之境"},
			1: {"Name": "苍穹之禁城"},
		}),
		instanceContents: makeSchemaSheet(map[int]map[string]any{
			501: {"ContentFinderCondition": 51},
		}),
		contentFinderConditions: makeSchemaSheet(map[int]map[string]any{
			51: {"Name": "天然要害沙斯塔夏溶洞"},
		}),
		emotes:  makeSchemaSheet(map[int]map[string]any{601: {"Name": "欢呼", "Icon": 24601}}),
		actions: makeSchemaSheet(map[int]map[string]any{801: {"Name": "战逃反应", "Icon": 2501}}),
		eobjNames: makeSchemaSheet(map[int]map[string]any{
			2000001: {"Singular": "闪亮的宝箱"},
		}),
		npcNames: map[int]RawNpcName{
			1000001: {Name: "巴德隆", Title: "沙之家引导员"},
		},
		levelLocations: map[int]RawNpcLocation{
			9001: {TerritoryID: 130, WorldX: 0, WorldZ: 0},
		},
		classJobs:          map[int]classJobDict{19: {name: "骑士", abbr: "PLD"}},
		classJobCategories: map[int]string{20: "骑士"},
		placeNames:         map[int]string{10: "萨纳兰", 20: "乌尔达哈现世回廊", 100: "沙之家"},
		territoryTypes: map[int]sqpack.Row{
			130: {0, 0, 0, 10, 20, 20, 1},
		},
		maps: map[int]sqpack.Row{
			1: {0, 0, 0, 0, 0, 0, "u1f1/00", 200, 0.0, 0.0},
		},
		itemMeta: func(id int) (string, int) {
			it := items[id]
			return it.name, it.icon
		},
	})

	if len(idx.quests) != 4 {
		t.Fatalf("期望解析 4 条任务，实际 %d", len(idx.quests))
	}
	q1 := idx.questByID[65536]
	if !q1.IsMSQ || q1.ExpReward != 1800 || q1.IconID != QuestIconMSQ || q1.BannerIconID != 100001 || q1.SpecialIconID != 80001 {
		t.Fatalf("主线任务解析异常: isMSQ=%v exp=%d icon=%d banner=%d special=%d", q1.IsMSQ, q1.ExpReward, q1.IconID, q1.BannerIconID, q1.SpecialIconID)
	}
	if q1.IssuerLocation == nil || q1.IssuerLocation.MapPath != "u1f1/00" || q1.TargetName != "闪亮的宝箱" {
		t.Fatalf("任务接取坐标或交互对象解析异常: loc=%+v target=%q", q1.IssuerLocation, q1.TargetName)
	}
	if len(q1.RewardItems) != 2 || len(q1.Unlocks) != 2 {
		t.Fatalf("任务奖励或解锁项解析异常: rewards=%+v unlocks=%+v", q1.RewardItems, q1.Unlocks)
	}

	q2 := idx.questByID[65537]
	if !q2.IsFeature || !q2.IsRepeatable || q2.RepeatIntervalLabel != "每日可重复" || q2.IconID != QuestIconFeature {
		t.Fatalf("功能/可重复任务解析异常: %+v", q2)
	}
	if len(q2.RewardItems) != 1 || q2.RewardItems[0].ClassJobCategory != "骑士" {
		t.Fatalf("QuestClassJobReward 奖励解析异常: %+v", q2.RewardItems)
	}
	q3 := idx.questByID[65538]
	if q3.IconID != QuestIconSide || !q3.HasLevelSync || CleanQuestTitle(q3.NameCn) != "友好部族日常" {
		t.Fatalf("普通任务图标或等级调整标记解析异常: %+v", q3)
	}
	q4 := idx.questByID[65539]
	if q4.IconID != QuestIconRepeatable {
		t.Fatalf("可重复任务图标映射异常: got=%d want=%d", q4.IconID, QuestIconRepeatable)
	}
	if len(idx.questsByRewardItem[1004]) != 1 {
		t.Fatalf("BeastRankBonus 道具反向索引异常: %+v", idx.questsByRewardItem[1004])
	}

	svc := NewService(&Store{
		Quests:           idx.quests,
		QuestByID:        idx.questByID,
		QuestSectionList: idx.sectionList,
	})
	msqRes := svc.SearchQuests(QuestSearchQuery{SectionID: -1, ExpansionID: -1, Filter: "msq"})
	if msqRes.Total != 1 || msqRes.Items[0].ID != 65536 {
		t.Fatalf("主线筛选异常: %+v", msqRes)
	}
	qRes := svc.SearchQuests(QuestSearchQuery{SectionID: -1, ExpansionID: -1, Q: "巴德隆"})
	if qRes.Total != 1 || qRes.Items[0].ID != 65536 {
		t.Fatalf("按接取 NPC 搜索任务失败: %+v", qRes)
	}
}

func TestQuestTextParserAndRealClient(t *testing.T) {
	if got := BuildQuestTextSheetPath("FesNyx301_05377"); got != "quest/053/FesNyx301_05377" {
		t.Fatalf("任务文本表路径构造异常: %q", got)
	}

	rows := map[int]sqpack.Row{
		0:  {"TEXT_FESNYX301_05377_SEQ_00", "※本任务是特定期间内的任务。"},
		1:  {"TEXT_FESNYX301_05377_SEQ_01", "前往沉溺海豚亭寻找梦幻。"},
		24: {"TEXT_FESNYX301_05377_TODO_00", "和梦幻对话"},
		48: {"TEXT_FESNYX301_05377_MUGEN_000_010", "莫非你是……"},
		49: {"TEXT_FESNYX301_05377_MUGEN_000_011", "我是梦幻。"},
		50: {"TEXT_FESNYX301_05377_Q1_000_000", "要说什么？"},
		51: {"TEXT_FESNYX301_05377_A1_000_000", "没问题"},
		52: {"TEXT_FESNYX301_05377_A1_000_001", "再考虑一下"},
		53: {"TEXT_FESNYX301_05377_YUMEMAWASHIBRO_000_100", "(-转梦兽的弟弟妹妹们-)梦梦梦！"},
		54: {"TEXT_FESNYX301_05377_SYSTEM_000_110", "降神节期间开放好运签占卜。"},
	}
	parsed := ParseQuestTextRows(70913, "FesNyx301_05377", rows, map[string]string{"MUGEN": "梦幻"}, nil)
	if parsed == nil || len(parsed.Journals) != 2 || len(parsed.Objectives) != 1 || len(parsed.Dialogues) != 6 {
		t.Fatalf("任务文本结构化解析异常: %+v", parsed)
	}
	if parsed.Dialogues[0].Speaker != "梦幻" || parsed.Dialogues[0].Text != "莫非你是……\n我是梦幻。" {
		t.Fatalf("说话人映射或连续台词合并异常: %+v", parsed.Dialogues[0])
	}
	if parsed.Dialogues[4].Speaker != "转梦兽的弟弟妹妹们" || parsed.Dialogues[4].Text != "梦梦梦！" {
		t.Fatalf("内联说话人提取异常: %+v", parsed.Dialogues[4])
	}
	if parsed.Dialogues[5].Kind != "system" || parsed.Dialogues[5].Speaker != "系统提示" {
		t.Fatalf("系统提示解析异常: %+v", parsed.Dialogues[5])
	}

	// 若本地 .env 配置了真实 FF14 客户端，验证羊佑织梦降神节等真实任务图标、详情大图与文本解析
	cfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过真实任务解析验证: %v", err)
	}
	store, err := LoadGameDataStoreWithSchemas(resolved, os.DirFS(filepath.Join("..", "..", "schemas")))
	if err != nil {
		t.Fatalf("加载真实游戏数据失败: %v", err)
	}
	defer store.Close()

	svc := NewService(store)
	if q := svc.GetQuestDetail(70913); q != nil {
		if q.IconID != QuestIconSide {
			t.Fatalf("期望普通任务图标 %d，实际 %d", QuestIconSide, q.IconID)
		}
		if q.BannerIconID <= 0 {
			t.Fatalf("期望任务 #70913 存在详情横幅图 BannerIconID")
		}
		qt := svc.GetQuestText(70913)
		if qt == nil || len(qt.Journals) == 0 || len(qt.Objectives) == 0 || len(qt.Dialogues) == 0 {
			t.Fatalf("任务 #70913 概要与对话文本解析不完整: %+v", qt)
		}
	}

	// 验证在「主线任务1」（SectionID=0）中，#69186 -> #69187..#69190（暗影之逆焰）-> #69209 按剧情顺序连续排列，不发生跨版本跳号
	sec0 := svc.filterQuests(QuestSearchQuery{SectionID: 0, ExpansionID: -1, Filter: "all"})
	pos := make(map[int]int)
	for i, qe := range sec0 {
		pos[qe.ID] = i
	}
	for _, id := range []int{69186, 69187, 69188, 69189, 69190, 69209} {
		if _, ok := pos[id]; !ok {
			t.Fatalf("Section 0 中缺少主线任务 #%d", id)
		}
	}
	if !(pos[69186] < pos[69187] && pos[69187] < pos[69188] && pos[69188] < pos[69189] && pos[69189] < pos[69190] && pos[69190]+1 == pos[69209]) {
		t.Fatalf("主线任务1 中 5.0 结局与 5.1 开头顺序异常: positions=%v", map[int]int{
			69186: pos[69186], 69187: pos[69187], 69188: pos[69188], 69189: pos[69189], 69190: pos[69190], 69209: pos[69209],
		})
	}
	if status := svc.SearchQuests(QuestSearchQuery{SectionID: 0, ExpansionID: -1, Filter: "all", Q: "暗影之逆焰"}); status.Total == 0 || status.Items[0].ID != 69190 {
		t.Fatalf("主线任务1 内搜索「暗影之逆焰」应将同名任务 #69190 排在首位: %+v", status.Items)
	}
}

func TestQuestJournalSortingAndSearchRelevance(t *testing.T) {
	idx := buildQuestData(questLoaderInput{
		quests: makeSchemaSheet(map[int]map[string]any{
			// 跨版本同等级 SortKey 重置：5.0 结局 (80级, SortKey=1060) 应紧邻 5.1 开头 (80级, SortKey=10) 之前
			69186: {"Name": "光辉不灭", "Id": "LucNia409_03650", "JournalGenre": 8, "EventIconType": 3, "ClassJobLevel[0]": 79, "SortKey": 1020},
			69190: {"Name": "暗影之逆焰", "Id": "LucNia503_03654", "JournalGenre": 8, "EventIconType": 3, "ClassJobLevel[0]": 80, "SortKey": 1060},
			69209: {"Name": "生命力的波动", "Id": "LucNia504_03673", "JournalGenre": 9, "EventIconType": 3, "ClassJobLevel[0]": 80, "SortKey": 10},
			// 同章节内低等级前置与高等级解锁：暗黑骑士 Lv.50 转职应排在 Lv.30 后续之前
			67590: {"Name": "暗黑涌动", "Id": "JobDrk301_02054", "JournalGenre": 186, "EventIconType": 8, "ClassJobLevel[0]": 30, "SortKey": 301},
			67589: {"Name": "来自深渊", "Id": "JobDrk300_02053", "JournalGenre": 186, "EventIconType": 8, "ClassJobLevel[0]": 50, "SortKey": 300},
			// 同章节内既有 SortKey>0 又有 SortKey=0 的条目，SortKey=0 放章末
			70121: {"Name": "幻影对战水晶冲突", "Id": "SubSea900_04585", "JournalGenre": 112, "EventIconType": 1, "ClassJobLevel[0]": 30, "SortKey": 0},
			65643: {"Name": "欢迎来到海洋之都", "Id": "SubSea001_00107", "JournalGenre": 112, "EventIconType": 1, "ClassJobLevel[0]": 1, "SortKey": 1},
			// 全章 SortKey=0 时回退按 Level -> ID 排序
			66755: {"Name": "孤独的复仇", "Id": "BanAma003_01219", "JournalGenre": 40, "EventIconType": 1, "ClassJobLevel[0]": 46, "SortKey": 0},
			66753: {"Name": "荒野之乱", "Id": "BanAma001_01217", "JournalGenre": 40, "EventIconType": 1, "ClassJobLevel[0]": 43, "SortKey": 0},
			// 未归入日志分类的任务（例如带有主线图标但 Genre=0 的历史任务）排在最后
			65732: {"Name": "追随前人的背影", "Id": "SubFst039_00196", "JournalGenre": 0, "EventIconType": 3, "ClassJobLevel[0]": 4, "SortKey": 21},
		}),
		journalGenres: makeSchemaSheet(map[int]map[string]any{
			8:   {"Name": "暗影之逆焰", "JournalCategory": 8},
			9:   {"Name": "拂晓回归", "JournalCategory": 9},
			40:  {"Name": "剧情任务", "JournalCategory": 36},
			112: {"Name": "支线任务：拉诺西亚", "JournalCategory": 59},
			186: {"Name": "暗黑骑士任务", "JournalCategory": 93},
		}),
		journalCategories: makeSchemaSheet(map[int]map[string]any{
			8:  {"Name": "暗影之逆焰主线任务", "JournalSection": 0},
			9:  {"Name": "拂晓回归主线任务", "JournalSection": 0},
			36: {"Name": "蜥蜴人族任务", "JournalSection": 4},
			59: {"Name": "拉诺西亚支线任务", "JournalSection": 3},
			93: {"Name": "战斗精英特职任务", "JournalSection": 6},
		}),
		journalSections: makeSchemaSheet(map[int]map[string]any{
			0: {"Name": "主线任务1"},
			3: {"Name": "支线任务"},
			4: {"Name": "友好部族任务1"},
			6: {"Name": "职业任务"},
		}),
	})

	wantOrder := []int{69186, 69190, 69209, 65643, 70121, 66753, 66755, 67589, 67590, 65732}
	if len(idx.quests) != len(wantOrder) {
		t.Fatalf("期望 %d 条任务，实际 %d", len(wantOrder), len(idx.quests))
	}
	for i, wantID := range wantOrder {
		if idx.quests[i].ID != wantID {
			gotIDs := make([]int, len(idx.quests))
			for k, q := range idx.quests {
				gotIDs[k] = q.ID
			}
			t.Fatalf("任务日志排序不符: got=%v want=%v", gotIDs, wantOrder)
		}
	}

	svc := NewService(&Store{
		Quests:           idx.quests,
		QuestByID:        idx.questByID,
		QuestSectionList: idx.sectionList,
	})
	sec0 := svc.SearchQuests(QuestSearchQuery{SectionID: 0, ExpansionID: -1, Filter: "all", PageSize: 30})
	if sec0.Total != 3 || sec0.Items[0].ID != 69186 || sec0.Items[1].ID != 69190 || sec0.Items[2].ID != 69209 {
		t.Fatalf("筛选 Section 0 时跨版本任务顺序异常: %+v", sec0.Items)
	}
	// 搜索「暗影之逆焰」时命中标题 #69190 与同 Genre 的 #69186，精确标题 #69190 应排在第一位
	qMatch := svc.SearchQuests(QuestSearchQuery{SectionID: 0, ExpansionID: -1, Filter: "all", Q: "暗影之逆焰", PageSize: 30})
	if qMatch.Total != 2 || qMatch.Items[0].ID != 69190 || qMatch.Items[1].ID != 69186 {
		t.Fatalf("任务搜索精确标题未置顶: %+v", qMatch.Items)
	}
}

func TestLeveParserAndSearch(t *testing.T) {
	itemSummaries := map[int]ItemSummary{
		5001: {ID: 5001, NameCn: "精炼铁锭", IconID: 20501},
		5002: {ID: 5002, NameCn: "火之碎晶", IconID: 20001},
	}

	idx := buildLeveData(leveLoaderInput{
		leves: makeSchemaSheet(map[int]map[string]any{
			101: {
				"Name":               "大量筹备：铁锭",
				"Description":        "需要大量锻造用的铁锭。",
				"ExpFactor":          1.5,
				"ExpReward":          12500,
				"GilReward":          420,
				"LeveRewardItem":     301,
				"JournalGenre":       10,
				"LevelLevemete":      9001,
				"LevelStart":         9002,
				"LeveClient":         401,
				"LeveAssignmentType": 5,
				"Town":               1,
				"ClassJobCategory":   33,
				"ClassJobLevel":      25,
				"TimeLimit":          1440,
				"AllowanceCost":      1,
				"DataId":             917504,
			},
			102: {
				"Name":               "讨伐巡逻：野狼",
				"LeveAssignmentType": 1,
				"ClassJobLevel":      15,
				"ExpReward":          3500,
				"GilReward":          180,
				"DataId":             65536,
			},
		}),
		craftLeves: makeSchemaSheet(map[int]map[string]any{
			917504: {
				"Item[0]":      5001,
				"ItemCount[0]": 3,
				"Repeats":      2,
			},
		}),
		battleLeves: makeSchemaSheet(map[int]map[string]any{
			65536: {
				"Rule":                           1,
				"Objectives[0]":                  1,
				"LeveData[0].BNpcName":           55,
				"LeveData[0].EnemyLevel":         16,
				"LeveData[0].NumOfAppearance[0]": 4,
				"LeveData[0].ItemsInvolved":      701,
				"LeveData[0].ItemsInvolvedQty":   5,
				"LeveData[0].ItemDropRate":       100,
			},
		}),
		battleLeveRules: makeSchemaSheet(map[int]map[string]any{1: {"Rule": "索敌讨伐"}}),
		leveStrings:     makeSchemaSheet(map[int]map[string]any{1: {"Objective": "击败指定范围内的敌人"}}),
		bnpcNames:       makeSchemaSheet(map[int]map[string]any{55: {"Singular": "森林狼"}}),
		eventItems:      makeSchemaSheet(map[int]map[string]any{701: {"Singular": "狼牙"}}),
		leveRewardItems: makeSchemaSheet(map[int]map[string]any{
			301: {
				"LeveRewardItemGroup[0]": 4001,
				"ProbabilityPercent[0]":  40,
			},
		}),
		leveRewardItemGroups: makeSchemaSheet(map[int]map[string]any{
			4001: {
				"Item[0]":  5002,
				"Count[0]": 12,
				"IsHQ[0]":  false,
			},
		}),
		leveAssignmentTypes: makeSchemaSheet(map[int]map[string]any{
			5: {"Name": "大量筹备", "Icon": 71011},
			1: {"Name": "诱导任务", "Icon": 71012},
		}),
		leveClients:        makeSchemaSheet(map[int]map[string]any{401: {"Name": "黑铁匠行会"}}),
		towns:              makeSchemaSheet(map[int]map[string]any{1: {"Name": "利姆萨·罗敏萨"}}),
		journalGenres:      makeSchemaSheet(map[int]map[string]any{10: {"Name": "锻铁匠理符"}}),
		classJobCategories: map[int]string{33: "锻铁匠"},
		itemSummaryByID:    itemSummaries,
	})

	if len(idx.leves) != 2 {
		t.Fatalf("期望解析 2 条理符，实际 %d", len(idx.leves))
	}
	craftLv := idx.leveByID[101]
	if craftLv.LeveKind != "craft" || craftLv.CraftRepeats != 2 || len(craftLv.TurnInItems) != 1 || craftLv.TurnInItems[0].Count != 3 {
		t.Fatalf("制作理符解析异常: %+v", craftLv)
	}
	if len(craftLv.RewardGroups) != 1 || craftLv.RewardGroups[0].ProbabilityPercent != 40 {
		t.Fatalf("理符奖励组解析异常: %+v", craftLv.RewardGroups)
	}
	battleLv := idx.leveByID[102]
	if battleLv.LeveKind != "battle" || len(battleLv.CombatTargets) != 1 || battleLv.CombatTargets[0].BNpcName != "森林狼" {
		t.Fatalf("佣兵理符目标解析异常: %+v", battleLv)
	}
	if len(idx.levesByItem[5001]) != 1 || !idx.levesByItem[5001][0].IsTurnIn || idx.levesByItem[5001][0].Count != 9 {
		t.Fatalf("理符交纳反向索引异常 (3*(1+2)=9): %+v", idx.levesByItem[5001])
	}

	svc := NewService(&Store{
		Leves:               idx.leves,
		LeveByID:            idx.leveByID,
		LeveAssignmentTypes: idx.assignmentTypeList,
	})
	res := svc.SearchLeves(LeveSearchQuery{Kind: "craft", Q: "精炼铁锭"})
	if res.Total != 1 || res.Items[0].ID != 101 {
		t.Fatalf("按委托交纳物品检索理符失败: %+v", res)
	}
}

func TestCollectablesParserAndItemIntegration(t *testing.T) {
	items := map[int]struct {
		name string
		icon int
	}{
		44099: {"收藏用图拉尔菠萝蛋糕", 24409},
		44100: {"稀少品图拉尔食材", 24410},
		33913: {"巧手紫票", 65095},
		41784: {"巧手橙票", 65115},
		28063: {"苍天街振兴票", 65073},
	}

	colIdx := buildCollectablesData(collectablesLoaderInput{
		shops: makeSchemaSheet(map[int]map[string]any{
			3866625: {
				"Name":         "收藏品交易",
				"Quest":        69000,
				"ShopItems[0]": 5,
				"RewardType":   1,
			},
		}),
		shopItems: makeSchemaSubSheet(map[int][]map[string]any{
			5: {{
				"Item":                        44099,
				"LevelMin":                    100,
				"LevelMax":                    100,
				"Stars":                       2,
				"CollectablesShopRefine":      10,
				"CollectablesShopRewardScrip": 20,
				"CollectablesShopItemGroup":   30,
			}},
		}),
		shopItemGroups: makeSchemaSheet(map[int]map[string]any{30: {"Name": "Lv.100"}}),
		shopRefines: makeSchemaSheet(map[int]map[string]any{
			10: {"LowCollectability": 650, "MidCollectability": 1000, "HighCollectability": 1350},
		}),
		shopRewardScrips: makeSchemaSheet(map[int]map[string]any{
			20: {"Currency": 2, "LowReward": 60, "MidReward": 100, "HighReward": 144, "ExpRatioLow": 120, "ExpRatioMid": 150, "ExpRatioHigh": 180},
		}),
		satisfactionNpcs: makeSchemaSheet(map[int]map[string]any{
			11: {
				"Npc":                                  1048605,
				"QuestRequired":                        70775,
				"LevelUnlock":                          90,
				"Level":                                9101,
				"SatisfactionNpcParams[0].SupplyIndex": 51,
			},
		}),
		satisfactionSupplies: makeSchemaSubSheet(map[int][]map[string]any{
			51: {{
				"Item":               44099,
				"CollectabilityLow":  300,
				"CollectabilityMid":  500,
				"CollectabilityHigh": 750,
				"Reward":             61,
				"IsBonus":            true,
			}},
		}),
		satisfactionRewards: makeSchemaSheet(map[int]map[string]any{
			61: {
				"SatisfactionSupplyRewardData[0].RewardCurrency": 6,
				"SatisfactionSupplyRewardData[0].QuantityLow":    100,
				"SatisfactionSupplyRewardData[0].QuantityMid":    140,
				"SatisfactionSupplyRewardData[0].QuantityHigh":   200,
				"SatisfactionLow":  60,
				"SatisfactionMid":  90,
				"SatisfactionHigh": 150,
				"GilLow":           1500,
				"GilMid":           2000,
				"GilHigh":          3000,
			},
		}),
		hwdSupplies: makeSchemaSheet(map[int]map[string]any{
			1: {
				"HWDCrafterSupplyParams[0].ItemTradeIn":           44100,
				"HWDCrafterSupplyParams[0].BaseCollectableRating": 100,
				"HWDCrafterSupplyParams[0].MidCollectableRating":  200,
				"HWDCrafterSupplyParams[0].HighCollectableRating": 300,
				"HWDCrafterSupplyParams[0].BaseCollectableReward": 81,
				"HWDCrafterSupplyParams[0].MidCollectableReward":  82,
				"HWDCrafterSupplyParams[0].HighCollectableReward": 83,
				"HWDCrafterSupplyParams[0].Level":                 80,
				"HWDCrafterSupplyParams[0].LevelMax":              80,
				"HWDCrafterSupplyParams[0].TermName":              91,
			},
		}),
		hwdRewards: makeSchemaSheet(map[int]map[string]any{
			81: {"ExpReward": 100000, "ScriptRewardAmount": 20, "Points": 50},
			82: {"ExpReward": 150000, "ScriptRewardAmount": 35, "Points": 80},
			83: {"ExpReward": 220000, "ScriptRewardAmount": 60, "Points": 120},
		}),
		hwdTerms: makeSchemaSheet(map[int]map[string]any{91: {"Name": "第四次重建"}}),
		quests: makeSchemaSheet(map[int]map[string]any{
			69000: {"Name": "收藏品的鉴赏法"},
			70775: {"Name": "新老主顾的委托"},
		}),
		collectablesShopToNPCs: map[int][]int{
			3866625: {1048001},
		},
		npcNames: map[int]RawNpcName{
			1048001: {Name: "收藏品交易员", Title: "工票交换"},
			1048605: {Name: "尼托维科", Title: "列车长"},
		},
		npcLocations: map[int]RawNpcLocation{
			1048001: {TerritoryID: 1185, WorldX: 10, WorldZ: -20},
		},
		levelLocations: map[int]RawNpcLocation{
			9101: {TerritoryID: 1185, WorldX: -30, WorldZ: 40},
		},
		placeNames:     map[int]string{1: "图拉尔", 2: "图莱尤拉"},
		territoryTypes: map[int]sqpack.Row{1185: {0, 0, 0, 1, 2, 2, 1}},
		maps:           map[int]sqpack.Row{1: {0, 0, 0, 0, 0, 0, "z6t1/00", 100, 0.0, 0.0}},
		itemMeta: func(id int) (string, int) {
			it := items[id]
			return it.name, it.icon
		},
	})

	if len(colIdx.turnInsByItem[44099]) != 2 {
		t.Fatalf("期望物品 #44099 有收藏品交易+老主顾 2 条记录，实际 %d", len(colIdx.turnInsByItem[44099]))
	}
	shopEntry := colIdx.turnInsByItem[44099][0]
	if shopEntry.SourceKind != "shop" || shopEntry.Tiers[2].RewardItemID != 33913 || shopEntry.Tiers[2].RewardAmount != 144 {
		t.Fatalf("收藏品商店条目解析异常: %+v", shopEntry)
	}
	if len(shopEntry.NPCs) != 1 || shopEntry.NPCs[0].MapPath != "z6t1/00" {
		t.Fatalf("收藏品 NPC 地图信息异常: %+v", shopEntry.NPCs)
	}
	satEntry := colIdx.turnInsByItem[44099][1]
	if satEntry.SourceKind != "satisfaction" || !satEntry.IsBonus || len(satEntry.NPCs) != 1 || satEntry.NPCs[0].MapPath != "z6t1/00" {
		t.Fatalf("老主顾条目解析异常: %+v", satEntry)
	}
	if len(colIdx.turnInsByItem[44100]) != 1 || colIdx.turnInsByItem[44100][0].Tiers[2].Points != 120 {
		t.Fatalf("苍天街供货条目解析异常: %+v", colIdx.turnInsByItem[44100])
	}
	if len(colIdx.rewardsByItem[33913]) != 1 || colIdx.rewardsByItem[33913][0].RewardHigh != 144 {
		t.Fatalf("巧手紫票反向来源异常: %+v", colIdx.rewardsByItem[33913])
	}

	// 验证集成至物品检索筛选与物品详情返回中
	row44099 := make(sqpack.Row, 95)
	row44099[9] = "收藏用图拉尔菠萝蛋糕"
	row33913 := make(sqpack.Row, 95)
	row33913[9] = "巧手紫票"

	svc := NewService(&Store{
		ItemsPrimary: map[int]sqpack.Row{44099: row44099, 33913: row33913},
		ItemSummaries: []ItemSummary{
			{ID: 33913, NameCn: "巧手紫票", IsCollectableReward: true},
			{ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", IsCollectableTurnIn: true},
		},
		ItemSummaryByID: map[int]ItemSummary{
			33913: {ID: 33913, NameCn: "巧手紫票", IsCollectableReward: true},
			44099: {ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", IsCollectableTurnIn: true},
		},
		CollectableTurnInsByItem: colIdx.turnInsByItem,
		CollectableRewardsByItem: colIdx.rewardsByItem,
		TotalCollectables:        colIdx.totalEntries,
	})

	searchRes := svc.SearchItems(ItemSearchQuery{Filter: "collectable_turnin"})
	if searchRes.Total != 1 || searchRes.Items[0].ID != 44099 {
		t.Fatalf("物品内收藏品筛选异常: %+v", searchRes)
	}
	detail44099 := svc.GetItemDetail(44099)
	if detail44099 == nil || len(detail44099.CollectableTurnIns) != 2 {
		t.Fatalf("物品详情缺少收藏品交纳信息: %+v", detail44099)
	}
	detail33913 := svc.GetItemDetail(33913)
	if detail33913 == nil || len(detail33913.CollectableRewards) != 1 {
		t.Fatalf("票据物品详情缺少收藏品获得反向索引: %+v", detail33913)
	}
}

func TestSearchGlobal(t *testing.T) {
	svc := NewService(&Store{
		ItemSummaries: []ItemSummary{
			{ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", NameEn: "Rarefied Turali Pineapple Ponzecake", NameJa: "収集用トラルパイナップルケーキ", CategoryName: "食品", ItemLevel: 690},
			{ID: 44098, NameCn: "菠萝", NameEn: "Pineapple", NameJa: "パイナップル", CategoryName: "食材", ItemLevel: 10},
			{ID: 44097, NameCn: "菠萝汁", NameEn: "Pineapple Juice", NameJa: "パインジュース", CategoryName: "食品", ItemLevel: 20},
			{ID: 1, NameCn: "金币", NameEn: "Gil", NameJa: "ギル", CategoryName: "货币", ItemLevel: 1},
		},
		Quests: []QuestEntry{
			{ID: 65536, NameCn: "\ue0be 启程之日", NameEn: "Coming to Gridania", NameJa: "旅立ちの日", GenreName: "第七星历主线", Level: 15},
		},
		Leves: []LeveEntry{
			{ID: 201, NameCn: "大量制作：甜点委托", NameEn: "Sweet Treat", NameJa: "大口依頼：お菓子", AssignmentTypeName: "大量筹备", ClassJobLevel: 98},
		},
		Achievements: []AchievementEntry{
			{
				ID:               10,
				NameCn:           "胜利的荣光1",
				NameEn:           "To Crush Your Enemies I",
				NameJa:           "勝利の栄光：ランク1",
				KindName:         "战斗",
				CategoryName:     "整体",
				Points:           5,
				TitleID:          1,
				TitleMasculineCn: "沙场勇将",
				TitleFeminineCn:  "沙场女杰",
				TitleNameEn:      "Battleborn",
			},
		},
	})

	// 1. 不存在关键词返回空结果
	if res := svc.SearchGlobal("不存在的关键词", 4); res.TotalMatched != 0 || len(res.Hits) != 0 {
		t.Fatalf("未匹配词不应返回结果: %+v", res)
	}

	// 2. 包含检索与相关性排序：完全一致 > 前缀匹配 > 包含匹配
	resPine := svc.SearchGlobal("菠萝", 4)
	if resPine.TotalMatched != 3 || len(resPine.Hits) != 3 {
		t.Fatalf("包含检索命中数异常: %+v", resPine)
	}
	if resPine.Hits[0].ID != 44098 || resPine.Hits[1].ID != 44097 || resPine.Hits[2].ID != 44099 {
		t.Fatalf("期望排序为 完全一致(44098) > 前缀(44097) > 包含(44099)，实际: %+v", resPine.Hits)
	}

	// 3. 英文不区分大小写 & 日文包含匹配
	if res := svc.SearchGlobal("gil", 4); res.TotalMatched != 1 || res.Hits[0].ID != 1 || res.Hits[0].MatchedAlias != "Gil" {
		t.Fatalf("英文不区分大小写匹配异常: %+v", res)
	}
	if res := svc.SearchGlobal("旅立ち", 4); res.TotalMatched != 1 || res.Hits[0].Kind != GlobalHitQuest || res.Hits[0].ID != 65536 {
		t.Fatalf("日文任务包含匹配异常: %+v", res)
	}

	// 4. 含 PUA 前缀的任务标题包含匹配（清理后匹配，不重复显示别名）
	if res := svc.SearchGlobal("启程", 4); res.TotalMatched != 1 || res.Hits[0].Kind != GlobalHitQuest || res.Hits[0].Title != "启程之日" || res.Hits[0].MatchedAlias != "" {
		t.Fatalf("含 PUA 前缀任务包含匹配异常: %+v", res)
	}

	// 5. 理符、成就及成就称号包含匹配
	if res := svc.SearchGlobal("甜点委托", 4); res.TotalMatched != 1 || res.Hits[0].Kind != GlobalHitLeve || res.Hits[0].ID != 201 {
		t.Fatalf("理符包含匹配异常: %+v", res)
	}
	if res := svc.SearchGlobal("battle", 4); res.TotalMatched != 1 || res.Hits[0].Kind != GlobalHitAchievement || res.Hits[0].ID != 10 || res.Hits[0].MatchedAlias != "Battleborn" {
		t.Fatalf("成就称号包含匹配异常: %+v", res)
	}

	// 6. 精确 ID（支持带 # 前缀）
	if res := svc.SearchGlobal("#44099", 50); res.TotalMatched != 1 || res.Hits[0].ID != 44099 {
		t.Fatalf("带 # 前缀 ID 匹配异常: %+v", res)
	}

	// 7. 超过 50 项命中时限制返回前 50 条并均衡保留不同模块，同时验证分页定位辅助方法
	manyItems := make([]ItemSummary, 0, 65)
	for i := 1; i <= 65; i++ {
		manyItems = append(manyItems, ItemSummary{ID: i, NameCn: "以太药剂", ItemLevel: 100 + i})
	}
	manyQuests := make([]QuestEntry, 0, 35)
	for i := 1; i <= 35; i++ {
		manyQuests = append(manyQuests, QuestEntry{ID: 65000 + i, NameCn: "以太调查", Level: i})
	}
	manyLeves := make([]LeveEntry, 0, 35)
	for i := 1; i <= 35; i++ {
		manyLeves = append(manyLeves, LeveEntry{ID: 200 + i, NameCn: "以太委托", ClassJobLevel: i})
	}
	manySvc := NewService(&Store{
		ItemSummaries: manyItems,
		Quests:        manyQuests,
		Leves:         manyLeves,
	})
	top50 := manySvc.SearchGlobal("以太", 50)
	if top50.TotalMatched != 135 || len(top50.Hits) != 50 {
		t.Fatalf("期望返回前 50 条命中且记录总数 135，实际 len=%d total=%d", len(top50.Hits), top50.TotalMatched)
	}
	hasQ, hasL := false, false
	for _, h := range top50.Hits {
		if h.Kind == GlobalHitQuest {
			hasQ = true
		}
		if h.Kind == GlobalHitLeve {
			hasL = true
		}
	}
	if !hasQ || !hasL {
		t.Fatalf("前 50 条全局搜索结果应保留任务与理符等模块条目: hasQ=%v hasL=%v", hasQ, hasL)
	}

	if pageRes, ok := manySvc.LocateItemPage(ItemSearchQuery{Filter: "all", Sort: "id_asc", PageSize: 30}, 55); !ok || pageRes.Page != 2 || len(pageRes.Items) != 30 {
		t.Fatalf("LocateItemPage 定位第 2 页失败: ok=%v page=%d len=%d", ok, pageRes.Page, len(pageRes.Items))
	}
	if pageRes, ok := manySvc.LocateQuestPage(QuestSearchQuery{SectionID: -1, ExpansionID: -1, Filter: "all", PageSize: 30}, 65033); !ok || pageRes.Page != 2 {
		t.Fatalf("LocateQuestPage 定位第 2 页失败: ok=%v page=%d", ok, pageRes.Page)
	}
	if pageRes, ok := manySvc.LocateLevePage(LeveSearchQuery{Kind: "all", PageSize: 30}, 234); !ok || pageRes.Page != 2 {
		t.Fatalf("LocateLevePage 定位第 2 页失败: ok=%v page=%d", ok, pageRes.Page)
	}
}
