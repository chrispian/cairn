---
name: search-first
description: Check Tesseract before exploring the filesystem or the web. Use before grep, find, rg, a directory walk, reading a docs tree, or a web search — anything answering "is there an X" or "how does X work".
---

Prior sessions already paid for a lot of this. Check before re-deriving it.

```
tesseract_recall
  namespaces = ["user/chrispian/memory", "user/chrispian/knowledge"]
  query      = <the question, phrased for semantic match>
  limit      = 5
```

Sparse or low-confidence results → fall back to filesystem/web as normal. When you do find the answer the expensive way, that's a capture candidate — a future session shouldn't pay twice.

**Scope the call.** An unscoped recall over `user/chrispian/memory` returns ~75K characters and blows the tool-result budget. Namespace + `limit`, always.

## Why it's worth the one call

Cheap, and it compounds. The corpus is only useful if agents actually read it before working — an unread store is just a slower filesystem.
