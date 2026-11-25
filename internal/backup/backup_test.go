package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
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
	if diff.Summary.AddedItems == 0 || diff.Summary.AddedQuests == 0 || diff.Summary.AddedLeves == 0 || diff.TotalFiltered == 0 {
		t.Fatalf("版本差异结果缺少任务/理符/道具变更: %+v", diff.Summary)
	}
}
