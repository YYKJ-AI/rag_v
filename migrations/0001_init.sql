-- 可溯源知识库问答：建表起点
-- 迁移工具：golang-migrate / goose，从 0001_init 开始版本化
-- 默认向量路径未启用：embedding 相关列建表即留，nullable

CREATE TABLE documents (
    id             text PRIMARY KEY,                    -- document_id
    version        text NOT NULL,                       -- v1/v2/...
    title          text NOT NULL,
    publish_date   date,
    effective_date date,                                -- 业务时间
    scope          text,                                -- 适用范围
    nature         text,                                -- 基础规范/修订通知/补充说明
    replaces       text[] NOT NULL DEFAULT '{}',        -- 替代关系 ["doc-a@v1"]
    superseded_by  text,                                -- 反向指针，NULL=现行
    fingerprint    bigint NOT NULL,                     -- 文档级 SimHash
    activated_at   timestamptz,                         -- 系统时间轴 valid_from
    superseded_at  timestamptz,                         -- 系统时间轴 valid_until，NULL=现行
    status         text NOT NULL DEFAULT 'active',      -- active|superseded|needs_review
    UNIQUE (id, version)
);
CREATE INDEX idx_docs_fingerprint ON documents(fingerprint);
-- 幂等键 = 业务主键合一：(id, version, fingerprint)

CREATE TABLE sections (                                -- 父块 + 大纲层
    id           text PRIMARY KEY,                     -- docId@version#sec-2
    doc_id       text NOT NULL,
    version      text NOT NULL,
    section_path text NOT NULL,                        -- "第2节 申请条件"（citation 定位符）
    summary      text,                                 -- 大纲层检索用
    para_start   int,
    simhash      bigint NOT NULL,
    FOREIGN KEY (doc_id, version) REFERENCES documents(id, version)
);

CREATE TABLE chunks (                                  -- 子块，检索最小单位
    id                text PRIMARY KEY,
    section_id        text NOT NULL REFERENCES sections(id),
    text              text NOT NULL,
    tokens            jsonb NOT NULL,                  -- 分词结果，启动时重建内存倒排
    content_hash      text NOT NULL,                   -- sha256：embedding 复用判据
    simhash           bigint NOT NULL,                 -- 块级去重
    embedding         vector(1024),                    -- nullable，向量路径未启用时为空
    embedding_model   text,                            -- 出生信息，守卫用
    embedding_version text,
    image_refs        jsonb NOT NULL DEFAULT '[]'      -- [{storage_url,page,caption}]
);
CREATE INDEX idx_chunks_hash ON chunks(content_hash);
-- 向量启用后：CREATE INDEX ON chunks USING hnsw (embedding vector_cosine_ops);

CREATE TABLE embedding_registry (                      -- 向量版本守卫
    model         text NOT NULL,
    version       text NOT NULL,
    dims          int  NOT NULL,
    canary_text   text NOT NULL,                       -- 金丝雀句子
    canary_vector vector(1024),                        -- 注册时向量，探针比对基准
    status        text NOT NULL DEFAULT 'active',      -- active|stale|rebuilding
    PRIMARY KEY (model, version)
);

CREATE TABLE ingest_log (                              -- 幂等审计 + outbox 形态预留
    doc_fingerprint bigint NOT NULL,
    doc_id          text,
    version         text,
    imported_at     timestamptz NOT NULL DEFAULT now(),
    event_type      text NOT NULL DEFAULT 'ingested',   -- outbox 演化位
    dispatched      boolean NOT NULL DEFAULT false,     -- outbox 演化位
    attempts        int NOT NULL DEFAULT 0
);
CREATE INDEX idx_ingest_fp ON ingest_log(doc_fingerprint);

-- 评测基线提交进仓库 eval/baseline/，不进库
