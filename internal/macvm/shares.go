package macvm

// Share is an explicit folder grant supplied by the human-owned native registry.
type Share struct {
	Name     string
	Path     string
	ReadOnly bool
}
