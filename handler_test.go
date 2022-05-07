package httphandler

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type TestParams struct {
	Method    string
	URLs      []string
	RespCode  int
	RespSizes []int
}

// RequestBody creates a request body, a list of URLs separated by new line character.
func (tp TestParams) RequestBody() string {
	buf := bytes.NewBuffer(nil)
	for _, url := range tp.URLs {
		fmt.Fprintln(buf, url)
	}
	return buf.String()
}

// IsResponseBodyValid validates the response body and returns a sorted slice of received sizes.
// Side effect: tp.RespSizes will be sorted too.
func (tp *TestParams) IsResponseBodyValid(b *bytes.Buffer) (sizes []int, valid bool) {
	scanner := bufio.NewScanner(b)
	for scanner.Scan() {
		size, err := strconv.Atoi(scanner.Text())
		if err != nil {
			valid = false
			return
		}
		sizes = append(sizes, size)
	}
	if len(sizes) != len(tp.RespSizes) {
		valid = false
		return
	}
	sort.Ints(sizes)
	sort.Ints(tp.RespSizes)
	for i := range sizes {
		if sizes[i] != tp.RespSizes[i] {
			valid = false
			return
		}
	}
	valid = true
	return
}

func TestHTTPHandlerResponses(t *testing.T) {
	params := []TestParams{
		{
			Method:   http.MethodGet,
			URLs:     []string{},
			RespCode: http.StatusMethodNotAllowed,
		},
		{
			Method:   http.MethodPost,
			URLs:     []string{"http://abcdefgh.ijk", "http://lmnopqrs.tuv"},
			RespCode: http.StatusRequestTimeout,
		},
		{
			Method:   http.MethodPost,
			URLs:     []string{"invalidurl"},
			RespCode: http.StatusBadRequest,
		},
		{
			Method:    http.MethodPost,
			URLs:      []string{"http://abcdefgh.ijk", "http://example.com"},
			RespCode:  http.StatusMultiStatus,
			RespSizes: []int{-1, 1256},
		},
		{
			Method:    http.MethodPost,
			URLs:      []string{"http://example.com", "https://www.random.org/cgi-bin/randbyte?nbytes=32&format=h"},
			RespCode:  http.StatusOK,
			RespSizes: []int{1256, 98},
		},
	}

	for i, param := range params {
		testRequestResponse(t, i, param)
	}
}

func testRequestResponse(t *testing.T, i int, param TestParams) {
	req, err := http.NewRequest(param.Method, "/", strings.NewReader(param.RequestBody()))
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := NewHTTPHandler()
	handler.ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if param.RespCode != 0 && rr.Code != param.RespCode {
		t.Errorf("test #%d: handler returned wrong status code:\ngot:\n%v\nwant:\n%v\n",
			i+1, rr.Code, param.RespCode)
	}

	// Check the response body is what we expect.
	if sizes, valid := param.IsResponseBodyValid(rr.Body); !valid {
		t.Errorf("test #%d: handler returned unexpected body:\ngot:\n%v\nwant:\n%v\n",
			i+1, sizes, param.RespSizes)
	}
}

func TestHTTPHandlerLimiterPreThreshold(t *testing.T) {
	const n = 100
	if overloaded := testHandlerLimiter(n); overloaded {
		t.Errorf("limiter test: handler unexpectedly overloaded at %d requests", n)
	}
}

func TestHTTPHandlerLimiterPostThreshold(t *testing.T) {
	const n = 105
	if overloaded := testHandlerLimiter(n); !overloaded {
		t.Errorf("limiter test: handler didn't overload at %d requests", n)
	}
}

func testHandlerLimiter(n int) (overloaded bool) {
	handler := NewHTTPHandler()
	ch := make(chan struct{}, 1)
	wg := new(sync.WaitGroup)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go testRequestOverload(handler, i, ch, wg)
	}
	wg.Wait()
	select {
	case <-ch:
		overloaded = true
	default:
	}
	return
}

func testRequestOverload(handler http.Handler, i int, overloaded chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	req, err := http.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf("http://abcdefgh%d.ijk", i)))
	if err != nil {
		return
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	// Check the status code is what we expect.
	if rr.Code == http.StatusTooManyRequests {
		select {
		case overloaded <- struct{}{}:
		default:
		}
	}
}
