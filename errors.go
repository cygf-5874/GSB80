package postindex

import "errors"

// 本包所有失败路径都返回下面两个哨兵错误之一（或其包装）。
// 调用方用 errors.Is 判定，不要比较错误字符串。
var (
	// ErrBadQuery 表示查询表达式非法。满足下列任一条即非法：
	//
	//   - 一个节点没有设置五种子句中的任何一种，或同时设置了两种以上；
	//   - Not 出现在顶层节点以外的位置（包括 Not 自己的子树里）。
	//
	// Query / Explain 都不会因为非法查询 panic：Explain 返回 ErrBadQuery，
	// Query 返回零值 Result。
	ErrBadQuery = errors.New("postindex: malformed query")

	// ErrBadDoc 表示文档本身非法（例如 ID 为负）。
	//
	// Build 采用宽松口径、不做校验，因此不会主动返回它；需要严格校验的
	// 调用方可以在自己的校验层里复用这个错误值。
	ErrBadDoc = errors.New("postindex: malformed document")
)
