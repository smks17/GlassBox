package systemd

import "errors"

var (
	ErrMissingExecStart   = errors.New("service is missing ExecStart")
	ErrUnknownExecutable  = errors.New("unknown executable")
	ErrInvalidServiceName = errors.New("invalid service name")
	ErrJournalTooLarge    = errors.New("journal exceeds its size limit")
)
