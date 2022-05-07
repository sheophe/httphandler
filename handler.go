package httphandler

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Response struct {
	*http.Response
	Error error
}

type ResponseMap struct {
	sync.Mutex
	Map    map[string]Response
	failed int
}

func NewResponseMap() *ResponseMap {
	return &ResponseMap{
		Map: make(map[string]Response),
	}
}

// Create is used to add an URL to the set.
// This method should be used before any requests are actually made.
// It should not be called concurrently.
func (rs *ResponseMap) Create(urlString string) error {
	_, err := url.Parse(urlString)
	if err != nil {
		return err
	}
	rs.Map[urlString] = Response{}
	return nil
}

// SetResponse assigns response to the URL.
func (rs *ResponseMap) SetResponse(url string, r Response) error {
	rs.Lock()
	defer rs.Unlock()
	if v, ok := rs.Map[url]; ok && (v != Response{}) {
		return fmt.Errorf("response from %s already exists", url)
	}
	rs.Map[url] = r
	if r.Error != nil {
		rs.failed++
	}
	return nil
}

// AllFailed returns true if all the requests have failed.
// It should not be called concurrently.
func (rs *ResponseMap) AllFailed() bool {
	return rs.failed == len(rs.Map)
}

// AllSuccessful returns true if all the requests were successful.
// It should not be called concurrently.
func (rs *ResponseMap) AllSuccessful() bool {
	return rs.failed == 0
}

// Len returns length of the response map.
// It should not be called concurrently.
func (rs *ResponseMap) Len() int {
	return len(rs.Map)
}

type HTTPHandler struct {
	requestLocks   chan struct{}
	requestTimeout time.Duration
}

// NewHTTPHandler creates a handler with the default limit of 100 simultaneous requests
func NewHTTPHandler() *HTTPHandler {
	return NewHTTPHandlerWithRequestLimit(100)
}

// NewHTTPHandlerWithRequestLimit creates a handler with the user-defined limit of simultaneous requests
func NewHTTPHandlerWithRequestLimit(limit int) *HTTPHandler {
	return &HTTPHandler{
		requestLocks:   make(chan struct{}, limit),
		requestTimeout: time.Second,
	}
}

// SetRequestTimeout sets the timeout for each single request in the list
func (h *HTTPHandler) SetRequestTimeout(timeout time.Duration) {
	h.requestTimeout = timeout
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	select {
	case h.requestLocks <- struct{}{}:
		defer func() { <-h.requestLocks }()
		resps, err := h.executeAllRequests(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		h.writeResponse(w, resps)
	default:
		w.WriteHeader(http.StatusTooManyRequests)
	}
}

// writeResponse formats the response and sets the status code.
// Status codes:
//  200 — All of the requested URL have responded.
//  207 — Some of the requests have failed.
//  408 — None of the requests were successful.
func (h *HTTPHandler) writeResponse(w http.ResponseWriter, resps *ResponseMap) {
	if resps.AllFailed() {
		w.WriteHeader(http.StatusRequestTimeout)
		return
	}
	if resps.AllSuccessful() {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusMultiStatus)
	}
	for _, resp := range resps.Map {
		respString := "-1\n"
		if resp.Response != nil {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				panic(err)
			}
			respString = fmt.Sprintln(len(body))
		}
		_, err := w.Write([]byte(respString))
		if err != nil {
			panic(err)
		}
	}
}

// executeAllRequests iterates over the original request body and performs GET request for all the URLs listed.
// It blocks until either all requests have responded, timed out or the original request context is cancelled.
func (h *HTTPHandler) executeAllRequests(r *http.Request) (resps *ResponseMap, err error) {
	resps = NewResponseMap()
	scanner := bufio.NewScanner(r.Body)
	defer r.Body.Close()
	for scanner.Scan() {
		urlString := scanner.Text()
		_, err = url.ParseRequestURI(urlString)
		if err != nil {
			return
		}
		resps.Create(urlString)
	}
	if resps.Len() == 0 {
		err = errors.New("empty request body")
		return
	}
	wg := new(sync.WaitGroup)
	wg.Add(resps.Len())
	for url := range resps.Map {
		go h.executeRequest(r.Context(), url, resps, wg)
	}
	wg.Wait()
	return
}

// executeRequest performs request on a single URL.
// It blocks until response is received, request have timed out or the original request context is cancelled.
func (h *HTTPHandler) executeRequest(pctx context.Context, url string, resps *ResponseMap, wg *sync.WaitGroup) {
	defer wg.Done()
	ctx, cancel := context.WithTimeout(pctx, h.requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		resps.SetResponse(url, Response{Error: err})
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		resps.SetResponse(url, Response{Error: err})
		return
	}
	resps.SetResponse(url, Response{Response: resp})
}
