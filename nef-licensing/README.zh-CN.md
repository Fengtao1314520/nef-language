# Nef 开源许可方案说明

适用对象：Feng Tao（Nate），GitHub 账号 `Fengtao1314520`
语言名：**Nef**
生成日期：2026-10-06

这套文件的目标是把采用的门槛降到最低，同时保留只有作者能够主张的那部分权利：代码贡献权（CLA）、品牌（商标），以及以后更换许可证的可能性。

---

## 一、文件清单与放置位置

```
NF_Language/                        ← 根
├── LICENSE                         ← Apache-2.0 全文（2026-10-28 起不含例外条款）
├── LICENSE-EXCEPTION               ← Nef Output Exception（Apache-2.0 第 4 条下的额外授权）
├── NOTICE                          ← 第三方归属声明（同日到位）
└── nef-licensing/                  ← 其余文本留这一格
    ├── LICENSE-DOCS.md             ← 规范/文档用 CC BY 4.0
    ├── CONTRIBUTOR_LICENSE_AGREEMENT.md  ← CLA（v1.0）
    ├── TRADEMARK_POLICY.md         ← 商标政策
    ├── CONTRIBUTING.md             ← 贡献流程，引用上面几份
    ├── APACHE-2.0.txt  _header.txt  _exception.txt
    └── templates/FILE_HEADER.txt   ← 源码版权头模板
```

`LICENSE` 与 `NOTICE` 必须是仓库根目录下的这两个准确文件名（大写、无扩展名），合规扫描工具（REUSE、ORT、FOSSA、ScanCode）默认只识别这两个文件名。

---

## 二、每份文件在解决什么

| 文件 | 解决的问题 | 没有它会怎样 |
|---|---|---|
| `LICENSE`（Apache-2.0） | 宽松授权 + **专利授权** + 专利报复条款 | 改用 MIT 时贡献者的专利不会自动授予，留下隐患 |
| **Nef Output Exception** | 明确“用 Nef 编译的程序归你，不受本许可证约束” | **最关键的一条**。大厂法务一旦怀疑 copyleft 传染用户代码，就会直接禁用整个语言 |
| `NOTICE` | Apache-2.0 第 4(d) 条的硬性要求 | 下游分发不符合要求，第三方依赖的归属也会混乱 |
| `LICENSE-DOCS.md` | 规范/文档单独使用 CC BY 4.0 | 文档套用代码的许可证，会让人看不清义务的边界 |
| `CONTRIBUTOR_LICENSE_AGREEMENT.md` | **版权集中**：授权你转授、改许可证、单独维权 | 日后想更换协议，需要逐个请几百个贡献者签字，实际上等于无法更换 |
| `TRADEMARK_POLICY.md` | 保住“Nef”这个名字和 logo | Redis 更换协议后几天内就被 fork 成 Valkey；代码本身限制不了继续使用，名称可以 |
| `CONTRIBUTING.md` | 让贡献者知道规则，形成可举证的同意链 | 贡献者不了解 CLA，签署的效力会减弱 |

---

## 三、关于 Output Exception 的两个设计决定

**1. 它是“额外授予”，不是“限制”。**
措辞上写成 Apache-2.0 第 4 条下的 additional grant（与 LLVM Exception、Swift Runtime Exception 是同一种写法），因此即使有人主张例外条款无效，其下的 Apache-2.0 本体仍然成立，不会出现整体失效。

**2. `SPDX-License-Identifier: Apache-2.0 WITH Nef-Output-Exception`**
`Nef-Output-Exception` 是本项目自定义的标识，不在 SPDX 的例外列表里，扫描工具报 unknown 属于正常情况。模板文件里已经提示要把它写进 README，以免贡献者来“修正”它。

例外条款的边界（`LICENSE` 里的 Limits 段）有意排除以下三种规避限制的用法：
- 修改编译器本身 → 仍然受完整的 Apache-2.0 约束
- 换一个名字把 Nef 编译器当作自己的语言实现来出售 → 仍然受 Apache-2.0 约束，而且不得使用商标
- 声称“官方认证/由 Nef 作者支持” → 不允许

---

## 四、需要你确认或补充的（4 项）

1. **年份**：目前全部写作 `Copyright 2026 Feng Tao (Nate)`。如果仓库第一次公开提交的实际年份与此不同，就改成实际年份，并保持整个仓库一致。
2. **署名形式**：三处都用了“Feng Tao (Nate)”加 GitHub 账号。法律上有效的是可以识别的真实姓名（Feng Tao），Nate 是昵称。建议保留现在这种“真名在前”的写法，不要只留 Nate。
3. **`NOTICE` 目前只是模板**：里面放了示例条目和填写说明，要在引入第一个第三方组件时把它填写完整。**GPL/AGPL 依赖一律不能引入**，否则会把整套工具链带入 copyleft。
4. **商标注册**：`TRADEMARK_POLICY.md` 第 7 节说明了一个现实约束：中国大陆的自然人通常不能直接申请普通商标，需要个体工商户等经营主体资格，或者通过公司、代理机构申请。所以低成本的做法是先在美国（USPTO，个人可以直接申请）和欧盟（EUIPO）注册，在中国大陆则用“公开提交时间戳加本政策”作为在先使用的证据。**这是本节里唯一需要本人判断的合规事项，建议有进展之后再找律师确认。**

---

## 五、实施顺序

1. 把 `templates/FILE_HEADER.txt` 的版权头批量加入现有源码；如果已有 GPL 代码混在其中，先清理。
2. 把 `CONTRIBUTING.md` 链接进 README，并配置第一位 contributor 的签署流程（CLA Assistant / EasyCLA，或者先用 README 里提到的 PR 评论签署方式）。
3. 在 README 里用一段话讲清楚“你的代码不用开源”。这句话对语言采用率的提升，比任何许可证条款都大。
4. 语言名和 logo 要尽早公开使用并留下时间戳（第一版发布、官网、README），为在先使用建立证据。

---

## 六、坦率的局限

- **挡不住 Fork**。任何人都可以把 Nef 改名之后重新发布，Apache-2.0 明确允许这种做法。唯一的防线是商标，加上你作为原作者的社区权威。
- **语言设计本身不受版权保护**。别人照着语法自己写一套编译器，许可证对此毫无办法（Go 之于 Rust、Swift 之于 Kotlin 都属于这类竞争）。
- **单人项目的维权成本是现实问题**。CLA 给你的是“资格”，不是“能力”。真要主张权利仍然需要时间与费用，这也是这套方案把重心放在商标和生态上、而不是靠许可证条款形成威慑的原因。
- 这些文件是可直接使用的工程起点，**不构成法律意见**。如果 Nef 将来接受公司贡献、融资或者成立主体，CLA 与商标政策应当请律师复核；届时也建议把“改许可证需 30 天公开提案”的治理承诺一并保留，因为它是贡献者愿意签署 CLA 的前提。
