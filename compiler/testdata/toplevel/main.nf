import { count, line } from "./stock.nf"

// stock 的顶层这时已经跑完两轮了：count() 与 line() 都还在读它顶层那一只清单
console.print(count(), line(), count())
