# Laya as a fourth annotator engine

*A phased plan. 3 October 2026.*

## The hole this fills

A gliner annotator **cannot label**. So every label a query cannot express has
to be `llm` — the engine that answered a receipt with three `"N/A"` fabrications
at confidence 1.0.

| Engine | labels | extracts | per message | can invent |
|---|---|---|---|---|
| `rule` | yes | no | ~0 | no |
| `gliner` | **no** | yes | 15–20 s | no |
| `llm` | yes | yes | ~40 s | **yes** |
| `laya` | **yes** | no | **~0.2 s** | no |

Laya does what `llm` does badly, two orders of magnitude faster, with no output
tokens to invent. It also adds two things the language cannot express today:
`choice` for multi-way routing in one pass, and `score` for ordinal urgency.

And the confidence plumbing already exists — the `confidence` column,
`label:invoice@0.9`, `StarterFloor`. Those were built for calibrated labels.
Laya emits exactly that.

## Why this is cheaper than GLiNER was

| | GLiNER | Laya |
|---|---|---|
| Data-dependent shapes | 7 blockers, hand-rewritten | **already explicit inputs** |
| Reference to check against | none — reverse-engineered | `@receptron/laya`, matches Python to 4 dp |
| Tokenizer | SentencePiece, new work | `tokenizer.json`, **goSentencePiece already reads it** |
| Weights | 793 MB fp32 | 647 MB fp16 |

The export at `mizchi/laya-multilingual-onnx` takes `input_ids`,
`attention_mask`, `marker_pos`, `marker_mask`, `qtype` and returns `logits`,
`act_logits`. `marker_pos` and `marker_mask` being inputs is the whole fight
from last time, already won — it was exported for onnxruntime-web, which has
the same static-shape pressure that AOT compilation does.

**Multilingual, not English**: 322M against 421M, 3× faster on CPU (193 ms vs
580 ms), and a 1024-token context against 512.

---

## Phase 1 — The spike, and the gate

Download the export; try to compile it with `onnx-gomlx`. Nothing else matters
until this answers.

**Gate:** it compiles, or it compiles after edits of the kind `prepare.go`
already makes. If it needs a new op implemented in the converter, stop and
report — that is a different project.

Then one decision on one real message, checked against the published reference.

## Phase 2 — Inference

Tokenizer, input construction, forward pass, decode. Three primitives:
`choice` (argmax over options), `score` (expected level), `noul` (P(true)).

Input construction is the risk: how instructions and criteria become tokens is
not documented. `@receptron/laya` is the oracle — diff against it on fixed
inputs rather than guessing, which is the loop that eventually worked for
GLiNER anyway.

## Phase 3 — The engine

`laya` beside `rule`, `llm`, `gliner`. Labels only in this phase: `noul` with a
threshold is a label, and the annotation row already carries confidence.

`choice` and `score` need somewhere to put a non-boolean answer, which is a
schema question, so they come after.

## Phase 4 — Calibration

`rl_agent_config.json` ships `temperature: [1.0, 1.0, 1.0]`. Uncalibrated, as
the project's own ECE table says (0.466 → 0.081 only after refitting).

So `label:x@0.9` against a raw Laya score is a floor on noise. This phase fits
temperature on the user's own corrections — which exist, and are exactly the
held-out set this needs — and stores it beside the model.

**Until this lands, the engine does not claim calibrated confidence.**

## Phase 5 — Starters

Label starters that a query cannot express, to sit beside the rule ones.

## Not doing

**`choice`/`score` in phase 3.** A label is yes/no and fits the existing row.
The other two are a schema change, and bundling them would hold up the half
that needs nothing.

**Replacing the `llm` engine.** It still does extraction with understanding.
Laya cannot extract at all.

---

## What was built

All five phases. The engine runs in pure Go — no Python, no ONNX Runtime, no
cgo beyond sqlite3.

| Phase | Where |
|---|---|
| 1 — the gate | proved the graph compiles and runs |
| 2 — inference | `internal/laya/` — tokenizer, sequence, forward pass, decode |
| 3 — the engine | `internal/annotate/laya.go`, `--engine laya` |
| 4 — calibration | `internal/laya/calibrate.go`, `iql laya calibrate` |
| 5 — starters | `purchased`, `needs-reply`, `automated` |

### Phase 1 was easier than GLiNER, as predicted

The export takes `marker_pos` and `marker_mask` as explicit inputs, so the
data-dependent shapes that cost seven blockers last time were already gone. One
blocker remained, and it was not a shape:

```
FusedLayerNorm: dtype Float16: op not implemented
```

The pure Go backend normalises in float32 and float64. The published file is
float16. Widening it at install is `internal/laya/prepare.go` — 616 MB becomes
1230 MB, and no value changes, because every float16 is exactly a float32.

### Measured here, not quoted

| | |
|---|---|
| ~9.5 ms per token, linear | so `MaxLen` is 256, not the checkpoint's 1024 |
| 2.4 s per question at 256 tokens | 1.1 s at 128 |
| **3.1 s per message** end to end | against 15–20 s for a span run, ~40 s for an LLM |
| Batching | **no gain** — 1.11 s/row at batch 1, 1.43 s/row at batch 8 |

The report that preceded this quoted ~0.2 s from the project's own CPU
benchmark. That is ONNX Runtime; the pure Go backend is about an order of
magnitude slower. Still 5–13× faster than the engines it sits beside, and the
honest number is 3.1 s.

### A bug of mine, found on real mail

`Fwd: Order Confirmation (#109870)` — a message whose subject says what it is —
scored **0.002**. Bisecting the body found the cause: I was feeding the quoted
header block a forward carries (`---------- Forwarded message ----------`, then
From, Date, To, Cc). On this mailbox that is 150–250 characters of someone
else's addresses before any content, in a window of about 200 tokens.

Stripping it moved the same message to **0.795**. `stripQuotedHeaders` does
that, keeping anything that merely looks like a header — a receipt's body is
full of `Order #:` and `Payment Method:` lines, and those are the evidence.

### A pre-existing bug, found by trying to calibrate

Calibration needs the score the model was wrong at, which means pairing a human
ruling with the machine answer it overruled. The pairing returned nothing.

`SetHumanAnnotation` writes the ruling at seq 0 with `INSERT OR REPLACE`,
against a unique key of `(message_id, annotator_id, annotator_version, seq)` —
so **recording a correction deleted the answer it was correcting**. You could
see that somebody disagreed and never what they disagreed with.

Schema v33 adds `source` to that key. "Outranks" was never supposed to mean
"erases".

The migration itself took three attempts, each failing in a way worth keeping:

1. `DROP INDEX` on a constraint-backed index — SQLite refuses, and the
   migration *appeared to succeed and changed nothing*. The worst failure
   available. It now rebuilds the table.
2. The rebuild hit a foreign key: 188 annotations reference an annotator that
   no longer exists, left by a cascade that never fired. Rebuilt with
   enforcement off, and those rows are carried rather than dropped — deleting
   somebody's annotations to change an index is not a schema bump's business.
3. A failed attempt left `annotations_v33` behind, so the retry failed too. It
   drops the temporary first.

Verified on the live database: new constraint, all 1375 rows kept.

### What it is actually like on this mailbox

Thirty messages, `"this message is a receipt, an invoice, an order
confirmation or a payment"`:

Right, with conviction — receipts, invoices, medical bills, payment receipts,
reservation confirmations. Wrong, also with conviction — a software newsletter
at **0.993**, an HVAC inspection request at 0.933.

That is the published ECE of 0.466 in the flesh, and it is why Phase 4 exists
and why nothing here claims the score is a frequency. `iql laya status` says so
every time it is asked.

**Calibration is built and unfitted.** It needs 20 rulings and refuses below
that; there is 1. Fabricating the other 19 would have produced a temperature
fitted to my guesses about someone else's mail, wearing the authority of a
measurement.

### Tests

`internal/laya/` — option rendering, the budget keeping a marker per option,
softmax, argmax, the weighted mean, temperature fitting, and that rescaling
never reorders. `internal/annotate/` — header stripping, including the content
that must survive it. `internal/store/` — the ruling join, and a regression
test for the correction bug.

Full suites green, `-race` on the six changed packages.

### Left undone

**`choice` and `score`.** The engine supports both and the annotation row has
nowhere to put a non-boolean answer. That is a schema question.

**A UI.** `--engine laya` is CLI and API only.

**The 188 orphaned annotations.** Real, pre-existing, and not this plan's
business.
