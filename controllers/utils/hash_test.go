package utils

import (
	"testing"
)

func TestFnvHashTest(t *testing.T) {
	testCases := []struct {
		input          string
		expectedOutput uint32
	}{
		{"5m", 1625360775},
		{"10m", 473128587},
		{"1h", 1676385180},
	}

	for _, testCase := range testCases {
		actualOutput := FnvHash(testCase.input)
		if actualOutput != testCase.expectedOutput {
			t.Errorf("FnvHash(%s) = %d, expected %d", testCase.input, actualOutput, testCase.expectedOutput)
		}
	}
}
