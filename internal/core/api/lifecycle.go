// Generated file. DO NOT EDIT.

package api

import (
	"context"
	"net/http"

	josh "github.com/deref/exo/internal/josh/server"
)

type Lifecycle interface {
	Dependencies(context.Context, *DependenciesInput) (*DependenciesOutput, error)
	Initialize(context.Context, *InitializeInput) (*InitializeOutput, error)
	Refresh(context.Context, *RefreshInput) (*RefreshOutput, error)
	Dispose(context.Context, *DisposeInput) (*DisposeOutput, error)
}

type DependenciesInput struct {
	Spec string `json:"spec"`
}

type DependenciesOutput struct {

	// Refs of components that this component depends on.
	Components []string `json:"components"`
}

type InitializeInput struct {
	Spec string `json:"spec"`
}

type InitializeOutput struct {
}

type RefreshInput struct {
	Spec string `json:"spec"`
}

type RefreshOutput struct {
}

type DisposeInput struct {
}

type DisposeOutput struct {
}

func BuildLifecycleMux(b *josh.MuxBuilder, factory func(req *http.Request) Lifecycle) {
	b.AddMethod("dependencies", func(req *http.Request) interface{} {
		return factory(req).Dependencies
	})
	b.AddMethod("initialize", func(req *http.Request) interface{} {
		return factory(req).Initialize
	})
	b.AddMethod("refresh", func(req *http.Request) interface{} {
		return factory(req).Refresh
	})
	b.AddMethod("dispose", func(req *http.Request) interface{} {
		return factory(req).Dispose
	})
}
