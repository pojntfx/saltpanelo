package main

/*
#include "adapter.h"
*/
import "C"

import (
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

//export SaltpaneloNewAdapter
func SaltpaneloNewAdapter(
	onRequestCall C.on_request_call,
	onCallDisconnected C.on_call_disconnected,
	onHandleCall C.on_handle_call,
	openURL C.open_url,

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
				rv := C.struct_SaltpaneloOnRequestCallResponse{}

				C.bridge_on_request_call(onRequestCall, C.CString(srcID), C.CString(srcEmail), C.CString(routeID), C.CString(channelID), &rv)

				err := C.GoString(rv.Err)
				if err == "" {
					return rv.Accept == CBoolTrue, nil
				}

				return rv.Accept == CBoolTrue, errors.New(err)
			},
			func(ctx context.Context, routeID string) error {
				rv := C.CString("")

				C.bridge_on_call_disconnected(onCallDisconnected, C.CString(routeID), &rv)

				err := C.GoString(rv)
				if err == "" {
					return nil
				}

				return errors.New(err)
			},
			func(ctx context.Context, routeID, raddr string) error {
				rv := C.CString("")

				C.bridge_on_handle_call(onHandleCall, C.CString(routeID), C.CString(raddr), &rv)

				err := C.GoString(rv)
				if err == "" {
					return nil
				}

				return errors.New(err)
			},
			func(url string) error {
				rv := C.CString("")

				C.bridge_open_url(openURL, C.CString(url), &rv)

				err := C.GoString(rv)
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
