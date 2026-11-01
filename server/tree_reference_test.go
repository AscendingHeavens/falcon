package server

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// referenceMatch is a slow, obvious matcher to check the tree against. It tests every
// route, and among those that match picks the best by comparing segment by segment:
// static beats param, which beats catch-all.
func referenceMatch(routes []string, path string) (string, Params) {
	segments := strings.Split(path, "/")

	best := ""
	var bestParams Params
	var bestRank []int

	for _, route := range routes {
		params, rank, ok := matchRoute(strings.Split(route, "/"), segments)
		if ok && (best == "" || lessRank(rank, bestRank)) {
			best, bestParams, bestRank = route, params, rank
		}
	}
	return best, bestParams
}

func matchRoute(route, path []string) (Params, []int, bool) {
	var params Params
	var rank []int

	for i, part := range route {
		if strings.HasPrefix(part, "*") {
			if i >= len(path) {
				return nil, nil, false
			}
			params = append(params, Param{part[1:], strings.Join(path[i:], "/")})
			return params, append(rank, 2), true
		}
		if i >= len(path) {
			return nil, nil, false
		}
		if strings.HasPrefix(part, ":") {
			if path[i] == "" {
				return nil, nil, false
			}
			params = append(params, Param{part[1:], path[i]})
			rank = append(rank, 1)
			continue
		}
		if part != path[i] {
			return nil, nil, false
		}
		rank = append(rank, 0)
	}

	return params, rank, len(route) == len(path)
}

func lessRank(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func TestRouter_AgreesWithReferenceMatcher(t *testing.T) {
	rng := rand.New(rand.NewSource(1))

	// Short and long segments with shared prefixes, around the 8-byte comparison boundary.
	static := []string{"a", "ab", "abc", "abcdefg", "abcdefgh", "abcdefghi", "abcdefgx", "abcdefghijklmnopq", "abcdefghijklmnopx", "users", "uploads"}
	pathOnly := []string{"", "x", "abcd", "abcdefghijklmnop", "abcdefghijklmnopqr", "user", "usersx"}

	randomRoute := func() string {
		var b strings.Builder
		depth := 1 + rng.Intn(4)
		for i := range depth {
			b.WriteByte('/')
			switch n := rng.Intn(10); {
			case n < 6:
				b.WriteString(static[rng.Intn(len(static))])
			case n < 9:
				fmt.Fprintf(&b, ":p%d", i) // Named by position, so that names never conflict
			case i == depth-1:
				fmt.Fprintf(&b, "*c%d", i)
			default:
				b.WriteString(static[rng.Intn(len(static))])
			}
		}
		return b.String()
	}

	randomPath := func() string {
		var b strings.Builder
		for range 1 + rng.Intn(5) {
			b.WriteByte('/')
			if rng.Intn(4) == 0 {
				b.WriteString(pathOnly[rng.Intn(len(pathOnly))])
			} else {
				b.WriteString(static[rng.Intn(len(static))])
			}
		}
		return b.String()
	}

	matched := 0
	for range 300 {
		router := NewRouter()
		seen := map[string]bool{}
		var routes []string
		for range 1 + rng.Intn(30) {
			route := randomRoute()
			if !seen[route] {
				seen[route] = true
				routes = append(routes, route)
				router.Handle("GET", route, func(c *Context) *Response {
					return &Response{Message: route}
				})
			}
		}

		for range 100 {
			path := randomPath()
			wantRoute, wantParams := referenceMatch(routes, path)

			var params Params
			handler := router.FindHandler("GET", path, &params)

			if wantRoute == "" {
				assert.Nil(t, handler, "path %q in %v", path, routes)
				assert.Empty(t, params, "path %q in %v: no params may be left behind", path, routes)
				continue
			}

			matched++
			if assert.NotNil(t, handler, "path %q should match %q in %v", path, wantRoute, routes) {
				assert.Equal(t, wantRoute, handler(nil).Message, "path %q in %v", path, routes)
			}
			assert.Equal(t, wantParams, params, "path %q in %v", path, routes)
		}
	}

	assert.Greater(t, matched, 1000, "the test must actually exercise matches")
}

func TestRouter_LongPrefixesCompareEveryByte(t *testing.T) {
	// Lengths around the 8-byte comparison: just under, on, and just over each boundary.
	for _, length := range []int{1, 7, 8, 9, 15, 16, 17, 24, 25, 100} {
		route := "/" + strings.Repeat("a", length-1) + "z"

		r := NewRouter()
		r.Handle("GET", route, func(c *Context) *Response { return nil })

		var params Params
		assert.NotNil(t, r.FindHandler("GET", route, &params), "exact match of length %d", length)

		for i := 1; i < len(route); i++ {
			wrong := []byte(route)
			wrong[i] = '!'
			assert.Nil(t, r.FindHandler("GET", string(wrong), &params), "byte %d of %d changed", i, len(route))
		}

		assert.Nil(t, r.FindHandler("GET", route[:len(route)-1], &params), "one byte short")
		assert.Nil(t, r.FindHandler("GET", route+"z", &params), "one byte long")
	}
}

func TestRouter_FailedWalkLeavesNoParamsBehind(t *testing.T) {
	r := NewRouter()
	noop := func(c *Context) *Response { return nil }
	r.Handle("GET", "/a/:x/:w/end", noop)
	r.Handle("GET", "/:y/*rest", noop)
	r.Handle("GET", "/deep/:one/:two/:three/stop", noop)

	t.Run("params from the failed branch are gone after backtracking", func(t *testing.T) {
		var params Params
		assert.NotNil(t, r.FindHandler("GET", "/a/1/2/nope", &params))
		assert.Equal(t, Params{{"y", "a"}, {"rest", "1/2/nope"}}, params)
	})

	t.Run("params already there survive a failed lookup", func(t *testing.T) {
		other := NewRouter()
		other.Handle("GET", "/deep/:one/:two/stop", noop)

		params := Params{{"existing", "1"}}
		assert.Nil(t, other.FindHandler("GET", "/deep/1/2/nope", &params))
		assert.Equal(t, Params{{"existing", "1"}}, params)
	})
}

func TestSameString(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"GET", "GET", true},
		{"GET", "PUT", false},
		{"GET", "GE", false},
		{"", "GET", false},
		{"DELETE", "DELETF", false},
	} {
		assert.Equal(t, tt.want, sameString(tt.a, tt.b), "%q and %q", tt.a, tt.b)
	}
}
