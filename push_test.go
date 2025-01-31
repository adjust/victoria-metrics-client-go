package metrics

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAddExtraLabels(t *testing.T) {
	f := func(s, extraLabels, expectedResult string) {
		t.Helper()
		result := addExtraLabels(nil, []byte(s), extraLabels)
		if string(result) != expectedResult {
			t.Fatalf("unexpected result; got\n%s\nwant\n%s", result, expectedResult)
		}
	}
	f("", `foo="bar"`, "")
	f("a 123", `foo="bar"`, `a{foo="bar"} 123`+"\n")
	f(`a{b="c"} 1.3`, `foo="bar"`, `a{foo="bar",b="c"} 1.3`+"\n")
	f(`a{b="c}{"} 1.3`, `foo="bar",baz="x"`, `a{foo="bar",baz="x",b="c}{"} 1.3`+"\n")
	f(`foo 1
bar{a="x"} 2
`, `foo="bar"`, `foo{foo="bar"} 1
bar{foo="bar",a="x"} 2
`)
	f(`
foo 1
# some counter
# type foobar counter
	  foobar{a="b",c="d"} 4`, `x="y"`, `foo{x="y"} 1
# some counter
# type foobar counter
foobar{x="y",a="b",c="d"} 4
`)
}

func TestInitPushFailure(t *testing.T) {
	f := func(pushURL string, interval time.Duration, extraLabels string) {
		t.Helper()
		if err := InitPush(pushURL, interval, extraLabels, false); err == nil {
			t.Fatalf("expecting non-nil error")
		}
	}

	// Invalid url
	f("foobar", time.Second, "")
	f("aaa://foobar", time.Second, "")
	f("http:///bar", time.Second, "")

	// Non-positive interval
	f("http://foobar", 0, "")
	f("http://foobar", -time.Second, "")

	// Invalid extraLabels
	f("http://foobar", time.Second, "foo")
	f("http://foobar", time.Second, "foo{bar")
	f("http://foobar", time.Second, "foo=bar")
	f("http://foobar", time.Second, "foo='bar'")
	f("http://foobar", time.Second, `foo="bar",baz`)
	f("http://foobar", time.Second, `{foo="bar"}`)
	f("http://foobar", time.Second, `a{foo="bar"}`)
}

func TestRunCloseTests(t *testing.T) {
	f := func(name string, f func(t *testing.T)) {
		t.Helper()
		closeWG = &sync.WaitGroup{}
		t.Run(name, f)
	}
	f("test sigChan length", testSigChanLength)
	f("test global close", testCloseGlobal)
}

func testSigChanLength(t *testing.T) {
	pushUrl := setupRequestRecorder(t, new(bytes.Buffer))
	wg := &sync.WaitGroup{}
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func(t *testing.T) {
			_ = InitPush(pushUrl, 10*time.Second, "", false)
			wg.Done()
		}(t)
	}
	wg.Wait()
	if len(chanSlice) != 500 {
		t.Fatalf("expecting signChan leght to be 1")
	}
	_ = Close()
	if len(chanSlice) != 0 {
		t.Fatalf("expecting signChan leght to be 0 after close")
	}
}

func testCloseGlobal(t *testing.T) {
	bb := new(bytes.Buffer)
	svrUrl := setupRequestRecorder(t, bb)

	_ = InitPush(svrUrl, 10*time.Minute, "", true)

	counter := NewCounter("foo_bar_total")
	counter.Inc()

	_ = Close()

	resp := bb.String()
	if !strings.Contains(resp, "foo_bar_total 1") {
		t.Errorf(`metrics does not contain "foo_bar_total 1"`)
	}

	if !strings.Contains(resp, fmt.Sprintf(`metrics_push_close_total{url="%s"} 1`, svrUrl)) {
		t.Errorf(`close metric not increamented"`)
	}
}

func setupRequestRecorder(t *testing.T, bb *bytes.Buffer) string {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatalf("recording request body failed")
		}
		_, err = io.Copy(bb, body)
		if err != nil {
			t.Fatalf("recording request body failed")
		}
		w.WriteHeader(http.StatusOK)
	}))
	return svr.URL
}
