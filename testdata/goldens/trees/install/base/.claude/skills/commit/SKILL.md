---
name: commit
description: Commit work in a tree another session may be using. Use before any git commit.
---

**Invoker precedence.** If Chrispian's instruction contradicts a default here, they win.

## The form

```
git commit -m "<message>" -- <path> [<path>...]
```

Name the paths you mean. This builds the tree from `HEAD` plus exactly what you
name and leaves the index untouched — which is what makes it safe when another
session shares the working tree.

**Flags go before the `--`.** Everything after it is read as a pathspec.

## Before

- `git status --short` — know what is yours and what arrived from elsewhere.
- `git diff --cached` if the index matters to you.

## The message

Say what changed and why it changed. The subject is the claim; the body is the
evidence. If the commit carries a measurement, carry the command that produced
it beside the number.
