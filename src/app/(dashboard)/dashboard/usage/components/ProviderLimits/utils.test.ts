import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { getConnectionsPageRange, type Pagination } from "./utils";

describe("getConnectionsPageRange", () => {
  it("returns { start: 0, end: 0 } when total is 0", () => {
    const pagination: Pagination = {
      page: 1,
      pageSize: 20,
      total: 0,
      totalPages: 0,
    };
    assert.deepStrictEqual(getConnectionsPageRange(pagination), {
      start: 0,
      end: 0,
    });
  });

  it("calculates range for first page correctly", () => {
    const pagination: Pagination = {
      page: 1,
      pageSize: 20,
      total: 50,
      totalPages: 3,
    };
    assert.deepStrictEqual(getConnectionsPageRange(pagination), {
      start: 1,
      end: 20,
    });
  });

  it("calculates range for middle page correctly", () => {
    const pagination: Pagination = {
      page: 2,
      pageSize: 20,
      total: 50,
      totalPages: 3,
    };
    assert.deepStrictEqual(getConnectionsPageRange(pagination), {
      start: 21,
      end: 40,
    });
  });

  it("clips end range on last page when total is not a multiple of pageSize", () => {
    const pagination: Pagination = {
      page: 3,
      pageSize: 20,
      total: 50,
      totalPages: 3,
    };
    assert.deepStrictEqual(getConnectionsPageRange(pagination), {
      start: 41,
      end: 50,
    });
  });

  it("handles single page where total is less than pageSize", () => {
    const pagination: Pagination = {
      page: 1,
      pageSize: 20,
      total: 5,
      totalPages: 1,
    };
    assert.deepStrictEqual(getConnectionsPageRange(pagination), {
      start: 1,
      end: 5,
    });
  });

  it("handles exact page boundaries on last page", () => {
    const pagination: Pagination = {
      page: 2,
      pageSize: 20,
      total: 40,
      totalPages: 2,
    };
    assert.deepStrictEqual(getConnectionsPageRange(pagination), {
      start: 21,
      end: 40,
    });
  });
});
