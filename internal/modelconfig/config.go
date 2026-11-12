// Package modelconfig reads the opt-in vector experiment settings.
package modelconfig

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Embedding struct {
	Endpoint string
	Model    string
	APIKey   string
	Timeout  time.Duration
}

type Vector struct {
	DSN       string
	Embedding Embedding
}

// LoadVector reads only the opt-in vector settings. Process environment wins
// over the local file; errors never include secret values.
func LoadVector(path string) (Vector, error) {
	values, err := readFile(path)
	if err != nil {
		return Vector{}, err
	}
	get := func(key string) string {
		if v, ok := os.LookupEnv(key); ok {
			return v
		}
		return values[key]
	}
	if get("RAGBOT_EMBEDDING_ENABLED") != "true" {
		return Vector{}, fmt.Errorf("向量命令需要 RAGBOT_EMBEDDING_ENABLED=true")
	}
	if get("RAGBOT_EMBEDDING_PROVIDER") != "remote_http" {
		return Vector{}, fmt.Errorf("向量命令当前只支持 RAGBOT_EMBEDDING_PROVIDER=remote_http")
	}
	for _, key := range []string{"RAGBOT_EMBEDDING_ENDPOINT_URL", "RAGBOT_EMBEDDING_MODEL", "RAGBOT_EMBEDDING_API_KEY", "RAGBOT_PG_DSN"} {
		if strings.TrimSpace(get(key)) == "" {
			return Vector{}, fmt.Errorf("缺少 %s", key)
		}
	}
	endpoint, err := embeddingEndpoint(get("RAGBOT_EMBEDDING_ENDPOINT_URL"))
	if err != nil {
		return Vector{}, err
	}
	seconds := 30
	if raw := strings.TrimSpace(get("RAGBOT_EMBEDDING_TIMEOUT_SECONDS")); raw != "" {
		seconds, err = strconv.Atoi(raw)
		if err != nil || seconds < 1 || seconds > 300 {
			return Vector{}, fmt.Errorf("RAGBOT_EMBEDDING_TIMEOUT_SECONDS 必须为 1–300")
		}
	}
	return Vector{
		DSN: get("RAGBOT_PG_DSN"),
		Embedding: Embedding{
			Endpoint: endpoint, Model: get("RAGBOT_EMBEDDING_MODEL"),
			APIKey: get("RAGBOT_EMBEDDING_API_KEY"), Timeout: time.Duration(seconds) * time.Second,
		},
	}, nil
}

func embeddingEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("RAGBOT_EMBEDDING_ENDPOINT_URL 必须是无凭证和查询参数的 HTTP(S) 地址")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return "", fmt.Errorf("远端 embedding 地址必须使用 HTTPS；HTTP 仅用于本机测试")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(u.Path, "/v1") {
		u.Path += "/embeddings"
	} else if !strings.HasSuffix(u.Path, "/embeddings") {
		return "", fmt.Errorf("RAGBOT_EMBEDDING_ENDPOINT_URL 应以 /v1 或 /embeddings 结尾")
	}
	return u.String(), nil
}

func readFile(path string) (map[string]string, error) {
	values := map[string]string{}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return values, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取环境文件失败: %w", err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("环境文件第 %d 行缺少等号", lineNo)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("读取环境文件失败: %w", err)
	}
	return values, nil
}
