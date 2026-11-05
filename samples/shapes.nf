// 示例程序三：形状与周长
// 用到：new 带初值、装箱与拆箱、从容器取 object 分两步、`is` 只做类型判断不取值、接口名不能当类型用
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

let one_box:list<object> = [new Circle(r: 2.0) as object]
let own:object = one_box.remove_at(0)      // 第一步：把那个位置从容器里摘出来，此后 one_box[0] 是空句柄
let back:Circle = own as Circle            // 第二步：拆箱
console.print("拆回来:", back.perim())

// 接口名不能当类型用，混装时只能逐个判断盒子里的值
for (i:int in 0 until length(box)) {
    if (box[i] is Circle) {
        console.print("是个圆，周长:", (box[i] as Circle).perim())
    } else if (box[i] is Square) {
        console.print("是个方，周长:", (box[i] as Square).perim())
    } else {
        // 盒子里那个位置是空句柄时，`is` 两种都返回否，走到这一支
        console.print("第", i, "格是空的")
    }
}
