import type { QueryRequest, QueryResult } from "../models/query";
import { BaseResource } from "./base";

export class QueryResource extends BaseResource {
  async execute(req: QueryRequest): Promise<QueryResult> {
    return this.transport.request<QueryResult>("POST", "/api/v1/query", {
      json: {
        sql: req.sql,
        namespace: req.namespace ?? "",
        limit: req.limit ?? 1000,
      },
    });
  }
}
