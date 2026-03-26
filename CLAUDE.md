# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

"Aliens Like Us" — a Go web app that presents yes/no survey questions about hypothetical alien life. Users answer questions sequentially and see aggregate results (% who said yes) from all previous respondents. Backed by a MySQL/MariaDB database.

## Build and run

```bash
# Build (Windows)
go build -o alien.exe .

# Build for Linux deployment
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o alien .

# Run (requires DATASOURCE env var)
# On Windows, source alien-env or set manually:
export DATASOURCE="user:pass@tcp(127.0.0.1:3306)/alien"
./alien.exe
```

Server listens on port **8787**.

## Tests

```bash
go test ./...           # run all tests
go test -run TestSiteRootGetQuestion  # run a single test
```

Tests use `go-sqlmock` to mock the MySQL database — no real DB connection needed. Templates must be parseable from `templates/` relative to the working directory.

## Architecture

Single-package `main` app with three Go files:
- **alien.go** — HTTP handlers, CSRF logic, rate limiter, `main()`. Routes: `/` (question flow), `/intro`, `/about`, `/robots.txt`, plus static file serving for `/css/`, `/images/`, `/static/`.
- **db.go** — MySQL connection init via `database/sql` + `go-sql-driver/mysql`. Exposes package-level `db *sql.DB`.
- **alien_test.go** — handler tests using `httptest` + `sqlmock`.

### Request flow

`GET /?question=0` → random question (1–59). `GET /?question=N` → show question N. `POST /` with `question=N&answer=yes|no` → record vote, show results for N, then present question N+1 (wraps at 59→1). No `question` param → redirect to `/intro`.

### Database schema (MySQL/MariaDB)

- **question** — `id`, `category`, `question`, `picture`, `short`, `yes` (count), `no` (count). 59 rows, IDs 1–59.
- **answer** — individual vote log with `question` FK, `answer` (1=yes/0=no), `submitter` (IP:port), `submitdate`, `submitteragent`.

Schema dump is in `backup/alien.sql`.

### Security

- CSRF: cookie-based double-submit token (`csrf_token`), verified with constant-time compare on POST.
- Rate limiting: in-memory per-IP limiter, 30 votes/minute.

### Static assets

- `css/` — `skeleton.css` (CSS framework), `alien.css` (custom styles).
- `images/` — question background images with responsive variants in `images/mobile/` and `images/huge/`.
- `templates/` — Go `html/template` files: `index.html` (question page), `intro.html` (landing), `about.html`.

## Environment

| Variable | Required | Description |
|---|---|---|
| `DATASOURCE` | Yes | MySQL DSN, e.g. `user:pass@tcp(host:3306)/alien` |
