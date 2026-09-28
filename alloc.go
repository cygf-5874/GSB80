package postindex

import "testing"

// Allocs 返回一次 fn 调用的堆分配次数（用 testing.AllocsPerRun 度量）。
//
// 固定件用它判定「一次 Query 调用的分配预算」。**签名不要改**。
func Allocs(fn func()) float64 {
	return testing.AllocsPerRun(1, fn)
}
