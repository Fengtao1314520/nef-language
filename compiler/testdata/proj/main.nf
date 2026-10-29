import { twice, Pair } from "./util.nf"

let p:Pair = Pair { left: twice(3), right: 8 }
console.print("合计:", p.left + p.right)
