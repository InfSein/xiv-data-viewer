package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
	"github.com/klauspost/compress/zstd"
)

func TestVersionDiffQuestsLevesAndCollectables(t *testing.T) {
	snapDir := t.TempDir()

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

	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.03.0000.0000",
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

	svc := NewService(storeV2, snapDir)
	defer svc.Close()

	// 未下载任何快照时应提示 NeedsDownload，而不在本地自动生成伪造快照
	emptyDiff, err := svc.CompareVersions(VersionDiffQuery{Page: 1, PageSize: 50})
	if err != nil || !emptyDiff.NeedsDownload {
		t.Fatalf("无快照时应返回 NeedsDownload=true: res=%+v err=%v", emptyDiff, err)
	}

	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region:  snapshot.RegionCN,
		Patch:   "7.55",
		Version: "2026.10.01.0000.0000",
		Store:   storeV1,
	})

	status := svc.GetVersionStatus()
	if status.DefaultCNFrom != "7.55" || status.DefaultCNTo != LiveVersionID {
		t.Fatalf("期望默认从 7.55 对比当前客户端: %+v", status)
	}

	diff, err := svc.CompareVersions(VersionDiffQuery{Page: 1, PageSize: 50})
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

	questOnly, err := svc.CompareVersions(VersionDiffQuery{EntityType: "quest", Page: 1, PageSize: 20})
	if err != nil || questOnly.TotalFiltered != 3 {
		t.Fatalf("按任务筛选 Diff 结果异常: total=%d err=%v", questOnly.TotalFiltered, err)
	}
	leveOnly, err := svc.CompareVersions(VersionDiffQuery{EntityType: "leve", Page: 1, PageSize: 20})
	if err != nil || leveOnly.TotalFiltered != 2 {
		t.Fatalf("按理符筛选 Diff 结果异常: total=%d err=%v", leveOnly.TotalFiltered, err)
	}
	colSearch, err := svc.CompareVersions(VersionDiffQuery{EntityType: "item", Q: "收藏品", Page: 1, PageSize: 20})
	if err != nil || colSearch.TotalFiltered != 3 {
		t.Fatalf("按收藏品关键词检索物品 Diff 异常: total=%d err=%v", colSearch.TotalFiltered, err)
	}
}

func TestVersionDiffIgnoresPriceMidChanges(t *testing.T) {
	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 50001, NameCn: "仅售价变化物品", ItemLevel: 100, CategoryName: "杂货", PriceMid: 100},
			{ID: 50002, NameCn: "品级与售价同变物品", ItemLevel: 100, CategoryName: "杂货", PriceMid: 100},
		},
	}
	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.03.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 50001, NameCn: "仅售价变化物品", ItemLevel: 100, CategoryName: "杂货", PriceMid: 250},
			{ID: 50002, NameCn: "品级与售价同变物品", ItemLevel: 110, CategoryName: "杂货", PriceMid: 250},
		},
	}

	svc := NewService(storeV2, t.TempDir())
	defer svc.Close()
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region: snapshot.RegionCN, Patch: "7.55", Version: "2026.10.01.0000.0000",
		Store: storeV1,
	})

	diff, err := svc.CompareVersions(VersionDiffQuery{EntityType: "item", Page: 1, PageSize: 20})
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
	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 44099, NameCn: "菠萝蛋糕", NameJa: "パイナップルケーキ", NameEn: "Pineapple Cake", ItemLevel: 680, CategoryName: "食品"},
		},
		Quests: []gamedata.QuestEntry{
			{ID: 65536, NameCn: "主线任务", NameJa: "メインクエスト", NameEn: "Main Quest", Level: 90, GenreName: "MSQ", GilReward: 500, ExpReward: 1000},
		},
	}
	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.03.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 44099, NameCn: "菠萝蛋糕", NameJa: "パイナップルケーキ", NameEn: "Pineapple Cake", ItemLevel: 690, CategoryName: "食品"},
		},
		Quests: []gamedata.QuestEntry{
			{ID: 65536, NameCn: "主线任务", NameJa: "メインクエスト", NameEn: "Main Quest", Level: 90, GenreName: "MSQ", GilReward: 800, ExpReward: 1000},
		},
	}

	svc := NewService(storeV2, t.TempDir())
	defer svc.Close()
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region: snapshot.RegionCN, Patch: "7.55", Version: "2026.10.01.0000.0000",
		Store: storeV1,
	})

	svc.SetLang(i18n.LangEN)
	diffEN, err := svc.CompareVersions(VersionDiffQuery{Page: 1, PageSize: 20})
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
		t.Fatalf("英语 Diff 字段标签不符合预期: itemEN=%v questEN=%v", foundItemEN, foundQuestEN)
	}

	svc.SetLang(i18n.LangJA)
	diffJA, err := svc.CompareVersions(VersionDiffQuery{EntityType: "item", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("日语 Diff 对比失败: %v", err)
	}
	wantItemLabelJA := i18n.T(i18n.LangJA, "backup.field_ilvl")
	if len(diffJA.Items) != 1 || len(diffJA.Items[0].Changes) == 0 || !strings.EqualFold(diffJA.Items[0].Changes[0].Label, wantItemLabelJA) {
		t.Fatalf("日语 Diff 字段标签不符合预期: %+v", diffJA.Items)
	}
}

func TestVersionDiffBGM(t *testing.T) {
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
	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.03.0000.0000",
		BGMs: []gamedata.MusicEntry{
			{ID: 1, Title: "BGM_Unchanged", File: "music/ffxiv/BGM_Unchanged.scd", FileExists: true, FileSize: 1024, ContentMD5: sameMD5},
			{ID: 2, Title: "BGM_Modified_Track", File: "music/ex5/BGM_Modified_Track.scd", FileExists: true, FileSize: 2150400, ContentMD5: newMD5, ExpansionName: "金曦之遗辉"},
			{ID: 4, Title: "BGM_Added_Track", File: "music/ex5/BGM_Added_Track.scd", FileExists: true, FileSize: 3145728, ContentMD5: newMD5, ExpansionName: "金曦之遗辉", UsageSummary: "新副本战斗"},
		},
	}

	svc := NewService(storeV2, t.TempDir())
	defer svc.Close()
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region: snapshot.RegionCN, Patch: "7.55", Version: "2026.10.01.0000.0000",
		Store: storeV1,
	})

	diff, err := svc.CompareVersions(VersionDiffQuery{EntityType: "bgm", Page: 1, PageSize: 20})
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
}

func TestQuestCutsceneDiff(t *testing.T) {
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
	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.03.0000.0000",
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

	svc := NewService(storeV2, t.TempDir())
	defer svc.Close()
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region: snapshot.RegionCN, Patch: "7.55", Version: "2026.10.01.0000.0000",
		Store: storeV1,
	})

	diff, err := svc.CompareVersions(VersionDiffQuery{EntityType: "quest", Page: 1, PageSize: 20})
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
}

func makeTinyTestPNG(r, g, b byte) []byte {
	const texHdrSz = 80
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

func TestVersionDiffIconsPartitionAndOldBlobDecoding(t *testing.T) {
	oldBlob := makeTinyTestPNG(200, 40, 40)
	newBlob := makeTinyTestPNG(40, 200, 80)

	storeV1 := &gamedata.Store{
		PrimaryVersion: "2026.10.01.0000.0000",
		ItemSummaries:  []gamedata.ItemSummary{{ID: 100, NameCn: "未改动道具"}},
	}
	storeV2 := &gamedata.Store{
		PrimaryVersion: "2026.11.03.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 100, NameCn: "未改动道具"},
			{ID: 200, NameCn: "新增对比道具"},
		},
	}

	svc := NewService(storeV2, t.TempDir())
	defer svc.Close()

	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region: snapshot.RegionCN, Patch: "7.55", Version: "2026.10.01.0000.0000",
		Store: storeV1,
		Icons: []snapshot.IconDigestRecord{
			{ID: 37618, Digest: "v1_37618", Variants: "default:std|default:hr1", BlobKey: "old_37618", HasHR1: true},
			{ID: 37619, Digest: "v1_37619", Variants: "default:std", BlobKey: "same_37619"},
			{ID: 99999, Digest: "v1_99999", Variants: "default:std", BlobKey: "old_99999"},
		},
		IconBlobs: map[string][]byte{
			"old_37618": oldBlob,
			"old_99999": oldBlob,
		},
	})
	svc.SetLiveMemoryIcons(
		[]snapshot.IconDigestRecord{
			{ID: 37618, Digest: "v2_37618", Variants: "default:std|default:hr1", BlobKey: "new_37618", HasHR1: true},
			{ID: 37619, Digest: "v1_37619", Variants: "default:std", BlobKey: "same_37619"},
			{ID: 65001, Digest: "v2_65001", Variants: "en:hr1", BlobKey: "new_65001", HasHR1: true, HasLocale: true},
		},
		map[string][]byte{
			"new_37618": newBlob,
			"new_65001": newBlob,
		},
	)

	// 1. EntityType="all" 不混入图标条目
	allDiff, err := svc.CompareVersions(VersionDiffQuery{EntityType: "all", Page: 1, PageSize: 30})
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

	// 2. EntityType="icon" 返回新增/变更/移除记录，且可还原旧图标缩略图
	iconDiff, err := svc.CompareVersions(VersionDiffQuery{EntityType: "icon", Page: 1, PageSize: 120})
	if err != nil {
		t.Fatalf("CompareVersions(icon) 失败: %v", err)
	}
	if iconDiff.TotalFiltered != 3 {
		t.Fatalf("期望 3 条图标差异，实际 %d", iconDiff.TotalFiltered)
	}
	if iconDiff.Items[0].ID != 37618 || iconDiff.Items[0].ChangeType != "modified" || iconDiff.Items[0].OldBlobKey != "old_37618" {
		t.Fatalf("首条应为变更图标 37618: %+v", iconDiff.Items[0])
	}
	oldRGBA, err := svc.ReadIconBlobImage("old_37618")
	if err != nil || oldRGBA == nil || oldRGBA.Bounds().Dx() != 1 {
		t.Fatalf("读取旧图标载荷失败: rgba=%v err=%v", oldRGBA, err)
	}
}

func buildTestXivSnapBytes(t *testing.T, region, patch, gameVer string, langs []string) ([]byte, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), snapshot.AssetFileName(patch, region))
	w, err := snapshot.NewWriter(p, zstd.SpeedDefault)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.AddFile("exd/root.exl", []byte("EXLT,2\n"))
	if err := w.Close(snapshot.SnapshotMeta{
		ID:          snapshot.MakeSnapshotID(region, patch),
		Region:      region,
		Patch:       patch,
		GameVersion: gameVer,
		Languages:   langs,
		CreatedAt:   "2026-09-10T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:])
}

func TestCatalogPatchMappingDownloadAndDelete(t *testing.T) {
	dir := t.TempDir()
	snapBytes, shaHex := buildTestXivSnapBytes(t, snapshot.RegionCN, "7.55", "2026.09.10.0000.0000", []string{"chs"})

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog.json":
			cat := snapshot.Catalog{
				SchemaVersion: snapshot.CatalogSchemaVersion,
				UpdatedAt:     "2026-11-26T00:00:00Z",
				Versions: []snapshot.VersionCatalogEntry{
					{
						Patch:      "7.58",
						CreatedAt:  "2026-11-03T00:00:00Z",
						ReleaseTag: "v7.58",
						CN: &snapshot.RegionAsset{
							GameVersion: "2026.11.03.0000.0000",
							Languages:   []string{"chs"},
							AssetName:   snapshot.AssetFileName("7.58", snapshot.RegionCN),
							DownloadURL: srv.URL + "/assets/cn-7.58",
							Size:        1024,
						},
					},
					{
						Patch:      "7.55",
						CreatedAt:  "2026-09-10T00:00:00Z",
						ReleaseTag: "v7.55",
						CN: &snapshot.RegionAsset{
							GameVersion: "2026.09.10.0000.0000",
							Languages:   []string{"chs"},
							AssetName:   snapshot.AssetFileName("7.55", snapshot.RegionCN),
							DownloadURL: srv.URL + "/assets/cn-7.55",
							Size:        int64(len(snapBytes)),
							SHA256:      shaHex,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(cat)
		case "/assets/cn-7.55":
			_, _ = w.Write(snapBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	store := &gamedata.Store{
		HasCN:          true,
		CNVersion:      "2026.11.03.0000.0000",
		PrimaryVersion: "2026.11.03.0000.0000",
		PrimaryLang:    sqpack.LangChineseSimplified,
		AppLang:        i18n.LangZH,
	}
	snapDir := filepath.Join(dir, "snapshots")
	svc := NewService(store, snapDir)
	defer svc.Close()
	svc.SetSnapshotClient(snapshot.NewClient(snapshot.ClientOptions{
		CatalogURL: srv.URL + "/catalog.json",
		HTTPClient: srv.Client(),
	}))

	// 1. 拉取目录后应自动将 2026.11.03.0000.0000 映射为玩家可读版本名 7.58
	st, err := svc.RefreshCatalog(context.Background())
	if err != nil {
		t.Fatalf("RefreshCatalog 失败: %v", err)
	}
	if st.CNPatch != "7.58" || !strings.Contains(st.CNDisplayName, "7.58") {
		t.Fatalf("未正确映射客户端版本名 7.58: patch=%q disp=%q", st.CNPatch, st.CNDisplayName)
	}
	if len(st.RemoteVersions) != 2 || len(st.CNBackups) != 1 {
		t.Fatalf("目录与本地列表状态不符: remote=%d local=%d", len(st.RemoteVersions), len(st.CNBackups))
	}

	// 2. 按统一版本号下载 7.55 后应出现在本地可用列表并自动成为 DefaultCNFrom
	stAfterDL, err := svc.DownloadVersion(context.Background(), "7.55", nil)
	if err != nil {
		t.Fatalf("DownloadVersion 失败: %v", err)
	}
	if stAfterDL.DefaultCNFrom != "7.55" || len(stAfterDL.CNBackups) != 2 {
		t.Fatalf("下载后状态不符: %+v", stAfterDL)
	}

	// 3. 删除本地快照后应从可用列表移除
	stAfterDel, err := svc.DeleteVersion("7.55")
	if err != nil {
		t.Fatalf("DeleteVersion 失败: %v", err)
	}
	if len(stAfterDel.CNBackups) != 1 || stAfterDel.DefaultCNFrom != "" {
		t.Fatalf("删除后状态不符: %+v", stAfterDel)
	}
}

func TestDualServerRecognizesBothAndComparesSameRegionOnly(t *testing.T) {
	dir := t.TempDir()
	cnBytes, cnSHA := buildTestXivSnapBytes(t, snapshot.RegionCN, "7.55", "2026.09.10.0000.0000", []string{"chs"})
	glBytes, glSHA := buildTestXivSnapBytes(t, snapshot.RegionGlobal, "7.55", "2026.09.10.0000.0000", []string{"ja", "en"})

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog.json":
			cat := snapshot.Catalog{
				SchemaVersion: snapshot.CatalogSchemaVersion,
				UpdatedAt:     "2026-11-26T00:00:00Z",
				Versions: []snapshot.VersionCatalogEntry{
					{
						Patch:      "7.58",
						CreatedAt:  "2026-11-03T00:00:00Z",
						ReleaseTag: "v7.58",
						CN: &snapshot.RegionAsset{
							GameVersion: "2026.11.03.0000.0000",
							Languages:   []string{"chs"},
							AssetName:   snapshot.AssetFileName("7.58", snapshot.RegionCN),
							DownloadURL: srv.URL + "/assets/cn-7.58",
							Size:        1024,
						},
						Global: &snapshot.RegionAsset{
							GameVersion: "2026.11.03.0000.0000",
							Languages:   []string{"ja", "en"},
							AssetName:   snapshot.AssetFileName("7.58", snapshot.RegionGlobal),
							DownloadURL: srv.URL + "/assets/gl-7.58",
							Size:        2048,
						},
					},
					{
						Patch:      "7.55",
						CreatedAt:  "2026-09-10T00:00:00Z",
						ReleaseTag: "v7.55",
						CN: &snapshot.RegionAsset{
							GameVersion: "2026.09.10.0000.0000",
							Languages:   []string{"chs"},
							AssetName:   snapshot.AssetFileName("7.55", snapshot.RegionCN),
							DownloadURL: srv.URL + "/assets/cn-7.55",
							Size:        int64(len(cnBytes)),
							SHA256:      cnSHA,
						},
						Global: &snapshot.RegionAsset{
							GameVersion: "2026.09.10.0000.0000",
							Languages:   []string{"ja", "en"},
							AssetName:   snapshot.AssetFileName("7.55", snapshot.RegionGlobal),
							DownloadURL: srv.URL + "/assets/gl-7.55",
							Size:        int64(len(glBytes)),
							SHA256:      glSHA,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(cat)
		case "/assets/cn-7.55":
			_, _ = w.Write(cnBytes)
		case "/assets/gl-7.55":
			_, _ = w.Write(glBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// 同时配置国服 + 国际服客户端
	dualStore := &gamedata.Store{
		HasCN:          true,
		HasGlobal:      true,
		CNVersion:      "2026.11.03.0000.0000",
		GlobalVersion:  "2026.11.03.0000.0000",
		PrimaryVersion: "2026.11.03.0000.0000",
		PrimaryLang:    sqpack.LangChineseSimplified,
		AppLang:        i18n.LangZH,
	}
	svc := NewService(dualStore, filepath.Join(dir, "snapshots"))
	defer svc.Close()
	svc.SetSnapshotClient(snapshot.NewClient(snapshot.ClientOptions{
		CatalogURL: srv.URL + "/catalog.json",
		HTTPClient: srv.Client(),
	}))

	// 1. 拉取目录后应同时识别国服 7.58 与国际服 7.58，且目录仅显示 2 条统一版本记录（不拆分国服/国际服）
	st, err := svc.RefreshCatalog(context.Background())
	if err != nil {
		t.Fatalf("RefreshCatalog 失败: %v", err)
	}
	if !st.HasCN || !st.HasGlobal || st.CNPatch != "7.58" || st.GlobalPatch != "7.58" {
		t.Fatalf("双服模式应同时识别国服与国际服当前版本: %+v", st)
	}
	if len(st.RemoteVersions) != 2 {
		t.Fatalf("统一版本目录应合并为 2 条（7.58 / 7.55），实际 %d 条", len(st.RemoteVersions))
	}

	// 2. 用户只需按统一版本号下载 "7.55"，底层自动补齐国服与国际服两个快照包
	stDL, err := svc.DownloadVersion(context.Background(), "7.55", nil)
	if err != nil {
		t.Fatalf("双服统一下载 7.55 失败: %v", err)
	}
	if stDL.DefaultCNFrom != "7.55" || stDL.DefaultGlobalFrom != "7.55" {
		t.Fatalf("双服下载后国服与国际服均应具备 7.55 对比基准: %+v", stDL)
	}
	var found755 *VersionBackupMeta
	for i := range stDL.RemoteVersions {
		if stDL.RemoteVersions[i].Patch == "7.55" {
			found755 = &stDL.RemoteVersions[i]
			break
		}
	}
	if found755 == nil || !found755.Downloaded || !found755.DownloadedCN || !found755.DownloadedGlobal || found755.NeedsSupplement {
		t.Fatalf("7.55 下载状态异常: %+v", found755)
	}

	// 3. 注入国服与国际服各自独立的当前数据与 7.55 旧数据，校验严格同服对比（不串服）
	svc.SetLiveMemoryStore(snapshot.RegionCN, &gamedata.Store{
		HasCN: true, CNVersion: "2026.11.03.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 1000, NameCn: "国服旧剑", ItemLevel: 700, CategoryName: "武器"},
			{ID: 1001, NameCn: "国服独占新装备", ItemLevel: 730, CategoryName: "武器"},
		},
	})
	svc.SetLiveMemoryStore(snapshot.RegionGlobal, &gamedata.Store{
		HasGlobal: true, GlobalVersion: "2026.11.03.0000.0000",
		ItemSummaries: []gamedata.ItemSummary{
			{ID: 2000, NameCn: "Global Old Sword", ItemLevel: 700, CategoryName: "Weapon"},
			{ID: 2001, NameCn: "Global Exclusive Blade", ItemLevel: 730, CategoryName: "Weapon"},
		},
	})
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region: snapshot.RegionCN, Patch: "7.55", Version: "2026.09.10.0000.0000",
		Store: &gamedata.Store{
			HasCN: true, CNVersion: "2026.09.10.0000.0000",
			ItemSummaries: []gamedata.ItemSummary{{ID: 1000, NameCn: "国服旧剑", ItemLevel: 700, CategoryName: "武器"}},
		},
	})
	svc.RegisterMemorySnapshot(MemorySnapshot{
		Region: snapshot.RegionGlobal, Patch: "7.55", Version: "2026.09.10.0000.0000",
		Store: &gamedata.Store{
			HasGlobal: true, GlobalVersion: "2026.09.10.0000.0000",
			ItemSummaries: []gamedata.ItemSummary{{ID: 2000, NameCn: "Global Old Sword", ItemLevel: 700, CategoryName: "Weapon"}},
		},
	})

	cnDiff, err := svc.CompareVersions(VersionDiffQuery{Region: snapshot.RegionCN, From: "7.55", To: LiveVersionID, EntityType: "item"})
	if err != nil {
		t.Fatalf("国服对比失败: %v", err)
	}
	if cnDiff.TotalFiltered != 1 || cnDiff.Items[0].ID != 1001 || cnDiff.Items[0].NameCn != "国服独占新装备" {
		t.Fatalf("国服对比不应混入国际服数据: %+v", cnDiff.Items)
	}

	glDiff, err := svc.CompareVersions(VersionDiffQuery{Region: snapshot.RegionGlobal, From: "7.55", To: LiveVersionID, EntityType: "item"})
	if err != nil {
		t.Fatalf("国际服对比失败: %v", err)
	}
	if glDiff.TotalFiltered != 1 || glDiff.Items[0].ID != 2001 || glDiff.Items[0].NameCn != "Global Exclusive Blade" {
		t.Fatalf("国际服对比不应混入国服数据: %+v", glDiff.Items)
	}

	// 4. 删除统一版本 7.55 应同时清理本地国服与国际服文件
	stDel, err := svc.DeleteVersion("7.55")
	if err != nil {
		t.Fatalf("DeleteVersion 失败: %v", err)
	}
	if len(stDel.CNBackups) != 1 || len(stDel.GlobalBackups) != 1 {
		t.Fatalf("删除 7.55 后双服本地快照均应清空: cn=%d global=%d", len(stDel.CNBackups), len(stDel.GlobalBackups))
	}
}

func TestGlobalCompareEXDLangAndSetLangCacheClear(t *testing.T) {
	if got := globalCompareEXDLang(i18n.LangZH); got != sqpack.LangEnglish {
		t.Fatalf("中文界面下国际服对比语言应为 en，实际 %v", got)
	}
	if got := globalCompareEXDLang(i18n.LangEN); got != sqpack.LangEnglish {
		t.Fatalf("英语界面下国际服对比语言应为 en，实际 %v", got)
	}
	if got := globalCompareEXDLang(i18n.LangJA); got != sqpack.LangJapanese {
		t.Fatalf("日语界面下国际服对比语言应为 ja，实际 %v", got)
	}

	svc := NewService(&gamedata.Store{HasCN: true, HasGlobal: true}, t.TempDir())
	defer svc.Close()
	svc.mu.Lock()
	svc.liveRegionStores["cn\x00en"] = &gamedata.Store{AppLang: i18n.LangEN}
	svc.liveRegionStores["global\x00zh"] = &gamedata.Store{AppLang: i18n.LangZH}
	svc.mu.Unlock()

	svc.SetLang(i18n.LangJA)
	svc.mu.Lock()
	left := len(svc.liveRegionStores)
	svc.mu.Unlock()
	if left != 0 {
		t.Fatalf("切换界面语言后应清空双服实时 Store 缓存，剩余 %d", left)
	}
}

// TestRealSameVersionZeroDiffAcrossAllUILanguages 在本机同时存在客户端与同版 .xivsnap 快照时，
// 校验简体中文 / 英语 / 日语三种界面语言下，国服与国际服对比自身同版本快照均不产生任何跨语言伪差异。
func TestRealSameVersionZeroDiffAcrossAllUILanguages(t *testing.T) {
	cfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过真实同版本零差异回归测试: %v", err)
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		t.Skip("无法定位用户配置目录")
	}
	snapDir := filepath.Join(cfgDir, "xiv-data-viewer", "snapshots")
	locals, err := snapshot.ListLocalSnapshots(snapDir)
	if err != nil || len(locals) == 0 {
		t.Skip("本地未下载 .xivsnap 快照，跳过真实同版本零差异回归测试")
	}

	matchPatch := func(region, gameVer string) string {
		for _, info := range locals {
			if snapshot.NormalizeRegion(info.Meta.Region) == region && info.Meta.GameVersion == gameVer && info.Meta.Patch != "" {
				return info.Meta.Patch
			}
		}
		return ""
	}
	cnPatch := ""
	if resolved.HasCN() {
		cnPatch = matchPatch(snapshot.RegionCN, resolved.CN.Version)
	}
	glPatch := ""
	if resolved.HasGlobal() {
		glPatch = matchPatch(snapshot.RegionGlobal, resolved.Global.Version)
	}
	if cnPatch == "" && glPatch == "" {
		t.Skip("本地已下载快照与当前客户端版本均不相同，跳过同版本零差异回归测试")
	}

	schemasFS := os.DirFS(filepath.Join("..", "..", "schemas"))
	var baseStore *gamedata.Store
	defer func() {
		if baseStore != nil {
			_ = baseStore.Close()
		}
	}()

	for _, uiLang := range []i18n.Lang{i18n.LangZH, i18n.LangEN, i18n.LangJA} {
		store, err := gamedata.LoadGameDataStoreWithOptions(resolved, schemasFS, gamedata.StoreLoadOptions{
			AppLang:       uiLang,
			BaseStore:     baseStore,
			SharedReaders: baseStore != nil,
		})
		if err != nil {
			t.Fatalf("加载真实客户端数据失败 (lang=%s): %v", uiLang, err)
		}
		if baseStore == nil {
			baseStore = store
		}

		svc := NewService(store, snapDir)
		svc.SetLang(uiLang)

		checkRegion := func(region, patch string) {
			if patch == "" {
				return
			}
			res, err := svc.CompareVersions(VersionDiffQuery{
				Region:     region,
				From:       patch,
				To:         LiveVersionID,
				EntityType: "all",
				Page:       1,
				PageSize:   10,
			})
			if err != nil {
				t.Fatalf("对比同版本失败 (lang=%s region=%s patch=%s): %v", uiLang, region, patch, err)
			}
			if res.TotalFiltered != 0 {
				t.Fatalf("同版本产生伪差异 (lang=%s region=%s patch=%s): total=%d summary=%+v first=%+v",
					uiLang, region, patch, res.TotalFiltered, res.Summary, res.Items[0])
			}
			txtRes, err := svc.CompareVersions(VersionDiffQuery{
				Region:     region,
				From:       patch,
				To:         LiveVersionID,
				EntityType: "text",
				Page:       1,
				PageSize:   10,
			})
			if err != nil {
				t.Fatalf("文本同版本对比失败 (lang=%s region=%s patch=%s): %v", uiLang, region, patch, err)
			}
			if txtRes.TotalFiltered != 0 {
				t.Fatalf("同版本文本产生伪差异 (lang=%s region=%s patch=%s): total=%d first=%+v",
					uiLang, region, patch, txtRes.TotalFiltered, txtRes.Items[0])
			}
		}

		checkRegion(snapshot.RegionCN, cnPatch)
		checkRegion(snapshot.RegionGlobal, glPatch)
		svc.Close()
	}
}
