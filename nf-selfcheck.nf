struct Minimal {
    a:boolean
    b:int
    c:boolean
    d:string
}

struct Login {
    instance:int
    sessionUuid:string
    id:string
    password:string
    isMTLogin:boolean
    loginTime:string
}

struct Heartbeat {
    ip:string
    sessionUuid:string
    userInstance:int
    timeStamp:string
    pageVisible:boolean
    webStatisticsHeartbeatCount:int
    isFallback:boolean
    tabOwnerId:string
    tabOwnerGeneration:string
}

class Board {
    hits:int
    fails:int
    notes:list<string>
}

method body_of<Value>(value:Value):string {
    return json.from_value(value).to_json()
}

method step_ok(n:int) {
    console.print("step", n, "ok")
}

method pad_of(n:int):string {
    let s:string = number.to_string(n)
    while (length(s) < 3) {
        s = "0" + s
    }
    return s
}

let one:Minimal = Minimal { a: true, b: 7, c: false, d: "tail" }
console.print("step 1 print:", one)
step_ok(1)

let beat:Heartbeat = Heartbeat {
    ip: "127.0.0.1",
    sessionUuid: "probe",
    userInstance: 1,
    timeStamp: "1795000000000",
    pageVisible: true,
    webStatisticsHeartbeatCount: 2,
    isFallback: false,
    tabOwnerId: "owner",
    tabOwnerGeneration: "gen"
}
console.print("step 2 json:", length(json.from_value(beat).to_json()))
step_ok(2)

let login:Login = Login {
    instance: 1,
    sessionUuid: "s",
    id: "002",
    password: "1234",
    isMTLogin: false,
    loginTime: "1794886000"
}
let via_generic:string = body_of(login)
console.print("step 3 generic:", length(via_generic))
step_ok(3)

let copy_beat:Heartbeat = beat
console.print("step 4 copy:", copy_beat.tabOwnerGeneration, copy_beat.webStatisticsHeartbeatCount)
step_ok(4)

let board:Board = new Board(hits: 0, fails: 0, notes: [])
let a:int = 0
while (a < 20) {
    let c:int = 0
    while (c < 20) {
        let e:int = 0
        while (e < 5) {
            if (e == 3) {
                e = e + 1
                continue
            }
            board.hits = board.hits + 1
            if (board.hits % 97 == 0) {
                board.fails = board.fails + 1
            }
            e = e + 1
        }
        c = c + 1
    }
    a = a + 1
}
console.print("step 5 loops:", board.hits, board.fails)
step_ok(5)

let l6:Json = json.empty_object()
let w:int = 0
while (w < 5) {
    l6.set("k" + number.to_string(w), w)
    w = w + 1
}
let l5:Json = json.empty_object()
l5.set("next", l6)
l5.set("v", 5)
let l4:Json = json.empty_object()
l4.set("next", l5)
l4.set("v", 4)
let l3:Json = json.empty_object()
l3.set("next", l4)
l3.set("v", 3)
let l2:Json = json.empty_object()
l2.set("next", l3)
l2.set("v", 2)
let l1:Json = json.empty_object()
l1.set("next", l2)
l1.set("v", 1)
let top:Json = json.empty_object()
top.set("next", l1)
top.set("seq", 9)
let rendered:string = top.to_json()
console.print("step 6 tree:", length(rendered))
step_ok(6)

let back:Json = json.parse(rendered)
let got:int = back["next"]["next"]["next"]["next"]["next"]["v"].to_int(-1)
console.print("step 7 parse:", got)
step_ok(7)

let rows:list<Minimal> = []
let i:int = 0
while (i < 5000) {
    rows.add(Minimal { a: true, b: i, c: false, d: "s" })
    i = i + 1
}
let sum:int = 0
for (row:Minimal in rows) {
    sum = sum + row.b
}
console.print("step 8 list:", length(rows), sum)
step_ok(8)

let table:dict<string, Minimal> = {}
let k:int = 0
while (k < 300) {
    table["k" + pad_of(k)] = Minimal { a: false, b: k, c: true, d: "d" }
    k = k + 1
}
console.print("step 9 dict:", length(table), table["k299"].b)
step_ok(9)

method worker(v:Heartbeat):string {
    return v.tabOwnerId + number.to_string(v.userInstance)
}
let t:Thread<string> = thread.start(worker, beat)
console.print("step 10 thread:", t.wait())
step_ok(10)

method dig(n:int):int {
    if (n <= 0) {
        return 0
    }
    return 1 + dig(n - 1)
}
console.print("step 11 deep:", dig(400))
step_ok(11)

method guard(n:int):boolean {
    let pad2:Minimal = Minimal { a: true, b: n, c: false, d: "v" + number.to_string(n) }
    let xs2:list<int> = [n, n + 1, n + 2]
    let tree2:Json = json.from_value(pad2)
    let shown:string = tree2.to_json()
    let ok:boolean = length(shown) > 10 && length(xs2) == 3 && pad2.b == n && pad2.d == "v" + number.to_string(n)
    return ok
}

method early(target:int):int {
    let pad3:Minimal = Minimal { a: true, b: target, c: false, d: "v" }
    let xs3:list<int> = [1, 2, 3]
    let tree3:Json = json.from_value(pad3)
    let shown3:string = tree3.to_json()
    let ei:int = 0
    while (ei < 40) {
        if (ei == target) {
            return ei + length(xs3) + length(shown3)
        }
        ei = ei + 1
    }
    return -1
}

let good:int = 0
let m:int = 0
while (m < 300) {
    if (guard(m)) {
        good = good + 1
    }
    m = m + 1
}
let scans:int = 0
let q:int = 0
while (q < 300) {
    scans = scans + early(q % 40)
    q = q + 1
}
console.print("step 12 closing brace:", good, scans)
step_ok(12)

console.print("ALL OK")
