---
layout: home

title: Nef
titleTemplate: false
description: Nef 编译为机器码：无运行时依赖、无垃圾回收；类型与所有权在编译期确定。

hero:
  name: "Nef"
  text: "不用建工程、不用写 main 的静态语言"
  tagline: "字字有类，物各有主。"
  actions:
    - theme: brand
      text: 语法一览
      link: /reference/034_syntax
    - theme: alt
      text: 从第一课开始
      link: /learn/022_start

features:
  - title: "<span class=\"key\">机器码</span>：编译产出独立可执行文件"
    details: 不依赖运行时，也不带垃圾回收器，因此没有回收停顿。
  - title: "<span class=\"key\">类型标注</span>：每个位置都要写"
    details: 变量、参数、返回值与字段一律标注类型。编译器不做类型推断，读任何一行都不必回到上文。
  - title: "<span class=\"key\">交出</span>：赋值与传参之后，原名称失效"
    details: 不设引用计数、不写生命周期标注，双重释放与悬垂引用在编译期排除。
  - title: "<span class=\"key\">内核精简</span>：34 个关键字、23 个运算记号"
    details: 十四组标准库函数。计数循环只有一种写法，条件位置仅接受布尔值。
---

## 程序示例

一个文件就是一个程序。

单个文件从第一行顺序执行到最后一行，没有入口函数。

拆成几个文件时，顶层的执行顺序有约定：被引用的文件先执行，入口文件最后执行。具体规则见[多个文件](/reference/042_modules)。

```nf
let items:list<string> = ["书包", "笔记本", "雨伞"]

method count_chars(words:list<string>):int {
    let total:int = 0
    for (w:string in words) {
        total = total + length(w)
    }
    return total
}

console.print("一共", count_chars(items), "个字")
```

```text
一共 7 个字
```

## 从哪里开始

| 页面 | 内容 |
| --- | --- |
| [入门指南](/learn/021_index) | 十一课循序渐进的教程，每课附可运行的示例 |
| [语法参考](/reference/033_index) | 记号、关键字、类型尺寸与编译期约束的分项说明 |
| [标准库](/stdlib/044_index) | 十四组内建函数的成员表与用法说明 |
| [库](/libs/062_web_test) | 用 Nef 编写的外部库：浏览器驱动层与用例运行层 |
| [工具与调试](/tools/059_cli) | 九个子命令的用法与退出码、编译器报错原文与对应的修正方式、编辑器插件与断点调试 |
| [关于](/about/064_design) | 五个设计决定各自的理由与代价、编译产物与内存占用的实测数字、版本号三位数各自什么时候变，以及代码、文档、名字三档授权的范围 |
