package turn

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/gortc/stun"
)

const allocRuns = 10

// wasAllocs returns true if f allocates memory.
func wasAllocs(f func()) bool {
	return testing.AllocsPerRun(allocRuns, f) > 0
}

func TestBadAttrLength_Error(t *testing.T) {
	b := &BadAttrLength{
		Attr:     stun.AttrData,
		Expected: 100,
		Got:      11,
	}
	if b.Error() != "incorrect length for DATA: got 11, expected 100" {
		t.Error("Bad value", b.Error())
	}
}

func loadData(tb testing.TB, name string) []byte {
	name = filepath.Join("testdata", name)
	f, err := os.Open(name)
	if err != nil {
		tb.Fatal(err)
	}
	defer func() {
		if errClose := f.Close(); errClose != nil {
			tb.Fatal(errClose)
		}
	}()
	v, err := ioutil.ReadAll(f)
	if err != nil {
		tb.Fatal(err)
	}
	return v
}
