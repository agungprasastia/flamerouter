import { NextResponse } from "next/server";
import { getApiKeys, createApiKey } from "@/lib/localDb";
import { getConsistentMachineId } from "@/shared/utils/machineId";

export const dynamic = "force-dynamic";

let defaultKeyProvisionPromise = null;

// GET /api/keys - List API keys
export async function GET() {
  try {
    const keys = await getApiKeys();
    return NextResponse.json({ keys });
  } catch (error) {
    console.log("Error fetching keys:", error);
    return NextResponse.json(
      { error: "Failed to fetch keys" },
      { status: 500 },
    );
  }
}

// POST /api/keys - Create new API key
export async function POST(request) {
  try {
    const body = await request.json();
    const { name } = body;

    if (!name) {
      return NextResponse.json({ error: "Name is required" }, { status: 400 });
    }

    // First-load provisioning can race under React Strict Mode or two tabs.
    // Reuse existing bootstrap key instead of creating duplicate rows.
    if (name === "Default Key") {
      if (defaultKeyProvisionPromise) return defaultKeyProvisionPromise;
      defaultKeyProvisionPromise = (async () => {
        const existing = (await getApiKeys()).find((key) => key.name === name);
        if (existing) {
          return NextResponse.json(
            {
              key: existing.key,
              name: existing.name,
              id: existing.id,
              machineId: existing.machineId,
            },
            { status: 200 },
          );
        }

        const machineId = await getConsistentMachineId();
        const apiKey = await createApiKey(name, machineId);
        return NextResponse.json(
          {
            key: apiKey.key,
            name: apiKey.name,
            id: apiKey.id,
            machineId: apiKey.machineId,
          },
          { status: 201 },
        );
      })();
      try {
        return await defaultKeyProvisionPromise;
      } finally {
        defaultKeyProvisionPromise = null;
      }
    }

    // Always get machineId from server
    const machineId = await getConsistentMachineId();
    const apiKey = await createApiKey(name, machineId);

    return NextResponse.json(
      {
        key: apiKey.key,
        name: apiKey.name,
        id: apiKey.id,
        machineId: apiKey.machineId,
      },
      { status: 201 },
    );
  } catch (error) {
    console.log("Error creating key:", error);
    return NextResponse.json(
      { error: "Failed to create key" },
      { status: 500 },
    );
  }
}
