//go:build !windows

package main

import "errors"

func registerSummonHotkey(func()) (func(), error) {
	return nil, errors.New("全局热键仅在 Windows 下可用")
}
