package errorsx

import (
	"fmt"
	"net/http"
)

// Registry manages error templates and creates error responses based on them.
type Registry struct {
	templates map[int]Template
}

// registry is the package-level default registry initialized with common templates.
var registry = func() *Registry {
	return NewRegistryWithCommon()
}()

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{templates: make(map[int]Template, 64)}
}

// NewRegistryWithCommon creates a new Registry pre-populated with common error templates.
func NewRegistryWithCommon() *Registry {
	r := NewRegistry()
	r.MustRegisterAll(CommonTemplates())
	return r
}

// New creates a new ErrorResponse using the package-level default registry.
func New(code int, opts ...Option) *ErrorResponse {
	return registry.New(code, opts...)
}

// Register adds a new error template to the registry. It returns an error if the code is 0 or already exists.
func (r *Registry) Register(t Template) error {
	if t.Code == 0 {
		return fmt.Errorf("[common][errorsx][err: template code is 0]")
	}
	if _, exists := r.templates[t.Code]; exists {
		return fmt.Errorf("[common][errorsx][err: duplicate error code: %d]", t.Code)
	}
	if t.HTTPStatus == 0 {
		t.HTTPStatus = http.StatusInternalServerError
	}
	r.templates[t.Code] = t
	return nil
}

// MustRegisterAll registers multiple templates and panics if any registration fails (e.g., duplicate codes).
func (r *Registry) MustRegisterAll(mm map[int]Template) {
	for _, t := range mm {
		if err := r.Register(t); err != nil {
			panic(err)
		}
	}
}

// New creates a new ErrorResponse based on the template for the given code and applies any provided options. If the code is not found, it falls back to the InternalServerError template.
func (r *Registry) New(code int, opts ...Option) *ErrorResponse {
	t, ok := r.templates[code]
	if !ok {
		// fallback: internal server (common)
		t = r.templates[InternalServerError]
	}

	er := &ErrorResponse{
		Code:       t.Code,
		Message:    t.Message,
		HTTPStatus: t.HTTPStatus,
	}

	for _, opt := range opts {
		opt(er)
	}
	return er
}

// Override optional: allow per-service override if needed. This can be used to replace a common template with a service-specific one.
func (r *Registry) Override(t Template) {
	r.templates[t.Code] = t
}
