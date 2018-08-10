package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type mockDB struct {
	t  *testing.T
	rr []*record
}

func newMockDB(t *testing.T) *mockDB {
	return &mockDB{t: t}
}

func (md *mockDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	md.rr = append(md.rr, &record{
		rat:      toTime(md.t, args[0]),
		url:      toString(md.t, args[1]),
		method:   toString(md.t, args[2]),
		remote:   toString(md.t, args[3]),
		headers:  toString(md.t, args[4]),
		length:   toInt(md.t, args[5]),
		protocol: toString(md.t, args[6]),
	})
	return nil, nil
}

func (md *mockDB) reset() {
	md.rr = nil
}

type record struct {
	rat      time.Time
	url      string
	method   string
	remote   string
	headers  string
	length   int
	protocol string
}

func toTime(t *testing.T, v interface{}) time.Time {
	tv, ok := v.(time.Time)
	if !ok {
		t.Fatalf("expected time value passed for field")
	}
	return tv
}

func toString(t *testing.T, v interface{}) string {
	sv, ok := v.(string)
	if !ok {
		t.Fatalf("expected string value passed for field")
	}
	return sv
}

func toInt(t *testing.T, v interface{}) int {
	iv, ok := v.(int)
	if !ok {
		t.Fatalf("expected int value passed for field")
	}
	return iv
}

func Test(t *testing.T) {
	mockDB := newMockDB(t)
	now := time.Now().UTC()
	testServer := &server{
		db:  mockDB,
		now: func() time.Time { return now },
	}
	ts := httptest.NewServer(http.HandlerFunc(testServer.serveReqLog))
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		req         *http.Request
		wantCode    int
		wantRecords []*record
	}{
		"request record is inserted into database": {
			req:      req,
			wantCode: 200,
			wantRecords: []*record{{
				rat:      now,
				url:      ts.URL + "/",
				method:   "GET",
				remote:   "127.0.0.1",
				headers:  "map[User-Agent:[Go-http-client/1.1] Accept-Encoding:[gzip]]",
				length:   0,
				protocol: "HTTP/1.1",
			}},
		},
		"request record is inserted into database again": {
			req:      req,
			wantCode: 200,
			wantRecords: []*record{{
				rat:      now,
				url:      ts.URL + "/",
				method:   "GET",
				remote:   "127.0.0.1",
				headers:  "map[User-Agent:[Go-http-client/1.1] Accept-Encoding:[gzip]]",
				length:   0,
				protocol: "HTTP/1.1",
			}},
		},
	}

	for name, test := range tests {
		mockDB.reset()
		res, err := ts.Client().Do(test.req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != test.wantCode {
			t.Errorf("%q: want status code %v, got status code %v", name, test.wantCode, res.StatusCode)
		}
		if len(mockDB.rr) != len(test.wantRecords) {
			t.Errorf("%q: expected one record in db, got %v", name, len(mockDB.rr))
			continue
		}
		for i := range mockDB.rr {
			wantr := test.wantRecords[i]
			gotr := mockDB.rr[i]
			if wantr.rat != gotr.rat {
				t.Errorf("%q: at index %d expected record with rat %v, got %v", name, i, wantr.rat, gotr.rat)
			}
			if wantr.url != gotr.url {
				t.Errorf("%q: at index %d expected record with url %v, got %v", name, i, wantr.url, gotr.url)
			}
			if wantr.method != gotr.method {
				t.Errorf("%q: at index %d expected record with method %v, got %v", name, i, wantr.method, gotr.method)
			}
			if strings.Split(wantr.remote, ":")[0] != strings.Split(gotr.remote, ":")[0] {
				t.Errorf("%q: at index %d expected record with remote %v, got %v", name, i, wantr.remote, gotr.remote)
			}
			if wantr.headers != gotr.headers {
				t.Errorf("%q: at index %d expected record with headers %v, got %v", name, i, wantr.headers, gotr.headers)
			}
			if wantr.length != gotr.length {
				t.Errorf("%q: at index %d expected record with length %v, got %v", name, i, wantr.length, gotr.length)
			}
			if wantr.protocol != gotr.protocol {
				t.Errorf("%q: at index %d expected record with protocol %v, got %v", name, i, wantr.protocol, gotr.protocol)
			}
		}
	}
}
