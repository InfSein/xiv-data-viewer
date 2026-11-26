package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/netproxy"
)

const (
	catalogCacheFileName = "catalog_cache.json"
	maxCatalogBytes      = 4 << 20 // 4 MiB
	maxSnapshotBytes     = 1 << 30 // 1 GiB 安全上限
	headerTimeout        = 20 * time.Second
	stallTimeout         = 45 * time.Second
)

// ClientOptions 配置 GitHub 快照目录拉取与下载客户端。
type ClientOptions struct {
	CatalogURL string
	HTTPClient *http.Client
	UserAgent  string
}

// Client 负责从 GitHub 仓库直连（走系统代理）拉取统一版本目录清单、按需下载并校验对应区服的 .xivsnap 文件。
type Client struct {
	catalogURL string
	httpClient *http.Client
	userAgent  string
}

// NewClient 创建快照下载客户端（默认走系统代理直连 GitHub）。
func NewClient(opts ClientOptions) *Client {
	catalogURL := strings.TrimSpace(opts.CatalogURL)
	if catalogURL == "" {
		catalogURL = DefaultCatalogURL
	}
	hc := opts.HTTPClient
	if hc == nil {
		tr := netproxy.Transport().Clone()
		tr.ResponseHeaderTimeout = headerTimeout
		hc = &http.Client{Transport: tr}
	}
	ua := strings.TrimSpace(opts.UserAgent)
	if ua == "" {
		ua = "XIV-Data-Viewer"
	}
	return &Client{
		catalogURL: catalogURL,
		httpClient: hc,
		userAgent:  ua,
	}
}

// FetchCatalog 从 GitHub 拉取最新的统一版本快照目录清单；成功时顺带写入本地缓存文件。
func (c *Client) FetchCatalog(ctx context.Context, cacheDir string) (Catalog, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.catalogURL, nil)
	if err != nil {
		return Catalog{}, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Catalog{}, fmt.Errorf("连接 GitHub 获取快照目录失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Catalog{}, fmt.Errorf("获取快照目录返回异常状态码: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return Catalog{}, fmt.Errorf("读取快照目录响应失败: %w", err)
	}
	if int64(len(raw)) > maxCatalogBytes {
		return Catalog{}, errors.New("快照目录文件超出体积上限")
	}

	var cat Catalog
	if err := json.Unmarshal(raw, &cat); err != nil {
		return Catalog{}, fmt.Errorf("解析快照目录 JSON 失败: %w", err)
	}
	if cacheDir != "" {
		_ = SaveCachedCatalog(cacheDir, cat)
	}
	return cat, nil
}

// LoadCachedCatalog 读取上次成功同步并落盘的本地快照目录缓存。
func LoadCachedCatalog(cacheDir string) (Catalog, bool) {
	if strings.TrimSpace(cacheDir) == "" {
		return Catalog{}, false
	}
	raw, err := os.ReadFile(filepath.Join(cacheDir, catalogCacheFileName))
	if err != nil {
		return Catalog{}, false
	}
	var cat Catalog
	if err := json.Unmarshal(raw, &cat); err != nil {
		return Catalog{}, false
	}
	return cat, true
}

// SaveCachedCatalog 将快照目录清单缓存到本地目录以支持离线展示。
func SaveCachedCatalog(cacheDir string, cat Catalog) error {
	if strings.TrimSpace(cacheDir) == "" {
		return nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(cacheDir, catalogCacheFileName+".tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(cacheDir, catalogCacheFileName))
}

// LocalArchivePath 返回指定区服快照 ID（如 "cn-7.58"）在本地目录中的存储路径。
func LocalArchivePath(dir, id string) string {
	cleanID := sanitizeSnapshotID(id)
	return filepath.Join(dir, cleanID+ArchiveExt)
}

func sanitizeSnapshotID(id string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(id) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// DownloadVersion 按用户当前配置的区服集合，一键下载指定统一版本所需的全部未下载区服快照文件，
// 并将多文件下载进度合并为单一连续进度条。
func (c *Client) DownloadVersion(
	ctx context.Context,
	entry VersionCatalogEntry,
	regions []string,
	destDir string,
	onProgress func(downloaded, total int64),
) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	patch := strings.TrimSpace(entry.Patch)
	if patch == "" {
		return nil, errors.New("快照版本缺少版本号")
	}

	type downloadTask struct {
		snapID string
		asset  RegionAsset
	}
	seen := make(map[string]bool, 2)
	var tasks []downloadTask
	var totalBytes int64
	for _, r := range regions {
		nr := NormalizeRegion(r)
		if seen[nr] {
			continue
		}
		seen[nr] = true
		asset := entry.AssetForRegion(nr)
		if asset == nil || strings.TrimSpace(asset.DownloadURL) == "" {
			continue
		}
		snapID := MakeSnapshotID(nr, patch)
		// 已存在且可正常读取的区服文件自动跳过，避免重复下载（例如后配第二区服时仅补全缺失侧）
		existingPath := LocalArchivePath(destDir, snapID)
		if _, _, err := ReadArchiveMeta(existingPath); err == nil {
			continue
		}
		tasks = append(tasks, downloadTask{snapID: snapID, asset: *asset})
		totalBytes += asset.Size
	}

	if len(tasks) == 0 {
		// 若所需区服已全部在本地，直接返回成功；若远端完全不含所需区服则返回清晰提示
		hasAnyAvailable := false
		for _, r := range regions {
			if entry.AssetForRegion(r) != nil {
				hasAnyAvailable = true
				break
			}
		}
		if !hasAnyAvailable {
			return nil, fmt.Errorf("版本 %s 暂未发布当前客户端所需区服的数据快照", patch)
		}
		return nil, nil
	}

	var completedBytes int64
	var savedPaths []string
	if onProgress != nil {
		onProgress(0, totalBytes)
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			return savedPaths, ctx.Err()
		}
		baseDone := completedBytes
		p, err := c.DownloadRegionAsset(ctx, task.snapID, task.asset, destDir, func(down, fileTotal int64) {
			if onProgress == nil {
				return
			}
			estTotal := totalBytes
			if fileTotal > task.asset.Size {
				estTotal = totalBytes - task.asset.Size + fileTotal
			}
			onProgress(baseDone+down, max(estTotal, baseDone+down))
		})
		if err != nil {
			return savedPaths, err
		}
		savedPaths = append(savedPaths, p)
		if task.asset.Size > 0 {
			completedBytes += task.asset.Size
		} else if st, err := os.Stat(p); err == nil {
			completedBytes += st.Size()
		}
	}
	if onProgress != nil {
		onProgress(max(completedBytes, totalBytes), max(completedBytes, totalBytes))
	}
	return savedPaths, nil
}

// DownloadRegionAsset 流式下载单个区服 .xivsnap 文件，校验 SHA256 与归档完整性后原子落盘。
func (c *Client) DownloadRegionAsset(
	ctx context.Context,
	snapID string,
	asset RegionAsset,
	destDir string,
	onProgress func(downloaded, total int64),
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(snapID) == "" || strings.TrimSpace(asset.DownloadURL) == "" {
		return "", errors.New("快照条目缺少 ID 或下载地址")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("创建快照存储目录失败: %w", err)
	}

	finalPath := LocalArchivePath(destDir, snapID)
	tmpPath := finalPath + ".download.tmp"
	_ = os.Remove(tmpPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载快照失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载快照返回异常状态码: HTTP %d", resp.StatusCode)
	}
	total := resp.ContentLength
	if total <= 0 && asset.Size > 0 {
		total = asset.Size
	}

	outFile, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("创建临时下载文件失败: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = outFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	mw := io.MultiWriter(outFile, hasher)
	buf := make([]byte, 64*1024)
	var downloaded int64
	lastEmit := time.Now()
	if onProgress != nil {
		onProgress(0, total)
	}

	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		n, rErr := readWithStallGuard(ctx, resp.Body, buf, stallTimeout)
		if n > 0 {
			if downloaded+int64(n) > maxSnapshotBytes {
				return "", errors.New("下载文件超出安全体积上限")
			}
			if _, wErr := mw.Write(buf[:n]); wErr != nil {
				return "", fmt.Errorf("写入快照文件失败: %w", wErr)
			}
			downloaded += int64(n)
			if onProgress != nil && (time.Since(lastEmit) >= 80*time.Millisecond || (total > 0 && downloaded >= total)) {
				lastEmit = time.Now()
				onProgress(downloaded, total)
			}
		}
		if errors.Is(rErr, io.EOF) {
			break
		}
		if rErr != nil {
			return "", fmt.Errorf("下载传输中断: %w", rErr)
		}
	}

	if err := outFile.Close(); err != nil {
		return "", err
	}
	if onProgress != nil {
		onProgress(downloaded, max(total, downloaded))
	}

	gotSHA := hex.EncodeToString(hasher.Sum(nil))
	wantSHA := strings.ToLower(strings.TrimSpace(asset.SHA256))
	if wantSHA != "" && gotSHA != wantSHA {
		return "", errors.New("快照完整性校验失败（SHA256 不匹配）")
	}

	if _, _, err := ReadArchiveMeta(tmpPath); err != nil {
		return "", fmt.Errorf("下载的快照文件损坏或格式无效: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("保存快照文件失败: %w", err)
	}
	cleanup = false
	return finalPath, nil
}

func readWithStallGuard(ctx context.Context, r io.Reader, buf []byte, timeout time.Duration) (int, error) {
	type readRes struct {
		n   int
		err error
	}
	ch := make(chan readRes, 1)
	go func() {
		n, err := r.Read(buf)
		ch <- readRes{n: n, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
		return 0, errors.New("网络连接长时间无响应")
	}
}

// ListLocalSnapshots 扫描本地快照目录中已下载的全部有效 .xivsnap 文件。
func ListLocalSnapshots(dir string) ([]LocalSnapshotInfo, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]LocalSnapshotInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ArchiveExt) {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		meta, sz, rErr := ReadArchiveMeta(fullPath)
		if rErr != nil || meta.ID == "" {
			continue
		}
		out = append(out, LocalSnapshotInfo{
			Meta:     meta,
			FilePath: fullPath,
			Size:     sz,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Meta.GameVersion != out[j].Meta.GameVersion {
			return out[i].Meta.GameVersion > out[j].Meta.GameVersion
		}
		return out[i].Meta.ID > out[j].Meta.ID
	})
	return out, nil
}

// DeleteLocalSnapshot 删除本地已下载的单个区服快照文件。
func DeleteLocalSnapshot(dir, id string) error {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(id) == "" {
		return nil
	}
	err := os.Remove(LocalArchivePath(dir, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// DeleteLocalVersion 一并删除本地指定统一版本下的全部区服快照文件（国服与国际服）以释放磁盘空间。
func DeleteLocalVersion(dir, patch string) error {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(patch) == "" {
		return nil
	}
	var firstErr error
	for _, reg := range []string{RegionCN, RegionGlobal} {
		if err := DeleteLocalSnapshot(dir, MakeSnapshotID(reg, patch)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
