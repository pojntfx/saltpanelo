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
	onRequestCallCallback C.on_request_call_callback,
	onRequestCallUserdata unsafe.Pointer,

	onCallDisconnectedCallback C.on_call_disconnected_callback,
	onCallDisconnectedUserdata unsafe.Pointer,

	onHandleCallCallback C.on_handle_call_callback,
	onHandleCallUserdata unsafe.Pointer,

	openURLCallback C.open_url_callback,
	openURLUserdata unsafe.Pointer,

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
			context.Background(),

			func(ctx context.Context, srcID, srcEmail, routeID, channelID string, userdata unsafe.Pointer) (bool, error) {
				rv := C.bridge_on_request_call(onRequestCallCallback, C.CString(srcID), C.CString(srcEmail), C.CString(routeID), C.CString(channelID), userdata)

				err := C.GoString(rv.Err)
				if err == "" {
					return rv.Accept == CBoolTrue, nil
				}

				return rv.Accept == CBoolTrue, errors.New(err)
			},
			onRequestCallUserdata,

			func(ctx context.Context, routeID string, userdata unsafe.Pointer) error {
				err := C.GoString(C.bridge_on_call_disconnected(onCallDisconnectedCallback, C.CString(routeID), userdata))
				if err == "" {
					return nil
				}

				return errors.New(err)
			},
			onCallDisconnectedUserdata,

			func(ctx context.Context, routeID, raddr string, userdata unsafe.Pointer) error {
				err := C.GoString(C.bridge_on_handle_call(onHandleCallCallback, C.CString(routeID), C.CString(raddr), userdata))
				if err == "" {
					return nil
				}

				return errors.New(err)
			},
			onHandleCallUserdata,

			func(url string, userdata unsafe.Pointer) error {
				err := C.GoString(C.bridge_open_url(openURLCallback, C.CString(url), userdata))
				if err == "" {
					return nil
				}

				return errors.New(err)
			},
			openURLUserdata,

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

//export SaltpaneloAdapterHangupCall
func SaltpaneloAdapterHangupCall(adapter unsafe.Pointer, routeID CString) CError {
	err := (pointer.Restore(adapter)).(*backends.Adapter).HangupCall(C.GoString(routeID))
	if err != nil {
		return C.CString(err.Error())
	}

	return C.CString("")
}

func main() {}
