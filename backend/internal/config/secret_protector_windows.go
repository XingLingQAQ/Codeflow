//go:build windows

package config

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func secretProtectorAvailable() error { return nil }

func protectSecret(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("secret is empty")
	}
	in := windows.DataBlob{Size: uint32(len(plaintext)), Data: &plaintext[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func unprotectSecret(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, errors.New("ciphertext is empty")
	}
	in := windows.DataBlob{Size: uint32(len(ciphertext)), Data: &ciphertext[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
