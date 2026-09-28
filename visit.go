package postindex

// 叶子访问计数器：固定件用它度量一次查询在倒排表上读了多少。
//
// 「叶子访问次数」的计数口径（README 契约第 10 条）：
//   - 从某个词叶子的倒排表里取出一个文档条目，记 1 次；
//   - 对一个当前候选文档在某个词叶子上做一次命中判定，记 1 次。
//
// 实现必须在求值过程中调用 CountVisit 汇报，否则规模判据不成立。
var leafVisits int64

// ResetVisits 把计数清零。固定件在每个规模场景开始前调用。**签名不要改**。
func ResetVisits() { leafVisits = 0 }

// LeafVisits 返回自上次 ResetVisits 以来的叶子访问次数。**签名不要改**。
func LeafVisits() int64 { return leafVisits }

// CountVisit 累加叶子访问次数；n <= 0 时不做任何事。
func CountVisit(n int) {
	if n > 0 {
		leafVisits += int64(n)
	}
}
