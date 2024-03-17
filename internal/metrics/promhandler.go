package metrics

// Everything in here is to support making a new prometheus registry and having the http endpoint pick it up seamlessly

import (
	"net/http"
	"sync/atomic"
)

type dynamicPromHandler struct {
	handler atomic.Value // Stores the current http.Handler
}

func (d *dynamicPromHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Retrieve the current handler and serve the request
	handler := d.handler.Load().(http.Handler)
	handler.ServeHTTP(w, r)
}

// Function to update the handler
func (d *dynamicPromHandler) updateHandler(newHandler http.Handler) {
	d.handler.Store(newHandler)
}
