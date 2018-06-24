package ice

import (
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/gortc/sdp"
)

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

func TestConnectionAddress(t *testing.T) {
	data := loadData(t, "candidates_ex1.sdp")
	s, err := sdp.DecodeSession(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range s {
		p := candidateParser{
			c:   new(Candidate),
			buf: c.Value,
		}
		if err = p.parse(); err != nil {
			t.Fatal(err)
		}
	}

	// a=candidate:3862931549 1 udp 2113937151 192.168.220.128 56032
	//     foundation ---┘    |  |      |            |          |
	//   component id --------┘  |      |            |          |
	//      transport -----------┘      |            |          |
	//       priority ------------------┘            |          |
	//  conn. address -------------------------------┘          |
	//           port ------------------------------------------┘
}

func TestParse(t *testing.T) {
	data := loadData(t, "candidates_ex1.sdp")
	s, err := sdp.DecodeSession(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := []Candidate{
		{
			Foundation:  3862931549,
			ComponentID: 1,
			Priority:    2113937151,
			ConnectionAddress: ConnectionAddress{
				IP: net.ParseIP("192.168.220.128"),
			},
			Port:        56032,
			Type:        CandidateHost,
			NetworkCost: 50,
			Attributes: Attributes{
				Attribute{
					Key:   []byte("alpha"),
					Value: []byte("beta"),
				},
			},
		},
	}
	tCases := []struct {
		input    []byte
		expected Candidate
	}{
		{s[0].Value, expected[0]}, // 0
	}

	for i, c := range tCases {
		parser := candidateParser{
			buf: c.input,
			c:   new(Candidate),
		}
		if err := parser.parse(); err != nil {
			t.Errorf("[%d]: unexpected error %s",
				i, err,
			)
		}
		if !c.expected.Equal(parser.c) {
			t.Errorf("[%d]: %v != %v (exp)",
				i, parser.c, c.expected,
			)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	data := loadData(b, "candidates_ex1.sdp")
	s, err := sdp.DecodeSession(data, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	value := s[0].Value
	p := candidateParser{
		c: new(Candidate),
	}
	for i := 0; i < b.N; i++ {
		p.buf = value
		if err = p.parse(); err != nil {
			b.Fatal(err)
		}
		p.c.reset()
	}
}

func BenchmarkParseIP(b *testing.B) {
	v := []byte("127.0.0.2")
	var (
		result = make([]byte, net.IPv4len)
	)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result = parseIP(result, v)
		result = result[:net.IPv4len]
	}
}
