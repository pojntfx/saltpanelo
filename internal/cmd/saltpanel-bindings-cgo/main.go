package main

import (
	"C"

	"bufio"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/pojntfx/saltpanelo/internal/bindings"
)

func main() {
	raddr := flag.String("raddr", "ws://localhost:1338", "Gateway remote address")
	ahost := flag.String("ahost", "127.0.0.1", "Host to bind to when receiving calls")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	timeout := flag.Int("timeout", 1000, "Milliseconds after which to assume that a call has timed out")

	oidcIssuer := flag.String("oidc-issuer", "", "OIDC issuer (e.g. https://pojntfx.eu.auth0.com/)")
	oidcClientID := flag.String("oidc-client-id", "", "OIDC client ID")
	oidcRedirectURL := flag.String("oidc-redirect-url", "http://localhost:11337", "OIDC redirect URL")

	flag.Parse()

	adapter := bindings.NewAdapter(
		func(srcID, srcEmail, routeID, channelID bindings.CString) (bindings.CBool, bindings.CError) {
			log.Printf("Call with src ID %v, src email %v, route ID %v and channel ID %v requested and accepted", srcID, srcEmail, routeID, channelID)

			return bindings.CBoolTrue, bindings.CString(C.CString(""))
		},
		func(routeID bindings.CString) bindings.CError {
			log.Println("Call with route ID", routeID, "disconnected")

			return bindings.CString(C.CString(""))
		},
		func(routeID, raddr bindings.CString) bindings.CError {
			log.Println("Call with route ID", routeID, "and remote address", raddr, "started")

			return bindings.CString(C.CString(""))
		},
		func(url bindings.CString) bindings.CError {
			log.Println("Open the following URL in your browser:", url)

			return bindings.CString(C.CString(""))
		},

		bindings.CString(C.CString(*raddr)),
		bindings.CString(C.CString(*ahost)),
		func() bindings.CBool {
			if *verbose {
				return bindings.CBoolTrue
			}

			return bindings.CBoolFalse
		}(),
		bindings.CInt(*timeout),

		*oidcIssuer,
		*oidcClientID,
		*oidcRedirectURL,
	)

	if e := bindings.SaltpaneloAdapterLogin(adapter); C.GoString((*C.char)(e)) != "" {
		panic(e)
	}

	go func() {
		if e := bindings.SaltpaneloAdapterLink(adapter); C.GoString((*C.char)(e)) != "" {
			panic(e)
		}
	}()

	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Email to call: ")

		email, err := r.ReadString('\n')
		if err != nil {
			panic(err)
		}

		fmt.Print("Channel ID to call: ")

		channelID, err := r.ReadString('\n')
		if err != nil {
			panic(err)
		}

		accepted, e := bindings.SaltpaneloAdapterRequestCall(adapter, bindings.CString(C.CString(email)), bindings.CString(C.CString(channelID)))
		if C.GoString((*C.char)(e)) != "" {
			panic(err)
		}

		if accepted == bindings.CBoolTrue {
			log.Println("Callee accepted the call")
		} else {
			log.Println("Callee denied the call")
		}
	}
}
