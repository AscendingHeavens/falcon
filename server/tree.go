package server

import (
	"fmt"
	"strings"
)

// node is a node of a method's radix tree. Static text is stored compressed, so
// "/users" and "/uploads" share a "/u" node. Params and catch-alls are kept apart
// from static children, which gives the matching priority: static > param > catch-all.
type node struct {
	prefix   string  // Static text this node matches; empty for param and catch-all nodes
	indices  string  // First byte of each child's prefix, in the same order as children
	children []*node // Static children
	param    *node   // Child matching one whole segment (":id")
	catchAll *node   // Child matching the rest of the path ("*filepath")

	paramName string // Name of the param, on param and catch-all nodes
	pattern   string // Route that created this param or catch-all node, for conflict messages
	route     *Route // Route ending at this node, if any
}

// insert adds a route to the tree. It panics if the route conflicts with an existing one.
func (n *node) insert(rt *Route) {
	path := rt.Path
	for len(path) > 0 {
		if i := wildcardIndex(path); i > 0 {
			n = n.insertStatic(path[:i])
			path = path[i:]
			continue
		}

		end := strings.IndexByte(path, '/')
		if end < 0 {
			end = len(path)
		}
		name := path[1:end]
		if name == "" {
			panic(fmt.Sprintf("falcon: route %q has a %q with no name", routeName(rt), path[:1]))
		}

		if path[0] == '*' {
			if end != len(path) {
				panic(fmt.Sprintf("falcon: route %q has a catch-all that isn't its last segment", routeName(rt)))
			}
			if n.catchAll != nil {
				panic(fmt.Sprintf("falcon: route %q conflicts with %q: both catch everything at the same position",
					routeName(rt), rt.Method+" "+n.catchAll.pattern))
			}
			n.catchAll = &node{paramName: name, pattern: rt.Path}
			n = n.catchAll
			break
		}

		if n.param == nil {
			n.param = &node{paramName: name, pattern: rt.Path}
		} else if n.param.paramName != name {
			panic(fmt.Sprintf("falcon: route %q conflicts with %q: a param can't be named both :%s and :%s at the same position",
				routeName(rt), rt.Method+" "+n.param.pattern, name, n.param.paramName))
		}
		n = n.param
		path = path[end:]
	}

	if n.route != nil {
		panic(fmt.Sprintf("falcon: route %q is registered twice", routeName(rt)))
	}
	n.route = rt
}

// insertStatic adds static text below n, splitting nodes where prefixes diverge,
// and returns the node where the text ends.
func (n *node) insertStatic(text string) *node {
	for {
		common := commonPrefix(n.prefix, text)

		if common < len(n.prefix) {
			// Everything n matched so far moves down into a new child.
			child := *n
			child.prefix = n.prefix[common:]
			*n = node{
				prefix:   n.prefix[:common],
				indices:  child.prefix[:1],
				children: []*node{&child},
			}
		}

		text = text[common:]
		if text == "" {
			return n
		}

		i := strings.IndexByte(n.indices, text[0])
		if i < 0 {
			child := &node{prefix: text}
			n.indices += text[:1]
			n.children = append(n.children, child)
			return child
		}
		n = n.children[i]
	}
}

// find returns the route matching path and appends its params to params.
// If a static branch turns out to be a dead end it falls back to the param and then
// the catch-all branch, undoing any params captured along the way. It doesn't allocate
// as long as params has enough capacity.
func (n *node) find(path string, params *Params) *Route {
	if len(path) < len(n.prefix) || path[:len(n.prefix)] != n.prefix {
		return nil
	}
	path = path[len(n.prefix):]

	if path == "" {
		if n.route != nil {
			return n.route
		}
		if n.catchAll != nil {
			*params = append(*params, Param{Key: n.catchAll.paramName})
			return n.catchAll.route
		}
		return nil
	}

	// indices is scanned by hand because it's short and this is the hottest loop in the router.
	for i := 0; i < len(n.indices); i++ {
		if n.indices[i] == path[0] {
			if rt := n.children[i].find(path, params); rt != nil {
				return rt
			}
			break
		}
	}

	if n.param != nil {
		end := strings.IndexByte(path, '/')
		if end < 0 {
			end = len(path)
		}
		// A param never matches an empty segment.
		if end > 0 {
			mark := len(*params)
			*params = append(*params, Param{Key: n.param.paramName, Value: path[:end]})
			if rt := n.param.find(path[end:], params); rt != nil {
				return rt
			}
			*params = (*params)[:mark]
		}
	}

	if n.catchAll != nil {
		*params = append(*params, Param{Key: n.catchAll.paramName, Value: path})
		return n.catchAll.route
	}
	return nil
}

// wildcardIndex returns the index of the first ':' or '*' that starts a segment,
// or len(path) if there is none. Elsewhere those characters are plain text.
func wildcardIndex(path string) int {
	if path[0] == ':' || path[0] == '*' {
		return 0
	}
	for i := 1; i < len(path); i++ {
		if (path[i] == ':' || path[i] == '*') && path[i-1] == '/' {
			return i
		}
	}
	return len(path)
}

// commonPrefix returns the length of the prefix shared by a and b.
func commonPrefix(a, b string) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return i
}

// routeName formats a route for panic messages, e.g. "GET /users/:id".
func routeName(rt *Route) string {
	return rt.Method + " " + rt.Path
}
