package callsign

import (
	"regexp"
	"strconv"
	"sync"
	"testing"
)

func TestEdgeGenerateDeviceName_Format(t *testing.T) {
	name, err := GenerateDeviceName()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name == "" {
		t.Fatal("empty name returned")
	}

	re := regexp.MustCompile(`^([a-z]+)([a-z]+)(\d{3})$`)
	matches := re.FindStringSubmatch(name)
	if len(matches) != 4 {
		t.Fatalf("callsign %q did not match expected format <adj><noun><3digits>", name)
	}

	num, err := strconv.Atoi(matches[3])
	if err != nil {
		t.Fatalf("invalid digits in %q: %v", name, err)
	}
	if num < 100 || num > 999 {
		t.Fatalf("number out of range [100, 999]: %d", num)
	}
}

func TestEdgeGenerateDeviceName_Concurrent(t *testing.T) {
	const count = 50
	var wg sync.WaitGroup
	errCh := make(chan error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := GenerateDeviceName()
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent call failed: %v", err)
	}
}

func TestEdgeGenerateDeviceName_Randomness(t *testing.T) {
	names := make(map[string]bool)
	for i := 0; i < 20; i++ {
		name, err := GenerateDeviceName()
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
		names[name] = true
	}
	if len(names) < 2 {
		t.Errorf("expected variety in generated names, got %d unique across 20 calls", len(names))
	}
}
