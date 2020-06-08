package errors

import (
	"fmt"
	"testing"

	pkgerrors "github.com/pkg/errors"
)

func TestUserErrorDetection(t *testing.T) {
	testCases := []struct {
		input          error
		expectedResult bool
	}{
		{
			input:          fmt.Errorf("not a user error"),
			expectedResult: false,
		},
		{
			input:          UserError("you did this"),
			expectedResult: true,
		},
		{
			input:          pkgerrors.Wrap(UserError("you did this"), "some context"),
			expectedResult: true,
		},
		{
			input:          pkgerrors.Wrap(pkgerrors.Wrap(UserError("you did this"), "some context"), "more context"),
			expectedResult: true,
		},
		{
			input:          pkgerrors.Wrap(fmt.Errorf("not a user error"), "some context"),
			expectedResult: false,
		},
	}
	for _, testCase := range testCases {
		outputResult := IsUserError(testCase.input)
		if outputResult != testCase.expectedResult {
			t.Fatalf("Incorrect user error detection, input (%v), expected: (%v)", testCase.input, testCase.expectedResult)
		}
	}
}
