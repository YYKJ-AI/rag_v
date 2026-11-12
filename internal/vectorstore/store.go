// Package vectorstore keeps model vectors in PostgreSQL/pgvector. The JSON
// document store remains the source of truth for text and citations.
package vectorstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Store struct {
	conn       *pgx.Conn
	collection string
	model      string
}

type Match struct {
	ChunkID    string
	SourceHash string
	Distance   float64
}

func Open(ctx context.Context, dsn, collection, model string) (*Store, error) {
	if dsn == "" || collection == "" || model == "" {
		return nil, fmt.Errorf("PostgreSQL DSN、collection 和 embedding 模型均为必填")
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("连接 PostgreSQL 失败")
	}
	s := &Store{conn: conn, collection: collection, model: model}
	for _, statement := range []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`CREATE TABLE IF NOT EXISTS ragbot_vector_collections (
            collection text PRIMARY KEY, model text NOT NULL, dimensions integer NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ragbot_chunk_vectors (
            collection text NOT NULL REFERENCES ragbot_vector_collections(collection),
            chunk_id text NOT NULL, source_hash text NOT NULL, embedding vector NOT NULL,
            PRIMARY KEY (collection, chunk_id))`,
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			conn.Close(ctx)
			return nil, fmt.Errorf("初始化 pgvector 失败: %w", err)
		}
	}
	if err := s.checkModel(ctx); err != nil {
		conn.Close(ctx)
		return nil, err
	}
	return s, nil
}

func (s *Store) Close(ctx context.Context) error { return s.conn.Close(ctx) }

func Hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (s *Store) Has(ctx context.Context, chunkID, sourceHash string) (bool, error) {
	var found bool
	err := s.conn.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM ragbot_chunk_vectors WHERE collection=$1 AND chunk_id=$2 AND source_hash=$3)`,
		s.collection, chunkID, sourceHash).Scan(&found)
	return found, err
}

func (s *Store) Upsert(ctx context.Context, chunkID, sourceHash string, vector []float32) error {
	literal, err := vectorLiteral(vector)
	if err != nil {
		return err
	}
	if err := s.ensureDimensions(ctx, len(vector)); err != nil {
		return err
	}
	_, err = s.conn.Exec(ctx, `INSERT INTO ragbot_chunk_vectors(collection, chunk_id, source_hash, embedding)
        VALUES ($1, $2, $3, $4::vector)
        ON CONFLICT (collection, chunk_id) DO UPDATE SET
            source_hash=EXCLUDED.source_hash, embedding=EXCLUDED.embedding`,
		s.collection, chunkID, sourceHash, literal)
	if err != nil {
		return fmt.Errorf("写入 pgvector 失败: %w", err)
	}
	return nil
}

func (s *Store) Search(ctx context.Context, vector []float32, limit int) ([]Match, error) {
	literal, err := vectorLiteral(vector)
	if err != nil {
		return nil, err
	}
	if err := s.checkDimensions(ctx, len(vector)); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("向量查询 limit 必须为 1–1000")
	}
	rows, err := s.conn.Query(ctx, `SELECT chunk_id, source_hash, embedding <=> $2::vector AS distance
        FROM ragbot_chunk_vectors WHERE collection=$1
        ORDER BY distance, chunk_id LIMIT $3`, s.collection, literal, limit)
	if err != nil {
		return nil, fmt.Errorf("pgvector 查询失败: %w", err)
	}
	defer rows.Close()
	var matches []Match
	for rows.Next() {
		var match Match
		if err := rows.Scan(&match.ChunkID, &match.SourceHash, &match.Distance); err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, rows.Err()
}

func (s *Store) Count(ctx context.Context) (int, error) {
	var count int
	err := s.conn.QueryRow(ctx, `SELECT count(*) FROM ragbot_chunk_vectors WHERE collection=$1`, s.collection).Scan(&count)
	return count, err
}

// DeleteAbsent removes rows that no longer exist in the current certified
// snapshot, so stale vector rows cannot displace live hits in top-N queries.
func (s *Store) DeleteAbsent(ctx context.Context, currentIDs []string) (int, error) {
	tag, err := s.conn.Exec(ctx, `DELETE FROM ragbot_chunk_vectors
        WHERE collection=$1 AND NOT (chunk_id = ANY($2::text[]))`, s.collection, currentIDs)
	if err != nil {
		return 0, fmt.Errorf("清理过期向量失败: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (s *Store) ensureDimensions(ctx context.Context, dimensions int) error {
	_, err := s.conn.Exec(ctx, `INSERT INTO ragbot_vector_collections(collection, model, dimensions)
        VALUES ($1, $2, $3) ON CONFLICT (collection) DO NOTHING`, s.collection, s.model, dimensions)
	if err != nil {
		return fmt.Errorf("登记向量维度失败: %w", err)
	}
	return s.checkDimensions(ctx, dimensions)
}

func (s *Store) checkModel(ctx context.Context) error {
	var existing string
	err := s.conn.QueryRow(ctx, `SELECT model FROM ragbot_vector_collections WHERE collection=$1`, s.collection).Scan(&existing)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if existing != s.model {
		return fmt.Errorf("collection 已使用其他 embedding 模型；请使用新 collection")
	}
	return nil
}

func (s *Store) checkDimensions(ctx context.Context, dimensions int) error {
	var model string
	var expected int
	err := s.conn.QueryRow(ctx, `SELECT model, dimensions FROM ragbot_vector_collections WHERE collection=$1`, s.collection).Scan(&model, &expected)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("该 collection 尚无向量，请先运行 vector-sync")
	}
	if err != nil {
		return err
	}
	if model != s.model || expected != dimensions {
		return fmt.Errorf("collection 的模型或向量维度不匹配：已有 %d 维，本次 %d 维；请使用新 collection", expected, dimensions)
	}
	return nil
}

func vectorLiteral(vector []float32) (string, error) {
	if len(vector) < 1 || len(vector) > 16000 {
		return "", fmt.Errorf("向量维度必须为 1–16000")
	}
	var b strings.Builder
	b.WriteByte('[')
	norm := float64(0)
	for i, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return "", fmt.Errorf("向量包含无效数值")
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(value), 'g', -1, 32))
		norm += float64(value) * float64(value)
	}
	b.WriteByte(']')
	if norm == 0 {
		return "", fmt.Errorf("零向量不能用于余弦距离")
	}
	return b.String(), nil
}
