<img src="assets/icon.svg" width="64" height="64" alt="">

# mcp-rag-demo — SiteDocs

Documentation for construction sites. A project Admin uploads the project's documents; site foremen ask Questions and get Answers with exact Citations (document, page, verbatim passage). The same features are available to AI assistants over MCP.

## Try it in 5 minutes

You need [Docker Desktop](https://www.docker.com/products/docker-desktop/) and an [OpenAI API key](https://platform.openai.com/api-keys) (a few questions cost well under a cent).

**1. Start it** — clone the repo and run one command with your key:

```bash
git clone https://github.com/autohn/mcp-rag-demo.git && cd mcp-rag-demo

# macOS / Linux
OPENAI_API_KEY=sk-... docker compose up --build

# Windows PowerShell
$env:OPENAI_API_KEY="sk-..."; docker compose up --build
```

The first build takes a couple of minutes. It's ready when the log shows `listening addr=:8080`.

**2. Ask the CV** — open http://localhost:8080 and click **Open the demo project**. No sign-up: it opens the Questions tab of a demo project holding a sample CV. Ask, for example:

- *What does he do at SIGNAX?*
- *Has he built RAG systems? Where?*
- *Which backend technologies does he know?*

Every claim in the answer has a numbered citation. Click the document name under a citation: the PDF opens on that page with the quoted passage highlighted. Ask something the CV doesn't cover (*What is his salary expectation?*) and you get "not found" instead of a guess.

**3. Try MCP (optional)** — still signed in as the demo user, go to **Access Tokens & MCP**, create a token, and copy the Claude Code / Cursor / Claude Desktop snippet shown there. Then ask your assistant: *"Using sitedocs, summarize Mikhail's AI experience with sources."*

To try uploads and member management, register your own account and create a project — you become its Admin. In the demo project the demo user can ask questions but not change documents.

Stop with `Ctrl+C`; `docker compose down -v` also wipes the data. If port 8080 or 5433 is busy, stop whatever uses it or change the ports in `docker-compose.yml`. Without a key everything except Answers still works (full-text search only); add the key later and restart — existing documents get their embeddings automatically.

## Getting Started

Instead of passing the key on the command line you can keep it in `.env`:

```bash
cp .env.example .env        # set OPENAI_API_KEY
docker compose up --build   # http://localhost:8080
```

Demo accounts:

| User | Password | Role |
|---|---|---|
| `admin@demo.local` | `demo1234` | Admin of the demo project |
| `prorab@demo.local` | `demo1234` | Participant — the **Open the demo project** button signs in as this user |

Set `SEED=false` to skip the demo data and hide the button.

Development (Go 1.27+, Node 22+):

```bash
docker compose up -d postgres                         # Postgres on localhost:5433
cd backend && OPENAI_API_KEY=... go run ./cmd/server  # API on :8080
cd web && npm install && npm run dev                  # UI on :5173, proxies /api and /mcp
cd backend && go test ./...
```

## How It Works

**Upload and indexing**
1. `POST /api/projects/:id/documents` (or MCP `upload_document`) stores the file at `DATA_DIR/files/<project>/<document>`; the document starts in status `processing`.
2. A background worker extracts text: PDF page by page, DOCX/TXT/MD as conventional blocks.
3. Text is split into ~800-token chunks with ~100-token overlap, never crossing a page boundary.
4. Each chunk gets an embedding (`text-embedding-3-small`) and a `russian` `tsvector`; status becomes `ready` (or `failed` with the error). A document with no text is shown as "No text".

**Question → Answer**
1. Membership check — a single `authorize` function shared by REST and MCP.
2. Hybrid search in Postgres: pgvector (cosine) + full-text, fused with Reciprocal Rank Fusion, top 8, always scoped to the project (and to a folder subtree when one is chosen).
3. The model sees only the retrieved fragments and replies with strict-schema JSON `{answer, found, citations:[{fragment, quote}]}`.
4. The backend drops citations whose quote is not found verbatim (after normalization) in the fragment, replaces quotes with the exact source text, and renumbers the `[n]` markers. No citations left → "not found".
5. The Question, Answer and copies of the Citations are saved to the user's personal history. If a document is deleted later, its citations keep the quote and are marked "document deleted".

**MCP**
1. `/mcp`, Streamable HTTP (stateless), header `Authorization: Bearer <Access Token>`.
2. The token is checked on every request; tools call the same `app` methods as REST, so the assistant has exactly the user's rights.
3. Connection instructions for Claude Desktop, Cursor and Claude Code are on the "Access Tokens & MCP" page. ChatGPT and claude.ai web connectors require OAuth, which this demo does not implement.

## Project Structure

```
backend/
  cmd/server/            entry point: DB, indexer, seed, HTTP
  internal/app/          all business logic: authorize, projects, files, indexer, RAG
  internal/httpapi/      REST API + static frontend
  internal/mcpserver/    MCP tools
  internal/extract/      PDF / DOCX / TXT / MD → pages
  internal/chunk/        chunking
  internal/llm/          OpenAI client (embeddings, JSON-schema chat)
  internal/db/           connection pool and SQL migrations
  internal/seed/         demo project and documents
web/src/
  pages/                 screens: sign-in, projects, files, questions, members, document, tokens
  components/            shell, dialogs, UI primitives
  lib/api.ts             typed REST client
```

## Customizing

| What | Where |
|---|---|
| Models, key, upload limit, addresses | env: `OPENAI_*`, `MAX_UPLOAD_MB`, `DATABASE_URL`, `DATA_DIR`, `HTTP_ADDR` ([config.go](backend/internal/config/config.go)) |
| Prompt and answer schema | [rag.go](backend/internal/app/rag.go) — `systemPrompt`, `answerSchema` |
| Top-K and RRF parameters | [rag.go](backend/internal/app/rag.go) — `TopK`, `rrfK`, `candidatePool` |
| Chunk size | [chunk.go](backend/internal/chunk/chunk.go) |
| MCP tools and descriptions | [mcpserver.go](backend/internal/mcpserver/mcpserver.go) |
| Database schema | [backend/internal/db/migrations/](backend/internal/db/migrations/) |
| Demo project and button | [backend/internal/seed/](backend/internal/seed/) |
