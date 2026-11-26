package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/backup"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

// DiffExportKind 定义版本对比支持的内容导出类型。
type DiffExportKind string

const (
	DiffExportIcons    DiffExportKind = "icons"
	DiffExportAudio    DiffExportKind = "audio"
	DiffExportCSV      DiffExportKind = "csv"
	DiffExportJSON     DiffExportKind = "json"
	DiffExportMarkdown DiffExportKind = "markdown"
)

// DiffIconNaming 控制版本对比批量导出图标时的文件命名规则。
type DiffIconNaming string

const (
	// DiffIconNamingIconID 按 6 位补零图标编号命名（例如 026290.png）。
	DiffIconNamingIconID DiffIconNaming = "icon_id"
	// DiffIconNamingEntityIDName 按条目编号与名称命名（例如物品：44099_辉光魔晶石.png）。
	DiffIconNamingEntityIDName DiffIconNaming = "id_name"
)

// DiffAudioFormat 控制版本对比批量导出音乐时的输出格式。
type DiffAudioFormat string

const (
	DiffAudioOGG DiffAudioFormat = "ogg"
	DiffAudioSCD DiffAudioFormat = "scd"
)

// DiffExportOptions 描述一次版本对比导出任务的全部参数。
type DiffExportOptions struct {
	Query           backup.VersionDiffQuery
	Kind            DiffExportKind
	Lang            i18n.Lang
	PreferHR1       bool
	IconNaming      DiffIconNaming
	IncludeOldIcons bool
	AudioFormat     DiffAudioFormat
	ToZip           bool
	OutputPath      string
}

// DiffExportProgress 汇报版本对比导出任务的实时进度与结果汇总。
type DiffExportProgress struct {
	IsRunning      bool
	IsCompleted    bool
	IsCancelled    bool
	Kind           DiffExportKind
	CurrentFile    string
	ProcessedTasks int
	TotalTasks     int
	SuccessCount   int
	SkippedCount   int
	ErrorCount     int
	Percent        int
	OutputPath     string
	ErrorMessage   string
	StartTime      time.Time
	EndTime        time.Time
}

type diffIconTask struct {
	entry     backup.DiffItemEntry
	fileName  string
	preferHR1 bool
	wantOld   bool
}

type diffAudioTask struct {
	entry       backup.DiffItemEntry
	baseName    string
	audioFormat DiffAudioFormat
}

// DiffService 负责将「版本对比」筛选出的全量差异导出为图标 PNG、音乐音频或 CSV/JSON/Markdown 清单。
type DiffService struct {
	gameSvc   *gamedata.Service
	backupSvc *backup.Service

	// 供单元测试替换底层只读客户端资源解析（正常运行时为空）
	iconResolver  func(entry backup.DiffItemEntry, region string, preferHR1 bool, wantOld bool) ([]byte, error)
	audioResolver func(filePath, region string) ([]byte, error)

	mu       sync.Mutex
	progress *DiffExportProgress
	cancelFn context.CancelFunc
}

// NewDiffService 创建版本对比导出服务。
func NewDiffService(gameSvc *gamedata.Service, backupSvc *backup.Service) *DiffService {
	return &DiffService{
		gameSvc:   gameSvc,
		backupSvc: backupSvc,
	}
}

// GetProgress 获取当前版本对比导出任务的最新进度快照。
func (s *DiffService) GetProgress() *DiffExportProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress == nil {
		return nil
	}
	cp := *s.progress
	return &cp
}

// Cancel 中断当前运行中的版本对比导出任务。
func (s *DiffService) Cancel() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress != nil && s.progress.IsRunning && s.cancelFn != nil {
		s.cancelFn()
		return true
	}
	return false
}

// DefaultDiffExportKind 根据当前选中的版本对比分类返回最合适的默认导出方式。
func DefaultDiffExportKind(entityType string) DiffExportKind {
	switch entityType {
	case "icon", "item":
		return DiffExportIcons
	case "bgm":
		return DiffExportAudio
	default:
		return DiffExportCSV
	}
}

// DiffEntitySupportsIcons 返回当前对比分类是否包含可导出的图标资源。
func DiffEntitySupportsIcons(entityType string) bool {
	switch entityType {
	case "icon", "item", "quest", "leve", "achievement", "all", "":
		return true
	default:
		return false
	}
}

// DiffEntitySupportsAudio 返回当前对比分类是否包含可导出的音乐音频。
func DiffEntitySupportsAudio(entityType string) bool {
	return entityType == "bgm"
}

// DefaultDiffIconNaming 返回指定分类导出图标时的默认命名方式。
func DefaultDiffIconNaming(entityType string) DiffIconNaming {
	if entityType == "icon" {
		return DiffIconNamingIconID
	}
	return DiffIconNamingEntityIDName
}

// DefaultDiffExportBaseName 为保存对话框生成语义清晰且跨平台安全的默认文件名（不含扩展名）。
func DefaultDiffExportBaseName(res *backup.VersionDiffResponse, entityType, filter string, kind DiffExportKind) string {
	fromTag := "old"
	toTag := "new"
	if res != nil {
		if t := sanitizeSlug(res.FromID); t != "" {
			fromTag = t
		}
		if res.ToID == backup.LiveVersionID {
			if p := sanitizeSlug(res.ToDisplayName); p != "" {
				toTag = p
			} else {
				toTag = "current"
			}
		} else if t := sanitizeSlug(res.ToID); t != "" {
			toTag = t
		}
	}

	ent := strings.TrimSpace(entityType)
	if ent == "" {
		ent = "all"
	}
	parts := []string{"xiv-diff", fromTag + "-to-" + toTag, ent}
	if flt := strings.TrimSpace(filter); flt != "" && flt != "all" {
		parts = append(parts, flt)
	}
	switch kind {
	case DiffExportIcons:
		if ent != "icon" {
			parts = append(parts, "icons")
		}
	case DiffExportAudio:
		parts = append(parts, "audio")
	}
	return strings.Join(parts, "-")
}

func sanitizeSlug(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == backup.LiveVersionID {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), ".-_")
}

var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SanitizeFileName 清理文件名中的非法路径字符、控制字符与系统保留名，同时保留完整中/日/英字符。
func SanitizeFileName(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	cleaned := strings.TrimSpace(b.String())
	cleaned = strings.TrimRight(cleaned, ". ")
	if cleaned == "" {
		return ""
	}
	// 按字符数截断过长文件名，避免在 Windows 上触发路径长度限制
	runes := []rune(cleaned)
	if len(runes) > 100 {
		cleaned = strings.TrimRight(string(runes[:100]), ". ")
	}
	if windowsReservedNames[strings.ToUpper(cleaned)] {
		cleaned += "_"
	}
	return cleaned
}

func diffEntryDisplayName(entry backup.DiffItemEntry) string {
	name := strings.TrimSpace(entry.NameCn)
	if name == "" {
		name = strings.TrimSpace(entry.NameJa)
	}
	if name == "" {
		name = strings.TrimSpace(entry.NameEn)
	}
	if entry.EntityType == "quest" && name != "" {
		if cleaned := strings.TrimSpace(gamedata.CleanQuestTitle(name)); cleaned != "" {
			name = cleaned
		}
	}
	return name
}

// FormatDiffIconFileName 按指定规则生成单条版本对比记录的图标导出文件名（含 .png 扩展名）。
func FormatDiffIconFileName(entry backup.DiffItemEntry, queryEntity string, naming DiffIconNaming, isOld bool) string {
	if queryEntity == "icon" || entry.EntityType == "icon" || naming == DiffIconNamingIconID {
		code := fmt.Sprintf("%06d", entry.IconID)
		if isOld {
			return code + "_old.png"
		}
		return code + ".png"
	}

	safeName := SanitizeFileName(diffEntryDisplayName(entry))
	prefix := strconv.Itoa(entry.ID)
	if queryEntity == "all" || queryEntity == "" {
		prefix = fmt.Sprintf("%s_%d", entry.EntityType, entry.ID)
	}
	if safeName != "" {
		return fmt.Sprintf("%s_%s.png", prefix, safeName)
	}
	return prefix + ".png"
}

func formatDiffAudioBaseName(entry backup.DiffItemEntry) string {
	name := diffEntryDisplayName(entry)
	if name == "" && entry.File != "" {
		base := path.Base(strings.ReplaceAll(entry.File, "\\", "/"))
		name = strings.TrimSuffix(base, path.Ext(base))
	}
	safeName := SanitizeFileName(name)
	if safeName != "" {
		return fmt.Sprintf("%d_%s", entry.ID, safeName)
	}
	return strconv.Itoa(entry.ID)
}

func ensureUniqueFileName(name string, used map[string]int) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	key := strings.ToLower(name)
	if _, exists := used[key]; !exists {
		used[key] = 1
		return name
	}
	for n := used[key] + 1; ; n++ {
		cand := fmt.Sprintf("%s_%d%s", stem, n, ext)
		ck := strings.ToLower(cand)
		if _, exists := used[ck]; !exists {
			used[key] = n
			used[ck] = 1
			return cand
		}
	}
}

// StartExport 在后台启动版本对比导出任务（图标批量 PNG、音乐批量 OGG/SCD 或 CSV/JSON/Markdown 清单）。
func (s *DiffService) StartExport(opts DiffExportOptions, onUpdate func(DiffExportProgress)) error {
	if s == nil || s.backupSvc == nil {
		return errors.New("版本对比服务尚未就绪")
	}
	cleanOut := filepath.Clean(strings.TrimSpace(opts.OutputPath))
	if cleanOut == "" || cleanOut == "." {
		return errors.New("请先指定有效的导出目标路径")
	}
	opts.OutputPath = cleanOut
	if opts.Lang == "" {
		opts.Lang = i18n.LangZH
	}
	if opts.Kind == "" {
		opts.Kind = DefaultDiffExportKind(opts.Query.EntityType)
	}
	if opts.IconNaming == "" {
		opts.IconNaming = DefaultDiffIconNaming(opts.Query.EntityType)
	}
	if opts.AudioFormat == "" {
		opts.AudioFormat = DiffAudioOGG
	}

	s.mu.Lock()
	if s.progress != nil && s.progress.IsRunning {
		s.mu.Unlock()
		return errors.New("当前已有版本对比导出任务正在执行中，请等待完成或先取消")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFn = cancel
	s.progress = &DiffExportProgress{
		IsRunning:  true,
		Kind:       opts.Kind,
		OutputPath: cleanOut,
		StartTime:  time.Now(),
	}
	initSnap := *s.progress
	s.mu.Unlock()

	if onUpdate != nil {
		onUpdate(initSnap)
	}

	go func() {
		defer cancel()
		s.runExport(ctx, opts, onUpdate)
	}()

	return nil
}

func (s *DiffService) runExport(ctx context.Context, opts DiffExportOptions, onUpdate func(DiffExportProgress)) {
	res, err := s.backupSvc.CollectDiffEntries(opts.Query)
	if err != nil {
		s.failProgress(err, onUpdate)
		return
	}
	if ctx.Err() != nil {
		s.cancelProgress(onUpdate)
		return
	}
	if res.NeedsDownload || len(res.Items) == 0 {
		s.failProgress(errors.New(i18n.T(opts.Lang, "diff_export.err_empty")), onUpdate)
		return
	}

	switch opts.Kind {
	case DiffExportIcons:
		s.runIconBatchExport(ctx, res, opts, onUpdate)
	case DiffExportAudio:
		s.runAudioBatchExport(ctx, res, opts, onUpdate)
	case DiffExportCSV, DiffExportJSON, DiffExportMarkdown:
		s.runReportExport(ctx, res, opts, onUpdate)
	default:
		s.failProgress(fmt.Errorf("不支持的导出类型: %s", opts.Kind), onUpdate)
	}
}

func (s *DiffService) buildIconTasks(res backup.VersionDiffResponse, opts DiffExportOptions) ([]diffIconTask, int) {
	queryEntity := opts.Query.EntityType
	if queryEntity == "" {
		queryEntity = "all"
	}
	tasks := make([]diffIconTask, 0, len(res.Items))
	usedNames := make(map[string]int, len(res.Items))
	seenIconIDs := make(map[int]bool)
	skipped := 0

	dedupByIconID := queryEntity != "icon" && opts.IconNaming == DiffIconNamingIconID

	for _, entry := range res.Items {
		if entry.EntityType == "icon" {
			if entry.IconID < 0 {
				skipped++
				continue
			}
		} else {
			if entry.IconID <= 0 {
				skipped++
				continue
			}
		}
		if dedupByIconID {
			if seenIconIDs[entry.IconID] {
				continue
			}
			seenIconIDs[entry.IconID] = true
		}

		fn := ensureUniqueFileName(FormatDiffIconFileName(entry, queryEntity, opts.IconNaming, false), usedNames)
		tasks = append(tasks, diffIconTask{
			entry:     entry,
			fileName:  fn,
			preferHR1: opts.PreferHR1,
			wantOld:   entry.EntityType == "icon" && entry.ChangeType == "removed",
		})

		if queryEntity == "icon" && opts.IncludeOldIcons && entry.ChangeType == "modified" &&
			(entry.OldBlobKey != "" || len(entry.OldThumbPNG) > 0) {
			oldFn := ensureUniqueFileName(FormatDiffIconFileName(entry, queryEntity, opts.IconNaming, true), usedNames)
			tasks = append(tasks, diffIconTask{
				entry:     entry,
				fileName:  oldFn,
				preferHR1: opts.PreferHR1,
				wantOld:   true,
			})
		}
	}
	return tasks, skipped
}

func (s *DiffService) runIconBatchExport(
	ctx context.Context,
	res backup.VersionDiffResponse,
	opts DiffExportOptions,
	onUpdate func(DiffExportProgress),
) {
	tasks, skipped := s.buildIconTasks(res, opts)
	if len(tasks) == 0 {
		s.failProgress(errors.New(i18n.T(opts.Lang, "diff_export.err_no_icons")), onUpdate)
		return
	}

	s.setTotalTasks(len(tasks), skipped, onUpdate)

	// 按 (iconID, preferHR1) 复用已解码 PNG 字节，大幅加速多个物品共用同一图标时的导出速度
	var pngCacheMu sync.Mutex
	pngCache := make(map[string][]byte, min(len(tasks), 512))

	decodeTask := func(task diffIconTask) ([]byte, error) {
		if !task.wantOld && task.entry.ChangeType != "removed" {
			cacheKey := fmt.Sprintf("%d|%v", task.entry.IconID, task.preferHR1)
			pngCacheMu.Lock()
			cached, ok := pngCache[cacheKey]
			pngCacheMu.Unlock()
			if ok {
				return cached, nil
			}
			data, err := s.resolveIconPNG(task.entry, res.Region, task.preferHR1, false)
			if err == nil && len(data) > 0 {
				pngCacheMu.Lock()
				if len(pngCache) < 2048 {
					pngCache[cacheKey] = data
				}
				pngCacheMu.Unlock()
			}
			return data, err
		}
		return s.resolveIconPNG(task.entry, res.Region, task.preferHR1, task.wantOld)
	}

	if opts.ToZip {
		s.writeBatchZip(ctx, opts.OutputPath, len(tasks), skipped, zip.Store, onUpdate, func(emit func(name string, data []byte, err error)) {
			workers := min(len(tasks), max(4, runtime.NumCPU()))
			jobCh := make(chan diffIconTask, min(len(tasks), workers*2))
			var wg sync.WaitGroup
			wg.Add(workers)
			for range workers {
				go func() {
					defer wg.Done()
					for task := range jobCh {
						if ctx.Err() != nil {
							return
						}
						data, err := decodeTask(task)
						emit(task.fileName, data, err)
					}
				}()
			}
		feedLoop:
			for _, t := range tasks {
				select {
				case <-ctx.Done():
					break feedLoop
				case jobCh <- t:
				}
			}
			close(jobCh)
			wg.Wait()
		})
		return
	}

	if err := os.MkdirAll(opts.OutputPath, 0o755); err != nil {
		s.failProgress(err, onUpdate)
		return
	}

	total := len(tasks)
	workers := min(total, max(4, runtime.NumCPU()))
	jobCh := make(chan diffIconTask, min(total, workers*4))

	var processed atomic.Int64
	var succeeded atomic.Int64
	var errCount atomic.Int64
	lastNotify := time.Now()

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for task := range jobCh {
				if ctx.Err() != nil {
					return
				}
				data, err := decodeTask(task)
				if err != nil || len(data) == 0 {
					errCount.Add(1)
				} else {
					target := filepath.Join(opts.OutputPath, task.fileName)
					if wErr := os.WriteFile(target, data, 0o644); wErr != nil {
						errCount.Add(1)
					} else {
						succeeded.Add(1)
					}
				}
				done := int(processed.Add(1))
				s.emitBatchProgress(task.fileName, done, total, int(succeeded.Load()), skipped, int(errCount.Load()), &lastNotify, onUpdate)
			}
		}()
	}

feedDirLoop:
	for _, t := range tasks {
		select {
		case <-ctx.Done():
			break feedDirLoop
		case jobCh <- t:
		}
	}
	close(jobCh)
	wg.Wait()

	if ctx.Err() != nil {
		s.cancelProgress(onUpdate)
		return
	}
	s.completeProgress(total, int(succeeded.Load()), skipped, int(errCount.Load()), onUpdate)
}

func (s *DiffService) runAudioBatchExport(
	ctx context.Context,
	res backup.VersionDiffResponse,
	opts DiffExportOptions,
	onUpdate func(DiffExportProgress),
) {
	tasks := make([]diffAudioTask, 0, len(res.Items))
	skipped := 0
	for _, entry := range res.Items {
		if entry.EntityType != "bgm" || entry.ChangeType == "removed" || strings.TrimSpace(entry.File) == "" {
			skipped++
			continue
		}
		tasks = append(tasks, diffAudioTask{
			entry:       entry,
			baseName:    formatDiffAudioBaseName(entry),
			audioFormat: opts.AudioFormat,
		})
	}
	if len(tasks) == 0 {
		s.failProgress(errors.New(i18n.T(opts.Lang, "diff_export.err_no_audio")), onUpdate)
		return
	}

	s.setTotalTasks(len(tasks), skipped, onUpdate)

	var nameMu sync.Mutex
	usedNames := make(map[string]int, len(tasks))
	allocName := func(base, ext string) string {
		nameMu.Lock()
		defer nameMu.Unlock()
		return ensureUniqueFileName(base+ext, usedNames)
	}

	decodeAudio := func(task diffAudioTask) (string, []byte, error) {
		raw, err := s.resolveAudioRaw(task.entry.File, res.Region)
		if err != nil || len(raw) == 0 {
			if err == nil {
				err = errors.New("音频文件不存在")
			}
			return task.baseName + ".ogg", nil, err
		}
		if task.audioFormat == DiffAudioOGG {
			if info, pErr := sqpack.ParseSCD(raw); pErr == nil && info.Format == sqpack.ScdFormatOggVorbis && len(info.OggData) > 0 {
				return allocName(task.baseName, ".ogg"), info.OggData, nil
			}
		}
		return allocName(task.baseName, ".scd"), raw, nil
	}

	if opts.ToZip {
		s.writeBatchZip(ctx, opts.OutputPath, len(tasks), skipped, zip.Store, onUpdate, func(emit func(name string, data []byte, err error)) {
			workers := min(len(tasks), max(2, runtime.NumCPU()/2))
			jobCh := make(chan diffAudioTask, min(len(tasks), workers*2))
			var wg sync.WaitGroup
			wg.Add(workers)
			for range workers {
				go func() {
					defer wg.Done()
					for task := range jobCh {
						if ctx.Err() != nil {
							return
						}
						fn, data, err := decodeAudio(task)
						emit(fn, data, err)
					}
				}()
			}
		feedLoop:
			for _, t := range tasks {
				select {
				case <-ctx.Done():
					break feedLoop
				case jobCh <- t:
				}
			}
			close(jobCh)
			wg.Wait()
		})
		return
	}

	if err := os.MkdirAll(opts.OutputPath, 0o755); err != nil {
		s.failProgress(err, onUpdate)
		return
	}

	total := len(tasks)
	workers := min(total, max(2, runtime.NumCPU()/2))
	jobCh := make(chan diffAudioTask, min(total, workers*2))

	var processed atomic.Int64
	var succeeded atomic.Int64
	var errCount atomic.Int64
	lastNotify := time.Now()

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for task := range jobCh {
				if ctx.Err() != nil {
					return
				}
				fn, data, err := decodeAudio(task)
				if err != nil || len(data) == 0 {
					errCount.Add(1)
				} else {
					target := filepath.Join(opts.OutputPath, fn)
					if wErr := os.WriteFile(target, data, 0o644); wErr != nil {
						errCount.Add(1)
					} else {
						succeeded.Add(1)
					}
				}
				done := int(processed.Add(1))
				s.emitBatchProgress(fn, done, total, int(succeeded.Load()), skipped, int(errCount.Load()), &lastNotify, onUpdate)
			}
		}()
	}

feedAudioLoop:
	for _, t := range tasks {
		select {
		case <-ctx.Done():
			break feedAudioLoop
		case jobCh <- t:
		}
	}
	close(jobCh)
	wg.Wait()

	if ctx.Err() != nil {
		s.cancelProgress(onUpdate)
		return
	}
	s.completeProgress(total, int(succeeded.Load()), skipped, int(errCount.Load()), onUpdate)
}

func (s *DiffService) writeBatchZip(
	ctx context.Context,
	zipPath string,
	total int,
	skipped int,
	method uint16,
	onUpdate func(DiffExportProgress),
	produce func(emit func(name string, data []byte, err error)),
) {
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		s.failProgress(err, onUpdate)
		return
	}
	outFile, err := os.Create(zipPath)
	if err != nil {
		s.failProgress(err, onUpdate)
		return
	}
	zw := zip.NewWriter(outFile)

	type itemResult struct {
		name string
		data []byte
		err  error
	}
	resCh := make(chan itemResult, 16)
	go func() {
		produce(func(name string, data []byte, err error) {
			select {
			case <-ctx.Done():
			case resCh <- itemResult{name: name, data: data, err: err}:
			}
		})
		close(resCh)
	}()

	processed := 0
	succeeded := 0
	errCount := 0
	lastNotify := time.Now()
	modTime := time.Now()
	var writeFatal error

	for res := range resCh {
		if ctx.Err() != nil {
			break
		}
		processed++
		if res.err != nil || len(res.data) == 0 {
			errCount++
		} else {
			fh := &zip.FileHeader{
				Name:     res.name,
				Method:   method,
				Modified: modTime,
			}
			w, wErr := zw.CreateHeader(fh)
			if wErr != nil {
				writeFatal = wErr
				s.Cancel()
				break
			}
			if _, wErr = w.Write(res.data); wErr != nil {
				writeFatal = wErr
				s.Cancel()
				break
			}
			succeeded++
		}
		s.emitBatchProgress(res.name, processed, total, succeeded, skipped, errCount, &lastNotify, onUpdate)
	}

	for range resCh {
	}

	if writeFatal != nil {
		_ = zw.Close()
		_ = outFile.Close()
		_ = os.Remove(zipPath)
		s.failProgress(writeFatal, onUpdate)
		return
	}
	if ctx.Err() != nil {
		_ = zw.Close()
		_ = outFile.Close()
		_ = os.Remove(zipPath)
		s.cancelProgress(onUpdate)
		return
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
	s.completeProgress(total, succeeded, skipped, errCount, onUpdate)
}

func (s *DiffService) runReportExport(
	ctx context.Context,
	res backup.VersionDiffResponse,
	opts DiffExportOptions,
	onUpdate func(DiffExportProgress),
) {
	s.setTotalTasks(len(res.Items), 0, onUpdate)
	data, err := BuildDiffReportBytes(res, opts)
	if err != nil {
		s.failProgress(err, onUpdate)
		return
	}
	if ctx.Err() != nil {
		s.cancelProgress(onUpdate)
		return
	}
	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		s.failProgress(err, onUpdate)
		return
	}
	if err := os.WriteFile(opts.OutputPath, data, 0o644); err != nil {
		s.failProgress(err, onUpdate)
		return
	}
	s.completeProgress(len(res.Items), len(res.Items), 0, 0, onUpdate)
}

// BuildDiffReportBytes 将完整版本对比结果序列化为 CSV、JSON 或 Markdown 文档字节。
func BuildDiffReportBytes(res backup.VersionDiffResponse, opts DiffExportOptions) ([]byte, error) {
	lang := opts.Lang
	if lang == "" {
		lang = i18n.LangZH
	}
	switch opts.Kind {
	case DiffExportCSV:
		return buildDiffCSV(res, opts.Query, lang)
	case DiffExportJSON:
		return buildDiffJSON(res, opts.Query)
	case DiffExportMarkdown:
		return buildDiffMarkdown(res, opts.Query, lang)
	default:
		return nil, fmt.Errorf("不支持的清单导出格式: %s", opts.Kind)
	}
}

func formatFieldChangesSummary(changes []backup.ItemFieldChange) string {
	if len(changes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(changes))
	for _, ch := range changes {
		if ch.Before != "" && ch.After != "" {
			parts = append(parts, fmt.Sprintf("%s: %s -> %s", ch.Label, ch.Before, ch.After))
		} else if ch.After != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", ch.Label, ch.After))
		} else if ch.Before != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", ch.Label, ch.Before))
		}
	}
	return strings.Join(parts, "; ")
}

func localizedEntityLabel(lang i18n.Lang, entity string) string {
	switch entity {
	case "item":
		return i18n.T(lang, "diff.entity_item")
	case "quest":
		return i18n.T(lang, "diff.entity_quest")
	case "leve":
		return i18n.T(lang, "diff.entity_leve")
	case "achievement":
		return i18n.T(lang, "diff.entity_achievement")
	case "bgm":
		return i18n.T(lang, "diff.entity_bgm")
	case "icon":
		return i18n.T(lang, "diff.entity_icon")
	case "text":
		return i18n.T(lang, "diff.entity_text")
	default:
		return i18n.T(lang, "diff.entity_all")
	}
}

func localizedChangeLabel(lang i18n.Lang, changeType string) string {
	switch changeType {
	case "added":
		return i18n.T(lang, "diff.change_added")
	case "modified":
		return i18n.T(lang, "diff.change_modified")
	case "removed":
		return i18n.T(lang, "diff.change_removed")
	default:
		return changeType
	}
}

func buildDiffCSV(res backup.VersionDiffResponse, query backup.VersionDiffQuery, lang i18n.Lang) ([]byte, error) {
	var buf bytes.Buffer
	// 写入 UTF-8 BOM 确保 Excel 打开中文/日文不乱码
	buf.WriteString("\uFEFF")
	w := csv.NewWriter(&buf)
	w.UseCRLF = true

	switch query.EntityType {
	case "icon":
		_ = w.Write([]string{"ChangeType", "IconID", "Code", "Group", "Variants", "Details"})
		for _, e := range res.Items {
			_ = w.Write([]string{
				e.ChangeType,
				strconv.Itoa(e.IconID),
				fmt.Sprintf("%06d", e.IconID),
				e.CategoryName,
				e.IconVariantSummary,
				formatFieldChangesSummary(e.Changes),
			})
		}
	case "text":
		_ = w.Write([]string{"ChangeType", "Sheet", "RowKey", "Column", "Before", "After"})
		for _, e := range res.Items {
			_ = w.Write([]string{
				e.ChangeType,
				e.TextSheet,
				gamedata.FormatTextRowKey(e.ID, e.TextSubRow),
				e.TextColName,
				e.TextBefore,
				e.TextAfter,
			})
		}
	default:
		_ = w.Write([]string{
			"EntityType", "ChangeType", "ID", "Name", "NameJa", "NameEn",
			"Category", "IconID", "ItemLevel", "EquipLevel", "File", "Details",
		})
		for _, e := range res.Items {
			iconStr := ""
			if e.IconID > 0 {
				iconStr = fmt.Sprintf("%06d", e.IconID)
			}
			_ = w.Write([]string{
				e.EntityType,
				e.ChangeType,
				strconv.Itoa(e.ID),
				diffEntryDisplayName(e),
				e.NameJa,
				e.NameEn,
				e.CategoryName,
				iconStr,
				strconv.Itoa(e.ItemLevel),
				strconv.Itoa(e.EquipLevel),
				e.File,
				formatFieldChangesSummary(e.Changes),
			})
		}
	}
	_ = lang
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type diffJSONExportDoc struct {
	ExportedAt      string                 `json:"exportedAt"`
	Region          string                 `json:"region"`
	FromID          string                 `json:"fromId"`
	ToID            string                 `json:"toId"`
	FromVersion     string                 `json:"fromVersion,omitempty"`
	ToVersion       string                 `json:"toVersion,omitempty"`
	FromDisplayName string                 `json:"fromDisplayName"`
	ToDisplayName   string                 `json:"toDisplayName"`
	EntityType      string                 `json:"entityType"`
	Filter          string                 `json:"filter"`
	Query           string                 `json:"query,omitempty"`
	TotalFiltered   int                    `json:"totalFiltered"`
	Summary         backup.DiffSummary     `json:"summary"`
	Items           []backup.DiffItemEntry `json:"items"`
}

func buildDiffJSON(res backup.VersionDiffResponse, query backup.VersionDiffQuery) ([]byte, error) {
	items := make([]backup.DiffItemEntry, len(res.Items))
	for i, e := range res.Items {
		e.OldThumbPNG = nil
		e.NewThumbPNG = nil
		items[i] = e
	}
	ent := query.EntityType
	if ent == "" {
		ent = "all"
	}
	flt := query.Filter
	if flt == "" {
		flt = "all"
	}
	doc := diffJSONExportDoc{
		ExportedAt:      time.Now().UTC().Format(time.RFC3339),
		Region:          res.Region,
		FromID:          res.FromID,
		ToID:            res.ToID,
		FromVersion:     res.FromVersion,
		ToVersion:       res.ToVersion,
		FromDisplayName: res.FromDisplayName,
		ToDisplayName:   res.ToDisplayName,
		EntityType:      ent,
		Filter:          flt,
		Query:           strings.TrimSpace(query.Q),
		TotalFiltered:   res.TotalFiltered,
		Summary:         res.Summary,
		Items:           items,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func escapeMDCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "<br>")
	s = strings.ReplaceAll(s, "\n", "<br>")
	s = strings.ReplaceAll(s, "\r", "<br>")
	s = strings.ReplaceAll(s, "|", `\|`)
	return s
}

func buildDiffMarkdown(res backup.VersionDiffResponse, query backup.VersionDiffQuery, lang i18n.Lang) ([]byte, error) {
	var b strings.Builder
	fromLabel := res.FromDisplayName
	if fromLabel == "" {
		fromLabel = res.FromID
	}
	toLabel := res.ToDisplayName
	if toLabel == "" {
		toLabel = res.ToID
	}

	b.WriteString(fmt.Sprintf("# %s: %s → %s\n\n", i18n.T(lang, "diff.title"), fromLabel, toLabel))
	b.WriteString(fmt.Sprintf("- %s: %s\n", i18n.T(lang, "diff_export.md_scope"), localizedEntityLabel(lang, query.EntityType)))
	flt := query.Filter
	if flt == "" {
		flt = "all"
	}
	b.WriteString(fmt.Sprintf("- %s: %s\n", i18n.T(lang, "diff_export.md_filter"), localizedFilterLabel(lang, flt)))
	if q := strings.TrimSpace(query.Q); q != "" {
		b.WriteString(fmt.Sprintf("- %s: `%s`\n", i18n.T(lang, "diff_export.md_keyword"), strings.ReplaceAll(q, "`", "'")))
	}
	b.WriteString(fmt.Sprintf("- %s: %d\n\n", i18n.T(lang, "diff_export.md_total"), len(res.Items)))

	switch query.EntityType {
	case "icon":
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			i18n.T(lang, "diff.col_id"),
			i18n.T(lang, "diff.col_type"),
			i18n.T(lang, "icons.meta_group"),
			i18n.T(lang, "diff.col_changes"),
		))
		b.WriteString("| --- | --- | --- | --- |\n")
		for _, e := range res.Items {
			b.WriteString(fmt.Sprintf("| `%06d` | %s | `%s` | %s |\n",
				e.IconID,
				escapeMDCell(localizedChangeLabel(lang, e.ChangeType)),
				escapeMDCell(e.CategoryName),
				escapeMDCell(formatFieldChangesSummary(e.Changes)),
			))
		}
	case "text":
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			i18n.T(lang, "diff.text_col_source"),
			i18n.T(lang, "diff.text_col_type"),
			i18n.T(lang, "diff.text_before_label"),
			i18n.T(lang, "diff.text_after_label"),
		))
		b.WriteString("| --- | --- | --- | --- |\n")
		for _, e := range res.Items {
			src := fmt.Sprintf("%s #%s", e.TextSheet, gamedata.FormatTextRowKey(e.ID, e.TextSubRow))
			if e.TextColName != "" {
				src += " (" + e.TextColName + ")"
			}
			b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n",
				escapeMDCell(src),
				escapeMDCell(localizedChangeLabel(lang, e.ChangeType)),
				escapeMDCell(e.TextBefore),
				escapeMDCell(e.TextAfter),
			))
		}
	default:
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
			i18n.T(lang, "diff.col_id"),
			i18n.T(lang, "diff.col_name"),
			i18n.T(lang, "diff.col_type"),
			i18n.T(lang, "diff_export.md_category"),
			i18n.T(lang, "diff.col_changes"),
		))
		b.WriteString("| ---: | --- | --- | --- | --- |\n")
		for _, e := range res.Items {
			name := diffEntryDisplayName(e)
			if query.EntityType == "all" || query.EntityType == "" {
				name = fmt.Sprintf("[%s] %s", localizedEntityLabel(lang, e.EntityType), name)
			}
			b.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %s |\n",
				e.ID,
				escapeMDCell(name),
				escapeMDCell(localizedChangeLabel(lang, e.ChangeType)),
				escapeMDCell(e.CategoryName),
				escapeMDCell(formatFieldChangesSummary(e.Changes)),
			))
		}
	}

	return []byte(b.String()), nil
}

func localizedFilterLabel(lang i18n.Lang, filter string) string {
	switch filter {
	case "added":
		return i18n.T(lang, "diff.filter_added")
	case "modified":
		return i18n.T(lang, "diff.filter_modified")
	case "removed":
		return i18n.T(lang, "diff.filter_removed")
	default:
		return i18n.T(lang, "diff.filter_all")
	}
}

func (s *DiffService) readersForRegion(region string) []*sqpack.GameReader {
	if s.gameSvc == nil || s.gameSvc.Store() == nil {
		return nil
	}
	st := s.gameSvc.Store()
	var ordered []*sqpack.GameReader
	if snapshot.NormalizeRegion(region) == snapshot.RegionGlobal {
		ordered = []*sqpack.GameReader{st.GlobalReader, st.PrimaryReader, st.CNReader}
	} else {
		ordered = []*sqpack.GameReader{st.CNReader, st.PrimaryReader, st.GlobalReader}
	}
	out := make([]*sqpack.GameReader, 0, 3)
	for _, r := range ordered {
		if r == nil {
			continue
		}
		dup := false
		for _, prev := range out {
			if prev == r {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, r)
		}
	}
	return out
}

func (s *DiffService) iconVariantCandidates(iconID int, region string) []string {
	cands := []string{""}
	seen := map[string]bool{"": true}
	add := func(v string) {
		v = strings.Trim(strings.ToLower(strings.TrimSpace(v)), "/")
		if !seen[v] {
			seen[v] = true
			cands = append(cands, v)
		}
	}
	if s.gameSvc != nil {
		if detail := s.gameSvc.GetIconDetail(iconID); detail != nil {
			add(detail.DefaultVariant)
			if snapshot.NormalizeRegion(region) == snapshot.RegionGlobal {
				for _, lv := range []string{"ja", "en"} {
					for _, dv := range detail.Variants {
						if dv.Variant == lv {
							add(lv)
						}
					}
				}
			} else {
				for _, dv := range detail.Variants {
					if dv.Variant == "chs" {
						add("chs")
					}
				}
			}
			for _, dv := range detail.Variants {
				if dv.Variant != "hq" {
					add(dv.Variant)
				}
			}
			for _, dv := range detail.Variants {
				if dv.Variant == "hq" {
					add(dv.Variant)
				}
			}
		}
	}
	return cands
}

func (s *DiffService) resolveIconPNG(entry backup.DiffItemEntry, region string, preferHR1 bool, wantOld bool) ([]byte, error) {
	if s.iconResolver != nil {
		return s.iconResolver(entry, region, preferHR1, wantOld)
	}

	// 请求历史旧图或已移除图标时优先从快照历史缩略图还原
	if wantOld || (entry.EntityType == "icon" && entry.ChangeType == "removed") {
		if s.backupSvc != nil {
			if pngBytes, err := s.backupSvc.ReadIconBlobPNG(entry.OldBlobKey, entry.OldThumbPNG); err == nil && len(pngBytes) > 0 {
				return pngBytes, nil
			}
		}
		return nil, errors.New("找不到该历史图标的图像数据")
	}

	readers := s.readersForRegion(region)
	variants := s.iconVariantCandidates(entry.IconID, region)
	resOrder := []bool{false}
	if preferHR1 {
		resOrder = []bool{true, false}
	}

	// 按变体优先顺序尝试读取：同一变体优先高清 (_hr1)，不存在时自动回退标准清晰度
	for _, variant := range variants {
		for _, hr1 := range resOrder {
			for _, r := range readers {
				raw, _, err := sqpack.ReadIconVariantRaw(r, entry.IconID, variant, hr1)
				if err == nil && len(raw) > 0 {
					return sqpack.DecodeTexToPNGWithCompression(raw, png.BestSpeed)
				}
			}
		}
	}

	// 回退：对比两个历史快照且当前客户端无该图标时，尝试从快照附带的缩略图还原
	if s.backupSvc != nil {
		if pngBytes, err := s.backupSvc.ReadIconBlobPNG(entry.NewBlobKey, entry.NewThumbPNG); err == nil && len(pngBytes) > 0 {
			return pngBytes, nil
		}
		if entry.ChangeType == "removed" {
			if pngBytes, err := s.backupSvc.ReadIconBlobPNG(entry.OldBlobKey, entry.OldThumbPNG); err == nil && len(pngBytes) > 0 {
				return pngBytes, nil
			}
		}
	}
	return nil, fmt.Errorf("未找到图标 #%06d 的贴图资源", entry.IconID)
}

func (s *DiffService) resolveAudioRaw(filePath, region string) ([]byte, error) {
	if s.audioResolver != nil {
		return s.audioResolver(filePath, region)
	}
	for _, r := range s.readersForRegion(region) {
		raw, err := r.ReadFile(filePath)
		if err == nil && len(raw) > 0 {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("未找到音频资源: %s", filePath)
}

func (s *DiffService) setTotalTasks(total, skipped int, onUpdate func(DiffExportProgress)) {
	s.mu.Lock()
	if s.progress != nil {
		s.progress.TotalTasks = total
		s.progress.SkippedCount = skipped
		snap := *s.progress
		s.mu.Unlock()
		if onUpdate != nil {
			onUpdate(snap)
		}
		return
	}
	s.mu.Unlock()
}

func (s *DiffService) emitBatchProgress(
	file string,
	done, total, succeeded, skipped, errs int,
	lastNotify *time.Time,
	onUpdate func(DiffExportProgress),
) {
	s.mu.Lock()
	if s.progress == nil {
		s.mu.Unlock()
		return
	}
	if done < s.progress.ProcessedTasks {
		done = s.progress.ProcessedTasks
	} else {
		s.progress.CurrentFile = file
		s.progress.ProcessedTasks = done
		if total > 0 {
			s.progress.Percent = (done * 100) / total
		}
	}
	s.progress.SuccessCount = succeeded
	s.progress.SkippedCount = skipped
	s.progress.ErrorCount = errs
	shouldEmit := time.Since(*lastNotify) >= 120*time.Millisecond || done == total
	if shouldEmit {
		*lastNotify = time.Now()
	}
	snap := *s.progress
	s.mu.Unlock()

	if shouldEmit && onUpdate != nil {
		onUpdate(snap)
	}
}

func (s *DiffService) completeProgress(total, succeeded, skipped, errs int, onUpdate func(DiffExportProgress)) {
	s.mu.Lock()
	if s.progress != nil {
		s.progress.IsRunning = false
		s.progress.IsCompleted = true
		s.progress.ProcessedTasks = total
		s.progress.TotalTasks = total
		s.progress.SuccessCount = succeeded
		s.progress.SkippedCount = skipped
		s.progress.ErrorCount = errs
		s.progress.Percent = 100
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

func (s *DiffService) cancelProgress(onUpdate func(DiffExportProgress)) {
	s.mu.Lock()
	if s.progress != nil {
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
	s.mu.Unlock()
}

func (s *DiffService) failProgress(err error, onUpdate func(DiffExportProgress)) {
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
