package gamedata

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/i18n"
)

func TestEorzeaTime(t *testing.T) {
	et := GetCurrentEorzeaTime(time.Unix(1700000000, 0))
	if et.Hours < 0 || et.Hours >= 24 || et.Minutes < 0 || et.Minutes >= 60 {
		t.Fatalf("非法 ET 时间: %+v", et)
	}
	st := EvaluateGatheringWindows([]GatheringTimeWindow{{
		StartET: "00:00", EndET: "23:59", StartMinuteOfDay: 0, DurationMinutes: 1439,
	}}, time.Unix(1700000000, 0))
	if !st.IsActive {
		t.Fatalf("全天窗口应处于开放中: %+v", st)
	}
}

func TestRealGameDataService(t *testing.T) {
	cfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过真实数据测试: %v", err)
	}

	schemasFS := os.DirFS(filepath.Join("..", "..", "schemas"))
	store, err := LoadGameDataStoreWithSchemas(resolved, schemasFS)
	if err != nil {
		t.Fatalf("加载双服数据失败: %v", err)
	}
	defer store.Close()

	svc := NewService(store)
	status := svc.GetSystemStatus()
	verRe := regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}`)
	if status.HasCN && !verRe.MatchString(status.CNVersion) {
		t.Fatalf("国服版本号格式异常: %q", status.CNVersion)
	}
	if status.HasGlobal && !verRe.MatchString(status.GlobalVersion) {
		t.Fatalf("国际服版本号格式异常: %q", status.GlobalVersion)
	}
	if status.Stats.TotalItems <= 30000 || status.Stats.TotalRecipes <= 10000 {
		t.Fatalf("道具或配方数量异常: items=%d recipes=%d", status.Stats.TotalItems, status.Stats.TotalRecipes)
	}
	if status.Stats.TotalQuests <= 3000 || status.Stats.TotalLeves <= 800 || status.Stats.TotalCollectables <= 100 {
		t.Fatalf("任务/理符/收藏品数量异常: quests=%d leves=%d collectables=%d",
			status.Stats.TotalQuests, status.Stats.TotalLeves, status.Stats.TotalCollectables)
	}
	if status.Stats.PurchasableItems <= 5000 || status.Stats.TotalShops <= 500 {
		t.Fatalf("商店数量异常: purch=%d shops=%d", status.Stats.PurchasableItems, status.Stats.TotalShops)
	}
	if status.Stats.RetainerVentureItems <= 800 || len(status.CraftTypes) != 8 {
		t.Fatalf("雇员探险或制作职业异常: retainer=%d crafts=%d", status.Stats.RetainerVentureItems, len(status.CraftTypes))
	}
	if len(status.AchievementKinds) == 0 || len(status.AchievementCategories) == 0 {
		t.Fatalf("成就大类或二级分类统计为空: kinds=%d cats=%d", len(status.AchievementKinds), len(status.AchievementCategories))
	}
	var dungeonCat AchievementCategoryInfo
	for _, c := range status.AchievementCategories {
		if c.KindID == 1 && c.ID == 2 {
			dungeonCat = c
			break
		}
	}
	if dungeonCat.ID == 0 || dungeonCat.Count == 0 {
		t.Fatalf("未在战斗大类下找到迷宫探险二级分类: %+v", status.AchievementCategories)
	}
	achCatRes := svc.SearchAchievements(AchievementSearchQuery{CategoryID: dungeonCat.ID, PageSize: 20})
	if achCatRes.Total != dungeonCat.Count || len(achCatRes.Items) == 0 {
		t.Fatalf("按成就二级分类筛选数量不匹配: total=%d want=%d", achCatRes.Total, dungeonCat.Count)
	}
	for _, ach := range achCatRes.Items {
		if ach.CategoryID != dungeonCat.ID || ach.KindID != dungeonCat.KindID {
			t.Fatalf("成就二级分类筛选混入无关条目: %+v", ach)
		}
	}
	if status.HasGlobal && achCatRes.Items[0].NameJa == "" {
		t.Fatalf("已配置国际服时日文成就名不应为空: %+v", achCatRes.Items[0])
	}

	// 多语言检索
	if status.HasCN {
		res := svc.SearchItems(ItemSearchQuery{Q: "图拉尔菠萝蛋糕"})
		if res.Total == 0 || res.Items[0].ID != 44099 {
			t.Fatalf("中文搜索图拉尔菠萝蛋糕失败: %+v", res)
		}
	}
	if status.HasGlobal {
		res := svc.SearchItems(ItemSearchQuery{Q: "Turali Pineapple Ponzecake"})
		if res.Total == 0 || res.Items[0].ID != 44099 {
			t.Fatalf("英文搜索失败: %+v", res)
		}
		jaRes := svc.SearchItems(ItemSearchQuery{Q: "トラルパイナップルケーキ"})
		if jaRes.Total == 0 || jaRes.Items[0].ID != 44099 {
			t.Fatalf("日文搜索失败: %+v", jaRes)
		}
	}

	// 制作装备、HQ 增益与配方树
	detail := svc.GetItemDetail(34577)
	if detail == nil || detail.ItemLevel != 539 || detail.EquipLevel != 86 || !detail.IsEquipment {
		t.Fatalf("装备 #34577 详情异常: %+v", detail)
	}
	if detail.EquipmentStats == nil || detail.EquipmentStats.DefensePhysHQ <= detail.EquipmentStats.DefensePhys {
		t.Fatalf("装备 #34577 HQ 防御力增益计算异常: %+v", detail.EquipmentStats)
	}
	if len(detail.Recipes) == 0 || len(detail.RecipeTrees) == 0 || len(detail.RecipeTrees[0].Children) == 0 {
		t.Fatalf("装备 #34577 配方树解析异常")
	}

	// 物理武器与魔法武器自动攻击及 HQ 计算
	physWeapon := svc.GetItemDetail(51110)
	if physWeapon != nil && physWeapon.EquipmentStats != nil {
		es := physWeapon.EquipmentStats
		if es.WeaponKind != "physical" || es.DamagePhys != 139 || es.DamagePhysHQ != 155 || es.DamageMag != 0 {
			t.Fatalf("物理武器 #51110 属性不符: %+v", es)
		}
		if es.AutoAttack != 103.78 || es.AutoAttackHQ != 115.73 {
			t.Fatalf("物理武器 #51110 自动攻击不符: aa=%.2f aaHq=%.2f", es.AutoAttack, es.AutoAttackHQ)
		}
	}
	magWeapon := svc.GetItemDetail(51115)
	if magWeapon != nil && magWeapon.EquipmentStats != nil {
		es := magWeapon.EquipmentStats
		if es.WeaponKind != "magical" || es.DamageMag != 139 || es.DamageMagHQ != 155 || es.DamagePhys != 0 || es.AutoAttack <= 0 {
			t.Fatalf("魔法武器 #51115 属性不符: %+v", es)
		}
	}

	// 验证单国服模式与单国际服模式各自独立运行无报错
	if resolved.HasCN() {
		cnOnly, err := LoadGameDataStore(&config.ResolvedPaths{CN: resolved.CN})
		if err != nil {
			t.Fatalf("仅国服模式加载失败: %v", err)
		}
		if len(cnOnly.ItemSummaries) < 30000 {
			t.Fatalf("仅国服模式道具数异常: %d", len(cnOnly.ItemSummaries))
		}
		_ = cnOnly.Close()
	}
	if resolved.HasGlobal() {
		glOnly, err := LoadGameDataStore(&config.ResolvedPaths{Global: resolved.Global})
		if err != nil {
			t.Fatalf("仅国际服模式加载失败: %v", err)
		}
		if len(glOnly.ItemSummaries) < 30000 || glOnly.ItemSummaryByID[44099].NameCn != "Turali Pineapple Ponzecake" {
			t.Fatalf("仅国际服模式道具名回退异常: %+v", glOnly.ItemSummaryByID[44099])
		}
		_ = glOnly.Close()

		// 验证切换主数据语言为日语 (ja) 与英语 (en) 时，主显示名与三语检索均正常
		storeJA, err := LoadGameDataStoreForLang(resolved, schemasFS, i18n.LangJA, nil)
		if err != nil {
			t.Fatalf("加载日语主数据失败: %v", err)
		}
		defer storeJA.Close()
		itemJA := storeJA.ItemSummaryByID[44099]
		if itemJA.NameCn != "トラルパイナップルケーキ" || itemJA.NameJa != "トラルパイナップルケーキ" || itemJA.NameEn != "Turali Pineapple Ponzecake" {
			t.Fatalf("日语主数据道具名异常: %+v", itemJA)
		}
		svcJA := NewService(storeJA)
		if resolved.HasCN() {
			if itemJA.NameZh != "图拉尔菠萝蛋糕" {
				t.Fatalf("双服挂载时日语模式下应保留中文检索名 NameZh: %+v", itemJA)
			}
			if res := svcJA.SearchItems(ItemSearchQuery{Q: "图拉尔菠萝蛋糕"}); res.Total == 0 || res.Items[0].ID != 44099 {
				t.Fatalf("日语模式下中文关键词搜索应继续命中 #44099: %+v", res)
			}
		}

		storeEN, err := LoadGameDataStoreForLang(resolved, schemasFS, i18n.LangEN, nil)
		if err != nil {
			t.Fatalf("加载英语主数据失败: %v", err)
		}
		defer storeEN.Close()
		itemEN := storeEN.ItemSummaryByID[44099]
		if itemEN.NameCn != "Turali Pineapple Ponzecake" || itemEN.NameEn != "Turali Pineapple Ponzecake" || itemEN.NameJa != "トラルパイナップルケーキ" {
			t.Fatalf("英语主数据道具名异常: %+v", itemEN)
		}
		svcEN := NewService(storeEN)
		if res := svcEN.SearchItems(ItemSearchQuery{Q: "トラルパイナップルケーキ"}); res.Total == 0 || res.Items[0].ID != 44099 {
			t.Fatalf("英语模式下日文关键词搜索应继续命中 #44099: %+v", res)
		}

		// 验证通过 StoreLoadOptions 复用 BaseStore 的只读句柄与语言无关表
		sharedEN, err := LoadGameDataStoreWithOptions(resolved, schemasFS, StoreLoadOptions{
			AppLang:       i18n.LangEN,
			BaseStore:     storeJA,
			SharedReaders: true,
		})
		if err != nil {
			t.Fatalf("复用 BaseStore 加载英语主数据失败: %v", err)
		}
		if sharedEN.GlobalReader != storeJA.GlobalReader || sharedEN.Recipes == nil || len(sharedEN.Recipes) != len(storeJA.Recipes) {
			t.Fatalf("未能复用共享 GlobalReader 或语言无关配方表")
		}
		if err := sharedEN.Close(); err != nil {
			t.Fatalf("SharedReaders 模式下 Close 不应报错: %v", err)
		}
		if _, err := storeJA.GlobalReader.HasFile("exd/root.exl"); err != nil {
			t.Fatalf("SharedReaders 子 Store 关闭后不应关闭底层共享句柄: %v", err)
		}
		sharedDetail := NewService(sharedEN).GetItemDetail(34577)
		if sharedDetail == nil || len(sharedDetail.Recipes) == 0 {
			t.Fatalf("复用加载后装备 #34577 配方解析异常: %+v", sharedDetail)
		}
	}
}

func TestRealGameDataDeferredIconsLoad(t *testing.T) {
	cfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过延迟图标加载实测: %v", err)
	}

	schemasFS := os.DirFS(filepath.Join("..", "..", "schemas"))
	cacheDir := t.TempDir()

	t0 := time.Now()
	store, err := LoadGameDataStoreWithOptions(resolved, schemasFS, StoreLoadOptions{
		AppLang:      i18n.LangZH,
		DeferIcons:   true,
		IconCacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("延迟图标模式加载失败: %v", err)
	}
	defer store.Close()
	bootElapsed := time.Since(t0)

	if store.IconsReady() {
		t.Fatalf("DeferIcons=true 刚加载完成时 IconsReady 应为 false")
	}

	t1 := time.Now()
	if err := store.EnsureIconsLoaded(); err != nil {
		t.Fatalf("首次 EnsureIconsLoaded 失败: %v", err)
	}
	firstIconElapsed := time.Since(t1)

	if !store.IconsReady() || len(store.Icons) < 10000 {
		t.Fatalf("EnsureIconsLoaded 完成后图标索引异常: ready=%v count=%d", store.IconsReady(), len(store.Icons))
	}

	// 二次打开并命中磁盘指纹缓存验证加速效果
	store2, err := LoadGameDataStoreWithOptions(resolved, schemasFS, StoreLoadOptions{
		AppLang:      i18n.LangZH,
		DeferIcons:   true,
		IconCacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("二次加载失败: %v", err)
	}
	defer store2.Close()

	t2 := time.Now()
	if err := store2.EnsureIconsLoaded(); err != nil {
		t.Fatalf("命中缓存 EnsureIconsLoaded 失败: %v", err)
	}
	cachedIconElapsed := time.Since(t2)
	if len(store2.Icons) != len(store.Icons) {
		t.Fatalf("缓存加载图标数不一致: got=%d want=%d", len(store2.Icons), len(store.Icons))
	}
	t.Logf("延迟图标启动耗时=%s | 首次后台图标索引=%s | 命中指纹缓存图标索引=%s | 图标总数=%d",
		bootElapsed, firstIconElapsed, cachedIconElapsed, len(store.Icons))
}
