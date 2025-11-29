package internal

import (
	"context"
	"database/sql"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// SQLExecutor is an interface that both *sql.DB and *sql.Tx implement
type SQLExecutor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// SQLTracer provides methods for tracing SQL operations
type SQLTracer struct {
	tracer trace.Tracer
}

// NewSQLTracer creates a new SQL tracer with the given tracer name
func NewSQLTracer(tracerName string) *SQLTracer {
	return &SQLTracer{
		tracer: otel.Tracer(tracerName),
	}
}

// ExecWithSpan executes a SQL command with automatic tracing
func (st *SQLTracer) ExecWithSpan(ctx context.Context, spanName string, attrs []attribute.KeyValue, exec SQLExecutor, query string, args ...interface{}) (sql.Result, error) {
	ctx, span := st.tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
	defer span.End()

	result, err := exec.ExecContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetStatus(codes.Ok, "success")
	return result, nil
}

// QueryWithSpan executes a SQL query with automatic tracing
func (st *SQLTracer) QueryWithSpan(ctx context.Context, spanName string, attrs []attribute.KeyValue, exec SQLExecutor, query string, args ...interface{}) (*sql.Rows, error) {
	ctx, span := st.tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
	defer span.End()

	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetStatus(codes.Ok, "success")
	return rows, nil
}

// QueryRowResult wraps a sql.Row with span handling
type QueryRowResult struct {
	row  *sql.Row
	span trace.Span
}

// Scan scans the row and updates the span based on the result
// sql.ErrNoRows is treated as a successful "not found" case, not an error
func (qr *QueryRowResult) Scan(dest ...interface{}) error {
	err := qr.row.Scan(dest...)
	if err == sql.ErrNoRows || errors.Is(err, sql.ErrNoRows) {
		qr.span.SetStatus(codes.Ok, "no rows found")
	} else if err != nil {
		qr.span.RecordError(err)
		qr.span.SetStatus(codes.Error, err.Error())
	} else {
		qr.span.SetStatus(codes.Ok, "success")
	}
	qr.span.End()
	return err
}

// QueryRowWithSpan executes a SQL query that returns a single row with automatic tracing
func (st *SQLTracer) QueryRowWithSpan(ctx context.Context, spanName string, attrs []attribute.KeyValue, exec SQLExecutor, query string, args ...interface{}) *QueryRowResult {
	ctx, span := st.tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
	row := exec.QueryRowContext(ctx, query, args...)
	return &QueryRowResult{row: row, span: span}
}

