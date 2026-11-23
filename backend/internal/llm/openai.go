// Package llm is a minimal OpenAI HTTP client: embeddings and JSON-schema chat completions.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrNoKey = errors.New("OPENAI_API_KEY is not set")

type Client struct {
	key, base, chatModel, embedModel string
	http                             *http.Client
}

func New(key, base, chatModel, embedModel string) *Client {
	return &Client{
		key: key, base: strings.TrimRight(base, "/"),
		chatModel: chatModel, embedModel: embedModel,
		http: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) Enabled() bool { return c.key != "" }

// Embed returns one 1536-dim vector per input, in order.
func (c *Client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if !c.Enabled() {
		return nil, ErrNoKey
	}
	out := make([][]float32, 0, len(inputs))
	const batch = 96
	for start := 0; start < len(inputs); start += batch {
		end := min(start+batch, len(inputs))
		var resp struct {
			Data []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}
		err := c.post(ctx, "/embeddings", map[string]any{
			"model": c.embedModel, "input": inputs[start:end], "dimensions": 1536,
		}, &resp)
		if err != nil {
			return nil, err
		}
		part := make([][]float32, end-start)
		for _, d := range resp.Data {
			if d.Index < len(part) {
				part[d.Index] = d.Embedding
			}
		}
		for i, v := range part {
			if v == nil {
				return nil, fmt.Errorf("embedding %d missing", start+i)
			}
		}
		out = append(out, part...)
	}
	return out, nil
}

// ChatJSON runs a chat completion constrained to a strict JSON schema and decodes the reply into out.
func (c *Client) ChatJSON(ctx context.Context, system, user, schemaName string, schema any, out any) error {
	if !c.Enabled() {
		return ErrNoKey
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	err := c.post(ctx, "/chat/completions", map[string]any{
		"model":       c.chatModel,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": schemaName, "strict": true, "schema": schema},
		},
	}, &resp)
	if err != nil {
		return err
	}
	if len(resp.Choices) == 0 {
		return errors.New("empty model response")
	}
	msg := resp.Choices[0].Message
	if msg.Refusal != "" {
		return fmt.Errorf("model refused to answer: %s", msg.Refusal)
	}
	if err := json.Unmarshal([]byte(msg.Content), out); err != nil {
		return fmt.Errorf("model returned invalid JSON: %w", err)
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			lastErr = fmt.Errorf("OpenAI %s: %d %s", path, res.StatusCode, truncate(string(data)))
			continue
		}
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("OpenAI %s: %d %s", path, res.StatusCode, truncate(string(data)))
		}
		return json.Unmarshal(data, out)
	}
	return lastErr
}

func truncate(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
