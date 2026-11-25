package updater

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/netproxy"
)

const (
	// DefaultRepo 是应用公开托管并发布 GitHub Release 的仓库路径。
	DefaultRepo = "InfSein/xiv-data-viewer"
	// DefaultAPIBaseURL 是 GitHub REST API 根地址。
	DefaultAPIBaseURL = "https://api.github.com"
	// DefaultWebBaseURL 是 GitHub 网页与 Release 下载根地址。
	DefaultWebBaseURL = "https://github.com"

	defaultAppSlug    = "xiv-data-viewer"
	defaultExeName    = "XIV Data Viewer.exe"
	checksumsAsset    = "SHA256SUMS.txt"
	maxManifestBytes  = 2 << 20   // 2 MiB
	maxChecksumsBytes = 1 << 20   // 1 MiB
	maxArchiveBytes   = 256 << 20 // 256 MiB
	maxExtractedBytes = 512 << 20 // 512 MiB
	headerTimeout     = 20 * time.Second
	stallTimeout      = 45 * time.Second
)

// SelfUpdateDisableReason 描述当前运行环境无法自动替换自身可执行文件的原因。
type SelfUpdateDisableReason string

const (
	DisableReasonNone       SelfUpdateDisableReason = ""
	DisableReasonDev        SelfUpdateDisableReason = "dev"
	DisableReasonPlatform   SelfUpdateDisableReason = "platform"
	DisableReasonPath       SelfUpdateDisableReason = "path"
	DisableReasonPermission SelfUpdateDisableReason = "permission"
)

// ReleaseInfo 描述单个 GitHub Release 及其适配当前平台的更新包信息。
type ReleaseInfo struct {
	Version      string
	TagName      string
	Name         string
	Notes        string
	HTMLURL      string
	PublishedAt  time.Time
	AssetName    string
	AssetURL     string
	AssetSize    int64
	AssetSHA256  string
	ChecksumsURL string
}

// CheckResult 表示一次检查更新的比对结果。
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	HasUpdate      bool
	Release        *ReleaseInfo
}

// InstallPhase 标识下载与替换的当前阶段。
type InstallPhase string

const (
	PhaseDownloading InstallPhase = "downloading"
	PhaseVerifying   InstallPhase = "verifying"
	PhaseExtracting  InstallPhase = "extracting"
	PhaseReplacing   InstallPhase = "replacing"
)

// Progress 表示更新包下载与安装实时进度。
type Progress struct {
	Phase      InstallPhase
	Downloaded int64
	Total      int64
}

// Options 配置 Client 行为（未设置字段自动回退到生产环境默认值）。
type Options struct {
	Repo           string
	AppSlug        string
	CurrentVersion string
	OS             string
	Arch           string
	ExePath        string
	IsDev          *bool
	HTTPClient     *http.Client
	APIBaseURL     string
	WebBaseURL     string
	ChecksumsName  string
}

// Client 负责从 GitHub Release 检查、校验、下载并安全替换客户端文件。
type Client struct {
	repo           string
	appSlug        string
	currentVersion string
	goos           string
	goarch         string
	exePath        string
	isDev          bool
	httpClient     *http.Client
	apiBaseURL     string
	webBaseURL     string
	checksumsName  string
}

// NewClient 根据配置构造 GitHub Release 更新客户端。
func NewClient(opts Options) *Client {
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = DefaultRepo
	}
	slug := strings.TrimSpace(opts.AppSlug)
	if slug == "" {
		slug = defaultAppSlug
	}
	goos := strings.TrimSpace(opts.OS)
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := strings.TrimSpace(opts.Arch)
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	exePath := strings.TrimSpace(opts.ExePath)
	if exePath == "" {
		if p, err := os.Executable(); err == nil {
			if resolved, rErr := filepath.EvalSymlinks(p); rErr == nil {
				p = resolved
			}
			exePath = p
		}
	}
	isDev := mygo.IsDev()
	if opts.IsDev != nil {
		isDev = *opts.IsDev
	}
	apiBase := strings.TrimRight(strings.TrimSpace(opts.APIBaseURL), "/")
	if apiBase == "" {
		apiBase = DefaultAPIBaseURL
	}
	webBase := strings.TrimRight(strings.TrimSpace(opts.WebBaseURL), "/")
	if webBase == "" {
		webBase = DefaultWebBaseURL
	}
	checksumsName := strings.TrimSpace(opts.ChecksumsName)
	if checksumsName == "" {
		checksumsName = checksumsAsset
	}
	client := opts.HTTPClient
	if client == nil {
		tr := netproxy.Transport().Clone()
		tr.ResponseHeaderTimeout = headerTimeout
		client = &http.Client{Transport: tr}
	}

	return &Client{
		repo:           repo,
		appSlug:        slug,
		currentVersion: strings.TrimSpace(opts.CurrentVersion),
		goos:           goos,
		goarch:         goarch,
		exePath:        exePath,
		isDev:          isDev,
		httpClient:     client,
		apiBaseURL:     apiBase,
		webBaseURL:     webBase,
		checksumsName:  checksumsName,
	}
}

// ReleasesPageURL 返回仓库的 GitHub Releases 列表页地址。
func (c *Client) ReleasesPageURL() string {
	return c.webBaseURL + "/" + c.repo + "/releases"
}

// CurrentVersion 返回规范化后的当前版本号（不含前导 v）。
func (c *Client) CurrentVersion() string {
	return normalizeVersion(c.currentVersion)
}

// CanSelfUpdate 判断当前运行环境是否允许就地下载并替换自身文件。
func (c *Client) CanSelfUpdate() (bool, SelfUpdateDisableReason) {
	if c.isDev {
		return false, DisableReasonDev
	}
	if c.goos != "windows" {
		return false, DisableReasonPlatform
	}
	if strings.TrimSpace(c.exePath) == "" {
		return false, DisableReasonPath
	}
	dir := filepath.Dir(c.exePath)
	probe, err := os.MkdirTemp(dir, ".xiv-update-probe-")
	if err != nil {
		return false, DisableReasonPermission
	}
	_ = os.RemoveAll(probe)
	return true, DisableReasonNone
}

// Check 查询 GitHub Release 最新正式版本并与当前版本比对。
func (c *Client) Check(ctx context.Context) (CheckResult, error) {
	cur := normalizeVersion(c.currentVersion)
	rel, err := c.fetchLatestRelease(ctx)
	if err != nil {
		return CheckResult{CurrentVersion: cur}, err
	}
	hasUpdate := CompareVersions(rel.Version, cur) > 0
	return CheckResult{
		CurrentVersion: cur,
		LatestVersion:  rel.Version,
		HasUpdate:      hasUpdate,
		Release:        rel,
	}, nil
}

// DownloadAndInstall 下载更新包、校验 SHA-256、安全解包并以可回滚方式替换当前应用文件。
func (c *Client) DownloadAndInstall(ctx context.Context, rel ReleaseInfo, onProgress func(Progress)) error {
	if ok, reason := c.CanSelfUpdate(); !ok {
		return fmt.Errorf("self-update unavailable: %s", reason)
	}
	if strings.TrimSpace(rel.AssetURL) == "" {
		return errors.New("release asset URL is empty")
	}

	targetDir := filepath.Dir(c.exePath)
	workDir, err := os.MkdirTemp(targetDir, ".xiv-update-")
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(workDir)
	}()

	expectedSHA := strings.ToLower(strings.TrimSpace(rel.AssetSHA256))
	if !isValidSHA256Hex(expectedSHA) {
		if strings.TrimSpace(rel.ChecksumsURL) == "" {
			return errors.New("missing SHA-256 checksum for release asset")
		}
		if onProgress != nil {
			onProgress(Progress{Phase: PhaseVerifying})
		}
		sums, err := c.fetchChecksums(ctx, rel.ChecksumsURL)
		if err != nil {
			return fmt.Errorf("fetch checksums: %w", err)
		}
		expectedSHA = sums[rel.AssetName]
		if !isValidSHA256Hex(expectedSHA) {
			return fmt.Errorf("checksum entry not found for %s", rel.AssetName)
		}
	}

	archivePath := filepath.Join(workDir, "update.zip")
	if err := c.downloadAndVerify(ctx, rel.AssetURL, rel.AssetSize, expectedSHA, archivePath, onProgress); err != nil {
		return err
	}

	if onProgress != nil {
		onProgress(Progress{Phase: PhaseExtracting, Downloaded: rel.AssetSize, Total: rel.AssetSize})
	}
	unpackedDir := filepath.Join(workDir, "app")
	if err := extractZipSafe(archivePath, unpackedDir); err != nil {
		return fmt.Errorf("extract archive: %w", err)
	}

	if err := prepareExtractedExecutable(unpackedDir, filepath.Base(c.exePath)); err != nil {
		return err
	}

	if onProgress != nil {
		onProgress(Progress{Phase: PhaseReplacing, Downloaded: rel.AssetSize, Total: rel.AssetSize})
	}
	if err := swapDirectoryEntries(ctx, unpackedDir, targetDir); err != nil {
		return fmt.Errorf("replace files: %w", err)
	}
	return nil
}

// CleanupLeftovers 清理上次就地更新遗留的 .old-<pid> 备份与中断残留临时目录。
func CleanupLeftovers(exePath string) {
	if strings.TrimSpace(exePath) == "" {
		if p, err := os.Executable(); err == nil {
			exePath = p
		}
	}
	if strings.TrimSpace(exePath) == "" {
		return
	}
	dir := filepath.Dir(exePath)
	curSuffix := ".old-" + strconv.Itoa(os.Getpid())

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && strings.Contains(name, ".old-") && !strings.HasSuffix(name, curSuffix) {
			_ = os.RemoveAll(filepath.Join(dir, name))
			continue
		}
		if strings.HasPrefix(name, ".xiv-update-") {
			if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > 2*time.Minute {
				_ = os.RemoveAll(filepath.Join(dir, name))
			}
		}
	}
}

type githubReleaseDTO struct {
	TagName     string           `json:"tag_name"`
	Name        string           `json:"name"`
	Body        string           `json:"body"`
	HTMLURL     string           `json:"html_url"`
	Draft       bool             `json:"draft"`
	Prerelease  bool             `json:"prerelease"`
	PublishedAt string           `json:"published_at"`
	Assets      []githubAssetDTO `json:"assets"`
}

type githubAssetDTO struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

func (c *Client) fetchLatestRelease(ctx context.Context) (*ReleaseInfo, error) {
	apiURL := c.apiBaseURL + "/repos/" + c.repo + "/releases/latest"
	body, status, err := c.httpGetBytes(ctx, apiURL, map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}, maxManifestBytes)
	if err == nil {
		var dto githubReleaseDTO
		if decErr := json.Unmarshal(body, &dto); decErr != nil {
			return nil, fmt.Errorf("parse github release json: %w", decErr)
		}
		return c.buildReleaseFromDTO(dto)
	}

	// 若触发 GitHub REST API 限流（403 / 429），回退通过网页重定向 + SHA256SUMS.txt 探测最新版本，不受 API 配额限制。
	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		if fb, fbErr := c.fetchLatestReleaseViaWebFallback(ctx); fbErr == nil {
			return fb, nil
		}
	}
	return nil, err
}

func (c *Client) buildReleaseFromDTO(dto githubReleaseDTO) (*ReleaseInfo, error) {
	if dto.Draft || dto.Prerelease {
		return nil, errors.New("latest release is draft or prerelease")
	}
	ver := normalizeVersion(dto.TagName)
	if ver == "" {
		return nil, fmt.Errorf("invalid release tag %q", dto.TagName)
	}

	expectedAsset := c.portableAssetName(ver)
	fallbackSuffix := fmt.Sprintf("-%s-%s.zip", c.goos, c.goarch)
	fallbackPrefix := c.appSlug + "-portable-"

	var matched *githubAssetDTO
	var checksumsURL string
	for i := range dto.Assets {
		a := &dto.Assets[i]
		if a.Name == c.checksumsName {
			checksumsURL = a.BrowserDownloadURL
		}
		if a.Name == expectedAsset {
			matched = a
		} else if matched == nil && strings.HasPrefix(a.Name, fallbackPrefix) && strings.HasSuffix(a.Name, fallbackSuffix) {
			matched = a
		}
	}

	if matched == nil {
		return nil, fmt.Errorf("release %s has no asset for %s/%s (%s)", dto.TagName, c.goos, c.goarch, expectedAsset)
	}

	var shaHex string
	if rest, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(matched.Digest)), "sha256:"); ok && isValidSHA256Hex(rest) {
		shaHex = rest
	}

	pubAt, _ := time.Parse(time.RFC3339, dto.PublishedAt)
	htmlURL := strings.TrimSpace(dto.HTMLURL)
	if htmlURL == "" {
		htmlURL = c.webBaseURL + "/" + c.repo + "/releases/tag/" + url.PathEscape(dto.TagName)
	}

	return &ReleaseInfo{
		Version:      ver,
		TagName:      dto.TagName,
		Name:         strings.TrimSpace(dto.Name),
		Notes:        strings.TrimSpace(dto.Body),
		HTMLURL:      htmlURL,
		PublishedAt:  pubAt,
		AssetName:    matched.Name,
		AssetURL:     matched.BrowserDownloadURL,
		AssetSize:    matched.Size,
		AssetSHA256:  shaHex,
		ChecksumsURL: checksumsURL,
	}, nil
}

func (c *Client) fetchLatestReleaseViaWebFallback(ctx context.Context) (*ReleaseInfo, error) {
	latestURL := c.webBaseURL + "/" + c.repo + "/releases/latest"
	if err := validateUpdateURL(latestURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent())

	var finalURL string
	clientCopy := *c.httpClient
	origCheck := clientCopy.CheckRedirect
	clientCopy.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		finalURL = r.URL.String()
		if origCheck != nil {
			return origCheck(r, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	resp, err := clientCopy.Do(req)
	if err != nil {
		return nil, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()

	resolved := resp.Request.URL.String()
	if finalURL != "" {
		resolved = finalURL
	}
	tag, ok := extractTagFromReleaseURL(resolved)
	if !ok {
		return nil, fmt.Errorf("cannot resolve latest release tag from %q", resolved)
	}
	ver := normalizeVersion(tag)
	if ver == "" {
		return nil, fmt.Errorf("invalid tag %q", tag)
	}

	checksumsURL := c.webBaseURL + "/" + c.repo + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(c.checksumsName)
	sums, err := c.fetchChecksums(ctx, checksumsURL)
	if err != nil {
		return nil, err
	}

	assetName := c.portableAssetName(ver)
	shaHex := sums[assetName]
	if !isValidSHA256Hex(shaHex) {
		suffix := fmt.Sprintf("-%s-%s.zip", c.goos, c.goarch)
		prefix := c.appSlug + "-portable-"
		for name, sum := range sums {
			if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
				assetName = name
				shaHex = sum
				break
			}
		}
	}
	if !isValidSHA256Hex(shaHex) {
		return nil, fmt.Errorf("checksum entry not found for %s/%s", c.goos, c.goarch)
	}

	return &ReleaseInfo{
		Version:      ver,
		TagName:      tag,
		Name:         "XIV Data Viewer " + tag,
		HTMLURL:      c.webBaseURL + "/" + c.repo + "/releases/tag/" + url.PathEscape(tag),
		AssetName:    assetName,
		AssetURL:     c.webBaseURL + "/" + c.repo + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(assetName),
		AssetSHA256:  shaHex,
		ChecksumsURL: checksumsURL,
	}, nil
}

func extractTagFromReleaseURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "tag" && parts[i+1] != "" {
			tag, err := url.PathUnescape(parts[i+1])
			if err != nil || tag == "" {
				return "", false
			}
			return tag, true
		}
	}
	return "", false
}

func (c *Client) portableAssetName(ver string) string {
	return fmt.Sprintf("%s-portable-%s-%s-%s.zip", c.appSlug, normalizeVersion(ver), c.goos, c.goarch)
}

func (c *Client) userAgent() string {
	ver := c.CurrentVersion()
	if ver == "" {
		ver = "dev"
	}
	// GitHub CDN 要求 User-Agent 仅包含 ASCII 字符。
	var b strings.Builder
	b.WriteString("xiv-data-viewer/")
	for i := 0; i < len(ver); i++ {
		if ver[i] >= 0x20 && ver[i] <= 0x7e {
			b.WriteByte(ver[i])
		}
	}
	return b.String()
}

func (c *Client) fetchChecksums(ctx context.Context, rawURL string) (map[string]string, error) {
	data, _, err := c.httpGetBytes(ctx, rawURL, nil, maxChecksumsBytes)
	if err != nil {
		return nil, err
	}
	return ParseChecksums(data), nil
}

// ParseChecksums 解析 sha256sum 输出格式文本（`<hex64>  <filename>`）为文件名到小写十六进制摘要的映射。
func ParseChecksums(data []byte) map[string]string {
	out := make(map[string]string)
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sum := strings.ToLower(strings.TrimSpace(fields[0]))
		if !isValidSHA256Hex(sum) {
			continue
		}
		idx := strings.Index(line, fields[0])
		rest := strings.TrimSpace(line[idx+len(fields[0]):])
		rest = strings.TrimPrefix(rest, "*")
		rest = strings.TrimPrefix(rest, "./")
		if rest != "" {
			out[rest] = sum
		}
	}
	return out
}

func (c *Client) httpGetBytes(ctx context.Context, rawURL string, headers map[string]string, limit int64) ([]byte, int, error) {
	if err := validateUpdateURL(rawURL); err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", c.userAgent())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d (%s)", resp.StatusCode, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(data)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("response exceeds limit (%d bytes)", limit)
	}
	return data, resp.StatusCode, nil
}

func (c *Client) downloadAndVerify(
	ctx context.Context,
	rawURL string,
	expectedSize int64,
	expectedSHA256 string,
	destPath string,
	onProgress func(Progress),
) error {
	if err := validateUpdateURL(rawURL); err != nil {
		return err
	}
	if expectedSize > maxArchiveBytes {
		return fmt.Errorf("archive size %d exceeds max limit %d", expectedSize, maxArchiveBytes)
	}

	reqCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d (%s)", resp.StatusCode, resp.Status)
	}

	total := expectedSize
	if total <= 0 && resp.ContentLength > 0 {
		total = resp.ContentLength
	}
	if total > maxArchiveBytes {
		return fmt.Errorf("archive size %d exceeds max limit %d", total, maxArchiveBytes)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	errStalled := errors.New("download stalled")
	stallTimer := time.AfterFunc(stallTimeout, func() { cancel(errStalled) })
	defer stallTimer.Stop()

	hasher := sha256.New()
	mw := io.MultiWriter(f, hasher)
	buf := make([]byte, 32*1024)
	var downloaded int64
	lastReport := time.Time{}

	if onProgress != nil {
		onProgress(Progress{Phase: PhaseDownloading, Downloaded: 0, Total: total})
	}

	for {
		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			stallTimer.Reset(stallTimeout)
			downloaded += int64(n)
			if downloaded > maxArchiveBytes {
				_ = f.Close()
				return fmt.Errorf("archive exceeded max size limit %d", maxArchiveBytes)
			}
			if _, wErr := mw.Write(buf[:n]); wErr != nil {
				_ = f.Close()
				return wErr
			}
			if onProgress != nil && (lastReport.IsZero() || time.Since(lastReport) >= 100*time.Millisecond) {
				lastReport = time.Now()
				onProgress(Progress{Phase: PhaseDownloading, Downloaded: downloaded, Total: total})
			}
		}
		if rErr == io.EOF {
			break
		}
		if rErr != nil {
			_ = f.Close()
			if errors.Is(context.Cause(reqCtx), errStalled) {
				return errStalled
			}
			return rErr
		}
	}

	if err := f.Close(); err != nil {
		return err
	}
	if onProgress != nil {
		onProgress(Progress{Phase: PhaseVerifying, Downloaded: downloaded, Total: max(total, downloaded)})
	}
	if expectedSize > 0 && downloaded != expectedSize {
		return fmt.Errorf("downloaded %d bytes, expected %d", downloaded, expectedSize)
	}

	gotSHA := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(gotSHA, expectedSHA256) {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", gotSHA, expectedSHA256)
	}
	return nil
}

func validateUpdateURL(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLoopbackHost(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("refusing non-HTTPS URL %q", rawURL)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isValidSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

func extractZipSafe(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	cleanDest := filepath.Clean(destDir)
	var totalWritten int64

	for _, f := range r.File {
		rel := filepath.Clean(filepath.FromSlash(f.Name))
		if rel == "." || rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") || filepath.VolumeName(rel) != "" {
			return fmt.Errorf("unsafe zip entry path: %q", f.Name)
		}
		target := filepath.Join(cleanDest, rel)
		if target != cleanDest && !strings.HasPrefix(target, cleanDest+string(filepath.Separator)) {
			return fmt.Errorf("zip entry escapes target dir: %q", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symlink in update archive: %q", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}

		remaining := maxExtractedBytes - totalWritten
		if remaining <= 0 {
			out.Close()
			rc.Close()
			return errors.New("extracted archive exceeds max size")
		}
		n, copyErr := io.Copy(out, io.LimitReader(rc, remaining+1))
		totalWritten += n
		closeErr := out.Close()
		rc.Close()

		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if totalWritten > maxExtractedBytes {
			return errors.New("extracted archive exceeds max size")
		}
	}
	return nil
}

func prepareExtractedExecutable(unpackedDir, targetExeName string) error {
	entries, err := os.ReadDir(unpackedDir)
	if err != nil {
		return err
	}

	var exeEntry string
	var exeCandidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.EqualFold(name, defaultExeName) {
			exeEntry = name
			break
		}
		if strings.HasSuffix(strings.ToLower(name), ".exe") {
			exeCandidates = append(exeCandidates, name)
		}
	}
	if exeEntry == "" && len(exeCandidates) == 1 {
		exeEntry = exeCandidates[0]
	}
	if exeEntry == "" {
		return errors.New("update archive does not contain main executable")
	}

	exeFullPath := filepath.Join(unpackedDir, exeEntry)
	if err := validatePEHeader(exeFullPath); err != nil {
		return err
	}

	// 若用户曾重命名主可执行文件，对齐名称确保 Relaunch 能启动新版。
	wantName := strings.TrimSpace(targetExeName)
	if wantName != "" && !strings.EqualFold(exeEntry, wantName) {
		renamed := filepath.Join(unpackedDir, wantName)
		if err := os.Rename(exeFullPath, renamed); err != nil {
			return fmt.Errorf("align executable name: %w", err)
		}
	}
	return nil
}

func validatePEHeader(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < 64 {
		return errors.New("extracted executable is too small")
	}
	var magic [2]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return err
	}
	if magic[0] != 'M' || magic[1] != 'Z' {
		return errors.New("extracted executable lacks valid PE header")
	}
	return nil
}

// swapDirectoryEntries 将 unpackedDir 中的顶层文件原子替换进 targetDir；
// 任一步骤失败均完整回滚已移动和已放置的文件，确保原程序目录不被破坏。
func swapDirectoryEntries(ctx context.Context, unpackedDir, targetDir string) error {
	entries, err := os.ReadDir(unpackedDir)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return errors.New("extracted update archive is empty")
	}

	suffix := ".old-" + strconv.Itoa(os.Getpid())
	var movedAside [][2]string // [0]=原目标路径, [1]=临时退避备份路径
	var placed [][2]string     // [0]=暂存区源路径, [1]=已放入目标路径

	rollback := func() {
		for i := len(placed) - 1; i >= 0; i-- {
			src, dst := placed[i][0], placed[i][1]
			if renameWithRetry(context.Background(), dst, src) != nil {
				_ = os.RemoveAll(dst)
			}
		}
		for i := len(movedAside) - 1; i >= 0; i-- {
			orig, aside := movedAside[i][0], movedAside[i][1]
			_ = renameWithRetry(context.Background(), aside, orig)
		}
	}

	for _, name := range names {
		src := filepath.Join(unpackedDir, name)
		dst := filepath.Join(targetDir, name)
		if _, statErr := os.Lstat(dst); statErr == nil {
			aside := filepath.Join(targetDir, "."+name+suffix)
			if err := renameWithRetry(ctx, dst, aside); err != nil {
				rollback()
				return err
			}
			movedAside = append(movedAside, [2]string{dst, aside})
		}
		if err := renameWithRetry(ctx, src, dst); err != nil {
			rollback()
			return err
		}
		placed = append(placed, [2]string{src, dst})
	}

	// 尽最大努力删除旧文件备份；Windows 上正在运行的旧 exe 重命名后仍被锁定，会在下次启动时由 CleanupLeftovers 删除。
	for _, pair := range movedAside {
		_ = os.RemoveAll(pair[1])
	}
	return nil
}

func renameWithRetry(ctx context.Context, from, to string) error {
	const maxAttempts = 6
	for attempt := 1; ; attempt++ {
		err := os.Rename(from, to)
		if err == nil || attempt >= maxAttempts || runtime.GOOS != "windows" {
			return err
		}
		// Windows 上杀毒软件或文件索引短暂占用时返回 ERROR_SHARING_VIOLATION (32) 或 ERROR_ACCESS_DENIED (5)。
		if !errors.Is(err, syscall.Errno(32)) && !errors.Is(err, syscall.Errno(5)) {
			return err
		}
		wait := time.Duration(attempt) * 100 * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// CompareVersions 按语义化版本规则比较两个版本号：a > b 返回 1，a < b 返回 -1，相等返回 0。
func CompareVersions(a, b string) int {
	a = normalizeVersion(a)
	b = normalizeVersion(b)
	if a == b {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}

	ar, apre, _ := strings.Cut(a, "-")
	br, bpre, _ := strings.Cut(b, "-")
	if c := compareDotSegments(ar, br); c != 0 {
		return c
	}
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	default:
		return compareDotSegments(apre, bpre)
	}
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	v, _, _ = strings.Cut(v, "+")
	return strings.TrimSpace(v)
}

func compareDotSegments(a, b string) int {
	af := strings.Split(a, ".")
	bf := strings.Split(b, ".")
	for i := 0; i < len(af) || i < len(bf); i++ {
		var x, y string
		if i < len(af) {
			x = af[i]
		}
		if i < len(bf) {
			y = bf[i]
		}
		if x == y {
			continue
		}
		if x == "" {
			return -1
		}
		if y == "" {
			return 1
		}
		xn, xErr := strconv.Atoi(x)
		yn, yErr := strconv.Atoi(y)
		switch {
		case xErr == nil && yErr == nil:
			if xn < yn {
				return -1
			}
			if xn > yn {
				return 1
			}
		case xErr == nil:
			return -1
		case yErr == nil:
			return 1
		default:
			return strings.Compare(x, y)
		}
	}
	return 0
}

// FormatReleaseNotes 将 GitHub Release Markdown 正文精简为原生界面易读的纯文本摘要。
func FormatReleaseNotes(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	var out []string
	prevBlank := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" || trimmed == "***" {
			continue
		}
		if trimmed == "" {
			if !prevBlank {
				out = append(out, "")
				prevBlank = true
			}
			continue
		}
		prevBlank = false
		for strings.HasPrefix(trimmed, "#") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		}
		trimmed = strings.ReplaceAll(trimmed, "**", "")
		trimmed = strings.ReplaceAll(trimmed, "`", "")
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			trimmed = "• " + strings.TrimSpace(trimmed[2:])
		}
		out = append(out, trimmed)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
