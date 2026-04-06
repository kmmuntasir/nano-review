package api

import "errors"

var (
	ErrInvalidPayload = errors.New("invalid review payload")
	ErrUnauthorized   = errors.New("invalid or missing webhook secret")
	ErrCloneFailed    = errors.New("git clone failed")
)
