package backup

import (
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
)

func TestVersionDiffTextModifiedAndRemovedOnly(t *testing.T) {
	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		ItemSummaries:  []gamedata.ItemSummary{{ID: 1001, NameCn: "旧圣剑", ItemLevel: 650, CategoryName: "武器"}},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			1001: {ID: 1001, NameCn: "旧圣剑", ItemLevel: 650, CategoryName: "武器"},
		},
	}
	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.03.0000.0000",
		ItemSummaries:  []gamedata.ItemSummary{{ID: 1001, NameCn: "新圣剑", ItemLevel: 650, CategoryName: "武器"}},
		ItemSummaryByID: map[int]gamedata.ItemSummary{
			1001: {ID: 1001, NameCn: "新圣剑", ItemLevel: 650, CategoryName: "武器"},
		},
	}

	svc := NewService(storeV2, t.TempDir())
	defer svc.Close()

	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region: snapshot.RegionCN, Patch: "7.55", Version: "2026.10.01.0000.0000",
		Store: storeV1,
		Texts: []TextSnapshotRecord{
			{SheetName: "Item", RowID: 1001, SubRowID: -1, ColIdx: 0, Text: "旧圣剑"},
			{SheetName: "Item", RowID: 1002, SubRowID: -1, ColIdx: 0, Text: "未改动盾牌"},
			{SheetName: "Addon", RowID: 50, SubRowID: -1, ColIdx: 0, Text: "旧系统提示文本"},
			{SheetName: "LegacySheet", RowID: 1, SubRowID: -1, ColIdx: 0, Text: "被整表移除的文本"},
		},
	})
	svc.SetLiveMemoryTexts([]TextSnapshotRecord{
		{SheetName: "Item", RowID: 1001, SubRowID: -1, ColIdx: 0, Text: "新圣剑"},
		{SheetName: "Item", RowID: 1002, SubRowID: -1, ColIdx: 0, Text: "未改动盾牌"},
		// 新增行与新增整表：必须被彻底忽略，不纳入文本对比
		{SheetName: "Item", RowID: 9999, SubRowID: -1, ColIdx: 0, Text: "版本新增道具不应出现"},
		{SheetName: "BrandNewSheet", RowID: 1, SubRowID: -1, ColIdx: 0, Text: "新增整表文本不应出现"},
	})

	// 1. 常规「全部」视图中不混入文本分区记录
	allRes, err := svc.CompareVersions(VersionDiffQuery{
		From:       "7.55",
		To:         LiveVersionID,
		EntityType: "all",
		Page:       1,
		PageSize:   50,
	})
	if err != nil {
		t.Fatalf("常规版本对比失败: %v", err)
	}
	for _, it := range allRes.Items {
		if it.EntityType == "text" {
			t.Fatalf("文本独立分区不应混入「全部」列表: %+v", it)
		}
	}
	if !allRes.Summary.TextDiffAvailable {
		t.Fatalf("两个版本均有文本源时应标记 TextDiffAvailable=true")
	}

	// 2. 独立「文本」分区：仅包含 1 条变更 + 2 条移除，绝不包含任何新增
	txtRes, err := svc.CompareVersions(VersionDiffQuery{
		From:       "7.55",
		To:         LiveVersionID,
		EntityType: "text",
		Page:       1,
		PageSize:   50,
	})
	if err != nil {
		t.Fatalf("文本版本对比失败: %v", err)
	}
	if txtRes.Summary.ModifiedTexts != 1 || txtRes.Summary.RemovedTexts != 2 || txtRes.TotalFiltered != 3 {
		t.Fatalf("文本差异统计异常: mod=%d rem=%d total=%d items=%+v",
			txtRes.Summary.ModifiedTexts, txtRes.Summary.RemovedTexts, txtRes.TotalFiltered, txtRes.Items)
	}
	for _, it := range txtRes.Items {
		if it.ChangeType == "added" || it.TextAfter == "版本新增道具不应出现" || it.TextSheet == "BrandNewSheet" {
			t.Fatalf("文本对比不应包含新增记录: %+v", it)
		}
	}
	if txtRes.Items[0].ChangeType != "modified" || txtRes.Items[0].TextBefore != "旧圣剑" || txtRes.Items[0].TextAfter != "新圣剑" {
		t.Fatalf("首条应为变更文本 Item #1001: %+v", txtRes.Items[0])
	}
	if txtRes.Items[0].TextJumpKind != "item" || txtRes.Items[0].TextJumpID != 1001 {
		t.Fatalf("变更的 Item 文本应支持直跳物品 #1001: %+v", txtRes.Items[0])
	}

	// 3. 筛选 added 必须返回 0 条；筛选 removed 返回 2 条；按关键词检索支持匹配旧文本与表名
	addedOnly, _ := svc.CompareVersions(VersionDiffQuery{
		From: "7.55", To: LiveVersionID, EntityType: "text", Filter: "added",
	})
	if addedOnly.TotalFiltered != 0 {
		t.Fatalf("文本分区筛选 added 必须返回 0 条，实际: %d", addedOnly.TotalFiltered)
	}
	remOnly, _ := svc.CompareVersions(VersionDiffQuery{
		From: "7.55", To: LiveVersionID, EntityType: "text", Filter: "removed",
	})
	if remOnly.TotalFiltered != 2 {
		t.Fatalf("文本分区筛选 removed 期望 2 条，实际: %d", remOnly.TotalFiltered)
	}
	qHit, _ := svc.CompareVersions(VersionDiffQuery{
		From: "7.55", To: LiveVersionID, EntityType: "text", Q: "旧系统提示",
	})
	if qHit.TotalFiltered != 1 || qHit.Items[0].TextSheet != "Addon" {
		t.Fatalf("文本分区搜索旧文本失败: %+v", qHit)
	}

	// 4. 缺失文本源的历史快照平滑跳过，不误报全量移除
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region: snapshot.RegionCN, Patch: "7.00", Version: "2024.07.01.0000.0000",
		Store: &gamedata.Store{PrimaryVersion: "2024.07.01.0000.0000"},
	})
	legacyRes, err := svc.CompareVersions(VersionDiffQuery{
		From:       "7.00",
		To:         LiveVersionID,
		EntityType: "text",
	})
	if err != nil {
		t.Fatalf("对比缺失文本源的历史版本不应报错: %v", err)
	}
	if legacyRes.Summary.TextDiffAvailable || legacyRes.TotalFiltered != 0 {
		t.Fatalf("缺失文本源时应平滑跳过并返回 0 条: avail=%v total=%d", legacyRes.Summary.TextDiffAvailable, legacyRes.TotalFiltered)
	}
}

func TestComputeInlineTextDiffBounds(t *testing.T) {
	bS, bE, aS, aE := ComputeInlineTextDiffBounds("威力：200点", "威力：240点")
	if gotB, gotA := "威力：200点"[bS:bE], "威力：240点"[aS:aE]; gotB != "0" || gotA != "4" {
		t.Fatalf("行内字符差异边界异常: before=%q after=%q", gotB, gotA)
	}

	bS, bE, aS, aE = ComputeInlineTextDiffBounds("相同文本", "相同文本")
	if bS != 0 || bE != 0 || aS != 0 || aE != 0 {
		t.Fatalf("相同文本差异区间应为全 0")
	}
}

func TestComputeInlineTextDiffSpansMultiHunk(t *testing.T) {
	before := "在指定地点设置象式浮空炮塔\n运转时间：10秒\n设置的象式浮空炮塔持续对周围的敌人发动范围魔法攻击\n威力：7500\n追加效果：为周围的队员附加能够抵御一定伤害的防护罩\n该防护罩能够抵消相当于恢复力7500的伤害量　持续时间：6秒"
	after := "在指定地点设置象式浮空炮塔\n运转时间：10秒\n设置的象式浮空炮塔持续对周围的敌人发动范围魔法攻击\n威力：8000\n追加效果：为周围的队员附加能够抵御一定伤害的防护罩\n该防护罩能够抵消相当于恢复力8000的伤害量　持续时间：6秒"

	bSpans, aSpans := ComputeInlineTextDiffSpans(before, after)
	if len(bSpans) != 2 || len(aSpans) != 2 {
		t.Fatalf("期望精确分离出 2 处数值变更，实际 before=%d after=%d", len(bSpans), len(aSpans))
	}
	for i := 0; i < 2; i++ {
		gotB := before[bSpans[i].Start:bSpans[i].End]
		gotA := after[aSpans[i].Start:aSpans[i].End]
		if gotB != "7500" || gotA != "8000" {
			t.Fatalf("第 %d 处差异片段不符合预期: before=%q after=%q（不应包含中间过渡文本）", i, gotB, gotA)
		}
	}

	// 验证相同文本返回空切片
	if bs, as := ComputeInlineTextDiffSpans("相同文本", "相同文本"); len(bs) != 0 || len(as) != 0 {
		t.Fatalf("相同文本不应产生差异片段: bs=%v as=%v", bs, as)
	}

	// 验证英文词元与小数数值不会被拆成零散单字母
	bEn := "Potency: 2.5s Fire spell"
	aEn := "Potency: 2.0s Blizzard spell"
	bs, as := ComputeInlineTextDiffSpans(bEn, aEn)
	if len(bs) != 2 || len(as) != 2 {
		t.Fatalf("期望 2 处英文/小数变更，实际 bs=%v as=%v", bs, as)
	}
	if bEn[bs[0].Start:bs[0].End] != "2.5" || aEn[as[0].Start:as[0].End] != "2.0" {
		t.Fatalf("小数差异片段异常: %q -> %q", bEn[bs[0].Start:bs[0].End], aEn[as[0].Start:as[0].End])
	}
	if bEn[bs[1].Start:bs[1].End] != "Fire" || aEn[as[1].Start:as[1].End] != "Blizzard" {
		t.Fatalf("英文词元差异片段异常: %q -> %q", bEn[bs[1].Start:bs[1].End], aEn[as[1].Start:as[1].End])
	}
}
