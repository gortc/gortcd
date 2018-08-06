// +build gofuzz

package ice

var c = new(Candidate)

func FuzzCandidate(data []byte) int {
	c.Reset()
	if err := ParseAttribute(data, c); err != nil {
		return 0
	}
	return 1
}
