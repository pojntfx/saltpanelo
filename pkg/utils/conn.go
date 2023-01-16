package utils

import (
	"io"
	"log"
	"math/rand"
	"net"
	"time"
)

func HandleTestConn(verbose bool, conn net.Conn, benchmarkLimit int64) error {
	if verbose {
		log.Println("Handling throughput or latency test")
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	errs := make(chan error)

	go func() {
		if _, err := io.CopyN(conn, r, benchmarkLimit); err != nil {
			errs <- err

			return
		}
	}()

	go func() {
		if _, err := io.CopyN(io.Discard, conn, benchmarkLimit); err != nil {
			errs <- err

			return
		}
	}()

	return <-errs
}
