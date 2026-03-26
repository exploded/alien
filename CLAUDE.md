# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

"Aliens Like Us" — a Go web app that presents yes/no survey questions about hypothetical alien life. Users answer questions sequentially and see aggregate results (% who said yes) from all previous respondents. Backed by SQLite via SQLC. Frontend uses Go `html/template` with HTMX for smooth navigation.

## Build and run

```bash
# Build (Windows)
go build -o alien.exe .

# Build for Linux deployment
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o alien .

# Run (no env vars required for database — uses alien.db file)
./alien.exe
```

Server listens on port **8787**. The SQLite database file `alien.db` is created automatically on first run (empty). Use the migration tool to import existing data from MySQL.

### Migration from MySQL

```bash
# One-time migration from existing MySQL database
export DATASOURCE="user:pass@tcp(127.0.0.1:3306)/alien"
go run ./cmd/migrate/
```

This reads all questions and answers from MySQL and writes them to `alien.db`.

## Tests

```bash
go test ./...           # run all tests
go test -run TestSiteRootGetQuestion  # run a single test
```

Tests use an in-memory SQLite database — no external DB connection needed. Templates must be parseable from `templates/` relative to the working directory.

## SQLC

Queries are in `db/queries.sql`, schema in `db/schema.sql`. After editing either, regenerate:

```bash
sqlc generate
```

Generated code lives in `db/` (do not edit `db/db.go`, `db/models.go`, `db/queries.sql.go`).

## Architecture

- **alien.go** — HTTP handlers, CSRF logic, rate limiter, `main()`. Routes: `/` (question flow), `/intro`, `/about`, `/robots.txt`, plus static file serving for `/css/`, `/images/`, `/static/`.
- **db/** — SQLC-generated database layer (SQLite via `modernc.org/sqlite`).
- **alien_test.go** — handler tests using `httptest` + in-memory SQLite.
- **cmd/migrate/** — one-time MySQL→SQLite migration tool.

### Request flow

`GET /?question=0` → random question (1–59). `GET /?question=N` → show question N. `POST /` with `question=N&answer=yes|no` → record vote, show results for N, then present question N+1 (wraps at 59→1). No `question` param → redirect to `/intro`.

### Database schema (SQLite)

- **question** — `id`, `category`, `question`, `picture`, `short`, `yes` (count), `no` (count). 59 rows, IDs 1–59.
- **answer** — individual vote log with `question` FK, `answer` (1=yes/0=no), `submitter` (IP:port), `submitdate`, `submitteragent`.

Schema is in `db/schema.sql`. Historical MySQL dump is in `backup/alien.sql`.

### Templates

Go `html/template` with base layout pattern:
- `templates/base.html` — shared HTML shell, loads HTMX.
- `templates/question.html` — question page with vote form and results pie chart.
- `templates/intro.html` — landing page.
- `templates/about.html` — about page with response count.

HTMX (`hx-boost="true"` on body) provides smooth page transitions without full reloads.

### Security

- CSRF: cookie-based double-submit token (`csrf_token`), verified with constant-time compare on POST.
- Rate limiting: in-memory per-IP limiter, 30 votes/minute.

### Static assets

- `css/` — `skeleton.css` (CSS framework), `alien.css` (custom styles).
- `images/` — question background images with responsive variants in `images/mobile/` and `images/huge/`.
- `static/` — `htmx.min.js` (HTMX 2.0.4).

## Environment

| Variable | Required | Description |
|---|---|---|
| `PORT` | No | HTTP listen port (default 8787) |
| `LOG_API_KEY` | No | API key for remote log shipping |
