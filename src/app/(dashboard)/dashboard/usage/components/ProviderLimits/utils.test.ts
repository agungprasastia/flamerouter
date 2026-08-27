import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { calculatePercentage } from "./utils";

describe("calculatePercentage", () => {
  it("returns 0 when total is 0 or negative", () => {
    assert.strictEqual(calculatePercentage(10, 0), 0);
    assert.strictEqual(calculatePercentage(0, 0), 0);
    assert.strictEqual(calculatePercentage(10, -5), 0);
  });

  it("returns 100 when used is 0 or negative", () => {
    assert.strictEqual(calculatePercentage(0, 100), 100);
    assert.strictEqual(calculatePercentage(-10, 100), 100);
  });

  it("returns 0 when used is equal to or greater than total", () => {
    assert.strictEqual(calculatePercentage(100, 100), 0);
    assert.strictEqual(calculatePercentage(150, 100), 0);
  });

  it("calculates remaining percentage correctly and rounds to nearest integer", () => {
    assert.strictEqual(calculatePercentage(25, 100), 75);
    assert.strictEqual(calculatePercentage(50, 100), 50);
    assert.strictEqual(calculatePercentage(1, 3), 67);
    assert.strictEqual(calculatePercentage(2, 3), 33);
  });
});
