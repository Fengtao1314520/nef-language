import { step, put, count } from "./dep.nf"

// 函数体里用引来的 const：这一条以前过不去，登记的名字晚了一步才摆进作用域
method doubled():int {
    return step * 2
}

put(step)
put(doubled())
console.print(doubled(), count())
