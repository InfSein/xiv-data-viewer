package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MaxRecentRecords 定义“最近”浏览记录的最大保留条数。
const MaxRecentRecords = 500

// RecordKind 标识历史记录与收藏夹支持的游戏实体类型。
type RecordKind string

const (
	RecordKindItem        RecordKind = "item"
	RecordKindQuest       RecordKind = "quest"
	RecordKindLeve        RecordKind = "leve"
	RecordKindAchievement RecordKind = "achievement"
	RecordKindBGM         RecordKind = "bgm"
	RecordKindOrchestrion RecordKind = "orchestrion"
)

// IsMusicRecordKind 返回记录类型是否属于音乐（原生 BGM 或管弦乐琴）。
func IsMusicRecordKind(kind RecordKind) bool {
	return kind == RecordKindBGM || kind == RecordKindOrchestrion
}

// MusicRecordKind 将业务层的音乐子类型字符串转换为持久化记录类型。
func MusicRecordKind(musicKind string) RecordKind {
	if musicKind == string(RecordKindOrchestrion) {
		return RecordKindOrchestrion
	}
	return RecordKindBGM
}

// MusicKindOfRecord 将音乐持久化记录类型还原为业务层音乐子类型字符串。
func MusicKindOfRecord(kind RecordKind) string {
	if kind == RecordKindOrchestrion {
		return string(RecordKindOrchestrion)
	}
	return string(RecordKindBGM)
}

// HistoryRecord 表示单条最近浏览或收藏实体索引记录。
type HistoryRecord struct {
	Kind      RecordKind `json:"kind"`
	ID        int        `json:"id"`
	Timestamp int64      `json:"timestamp"`
}

// UserRecords 存储持久化于应用数据目录的最近浏览与收藏列表。
type UserRecords struct {
	Recent    []HistoryRecord `json:"recent,omitempty"`
	Favorites []HistoryRecord `json:"favorites,omitempty"`
}

// RecordsFilePath 返回指定应用数据目录下的历史与收藏文件路径。
func RecordsFilePath(dataDir string) string {
	return filepath.Join(dataDir, "history.json")
}

func isValidRecentKind(kind RecordKind) bool {
	return kind == RecordKindItem || kind == RecordKindQuest || kind == RecordKindLeve || IsMusicRecordKind(kind)
}

func isValidFavoriteKind(kind RecordKind) bool {
	return isValidRecentKind(kind) || kind == RecordKindAchievement
}

type recordPairKey struct {
	kind RecordKind
	id   int
}

func normalizeRecordSlice(in []HistoryRecord, allow func(RecordKind) bool, maxCount int) []HistoryRecord {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[recordPairKey]bool, len(in))
	out := make([]HistoryRecord, 0, min(len(in), maxCountOrLen(maxCount, len(in))))
	for _, rec := range in {
		if rec.ID <= 0 || !allow(rec.Kind) {
			continue
		}
		key := recordPairKey{kind: rec.Kind, id: rec.ID}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rec)
		if maxCount > 0 && len(out) >= maxCount {
			break
		}
	}
	return out
}

func maxCountOrLen(maxCount, n int) int {
	if maxCount > 0 && maxCount < n {
		return maxCount
	}
	return n
}

func normalizeUserRecords(r *UserRecords) {
	r.Recent = normalizeRecordSlice(r.Recent, isValidRecentKind, MaxRecentRecords)
	r.Favorites = normalizeRecordSlice(r.Favorites, isValidFavoriteKind, 0)
}

// LoadUserRecords 从应用数据目录读取最近浏览与收藏记录；文件不存在时返回空结构。
func LoadUserRecords(dataDir string) (UserRecords, error) {
	var rec UserRecords
	p := RecordsFilePath(dataDir)
	raw, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rec, nil
		}
		return rec, fmt.Errorf("读取历史与收藏记录失败: %w", err)
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return UserRecords{}, fmt.Errorf("解析历史与收藏记录 JSON 失败: %w", err)
	}
	normalizeUserRecords(&rec)
	return rec, nil
}

// SaveUserRecords 将最近浏览与收藏记录以 UTF-8 JSON 格式持久化到应用数据目录。
func SaveUserRecords(dataDir string, rec UserRecords) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("创建应用数据目录失败: %w", err)
	}
	normalizeUserRecords(&rec)

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化历史与收藏记录失败: %w", err)
	}
	p := RecordsFilePath(dataDir)
	tmp, err := os.CreateTemp(dataDir, "history-*.json.tmp")
	if err == nil {
		tmpName := tmp.Name()
		_, writeErr := tmp.Write(data)
		closeErr := tmp.Close()
		if writeErr == nil && closeErr == nil {
			if renameErr := os.Rename(tmpName, p); renameErr == nil {
				return nil
			}
		}
		_ = os.Remove(tmpName)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("写入历史与收藏记录失败: %w", err)
	}
	return nil
}

// RecordRecent 将一次打开行为记录到“最近”列表顶部并去重，最多保留 MaxRecentRecords 条。
func (r *UserRecords) RecordRecent(kind RecordKind, id int, now time.Time) bool {
	if r == nil || id <= 0 || !isValidRecentKind(kind) {
		return false
	}
	ts := now.UnixMilli()
	if len(r.Recent) > 0 && r.Recent[0].Kind == kind && r.Recent[0].ID == id {
		r.Recent[0].Timestamp = ts
		return true
	}

	next := make([]HistoryRecord, 0, min(len(r.Recent)+1, MaxRecentRecords))
	next = append(next, HistoryRecord{Kind: kind, ID: id, Timestamp: ts})
	for _, item := range r.Recent {
		if item.ID <= 0 || !isValidRecentKind(item.Kind) {
			continue
		}
		if item.Kind == kind && item.ID == id {
			continue
		}
		next = append(next, item)
		if len(next) >= MaxRecentRecords {
			break
		}
	}
	r.Recent = next
	return true
}

// RemoveRecent 从“最近”列表中移除指定条目。
func (r *UserRecords) RemoveRecent(kind RecordKind, id int) bool {
	if r == nil || len(r.Recent) == 0 {
		return false
	}
	for i, item := range r.Recent {
		if item.Kind == kind && item.ID == id {
			r.Recent = append(r.Recent[:i], r.Recent[i+1:]...)
			return true
		}
	}
	return false
}

// ClearRecent 清空全部“最近”浏览记录。
func (r *UserRecords) ClearRecent() bool {
	if r == nil || len(r.Recent) == 0 {
		return false
	}
	r.Recent = nil
	return true
}

// IsFavorite 返回指定实体是否已加入收藏名单。
func (r *UserRecords) IsFavorite(kind RecordKind, id int) bool {
	if r == nil || id <= 0 || !isValidFavoriteKind(kind) {
		return false
	}
	for _, item := range r.Favorites {
		if item.Kind == kind && item.ID == id {
			return true
		}
	}
	return false
}

// ToggleFavorite 切换指定实体的收藏状态，返回切换后的最新收藏状态。
func (r *UserRecords) ToggleFavorite(kind RecordKind, id int, now time.Time) (favorited bool, changed bool) {
	if r == nil || id <= 0 || !isValidFavoriteKind(kind) {
		return false, false
	}
	for i, item := range r.Favorites {
		if item.Kind == kind && item.ID == id {
			r.Favorites = append(r.Favorites[:i], r.Favorites[i+1:]...)
			return false, true
		}
	}
	next := make([]HistoryRecord, 0, len(r.Favorites)+1)
	next = append(next, HistoryRecord{Kind: kind, ID: id, Timestamp: now.UnixMilli()})
	next = append(next, r.Favorites...)
	r.Favorites = next
	return true, true
}
