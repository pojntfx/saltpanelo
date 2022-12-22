package utils

import (
	"io"
	"log"
	"math/rand"
	"net"
	"time"
)

func HandleTestConn(verbose bool, conn net.Conn) error {
	if verbose {
		log.Println("Starting throughput test")
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	errs := make(chan error)

	go func() {
		if _, err := io.Copy(conn, r); err != nil {
			errs <- err

			return
		}
	}()

	go func() {
		if _, err := io.Copy(io.Discard, conn); err != nil {
			errs <- err

			return
		}
	}()

	return <-errs
}
