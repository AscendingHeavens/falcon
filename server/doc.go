// Package server holds the types a Falcon handler works with: the [Context] for each
// request, the [Response] it returns, errors, validation, templates and the router.
//
// The falcon package makes the ones you use day to day available under the same names,
// such as falcon.Context and falcon.Response, so most apps never import this package. It's
// needed for [NewValidator], to build a custom validator that reports fields by their json
// names, and when writing middleware outside the falcon package.
//
// A Context is reused for later requests once its handler returns. A goroutine that
// outlives the handler must use [Context.Copy].
package server
