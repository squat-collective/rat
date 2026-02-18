import { describe, it, expect } from "vitest";
import { buildCmSchema, extractAliases, type SchemaData } from "../sql-schema";

const SAMPLE_SCHEMA: SchemaData = {
  default: {
    bronze: {
      orders: { id: "INTEGER", customer_id: "INTEGER", amount: "DECIMAL", created_at: "TIMESTAMP" },
      customers: { id: "INTEGER", name: "VARCHAR", email: "VARCHAR" },
    },
    silver: {
      orders_clean: { order_id: "INTEGER", customer_id: "INTEGER", total: "DECIMAL" },
    },
  },
};

const MULTI_NS_SCHEMA: SchemaData = {
  sales: {
    bronze: {
      transactions: { txn_id: "INTEGER", amount: "DECIMAL" },
    },
  },
  marketing: {
    bronze: {
      campaigns: { campaign_id: "INTEGER", name: "VARCHAR" },
    },
  },
};

describe("buildCmSchema", () => {
  it("builds nested namespace.layer.table structure", () => {
    const result = buildCmSchema(SAMPLE_SCHEMA);
    // Should have namespace-level entry
    expect(result["default"]).toBeDefined();
    const defaultNs = result["default"] as Record<string, Record<string, string[]>>;
    expect(defaultNs["bronze"]).toBeDefined();
    expect(defaultNs["bronze"]["orders"]).toEqual(["id", "customer_id", "amount", "created_at"]);
    expect(defaultNs["bronze"]["customers"]).toEqual(["id", "name", "email"]);
    expect(defaultNs["silver"]["orders_clean"]).toEqual(["order_id", "customer_id", "total"]);
  });

  it("adds short-form layer.table aliases for the first namespace", () => {
    const result = buildCmSchema(SAMPLE_SCHEMA);
    // Short-form aliases at the top level (layer -> table -> columns)
    const bronze = result["bronze"] as Record<string, string[]>;
    expect(bronze).toBeDefined();
    expect(bronze["orders"]).toEqual(["id", "customer_id", "amount", "created_at"]);
    expect(bronze["customers"]).toEqual(["id", "name", "email"]);

    const silver = result["silver"] as Record<string, string[]>;
    expect(silver).toBeDefined();
    expect(silver["orders_clean"]).toEqual(["order_id", "customer_id", "total"]);
  });

  it("handles empty schema", () => {
    const result = buildCmSchema({});
    expect(result).toEqual({});
  });

  it("handles multiple namespaces but only aliases first", () => {
    const result = buildCmSchema(MULTI_NS_SCHEMA);
    // First namespace is "sales" (or whichever Object.keys returns first)
    const nsKeys = Object.keys(MULTI_NS_SCHEMA);
    const firstNs = nsKeys[0];
    const firstNsLayer = Object.keys(MULTI_NS_SCHEMA[firstNs])[0];
    const firstTable = Object.keys(MULTI_NS_SCHEMA[firstNs][firstNsLayer])[0];

    // Short-form alias should exist for first namespace tables
    const layerAlias = result[firstNsLayer] as Record<string, string[]>;
    expect(layerAlias).toBeDefined();
    expect(layerAlias[firstTable]).toBeDefined();
  });

  it("returns column names as arrays (not types)", () => {
    const result = buildCmSchema(SAMPLE_SCHEMA);
    const bronze = result["bronze"] as Record<string, string[]>;
    // Should be column names, not column types
    expect(bronze["orders"]).toContain("id");
    expect(bronze["orders"]).not.toContain("INTEGER");
  });
});

describe("extractAliases", () => {
  describe("plain mode", () => {
    it("extracts alias from FROM clause", () => {
      const sql = "SELECT o.id FROM orders o WHERE o.amount > 100";
      const aliases = extractAliases(sql, "plain", SAMPLE_SCHEMA);
      // "orders" is a bare table name, resolved from schema
      expect(aliases.has("o")).toBe(true);
      expect(aliases.get("o")?.tableName).toBe("orders");
      expect(aliases.get("o")?.columns).toHaveProperty("id");
    });

    it("extracts alias from JOIN clause", () => {
      const sql = `
        SELECT o.id, c.name
        FROM orders o
        JOIN customers c ON o.customer_id = c.id
      `;
      const aliases = extractAliases(sql, "plain", SAMPLE_SCHEMA);
      expect(aliases.has("o")).toBe(true);
      expect(aliases.has("c")).toBe(true);
      expect(aliases.get("c")?.columns).toHaveProperty("name");
      expect(aliases.get("c")?.columns).toHaveProperty("email");
    });

    it("extracts alias with explicit AS keyword", () => {
      const sql = "SELECT o.id FROM orders AS o";
      const aliases = extractAliases(sql, "plain", SAMPLE_SCHEMA);
      expect(aliases.has("o")).toBe(true);
    });

    it("ignores SQL keywords as potential aliases", () => {
      const sql = "SELECT * FROM orders WHERE amount > 100";
      const aliases = extractAliases(sql, "plain", SAMPLE_SCHEMA);
      // "WHERE" should not be treated as an alias
      expect(aliases.has("WHERE")).toBe(false);
      expect(aliases.has("where")).toBe(false);
    });

    it("returns empty map when no aliases found", () => {
      const sql = "SELECT * FROM orders";
      const aliases = extractAliases(sql, "plain", SAMPLE_SCHEMA);
      // "orders" does not have an alias in this SQL
      // Actually FROM orders (\w+) won't match here because there's no alias after "orders"
      expect(aliases.size).toBe(0);
    });

    it("resolves layer.table references", () => {
      const sql = "SELECT o.id FROM bronze.orders o";
      const aliases = extractAliases(sql, "plain", SAMPLE_SCHEMA);
      expect(aliases.has("o")).toBe(true);
      expect(aliases.get("o")?.tableName).toBe("bronze.orders");
    });

    it("resolves ns.layer.table references", () => {
      const sql = "SELECT o.id FROM default.bronze.orders o";
      const aliases = extractAliases(sql, "plain", SAMPLE_SCHEMA);
      expect(aliases.has("o")).toBe(true);
      expect(aliases.get("o")?.tableName).toBe("default.bronze.orders");
    });

    it("ignores aliases for tables not in schema", () => {
      const sql = "SELECT x.id FROM nonexistent_table x";
      const aliases = extractAliases(sql, "plain", SAMPLE_SCHEMA);
      expect(aliases.has("x")).toBe(false);
    });
  });

  describe("jinja mode", () => {
    it("extracts alias from Jinja ref() expression", () => {
      const sql = `SELECT o.id FROM {{ ref('bronze.orders') }} o WHERE o.amount > 100`;
      const aliases = extractAliases(sql, "jinja", SAMPLE_SCHEMA);
      expect(aliases.has("o")).toBe(true);
      expect(aliases.get("o")?.tableName).toBe("bronze.orders");
    });

    it("extracts alias from Jinja ref() with AS keyword", () => {
      const sql = `SELECT o.id FROM {{ ref('bronze.orders') }} AS o`;
      const aliases = extractAliases(sql, "jinja", SAMPLE_SCHEMA);
      expect(aliases.has("o")).toBe(true);
    });

    it("extracts alias from Jinja ref() with double quotes", () => {
      const sql = `SELECT o.id FROM {{ ref("bronze.orders") }} o`;
      const aliases = extractAliases(sql, "jinja", SAMPLE_SCHEMA);
      expect(aliases.has("o")).toBe(true);
    });

    it("jinja ref() aliases take priority over plain SQL aliases", () => {
      const sql = `
        SELECT o.id
        FROM {{ ref('bronze.orders') }} o
        JOIN customers o ON 1=1
      `;
      const aliases = extractAliases(sql, "jinja", SAMPLE_SCHEMA);
      // Jinja match should take priority
      expect(aliases.get("o")?.tableName).toBe("bronze.orders");
    });

    it("also extracts plain SQL aliases in jinja mode", () => {
      const sql = `
        SELECT o.id, c.name
        FROM {{ ref('bronze.orders') }} o
        JOIN customers c ON o.customer_id = c.id
      `;
      const aliases = extractAliases(sql, "jinja", SAMPLE_SCHEMA);
      expect(aliases.has("o")).toBe(true);
      expect(aliases.has("c")).toBe(true);
    });

    it("ignores SQL keywords after Jinja ref()", () => {
      const sql = `SELECT * FROM {{ ref('bronze.orders') }} WHERE amount > 100`;
      const aliases = extractAliases(sql, "jinja", SAMPLE_SCHEMA);
      expect(aliases.has("WHERE")).toBe(false);
    });
  });

  it("handles empty SQL", () => {
    const aliases = extractAliases("", "plain", SAMPLE_SCHEMA);
    expect(aliases.size).toBe(0);
  });

  it("handles empty schema", () => {
    const sql = "SELECT o.id FROM orders o";
    const aliases = extractAliases(sql, "plain", {});
    expect(aliases.size).toBe(0);
  });
});
