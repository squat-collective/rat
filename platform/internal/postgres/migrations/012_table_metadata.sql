CREATE TABLE table_metadata (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace VARCHAR(63) NOT NULL,
    layer VARCHAR(10) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    owner VARCHAR(255),
    column_descriptions JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(namespace, layer, name)
);
