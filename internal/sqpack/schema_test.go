package sqpack

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSchemaLoadAndAlignColumns(t *testing.T) {
	schemasFS := os.DirFS(filepath.Join("..", "..", "schemas"))

	questFields, err := LoadSchemaFieldNames(schemasFS, "Quest")
	if err != nil {
		t.Fatalf("读取 Quest.yml 失败: %v", err)
	}
	for _, want := range []string{"Name", "Reward[0]", "OptionalItemReward[4]", "PreviousQuest[2]", "ClassJobLevel[0]", "EventIconType"} {
		if !slices.Contains(questFields, want) {
			t.Fatalf("Quest 展开字段缺少 %q", want)
		}
	}

	battleFields, err := LoadSchemaFieldNames(schemasFS, "BattleLeve")
	if err != nil {
		t.Fatalf("读取 BattleLeve.yml 失败: %v", err)
	}
	for _, want := range []string{"LeveData[0].BNpcName", "LeveData[7].ItemDropRate", "Objectives[2]"} {
		if !slices.Contains(battleFields, want) {
			t.Fatalf("BattleLeve 展开字段缺少 %q", want)
		}
	}

	satFields, err := LoadSchemaFieldNames(schemasFS, "SatisfactionNpc")
	if err != nil {
		t.Fatalf("读取 SatisfactionNpc.yml 失败: %v", err)
	}
	for _, want := range []string{"SatisfactionNpcParams[0].SupplyIndex", "SatisfactionNpcParams[5].Item[2]", "QuestRequired"} {
		if !slices.Contains(satFields, want) {
			t.Fatalf("SatisfactionNpc 展开字段缺少 %q", want)
		}
	}

	// 验证 EXH 字节偏移乱序排列及同偏移 packed bool 位标志的类型顺序对齐
	exh := &EXHHeader{
		ColumnCount: 4,
		Columns: []EXHColumn{
			{Type: 0x0019, Offset: 8}, // bool bit 0x01 -> 第3个逻辑字段 FlagA
			{Type: 0x0000, Offset: 0}, // string -> 第1个逻辑字段 Name
			{Type: 0x001A, Offset: 8}, // bool bit 0x02 -> 第4个逻辑字段 FlagB
			{Type: 0x0005, Offset: 4}, // uint16 -> 第2个逻辑字段 Level
		},
	}
	_, colIdx := AlignEXHColumns(exh, []string{"Name", "Level", "FlagA", "FlagB"})
	if colIdx["Name"] != 1 || colIdx["Level"] != 3 || colIdx["FlagA"] != 0 || colIdx["FlagB"] != 2 {
		t.Fatalf("EXH 列按字节偏移对齐结果异常: %+v", colIdx)
	}

	sheet := &SchemaSheet{
		Cols: colIdx,
		Rows: map[int]Row{
			10: {true, "测试任务", false, 50},
		},
	}
	r := sheet.Rows[10]
	if sheet.Str(r, "Name") != "测试任务" || sheet.Int(r, "Level") != 50 || !sheet.Bool(r, "FlagA") || sheet.Bool(r, "FlagB") {
		t.Fatalf("SchemaSheet 安全取值异常")
	}
	if sheet.Int(r, "NotExists") != 0 || sheet.Str(r, "NotExists") != "" {
		t.Fatalf("未知字段应安全返回零值")
	}
}
