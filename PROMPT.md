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
