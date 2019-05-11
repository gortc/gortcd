package ice

// Role represents ICE agent role, which can be controlling or controlled.
type Role byte

// Possible ICE agent roles.
const (
	Controlling Role = iota
	Controlled
)
