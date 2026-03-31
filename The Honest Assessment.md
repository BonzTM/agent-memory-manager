The Honest Assessment,
The architecture is genuinely good. This isn't a toy. The retrieval pipeline — 12-signal scoring with Bayesian weight learning, 3-axis candidate generation (FTS + embeddings + entity graph), hub dampening, repetition suppression, kind boosting — that's sophisticated and well-thought-out. Most "agent memory" projects are just a vector DB with cosine similarity slapped on top. This is leagues beyond that.

But there are real gaps between the design and the current state of the deployment.

---

Where Noise Leaks In,
The extraction prompt already says "at most 10-15% of events should yield a memory" and "fewer high-quality memories is always better than many thin ones." The problem is the LLM doesn't always follow those rules, and there's no backstop.

Five specific noise vectors:

No minimum confidence gate at creation. If the LLM returns a candidate with confidence: 0.3 and valid body/description, it gets stored. A minConfidenceForCreation threshold of ~0.5 would cut the weakest stuff.,

No minimum importance gate. Same problem. The LLM can stamp importance: 0.1 on junk and it still passes all structural validation.,

Event quality classification is computed but never used. The AnalyzeEvents prompt asks the LLM to classify events as durable/ephemeral/noise/context-dependent — but nothing reads that field. It's dead code. If we actually filtered on it, ephemeral events would never spawn memories.,

The heuristic extraction path is too broad. Phrase cues like "uses", "decided", "prefers" match way too aggressively. Fixed confidence 0.6. This is how "decided not to save a skill" becomes a memory — the heuristic path grabs it by keyword and nobody checks whether it's actually durable.

Session-scoped memories linger 7 days. That's reasonable as a default but generous for things like "update image tag to X" which are stale within hours.,

---

Tuning Recommendations (Practical, Not Architectural),
Tier 1: Quick wins, no code changes,
Enable embeddings. Right now AMM_ENABLE_SEMANTIC=false is the default. Without embeddings, you're running on FTS + entity overlap only — the semantic signal (weight 0.237, the highest single signal) is completely dark. Turn it on, point it at an embedding endpoint, and recall quality jumps significantly. This also lights up the cross-project transfer and dedup-by-embedding paths.  (My comments: In order to do this, we should build the ONNX embedding into a separate binary, so users have the option of having it included or not.  Standard binary can leave it off, built-in-embeddings can have it on by default and use the build in embedding model)

Tighten the ambient recall limit. Currently 5. That's fine, but consider whether 3 would be better for the Hermes plugin injection — less noise in the system prompt means the LLM focuses on the best matches., (My comment.  5 is fine for now)

Tier 2: Small code changes, high impact,
Add a minimum confidence gate at memory creation (~0.5). Reject candidates below this in prepareMemoryCandidate. The lifecycle review can still promote things later if they turn out to be useful.,

Wire up the event quality classification. The LLM already categorizes events — just add a check: if EventQuality == "ephemeral" || EventQuality == "noise", skip memory extraction for that event. The data is already there, it's just not being read.,

Reduce session-trace archive age from 7 days to 2-3 days. Ephemeral action items ("update tag to X") are session-scoped; archiving them faster cuts the noise window.,

Tier 3: Worth doing eventually,
Tighten the heuristic extraction path. Either raise the bar on phrase-cue matching (require 2+ cues instead of 1), or lower the heuristic confidence to 0.4 so those memories lose in ranking to LLM-extracted ones.(2/4),

Add a "durability check" to the extraction prompt. Something like: "Before creating a memory, ask: will this still matter in 30 days? If the answer is 'only if something goes wrong,' skip it.",

Use pgvecto.rs for ANN search. Right now vector search loads ALL embeddings of a kind into Go memory and brute-forces cosine. Fine at 14 memories. Not fine at 14,000. The pgvecto.rs extension is already on the Postgres instance — the code just doesn't use it for indexing., (My comment - for postgres, we should force the use of pgvecto.rs or vectorchord.  If the user is still using sqlite, we prob need to keep the same path we're on)

---

On Your Point About "Occasionally Finding Useful Things",
This is the tension: aggressive filtering means cleaner ambient recall but risks losing the long-tail discoveries. The architecture actually handles this well in theory:

Ambient recall (what the plugin injects) has a minimum score of 0.2 and returns only 5 items. This should be tight — only genuinely relevant stuff.,
Explicit recall (amm_recall tool) casts a wider net — includes raw events, history, 10 results. This is where you find the "not always useful but occasionally gold" items.,
Episodes tie events into narrative threads. Even if individual memories get archived, the episode persists as a searchable unit.,

The problem isn't the recall architecture — it's that low-quality memories are polluting the candidate pool. When you have 14 memories and 5 are noise, that's a 36% noise rate in a tiny pool. Fix the intake quality and the retrieval system works as designed.

---

Product Assessment,
Where it's strong:
12-signal hybrid scoring with learned weights — this is production-grade retrieval,
Entity graph with 2-hop expansion and hub dampening — real graph intelligence, not just tagging,
Episode formation gives narrative structure most systems lack,
The lifecycle pipeline (decay, review, merge, archive) is a proper memory management system, not just append-only storage,
Claims extraction adds a predicate layer for future reasoning,

Where it's incomplete: (3/4)

Embeddings disabled by default — the most powerful signal is off,
Vector search is brute-force in Go, not using the pgvecto.rs index that's sitting right there,
Entity extraction is capitalized-word heuristics, not NER — misses lowercase entities, false positives from sentence starts,
No post-extraction quality gate means the LLM's quality judgment is unaudited,

Bottom line: The engine is built to be a serious product. It's got the right architecture for recall, ranking, and lifecycle management. But right now it's running on maybe 60% of its designed capability — embeddings off, event quality unused, no intake filtering. Turn those knobs and you've got something that genuinely competes with commercial agent memory solutions.
