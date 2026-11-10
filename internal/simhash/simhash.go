// Package simhash 64 位 SimHash 指纹（Charikar 算法），纯标准库实现。
// 两级用途：文档级（幂等导入）+ 块级（近重复去重）。
// 判定：汉明距离 <= 3 视为近重复（经验参数，块级阈值可实测校准）。
package simhash

import "ragbot/internal/tokenize"

// Fingerprint 对 token 序列计算 64 位 SimHash
func Fingerprint(tokens []string) uint64 {
	var v [64]int
	for _, tok := range tokens {
		h := fnv64(tok)
		for i := 0; i < 64; i++ {
			if (h>>uint(i))&1 == 1 {
				v[i]++
			} else {
				v[i]--
			}
		}
	}
	var out uint64
	for i := 0; i < 64; i++ {
		if v[i] > 0 {
			out |= 1 << uint(i)
		}
	}
	return out
}

// Text 便捷入口
func Text(text string) uint64 {
	return Fingerprint(tokenize.Tokenize(text))
}

// Hamming 汉明距离
func Hamming(a, b uint64) int {
	x := a ^ b
	n := 0
	for x != 0 {
		x &= x - 1
		n++
	}
	return n
}

// NearDuplicate 汉明距离阈值判定
func NearDuplicate(a, b uint64) bool {
	return Hamming(a, b) <= 3
}

// fnv64 FNV-1a 64 位哈希（确定性，无碰撞担忧场景够用）
func fnv64(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}
