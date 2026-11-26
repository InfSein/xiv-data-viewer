package backup

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

func TestVersionDiffQuestsLevesAndCollectables(t *testing.T) {
	backupDir := t.TempDir()

	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 44098, NameCn: "普通菠萝饼", ItemLevel: 680, CategoryName: "食品"},
			{ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", ItemLevel: 690, CategoryName: "食品", IsCollectableTurnIn: true},
			{ID: 33913, NameCn: "巧手紫票", ItemLevel: 1, CategoryName: "货币", IsCollectableReward: true},
		},
		CollectableTurnInsByItem: map[int][]gamedata.CollectableTurnInSource{
			44099: {{
				SourceLabel: "收藏品交易",
				LevelMin:    100,
				Tiers: []gamedata.CollectableTier{
					{Label: "低档", MinCollectability: 650, RewardItemName: "巧手紫票", RewardAmount: 50},
					{Label: "高档", MinCollectability: 1350, RewardItemName: "巧手紫票", RewardAmount: 120},
				},
			}},
		},
		Quests: []gamedata.QuestEntry{
			{ID: 65536, NameCn: "旧任务保留修改", Level: 90, GenreName: "主线任务", IssuerName: "敏菲利亚", GilReward: 500, ExpReward: 1000},
			{ID: 65537, NameCn: "即将移除任务", Level: 91, GenreName: "支线任务", GilReward: 300},
		},
		Leves: []gamedata.LeveEntry{
			{ID: 201, NameCn: "大量制作：旧甜点", LeveKind: "craft", LeveKindLabel: "制作理符", AssignmentTypeName: "大量筹备", ClassJobLevel: 95, ExpReward: 500000, GilReward: 1500},
		},
		TotalCollectables: 1,
	}

	svcV1 := NewService(storeV1, backupDir)
	if _, err := svcV1.InitializeOnStartup(); err != nil {
		t.Fatalf("v1 初始化快照失败: %v", err)
	}

	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.01.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 44098, NameCn: "普通菠萝饼", ItemLevel: 680, CategoryName: "食品", IsCollectableTurnIn: true},
			{ID: 44099, NameCn: "收藏用图拉尔菠萝蛋糕", ItemLevel: 690, CategoryName: "食品", IsCollectableTurnIn: true},
			{ID: 44100, NameCn: "新增收藏品食材", ItemLevel: 700, CategoryName: "食材", IsCollectableTurnIn: true},
			{ID: 33913, NameCn: "巧手紫票", ItemLevel: 1, CategoryName: "货币", IsCollectableReward: true},
		},
		CollectableTurnInsByItem: map[int][]gamedata.CollectableTurnInSource{
			44098: {{
				SourceLabel: "收藏品交易",
				LevelMin:    98,
				Tiers:       []gamedata.CollectableTier{{Label: "高档", MinCollectability: 1000, RewardItemName: "巧手紫票", RewardAmount: 90}},
			}},
			44099: {{
				SourceLabel: "收藏品交易",
				LevelMin:    100,
				Tiers: []gamedata.CollectableTier{
					{Label: "低档", MinCollectability: 650, RewardItemName: "巧手紫票", RewardAmount: 60},
					{Label: "高档", MinCollectability: 1350, RewardItemName: "巧手紫票", RewardAmount: 144},
				},
			}},
			44100: {{
				SourceLabel: "苍天街供货",
				LevelMin:    80,
				Tiers:       []gamedata.CollectableTier{{Label: "高档", MinCollectability: 300, RewardItemName: "苍天街振兴票", RewardAmount: 60}},
			}},
		},
		Quests: []gamedata.QuestEntry{
			{
				ID: 65536, NameCn: "旧任务保留修改", Level: 90, GenreName: "主线任务", IssuerName: "敏菲利亚",
				GilReward: 800, ExpReward: 1500,
				RewardItems: []gamedata.QuestRewardItem{{ItemID: 44099, ItemName: "收藏用图拉尔菠萝蛋糕", Count: 2}},
			},
			{ID: 65538, NameCn: "新增主线章节", Level: 100, GenreName: "主线任务", IssuerName: "阿尔菲诺", IsMSQ: true, GilReward: 1200, ExpReward: 50000},
		},
		Leves: []gamedata.LeveEntry{
			{
				ID: 201, NameCn: "大量制作：旧甜点", LeveKind: "craft", LeveKindLabel: "制作理符", AssignmentTypeName: "大量筹备",
				ClassJobLevel: 95, ExpReward: 650000, GilReward: 1800, CraftRepeats: 2,
				TurnInItems: []gamedata.LeveTurnInItem{{ItemID: 44099, ItemName: "收藏用图拉尔菠萝蛋糕", Count: 3}},
			},
			{
				ID: 202, NameCn: "新增采集理符", LeveKind: "gathering", LeveKindLabel: "采集理符", AssignmentTypeName: "索敌采集",
				ClassJobLevel: 98, ExpReward: 700000, GilReward: 2000,
			},
		},
		TotalCollectables: 3,
	}

	svcV2 := NewService(storeV2, backupDir)
	status, err := svcV2.InitializeOnStartup()
	if err != nil {
		t.Fatalf("v2 初始化快照失败: %v", err)
	}
	if !status.HasVersionUpdate || status.PreviousVersion != "2026.10.01.0000.0000" {
		t.Fatalf("期望检测到 v1 -> v2 更新: %+v", status)
	}

	diff, err := svcV2.CompareVersions(VersionDiffQuery{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("对比差异失败: %v", err)
	}
	if diff.Summary.AddedQuests != 1 || diff.Summary.ModifiedQuests != 1 || diff.Summary.RemovedQuests != 1 {
		t.Fatalf("任务 Diff 汇总异常: %+v", diff.Summary)
	}
	if diff.Summary.AddedLeves != 1 || diff.Summary.ModifiedLeves != 1 || diff.Summary.RemovedLeves != 0 {
		t.Fatalf("理符 Diff 汇总异常: %+v", diff.Summary)
	}
	if diff.Summary.NewCollectableTurnInItems != 2 || diff.Summary.ModifiedCollectableItems != 1 {
		t.Fatalf("收藏品 Diff 汇总异常: %+v", diff.Summary)
	}

	questOnly, err := svcV2.CompareVersions(VersionDiffQuery{EntityType: "quest", Page: 1, PageSize: 20})
	if err != nil || questOnly.TotalFiltered != 3 {
		t.Fatalf("按任务筛选 Diff 结果异常: total=%d err=%v", questOnly.TotalFiltered, err)
	}
	leveOnly, err := svcV2.CompareVersions(VersionDiffQuery{EntityType: "leve", Page: 1, PageSize: 20})
	if err != nil || leveOnly.TotalFiltered != 2 {
		t.Fatalf("按理符筛选 Diff 结果异常: total=%d err=%v", leveOnly.TotalFiltered, err)
	}
	colSearch, err := svcV2.CompareVersions(VersionDiffQuery{EntityType: "item", Q: "收藏品", Page: 1, PageSize: 20})
	if err != nil || colSearch.TotalFiltered != 3 {
		t.Fatalf("按收藏品关键词检索物品 Diff 异常: total=%d err=%v", colSearch.TotalFiltered, err)
	}
}

func TestVersionDiffIgnoresPriceMidChanges(t *testing.T) {
	backupDir := t.TempDir()

	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 50001, NameCn: "仅售价变化物品", ItemLevel: 100, CategoryName: "杂货", PriceMid: 100},
			{ID: 50002, NameCn: "品级与售价同变物品", ItemLevel: 100, CategoryName: "杂货", PriceMid: 100},
		},
	}
	if _, err := NewService(storeV1, backupDir).InitializeOnStartup(); err != nil {
		t.Fatalf("v1 初始化快照失败: %v", err)
	}

	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.01.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 50001, NameCn: "仅售价变化物品", ItemLevel: 100, CategoryName: "杂货", PriceMid: 250},
			{ID: 50002, NameCn: "品级与售价同变物品", ItemLevel: 110, CategoryName: "杂货", PriceMid: 250},
		},
	}
	svcV2 := NewService(storeV2, backupDir)
	if _, err := svcV2.InitializeOnStartup(); err != nil {
		t.Fatalf("v2 初始化快照失败: %v", err)
	}

	diff, err := svcV2.CompareVersions(VersionDiffQuery{EntityType: "item", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("对比差异失败: %v", err)
	}
	if diff.Summary.ModifiedItems != 1 || diff.TotalFiltered != 1 || len(diff.Items) != 1 {
		t.Fatalf("仅商店售价变动不应记为物品变更: summary=%+v items=%+v", diff.Summary, diff.Items)
	}
	entry := diff.Items[0]
	if entry.ID != 50002 || len(entry.Changes) != 1 || entry.Changes[0].Field != "itemLevel" {
		t.Fatalf("物品变更明细不应包含商店售价: %+v", entry)
	}
}

func TestVersionDiffLocalizedFieldLabels(t *testing.T) {
	backupDir := t.TempDir()

	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 44099, NameCn: "菠萝蛋糕", NameJa: "パイナップルケーキ", NameEn: "Pineapple Cake", ItemLevel: 680, CategoryName: "食品"},
		},
		Quests: []gamedata.QuestEntry{
			{ID: 65536, NameCn: "主线任务", NameJa: "メインクエスト", NameEn: "Main Quest", Level: 90, GenreName: "MSQ", GilReward: 500, ExpReward: 1000},
		},
	}
	if _, err := NewService(storeV1, backupDir).InitializeOnStartup(); err != nil {
		t.Fatalf("v1 初始化快照失败: %v", err)
	}

	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.01.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 44099, NameCn: "菠萝蛋糕", NameJa: "パイナップルケーキ", NameEn: "Pineapple Cake", ItemLevel: 690, CategoryName: "食品"},
		},
		Quests: []gamedata.QuestEntry{
			{ID: 65536, NameCn: "主线任务", NameJa: "メインクエスト", NameEn: "Main Quest", Level: 90, GenreName: "MSQ", GilReward: 800, ExpReward: 1000},
		},
	}
	svcV2 := NewService(storeV2, backupDir)
	if _, err := svcV2.InitializeOnStartup(); err != nil {
		t.Fatalf("v2 初始化快照失败: %v", err)
	}

	// 验证英语 Diff 标签与日文名保留
	svcV2.SetLang(i18n.LangEN)
	diffEN, err := svcV2.CompareVersions(VersionDiffQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("英语 Diff 对比失败: %v", err)
	}
	if len(diffEN.Items) != 2 {
		t.Fatalf("期望物品与任务各一条变更，实际 %d", len(diffEN.Items))
	}
	wantItemLabelEN := i18n.T(i18n.LangEN, "backup.field_ilvl")
	wantQuestGilEN := i18n.T(i18n.LangEN, "backup.quest_field_gil")
	var foundItemEN, foundQuestEN bool
	for _, it := range diffEN.Items {
		if it.EntityType == "item" && it.ID == 44099 {
			if it.NameJa != "パイナップルケーキ" {
				t.Fatalf("Diff 条目未保留日文名称: %+v", it)
			}
			for _, ch := range it.Changes {
				if ch.Label == wantItemLabelEN {
					foundItemEN = true
				}
			}
		}
		if it.EntityType == "quest" && it.ID == 65536 {
			for _, ch := range it.Changes {
				if ch.Label == wantQuestGilEN {
					foundQuestEN = true
				}
			}
		}
	}
	if !foundItemEN || !foundQuestEN {
		t.Fatalf("英语 Diff 字段标签不符合预期: itemEN=%v questEN=%v items=%+v", foundItemEN, foundQuestEN, diffEN.Items)
	}

	// 验证日语 Diff 标签
	svcV2.SetLang(i18n.LangJA)
	diffJA, err := svcV2.CompareVersions(VersionDiffQuery{EntityType: "item", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("日语 Diff 对比失败: %v", err)
	}
	wantItemLabelJA := i18n.T(i18n.LangJA, "backup.field_ilvl")
	if len(diffJA.Items) != 1 || len(diffJA.Items[0].Changes) == 0 || !strings.EqualFold(diffJA.Items[0].Changes[0].Label, wantItemLabelJA) {
		t.Fatalf("日语 Diff 字段标签不符合预期: %+v", diffJA.Items)
	}
}

func TestVersionBackupAndDiff(t *testing.T) {
	cfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过版本快照对比测试: %v", err)
	}

	store, err := gamedata.LoadGameDataStoreWithSchemas(resolved, os.DirFS(filepath.Join("..", "..", "schemas")))
	if err != nil {
		t.Fatalf("加载数据失败: %v", err)
	}
	defer store.Close()

	svc := NewService(store, t.TempDir())
	status, err := svc.InitializeOnStartup()
	if err != nil {
		t.Fatalf("初始化快照失败: %v", err)
	}
	if len(status.Backups) < 2 {
		t.Fatalf("期望至少包含当前版与历史基准快照，实际 %d 个", len(status.Backups))
	}

	diff, err := svc.CompareVersions(VersionDiffQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("对比版本差异失败: %v", err)
	}
	if diff.Summary.AddedItems == 0 || diff.Summary.AddedQuests == 0 || diff.Summary.AddedLeves == 0 || diff.Summary.AddedBGMs == 0 || diff.TotalFiltered == 0 {
		t.Fatalf("版本差异结果缺少任务/理符/道具/音乐变更: %+v", diff.Summary)
	}
}

func TestVersionDiffBGM(t *testing.T) {
	backupDir := t.TempDir()
	oldMD5 := "d41d8cd98f00b204e9800998ecf8427e"
	newMD5 := "9e107d9d372bb6826bd81d3542a419d6"
	sameMD5 := "0cc175b9c0f1b6a831c399e269772661"

	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		BGMs: []gamedata.MusicEntry{
			{ID: 1, Title: "BGM_Unchanged", File: "music/ffxiv/BGM_Unchanged.scd", FileExists: true, FileSize: 1024, ContentMD5: sameMD5},
			{ID: 2, Title: "BGM_Modified_Track", File: "music/ex5/BGM_Modified_Track.scd", FileExists: true, FileSize: 2048000, ContentMD5: oldMD5, ExpansionName: "金曦之遗辉"},
			{ID: 3, Title: "BGM_Removed_Track", File: "music/ffxiv/BGM_Removed_Track.scd", FileExists: true, FileSize: 512000, ContentMD5: sameMD5},
		},
	}
	if _, err := NewService(storeV1, backupDir).InitializeOnStartup(); err != nil {
		t.Fatalf("v1 初始化失败: %v", err)
	}

	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.01.0000.0000",
		BGMs: []gamedata.MusicEntry{
			{ID: 1, Title: "BGM_Unchanged", File: "music/ffxiv/BGM_Unchanged.scd", FileExists: true, FileSize: 1024, ContentMD5: sameMD5},
			{ID: 2, Title: "BGM_Modified_Track", File: "music/ex5/BGM_Modified_Track.scd", FileExists: true, FileSize: 2150400, ContentMD5: newMD5, ExpansionName: "金曦之遗辉"},
			{ID: 4, Title: "BGM_Added_Track", File: "music/ex5/BGM_Added_Track.scd", FileExists: true, FileSize: 3145728, ContentMD5: newMD5, ExpansionName: "金曦之遗辉", UsageSummary: "新副本战斗"},
		},
	}
	svcV2 := NewService(storeV2, backupDir)
	if _, err := svcV2.InitializeOnStartup(); err != nil {
		t.Fatalf("v2 初始化失败: %v", err)
	}

	diff, err := svcV2.CompareVersions(VersionDiffQuery{EntityType: "bgm", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("对比 BGM 差异失败: %v", err)
	}
	if diff.Summary.AddedBGMs != 1 || diff.Summary.ModifiedBGMs != 1 || diff.Summary.RemovedBGMs != 1 || diff.TotalFiltered != 3 {
		t.Fatalf("BGM Diff 统计异常: summary=%+v total=%d", diff.Summary, diff.TotalFiltered)
	}

	wantChangedText := i18n.T(i18n.LangZH, "backup.bgm_content_changed")
	var foundAdded, foundMod bool
	for _, entry := range diff.Items {
		for _, ch := range entry.Changes {
			if strings.Contains(ch.Label, oldMD5) || strings.Contains(ch.Before, oldMD5) || strings.Contains(ch.After, oldMD5) ||
				strings.Contains(ch.Label, newMD5) || strings.Contains(ch.Before, newMD5) || strings.Contains(ch.After, newMD5) {
				t.Fatalf("Diff 展示结果不得包含新旧 MD5 明文: %+v", ch)
			}
		}
		if entry.ID == 4 && entry.ChangeType == "added" {
			foundAdded = true
		}
		if entry.ID == 2 && entry.ChangeType == "modified" {
			if len(entry.Changes) == 0 || entry.Changes[0].After != wantChangedText {
				t.Fatalf("已有 BGM 修改应提示有变化而非暴露 MD5: %+v", entry.Changes)
			}
			foundMod = true
		}
	}
	if !foundAdded || !foundMod {
		t.Fatalf("未完整捕获新增与内容变化的 BGM 条目: %+v", diff.Items)
	}

	// 验证缺少 BGM 记录的历史快照兼容性（不把老快照全量 BGM 误报为新增）
	noBGMSnap := &snapshotPayload{
		Meta:  VersionBackupMeta{Version: "2026.01.01.0000.0000", ItemCount: 1},
		Items: []compactItemRecord{{ID: 1, NameCn: "旧物品"}},
	}
	if err := svcV2.writeSnapshot("2026.01.01.0000.0000", noBGMSnap); err != nil {
		t.Fatalf("写入无 BGM 历史快照失败: %v", err)
	}
	compatDiff, err := svcV2.CompareVersions(VersionDiffQuery{From: "2026.01.01.0000.0000", EntityType: "bgm", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("兼容旧快照对比失败: %v", err)
	}
	if compatDiff.Summary.AddedBGMs != 0 || compatDiff.TotalFiltered != 0 {
		t.Fatalf("缺少 BGM 的历史快照不应误报全量新增: summary=%+v", compatDiff.Summary)
	}
}

func TestQuestCutsceneDiffAndCompatibility(t *testing.T) {
	backupDir := filepath.Join(t.TempDir(), "backups")

	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		Quests: []gamedata.QuestEntry{
			{
				ID:     68836,
				NameCn: "灵光卫",
				Level:  71,
				Cutscenes: []gamedata.QuestCutsceneRef{
					{CutsceneID: 1975, Path: "ex3/lucact/lucact01230/lucact01230", Source: "script+journal"},
				},
			},
		},
	}
	svcV1 := NewService(storeV1, backupDir)
	if _, err := svcV1.InitializeOnStartup(); err != nil {
		t.Fatalf("v1 初始化失败: %v", err)
	}

	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.01.0000.0000",
		Quests: []gamedata.QuestEntry{
			{
				ID:     68836,
				NameCn: "灵光卫",
				Level:  71,
				Cutscenes: []gamedata.QuestCutsceneRef{
					{CutsceneID: 1975, Path: "ex3/lucact/lucact01230/lucact01230", Source: "script+journal"},
					{CutsceneID: 1976, Path: "ex3/lucact/lucact01240/lucact01240", Source: "journal"},
				},
			},
		},
	}
	svcV2 := NewService(storeV2, backupDir)
	if _, err := svcV2.InitializeOnStartup(); err != nil {
		t.Fatalf("v2 初始化失败: %v", err)
	}

	diff, err := svcV2.CompareVersions(VersionDiffQuery{EntityType: "quest", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("对比任务过场差异失败: %v", err)
	}
	if diff.Summary.ModifiedQuests != 1 || diff.TotalFiltered != 1 {
		t.Fatalf("期望 1 条任务修改，实际 summary=%+v total=%d", diff.Summary, diff.TotalFiltered)
	}
	wantLabel := i18n.T(i18n.LangZH, "backup.quest_field_cutscene")
	foundCutDiff := false
	for _, ch := range diff.Items[0].Changes {
		if ch.Field == "cutscenes" && ch.Label == wantLabel &&
			strings.Contains(ch.Before, "lucact01230") && strings.Contains(ch.After, "lucact01240") {
			foundCutDiff = true
		}
	}
	if !foundCutDiff {
		t.Fatalf("任务 Diff 缺少过场动画变更明细: %+v", diff.Items[0].Changes)
	}

	// 验证缺少 CutsceneCount 字段的历史快照平滑兼容，不误报任务变更
	legacySnap := &snapshotPayload{
		Meta: VersionBackupMeta{Version: "2026.01.01.0000.0000", QuestCount: 1},
		Quests: []compactQuestRecord{
			{ID: 68836, NameCn: "灵光卫", Level: 71},
		},
	}
	if err := svcV2.writeSnapshot("2026.01.01.0000.0000", legacySnap); err != nil {
		t.Fatalf("写入历史任务快照失败: %v", err)
	}
	compatDiff, err := svcV2.CompareVersions(VersionDiffQuery{From: "2026.01.01.0000.0000", EntityType: "quest", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("兼容旧任务快照对比失败: %v", err)
	}
	if compatDiff.Summary.ModifiedQuests != 0 {
		t.Fatalf("缺少 CutsceneCount 的历史快照不应误报任务修改: %+v", compatDiff.Items)
	}
}

func makeTinyTestPNG(r, g, b byte) []byte {
	const texHdrSz = 80
	// 构造最小合法 BGRA8 .tex 并转换为持久化载荷验证 Round-Trip
	hdr := make([]byte, texHdrSz+4)
	hdr[4] = byte(sqpack.TexFormatB8G8R8A8 & 0xff)
	hdr[5] = byte((sqpack.TexFormatB8G8R8A8 >> 8) & 0xff)
	hdr[8] = 1
	hdr[10] = 1
	hdr[12] = 1
	hdr[14] = 1
	hdr[28] = texHdrSz
	hdr[len(hdr)-4] = b
	hdr[len(hdr)-3] = g
	hdr[len(hdr)-2] = r
	hdr[len(hdr)-1] = 255
	payload, err := sqpack.BuildIconBlobPayload(hdr)
	if err != nil {
		return nil
	}
	return payload
}

func TestVersionDiffIconsPartitionAndCompatibility(t *testing.T) {
	backupDir := filepath.Join(t.TempDir(), "backups")
	svc := NewService(&gamedata.Store{PrimaryVersion: "2026.11.01.0000.0000"}, backupDir)

	oldPNG := makeTinyTestPNG(200, 40, 40)
	newPNG := makeTinyTestPNG(40, 200, 80)

	snapV1 := &snapshotPayload{
		Meta: VersionBackupMeta{Version: "2026.10.01.0000.0000", IconCount: 3},
		Items: []compactItemRecord{
			{ID: 100, NameCn: "未改动道具"},
		},
		Icons: []compactIconRecord{
			{ID: 37618, Digest: "v1_37618", Variants: "default:std|default:hr1", ThumbPNG: oldPNG, HasHR1: true},
			{ID: 37619, Digest: "v1_37619", Variants: "default:std", ThumbPNG: oldPNG},
			{ID: 99999, Digest: "v1_99999", Variants: "default:std", ThumbPNG: oldPNG},
		},
	}
	snapV2 := &snapshotPayload{
		Meta: VersionBackupMeta{Version: "2026.11.01.0000.0000", IconCount: 3},
		Items: []compactItemRecord{
			{ID: 100, NameCn: "未改动道具"},
			{ID: 200, NameCn: "新增对比道具"},
		},
		Icons: []compactIconRecord{
			{ID: 37618, Digest: "v2_37618", Variants: "default:std|default:hr1", ThumbPNG: newPNG, HasHR1: true},
			{ID: 37619, Digest: "v1_37619", Variants: "default:std", ThumbPNG: oldPNG},
			{ID: 65001, Digest: "v2_65001", Variants: "en:hr1", ThumbPNG: newPNG, HasHR1: true, HasLocale: true},
		},
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("创建备份目录失败: %v", err)
	}
	if err := svc.writeSnapshot(snapV1.Meta.Version, snapV1); err != nil {
		t.Fatalf("写入 V1 快照失败: %v", err)
	}
	if err := svc.writeSnapshot(snapV2.Meta.Version, snapV2); err != nil {
		t.Fatalf("写入 V2 快照失败: %v", err)
	}
	manifest := &backupManifest{
		LatestVersion:   snapV2.Meta.Version,
		PreviousVersion: snapV1.Meta.Version,
		Backups:         []VersionBackupMeta{snapV1.Meta, snapV2.Meta},
	}
	if err := svc.writeManifest(manifest); err != nil {
		t.Fatalf("写入 manifest 失败: %v", err)
	}
	svc.manifest = manifest

	// 1. EntityType="all" 时绝不混入图标条目，仅展示道具等常规数据
	allDiff, err := svc.CompareVersions(VersionDiffQuery{
		From:       snapV1.Meta.Version,
		To:         snapV2.Meta.Version,
		EntityType: "all",
		Page:       1,
		PageSize:   30,
	})
	if err != nil {
		t.Fatalf("CompareVersions(all) 失败: %v", err)
	}
	if allDiff.Summary.AddedIcons != 1 || allDiff.Summary.ModifiedIcons != 1 || allDiff.Summary.RemovedIcons != 1 {
		t.Fatalf("Summary 图标统计不符: %+v", allDiff.Summary)
	}
	for _, it := range allDiff.Items {
		if it.EntityType == "icon" {
			t.Fatalf("全部对比分区不应混入图标差异: %+v", it)
		}
	}
	if allDiff.TotalFiltered != 1 || allDiff.Items[0].ID != 200 {
		t.Fatalf("全部对比结果应仅含道具 #200，实际: %+v", allDiff.Items)
	}

	// 2. EntityType="icon" 仅返回新增/变更/移除的图标记录并携带新旧缩略图
	iconDiff, err := svc.CompareVersions(VersionDiffQuery{
		From:       snapV1.Meta.Version,
		To:         snapV2.Meta.Version,
		EntityType: "icon",
		Page:       1,
		PageSize:   120,
	})
	if err != nil {
		t.Fatalf("CompareVersions(icon) 失败: %v", err)
	}
	if iconDiff.TotalFiltered != 3 {
		t.Fatalf("期望 3 条图标差异（修改+新增+移除），实际 %d", iconDiff.TotalFiltered)
	}
	if iconDiff.Items[0].ID != 37618 || iconDiff.Items[0].ChangeType != "modified" || len(iconDiff.Items[0].OldThumbPNG) == 0 || len(iconDiff.Items[0].NewThumbPNG) == 0 {
		t.Fatalf("首条变更图标应同时携带旧图与新图: %+v", iconDiff.Items[0])
	}
	if iconDiff.Items[1].ID != 65001 || iconDiff.Items[1].ChangeType != "added" {
		t.Fatalf("次条应为新增图标 65001: %+v", iconDiff.Items[1])
	}
	if iconDiff.Items[2].ID != 99999 || iconDiff.Items[2].ChangeType != "removed" || len(iconDiff.Items[2].OldThumbPNG) == 0 {
		t.Fatalf("末条应为保留旧图的移除图标 99999: %+v", iconDiff.Items[2])
	}

	// 验证载荷可还原为合法 RGBA 像素
	oldRGBA, err := sqpack.DecodeIconBlobToRGBA(iconDiff.Items[0].OldThumbPNG)
	if err != nil || oldRGBA.Bounds().Dx() != 1 {
		t.Fatalf("旧图载荷解码异常: rgba=%v err=%v", oldRGBA, err)
	}

	// 3. 兼容无 Icons 字段的旧版历史快照，不误报全量新增
	legacySnap := &snapshotPayload{Meta: VersionBackupMeta{Version: "2026.01.01.0000.0000"}}
	if err := svc.writeSnapshot(legacySnap.Meta.Version, legacySnap); err != nil {
		t.Fatalf("写入历史快照失败: %v", err)
	}
	compatDiff, err := svc.CompareVersions(VersionDiffQuery{
		From:       legacySnap.Meta.Version,
		To:         snapV2.Meta.Version,
		EntityType: "icon",
		Page:       1,
		PageSize:   120,
	})
	if err != nil {
		t.Fatalf("历史快照对比失败: %v", err)
	}
	if compatDiff.TotalFiltered != 0 || compatDiff.Summary.AddedIcons != 0 {
		t.Fatalf("缺少 Icons 的旧快照不应误报全量新增: total=%d summary=%+v", compatDiff.TotalFiltered, compatDiff.Summary)
	}
}

func TestRealClientIconSnapshotAndBlobRoundTripIfConfigured(t *testing.T) {
	cnPath := ""
	if v := strings.TrimSpace(os.Getenv("FFXIV_CN_PATH")); v != "" {
		cnPath = v
	} else if raw, err := os.ReadFile(filepath.Join("..", "..", ".env")); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if k, val, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "FFXIV_CN_PATH" {
				cnPath = strings.Trim(strings.TrimSpace(val), `"'`)
			}
		}
	}
	if cnPath == "" {
		t.Skip("未配置 FFXIV_CN_PATH，跳过真实客户端图标快照性能验证")
	}

	info, err := config.ValidateClientPath(cnPath, "")
	if err != nil || info == nil {
		t.Skipf("国服路径无效: %v", err)
	}
	reader := sqpack.NewGameReader(info.GamePath)
	defer func() { _ = reader.Close() }()

	files, err := sqpack.ScanIconFiles(reader)
	if err != nil || len(files) == 0 {
		t.Fatalf("ScanIconFiles 失败: len=%d err=%v", len(files), err)
	}
	byID := make(map[int]gamedata.IconEntry, 70000)
	var icons []gamedata.IconEntry
	for _, f := range files {
		if _, ok := byID[f.IconID]; !ok {
			ic := gamedata.IconEntry{ID: f.IconID, GroupID: f.GroupID}
			byID[f.IconID] = ic
			icons = append(icons, ic)
		}
	}

	backupDir := filepath.Join(t.TempDir(), "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("创建测试备份目录失败: %v", err)
	}
	store := &gamedata.Store{
		PrimaryVersion: "2026.11.01.0000.0000",
		PrimaryReader:  reader,
		CNReader:       reader,
		Icons:          icons,
	}
	svc := NewService(store, backupDir)
	datFile, err := os.Create(svc.iconBlobDataPath())
	if err != nil {
		t.Fatalf("创建测试包失败: %v", err)
	}
	idxFile, err := os.Create(svc.iconBlobIndexPath())
	if err != nil {
		_ = datFile.Close()
		t.Fatalf("创建索引文件失败: %v", err)
	}
	datBw := bufio.NewWriterSize(datFile, 256*1024)
	idxBw := bufio.NewWriterSize(idxFile, 64*1024)
	var datOff int64
	svc.blobIndex = make(map[string]iconBlobLoc, 70000)
	svc.blobLoaded = true

	start := time.Now()
	snaps, tasks, err := sqpack.ComputeIconDigests([]sqpack.IconDigestSource{{Reader: reader, Files: files}})
	digestElapsed := time.Since(start)
	if err != nil || len(snaps) < 50000 {
		t.Fatalf("ComputeIconDigests 失败: len=%d err=%v", len(snaps), err)
	}

	blobStart := time.Now()
	if err := sqpack.ArchiveIconBlobs(nil, tasks, svc.hasIconBlob, func(blobKey string, payload []byte) error {
		off := datOff
		n, wErr := datBw.Write(payload)
		if wErr != nil {
			return wErr
		}
		datOff += int64(n)
		if _, wErr := fmt.Fprintf(idxBw, "%s\t%d\t%d\n", blobKey, off, n); wErr != nil {
			return wErr
		}
		svc.blobIndex[blobKey] = iconBlobLoc{offset: off, length: uint32(n)}
		return nil
	}, 0); err != nil {
		t.Fatalf("ArchiveIconBlobs 失败: %v", err)
	}
	_ = datBw.Flush()
	_ = idxBw.Flush()
	_ = datFile.Close()
	_ = idxFile.Close()
	blobElapsed := time.Since(blobStart)

	var blobKey37618 string
	for _, r := range snaps {
		if r.IconID == 37618 {
			blobKey37618 = r.BlobKey
			break
		}
	}
	if blobKey37618 == "" {
		t.Fatalf("未找到 #037618 图标指纹记录")
	}
	img, err := svc.ReadIconBlobImage(blobKey37618)
	if err != nil || img == nil || img.Bounds().Dx() == 0 {
		t.Fatalf("ReadIconBlobImage(#037618) 失败: img=%v err=%v", img, err)
	}
	t.Logf("真实客户端图标指纹(%v) + 缩略图归档(%v): %d 个唯一图标, blob总大小=%.2f MB, #037618=%dx%d",
		digestElapsed, blobElapsed, len(snaps), float64(datOff)/(1024*1024), img.Bounds().Dx(), img.Bounds().Dy())
}
