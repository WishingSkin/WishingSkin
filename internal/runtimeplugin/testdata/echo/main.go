package main

import (
	"context"
	"encoding/json"
	"os"
	"time"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

func main() {
	_ = sdk.ServeRuntime(func(ctx context.Context, r sdk.RuntimeRequest) (any, error) {
		if r.ID == "wait" {
			time.Sleep(time.Minute)
		}
		if r.Kind == "page" {
			return sdk.RuntimeResponse{Status: 200, Body: json.RawMessage(`{"title":"Echo page"}`)}, nil
		}
		if r.Kind == "event" || r.Kind == "task" || r.Path == "/data" {
			return sdk.RuntimeResponse{Status: 200, Body: json.RawMessage(`{"ok":true}`), Writes: map[string]json.RawMessage{"last": json.RawMessage(`"saved"`)}}, nil
		}
		data, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "id": r.ID, "kind": r.Kind})
		return sdk.RuntimeResponse{Status: 200, Body: data}, nil
	})
}
