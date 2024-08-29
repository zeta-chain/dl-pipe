package dlpipe

import (
	"errors"
	"fmt"
)

var ErrRetryParametersExceeded = errors.New("retry parameters exceeded")

type ErrHashMismatch struct {
	ExpectedHash []byte
	GivenHash    []byte
}

func (e ErrHashMismatch) Error() string {
	return fmt.Sprintf("hash mismatch: expected %x, got %x", e.ExpectedHash, e.GivenHash)
}

type ErrNonRetryable struct {
	inner error
}

func (e ErrNonRetryable) Error() string {
	return e.inner.Error()
}

func (e ErrNonRetryable) Unwrap() error {
	return e.inner
}

func NonRetryableWrap(err error) error {
	return ErrNonRetryable{inner: err}
}

func NonRetryableWrapf(format string, args ...interface{}) error {
	return ErrNonRetryable{inner: fmt.Errorf(format, args...)}
}
