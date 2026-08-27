import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import PricingSettingsPage from "./page";

// Mock next/navigation
vi.mock("next/navigation", () => ({
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    prefetch: vi.fn(),
  }),
}));

// Mock PricingModal component since it's child component
vi.mock("@/shared/components/PricingModal", () => ({
  default: () => <div data-testid="pricing-modal">Pricing Modal</div>,
}));

describe("PricingSettingsPage - loadPricing Error Handling", () => {
  const originalFetch = global.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  it("handles fetch network error gracefully when loading pricing", async () => {
    // Mock fetch to reject with a network error
    global.fetch = vi.fn().mockRejectedValue(new Error("Network Error"));

    render(<PricingSettingsPage />);

    // Initially should show loading state
    expect(screen.getByText("Loading pricing data...")).toBeDefined();

    // After fetch fails, loading state should clear and show the error message
    await waitFor(() => {
      expect(screen.getByText("Failed to load pricing data")).toBeDefined();
    });
  });

  it("handles non-OK HTTP response gracefully when loading pricing", async () => {
    // Mock fetch to return response with ok: false (e.g. 500 status)
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: "Internal Server Error" }),
    });

    render(<PricingSettingsPage />);

    // After loadPricing finishes with response.ok === false
    await waitFor(() => {
      expect(screen.getByText("Failed to load pricing data")).toBeDefined();
    });

    // Loading indicator text for overview should not be present
    expect(screen.queryByText("Loading pricing data...")).toBeNull();
  });

  it("renders pricing data correctly on successful loadPricing", async () => {
    const mockPricing = {
      openai: {
        "gpt-4o": { input: 2.5, output: 10.0 },
      },
      anthropic: {
        "claude-3-5-sonnet": { input: 3.0, output: 15.0 },
      },
    };

    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => mockPricing,
    });

    render(<PricingSettingsPage />);

    await waitFor(() => {
      expect(screen.getByText("OPENAI:")).toBeDefined();
      expect(screen.getByText("ANTHROPIC:")).toBeDefined();
    });
  });
});
