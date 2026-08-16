import { CURSOR_CONFIG } from "../constants/oauth";

const cursor = {
  config: CURSOR_CONFIG,
  flowType: "import_token",
  mapTokens: (tokens: Record<string, unknown>) => ({
    accessToken: tokens.accessToken,
    refreshToken: null, // Cursor doesn't have public refresh endpoint
    expiresIn: tokens.expiresIn || 86400,
    providerSpecificData: {
      machineId: tokens.machineId,
      authMethod: "imported",
    },
  }),
};

export default cursor;
