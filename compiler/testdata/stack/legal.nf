// 同一支函数只走 5 千层：那道检查必须不碰它。把栈底算错一格的症状正是"普通递归也报栈用完"。
method walk(n:int, seen:int, depth:int):int {
    if (n == 0) {
        return seen + depth;
    }
    return walk(n - 1, seen + 1, depth + 1);
}
console.print(walk(5000, 0, 0))
