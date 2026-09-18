-- Opt-in M4.2c schema. Apply only after review, to the intended Mall database.
-- No backfill, inferred labels, application grants or automatic startup DDL.
-- Classification is maintained by trusted catalog operators, not end users.
BEGIN;
CREATE TABLE product_demand_categories (
    product_id varchar(64) PRIMARY KEY REFERENCES products(id),
    taxonomy_version varchar(64) NOT NULL
        CHECK (taxonomy_version = 'mall_demand_taxonomy_v1'),
    category_code varchar(32) NOT NULL CHECK (category_code IN (
        'unknown', 'keyboard', 'mouse', 'lighting', 'stationery', 'monitor',
        'headphones', 'tablet', 'phone', 'computer', 'accessories'
    )),
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE product_demand_categories IS
    'Operator-managed SPU classification for bounded Agent demand execution; no attribute/quality guarantees';
COMMIT;
