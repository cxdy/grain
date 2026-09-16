//go:build !unix

package netutil

func errnoHostUnreachable(err error) bool {
	return false
}
