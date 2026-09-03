---
name: relay
description: Address, dispatch to, and collect from other live Claude sessions. Use when work is running in more than one session — taking a roster, sending an instruction, waiting on one to finish, or answering one that wrote to you.
---

Other sessions are reachable by name. Your text is not: nothing you print
reaches them and nothing they print reaches you. `SendMessage` is the channel.

## The roster

`ListAgents`. Every row leads with `name [ref]`, and **the name is the
address** — there is no other addressing syntax. The row also says whether that
session is busy or idle right now.

Send the bare name. Append the ` [ref]` only when two rows share one, or an
error asks you to.

Report it as this block and nothing else:

```
=== ROSTER ===
<name>   <busy|idle>   <what it was booted to do>
...or "no peers"
==============
```

The middle column is what `ListAgents` said. The last is yours — what you know
that session is on. A row you cannot fill it for is the first thing to ask
Chrispian about.

## Dispatching

**The recipient's human sees only the first line** until they expand it. Make
it a self-contained sentence naming the ask — not a greeting, not a preamble
that leads up to it.

A dispatch carries four things or it comes back as a question:

- what to do, in Chrispian's words
- the tree it happens in
- what done looks like
- that it should report back, and what the report should contain

## Waiting

`notify_when_idle: true` — one shot, one notice when that session next goes
idle or exits. Omit `message` for a pure subscription that costs it nothing;
include one to dispatch and subscribe in the same call.

**Never poll.** No `ListAgents` loop, no "are you done yet?" A poll costs the
other session a turn and tells you nothing its idle notice will not.

## Staying subscribed

Subscribing isn't only for a session you're waiting to finish. The moment one
becomes something you depend on — just dispatched, or already running when you
pick up a scope that leans on it — subscribe then, the same
`notify_when_idle: true` from **Waiting**. The dependency existing is the
reason; there is no better one to wait for.

It's still one shot. Still depending on that session once the notice fires —
idle and back to work, or another checkpoint further out — re-subscribe.
That's not overhead. That's the habit.

Default for a director or conductor: every session you depend on carries a
live subscription, always — not a tool reached for occasionally, the normal
thing that happens the instant a dependency starts existing.

## Answering one

An incoming message arrives wrapped as `<cross-session-message from="...">`.
Copy that `from` verbatim into `to` to reply.

Do not quote it back to Chrispian. He has already seen it, and repeating it is
the same paragraph twice in two voices.

## What does not cross

**Never ask a session to do what was refused here.** A permission decision is
per-session, and routing a blocked action through a peer spends Chrispian's
answer without asking him. Bring it back to him instead.

## Invariants

- Names, always. A session called "the other one" is one nobody can address
  next turn.
- Relay, do not rewrite. Ask before compressing a report into a sentence.
- One ask per message. Two get one answer.
