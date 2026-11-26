package sqpack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// CutbBGMTrack 表示单个 `.cutb` 过场动画时间线中记录的 BGM 触发条目（`C070` 节点）。
type CutbBGMTrack struct {
	BGMID  int
	Flags  uint32
	FadeMs int
}

// CutbInfo 汇总解析单个 `.cutb` 过场动画文件提取到的 BGM 引用与预渲染影片音频路径。
type CutbInfo struct {
	Tracks          []CutbBGMTrack
	MovieAudioPaths []string
}

var (
	cutbMagic     = []byte("CUTB")
	cutbTagC070   = []byte("C070")
	cutbMusicPref = []byte("music/")
	luabHeader    = []byte("\x1bLua\x51")
)

// ExtractCutbAudio 从 FF14 `.cutb` 过场动画二进制文件中提取时间线 BGM 轨道（`C070` 记录）与内嵌音轨路径。
// 全流程仅做只读内存解析并执行严格边界检查。
func ExtractCutbAudio(data []byte) (CutbInfo, error) {
	if len(data) < 12 || !bytes.Equal(data[:4], cutbMagic) {
		return CutbInfo{}, fmt.Errorf("无效的 CUTB 过场动画文件头")
	}

	var out CutbInfo
	pos := 12
	for pos+40 <= len(data) {
		rel := bytes.Index(data[pos:], cutbTagC070)
		if rel < 0 {
			break
		}
		at := pos + rel
		pos = at + 4
		if at+40 > len(data) {
			break
		}
		recSize := binary.LittleEndian.Uint32(data[at+4 : at+8])
		if recSize != 40 {
			continue
		}
		flags := binary.LittleEndian.Uint32(data[at+24 : at+28])
		bgmID := binary.LittleEndian.Uint32(data[at+28 : at+32])
		fadeMs := binary.LittleEndian.Uint32(data[at+32 : at+36])
		reserved := binary.LittleEndian.Uint32(data[at+36 : at+40])
		// C070 固定 40 字节结构：末尾保留字段为 0，状态标记仅包含已知标志位（0x0001 / 0x0100）
		if reserved != 0 || (flags&^uint32(0x0101)) != 0 || bgmID == 0 {
			continue
		}
		out.Tracks = append(out.Tracks, CutbBGMTrack{
			BGMID:  int(bgmID),
			Flags:  flags,
			FadeMs: int(fadeMs),
		})
	}

	// 部分预渲染影片型 .cutb 会直接内嵌 "music/.../*.scd" 字符串
	pos = 12
	seenMovie := make(map[string]bool)
	for pos < len(data) {
		rel := bytes.Index(data[pos:], cutbMusicPref)
		if rel < 0 {
			break
		}
		at := pos + rel
		pos = at + len(cutbMusicPref)
		end := at
		for end < len(data) && end-at < 260 && data[end] >= 0x20 && data[end] <= 0x7e {
			end++
		}
		pathStr := string(data[at:end])
		if strings.HasSuffix(strings.ToLower(pathStr), ".scd") && !seenMovie[pathStr] {
			seenMovie[pathStr] = true
			out.MovieAudioPaths = append(out.MovieAudioPaths, pathStr)
		}
	}

	return out, nil
}

// LuabFuncCutsceneBGM 描述单个 Lua 函数作用域内同现的 PlayCutScene 参数与 PlayBGM 参数。
type LuabFuncCutsceneBGM struct {
	CutsceneArgs []string
	BGMArgs      []string
}

// LuabScriptInfo 保存从任务 `.luab`（Lua 5.1 字节码）提取的过场与 BGM 调用关系。
type LuabScriptInfo struct {
	FuncLinks []LuabFuncCutsceneBGM
	BGMArgs   []string
}

type lua51Const struct {
	typ byte
	str string
	num float64
}

type lua51Proto struct {
	code   []uint32
	consts []lua51Const
	subs   []*lua51Proto
}

type lua51Reader struct {
	data    []byte
	pos     int
	intSize int
	szSize  int
}

func (r *lua51Reader) readByte() (byte, bool) {
	if r.pos >= len(r.data) {
		return 0, false
	}
	v := r.data[r.pos]
	r.pos++
	return v, true
}

func (r *lua51Reader) readUint32() (uint32, bool) {
	if r.pos+4 > len(r.data) {
		return 0, false
	}
	v := binary.LittleEndian.Uint32(r.data[r.pos : r.pos+4])
	r.pos += 4
	return v, true
}

func (r *lua51Reader) readInt() (int, bool) {
	if r.intSize != 4 {
		return 0, false
	}
	v, ok := r.readUint32()
	return int(v), ok
}

func (r *lua51Reader) readSizeT() (int, bool) {
	if r.szSize == 4 {
		v, ok := r.readUint32()
		return int(v), ok
	}
	if r.szSize == 8 {
		if r.pos+8 > len(r.data) {
			return 0, false
		}
		v := binary.LittleEndian.Uint64(r.data[r.pos : r.pos+8])
		r.pos += 8
		if v > uint64(len(r.data)) {
			return 0, false
		}
		return int(v), true
	}
	return 0, false
}

func (r *lua51Reader) readString() (string, bool) {
	sz, ok := r.readSizeT()
	if !ok {
		return "", false
	}
	if sz == 0 {
		return "", true
	}
	if sz < 0 || r.pos+sz > len(r.data) {
		return "", false
	}
	raw := r.data[r.pos : r.pos+sz]
	r.pos += sz
	if raw[len(raw)-1] == 0 {
		raw = raw[:len(raw)-1]
	}
	return string(raw), true
}

func (r *lua51Reader) readProto() (*lua51Proto, bool) {
	if _, ok := r.readString(); !ok {
		return nil, false
	}
	// lineDefined(int), lastLineDefined(int), nups(1), numparams(1), is_vararg(1), maxstacksize(1)
	if r.pos+r.intSize*2+4 > len(r.data) {
		return nil, false
	}
	r.pos += r.intSize*2 + 4

	codeN, ok := r.readInt()
	if !ok || codeN < 0 || r.pos+codeN*4 > len(r.data) {
		return nil, false
	}
	p := &lua51Proto{
		code: make([]uint32, codeN),
	}
	for i := 0; i < codeN; i++ {
		ins, ok := r.readUint32()
		if !ok {
			return nil, false
		}
		p.code[i] = ins
	}

	constN, ok := r.readInt()
	if !ok || constN < 0 {
		return nil, false
	}
	p.consts = make([]lua51Const, constN)
	for i := 0; i < constN; i++ {
		t, ok := r.readByte()
		if !ok {
			return nil, false
		}
		c := lua51Const{typ: t}
		switch t {
		case 0: // LUA_TNIL
		case 1: // LUA_TBOOLEAN
			b, ok := r.readByte()
			if !ok {
				return nil, false
			}
			if b != 0 {
				c.num = 1
			}
		case 3: // LUA_TNUMBER (IEEE 754 float64)
			if r.pos+8 > len(r.data) {
				return nil, false
			}
			bits := binary.LittleEndian.Uint64(r.data[r.pos : r.pos+8])
			r.pos += 8
			c.num = math.Float64frombits(bits)
		case 4: // LUA_TSTRING
			s, ok := r.readString()
			if !ok {
				return nil, false
			}
			c.str = s
		default:
			return nil, false
		}
		p.consts[i] = c
	}

	subN, ok := r.readInt()
	if !ok || subN < 0 {
		return nil, false
	}
	p.subs = make([]*lua51Proto, 0, subN)
	for i := 0; i < subN; i++ {
		sp, ok := r.readProto()
		if !ok {
			return nil, false
		}
		p.subs = append(p.subs, sp)
	}

	// 跳过调试行号、局部变量与 upvalue 名称区，保证兄弟/父级作用域续读对齐
	lineInfoN, ok := r.readInt()
	if !ok || lineInfoN < 0 || r.pos+lineInfoN*4 > len(r.data) {
		return nil, false
	}
	r.pos += lineInfoN * 4

	locVarN, ok := r.readInt()
	if !ok || locVarN < 0 {
		return nil, false
	}
	for i := 0; i < locVarN; i++ {
		if _, ok := r.readString(); !ok || r.pos+8 > len(r.data) {
			return nil, false
		}
		r.pos += 8
	}

	upvalN, ok := r.readInt()
	if !ok || upvalN < 0 {
		return nil, false
	}
	for i := 0; i < upvalN; i++ {
		if _, ok := r.readString(); !ok {
			return nil, false
		}
	}

	return p, true
}

// ParseLuabCutsceneAndBGM 解析 FF14 客户端 `.luab`（Lua 5.1 字节码）中的 PlayCutScene 与 PlayBGM 调用关系。
func ParseLuabCutsceneAndBGM(data []byte) (LuabScriptInfo, error) {
	if len(data) < 12 || !bytes.Equal(data[:5], luabHeader) {
		return LuabScriptInfo{}, fmt.Errorf("无效的 Lua 5.1 字节码文件头")
	}
	// header: magic(4) version(1) format(1) endian(1) int(1) size_t(1) inst(1) number(1) integral(1)
	if data[6] != 1 || data[9] != 4 || data[10] != 8 || data[11] != 0 {
		return LuabScriptInfo{}, fmt.Errorf("不支持的 Lua 5.1 字节码平台字宽")
	}
	r := &lua51Reader{
		data:    data,
		pos:     12,
		intSize: int(data[7]),
		szSize:  int(data[8]),
	}
	root, ok := r.readProto()
	if !ok || root == nil {
		return LuabScriptInfo{}, fmt.Errorf("解析 Lua 5.1 字节码结构失败")
	}

	var info LuabScriptInfo
	seenAllBGM := make(map[string]bool)
	var walk func(p *lua51Proto)
	walk = func(p *lua51Proto) {
		link := traceLua51ProtoCalls(p)
		if len(link.CutsceneArgs) > 0 && len(link.BGMArgs) > 0 {
			info.FuncLinks = append(info.FuncLinks, link)
		}
		for _, bArg := range link.BGMArgs {
			if !seenAllBGM[bArg] {
				seenAllBGM[bArg] = true
				info.BGMArgs = append(info.BGMArgs, bArg)
			}
		}
		for _, sub := range p.subs {
			walk(sub)
		}
	}
	walk(root)
	return info, nil
}

func traceLua51ProtoCalls(p *lua51Proto) LuabFuncCutsceneBGM {
	const (
		opMOVE    = 0
		opLOADK   = 1
		opLOADNIL = 3
		opGETGLOB = 5
		opGETTAB  = 6
		opSELF    = 11
		opCALL    = 28
	)

	regStr := make([]string, 256)
	regCallKind := make([]byte, 256) // 1=PlayCutScene, 2=PlayBGM/PlayBGMWithVolume
	constStr := func(idx int) string {
		if idx >= 0 && idx < len(p.consts) && p.consts[idx].typ == 4 {
			return p.consts[idx].str
		}
		return ""
	}
	rkStr := func(rk int) string {
		if rk >= 256 {
			return constStr(rk - 256)
		}
		if rk >= 0 && rk < 256 {
			return regStr[rk]
		}
		return ""
	}

	var out LuabFuncCutsceneBGM
	seenCut := make(map[string]bool)
	seenBGM := make(map[string]bool)

	for _, ins := range p.code {
		op := int(ins & 0x3F)
		a := int((ins >> 6) & 0xFF)
		c := int((ins >> 14) & 0x1FF)
		b := int((ins >> 23) & 0x1FF)
		bx := int(ins >> 14)

		switch op {
		case opMOVE:
			if a < 256 && b < 256 {
				regStr[a] = regStr[b]
				regCallKind[a] = regCallKind[b]
			}
		case opLOADK:
			if a < 256 {
				regStr[a] = constStr(bx)
				regCallKind[a] = 0
			}
		case opLOADNIL:
			for r := a; r <= b && r < 256; r++ {
				regStr[r] = ""
				regCallKind[r] = 0
			}
		case opGETGLOB:
			if a < 256 {
				regStr[a] = constStr(bx)
				regCallKind[a] = 0
			}
		case opGETTAB:
			if a < 256 {
				regStr[a] = rkStr(c)
				regCallKind[a] = 0
			}
		case opSELF:
			if a < 256 {
				method := rkStr(c)
				switch method {
				case "PlayCutScene":
					regCallKind[a] = 1
				case "PlayBGM", "PlayBGMWithVolume":
					regCallKind[a] = 2
				default:
					regCallKind[a] = 0
				}
				regStr[a] = method
				if a+1 < 256 {
					regStr[a+1] = ""
					regCallKind[a+1] = 0
				}
			}
		case opCALL:
			if a < 256 && regCallKind[a] != 0 && (b == 0 || b >= 3) {
				arg := strings.TrimSpace(regStr[a+2])
				if arg == "" {
					continue
				}
				switch regCallKind[a] {
				case 1:
					if !seenCut[arg] {
						seenCut[arg] = true
						out.CutsceneArgs = append(out.CutsceneArgs, arg)
					}
				case 2:
					if !strings.EqualFold(arg, "BGM_MUSIC_NO_MUSIC") && !seenBGM[arg] {
						seenBGM[arg] = true
						out.BGMArgs = append(out.BGMArgs, arg)
					}
				}
			}
		}
	}
	return out
}
