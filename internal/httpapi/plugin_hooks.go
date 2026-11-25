package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode"

	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

const defaultHookRejectReason = "内容未通过合规审核"

type hookSkipPluginKey struct{}

func withHookSkipPlugin(ctx context.Context, pluginID string) context.Context {
	if pluginID == "" {
		return ctx
	}
	return context.WithValue(ctx, hookSkipPluginKey{}, pluginID)
}

func hookSkipPlugin(ctx context.Context) string {
	id, _ := ctx.Value(hookSkipPluginKey{}).(string)
	return id
}

func (a *Server) runTextHook(ctx context.Context, scope string, userID int64, fields map[string]string) sdk.HookDecision {
	return a.executePluginHook(ctx, "text.check", sdk.HookRequest{
		Name:   "text.check",
		Scope:  scope,
		UserID: userID,
		Fields: fields,
	}, nil)
}

func (a *Server) runTextureUploadHook(ctx context.Context, req sdk.HookRequest, pngBytes []byte) sdk.HookDecision {
	req.Name = "texture.upload.check"
	return a.executePluginHook(ctx, "texture.upload.check", req, pngBytes)
}

func (a *Server) executePluginHook(ctx context.Context, hookName string, base sdk.HookRequest, pngBytes []byte) sdk.HookDecision {
	if a.dynamic == nil {
		return sdk.HookDecision{Decision: "allow"}
	}
	var (
		review       bool
		reviewReason string
		pngEncoded   string
	)
	skipID := hookSkipPlugin(ctx)
	for _, item := range a.dynamic.Items() {
		id := item.Package.Manifest.ID
		if (skipID != "" && id == skipID) || !slices.Contains(item.Package.Hooks, hookName) || !slices.Contains(item.Grants, "hooks") {
			continue
		}
		enabled, err := a.availableCommunityCtx(ctx, id)
		if err != nil || !enabled {
			continue
		}
		reqCopy := base
		if len(pngBytes) > 0 && slices.Contains(item.Grants, "hooks.image") {
			if pngEncoded == "" {
				pngEncoded = base64.StdEncoding.EncodeToString(pngBytes)
			}
			reqCopy.PNGBase64 = pngEncoded
		} else {
			reqCopy.PNGBase64 = ""
		}

		hookCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		rawResp, err := a.callDynamic(hookCtx, id, sdk.RuntimeRequest{
			Kind: "hook",
			ID:   hookName,
			Hook: &reqCopy,
		})
		cancel()
		if err != nil {
			slog.Warn("plugin hook execution failed", "plugin", id, "hook", hookName, "err", err)
			continue
		}
		var result sdk.RuntimeResponse
		if err := json.Unmarshal(rawResp, &result); err != nil || result.Status >= 400 {
			slog.Warn("plugin hook returned error status", "plugin", id, "hook", hookName, "status", result.Status)
			continue
		}
		var decision sdk.HookDecision
		if err := json.Unmarshal(result.Body, &decision); err != nil {
			slog.Warn("plugin hook invalid decision payload", "plugin", id, "hook", hookName, "err", err)
			continue
		}
		reason := sanitizeHookReason(decision.Reason)
		switch decision.Decision {
		case "reject":
			if reason == "" {
				reason = defaultHookRejectReason
			}
			return sdk.HookDecision{Decision: "reject", Reason: reason}
		case "review":
			if !review {
				review = true
				reviewReason = reason
			} else if reviewReason == "" && reason != "" {
				reviewReason = reason
			}
		}
	}
	if review {
		return sdk.HookDecision{Decision: "review", Reason: reviewReason}
	}
	return sdk.HookDecision{Decision: "allow"}
}

func (a *Server) availableCommunityCtx(ctx context.Context, id string) (bool, error) {
	state, ok := a.plugins.State(id)
	if !ok || !a.isExternalPlugin(id) || state.Error != "" {
		return false, nil
	}
	return a.communityEnabled(ctx, id)
}

func sanitizeHookReason(raw string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != ' ' {
			return -1
		}
		return r
	}, raw)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return ""
	}
	runes := []rune(cleaned)
	if len(runes) > 200 {
		runes = runes[:200]
	}
	// Ensure review_reason column (VARCHAR(600)) is never exceeded even on 4-byte UTF-8 runes.
	for len(string(runes)) > 580 && len(runes) > 0 {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
