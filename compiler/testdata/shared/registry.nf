// 跨文件共用一份可变的清单（§11.6）
export shared bag:list<string> = []

export method size():int {
    return length(bag)
}
