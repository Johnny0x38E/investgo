package hot

import "testing"

func TestParseSinaTrade(t *testing.T) {
	value, err := parseSinaTrade(" 16.88 ")
	if err != nil || value != 16.88 {
		t.Fatalf("parseSinaTrade() = %v, %v; want 16.88, nil", value, err)
	}

	if _, err := parseSinaTrade("not-a-price"); err == nil {
		t.Fatal("malformed trade returned nil error")
	}
	if _, err := parseSinaTrade("NaN"); err == nil {
		t.Fatal("NaN trade returned nil error")
	}
}
