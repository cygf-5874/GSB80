// Command check 是 postindex 的固定验收程序。
//
// ⚠️ 不要修改本文件。它是判定「题目有没有做对」的依据；
// 修改它只会让判定失效，不会让实现变对。
//
// 12 个场景分六组：
//
//	build 2：位置升序去重 / 重复 Build 无残留
//	query 3：多层嵌套 And/Or/Not / Phrase 连续递增 / Not 的差集语义
//	order 2：Docs 升序去重 / 同输入多次调用逐字段相同
//	plan  2：Candidates 与 Plan 词叶子计数一致 / Steps 的先后与候选数升序一致
//	alloc 2：一次 Query 的分配预算 / 分配数不随规模增长
//	scale 1：10 万文档下一次 And 查询的叶子访问次数上界
//
// 每个场景都在本进程里跑，panic 会被逐个捕获并记成 FAIL —— 起点是空壳时
// 每个场景都会 panic，固定件必须把 12 个场景全部跑完再收尾。
//
// 用法：
//
//	go run ./check                  # 跑全部场景
//	go run ./check --only plan      # 只跑一组
//	go run ./check -list            # 列出场景
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"postindex"
)

type scenario struct {
	Group string
	Name  string
	Run   func() error
}

var scenarios = []scenario{
	{"build", "B1_positions_dedup_sorted", b1},
	{"build", "B2_rebuild_no_residue", b2},
	{"query", "Q1_nested_and_or_not", q1},
	{"query", "Q2_phrase_contiguous", q2},
	{"query", "Q3_not_universe_diff", q3},
	{"order", "O1_docs_sorted_unique", o1},
	{"order", "O2_repeated_calls_identical", o2},
	{"plan", "P1_candidates_leaf_sum", p1},
	{"plan", "P2_steps_cost_order", p2},
	{"alloc", "A1_query_alloc_budget", a1},
	{"alloc", "A2_alloc_independent_of_size", a2},
	{"scale", "S1_leaf_visits_bounded", s1},
}

var groups = []string{"build", "query", "order", "plan", "alloc", "scale"}

func main() {
	only := flag.String("only", "", "只跑指定组：build / query / order / plan / alloc / scale")
	list := flag.Bool("list", false, "列出全部场景")
	flag.Parse()

	if *list {
		for _, sc := range scenarios {
			fmt.Printf("%-8s %s\n", sc.Group, sc.Name)
		}
		return
	}

	selected := groups
	if *only != "" {
		found := false
		for _, g := range groups {
			if g == *only {
				found = true
			}
		}
		if !found {
			fmt.Printf("未知分组 %q（可选：build / query / order / plan / alloc / scale）\n", *only)
			os.Exit(2)
		}
		selected = []string{*only}
	}

	total, passed := 0, 0
	for _, g := range selected {
		fmt.Printf("== 组 %s ==\n", g)
		for _, sc := range scenarios {
			if sc.Group != g {
				continue
			}
			total++
			if err := runScenario(sc); err != nil {
				fmt.Printf("FAIL %s/%s  %s\n", sc.Group, sc.Name, err.Error())
				continue
			}
			passed++
			fmt.Printf("PASS %s/%s\n", sc.Group, sc.Name)
		}
	}

	fmt.Println()
	fmt.Printf("结果：通过 %d/%d\n", passed, total)
	if passed != total {
		os.Exit(1)
	}
}

// runScenario 捕获场景里的 panic，把它变成一条 FAIL 记录后继续跑下一个场景。
func runScenario(sc scenario) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = expect("场景正常结束（不 panic）", fmt.Sprintf("panic: %v", r))
		}
	}()
	return sc.Run()
}

// ---------------------------------------------------------------- 失败描述

// detailError 的 Error() 恰好就是「期望=… 实际=…」。
type detailError struct{ msg string }

func (e *detailError) Error() string { return e.msg }

func expect(want, got string) error {
	return &detailError{msg: "期望=" + want + " 实际=" + got}
}

func errStr(err error) string {
	if err == nil {
		return "nil（没有返回错误）"
	}
	return err.Error()
}

func f2s(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ------------------------------------------------------------------ 断言

func doc(id int, terms ...string) postindex.Doc {
	if terms == nil {
		terms = []string{}
	}
	return postindex.Doc{ID: id, Terms: terms}
}

func fmtIDs(ids []int) string {
	if len(ids) == 0 {
		return "(空)"
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ",")
}

func sameIDs(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func checkDocs(what string, got []int, want ...int) error {
	if !sameIDs(got, want) {
		return expect(what+" 的 Docs=["+fmtIDs(want)+"]", "["+fmtIDs(got)+"]")
	}
	return nil
}

func checkCount(what string, got, want int) error {
	if got != want {
		return expect(fmt.Sprintf("%s 的 Candidates=%d", what, want), strconv.Itoa(got))
	}
	return nil
}

// checkStrictAsc 要求 ids 严格递增（同时覆盖「升序」与「去重」）。
func checkStrictAsc(what string, ids []int) error {
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			return expect(what+" 的 Docs 严格升序且无重复", fmt.Sprintf("[%s]", fmtIDs(ids)))
		}
	}
	return nil
}

func fmtPostings(ps []postindex.Posting) string {
	if len(ps) == 0 {
		return "(空)"
	}
	parts := make([]string, 0, len(ps))
	for _, p := range ps {
		parts = append(parts, fmt.Sprintf("%d:%v", p.Doc, p.Positions))
	}
	return strings.Join(parts, " ")
}

func samePostings(got, want []postindex.Posting) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].Doc != want[i].Doc || !sameIDs(got[i].Positions, want[i].Positions) {
			return false
		}
	}
	return true
}

func checkPostings(what string, got []postindex.Posting, want ...postindex.Posting) error {
	if !samePostings(got, want) {
		return expect(what+" 的倒排="+fmtPostings(want), fmtPostings(got))
	}
	for _, p := range got {
		if err := checkStrictAsc(fmt.Sprintf("%s 的文档 %d 的位置列表", what, p.Doc), p.Positions); err != nil {
			return err
		}
	}
	return nil
}

func pathString(p []int) string {
	if len(p) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(p))
	for _, v := range p {
		parts = append(parts, strconv.Itoa(v))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func samePath(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fmtSteps(steps []postindex.PlanStep) string {
	parts := make([]string, 0, len(steps))
	for _, s := range steps {
		parts = append(parts, fmt.Sprintf("%s:%s(%d)", pathString(s.Path), s.Kind, s.Candidates))
	}
	return strings.Join(parts, " ")
}

// sumLeafCounts 把 Plan 里所有词叶子（Kind == KindTerm）的候选数相加。
// 契约第 6 条：Result.Candidates 必须与它完全一致。Not 子树里的节点候选数全是 0，
// 因此词叶子求和天然把整个 Not 分支排除在外。
func sumLeafCounts(steps []postindex.PlanStep) int {
	total := 0
	for _, s := range steps {
		if s.Kind == postindex.KindTerm {
			total += s.Candidates
		}
	}
	return total
}

// requirePlan 取出一个必须合法的查询的实际求值计划。
func requirePlan(ix *postindex.Index, what string, q postindex.Query) (postindex.Plan, error) {
	plan, err := ix.Explain(q)
	if err != nil {
		return postindex.Plan{}, expect(what+" 的 Explain 成功", errStr(err))
	}
	if len(plan.Steps) == 0 {
		return postindex.Plan{}, expect(what+" 的 Plan.Steps 非空", "0 步")
	}
	return plan, nil
}

// requireBadQuery 要求 Explain 因为非法查询返回 ErrBadQuery，且 Query 不 panic 并给出零值。
func requireBadQuery(ix *postindex.Index, what string, q postindex.Query) error {
	plan, err := ix.Explain(q)
	if !errors.Is(err, postindex.ErrBadQuery) {
		return expect(what+" 的 Explain 返回 ErrBadQuery", errStr(err))
	}
	if len(plan.Steps) != 0 {
		return expect(what+" 的 Explain 在报错时返回零值 Plan",
			fmt.Sprintf("Steps=%d", len(plan.Steps)))
	}
	got := ix.Query(q)
	if len(got.Docs) != 0 || got.Candidates != 0 {
		return expect(what+" 的 Query 返回零值 Result",
			fmt.Sprintf("Docs=[%s] Candidates=%d", fmtIDs(got.Docs), got.Candidates))
	}
	return nil
}

// -------------------------------------------------------------------- build

func b1() error {
	ix := postindex.Build([]postindex.Doc{
		doc(1, "go", "rust", "go", "go"),
		doc(4, "go", "c", "rust"),
		doc(2),
		doc(3, "rust", "rust"),
	})

	if err := checkPostings("Postings(go)", ix.Postings("go"),
		postindex.Posting{Doc: 1, Positions: []int{0, 2, 3}},
		postindex.Posting{Doc: 4, Positions: []int{0}},
	); err != nil {
		return err
	}
	if err := checkPostings("Postings(rust)", ix.Postings("rust"),
		postindex.Posting{Doc: 1, Positions: []int{1}},
		postindex.Posting{Doc: 3, Positions: []int{0, 1}},
		postindex.Posting{Doc: 4, Positions: []int{2}},
	); err != nil {
		return err
	}
	if err := checkPostings("Postings(c)", ix.Postings("c"),
		postindex.Posting{Doc: 4, Positions: []int{1}},
	); err != nil {
		return err
	}
	if got := ix.Postings("nosuch"); len(got) != 0 {
		return expect("Postings(nosuch) 是空切片", fmtPostings(got))
	}
	// 空文档（ID 2，一个词都没有）不得 panic，也不出现在任何倒排里。
	if got := ix.Postings(""); len(got) != 0 {
		return expect("Postings(空词) 是空切片", fmtPostings(got))
	}

	// 同一个 ID 出现多次：以最后一条为准。
	dup := postindex.Build([]postindex.Doc{
		doc(9, "first"),
		doc(9, "second", "second"),
	})
	if err := checkPostings("重复 ID 的 Postings(second)", dup.Postings("second"),
		postindex.Posting{Doc: 9, Positions: []int{0, 1}},
	); err != nil {
		return err
	}
	if got := dup.Postings("first"); len(got) != 0 {
		return expect("重复 ID 时以最后一条为准，Postings(first) 是空切片", fmtPostings(got))
	}
	return nil
}

func b2() error {
	ix := postindex.Build([]postindex.Doc{
		doc(1, "alpha", "beta"),
		doc(2, "gamma"),
	})
	if err := checkPostings("第一次构建的 Postings(alpha)", ix.Postings("alpha"),
		postindex.Posting{Doc: 1, Positions: []int{0}},
	); err != nil {
		return err
	}
	if err := checkDocs("第一次构建的 Term(gamma)",
		ix.Query(postindex.Query{Term: "gamma"}).Docs, 2); err != nil {
		return err
	}

	// 同一个包级函数再建一版：上一版的词、文档与状态一点都不能留。
	ix = postindex.Build([]postindex.Doc{
		doc(5, "delta"),
		doc(7, "alpha", "alpha"),
	})
	if err := checkPostings("重建后 Postings(alpha)", ix.Postings("alpha"),
		postindex.Posting{Doc: 7, Positions: []int{0, 1}},
	); err != nil {
		return err
	}
	for _, t := range []string{"beta", "gamma"} {
		if got := ix.Postings(t); len(got) != 0 {
			return expect(fmt.Sprintf("重建后 Postings(%s) 是空切片", t), fmtPostings(got))
		}
	}
	if err := checkDocs("重建后 Term(gamma)",
		ix.Query(postindex.Query{Term: "gamma"}).Docs); err != nil {
		return err
	}
	// 全集也必须只剩 {5,7}：Not{alpha} = 全集 \ {7} = {5}。
	if err := checkDocs("重建后 Not{alpha}",
		ix.Query(postindex.Query{Not: []postindex.Query{{Term: "alpha"}}}).Docs, 5); err != nil {
		return err
	}

	// 空索引：nil 文档切片。
	empty := postindex.Build(nil)
	if got := empty.Postings("alpha"); len(got) != 0 {
		return expect("空索引的 Postings(alpha) 是空切片", fmtPostings(got))
	}
	if err := checkDocs("空索引的 Term(alpha)",
		empty.Query(postindex.Query{Term: "alpha"}).Docs); err != nil {
		return err
	}
	if err := checkDocs("空索引的 Not{}",
		empty.Query(postindex.Query{Not: []postindex.Query{}}).Docs); err != nil {
		return err
	}
	return nil
}

// -------------------------------------------------------------------- query

func q1() error {
	ix := postindex.Build([]postindex.Doc{
		doc(1, "a", "b", "c"),
		doc(2, "a", "c"),
		doc(3, "b", "c"),
		doc(4, "a", "b"),
		doc(5, "c"),
	})

	cases := []struct {
		what string
		q    postindex.Query
		want []int
		cn   int
	}{
		{
			"And{a, Or{b,c}}",
			postindex.Query{And: []postindex.Query{
				{Term: "a"},
				{Or: []postindex.Query{{Term: "b"}, {Term: "c"}}},
			}},
			[]int{1, 2, 4}, 10,
		},
		{
			"Or{And{a,b}, c}",
			postindex.Query{Or: []postindex.Query{
				{And: []postindex.Query{{Term: "a"}, {Term: "b"}}},
				{Term: "c"},
			}},
			[]int{1, 2, 3, 4, 5}, 10,
		},
		{
			"Not{Or{a, And{b,c}}}",
			postindex.Query{Not: []postindex.Query{
				{Or: []postindex.Query{
					{Term: "a"},
					{And: []postindex.Query{{Term: "b"}, {Term: "c"}}},
				}},
			}},
			[]int{5}, 0,
		},
		{
			"And{Or{a,b}, Or{b,c}}",
			postindex.Query{And: []postindex.Query{
				{Or: []postindex.Query{{Term: "a"}, {Term: "b"}}},
				{Or: []postindex.Query{{Term: "b"}, {Term: "c"}}},
			}},
			[]int{1, 2, 3, 4}, 13,
		},
	}

	for _, c := range cases {
		got := ix.Query(c.q)
		if err := checkDocs(c.what, got.Docs, c.want...); err != nil {
			return err
		}
		if err := checkCount(c.what, got.Candidates, c.cn); err != nil {
			return err
		}
	}

	// 非法查询：Not 只允许出现在顶层节点上。
	bad := []struct {
		what string
		q    postindex.Query
	}{
		{"And 里嵌了 Not", postindex.Query{And: []postindex.Query{
			{Term: "a"}, {Not: []postindex.Query{{Term: "b"}}},
		}}},
		{"Not 的子树里又出现 Not", postindex.Query{Not: []postindex.Query{
			{Not: []postindex.Query{{Term: "a"}}},
		}}},
		{"一个节点同时设置了两种子句", postindex.Query{Term: "a", Phrase: []string{"a"}}},
		{"一个节点一种子句都没设置", postindex.Query{}},
	}
	for _, c := range bad {
		if err := requireBadQuery(ix, c.what, c.q); err != nil {
			return err
		}
	}
	if postindex.ErrBadQuery == nil || postindex.ErrBadDoc == nil ||
		postindex.ErrBadQuery == postindex.ErrBadDoc {
		return expect("ErrBadQuery 与 ErrBadDoc 是两个不同的非 nil 哨兵错误", "见 errors.go")
	}
	return nil
}

func q2() error {
	ix := postindex.Build([]postindex.Doc{
		doc(1, "x", "y", "x", "y"),
		doc(2, "x", "y", "y"),
		doc(3, "x", "z", "y"),
		doc(4, "y", "x"),
	})

	cases := []struct {
		what string
		ph   []string
		want []int
	}{
		{"Phrase{x,y}", []string{"x", "y"}, []int{1, 2}},
		{"Phrase{x,y,x}", []string{"x", "y", "x"}, []int{1}},
		{"Phrase{y,x}", []string{"y", "x"}, []int{1, 4}},
		{"Phrase{x,z,y}", []string{"x", "z", "y"}, []int{3}},
		{"Phrase{z,y}", []string{"z", "y"}, []int{3}},
		{"Phrase{y,z}", []string{"y", "z"}, nil},
		{"Phrase{nosuch}", []string{"nosuch"}, nil},
		{"Phrase{}（空短语）", []string{}, []int{1, 2, 3, 4}},
	}
	for _, c := range cases {
		got := ix.Query(postindex.Query{Phrase: c.ph})
		if err := checkDocs(c.what, got.Docs, c.want...); err != nil {
			return err
		}
	}
	got := ix.Query(postindex.Query{Phrase: []string{"x", "y"}})
	if err := checkCount("Phrase{x,y}", got.Candidates, 8); err != nil {
		return err
	}
	if got := ix.Query(postindex.Query{Phrase: []string{}}).Candidates; got != 0 {
		return expect("空短语的 Candidates=0（没有词叶子）", strconv.Itoa(got))
	}
	return nil
}

func q3() error {
	ix := postindex.Build([]postindex.Doc{
		doc(1, "p"),
		doc(2, "p", "q"),
		doc(3, "q"),
		doc(5, "r"),
		doc(9, "p", "r"),
	})
	// 全集是 {1,2,3,5,9} —— 故意既不连续也不从 0 开始。
	cases := []struct {
		what string
		q    postindex.Query
		want []int
	}{
		{"Not{p}", postindex.Query{Not: []postindex.Query{{Term: "p"}}}, []int{3, 5}},
		{"Not{Or{p,q}}", postindex.Query{Not: []postindex.Query{
			{Or: []postindex.Query{{Term: "p"}, {Term: "q"}}},
		}}, []int{5}},
		{"Not{And{p,q}}", postindex.Query{Not: []postindex.Query{
			{And: []postindex.Query{{Term: "p"}, {Term: "q"}}},
		}}, []int{1, 3, 5, 9}},
		{"Not{nosuch}", postindex.Query{Not: []postindex.Query{{Term: "nosuch"}}}, []int{1, 2, 3, 5, 9}},
		{"Not{}", postindex.Query{Not: []postindex.Query{}}, []int{1, 2, 3, 5, 9}},
	}
	for _, c := range cases {
		got := ix.Query(c.q)
		if err := checkDocs(c.what, got.Docs, c.want...); err != nil {
			return err
		}
		if got.Candidates != 0 {
			return expect(c.what+" 的 Candidates=0（Not 不计入候选数）", strconv.Itoa(got.Candidates))
		}
	}
	return nil
}

// -------------------------------------------------------------------- order

func o1() error {
	ix := postindex.Build([]postindex.Doc{
		doc(7, "a", "b"),
		doc(2, "a", "c"),
		doc(5, "b", "c"),
		doc(3, "a"),
	})

	cases := []struct {
		what string
		q    postindex.Query
		want []int
	}{
		{"Or{a,b,c}", postindex.Query{Or: []postindex.Query{
			{Term: "a"}, {Term: "b"}, {Term: "c"},
		}}, []int{2, 3, 5, 7}},
		{"Or{a,a}", postindex.Query{Or: []postindex.Query{
			{Term: "a"}, {Term: "a"},
		}}, []int{2, 3, 7}},
		{"And{Or{a,b}, Or{b,c}}", postindex.Query{And: []postindex.Query{
			{Or: []postindex.Query{{Term: "a"}, {Term: "b"}}},
			{Or: []postindex.Query{{Term: "b"}, {Term: "c"}}},
		}}, []int{2, 5, 7}},
		{"Not{Or{a,a}}", postindex.Query{Not: []postindex.Query{
			{Or: []postindex.Query{{Term: "a"}, {Term: "a"}}},
		}}, []int{5}},
	}
	for _, c := range cases {
		got := ix.Query(c.q).Docs
		if err := checkDocs(c.what, got, c.want...); err != nil {
			return err
		}
		if err := checkStrictAsc(c.what, got); err != nil {
			return err
		}
	}
	return nil
}

func o2() error {
	docs := make([]postindex.Doc, 0, 30)
	for i := 1; i <= 30; i++ {
		docs = append(docs, doc(i,
			fmt.Sprintf("t%d", i%7),
			fmt.Sprintf("t%d", (i+3)%11),
			fmt.Sprintf("t%d", (i*2)%13),
		))
	}
	ix := postindex.Build(docs)

	q := postindex.Query{Or: []postindex.Query{
		{Term: "t1"},
		{Term: "t2"},
		{Term: "t3"},
		{And: []postindex.Query{{Term: "t1"}, {Term: "t4"}}},
		{Phrase: []string{"t2", "t5"}},
	}}

	first := ix.Query(q)
	if err := checkStrictAsc("反复查询的结果", first.Docs); err != nil {
		return err
	}
	for i := 0; i < 200; i++ {
		got := ix.Query(q)
		if !sameIDs(got.Docs, first.Docs) {
			return expect(fmt.Sprintf("第 %d 次 Query 与第 1 次逐字段相同（Docs=[%s]）",
				i+2, fmtIDs(first.Docs)), "["+fmtIDs(got.Docs)+"]")
		}
		if got.Candidates != first.Candidates {
			return expect(fmt.Sprintf("第 %d 次 Query 的 Candidates=%d", i+2, first.Candidates),
				strconv.Itoa(got.Candidates))
		}
	}

	firstPlan, err := requirePlan(ix, "反复查询", q)
	if err != nil {
		return err
	}
	for i := 0; i < 50; i++ {
		gotPlan, e := ix.Explain(q)
		if e != nil {
			return expect("重复 Explain 成功", errStr(e))
		}
		if !reflect.DeepEqual(gotPlan, firstPlan) {
			return expect(fmt.Sprintf("第 %d 次 Explain 与第 1 次逐字段相同（%s）", i+2, fmtSteps(firstPlan.Steps)),
				fmtSteps(gotPlan.Steps))
		}
	}
	return nil
}

// --------------------------------------------------------------------- plan

func p1() error {
	ix := postindex.Build([]postindex.Doc{
		doc(1, "a", "b"),
		doc(2, "a"),
		doc(3, "b", "a"),
		doc(4, "c"),
	})

	cases := []struct {
		what string
		q    postindex.Query
		want []int
		cn   int
	}{
		{"Term{a}", postindex.Query{Term: "a"}, []int{1, 2, 3}, 3},
		{"And{a,b}", postindex.Query{And: []postindex.Query{
			{Term: "a"}, {Term: "b"},
		}}, []int{1, 3}, 5},
		{"Or{c, And{a,b}}", postindex.Query{Or: []postindex.Query{
			{Term: "c"},
			{And: []postindex.Query{{Term: "a"}, {Term: "b"}}},
		}}, []int{1, 3, 4}, 6},
		{"Not{a}", postindex.Query{Not: []postindex.Query{{Term: "a"}}}, []int{4}, 0},
		{"Not{Or{a, And{b,c}}}", postindex.Query{Not: []postindex.Query{
			{Or: []postindex.Query{
				{Term: "a"},
				{And: []postindex.Query{{Term: "b"}, {Term: "c"}}},
			}},
		}}, []int{4}, 0},
		{"Phrase{a,b}", postindex.Query{Phrase: []string{"a", "b"}}, []int{1}, 5},
		{"Or{Phrase{b,a}, And{c,a}}", postindex.Query{Or: []postindex.Query{
			{Phrase: []string{"b", "a"}},
			{And: []postindex.Query{{Term: "c"}, {Term: "a"}}},
		}}, []int{3}, 9},
	}

	for _, c := range cases {
		got := ix.Query(c.q)
		if err := checkDocs(c.what, got.Docs, c.want...); err != nil {
			return err
		}
		if err := checkCount(c.what, got.Candidates, c.cn); err != nil {
			return err
		}
		plan, err := requirePlan(ix, c.what, c.q)
		if err != nil {
			return err
		}
		if plan.Steps[0].Candidates != c.cn {
			return expect(fmt.Sprintf("%s 的 Plan 根步 Candidates=%d", c.what, c.cn),
				strconv.Itoa(plan.Steps[0].Candidates))
		}
		if sum := sumLeafCounts(plan.Steps); sum != c.cn {
			return expect(fmt.Sprintf("%s 的 Plan 词叶子 Candidates 之和=%d", c.what, c.cn),
				strconv.Itoa(sum))
		}
	}

	// Not 的整棵子树都不计入候选数，而且整棵树照常出现在 Steps 里。
	exN, err := requirePlan(ix, "Not{Or{a, And{b,c}}}", postindex.Query{Not: []postindex.Query{
		{Or: []postindex.Query{
			{Term: "a"},
			{And: []postindex.Query{{Term: "b"}, {Term: "c"}}},
		}},
	}})
	if err != nil {
		return err
	}
	if exN.Steps[0].Kind != postindex.KindNot || exN.Steps[0].Candidates != 0 {
		return expect("Not 节点是第一步且 Candidates=0",
			fmt.Sprintf("%s(%d)", exN.Steps[0].Kind, exN.Steps[0].Candidates))
	}
	if len(exN.Steps) < 6 {
		return expect("Not 的子树照常展开（至少 6 步）", strconv.Itoa(len(exN.Steps)))
	}
	for _, s := range exN.Steps {
		if s.Candidates != 0 {
			return expect("Not 子树里每一步的 Candidates 都是 0", fmtSteps(exN.Steps))
		}
	}
	return nil
}

func p2() error {
	// 文档 1..7；词表长度：big=6、mid=3、small=1、tiny=2。
	ix := postindex.Build([]postindex.Doc{
		doc(1, "big", "mid", "big"),
		doc(2, "big", "mid"),
		doc(3, "big", "mid"),
		doc(4, "big"),
		doc(5, "big"),
		doc(6, "big", "tiny", "small"),
		doc(7, "tiny"),
	})

	q := postindex.Query{And: []postindex.Query{
		{Term: "big"}, // 子索引 0，候选数 6
		{Or: []postindex.Query{{Term: "mid"}, {Term: "small"}}}, // 子索引 1，候选数 4
		{Phrase: []string{"tiny", "small"}},                     // 子索引 2，候选数 3
	}}

	got := ix.Query(q)
	if err := checkDocs("And{big, Or{mid,small}, Phrase{tiny,small}}", got.Docs, 6); err != nil {
		return err
	}
	if err := checkCount("And{big, Or{mid,small}, Phrase{tiny,small}}", got.Candidates, 13); err != nil {
		return err
	}

	plan, err := requirePlan(ix, "代价重排", q)
	if err != nil {
		return err
	}

	// 期望的求值次序（前序，同一层按候选数升序，并列按书写顺序）：
	//   []       and     （根：候选数 13）
	//   [2]      phrase  （候选数 3）
	//   [2,1]    term    small（候选数 1，短语里第二顺位但更短）
	//   [2,0]    term    tiny（候选数 2）
	//   [1]      or      （候选数 4）
	//   [1,1]    term    small（候选数 1）
	//   [1,0]    term    mid  （候选数 3）
	//   [0]      term    big  （候选数 6）
	type want struct {
		path []int
		kind string
		term string
		cand int
	}
	wants := []want{
		{nil, postindex.KindAnd, "", 13},
		{[]int{2}, postindex.KindPhrase, "", 3},
		{[]int{2, 1}, postindex.KindTerm, "small", 1},
		{[]int{2, 0}, postindex.KindTerm, "tiny", 2},
		{[]int{1}, postindex.KindOr, "", 4},
		{[]int{1, 1}, postindex.KindTerm, "small", 1},
		{[]int{1, 0}, postindex.KindTerm, "mid", 3},
		{[]int{0}, postindex.KindTerm, "big", 6},
	}
	if len(plan.Steps) != len(wants) {
		return expect(fmt.Sprintf("Steps 应有 %d 步（%s）", len(wants), "[]:and [2]:phrase [2,1]:term [2,0]:term [1]:or [1,1]:term [1,0]:term [0]:term"),
			fmt.Sprintf("%d 步：%s", len(plan.Steps), fmtSteps(plan.Steps)))
	}
	for i, w := range wants {
		s := plan.Steps[i]
		if !samePath(s.Path, w.path) || s.Kind != w.kind || s.Candidates != w.cand {
			return expect(fmt.Sprintf("第 %d 步是 %s:%s(%d)", i+1, pathString(w.path), w.kind, w.cand),
				fmt.Sprintf("%s:%s(%d)", pathString(s.Path), s.Kind, s.Candidates))
		}
		if w.term != "" && s.Term != w.term {
			return expect(fmt.Sprintf("第 %d 步的词是 %q", i+1, w.term), strconv.Quote(s.Term))
		}
	}
	return nil
}

// -------------------------------------------------------------------- alloc

// genIndex 造一个确定性的索引：n 篇文档，每篇三个词，词表固定 10 个。
func genIndex(n int) *postindex.Index {
	docs := make([]postindex.Doc, 0, n)
	for i := 1; i <= n; i++ {
		docs = append(docs, doc(i,
			fmt.Sprintf("t%d", i%10),
			fmt.Sprintf("t%d", (i/3)%10),
			fmt.Sprintf("t%d", (i*7)%10),
		))
	}
	return postindex.Build(docs)
}

func allocQuery() postindex.Query {
	return postindex.Query{And: []postindex.Query{
		{Term: "t1"},
		{Or: []postindex.Query{{Term: "t2"}, {Term: "t3"}}},
	}}
}

func a1() error {
	ix := genIndex(2000)
	q := allocQuery()
	got := postindex.Allocs(func() { _ = ix.Query(q) })
	if got > 12 {
		return expect("一次 Query 的堆分配次数 ≤ 12", f2s(got))
	}
	return nil
}

func a2() error {
	small := genIndex(400)
	large := genIndex(40000)
	q := allocQuery()
	as := postindex.Allocs(func() { _ = small.Query(q) })
	al := postindex.Allocs(func() { _ = large.Query(q) })
	if al > 12 {
		return expect("4 万文档下一次 Query 的堆分配次数 ≤ 12", f2s(al))
	}
	if as > 12 {
		return expect("400 文档下一次 Query 的堆分配次数 ≤ 12", f2s(as))
	}
	if al > as {
		return expect(fmt.Sprintf("分配次数不随规模增长（400 文档时 %s）", f2s(as)), f2s(al))
	}
	return nil
}

// -------------------------------------------------------------------- scale

func s1() error {
	const n = 100000
	docs := make([]postindex.Doc, 0, n)
	for i := 1; i <= n; i++ {
		terms := make([]string, 0, 3)
		if i <= 80000 {
			terms = append(terms, "common")
		}
		if i <= 2000 {
			terms = append(terms, "rare")
		}
		terms = append(terms, fmt.Sprintf("f%d", i%48998))
		docs = append(docs, postindex.Doc{ID: i, Terms: terms})
	}
	ix := postindex.Build(docs)

	if got := len(ix.Postings("rare")); got != 2000 {
		return expect("10 万文档下 rare 的倒排长度=2000", strconv.Itoa(got))
	}
	if got := len(ix.Postings("common")); got != 80000 {
		return expect("10 万文档下 common 的倒排长度=80000", strconv.Itoa(got))
	}

	// 书写顺序故意把长子句写在前面；最短倒排长度 = 2000。
	q := postindex.Query{And: []postindex.Query{{Term: "common"}, {Term: "rare"}}}

	postindex.ResetVisits()
	got := ix.Query(q)
	visits := postindex.LeafVisits()

	want := make([]int, 0, 2000)
	for i := 1; i <= 2000; i++ {
		want = append(want, i)
	}
	if !sameIDs(got.Docs, want) {
		return expect("And{common,rare} 命中 1..2000", fmt.Sprintf("%d 篇（前几个 %s）",
			len(got.Docs), fmtIDs(got.Docs[:minInt(6, len(got.Docs))])))
	}
	if visits <= 0 {
		return expect("叶子访问次数 > 0（必须真的求值并计数）", strconv.FormatInt(visits, 10))
	}
	const bound = 3 * 2000
	if visits >= bound {
		return expect(fmt.Sprintf("叶子访问次数 < 3×最短倒排长度 = %d", bound),
			strconv.FormatInt(visits, 10))
	}
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
