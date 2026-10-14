package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const testOrigin = "http://test.local"

type env struct {
	t     *testing.T
	a     *app
	srv   http.Handler
	admin *http.Cookie
}

func setup(t *testing.T) *env {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://thaivote:thaivote@localhost:55432/thaivote_test?sslmode=disable"
	}
	pool, err := openDB(context.Background(), url)
	if err != nil {
		t.Skipf("no test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(), `TRUNCATE audit_log, comment_hidden, ballot_choices, ballots,
		participations, poll_options, polls, sessions, voters, admins CASCADE`); err != nil {
		t.Fatal(err)
	}
	a := &app{
		cfg:        config{Pepper: []byte("test-pepper"), Origin: testOrigin, MockThaiID: true},
		db:         pool,
		loginLimit: newLimiter(time.Millisecond, 1000),
		voteLimit:  newLimiter(time.Millisecond, 1000),
	}
	hash, _ := hashPassword("correct horse battery")
	if _, err := pool.Exec(context.Background(), `INSERT INTO admins (username, password_hash, display_name) VALUES ('root',$1,'Root')`, hash); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, a: a, srv: a.routes()}
	rec := e.do("POST", "/api/admin/login", nil, map[string]string{"username": "root", "password": "correct horse battery"})
	if rec.Code != 200 {
		t.Fatalf("admin login: %d %s", rec.Code, rec.Body)
	}
	e.admin = cookieOf(rec, adminCookie)
	return e
}

func cookieOf(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (e *env) do(method, path string, c *http.Cookie, body any) *httptest.ResponseRecorder {
	var rd bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = *bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, &rd)
	req.Header.Set("Origin", testOrigin)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func (e *env) decode(rec *httptest.ResponseRecorder, v any) {
	e.t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		e.t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

func mkCitizenID(prefix12 string) string {
	sum := 0
	for i := 0; i < 12; i++ {
		sum += int(prefix12[i]-'0') * (13 - i)
	}
	return prefix12 + fmt.Sprint((11-sum%11)%10)
}

func (e *env) voter(n int) *http.Cookie {
	e.t.Helper()
	id := mkCitizenID(fmt.Sprintf("1%011d", n))
	rec := e.do("POST", "/api/auth/mock-thaiid", nil, map[string]string{"citizen_id": id, "name": fmt.Sprintf("Voter %d", n)})
	if rec.Code != 200 {
		e.t.Fatalf("voter login: %d %s", rec.Code, rec.Body)
	}
	return cookieOf(rec, voterCookie)
}

type pollSpec map[string]any

// poll creates + publishes a poll; window: "open" | "upcoming".
func (e *env) poll(kind string, anonymous bool, visibility, window string, options []string, maxChoices int) (id string, opts []optionOut) {
	e.t.Helper()
	start := time.Now().Add(-time.Hour)
	if window == "upcoming" {
		start = time.Now().Add(time.Hour)
	}
	rec := e.do("POST", "/api/admin/polls", e.admin, pollSpec{
		"title": "t", "description": "", "kind": kind, "max_choices": maxChoices, "anonymous": anonymous,
		"results_visibility": visibility, "starts_at": start, "ends_at": time.Now().Add(2 * time.Hour), "options": options,
	})
	if rec.Code != 201 {
		e.t.Fatalf("create poll: %d %s", rec.Code, rec.Body)
	}
	var out struct{ ID string }
	e.decode(rec, &out)
	if rec := e.do("POST", "/api/admin/polls/"+out.ID+"/publish", e.admin, nil); rec.Code != 200 {
		e.t.Fatalf("publish: %d %s", rec.Code, rec.Body)
	}
	var p pollOut
	e.decode(e.do("GET", "/api/polls/"+out.ID, nil, nil), &p)
	return out.ID, p.Options
}

func (e *env) vote(pollID string, c *http.Cookie, optionIDs []string, comment string) *httptest.ResponseRecorder {
	if optionIDs == nil {
		optionIDs = []string{}
	}
	return e.do("POST", "/api/polls/"+pollID+"/vote", c, map[string]any{"option_ids": optionIDs, "comment": comment})
}

func ids(o []optionOut, idx ...int) []string {
	var s []string
	for _, i := range idx {
		s = append(s, o[i].ID)
	}
	return s
}

func want(t *testing.T, rec *httptest.ResponseRecorder, code int) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("want %d got %d: %s", code, rec.Code, rec.Body)
	}
}

func TestCitizenIDChecksum(t *testing.T) {
	good := mkCitizenID("110170020345")
	if !validCitizenID(good) {
		t.Fatal("generated id should be valid")
	}
	bad := good[:12] + fmt.Sprint((int(good[12]-'0')+1)%10)
	if validCitizenID(bad) || validCitizenID("123") || validCitizenID("abcdefghijklm") {
		t.Fatal("invalid ids accepted")
	}
}

func TestVoteOncePerVoter(t *testing.T) {
	e := setup(t)
	pid, o := e.poll("single", false, "live", "open", []string{"A", "B"}, 1)
	a, b := e.voter(1), e.voter(2)

	want(t, e.vote(pid, nil, ids(o, 0), ""), 401)
	want(t, e.vote(pid, a, nil, ""), 400)          // no choice
	want(t, e.vote(pid, a, ids(o, 0, 1), ""), 400) // 2 choices on single
	want(t, e.vote(pid, a, []string{"not-a-uuid"}, ""), 400)
	want(t, e.vote(pid, a, ids(o, 0), "hi"), 400) // comment not allowed
	want(t, e.vote(pid, a, ids(o, 0), ""), 201)
	want(t, e.vote(pid, a, ids(o, 1), ""), 409) // second vote
	want(t, e.vote(pid, b, ids(o, 0), ""), 201)

	rec := e.do("GET", "/api/polls/"+pid+"/results", nil, nil)
	want(t, rec, 200)
	var res resultsOut
	e.decode(rec, &res)
	if res.Total != 2 || res.Options[0].Count != 2 || res.Options[1].Count != 0 {
		t.Fatalf("bad tally: %+v", res)
	}
	if len(res.Ballots) != 2 || res.Ballots[0].Name == nil {
		t.Fatalf("public poll should list named ballots: %+v", res.Ballots)
	}
}

func TestMultiMaxEnforced(t *testing.T) {
	e := setup(t)
	pid, o := e.poll("multi", false, "live", "open", []string{"A", "B", "C"}, 2)
	v := e.voter(1)
	want(t, e.vote(pid, v, ids(o, 0, 1, 2), ""), 400)
	want(t, e.vote(pid, v, ids(o, 0, 0), ""), 400) // duplicate option
	want(t, e.vote(pid, v, ids(o, 0, 2), ""), 201)
}

func TestVoteWindow(t *testing.T) {
	e := setup(t)
	pid, o := e.poll("single", false, "live", "upcoming", []string{"A", "B"}, 1)
	v := e.voter(1)
	want(t, e.vote(pid, v, ids(o, 0), ""), 409) // not started

	open, oo := e.poll("single", false, "live", "open", []string{"A", "B"}, 1)
	if _, err := e.a.db.Exec(context.Background(), `UPDATE polls SET ends_at=now()-interval '1 minute', starts_at=now()-interval '2 hours' WHERE id=$1`, open); err != nil {
		t.Fatal(err)
	}
	want(t, e.vote(open, v, ids(oo, 0), ""), 409) // ended
}

func TestAnonymousBallotsUnlinkable(t *testing.T) {
	e := setup(t)
	pid, o := e.poll("single", true, "live", "open", []string{"A", "B"}, 1)
	want(t, e.vote(pid, e.voter(1), ids(o, 0), ""), 201)

	var voter, created, pcreated *string
	err := e.a.db.QueryRow(context.Background(), `SELECT b.voter_id::text, b.created_at::text FROM ballots b WHERE b.poll_id=$1`, pid).Scan(&voter, &created)
	if err != nil {
		t.Fatal(err)
	}
	if voter != nil || created != nil {
		t.Fatalf("anonymous ballot leaks voter/time: %v %v", voter, created)
	}
	if err := e.a.db.QueryRow(context.Background(), `SELECT created_at::text FROM participations WHERE poll_id=$1`, pid).Scan(&pcreated); err != nil {
		t.Fatal(err)
	}
	if pcreated != nil {
		t.Fatal("anonymous participation must not carry a timestamp")
	}
	var res resultsOut
	e.decode(e.do("GET", "/api/polls/"+pid+"/results", nil, nil), &res)
	if len(res.Ballots) != 0 || res.Options[0].Count != 1 {
		t.Fatalf("anonymous results must be tally-only: %+v", res)
	}
}

func TestAfterCloseVisibility(t *testing.T) {
	e := setup(t)
	pid, o := e.poll("single", true, "after_close", "open", []string{"A", "B"}, 1)
	want(t, e.vote(pid, e.voter(1), ids(o, 0), ""), 201)
	want(t, e.do("GET", "/api/polls/"+pid+"/results", nil, nil), 403)
	want(t, e.do("GET", "/api/admin/polls/"+pid+"/results", e.admin, nil), 200)
	want(t, e.do("POST", "/api/admin/polls/"+pid+"/close", e.admin, nil), 200)
	want(t, e.do("GET", "/api/polls/"+pid+"/results", nil, nil), 200)
	want(t, e.vote(pid, e.voter(2), ids(o, 0), ""), 409) // closed early
}

func TestChainVerifyDetectsTamper(t *testing.T) {
	e := setup(t)
	pid, o := e.poll("single", false, "live", "open", []string{"A", "B"}, 1)
	for i := 1; i <= 3; i++ {
		want(t, e.vote(pid, e.voter(i), ids(o, i%2), ""), 201)
	}
	verify := func() (ok bool, problems []string) {
		rec := e.do("GET", "/api/admin/polls/"+pid+"/verify", e.admin, nil)
		want(t, rec, 200)
		var out struct {
			OK       bool     `json:"ok"`
			Problems []string `json:"problems"`
		}
		e.decode(rec, &out)
		return out.OK, out.Problems
	}
	if ok, p := verify(); !ok {
		t.Fatalf("clean chain should verify: %v", p)
	}

	ctx := context.Background()
	if _, err := e.a.db.Exec(ctx, `UPDATE ballot_choices SET option_id=$1`, o[0].ID); err == nil {
		t.Fatal("append-only trigger should reject UPDATE")
	}
	if _, err := e.a.db.Exec(ctx, `DELETE FROM ballots`); err == nil {
		t.Fatal("append-only trigger should reject DELETE")
	}

	// simulate a DB admin bypassing the trigger
	for _, q := range []string{
		`ALTER TABLE ballot_choices DISABLE TRIGGER ballot_choices_append_only`,
		`UPDATE ballot_choices SET option_id='` + o[1].ID + `' WHERE ballot_id=(SELECT id FROM ballots WHERE seq=2)`,
		`ALTER TABLE ballot_choices ENABLE TRIGGER ballot_choices_append_only`,
	} {
		if _, err := e.a.db.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	ok, problems := verify()
	if ok || len(problems) == 0 || !strings.Contains(problems[0], "#2") {
		t.Fatalf("tamper not detected: ok=%v problems=%v", ok, problems)
	}
}

func TestCommentPollAndHide(t *testing.T) {
	e := setup(t)
	pid, _ := e.poll("comment", true, "live", "open", nil, 1)
	a := e.voter(1)
	want(t, e.vote(pid, a, nil, ""), 400)
	want(t, e.vote(pid, a, nil, "ความเห็นของฉัน"), 201)

	var adminRes resultsOut
	e.decode(e.do("GET", "/api/admin/polls/"+pid+"/results", e.admin, nil), &adminRes)
	if len(adminRes.Ballots) != 1 || adminRes.Ballots[0].ID == "" {
		t.Fatalf("admin should see ballot id: %+v", adminRes.Ballots)
	}
	want(t, e.do("POST", "/api/admin/ballots/"+adminRes.Ballots[0].ID+"/hide", e.admin, map[string]string{"reason": "spam"}), 200)

	var pub resultsOut
	e.decode(e.do("GET", "/api/polls/"+pid+"/results", nil, nil), &pub)
	if len(pub.Ballots) != 0 {
		t.Fatalf("hidden anonymous comment must not be public: %+v", pub.Ballots)
	}
}

func TestSecurityGuards(t *testing.T) {
	e := setup(t)
	// admin endpoints need admin cookie; voter cookie is not enough
	v := e.voter(1)
	want(t, e.do("GET", "/api/admin/polls", nil, nil), 401)
	want(t, e.do("GET", "/api/admin/polls", v, nil), 401)
	// wrong origin blocked
	req := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(`{}`))
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	want(t, rec, 403)
	// bad password
	want(t, e.do("POST", "/api/admin/login", nil, map[string]string{"username": "root", "password": "nope"}), 401)
	want(t, e.do("POST", "/api/admin/login", nil, map[string]string{"username": "ghost", "password": "nope"}), 401)
	// invalid mock id
	want(t, e.do("POST", "/api/auth/mock-thaiid", nil, map[string]string{"citizen_id": "1234567890123", "name": "x"}), 400)
	// published poll can't be edited after it starts
	pid, _ := e.poll("single", false, "live", "open", []string{"A", "B"}, 1)
	rec = e.do("PUT", "/api/admin/polls/"+pid, e.admin, pollSpec{
		"title": "x", "kind": "single", "anonymous": false, "results_visibility": "live",
		"starts_at": time.Now(), "ends_at": time.Now().Add(time.Hour), "options": []string{"A", "B"},
	})
	want(t, rec, 409)
}
