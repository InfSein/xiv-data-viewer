package sqpack

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExtractCutbAudio(t *testing.T) {
	// 构造含多个 C070 记录（有效 BGM + 干扰字段 + 内嵌电影音轨）的合成 CUTB 载荷
	buf := make([]byte, 0, 256)
	buf = append(buf, []byte("CUTB")...)
	buf = binary.LittleEndian.AppendUint32(buf, 256)
	buf = binary.LittleEndian.AppendUint32(buf, 1)

	appendC070 := func(flags, bgmID, fadeMs, reserved uint32) {
		rec := make([]byte, 40)
		copy(rec[0:4], "C070")
		binary.LittleEndian.PutUint32(rec[4:8], 40)
		binary.LittleEndian.PutUint32(rec[24:28], flags)
		binary.LittleEndian.PutUint32(rec[28:32], bgmID)
		binary.LittleEndian.PutUint32(rec[32:36], fadeMs)
		binary.LittleEndian.PutUint32(rec[36:40], reserved)
		buf = append(buf, rec...)
	}

	appendC070(0, 578, 0, 0)
	appendC070(0x0100, 707, 1500, 0)
	// 非法的保留字段与超界标志应被过滤
	appendC070(0, 999, 0, 123)
	appendC070(0x0200, 888, 0, 0)
	// 预渲染影片音频路径
	buf = append(buf, []byte("music/ffxiv/movie/Ginruiko_ja.scd\x00")...)

	info, err := ExtractCutbAudio(buf)
	if err != nil {
		t.Fatalf("ExtractCutbAudio 失败: %v", err)
	}
	wantTracks := []CutbBGMTrack{
		{BGMID: 578, Flags: 0, FadeMs: 0},
		{BGMID: 707, Flags: 0x0100, FadeMs: 1500},
	}
	if !reflect.DeepEqual(info.Tracks, wantTracks) {
		t.Fatalf("Tracks = %+v, want %+v", info.Tracks, wantTracks)
	}
	wantMovies := []string{"music/ffxiv/movie/Ginruiko_ja.scd"}
	if !reflect.DeepEqual(info.MovieAudioPaths, wantMovies) {
		t.Fatalf("MovieAudioPaths = %+v, want %+v", info.MovieAudioPaths, wantMovies)
	}
}

func TestExtractCutbAudioInvalidHeader(t *testing.T) {
	if _, err := ExtractCutbAudio([]byte("NOPE12345678")); err == nil {
		t.Fatal("期望无效文件头返回错误")
	}
}

func encodeABC(op, a, b, c int) uint32 {
	return uint32(op&0x3F) | uint32(a&0xFF)<<6 | uint32(c&0x1FF)<<14 | uint32(b&0x1FF)<<23
}

func encodeABx(op, a, bx int) uint32 {
	return uint32(op&0x3F) | uint32(a&0xFF)<<6 | uint32(bx)<<14
}

func TestParseLuabCutsceneAndBGM(t *testing.T) {
	// 构建标准 Lua 5.1 字节码：root 含一个子函数，内部依次调用 PlayCutScene(self.CUTSCENE1)、PlayBGM(self.BGM_MUSIC_NO_MUSIC) 与 PlayBGM(self.LCUT_BGM1)
	var b bytes.Buffer
	b.Write([]byte("\x1bLua\x51\x00\x01\x04\x04\x04\x08\x00"))

	writeLuaStr := func(s string) {
		if s == "" {
			binary.Write(&b, binary.LittleEndian, uint32(0))
			return
		}
		binary.Write(&b, binary.LittleEndian, uint32(len(s)+1))
		b.WriteString(s)
		b.WriteByte(0)
	}

	// root proto
	writeLuaStr("")
	binary.Write(&b, binary.LittleEndian, uint32(0))
	binary.Write(&b, binary.LittleEndian, uint32(0))
	b.Write([]byte{0, 0, 2, 2})
	binary.Write(&b, binary.LittleEndian, uint32(1))
	binary.Write(&b, binary.LittleEndian, encodeABC(30, 0, 1, 0)) // RETURN
	binary.Write(&b, binary.LittleEndian, uint32(0))              // consts
	binary.Write(&b, binary.LittleEndian, uint32(1))              // 1 sub proto

	// sub proto
	writeLuaStr("")
	binary.Write(&b, binary.LittleEndian, uint32(10))
	binary.Write(&b, binary.LittleEndian, uint32(20))
	b.Write([]byte{0, 1, 2, 16}) // numparams=1 (self in R0)

	consts := []string{
		"PlayCutScene",
		"CUTSCENE1",
		"PlayBGM",
		"BGM_MUSIC_NO_MUSIC",
		"LCUT_BGM1",
	}
	code := []uint32{
		encodeABC(11, 2, 0, 256+0), // SELF R2 R0 K0("PlayCutScene")
		encodeABC(6, 4, 0, 256+1),  // GETTABLE R4 R0 K1("CUTSCENE1")
		encodeABC(28, 2, 3, 1),     // CALL R2 B=3 C=1
		encodeABC(11, 2, 0, 256+2), // SELF R2 R0 K2("PlayBGM")
		encodeABC(6, 4, 0, 256+3),  // GETTABLE R4 R0 K3("BGM_MUSIC_NO_MUSIC")
		encodeABC(28, 2, 3, 1),     // CALL R2 B=3 C=1
		encodeABC(11, 2, 0, 256+2), // SELF R2 R0 K2("PlayBGM")
		encodeABC(6, 4, 0, 256+4),  // GETTABLE R4 R0 K4("LCUT_BGM1")
		encodeABC(28, 2, 3, 1),     // CALL R2 B=3 C=1
		encodeABC(30, 0, 1, 0),     // RETURN
		encodeABx(1, 0, 0),         // unreachable no-op check
	}
	binary.Write(&b, binary.LittleEndian, uint32(len(code)))
	for _, ins := range code {
		binary.Write(&b, binary.LittleEndian, ins)
	}
	binary.Write(&b, binary.LittleEndian, uint32(len(consts)))
	for _, k := range consts {
		b.WriteByte(4)
		writeLuaStr(k)
	}
	binary.Write(&b, binary.LittleEndian, uint32(0)) // subs
	binary.Write(&b, binary.LittleEndian, uint32(0)) // lineinfo
	binary.Write(&b, binary.LittleEndian, uint32(0)) // locvars
	binary.Write(&b, binary.LittleEndian, uint32(0)) // upvalues

	// root debug tail
	binary.Write(&b, binary.LittleEndian, uint32(0))
	binary.Write(&b, binary.LittleEndian, uint32(0))
	binary.Write(&b, binary.LittleEndian, uint32(0))

	info, err := ParseLuabCutsceneAndBGM(b.Bytes())
	if err != nil {
		t.Fatalf("ParseLuabCutsceneAndBGM 失败: %v", err)
	}
	if len(info.FuncLinks) != 1 {
		t.Fatalf("FuncLinks len = %d, want 1", len(info.FuncLinks))
	}
	if !reflect.DeepEqual(info.FuncLinks[0].CutsceneArgs, []string{"CUTSCENE1"}) {
		t.Fatalf("CutsceneArgs = %v, want [CUTSCENE1]", info.FuncLinks[0].CutsceneArgs)
	}
	if !reflect.DeepEqual(info.FuncLinks[0].BGMArgs, []string{"LCUT_BGM1"}) {
		t.Fatalf("BGMArgs = %v, want [LCUT_BGM1]", info.FuncLinks[0].BGMArgs)
	}
	if !reflect.DeepEqual(info.BGMArgs, []string{"LCUT_BGM1"}) {
		t.Fatalf("info.BGMArgs = %v, want [LCUT_BGM1]", info.BGMArgs)
	}
}

func TestRealClientCutsceneAndLuaParsing(t *testing.T) {
	cnRoot := readDotEnvValue("FFXIV_CN_PATH")
	if cnRoot == "" {
		t.Skip("未在 .env 中配置 FFXIV_CN_PATH，跳过实机客户端过场动画解析测试")
	}
	gameDir := filepath.Join(cnRoot, "game")
	if _, err := os.Stat(filepath.Join(gameDir, "sqpack")); err != nil {
		t.Skipf("未找到国服 sqpack 目录 (%v)，跳过", err)
	}

	reader := NewGameReader(gameDir)
	defer reader.Close()

	// 《熄灭最后的光》结局过场 ex3/lucact/lucact05160/lucact05160 含 C070 多首剧情配乐（如 BGM #707/#708）
	cutbData, err := reader.ReadFile("cut/ex3/lucact/lucact05160/lucact05160.cutb")
	if err != nil || len(cutbData) == 0 {
		t.Fatalf("读取 ex3 过场 .cutb 失败: len=%d err=%v", len(cutbData), err)
	}
	cutbInfo, err := ExtractCutbAudio(cutbData)
	if err != nil {
		t.Fatalf("ExtractCutbAudio 失败: %v", err)
	}
	found707, found708 := false, false
	for _, tr := range cutbInfo.Tracks {
		if tr.BGMID == 707 {
			found707 = true
		}
		if tr.BGMID == 708 {
			found708 = true
		}
	}
	if !found707 || !found708 {
		t.Fatalf("lucact05160 未能提取到预期 BGM (707=%t, 708=%t), tracks=%+v", found707, found708, cutbInfo.Tracks)
	}

	// 《灵光卫》脚本 game_script/quest/033/LucKma401_03300.luab
	luaData, err := reader.ReadFile("game_script/quest/033/LucKma401_03300.luab")
	if err != nil || len(luaData) == 0 {
		t.Fatalf("读取任务 .luab 失败: len=%d err=%v", len(luaData), err)
	}
	luaInfo, err := ParseLuabCutsceneAndBGM(luaData)
	if err != nil {
		t.Fatalf("ParseLuabCutsceneAndBGM 失败: %v", err)
	}
	matchedLCUT1 := false
	for _, fn := range luaInfo.FuncLinks {
		hasCut1 := false
		hasBgm1 := false
		for _, c := range fn.CutsceneArgs {
			if strings.EqualFold(c, "CUTSCENE1") {
				hasCut1 = true
			}
		}
		for _, m := range fn.BGMArgs {
			if strings.EqualFold(m, "LCUT_BGM1") {
				hasBgm1 = true
			}
		}
		if hasCut1 && hasBgm1 {
			matchedLCUT1 = true
		}
	}
	if !matchedLCUT1 {
		t.Fatalf("LucKma401_03300.luab 未能解析出 CUTSCENE1 <-> LCUT_BGM1 配对: %+v", luaInfo.FuncLinks)
	}
}

func readDotEnvValue(key string) string {
	for _, cand := range []string{".env", filepath.Join("..", "..", ".env")} {
		b, err := os.ReadFile(cand)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, key+"=") {
				v := strings.TrimSpace(strings.TrimPrefix(line, key+"="))
				return strings.Trim(v, "\"'\r")
			}
		}
	}
	return ""
}
