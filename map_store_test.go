package throttle

import (
	"encoding/json"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"
)

func seedRandom() {
	rand.Seed(time.Now().UnixNano())
}

func sleepRandom() {
	time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
}

func TestSet(t *testing.T) {
	store := NewMapStore(accessCount{})
	store.Set("KEY", []byte("4"))
	value, err := store.Get("KEY")
	if err != nil {
		t.Errorf(err.Error())
	}

	expectSame(t, string(value), "4")
}

func TestGet(t *testing.T) {
	seedRandom()
	store := NewMapStore(accessCount{})

	wg := &sync.WaitGroup{}
	var values []string
	store.Set("KEY", []byte(strconv.FormatInt(int64(50), 10)))

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			sleepRandom()
			value, err := store.Get("KEY")
			if err != nil {
				t.Errorf(err.Error())
			}
			values = append(values, string(value))
			wg.Done()
		}()
	}

	wg.Wait()

	for _, val := range values {
		expectSame(t, val, "50")
	}
}

func TestRead(t *testing.T) {
	store := NewMapStore(accessCount{})

	wg := &sync.WaitGroup{}
	var values []bool
	marshalled, err := json.Marshal(accessCount{
		64,
		time.Now(),
		10 * time.Millisecond,
	})
	if err != nil {
		t.Errorf(err.Error())
	}
	store.Set("KEY", marshalled)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			value, err := store.Read("KEY")
			time.Sleep(10 * time.Millisecond)
			if err != nil {
				t.Errorf(err.Error())
			}
			values = append(values, value.IsFresh())
			wg.Done()
		}()
	}

	wg.Wait()

	for _, val := range values {
		expectSame(t, val, false)
	}
}

func TestDelete(t *testing.T) {
	store := NewMapStore(accessCount{})
	wg := &sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(k int) {
			store.Set("KEY", []byte(strconv.FormatInt(int64(k), 10)))
			store.Delete("KEY")
			wg.Done()
		}(i)
	}

	wg.Wait()

	value, err := store.Get("KEY")
	if err == nil {
		t.Errorf("Expected no key to exist, but did: %v", value)
	}

	expectSame(t, err.Error(), "Throttle Map Store Error: Key KEY does not exist")
}

func TestCleaning(t *testing.T) {
	store := NewMapStore(accessCount{}, &MapStoreOptions{
		5 * time.Millisecond,
	})

	marshalled, err := json.Marshal(accessCount{
		64,
		time.Now(),
		10 * time.Millisecond,
	})

	if err != nil {
		t.Errorf(err.Error())
	}

	wg := &sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(k int) {
			store.Set("KEY"+strconv.FormatInt(int64(k), 10), marshalled)
			wg.Done()
		}(i)
	}

	wg.Wait()
	time.Sleep(11 * time.Millisecond)

	for i := 0; i < 5; i++ {
		value, err := store.Get("KEY" + strconv.FormatInt(int64(i), 10))
		if err == nil {
			t.Errorf("Expected no key to exist, but did: %v", value)
		} else {
			expectSame(t, err.Error(), "Throttle Map Store Error: Key KEY"+strconv.FormatInt(int64(i), 10)+" does not exist")
		}

	}
}
