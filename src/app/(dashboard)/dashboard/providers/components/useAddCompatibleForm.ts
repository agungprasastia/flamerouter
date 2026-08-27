import { useState, useEffect } from "react";

export const VARIANT_CONFIG = {
  openai: {
    title: "Add OpenAI Compatible",
    type: "openai-compatible",
    defaultBaseUrl: "https://api.openai.com/v1",
    namePlaceholder: "OpenAI Compatible (Prod)",
    prefixPlaceholder: "oc-prod",
    baseUrlHint:
      "Use the base URL (ending in /v1) for your OpenAI-compatible API.",
    modelIdPlaceholder: "e.g. gpt-4, claude-3-opus",
    errorLabel: "OpenAI Compatible",
    hasApiType: true,
  },
  anthropic: {
    title: "Add Anthropic Compatible",
    type: "anthropic-compatible",
    defaultBaseUrl: "https://api.anthropic.com/v1",
    namePlaceholder: "Anthropic Compatible (Prod)",
    prefixPlaceholder: "ac-prod",
    baseUrlHint:
      "Use the base URL (ending in /v1) for your Anthropic-compatible API. The system will append /messages.",
    modelIdPlaceholder: "e.g. claude-3-opus",
    errorLabel: "Anthropic Compatible",
    hasApiType: false,
  },
};

export const API_TYPE_OPTIONS = [
  { value: "chat", label: "Chat Completions" },
  { value: "responses", label: "Responses API" },
];

export interface UseAddCompatibleFormProps {
  variant: "openai" | "anthropic";
  isOpen: boolean;
  onCreated: (node: unknown) => void;
}

export interface ValidationResult {
  valid?: boolean;
  error?: string;
  method?: string;
}

export function useAddCompatibleForm({
  variant,
  isOpen,
  onCreated,
}: UseAddCompatibleFormProps) {
  const config = VARIANT_CONFIG[variant];

  const initialFormData = () => ({
    name: "",
    prefix: "",
    ...(config.hasApiType ? { apiType: "chat" } : {}),
    baseUrl: config.defaultBaseUrl,
  });

  const [formData, setFormData] = useState(initialFormData);
  const [submitting, setSubmitting] = useState(false);
  const [checkKey, setCheckKey] = useState("");
  const [checkModelId, setCheckModelId] = useState("");
  const [validating, setValidating] = useState(false);
  const [validationResult, setValidationResult] =
    useState<ValidationResult | null>(null);

  // openai: reset baseUrl when apiType changes; anthropic: reset checks when opened
  useEffect(() => {
    /* eslint-disable react-hooks/set-state-in-effect */
    if (config.hasApiType) {
      setFormData((prev) => ({ ...prev, baseUrl: config.defaultBaseUrl }));
    } else if (isOpen) {
      setValidationResult(null);
      setCheckKey("");
      setCheckModelId("");
    }
  }, [config.hasApiType, config.defaultBaseUrl, formData.apiType, isOpen]);

  const handleSubmit = async () => {
    if (
      !formData.name.trim() ||
      !formData.prefix.trim() ||
      !formData.baseUrl.trim()
    )
      return;
    setSubmitting(true);
    try {
      const res = await fetch("/api/provider-nodes", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: formData.name,
          prefix: formData.prefix,
          ...(config.hasApiType ? { apiType: formData.apiType } : {}),
          baseUrl: formData.baseUrl,
          type: config.type,
        }),
      });
      const data = await res.json();
      if (res.ok) {
        onCreated(data.node);
        setFormData(initialFormData());
        setCheckKey("");
        setValidationResult(null);
      }
    } catch (error) {
      console.log(`Error creating ${config.errorLabel} node:`, error);
    } finally {
      setSubmitting(false);
    }
  };

  const handleValidate = async () => {
    setValidating(true);
    try {
      const res = await fetch("/api/provider-nodes/validate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          baseUrl: formData.baseUrl,
          apiKey: checkKey,
          type: config.type,
          modelId: checkModelId.trim() || undefined,
        }),
      });
      const data = await res.json();
      setValidationResult(data);
    } catch {
      setValidationResult({ valid: false, error: "Network error" });
    } finally {
      setValidating(false);
    }
  };

  const updateFormField = (field: string, value: string) => {
    setFormData((prev) => ({ ...prev, [field]: value }));
  };

  return {
    config,
    formData,
    updateFormField,
    submitting,
    checkKey,
    setCheckKey,
    checkModelId,
    setCheckModelId,
    validating,
    validationResult,
    handleSubmit,
    handleValidate,
  };
}
