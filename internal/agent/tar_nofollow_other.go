//go:build !unix

package agent

// tarNoFollow is 0 on non-Unix platforms; openTarFile still refuses existing
// symlinks via Lstat before OpenFile.
const tarNoFollow = 0
