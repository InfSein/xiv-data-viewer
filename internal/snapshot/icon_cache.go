package snapshot

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/InfSein/xiv-data-viewer/internal/sqpack"
)

type blobLoc struct {
	offset int64
	length uint32
}

// IconThumbCache 维护者本机专用去重图标缩略图缓存（仅用于打包快照时提取相比上一版本发生变更或移除的旧图标）。
// 普通最终用户客户端不会创建或写入此缓存。
type IconThumbCache struct {
	dir string

	mu     sync.Mutex
	loaded bool
	index  map[string]blobLoc
}

// OpenIconThumbCache 打开或初始化维护者本机的去重图标缩略图缓存目录。
func OpenIconThumbCache(dir string) *IconThumbCache {
	return &IconThumbCache{
		dir: dir,
	}
}

func (c *IconThumbCache) idxPath() string {
	return filepath.Join(c.dir, "icon_blobs.idx")
}

func (c *IconThumbCache) datPath() string {
	return filepath.Join(c.dir, "icon_blobs.dat")
}

func (c *IconThumbCache) ensureLoadedLocked() {
	if c.loaded {
		return
	}
	c.loaded = true
	c.index = make(map[string]blobLoc, 4096)
	f, err := os.Open(c.idxPath())
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}
		off, err1 := strconv.ParseInt(parts[1], 10, 64)
		sz, err2 := strconv.ParseUint(parts[2], 10, 32)
		if err1 != nil || err2 != nil || off < 0 || sz == 0 {
			continue
		}
		c.index[parts[0]] = blobLoc{offset: off, length: uint32(sz)}
	}
}

// Has 判断指定内容键的图标缩略图是否已缓存。
func (c *IconThumbCache) Has(blobKey string) bool {
	if c == nil || c.dir == "" || blobKey == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureLoadedLocked()
	_, ok := c.index[blobKey]
	return ok
}

// Read 读取指定内容键对应的原始压缩图标缩略图载荷。
func (c *IconThumbCache) Read(blobKey string) ([]byte, bool) {
	if c == nil || c.dir == "" || blobKey == "" {
		return nil, false
	}
	c.mu.Lock()
	c.ensureLoadedLocked()
	loc, ok := c.index[blobKey]
	c.mu.Unlock()
	if !ok || loc.length == 0 {
		return nil, false
	}
	f, err := os.Open(c.datPath())
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, loc.length)
	if _, err := io.ReadFull(io.NewSectionReader(f, loc.offset, int64(loc.length)), buf); err != nil {
		return nil, false
	}
	return buf, true
}

// SyncFromTasks 将当前客户端尚未缓存的图标缩略图批量追加写入本机去重缓存，供未来打包下一版快照时回溯旧图。
func (c *IconThumbCache) SyncFromTasks(ctx context.Context, tasks []sqpack.IconBlobTask, workers int) error {
	if c == nil || c.dir == "" || len(tasks) == 0 {
		return nil
	}
	c.mu.Lock()
	c.ensureLoadedLocked()
	c.mu.Unlock()

	var datFile, idxFile *os.File
	var datWriter, idxWriter *bufio.Writer
	var datOffset int64

	ensureWriters := func() error {
		if datFile != nil {
			return nil
		}
		if err := os.MkdirAll(c.dir, 0o755); err != nil {
			return err
		}
		df, err := os.OpenFile(c.datPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		st, err := df.Stat()
		if err != nil {
			_ = df.Close()
			return err
		}
		inf, err := os.OpenFile(c.idxPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			_ = df.Close()
			return err
		}
		datFile = df
		idxFile = inf
		datOffset = st.Size()
		datWriter = bufio.NewWriterSize(df, 256*1024)
		idxWriter = bufio.NewWriterSize(inf, 64*1024)
		return nil
	}

	defer func() {
		if datWriter != nil {
			_ = datWriter.Flush()
		}
		if idxWriter != nil {
			_ = idxWriter.Flush()
		}
		if datFile != nil {
			_ = datFile.Close()
		}
		if idxFile != nil {
			_ = idxFile.Close()
		}
	}()

	return sqpack.ArchiveIconBlobs(
		ctx,
		tasks,
		c.Has,
		func(blobKey string, payload []byte) error {
			if blobKey == "" || len(payload) == 0 {
				return nil
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			c.ensureLoadedLocked()
			if _, ok := c.index[blobKey]; ok {
				return nil
			}
			if err := ensureWriters(); err != nil {
				return err
			}
			off := datOffset
			n, err := datWriter.Write(payload)
			if err != nil {
				return err
			}
			datOffset += int64(n)
			if _, err := fmt.Fprintf(idxWriter, "%s\t%d\t%d\n", blobKey, off, n); err != nil {
				return err
			}
			c.index[blobKey] = blobLoc{offset: off, length: uint32(n)}
			return nil
		},
		workers,
	)
}

// ImportFromDir 从已有旧版 icon_blobs 目录（例如原应用 backups 目录）只读导入缺失缩略图，避免首次丢图。
func (c *IconThumbCache) ImportFromDir(srcDir string) (int, error) {
	if c == nil || c.dir == "" || srcDir == "" {
		return 0, nil
	}
	src := OpenIconThumbCache(srcDir)
	src.mu.Lock()
	src.ensureLoadedLocked()
	keys := make([]string, 0, len(src.index))
	for k := range src.index {
		keys = append(keys, k)
	}
	src.mu.Unlock()
	if len(keys) == 0 {
		return 0, nil
	}

	c.mu.Lock()
	c.ensureLoadedLocked()
	c.mu.Unlock()

	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return 0, err
	}
	srcDat, err := os.Open(src.datPath())
	if err != nil {
		return 0, nil
	}
	defer func() { _ = srcDat.Close() }()

	dstDat, err := os.OpenFile(c.datPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer func() { _ = dstDat.Close() }()
	st, err := dstDat.Stat()
	if err != nil {
		return 0, err
	}
	dstIdx, err := os.OpenFile(c.idxPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer func() { _ = dstIdx.Close() }()

	bwDat := bufio.NewWriterSize(dstDat, 256*1024)
	bwIdx := bufio.NewWriterSize(dstIdx, 64*1024)
	datOff := st.Size()
	imported := 0

	for _, k := range keys {
		if c.Has(k) {
			continue
		}
		loc := src.index[k]
		if loc.length == 0 {
			continue
		}
		buf := make([]byte, loc.length)
		if _, err := io.ReadFull(io.NewSectionReader(srcDat, loc.offset, int64(loc.length)), buf); err != nil {
			continue
		}
		n, err := bwDat.Write(buf)
		if err != nil {
			return imported, err
		}
		if _, err := fmt.Fprintf(bwIdx, "%s\t%d\t%d\n", k, datOff, n); err != nil {
			return imported, err
		}
		c.mu.Lock()
		c.index[k] = blobLoc{offset: datOff, length: uint32(n)}
		c.mu.Unlock()
		datOff += int64(n)
		imported++
	}
	if err := bwDat.Flush(); err != nil {
		return imported, err
	}
	if err := bwIdx.Flush(); err != nil {
		return imported, err
	}
	return imported, nil
}
