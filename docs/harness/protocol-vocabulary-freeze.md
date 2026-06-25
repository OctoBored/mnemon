# Protocol Vocabulary Freeze

This document freezes the first-class vocabulary for Mnemon Harness architecture
work. It governs new architecture docs, plans, code names, command wording, test
names, and reviews.

Freezing vocabulary does not freeze implementation. It freezes which concepts
may carry protocol meaning, and which words must remain implementation details.

## 1. Status

Status: frozen for new harness architecture work.

Scope: Mnemon Harness, including host integration, Local Mnemon, Remote
Workspace sync, event package governance, GUIDE assets, and agent-facing
read/write flows.

Older versioned documents may retain historical terms such as `loop`,
`capability`, `render`, or `presentation`. New work should use this document as
the canonical vocabulary and migrate older wording opportunistically when those
files are edited.

## 2. First-Class Concepts

Only these concepts should appear as primary nouns in architecture diagrams,
roadmaps, plans, goals, and module boundaries:

| Concept | Meaning | Boundary |
|---|---|---|
| `hostagent` | An agent runtime integrated with Mnemon. | Reads GUIDE, reads governed context, and observes events through mnemond. |
| `mnemond` | The local Mnemon governance daemon. | Admits, stores, reads, and imports events for one local workspace. |
| `mnemonhub` | The remote event exchange service. | Exchanges accepted events between mnemond instances. |
| `event` | The canonical protocol unit. | Produced, admitted, stored, synced, imported, and consumed. |
| `event package` | A governed event type declaration. | Defines event shape, admission contract, risk, sync behavior, and read projection. |
| `GUIDE` | Managed agent behavior guidance. | Tells a hostagent when to read context and when to observe durable events. |

Everything else is either an action, an implementation detail, or a legacy term.

## 3. Canonical Flow

Use this flow as the default explanation of the system:

```text
+------------------+        read GUIDE / read or observe event
| hostagent        | --------------------------------------------+
+------------------+                                             |
                                                                 v
                                                        +------------------+
                                                        | mnemond          |
                                                        | admit event      |
                                                        | store event      |
                                                        | read context     |
                                                        | import event     |
                                                        +------------------+
                                                                 |
                                                                 | sync accepted event
                                                                 v
                                                        +------------------+
                                                        | mnemonhub        |
                                                        | exchange events  |
                                                        +------------------+
```

Preferred prose:

```text
A hostagent reads GUIDE and reads or observes events through mnemond.
mnemond admits, stores, reads, and imports events.
mnemonhub exchanges accepted events between mnemond instances.
event package defines event type governance.
```

Avoid using hook, render, presentation, cue, or commit as the main flow.

## 4. Canonical Actions

Use these verbs consistently:

| Action | Actor | Meaning |
|---|---|---|
| `read` | hostagent, mnemond | Retrieve governed context derived from events. |
| `observe` | hostagent | Submit an event candidate to mnemond. |
| `admit` | mnemond | Accept or deny an observed event according to policy. |
| `store` | mnemond | Persist admitted event state and audit facts. |
| `sync` | mnemond, mnemonhub | Move accepted events across workspaces. |
| `import` | mnemond | Admit remote accepted event material into local state. |

`render` may remain as an implementation verb for event-to-context projection,
but it should not describe the protocol flow.

## 5. Auxiliary Terms

These words may remain in code and docs only with bounded meaning:

| Term | Allowed meaning | Not allowed as |
|---|---|---|
| `hook` | Host integration mechanism that reminds or bootstraps. | Domain behavior, scheduler, event writer, or protocol state. |
| `skill` | Hostagent action surface for read/schema/observe. | A protocol object or canonical state store. |
| `render` | Event-to-context projection implementation. | Main architecture flow or domain coordination layer. |
| `presentation` / `view` | Read format derived from events. | Canonical protocol state. |
| `envelope` | Internal event wrapper for storage or transport. | User-facing object or separate domain unit. |
| `store` | mnemond persistence implementation. | Protocol actor. |
| `daemon` | Process shape for mnemond. | Separate concept from mnemond. |
| `decision` | Admission result or ledger fact. | Replacement for event as the protocol unit. |
| `loop` | Legacy name for event package surfaces and commands. | New architecture concept. |
| `capability` | Legacy/spec term for selected event package behavior. | New architecture concept. |

When an auxiliary term appears in new text, anchor it back to a first-class
concept.

Example:

```text
Good: mnemond renders event-derived context for hostagent read.
Bad: render drives teamwork.
```

## 6. Discouraged Expressions

Do not use these expressions in new architecture text:

| Avoid | Prefer |
|---|---|
| hook renders teamwork presentation | hostagent reads event-derived context through mnemond |
| daemon pulls cue | hostagent reads governed context; mnemond syncs/imports events |
| commit produces cue | hostagent observes event; mnemond admits event |
| presentation drives agent | GUIDE guides hostagent behavior; event-derived context is read |
| capability implements teamwork | event package defines teamwork event governance |
| loop is the product unit | event package is the governed event type unit |

Historical docs may quote old terms when explaining migration, but new design
proposals should use the preferred forms.

## 7. Naming Rules

New names should prefer the frozen vocabulary:

```text
event
event package
GUIDE
hostagent
mnemond
mnemonhub
read
observe
admit
sync
import
```

New package, file, command, and test names should not introduce a new
first-class noun unless the Concept Change Rule below is satisfied.

Preferred patterns:

```text
event_package_*
hostagent_*
mnemond_*
mnemonhub_*
managed_guide_*
event_read_*
event_observe_*
```

Avoid new names centered on:

```text
cue
commit
presentation
render
loop
capability
projection
```

Existing released commands do not need to be renamed only for vocabulary
purity. New docs should explain their canonical concept mapping.

## 8. Concept Change Rule

A new first-class concept requires a short RFC that answers:

```text
1. Why can hostagent, mnemond, mnemonhub, event, event package, or GUIDE not express it?
2. Is it protocol state, an actor, a governed asset, an action, or only an implementation detail?
3. What are its boundaries against event and mnemond?
4. What code, command, docs, and test names would adopt it?
5. Can it remain an auxiliary term instead?
```

Default outcome: new words start as auxiliary terms. Promote only when the
existing first-class concepts cannot express the design cleanly.

## 9. Review Checklist

Use this checklist for new harness architecture changes:

```text
[ ] The main flow is expressible as hostagent -> mnemond -> mnemonhub through events.
[ ] Event remains the canonical protocol unit.
[ ] GUIDE and event package boundaries are explicit.
[ ] Hook and skill are integration surfaces, not domain logic.
[ ] Render, presentation, view, envelope, loop, and capability are not primary nouns.
[ ] New names follow the frozen vocabulary or include a concept RFC.
[ ] Historical terms are either mapped to canonical terms or left only in versioned legacy docs.
```
