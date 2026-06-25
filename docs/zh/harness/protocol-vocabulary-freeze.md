# Protocol Vocabulary Freeze

本文冻结 Mnemon Harness 架构工作的一等词汇。它约束新的架构文档、计划、代码命名、命令文案、测试命名和 review。

冻结词汇不是冻结实现。它冻结的是哪些概念可以承载协议语义，以及哪些词只能作为实现细节。

## 1. 状态

状态：对新的 harness 架构工作冻结。

范围：Mnemon Harness，包括 host integration、Local Mnemon、Remote Workspace sync、event package governance、GUIDE 资产，以及面向 agent 的 read/write 流程。

较早的版本化文档可以继续保留 `loop`、`capability`、`render`、`presentation` 等历史术语。新工作应以本文作为 canonical vocabulary，并在编辑旧文件时顺手迁移旧表达。

## 2. 一等概念

只有这些概念可以作为架构图、roadmap、plan、goal 和模块边界中的主语：

| Concept | 含义 | 边界 |
|---|---|---|
| `hostagent` | 接入 Mnemon 的 agent runtime。 | 读取 GUIDE，通过 mnemond 读取 governed context 并 observe event。 |
| `mnemond` | 本地 Mnemon governance daemon。 | 为一个 local workspace admit、store、read 和 import events。 |
| `mnemonhub` | 远端 event exchange service。 | 在多个 mnemond instance 之间交换 accepted events。 |
| `event` | canonical protocol unit。 | 被生产、admit、store、sync、import 和消费。 |
| `event package` | governed event type declaration。 | 定义 event shape、admission contract、risk、sync behavior 和 read projection。 |
| `GUIDE` | managed agent behavior guidance。 | 告诉 hostagent 何时读取 context，以及何时 observe durable events。 |

其他词要么是 action，要么是 implementation detail，要么是 legacy term。

## 3. 标准流程

默认用这个流程解释系统：

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

推荐表达：

```text
hostagent reads GUIDE and reads or observes events through mnemond.
mnemond admits, stores, reads, and imports events.
mnemonhub exchanges accepted events between mnemond instances.
event package defines event type governance.
```

不要把 hook、render、presentation、cue 或 commit 作为主流程。

## 4. 标准动作

一致使用这些动词：

| Action | Actor | 含义 |
|---|---|---|
| `read` | hostagent, mnemond | 取回由 events 派生的 governed context。 |
| `observe` | hostagent | 向 mnemond 提交 event candidate。 |
| `admit` | mnemond | 按 policy accept 或 deny observed event。 |
| `store` | mnemond | 持久化 admitted event state 和 audit facts。 |
| `sync` | mnemond, mnemonhub | 在多个 workspace 之间移动 accepted events。 |
| `import` | mnemond | 把 remote accepted event material admit 到本地 state。 |

`render` 可以继续作为 event-to-context projection 的实现动词，但不应该描述协议主流程。

## 5. 辅助术语

这些词可以继续出现在代码和文档中，但含义必须受限：

| Term | 允许含义 | 不允许作为 |
|---|---|---|
| `hook` | 用于 remind 或 bootstrap 的 host integration mechanism。 | Domain behavior、scheduler、event writer 或 protocol state。 |
| `skill` | hostagent 的 read/schema/observe action surface。 | Protocol object 或 canonical state store。 |
| `render` | Event-to-context projection implementation。 | Main architecture flow 或 domain coordination layer。 |
| `presentation` / `view` | 由 events 派生的 read format。 | Canonical protocol state。 |
| `envelope` | 用于 storage 或 transport 的 internal event wrapper。 | User-facing object 或独立 domain unit。 |
| `store` | mnemond persistence implementation。 | Protocol actor。 |
| `daemon` | mnemond 的 process shape。 | 与 mnemond 分离的新概念。 |
| `decision` | Admission result 或 ledger fact。 | 替代 event 成为 protocol unit。 |
| `loop` | event package surfaces 和 commands 的 legacy name。 | 新的 architecture concept。 |
| `capability` | selected event package behavior 的 legacy/spec term。 | 新的 architecture concept。 |

新的文字中出现辅助术语时，必须锚回一等概念。

Example:

```text
Good: mnemond renders event-derived context for hostagent read.
Bad: render drives teamwork.
```

## 6. 不推荐表达

新的架构文字不要使用这些表达：

| Avoid | Prefer |
|---|---|
| hook renders teamwork presentation | hostagent reads event-derived context through mnemond |
| daemon pulls cue | hostagent reads governed context; mnemond syncs/imports events |
| commit produces cue | hostagent observes event; mnemond admits event |
| presentation drives agent | GUIDE guides hostagent behavior; event-derived context is read |
| capability implements teamwork | event package defines teamwork event governance |
| loop is the product unit | event package is the governed event type unit |

历史文档可以在解释迁移时引用旧术语，但新的 design proposal 应使用推荐表达。

## 7. 命名规则

新命名优先使用冻结词汇：

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

新的 package、file、command 和 test name 不应该引入新的一等名词，除非满足下面的 Concept Change Rule。

推荐模式：

```text
event_package_*
hostagent_*
mnemond_*
mnemonhub_*
managed_guide_*
event_read_*
event_observe_*
```

避免以这些词作为新命名中心：

```text
cue
commit
presentation
render
loop
capability
projection
```

已有 released commands 不需要只为了词汇纯度而改名。新文档应解释它们映射到的 canonical concept。

## 8. 概念变更规则

新增一等概念必须有一份短 RFC，回答：

```text
1. Why can hostagent, mnemond, mnemonhub, event, event package, or GUIDE not express it?
2. Is it protocol state, an actor, a governed asset, an action, or only an implementation detail?
3. What are its boundaries against event and mnemond?
4. What code, command, docs, and test names would adopt it?
5. Can it remain an auxiliary term instead?
```

默认结果：新词先作为辅助术语。只有现有一等概念无法清晰表达设计时，才允许升级。

## 9. Review Checklist

新的 harness 架构改动使用这份 checklist：

```text
[ ] The main flow is expressible as hostagent -> mnemond -> mnemonhub through events.
[ ] Event remains the canonical protocol unit.
[ ] GUIDE and event package boundaries are explicit.
[ ] Hook and skill are integration surfaces, not domain logic.
[ ] Render, presentation, view, envelope, loop, and capability are not primary nouns.
[ ] New names follow the frozen vocabulary or include a concept RFC.
[ ] Historical terms are either mapped to canonical terms or left only in versioned legacy docs.
```
