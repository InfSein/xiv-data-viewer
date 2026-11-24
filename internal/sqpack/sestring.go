package sqpack

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	sePayloadStart byte = 0x02
	sePayloadEnd   byte = 0x03
)

const (
	exprInteger     = 0
	exprPlaceholder = 1
	exprBinary      = 2
	exprParameter   = 3
	exprString      = 4
)

type seExpression struct {
	exprType   int
	intVal     uint32
	comparison byte
	left       *seExpression
	right      *seExpression
	paramKind  byte
	subParts   []sePart
	raw        []byte
	length     int
}

type sePayload struct {
	tagType byte
	raw     []byte
	data    []seExpression
	length  int
}

type sePart struct {
	isPayload bool
	text      string
	payload   sePayload
}

// DecodeSeString 将 EXD 原始字符串字节解码为纯文本，若含 0x02 控制标签则解析并格式化。
func DecodeSeString(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	// 快速路径：不含控制标签起始字节 0x02 时直接按 UTF-8 返回
	if bytes.IndexByte(raw, sePayloadStart) < 0 {
		return strings.TrimSpace(string(raw))
	}
	parts, err := parseSeString(raw)
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return strings.TrimSpace(formatSeStringText(parts, "\n"))
}

func readSeExpression(buf []byte, from int) (seExpression, error) {
	if from < 0 || from >= len(buf) {
		return seExpression{}, fmt.Errorf("表达式偏移越界: %d", from)
	}
	t := buf[from]
	switch {
	case t < 208:
		return seExpression{
			exprType: exprInteger,
			intVal:   uint32(t) - 1,
			raw:      buf[from : from+1],
			length:   1,
		}, nil
	case (t >= 208 && t <= 223) || t == 236:
		return seExpression{
			exprType: exprPlaceholder,
			intVal:   uint32(t) - 1,
			raw:      buf[from : from+1],
			length:   1,
		}, nil
	case t >= 224 && t <= 229:
		left, err := readSeExpression(buf, from+1)
		if err != nil {
			return seExpression{}, err
		}
		right, err := readSeExpression(buf, from+1+left.length)
		if err != nil {
			return seExpression{}, err
		}
		total := 1 + left.length + right.length
		return seExpression{
			exprType:   exprBinary,
			comparison: t,
			left:       &left,
			right:      &right,
			raw:        buf[from : from+total],
			length:     total,
		}, nil
	case t >= 232 && t <= 235:
		if from+1 >= len(buf) {
			return seExpression{}, errors.New("参数表达式字节不足")
		}
		return seExpression{
			exprType:  exprParameter,
			paramKind: t,
			intVal:    uint32(buf[from+1]) - 1,
			raw:       buf[from : from+2],
			length:    2,
		}, nil
	case t >= 240 && t <= 254:
		typeByte := t - 240 + 1
		var val uint32
		valLen := 0
		if (typeByte & 8) != 0 {
			valLen++
			if from+valLen >= len(buf) {
				return seExpression{}, errors.New("变长整数截断")
			}
			val += uint32(buf[from+valLen]) << 24
		}
		if (typeByte & 4) != 0 {
			valLen++
			if from+valLen >= len(buf) {
				return seExpression{}, errors.New("变长整数截断")
			}
			val += uint32(buf[from+valLen]) << 16
		}
		if (typeByte & 2) != 0 {
			valLen++
			if from+valLen >= len(buf) {
				return seExpression{}, errors.New("变长整数截断")
			}
			val += uint32(buf[from+valLen]) << 8
		}
		if (typeByte & 1) != 0 {
			valLen++
			if from+valLen >= len(buf) {
				return seExpression{}, errors.New("变长整数截断")
			}
			val += uint32(buf[from+valLen])
		}
		total := valLen + 1
		return seExpression{
			exprType: exprInteger,
			intVal:   val,
			raw:      buf[from : from+total],
			length:   total,
		}, nil
	case t == 255:
		lenExp, err := readSeExpression(buf, from+1)
		if err != nil {
			return seExpression{}, err
		}
		if lenExp.exprType != exprInteger {
			return seExpression{}, errors.New("内嵌字符串长度表达式类型错误")
		}
		strStart := from + 1 + lenExp.length
		strEnd := strStart + int(lenExp.intVal)
		if strEnd > len(buf) {
			return seExpression{}, errors.New("内嵌字符串越界")
		}
		sub, err := parseSeString(buf[strStart:strEnd])
		if err != nil {
			return seExpression{}, err
		}
		total := 1 + lenExp.length + int(lenExp.intVal)
		return seExpression{
			exprType: exprString,
			subParts: sub,
			raw:      buf[from : from+total],
			length:   total,
		}, nil
	default:
		return seExpression{}, fmt.Errorf("未知的表达式类型: 0x%02x", t)
	}
}

func parseSeExpressions(buf []byte) ([]seExpression, error) {
	var out []seExpression
	cursor := 0
	for cursor < len(buf) {
		exp, err := readSeExpression(buf, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, exp)
		cursor += exp.length
	}
	return out, nil
}

func readSePayload(buf []byte, from int) (sePayload, error) {
	if len(buf)-from < 4 {
		return sePayload{}, errors.New("SeString 标签剩余字节不足")
	}
	tagType := buf[from+1]
	lenExp, err := readSeExpression(buf, from+2)
	if err != nil {
		return sePayload{}, err
	}
	if lenExp.exprType != exprInteger {
		return sePayload{}, errors.New("SeString 标签长度非整数")
	}
	dataStart := from + 2 + lenExp.length
	dataEnd := dataStart + int(lenExp.intVal)
	if dataEnd >= len(buf) || buf[dataEnd] != sePayloadEnd {
		return sePayload{}, errors.New("SeString 标签结束符无效")
	}
	var exps []seExpression
	if lenExp.intVal > 0 {
		exps, err = parseSeExpressions(buf[dataStart:dataEnd])
		if err != nil {
			return sePayload{}, err
		}
	}
	total := 2 + lenExp.length + int(lenExp.intVal) + 1
	return sePayload{
		tagType: tagType,
		raw:     buf[from:dataEnd],
		data:    exps,
		length:  total,
	}, nil
}

func parseSeString(buf []byte) ([]sePart, error) {
	var parts []sePart
	pos := 0
	cursor := 0
	for cursor < len(buf) {
		if buf[cursor] != sePayloadStart {
			cursor++
			continue
		}
		if pos != cursor {
			parts = append(parts, sePart{text: string(buf[pos:cursor])})
		}
		payload, err := readSePayload(buf, cursor)
		if err != nil {
			return nil, err
		}
		parts = append(parts, sePart{isPayload: true, payload: payload})
		pos = cursor + payload.length
		cursor = pos
	}
	if pos < len(buf) {
		parts = append(parts, sePart{text: string(buf[pos:])})
	}
	return parts, nil
}

func seTagName(t byte) string {
	names := map[byte]string{
		8:  "If",
		9:  "Switch",
		16: "LineBreak",
		19: "Color",
		22: "SoftHyphen",
		26: "Emphasis",
		32: "Value",
		34: "Format",
		36: "TwoDigitValue",
		41: "Highlight",
		80: "ZeroPaddedValue",
	}
	if n, ok := names[t]; ok {
		return n
	}
	return fmt.Sprintf("%d", t)
}

func formatSeExpression(exp seExpression, lineBreak string) string {
	switch exp.exprType {
	case exprInteger:
		return fmt.Sprintf("%d", exp.intVal)
	case exprPlaceholder:
		return fmt.Sprintf("TopLevelParameter(%d)", exp.intVal)
	case exprString:
		return formatSeStringText(exp.subParts, lineBreak)
	case exprParameter:
		switch exp.paramKind {
		case 232:
			return fmt.Sprintf("IntegerParameter(%d)", exp.intVal)
		case 233:
			return fmt.Sprintf("PlayerParameter(%d)", exp.intVal)
		case 234:
			return fmt.Sprintf("StringParameter(%d)", exp.intVal)
		case 235:
			return fmt.Sprintf("ObjectParameter(%d)", exp.intVal)
		}
	case exprBinary:
		if exp.left != nil && exp.right != nil {
			op := "Compare"
			switch exp.comparison {
			case 224:
				op = "GreaterThanOrEqualTo"
			case 225:
				op = "GreaterThan"
			case 226:
				op = "LessThanOrEqualTo"
			case 227:
				op = "LessThan"
			case 228:
				op = "Equal"
			case 229:
				op = "NotEqual"
			}
			return fmt.Sprintf("%s(%s,%s)", op, formatSeExpression(*exp.left, lineBreak), formatSeExpression(*exp.right, lineBreak))
		}
	}
	return ""
}

func formatSeStringText(parts []sePart, lineBreak string) string {
	var b strings.Builder
	for _, p := range parts {
		if !p.isPayload {
			b.WriteString(p.text)
			continue
		}
		pl := p.payload
		tagName := seTagName(pl.tagType)
		if len(pl.data) == 0 {
			switch pl.tagType {
			case 16: // LineBreak
				b.WriteString(lineBreak)
			case 22: // SoftHyphen
				b.WriteByte('-')
			}
			continue
		}

		switch pl.tagType {
		case 8: // If
			if len(pl.data) == 3 {
				fmt.Fprintf(&b, "<%s(%s)>%s<Else/>%s</%s>",
					tagName,
					formatSeExpression(pl.data[0], lineBreak),
					formatSeExpression(pl.data[1], lineBreak),
					formatSeExpression(pl.data[2], lineBreak),
					tagName,
				)
			}
		case 9: // Switch
			if len(pl.data) >= 2 {
				fmt.Fprintf(&b, "<%s(%s)>", tagName, formatSeExpression(pl.data[0], lineBreak))
				for i := 1; i < len(pl.data); i++ {
					fmt.Fprintf(&b, "<Case(%d)>%s</Case>", i, formatSeExpression(pl.data[i], lineBreak))
				}
				fmt.Fprintf(&b, "</%s>", tagName)
			}
		case 32, 36, 41: // Value, TwoDigitValue, Highlight
			b.WriteByte('<')
			b.WriteString(tagName)
			b.WriteByte('>')
			for _, e := range pl.data {
				b.WriteString(formatSeExpression(e, lineBreak))
			}
			b.WriteString("</")
			b.WriteString(tagName)
			b.WriteByte('>')
		case 80: // ZeroPaddedValue
			if len(pl.data) == 2 {
				fmt.Fprintf(&b, "<%s(%s)>%s</%s>",
					tagName,
					formatSeExpression(pl.data[1], lineBreak),
					formatSeExpression(pl.data[0], lineBreak),
					tagName,
				)
			}
		case 34: // Format
			if len(pl.data) == 2 {
				fmt.Fprintf(&b, "<%s(%s,%s)/>",
					tagName,
					formatSeExpression(pl.data[0], lineBreak),
					strings.ToUpper(hex.EncodeToString(pl.data[1].raw)),
				)
			}
		default:
			// 纯文本渲染模式忽略样式、颜色、图标、点击链接等控制标签
		}
	}
	return b.String()
}
