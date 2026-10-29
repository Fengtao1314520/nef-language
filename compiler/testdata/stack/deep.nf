// 二十万层的递归：每帧三个槽，一路叫到 n=0 才回头，要的栈远超语言答应的 8 MiB。
// 两条路（`nf 文件.nf` 与产物）都该撞同一句"调用套得太深，栈用完了"，退出码 1。
method walk(n:int, seen:int, depth:int):int {
    if (n == 0) {
        return seen + depth;
    }
    return walk(n - 1, seen + 1, depth + 1);
}
console.print(walk(200000, 0, 0))
