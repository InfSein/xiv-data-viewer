package gamedata

import (
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

func TestBuildIconIndexGroupingAndSearch(t *testing.T) {
	cnFiles := []sqpack.IconFileEntry{
		{IconID: 0, GroupID: 0, Variant: "", HR1: false},
		{IconID: 37618, GroupID: 37000, Variant: "", HR1: false},
		{IconID: 37618, GroupID: 37000, Variant: "", HR1: true},
		{IconID: 37618, GroupID: 37000, Variant: "hq", HR1: false},
		{IconID: 37618, GroupID: 37000, Variant: "chs", HR1: false},
		{IconID: 37619, GroupID: 37000, Variant: "", HR1: false},
	}
	glFiles := []sqpack.IconFileEntry{
		{IconID: 37618, GroupID: 37000, Variant: "", HR1: false},
		{IconID: 37618, GroupID: 37000, Variant: "en", HR1: true},
		{IconID: 120001, GroupID: 120000, Variant: "ja", HR1: false},
	}

	cnReader := sqpack.NewGameReader("cn_dummy")
	glReader := sqpack.NewGameReader("gl_dummy")
	idx := buildIconIndex(iconBuildInput{
		primaryReader:   cnReader,
		cnReader:        cnReader,
		globalReader:    glReader,
		cnIconFiles:     cnFiles,
		globalIconFiles: glFiles,
		itemSummaries: []ItemSummary{
			{ID: 44099, NameCn: "图拉尔菠萝蛋糕", IconID: 37618},
		},
	})

	// 应仅保留非空分组: 000000, 037000, 120000
	if len(idx.groups) != 3 {
		t.Fatalf("期望 3 个非空分组，实际 %d: %+v", len(idx.groups), idx.groups)
	}
	if idx.groups[0].Code != "000000" || idx.groups[1].Code != "037000" || idx.groups[2].Code != "120000" {
		t.Fatalf("分组编码排序异常: %+v", idx.groups)
	}
	if idx.groups[1].Count != 2 {
		t.Fatalf("037000 组应跳过无资源编号仅包含 2 个有效图标，实际 %d", idx.groups[1].Count)
	}

	ic37618, ok := idx.iconByID[37618]
	if !ok || !ic37618.HasHQ || !ic37618.HasHR1 || !ic37618.HasLocalized {
		t.Fatalf("图标 037618 元数据异常: %+v", ic37618)
	}
	// 验证双服合并来源: "" 优先主客户端 Primary，en_hr1 来自 GlobalReader
	var gotStdPrimary, gotEnGlobal bool
	for _, v := range ic37618.Variants {
		if v.Variant == "" && v.StdSource == IconSourcePrimary {
			gotStdPrimary = true
		}
		if v.Variant == "en" && v.HR1Source == IconSourceGlobal {
			gotEnGlobal = true
		}
	}
	if !gotStdPrimary || !gotEnGlobal {
		t.Fatalf("双服合并变体来源异常: %+v", ic37618.Variants)
	}

	// 验证组 120000 仅存在本地化资源时也能正确设 DefaultVariant
	ic120001 := idx.iconByID[120001]
	if ic120001.DefaultVariant != "ja" {
		t.Fatalf("120001 默认变体应回退到 ja，实际 %q", ic120001.DefaultVariant)
	}

	svc := NewService(&Store{
		PrimaryReader:         cnReader,
		CNReader:              cnReader,
		GlobalReader:          glReader,
		Icons:                 idx.icons,
		IconByID:              idx.iconByID,
		IconGroups:            idx.groups,
		IconsByGroup:          idx.iconsByGroup,
		AvailableIconVariants: idx.availableVariants,
		IconUsagesByID:        idx.usagesByID,
	})

	// 按分组检索
	resGrp := svc.SearchIcons(IconSearchQuery{GroupID: 37000})
	if resGrp.Total != 2 {
		t.Fatalf("分组 37000 检索应返回 2 项，实际 %d", resGrp.Total)
	}
	// 按关联物品名跨全组检索
	resName := svc.SearchIcons(IconSearchQuery{Q: "菠萝蛋糕", GroupID: -1})
	if resName.Total != 1 || resName.Items[0].ID != 37618 {
		t.Fatalf("按关联物品名搜索图标失败: %+v", resName)
	}
	// 按 HR1 筛选
	resHR1 := svc.SearchIcons(IconSearchQuery{GroupID: -1, VariantFilter: "hr1"})
	if resHR1.Total != 1 || resHR1.Items[0].ID != 37618 {
		t.Fatalf("HR1 筛选结果异常: %+v", resHR1)
	}
}
