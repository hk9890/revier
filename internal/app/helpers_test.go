package app_test

import (
	"os"
	"testing"

	"github.com/hk9890/revier/internal/testenv"
)

// TestMain keeps every test off the user's own revier files (testenv.Run).
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}
