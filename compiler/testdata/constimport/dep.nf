// 引来的名字要在**函数体里**也认得：`export const` 与 `export shared` 各一份。
export const step:int = 7

export shared bag:list<int> = []

export method put(n:int) {
    bag.add(n)
}

export method count():int {
    return length(bag)
}
