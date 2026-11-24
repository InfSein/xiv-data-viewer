package gamedata

import (
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/config"
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

	store, err := LoadGameDataStore(resolved)
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
	if status.Stats.PurchasableItems <= 5000 || status.Stats.TotalShops <= 500 {
		t.Fatalf("商店数量异常: purch=%d shops=%d", status.Stats.PurchasableItems, status.Stats.TotalShops)
	}
	if status.Stats.RetainerVentureItems <= 800 || len(status.CraftTypes) != 8 {
		t.Fatalf("雇员探险或制作职业异常: retainer=%d crafts=%d", status.Stats.RetainerVentureItems, len(status.CraftTypes))
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
	}
}
