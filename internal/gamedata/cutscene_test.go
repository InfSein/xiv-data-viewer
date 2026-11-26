package gamedata

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
)

func TestBuildQuestCutsceneLinksAndFilter(t *testing.T) {
	quests := makeSchemaSheet(map[int]map[string]any{
		68836: {
			"Name":                             "\ue0be 灵光卫",
			"Id":                               "LucKma401_03300",
			"QuestParams[0].ScriptInstruction": "CUTSCENE0",
			"QuestParams[0].ScriptArg":         1975,
			"QuestParams[1].ScriptInstruction": "ACTOR0",
			"QuestParams[1].ScriptArg":         1976,
			"QuestParams[2].ScriptInstruction": "LCUT_BGM1",
			"QuestParams[2].ScriptArg":         707,
		},
		66060: {
			"Name": "超越幻想",
			"Id":   "ManFst503_00524",
		},
	})
	cutscenes := makeSchemaSheet(map[int]map[string]any{
		1975: {"Path": "ex3/lucact/lucact01230/lucact01230"},
		1976: {"Path": "ex3/lucact/lucact01240/lucact01240"},
		9999: {"Path": "ffxiv/manfst/manfst50310/manfst50310"},
		8888: {"Path": "ffxiv/dunwil/fake_duty/fake_duty"},
	})
	cjs := makeSchemaSheet(map[int]map[string]any{
		// 匹配 68836 (65536 + 3300) 并去重合并 1975 + 1976
		3300: {
			"Name":        "灵光卫",
			"Unknown0":    3300,
			"Icon":        61419,
			"Cutscene[0]": 1975,
			"Cutscene[1]": 1976,
		},
		// 仅通过 CompleteJournal 关联 66060 (65536 + 524)
		560: {
			"Name":        "超越幻想",
			"Unknown0":    524,
			"Icon":        61419,
			"Cutscene[0]": 9999,
		},
		// 副本青光纪念录条目应被排除，不串台到任务
		7452: {
			"Name":        "超越幻想",
			"Unknown0":    524,
			"Icon":        completeJournalDutyIcon,
			"Cutscene[0]": 8888,
		},
	})
	bgms := makeSchemaSheet(map[int]map[string]any{
		707: {"File": "music/ex3/BGM_EX3_Event_15.scd"},
	})

	idx := buildQuestData(questLoaderInput{
		appLang:          i18n.LangZH,
		quests:           quests,
		cutscenes:        cutscenes,
		completeJournals: cjs,
		bgms:             bgms,
	})

	q1 := idx.questByID[68836]
	wantQ1 := []QuestCutsceneRef{
		{CutsceneID: 1975, Path: "ex3/lucact/lucact01230/lucact01230", Source: "script+journal"},
		{CutsceneID: 1976, Path: "ex3/lucact/lucact01240/lucact01240", Source: "journal"},
	}
	if !reflect.DeepEqual(q1.Cutscenes, wantQ1) {
		t.Fatalf("Quest 68836 Cutscenes = %+v, want %+v", q1.Cutscenes, wantQ1)
	}
	if idx.questBGMVars[68836]["LCUT_BGM1"] != 707 {
		t.Fatalf("questBGMVars[68836][LCUT_BGM1] = %d, want 707", idx.questBGMVars[68836]["LCUT_BGM1"])
	}

	q2 := idx.questByID[66060]
	wantQ2 := []QuestCutsceneRef{
		{CutsceneID: 9999, Path: "ffxiv/manfst/manfst50310/manfst50310", Source: "journal"},
	}
	if !reflect.DeepEqual(q2.Cutscenes, wantQ2) {
		t.Fatalf("Quest 66060 Cutscenes = %+v, want %+v (must ignore duty CompleteJournal row)", q2.Cutscenes, wantQ2)
	}

	svc := NewService(&Store{
		Quests:    idx.quests,
		QuestByID: idx.questByID,
	})
	res := svc.SearchQuests(QuestSearchQuery{
		SectionID:   -1,
		ExpansionID: -1,
		Filter:      "with_cutscene",
		Page:        1,
		PageSize:    30,
	})
	if res.Total != 2 {
		t.Fatalf("with_cutscene filter Total = %d, want 2", res.Total)
	}
	if got := SummarizeQuestCutscenes(q1.Cutscenes); got != "lucact01230, lucact01240" {
		t.Fatalf("SummarizeQuestCutscenes = %q, want %q", got, "lucact01230, lucact01240")
	}
}

func TestRealQuestCutscenesAndBGMResolution(t *testing.T) {
	cfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过实机任务过场动画解析测试: %v", err)
	}

	schemasFS := os.DirFS(filepath.Join("..", "..", "schemas"))
	store, err := LoadGameDataStoreWithSchemas(resolved, schemasFS)
	if err != nil {
		t.Fatalf("LoadGameDataStoreWithSchemas 失败: %v", err)
	}
	defer func() { _ = store.Close() }()

	svc := NewService(store)

	// 《熄灭最后的光》(69179) 关联多个 5.0 结局过场并包含 BGM_EX3_Event_16 (#708) 等配乐
	cuts := svc.GetQuestCutscenes(69179)
	if len(cuts) < 4 {
		t.Fatalf("任务 69179 过场动画数量异常少: %d", len(cuts))
	}
	has708 := false
	for _, cs := range cuts {
		for _, bgm := range cs.BGMs {
			if bgm.BGMID == 708 && bgm.File != "" && bgm.FileExists {
				has708 = true
			}
		}
	}
	if !has708 {
		t.Fatalf("任务 69179 过场动画未解析出 BGM #708: %+v", cuts)
	}

	// 《灵光卫》(68836) 同时验证 Script + Journal + .cutb + .luab 与缓存命中
	cuts68836 := svc.GetQuestCutscenes(68836)
	if len(cuts68836) != 4 {
		t.Fatalf("任务 68836 过场动画数量 = %d, want 4", len(cuts68836))
	}
	has707 := false
	for _, cs := range cuts68836 {
		for _, bgm := range cs.BGMs {
			if bgm.BGMID == 707 {
				has707 = true
			}
		}
	}
	if !has707 {
		t.Fatalf("任务 68836 未能解析出 BGM #707: %+v", cuts68836)
	}

	// 筛查含过场动画的任务总数（正常客户端 > 1400 条）
	withCut := svc.SearchQuests(QuestSearchQuery{
		SectionID:   -1,
		ExpansionID: -1,
		Filter:      "with_cutscene",
		Page:        1,
		PageSize:    10,
	})
	if withCut.Total < 1300 {
		t.Fatalf("含过场动画任务数量过低: %d", withCut.Total)
	}

	// 验证音乐被任务引用的反查接口（《熄灭最后的光》69179 -> BGM #708，《灵光卫》68836 -> BGM #707）
	qRefs708 := svc.GetMusicQuests(MusicKindBGM, 708)
	has69179 := false
	for _, r := range qRefs708 {
		if r.QuestID == 69179 && len(r.CutsceneNames) > 0 {
			has69179 = true
			break
		}
	}
	if !has69179 {
		t.Fatalf("BGM #708 应反向检索到《熄灭最后的光》(69179): %+v", qRefs708)
	}

	qRefs707 := svc.GetMusicQuests(MusicKindBGM, 707)
	has68836 := false
	for _, r := range qRefs707 {
		if r.QuestID == 68836 {
			has68836 = true
			break
		}
	}
	if !has68836 {
		t.Fatalf("BGM #707 应反向检索到《灵光卫》(68836): %+v", qRefs707)
	}
}

func TestGetMusicQuestsResolution(t *testing.T) {
	bgm707 := MusicEntry{
		Key:      musicKey(MusicKindBGM, 707),
		Kind:     MusicKindBGM,
		ID:       707,
		Title:    "BGM_EX3_Event_15",
		File:     "music/ex3/BGM_EX3_Event_15.scd",
		FileStem: "BGM_EX3_Event_15",
	}
	orch555 := MusicEntry{
		Key:      musicKey(MusicKindOrchestrion, 555),
		Kind:     MusicKindOrchestrion,
		ID:       555,
		Title:    "明日与明日",
		File:     "music/ex3/BGM_EX3_Event_15.scd",
		FileStem: "BGM_EX3_Event_15",
	}
	store := &Store{
		Quests: []QuestEntry{
			{ID: 68836, NameCn: "灵光卫", Level: 80, IsMSQ: true, SectionName: "主线任务"},
			{ID: 69179, NameCn: "熄灭最后的光", Level: 80, IsMSQ: true, SectionName: "主线任务"},
		},
		QuestByID: map[int]QuestEntry{
			68836: {ID: 68836, NameCn: "灵光卫", Level: 80, IsMSQ: true, SectionName: "主线任务"},
			69179: {ID: 69179, NameCn: "熄灭最后的光", Level: 80, IsMSQ: true, SectionName: "主线任务"},
		},
		QuestBGMVars: map[int]map[string]int{
			68836: {"LCUT_BGM1": 707},
		},
		BGMs:         []MusicEntry{bgm707},
		Orchestrions: []MusicEntry{orch555},
	}
	svc := NewService(store)
	svc.questCutCache = map[int][]QuestCutsceneEntry{
		69179: {
			{
				CutsceneID: 2050,
				Path:       "ex3/lucact/lucact05160/lucact05160",
				BGMs:       []QuestCutsceneBGM{{BGMID: 707, Source: "cutb"}},
			},
		},
	}

	bgmQuests := svc.GetMusicQuests(MusicKindBGM, 707)
	if len(bgmQuests) != 2 || bgmQuests[0].QuestID != 68836 || !bgmQuests[0].ViaScript ||
		bgmQuests[1].QuestID != 69179 || !reflect.DeepEqual(bgmQuests[1].CutsceneNames, []string{"lucact05160"}) {
		t.Fatalf("GetMusicQuests(bgm, 707) 结果异常: %+v", bgmQuests)
	}

	// 同音源文件的管弦乐琴乐谱应自动匹配对应的任务引用
	orchQuests := svc.GetMusicQuests(MusicKindOrchestrion, 555)
	if !reflect.DeepEqual(orchQuests, bgmQuests) {
		t.Fatalf("GetMusicQuests(orchestrion, 555) = %+v, want %+v", orchQuests, bgmQuests)
	}
}
