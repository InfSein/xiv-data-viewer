package sqpack

import (
	"context"
	"testing"
)

func TestResolvePreferredSheetLang(t *testing.T) {
	exhCN := &EXHHeader{Languages: []Language{LangChineseSimplified}}
	suf, loc, ok := resolvePreferredSheetLang(exhCN, LangChineseSimplified)
	if !ok || !loc || suf != "_chs" {
		t.Fatalf("中文表语言解析异常: suf=%q loc=%v ok=%v", suf, loc, ok)
	}

	exhGlobal := &EXHHeader{Languages: []Language{LangJapanese, LangEnglish, LangGerman, LangFrench}}
	suf, loc, ok = resolvePreferredSheetLang(exhGlobal, LangEnglish)
	if !ok || !loc || suf != "_en" {
		t.Fatalf("英语表语言解析异常: suf=%q loc=%v ok=%v", suf, loc, ok)
	}
	if _, _, ok := resolvePreferredSheetLang(exhGlobal, LangChineseSimplified); ok {
		t.Fatalf("不包含目标语言且非通用表时应跳过")
	}

	exhNone := &EXHHeader{Languages: []Language{LangNone}}
	suf, loc, ok = resolvePreferredSheetLang(exhNone, LangChineseSimplified)
	if !ok || loc || suf != "" {
		t.Fatalf("无语言通用表解析异常: suf=%q loc=%v ok=%v", suf, loc, ok)
	}
}

func TestAppendCleanedScannedText(t *testing.T) {
	var blob []byte

	off, n := appendCleanedScannedText(&blob, []byte("  Hello World  "), LangEnglish, nil)
	if string(blob[off:off+n]) != "Hello World" {
		t.Fatalf("ASCII 首尾空白修剪异常: %q", string(blob[off:off+n]))
	}

	off, n = appendCleanedScannedText(&blob, []byte("光之战士"), LangChineseSimplified, nil)
	if string(blob[off:off+n]) != "光之战士" {
		t.Fatalf("常规中文快速路径异常: %q", string(blob[off:off+n]))
	}

	off, n = appendCleanedScannedText(&blob, []byte("\ue0be 主线任务"), LangChineseSimplified, nil)
	if string(blob[off:off+n]) != "主线任务" {
		t.Fatalf("私用区字形清理异常: %q", string(blob[off:off+n]))
	}

	_, n = appendCleanedScannedText(&blob, []byte("   \t\r\n "), LangEnglish, nil)
	if n != 0 {
		t.Fatalf("纯空白文本应跳过，实际长度=%d", n)
	}
}

func TestScanAllEXDStringsNilReader(t *testing.T) {
	res, err := ScanAllEXDStrings(context.Background(), nil, EXDTextScanOptions{
		PreferredLang: LangChineseSimplified,
	})
	if err != nil {
		t.Fatalf("nil reader 不应报错: %v", err)
	}
	if res == nil || len(res.Sheets) != 0 || res.TotalRows != 0 {
		t.Fatalf("nil reader 应返回空结果: %+v", res)
	}
}
