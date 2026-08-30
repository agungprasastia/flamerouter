import { describe, it, expect } from "vitest";
import {
  getConnectionsPageRange,
  type Pagination,
  getConnectionLabel,
  type ConnectionItem,
  calculatePercentage,
  formatResetTime,
} from "./utils";

describe("getConnectionsPageRange", () => {
  it("returns { start: 0, end: 0 } when total is 0", () => {
    const pagination: Pagination = {
      page: 1,
      pageSize: 20,
      total: 0,
      totalPages: 0,
    };
    expect(getConnectionsPageRange(pagination)).toEqual({
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
    expect(getConnectionsPageRange(pagination)).toEqual({
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
    expect(getConnectionsPageRange(pagination)).toEqual({
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
    expect(getConnectionsPageRange(pagination)).toEqual({
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
    expect(getConnectionsPageRange(pagination)).toEqual({
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
    expect(getConnectionsPageRange(pagination)).toEqual({
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
    expect(getConnectionLabel(connectionWithWhitespaceName)).toBe(
      "user@example.com",
    );
  });

  it("falls back to trimmed displayName when name and email are missing or whitespace", () => {
    const connectionWithNoNameOrEmail: ConnectionItem = {
      id: "conn-4",
      displayName: "  Display Name  ",
    };
    expect(getConnectionLabel(connectionWithNoNameOrEmail)).toBe(
      "Display Name",
    );

    const connectionWithWhitespaceNameAndEmail: ConnectionItem = {
      id: "conn-5",
      name: "   ",
      email: "\t\n ",
      displayName: "  Display Name  ",
    };
    expect(getConnectionLabel(connectionWithWhitespaceNameAndEmail)).toBe(
      "Display Name",
    );
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
    expect(getConnectionLabel(emailAndDisplayNameOnly)).toBe(
      "email@example.com",
    );

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

describe("formatResetTime", () => {
  describe("falsy and invalid inputs", () => {
    it("returns '-' for null", () => {
      expect(formatResetTime(null)).toBe("-");
    });

    it("returns '-' for undefined", () => {
      expect(formatResetTime(undefined)).toBe("-");
    });

    it("returns '-' for empty string", () => {
      expect(formatResetTime("")).toBe("-");
    });

    it("returns '-' for 0", () => {
      expect(formatResetTime(0)).toBe("-");
    });

    it("returns '-' for invalid date string", () => {
      expect(formatResetTime("invalid-date-string")).toBe("-");
    });
  });

  describe("error catching block", () => {
    it("catches thrown error when input throws during conversion and returns '-'", () => {
      // Symbol throws TypeError when passed to new Date(Symbol()) or converted to primitive
      const throwOnConvert = Symbol("invalid-date-symbol") as unknown as string;
      expect(formatResetTime(throwOnConvert)).toBe("-");
    });

    it("catches thrown error when object throws in valueOf and returns '-'", () => {
      const throwingObj = {
        valueOf() {
          throw new Error("Date parsing error");
        },
      } as unknown as string;
      expect(formatResetTime(throwingObj)).toBe("-");
    });
  });

  describe("past and current dates", () => {
    it("returns '-' for past Date object", () => {
      const pastDate = new Date(Date.now() - 1000 * 60 * 5); // 5 mins ago
      expect(formatResetTime(pastDate)).toBe("-");
    });

    it("returns '-' for current date or exact past timestamp", () => {
      const now = new Date(Date.now());
      expect(formatResetTime(now)).toBe("-");
    });
  });

  describe("future reset times", () => {
    it("formats minutes (< 60 minutes)", () => {
      const futureDate = new Date(Date.now() + 15 * 60 * 1000); // 15 mins ahead
      expect(formatResetTime(futureDate)).toBe("15m");
    });

    it("formats hours and minutes (< 24 hours)", () => {
      const futureMs = (4 * 60 + 40) * 60 * 1000; // 4 hours 40 minutes ahead
      const futureDate = new Date(Date.now() + futureMs);
      expect(formatResetTime(futureDate)).toBe("4h 40m");
    });

    it("formats days, hours, and minutes (>= 24 hours)", () => {
      const futureMs = (2 * 24 * 60 + 5 * 60 + 30) * 60 * 1000; // 2d 5h 30m ahead
      const futureDate = new Date(Date.now() + futureMs);
      expect(formatResetTime(futureDate)).toBe("2d 5h 30m");
    });
  });

  describe("different date input types", () => {
    it("accepts ISO string format", () => {
      const futureDateStr = new Date(Date.now() + 30 * 60 * 1000).toISOString();
      expect(formatResetTime(futureDateStr)).toBe("30m");
    });

    it("accepts numeric timestamp format", () => {
      const futureTimestamp = Date.now() + 45 * 60 * 1000;
      expect(formatResetTime(futureTimestamp)).toBe("45m");
    });

    it("accepts Date instance", () => {
      const futureDate = new Date(Date.now() + 20 * 60 * 1000);
      expect(formatResetTime(futureDate)).toBe("20m");
    });
  });
});
