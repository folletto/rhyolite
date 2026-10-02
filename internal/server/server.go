// Package server exposes a vault as an MCP server over HTTP.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/folletto/rhyolite/internal/vault"
)

// Options configure New.
type Options struct {
	Vault   *vault.Vault
	Token   string
	Log     Logger // optional
	Version string
}

// New returns the HTTP handler: the MCP endpoint at /mcp behind origin
// checks, bearer-token authentication and request logging.
func New(o Options) http.Handler {
	log := o.Log
	if log == nil {
		log = nopLogger{}
	}
	version := o.Version
	if version == "" {
		version = "dev"
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "rhyolite", Title: "Rhyolite", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	registerTools(s, o.Vault)
	s.AddReceivingMiddleware(toolCallLog(log))

	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{
		SessionTimeout:      time.Hour,
		MaxRequestBodyBytes: 4 << 20,
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	return requestLog(log, originGuard(bearerAuth(o.Token, log, limitBody(5<<20, mux))))
}

// toolCallLog logs each tool call: name, session, paths, outcome, duration.
// Arguments other than path/destination (i.e. file content) are never logged.
func toolCallLog(log Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			start := time.Now()
			res, err := next(ctx, method, req)
			name, args := "?", ""
			if r, ok := req.(*mcp.CallToolRequest); ok && r.Params != nil {
				name = r.Params.Name
				var a struct {
					Path        string `json:"path"`
					Destination string `json:"destination"`
				}
				json.Unmarshal(r.Params.Arguments, &a)
				if a.Path != "" || name == "list" {
					args = " path=" + quote(a.Path)
				}
				if a.Destination != "" {
					args += " destination=" + quote(a.Destination)
				}
			}
			outcome := "ok"
			if err != nil {
				outcome = "error"
			} else if r, ok := res.(*mcp.CallToolResult); ok && r.IsError {
				outcome = "error: " + toolErrorText(r)
			}
			log.Printf("tool %s%s -> %s (%s)", name, args, outcome, time.Since(start).Round(time.Millisecond))
			return res, err
		}
	}
}

func toolErrorText(r *mcp.CallToolResult) string {
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			if len(t.Text) > 200 {
				return t.Text[:200] + "…"
			}
			return t.Text
		}
	}
	return ""
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
