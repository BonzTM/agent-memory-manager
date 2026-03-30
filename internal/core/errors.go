package core

import "errors"

var (
	ErrNotFound                  = errors.New("not found")
	ErrInvalidInput              = errors.New("invalid input")
	ErrInvalidScope              = errors.New("invalid scope")
	ErrInvalidType               = errors.New("invalid memory type")
	ErrInvalidMode               = errors.New("invalid recall mode")
	ErrInvalidStatus             = errors.New("invalid memory status")
	ErrExpansionRecursionBlocked = errors.New("expansion recursion blocked: delegation depth exceeds maximum")
)
