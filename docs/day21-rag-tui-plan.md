# Day 21 RAG TUI Plan

## 1. Goal

Day 21 asks for a local document indexing pipeline:

- collect a document corpus of at least 20-30 pages or equivalent code;
- split documents into chunks;
- generate embeddings;
- save a local index in FAISS, SQLite, or JSON;
- attach chunk metadata: `source`, `title/file`, `section`, `chunk_id`;
- implement and compare two chunking strategies:
  - fixed-size chunks;
  - structure-based chunks by headings, sections, or files.

For `coding_writer`, the product goal is stricter than a standalone script:

- the user-facing flow must happen inside the real `cw` TUI;
- CLI-only commands may exist later as diagnostics, but they are not the primary UX;
- RAG indexes the current workspace, not hardcoded `coding_writer` source;
- when `cw` is launched from this repo, using the project itself as the corpus is valid dogfooding;
- ordinary chat must keep working when RAG setup, indexing, or retrieval is unavailable.

The v1 user story:

```text
User opens a workspace with cw.
User sets up local embeddings from TUI.
User indexes workspace docs/code from TUI.
User sees chunk metadata, embeddings, saved JSONL index, and strategy comparison.
User searches the index from TUI.
Normal chat automatically receives visible RAG context when the index is available.
```

## 2. Core Decisions

### 2.1. Product Surface

All visible product behavior is TUI-first.

Required slash commands:

```text
/rag status
/rag setup
/rag index
/rag report
/rag search <query>
/rag chunks
/rag reset index
/rag reset model
/rag delete
/rag on
/rag off
```

Optional later commands:

```text
/rag inspect chunk <chunk_id>
/rag search --strategy fixed <query>
/rag search --strategy structural <query>
```

No demo-specific product command should be added. In particular:

- do not add `/rag reset demo`;
- do not make demo-specific storage paths or labels part of the product;
- manual tests can use product commands only.

### 2.2. Local Embedding Backend

Use Ollama as the local embedding provider.

Default model:

```text
bge-m3
```

Reasons:

- multilingual, suitable for Russian lecture notes and English code/docs;
- strong enough for repo-scale semantic retrieval;
- local and private;
- small enough for the user's Mac mini M4 with 16 GB RAM;
- embedding-only, so it is much lighter than running a local generative LLM.

Fallback model:

```text
nomic-embed-text
```

Use it only as an alternate supported model, not the default.

Test backend:

```text
deterministic fake embedder
```

The fake embedder is only for tests. It must not be presented as a real TUI fallback for semantic retrieval.

### 2.3. Generative Model Boundary

RAG embeddings and chat generation are separate.

```text
OpenRouter/chat model -> generates answers and plans.
Ollama/bge-m3 -> embeds chunks and user queries for retrieval.
```

`/model` continues to control the generative model.

RAG config controls only:

- embedding provider;
- embedding model;
- RAG enabled/disabled state;
- index metadata.

### 2.4. Index Storage

Use JSON/JSONL for v1.

Reasons:

- Day 21 explicitly allows JSON;
- the first corpus is repo-scale: hundreds or a few thousand chunks, not millions;
- linear cosine scan over in-memory vectors is acceptable at this size;
- JSONL is easy to inspect during demo;
- the repo already has safe JSON/JSONL storage helpers;
- no new SQLite/FAISS dependency is needed for the first TUI implementation.

Expected path:

```text
.codingwriter/rag/<workspace-id>/
  manifest.json
  chunks.fixed.jsonl
  chunks.structural.jsonl
  embeddings.fixed.jsonl
  embeddings.structural.jsonl
  report.json
```

The directory is already covered by ignored local storage patterns.

When JSONL stops being enough:

- tens or hundreds of thousands of chunks;
- frequent incremental updates;
- metadata filtering needs indexes;
- concurrent access becomes important;
- approximate nearest-neighbor search becomes necessary.

Then migrate to SQLite metadata plus vector blobs, or a real vector index.

## 3. TUI UX

### 3.1. `/rag status`

Shows the authoritative local RAG state.

Example before setup:

```text
RAG: off
Ollama: missing
model: bge-m3 missing
index: missing
next: /rag setup
```

Example after setup but before indexing:

```text
RAG: on
Ollama: reachable
model: bge-m3 ready
index: missing
next: /rag index
```

Example after indexing:

```text
RAG: on
Ollama: reachable
model: bge-m3 ready
index: ready
workspace: /Users/nikita/code/coding_writer
documents: 87
fixed chunks: 421
structural chunks: 384
embeddings: ready
default strategy: structural
last indexed: 2026-07-02 12:00
```

Status must also show stale/mismatch states:

```text
index: stale
reason: 12 workspace files changed after indexing
next: /rag index
```

```text
index: unusable
reason: embedding dimension/model mismatch
next: /rag index
```

### 3.2. `/rag setup`

Guided setup installs and verifies local embedding support.

The command first renders an approval screen:

```text
Install local embedding backend?

Will run:
- brew install ollama
- ollama pull bge-m3
- embedding smoke test through http://127.0.0.1:11434/api/embed

Network: required
Disk: about 1.2 GB for bge-m3

[Approve] [Cancel] [Manual instructions]
```

No install or download runs without explicit user approval.

After approval, timeline shows each step:

```text
rag setup started
homebrew found
installing ollama
starting/verifying ollama
pulling bge-m3
embedding smoke test passed
rag setup complete
```

Fallback behavior:

- Homebrew missing: show manual installation instructions, do not guess another installer;
- `brew install ollama` fails: keep chat usable, mark setup failed;
- Ollama installed but daemon unavailable: show command/hint to start Ollama;
- model pull fails: RAG remains not ready;
- smoke test fails: RAG remains not ready with concrete error.

### 3.3. `/rag index`

Builds the full Day 21 pipeline inside TUI.

Visible phases:

```text
scanning workspace
filtering files
extracting text
chunking fixed
chunking structural
generating embeddings fixed
generating embeddings structural
saving JSONL index
writing comparison report
index ready
```

The TUI timeline should show progress counts:

```text
accepted files: 87
ignored files: 31
fixed chunks: 421
structural chunks: 384
embeddings generated: 805
store: .codingwriter/rag/<workspace-id>/
```

The operation must be bounded and cancellable from the TUI if possible.

Hard failures:

- no workspace files accepted;
- Ollama unavailable and no embeddings can be generated;
- embedding dimension changes mid-run;
- unsafe storage path;
- JSONL write failure.

Soft warnings:

- some files too large and skipped;
- binary files skipped;
- unsupported extensions skipped;
- stale previous index replaced;
- lexical fallback unavailable until chunks are saved.

### 3.4. `/rag report`

Shows Day 21 comparison result.

Example:

```text
Chunking comparison

fixed:
  chunks: 421
  avg tokens: 680
  min tokens: 120
  max tokens: 760
  overlap: 100
  files covered: 87

structural:
  chunks: 384
  avg tokens: 735
  min tokens: 80
  max tokens: 820
  sections: 211
  files covered: 87

default search strategy: structural

Observation:
  structural keeps headings/functions together
  fixed gives more uniform chunk sizes
```

The comparison should be computed from saved index data, not from hardcoded text.

### 3.5. `/rag chunks`

Shows sample chunk metadata.

Example:

```text
Chunks sample

chunk_id: structural:README.md:000001
strategy: structural
source: workspace
title/file: README.md
section: Архитектура
path: README.md
lines: 1-64
embedding: present

chunk_id: fixed:internal/process/controller.go:000044
strategy: fixed
source: workspace
title/file: controller.go
section: lines 240-318
path: internal/process/controller.go
lines: 240-318
embedding: present
```

This command exists mostly to make the Day 21 metadata requirement visible in TUI.

### 3.6. `/rag search <query>`

Runs retrieval against the local index.

Default behavior:

- embed query with Ollama `bge-m3`;
- search the structural strategy by default;
- rank chunks by cosine similarity;
- show top results with source metadata and score.

Example:

```text
/rag search trusted verification
```

TUI output:

```text
RAG search
mode: semantic
model: bge-m3
strategy: structural

1. README.md / Проверка / chunk structural:README.md:000018 / score 0.84
2. internal/cli/root.go / runTrustedVerification / chunk structural:internal/cli/root.go:000031 / score 0.79
3. docs/manual-testing-demo.md / Day 15 / chunk structural:docs/manual-testing-demo.md:000011 / score 0.74
```

If semantic retrieval is unavailable but chunk text exists:

```text
RAG search
mode: lexical_fallback
reason: embedding model missing
```

Lexical fallback should be clearly labeled and must not pretend to be semantic search.

### 3.7. Normal Chat With RAG Context

When RAG is on and the index is ready, ordinary user input triggers retrieval before the model call.

Example user input:

```text
Объясни, как в этом проекте работает trusted verification. Используй локальный контекст.
```

Timeline:

```text
user question
rag retrieval: semantic, bge-m3, chunks=4, duration=182ms
model call started
assistant answer
```

Sidebar:

```text
RAG Context
- README.md / Проверка / chunk structural:README.md:000018
- internal/cli/root.go / runTrustedVerification / chunk structural:internal/cli/root.go:000031
- docs/manual-testing-demo.md / Day 15 / chunk structural:docs/manual-testing-demo.md:000011
```

Prompt injection boundary:

- retrieved chunks are untrusted workspace context;
- chunks are wrapped in a dedicated context block;
- chunks must not become system instructions;
- answer should cite files/sections/chunk ids where relevant.

### 3.8. `/rag on` and `/rag off`

`/rag off` disables automatic retrieval for normal chat.

Manual commands still work:

```text
/rag status
/rag search ...
/rag report
```

`/rag on` enables automatic retrieval again if the index and model are usable.

If not usable:

```text
RAG cannot turn on
reason: model missing
next: /rag setup
```

## 4. Workspace Corpus

### 4.1. What Gets Indexed

The v1 corpus is current workspace docs and code.

For `coding_writer`, this includes:

- `README.md`;
- `RAG.md`;
- `day21.md`;
- `docs/*.md`;
- `internal/**/*.go`;
- `cmd/**/*.go`;
- other supported text/code files if accepted by scanner.

This easily exceeds the 20-30 page equivalent requirement.

### 4.2. What Is Ignored

Always ignore:

- `.git/`;
- `.codingwriter/`;
- `.assistant/`;
- `.cache/`;
- `.kilo/plans/`;
- `artifacts/`;
- `manual_scratch/`;
- `tmp/`;
- `temp/`;
- `build/`;
- `dist/`;
- `coverage/`;
- binary files;
- files above the configured size limit;
- known secret/env files.

The scanner should reuse `.gitignore`-like intent where practical, but v1 can start with explicit denylist and extension allowlist.

### 4.3. Source Semantics

`source` in v1:

```text
workspace
```

Future values:

```text
pdf
url
manual_upload
external_docs
```

Do not hardcode `source=coding_writer`.

## 5. Chunking

### 5.1. Common Chunk Record

Each chunk stores:

```json
{
  "chunk_id": "structural:README.md:000001",
  "strategy": "structural",
  "source": "workspace",
  "path": "README.md",
  "title": "README.md",
  "section": "Архитектура",
  "start_line": 1,
  "end_line": 64,
  "text": "...",
  "token_count_estimate": 642,
  "content_sha256": "...",
  "created_at": "2026-07-02T12:00:00Z"
}
```

`chunk_id` must be stable enough for demo and citation.

Recommended format:

```text
<strategy>:<relative-path>:<zero-padded-sequence>
```

If paths contain spaces or unusual characters, store raw path separately and generate a safe ID component from a path hash.

### 5.2. Fixed Chunking

Fixed chunking splits one file at a time.

Parameters:

```text
target size: 700 token estimate
overlap: 100 token estimate
max chunk size: 850 token estimate
```

Rules:

- never mix two source files in one chunk;
- prefer line/paragraph boundaries near the target size;
- overlap carries the tail of the previous chunk into the next chunk;
- `section` can be the nearest heading/function if known, else `lines X-Y`;
- very small files become one chunk.

Purpose:

- uniform chunk size;
- stable baseline;
- easy comparison with structural strategy.

Known tradeoff:

- can split a semantic section or function body awkwardly.

### 5.3. Structural Chunking For Markdown

Markdown structural chunking splits by headings.

Rules:

- detect headings `#`, `##`, `###`, etc.;
- section name is the current heading text;
- include heading text in the chunk;
- keep a section together when it fits;
- if a section is too large, split inside it using fixed-size fallback with overlap;
- if sections are tiny, optionally merge adjacent small sections under the same parent until a reasonable size.

Example:

```text
README.md / Архитектура
README.md / Состояние задачи
docs/tui-frd.md / Functional Requirements
```

### 5.4. Structural Chunking For Go

Use Go stdlib parser where possible:

```text
go/parser
go/ast
token.FileSet
```

Structural sections:

- top-level `type`;
- top-level `func`;
- grouped `const` or `var` blocks;
- fallback file-level chunk when parsing fails.

Example sections:

```text
internal/process/controller.go / RunExchange
internal/tui/model.go / Model
internal/cli/root.go / runChatExchangeLocked
```

If a declaration is too large, split within the declaration using fixed-size fallback while keeping `section` as the declaration name.

### 5.5. Structural Chunking For Plain Text

Fallback:

- split by blank-line paragraphs;
- group paragraphs up to target size;
- split oversized paragraph blocks using fixed-size fallback.

## 6. Embeddings

### 6.1. Ollama Client

Use Ollama HTTP API:

```text
POST http://127.0.0.1:11434/api/embed
```

Request:

```json
{
  "model": "bge-m3",
  "input": ["chunk text 1", "chunk text 2"]
}
```

Implementation notes:

- support batching;
- apply context timeout;
- limit request size;
- normalize vectors before saving or normalize at search time;
- persist vector dimension in manifest;
- fail index if dimension is inconsistent.

### 6.2. Embedding Record

Each embedding row stores:

```json
{
  "chunk_id": "structural:README.md:000001",
  "model": "bge-m3",
  "provider": "ollama",
  "dimension": 1024,
  "vector": [0.0123, -0.0456],
  "content_sha256": "...",
  "created_at": "2026-07-02T12:00:00Z"
}
```

Use float values in JSON for inspectability. For v1 this is acceptable.

### 6.3. Search

Default search:

- embed the query;
- load structural chunks and embeddings;
- cosine similarity against all vectors;
- return top `k`, default 5;
- include metadata and score.

Fallback lexical search:

- use saved chunk text;
- tokenize query and chunk text;
- score by term overlap plus simple normalization;
- label results as `lexical_fallback`.

Lexical fallback exists so `cw` remains useful when the embedding model is missing. It is not an equivalent semantic search replacement.

## 7. Saved Index Layout

### 7.1. Manifest

`manifest.json`:

```json
{
  "version": 1,
  "workspace_root": "/Users/nikita/code/coding_writer",
  "workspace_id": "sha256...",
  "embedding_provider": "ollama",
  "embedding_model": "bge-m3",
  "embedding_dimension": 1024,
  "default_strategy": "structural",
  "indexed_at": "2026-07-02T12:00:00Z",
  "documents": 87,
  "strategies": {
    "fixed": {
      "chunks": 421,
      "path": "chunks.fixed.jsonl",
      "embeddings_path": "embeddings.fixed.jsonl"
    },
    "structural": {
      "chunks": 384,
      "path": "chunks.structural.jsonl",
      "embeddings_path": "embeddings.structural.jsonl"
    }
  },
  "files": [
    {
      "path": "README.md",
      "sha256": "...",
      "size_bytes": 28672,
      "mtime": "2026-07-02T12:00:00Z"
    }
  ]
}
```

### 7.2. Report

`report.json`:

```json
{
  "corpus": {
    "documents": 87,
    "text_bytes": 1327104,
    "page_equivalent_estimate": 120
  },
  "fixed": {
    "chunks": 421,
    "avg_tokens": 680,
    "min_tokens": 120,
    "max_tokens": 760,
    "overlap_tokens": 100,
    "files_covered": 87
  },
  "structural": {
    "chunks": 384,
    "avg_tokens": 735,
    "min_tokens": 80,
    "max_tokens": 820,
    "sections": 211,
    "files_covered": 87
  },
  "summary": [
    "structural keeps headings and functions together",
    "fixed produces more uniform chunk sizes"
  ]
}
```

## 8. Reset And Delete UX

### 8.1. `/rag reset index`

Deletes only the current workspace index.

Does not delete:

- Ollama;
- embedding model;
- chat/task/memory;
- repo files.

Approval screen:

```text
Delete RAG index for this workspace?

Will remove:
- .codingwriter/rag/<workspace-id>/

Will keep:
- Ollama
- bge-m3
- cw chat/task/memory

[Approve] [Cancel]
```

### 8.2. `/rag reset model`

Deletes only the configured embedding model:

```text
ollama rm bge-m3
```

Does not delete:

- Ollama app/package;
- workspace index;
- chat/task/memory.

After this:

- semantic search is unavailable;
- index may remain on disk but is unusable for semantic retrieval until model is restored;
- lexical fallback may still work from chunk text.

Approval screen:

```text
Remove local embedding model bge-m3?

This may affect other apps using Ollama.
The current index will need the same model again or a rebuild.

[Approve] [Cancel]
```

### 8.3. `/rag delete`

Deletes the local RAG stack.

Intended behavior:

- remove current workspace RAG index;
- remove cw RAG config/state;
- remove embedding model through `ollama rm bge-m3` if Ollama is available;
- uninstall Ollama through `brew uninstall ollama` if it was installed through Homebrew.

Do not remove manually installed Ollama from unknown paths.

Typed confirmation is required:

```text
Delete local RAG stack?

Will remove:
- this workspace RAG index
- cw RAG settings
- Ollama model: bge-m3
- Ollama Homebrew package, if installed

This may affect other apps using Ollama.

Type DELETE RAG to confirm:
```

The exact required text:

```text
DELETE RAG
```

If Ollama was not installed through Homebrew:

```text
Ollama appears to be installed outside Homebrew.
cw removed index/config/model if possible.
Manual Ollama uninstall required.
```

## 9. Fallback Policy

Ordinary chat must not fail only because RAG is unavailable.

### 9.1. Missing Ollama

State:

```text
RAG skipped: Ollama unavailable
next: /rag setup
```

Behavior:

- normal chat continues without RAG context;
- `/rag status` explains setup;
- `/rag search` uses lexical fallback only if chunks are already saved.

### 9.2. Missing Model

State:

```text
RAG skipped: embedding model missing
next: /rag setup
```

Behavior:

- normal chat continues;
- `/rag search` uses lexical fallback if index chunks exist;
- `/rag index` cannot generate semantic embeddings until model is available.

### 9.3. Missing Index

State:

```text
RAG skipped: index missing
next: /rag index
```

Behavior:

- normal chat continues;
- `/rag search` returns an actionable message.

### 9.4. Stale Index

State:

```text
RAG context: stale
changed files: 12
next: /rag index
```

Behavior:

- v1 may still use stale index with visible warning;
- later versions can auto-update incrementally.

### 9.5. Timeout

Query embedding timeout:

- use short timeout for automatic chat retrieval, for example 2-5 seconds;
- if timed out, skip semantic retrieval or use lexical fallback;
- never hang the TUI indefinitely.

Indexing timeout:

- indexing is a user-started long operation;
- show progress;
- allow cancellation later if practical.

## 10. Integration Points

### 10.1. Backend Flow

Current TUI path:

```text
tui.Model.runExchange
  -> tui.Backend.Exchange
    -> cli.ChatBackend.Exchange
      -> runChatExchange
        -> runChatExchangeLocked
          -> ProcessController.RunExchange
```

RAG retrieval should happen before `ProcessController.RunExchange`, inside backend/control-plane code, not directly inside Bubble Tea rendering code.

Target flow:

```text
ChatBackend.Exchange
  -> preflight
  -> maybe retrieve RAG context
  -> pass RAG context into prompt build input
  -> ProcessController.RunExchange
  -> return ChatResponse with RAG context summary
  -> TUI renders timeline/sidebar RAG Context
```

### 10.2. Prompt Builder

Add a RAG context block to prompt construction.

The block must be explicit untrusted context:

```xml
<context_block id="rag.workspace" type="retrieved_context" source="rag_index" trust="untrusted">
...
</context_block>
```

Each chunk entry should include:

- chunk id;
- path;
- section;
- score;
- text.

The model should be instructed to cite chunk metadata when using retrieved information.

### 10.3. TUI Data Structures

Extend chat response shape with a RAG context summary:

```text
mode: semantic | lexical_fallback | skipped | off
strategy: structural | fixed
model: bge-m3
chunks: []
warning: optional
duration_ms: optional
```

TUI renders:

- timeline event: `rag retrieval`;
- sidebar section: `RAG Context`;
- warnings if skipped/fallback/stale.

### 10.4. Slash Commands

Add `/rag` to slash command catalog and handler.

Command family:

```text
/rag
/rag status
/rag setup
/rag index
/rag report
/rag chunks
/rag search <query>
/rag reset index
/rag reset model
/rag delete
/rag on
/rag off
```

Slash command operations that call external processes or HTTP must have bounded context timeouts.

## 11. Manual TUI Demo Test

The manual test must run fully inside the real `cw` TUI.

### 11.1. Preparation Outside TUI

Only build/startup commands are outside TUI:

```bash
cd /Users/nikita/code/coding_writer
scripts/build-cw.sh
export PATH="$PWD/.codingwriter/bin:$PATH"
cw
```

No shell command should be used to prove RAG behavior during the demo.

### 11.2. Clean State

Inside TUI:

```text
/rag delete
```

Approve with:

```text
DELETE RAG
```

Then:

```text
/rag status
```

Visible proof:

- RAG off or not configured;
- index missing;
- model missing;
- setup suggested.

### 11.3. Setup

Inside TUI:

```text
/rag setup
```

Approve setup.

Visible proof:

- Ollama installation/check step;
- model pull step;
- embedding smoke test passed;
- `/rag status` shows model ready.

### 11.4. Indexing

Inside TUI:

```text
/rag index
```

Visible proof:

- scanning workspace;
- accepted and ignored file counts;
- corpus page equivalent is at least 20-30 pages;
- fixed chunking completed;
- structural chunking completed;
- embeddings generated;
- JSONL index saved.

### 11.5. Metadata

Inside TUI:

```text
/rag chunks
```

Visible proof for at least one chunk:

- `source`;
- `title/file`;
- `section`;
- `chunk_id`;
- strategy;
- path;
- line range;
- embedding present.

### 11.6. Strategy Comparison

Inside TUI:

```text
/rag report
```

Visible proof:

- fixed strategy statistics;
- structural strategy statistics;
- comparison summary;
- default strategy.

### 11.7. Search

Inside TUI:

```text
/rag search trusted verification
```

Visible proof:

- semantic mode;
- model `bge-m3`;
- ranked results;
- files and sections shown;
- scores shown.

### 11.8. Normal Chat Uses RAG

Inside TUI, ordinary input:

```text
Объясни, как в этом проекте работает trusted verification. Используй локальный контекст.
```

Visible proof:

- timeline shows RAG retrieval before model call;
- sidebar shows RAG Context with chunks;
- answer cites files/sections/chunk ids;
- chat remains normal user flow, not a debug command.

### 11.9. Fallback

Inside TUI:

```text
/rag reset model
```

Approve.

Then:

```text
/rag search trusted verification
```

Visible proof:

- semantic search unavailable;
- lexical fallback used if chunks remain;
- normal chat still works.

## 12. Implementation Phases

### Phase 1. RAG Core

Add internal RAG package with:

- workspace scanner;
- ignore rules;
- document model;
- fixed chunker;
- structural chunker for Markdown;
- structural chunker for Go using stdlib parser;
- JSONL store;
- report generation;
- deterministic fake embedder for tests.

Tests:

- fixed chunking preserves source file boundary;
- fixed overlap exists;
- Markdown headings become sections;
- Go functions/types become sections;
- metadata fields are always set;
- report compares both strategies.

### Phase 2. Ollama Embeddings

Add:

- Ollama client;
- setup/status detector;
- model presence check;
- embedding smoke test;
- vector normalization/cosine search;
- timeout handling.

Tests:

- fake embedder indexing;
- dimension mismatch rejected;
- missing model status maps to actionable warning;
- search ranks deterministic vectors.

### Phase 3. TUI Slash Commands

Add `/rag` command family through existing TUI backend/slash path.

Tests:

- slash catalog includes `/rag`;
- `/rag status` renders missing/setup-ready/index-ready states;
- `/rag report` renders saved comparison;
- `/rag chunks` renders metadata;
- `/rag search` renders semantic/fallback modes.

### Phase 4. Prompt Integration

Add automatic retrieval for normal chat.

Tests:

- when RAG is ready, prompt includes retrieved context block;
- when RAG is off, prompt does not include retrieved context;
- when RAG fails, chat continues with warning;
- retrieved chunks are escaped as untrusted context.

### Phase 5. Manual TUI Proof

Run full manual scenario in real `cw` TUI.

Evidence to capture:

- setup status;
- index completed;
- metadata shown;
- comparison report shown;
- semantic search results;
- ordinary chat with RAG Context;
- fallback after model reset.

## 13. Acceptance Criteria

Day 21 is complete when the TUI demo proves:

- corpus has at least 20-30 pages equivalent;
- documents are chunked;
- embeddings are generated by a local model;
- index is saved as JSON/JSONL;
- chunks include `source`, `title/file`, `section`, `chunk_id`;
- fixed chunking exists;
- structural chunking exists;
- fixed vs structural comparison is visible;
- local search uses embeddings;
- normal chat can use retrieved RAG context;
- fallback states are visible and ordinary chat remains usable.

Product acceptance is complete when:

- RAG is workspace-based, not hardcoded to this repo;
- setup/delete are explicit and confirmed;
- TUI does not hang on external commands;
- semantic retrieval never silently degrades without UI disclosure;
- deleted/missing model/index states are recoverable through TUI.
