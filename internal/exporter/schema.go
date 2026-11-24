package exporter

import (
	"fmt"
	"io/fs"
	"sync"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

// ColumnMeta 表示对齐后的单列 CSV 元信息。
type ColumnMeta struct {
	Index  int
	Name   string
	Type   string
	Offset uint16
}

// SheetSchemaMeta 表示数据表的完整列结构与变体类型。
type SheetSchemaMeta struct {
	SheetName string
	Columns   []ColumnMeta
	Variant   uint8
}

// CoreSheetNames 为当无法读取 root.exl 时的常用核心表清单。
var CoreSheetNames = []string{
	"Item", "Recipe", "Action", "Quest", "PlaceName",
	"ClassJob", "ClassJobCategory", "Status", "ENpcResident", "ENpcBase",
	"GatheringItem", "GatheringPoint", "FishingSpot", "GilShop", "SpecialShop",
	"Map", "TerritoryType",
}

// FormatColumnType 将 EXH 二进制列类型映射为 SaintCoinach CSV 类型描述符。
func FormatColumnType(colType uint16) string {
	switch colType {
	case 0:
		return "str"
	case 1:
		return "bool"
	case 2:
		return "sbyte"
	case 3:
		return "byte"
	case 4:
		return "int16"
	case 5:
		return "uint16"
	case 6:
		return "int32"
	case 7:
		return "uint32"
	case 9:
		return "single"
	case 10:
		return "int64"
	case 11:
		return "uint64"
	case 25:
		return "bit&01"
	case 26:
		return "bit&02"
	case 27:
		return "bit&04"
	case 28:
		return "bit&08"
	case 29:
		return "bit&10"
	case 30:
		return "bit&20"
	case 31:
		return "bit&40"
	case 32:
		return "bit&80"
	default:
		return "unk"
	}
}

// SchemaManager 管理 EXDSchema YAML 结构文件并与 EXH 二进制列偏移动态对齐。
type SchemaManager struct {
	schemasFS fs.FS

	mu            sync.RWMutex
	schemaCache   map[string]*SheetSchemaMeta
	allSheetNames []string
}

// NewSchemaManager 使用给定的只读文件系统（支持内置 embed.FS 或本地目录）创建 SchemaManager。
func NewSchemaManager(schemasFS fs.FS) *SchemaManager {
	return &SchemaManager{
		schemasFS:   schemasFS,
		schemaCache: make(map[string]*SheetSchemaMeta),
	}
}

// GetAllSheetNames 从客户端 exd/root.exl 解析全量数据表名称列表（7,900+ 张表）。
func (m *SchemaManager) GetAllSheetNames(reader *sqpack.GameReader) []string {
	m.mu.RLock()
	if len(m.allSheetNames) > 0 {
		out := append([]string(nil), m.allSheetNames...)
		m.mu.RUnlock()
		return out
	}
	m.mu.RUnlock()

	if reader == nil {
		return append([]string(nil), CoreSheetNames...)
	}
	exlRaw, err := reader.ReadFile("exd/root.exl")
	if err != nil || exlRaw == nil {
		return append([]string(nil), CoreSheetNames...)
	}
	exl := sqpack.ReadEXLFile(exlRaw)
	if len(exl.Entries) == 0 {
		return append([]string(nil), CoreSheetNames...)
	}
	names := make([]string, 0, len(exl.Entries))
	for _, e := range exl.Entries {
		if e.Name != "" {
			names = append(names, e.Name)
		}
	}

	m.mu.Lock()
	m.allSheetNames = names
	m.mu.Unlock()
	return append([]string(nil), names...)
}

// GetSheetSchema 获取指定表与 EXH 二进制结构精确对齐后的列定义。
func (m *SchemaManager) GetSheetSchema(reader *sqpack.GameReader, sheetName string) (*SheetSchemaMeta, error) {
	m.mu.RLock()
	if cached, ok := m.schemaCache[sheetName]; ok {
		m.mu.RUnlock()
		return cached, nil
	}
	m.mu.RUnlock()

	exhBuf, err := reader.ReadFile("exd/" + toLowerASCII(sheetName) + ".exh")
	if err != nil {
		return nil, err
	}
	if exhBuf == nil {
		return nil, fmt.Errorf("找不到数据表头文件: exd/%s.exh", toLowerASCII(sheetName))
	}
	exh, err := sqpack.ReadEXHHeader(exhBuf)
	if err != nil {
		return nil, err
	}

	flatNames, _ := sqpack.LoadSchemaFieldNames(m.schemasFS, sheetName)
	aligned, _ := sqpack.AlignEXHColumns(exh, flatNames)

	cols := make([]ColumnMeta, len(aligned))
	for i, c := range aligned {
		cols[i] = ColumnMeta{
			Index:  c.Index,
			Name:   c.Name,
			Type:   FormatColumnType(c.Type),
			Offset: c.Offset,
		}
	}

	meta := &SheetSchemaMeta{
		SheetName: sheetName,
		Columns:   cols,
		Variant:   exh.Variant,
	}

	m.mu.Lock()
	m.schemaCache[sheetName] = meta
	m.mu.Unlock()
	return meta, nil
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i, ch := range b {
		if ch >= 'A' && ch <= 'Z' {
			b[i] = ch + ('a' - 'A')
		}
	}
	return string(b)
}
