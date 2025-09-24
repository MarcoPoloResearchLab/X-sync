// internal/xresolver/logging.go
package xresolver

import "context"

type logFunction func(format string, args ...any)

type logFunctionContextKey struct{}

func withLogFunction(ctx context.Context, printer logFunction) context.Context {
	if printer == nil {
		return ctx
	}
	return context.WithValue(ctx, logFunctionContextKey{}, printer)
}

func logFunctionFromContext(ctx context.Context) logFunction {
	if ctx == nil {
		return nil
	}
	if printer, ok := ctx.Value(logFunctionContextKey{}).(logFunction); ok {
		return printer
	}
	return nil
}
