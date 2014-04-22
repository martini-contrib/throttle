package throttle

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"reflect"
	"strings"
	"strconv"
	"regexp"
	
	"github.com/go-martini/martini"
)

const (
	host            string = "http://localhost:3000"
	endpoint        string = "ws://localhost:3000"
)

// Test Helpers
func expectSame(t *testing.T, a interface{}, b interface{}) {
	if a != b {
		t.Errorf("Expected %T: %v to be %T: %v", b, b, a, a)
	}
}

func expectBiggerOrEqual(t *testing.T, a int64, b int64) {
	if a < b {
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

type Expectation struct {
	StatusCode int
	Body string
	RateLimitLimit string
	RateLimitRemaining string
	RateLimitReset int64
	Wait time.Duration
}

func setupMartiniWithPolicy(limit uint64, within time.Duration, options ...*Options) *martini.ClassicMartini {
	m := martini.Classic()
	
	m.Use(Policy(&Quota{
		Limit: limit,
		Within: within,
	}, options...))

	m.Any("/test", func() int {
		return http.StatusOK
	})
	
	return m
}

func setupMartiniWithPolicyAsHandler(limit uint64, within time.Duration, options ...*Options) *martini.ClassicMartini {
	m := martini.Classic()

	m.Any("/test", Policy(&Quota{
		Limit: limit,
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
		expectSame(t, recorder.Header()["X-Ratelimit-Limit"][0], expectation.RateLimitLimit)
		expectSame(t, recorder.Header()["X-Ratelimit-Remaining"][0], expectation.RateLimitRemaining)
		resetTime, err := strconv.ParseInt(recorder.Header()["X-Ratelimit-Reset"][0], 10, 64)
		if err != nil {
			t.Errorf(err.Error())
		}
		expectBiggerOrEqual(t, resetTime, expectation.RateLimitReset)
	}
}

func TestTimeLimit(t *testing.T) {
	m := setupMartiniWithPolicyAsHandler(1, 10 * time.Millisecond)
	testResponses(t, m, &Expectation{
		http.StatusOK,
		"",
		"1",
		"0",
		time.Now().Unix(),
		0,
	}, &Expectation{
		StatusTooManyRequests,
		"Too Many Requests",
		"1",
		"0",
		time.Now().Unix(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"1",
		"0",
		time.Now().Unix(),
		10 * time.Millisecond,
	})
}

func TestTimeLimitWithOptions(t *testing.T) {
	m := setupMartiniWithPolicy(1, 10 * time.Millisecond, &Options{
		StatusCode: http.StatusBadRequest,
		Message: "Server says no",
	})
	
	testResponses(t, m, &Expectation{
		http.StatusOK,
		"",
		"1",
		"0",
		time.Now().Unix(),
		0,
	}, &Expectation{
		http.StatusBadRequest,
		"Server says no",
		"1",
		"0",
		time.Now().Unix(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"1",
		"0",
		time.Now().Unix(),
		10 * time.Millisecond,
	})
}

func TestRateLimit(t *testing.T) {
	m := setupMartiniWithPolicy(2, 10 * time.Millisecond)
	testResponses(t, m, &Expectation{
		http.StatusOK,
		"",
		"2",
		"1",
		time.Now().Unix(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"2",
		"0",
		time.Now().Unix(),
		0,
	}, &Expectation{
		StatusTooManyRequests,
		"Too Many Requests",
		"2",
		"0",
		time.Now().Unix(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"2",
		"1",
		time.Now().Unix(),
		10 * time.Millisecond,
	})
}

func TestRateLimitWithOptions(t *testing.T) {
	m := setupMartiniWithPolicyAsHandler(2, 10 * time.Millisecond, &Options{
		StatusCode: http.StatusBadRequest,
		Message: "Server says no",
	})
	testResponses(t, m, &Expectation{
		http.StatusOK,
		"",
		"2",
		"1",
		time.Now().Unix(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"2",
		"0",
		time.Now().Unix(),
		0,
	}, &Expectation{
		http.StatusBadRequest,
		"Server says no",
		"2",
		"0",
		time.Now().Unix(),
		0,
	}, &Expectation{
		http.StatusOK,
		"",
		"2",
		"1",
		time.Now().Unix(),
		10 * time.Millisecond,
	})
}