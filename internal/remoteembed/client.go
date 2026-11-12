// Package remoteembed calls an OpenAI-compatible embedding endpoint.
package remoteembed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"

	"ragbot/internal/modelconfig"
)

type Client struct {
	endpoint string
	model    string
	key      string
	http     *http.Client
}

func New(config modelconfig.Embedding) *Client {
	return &Client{endpoint: config.Endpoint, model: config.Model, key: config.APIKey, http: &http.Client{
		Timeout:       config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *Client) Model() string { return c.model }

// Embed returns vectors in the same order as inputs, rejecting incomplete or
// non-finite responses before any row is written to PostgreSQL.
func (c *Client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("embedding 输入为空")
	}
	body, err := json.Marshal(struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{c.model, inputs})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建 embedding 请求失败")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			return nil, fmt.Errorf("embedding 请求超时")
		}
		return nil, fmt.Errorf("embedding 网络请求失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding 服务返回 HTTP %d", resp.StatusCode)
	}
	var result struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("embedding 响应格式错误")
	}
	if len(result.Data) != len(inputs) {
		return nil, fmt.Errorf("embedding 响应数量不符")
	}
	vectors := make([][]float32, len(inputs))
	dim := 0
	for _, item := range result.Data {
		if item.Index < 0 || item.Index >= len(inputs) || vectors[item.Index] != nil || len(item.Embedding) == 0 || len(item.Embedding) > 16000 {
			return nil, fmt.Errorf("embedding 响应序号或维度无效")
		}
		if dim == 0 {
			dim = len(item.Embedding)
		} else if len(item.Embedding) != dim {
			return nil, fmt.Errorf("embedding 响应维度不一致")
		}
		vector := make([]float32, dim)
		for j, value := range item.Embedding {
			if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > math.MaxFloat32 {
				return nil, fmt.Errorf("embedding 响应包含无效数值")
			}
			vector[j] = float32(value)
		}
		vectors[item.Index] = vector
	}
	return vectors, nil
}
