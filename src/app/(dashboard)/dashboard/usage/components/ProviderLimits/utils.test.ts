import { describe, it, expect } from "vitest";
import { getConnectionLabel, ConnectionItem, calculatePercentage } from "./utils";

describe("getConnectionLabel", () => {
  it("returns trimmed name when name is present", () => {
    const connection: ConnectionItem = {
      id: "conn-1",
      name: "  Primary Name  ",
      email: "user@example.com",
      displayName: "Display Name",
    };
    expect(getConnectionLabel(connection)).toBe("Primary Name");
  });

  it("falls back to trimmed email when name is missing or whitespace", () => {
    const connectionWithNoName: ConnectionItem = {
      id: "conn-2",
      email: "  user@example.com  ",
      displayName: "Display Name",
    };
    expect(getConnectionLabel(connectionWithNoName)).toBe("user@example.com");

    const connectionWithWhitespaceName: ConnectionItem = {
      id: "conn-3",
      name: "   ",
      email: "  user@example.com  ",
      displayName: "Display Name",
    };
    expect(getConnectionLabel(connectionWithWhitespaceName)).toBe("user@example.com");
  });

  it("falls back to trimmed displayName when name and email are missing or whitespace", () => {
    const connectionWithNoNameOrEmail: ConnectionItem = {
      id: "conn-4",
      displayName: "  Display Name  ",
    };
    expect(getConnectionLabel(connectionWithNoNameOrEmail)).toBe("Display Name");

    const connectionWithWhitespaceNameAndEmail: ConnectionItem = {
      id: "conn-5",
      name: "   ",
      email: "\t\n ",
      displayName: "  Display Name  ",
    };
    expect(getConnectionLabel(connectionWithWhitespaceNameAndEmail)).toBe("Display Name");
  });

  it("returns null when name, email, and displayName are missing, empty, or whitespace", () => {
    const connectionEmptyObj: ConnectionItem = {
      id: "conn-6",
    };
    expect(getConnectionLabel(connectionEmptyObj)).toBeNull();

    const connectionAllWhitespace: ConnectionItem = {
      id: "conn-7",
      name: "  ",
      email: "   ",
      displayName: "\t",
    };
    expect(getConnectionLabel(connectionAllWhitespace)).toBeNull();
  });

  it("respects precedence order: name > email > displayName", () => {
    const allPresent: ConnectionItem = {
      id: "conn-8",
      name: "Name",
      email: "email@example.com",
      displayName: "Display",
    };
    expect(getConnectionLabel(allPresent)).toBe("Name");

    const emailAndDisplayNameOnly: ConnectionItem = {
      id: "conn-9",
      name: "",
      email: "email@example.com",
      displayName: "Display",
    };
    expect(getConnectionLabel(emailAndDisplayNameOnly)).toBe("email@example.com");

    const displayNameOnly: ConnectionItem = {
      id: "conn-10",
      name: "  ",
      email: "",
      displayName: "Display",
    };
    expect(getConnectionLabel(displayNameOnly)).toBe("Display");
  });
});

describe("calculatePercentage", () => {
  it("returns 0 when total is 0 or negative", () => {
    expect(calculatePercentage(10, 0)).toBe(0);
    expect(calculatePercentage(0, 0)).toBe(0);
    expect(calculatePercentage(10, -5)).toBe(0);
  });

  it("returns 100 when used is 0 or negative", () => {
    expect(calculatePercentage(0, 100)).toBe(100);
    expect(calculatePercentage(-10, 100)).toBe(100);
  });

  it("returns 0 when used is equal to or greater than total", () => {
    expect(calculatePercentage(100, 100)).toBe(0);
    expect(calculatePercentage(150, 100)).toBe(0);
  });

  it("calculates remaining percentage correctly and rounds to nearest integer", () => {
    expect(calculatePercentage(25, 100)).toBe(75);
    expect(calculatePercentage(50, 100)).toBe(50);
    expect(calculatePercentage(1, 3)).toBe(67);
    expect(calculatePercentage(2, 3)).toBe(33);
  });
});
