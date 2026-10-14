package main

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// pollWith creates a poll from a base spec overridden by extra; publish=true also publishes it.
func (e *env) pollWith(extra pollSpec, publish bool) pollOut {
	e.t.Helper()
	spec := pollSpec{
		"title": "t", "description": "", "kind": "approve", "max_choices": 1, "anonymous": false,
		"results_visibility": "live", "starts_at": time.Now().Add(-time.Hour), "ends_at": time.Now().Add(2 * time.Hour),
		"options": []string{},
	}
	for k, v := range extra {
		spec[k] = v
	}
	rec := e.do("POST", "/api/admin/polls", e.admin, spec)
	want(e.t, rec, 201)
	var out struct{ ID string }
	e.decode(rec, &out)
	if publish {
		want(e.t, e.do("POST", "/api/admin/polls/"+out.ID+"/publish", e.admin, nil), 200)
	}
	var p pollOut
	e.decode(e.do("GET", "/api/admin/polls/"+out.ID, e.admin, nil), &p)
	return p
}

// answers picks, for each question, the option with the given label.
func answers(p pollOut, labels ...string) []string {
	var out []string
	for i, q := range p.Questions {
		for _, o := range p.Options {
			if o.QuestionID == q.ID && o.Label == labels[i] {
				out = append(out, o.ID)
			}
		}
	}
	return out
}

func TestMultiQuestionApprove(t *testing.T) {
	e := setup(t)
	p := e.pollWith(pollSpec{"questions": []string{"มาตรา 1", "มาตรา 2", "มาตรา 3"}}, true)
	if len(p.Questions) != 3 || len(p.Options) != 9 || p.MaxChoices != 3 {
		t.Fatalf("want 3 questions x 3 options: %+v", p)
	}
	a := e.voter(1)
	all := answers(p, "เห็นด้วย", "ไม่เห็นด้วย", "งดออกเสียง")

	want(t, e.vote(p.ID, a, all[:2], ""), 400)                            // unanswered question
	want(t, e.vote(p.ID, a, append(all[:2:2], all[0]), ""), 400)          // duplicate option
	want(t, e.vote(p.ID, a, append(all[:2:2], p.Options[0].ID), ""), 400) // two answers to q1, none to q3
	want(t, e.vote(p.ID, a, all, ""), 201)
	want(t, e.vote(p.ID, a, all, ""), 409)

	var res resultsOut
	e.decode(e.do("GET", "/api/polls/"+p.ID+"/results", nil, nil), &res)
	tally := map[string]int{}
	for _, o := range res.Options {
		tally[o.QuestionID+"/"+o.Label] += o.Count
	}
	if res.Total != 1 || tally[p.Questions[0].ID+"/เห็นด้วย"] != 1 || tally[p.Questions[1].ID+"/ไม่เห็นด้วย"] != 1 || tally[p.Questions[0].ID+"/ไม่เห็นด้วย"] != 0 {
		t.Fatalf("bad tally: %+v", res.Options)
	}
	if got := strings.Join(res.Ballots[0].Choices, "|"); !strings.Contains(got, "มาตรา 2: ไม่เห็นด้วย") {
		t.Fatalf("named ballot should show question: %v", res.Ballots[0].Choices)
	}

	var v struct{ OK bool }
	e.decode(e.do("GET", "/api/admin/polls/"+p.ID+"/verify", e.admin, nil), &v)
	if !v.OK {
		t.Fatal("chain should verify")
	}
}

func TestImplicitSingleQuestion(t *testing.T) {
	e := setup(t)
	p := e.pollWith(nil, true)
	if len(p.Questions) != 1 || p.Questions[0].Label != "" || len(p.Options) != 3 {
		t.Fatalf("approve without questions should get one implicit question: %+v", p)
	}
	want(t, e.vote(p.ID, e.voter(1), answers(p, "เห็นด้วย"), ""), 201)
}

func TestOptionalClosingComment(t *testing.T) {
	e := setup(t)
	off := e.pollWith(nil, true)
	want(t, e.vote(off.ID, e.voter(1), answers(off, "เห็นด้วย"), "hi"), 400)

	on := e.pollWith(pollSpec{"allow_comment": true}, true)
	want(t, e.vote(on.ID, e.voter(1), answers(on, "เห็นด้วย"), ""), 201)
	want(t, e.vote(on.ID, e.voter(2), answers(on, "ไม่เห็นด้วย"), "ขอแก้ไข"), 201)
	want(t, e.vote(on.ID, e.voter(3), answers(on, "ไม่เห็นด้วย"), strings.Repeat("ก", 2001)), 400)

	var res resultsOut
	e.decode(e.do("GET", "/api/polls/"+on.ID+"/results", nil, nil), &res)
	n := 0
	for _, b := range res.Ballots {
		if b.Comment != nil && *b.Comment == "ขอแก้ไข" {
			n++
		}
	}
	if res.Total != 2 || n != 1 {
		t.Fatalf("closing comment should appear next to the choice: %+v", res.Ballots)
	}

	// a comment-only poll never takes allow_comment (the comment is the ballot)
	c := e.pollWith(pollSpec{"kind": "comment", "allow_comment": true}, false)
	if c.AllowComment {
		t.Fatal("comment poll must not set allow_comment")
	}
}

func TestListShowsTalliesOnlyWhenVisible(t *testing.T) {
	e := setup(t)
	live := e.pollWith(pollSpec{"results_visibility": "live"}, true)
	hidden := e.pollWith(pollSpec{"results_visibility": "after_close"}, true)
	for _, p := range []pollOut{live, hidden} {
		want(t, e.vote(p.ID, e.voter(1), answers(p, "เห็นด้วย"), ""), 201)
	}

	var list pollPage
	e.decode(e.do("GET", "/api/polls", nil, nil), &list)
	got := map[string]pollOut{}
	for _, p := range list.Items {
		got[p.ID] = p
	}
	sum := func(p pollOut) (n int) {
		for _, o := range p.Options {
			n += o.Count
		}
		return
	}
	if !got[live.ID].ResultsVisible || sum(got[live.ID]) != 1 {
		t.Fatalf("live poll list should carry tallies: %+v", got[live.ID])
	}
	if got[hidden.ID].ResultsVisible || sum(got[hidden.ID]) != 0 {
		t.Fatalf("after_close poll must not leak tallies while open: %+v", got[hidden.ID])
	}
	var one pollOut
	e.decode(e.do("GET", "/api/polls/"+hidden.ID, nil, nil), &one)
	if sum(one) != 0 {
		t.Fatal("detail endpoint must not leak tallies either")
	}

	want(t, e.do("POST", "/api/admin/polls/"+hidden.ID+"/close", e.admin, nil), 200)
	e.decode(e.do("GET", "/api/polls/"+hidden.ID, nil, nil), &one)
	if !one.ResultsVisible || sum(one) != 1 {
		t.Fatalf("closed poll shows tallies: %+v", one)
	}
}

func TestListPagination(t *testing.T) {
	e := setup(t)
	for i := 0; i < 3; i++ {
		e.pollWith(pollSpec{}, true)
	}
	var pg pollPage
	e.decode(e.do("GET", "/api/polls?status=open&per_page=2&page=2", nil, nil), &pg)
	if pg.Total != 3 || len(pg.Items) != 1 || pg.Counts["open"] != 3 || pg.Page != 2 {
		t.Fatalf("page 2 of 3 open polls at 2/page: %+v", pg)
	}
	e.decode(e.do("GET", "/api/polls?status=closed", nil, nil), &pg)
	if pg.Total != 0 || len(pg.Items) != 0 || pg.Counts["open"] != 3 {
		t.Fatalf("closed filter: %+v", pg)
	}
	want(t, e.do("GET", "/api/polls?status=draft%27", nil, nil), 400)
}

func TestDescriptionSanitized(t *testing.T) {
	e := setup(t)
	p := e.pollWith(pollSpec{"description": `<h2>หัวข้อ</h2><p>ดู <a href="https://example.go.th/doc.pdf" onclick="x()">เอกสาร</a>` +
		`<script>alert(1)</script><img src=x onerror=alert(1)><a href="javascript:alert(1)">bad</a></p>`}, false)
	d := p.Description
	for _, bad := range []string{"<script", "<img", "onerror", "onclick", "javascript:", "alert(1)</"} {
		if strings.Contains(d, bad) {
			t.Fatalf("%q survived sanitising: %s", bad, d)
		}
	}
	if !strings.Contains(d, `href="https://example.go.th/doc.pdf"`) || !strings.Contains(d, "<h2>") {
		t.Fatalf("links and headings should be kept: %s", d)
	}
}

var tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x9a\xa0\xa0\x00\x00\x00\x00IEND\xaeB`\x82")

func (e *env) putImage(id string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PUT", "/api/admin/polls/"+id+"/image", bytes.NewReader(body))
	req.Header.Set("Origin", testOrigin)
	req.AddCookie(e.admin)
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func TestPromoImage(t *testing.T) {
	e := setup(t)
	p := e.pollWith(nil, false)
	if p.ImageURL != "" {
		t.Fatal("no image yet")
	}
	want(t, e.putImage(p.ID, []byte("<html>not an image</html>")), 400)
	want(t, e.putImage(p.ID, append(append([]byte{}, tinyPNG...), make([]byte, maxImageBytes)...)), 400) // too large
	want(t, e.putImage(p.ID, tinyPNG), 200)

	e.decode(e.do("GET", "/api/admin/polls/"+p.ID, e.admin, nil), &p)
	if !strings.HasPrefix(p.ImageURL, "/api/polls/"+p.ID+"/image?v=") {
		t.Fatalf("image_url missing: %q", p.ImageURL)
	}
	want(t, e.do("GET", "/api/polls/"+p.ID+"/image", nil, nil), 404) // unpublished: hidden from public
	rec := e.do("GET", "/api/polls/"+p.ID+"/image", e.admin, nil)
	want(t, rec, 200)
	if rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("bad headers: %v", rec.Header())
	}

	want(t, e.do("POST", "/api/admin/polls/"+p.ID+"/publish", e.admin, nil), 200)
	want(t, e.do("GET", "/api/polls/"+p.ID+"/image", nil, nil), 200)
	want(t, e.putImage(p.ID, tinyPNG), 409) // published and already open: locked
}

func TestPromoImageLockedOnceStarted(t *testing.T) {
	e := setup(t)
	open := e.pollWith(nil, true) // already open
	want(t, e.putImage(open.ID, tinyPNG), 409)
	want(t, e.do("DELETE", "/api/admin/polls/"+open.ID+"/image", e.admin, nil), 409)

	soon := e.pollWith(pollSpec{"starts_at": time.Now().Add(time.Hour)}, true)
	want(t, e.putImage(soon.ID, tinyPNG), 200)
	want(t, e.do("DELETE", "/api/admin/polls/"+soon.ID+"/image", e.admin, nil), 200)
	var p pollOut
	e.decode(e.do("GET", "/api/polls/"+soon.ID, nil, nil), &p)
	if p.ImageURL != "" {
		t.Fatal("image should be gone")
	}
}
