package utils

import (
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
)

func IsClosedErr(err any) bool {
	e, ok := err.(error)
	if !ok {
		return false
	}

	if errors.Is(e, io.EOF) || errors.Is(e, net.ErrClosed) || errors.Is(e, syscall.ECONNRESET) || strings.HasSuffix(e.Error(), "read: connection timed out") || strings.HasSuffix(e.Error(), "write: broken pipe") || strings.HasSuffix(e.Error(), "unexpected EOF") {
		return true
	}

	return false
}
