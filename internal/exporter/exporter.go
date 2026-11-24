package exporter

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

// ExportLangInfo 描述单个可导出语言的标识与所属客户端。
type ExportLangInfo struct {
	Code   string
	Label  string
	Server string // "国服" | "国际服"
	Lang   sqpack.Language
}

// AllSupportedLangs 列出全部 5 种标准导出语言。
var AllSupportedLangs = []ExportLangInfo{
	{Code: "chs", Label: "简体中文", Server: "国服", Lang: sqpack.LangChineseSimplified},
	{Code: "ja", Label: "日本語", Server: "国际服", Lang: sqpack.LangJapanese},
	{Code: "en", Label: "English", Server: "国际服", Lang: sqpack.LangEnglish},
	{Code: "de", Label: "Deutsch", Server: "国际服", Lang: sqpack.LangGerman},
	{Code: "fr", Label: "Français", Server: "国际服", Lang: sqpack.LangFrench},
}

var langEnumByCode = map[string]sqpack.Language{
	"chs": sqpack.LangChineseSimplified,
	"ja":  sqpack.LangJapanese,
	"en":  sqpack.LangEnglish,
	"de":  sqpack.LangGerman,
	"fr":  sqpack.LangFrench,
}

// FormatCSVCell 将单元格值格式化为 RFC 4180 标准 CSV 字段（文本列统一双引号包裹并转义内部引号）。
func FormatCSVCell(val any, isStringCol bool) string {
	if isStringCol {
		if val == nil {
			return `""`
		}
		var text string
		switch v := val.(type) {
		case []byte:
			text = sqpack.DecodeSeString(v)
		case string:
			text = v
		default:
			text = fmt.Sprint(v)
		}
		return `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
	}

	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case bool:
		if v {
			return "True"
		}
		return "False"
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case int:
		return strconv.Itoa(v)
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case []byte:
		text := sqpack.DecodeSeString(v)
		return `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
	case string:
		return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	default:
		return fmt.Sprint(v)
	}
}

// ExportProgress 汇报本地解包或 Zip 打包的实时进度。
type ExportProgress struct {
	IsRunning      bool
	IsCompleted    bool
	IsCancelled    bool
	CurrentSheet   string
	CurrentLang    string
	ProcessedTasks int
	TotalTasks     int
	Percent        int
	ErrorCount     int
	OutputPath     string
	ErrorMessage   string
	StartTime      time.Time
	EndTime        time.Time
}

// Service 提供 FF14 数据表全量/按表 CSV 解包（本地目录或 Zip 归档）服务。
type Service struct {
	cnReader     *sqpack.GameReader
	globalReader *sqpack.GameReader
	schemas      *SchemaManager

	mu       sync.Mutex
	progress *ExportProgress
	cancelFn context.CancelFunc
}

// NewService 创建 CSV 导出服务（支持仅配置国服、仅配置国际服或双服同时挂载）。
func NewService(cnReader, globalReader *sqpack.GameReader, schemas *SchemaManager) *Service {
	return &Service{
		cnReader:     cnReader,
		globalReader: globalReader,
		schemas:      schemas,
	}
}

// AvailableLangCodes 返回当前已挂载客户端支持的语言代码列表。
func (s *Service) AvailableLangCodes() []string {
	var out []string
	if s.cnReader != nil {
		out = append(out, "chs")
	}
	if s.globalReader != nil {
		out = append(out, "ja", "en", "de", "fr")
	}
	return out
}

// IsLangAvailable 检查指定语言是否具备对应客户端读取器。
func (s *Service) IsLangAvailable(code string) bool {
	return s.readerForLang(code) != nil
}

// TotalSheetCount 返回当前主客户端可导出的全量数据表数量。
func (s *Service) TotalSheetCount() int {
	reader := s.cnReader
	if reader == nil {
		reader = s.globalReader
	}
	return len(s.schemas.GetAllSheetNames(reader))
}

func (s *Service) readerForLang(langCode string) *sqpack.GameReader {
	if langCode == "chs" {
		return s.cnReader
	}
	return s.globalReader
}

// ExportSheetToCSV 将单张数据表导出为带 UTF-8 BOM 的 SaintCoinach 三行表头 CSV 文本。
func (s *Service) ExportSheetToCSV(sheetName, langCode string) (string, error) {
	reader := s.readerForLang(langCode)
	if reader == nil {
		return "", fmt.Errorf("未配置对应客户端，无法导出语言 %s", langCode)
	}
	preferredLang, ok := langEnumByCode[langCode]
	if !ok {
		return "", fmt.Errorf("不支持的导出语言代码: %s", langCode)
	}

	lower := toLowerASCII(sheetName)
	exhBuf, err := reader.ReadFile("exd/" + lower + ".exh")
	if err != nil {
		return "", err
	}
	if exhBuf == nil {
		return "", fmt.Errorf("找不到数据表头: exd/%s.exh", lower)
	}
	exh, err := sqpack.ReadEXHHeader(exhBuf)
	if err != nil {
		return "", err
	}
	langSuffix := sqpack.ResolveLanguageSuffix(exh, preferredLang)

	meta, err := s.schemas.GetSheetSchema(reader, sheetName)
	if err != nil {
		return "", err
	}
	cols := meta.Columns

	var sb strings.Builder
	sb.WriteString("\uFEFF")

	// 第 1 行: key,0,1,2...
	sb.WriteString("key")
	for i := range cols {
		sb.WriteByte(',')
		sb.WriteString(strconv.Itoa(i))
	}
	sb.WriteString("\r\n")

	// 第 2 行: #,FieldName...
	sb.WriteByte('#')
	for _, c := range cols {
		sb.WriteByte(',')
		sb.WriteString(c.Name)
	}
	sb.WriteString("\r\n")

	// 第 3 行: int32,type...
	sb.WriteString("int32")
	for _, c := range cols {
		sb.WriteByte(',')
		sb.WriteString(c.Type)
	}
	sb.WriteString("\r\n")

	for _, page := range exh.Paginations {
		exdPath := fmt.Sprintf("exd/%s_%d%s.exd", lower, page.StartID, langSuffix)
		exdBuf, err := reader.ReadFile(exdPath)
		if err != nil || exdBuf == nil {
			continue
		}
		exd, err := sqpack.NewEXDReader(exdBuf, exh)
		if err != nil {
			continue
		}
		rowIDs := exd.ListRowIDs()

		if exh.Variant == sqpack.ExcelVariantSubrows {
			for _, rowID := range rowIDs {
				subCnt, err := exd.GetSubRowCount(rowID)
				if err != nil {
					continue
				}
				for subIdx := 0; subIdx < subCnt; subIdx++ {
					sr, err := exd.ReadSubrow(rowID, subIdx, nil)
					if err != nil {
						continue
					}
					sb.WriteString(strconv.FormatUint(uint64(rowID), 10))
					sb.WriteByte('.')
					sb.WriteString(strconv.Itoa(subIdx))
					for _, c := range cols {
						sb.WriteByte(',')
						var val any
						if c.Index < len(sr.Data) {
							val = sr.Data[c.Index]
						}
						sb.WriteString(FormatCSVCell(val, c.Type == "str"))
					}
					sb.WriteString("\r\n")
				}
			}
		} else {
			for _, rowID := range rowIDs {
				rowData, err := exd.ReadRow(rowID, nil)
				if err != nil {
					continue
				}
				sb.WriteString(strconv.FormatUint(uint64(rowID), 10))
				for _, c := range cols {
					sb.WriteByte(',')
					var val any
					if c.Index < len(rowData) {
						val = rowData[c.Index]
					}
					sb.WriteString(FormatCSVCell(val, c.Type == "str"))
				}
				sb.WriteString("\r\n")
			}
		}
	}

	return sb.String(), nil
}

// GetProgress 获取当前后台导出任务的最新进度快照。
func (s *Service) GetProgress() *ExportProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress == nil {
		return nil
	}
	cp := *s.progress
	return &cp
}

// Cancel 中断当前运行中的导出任务。
func (s *Service) Cancel() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress != nil && s.progress.IsRunning && s.cancelFn != nil {
		s.cancelFn()
		return true
	}
	return false
}

// StartUnpackToLocalDirectory 清理目标目录下对应旧语言文件夹，并在后台全量解包写出 CSV 文件。
func (s *Service) StartUnpackToLocalDirectory(outputDir string, langs []string, sheets []string, onUpdate func(ExportProgress)) error {
	cleanDir := filepath.Clean(strings.TrimSpace(outputDir))
	if cleanDir == "" || cleanDir == "." {
		return errors.New("请先指定有效的本地输出目录")
	}

	validLangs := make([]string, 0, len(langs))
	for _, l := range langs {
		if s.IsLangAvailable(l) {
			validLangs = append(validLangs, l)
		}
	}
	if len(validLangs) == 0 {
		return errors.New("所选语言均未配置对应客户端，请至少选择一种可用语言")
	}

	s.mu.Lock()
	if s.progress != nil && s.progress.IsRunning {
		s.mu.Unlock()
		return errors.New("当前已有导出任务正在执行中，请等待完成或先取消")
	}

	if err := os.MkdirAll(cleanDir, 0o755); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	// 1. 扫描并清空旧语言子目录
	for _, lang := range validLangs {
		langDir := filepath.Join(cleanDir, lang)
		if err := os.RemoveAll(langDir); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("清理旧语言目录失败 (%s): %w", langDir, err)
		}
		if err := os.MkdirAll(langDir, 0o755); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("创建语言目录失败 (%s): %w", langDir, err)
		}
	}

	targetSheets := sheets
	if len(targetSheets) == 0 {
		primary := s.cnReader
		if primary == nil {
			primary = s.globalReader
		}
		targetSheets = s.schemas.GetAllSheetNames(primary)
	}
	totalTasks := len(targetSheets) * len(validLangs)

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFn = cancel
	s.progress = &ExportProgress{
		IsRunning:  true,
		TotalTasks: totalTasks,
		OutputPath: cleanDir,
		StartTime:  time.Now(),
	}
	initSnap := *s.progress
	s.mu.Unlock()

	if onUpdate != nil {
		onUpdate(initSnap)
	}

	go func() {
		defer cancel()
		processed := 0
		errCount := 0
		lastNotify := time.Now()

		for _, sheet := range targetSheets {
			for _, lang := range validLangs {
				if ctx.Err() != nil {
					s.mu.Lock()
					s.progress.IsRunning = false
					s.progress.IsCancelled = true
					s.progress.EndTime = time.Now()
					snap := *s.progress
					s.mu.Unlock()
					if onUpdate != nil {
						onUpdate(snap)
					}
					return
				}

				processed++
				csvStr, err := s.ExportSheetToCSV(sheet, lang)
				if err != nil {
					errCount++
				} else {
					targetFile := filepath.Join(cleanDir, lang, filepath.FromSlash(sheet)+".csv")
					if mkErr := os.MkdirAll(filepath.Dir(targetFile), 0o755); mkErr != nil {
						errCount++
					} else if wErr := os.WriteFile(targetFile, []byte(csvStr), 0o644); wErr != nil {
						errCount++
					}
				}

				pct := 0
				if totalTasks > 0 {
					pct = (processed * 100) / totalTasks
				}
				s.mu.Lock()
				s.progress.CurrentSheet = sheet
				s.progress.CurrentLang = lang
				s.progress.ProcessedTasks = processed
				s.progress.Percent = pct
				s.progress.ErrorCount = errCount
				snap := *s.progress
				s.mu.Unlock()

				if onUpdate != nil && (time.Since(lastNotify) >= 120*time.Millisecond || processed == totalTasks) {
					lastNotify = time.Now()
					onUpdate(snap)
				}
			}
		}

		s.mu.Lock()
		s.progress.IsRunning = false
		s.progress.IsCompleted = true
		s.progress.Percent = 100
		s.progress.EndTime = time.Now()
		snap := *s.progress
		s.mu.Unlock()
		if onUpdate != nil {
			onUpdate(snap)
		}
	}()

	return nil
}

// StartExportToZipFile 在后台将选中语言的 CSV 全量或按表打包写入指定 .zip 文件路径。
func (s *Service) StartExportToZipFile(zipPath string, langs []string, sheets []string, onUpdate func(ExportProgress)) error {
	cleanPath := filepath.Clean(strings.TrimSpace(zipPath))
	if cleanPath == "" || cleanPath == "." {
		return errors.New("请先指定有效的 Zip 保存路径")
	}

	validLangs := make([]string, 0, len(langs))
	for _, l := range langs {
		if s.IsLangAvailable(l) {
			validLangs = append(validLangs, l)
		}
	}
	if len(validLangs) == 0 {
		return errors.New("所选语言均未配置对应客户端")
	}

	s.mu.Lock()
	if s.progress != nil && s.progress.IsRunning {
		s.mu.Unlock()
		return errors.New("当前已有导出任务正在执行中，请等待完成或先取消")
	}

	targetSheets := sheets
	if len(targetSheets) == 0 {
		primary := s.cnReader
		if primary == nil {
			primary = s.globalReader
		}
		targetSheets = s.schemas.GetAllSheetNames(primary)
	}
	totalTasks := len(targetSheets) * len(validLangs)

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFn = cancel
	s.progress = &ExportProgress{
		IsRunning:  true,
		TotalTasks: totalTasks,
		OutputPath: cleanPath,
		StartTime:  time.Now(),
	}
	initSnap := *s.progress
	s.mu.Unlock()

	if onUpdate != nil {
		onUpdate(initSnap)
	}

	go func() {
		defer cancel()
		if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
			s.failProgress(err, onUpdate)
			return
		}
		outFile, err := os.Create(cleanPath)
		if err != nil {
			s.failProgress(err, onUpdate)
			return
		}
		zw := zip.NewWriter(outFile)

		processed := 0
		errCount := 0
		lastNotify := time.Now()

		for _, sheet := range targetSheets {
			for _, lang := range validLangs {
				if ctx.Err() != nil {
					_ = zw.Close()
					_ = outFile.Close()
					_ = os.Remove(cleanPath)
					s.mu.Lock()
					s.progress.IsRunning = false
					s.progress.IsCancelled = true
					s.progress.EndTime = time.Now()
					snap := *s.progress
					s.mu.Unlock()
					if onUpdate != nil {
						onUpdate(snap)
					}
					return
				}

				processed++
				csvStr, expErr := s.ExportSheetToCSV(sheet, lang)
				if expErr != nil {
					errCount++
				} else {
					w, wErr := zw.Create(fmt.Sprintf("%s/%s.csv", lang, sheet))
					if wErr != nil {
						errCount++
					} else if _, writeErr := w.Write([]byte(csvStr)); writeErr != nil {
						errCount++
					}
				}

				pct := 0
				if totalTasks > 0 {
					pct = (processed * 100) / totalTasks
				}
				s.mu.Lock()
				s.progress.CurrentSheet = sheet
				s.progress.CurrentLang = lang
				s.progress.ProcessedTasks = processed
				s.progress.Percent = pct
				s.progress.ErrorCount = errCount
				snap := *s.progress
				s.mu.Unlock()

				if onUpdate != nil && (time.Since(lastNotify) >= 120*time.Millisecond || processed == totalTasks) {
					lastNotify = time.Now()
					onUpdate(snap)
				}
			}
		}

		if err := zw.Close(); err != nil {
			_ = outFile.Close()
			s.failProgress(err, onUpdate)
			return
		}
		if err := outFile.Close(); err != nil {
			s.failProgress(err, onUpdate)
			return
		}

		s.mu.Lock()
		s.progress.IsRunning = false
		s.progress.IsCompleted = true
		s.progress.Percent = 100
		s.progress.EndTime = time.Now()
		snap := *s.progress
		s.mu.Unlock()
		if onUpdate != nil {
			onUpdate(snap)
		}
	}()

	return nil
}

func (s *Service) failProgress(err error, onUpdate func(ExportProgress)) {
	s.mu.Lock()
	if s.progress != nil {
		s.progress.IsRunning = false
		s.progress.ErrorMessage = err.Error()
		s.progress.EndTime = time.Now()
		snap := *s.progress
		s.mu.Unlock()
		if onUpdate != nil {
			onUpdate(snap)
		}
		return
	}
	s.mu.Unlock()
}
