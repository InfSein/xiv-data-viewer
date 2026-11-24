package sqpack

import (
	"fmt"
	"hash/crc32"
	"strings"
)

// JamCRC32 计算 FF14 SqPack 使用的 JamCRC32（IEEE 多项式但省略末尾按位取反）。
func JamCRC32(data []byte) uint32 {
	return ^crc32.ChecksumIEEE(data)
}

// CalculateIndexHash 计算 .index 文件使用的 64 位路径哈希（高 32 位为目录哈希，低 32 位为文件名哈希）。
func CalculateIndexHash(filePath string) (uint64, error) {
	lower := strings.ToLower(filePath)
	idx := strings.LastIndexByte(lower, '/')
	if idx < 0 {
		return 0, fmt.Errorf("无效的资源路径: %s", filePath)
	}
	dirHash := JamCRC32([]byte(lower[:idx]))
	fileHash := JamCRC32([]byte(lower[idx+1:]))
	return (uint64(dirHash) << 32) | uint64(fileHash), nil
}

// CalculateIndex2Hash 计算 .index2 文件使用的 32 位全路径哈希。
func CalculateIndex2Hash(filePath string) uint32 {
	return JamCRC32([]byte(strings.ToLower(filePath)))
}
