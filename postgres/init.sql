-- Create tenant table (just for the example)
CREATE TABLE tenants (
                       tenant_id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
                       tenant_name VARCHAR(255) UNIQUE NOT NULL
);

-- Insert some sample data
INSERT INTO tenants (tenant_name)
VALUES
    ('microsoft'),
    ('amazon'),
    ('google');

CREATE TABLE nodes (
    id        BIGINT PRIMARY KEY,
    type      TEXT   NOT NULL CHECK (type IN ('management_group', 'subscription', 'resource_group')),
    parent_id BIGINT REFERENCES nodes (id),
    position  INT    NOT NULL DEFAULT 0
);

CREATE INDEX nodes_parent_idx ON nodes (parent_id, position);
