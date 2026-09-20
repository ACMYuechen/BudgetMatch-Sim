package devrecords

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"syscall"
)

// Diagnostic contains allowlisted codes only, never server messages, SQL,
// account names, driver configuration, connection strings or credentials.
// It is diagnostic evidence, not permission to retry or change the target.
type Diagnostic struct {
	Code  string `json:"code"`
	Stage string `json:"stage"`
}

type diagnosedError struct {
	diagnostic Diagnostic
	message    string
}

func (e *diagnosedError) Error() string { return e.message }

func failure(stage, code, message string) error {
	// Do not retain or unwrap the original error: even formatting it with %+v
	// must not expose the pgx configuration or a server-supplied message.
	return &diagnosedError{diagnostic: Diagnostic{Code: code, Stage: stage}, message: message}
}

func diagnosticOf(err error) *Diagnostic {
	var diagnosed *diagnosedError
	if errors.As(err, &diagnosed) {
		copy := diagnosed.diagnostic
		return &copy
	}
	return nil
}

func databaseFailure(stage string, err error) error {
	code := "database_error"
	var sqlState interface{ SQLState() string }
	var networkError net.Error
	var certificateError *tls.CertificateVerificationError
	if prior := diagnosticOf(err); prior != nil {
		code = prior.Code
	} else if errors.As(err, &certificateError) {
		code = "tls_verification_failed"
	} else if errors.As(err, &sqlState) {
		// Use the driver's typed SQLState contract; never parse error text or
		// export arbitrary SQLSTATE values. No new direct driver dependency.
		switch sqlState.SQLState() {
		case "28P01", "28000":
			code = "authentication_failed"
		case "3D000":
			code = "database_not_found"
		case "42501":
			code = "database_permission_denied"
		case "53300", "57P03":
			code = "database_unavailable"
		case "57014":
			code = "query_canceled"
		}
	} else {
		switch {
		case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
			code = "network_permission_denied"
		case errors.Is(err, syscall.ECONNREFUSED):
			code = "connection_refused"
		case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
			code = "network_unreachable"
		case errors.Is(err, context.Canceled):
			code = "operation_canceled"
		case errors.As(err, &networkError) && networkError.Timeout():
			code = "operation_timeout"
		case errors.Is(err, context.DeadlineExceeded):
			code = "operation_timeout"
		}
	}
	return failure(stage, code, "database operation failed ("+code+"); connection and server details suppressed")
}
