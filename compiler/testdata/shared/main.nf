import { bag, size } from "./registry.nf"

bag.add("书包")
bag.add("雨伞")
for (thing:string in bag) {
    console.print("带了:", thing)
}
console.print("一共", size(), "件")
