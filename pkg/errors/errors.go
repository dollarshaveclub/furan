package errors

import "github.com/pkg/errors"

type user interface {
	User() bool
}

// UserError is an error caused by a user. This is supposed to represent both
// unintentional errors (like invalid input) and intentional "errors" (like
// cancellation).
type UserError string

// User returns true if the error is caused by the user
func (e UserError) User() bool { return true }

// Error stringifies the user error
func (e UserError) Error() string { return "user error: " + string(e) }

// IsUserError returns true if err is caused by the user
func IsUserError(err error) bool {
	e, ok := errors.Cause(err).(user)
	if ok {
		return e.User()
	}
	return false
}
