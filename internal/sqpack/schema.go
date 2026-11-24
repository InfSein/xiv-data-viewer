package sqpack

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaColumnMeta 描述 EXH 列与 EXDSchema YAML 字段对齐后的元信息。
type SchemaColumnMeta struct {
	Index  int
	Name   string
	Type   uint16
	Offset uint16
}

type schemaYAMLDoc struct {
	Name   string            `yaml:"name"`
	Fields []schemaYAMLField `yaml:"fields"`
}

type schemaYAMLField struct {
	Name   string            `yaml:"name"`
	Count  int               `yaml:"count"`
	Fields []schemaYAMLField `yaml:"fields"`
}

// LoadSchemaFieldNames 从给定的只读文件系统读取 <sheetName>.yml 并展开为一维字段名序列。
func LoadSchemaFieldNames(schemasFS fs.FS, sheetName string) ([]string, error) {
	if schemasFS == nil {
		return nil, fmt.Errorf("未提供 schemas 文件系统")
	}
	raw, err := fs.ReadFile(schemasFS, sheetName+".yml")
	if err != nil {
		return nil, err
	}
	var doc schemaYAMLDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("解析 schema %s.yml 失败: %w", sheetName, err)
	}
	var out []string
	flattenSchemaFields(doc.Fields, &out, "")
	return out, nil
}

func flattenSchemaFields(fields []schemaYAMLField, result *[]string, prefix string) {
	for _, f := range fields {
		cnt := f.Count
		if cnt <= 0 {
			cnt = 1
		}
		fullName := f.Name
		if prefix != "" {
			if f.Name != "" {
				fullName = prefix + "." + f.Name
			} else {
				fullName = prefix
			}
		}

		if len(f.Fields) > 0 {
			for c := 0; c < cnt; c++ {
				itemPrefix := fullName
				if cnt > 1 {
					itemPrefix = fmt.Sprintf("%s[%d]", fullName, c)
				}
				flattenSchemaFields(f.Fields, result, itemPrefix)
			}
		} else {
			for c := 0; c < cnt; c++ {
				if cnt > 1 {
					*result = append(*result, fmt.Sprintf("%s[%d]", fullName, c))
				} else {
					*result = append(*result, fullName)
				}
			}
		}
	}
}

// AlignEXHColumns 按字节偏移升序（同偏移按类型位顺序）将 EXH 原始列与 EXDSchema 字段名对齐。
func AlignEXHColumns(exh *EXHHeader, flatNames []string) ([]SchemaColumnMeta, map[string]int) {
	type colOrder struct {
		origIdx int
		offset  uint16
		colType uint16
	}
	sorted := make([]colOrder, len(exh.Columns))
	for i, c := range exh.Columns {
		sorted[i] = colOrder{origIdx: i, offset: c.Offset, colType: c.Type}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].offset != sorted[j].offset {
			return sorted[i].offset < sorted[j].offset
		}
		if sorted[i].colType != sorted[j].colType {
			return sorted[i].colType < sorted[j].colType
		}
		return sorted[i].origIdx < sorted[j].origIdx
	})

	nameByOrig := make(map[int]string, len(sorted))
	colIndex := make(map[string]int, len(sorted))
	for i, sc := range sorted {
		if i < len(flatNames) {
			name := flatNames[i]
			nameByOrig[sc.origIdx] = name
			if name != "" {
				if _, exists := colIndex[name]; !exists {
					colIndex[name] = sc.origIdx
				}
			}
		}
	}

	cols := make([]SchemaColumnMeta, len(exh.Columns))
	for i, c := range exh.Columns {
		cols[i] = SchemaColumnMeta{
			Index:  i,
			Name:   nameByOrig[i],
			Type:   c.Type,
			Offset: c.Offset,
		}
	}
	return cols, colIndex
}

// SchemaSheet 保存按 EXDSchema 字段名对齐后的普通数据表行数据。
type SchemaSheet struct {
	Rows map[int]Row
	Cols map[string]int
}

// Col 返回指定字段名对应的 EXH 列序号，不存在时返回 -1。
func (s *SchemaSheet) Col(name string) int {
	if s == nil {
		return -1
	}
	idx, ok := s.Cols[name]
	if !ok {
		return -1
	}
	return idx
}

// Int 按字段名安全读取整型列值。
func (s *SchemaSheet) Int(r Row, name string) int {
	idx := s.Col(name)
	if idx < 0 {
		return 0
	}
	return r.IntAt(idx)
}

// Float 按字段名安全读取浮点列值。
func (s *SchemaSheet) Float(r Row, name string) float64 {
	idx := s.Col(name)
	if idx < 0 {
		return 0
	}
	return r.FloatAt(idx)
}

// Bool 按字段名安全读取布尔列值。
func (s *SchemaSheet) Bool(r Row, name string) bool {
	idx := s.Col(name)
	if idx < 0 {
		return false
	}
	return r.BoolAt(idx)
}

// Str 按字段名安全读取解码后的 SeString 文本列值。
func (s *SchemaSheet) Str(r Row, name string) string {
	idx := s.Col(name)
	if idx < 0 {
		return ""
	}
	return r.StringAt(idx)
}

// SchemaSubrowSheet 保存按 EXDSchema 字段名对齐后的子行数据表。
type SchemaSubrowSheet struct {
	Rows map[int][]SubRow
	Cols map[string]int
}

// Col 返回指定字段名对应的 EXH 列序号，不存在时返回 -1。
func (s *SchemaSubrowSheet) Col(name string) int {
	if s == nil {
		return -1
	}
	idx, ok := s.Cols[name]
	if !ok {
		return -1
	}
	return idx
}

// Int 按字段名安全读取子行整型列值。
func (s *SchemaSubrowSheet) Int(r Row, name string) int {
	idx := s.Col(name)
	if idx < 0 {
		return 0
	}
	return r.IntAt(idx)
}

// Bool 按字段名安全读取子行布尔列值。
func (s *SchemaSubrowSheet) Bool(r Row, name string) bool {
	idx := s.Col(name)
	if idx < 0 {
		return false
	}
	return r.BoolAt(idx)
}

// Str 按字段名安全读取子行文本列值。
func (s *SchemaSubrowSheet) Str(r Row, name string) string {
	idx := s.Col(name)
	if idx < 0 {
		return ""
	}
	return r.StringAt(idx)
}

func resolveNeededColumnIndexes(colIndex map[string]int, neededFields []string) []int {
	if len(neededFields) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(neededFields))
	out := make([]int, 0, len(neededFields))
	for _, name := range neededFields {
		if idx, ok := colIndex[name]; ok {
			if _, dup := seen[idx]; !dup {
				seen[idx] = struct{}{}
				out = append(out, idx)
			}
		}
	}
	sort.Ints(out)
	return out
}

// LoadSheetWithSchema 读取普通表并按 EXDSchema YAML 自动对齐列名；neededFields 非空时仅解包所需列。
func LoadSheetWithSchema(reader *GameReader, schemasFS fs.FS, sheetName string, preferredLang Language, neededFields []string) (*SchemaSheet, error) {
	if reader == nil || schemasFS == nil {
		return &SchemaSheet{Rows: make(map[int]Row), Cols: make(map[string]int)}, nil
	}
	lower := strings.ToLower(sheetName)
	exhBuf, err := reader.ReadFile("exd/" + lower + ".exh")
	if err != nil {
		return nil, err
	}
	if exhBuf == nil {
		return nil, fmt.Errorf("未找到数据表头文件: exd/%s.exh", lower)
	}
	exh, err := ReadEXHHeader(exhBuf)
	if err != nil {
		return nil, err
	}
	flatNames, err := LoadSchemaFieldNames(schemasFS, sheetName)
	if err != nil {
		return nil, err
	}
	_, colIndex := AlignEXHColumns(exh, flatNames)
	colSlice := resolveNeededColumnIndexes(colIndex, neededFields)
	if len(neededFields) > 0 && len(colSlice) == 0 {
		return &SchemaSheet{Rows: make(map[int]Row), Cols: colIndex}, nil
	}

	rows, err := LoadSheetRows(reader, sheetName, preferredLang, colSlice)
	if err != nil {
		return nil, err
	}
	return &SchemaSheet{Rows: rows, Cols: colIndex}, nil
}

// LoadSubrowSheetWithSchema 读取子行表并按 EXDSchema YAML 自动对齐列名。
func LoadSubrowSheetWithSchema(reader *GameReader, schemasFS fs.FS, sheetName string, preferredLang Language) (*SchemaSubrowSheet, error) {
	if reader == nil || schemasFS == nil {
		return &SchemaSubrowSheet{Rows: make(map[int][]SubRow), Cols: make(map[string]int)}, nil
	}
	lower := strings.ToLower(sheetName)
	exhBuf, err := reader.ReadFile("exd/" + lower + ".exh")
	if err != nil {
		return nil, err
	}
	if exhBuf == nil {
		return nil, fmt.Errorf("未找到子行表头文件: exd/%s.exh", lower)
	}
	exh, err := ReadEXHHeader(exhBuf)
	if err != nil {
		return nil, err
	}
	flatNames, err := LoadSchemaFieldNames(schemasFS, sheetName)
	if err != nil {
		return nil, err
	}
	_, colIndex := AlignEXHColumns(exh, flatNames)

	rows, err := LoadSubrowSheet(reader, sheetName, preferredLang)
	if err != nil {
		return nil, err
	}
	return &SchemaSubrowSheet{Rows: rows, Cols: colIndex}, nil
}
