// 示例程序三：形状与周长
// 用到：new 带初值、装箱与拆盒、从容器取 object 分两步、`is` 只做类型判断不取值、接口名不能当类型用
// en: Shapes and perimeters. Covers new with initial values, boxing and unboxing, taking an object out of a container in two steps, `is` as a type check that takes nothing, and that an interface name is not a type.

interface Shape {
    perim():float
}

class Circle base Shape {
    r:float
    method perim():float { return 2.0 * 3.14159 * r }
}

class Square base Shape {
    side:float
    method perim():float { return side * 4.0 }
}

let c:Circle = new Circle(r: 2.0)         // 字段必须给全，顺序可以打乱
let s:Square = new Square(side: 3.0)
console.print("圆:", c.perim(), "方:", s.perim())

let box:list<object> = []
box.add(c as object)                       // 装箱必须明写；c 的值交出后，c 不再是持有者
box.add(s as object)

let own:object = box[0]                    // 第一步：那个位置过户给 own，此后 box[0] 的值为 null
let back:Circle = own as Circle            // 第二步：拆盒
console.print("拆回来:", back.perim())

// 接口名不能当类型用，混装时只能逐个判断盒子里的值
for (i:int in 0 until length(box)) {
    let one:object = box[i]
    if (one is Circle) {
        let round:Circle = one as Circle
        console.print("是个圆，周长:", round.perim())
    } else if (one is Square) {
        let boxy:Square = one as Square
        console.print("是个方，周长:", boxy.perim())
    } else {
        // 第 0 个位置已经过户给 own：现在它是空的，`is` 一律返回否
        console.print("第", i, "格是空的")
    }
}
