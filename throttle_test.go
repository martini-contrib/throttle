package throttle

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-martini/martini"
)

const (
	host     string = "http://localhost:3000"
	endpoint string = "ws://localhost:3000"
)

// Test Helpers
func expectSame(t *testing.T, a interface{}, b interface{}) {
	if a != b {
		t.Errorf("Expected %T: %v to be %T: %v", b, b, a, a)
	}
}

func expectEmpty(t *testing.T, a []string) {
	if len(a) != 0 {
		t.Errorf("Expected %T: %v to be empty", a, a)
	}
}

func expectApproximateTimestamp(t *testing.T, a int64, b int64) {
	if a != b && a != b+1 {
		t.Errorf("Expected %v to be bigger than or equal to %v", b, a)
	}
}

func expectMatches(t *testing.T, reg string, result string) {
	r := regexp.MustCompile(reg)
	if !r.Match([]byte(result)) {
		t.Errorf("Expected %v to match %v", result, reg)
	}
}

func expectStatusCode(t *testing.T, expectedStatusCode int, actualStatusCode int) {
	if actualStatusCode != expectedStatusCode {
		t.Errorf("Expected StatusCode %d, but received %d", expectedStatusCode, actualStatusCode)
	}
}

func utcTimestamp() int64 {
	return time.Now().Unix()
}

type Expectation struct {
	StatusCode         int
	Body               string
	RateLimitLimit     string
	RateLimitRemaining string
	RateLimitReset     int64
	Wait               time.Duration
	ForwardedFor       string
	Concurrent         bool
}

func setupMartiniWithPolicy(limit uint64, within time.Duration, options ...*Options) *martini.ClassicMartini {
	m := martini.Classic()

	addPolicy(m, limit, within, options...)

	m.Any("/test", func() int {
		return http.StatusOK
	})

	return m
}

func addPolicy(m *martini.ClassicMartini, limit uint64, within time.Duration, options ...*Options) {
	m.Use(Policy(&Quota{
		Limit:  limit,
		Within: within,
	}, options...))
}

func setupMartiniWithPolicyAsHandler(limit uint64, within time.Duration, options ...*Options) *martini.ClassicMartini {
	m := martini.Classic()

	m.Any("/test", Policy(&Quota{
		Limit:  limit,
		Within: within,
	}, options...),
		func() int {
			return http.StatusOK
		})

	return m
}

func testResponseToExpectation(t *testing.T, m *martini.ClassicMartini, expectation *Expectation) {
	req, err := http.NewRequest("GET", "/test", strings.NewReader(""))

	if expectation.ForwardedFor != "" {
		req.Header.Set("X-Forwarded-For", expectation.ForwardedFor)
	} else {
		reflect.ValueOf(req).Elem().FieldByName("RemoteAddr").SetString("1.2.3.4:5000")
	}

	if err != nil {
		t.Error(err)
	}

	time.Sleep(expectation.Wait)
	recorder := httptest.NewRecorder()
	m.ServeHTTP(recorder, req)

	expectStatusCode(t, expectation.StatusCode, recorder.Code)
	if expectation.Body != "" {
		expectSame(t, recorder.Body.String(), expectation.Body)
	}

	header := recorder.Header()
	rateLimitLimit := header["X-Ratelimit-Limit"]
	rateLimitRemaining := header["X-Ratelimit-Remaining"]
	rateLimitReset := header["X-Ratelimit-Reset"]

	if expectation.RateLimitLimit != "" {
		expectSame(t, rateLimitLimit[0], expectation.RateLimitLimit)
	}

	if expectation.RateLimitRemaining != "" {
		expectSame(t, rateLimitRemaining[0], expectation.RateLimitRemaining)
	}

	if expectation.RateLimitReset != 0 {
		resetTime, err := strconv.ParseInt(rateLimitReset[0], 10, 64)
		if err != nil {
			t.Errorf(err.Error())
		}
		expectApproximateTimestamp(t, resetTime, expectation.RateLimitReset)
	}
}

func testResponses(t *testing.T, m *martini.ClassicMartini, expectations ...*Expectation) {
	runtime.GOMAXPROCS(runtime.NumCPU() * 2)
	wg := sync.WaitGroup{}
	for i, e := range expectations {
		if e.Concurrent {
			wg.Add(1)
			go func(k int, expectation *Expectation) {
				defer wg.Done()
				testResponseToExpectation(t, m, expectation)
			}(i, e)
		} else {
			wg.Wait()
			testResponseToExpectation(t, m, e)
		}
	}

	wg.Wait()
}

func TestTimeLimit(t *testing.T) {
	m := setupMartiniWithPolicyAsHandler(1, 10*time.Millisecond)
	testResponses(t, m, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "1",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
	}, &Expectation{
		StatusCode:         StatusTooManyRequests,
		Body:               "Too Many Requests",
		RateLimitLimit:     "1",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
	}, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "1",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
		Wait:               10 * time.Millisecond,
	})
}

func TestTimeLimitWhenForwarded(t *testing.T) {
	m := setupMartiniWithPolicyAsHandler(1, 10*time.Millisecond)
	testResponses(t, m, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "1",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
		ForwardedFor:       "2.3.4.5",
	}, &Expectation{
		StatusCode:         StatusTooManyRequests,
		Body:               "Too Many Requests",
		RateLimitLimit:     "1",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
		ForwardedFor:       "2.3.4.5",
	}, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "1",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
		Wait:               10 * time.Millisecond,
		ForwardedFor:       "2.3.4.5",
	})
}

func TestTimeLimitWithOptions(t *testing.T) {
	m := setupMartiniWithPolicy(1, 10*time.Millisecond, &Options{
		StatusCode: http.StatusBadRequest,
		Message:    "Server says no",
	})

	testResponses(t, m, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "1",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
	}, &Expectation{
		StatusCode:         http.StatusBadRequest,
		Body:               "Server says no",
		RateLimitLimit:     "1",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
	}, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "1",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
		Wait:               10 * time.Millisecond,
	})
}

func TestLimitWhenDisabled(t *testing.T) {
	m := setupMartiniWithPolicy(1, 10*time.Millisecond, &Options{
		Disabled: true,
	})

	testResponses(t, m, &Expectation{
		StatusCode: http.StatusOK,
	}, &Expectation{
		StatusCode: http.StatusOK,
	}, &Expectation{
		StatusCode: http.StatusOK,
		Wait:       10 * time.Millisecond,
	})
}

func TestRateLimit(t *testing.T) {
	m := setupMartiniWithPolicy(2, 20*time.Millisecond)
	testResponses(t, m, &Expectation{
		StatusCode:     http.StatusOK,
		RateLimitLimit: "2",
		RateLimitReset: utcTimestamp(),
	}, &Expectation{
		StatusCode:     http.StatusOK,
		RateLimitLimit: "2",
		RateLimitReset: utcTimestamp(),
	}, &Expectation{
		StatusCode:         StatusTooManyRequests,
		Body:               "Too Many Requests",
		RateLimitLimit:     "2",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
	}, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "2",
		RateLimitRemaining: "1",
		RateLimitReset:     utcTimestamp(),
		Wait:               20 * time.Millisecond,
	})
}

func TestRateLimitWithOptions(t *testing.T) {
	m := setupMartiniWithPolicyAsHandler(2, 10*time.Millisecond, &Options{
		StatusCode: http.StatusBadRequest,
		Message:    "Server says no",
	})
	testResponses(t, m, &Expectation{
		StatusCode:     http.StatusOK,
		RateLimitLimit: "2",
		RateLimitReset: utcTimestamp(),
		Concurrent:     true,
	}, &Expectation{
		StatusCode:     http.StatusOK,
		RateLimitLimit: "2",
		RateLimitReset: utcTimestamp(),
		Concurrent:     true,
	}, &Expectation{
		StatusCode:         http.StatusBadRequest,
		Body:               "Server says no",
		RateLimitLimit:     "2",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
	}, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "2",
		RateLimitRemaining: "1",
		RateLimitReset:     utcTimestamp(),
		Wait:               10 * time.Millisecond,
	})
}

func TestMultiplePolicies(t *testing.T) {
	m := setupMartiniWithPolicyAsHandler(2, 20*time.Millisecond)
	addPolicy(m, 1, 5*time.Millisecond)

	testResponses(t, m, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "2",
		RateLimitRemaining: "1",
		RateLimitReset:     utcTimestamp(),
	}, &Expectation{ // Time Limit Throttling kicks in
		StatusCode:         StatusTooManyRequests,
		Body:               "Too Many Requests",
		RateLimitLimit:     "1",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
	}, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "2",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
		Wait:               5 * time.Millisecond,
	}, &Expectation{
		StatusCode:         StatusTooManyRequests,
		Body:               "Too Many Requests",
		RateLimitLimit:     "2",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
		Wait:               5 * time.Millisecond,
	})
}

func TestRateLimitWithConcurrentRequests(t *testing.T) {
	m := setupMartiniWithPolicy(5, 20*time.Millisecond)
	testResponses(t, m, &Expectation{
		StatusCode:     http.StatusOK,
		RateLimitLimit: "5",
		RateLimitReset: utcTimestamp(),
		Concurrent:     true,
	}, &Expectation{
		StatusCode:     http.StatusOK,
		RateLimitLimit: "5",
		RateLimitReset: utcTimestamp(),
		Concurrent:     true,
	}, &Expectation{
		StatusCode:     http.StatusOK,
		RateLimitLimit: "5",
		RateLimitReset: utcTimestamp(),
		Concurrent:     true,
	}, &Expectation{
		StatusCode:     http.StatusOK,
		RateLimitLimit: "5",
		RateLimitReset: utcTimestamp(),
		Concurrent:     true,
	}, &Expectation{
		StatusCode:     http.StatusOK,
		RateLimitLimit: "5",
		RateLimitReset: utcTimestamp(),
		Concurrent:     true,
	}, &Expectation{
		StatusCode:         StatusTooManyRequests,
		Body:               "Too Many Requests",
		RateLimitLimit:     "5",
		RateLimitRemaining: "0",
		RateLimitReset:     utcTimestamp(),
	}, &Expectation{
		StatusCode:         http.StatusOK,
		RateLimitLimit:     "5",
		RateLimitRemaining: "4",
		RateLimitReset:     utcTimestamp(),
		Wait:               20 * time.Millisecond,
	})
}
