package bindings

import (
	"C"

	"context"
	"unsafe"

	"github.com/mattn/go-pointer"
	"github.com/pojntfx/saltpanelo/internal/backends"
)
import "errors"

type CString = *C.char
type CError = *C.char
type CBool = C.char
type CInt = C.int

const (
	CBoolFalse = 0
	CBoolTrue  = 1
)

func NewAdapter(
	onRequestCall func(srcID, srcEmail, routeID, channelID CString) (CBool, CError),
	onCallDisconnected func(routeID CString) CError,
	onHandleCall func(routeID, raddr CString) CError,
	openURL func(url CString) CError,

	raddr,
	ahost CString,
	verbose CBool,
	timeout CInt,

	oidcIssuer,
	oidcClientID,
	oidcRedirectURL string,
) unsafe.Pointer {
	return pointer.Save(
		backends.NewAdapter(
			func(ctx context.Context, srcID, srcEmail, routeID, channelID string) (bool, error) {
				accept, e := onRequestCall(C.CString(srcID), C.CString(srcEmail), C.CString(routeID), C.CString(channelID))

				err := C.GoString(e)
				if err == "" {
					return accept == CBoolTrue, nil
				}

				return accept == CBoolTrue, errors.New(err)
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

			oidcIssuer,
			oidcClientID,
			oidcRedirectURL,
		),
	)
}

func SaltpaneloAdapterLogin(adapter unsafe.Pointer) CError {
	err := (pointer.Restore(adapter)).(*backends.Adapter).Login()
	if err != nil {
		return C.CString(err.Error())
	}

	return C.CString("")
}

func SaltpaneloAdapterLink(adapter unsafe.Pointer) CError {
	err := (pointer.Restore(adapter)).(*backends.Adapter).Link()
	if err != nil {
		return C.CString(err.Error())
	}

	return C.CString("")
}

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
