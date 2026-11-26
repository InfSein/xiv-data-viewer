package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/snapshot"
	"github.com/klauspost/compress/zstd"
)

func main() {
	var (
		regionFlag    = flag.String("region", "all", "打包区服: cn | global | all（默认按 .env 或参数自动打包已配置的客户端）")
		patchFlag     = flag.String("patch", "", "必填：玩家可读游戏版本号（例如 7.58）")
		cnPathFlag    = flag.String("cn-path", "", "国服客户端目录（留空时从 .env 读取 FFXIV_CN_PATH）")
		glPathFlag    = flag.String("global-path", "", "国际服客户端目录（留空时从 .env 读取 FFXIV_GLOBAL_PATH）")
		envFileFlag   = flag.String("env", ".env", ".env 配置文件路径")
		schemasFlag   = flag.String("schemas", "schemas", "EXDSchema YAML 目录路径")
		outDirFlag    = flag.String("out", filepath.Join("dist", "snapshots"), "输出 .xivsnap 与清单目录")
		prevCNFlag    = flag.String("prev-cn", "", "可选：上一版国服 .xivsnap 路径，用于提取变更/移除旧图标缩略图")
		prevGLFlag    = flag.String("prev-global", "", "可选：上一版国际服 .xivsnap 路径，用于提取变更/移除旧图标缩略图")
		iconCacheFlag = flag.String("icon-cache", "", "维护者本地去重图标缓存目录（默认位于系统用户缓存目录）")
		seedBlobsFlag = flag.String("seed-blobs", "", "可选：首次运行时从旧版 backups 目录导入已有 icon_blobs.dat")
		repoFlag      = flag.String("repo", snapshot.DefaultRepo, "托管 Release 的 GitHub 快照仓库（owner/name）")
		manifestOut   = flag.String("manifest-out", "", "可选：将本次生成的统一 VersionCatalogEntry 输出为 JSON 文件")
		fastFlag      = flag.Bool("fast", false, "使用较快的 zstd 默认压缩级别（仅用于本地调试，发版请勿开启）")
	)
	flag.Parse()

	patch := strings.TrimPrefix(strings.TrimSpace(*patchFlag), "v")
	if patch == "" {
		fatalf("缺少必填参数 -patch（例如 -patch 7.58）")
	}

	envCfg := config.LoadAppConfigFromEnv(*envFileFlag)
	cnPath := strings.TrimSpace(*cnPathFlag)
	if cnPath == "" {
		cnPath = envCfg.CNPath
	}
	glPath := strings.TrimSpace(*glPathFlag)
	if glPath == "" {
		glPath = envCfg.GlobalPath
	}

	reqRegion := strings.ToLower(strings.TrimSpace(*regionFlag))
	type targetClient struct {
		region   string
		rawPath  string
		prevSnap string
	}
	var targets []targetClient
	switch reqRegion {
	case "cn":
		if cnPath == "" {
			fatalf("未配置国服客户端路径（请传 -cn-path 或在 .env 中设置 FFXIV_CN_PATH）")
		}
		targets = append(targets, targetClient{region: snapshot.RegionCN, rawPath: cnPath, prevSnap: *prevCNFlag})
	case "global":
		if glPath == "" {
			fatalf("未配置国际服客户端路径（请传 -global-path 或在 .env 中设置 FFXIV_GLOBAL_PATH）")
		}
		targets = append(targets, targetClient{region: snapshot.RegionGlobal, rawPath: glPath, prevSnap: *prevGLFlag})
	case "all":
		if cnPath != "" {
			targets = append(targets, targetClient{region: snapshot.RegionCN, rawPath: cnPath, prevSnap: *prevCNFlag})
		}
		if glPath != "" {
			targets = append(targets, targetClient{region: snapshot.RegionGlobal, rawPath: glPath, prevSnap: *prevGLFlag})
		}
		if len(targets) == 0 {
			fatalf("未在 .env 或参数中找到任何有效的 FF14 客户端路径")
		}
	default:
		fatalf("无效的 -region 参数: %q（仅支持 cn | global | all）", reqRegion)
	}

	schemasDir := strings.TrimSpace(*schemasFlag)
	if st, err := os.Stat(schemasDir); err != nil || !st.IsDir() {
		fatalf("无法访问 EXDSchema 目录 %q: %v", schemasDir, err)
	}
	schemasFS := os.DirFS(schemasDir)

	iconCacheDir := strings.TrimSpace(*iconCacheFlag)
	if iconCacheDir == "" {
		if baseCache, err := os.UserCacheDir(); err == nil && baseCache != "" {
			iconCacheDir = filepath.Join(baseCache, "xiv-data-viewer-packer", "icon-cache")
		}
	}
	if iconCacheDir != "" && strings.TrimSpace(*seedBlobsFlag) != "" {
		cache := snapshot.OpenIconThumbCache(iconCacheDir)
		n, err := cache.ImportFromDir(*seedBlobsFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[警告] 从 %s 导入旧图标缓存失败: %v\n", *seedBlobsFlag, err)
		} else if n > 0 {
			fmt.Printf("已从 %s 导入 %d 条历史图标缩略图到本地缓存。\n", *seedBlobsFlag, n)
		}
	}

	if err := os.MkdirAll(*outDirFlag, 0o755); err != nil {
		fatalf("创建输出目录失败: %v", err)
	}

	level := zstd.SpeedBestCompression
	if *fastFlag {
		level = zstd.SpeedDefault
	}
	repo := strings.TrimSpace(*repoFlag)
	if repo == "" {
		repo = snapshot.DefaultRepo
	}

	releaseTag := snapshot.MakeReleaseTag(patch)
	versionEntry := snapshot.VersionCatalogEntry{
		Patch:      patch,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		ReleaseTag: releaseTag,
	}
	var sumLines []string

	for _, tgt := range targets {
		label := "国服客户端"
		if tgt.region == snapshot.RegionGlobal {
			label = "国际服客户端"
		}
		info, err := config.ValidateClientPath(tgt.rawPath, label)
		if err != nil {
			fatalf("校验%s路径失败: %v", label, err)
		}

		assetName := snapshot.AssetFileName(patch, tgt.region)
		outPath := filepath.Join(*outDirFlag, assetName)

		fmt.Printf("\n==> 开始打包版本 %s [%s]（%s，客户端版本 %s）\n", patch, tgt.region, label, info.Version)
		start := time.Now()
		lastStage := ""
		res, err := snapshot.PackSnapshot(context.Background(), snapshot.PackOptions{
			ClientGamePath:   info.GamePath,
			Region:           tgt.region,
			Patch:            patch,
			GameVersion:      info.Version,
			SchemasFS:        schemasFS,
			OutputPath:       outPath,
			PrevSnapshotPath: tgt.prevSnap,
			IconCacheDir:     iconCacheDir,
			CompressionLevel: level,
			OnProgress: func(stage string, done, total int) {
				if stage != lastStage {
					lastStage = stage
					fmt.Printf("  - 阶段: %s (%d/%d)\n", stage, done, total)
				} else if done == total && total > 1 {
					fmt.Printf("  - 完成: %s (%d/%d)\n", stage, done, total)
				}
			},
		})
		if err != nil {
			fatalf("打包快照 [%s/%s] 失败: %v", patch, tgt.region, err)
		}

		sumLines = append(sumLines, fmt.Sprintf("%s  %s", res.SHA256, assetName))
		regionAsset := &snapshot.RegionAsset{
			GameVersion:    res.Meta.GameVersion,
			Languages:      res.Meta.Languages,
			CreatedAt:      res.Meta.CreatedAt,
			AssetName:      assetName,
			DownloadURL:    fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, releaseTag, assetName),
			Size:           res.Size,
			SHA256:         res.SHA256,
			PrevSnapshotID: res.Meta.PrevSnapshotID,
			Stats:          res.Meta.Stats,
		}
		if tgt.region == snapshot.RegionGlobal {
			versionEntry.Global = regionAsset
		} else {
			versionEntry.CN = regionAsset
		}
		versionEntry.CreatedAt = res.Meta.CreatedAt

		fmt.Printf("  ✓ 打包完成: %s (%.2f MB, 耗时 %.1fs)\n", outPath, float64(res.Size)/(1024*1024), time.Since(start).Seconds())
		fmt.Printf("    数据表: %d | EXD 分片: %d | Schema: %d | BGM: %d | 图标: %d | 变动旧图: %d\n",
			res.Meta.Stats.SheetCount, res.Meta.Stats.EXDFileCount, res.Meta.Stats.SchemaCount,
			res.Meta.Stats.BGMCount, res.Meta.Stats.IconCount, res.Meta.Stats.PrevIconThumbCount)
		fmt.Printf("    SHA256: %s\n", res.SHA256)
	}

	sumPath := filepath.Join(*outDirFlag, "SHA256SUMS.txt")
	if err := os.WriteFile(sumPath, []byte(strings.Join(sumLines, "\n")+"\n"), 0o644); err != nil {
		fatalf("写出校验文件失败: %v", err)
	}

	if strings.TrimSpace(*manifestOut) != "" {
		if err := os.MkdirAll(filepath.Dir(*manifestOut), 0o755); err != nil {
			fatalf("创建清单输出目录失败: %v", err)
		}
		raw, err := json.MarshalIndent(versionEntry, "", "  ")
		if err != nil {
			fatalf("序列化清单失败: %v", err)
		}
		if err := os.WriteFile(*manifestOut, raw, 0o644); err != nil {
			fatalf("写出清单文件失败: %v", err)
		}
		fmt.Printf("\n已写入统一版本目录条目: %s\n", *manifestOut)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[错误] "+format+"\n", args...)
	os.Exit(1)
}
