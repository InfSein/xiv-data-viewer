package uiapp

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"
	_ "github.com/egoist/mygo/audio/vorbis"
)

// musicPlayerSnapshot 为界面渲染提供无锁只读的音频播放状态快照。
type musicPlayerSnapshot struct {
	TrackKey string
	HasTrack bool
	Playing  bool
	Paused   bool
	Position time.Duration
	Duration time.Duration
	Volume   float64 // 0.0 ~ 1.0
	Muted    bool
	Loop     bool
	ErrText  string
}

// musicPlayer 封装基于 mygo/audio 的纯 Go Ogg Vorbis 音频流播放器。
type musicPlayer struct {
	mu       sync.Mutex
	trackKey string
	detail   *gamedata.MusicAudioDetail
	player   *audio.Player
	volume   float64
	muted    bool
	loop     bool
	playing  bool
	paused   bool
	errText  string
}

func newMusicPlayer() *musicPlayer {
	return &musicPlayer{
		volume: 0.75,
		loop:   true,
	}
}

func safeOpenOggStream(oggData []byte) (player *audio.Player, err error) {
	defer func() {
		if r := recover(); r != nil {
			player = nil
			err = fmt.Errorf("音频流解码异常: %v", r)
		}
	}()
	return audio.OpenStream(bytes.NewReader(oggData))
}

func applyMusicLoop(p *audio.Player, loop bool, detail *gamedata.MusicAudioDetail) {
	if p == nil {
		return
	}
	if !loop {
		p.SetLoop(false)
		return
	}
	p.SetLoop(true)
	if detail != nil && detail.HasLoop && detail.SampleRate > 0 && detail.LoopEndSample > detail.LoopStartSample {
		sr := time.Duration(detail.SampleRate)
		start := time.Duration(detail.LoopStartSample) * time.Second / sr
		end := time.Duration(detail.LoopEndSample) * time.Second / sr
		if end > start {
			p.SetLoopRange(start, end)
		}
	}
}

func (mp *musicPlayer) effectiveVolumeLocked() float64 {
	if mp.muted {
		return 0
	}
	if mp.volume < 0 {
		return 0
	}
	if mp.volume > 1 {
		return 1
	}
	return mp.volume
}

// PlayDetail 加载并从头播放指定曲目的 Ogg Vorbis 音频流。
func (mp *musicPlayer) PlayDetail(trackKey string, detail *gamedata.MusicAudioDetail, win *mygo.Window) error {
	if mp == nil || detail == nil || !detail.Playable || len(detail.OggData) == 0 {
		return fmt.Errorf("当前音轨不支持解码播放")
	}

	mp.mu.Lock()
	oldPlayer := mp.player
	mp.player = nil
	mp.playing = false
	mp.paused = false
	mp.errText = ""
	loopEnabled := mp.loop
	effVol := mp.effectiveVolumeLocked()
	mp.mu.Unlock()

	if oldPlayer != nil {
		oldPlayer.Close()
	}

	p, err := safeOpenOggStream(detail.OggData)
	if err != nil {
		mp.mu.Lock()
		mp.trackKey = trackKey
		mp.detail = detail
		mp.errText = err.Error()
		mp.mu.Unlock()
		return err
	}
	if aErr := audio.Err(); aErr != nil {
		p.Close()
		mp.mu.Lock()
		mp.trackKey = trackKey
		mp.detail = detail
		mp.errText = aErr.Error()
		mp.mu.Unlock()
		return aErr
	}

	p.SetVolume(effVol)
	applyMusicLoop(p, loopEnabled, detail)
	p.OnEnd(func() {
		mp.handleTrackEnded(trackKey, win)
	})
	p.Play()

	mp.mu.Lock()
	mp.trackKey = trackKey
	mp.detail = detail
	mp.player = p
	mp.playing = true
	mp.paused = false
	mp.errText = ""
	mp.mu.Unlock()
	if win != nil {
		win.Invalidate()
	}
	return nil
}

func (mp *musicPlayer) handleTrackEnded(trackKey string, win *mygo.Window) {
	mp.mu.Lock()
	if mp.trackKey == trackKey && !mp.loop {
		mp.playing = false
		mp.paused = false
	}
	mp.mu.Unlock()
	if win != nil {
		win.Invalidate()
	}
}

// TogglePlayPause 切换当前音轨的播放/暂停状态；若当前已停止则从头播放。
func (mp *musicPlayer) TogglePlayPause(win *mygo.Window) bool {
	if mp == nil {
		return false
	}
	mp.mu.Lock()
	p := mp.player
	if p == nil {
		mp.mu.Unlock()
		return false
	}
	if p.Playing() {
		p.Pause()
		mp.playing = false
		mp.paused = true
	} else {
		p.Play()
		mp.playing = true
		mp.paused = false
	}
	mp.mu.Unlock()
	if win != nil {
		win.Invalidate()
	}
	return true
}

// Stop 停止当前播放并将进度归零。
func (mp *musicPlayer) Stop(win *mygo.Window) {
	if mp == nil {
		return
	}
	mp.mu.Lock()
	p := mp.player
	mp.playing = false
	mp.paused = false
	mp.mu.Unlock()
	if p != nil {
		p.Stop()
		p.Seek(0)
	}
	if win != nil {
		win.Invalidate()
	}
}

// Seek 将正在加载的音频定位到指定时间点。
func (mp *musicPlayer) Seek(pos time.Duration) {
	if mp == nil {
		return
	}
	mp.mu.Lock()
	p := mp.player
	d := mp.detail
	mp.mu.Unlock()
	if p == nil {
		return
	}
	if pos < 0 {
		pos = 0
	}
	if dur := p.Duration(); dur > 0 && pos > dur {
		pos = dur
	} else if d != nil && d.DurationSec > 0 {
		maxDur := time.Duration(d.DurationSec * float64(time.Second))
		if pos > maxDur {
			pos = maxDur
		}
	}
	p.Seek(pos)
}

// SetVolume 设置主音量（0.0 ~ 1.0）。
func (mp *musicPlayer) SetVolume(vol float64) {
	if mp == nil {
		return
	}
	if vol < 0 {
		vol = 0
	}
	if vol > 1 {
		vol = 1
	}
	mp.mu.Lock()
	mp.volume = vol
	if vol > 0 && mp.muted {
		mp.muted = false
	}
	p := mp.player
	eff := mp.effectiveVolumeLocked()
	mp.mu.Unlock()
	if p != nil {
		p.SetVolume(eff)
	}
}

// ToggleMute 切换静音状态。
func (mp *musicPlayer) ToggleMute() {
	if mp == nil {
		return
	}
	mp.mu.Lock()
	mp.muted = !mp.muted
	p := mp.player
	eff := mp.effectiveVolumeLocked()
	mp.mu.Unlock()
	if p != nil {
		p.SetVolume(eff)
	}
}

// ToggleLoop 切换单曲循环（存在内置循环点时同步启用无缝循环区间）。
func (mp *musicPlayer) ToggleLoop() {
	if mp == nil {
		return
	}
	mp.mu.Lock()
	mp.loop = !mp.loop
	loop := mp.loop
	p := mp.player
	detail := mp.detail
	mp.mu.Unlock()
	applyMusicLoop(p, loop, detail)
}

// Snapshot 返回当前播放状态的并发安全副本。
func (mp *musicPlayer) Snapshot() musicPlayerSnapshot {
	if mp == nil {
		return musicPlayerSnapshot{Volume: 0.75, Loop: true}
	}
	mp.mu.Lock()
	snap := musicPlayerSnapshot{
		TrackKey: mp.trackKey,
		HasTrack: mp.player != nil,
		Playing:  mp.playing,
		Paused:   mp.paused,
		Volume:   mp.volume,
		Muted:    mp.muted,
		Loop:     mp.loop,
		ErrText:  mp.errText,
	}
	p := mp.player
	detail := mp.detail
	mp.mu.Unlock()

	if p != nil {
		snap.Playing = p.Playing()
		snap.Position = p.Position()
		snap.Duration = p.Duration()
		if snap.Playing {
			snap.Paused = false
		}
	} else if detail != nil && detail.DurationSec > 0 {
		snap.Duration = time.Duration(detail.DurationSec * float64(time.Second))
	}
	return snap
}

// Close 释放音频后端资源。
func (mp *musicPlayer) Close() {
	if mp == nil {
		return
	}
	mp.mu.Lock()
	p := mp.player
	mp.player = nil
	mp.playing = false
	mp.paused = false
	mp.mu.Unlock()
	if p != nil {
		p.Close()
	}
}
