package ice

import "testing"

func TestGather(t *testing.T) {
	_, err := Gather()
	if err != nil {
		t.Fatal(err)
	}
}
