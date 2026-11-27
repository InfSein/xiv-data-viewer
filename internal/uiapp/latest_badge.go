package uiapp

import (
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo/ui"
)

// latestBadgeLabel 返回最新版本新增徽章与筛选按钮的展示文案：
// 若当前客户端版本号已识别（如 7.58）显示「7.58 新增」，否则显示「最新新增」。
func (a *App) latestBadgeLabel() string {
	if a.latestAdd == nil {
		return ""
	}
	if a.latestAdd.CurrentPatch != "" {
		return a.tf("latest.badge_patch", "patch", a.latestAdd.CurrentPatch)
	}
	return a.t("latest.badge_latest")
}

// latestCompactBadgeLabel 返回紧凑图标网格卡片使用的短徽章文案。
func (a *App) latestCompactBadgeLabel() string {
	if a.latestAdd == nil {
		return ""
	}
	return a.t("latest.badge_short")
}

func (a *App) drawLatestBadge(c *ui.Context) {
	if lbl := a.latestBadgeLabel(); lbl != "" {
		drawPill(c, lbl, pillPositive)
	}
}

func (a *App) renderLatestFilterChip(c *ui.Context, key string, count int, active bool) bool {
	if a.latestAdd == nil || count <= 0 {
		return false
	}
	return kitToggleChip(c, key, a.latestBadgeLabel(), formatInt(count), active, nil)
}

func (a *App) latestItemCount() int {
	if a.latestAdd == nil {
		return 0
	}
	return len(a.latestAdd.Items)
}

func (a *App) latestQuestCount() int {
	if a.latestAdd == nil {
		return 0
	}
	return len(a.latestAdd.Quests)
}

func (a *App) latestLeveCount() int {
	if a.latestAdd == nil {
		return 0
	}
	return len(a.latestAdd.Leves)
}

func (a *App) latestAchievementCount() int {
	if a.latestAdd == nil {
		return 0
	}
	return len(a.latestAdd.Achievements)
}

func (a *App) latestMusicCount() int {
	if a.latestAdd == nil {
		return 0
	}
	return len(a.latestAdd.BGMs) + len(a.latestAdd.Orchestrions)
}

func (a *App) latestIconCount() int {
	if a.latestAdd == nil || !a.latestAdd.HasIconBaseline {
		return 0
	}
	return len(a.latestAdd.Icons)
}

func (a *App) isNewItem(id int) bool {
	return a.latestAdd != nil && a.latestAdd.Items[id]
}

func (a *App) isNewQuest(id int) bool {
	return a.latestAdd != nil && a.latestAdd.Quests[id]
}

func (a *App) isNewLeve(id int) bool {
	return a.latestAdd != nil && a.latestAdd.Leves[id]
}

func (a *App) isNewAchievement(id int) bool {
	return a.latestAdd != nil && a.latestAdd.Achievements[id]
}

func (a *App) isNewMusic(kind string, id int) bool {
	if a.latestAdd == nil {
		return false
	}
	if kind == gamedata.MusicKindOrchestrion {
		return a.latestAdd.Orchestrions[id]
	}
	return a.latestAdd.BGMs[id]
}

func (a *App) isNewIcon(id int) bool {
	return a.latestAdd != nil && a.latestAdd.HasIconBaseline && a.latestAdd.Icons[id]
}

func (a *App) isNewGlobalHit(hit gamedata.GlobalSearchHit) bool {
	switch hit.Kind {
	case gamedata.GlobalHitItem:
		return a.isNewItem(hit.ID)
	case gamedata.GlobalHitQuest:
		return a.isNewQuest(hit.ID)
	case gamedata.GlobalHitLeve:
		return a.isNewLeve(hit.ID)
	case gamedata.GlobalHitAchievement:
		return a.isNewAchievement(hit.ID)
	case gamedata.GlobalHitMusic:
		return a.isNewMusic(hit.MusicKind, hit.ID)
	default:
		return false
	}
}
