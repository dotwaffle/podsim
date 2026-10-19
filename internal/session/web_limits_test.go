package session

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestWebStreamMirrorsLimits checks that web/stream.js has the limits of
// the stream codec: the inflated JSON limit and the gzip allowance.
func TestWebStreamMirrorsLimits(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("../../web/stream.js")
	if err != nil {
		t.Fatal(err)
	}
	if MaxStreamJSON%(1<<20) != 0 {
		t.Fatalf("MaxStreamJSON = %d is not a whole number of MiB", MaxStreamJSON)
	}
	// The expression is the formula of streamGzipOverhead.
	if overhead := 32*((MaxStreamJSON+65534)/65535) + 32; MaxStreamMessage != MaxStreamJSON+overhead {
		t.Fatalf("MaxStreamMessage = %d, want %d", MaxStreamMessage, MaxStreamJSON+overhead)
	}
	for _, want := range []string{
		fmt.Sprintf("const jsonLimit = %d * 1024 * 1024;", MaxStreamJSON>>20),
		"const gzipLimit = jsonLimit + 32 * Math.ceil(jsonLimit / 65535) + 32;",
	} {
		if !strings.Contains(string(source), want) {
			t.Errorf("web/stream.js does not have %s", want)
		}
	}
}
