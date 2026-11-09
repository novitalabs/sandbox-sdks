package code_interpreter

import (
	"io"
	"time"
)

// nowNanos returns the current Unix time in nanoseconds.
//
// The JS SDK derives this from a millisecond clock, so sub-millisecond
// precision is not meaningful here either.
func nowNanos() int64 {
	return time.Now().UnixNano()
}

// ClosableBody is an http response body whose Close releases the request's
// context, so timeouts do not leak when a caller is done reading early.
type ClosableBody interface {
	io.ReadCloser
}

// cancelOnCloseBody releases a request context when the body is closed.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel func()
}

// Close closes the underlying body and releases the context.
func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}
