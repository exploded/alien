package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSiteRootGetQuestion(t *testing.T) {
	// Initialize templates for testing
	var err error
	templates, err = template.ParseGlob("templates/*.html")
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}

	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer mockDB.Close()
	db = mockDB

	mock.ExpectQuery("SELECT category, question, picture, short FROM question WHERE id = \\?").
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"category", "question", "picture", "short"}).
			AddRow("cat", "Question text", "pic.jpg", "Short text"))

	req := httptest.NewRequest(http.MethodGet, "/?question=5", nil)
	rr := httptest.NewRecorder()

	siteroot(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	if !strings.Contains(rr.Body.String(), "Question text") {
		t.Fatalf("expected body to include question text")
	}

	if len(rr.Result().Cookies()) == 0 {
		t.Fatalf("expected csrf cookie to be set")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet db expectations: %v", err)
	}
}

func TestSiteRootPostVoteYes(t *testing.T) {
	// Initialize templates for testing
	var err error
	templates, err = template.ParseGlob("templates/*.html")
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}

	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer mockDB.Close()
	db = mockDB

	mock.ExpectExec("UPDATE question SET yes = yes \\+ 1 WHERE id=\\?").
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec("INSERT INTO answer \\(").
		WithArgs(int64(1), 1, "127.0.0.1:1234", "test-agent").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery("SELECT short, yes, no FROM question WHERE id = \\?").
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"short", "yes", "no"}).AddRow("Prev", 3, 1))

	mock.ExpectQuery("SELECT category, question, picture, short FROM question WHERE id = \\?").
		WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"category", "question", "picture", "short"}).
			AddRow("cat", "Next question", "pic2.jpg", "Short 2"))

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
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	if !strings.Contains(rr.Body.String(), "Prev") {
		t.Fatalf("expected body to include previous stats")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet db expectations: %v", err)
	}
}

func TestSiteRootPostMissingCSRF(t *testing.T) {
	// Initialize templates for testing
	var err error
	templates, err = template.ParseGlob("templates/*.html")
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}

	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer mockDB.Close()
	db = mockDB

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

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet db expectations: %v", err)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(2, 0) // Should allow 2 requests per window

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
