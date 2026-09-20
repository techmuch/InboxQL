# GLiNER as an annotator engine

*A phased plan. 19 September 2026.*

## What this is for

A GLiNER model scores spans of the text you gave it. It cannot return a value
that is not in the message, because the only thing it can return is a pair of
offsets. That is the entire argument for it, and it is a good one: the OCR pass
in this repo has already been caught reading "POWERS" as "POURS" and inventing a
loyalty-scheme line that was not on the receipt. An extractor built on a
generative model has the same failure mode with none of the visual excuse.

Everything else — scoping, versioning, three-way status, confidence, human
override, consent — already exists in `internal/store/annotators.go` and does
not need rebuilding. This plan is about the one thing that does not exist yet,
and about resisting the urge to build the six things that do.

---

## Three findings that change the shape of Phase 1

The idea arrived as "spike `hugot-gliner2`". Before planning around it, I checked
it. All three of these are load-bearing:

**1. `hugot-gliner2` does not exist.** Hugot is real
([knights-analytics/hugot](https://github.com/knights-analytics/hugot), v0.7.8,
2 September 2026) and its pipeline package defines ten pipelines —
TextClassification, TokenClassification, ZeroShotClassification, TextGeneration,
FeatureExtraction, ImageClassification, ObjectDetection, QuestionAnswering,
CrossEncoder, Tabular. **None of them is GLiNER.** TokenClassification is not a
substitute: it is BIO tagging over a fixed label set, which is the architecture
GLiNER exists to replace. The official non-Python engine for GLiNER2 is
[`gliner2-rs`](https://github.com/lmoe/gliner2-onnx), which is Rust.

**2. Hugot's fast path is the wrong platform.** Its ORT and XLA backends need
CGO plus a `libonnxruntime.so` *and* a statically linked `libtokenizers.a`, and
the project states it is "only built/tested on amd64-linux". This machine is
darwin/arm64. The pure-Go backend (gomlx + onnx-gomlx) needs no cgo and is the
only one that fits, but that is the backend with no GLiNER pipeline on top.

**3. Hugot requires Go 1.27.0.** `go.mod` here says 1.25.6. The installed
toolchain is go1.27.1 darwin/arm64, so this is a `go.mod` bump rather than a
blocker — but it is a bump taken on behalf of one optional feature.

**What does exist**: ONNX exports of the weights, published and downloadable —
`lion-ai/gliner2-base-v1-onnx` as a single encoder+span-head file, and
`SemplificaAI/gliner2-multi-v1-onnx` fragmented for the Rust engine.

So Phase 1 is not "wire up a library and see". It is **"write GLiNER span
decoding in Go"**, and the reason the gate comes first is that this is now
most of the work rather than a preliminary to it.

---

## Phase 1 — The spike, and the gate

Throwaway code. A `cmd/glinerspike` that is deleted whichever way the gate goes,
or kept for a week and then deleted. It does not import `internal/store`, it
does not touch the schema, and nothing else in the tree learns it exists.

### Spike GLiNER v1, not GLiNER2

The goal said gliner2. Do v1 first anyway. The question being gated is *does
span decoding work in Go*, and v1 has the simplest and best-documented decode:
one encoder, one span head, a score grid over (start × width), sigmoid,
greedy non-overlapping selection. GLiNER2 splits that across `span_rep`,
`count_pred`, `count_lstm` and `classifier` heads, and GLiNER2.5 is span-*free*
— a different decode again. Answering the question on the hardest variant first
is how a spike turns into a month. If v1 decodes correctly in Go, moving to
gliner2 is a bounded follow-on; if v1 does not, gliner2 certainly will not.

### The ten messages

Drawn from a **copy** of `/Users/david/projects/inboxQL/data/inboxql.db` — never
the original, as with every other pass in this project. Chosen deliberately, not
at random:

| # | Shape | What it tests |
|---|---|---|
| 1–2 | Receipt / invoice | amounts, dates, vendor — the bread-and-butter case |
| 3 | Booking or appointment confirmation | dates and times in prose, a location |
| 4 | Something with an account or order number | long alphanumerics, where tokenizers hurt |
| 5–6 | Ordinary human correspondence | person and org names in running text |
| 7 | A message whose real content is in an attachment | feeding `attachment_text` in, not the body |
| 8 | Non-English or accented content | tokenizer round-tripping |
| 9 | A long marketing email | offset drift at depth, truncation behaviour |
| 10 | **A newsletter with no extractable entities** | the negative control |

Number 10 is the one that matters most. A span model that confidently returns an
"amount" from a newsletter is worse than no extractor, because the query
language will happily hand those rows back under `extract:`. If the spike only
ever gets shown messages that do contain entities, it has not been tested.

Label set to score against: `amount`, `due date`, `vendor`, `order number`,
`account number`, `person`, `location`.

### Three routes, in order of preference

**Route A — pure Go: gomlx + onnx-gomlx directly.** No new native dependency, no
second shared library to install, still one binary. This is the only route whose
success means the feature can ship as InboxQL ships everything else. Risks, in
the order they will bite: whether onnx-gomlx implements every operator in the
GLiNER graph, and the tokenizer.

**Route B — hugot's pure-Go backend as a model runner.** Same engine underneath,
but hugot has already solved model loading and tokenizer plumbing. Costs a
`go.mod` bump to 1.27 and a large dependency tree for one optional feature.
Worth it only if Route A stalls on plumbing rather than on decoding.

**Route C — `yalue/onnxruntime_go` + `libonnxruntime.dylib`.** Fastest route to
a correct answer, and **diagnostic only**. It requires a second native library
installed on every machine that runs InboxQL — which is precisely the trade this
project already declined for sqlite-vec, and which `internal/filetext/pdf.go`
exists in its current from-scratch form to avoid. Route C can prove that the
decode logic is right. It cannot make the feature shippable. **A green Route C
with a red Route A and B is a no-go for Phase 2**, not a green light with an
install note.

### The tokenizer may decide the route before the decoder does

GLiNER's usual backbones are DeBERTa-v3 / mDeBERTa-v3, which tokenize with
SentencePiece unigram, and the input format prepends the labels as special
tokens: `[CLS] <<ENT>> amount <<ENT>> vendor <<SEP>> the text… [SEP]`. Two
things to establish in the first hour, because they are cheap to check and
expensive to discover late:

- Does a pure-Go SentencePiece unigram tokenizer exist that can load the model's
  own `tokenizer.json`, and does `<<ENT>>` survive it as a single token?
- If not — a GLiNER variant on a BERT/DistilBERT WordPiece backbone is a
  realistic fallback, because WordPiece in Go is a couple of hundred lines and
  has no external dependency at all.

Get this wrong and the spans will be off by a token or three in exactly the
messages that matter, which looks like a broken decoder and is not one.

### The gate

Go/no-go, judged on the ten messages, in one working session. Not two.

- **Offsets land on the right characters in at least 9 of 10.** This is the
  whole gate. Not "did it find something" — extract the span using the returned
  offsets and check it is the string a human would have highlighted.
- **Message 10 returns nothing**, or returns only spans below a threshold that
  also keeps the true positives on 1–9. A threshold that cannot separate them is
  a fail.
- **It finds at least what the existing LLM extractor finds** on 1–4, run on the
  same messages as the control.
- **Under ~2 seconds per message** on CPU for a base-sized model. Slower than
  that and the batch story has to be re-thought before anything is built on it.
- **Route A or B is the one that passed.** See above.

Any of these red: stop, write down what broke, and the answer to the whole idea
is no for now. That is a real outcome, not a failure — it costs a session and
saves the four phases below.

---

---

## Phase 1 result: green, 19 September 2026

**GLiNER v1 span decoding works in Go, with no cgo and no ONNX Runtime.**
`onnx-community/gliner_base` (deberta-v3-base, 793 MB fp32) running on
gomlx's pure-Go backend, tokenized by `tggo/goSentencePiece` — which is pure Go
too, reproduces DeBERTa-v3's pieces and round-trips exactly.

### Against the gate

| Criterion | Result |
|---|---|
| Offsets land correctly, ≥ 9/10 | **77 spans across two runs, 0 misaligned, 10/10 messages clean.** Checked mechanically: every span must begin and end on whitespace, because a broken word-to-subtoken alignment shows up as a plausible label on a shifted span. |
| Message 10 returns nothing | **Pass, once the label set is a real one.** With a loose `vendor` label it called "GPT-3" a vendor. With the five financial labels an extractor would actually use, **six of the ten messages return nothing at all** — including the negative control — and 1–4 return records. |
| Finds what the LLM extractor finds on 1–4 | **Pass, and then some.** See below. |
| Under ~2s per message | **2.8s.** Over, by 40%. |
| Route A or B passed | **Route A**, the one that can ship. |

### The comparison, run on the same messages

An LLM extractor (`gemma-4-e4b-it-4bit` on local Swama) with the same five
fields, over the same mail:

| | GLiNER | LLM extractor |
|---|---|---|
| Per message | 2.8s | 38s |
| The receipt (most content of the five) | 8 spans | **failed** — reply unparseable |
| Coolray invoice | `Invoice #605845319` @ 0.703 | `605845319` @ 0.99 |
| Order #109870 | `109870` @ 0.589, `$675.00` @ 0.878 | `109870`, `$675.00` @ 1.0 |

And the finding that settles the argument. Asked for five fields on the order
confirmation, the LLM returned:

```json
{"account number":"N/A","amount":"$675.00","due date":"N/A",
 "invoice number":"N/A","order number":"109870"}
```

**confidence 1.0.** Three of those five values are invented — the string "N/A"
is not in the message, and `extract:money.due_date` would now match a record
whose due date is a literal "N/A". A span model cannot do this: it has no way
to return a value that is not in the text, so it returns no span. That is the
whole premise, demonstrated on this mailbox rather than argued from first
principles. The 1.0 is worth noting on its own — a self-reported confidence of
certainty attached to three fabrications, against GLiNER's 0.589 on a real
order number and 0.878 on a real amount.

GLiNER is not clean: it offered `Table 31` as an invoice number and
`MC|CNF|en_US` as an account number, at 0.525 and 0.634. But both are real
strings at real offsets, so a human who checks sees exactly what the model saw,
and both sit below the true positives rather than above them.

### What it cost, and what that means for Phase 3

The stock export cannot be compiled ahead of time. Seven changes to the graph
were needed, each verified against ONNX Runtime running the same tensors:

1. `NonZero(words_mask > 0)` → an explicit `word_positions` input — data-dependent output shape.
2. `NonZero(input_ids == <<ENT>>)` → an explicit `prompt_positions` input — the same problem.
3. `Flatten(axis=2)` on a rank-2 tensor → the equivalent `Reshape` — legal ONNX, unimplemented in onnx-gomlx.
4. `ScatterElements` in the packed-sequence unsort → the batch-of-1 identity, which is what it computes.
5. `ReduceSum(input_ids == <<ENT>>)` → the fixed label count.
6. `text_lengths` from an input to an initializer.
7. All input shapes pinned, then the whole shape algebra constant-folded: 4951 nodes → 856.

**Consequence for Phase 3: InboxQL cannot use a stock GLiNER ONNX file.** It
needs a prepared one. Today that preparation is a Python script (`onnx` +
`onnxsim`) run once, offline — not a runtime dependency, and nothing about it
reaches the binary. But it is a step, and Phase 3 has to decide whether the
prepared model is what gets downloaded, or whether the rewrite moves into Go.

Two costs the fixed shapes impose, both to carry into Phase 2: **batch size 1**,
and **160 words / 384 tokens per pass**, so anything longer needs chunking.

### One wrong turn worth recording

Pinning the wrong reduction cost an hour. The graph has two that look alike —
one counts `<<ENT>>` markers, one relates to words — and pinning the label count
to 160 produced an `Add` of a `[12]` against a `[160]` several hundred nodes
downstream, with no indication of the cause. What found it was running the
tensors through ONNX Runtime, which names the failing node. Keep that loop:
**Go builds the inputs, ORT adjudicates the graph.**

---

## Phase 2 — An engine, not a pipeline, and not a provider

Phase 1 is green, so this proceeds.

### Not an `Extractor` in `internal/llm`

The goal offered two shapes. The code answers which one, and it is not the
capability interface:

`internal/llm` is a **completion gateway over HTTP**. Its whole surface is
`Provider{Name, Complete}`, `postJSON`, endpoints, API keys, `DefaultEndpoints`,
`IsRemote()`. A GLiNER backend is an in-process model file with no endpoint and
no key. Implementing `Provider` for it would mean a `Complete` that is
meaningless and a `Name()`/`IsRemote()` pair that report on something that does
not exist.

The `Viewer` precedent argues *against*, not for. Read its own doc comment:
`Viewer` is a capability some *already-existing providers* happen to have,
discovered by type assertion on a `Provider` you already built. There is no
GLiNER provider for an `Extractor` capability to hang off. Copying the shape
without the situation that produced it is how a codebase acquires an interface
nobody can explain a year later.

### `engine = "gliner"`, which costs one migration: none

The annotator row already carries the discriminator. From the v16 DDL in
`internal/store/store.go:677`:

```sql
engine       TEXT NOT NULL,
```

**No CHECK constraint.** A third value needs no migration, and `SaveAnnotator`
needs no change. The dispatch sites are enumerable — thirteen non-test
references to `EngineRule`/`EngineLLM` across five files:

| File | What changes |
|---|---|
| `internal/store/annotators.go:41` | add `EngineGLiNER = "gliner"` |
| `internal/annotate/annotate.go:200` | one more `case` in `Run`'s switch |
| `internal/annotate/annotate.go:141` | `Describe` reports a local engine |
| `internal/cli/annotate.go:230` | accept it in `--engine` validation |
| `internal/cli/annotate.go:233` | it *can* extract, unlike a rule |
| `internal/api/queryapi.go:539` | the same, on the API side |

### What already works and needs nothing

This is the part of the premise that holds up under inspection, so it is worth
being specific about *why* each one is free:

| Capability | Why it already works |
|---|---|
| Scoping | `PendingMessages`/`Progress` key on `annotator_id` + `version` and take a query filter. They never look at the engine. `--scope "label:invoice"` works on day one. |
| Incremental re-run | Same mechanism. A message with a row at the current version is done. |
| Version invalidation | `SaveAnnotator` bumps `Version` when `Instructions` change. For a GLiNER annotator the instructions *are the label set* — so changing the labels correctly invalidates prior results. |
| Three-way status | `StatusOK` / `StatusEmpty` / `StatusFailed`. Message 10 from the spike stores `StatusEmpty`, which is what makes `-extract:x` honest. |
| Human override | `SaveAnnotations` deletes `WHERE source != 'human'`; `PendingMessages` treats a human row as done. Both engine-blind. |
| Confidence | `annotations.confidence` is per row, and each record is its own row via `seq`. |
| Query surface | `extract:name`, `extract:name.field>1000`, `@0.8` — all compile against the annotations table, not the engine. |

### Confidence gets better, not just equal

Worth stating because it is the one place the new engine is *superior* rather
than merely local. In `parseResponse`, every record from an LLM extractor is
given the same self-reported number:

```go
out = append(out, Result{Matched: true, Data: rec, Confidence: reply.Confidence})
```

One value, invented by the model about its whole reply, copied onto each record.
GLiNER produces a sigmoid score **per span**. `extract:invoices.amount@0.8`
starts meaning "this particular amount scored 0.8", which is what a reader
already assumes it means.

### What actually has to be written

Small, and that is the point of choosing this shape:

- `internal/gliner` — load, tokenize, forward, decode. The spike code,
  rewritten properly, with the ten messages promoted to a fixture.
- `runGLiNER` in `internal/annotate`, alongside `runLLM` — fetch pending, decode
  spans, map each to a `Result`, write with `SourceLLM` and `Model` set to the
  GLiNER model name. Reusing `SourceLLM` is deliberate: the three sources are
  ordered by *authority*, and a GLiNER result has exactly the authority of an
  LLM result — below a human, above nothing. Provenance lives in `model`, which
  already records it. A fourth source value would be a schema change that buys
  nothing today.
- Label derivation from `schema_json`, which extractors already carry. The
  schema's field names are the label set; no new column.
- Consent, which is a deletion rather than an addition: `Describe` reports
  `Remote: false` and a local engine cannot trip `ConsentMissing`.

---

## Phase 3 — Where the model lives

The honest failure mode is "you asked for an extractor and there is no model on
this machine", and it has to be answered before the feature ships, not after.

- Weights go under the data directory, beside `vault.key` and the blob store —
  not vendored into the binary. A few hundred megabytes in a Go binary is not a
  single-binary story, it is a bad one.
- `iql doctor` gains a check, which is now genuinely cheap: `internal/health`
  already returns `Check{Name, Status, Detail, Remedy, Job}` and the Maintenance
  panel already puts a button on any check that names a `Job`. A `gliner model`
  check with `Job: "gliner-download"` gets a working Fix button in the UI for
  free, through machinery that exists.
- Download is an explicit command. Nothing fetches hundreds of megabytes because
  somebody ran `annotate run`.
- Verify the digest on download and record the model name in every annotation,
  so a result can always be traced to the weights that produced it.

## Phase 4 — The surfaces

Nothing new invented; existing surfaces extended.

- `iql annotate create x --kind extract --engine gliner --schema s.json`
- `iql annotate plan` already prints the engine and now reports "runs on this
  machine" where it would have printed an endpoint.
- The annotator UI in `queryapi.go` gains the third engine in its validation and
  its form.
- `iql maintenance` gains the model download as a job kind, which the panel
  renders without being taught anything new.

## Phase 5 — Judge it against the LLM engine

The phase that is easiest to skip and most worth doing, because until it is done
nobody knows whether the thing is better or merely local.

Run both engines over the same scope with the same schema. Compare on
precision — which is the entire premise — and then on recall, which is where a
span model is expected to lose. Record the result in this file. If GLiNER's
precision is not visibly better on a real mailbox, the feature has no
justification and should be deleted rather than kept for having been built.

---

## Not doing

**The Python sidecar.** Ruled out by instruction, and independently by the
project: a `pip install` and a background process on port 8000 is not something
this codebase can ask of anyone, having written its own PDF parser to avoid a
dependency two orders of magnitude smaller.

**The two-stage GLiClass-then-GLiNER pipeline.** The stage-1 classifier is the
half InboxQL already has, twice over — a rule annotator classifies a whole
mailbox in milliseconds with no model at all, and an LLM label annotator handles
what rules cannot. Scoping the extractor to the classification is already
`--scope "label:invoice"`, and it is better than the proposed pipeline because
the label is stored, queryable and human-overridable rather than being an
in-flight variable. Build the extractor half. The pipeline is already here.

**Vendoring ONNX Runtime into the release binary.** See Route C.

---

## The shape of the risk, in one line

Phase 1 is one session and answers everything. Phases 2–5 are a week and are
entirely contingent on it. Do not start Phase 2 early, and do not let Route C's
convenience quietly become the shipping plan.

---

# Built

## Phases 2–4: the engine, 19 September 2026

`engine = "gliner"` is a third annotator engine. As predicted, **no migration**:
`annotators.engine` has no CHECK constraint, so the column took a third value
and `SaveAnnotator` needed nothing. What was written:

| | |
|---|---|
| `internal/gliner` | the model — load, tokenize, window, score, decode |
| `internal/gliner/prepare.go` | the graph rewrite, in Go |
| `internal/gliner/install.go` | download and prepare |
| `internal/annotate/gliner.go` | `runGLiNER`, beside `runLLM` |
| `internal/cli/gliner.go` | `iql gliner install / status / remove` |
| `internal/health/gliner.go` | a check that appears only when something needs it |
| `internal/maintenance/gliner.go` | the install as a job, so the UI's Fix button works |

Everything the premise said was free turned out to be free. Scoping, incremental
re-run, the three-way status partition, human override and the `extract:` query
surface all work on the new engine without a line written for them, because
they key on the annotator row rather than on what produced it.

### Four things that had to be decided, not inherited

**The labels are the schema's field names.** An extractor already declares what
it produces; a separate label list would be the same information twice, free to
disagree with itself. That makes the schema the question for this engine, so
`SaveAnnotator` now bumps the version when a `gliner` annotator's schema
changes — compared by content, not text, so reindenting a file does not
invalidate a mailbox.

**The source stays `llm`.** The three sources are ordered by *authority*, and a
span result has exactly an LLM result's authority: below a human, above
nothing. Provenance lives in `annotations.model`, which already records it — and
had to be made worth recording: the first version wrote `gliner (gliner)`,
which identifies nothing. An install now writes a card beside the weights, and
every record says which ones produced it:

```
gliner onnx-community/gliner_base@bc87c9602a58
```

A model placed in the directory by hand reports `gliner (unrecorded)` rather
than a confident name that means nothing, and `iql gliner status` says so in
yellow.

**Offsets are stored against a named field.** The model is shown
`subject + "\n\n" + body`, so its offsets index a string nothing else holds.
Each record now carries `field` (`subject` or `body`) and offsets within that
field, verified end to end: **17 of 17 stored records resolve to exactly their
stored value.** They are byte offsets — the first version of the check read
them as character offsets and "failed" on every message containing the narrow
no-break space a mail client puts in `9:50 PM`.

**Long messages are bounded at 8 windows.** A pass costs ~2.5s, so a 50 KB
marketing email would otherwise take minutes by itself. This is the same
admission `maxEmbeddingChars` already makes, and `gliner.Reach` reports where
the line falls so "no amount in this message" stays distinguishable from
"nobody read the end of it".

### The Python went away

Phase 1 left the preparation as a Python script and said Phase 3 would have to
decide whether to ship a prepared model or move the rewrite into Go. It moved
into Go, and two findings made that easy:

- **The constant-folding step was never necessary.** It was masking the
  wrongly-pinned reduction from Phase 1. With the right node pinned, the
  unsimplified graph runs and gives identical spans.
- `github.com/gomlx/compute-onnx/support/protos` is a public module already in
  the tree, so the six edits are ordinary Go. `Prepare` rewrites the 793 MB
  graph in **678 ms**, and the result is span-for-span and score-for-score
  identical to what the Python produced.

So `iql gliner install` downloads what the publisher published and rewrites it
on this machine. No Python, no ONNX Runtime, no prepared binary from a host
with no claim to be trusted.

### What it costs to have

18.5 MB of extra binary and 80 modules in the tree, for a feature most people
will never switch on. Real, and worth restating whenever this is touched: it
buys a pure-Go runtime with no shared library to install, which is the only
version of this feature this project could ship.

## Phase 5: judged against the LLM engine

Both engines, same five fields, same five messages, run through the real
annotator machinery rather than a harness.

| | GLiNER | LLM (`gemma-4-e4b-it-4bit`, local) |
|---|---|---|
| Time | 80s (~16s each) | 190s (~38s each) |
| Failed | 0 | **1** — the receipt, reply unparseable |
| Matched / empty | 4 / 1 | 2 / 2 |
| Records | **29** | 2 |
| Invented values | 0, by construction | **3** |

Message by message:

- **USPS order confirmation** — GLiNER: order number
  `HA000C0JOW2B20C50IUT77365720` @ 0.827, due date `01/21/2022` @ 0.731, five
  amounts. LLM: **empty**.
- **Blue Moon receipt** — GLiNER: nine records, `$80.44` @ 0.817 among them.
  LLM: **failed**.
- **Order confirmation #109870** — GLiNER: `$675.00` at five places (0.905 down
  to 0.588), `Customer #: 75548839`, the card's last four. LLM: `$675.00`,
  `109870`, and three `"N/A"`s at confidence 1.0.
- **SpringHill reservation** — both empty. Agreement on a negative is worth as
  much as agreement on a positive.

**Verdict: it earns its place.** Not because it is cleverer — it is not; it
offered `Table 31` as an invoice number at 0.593 — but because it cannot
fabricate, it does not fail on the messages with the most in them, it is twice
as fast, and its confidence is a per-value number rather than a mood.

One thing to carry forward that neither the plan nor the spike anticipated: a
span extractor asked for "account number" will find **card numbers and customer
references**, because they are in the mail and it is doing what it was told.
That is not a defect, and it is arguably safer than the alternative — the
values are stored with offsets into the message they came from, so they can be
audited and deleted — but it means the label set is a privacy decision, not
only an accuracy one. Worth a sentence in the docs before anyone points this at
a whole mailbox.
