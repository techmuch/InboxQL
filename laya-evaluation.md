# Laya as a local System 1 decision model

*Research report. 2 October 2026.*

**Verdict up front:** Laya is real, genuinely open (Apache 2.0), and the fastest
thing in its category on a GPU. It is **not** a drop-in replacement for a hosted
API if you intend to run it on CPU — the authors' own numbers show 580 ms for a
single question on a 4-core server CPU, which is 17× the T4 figure and
disqualifying for the "high-throughput, low-latency" brief. Budget for a GPU or
do not do this.

Two further asterisks on the marketing, both from the project's own docs:
calibration is poor as shipped and must be refit, and the headline accuracy
belongs to one fine-tuned checkpoint, not the base ones.

---

## 0. Provenance, and why it matters here

Everything below is from sources fetched today. Laya shipped in **September
2026**, Cloudflare's Clef in roughly the same window — both after my training
data ends, so I have no prior knowledge of either and nothing here is recalled.

The first page of search results for "Laya" is almost entirely SEO aggregators
republishing each other, several with numbers that disagree. **Every figure in
this report comes from the project's own repository or documentation**, and the
caveats section is largely the maintainers' own words.

| | |
|---|---|
| Repository | `github.com/NandhaKishorM/laya` |
| Weights | `huggingface.co/convaiinnovations/laya` |
| Documentation | `nandhakishorm.github.io/laya` |
| Benchmarks | `BENCHMARKS.md` in the repo |
| Licence | Apache 2.0 |

Note the repo lives under a personal account (`NandhaKishorM`), not an org.
There is already a third-party PR elsewhere asking a downstream project to
credit Convai Innovations properly. For a dependency you intend to ship on,
pin a revision and vendor the weights.

---

## 1. Technical overview and architecture

### It is an encoder, not a small LLM

Laya is an **encoder-only transformer with classification heads** — BERT-family,
not a generative model with constrained decoding. There is no sampler, no
output token, and therefore nothing to hallucinate. One forward pass produces a
probability distribution per question.

This is the architectural difference from everything in the "structured output"
space: you are not constraining a generator, you are not running one. That is
where the speed comes from and it is also the ceiling on what it can do.

### Three checkpoints

| Checkpoint | Backbone | Params | Context | For |
|---|---|---|---|---|
| `laya` | ModernBERT-large | 421M | 512 tok | English |
| `laya-multilingual` | mmBERT-base | 322M | 1024 tok (to 8,192) | 100+ languages |
| `laya-typed-decisions` | ModernBERT-large | 421M | 1024 tok | fine-tuned for workflows |

A `Router` sniffs script and language per request and picks a checkpoint.

**The smaller multilingual checkpoint is also the faster one, by a lot** — 193 ms
vs 580 ms on server CPU for a single question, and 32.8 ms vs 39.5 ms on a T4.
That is a 3× CPU gap for a 1.3× parameter difference, which is more than size
explains; the 1024-token context and different backbone are doing something.
**Treat `laya-multilingual` as the default**, not the fallback, and benchmark
the English checkpoint against it on your data before assuming it wins.

### Memory

~1–2 GB per resident checkpoint. The runtime keeps 2 resident by default with
LRU eviction; `Router(preload=True)` loads all three and is what the docs tell
you to use in production to avoid rebuild latency mid-traffic.

So: **3–6 GB** if you preload everything, 2–4 GB for a two-checkpoint working
set. This fits comfortably beside other things on a 16 GB GPU.

### Hardware — read this before planning

| Hardware | 1 question | 10 questions | 50 questions | Cold load |
|---|---|---|---|---|
| Tesla T4 (`laya`) | 39.5 ms | 158.6 ms | 771.3 ms | — |
| Tesla T4 (`multilingual`) | 32.8 ms | 72.3 ms | 337.4 ms | — |
| GB10 / DGX Spark (p50) | 100.2 ms | 159.3 ms | 443.1 ms | — |
| Laptop CPU (Ryzen 9 6900HX, 8 threads) | 329 ms p50 | — | — | — |
| **Server CPU (EPYC 9R14, 4 cores) `english`** | **580 ms** | **6,244 ms** | **35,969 ms** | 4.4 s |
| Server CPU (EPYC 9R14, 4 cores) `multilingual` | 193 ms | 1,842 ms | 11,157 ms | 2.5 s |

Sustained GPU throughput, from the repo's capacity table:

| GPU (TensorRT FP16) | p99 ≤ 50 ms | p99 ≤ 130 ms |
|---|---|---|
| RTX PRO 5000 | 15 decisions/s | 42 decisions/s |
| RTX PRO 6000 | — | 146 decisions/s |
| H100 NVL | 105 decisions/s | 175 decisions/s |

**105 decisions/s at p99 ≤ 50 ms on an H100** is the number to plan against. If
your routing volume needs more than that, this is a multi-GPU deployment and
the economics against a hosted API need redoing.

### How the routing works

Two distinct things both called "routing", and conflating them will cost you a
day:

1. **Checkpoint routing** — Laya's internal `Router` detects script/language and
   picks which of the three checkpoints answers. Reported back in
   `result["routing"]` with a `reason` string.
2. **Your routing** — the `choice` primitive returns a label from your option
   set, which is what you act on.

### The three primitives

| Primitive | Question | Returns |
|---|---|---|
| `choice` | pick one of these labels | label + per-option probabilities + confidence |
| `score` | where on this ordinal scale | expected level + distribution |
| `noul` | is this statement true | P(true) as a float |

Everything — intent detection, guardrails, triage, escalation — is composed from
these three. There is no free-text escape hatch, by design.

---

## 2. Capabilities and benchmarks

### Where it fits

Intent classification, department/queue routing, guardrail and policy checks,
urgency and priority scoring, churn and risk flags, multilingual triage, and
long-document scanning via `predict_long` (overlapping windows, reports
`usage["windows"]`).

### Against Jev

From the repo's `BENCHMARKS.md`:

| Benchmark | Laya | Jev (published) | n |
|---|---|---|---|
| typed-decisions | **0.766** | 0.727 | 2,000 decisions |
| AG News (4 labels) | **0.953** | 0.910 | 400 |
| DAIR Emotion (6 labels) | **0.600** | 0.480 | 400 |
| ECE after temperature fitting | **0.081** | 0.246 | — |

These are self-reported by the challenger against a competitor's *published*
numbers, which is the weakest form of comparison there is. Treat the direction
as informative and the magnitudes as unverified until you run your own set.

### Against Clef-flash

Worth correcting the premise in the brief: **Clef-flash is not really a peer.**

| | Laya | Clef-flash |
|---|---|---|
| Parameters | 421M / 322M | **9B** |
| Latency | 32.8–39.5 ms (T4) | 191–205 ms median, 676 ms worst |
| Deployment | local, Apache 2.0 weights | Workers AI; weights on HF, Apache 2.0 |
| Vision | no | yes, up to 4 images |
| Context | 512–8,192 tok | 64,000 tok |

Clef-flash is 20× the parameters and roughly 5× the latency. It is a different
trade: far more capacity, vision, a huge context, and Cloudflare's edge — against
Laya's much smaller footprint and lower latency.

Caveat on that latency comparison: Laya's 33 ms is local GPU compute, Clef's
~200 ms is an API call with network in it. They are not measured the same way
and the gap on compute alone is smaller than the table implies.

**If you need a 64k context or images, Laya cannot do the job at any speed.**

### The two asterisks

**Calibration is bad out of the box.** The project's headline is "calibrated
probabilities", and the ECE table says:

| Checkpoint | As shipped | After temperature refit |
|---|---|---|
| `laya` | **0.466** | 0.081 |
| `laya-multilingual` | **0.314** | 0.106 |

An ECE of 0.466 means the confidence numbers as shipped are close to
meaningless. The docs say the base checkpoints are "over-confident on the
published suites as shipped." **If you gate on `min_confidence` without
refitting temperature on your own data, you are gating on noise.** This is a
required sprint task, not an optimisation.

**The headline accuracy is one checkpoint.** 0.766 is `laya-typed-decisions`.
The docs state the base checkpoints score *below the 0.461 majority-class
baseline* on that same task — i.e. worse than always guessing the commonest
answer. Which checkpoint the Router picks therefore matters enormously, and the
Router picks by language, not by task.

### The maintainers' own warnings

Quoted from the benchmarks guide, because they are unusually candid and each one
is a live risk:

- the English checkpoint "can be confident on text it handles badly outside
  English";
- it "performs poorly" with 77 labels at the default token budget — **keep option
  sets small**;
- the multilingual encoder gives "less reliable answers beyond about 4,000
  tokens" despite the 8,192 ceiling;
- "Option order can change a choice answer" — so it is not order-invariant, and
  shuffling options is a cheap robustness check worth running;
- "T4 figures do not predict CPU or cold-load time."

---

## 3. Integration

### Install

```bash
python -m pip install laya            # requires Python 3.10+
# extras: laya[serve] laya[mcp] laya[langchain] laya[onnx]
```

### The raw API

```python
from laya import Router

router = Router(preload=True)   # load all checkpoints at startup, not on first request

result = router.predict(
    {"body": "We were billed twice. Please refund the duplicate."},
    {
        "department": {
            "type": "choice",
            "instructions": "Which department handles this?",
            "criteria": {
                "billing":   "invoices, payments, refunds",
                "technical": "bugs, outages, errors",
                "other":     "everything else",
            },
        },
        "urgency": {
            "type": "score",
            "instructions": "How urgent?",
            "criteria": ["not urgent", "soon", "critical"],
        },
        "churn_risk": {
            "type": "noul",
            "instructions": "Does the user threaten to leave?",
        },
    },
)

result["answers"]["department"]["choice"]          # "billing"
result["answers"]["department"]["probabilities"]   # {"billing": 0.94, ...}
result["answers"]["churn_risk"]["noul"]            # P(true)
result["routing"]["model"]                         # which checkpoint answered
```

### Type-safe outputs — the part you actually want

Laya maps a **Pydantic model or JSON Schema** onto its primitives and projects
the answers back onto your types. This is native, not a bolt-on.

```python
from typing import Literal
from pydantic import BaseModel

class Ticket(BaseModel):
    department: Literal["billing", "support", "sales"]
    urgency: Literal[0, 1, 2]
    needs_human: bool

ticket = agent.decide("I was charged twice, refund me.", schema=Ticket)
```

The mapping is mechanical and worth knowing, because it determines what you can
express:

| Schema | Primitive | Output |
|---|---|---|
| `{"enum": [...]}` / `Literal[...]` | `choice` | original type preserved |
| `{"type": "boolean"}` / `bool` | `noul` | True/False |
| `integer`/`number` with `minimum`+`maximum` | `score` | integer level |
| `{"const": v}` | `choice` | that value |
| `Optional[...]` | unwrapped | field omitted when unanswered |

Limits: **32 properties, 32 options per field, 10 score levels.** Exceeding any
raises `laya.structured.SchemaError` naming the exact path — a hard failure, not
a silent truncation, which is the right behaviour.

Supporting functions: `laya.decide(...)`, `agent.decide(...)`,
`router.decide(...)`, `decide_batch(...)`, `questions_from_json_schema(...)`,
`questions_from_pydantic(...)`, `answers_to_json(...)`.

### Confidence gating — the production shape

```python
result = agent.decide(state, schema=Ticket, return_details=True)

result.values["department"]             # "billing"
result.answer_confidence["department"]  # 0.94  ← gate on this
result.probabilities["department"]      # full distribution
result.usage                            # tokens, truncation report
result.routing                          # which checkpoint, and why
```

`answer_confidence` is the max probability for the field. `confidence` is a
normalised entropy and **varies with option count**, so a threshold tuned on a
3-option field will not transfer to a 10-option one. Gate on `answer_confidence`,
escalate to an LLM below threshold, and set the threshold per field from your own
held-out set — after refitting temperature.

### Batching is where the throughput is

```python
values  = agent.decide_batch(ticket_texts, schema=Ticket)
results = router.decide_batch(states, schema=Ticket, return_details=True, batch_size=64)
```

Order is preserved. On a T4, 10 questions in one call is 72.3 ms versus 32.8 ms
for one — **7.2 ms per question batched against 32.8 ms unbatched, a 4.5×
improvement.** If your traffic allows a few milliseconds of accumulation, batch;
it is the single largest lever here.

### Node.js

There is no official Node binding. Two routes:

1. **The HTTP server** (official, `laya[serve]`) — run Python in a container, call
   it over HTTP from Node. This is what I would do.
2. **`github.com/receptron/laya`** — third-party ONNX Runtime binding for
   Node/TypeScript. Also `github.com/dockndevai/laya-models` (ONNX for the
   browser) and `github.com/enlotec/laya-serve` (third-party serving wrapper).
   All community, all weeks old. Do not put one on the critical path this sprint.

```ts
// Against the official server. Keep this behind your own typed wrapper.
const res = await fetch("http://laya:8000/v1/systemone", {
  method: "POST",
  headers: { "content-type": "application/json", authorization: `Bearer ${process.env.LAYA_API_KEY}` },
  body: JSON.stringify({
    state: "I was charged twice this month, I want my money back",
    questions: {
      queue: {
        type: "choice",
        instructions: "Which team?",
        criteria: { billing: "billing and refunds", tech: "login issues" },
      },
    },
    min_confidence: 0.7,
  }),
});
const { answers, routing, usage } = await res.json();
answers.queue.choice;             // "billing"
answers.queue.answer_confidence;  // 0.9519
// Response also carries: Server-Timing: inference;dur=<ms>
```

Server limits: 2 MiB body, 50,000 characters of state, **64 questions per
request, 100 options per choice**, `LAYA_MAX_CONCURRENT` default 16.

---

## 4. Deployment and trade-offs

### Docker

```bash
docker compose run --build --rm laya                                    # CPU
docker compose -f compose.yaml -f compose.cuda.yaml run --build --rm laya   # NVIDIA
docker compose -f compose.yaml -f compose.http.yaml up --build laya-serve   # HTTP
```

| Variable | Default | Purpose |
|---|---|---|
| `LAYA_DEVICE` | `cpu`/`cuda` | device |
| `LAYA_MODEL` | `auto` | `auto`, `english`, `multilingual`, `typed-decisions` |
| `LAYA_PORT` | `8000` | port |
| `LAYA_API_KEY` | unset | bearer token |
| `LAYA_PRELOAD` | `0` | **set to 1 in production** |
| `LAYA_BIND_ADDRESS` | loopback | `0.0.0.0` to expose |
| `HF_TOKEN` / `HF_TOKEN_FILE` | unset | HF credentials |

Model cache is the named volume `laya-model-cache` at
`/home/laya/.cache/huggingface`, **writable by UID 10001**. Get that wrong and
every pod re-downloads the weights on start.

`GET /health` reports `device`, `loaded`, `revisions`, and `cpu_fallbacks` —
that last one is your canary: a non-zero `cpu_fallbacks.count` means the GPU
silently dropped out and your p99 has quietly become 580 ms. **Alert on it.**

Port publishes to `127.0.0.1` by default. ARM64/DGX images exist.

### Kubernetes sketch

Nothing exotic: GPU node pool, `nvidia.com/gpu: 1`, 6–8 GB memory limit with
preload, readiness probe on `/health` with an **initial delay past cold load**
(0.5–4.4 s depending on checkpoint, plus first-run weight download — bake weights
into the image or warm the volume), HPA on request latency rather than CPU
(CPU will look idle while the GPU saturates).

### Suggested sprint sequence

1. **Decide the hardware question first.** Run your own traffic against CPU and
   GPU before anything else. If GPU is not on the table, stop — the CPU numbers
   do not meet the brief.
2. **Pin a revision**, vendor the weights, bake them into the image.
3. **Build a held-out set from your real traffic**, a few hundred labelled
   examples. Nothing below is meaningful without it.
4. **Refit temperature on that set.** Non-optional. Record ECE before and after.
5. **Pick a checkpoint on evidence**, not by assuming English is best for English
   — measure `multilingual` too, it is faster and may be as accurate.
6. **Define schemas as Pydantic models**, keep option sets small, and shuffle
   option order as a robustness check.
7. **Set per-field confidence thresholds** and wire the LLM escalation path.
8. **Shadow-run against Jev** on live traffic, compare on your set, then cut over.

### Trade-offs

| | |
|---|---|
| **Cold start** | 0.5–4.4 s checkpoint load, plus first-run download. Preload; never let a request pay it. |
| **Memory** | 1–2 GB per checkpoint; 3–6 GB preloaded. Cheap. |
| **CPU is not an option** | 580 ms/question, 36 s for 50. The headline numbers are GPU numbers. |
| **Calibration is yours** | Ships at ECE 0.466. You own the refit, and you own re-doing it whenever the model or your traffic changes. |
| **Maintenance** | You now run a GPU service: drivers, CUDA, OOM, checkpoint integrity, the `cpu_fallbacks` failure mode. That is the real cost against an API, and it is ongoing. |
| **Capability ceiling** | Three primitives, ≤32 fields, small option sets, 512–8k context, no vision. Anything else escalates. |
| **Ecosystem age** | Weeks old. Personal-account repo, community bindings, API surface likely to move. Wrap it; do not scatter `laya.` calls through your codebase. |
| **What you gain** | No per-call cost, no network hop, no data leaving your infrastructure, no vendor, Apache 2.0, and 33 ms. |

### The honest summary

The case for Laya is strong *if* you have a GPU and your decisions fit three
primitives with small option sets. On an H100 it will do ~105 decisions/s at
p99 ≤ 50 ms with no marginal cost and no data egress, which no hosted API
matches on either latency or privacy.

The case collapses on CPU, and it is weaker than the marketing suggests until
you have done the calibration work yourself. Budget a sprint for the held-out
set and the temperature refit — that is the actual project, not the integration,
which is an afternoon.
