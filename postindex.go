// Package postindex 是一个只用标准库实现的倒排索引库。
//
// 查询语义逐条写在 README 的「对外契约」一节里；本文件只给出类型与签名，
// 方法体全部是 panic，需要按契约实现。
package postindex

// Doc 是一篇已切词的文档。
//
// Terms 已经切好词，按出现顺序排列（下标即位置）。
type Doc struct {
	ID    int
	Terms []string
}

// Posting 是倒排表里的一条：某篇文档 + 该词在这篇文档里的位置（升序去重）。
type Posting struct {
	Doc       int
	Positions []int
}

// Query 是一层查询子句。五种子句里恰好设置一种：
//
//	Term    单个词        （Term != ""       表示设置了）
//	And     交集          （And != nil       表示设置了）
//	Or      并集          （Or != nil        表示设置了）
//	Not     全集差集      （Not != nil       表示设置了，只允许出现在顶层节点）
//	Phrase  位置连续递增的短语（Phrase != nil 表示设置了）
//
// 子句可以任意深浅递归嵌套。
type Query struct {
	Term   string
	And    []Query
	Or     []Query
	Not    []Query
	Phrase []string
}

// Result 是一次查询的结果。字段名与类型已定死，不要改。
//
//	Docs       命中文档 ID 的升序去重列表
//	Candidates 各子句候选数的递归求和（口径见 README 契约第 6 条）
type Result struct {
	Docs       []int
	Candidates int
}

// Explain 树里 Kind 字段的取值。
const (
	KindTerm   = "term"
	KindAnd    = "and"
	KindOr     = "or"
	KindNot    = "not"
	KindPhrase = "phrase"
)

// PlanStep 是 Plan.Steps 里的一步：按**实际求值次序**展开的一个子句。
//
//	Path       从根到该子句的子索引路径（按查询的书写顺序编号；根节点的 Path 为空）
//	Kind       该子句的种类：KindTerm / KindAnd / KindOr / KindNot / KindPhrase
//	Term       Kind == KindTerm 时该叶子对应的词
//	Candidates 该子句的候选数（口径见 README 契约第 6 条）
type PlanStep struct {
	Path       []int
	Kind       string
	Term       string
	Candidates int
}

// Plan 是一次查询的实际求值计划。Steps 的顺序就是求值发生的次序。
type Plan struct {
	Steps []PlanStep
}

// Index 是建好的一版倒排索引。内部表示完全自由，可以新增字段与方法。
type Index struct{}

// Build 按 docs 从头建一版索引。nil 文档切片表示空索引。
//
// 重复调用必须每次从头重建，不残留上一次的状态。
func Build(docs []Doc) *Index {
	panic("not implemented")
}

// Postings 返回词 term 的倒排表：按 Doc 升序；每条的 Positions 升序去重。
// 索引里没有这个词时返回空切片。
func (ix *Index) Postings(term string) []Posting {
	panic("not implemented")
}

// Query 求值查询表达式。非法查询返回零值 Result（不 panic）。
func (ix *Index) Query(q Query) Result {
	panic("not implemented")
}

// Explain 给出查询表达式的实际求值计划。非法查询返回 ErrBadQuery（不 panic）。
func (ix *Index) Explain(q Query) (Plan, error) {
	panic("not implemented")
}
