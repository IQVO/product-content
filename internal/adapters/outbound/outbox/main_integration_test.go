//go:build integration

package outbox_test

import (
	"os"
	"testing"

	"github.com/IQVO/product-content/internal/testing/kafkatest"
	"github.com/IQVO/product-content/internal/testing/pgtest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	pgtest.Shutdown()
	kafkatest.Shutdown()
	os.Exit(code)
}
