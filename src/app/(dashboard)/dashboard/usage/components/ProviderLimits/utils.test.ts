import test, { describe } from "node:test";
import assert from "node:assert/strict";
import { formatResetTime } from "./utils";

describe("formatResetTime", () => {
  describe("falsy and invalid inputs", () => {
    test("returns '-' for null", () => {
      assert.equal(formatResetTime(null), "-");
    });

    test("returns '-' for undefined", () => {
      assert.equal(formatResetTime(undefined), "-");
    });

    test("returns '-' for empty string", () => {
      assert.equal(formatResetTime(""), "-");
    });

    test("returns '-' for 0", () => {
      assert.equal(formatResetTime(0), "-");
    });

    test("returns '-' for invalid date string", () => {
      assert.equal(formatResetTime("invalid-date-string"), "-");
    });
  });

  describe("error catching block", () => {
    test("catches thrown error when input throws during conversion and returns '-'", () => {
      // Symbol throws TypeError when passed to new Date(Symbol()) or converted to primitive
      const throwOnConvert = Symbol("invalid-date-symbol") as unknown as string;
      assert.equal(formatResetTime(throwOnConvert), "-");
    });

    test("catches thrown error when object throws in valueOf and returns '-'", () => {
      const throwingObj = {
        valueOf() {
          throw new Error("Date parsing error");
        },
      } as unknown as string;
      assert.equal(formatResetTime(throwingObj), "-");
    });
  });

  describe("past and current dates", () => {
    test("returns '-' for past Date object", () => {
      const pastDate = new Date(Date.now() - 1000 * 60 * 5); // 5 mins ago
      assert.equal(formatResetTime(pastDate), "-");
    });

    test("returns '-' for current date or exact past timestamp", () => {
      const now = new Date(Date.now());
      assert.equal(formatResetTime(now), "-");
    });
  });

  describe("future reset times", () => {
    test("formats minutes (< 60 minutes)", () => {
      const futureDate = new Date(Date.now() + 15 * 60 * 1000); // 15 mins ahead
      assert.equal(formatResetTime(futureDate), "15m");
    });

    test("formats hours and minutes (< 24 hours)", () => {
      const futureMs = (4 * 60 + 40) * 60 * 1000; // 4 hours 40 minutes ahead
      const futureDate = new Date(Date.now() + futureMs);
      assert.equal(formatResetTime(futureDate), "4h 40m");
    });

    test("formats days, hours, and minutes (>= 24 hours)", () => {
      const futureMs = (2 * 24 * 60 + 5 * 60 + 30) * 60 * 1000; // 2d 5h 30m ahead
      const futureDate = new Date(Date.now() + futureMs);
      assert.equal(formatResetTime(futureDate), "2d 5h 30m");
    });
  });

  describe("different date input types", () => {
    test("accepts ISO string format", () => {
      const futureDateStr = new Date(Date.now() + 30 * 60 * 1000).toISOString();
      assert.equal(formatResetTime(futureDateStr), "30m");
    });

    test("accepts numeric timestamp format", () => {
      const futureTimestamp = Date.now() + 45 * 60 * 1000;
      assert.equal(formatResetTime(futureTimestamp), "45m");
    });

    test("accepts Date instance", () => {
      const futureDate = new Date(Date.now() + 20 * 60 * 1000);
      assert.equal(formatResetTime(futureDate), "20m");
    });
  });
});
