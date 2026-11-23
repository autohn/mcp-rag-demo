CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE access_tokens (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);

CREATE TABLE projects (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('admin', 'participant')),
    PRIMARY KEY (project_id, user_id)
);

CREATE TABLE folders (
    id         BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    parent_id  BIGINT REFERENCES folders(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX folders_project_parent ON folders(project_id, parent_id);

CREATE TABLE documents (
    id          BIGSERIAL PRIMARY KEY,
    project_id  BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    folder_id   BIGINT REFERENCES folders(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    mime        TEXT NOT NULL,
    size        BIGINT NOT NULL,
    path        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'processing' CHECK (status IN ('processing', 'ready', 'failed')),
    error       TEXT NOT NULL DEFAULT '',
    page_count  INT NOT NULL DEFAULT 0,
    chunk_count INT NOT NULL DEFAULT 0,
    paged       BOOLEAN NOT NULL DEFAULT false,
    uploaded_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX documents_project_folder ON documents(project_id, folder_id);

CREATE TABLE document_pages (
    document_id BIGINT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    page        INT NOT NULL,
    text        TEXT NOT NULL,
    PRIMARY KEY (document_id, page)
);

CREATE TABLE chunks (
    id          BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    project_id  BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    page        INT NOT NULL,
    ord         INT NOT NULL,
    text        TEXT NOT NULL,
    embedding   vector(1536),
    tsv         tsvector NOT NULL
);
CREATE INDEX chunks_project ON chunks(project_id);
CREATE INDEX chunks_document ON chunks(document_id);
CREATE INDEX chunks_tsv ON chunks USING gin(tsv);

CREATE TABLE questions (
    id         BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    folder_id  BIGINT,
    text       TEXT NOT NULL,
    answer     TEXT NOT NULL,
    found      BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX questions_project_user ON questions(project_id, user_id, created_at DESC);

CREATE TABLE citations (
    question_id   BIGINT NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    ord           INT NOT NULL,
    document_id   BIGINT REFERENCES documents(id) ON DELETE SET NULL,
    document_name TEXT NOT NULL,
    page          INT NOT NULL,
    paged         BOOLEAN NOT NULL,
    quote         TEXT NOT NULL,
    PRIMARY KEY (question_id, ord)
);
