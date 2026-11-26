package exporter

import (
	"archive/zip"
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

// IconExportOptions 控制按 SaintCoinach 路径规范导出游戏图标 PNG 的范围与变体选项。
type IconExportOptions struct {
	IncludeStandard bool
	IncludeHR1      bool
	Variants        []string // 为空时默认全部可用变体
	GroupID         int      // < 0 表示导出全部图标；>= 0 表示仅导出指定 1000 号段分组
}

// IconExportProgress 汇报图标导出到本地目录或 Zip 压缩包的实时进度。
type IconExportProgress struct {
	IsRunning      bool
	IsCompleted    bool
	IsCancelled    bool
	CurrentFile    string
	ProcessedTasks int
	TotalTasks     int
	Percent        int
	ErrorCount     int
	OutputPath     string
	ErrorMessage   string
	StartTime      time.Time
	EndTime        time.Time
}

type iconExportTask struct {
	iconID  int
	variant string
	hr1     bool
	relPath string
	reader  *sqpack.GameReader
}

// IconService 提供遵循 SaintCoinach 目录结构与 PNG 格式的并发图标导出能力。
type IconService struct {
	gameSvc *gamedata.Service

	mu       sync.Mutex
	progress *IconExportProgress
	cancelFn context.CancelFunc
}

// NewIconService 基于游戏数据服务创建图标批量导出器。
func NewIconService(gameSvc *gamedata.Service) *IconService {
	return &IconService{gameSvc: gameSvc}
}

// GetProgress 获取当前图标导出任务的最新进度快照。
func (s *IconService) GetProgress() *IconExportProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress == nil {
		return nil
	}
	cp := *s.progress
	return &cp
}

// Cancel 中断当前运行中的图标导出任务。
func (s *IconService) Cancel() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress != nil && s.progress.IsRunning && s.cancelFn != nil {
		s.cancelFn()
		return true
	}
	return false
}

// CountMatchingTasks 统计符合给定导出选项的 PNG 导出文件总数，用于界面实时预览规模。
func (s *IconService) CountMatchingTasks(opts IconExportOptions) int {
	return len(s.collectTasks(opts))
}

func (s *IconService) collectTasks(opts IconExportOptions) []iconExportTask {
	if s == nil || s.gameSvc == nil || s.gameSvc.Store() == nil {
		return nil
	}
	if !opts.IncludeStandard && !opts.IncludeHR1 {
		return nil
	}
	store := s.gameSvc.Store()

	var sourceIcons []gamedata.IconEntry
	if opts.GroupID >= 0 {
		sourceIcons = store.IconsByGroup[sqpack.IconGroupID(opts.GroupID)]
	} else {
		sourceIcons = store.Icons
	}
	if len(sourceIcons) == 0 {
		return nil
	}

	var variantFilter map[string]bool
	if len(opts.Variants) > 0 {
		variantFilter = make(map[string]bool, len(opts.Variants))
		for _, v := range opts.Variants {
			norm := strings.Trim(strings.ToLower(strings.TrimSpace(v)), "/")
			variantFilter[norm] = true
		}
	}

	tasks := make([]iconExportTask, 0, len(sourceIcons)*2)
	for _, ic := range sourceIcons {
		for _, v := range ic.Variants {
			if variantFilter != nil && !variantFilter[v.Variant] {
				continue
			}
			if opts.IncludeStandard && v.HasStd {
				if r := s.gameSvc.ResolveIconVariantSource(ic.ID, v.Variant, false); r != nil {
					tasks = append(tasks, iconExportTask{
						iconID:  ic.ID,
						variant: v.Variant,
						hr1:     false,
						relPath: sqpack.IconExportRelPath(ic.ID, v.Variant, false),
						reader:  r,
					})
				}
			}
			if opts.IncludeHR1 && v.HasHR1 {
				if r := s.gameSvc.ResolveIconVariantSource(ic.ID, v.Variant, true); r != nil {
					tasks = append(tasks, iconExportTask{
						iconID:  ic.ID,
						variant: v.Variant,
						hr1:     true,
						relPath: sqpack.IconExportRelPath(ic.ID, v.Variant, true),
						reader:  r,
					})
				}
			}
		}
	}
	return tasks
}

// StartExportToDirectory 在后台按 SaintCoinach 目录分布将选中的图标导出为 PNG 到指定本地目录。
func (s *IconService) StartExportToDirectory(outputDir string, opts IconExportOptions, onUpdate func(IconExportProgress)) error {
	cleanDir := filepath.Clean(strings.TrimSpace(outputDir))
	if cleanDir == "" || cleanDir == "." {
		return errors.New("请先指定有效的本地输出目录")
	}

	tasks := s.collectTasks(opts)
	if len(tasks) == 0 {
		return errors.New("当前所选范围内没有可导出的图标资源")
	}

	s.mu.Lock()
	if s.progress != nil && s.progress.IsRunning {
		s.mu.Unlock()
		return errors.New("当前已有图标导出任务正在执行中，请等待完成或先取消")
	}
	if err := os.MkdirAll(cleanDir, 0o755); err != nil {
		s.mu.Unlock()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFn = cancel
	s.progress = &IconExportProgress{
		IsRunning:  true,
		TotalTasks: len(tasks),
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

		// 预创建全部不重复父目录，消除多线程写入时的重复 MkdirAll 开销
		createdDirs := make(map[string]bool, 512)
		for _, t := range tasks {
			dir := filepath.Dir(filepath.Join(cleanDir, filepath.FromSlash(t.relPath)))
			if createdDirs[dir] {
				continue
			}
			createdDirs[dir] = true
			if err := os.MkdirAll(dir, 0o755); err != nil {
				s.failProgress(err, onUpdate)
				return
			}
		}

		total := len(tasks)
		workers := min(total, max(4, runtime.NumCPU()))
		jobCh := make(chan iconExportTask, min(total, workers*4))

		var processed atomic.Int64
		var errCount atomic.Int64
		lastNotify := time.Now()

		updateProgress := func(file string, force bool) {
			done := int(processed.Load())
			errs := int(errCount.Load())

			s.mu.Lock()
			// 并发 worker 完成顺序不定，确保进度计数与百分比严格单调递增不回退
			if done < s.progress.ProcessedTasks {
				done = s.progress.ProcessedTasks
			} else {
				s.progress.CurrentFile = file
				s.progress.ProcessedTasks = done
				pct := 0
				if total > 0 {
					pct = (done * 100) / total
				}
				s.progress.Percent = pct
			}
			s.progress.ErrorCount = errs
			shouldEmit := force || time.Since(lastNotify) >= 120*time.Millisecond || done == total
			if shouldEmit {
				lastNotify = time.Now()
			}
			snap := *s.progress
			s.mu.Unlock()

			if shouldEmit && onUpdate != nil {
				onUpdate(snap)
			}
		}

		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				for task := range jobCh {
					if ctx.Err() != nil {
						return
					}
					pngBytes, err := decodeTaskToPNG(task)
					if err != nil {
						errCount.Add(1)
					} else {
						target := filepath.Join(cleanDir, filepath.FromSlash(task.relPath))
						if wErr := os.WriteFile(target, pngBytes, 0o644); wErr != nil {
							errCount.Add(1)
						}
					}
					processed.Add(1)
					updateProgress(task.relPath, false)
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

		if ctx.Err() != nil {
			s.mu.Lock()
			s.progress.IsRunning = false
			s.progress.IsCancelled = true
			s.progress.ProcessedTasks = int(processed.Load())
			s.progress.ErrorCount = int(errCount.Load())
			s.progress.EndTime = time.Now()
			snap := *s.progress
			s.mu.Unlock()
			if onUpdate != nil {
				onUpdate(snap)
			}
			return
		}

		s.mu.Lock()
		s.progress.IsRunning = false
		s.progress.IsCompleted = true
		s.progress.ProcessedTasks = total
		s.progress.Percent = 100
		s.progress.ErrorCount = int(errCount.Load())
		s.progress.EndTime = time.Now()
		snap := *s.progress
		s.mu.Unlock()
		if onUpdate != nil {
			onUpdate(snap)
		}
	}()

	return nil
}

// StartExportToZipFile 在后台并发解码图标并流式写入遵循 SaintCoinach 目录结构的 Zip 压缩包。
func (s *IconService) StartExportToZipFile(zipPath string, opts IconExportOptions, onUpdate func(IconExportProgress)) error {
	cleanPath := filepath.Clean(strings.TrimSpace(zipPath))
	if cleanPath == "" || cleanPath == "." {
		return errors.New("请先指定有效的 Zip 保存路径")
	}

	tasks := s.collectTasks(opts)
	if len(tasks) == 0 {
		return errors.New("当前所选范围内没有可导出的图标资源")
	}

	s.mu.Lock()
	if s.progress != nil && s.progress.IsRunning {
		s.mu.Unlock()
		return errors.New("当前已有图标导出任务正在执行中，请等待完成或先取消")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFn = cancel
	s.progress = &IconExportProgress{
		IsRunning:  true,
		TotalTasks: len(tasks),
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

		type encodedResult struct {
			relPath string
			data    []byte
			err     error
		}

		total := len(tasks)
		workers := min(total, max(4, runtime.NumCPU()))
		jobCh := make(chan iconExportTask, min(total, workers*2))
		resCh := make(chan encodedResult, min(total, workers*2))

		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				for task := range jobCh {
					if ctx.Err() != nil {
						return
					}
					pngBytes, decErr := decodeTaskToPNG(task)
					select {
					case <-ctx.Done():
						return
					case resCh <- encodedResult{relPath: task.relPath, data: pngBytes, err: decErr}:
					}
				}
			}()
		}

		go func() {
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
			close(resCh)
		}()

		processed := 0
		errCount := 0
		lastNotify := time.Now()
		modTime := time.Now()
		var writeFatal error

		for res := range resCh {
			if ctx.Err() != nil {
				break
			}
			processed++
			if res.err != nil {
				errCount++
			} else {
				// PNG 本身已使用 zlib 压缩，采用 Store 避免二次无效压缩，大幅提升全量打包速度
				fh := &zip.FileHeader{
					Name:     res.relPath,
					Method:   zip.Store,
					Modified: modTime,
				}
				w, wErr := zw.CreateHeader(fh)
				if wErr != nil {
					writeFatal = wErr
					cancel()
					break
				}
				if _, wErr = w.Write(res.data); wErr != nil {
					writeFatal = wErr
					cancel()
					break
				}
			}

			pct := 0
			if total > 0 {
				pct = (processed * 100) / total
			}
			s.mu.Lock()
			s.progress.CurrentFile = res.relPath
			s.progress.ProcessedTasks = processed
			s.progress.Percent = pct
			s.progress.ErrorCount = errCount
			snap := *s.progress
			s.mu.Unlock()

			if onUpdate != nil && (time.Since(lastNotify) >= 120*time.Millisecond || processed == total) {
				lastNotify = time.Now()
				onUpdate(snap)
			}
		}

		// 确保后台生产者彻底退出后再清理临时压缩包
		for range resCh {
		}

		if writeFatal != nil {
			_ = zw.Close()
			_ = outFile.Close()
			_ = os.Remove(cleanPath)
			s.failProgress(writeFatal, onUpdate)
			return
		}

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
		s.progress.ProcessedTasks = total
		s.progress.Percent = 100
		s.progress.ErrorCount = errCount
		s.progress.EndTime = time.Now()
		snap := *s.progress
		s.mu.Unlock()
		if onUpdate != nil {
			onUpdate(snap)
		}
	}()

	return nil
}

func decodeTaskToPNG(task iconExportTask) ([]byte, error) {
	raw, _, err := sqpack.ReadIconVariantRaw(task.reader, task.iconID, task.variant, task.hr1)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("图标贴图资源为空")
	}
	return sqpack.DecodeTexToPNGWithCompression(raw, png.BestSpeed)
}

func (s *IconService) failProgress(err error, onUpdate func(IconExportProgress)) {
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
