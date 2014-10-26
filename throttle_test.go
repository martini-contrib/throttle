package throttle

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strconv"
	"strings"
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

func testResponses(t *testing.T, m *martini.ClassicMartini, expectations ...*Expectation) {
	for _, expectation := range expectations {
		req, err := http.NewRequest("GET", "/test", strings.NewReader(""))
		reflect.ValueOf(req).Elem().FieldByName("RemoteAddr").SetString("1.2.3.4:5000")

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
		} else {
			expectEmpty(t, rateLimitLimit)
		}

		if expectation.RateLimitRemaining != "" {
			expectSame(t, rateLimitRemaining[0], expectation.RateLimitRemaining)
		} else {
			expectEmpty(t, rateLimitRemaining)
		}

		if expectation.RateLimitReset == 0 {
			resetTime, err := strconv.ParseInt(rateLimitReset[0], 10, 64)
			if err != nil {
				t.Errorf(err.Error())
			}
			expectApproximateTimestamp(t, resetTime, expectation.RateLimitReset)
		}
	}
}

func TestTimeLimit(t *testing.T) {
	m := setupMartiniWithPolicyAsHandler(1, 10*time.Millisecond)
	testResponses(t, m, &Expectation{
		http.StatusOK,
		"",
		"1",
		"0",
		utcTimestamp(),
		0,
	}, &Expectation{
		StatusTooManyRequests,
		"Too Many Requests",
		"1",
		"0",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"1",
		"0",
		utcTimestamp(),
		10 * time.Millisecond,
	})
}

func TestTimeLimitWithOptions(t *testing.T) {
	m := setupMartiniWithPolicy(1, 10*time.Millisecond, &Options{
		StatusCode: http.StatusBadRequest,
		Message:    "Server says no",
	})

	testResponses(t, m, &Expectation{
		http.StatusOK,
		"",
		"1",
		"0",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusBadRequest,
		"Server says no",
		"1",
		"0",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"1",
		"0",
		utcTimestamp(),
		10 * time.Millisecond,
	})
}

func TestLimitWhenDisabled(t *testing.T) {
	m := setupMartiniWithPolicy(1, 10*time.Millisecond, &Options{
		Disabled: true,
	})

	testResponses(t, m, &Expectation{
		http.StatusOK,
		"",
		"",
		"",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"",
		"",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"",
		"",
		utcTimestamp(),
		10 * time.Millisecond,
	})
}

func TestRateLimit(t *testing.T) {
	m := setupMartiniWithPolicy(2, 10*time.Millisecond)
	testResponses(t, m, &Expectation{
		http.StatusOK,
		"",
		"2",
		"1",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"2",
		"0",
		utcTimestamp(),
		0,
	}, &Expectation{
		StatusTooManyRequests,
		"Too Many Requests",
		"2",
		"0",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"2",
		"1",
		utcTimestamp(),
		10 * time.Millisecond,
	})
}

func TestRateLimitWithOptions(t *testing.T) {
	m := setupMartiniWithPolicyAsHandler(2, 10*time.Millisecond, &Options{
		StatusCode: http.StatusBadRequest,
		Message:    "Server says no",
	})
	testResponses(t, m, &Expectation{
		http.StatusOK,
		"",
		"2",
		"1",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"2",
		"0",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusBadRequest,
		"Server says no",
		"2",
		"0",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"2",
		"1",
		utcTimestamp(),
		10 * time.Millisecond,
	})
}

func TestMultiplePolicies(t *testing.T) {
	m := setupMartiniWithPolicyAsHandler(2, 20*time.Millisecond)
	addPolicy(m, 1, 5*time.Millisecond)

	testResponses(t, m, &Expectation{
		http.StatusOK,
		"",
		"2",
		"1",
		utcTimestamp(),
		0,
	}, &Expectation{ // Time Limit Throttling kicks in
		StatusTooManyRequests,
		"Too Many Requests",
		"1",
		"0",
		utcTimestamp(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"2",
		"0",
		utcTimestamp(),
		5 * time.Millisecond,
	}, &Expectation{
		StatusTooManyRequests,
		"Too Many Requests",
		"2",
		"0",
		utcTimestamp(),
		5 * time.Millisecond,
	})
}
