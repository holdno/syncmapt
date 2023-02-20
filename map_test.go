package syncmapt_test

import (
	"math/rand"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/holdno/syncmapt"
)

type mapOp string

const (
	opLoad        = mapOp("Load")
	opStore       = mapOp("Store")
	opLoadOrStore = mapOp("LoadOrStore")
	opDelete      = mapOp("Delete")
)

type AnyKey string

var mapOps = [...]mapOp{opLoad, opStore, opLoadOrStore, opDelete}

// mapCall is a quick.Generator for calls on mapInterface.
type mapCall struct {
	op mapOp
	k  string
	v  interface{}
}

func (c mapCall) apply(m mapInterface[string]) (interface{}, bool) {
	switch c.op {
	case opLoad:
		return m.Load(c.k)
	case opStore:
		m.Store(c.k, c.v)
		return nil, false
	case opLoadOrStore:
		return m.LoadOrStore(c.k, c.v)
	case opDelete:
		m.Delete(c.k)
		return nil, false
	default:
		panic("invalid mapOp")
	}
}

type mapResult struct {
	value interface{}
	ok    bool
}

func randValue(r *rand.Rand) string {
	b := make([]byte, r.Intn(4))
	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}
	return string(b)
}

func (mapCall) Generate(r *rand.Rand, size int) reflect.Value {
	c := mapCall{op: mapOps[rand.Intn(len(mapOps))], k: randValue(r)}
	switch c.op {
	case opStore, opLoadOrStore:
		c.v = randValue(r)
	}
	return reflect.ValueOf(c)
}

func applyCalls(m mapInterface[string], calls []mapCall) (results []mapResult, final map[interface{}]interface{}) {
	for _, c := range calls {
		v, ok := c.apply(m)
		results = append(results, mapResult{v, ok})
	}

	final = make(map[interface{}]interface{})
	m.Range(func(k string, v interface{}) bool {
		final[k] = v
		return true
	})

	return results, final
}

func TestConcurrentRange(t *testing.T) {
	const mapSize = 1 << 10

	m := new(syncmapt.Map[int64, int64])
	for n := int64(1); n <= mapSize; n++ {
		m.Store(n, n)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	defer func() {
		close(done)
		wg.Wait()
	}()
	for g := int64(runtime.GOMAXPROCS(0)); g > 0; g-- {
		r := rand.New(rand.NewSource(g))
		wg.Add(1)
		go func(g int64) {
			defer wg.Done()
			for i := int64(0); ; i++ {
				select {
				case <-done:
					return
				default:
				}
				for n := int64(1); n < mapSize; n++ {
					if r.Int63n(mapSize) == 0 {
						m.Store(n, n*i*g)
					} else {
						m.Load(n)
					}
				}
			}
		}(g)
	}

	iters := 1 << 10
	if testing.Short() {
		iters = 16
	}
	for n := iters; n > 0; n-- {
		seen := make(map[int64]bool, mapSize)

		m.Range(func(k, v int64) bool {
			if v%k != 0 {
				t.Fatalf("while Storing multiples of %v, Range saw value %v", k, v)
			}
			if seen[k] {
				t.Fatalf("Range visited key %v twice", k)
			}
			seen[k] = true
			return true
		})

		if len(seen) != mapSize {
			t.Fatalf("Range visited %v elements of %v-element Map", len(seen), mapSize)
		}
	}
}

func Test_Any(t *testing.T) {
	type AnyValue struct {
		v interface{}
	}

	m := new(syncmapt.Map[string, *AnyValue])

	m.Store("a", &AnyValue{
		v: "any",
	})

	v, ok := m.Load("a")
	if !ok {
		t.Fatal("load wrong")
	}

	if v == nil {
		t.Fatal("want *AnyValue not nil")
	}

	res, ok := v.v.(string)
	if !ok || res != "any" {
		t.Log("unexpected")
	}

	vb, ok := m.Load("b") // nil
	if ok {
		t.Fatal("want nil got", vb)
	}

	if vb != nil {
		t.Fatal("unexpected nil value")
	}
}

func Test_Len(t *testing.T) {
	m := new(syncmapt.Map[int, any])

	length := 10

	for i := 0; i < length; i++ {
		m.Store(i, nil)
	}

	if m.Len() != length {
		t.Fatal("unexpected")
	}

	m.Delete(0)
	m.Delete(1)
	m.Delete(2)

	if m.Len() != length-3 {
		t.Fatal("unexpected")
	}

	m.Delete(3)
	m.Delete(4)

	if m.Len() != length-5 {
		t.Fatal("unexpected", m.Len())
	}
}

func TestCustome(t *testing.T) {
	type Custome struct {
		Address    []string
		expireTime time.Time
	}

	m := new(syncmapt.Map[string, Custome])

	resolver := func() Custome {
		return Custome{
			Address:    []string{"ip1", "ip2"},
			expireTime: time.Now().Add(time.Second * 3),
		}
	}

	loader := func(srv string) {
		r := resolver()
		m.Store(srv, r)
	}

	loader("srvA")

	cust, exist := m.Load("srvA")
	if !exist {
		t.Fatal("wrong")
	}

	t.Log(cust.Address)
}
