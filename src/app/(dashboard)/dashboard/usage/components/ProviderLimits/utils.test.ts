import { describe, it } from "node:test";
import assert from "node:assert";
import {
  getConnectionsPageRange,
  type Pagination,
  getConnectionLabel,
  type ConnectionItem,
} from "./utils";

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

describe("getConnectionLabel", () => {
  it("returns trimmed name when name is present", () => {
    const connection: ConnectionItem = {
      id: "conn-1",
      name: "  Primary Name  ",
      email: "user@example.com",
      displayName: "Display Name",
    };
    assert.strictEqual(getConnectionLabel(connection), "Primary Name");
  });

  it("falls back to trimmed email when name is missing or whitespace", () => {
    const connectionWithNoName: ConnectionItem = {
      id: "conn-2",
      email: "  user@example.com  ",
      displayName: "Display Name",
    };
    assert.strictEqual(
      getConnectionLabel(connectionWithNoName),
      "user@example.com",
    );

    const connectionWithWhitespaceName: ConnectionItem = {
      id: "conn-3",
      name: "   ",
      email: "  user@example.com  ",
      displayName: "Display Name",
    };
    assert.strictEqual(
      getConnectionLabel(connectionWithWhitespaceName),
      "user@example.com",
    );
  });

  it("falls back to trimmed displayName when name and email are missing or whitespace", () => {
    const connectionWithNoNameOrEmail: ConnectionItem = {
      id: "conn-4",
      displayName: "  Display Name  ",
    };
    assert.strictEqual(
      getConnectionLabel(connectionWithNoNameOrEmail),
      "Display Name",
    );

    const connectionWithWhitespaceNameAndEmail: ConnectionItem = {
      id: "conn-5",
      name: "   ",
      email: "\t\n ",
      displayName: "  Display Name  ",
    };
    assert.strictEqual(
      getConnectionLabel(connectionWithWhitespaceNameAndEmail),
      "Display Name",
    );
  });

  it("returns null when name, email, and displayName are missing, empty, or whitespace", () => {
    const connectionEmptyObj: ConnectionItem = {
      id: "conn-6",
    };
    assert.strictEqual(getConnectionLabel(connectionEmptyObj), null);

    const connectionAllWhitespace: ConnectionItem = {
      id: "conn-7",
      name: "  ",
      email: "   ",
      displayName: "\t",
    };
    assert.strictEqual(getConnectionLabel(connectionAllWhitespace), null);
  });

  it("respects precedence order: name > email > displayName", () => {
    const allPresent: ConnectionItem = {
      id: "conn-8",
      name: "Name",
      email: "email@example.com",
      displayName: "Display",
    };
    assert.strictEqual(getConnectionLabel(allPresent), "Name");

    const emailAndDisplayNameOnly: ConnectionItem = {
      id: "conn-9",
      name: "",
      email: "email@example.com",
      displayName: "Display",
    };
    assert.strictEqual(
      getConnectionLabel(emailAndDisplayNameOnly),
      "email@example.com",
    );

    const displayNameOnly: ConnectionItem = {
      id: "conn-10",
      name: "  ",
      email: "",
      displayName: "Display",
    };
    assert.strictEqual(getConnectionLabel(displayNameOnly), "Display");
  });
});
