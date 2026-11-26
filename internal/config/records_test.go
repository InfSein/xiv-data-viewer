package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUserRecordsRecentCapDedupeAndPersistence(t *testing.T) {
	tmp := t.TempDir()

	rec, err := LoadUserRecords(tmp)
	if err != nil {
		t.Fatalf("加载空历史记录不应报错: %v", err)
	}
	if len(rec.Recent) != 0 || len(rec.Favorites) != 0 {
		t.Fatalf("首次加载应为空: %+v", rec)
	}

	baseTime := time.Unix(1700000000, 0)
	for i := 1; i <= 550; i++ {
		kind := RecordKindItem
		switch i % 3 {
		case 1:
			kind = RecordKindQuest
		case 2:
			kind = RecordKindLeve
		}
		if ok := rec.RecordRecent(kind, i, baseTime.Add(time.Duration(i)*time.Second)); !ok {
			t.Fatalf("记录第 %d 条失败", i)
		}
	}
	if len(rec.Recent) != MaxRecentRecords {
		t.Fatalf("最近记录上限应为 %d，实际: %d", MaxRecentRecords, len(rec.Recent))
	}
	if rec.Recent[0].ID != 550 || rec.Recent[len(rec.Recent)-1].ID != 51 {
		t.Fatalf("最近记录顺序或淘汰范围异常: first=%+v last=%+v", rec.Recent[0], rec.Recent[len(rec.Recent)-1])
	}

	// 成就类型不得写入“最近”列表
	if rec.RecordRecent(RecordKindAchievement, 99, baseTime) {
		t.Fatalf("成就类型不应记录到最近浏览列表")
	}

	// 重新打开已存在的旧记录应去重并置顶
	target := rec.Recent[42]
	rec.RecordRecent(target.Kind, target.ID, baseTime.Add(1000*time.Second))
	if len(rec.Recent) != MaxRecentRecords {
		t.Fatalf("去重置顶后长度仍应为 %d，实际: %d", MaxRecentRecords, len(rec.Recent))
	}
	if rec.Recent[0].Kind != target.Kind || rec.Recent[0].ID != target.ID {
		t.Fatalf("去重置顶失败: got=%+v want=%+v", rec.Recent[0], target)
	}
	dupCount := 0
	for _, r := range rec.Recent {
		if r.Kind == target.Kind && r.ID == target.ID {
			dupCount++
		}
	}
	if dupCount != 1 {
		t.Fatalf("去重后出现重复条目 count=%d", dupCount)
	}

	// 收藏切换覆盖物品、任务、理符、成就四类
	favTargets := []HistoryRecord{
		{Kind: RecordKindItem, ID: 44099},
		{Kind: RecordKindQuest, ID: 65536},
		{Kind: RecordKindLeve, ID: 201},
		{Kind: RecordKindAchievement, ID: 1001},
	}
	for i, ft := range favTargets {
		fav, changed := rec.ToggleFavorite(ft.Kind, ft.ID, baseTime.Add(time.Duration(i)*time.Minute))
		if !fav || !changed || !rec.IsFavorite(ft.Kind, ft.ID) {
			t.Fatalf("添加收藏失败: %+v fav=%v changed=%v", ft, fav, changed)
		}
	}

	// 持久化并重新读取校验
	if err := SaveUserRecords(tmp, rec); err != nil {
		t.Fatalf("保存历史与收藏记录失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "history.json")); err != nil {
		t.Fatalf("持久化文件不存在: %v", err)
	}
	reloaded, err := LoadUserRecords(tmp)
	if err != nil {
		t.Fatalf("重新加载历史与收藏记录失败: %v", err)
	}
	if len(reloaded.Recent) != MaxRecentRecords || len(reloaded.Favorites) != len(favTargets) {
		t.Fatalf("持久化前后数量不一致: recent=%d fav=%d", len(reloaded.Recent), len(reloaded.Favorites))
	}

	// 取消收藏、单条移除与清空最近记录
	fav, changed := reloaded.ToggleFavorite(RecordKindAchievement, 1001, baseTime)
	if fav || !changed || reloaded.IsFavorite(RecordKindAchievement, 1001) {
		t.Fatalf("取消收藏失败")
	}
	if !reloaded.RemoveRecent(target.Kind, target.ID) || len(reloaded.Recent) != MaxRecentRecords-1 {
		t.Fatalf("移除单条最近记录失败")
	}
	if !reloaded.ClearRecent() || len(reloaded.Recent) != 0 {
		t.Fatalf("清空最近记录失败")
	}

	// 验证同 ID 的 BGM 与管弦乐琴可同时作为独立音乐记录进入最近浏览与收藏
	var musicRec UserRecords
	if !musicRec.RecordRecent(RecordKindBGM, 10, baseTime) || !musicRec.RecordRecent(RecordKindOrchestrion, 10, baseTime.Add(time.Second)) {
		t.Fatalf("音乐记录写入最近浏览失败: %+v", musicRec.Recent)
	}
	if len(musicRec.Recent) != 2 {
		t.Fatalf("相同 ID 的 BGM 与管弦乐琴不应互相去重，实际 %+v", musicRec.Recent)
	}
	if fav, chg := musicRec.ToggleFavorite(RecordKindBGM, 10, baseTime); !fav || !chg {
		t.Fatalf("收藏 BGM #10 失败")
	}
	if fav, chg := musicRec.ToggleFavorite(RecordKindOrchestrion, 10, baseTime); !fav || !chg {
		t.Fatalf("收藏管弦乐琴 #10 失败")
	}
	if !musicRec.IsFavorite(RecordKindBGM, 10) || !musicRec.IsFavorite(RecordKindOrchestrion, 10) {
		t.Fatalf("音乐双子类型收藏状态判定异常")
	}
	if !IsMusicRecordKind(MusicRecordKind("bgm")) || MusicKindOfRecord(RecordKindOrchestrion) != "orchestrion" {
		t.Fatalf("音乐类型转换辅助函数异常")
	}
}
