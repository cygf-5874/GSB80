# postindex

倒排索引小库（Go 1.24，仅标准库，`go.mod` 无 `require`）。

一篇文档被切成若干词以后，`postindex` 把「词 → 哪些文档、在这些文档的哪个位置」存下来，
再支持按表达式查询：单词、交集、并集、全集差集、位置连续的短语，五种可以任意嵌套。

查询**不是**照着写下来的顺序求值：`Explain` 要给出**实际求值次序**，而这个次序本身也是契约
的一部分（见下面第 7、8 条）。

```go
ix := postindex.Build([]postindex.Doc{
	{ID: 1, Terms: []string{"go", "rust", "go"}},
	{ID: 2, Terms: []string{"rust"}},
})

ix.Postings("go")   // [{Doc:1 Positions:[0 2]}]
ix.Query(postindex.Query{And: []postindex.Query{
	{Term: "go"},
	{Term: "rust"},
}})                 // Result{Docs: [1], Candidates: 4}
ix.Explain(postindex.Query{Term: "go"})
```

## 怎么跑

```bash
go build ./...             # 编译
go run ./check             # 验收场景（固定件）
go run ./check -list       # 列出全部场景
go run ./check --only plan
bash scripts/check.sh      # 同上，薄封装
```

前提：Go 1.24。不引入任何第三方依赖。

## 对外契约

`Doc` / `Posting` / `Query` / `Result` / `PlanStep` / `Plan` 的字段名与类型已经定死
（见 `postindex.go`），下面 12 条是语义。**它们是契约，不是「当前行为」的转述。**

1. **构建**：`Build(docs []Doc) *Index`。每条文档有 `ID int` 与 `Terms []string`
   （**已切好词**，按出现顺序）；位置 = 该词在 `Terms` 里的下标。同一文档里同一个词重复出现时，
   它的**位置列表**要按升序去重。`docs` 里同一个 `ID` 出现多次时以**最后**一条为准。
   `nil` 文档切片 = 空索引。重复调用 `Build` 必须每次从头重建（不得残留上一次的状态）。
   可观测口径：`Postings(term string) []Posting` 返回该词的倒排 —— 按 `Doc` 升序，
   每条的 `Positions` 升序去重；索引里没有这个词（含空词）时返回空切片。`Build` 不做文档校验。
2. **查询表达式**：`Query(q Query) Result`。每个节点用五种子句中的一种，可以任意深浅递归嵌套：
   `Term`（`Term != ""` 表示设置了）、`And` / `Or` / `Not`（`[]Query`，**非 `nil`** 表示设置了）、
   `Phrase`（`[]string`，**非 `nil`** 表示设置了）。`Term` = 含该词的文档集合；
   `And` = 各子句的交集；`Or` = 各子句的并集；`Phrase` = 命中该短语的文档集合（第 4 条）；
   `Not` 见第 3 条。空但非 `nil` 的 `And` = 全集（零个约束）；空但非 `nil` 的 `Or` = 空集；
   空但非 `nil` 的 `Phrase` = 全集。
3. **`Not` 是全集差集**：全集 = 本次 `Build` 里的所有文档 `ID`；`Not: [c1, c2, ...]` 的结果 =
   全集 \ (c1 ∪ c2 ∪ ...)。`Not` **只允许出现在顶层节点**：任何子句里（包括 `Not` 自己的子树里）
   再出现 `Not` 都是非法查询。一个节点必须**恰好**设置五种子句中的一种 —— 一种都没设置、
   或同时设置两种以上，也是非法查询。两种非法查询都返回 `ErrBadQuery`：`Explain` 把它作为错误
   返回（同时给出零值 `Plan`），`Query` 返回零值 `Result`，无论哪种都**不得 panic**。
4. **`Phrase` 要求位置连续递增**：`Phrase: [t1, t2, ..., tk]` 命中文档 d，当且仅当存在位置 p，
   使得 d 里 `t1` 在 p、`t2` 在 p+1、……、`tk` 在 p+k-1（位置都按第 1 条去重升序后的列表）。
5. **`Result.Docs` 是命中文档 `ID` 的升序去重列表**。
6. **`Result.Candidates` 与 `Plan` 必须来自同一棵求值树**。词叶子（`Term`，以及 `Phrase` 里的
   每个词）的候选数 = `len(Postings(该词))`；`And` / `Or` / `Phrase` 节点 = 其子句候选数之和；
   `Not` 节点及其**整棵子树**一律记 0。于是 `Result.Candidates` = 根节点的候选数 =
   `Plan` 里所有**词叶子**那几步的候选数之和（内部节点的候选数本身就是子句之和，
   所以只对词叶子求和，别把内部节点也加一遍）。
7. **`Explain(q Query) (Plan, error)` 给出实际求值计划**。`Plan.Steps` 里的每一步对应查询里的
   一个子句（含内部节点），按**实际求值次序**排列：父节点先于它的子节点（前序），
   同一层的子节点按第 8 条的次序排。每一步带：
   `Path` —— 从根到该子句的**子索引路径**（编号按**查询的书写顺序**，根节点是空路径；
   重排只改 Steps 里的先后，不改 `Path` 的编号）；`Kind` —— 五种子句之一；
   `Term` —— `Kind == KindTerm` 时该叶子对应的词；`Candidates` —— 该子句的候选数（第 6 条口径）。
8. **代价重排**：`And` / `Or` / `Phrase` 的子句必须按**首次求值时的候选数升序**依次求值；
   `Plan.Steps` 里同一层子句的先后必须与「候选数升序」一致 —— 候选数并列时按
   **子句在原查询里的出现顺序**（确定性 tie-break）。`Not` 自身的子句列表不重排（保持书写顺序）。
9. **分配预算**：一次 `Query` 调用的堆分配次数必须 **≤ 12**（用 `alloc.go` 的 `Allocs` 度量），
   且**不得**随文档数或词表大小增长。
10. **规模轴**：10 万文档、5 万词表下，一次 `And` 查询的**叶子访问次数**必须
    `< 3 ×（最短倒排长度）`。叶子访问次数用 `visit.go` 计数，口径写在 `visit.go` 里：
    实现必须在求值时调用 `CountVisit` 汇报（取倒排条目 +1、做候选命中判定 +1），
    固定件在场景前调 `ResetVisits`、之后读 `LeafVisits`。这个上界逼着实现
    **先按代价重排、再按候选逐条判定**，而不是按书写顺序整表展开。
11. 边界：空索引、空文档（`Terms` 为空）、不存在的词、空 `Phrase`、`nil` 文档切片 = 空索引；
    重复 `Build` 每次从头重建。任何一种都不得 panic。
12. **结果不依赖 `map` 迭代顺序**：同一输入任意多次 `Query` 必须逐字段相同（`Docs` 逐元素相等、
    `Candidates` 相等），任意多次 `Explain` 必须逐字段相同（含 `Plan.Steps` 的次序与 `Path`）；
    `Docs` 与 `Postings` 的顺序都是升序 —— 任何 `map` 遍历都不许直接决定输出顺序。

> 已导出的类型、字段名与哨兵错误（`ErrBadQuery`、`ErrBadDoc`，见 `errors.go`）是契约的一部分：
> 不得改名、不得改类型、不得改含义。`Index` 的内部表示完全自由，可以新增类型、字段与方法。
> `alloc.go` 的 `Allocs` 与 `visit.go` 的 `ResetVisits` / `LeafVisits` / `CountVisit`
> 是固定件的度量入口，签名不要改。

## 验收

`check/` 是固定验收程序，**不要修改**。场景分六组，共 12 个：

| 组 | 场景 | 覆盖 |
| --- | --- | --- |
| `build` | `B1_positions_dedup_sorted` | 契约 1、11：位置升序去重、重复 ID、空文档、不存在的词 |
| `build` | `B2_rebuild_no_residue` | 契约 1、11：重复 `Build` 从头重建、`nil` 切片 = 空索引 |
| `query` | `Q1_nested_and_or_not` | 契约 2、3、5：多层嵌套、非法查询 → `ErrBadQuery` |
| `query` | `Q2_phrase_contiguous` | 契约 4、11：位置连续递增、空短语 |
| `query` | `Q3_not_universe_diff` | 契约 3、5：全集差集、全集是文档 `ID` 集合 |
| `order` | `O1_docs_sorted_unique` | 契约 5、12：`Docs` 升序去重 |
| `order` | `O2_repeated_calls_identical` | 契约 12：同输入反复查询逐字段相同 |
| `plan` | `P1_candidates_leaf_sum` | 契约 6、7：`Candidates` 与 `Plan` 词叶子计数一致 |
| `plan` | `P2_steps_cost_order` | 契约 7、8：Steps 的先后与候选数升序一致（含 tie-break） |
| `alloc` | `A1_query_alloc_budget` | 契约 9：一次 `Query` 的堆分配 ≤ 12 |
| `alloc` | `A2_alloc_independent_of_size` | 契约 9：分配次数不随规模增长 |
| `scale` | `S1_leaf_visits_bounded` | 契约 10：10 万文档下叶子访问次数上界 |

`--only build|query|order|plan|alloc|scale` 可以只跑一组。12 个场景都在同一个进程里跑，
每个场景的 panic 会被单独捕获并记成一条 `FAIL`，**一个场景挂掉不会遮蔽其余场景**。
判据全部是硬编码的期望值：不依赖墙钟、机器速度、随机源或 `map` 迭代顺序。

## 目录

```
.
├── .gitattributes
├── .gitignore
├── go.mod               module postindex
├── errors.go            ErrBadQuery / ErrBadDoc
├── postindex.go         类型与签名（方法体是 panic，待实现）
├── alloc.go             固定入口：Allocs（分配计数）
├── visit.go             固定入口：ResetVisits / LeafVisits / CountVisit（叶子访问计数）
├── PROMPT.md            本题的 User Prompt（逐字）
├── check/main.go        固定验收程序（勿改）
├── scripts/check.sh     薄封装：go run ./check "$@"
└── README.md
```

## User Prompt

```
把查询看成代数式，先定求值顺序，再定计数口径。postindex 是 Go 1.24 的
倒排索引库，只用标准库（无第三方依赖），构建走 go 工具链，
自检走 `scripts/check.sh`（`check/` 是固定验收程序，别改）。

`postindex.go` 现在只有类型和签名，方法体全是 panic；`errors.go` 里两个错误值已经给全；
`alloc.go` 与 `visit.go` 是固定件要用的两个度量入口，别改它们的签名。

任务：按 README「对外契约」的 12 条把索引实现出来，让固定件全过。

验收：
- go build ./... 退出码 0；
- go run ./check 退出码 0，12 个场景全过（build 2 + query 3 + order 2 + plan 2 + alloc 2 + scale 1）。

约束：
1. 不改 `check/`、不改 `errors.go`、不改 `alloc.go` 与 `visit.go` 的签名；可以新增文件与类型。
2. 不引入第三方依赖，`go.mod` 里不许出现 `require`。
3. `Result` 与 `Plan` 的字段名与类型已定死，不要改。
4. 结果必须与 `map` 迭代顺序无关。
```
