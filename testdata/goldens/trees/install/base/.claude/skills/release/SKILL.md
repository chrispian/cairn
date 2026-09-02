---
name: release
description: Cut a release. Use before git tag or gh release create.
---

**Invoker precedence.** If Chrispian's instruction contradicts a default here, they win.

> First pass, assembled from what the repos already do. Correct it as the real
> shape settles — this file is the record of the process, so it is the thing to
> edit rather than work around.

## Steps

**1. `make check` on a clean tree**, from the commit being tagged. Not from a
tree with local changes, and not from memory of an earlier run.

**2. `govulncheck ./...`** where the repo is Go. A release is the moment this
matters most.

**3. The changelog reflects the diff.** `git log --oneline <last-tag>..HEAD` and
reconcile — an entry per user-visible change, and nothing claimed that is not in
the range.

**4. Tag and push the tag.**

**5. Torque.** Transition what this release closes, and comment the tag on each.
