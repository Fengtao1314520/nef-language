# Nef Trademark Policy

Version 1.0 — 2026-10-06
Owner of the marks described below: **Feng Tao (Nate)**, GitHub: [`Fengtao1314520`](https://github.com/Fengtao1314520)

## Why this document exists

Nef's source code is licensed under Apache-2.0 (`LICENSE`) together with the Nef
Output Exception (`LICENSE-EXCEPTION`). That license is
deliberately generous: anyone may read it, modify it, ship it, sell tooling around it, and compile
programs with it, without owing anything to the author.

A copyright license cannot, however, control what people **call** things. The name and the logo are
separate rights, and they are the only part of a programming language that a large company cannot
simply take and rebuild. This policy says what you may do with them and what you may not.

In short: **use Nef freely, build a business on Nef, sell Nef services — but do not sell *as* Nef.**

## 1. What the marks are

| Mark | Description | Examples of protected use |
|---|---|---|
| "Nef" wordmark | The name of the programming language, in any script, size or stylisation, including obvious variants and abbreviations ("Nef lang", "NefLang", "NƐF") | product names, domains, app names, service names |
| Nef logo | The official logo artwork published in this repository: a geometric letter `N`, taken from the three letters of the name, set in a rounded square with a short bar beneath it. The source is `site/public/favicon.svg`; `logo.svg`, `logo-dark.svg` and the VS Code extension icon are rendered or recoloured from that one file | icons, splash screens, marketing imagery, merch |
| "Nef" + specifier combinations | Names implying official status: "Official Nef", "Nef Certified", "Nef Cloud", "Nef Enterprise", "Nef Academy" | any commercial offering |

The marks include the word "Nef" whenever it is used to identify software, tooling, hosting,
training, certification or merchandise, insofar as the author holds rights in them.

## 2. What you may do without asking

- **Name-free description.** Say that your product, library, editor plugin, book, course or blog
  post "supports", "targets", "is written in", "is compatible with" or "compiles to" Nef.
- **Refer to the language itself.** Use "Nef" as the name of the language in documentation,
  tutorials, Stack Overflow answers, job postings, package manifests, language IDs, file extensions
  and grammar definitions.
- **Distribute unmodified builds.** Ship the official compiler, toolchain, runtime or spec unchanged,
  including the logo, and say so.
- **Use the logo as a link to the official project**, at a size no more prominent than your own
  branding, and never as your own primary mark.
- **Community events and content.** Meetups, user groups, podcasts, videos and fan merchandise that
  are clearly unofficial, non-endorsing, and do not use the logo as their main identity — please tell
  the maintainer so the event can be listed.
- **Packages on public registries.** Library names such as `nef-json` or `nef-lsp` are fine where the
  registry allows them, provided they do not imply the package is from the language author.
- **Forks.** A fork of the compiler may be distributed, modified and even sold. It must not be called
  "Nef", and it must not use the logo. Say "a fork based on Nef" or pick your own name. (This is the
  OpenJDK/Temurin, MySQL/MariaDB, Redis/Valkey pattern: the code is yours to take, the name is not.)

## 3. What you may not do

The following uses require written permission:

a. Using "Nef" or the logo as, or as the dominant part of, the name of a **product, company, service,
   domain, app, SDK or hosted platform** — for example `nef-lang.com`, `Nef Cloud`, `NefCI`,
   `Managed Nef on <your cloud>`, `Nef IDE Pro`.
b. Using the marks in a way that suggests **affiliation, sponsorship or endorsement** by the author
   of Nef, including "official", "certified", "approved", "endorsed" or "partnered with Nef".
c. Modifying, recolouring, adding to, or otherwise restyling the logo, or using it as your own mark.
d. Offering **training, certification or exams** under the Nef name.
e. Registering, or applying to register, any trademark, domain, social handle or app-store name that
   contains the marks, in any jurisdiction.
f. Using the marks in advertising material, merchandise for sale, merchandise bundled with paid
   products, or crowdfunding campaign pages.
g. Using the marks in a way that disparages the language or distributes low-quality or malicious
   software under a name resembling them.

## 4. Correct usage conventions

- Write the name as **"Nef"**, capitalised, as a proper adjective followed by a generic noun:
  "the Nef programming language", "a Nef compiler", "Nef source code". Do not use it as a verb,
  plural or possessive ("nef it", "the Nefs", "Nef's compiler" for the author's own).
- Do not abbreviate to a confusing form in product names.
- On first prominent use in documentation, add:
  `Nef™ is a trademark of Feng Tao.` (until registration is confirmed; see Section 7).
- Always link from the name to the official project when the context is a competing or commercial
  offering.

## 5. Enforcement approach

The author is an individual, not a company with a legal department, and the goal is clarity rather
than litigation. If a use falls outside this policy, the first step will normally be a friendly
message asking for a rename or a disclaimer, with a reasonable transition period. Enforcement
escalates only when the use is deliberately confusing, commercial in the categories above, or
unresponsive.

Uses this policy does **not** target: internal tools that are never distributed or advertised; the
name of a company that merely uses Nef; academic work; honest comparative statements; and packages
already published in good faith.

## 6. Requesting permission

Open an issue, or contact the maintainer through the GitHub profile above. State: the exact name or
artwork you want to use, what the product is, whether it is free or paid, and your territory.
Permission may be granted in writing; it is revocable if misused, and never transfers ownership.
Permission is granted case by case; the author is free to say yes to a small project and no to a
large one, and to charge for commercial endorsement deals if the project grows.

## 7. Status of the marks, and a note on registration

This policy asserts rights by **prior use and publication**, which is the only thing an unincorporated
individual can rely on today. It is not the same as a registered trademark, and registration is what
makes enforcement cheap rather than expensive.

If Nef gets any traction, register before someone else does — this is the single highest-value
defensive action for a solo language author:

- **United States (USPTO)** — a natural person can file directly; intent-to-use applications are
  cheap relative to the exposure. Class 9 (downloadable software) and Class 42 (development tools,
  hosted services).
- **European Union (EUIPO)** — one filing covers the EU; individuals may file in some member states.
- **Mainland China (CNIPA)** — a mainland natural person generally cannot file a plain trademark
  application without business-operator qualification such as an 个体工商户 establishment; the usual
  route is to file through a company or a licensed agency. If you have no entity yet, start with the
  US/EU filings and use this policy plus dated public commits as evidence of prior use in China.
- **Madrid Protocol** — once you hold one national registration, you can extend it internationally.

⚠️ This section is general information, not legal advice. Trademark outcomes depend on jurisdiction,
class selection and prior marks already on file. If Nef ever takes funding or is contributed to by a
company, get this policy and the CLA reviewed by a lawyer.

## 8. Relationship to the licenses

- `LICENSE` (Apache-2.0) and `LICENSE-EXCEPTION` (Nef Output Exception) govern **code**.
- The specification and the documentation are under **CC-BY-4.0** and govern **text**.
- This policy governs **names and artwork**, and overrides any implied right to use them.
- Nothing in this policy reduces the rights granted by the Apache-2.0 license. You do not need
  trademark permission to compile, modify, distribute or sell software built from Nef source.
