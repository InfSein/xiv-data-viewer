package gamedata

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/i18n"
)

// eorzeaMultiplier 表示艾欧泽亚时间与现实时间换算比例（1 艾欧泽亚日 = 70 现实分钟）。
const eorzeaMultiplier = 3600.0 / 175.0

// EorzeaClock 表示当前时刻对应的艾欧泽亚时钟。
type EorzeaClock struct {
	Hours             int
	Minutes           int
	TotalMinutesOfDay int
	Formatted         string
}

// GetCurrentEorzeaTime 将指定现实时刻换算为艾欧泽亚时间 (ET)。
func GetCurrentEorzeaTime(now time.Time) EorzeaClock {
	eorzeaMs := float64(now.UnixMilli()) * eorzeaMultiplier
	totalMinutes := int(math.Floor(eorzeaMs / 60000.0))
	modDay := ((totalMinutes % 1440) + 1440) % 1440
	hours := modDay / 60
	minutes := modDay % 60
	return EorzeaClock{
		Hours:             hours,
		Minutes:           minutes,
		TotalMinutesOfDay: modDay,
		Formatted:         fmt.Sprintf("%02d:%02d", hours, minutes),
	}
}

func eorzeaMinutesToRealSeconds(etMinutes int) int {
	secs := math.Round((float64(etMinutes) * 60.0) / eorzeaMultiplier)
	if secs < 0 {
		return 0
	}
	return int(secs)
}

// FormatRealDuration 将现实秒数格式化为 "X分YY秒" 或 "Y秒"。
func FormatRealDuration(seconds int) string {
	return FormatRealDurationForLang(seconds, i18n.LangZH)
}

// FormatRealDurationForLang 按指定界面语言将现实秒数格式化为可读倒计时。
func FormatRealDurationForLang(seconds int, lang i18n.Lang) string {
	if seconds < 0 {
		seconds = 0
	}
	mins := seconds / 60
	secs := seconds % 60
	if mins <= 0 {
		return i18n.Tf(lang, "time.sec", "sec", strconv.Itoa(secs))
	}
	return i18n.Tf(lang, "time.min_sec", "min", strconv.Itoa(mins), "sec", fmt.Sprintf("%02d", secs))
}

// TimedNodeActiveStatus 表示限时采集点在当前 ET 下的开放状态与现实倒计时。
type TimedNodeActiveStatus struct {
	IsActive             bool
	ActiveWindow         *GatheringTimeWindow
	NextWindow           *GatheringTimeWindow
	RealSecondsRemaining int
	StatusText           string
}

// EvaluateGatheringWindows 评估限时采集点当前是否处于开放窗口及距离下次变化的现实时间。
func EvaluateGatheringWindows(windows []GatheringTimeWindow, now time.Time) TimedNodeActiveStatus {
	return EvaluateGatheringWindowsForLang(windows, now, i18n.LangZH)
}

// EvaluateGatheringWindowsForLang 按指定界面语言评估限时采集点开放窗口及倒计时文本。
func EvaluateGatheringWindowsForLang(windows []GatheringTimeWindow, now time.Time, lang i18n.Lang) TimedNodeActiveStatus {
	if len(windows) == 0 {
		return TimedNodeActiveStatus{
			IsActive:   true,
			StatusText: i18n.T(lang, "time.always_open"),
		}
	}

	et := GetCurrentEorzeaTime(now)
	curMin := et.TotalMinutesOfDay

	for i := range windows {
		w := windows[i]
		elapsed := (curMin - w.StartMinuteOfDay + 1440) % 1440
		if elapsed < w.DurationMinutes {
			etRemain := w.DurationMinutes - elapsed
			realSec := eorzeaMinutesToRealSeconds(etRemain)
			winCopy := w
			return TimedNodeActiveStatus{
				IsActive:             true,
				ActiveWindow:         &winCopy,
				RealSecondsRemaining: realSec,
				StatusText:           i18n.Tf(lang, "time.open_remaining", "dur", FormatRealDurationForLang(realSec, lang)),
			}
		}
	}

	bestWait := 1441
	nextWin := windows[0]
	for _, w := range windows {
		wait := (w.StartMinuteOfDay - curMin + 1440) % 1440
		if wait < bestWait {
			bestWait = wait
			nextWin = w
		}
	}

	realWait := eorzeaMinutesToRealSeconds(bestWait)
	winCopy := nextWin
	return TimedNodeActiveStatus{
		IsActive:             false,
		NextWindow:           &winCopy,
		RealSecondsRemaining: realWait,
		StatusText:           i18n.Tf(lang, "time.next_open", "et", nextWin.StartET, "dur", FormatRealDurationForLang(realWait, lang)),
	}
}
