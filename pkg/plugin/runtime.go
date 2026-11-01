package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
)

type RuntimeRequest struct {
	Data      map[string]json.RawMessage `json:"data,omitempty"`
	Kind      string                     `json:"kind"`
	Method    string                     `json:"method,omitempty"`
	Path      string                     `json:"path,omitempty"`
	Query     string                     `json:"query,omitempty"`
	Body      json.RawMessage            `json:"body,omitempty"`
	Principal Principal                  `json:"principal"`
	Settings  map[string]any             `json:"settings,omitempty"`
	Event     *Event                     `json:"event,omitempty"`
	ID        string                     `json:"id,omitempty"`
}
type RuntimeResponse struct {
	Writes map[string]json.RawMessage `json:"writes,omitempty"`
	Status int                        `json:"status"`
	Body   json.RawMessage            `json:"body"`
}

// ServeRuntime reserves stdout for protocol responses; logs belong on stderr.
func ServeRuntime(handler func(context.Context, RuntimeRequest) (any, error)) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request RuntimeRequest
		var result any
		err := json.Unmarshal(scanner.Bytes(), &request)
		if err == nil {
			if request.Kind == "health" {
				result = map[string]int{"protocol": 1}
			} else {
				result, err = handler(context.Background(), request)
			}
		}
		response := map[string]any{"result": result}
		if err != nil {
			response = map[string]any{"error": "request failed"}
		}
		if err = encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}
