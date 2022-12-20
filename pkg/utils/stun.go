package utils

import (
	"net"

	"github.com/pion/stun"
)

func GetPublicIP(stunAddr string) (net.IP, error) {
	c, err := stun.Dial("udp", stunAddr)
	if err != nil {
		return nil, err
	}

	errChan := make(chan error)
	resChan := make(chan net.IP)

	go func() {
		if err := c.Do(stun.MustBuild(stun.TransactionID, stun.BindingRequest), func(e stun.Event) {
			if e.Error != nil {
				errChan <- err

				return
			}

			var addr stun.XORMappedAddress
			if err := addr.GetFrom(e.Message); err != nil {
				errChan <- err

				return
			}

			resChan <- addr.IP
		}); err != nil {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return nil, err
	case r := <-resChan:
		return r, nil
	}
}
