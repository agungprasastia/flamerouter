export interface CustomModelItem {
  id: string;
  name?: string;
  providerAlias?: string;
  kind?: string;
  type?: string;
  [key: string]: unknown;
}

export interface BuiltInModelItem {
  id: string;
  name?: string;
  [key: string]: unknown;
}

export interface CustomModelRow {
  id: string;
  name?: string;
  alias?: string;
  fullModel: string;
  source: "custom" | "legacyAlias";
  type: string;
}

export interface GetProviderCustomModelRowsParams {
  customModels?: CustomModelItem[];
  modelAliases?: Record<string, string>;
  providerAlias: string;
  builtInModels?: BuiltInModelItem[];
  type?: string | null;
  includeLegacyAliases?: boolean;
}

function modelType(model?: CustomModelItem | null): string {
  return model?.kind || model?.type || "llm";
}

export function getProviderCustomModelRows({
  customModels = [],
  modelAliases = {},
  providerAlias,
  builtInModels = [],
  type = "llm",
  includeLegacyAliases = true,
}: GetProviderCustomModelRowsParams): CustomModelRow[] {
  const builtInIds = new Set(builtInModels.map((model) => model.id));
  const seenFullModels = new Set<string>();
  const rows: CustomModelRow[] = [];

  for (const model of customModels) {
    if (!model?.id || model.providerAlias !== providerAlias) continue;
    const rowType = modelType(model);
    if (type && rowType !== type) continue;
    if (builtInIds.has(model.id)) continue;

    const fullModel = `${providerAlias}/${model.id}`;
    if (seenFullModels.has(fullModel)) continue;
    seenFullModels.add(fullModel);
    rows.push({
      id: model.id,
      name: model.name || model.id,
      fullModel,
      source: "custom",
      type: rowType,
    });
  }

  if (!includeLegacyAliases) return rows;

  const prefix = `${providerAlias}/`;
  for (const [alias, fullModel] of Object.entries(modelAliases || {})) {
    if (typeof fullModel !== "string" || !fullModel.startsWith(prefix))
      continue;
    const id = fullModel.slice(prefix.length);
    if (!id || builtInIds.has(id) || seenFullModels.has(fullModel)) continue;

    seenFullModels.add(fullModel);
    rows.push({
      id,
      alias,
      fullModel,
      source: "legacyAlias",
      type: type || "llm",
    });
  }

  return rows;
}
