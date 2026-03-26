package main

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"alien/db"

	_ "modernc.org/sqlite"
)

func setupTest(t *testing.T) {
	t.Helper()

	testDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testDB.Close() })

	if _, err := testDB.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}

	queries = db.New(testDB)

	ctx := context.Background()
	for _, q := range []struct {
		id       int64
		category string
		question string
		picture  string
		short    string
	}{
		{1, "cat", "Question 1 text", "pic1.jpg", "Short1"},
		{2, "cat", "Question 2 text", "pic2.jpg", "Short2"},
		{5, "cat", "Question 5 text", "pic5.jpg", "Short5"},
		{59, "cat", "Question 59 text", "pic59.jpg", "Short59"},
	} {
		testDB.ExecContext(ctx,
			"INSERT INTO question (id, category, question, picture, yes, no, short) VALUES (?, ?, ?, ?, 0, 0, ?)",
			q.id, q.category, q.question, q.picture, q.short)
	}

	base := filepath.Join("templates", "base.html")
	tmplQuestion = template.Must(template.ParseFiles(base, filepath.Join("templates", "question.html")))
	tmplIntro = template.Must(template.ParseFiles(base, filepath.Join("templates", "intro.html")))
	tmplAbout = template.Must(template.ParseFiles(base, filepath.Join("templates", "about.html")))
}

func TestSiteRootGetQuestion(t *testing.T) {
	setupTest(t)

	req := httptest.NewRequest(http.MethodGet, "/?question=5", nil)
	rr := httptest.NewRecorder()

	siteroot(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	if !strings.Contains(rr.Body.String(), "Question 5 text") {
		t.Fatalf("expected body to include question text")
	}

	if len(rr.Result().Cookies()) == 0 {
		t.Fatalf("expected csrf cookie to be set")
	}
}

func TestSiteRootPostVoteYes(t *testing.T) {
	setupTest(t)

	form := url.Values{}
	form.Set("question", "1")
	form.Set("answer", "yes")
	form.Set("csrf_token", "testtoken")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test-agent")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "testtoken"})
	req.RemoteAddr = "127.0.0.1:1234"

	rr := httptest.NewRecorder()

	siteroot(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), "Short1") {
		t.Fatalf("expected body to include previous question short name")
	}

	// Verify vote was recorded in database
	q, err := queries.GetQuestion(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if q.Yes != 1 {
		t.Fatalf("expected yes count 1, got %d", q.Yes)
	}
}

func TestSiteRootPostMissingCSRF(t *testing.T) {
	setupTest(t)

	form := url.Values{}
	form.Set("question", "1")
	form.Set("answer", "yes")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()

	siteroot(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
}

func TestSiteRootGetRedirectsToIntro(t *testing.T) {
	setupTest(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	siteroot(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected status 302, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/intro" {
		t.Fatalf("expected redirect to /intro, got %s", loc)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(2, 0)

	if !rl.Allow("ip1") {
		t.Errorf("expected first request to be allowed")
	}
	if !rl.Allow("ip1") {
		t.Errorf("expected second request to be allowed")
	}
	if rl.Allow("ip1") {
		t.Errorf("expected third request to be rejected")
	}

	if !rl.Allow("ip2") {
		t.Errorf("expected request from other ip to be allowed")
	}
}
