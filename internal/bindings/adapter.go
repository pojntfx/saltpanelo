package main

import (
	"C"

	"context"
	"unsafe"

	"errors"

	"github.com/mattn/go-pointer"
	"github.com/pojntfx/saltpanelo/internal/backends"
)

type CString = *C.char
type CError = *C.char
type CBool = C.char
type CInt = C.int

const (
	CBoolTrue  = 1
	CBoolFalse = 0
)

type SaltpaneloOnRequestCallResponse struct {
	Accept CBool
	Err    CError
}

//export SaltpaneloNewAdapter
func SaltpaneloNewAdapter(
	onRequestCall func(srcID, srcEmail, routeID, channelID CString) SaltpaneloOnRequestCallResponse,
	onCallDisconnected func(routeID CString) CError,
	onHandleCall func(routeID, raddr CString) CError,
	openURL func(url CString) CError,

	raddr,
	ahost CString,
	verbose CBool,
	timeout CInt,

	oidcIssuer,
	oidcClientID,
	oidcRedirectURL CString,
) unsafe.Pointer {
	return pointer.Save(
		backends.NewAdapter(
			func(ctx context.Context, srcID, srcEmail, routeID, channelID string) (bool, error) {
				rv := onRequestCall(C.CString(srcID), C.CString(srcEmail), C.CString(routeID), C.CString(channelID))

				err := C.GoString(rv.Err)
				if err == "" {
					return rv.Accept == CBoolTrue, nil
				}

				return rv.Accept == CBoolTrue, errors.New(err)
			},
			func(ctx context.Context, routeID string) error {
				err := C.GoString(onCallDisconnected(C.CString(routeID)))
				if err == "" {
					return nil
				}

				return errors.New(err)
			},
			func(ctx context.Context, routeID, raddr string) error {
				err := C.GoString(onHandleCall(C.CString(routeID), C.CString(raddr)))
				if err == "" {
					return nil
				}

				return errors.New(err)
			},
			func(url string) error {
				err := C.GoString(openURL(C.CString(url)))
				if err == "" {
					return nil
				}

				return errors.New(err)
			},

			C.GoString(raddr),
			C.GoString(ahost),
			verbose == CBoolTrue,
			int(timeout),

			C.GoString(oidcIssuer),
			C.GoString(oidcClientID),
			C.GoString(oidcRedirectURL),
		),
	)
}

//export SaltpaneloAdapterLogin
func SaltpaneloAdapterLogin(adapter unsafe.Pointer) CError {
	err := (pointer.Restore(adapter)).(*backends.Adapter).Login()
	if err != nil {
		return C.CString(err.Error())
	}

	return C.CString("")
}

//export SaltpaneloAdapterLink
func SaltpaneloAdapterLink(adapter unsafe.Pointer) CError {
	err := (pointer.Restore(adapter)).(*backends.Adapter).Link()
	if err != nil {
		return C.CString(err.Error())
	}

	return C.CString("")
}

//export SaltpaneloAdapterRequestCall
func SaltpaneloAdapterRequestCall(adapter unsafe.Pointer, email, channelID CString) (CBool, CError) {
	accept, err := (pointer.Restore(adapter)).(*backends.Adapter).RequestCall(C.GoString(email), C.GoString(channelID))
	if err != nil {
		return CBoolFalse, C.CString(err.Error())
	}

	if accept {
		return CBoolTrue, C.CString("")
	}

	return CBoolFalse, C.CString("")
}

func main() {}
