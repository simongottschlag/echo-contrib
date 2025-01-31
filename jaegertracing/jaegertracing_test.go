package jaegertracing

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"github.com/stretchr/testify/assert"
)

// Mock opentracing.Span
type mockSpan struct {
	tracer   opentracing.Tracer
	tags     map[string]interface{}
	opName   string
	finished bool
}

func createSpan(tracer opentracing.Tracer) *mockSpan {
	return &mockSpan{
		tracer: tracer,
		tags:   make(map[string]interface{}),
	}
}

func (sp *mockSpan) isFinished() bool {
	return sp.finished
}

func (sp *mockSpan) getOpName() string {
	return sp.opName
}

func (sp *mockSpan) getTag(key string) interface{} {
	return sp.tags[key]
}

func (sp *mockSpan) Finish() {
	sp.finished = true
}
func (sp *mockSpan) FinishWithOptions(opts opentracing.FinishOptions) {
}
func (sp *mockSpan) Context() opentracing.SpanContext {
	return nil
}
func (sp *mockSpan) SetOperationName(operationName string) opentracing.Span {
	sp.opName = operationName
	return sp
}
func (sp *mockSpan) SetTag(key string, value interface{}) opentracing.Span {
	sp.tags[key] = value
	return sp
}
func (sp *mockSpan) LogFields(fields ...log.Field) {
}
func (sp *mockSpan) LogKV(alternatingKeyValues ...interface{}) {
}
func (sp *mockSpan) SetBaggageItem(restrictedKey, value string) opentracing.Span {
	return sp
}
func (sp *mockSpan) BaggageItem(restrictedKey string) string {
	return ""
}
func (sp *mockSpan) Tracer() opentracing.Tracer {
	return sp.tracer
}
func (sp *mockSpan) LogEvent(event string) {
}
func (sp *mockSpan) LogEventWithPayload(event string, payload interface{}) {
}
func (sp *mockSpan) Log(data opentracing.LogData) {
}

// Mock opentracing.Tracer
type mockTracer struct {
	span                   *mockSpan
	hasStartSpanWithOption bool
}

func (tr *mockTracer) currentSpan() *mockSpan {
	return tr.span
}

func (tr *mockTracer) StartSpan(operationName string, opts ...opentracing.StartSpanOption) opentracing.Span {
	tr.hasStartSpanWithOption = (len(opts) > 0)
	if tr.span != nil {
		return tr.span
	}
	return createSpan(tr)
}

func (tr *mockTracer) Inject(sm opentracing.SpanContext, format interface{}, carrier interface{}) error {
	return nil
}

func (tr *mockTracer) Extract(format interface{}, carrier interface{}) (opentracing.SpanContext, error) {
	if tr.span != nil {
		return nil, nil
	}
	return nil, errors.New("no span")
}

func createMockTracer() *mockTracer {
	tracer := mockTracer{}
	span := createSpan(&tracer)
	tracer.span = span
	return &tracer
}

func TestTraceWithDefaultConfig(t *testing.T) {
	tracer := createMockTracer()

	e := echo.New()
	e.Use(Trace(tracer))

	e.GET("/hello", func(c echo.Context) error {
		return c.String(http.StatusOK, "world")
	})

	e.GET("/giveme400", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "baaaad request")
	})

	e.GET("/givemeerror", func(c echo.Context) error {
		return fmt.Errorf("internal stuff went wrong")
	})

	t.Run("successful call", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/hello", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, "GET", tracer.currentSpan().getTag("http.method"))
		assert.Equal(t, "/hello", tracer.currentSpan().getTag("http.url"))
		assert.Equal(t, defaultComponentName, tracer.currentSpan().getTag("component"))
		assert.Equal(t, uint16(200), tracer.currentSpan().getTag("http.status_code"))
		assert.NotEqual(t, true, tracer.currentSpan().getTag("error"))
	})

	t.Run("error from echo", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/idontexist", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, "GET", tracer.currentSpan().getTag("http.method"))
		assert.Equal(t, "/idontexist", tracer.currentSpan().getTag("http.url"))
		assert.Equal(t, defaultComponentName, tracer.currentSpan().getTag("component"))
		assert.Equal(t, uint16(404), tracer.currentSpan().getTag("http.status_code"))
		assert.Equal(t, true, tracer.currentSpan().getTag("error"))
	})

	t.Run("custom http error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/giveme400", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, uint16(400), tracer.currentSpan().getTag("http.status_code"))
		assert.Equal(t, true, tracer.currentSpan().getTag("error"))
		assert.Equal(t, "baaaad request", tracer.currentSpan().getTag("error.message"))
	})

	t.Run("unknown error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/givemeerror", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, uint16(500), tracer.currentSpan().getTag("http.status_code"))
		assert.Equal(t, true, tracer.currentSpan().getTag("error"))
		assert.Equal(t, "internal stuff went wrong", tracer.currentSpan().getTag("error.message"))
	})
}

func TestTraceWithConfig(t *testing.T) {
	tracer := createMockTracer()

	e := echo.New()
	e.Use(TraceWithConfig(TraceConfig{
		Tracer:        tracer,
		ComponentName: "EchoTracer",
	}))
	req := httptest.NewRequest(http.MethodGet, "/trace", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, true, tracer.currentSpan().isFinished())
	assert.Equal(t, "/trace", tracer.currentSpan().getTag("http.url"))
	assert.Equal(t, "EchoTracer", tracer.currentSpan().getTag("component"))
	assert.Equal(t, true, tracer.hasStartSpanWithOption)

}

func TestTraceWithConfigOfBodyDump(t *testing.T) {
	tracer := createMockTracer()

	e := echo.New()
	e.Use(TraceWithConfig(TraceConfig{
		Tracer:        tracer,
		ComponentName: "EchoTracer",
		IsBodyDump:    true,
	}))
	e.POST("/trace", func(c echo.Context) error {
		return c.String(200, "Hi")
	})

	req := httptest.NewRequest(http.MethodPost, "/trace", bytes.NewBufferString(`{"name": "Lorem"}`))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, true, tracer.currentSpan().isFinished())
	assert.Equal(t, "EchoTracer", tracer.currentSpan().getTag("component"))
	assert.Equal(t, "/trace", tracer.currentSpan().getTag("http.url"))
	assert.Equal(t, `{"name": "Lorem"}`, tracer.currentSpan().getTag("http.req.body"))
	assert.Equal(t, `Hi`, tracer.currentSpan().getTag("http.resp.body"))
	assert.Equal(t, uint16(200), tracer.currentSpan().getTag("http.status_code"))
	assert.Equal(t, nil, tracer.currentSpan().getTag("error"))
	assert.Equal(t, true, tracer.hasStartSpanWithOption)

}

func TestTraceWithConfigOfNoneComponentName(t *testing.T) {
	tracer := createMockTracer()

	e := echo.New()
	e.Use(TraceWithConfig(TraceConfig{
		Tracer: tracer,
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, true, tracer.currentSpan().isFinished())
	assert.Equal(t, defaultComponentName, tracer.currentSpan().getTag("component"))
}

func TestTraceWithConfigOfSkip(t *testing.T) {
	tracer := createMockTracer()
	e := echo.New()
	e.Use(TraceWithConfig(TraceConfig{
		Skipper: func(echo.Context) bool {
			return true
		},
		Tracer: tracer,
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, false, tracer.currentSpan().isFinished())
}

func TestTraceOfNoCurrentSpan(t *testing.T) {
	tracer := &mockTracer{}
	e := echo.New()
	e.Use(Trace(tracer))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, false, tracer.hasStartSpanWithOption)
}
