# Contributing to Nef

Thanks for your interest in Nef. Please read this before your first pull request.

## 1. Licensing of your contribution

By opening a pull request against this repository you agree to the
**[Nef Contributor License Agreement v1.0](CONTRIBUTOR_LICENSE_AGREEMENT.md)**.
Signing happens once per person, not per PR. The current convention is:

> ⚠️ A licence is not a licence if nobody can read it. Every sentence below is
> short on purpose.

- Your code stays open source under **Apache-2.0 + Nef Output Exception**, for
  every version already released.
- You grant the maintainer a license to your contribution, **not** an assignment:
  you keep your copyright, and you can keep using your own code for anything.
- The reason the agreement exists is that it lets the project (a) enforce the
  patent clause without chasing every past contributor, and (b) relicense
  *future* versions if the project ever needs to. Without it, "we may relicense"
  is empty, because copyright stays scattered across hundreds of people.
- The agreement explicitly promises released versions are never pulled back.
  Relicensing is forward-looking only.

The bot asks first-time contributors to add a comment:

```
I have read the Nef Contributor License Agreement v1.0 and I agree to it.
Signed: <your name>
```

### Before you contribute, make sure:

- **Your employer does not own it.** If you wrote the code at work, or under a
  contract, you need written permission from that employer, or an authorized
  representative must sign the Corporate CLA (Appendix B of the agreement). This
  is the single most common legal problem in open source, and it is also how a
  large company later claims rights over the language. When in doubt, ask.
- **Third-party code is disclosed.** Any snippet, library, or file you did not
  write must be named in the PR description, with its license, and that license
  must be compatible with Apache-2.0. **Do not bring in GPL or AGPL code** — it
  would drag the entire toolchain into copyleft and make the Output Exception
  unusable.
- **AI-assisted work is disclosed.** If a substantial portion of a change came
  from a model, say so in the PR. We care because the copyright and license
  status of that output is unsettled, and the project cannot accept a grant from
  someone who may not hold the rights.
- **You have the rights you claim.** Original work, no patent or trade-secret
  obligations attached to it.

## 2. What counts as a contribution

Compiler and toolchain code, the runtime and standard library, tests and
benchmarks, the specification and grammar, documentation, website code, artwork,
and names. All of it falls under the CLA. Issue comments and chat messages are
not contributions unless you attach a file to a PR.

## 3. File headers

Every new source file carries the Apache-2.0 header; files in the runtime and
standard library additionally carry the Output Exception line. See
[templates/FILE_HEADER.txt](templates/FILE_HEADER.txt) for copy-paste blocks.
As of 2026-10-28 every `.go` file under `compiler/` already carries one of
those blocks, so a new file matches its neighbours. The License text is
`LICENSE` and the exception is `LICENSE-EXCEPTION`, both at the repository
root.

## 4. Development flow

1. Open an issue first for anything that changes observable behaviour: syntax,
   semantics, the standard library surface, or the ABI. Language changes are
   cheap to add and expensive to remove.
2. Branch from `main`, keep PRs small, and make sure tests pass locally.
3. Write a CHANGELOG-style sentence in the PR description — "what a Nef user
   would notice".
4. Do not rename, reformat or restructure files you are not otherwise touching;
   it destroys `git blame` for copyright history, which is the evidence trail
   the CLA depends on.
5. A maintainer review is required before merge. Silence is not approval.

## 5. The name and the logo

"Nef" and its logo are **not** licensed under Apache-2.0 or CC BY 4.0. They are
governed by [TRADEMARK_POLICY.md](TRADEMARK_POLICY.md). You may describe your
work as "written in Nef" or "supports Nef". You may not call a product, service,
domain or hosted platform "Nef", and you may not use the logo as your own
identity. This is the part of the project that survives a fork, so it stays with
the author.

## 6. Code of conduct

Be decent. Assume good faith, criticise code not people, and remember that most
contributions here are unpaid. Harassment, hate speech, or deliberate bad-faith
behaviour ends participation in the project.

## 7. Governance, and what "relicensing" would mean in practice

Today Nef is a single-author project, which means the maintainer decides. That
is fast now and will not scale. The commitments in writing, so nobody has to
trust goodwill later:

- **Released versions keep the license they were released under.** No retroactive
  change, ever.
- **Any future relicense of the toolchain requires** a public proposal period of
  at least 30 days in this repository, with the reasons and the exact text
  diff, and a summary of objections.
- **The project will not adopt a license that forbids** ordinary use of the
  language, reading the source, self-hosting the compiler, or keeping programs
  written in Nef closed. Those four are the reason the Output Exception exists
  and are not on the table.
- If Nef is ever donated to a foundation, transferred, or taken over by a
  company, the CLA and this section transfer with it, and the transfer is
  announced publicly before it happens.

If you disagree with any of the above, say so now — it is far easier to change
these words than to undo a contribution you later regret licensing.
