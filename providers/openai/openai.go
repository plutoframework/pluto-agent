// Package openai implements a bounded text-only Chat Completions adapter for
// GPT-5.4. It uses strict JSON schemas, explicit function tools, and SSE. It
// does not silently switch protocols or claim support for other model families.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	agent "github.com/plutoframework/pluto-agent"
)

type Config struct {
	APIKey     string
	Model      string
	HTTPClient *http.Client

	// Endpoint defaults to OpenAI's exact Chat Completions URL. A custom
	// endpoint is a trusted host setting and receives the configured key.
	Endpoint              string
	AllowInsecureLoopback bool
	MaxBodyBytes          int64
	MaxOutputBytes        int
}

type Provider struct {
	key       string
	model     string
	client    *http.Client
	endpoint  string
	maxBody   int64
	maxOutput int
}

// HTTPError intentionally excludes response bodies, URLs, and credentials.
// Retryable is advisory; this adapter and the agent never retry requests.
type HTTPError struct{ StatusCode int }

func (e *HTTPError) Error() string {
	return fmt.Sprintf("OpenAI HTTP status %d", e.StatusCode)
}

func (e *HTTPError) Retryable() bool {
	return e.StatusCode == 429 || e.StatusCode >= 500
}

func New(c Config) (*Provider, error) {
	if c.Model != "gpt-5.4" && c.Model != "gpt-5.4-2026-03-05" {
		return nil, fmt.Errorf(
			"%w: OpenAI adapter supports GPT-5.4 only",
			agent.ErrCapability,
		)
	}

	if c.APIKey == "" || len(c.APIKey) > 8192 ||
		strings.ContainsAny(c.APIKey, " \t\r\n") {
		return nil, fmt.Errorf("%w: invalid API credential", agent.ErrConfig)
	}

	if c.Endpoint == "" {
		c.Endpoint = "https://api.openai.com/v1/chat/completions"
	}

	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("%w: invalid endpoint", agent.ErrConfig)
	}

	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !c.AllowInsecureLoopback ||
			ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf(
				"%w: HTTPS endpoint required",
				agent.ErrConfig,
			)
		}
	}

	if c.MaxBodyBytes == 0 {
		c.MaxBodyBytes = 1 << 20
	}

	if c.MaxOutputBytes == 0 {
		c.MaxOutputBytes = 64 << 10
	}

	if c.MaxBodyBytes <= 0 || c.MaxBodyBytes > 16<<20 ||
		c.MaxOutputBytes <= 0 || c.MaxOutputBytes > 1<<20 {
		return nil, agent.ErrConfig
	}

	client := http.Client{Timeout: 45 * time.Second}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	}

	// Copy the client and always disable redirects, even same-origin ones.
	// An injected transport remains trusted to handle credentials securely.
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Provider{
		key: c.APIKey, model: c.Model, client: &client,
		endpoint: c.Endpoint, maxBody: c.MaxBodyBytes,
		maxOutput: c.MaxOutputBytes,
	}, nil
}

func (p *Provider) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Tools:            true,
		StructuredOutput: true,
		Streaming:        true,
		Usage:            true,
	}
}

func (p *Provider) Generate(
	ctx context.Context,
	req agent.Request,
) (agent.Response, error) {
	return p.call(ctx, req, nil)
}

func (p *Provider) Stream(
	ctx context.Context,
	req agent.Request,
	emit func(agent.Delta) error,
) (agent.Response, error) {
	if emit == nil {
		return agent.Response{}, agent.ErrConfig
	}

	return p.call(ctx, req, emit)
}

func (p *Provider) call(
	ctx context.Context,
	req agent.Request,
	emit func(agent.Delta) error,
) (result agent.Response, err error) {
	if ctx == nil {
		return agent.Response{}, agent.ErrConfig
	}

	defer func() {
		if ctx.Err() != nil {
			result = agent.Response{}
			err = ctx.Err()
		}
	}()

	body, err := p.request(req, emit != nil)
	if err != nil {
		return agent.Response{}, err
	}

	if int64(len(body)) > p.maxBody {
		return agent.Response{}, agent.ErrLimit
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return agent.Response{}, agent.ErrConfig
	}

	httpReq.Header.Set("Authorization", "Bearer "+p.key)
	httpReq.Header.Set("Content-Type", "application/json")

	response, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return agent.Response{}, ctx.Err()
		}

		return agent.Response{}, errors.New("OpenAI transport failed")
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return agent.Response{}, &HTTPError{StatusCode: response.StatusCode}
	}

	if response.ContentLength > p.maxBody {
		return agent.Response{}, agent.ErrLimit
	}

	contentType := strings.TrimSpace(
		strings.Split(response.Header.Get("Content-Type"), ";")[0],
	)
	if emit != nil {
		if contentType != "text/event-stream" {
			return agent.Response{}, agent.ErrProtocol
		}

		return p.decodeStream(ctx, response.Body, emit)
	}

	if contentType != "application/json" {
		return agent.Response{}, agent.ErrProtocol
	}

	raw, err := io.ReadAll(io.LimitReader(response.Body, p.maxBody+1))
	if err != nil {
		return agent.Response{}, agent.ErrProtocol
	}

	if int64(len(raw)) > p.maxBody {
		return agent.Response{}, agent.ErrLimit
	}

	var wire completion
	if err := json.Unmarshal(raw, &wire); err != nil {
		return agent.Response{}, agent.ErrProtocol
	}

	if len(wire.Error) != 0 || len(wire.Choices) != 1 ||
		wire.Choices[0].Index != 0 {
		return agent.Response{}, agent.ErrProtocol
	}

	choice := wire.Choices[0]

	return p.finish(choice.Message, choice.Finish, wire.Usage)
}
