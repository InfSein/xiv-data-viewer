package snapshot

import (
	"fmt"
	"strings"
)

const (
	// DefaultRepo 是公开托管版本快照与目录清单的 GitHub 仓库。
	DefaultRepo = "InfSein/xiv-data-snapshots"
	// DefaultCatalogURL 是默认版本快照目录清单地址（直连 GitHub Raw，不消耗 API 额度）。
	DefaultCatalogURL = "https://raw.githubusercontent.com/InfSein/xiv-data-snapshots/main/catalog.json"
	// DefaultReleaseDownloadBase 是默认 GitHub Release 资源下载前缀。
	DefaultReleaseDownloadBase = "https://github.com/InfSein/xiv-data-snapshots/releases/download"

	// FormatVersion 表示当前 .xivsnap 归档格式版本号。
	FormatVersion = 1
	// CatalogSchemaVersion 表示当前 catalog.json 模式版本号。
	CatalogSchemaVersion = 1
	// ArchiveExt 是快照文件统一后缀名。
	ArchiveExt = ".xivsnap"

	// RegionCN 表示国服数据快照。
	RegionCN = "cn"
	// RegionGlobal 表示国际服数据快照（含日/英双语）。
	RegionGlobal = "global"

	metaBGMPath        = "meta/bgm_digests.json"
	metaIconDigestPath = "meta/icon_digests.bin"
	metaIconThumbsPath = "meta/icon_thumbs.bin"
)

// NormalizeRegion 规范化区服标识为 "cn" 或 "global"。
func NormalizeRegion(region string) string {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "global", "gl", "intl", "jp", "en":
		return RegionGlobal
	default:
		return RegionCN
	}
}

// MakeSnapshotID 生成单个区服子快照的内部唯一标识（例如 "cn-7.58" / "global-7.58"）。
func MakeSnapshotID(region, patch string) string {
	r := NormalizeRegion(region)
	p := strings.TrimSpace(patch)
	return r + "-" + p
}

// MakeReleaseTag 生成统一版本在 GitHub Release 中的标签名（例如 "v7.58"）。
func MakeReleaseTag(patch string) string {
	p := strings.TrimPrefix(strings.TrimSpace(patch), "v")
	return "v" + p
}

// AssetFileName 返回指定版本和区服在 GitHub Release 中的标准资产文件名（例如 "xivsnap-7.58-cn.xivsnap"）。
func AssetFileName(patch, region string) string {
	p := strings.TrimPrefix(strings.TrimSpace(patch), "v")
	r := NormalizeRegion(region)
	return fmt.Sprintf("xivsnap-%s-%s%s", p, r, ArchiveExt)
}

// SnapshotStats 记录快照中包含的各类数据规模摘要。
type SnapshotStats struct {
	SheetCount         int `json:"sheetCount"`
	EXDFileCount       int `json:"exdFileCount"`
	SchemaCount        int `json:"schemaCount"`
	BGMCount           int `json:"bgmCount"`
	IconCount          int `json:"iconCount"`
	PrevIconThumbCount int `json:"prevIconThumbCount,omitempty"`
}

// SnapshotMeta 保存单个 .xivsnap 快照的完整元信息。
type SnapshotMeta struct {
	FormatVersion  int           `json:"formatVersion"`
	ID             string        `json:"id"`
	Region         string        `json:"region"`
	Patch          string        `json:"patch"`
	GameVersion    string        `json:"gameVersion"`
	Languages      []string      `json:"languages"`
	CreatedAt      string        `json:"createdAt"`
	PrevSnapshotID string        `json:"prevSnapshotId,omitempty"`
	Stats          SnapshotStats `json:"stats"`
}

// RegionAsset 描述某个统一游戏版本下单个区服（国服或国际服）对应的 .xivsnap 下载资产。
type RegionAsset struct {
	GameVersion    string        `json:"gameVersion"`
	Languages      []string      `json:"languages"`
	CreatedAt      string        `json:"createdAt"`
	AssetName      string        `json:"assetName"`
	DownloadURL    string        `json:"downloadUrl"`
	Size           int64         `json:"size"`
	SHA256         string        `json:"sha256"`
	PrevSnapshotID string        `json:"prevSnapshotId,omitempty"`
	Stats          SnapshotStats `json:"stats"`
}

// VersionCatalogEntry 描述 GitHub catalog.json 中单个面向用户的统一游戏版本（例如 "7.58"），
// 内部按区服挂载国服（cn）与国际服（global）数据资产，用户下载时无需手工区分区服。
type VersionCatalogEntry struct {
	Patch      string       `json:"patch"`
	CreatedAt  string       `json:"createdAt"`
	ReleaseTag string       `json:"releaseTag"`
	CN         *RegionAsset `json:"cn,omitempty"`
	Global     *RegionAsset `json:"global,omitempty"`
}

// AssetForRegion 返回指定区服的下载资产（不存在时返回 nil）。
func (e VersionCatalogEntry) AssetForRegion(region string) *RegionAsset {
	if NormalizeRegion(region) == RegionGlobal {
		return e.Global
	}
	return e.CN
}

// AvailableRegions 返回该版本当前在 GitHub 上已发布的区服列表。
func (e VersionCatalogEntry) AvailableRegions() []string {
	var out []string
	if e.CN != nil {
		out = append(out, RegionCN)
	}
	if e.Global != nil {
		out = append(out, RegionGlobal)
	}
	return out
}

// TotalSizeForRegions 计算下载目标区服集合所需的总字节数。
func (e VersionCatalogEntry) TotalSizeForRegions(regions []string) int64 {
	var total int64
	seen := make(map[string]bool, 2)
	for _, r := range regions {
		nr := NormalizeRegion(r)
		if seen[nr] {
			continue
		}
		seen[nr] = true
		if a := e.AssetForRegion(nr); a != nil {
			total += a.Size
		}
	}
	return total
}

// Catalog 描述托管在 GitHub 上的全量统一版本快照目录清单。
type Catalog struct {
	SchemaVersion int                   `json:"schemaVersion"`
	UpdatedAt     string                `json:"updatedAt"`
	Versions      []VersionCatalogEntry `json:"versions"`
}

// BGMDigestRecord 描述单条 BGM（或额外音轨路径）的音频文件存在性与内容指纹。
type BGMDigestRecord struct {
	ID         int    `json:"id"`
	File       string `json:"file"`
	FileExists bool   `json:"fileExists"`
	FileSize   uint32 `json:"fileSize,omitempty"`
	ContentMD5 string `json:"contentMd5,omitempty"`
}

// IconDigestRecord 描述单个图标编号的跨版本指纹与变体概况。
type IconDigestRecord struct {
	ID        int
	Digest    string
	Variants  string
	BlobKey   string
	HasHR1    bool
	HasHQ     bool
	HasLocale bool
}

// LocalSnapshotInfo 描述已下载到本地快照目录中的单个区服 .xivsnap 文件状态。
type LocalSnapshotInfo struct {
	Meta     SnapshotMeta
	FilePath string
	Size     int64
}
