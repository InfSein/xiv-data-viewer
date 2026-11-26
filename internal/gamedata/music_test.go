package gamedata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/config"
)

func TestMusicServiceSearchAndLocate(t *testing.T) {
	store := &Store{
		BGMs: []MusicEntry{
			{
				Key:           musicKey(MusicKindBGM, 10),
				Kind:          MusicKindBGM,
				ID:            10,
				Title:         "BGM_System_Title",
				File:          "music/ffxiv/BGM_System_Title.scd",
				FileStem:      "BGM_System_Title",
				ExpansionID:   0,
				ExpansionName: "重生之境",
				FileExists:    true,
				Usages: []MusicUsage{
					{Kind: "duty", Role: "战斗", Label: "萨斯塔夏溶洞", RefID: 1},
				},
				UsageSummary: "萨斯塔夏溶洞",
			},
			{
				Key:           musicKey(MusicKindBGM, 20),
				Kind:          MusicKindBGM,
				ID:            20,
				Title:         "BGM_EX5_Title",
				File:          "music/ex5/BGM_EX5_Title.scd",
				FileStem:      "BGM_EX5_Title",
				ExpansionID:   5,
				ExpansionName: "金曦之遗辉",
				FileExists:    true,
			},
		},
		Orchestrions: []MusicEntry{
			{
				Key:           musicKey(MusicKindOrchestrion, 100),
				Kind:          MusicKindOrchestrion,
				ID:            100,
				Title:         "Prelude - Discoveries",
				NameCn:        "序曲 - 发现",
				NameEn:        "Prelude - Discoveries",
				NameJa:        "プレリュード ～発見～",
				File:          "music/ffxiv/Orchestrion/BGM_ORCH_001.scd",
				FileStem:      "BGM_ORCH_001",
				ExpansionID:   0,
				ExpansionName: "重生之境",
				CategoryID:    1,
				CategoryName:  "野外场景",
				ItemID:        14284,
				ItemName:      "管弦乐琴乐谱：序曲",
				FileExists:    true,
			},
		},
		MusicCategoryList: []MusicCategoryInfo{
			{ID: 1, Name: "野外场景", Count: 1},
		},
		MusicExpansionList: []MusicExpansionInfo{
			{ID: 0, Name: "重生之境", Count: 2},
			{ID: 5, Name: "金曦之遗辉", Count: 1},
		},
	}
	svc := NewService(store)

	all := svc.SearchMusic(MusicSearchQuery{ExpansionID: -1, Page: 1, PageSize: 30})
	if all.Total != 3 {
		t.Fatalf("期望全部音乐 3 条，实际 %d", all.Total)
	}
	byUsage := svc.SearchMusic(MusicSearchQuery{ExpansionID: -1, WithUsageOnly: true, Page: 1, PageSize: 30})
	if byUsage.Total != 2 {
		t.Fatalf("期望含关联来源或乐谱道具条目为 2 条，实际 %d", byUsage.Total)
	}
	byQuery := svc.SearchMusic(MusicSearchQuery{Q: "萨斯塔夏", ExpansionID: -1, Page: 1, PageSize: 30})
	if byQuery.Total != 1 || byQuery.Items[0].ID != 10 {
		t.Fatalf("按用途文本筛选异常: %+v", byQuery.Items)
	}
	located, ok := svc.LocateMusicPage(MusicSearchQuery{ExpansionID: -1, PageSize: 1}, MusicKindOrchestrion, 100)
	if !ok || located.Page != 3 || len(located.Items) != 1 || located.Items[0].ID != 100 {
		t.Fatalf("LocateMusicPage 异常: ok=%v resp=%+v", ok, located)
	}
	if detail := svc.GetMusicDetail(MusicKindBGM, 20); detail == nil || detail.ExpansionID != 5 {
		t.Fatalf("GetMusicDetail 异常: %+v", detail)
	}
	hits := svc.SearchGlobal("BGM_System_Title", 5)
	if len(hits.Hits) != 1 || hits.Hits[0].Kind != GlobalHitMusic || hits.Hits[0].ID != 10 {
		t.Fatalf("全局检索应命中音乐条目: %+v", hits.Hits)
	}
}

func TestRealMusicLoaderAndAudio(t *testing.T) {
	cfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过真实客户端音乐加载测试: %v", err)
	}

	schemasFS := os.DirFS(filepath.Join("..", "..", "schemas"))
	store, err := LoadGameDataStoreWithSchemas(resolved, schemasFS)
	if err != nil {
		t.Fatalf("LoadGameDataStoreWithSchemas 失败: %v", err)
	}
	defer func() { _ = store.Close() }()

	if len(store.BGMs) < 1000 || len(store.Orchestrions) < 500 {
		t.Fatalf("音乐数据数量过少: bgm=%d orch=%d", len(store.BGMs), len(store.Orchestrions))
	}
	svc := NewService(store)
	status := svc.GetSystemStatus()
	if status.Stats.TotalBGMs != len(store.BGMs) || status.Stats.TotalOrchestrions != len(store.Orchestrions) {
		t.Fatalf("SystemStatus 音乐计数不一致: %+v", status.Stats)
	}

	// 验证存在关联场景来源的 BGM 及真实 .scd 音频解包
	withUsage := svc.SearchMusic(MusicSearchQuery{Kind: MusicKindBGM, ExpansionID: -1, WithUsageOnly: true, Page: 1, PageSize: 20})
	if withUsage.Total == 0 {
		t.Fatalf("期望至少存在带用途来源的 BGM")
	}
	var audioTested bool
	for _, entry := range withUsage.Items {
		if !entry.FileExists {
			continue
		}
		audio, err := svc.LoadMusicAudio(entry.Kind, entry.ID)
		if err != nil {
			t.Fatalf("LoadMusicAudio(%s) 失败: %v", entry.File, err)
		}
		if audio.Playable && len(audio.OggData) > 0 && audio.Channels > 0 && audio.SampleRate > 0 {
			audioTested = true
			break
		}
	}
	if !audioTested {
		t.Fatalf("未能在带用途 BGM 中验证可播放的 OggVorbis SCD 音频")
	}

	// 验证管弦乐琴条目关联乐谱物品
	orch := svc.SearchMusic(MusicSearchQuery{Kind: MusicKindOrchestrion, ExpansionID: -1, WithUsageOnly: true, Page: 1, PageSize: 5})
	if orch.Total == 0 || orch.Items[0].ItemID <= 0 || orch.Items[0].ItemName == "" {
		t.Fatalf("管弦乐琴乐谱关联道具解析异常: %+v", orch.Items)
	}

	// 验证 ComputeBGMDigests 返回非空 MD5 与文件字节数
	digests := store.ComputeBGMDigests(false)
	var nonEmptyDigests int
	for _, d := range digests {
		if d.FileExists && d.FileSize > 0 && len(d.ContentMD5) == 32 {
			nonEmptyDigests++
		}
	}
	if nonEmptyDigests < 900 {
		t.Fatalf("有效 BGM MD5 摘要数过少: %d", nonEmptyDigests)
	}
	// 验证二次调用命中缓存仍一致
	cached := store.ComputeBGMDigests(false)
	if len(cached) != len(digests) {
		t.Fatalf("二次 ComputeBGMDigests 数量不一致: %d != %d", len(cached), len(digests))
	}
}
