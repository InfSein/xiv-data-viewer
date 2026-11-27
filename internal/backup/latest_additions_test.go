package backup

import (
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
)

func TestComputeLatestAdditionsSelectionAndFiltering(t *testing.T) {
	store757 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		ItemSummaries:  []gamedata.ItemSummary{{ID: 1001, NameCn: "旧剑"}},
		Quests:         []gamedata.QuestEntry{{ID: 2001, NameCn: "旧任务"}},
		Leves:          []gamedata.LeveEntry{{ID: 3001, NameCn: "旧理符"}},
		Achievements:   []gamedata.AchievementEntry{{ID: 4001, NameCn: "旧成就"}},
		BGMs:           []gamedata.MusicEntry{{ID: 5001, Kind: gamedata.MusicKindBGM, Title: "旧BGM"}},
		Orchestrions:   []gamedata.MusicEntry{{ID: 6001, Kind: gamedata.MusicKindOrchestrion, Title: "旧乐谱"}},
	}
	store758 := &gamedata.Store{
		PrimaryVersion: "2026.11.03.0000.0000",
		CNVersion:      "2026.11.03.0000.0000",
		HasCN:          true,
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 1001, NameCn: "旧剑"},
			{ID: 1002, NameCn: "7.58新剑"},
		},
		Quests: []gamedata.QuestEntry{
			{ID: 2001, NameCn: "旧任务"},
			{ID: 2002, NameCn: "7.58新任务"},
		},
		Leves: []gamedata.LeveEntry{
			{ID: 3001, NameCn: "旧理符"},
			{ID: 3002, NameCn: "7.58新理符"},
		},
		Achievements: []gamedata.AchievementEntry{
			{ID: 4001, NameCn: "旧成就"},
			{ID: 4002, NameCn: "7.58新成就"},
		},
		BGMs: []gamedata.MusicEntry{
			{ID: 5001, Kind: gamedata.MusicKindBGM, Title: "旧BGM"},
			{ID: 5002, Kind: gamedata.MusicKindBGM, Title: "7.58新BGM"},
		},
		Orchestrions: []gamedata.MusicEntry{
			{ID: 6001, Kind: gamedata.MusicKindOrchestrion, Title: "旧乐谱"},
			{ID: 6002, Kind: gamedata.MusicKindOrchestrion, Title: "7.58新乐谱"},
		},
		Icons: []gamedata.IconEntry{
			{ID: 100, Code: "000100"},
			{ID: 101, Code: "000101"},
		},
	}

	svc := NewService(store758, t.TempDir())
	defer svc.Close()

	// 1. 未下载任何快照时返回 nil
	if add, err := svc.ComputeLatestAdditions(snapshot.RegionCN); err != nil || add != nil {
		t.Fatalf("无本地快照时应返回 nil: add=%+v err=%v", add, err)
	}

	// 2. 仅下载了与当前客户端同版的 7.58 快照时，不应把 7.58 视为上一版本
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region:  snapshot.RegionCN,
		Patch:   "7.58",
		Version: "2026.11.03.0000.0000",
		Store:   store758,
	})
	if add, err := svc.ComputeLatestAdditions(snapshot.RegionCN); err != nil || add != nil {
		t.Fatalf("仅有当前版本快照时应返回 nil: add=%+v err=%v", add, err)
	}

	// 3. 再下载 7.57（含图标指纹）与 7.55：应跳过 7.58 选中 7.57 作为上一版本，并识别当前版本为 7.58
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region:  snapshot.RegionCN,
		Patch:   "7.55",
		Version: "2026.08.01.0000.0000",
		Store:   &gamedata.Store{},
	})
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region:  snapshot.RegionCN,
		Patch:   "7.57",
		Version: "2026.10.01.0000.0000",
		Store:   store757,
		Icons:   []snapshot.IconDigestRecord{{ID: 100, Digest: "d100"}},
	})

	add, err := svc.ComputeLatestAdditions(snapshot.RegionCN)
	if err != nil || add == nil {
		t.Fatalf("计算最新版本新增失败: add=%+v err=%v", add, err)
	}
	if add.PrevPatch != "7.57" || add.CurrentPatch != "7.58" {
		t.Fatalf("期望上一版本 7.57、当前版本 7.58，实际: prev=%q cur=%q", add.PrevPatch, add.CurrentPatch)
	}
	if !add.Items[1002] || add.Items[1001] || !add.Quests[2002] || !add.Leves[3002] || !add.Achievements[4002] || !add.BGMs[5002] || !add.Orchestrions[6002] {
		t.Fatalf("新增实体 ID 识别异常: %+v", add)
	}
	if !add.HasIconBaseline || !add.Icons[101] || add.Icons[100] {
		t.Fatalf("新增图标 ID 识别异常: %+v", add)
	}

	// 4. 模拟客户端已更新到目录未知的新版本（如 7.59，版本号未出现在目录/快照中）：应自动取快照最高版本 7.58 作为上一版本
	store759 := &gamedata.Store{
		PrimaryVersion: "2026.12.15.0000.0000",
		CNVersion:      "2026.12.15.0000.0000",
		HasCN:          true,
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 1001, NameCn: "旧剑"},
			{ID: 1002, NameCn: "7.58新剑"},
			{ID: 1003, NameCn: "7.59未知版新剑"},
		},
		Icons: []gamedata.IconEntry{{ID: 100}, {ID: 101}, {ID: 102}},
	}
	svc.SetLiveMemoryStore(snapshot.RegionCN, store759)
	add759, err := svc.ComputeLatestAdditions(snapshot.RegionCN)
	if err != nil || add759 == nil {
		t.Fatalf("未知新版本时计算新增失败: add=%+v err=%v", add759, err)
	}
	if add759.PrevPatch != "7.58" || add759.CurrentPatch != "" {
		t.Fatalf("未知当前版本时应回退取快照最高版本 7.58: prev=%q cur=%q", add759.PrevPatch, add759.CurrentPatch)
	}
	if !add759.Items[1003] || add759.Items[1002] {
		t.Fatalf("相较于 7.58 仅应将 1003 标为新增: %+v", add759.Items)
	}
	// 7.58 快照不含图标指纹（模拟仅 0a 快照），不得误将全部图标标为新增
	if add759.HasIconBaseline || len(add759.Icons) != 0 {
		t.Fatalf("上一版本为仅 0a 快照时不应误标新增图标: hasBase=%v icons=%v", add759.HasIconBaseline, add759.Icons)
	}

	// 5. 验证 gamedata.Service 配合 NewOnly 筛选生效
	gameSvc := gamedata.NewService(store758)
	gameSvc.SetLatestAdditions(add)
	if res := gameSvc.SearchItems(gamedata.ItemSearchQuery{NewOnly: true, Page: 1, PageSize: 20}); res.Total != 1 || res.Items[0].ID != 1002 {
		t.Fatalf("物品 NewOnly 筛选异常: %+v", res)
	}
	if res := gameSvc.SearchQuests(gamedata.QuestSearchQuery{SectionID: -1, ExpansionID: -1, NewOnly: true, Page: 1, PageSize: 20}); res.Total != 1 || res.Items[0].ID != 2002 {
		t.Fatalf("任务 NewOnly 筛选异常: %+v", res)
	}
	if res := gameSvc.SearchLeves(gamedata.LeveSearchQuery{NewOnly: true, Page: 1, PageSize: 20}); res.Total != 1 || res.Items[0].ID != 3002 {
		t.Fatalf("理符 NewOnly 筛选异常: %+v", res)
	}
	if res := gameSvc.SearchAchievements(gamedata.AchievementSearchQuery{NewOnly: true, Page: 1, PageSize: 20}); res.Total != 1 || res.Items[0].ID != 4002 {
		t.Fatalf("成就 NewOnly 筛选异常: %+v", res)
	}
	if res := gameSvc.SearchMusic(gamedata.MusicSearchQuery{ExpansionID: -1, NewOnly: true, Page: 1, PageSize: 20}); res.Total != 2 {
		t.Fatalf("音乐 NewOnly 筛选应命中 1 BGM + 1 乐谱，实际: %+v", res)
	}
	if res := gameSvc.SearchIcons(gamedata.IconSearchQuery{GroupID: -1, NewOnly: true}); res.Total != 1 || res.Items[0].ID != 101 {
		t.Fatalf("图标 NewOnly 筛选异常: %+v", res)
	}
}

func TestVersionDiffIconMissingDataAndAchievementMetadata(t *testing.T) {
	storeOld := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		Achievements: []gamedata.AchievementEntry{
			{ID: 1, NameCn: "旧成就", Points: 10},
		},
	}
	storeNew := &gamedata.Store{
		PrimaryVersion: "2026.11.03.0000.0000",
		Achievements: []gamedata.AchievementEntry{
			{ID: 1, NameCn: "旧成就", Points: 10},
			{
				ID:               2,
				NameCn:           "绝境征服者",
				NameEn:           "Ultimate Legend",
				DescriptionCn:    "通关高难副本",
				Points:           20,
				KindName:         "战斗",
				CategoryName:     "高难度任务",
				TitleID:          77,
				TitleMasculineCn: "绝境传奇",
				TitleNameEn:      "The Ultimate Legend",
				RewardItemID:     44099,
				RewardItemNameCn: "光辉武器箱",
				RewardItemIconID: 26000,
			},
		},
		Icons: []gamedata.IconEntry{{ID: 100, Code: "000100"}},
	}

	svc := NewService(storeNew, t.TempDir())
	defer svc.Close()

	// 注册仅 0a 的旧版本快照（无图标指纹）
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region:  snapshot.RegionCN,
		Patch:   "7.57",
		Version: "2026.10.01.0000.0000",
		Store:   storeOld,
	})

	// 1. 图标对比应标记 IconDiffAvailable=false 且 IconMissingFrom=true、IconMissingTo=false
	iconRes, err := svc.CompareVersions(VersionDiffQuery{
		From:       "7.57",
		To:         LiveVersionID,
		EntityType: "icon",
		Page:       1,
		PageSize:   30,
	})
	if err != nil {
		t.Fatalf("图标对比不应返回错误: %v", err)
	}
	if iconRes.Summary.IconDiffAvailable || !iconRes.Summary.IconMissingFrom || iconRes.Summary.IconMissingTo {
		t.Fatalf("仅 0a 旧版本快照的图标缺失标记异常: %+v", iconRes.Summary)
	}

	// 2. 成就对比应携带新增成就的完整称号、达成条件与奖励道具图标
	achRes, err := svc.CompareVersions(VersionDiffQuery{
		From:       "7.57",
		To:         LiveVersionID,
		EntityType: "achievement",
		Page:       1,
		PageSize:   30,
	})
	if err != nil || len(achRes.Items) != 1 {
		t.Fatalf("成就对比结果异常: items=%+v err=%v", achRes.Items, err)
	}
	got := achRes.Items[0]
	if got.DescriptionCn != "通关高难副本" || got.TitleMasculineCn != "绝境传奇" || got.TitleNameEn != "The Ultimate Legend" || got.RewardItemIconID != 26000 || got.AchKindName != "战斗" {
		t.Fatalf("新增成就完整展示元数据缺失: %+v", got)
	}
}
