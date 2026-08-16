export const ZED_CLIENT_ID = "client_01J...";
export function buildZedHeaders(t: string): Record<string, string> { return { Authorization: 'Bearer ' + t }; }

const zed = {
  config: {},
  flowType: "authorization_code",
  buildAuthUrl: () => "",
  exchangeToken: async () => ({}),
  mapTokens: (tokens: Record<string, unknown>) => tokens,
};

export default zed;
